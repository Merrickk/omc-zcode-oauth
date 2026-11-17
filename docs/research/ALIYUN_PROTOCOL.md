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

## Follow-up: request encryption fully recovered (this session)

Playing with the two string tables paid off. The old "garbled" key derivation
was because the `ie` table rotation had never been computed. Simulating its
checksum IIFE gives rotation **6**, after which the whole CryptoJS setup decodes:

```
V(462)=AES  V(401)=enc  V(291)=Utf8  V(380)=Base64  V(442)=Hex
V(352)=pad  V(345)=Pkcs7  V(294)=parse  V(326)=decrypt
```

Derived AES parameters (CBC, PKCS7):

```
IV      = Utf8(Base64(Hex("d35db7e39ebbf3d001083105"))) = b"0123456789ABCDEF"
access  = "FqJB6iRNVYdEGpwb"      # Utf8 key for ye()/de()
```

Decrypting the embedded ciphertexts with that key/IV yields the **full key
chain** (all base64 blobs in the loader's `Yt` table):

```
LOG AccessKey : DuaneAprqkYsF3nt1yjK29Bf / DuanemHmyeE6LXCC46sJEDUw5DTlSZ
Yt[REQ]       -> 45f8ac1e1de14397   (request Data encryption key)
Yt[RES]       -> 87f879f135f27da7   (response decryption key)
Yt[FLAG]      -> c175a358550d02e2   (inner flag encryption key)
```

`Ne()` (the InitCaptcha builder) is now reproducible end to end:

1. `flag = AES-CBC(flag_key, PLATFORM#appName#sceneId#captcha-front#prefix#region)`
2. `Data = AES-CBC(req_key, appKey#WEB#flag#APP_VERSION#CLOUD#)`
3. POP-sign `{AaduaneId, Version=2020-10-15, Action, Data, SignatureNonce}` with
   the LOG secret over `POST&%2F&` + sorted encoded query.

`aliyun_protocol_probe.py:init_request()` builds this. It produces a well-formed
encrypted `Data` blob.

### Where it still stops

- The LOG AccessKey signs fine (the gateway answers `No Mapping Action Url!`,
  not `InvalidAccessKeyId`), but that key is scoped to the log gateway. The
  second embedded key pair (`111jdk...`/`222aiJ...`) is a placeholder and is
  rejected as `Invalid AccessKeyId` on the device endpoints.
- The device OpenAPI needs its own valid AccessKey/routing that is **not** fully
  static in this loader; it is selected by `Xe()` from `Be[c][f]` using verifyType
  and region, and the endpoint list is populated at runtime.
- Even with a valid InitCaptcha response, `VerifyCaptchaV2/V3` still needs the
  second-stage dynamic JS + FeiLin fingerprint payload, which is versioned and
  behaviour-driven.

So: the crypto layer is fully solved; the remaining blockers are a valid device
AccessKey/route and the anti-bot payload, both of which are outside what a
stable pure-protocol client can own. The conclusion from the main document is
unchanged; no runtime code was altered.
