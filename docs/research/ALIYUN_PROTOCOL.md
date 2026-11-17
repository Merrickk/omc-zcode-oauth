# Aliyun Captcha protocol research (exploratory)

This is a research artifact only. The shipped claim flow is unchanged: it runs
the official AliyunCaptcha.js widget in a browser and submits the resulting
`captchaVerifyParam` through the plugin's Management API. Nothing here is used
at runtime.

## What was decoded

The obfuscated loader used by `zcode-switch` is served from
`https://o.alicdn.com/captcha-frontend/aliyunCaptcha/AliyunCaptcha.js`. Two
string tables are rotated by checksum IIFEs at load time:

- `Nt` (decoder `q`, index base 206, rotation 196)
- `ie` (decoder `ne`, index base 267, rotation 6)

`aliyun_protocol_probe.py` reproduces both rotations and recovers the loader's
constants from the live script:

- OpenAPI version `2023-03-05` (API version for the device service)
- `AaduaneId` / `KEY_ID` and `KEY_SECRET`, recovered by AES-CBC decryption of
  the embedded ciphertexts. Key = `Utf8("FqJB6iRNVYdEGpwb")`, IV =
  `Utf8(Base64(Hex("d35db7e39ebbf3d001083105")))` = ASCII `0123456789ABCDEF`.
- appKey `ab034ec0643f91399eb33e062dc7fae1`, appName `saf-aliyun-com`
- endpoints: `device.captcha-open.aliyuncs.com` (WAF),
  `cloudauth-device.aliyuncs.com` (CN),
  `cloudauth-device.ap-southeast-1.aliyuncs.com` (intl)

Requests are signed with the classic Aliyun POP scheme: `POST&%2F&` plus the
RFC3986-encoded sorted query, HMAC-SHA1 with `<secret>&`, base64.

## What was verified

- The recovered AccessKey is accepted: requests to
  `https://device.captcha-open.aliyuncs.com/` return
  `{"Code":401,"Message":"No Mapping Action Url!"}` instead of
  `InvalidAccessKeyId`. That error is a routing response, not an auth failure,
  so the signature process is correct.
- The action names in the loader are `InitCaptcha`, `InitCaptchaV2`,
  `InitCaptchaV3`, `VerifyCaptchaV2`, `VerifyCaptchaV3` and `UploadLog`.
  Direct calls with the obvious action/version combinations did not map to a
  production route, so the exact action-to-endpoint routing (and the required
  `Data` payload) is not fully recovered.

## Why this is not wired into the claim flow

Even with a valid signature and correct routing, the verify step needs a
payload built from the *second-stage* dynamic JS and the FeiLin device-fingerprint
script. That payload mixes device telemetry, encrypted context, PoW answers and
behavioural data. The vendor's risk engine decides pass/fail server-side; a
synthetically assembled payload is rejected (`F001`-style risk interception) and
the dynamic scripts are versioned/replaced independently of this loader.

As a result, pure-protocol generation of `captchaVerifyParam` is not a stable
or reliable basis for automated claiming. The browser widget remains the
supported path; a headless-browser + trajectory-generation sidecar is the only
practical fully automatic approach, and it must be maintained against the
captcha vendor's changes.

## Reproducing

```sh
python3 docs/research/aliyun_protocol_probe.py /path/to/AliyunCaptcha.js
```
