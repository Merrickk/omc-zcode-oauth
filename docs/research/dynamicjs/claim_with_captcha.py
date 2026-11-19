#!/usr/bin/env python3
"""Use a captured successful CaptchaVerifyParam to call the ZCode billing claim API.

This is a research utility. It reads a local credential JSON (same shape the
plugin uses) and a solver result JSON from solve_captcha_e2e.js. It prints only
non-secret status information.
"""
import base64, json, os, sys, urllib.error, urllib.parse, urllib.request, uuid
from datetime import datetime, timezone

ROOT = "https://zcode.z.ai"
PREVIEW = ROOT + "/api/v1/zcode-plan/billing/preview"
CLAIM = ROOT + "/api/v1/zcode-plan/billing/claim"


def load_json(path):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def headers(jwt, device, app_version="3.16.3", platform="linux-x64"):
    h = {
        "Content-Type": "application/json",
        "Accept": "application/json",
        "User-Agent": "ZCode/" + app_version,
        "HTTP-Referer": ROOT,
        "X-Title": "Z Code@electron",
        "X-ZCode-App-Version": app_version,
        "X-Platform": platform,
        "X-Release-Channel": "stable",
        "X-Client-Language": "en-US",
        "X-Client-Timezone": "Asia/Shanghai",
        "X-Os-Category": "linux",
        "X-Os-Version": os.uname().release,
        "X-Request-Id": str(uuid.uuid4()),
        "Authorization": "Bearer " + jwt,
    }
    if device:
        h["X-Device-Mid"] = device
    return h


def post(url, hdrs, body):
    req = urllib.request.Request(url, data=json.dumps(body).encode(), headers=hdrs, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=25) as r:
            return r.status, r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")


def get(url, hdrs):
    req = urllib.request.Request(url, headers=hdrs, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=25) as r:
            return r.status, r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")


def first_captcha_param(solver_result):
    posts = solver_result.get("verifyPosts") or []
    for raw in posts:
        try:
            fields = urllib.parse.parse_qs(raw)
            cvp = fields.get("CaptchaVerifyParam", [None])[0]
            if cvp:
                return cvp
        except Exception:
            pass
    return None


def main():
    if len(sys.argv) < 3:
        print("usage: claim_with_captcha.py <credential.json> <solver_result.json> [plan_id]", file=sys.stderr)
        return 2
    cred = load_json(sys.argv[1])
    solver = load_json(sys.argv[2])
    plan_id = sys.argv[3] if len(sys.argv) > 3 else None
    jwt = cred.get("jwt") or ""
    device = cred.get("device") or ""
    if not jwt:
        print("credential has no jwt", file=sys.stderr)
        return 2
    captcha = first_captcha_param(solver)
    if not captcha:
        print("solver result has no CaptchaVerifyParam", file=sys.stderr)
        return 2

    hdrs = headers(jwt, device)
    qs = urllib.parse.urlencode({"app_version": "3.16.3", "platform": "linux-x64"})
    status, body = get(PREVIEW + "?" + qs, hdrs)
    print("PREVIEW_STATUS", status)
    try:
        doc = json.loads(body)
    except Exception:
        print("PREVIEW_BODY", body[:500])
        return 1
    plans = (doc.get("data") or {}).get("plans") or doc.get("plans") or []
    print("PLAN_COUNT", len(plans))
    if not plans:
        print("NO_CLAIMABLE_PLANS")
        return 0
    if not plan_id:
        plan_id = plans[0].get("plan_id") or plans[0].get("planId")
    if not plan_id:
        print("cannot resolve plan_id", file=sys.stderr)
        return 2
    print("PLAN_ID", plan_id)

    claim_headers = dict(hdrs)
    claim_headers["X-Aliyun-Captcha-Verify-Param"] = captcha
    claim_headers["X-Aliyun-Captcha-Verify-Region"] = "cn"
    status, body = post(CLAIM, claim_headers, {"plan_id": plan_id})
    print("CLAIM_STATUS", status)
    print("CLAIM_BODY", body[:1000])
    return 0 if 200 <= status < 300 else 1


if __name__ == "__main__":
    raise SystemExit(main())
