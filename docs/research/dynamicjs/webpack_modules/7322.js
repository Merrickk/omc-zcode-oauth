// webpack module 7322
7322:function(t,n,e){"use strict";var r=e(7441),i=Math.floor,a=function(t,n){var e=t.length;if(e<8)for(var o,u,s=1;s<e;){for(u=s,o=t[s];u&&n(t[u-1],o)>0;)t[u]=t[--u];u!==s++&&(t[u]=o)}else for(var c=i(e/2),f=a(r(t,0,c),n),l=a(r(t,c),n),h=f.length,p=l.length,d=0,A=0;d<h||A<p;)t[d+A]=d<h&&A<p?0>=n(f[d],l[A])?f[d++]:l[A++]:d<h?f[d++]:l[A++];return t};t.exports=a}
