package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Claim flow against the ZCode billing API, mirroring the vendor client:
// preview lists claimable plans, claim spends an Aliyun captcha token that a
// panel-side widget produces, and the activation event report plays the
// client-presence role the official app plays before a claim can succeed.

const (
	billingPreviewURL = zcodeRoot + "/api/v1/zcode-plan/billing/preview"
	billingClaimURL   = zcodeRoot + "/api/v1/zcode-plan/billing/claim"
	balanceURL        = zcodeRoot + "/api/v1/zcode-plan/billing/balance"
	clientConfigsURL  = zcodeRoot + "/api/v1/client/configs"
	eventReportURL    = zcodeRoot + "/api/v1/event/report"
)

type claimGrant struct {
	Name   string  `json:"name"`
	Units  float64 `json:"units"`
	Period string  `json:"period"`
}

type claimPlan struct {
	PlanID      string       `json:"plan_id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Priority    int64        `json:"priority"`
	GrantItems  []claimGrant `json:"grant_items"`
	Grants      []string     `json:"grants"`
}

type captchaConfig struct {
	Enabled bool   `json:"enabled"`
	Region  string `json:"region"`
	Prefix  string `json:"prefix"`
	SceneID string `json:"scene_id"`
}

type claimOutcome struct {
	PlanID      string `json:"plan_id"`
	PlanName    string `json:"plan_name"`
	Claimed     bool   `json:"claimed"`
	Code        string `json:"code,omitempty"`
	Message     string `json:"message,omitempty"`
	NextClaimAt int64  `json:"next_claim_at,omitempty"`
}

// claimError is a vendor rejection with its business code; code 1005 carries
// the plan's end time so a caller knows when the next claim window opens.
type claimError struct {
	Code    string
	Message string
	NextAt  int64
}

func (e *claimError) Error() string { return e.Message }

func claimFailureMessage(code string, msg string) string {
	var base string
	switch code {
	case "1001":
		base = "plan does not exist"
	case "1002":
		base = "the event has ended or the plan is not claimable yet"
	case "1003":
		base = "this plan was already claimed"
	case "1004":
		base = "the account does not meet the claim conditions"
	case "1005":
		base = "daily claim limit reached"
	case "3001":
		base = "invalid claim parameters; refresh and retry"
	case "3007":
		base = "captcha verification failed; retry"
	case "401":
		base = "sign in before claiming"
	default:
		base = "claim failed"
	}
	if msg != "" {
		return base + " (" + msg + ")"
	}
	return base
}

func claimErrorFrom(err error) error {
	var api *apiError
	if !errors.As(err, &api) {
		return err
	}
	e := &claimError{Code: api.Code, Message: claimFailureMessage(api.Code, api.Message)}
	if e.Code == "1005" {
		// The claim window ends with the plan itself.
		e.NextAt = 0
	}
	return e
}

// billingHeaders raises apiCall's minimal header set to what the billing and
// event endpoints expect from the official client.
func billingHeaders(token string) http.Header {
	requestID, err := randomUUID()
	if err != nil {
		requestID = ""
	}
	h := http.Header{
		"Content-Type":        {"application/json"},
		"Accept":              {"application/json"},
		"User-Agent":          {"ZCode/" + getConfig().AppVersion},
		"HTTP-Referer":        {zcodeRoot},
		"X-Title":             {"Z Code@electron"},
		"X-ZCode-App-Version": {getConfig().AppVersion},
		"X-Platform":          {zcodePlatform()},
		"X-Release-Channel":   {"stable"},
		"X-Client-Language":   {clientLanguage()},
		"X-Client-Timezone":   {clientTimezone()},
		"X-Os-Category":       {osCategory()},
		"X-Os-Version":        {osRelease()},
	}
	if requestID != "" {
		h.Set("X-Request-Id", requestID)
	}
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	return h
}

func osCategory() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

// previewPlans lists what the account can claim. The vendor has shipped both a
// wrapped object and a bare array here, and it types numbers as either JSON
// numbers or strings, so every field is read through the tolerant helpers below
// rather than a fixed struct: one mismatched field would otherwise fail the
// whole document and hide a perfectly usable plan list.
func previewPlans(sender httpSender, c credential) ([]claimPlan, error) {
	if c.JWT == "" {
		return nil, errors.New("ZCode sign-in required")
	}
	if exp := jwtExpiry(c.JWT); exp > 0 && time.Now().Unix() >= exp {
		return nil, errors.New("ZCode sign-in expired; sign in again")
	}
	target := billingPreviewURL + "?" + url.Values{
		"app_version": {getConfig().AppVersion},
		"platform":    {zcodePlatform()},
	}.Encode()
	data, body, err := apiCallEnvelope(sender, "GET", target, "Bearer "+c.JWT, c.Device, billingHeaders(c.JWT), nil)
	if err != nil {
		return nil, err
	}
	return plansFromRaw(payloadOrEnvelope(data, body))
}

func plansFromRaw(raw json.RawMessage) ([]claimPlan, error) {
	entries, err := planEntries(raw)
	if err != nil {
		return nil, err
	}
	plans := make([]claimPlan, 0, len(entries))
	for _, entry := range entries {
		id := firstNonEmpty(rawText(entry["plan_id"]), rawText(entry["planId"]))
		if id == "" {
			continue
		}
		plan := claimPlan{
			PlanID:      id,
			Name:        rawText(entry["name"]),
			Description: rawText(entry["description"]),
			Priority:    rawInt(entry["priority"]),
			GrantItems:  []claimGrant{},
			Grants:      []string{},
		}
		for _, item := range rawList(entry["entitlements"]) {
			if !strings.EqualFold(rawText(item["meter"]), "model_usage") {
				continue
			}
			if !strings.EqualFold(rawText(item["unit_type"]), "token") && !strings.EqualFold(rawText(item["unitType"]), "token") {
				continue
			}
			name := firstNonEmpty(rawText(item["show_name"]), rawText(item["showName"]))
			if name == "" {
				continue
			}
			period := rawText(item["period"])
			if period == "" {
				period = "one_time"
			}
			plan.GrantItems = append(plan.GrantItems, claimGrant{
				Name:   name,
				Units:  rawFloat(firstNonNil(item["grant_units"], item["grantUnits"])),
				Period: period,
			})
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// planEntries finds the claimable plans in a preview document. The payload has
// been seen inside "data", inside "result", and at the top level, so the search
// is structural rather than a fixed path.
func planEntries(raw json.RawMessage) ([]map[string]json.RawMessage, error) {
	if entries := planListAt(raw); entries != nil {
		return entries, nil
	}
	return nil, fmt.Errorf("invalid claim preview response: %s", rawSnippet(raw))
}

// payloadOrEnvelope prefers a non-empty "data" payload and falls back to the
// whole response when the endpoint answered without one.
func payloadOrEnvelope(data, body json.RawMessage) json.RawMessage {
	if text := strings.TrimSpace(string(data)); text != "" && text != "null" {
		return data
	}
	return body
}

func planListAt(raw json.RawMessage) []map[string]json.RawMessage {
	if text := strings.TrimSpace(string(raw)); text == "" || text == "null" {
		return nil
	}
	if list := objectList(raw); list != nil {
		return list
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return nil
	}
	for _, key := range []string{"plans", "plan_list", "planList", "items", "list"} {
		if list := objectList(object[key]); list != nil {
			return list
		}
	}
	for _, key := range []string{"data", "result", "payload"} {
		if nested, ok := object[key]; ok {
			if list := planListAt(nested); list != nil {
				return list
			}
		}
	}
	return nil
}

func objectList(raw json.RawMessage) []map[string]json.RawMessage {
	var list []map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &list) != nil {
		return nil
	}
	return list
}

func rawList(raw json.RawMessage) []map[string]json.RawMessage {
	return objectList(raw)
}

// rawText returns a trimmed string for a JSON string, number or bool field.
func rawText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return strings.TrimSpace(n.String())
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		if b {
			return "true"
		}
		return "false"
	}
	return ""
}

// rawFloat accepts a JSON number or a numeric string.
func rawFloat(raw json.RawMessage) float64 {
	if text := rawText(raw); text != "" {
		if value, err := strconv.ParseFloat(text, 64); err == nil {
			return value
		}
	}
	return 0
}

func rawInt(raw json.RawMessage) int64 {
	return int64(rawFloat(raw))
}

func rawBool(raw json.RawMessage) bool {
	if text := rawText(raw); text != "" {
		value, err := strconv.ParseBool(text)
		return err == nil && value
	}
	return false
}

func firstNonNil(values ...json.RawMessage) json.RawMessage {
	for _, value := range values {
		if len(value) > 0 && string(value) != "null" {
			return value
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// rawSnippet keeps an unparsable upstream body short and on one line so the
// caller can see what the vendor actually answered.
func rawSnippet(raw json.RawMessage) string {
	text := strings.Join(strings.Fields(string(raw)), " ")
	if len(text) > 200 {
		text = text[:200] + "…"
	}
	if text == "" {
		text = "empty body"
	}
	return text
}

// previewPlansDocument parses one preview document; the senders below keep the
// parsing rules testable without a fake host.
func previewPlansDocument(raw []byte) ([]claimPlan, error) {
	return plansFromRaw(json.RawMessage(raw))
}

func captchaConfigDocument(raw []byte) (captchaConfig, error) {
	return captchaConfigFromRaw(json.RawMessage(raw))
}

func fetchCaptchaConfig(sender httpSender) (captchaConfig, error) {
	var out captchaConfig
	target := clientConfigsURL + "?" + url.Values{
		"app_version": {getConfig().AppVersion},
		"platform":    {zcodePlatform()},
	}.Encode()
	data, body, err := apiCallEnvelope(sender, "GET", target, "", "", billingHeaders(""), nil)
	if err != nil {
		return out, err
	}
	return captchaConfigFromRaw(payloadOrEnvelope(data, body))
}

func captchaConfigFromRaw(raw json.RawMessage) (captchaConfig, error) {
	var out captchaConfig
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return out, fmt.Errorf("invalid captcha configuration response: %s", rawSnippet(raw))
	}
	var configs map[string]json.RawMessage
	_ = json.Unmarshal(doc["configs"], &configs)
	var captcha map[string]json.RawMessage
	_ = json.Unmarshal(configs["captcha"], &captcha)
	if captcha == nil {
		// A configuration without the block is a valid answer: the feature is off.
		return out, nil
	}
	return captchaConfig{
		Enabled: rawBool(captcha["enabled"]),
		Region:  rawText(captcha["region"]),
		Prefix:  rawText(captcha["prefix"]),
		SceneID: firstNonEmpty(rawText(captcha["sceneId"]), rawText(captcha["scene_id"])),
	}, nil
}

func submitClaim(sender httpSender, c credential, planID, captchaParam, captchaRegion string) (claimOutcome, error) {
	out := claimOutcome{PlanID: planID, Claimed: false}
	if c.JWT == "" {
		return out, errors.New("ZCode sign-in required")
	}
	if exp := jwtExpiry(c.JWT); exp > 0 && time.Now().Unix() >= exp {
		return out, errors.New("ZCode sign-in expired; sign in again")
	}
	if strings.TrimSpace(captchaParam) == "" {
		return out, errors.New("captcha verification parameter is required")
	}
	headers := billingHeaders(c.JWT)
	headers.Set("X-Aliyun-Captcha-Verify-Param", strings.TrimSpace(captchaParam))
	if strings.TrimSpace(captchaRegion) != "" {
		headers.Set("X-Aliyun-Captcha-Verify-Region", strings.TrimSpace(captchaRegion))
	}
	if _, err := apiCall(sender, "POST", billingClaimURL, "Bearer "+c.JWT, c.Device, headers, map[string]string{"plan_id": planID}); err != nil {
		cerr := claimErrorFrom(err)
		if ce, ok := cerr.(*claimError); ok {
			out.Code = ce.Code
			out.Message = ce.Message
			out.NextClaimAt = ce.NextAt
		} else {
			out.Message = cerr.Error()
		}
		return out, err
	}
	out.Claimed = true
	if name := claimPlanName(sender, c, planID); name != "" {
		out.PlanName = name
	}
	return out, nil
}

// claimPlanName resolves a display name from the preview list so the outcome
// can echo what the vendor calls the plan without a second claim request.
func claimPlanName(sender httpSender, c credential, planID string) string {
	plans, err := previewPlans(sender, c)
	if err != nil {
		return ""
	}
	for _, p := range plans {
		if p.PlanID == planID {
			return p.Name
		}
	}
	return ""
}

// reportActivationEvents replays the two presence events the official client
// emits; a plan becomes claimable only after the account looks active.
func reportActivationEvents(sender httpSender, c credential) error {
	if c.JWT == "" {
		return errors.New("ZCode sign-in required")
	}
	uid := c.UID
	if uid == "" {
		return errors.New("account has no user identity")
	}
	device, err := randomUUID()
	if err != nil {
		return err
	}
	traceID, err := randomUUID()
	if err != nil {
		return err
	}
	for _, element := range []string{"app_launch", "app_daily_active"} {
		eventID, err := randomUUID()
		if err != nil {
			return err
		}
		body := map[string]any{
			"event_id":           eventID,
			"client_timezone":    clientTimezone(),
			"client_language":    "zh-CN",
			"element_name":       element,
			"event_region":       "app",
			"event_type":         "view",
			"event_text":         "",
			"event_extra_detail": map[string]any{},
			"user_id":            uid,
			"screen_resolution":  "2560x1440",
			"app_version":        getConfig().AppVersion,
			"device_os_category": osCategory(),
			"device_os_version":  osRelease(),
			"device_mid":         c.Device,
			"mac_id":             "",
			"marketing_params":   "{}",
			"trace_id":           traceID,
		}
		if _, err := apiCall(sender, "POST", eventReportURL, "", device, billingHeaders(""), body); err != nil {
			return err
		}
	}
	return nil
}
