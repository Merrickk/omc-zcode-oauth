// Offline runner for the recovered dynamicJS stack VM.
// Usage: NODE_PATH=<dir-with-jsdom> node vm_offline_runner.js
const { JSDOM } = require('jsdom');
const fs = require('fs');
const path = require('path');

const here = __dirname;
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
const args = process.argv.slice(2);
const input = args.length ? args : ['hello', 'x'];
try {
  const out = t(0, [], st.r, st.i, { s: scope, t: w, r: 1 }, input);
  console.log(String(out));
} catch (e) {
  console.error(String(e && e.stack || e));
  process.exit(1);
}
