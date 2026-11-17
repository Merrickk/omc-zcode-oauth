package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"runtime"
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
	raw, err := apiCall(sender, "GET", target, "Bearer "+c.JWT, c.Device, billingHeaders(c.JWT), nil)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Plans []struct {
			PlanID       string `json:"plan_id"`
			PlanIDCamel  string `json:"planId"`
			Name         string `json:"name"`
			Description  string `json:"description"`
			Priority     int64  `json:"priority"`
			Entitlements []struct {
				Meter      string  `json:"meter"`
				UnitType   string  `json:"unit_type"`
				ShowName   string  `json:"show_name"`
				GrantUnits float64 `json:"grant_units"`
				Period     string  `json:"period"`
			} `json:"entitlements"`
		} `json:"plans"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil, errors.New("invalid claim preview response")
	}
	plans := make([]claimPlan, 0, len(doc.Plans))
	for _, p := range doc.Plans {
		id := strings.TrimSpace(p.PlanID)
		if id == "" {
			id = strings.TrimSpace(p.PlanIDCamel)
		}
		if id == "" {
			continue
		}
		plan := claimPlan{
			PlanID:      id,
			Name:        strings.TrimSpace(p.Name),
			Description: strings.TrimSpace(p.Description),
			Priority:    p.Priority,
			GrantItems:  []claimGrant{},
			Grants:      []string{},
		}
		for _, e := range p.Entitlements {
			if e.Meter != "model_usage" || e.UnitType != "token" || strings.TrimSpace(e.ShowName) == "" {
				continue
			}
			period := e.Period
			if period == "" {
				period = "one_time"
			}
			plan.GrantItems = append(plan.GrantItems, claimGrant{
				Name:   strings.TrimSpace(e.ShowName),
				Units:  e.GrantUnits,
				Period: period,
			})
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func fetchCaptchaConfig(sender httpSender) (captchaConfig, error) {
	var out captchaConfig
	target := clientConfigsURL + "?" + url.Values{
		"app_version": {getConfig().AppVersion},
		"platform":    {zcodePlatform()},
	}.Encode()
	raw, err := apiCall(sender, "GET", target, "", "", billingHeaders(""), nil)
	if err != nil {
		return out, err
	}
	var doc struct {
		Configs struct {
			Captcha struct {
				Enabled bool   `json:"enabled"`
				Region  string `json:"region"`
				Prefix  string `json:"prefix"`
				SceneID string `json:"sceneId"`
			} `json:"captcha"`
		} `json:"configs"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return out, errors.New("invalid captcha configuration response")
	}
	return captchaConfig{
		Enabled: doc.Configs.Captcha.Enabled,
		Region:  doc.Configs.Captcha.Region,
		Prefix:  doc.Configs.Captcha.Prefix,
		SceneID: doc.Configs.Captcha.SceneID,
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
