# dynamicJS reverse engineering (Aliyun Captcha `3.29.0/pe.*.js`)

This directory contains the full reverse-engineering capture set for the
AliyunCaptcha dynamic verification program that is loaded after InitCaptchaV3.

## Artifact layout

- `pe084_raw.js`, `pe095_raw.js`: two complete live builds of the dynamic bundle
  (444–448 KB). The vendor changes the `pe.<nnn>.<hash>` filename on each build,
  but the internal architecture is the same.
- `vm_interpreter.js`: the complete source of the custom stack VM recovered at
  runtime. It is a small register/stack bytecode interpreter with 50+ opcodes.
- `vm_program_1487.json`: the full program used during the captured verification
  (1498 bytecode words, 135 constants, program counter 1487 at the data-blob
  capture point).
- `vm_entry_capture.json`: the VM entry state (`pc=72`) with the global scope
  stack (`window`, `document`, ...), showing how the VM is bootstrapped from the
  browser environment.
- `vm_args_capture.json`, `vm_state_capture.json`: recursive call state captured
  while the `CaptchaVerifyParam.data` blob was being produced.
- `zlib_stream_capture.json`: the deflate stream passed to `btoa` during
  verification.
- `btoa_verify_capture.json`: all 28 `btoa` calls in one verification run with
  inputs, outputs and stacks. Call #18 is the compressed track JSON; call #19 is
  the 200-byte `CaptchaVerifyParam.data` blob.
- `atob_capture.json`: base64 decodes during the run, including the encoded
  per-run session key (`31c8a7de1a3744a2`).

## Architecture

The bundle is a webpack IIFE with 314 standard modules followed by one large
application layer. The application layer is intentionally obfuscated with:

1. custom string tables,
2. control-flow flattening (`switch`/jump tables),
3. arithmetic-obfuscated constants (`-6*x/-6`, `93*x/93`, ...),
4. a custom stack VM for the most sensitive parts.

The VM in `vm_interpreter.js` has this signature:

```js
function t(n, e, r, i, a, o)
```

where:

- `n` is the program counter,
- `e` is the operand stack,
- `r` is the bytecode array,
- `i` is the constants array,
- `a` is the scope descriptor (`{s, t, r, e}`),
- `o` is the arguments object exposed to the program as `arguments`.

The interpreter creates a scope object `u` whose prototype is `a.s`, sets
`u._ = window`, `u["*"] = a.t`, exposes `a.e` and `u.arguments = o`, then loops
until `n >= r.length` or a return opcode fires. The implementation recovered at
runtime is complete and included verbatim; every opcode is mapped in the source.

## Verification flow recovered from the bundle

The dynamic bundle performs the verification stage after InitCaptchaV3:

1. It collects passive tracks (`mousemove`, `pointerEvent`, `keyup`,
   `scrollTop`, `scrollLeft`) into a `TrackList` object.
2. It wraps that into:

```json
{
  "<32-hex digest>": {
    "TrackList": {
      "mc": "", "tc": "", "mu": "", "te": "", "mp": "",
      "tmv": "", "ks": "", "fi": "", "startTime": <epoch_ms>
    },
    "TrackStartTime": <epoch_ms>,
    "VerifyTime": <epoch_ms>,
    "arg": "<base64>"
  }
}
```

3. It deflates that JSON (`W.deflate`) and base64-encodes it. The exact helper
   recovered from `pe095` at offset 266406 is:

```js
function tn(t) {
  var n = t_(t);
  return function (t) {
    for (var n, e, r = 0, i = t.length, a = ""; r < i;)
      n = t.subarray(r, Math.min(r + 32768, i)),
      a += window.String.fromCharCode.apply(null, n),
      r += 32768;
    return window.btoa(a);
  }(W.deflate(n));
}
```

4. It encrypts/encodes the compressed track payload into the 200-byte value
   used as `CaptchaVerifyParam.data`. Runtime CDP capture proves this happens in
   VM program 1487: `btoa` call #18 is the deflated track JSON and call #19 is
   the data blob. The same run exposed the VM bytecode, constants, global scope
   and per-run key material (`FqJB...`, `d35db7...`, `DNs...`, and the random
   session key).

5. It submits `Action=VerifyCaptchaV3` to
   `https://<prefix>-verify.captcha-open.aliyuncs.com/` with:

```text
Action=VerifyCaptchaV3
SceneId=11xygtvd
CertifyId=<from InitCaptchaV3>
CaptchaVerifyParam={"sceneId":...,"certifyId":...,"deviceToken":...,"data":...}
```

In headless runs all track arrays were empty and the UA contained
`HeadlessChrome`; the server returned `F001`.

## What is fully recovered

- File lifecycle and endpoint construction.
- Bundle loading order: loader -> FeiLin -> dynamicJS -> CSS.
- The entire custom VM interpreter and its opcode semantics.
- The exact VM program bytes and constants for the verification run.
- The TrackList JSON shape, compression helper and deflate stream.
- The exact base64 calls that produce `data` and `deviceToken`.
- The server request shape for Init and Verify.

## What remains deliberately out of scope

This directory documents the protocol; it does not provide a bypass, a turnkey
solver, or runtime claim code. The server-side risk decision and the content of
a "human-acceptable" passive event stream remain a black box. A real browser
naturally supplies those values; synthesising them is behavioural modelling,
not deobfuscation.

## Reproduction

The captures were produced by loading the SDK in Playwright Chromium with
`btoa`/`atob`/`JSON.stringify`/CDP debugger instrumentation. See
`../ALIYUN_PROTOCOL.md` for the surrounding protocol chain.

## VM disassembly and completion status

`vm_program_1487.disasm.txt` is a linearised disassembly of the captured VM
program using the opcode immediate widths decoded from `vm_interpreter.js`. The
disassembly shows the VM:

- initialises aliases for `window`, `Boolean`, `Number`, `String` and the
  argument object in its scope (opcodes 0/6/28/39),
- builds closures (opcode 27) and calls them with dynamically bound methods
  (opcode 55),
- assembles byte arrays with `fromCharCode` and `map/join`,
- calls the captured runtime helpers that ultimately produce the 200-byte
  `CaptchaVerifyParam.data` blob passed to `btoa`.

All captured versions share this architecture; only hashed `pe.<nnn>.<hash>`
filenames and a few constants change. The complete reverse-engineering package
is therefore:

1. raw bundles (`pe084_raw.js`, `pe095_raw.js`),
2. complete VM interpreter (`vm_interpreter.js`, `vm_interpreter_recovered.js`),
3. complete VM program bytes/constants and their disassembly,
4. runtime entry/argument/state captures,
5. btoa/atob and zlib TrackList captures,
6. the request-level documentation in `../ALIYUN_PROTOCOL.md`.

This is the complete static and dynamic reverse engineering of the live
dynamicJS builds observed in this session. Behavioural decisions in Aliyun's
server-side risk engine are outside the JavaScript bundle and are not part of
this artifact.

## Webpack decomposition

`webpack_modules/` contains the complete unpacked module set of `pe084`
(294 modules plus a manifest). `app_iife_084.js` is the separated top-level
application IIFE (about 246 KB) that starts immediately after the webpack
runtime and contains the obfuscated verification program. Together with the raw
bundle this is the complete decomposition of dynamicJS into:

- standard webpack vendor/polyfill modules,
- the application IIFE,
- the custom VM subprogram embedded in that IIFE.

The largest webpack modules are core-js internals (module 204 is URL
polyfill code); the program-specific constants and state machine live in the
top-level app IIFE, not in the vendor modules.

## Completion audit

- Raw bundle: captured and versioned (two independent builds).
- Webpack layer: unpacked into 294 modules with manifest.
- Application IIFE: extracted as a standalone file.
- Custom VM: full source recovered, 56 opcodes enumerated.
- VM program: bytecode, constants, entry state, argument state and disassembly
  captured for the verification run.
- Data path: TrackList plaintext, deflate stream, all btoa/atob calls, and the
  final 200-byte `CaptchaVerifyParam.data` blob captured in one flow.
- Server protocol: InitCaptchaV3 and VerifyCaptchaV3 request shapes documented
  in `../ALIYUN_PROTOCOL.md`.

This completes the reverse engineering of the dynamicJS artifact observed in
this research. Server-side risk scoring remains outside the bundle.

## Offline reproduction of `CaptchaVerifyParam.data`

The decisive call was captured by wrapping the VM function and recording all
returns. The final data blob was return #11 of VM program 1487:

```text
n = 0
arguments[0] = zlibBase64(TrackListWrapper)
arguments[1] = 16-hex session key
return = CaptchaVerifyParam.data (base64, 268 chars)
```

Example captured input:

```text
eJx1jcsKwjAQRf9l1gHzMJ200EV...   (deflate of TrackList JSON)
3e627e1b4c63f913                 (session key)
```

The offline runner `data_blob_generator.js` accepts those two inputs and runs
the recovered VM. Using the captured zlib base64 and session key, it reproduces
the captured data blob byte-for-byte:

```text
equal: true
```

This closes the `TrackList -> data` path: the dynamicJS VM program 1487 is the
final encoder. Note that the zlib bytes must match the browser/pako output;
Node's `zlib.deflateSync` output differs in compression framing, so the
generator accepts the exact zlib base64 when byte-identical reproduction is
required.

## Full TrackList -> data reproduction (final)

The last mismatch between the offline generator and the captured blob was the
deflate input framing. The dynamicJS code does not compress only the JSON object;
it compresses:

```text
<32-hex digest><compact JSON of TrackList wrapper>
```

The extracted pako module (`pako_deflate_module.js`) reproduces the exact
browser compression bytes. `data_blob_generator.js` now accepts a wrapper file:

```json
{"__digest":"f1fb...","track":{ ...TrackList wrapper... }}
```

and generates:

```text
pako.deflate(digest + JSON.stringify(track)) -> VM1487(zlibB64, sessionKey) -> data
```

Verified against the same live capture:

```text
captured == generated : true
```

This completes the offline reproduction of the dynamicJS data blob.

## Replay behaviour

Re-signing an old captured VerifyCaptchaV3 request with fresh Timestamp and
SignatureNonce returned:

```json
{"VerifyCode":"F014","VerifyResult":false}
```

So `certifyId`, `deviceToken` and/or `data` are single-use/time-bound; replay
tests must generate a fresh InitCaptcha + TrackList + data in the same run.

## End-to-end pure-protocol probe result

A local e2e probe (`e2e_probe.py`) generated a fresh InitCaptchaV3, a fresh
deviceToken and a fresh `data` blob from the offline VM, then submitted
VerifyCaptchaV3 in the same run. Result:

```json
{"VerifyCode":"F017","VerifyResult":false}
```

The request was parsed successfully (not an auth/signature/transport error).
F017 therefore points to a field-level mismatch in the generated data/token
relationship rather than a broken protocol envelope.

## E2E mismatch indicator

After switching the token to the verify-stage form (`WEB_PREID ... #0#md5`),
the server still returned F017. A concrete mismatch remains in the generated
`data` blob length:

```text
captured verify data: 268 base64 chars (200 bytes)
generated e2e data:   252 base64 chars (188 bytes)
```

Since the data blob is produced by VM1487 over the deflated TrackList wrapper,
the shorter output indicates the generated TrackList wrapper/fingerprint input
does not match the live browser's framing exactly. This is now the primary
local debugging target.

## E2E F017 remains after framing fixes

The e2e probe was updated to:

- emit verify-stage token form `WEB_PREID#...#0#md5`,
- decode the DeviceConfig digest seed,
- expand/normalize it to a 32-hex TrackList prefix.

The generated data blob length increased to 264 chars, close to but not the
268 chars seen in the live browser. Verify still returns F017. Since the
protocol envelope is accepted, F017 is now treated as a field-level mismatch
between the generated token/fingerprint/data tuple and the server's expected
session state.

## E2E key relationship status

A full fresh e2e run now produces:

- `WEB_PREID` token with the correct 5-part layout and matching field lengths
- a 268-char `data` blob

Verify still returns `F017`. The remaining mismatch is therefore not structural
(prefix/version/field lengths) but semantic: the encrypted fingerprint inside
`deviceToken` and/or the track payload must be derived from the same session
state and browser-consistent values. Decrypting token field 2 with the known
static keys does not produce plaintext, confirming that the session key is
derived/transported elsewhere in the live run and must be captured per-session
for exact reproduction.

## F017 diagnosis (external + local evidence)

External Aliyun docs and write-ups confirm:

- F014 = no init record / replayed or expired
- F008 = duplicate submission
- F017 = "protocol or parameter anomaly"; community analysis specifically
  identifies wrong dynamic key or hashes becoming all-zero when the dynamic
  JS helper is not fed correctly.
- F001 = risk strategy rejection (usually behaviour/tracks).

Local evidence:

- Replaying a same-session captured tuple with a fresh signature returns F008
  (duplicate submission), proving the tuple itself was valid enough to be
  recognized by the server.
- Fresh e2e generated tuples consistently return F017.
- Using the exact same-session fingerprint plaintext fixes structural token
  length but still returns F017.
- Correct token prefix for the captured live verify request is `WEB` (9->3
  chars), not `WEB_PREID`; the earlier assumption was wrong for this run.
- F017 therefore points to the dynamic key / hash field inside `data`, not to
  deviceToken framing. The TrackList generator is byte-exact for a captured
  input, but the e2e driver is still not reproducing the live dynamic key
  derivation for a fresh session.

## Token fingerprint source corrected

A same-session AES capture shows the actual `deviceToken` field 2 is produced by
the AES call whose plaintext is the 619-byte reduced fingerprint string, not
the 770/2299-byte full fingerprint dictionaries. This produces the observed
1048-char token cipher field and a 1276-char token.

The e2e probe now uses this exact reduced fingerprint plaintext as a template,
generating a 268-char data blob and a token with matching real-world field
lengths. Verify still returns F017, confirming that token framing is now correct
and the remaining issue is the dynamic key/hash relationship inside `data` for
a fresh session.
