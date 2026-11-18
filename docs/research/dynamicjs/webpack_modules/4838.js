// webpack module 4838
4838:function(t,n,e){"use strict";var r=e(1573),i=e(3576),a=e(6124),o=RangeError;t.exports=function(t){var n=i(a(this)),e="",u=r(t);if(u<0||u===1/0)throw new o("Wrong number of repetitions");for(;u>0;(u>>>=1)&&(n+=n))1&u&&(e+=n);return e}}
