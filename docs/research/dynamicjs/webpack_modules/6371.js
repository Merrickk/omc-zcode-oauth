// webpack module 6371
6371:function(t,n,e){"use strict";var r=e(1455),i=e(456).RegExp,a=r(function(){var t=i("a","y");return t.lastIndex=2,null!==t.exec("abcd")}),o=a||r(function(){return!i("a","y").sticky});t.exports={BROKEN_CARET:a||r(function(){var t=i("^r","gy");return t.lastIndex=2,null!==t.exec("str")}),MISSED_STICKY:o,UNSUPPORTED_Y:a}}
