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

## Device-token breakthrough and its hard boundary (latest session)

### Dynamic capture via jsdom succeeded

Running the real `AliyunCaptcha.js` under jsdom (with XHR/fetch/script hooks)
captured the **actual** requests. Every guessed parameter was confirmed:

```
POST https://<prefix>.captcha-open.aliyuncs.com/
  AaduaneId=111jdk439dJJIjd023823201     <-- the "placeholder" is REAL
  Version=2023-03-05
  Action=InitCaptchaV3                    <-- not InitCaptcha
  SceneId=11xygtvd  Language=cn  Mode=popup
  DeviceData=<double-AES blob>           <-- field is DeviceData, not Data
  Signature=<HMAC-SHA1>
```

Then a **pure-protocol InitCaptcha call succeeded** with a freshly signed
request (new timestamp/nonce), no browser at all:

```json
{"Success":true, "CaptchaType":"TRACELESS", "CertifyId":"a9xnFXEwUY",
 "DeviceConfig":"<AES>", "StaticPath":"3.29.0/..."}
```

`CaptchaType: TRACELESS` confirms the verify stage can be puzzle-free for this
scene. Decrypting `DeviceConfig` with the response key
(`87f879f135f27da7`) yields:

```
f16f1b09af791bcb # 1 # <appKey>-h-<ts>-<hash> # 1.5.4/feilin081...js # ...
```

The real scene config is public at `GET /api/v1/client/configs`:
`prefix=no8xfe`, `sceneId=11xygtvd`, `region=cn`, `enabled=true`.

### Exactly where deviceToken comes from

The SDK builds the final `captchaVerifyParam` here (decoded from `fn()`):

```js
{ sceneId, certifyId, deviceToken: deviceToken || hr(), failover: "T" }
```

`hr()` returns `window.um.getToken()` / `window.z_um.getToken()`. That token is
produced by the FeiLin script (`https://g.alicdn.com/captcha-frontend/FeiLin/1.5.4/...`),
which collects browser fingerprints (Canvas/WebGL/fonts/plugins/screen/Worker/...)
and is loaded dynamically after InitCaptcha.

### Why jsdom cannot generate it (attempted this session)

- FeiLin is a 629 KB webpack bundle; evaluating it standalone fails with
  `Cannot read properties of undefined (reading 'AES')` — it depends on the
  CryptoJS instance bundled *inside* AliyunCaptcha.js's webpack modules, which
  is not exposed on `window`.
- With the main SDK loaded first, FeiLin evaluates and exposes
  `window.FEILIN.initFeiLin`, but calling it requires real `Worker`, Canvas 2D
  + WebGL contexts, `matchMedia`, `navigator.plugins`, and produces no `um`
  object without a genuine browser environment. The script actively probes
  for automation and refuses to initialise under stubs.
- The dynamicJS bundle (`3.29.0/pe.097...js`, 448 KB) is separately obfuscated
  and needs the same real DOM. It is the component that would issue
  `VerifyCaptchaV3`.

### Bottom line for deviceToken

The cryptography, signing, routing and InitCaptcha are fully solved in pure
code. The single remaining input — `deviceToken` — is by design a proof that a
real browser was present. It cannot be synthesised from the decrypted
`DeviceConfig`: the `<hash>` there is a session/device id, while `getToken()`
is computed by FeiLin from live environment telemetry and is validated by
Aliyun's server at verify time. Producing it outside a real browser is exactly
the anti-automation boundary this captcha exists to enforce.

$$
\text{pure-protocol claim} = \text{InitCaptcha (SOLVED)} + \text{deviceToken (browser-only)} + \text{Verify (server risk check)}
$$

The practical consequence is unchanged: a real browser (the shipped page, or
a headless Playwright sidecar) is required to mint deviceToken. No runtime code
was altered by this research.

## Encrypted-payload plaintext dumped from a real browser (latest)

Rather than statically guessing, the FeiLin script was run in a real Chromium
via Playwright while hooking `btoa` and `JSON.stringify`. That exposed the
**plaintext immediately before encryption**:

`deviceToken` (the decoded value of `window.um.getToken()`) is:

```
base64( "WEB#<deviceId>#<encrypted-fingerprint>#505#<md5>" )
```

- `deviceId` = `<appKey>-h-<epoch_ms>-<32hex>` — the same id shipped inside the
  InitCaptcha `DeviceConfig` response.
- `505` is a constant build/version tag.
- the final 32-hex is an integrity digest of the preceding fields.

The btoa hook also captured the pre-encryption assembly:

1. A **155-field JSON dictionary** with deliberately meaningless keys
   (`dghfh565`, `asdfasdf`, `sdfgsdf`, ...). Full capture:
   `feilin_fingerprint_fields.json`. Values include:
   - platform/version (`W.10056`, `1.5`, `11`)
   - renderer (`Blink`), OS (`Linux x86_64`), browser (`Chrome Headless`,
     `153.0.8010.12`), UA, language (`en-US`), timezone (`Asia/Shanghai`)
   - WebGL vendor/renderer (`Google Inc.`, `ANGLE ... SwiftShader`), a WebGL
     hash, canvas rendering data URL, dozens of canvas color probes
   - font metrics (`20-442|23-690|...`), screen/viewport (`1440*2560`), dpr,
     color depth, touch/keyboard/plugin flags
   - many `<md5-ish>:width:height` texture hashes, timing values (`GatherCost`),
     and hard-coded 0/1 capability flags.
2. A compact feature-flag string:
   `0#515#1#0#...#1#1#1#11111110111111111111111111`
   (also visible as `dfsf3244` in the dictionary).
3. That 155-field dict is serialised, encrypted/compressed into a raw blob
   (observed: 2188 bytes), base64'd, and embedded as field 3 of the token.

The encryption key for the fingerprint blob is **not** any of the static keys
recovered from the loader (`87f8...`, `45f8...`, `c175...`, appKey, deviceId,
md5 were all tried and failed). It is derived at runtime, presumably bound to
the device/session, which is why the captured first bytes
(`9e6d867c4ee82df0...`) do not decrypt with those constants.

### What "forge the environment" would actually require

This dump turns the question from abstract into concrete. Forging a token
outside a browser would require:

1. Producing all 155 field values self-consistently (the canvas data URL,
   WebGL hashes, font metrics and texture hashes are mutually dependent and
   device-specific; the values seen here come from a real Linux/Chromium
   instance and are not interchangeable).
2. Reproducing the runtime key derivation and the compression/encryption that
   turns the dict into the 2188-byte cipher blob.
3. Keeping pace with the `1.5.4`/`505` build numbers and key schedule as the
   vendor ships new FeiLin versions.
4. Most importantly, having the forged fingerprint agree with the `DeviceData`
   submitted in InitCaptcha and with the server-side risk model that scores
   `VerifyCaptchaV3`.

This is now a bounded reverse-engineering problem with a concrete target: the
155-field plaintext and its cipher blob are captured and reproducible. It is
still a full SDK reimplementation (with anti-automation probes designed to make
exactly this hard), not a small patch — which is why it remains research-only
and no runtime claim code was changed.
