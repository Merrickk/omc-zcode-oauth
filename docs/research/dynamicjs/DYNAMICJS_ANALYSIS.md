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
