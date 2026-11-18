#!/usr/bin/env node
// Fully offline reproduction of the dynamicJS TrackList -> CaptchaVerifyParam.data blob.
// Inputs:
//   argv[2]: JSON file containing the TrackList wrapper object
//   argv[3]: 16-hex session key (or omitted: generate one)
// Output: base64 data blob.

const { JSDOM } = require('jsdom');
const fs = require('fs');
const path = require('path');
const zlib = require('zlib');

const here = __dirname;
const trackFile = process.argv[2];
const sessionKey = process.argv[3] || require('crypto').randomBytes(8).toString('hex');
if (!trackFile) {
  console.error('usage: data_blob_generator.js <track.json> [16-hex-session-key]');
  process.exit(2);
}

const rawInput = fs.readFileSync(trackFile, 'utf8').trim();
let zlibB64;
try {
  const parsed = JSON.parse(rawInput);
  if (typeof parsed === 'string' && parsed.startsWith('eJ')) {
    zlibB64 = parsed;
  } else {
    zlibB64 = zlib.deflateSync(Buffer.from(JSON.stringify(parsed), 'utf8')).toString('base64');
  }
} catch (_) {
  if (!rawInput.startsWith('eJ')) throw new Error('input must be a TrackList JSON or zlib base64 string');
  zlibB64 = rawInput;
}

// Reuse the VM helper extracted from the live dynamicJS bundle.
const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'https://example.com/', runScripts: 'outside-only', pretendToBeVisual: true,
});
const w = dom.window;
global.window = w; global.document = w.document; global.navigator = w.navigator;
const scope = {
  window: w, self: w, document: w.document, String: w.String, Number: w.Number,
  Boolean: w.Boolean, Object: w.Object, Array: w.Array, Math: w.Math, JSON: w.JSON,
  Date: w.Date, RegExp: w.RegExp, Uint8Array: w.Uint8Array, ArrayBuffer: w.ArrayBuffer,
  atob: w.atob.bind(w), btoa: w.btoa.bind(w),
  encodeURIComponent: w.encodeURIComponent.bind(w),
  decodeURIComponent: w.decodeURIComponent.bind(w),
  parseInt: w.parseInt.bind(w), parseFloat: w.parseFloat.bind(w), isNaN: w.isNaN.bind(w),
};
eval(fs.readFileSync(path.join(here, 'vm_interpreter_recovered.js'), 'utf8'));
const st = JSON.parse(fs.readFileSync(path.join(here, 'vm_program_1487.json'), 'utf8'));
const out = t(0, [], st.r, st.i, { s: scope, t: w, r: 1 }, [zlibB64, sessionKey]);
process.stdout.write(String(out) + '\n');
