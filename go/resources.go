package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const pluginStoreID = "omc-zcode-oauth"
const resourcePrefix = "/v0/resource/plugins/" + pluginStoreID
const claimAPIPrefix = "/v0/management/plugins/" + pluginStoreID + "/claim"

// claimGate serializes claim-flow operations per credential file. The vendor
// claim endpoint is idempotent-unfriendly: two racing submissions can burn two
// captcha tokens or trip business code 1003, so a mutex per auth file keeps a
// panel's double-click and an auto-claim loop from racing each other.
var claimGate struct {
	sync.Mutex
	busy map[string]*sync.Mutex
}

func claimLock(name string) func() {
	claimGate.Lock()
	if claimGate.busy == nil {
		claimGate.busy = map[string]*sync.Mutex{}
	}
	mu, ok := claimGate.busy[name]
	if !ok {
		mu = &sync.Mutex{}
		claimGate.busy[name] = mu
	}
	claimGate.Unlock()
	mu.Lock()
	return mu.Unlock
}

// claimAuthID turns a requested credential reference into a safe file name.
// Only exact .json file names inside the auth directory are addressable; the
// whitelist keeps a path from escaping the directory or naming another
// plugin's credential.
func claimAuthID(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name != filepath.Base(name) || !strings.HasSuffix(name, ".json") || strings.ContainsAny(name, "\\/") {
		return "", fmt.Errorf("invalid auth id")
	}
	for _, r := range name[:len(name)-len(".json")] {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("invalid auth id")
	}
	return name, nil
}

// claimCredential reads one zcode credential file from the host auth directory.
func claimCredential(dir, name string) (credential, error) {
	id, err := claimAuthID(name)
	if err != nil {
		return credential{}, err
	}
	if dir == "" {
		return credential{}, fmt.Errorf("auth directory unavailable")
	}
	path := filepath.Join(dir, id)
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return credential{}, fmt.Errorf("credential %s not found", id)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return credential{}, err
	}
	c, err := readCredential(raw)
	if err != nil {
		return credential{}, fmt.Errorf("%s is not a zcode credential", id)
	}
	return c, nil
}

func managementJSON(status int, value any) pluginapi.ManagementResponse {
	body, err := json.Marshal(value)
	if err != nil {
		return managementJSON(http.StatusInternalServerError, map[string]string{"error": "encode failed"})
	}
	return pluginapi.ManagementResponse{
		StatusCode: status,
		Headers:    http.Header{"Content-Type": {"application/json; charset=utf-8"}},
		Body:       body,
	}
}

func managementError(status int, err error) pluginapi.ManagementResponse {
	code := http.StatusBadGateway
	switch err.(type) {
	case *claimError:
		// A vendor business rejection is an expected answer, not a fault.
		code = http.StatusConflict
	case *apiError:
		code = http.StatusBadGateway
		hostLog("warn", "claim request rejected upstream", map[string]any{"error": err.Error()})
	default:
		hostLog("error", "claim request failed", map[string]any{"error": err.Error()})
	}
	if status != 0 {
		code = status
	}
	return managementJSON(code, map[string]string{"error": err.Error()})
}

// claimAPIHandler runs one claim request. A fault inside the handler becomes a
// JSON response instead of an ABI error: the host turns an ABI error into an
// opaque plain-text 502, which hides the reason from whoever clicked.
func claimAPIHandler(req pluginapi.ManagementRequest) (resp pluginapi.ManagementResponse, handled bool) {
	return recoverClaim(req.Path, func() (pluginapi.ManagementResponse, bool) { return handleClaimAPI(req) })
}

// recoverClaim is claimAPIHandler's seam: the recovery contract is tested
// without needing a handler that panics for real.
func recoverClaim(path string, run func() (pluginapi.ManagementResponse, bool)) (resp pluginapi.ManagementResponse, handled bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			hostLog("error", "claim handler panicked", map[string]any{"panic": fmt.Sprint(recovered), "path": path})
			resp = managementJSON(http.StatusInternalServerError, map[string]string{"error": "claim handler failed: " + fmt.Sprint(recovered)})
			handled = true
		}
	}()
	return run()
}

// hostLog writes one line to the host's log when a host callback is available.
// Outside a host (tests, or a resource-only call) it is a no-op.
func hostLog(level, message string, fields map[string]any) {
	_ = hostRPC(pluginabi.MethodHostLog, map[string]any{"level": level, "message": message, "fields": fields}, nil)
}

// claimRoutes registers the authenticated Management API claim endpoints.
// Captcha tokens are single-use and produced by a panel-side widget, so the
// API stays a thin proxy: the panel previews, runs the captcha, and submits
// the token here; nothing auto-fires without a fresh token.
func claimRoutes() []map[string]any {
	desc := "ZCode claim flow."
	return []map[string]any{
		{"method": http.MethodGet, "path": claimAPIPrefix + "/plans", "menu": "", "description": desc},
		{"method": http.MethodPost, "path": claimAPIPrefix + "/claim", "menu": "", "description": desc},
		{"method": http.MethodGet, "path": claimAPIPrefix + "/captcha-config", "menu": "", "description": desc},
		{"method": http.MethodPost, "path": claimAPIPrefix + "/activate", "menu": "", "description": desc},
		{"method": http.MethodGet, "path": claimAPIPrefix + "/ping", "menu": "", "description": desc},
		{"method": http.MethodGet, "path": claimAPIPrefix + "/diagnose", "menu": "", "description": "Raw preview response and device context for debugging."},
		{"method": http.MethodPost, "path": claimAPIPrefix + "/auto", "menu": "", "description": "Solve captcha in-process and claim automatically."},
	}
}

// claimSender is a test seam: production always sends requests through the
// host's HTTP callback; tests swap in a fake sender to script vendor replies.
var claimSender func() httpSender = func() httpSender { return cpaSender{} }

// handleClaimAPI dispatches one authenticated claim-flow request.
func handleClaimAPI(req pluginapi.ManagementRequest) (pluginapi.ManagementResponse, bool) {
	path := strings.TrimSuffix(req.Path, "/")
	if !strings.HasPrefix(path, claimAPIPrefix) {
		return pluginapi.ManagementResponse{}, false
	}
	dir := riskState.authDir
	sub := strings.TrimPrefix(path, claimAPIPrefix)
	switch {
	case sub == "/ping" && req.Method == http.MethodGet:
		// Vendor-free self check: it proves the management route itself works,
		// so a later failure can be blamed on the outbound call instead.
		return managementJSON(http.StatusOK, map[string]any{
			"ok":       true,
			"version":  pluginVersion,
			"auth_dir": dir != "",
		}), true

	case sub == "/plans" && req.Method == http.MethodGet:
		name := req.Query.Get("auth")
		c, err := claimCredential(dir, name)
		if err != nil {
			return managementError(http.StatusBadRequest, err), true
		}
		c, err = credentialWithVirtualDevice(dir, name, c)
		if err != nil {
			return managementError(http.StatusBadRequest, err), true
		}
		unlock := claimLock(name)
		defer unlock()
		// zcode-switch reports presence before previewing claimable plans;
		// without this the vendor often answers with an empty plan list.
		_ = reportActivationEvents(claimSender(), c)
		plans, err := previewPlans(claimSender(), c)
		if err != nil {
			return managementError(0, err), true
		}
		bal, _ := fetchBalance(claimSender(), c)
		return managementJSON(http.StatusOK, map[string]any{
			"auth":         name,
			"plans":        plans,
			"active_plans": bal.Plans,
		}), true

	case sub == "/diagnose" && req.Method == http.MethodGet:
		name := req.Query.Get("auth")
		c, err := claimCredential(dir, name)
		if err != nil {
			return managementError(http.StatusBadRequest, err), true
		}
		c, err = credentialWithVirtualDevice(dir, name, c)
		if err != nil {
			return managementError(http.StatusBadRequest, err), true
		}
		unlock := claimLock(name)
		defer unlock()
		_ = reportActivationEvents(claimSender(), c)
		target := billingPreviewURL + "?" + url.Values{
			"app_version": {getConfig().AppVersion},
			"platform":    {zcodePlatform()},
		}.Encode()
		h := billingHeaders(c.JWT)
		h.Set("X-Device-Mid", c.Device)
		raw, err := claimSender().Do(pluginapi.HTTPRequest{Method: http.MethodGet, URL: target, Headers: h})
		if err != nil {
			return managementError(0, err), true
		}
		plans, parseErr := plansFromRaw(raw.Body)
		return managementJSON(http.StatusOK, map[string]any{
			"auth":        name,
			"device_mid":  c.Device,
			"app_version": getConfig().AppVersion,
			"platform":    zcodePlatform(),
			"status":      raw.StatusCode,
			"raw":         string(raw.Body),
			"plans":       plans,
			"parse_error": fmt.Sprint(parseErr),
		}), true

	case sub == "/captcha-config" && req.Method == http.MethodGet:
		cfg, err := fetchCaptchaConfig(claimSender())
		if err != nil {
			return managementError(0, err), true
		}
		return managementJSON(http.StatusOK, cfg), true

	case sub == "/activate" && req.Method == http.MethodPost:
		name := req.Query.Get("auth")
		c, err := claimCredential(dir, name)
		if err != nil {
			return managementError(http.StatusBadRequest, err), true
		}
		c, err = credentialWithVirtualDevice(dir, name, c)
		if err != nil {
			return managementError(http.StatusBadRequest, err), true
		}
		unlock := claimLock(name)
		defer unlock()
		if err := reportActivationEvents(claimSender(), c); err != nil {
			return managementError(0, err), true
		}
		plans, _ := previewPlans(claimSender(), c)
		bal, _ := fetchBalance(claimSender(), c)
		return managementJSON(http.StatusOK, map[string]any{
			"activated":    true,
			"plans":        plans,
			"active_plans": bal.Plans,
		}), true

	case sub == "/auto" && req.Method == http.MethodPost:
		body, err := decodeAutoClaimBody(req.Body)
		if err != nil {
			return managementJSON(http.StatusBadRequest, map[string]string{"error": err.Error()}), true
		}
		if body.Auth == "" {
			body.Auth = req.Query.Get("auth")
		}
		unlock := claimLock(body.Auth)
		defer unlock()
		out := autoClaim(claimSender(), dir, body)
		return managementJSON(http.StatusOK, out), true

	case sub == "/claim" && req.Method == http.MethodPost:
		var body struct {
			Auth          string `json:"auth"`
			PlanID        string `json:"plan_id"`
			CaptchaParam  string `json:"captcha_param"`
			CaptchaRegion string `json:"captcha_region"`
		}
		if len(req.Body) == 0 || json.Unmarshal(req.Body, &body) != nil || strings.TrimSpace(body.PlanID) == "" {
			return managementJSON(http.StatusBadRequest, map[string]string{"error": "plan_id is required"}), true
		}
		name := body.Auth
		if name == "" {
			name = req.Query.Get("auth")
		}
		c, err := claimCredential(dir, name)
		if err != nil {
			return managementError(http.StatusBadRequest, err), true
		}
		unlock := claimLock(name)
		defer unlock()
		outcome, err := submitClaim(claimSender(), c, strings.TrimSpace(body.PlanID), body.CaptchaParam, body.CaptchaRegion)
		if err != nil {
			resp := managementError(0, err)
			// The structured outcome rides alongside the error text so a panel
			// can read the business code and the next-claim timestamp.
			if enriched, jerr := json.Marshal(map[string]any{
				"error":   err.Error(),
				"outcome": outcome,
			}); jerr == nil {
				resp.Body = enriched
			}
			return resp, true
		}
		return managementJSON(http.StatusOK, outcome), true
	}
	return pluginapi.ManagementResponse{StatusCode: http.StatusNotFound, Body: []byte("not found")}, true
}

// publicLogoBaseURL is the published location of the same artwork that the
// resource routes serve. OAuth entry logos use it because a panel reads a
// plugin's logo as a document (the store manifests are URLs too) and only the
// CPA-native resource routes are guaranteed to be served to an unauthenticated
// management client.
const publicLogoBaseURL = "https://raw.githubusercontent.com/omc-community/omc-zcode-oauth/refs/heads/main/assets/"

// resourceAssetURL is the public address of one embedded logo. Only the two
// names registered below are addressable, so the set of URLs the plugin can
// publish stays equal to the set of bytes it ships.
func resourceAssetURL(name string) string {
	switch name {
	case "zai.png", "bigmodel.png":
		return publicLogoBaseURL + name
	default:
		return ""
	}
}

//go:embed assets/zai.png
var zaiLogoPNG []byte

//go:embed assets/bigmodel.png
var bigModelLogoPNG []byte

//go:embed assets/claim.html
var claimPageHTML []byte

// handleManagementResource serves only deployed public bytes. It deliberately
// has no host callback, auth directory, configuration or OAuth-state dependency.
func handleManagementResource(req pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	var body []byte
	contentType := "image/png"
	cacheControl := "public, max-age=86400"
	switch req.Path {
	case resourcePrefix + "/zai.png":
		body = zaiLogoPNG
	case resourcePrefix + "/bigmodel.png":
		body = bigModelLogoPNG
	case resourcePrefix + "/claim.html":
		body = claimPageHTML
		contentType = "text/html; charset=utf-8"
		// The claim page is the captcha entry point; the browser must re-fetch
		// it every time so a repacked plugin never serves a stale page.
		cacheControl = "no-cache"
	case omcResourcePath:
		body = []byte(omcDescriptorJSON)
		contentType = "application/json; charset=utf-8"
		cacheControl = "no-cache"
	default:
		return pluginapi.ManagementResponse{StatusCode: http.StatusNotFound, Body: []byte("not found")}, nil
	}
	if req.Method != http.MethodGet {
		return pluginapi.ManagementResponse{
			StatusCode: http.StatusMethodNotAllowed,
			Headers:    http.Header{"Allow": {http.MethodGet}},
			Body:       []byte("method not allowed"),
		}, nil
	}
	return pluginapi.ManagementResponse{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Content-Type":           {contentType},
			"Cache-Control":          {cacheControl},
			"X-Content-Type-Options": {"nosniff"},
		},
		Body: append([]byte(nil), body...),
	}, nil
}
