package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type autoClaimRequest struct {
	Auth          string `json:"auth"`
	PlanID        string `json:"plan_id"`
	Activate      bool   `json:"activate"`
	CaptchaRegion string `json:"captcha_region"`
}

type autoClaimResult struct {
	Auth        string `json:"auth"`
	PlanID      string `json:"plan_id"`
	Solved      bool   `json:"solved"`
	Claimed     bool   `json:"claimed"`
	Code        string `json:"code,omitempty"`
	Message     string `json:"message,omitempty"`
	NextClaimAt int64  `json:"next_claim_at,omitempty"`
}

const fallbackStartPlanID = "zcode-v3-start-plan-wk-1120"

func autoClaim(sender httpSender, dir string, req autoClaimRequest) autoClaimResult {
	result := autoClaimResult{Auth: req.Auth, PlanID: req.PlanID}
	name := strings.TrimSpace(req.Auth)
	c, err := claimCredential(dir, name)
	if err != nil {
		result.Message = err.Error()
		return result
	}
	c, err = credentialWithVirtualDevice(dir, name, c)
	if err != nil {
		result.Message = err.Error()
		return result
	}

	// Always report activation presence BEFORE checking plans, matching zcode-switch's claim_refresh.
	if req.Activate {
		if err := reportActivationEvents(sender, c); err != nil {
			result.Message = "activation failed: " + err.Error()
			return result
		}
	}

	planID := strings.TrimSpace(req.PlanID)
	if planID == "" {
		plans, err := previewPlans(sender, c)
		if err == nil && len(plans) > 0 {
			planID = plans[0].PlanID
		} else {
			// If preview returned no plans (e.g. weekend marketing banner or gateway delay),
			// try known active campaign plan ID fallback.
			planID = fallbackStartPlanID
		}
		result.PlanID = planID
	}

	cfg, err := fetchCaptchaConfig(sender)
	if err != nil {
		result.Message = err.Error()
		return result
	}
	region := strings.TrimSpace(req.CaptchaRegion)
	if region == "" {
		region = cfg.Region
	}
	if region == "" {
		region = "cn"
	}
	param, err := runBuiltinCaptchaSolver(cfg.SceneID, cfg.Prefix, region)
	if err != nil {
		result.Message = err.Error()
		return result
	}
	result.Solved = true

	outcome, err := submitClaim(sender, c, planID, param, region)
	result.Claimed = outcome.Claimed
	result.Code = outcome.Code
	result.Message = outcome.Message
	result.NextClaimAt = outcome.NextClaimAt
	if err != nil && result.Message == "" {
		result.Message = err.Error()
	}
	return result
}

func decodeAutoClaimBody(raw []byte) (autoClaimRequest, error) {
	var body autoClaimRequest
	if len(raw) == 0 {
		return body, errors.New("empty body")
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return body, err
	}
	if strings.TrimSpace(body.Auth) == "" {
		return body, errors.New("auth is required")
	}
	return body, nil
}

var _ = http.MethodPost
