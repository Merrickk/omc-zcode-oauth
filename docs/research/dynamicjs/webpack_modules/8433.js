// webpack module 8433
8433:function(t,n,e){"use strict";var r=e(456),i=e(7579),a=e(2185),o=function(t){return i.slice(0,t.length)===t};t.exports=o("Bun/")?"BUN":o("Cloudflare-Workers")?"CLOUDFLARE":o("Deno/")?"DENO":o("Node.js/")?"NODE":r.Bun&&"string"==typeof Bun.version?"BUN":r.Deno&&"object"==typeof Deno.version?"DENO":"process"===a(r.process)?"NODE":r.window&&r.document?"BROWSER":"REST"}
