// webpack module 3987
3987:function(t,n,e){"use strict";var r=e(163),i=e(3663),a=e(7732),o=r.aTypedArray,u=r.getTypedArrayConstructor;(0,r.exportTypedArrayMethod)("subarray",function(t,n){var e=o(this),r=e.length,s=a(t,r);return new(u(e))(e.buffer,e.byteOffset+s*e.BYTES_PER_ELEMENT,i((void 0===n?r:a(n,r))-s))})}
