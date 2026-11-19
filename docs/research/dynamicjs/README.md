# Aliyun captcha protocol solver (research)

This directory contains the working end-to-end captcha protocol implementation
recovered during debugging.

## What works

`solve_captcha_e2e.js` is a complete captcha solver:

1. Launches full Chromium over raw CDP (Playwright connects over CDP).
2. Loads the real `AliyunCaptcha.js`.
3. Runs `startTracelessVerification()`, then falls back to the interactive slider.
4. Captures `#aliyunCaptcha-img` and `#aliyunCaptcha-puzzle`.
5. Uses `compute_gap_yolo.py` (YOLO) to locate the gap.
6. Converts desired `piece.left` to mouse travel with the calibrated formula:
   `piece.left = 0.00355125*m² + 0.076525*m + 0.041`.
7. Injects a human-like drag (slow approach, valley hesitation, terminal whip,
   settle wiggle).
8. Records every VerifyCaptchaV3 request/response and detects `T001`.

Verified live result (`solve_success.json`):

```text
responses: T001, F001, T001
success CaptchaVerifyParam length: 1786 / 2998
securityToken present in T001 responses
```

## Files

- `solve_captcha_e2e.js`: end-to-end solver
- `compute_gap_yolo.py`: YOLO gap locator
- `aliyunCaptcha.js`: exact SDK build used during capture
- `solve_success.json`: live successful T001 capture
- `data_blob_generator.js`: offline reproduction of `CaptchaVerifyParam.data`
- `vm_*`: recovered dynamicJS VM and captures
- `DYNAMICJS_ANALYSIS.md`: full reverse-engineering notes

## Requirements

- Node + `playwright-core`
- Full Chromium binary (not headless shell)
- Xvfb on Linux
- Python with `opencv-python`, `numpy`, `captcha-recognizer`

## Usage

```sh
NODE_PATH=/tmp/node_modules xvfb-run -a \
  node docs/research/dynamicjs/solve_captcha_e2e.js
```

The script writes `.last_solve_result.json` (or `$OUT`) containing:

- `verifyPosts`: raw form bodies, including `CaptchaVerifyParam`
- `verifyResponses`: all VerifyCaptchaV3 responses
- `success`: the first T001 response
- `initResp`: InitCaptcha response

## Scope

This is the complete client-side captcha flow. It produces a valid
`CaptchaVerifyParam` that Aliyun accepts (`T001`). The business-side claim
call still requires a real ZCode JWT and is intentionally not embedded here.

## Verification gate

A live rerun of the solver completed with:

```text
YOLO conf=0.954 gap=243
{"gap":243,"target":243,"success":true,"responses":["T001","F001","T001"]}
```

The run wrote `.last_solve_result.json` with a T001 `success` response and the
matched `CaptchaVerifyParam` in `verifyPosts`.

## Business claim bridge

`claim_with_captcha.py` takes a local ZCode credential JSON and a solver result
JSON. It reads the first `CaptchaVerifyParam` from `verifyPosts`, previews
claimable plans, and submits the highest/first plan using the exact billing
headers from the plugin implementation. It prints only non-secret status data.

```sh
python3 docs/research/dynamicjs/claim_with_captcha.py \
  <credential.json> docs/research/dynamicjs/.last_solve_result.json [plan_id]
```

The credential file must contain `jwt` (and optionally `device`) as in the
plugin's credential format.
