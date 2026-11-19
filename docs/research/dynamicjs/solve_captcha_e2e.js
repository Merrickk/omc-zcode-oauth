#!/usr/bin/env node
/*
 * End-to-end AliyunCaptcha V3 solver used for protocol debugging.
 * It launches full Chromium over raw CDP, runs traceless verification, solves
 * the interactive slider with YOLO gap detection and a calibrated human-like
 * drag, and prints the first VerifyCaptchaV3 response plus the captured
 * CaptchaVerifyParam. Requires: playwright-core, python3, opencv, numpy,
 * captcha-recognizer, and a full Chromium binary.
 */
const { chromium } = require('playwright-core');
const { spawn } = require('child_process');
const http = require('http');
const fs = require('fs');
const path = require('path');

const here = __dirname;
const PORT = Number(process.env.CDP_PORT || 9341);
const exe = process.env.CHROME_BIN || '/home/merrick/.cache/ms-playwright/chromium-1187/chrome-linux/chrome';
const profile = process.env.CHROME_PROFILE || `/tmp/cdp-solver-${Date.now()}`;
const sceneId = process.env.SCENE_ID || '11xygtvd';
const prefix = process.env.PREFIX || 'no8xfe';
const targetEnv = process.env.TARGET ? Number(process.env.TARGET) : null;
const browserArgs = [
  `--remote-debugging-port=${PORT}`, `--user-data-dir=${profile}`,
  '--no-sandbox', '--disable-dev-shm-usage', '--no-first-run',
  '--no-default-browser-check', '--lang=zh-CN', '--window-size=2560,1440',
  '--disable-blink-features=AutomationControlled',
];
const chrome = spawn(exe, browserArgs, { stdio: 'ignore' });
const getJSON = p => new Promise((res, rej) => http.get({host:'127.0.0.1',port:PORT,path:p}, r => {
  let d=''; r.on('data',c=>d+=c); r.on('end',()=>{try{res(JSON.parse(d))}catch(e){rej(e)}});
}).on('error', rej));
async function waitCDP(){ for(let i=0;i<100;i++){ try{await getJSON('/json/version');return}catch(e){await new Promise(r=>setTimeout(r,250))} } throw Error('CDP timeout'); }
function angle() { return process.env.HEADLESS === '1' ? true : false; }

(async()=>{
 await waitCDP();
 const browser = await chromium.connectOverCDP(`http://127.0.0.1:${PORT}`);
 const ctx = browser.contexts()[0];
 const page = await ctx.newPage();
 await page.setViewportSize({width:2560,height:1440});
 let initResp=null; const verifyResponses=[], verifyPosts=[];
 page.on('request', r => { if (r.url().includes('verify') && r.postData()) verifyPosts.push(r.postData()); });
 page.on('response', async r => {
   const u=r.url();
   if (u.includes('captcha-open.aliyuncs.com')) {
     try { const b=await r.text(); if (b.includes('DeviceConfig')) initResp=b; if (b.includes('VerifyCode')) verifyResponses.push(b); } catch(e){}
   }
 });
 await page.addInitScript(() => {
   Object.defineProperty(navigator,'webdriver',{get:()=>undefined});
   Object.defineProperty(navigator,'languages',{get:()=>['zh-CN','zh','en']});
 });
 await page.goto('https://example.com/', { waitUntil:'domcontentloaded' }).catch(()=>{});
 await page.setContent('<html><body style="margin:0;background:#fff"><div id="holder" style="width:340px;height:60px;margin:180px;background:#eee"></div><button id="cap-btn" style="width:120px;height:40px;margin:180px">verify</button><div style="height:900px"></div></body></html>');
 await page.addScriptTag({ content: fs.readFileSync(path.join(here,'aliyunCaptcha.js'),'utf8') });
 await page.evaluate((cfg) => new Promise(resolve => {
   let done=false; const fin=()=>{if(!done){done=true;resolve()}};
   window.AliyunCaptchaConfig = { region: cfg.region, prefix: cfg.prefix };
   window.initAliyunCaptcha({
     SceneId: cfg.sceneId, mode:'popup', element:'#holder', button:'#cap-btn', showErrorTip:false,
     getInstance(i){ window.__inst=i; setTimeout(()=>{ try{i.startTracelessVerification()}catch(e){}; setTimeout(()=>{const b=document.querySelector('#cap-btn'); if(b)b.click()}, 7000) }, 50); },
     success: fin, fail: fin, onError: fin,
   });
   setTimeout(fin, 18000);
 }), { region:'cn', prefix, sceneId });

 // Let the fallback interactive captcha render.
 await page.waitForTimeout(2500);
 let st = await page.evaluate(() => {
   const img=document.querySelector('#aliyunCaptcha-img'), pz=document.querySelector('#aliyunCaptcha-puzzle'), sl=document.querySelector('#aliyunCaptcha-sliding-slider');
   const du=u=>u&&u.startsWith('data:image')?u.split(',')[1]:null;
   return {img:du(img&&img.src), pz:du(pz&&pz.src), slider:sl?sl.getBoundingClientRect().toJSON():null};
 });
 if (!st.img || !st.pz || !st.slider) {
   // Re-run the widget once: the first traceless attempt sometimes leaves no slider.
   await page.evaluate((cfg) => new Promise(resolve => {
     let done=false; const fin=()=>{if(!done){done=true;resolve()}};
     window.initAliyunCaptcha({SceneId:cfg.sceneId,mode:'popup',element:'#holder',button:'#cap-btn',showErrorTip:false,
       getInstance(i){setTimeout(()=>{const b=document.querySelector('#cap-btn');if(b)b.click()},100)},success:fin,fail:fin,onError:fin});
     setTimeout(fin,8000);
   }), {sceneId});
   await page.waitForTimeout(2500);
   st = await page.evaluate(() => {
     const img=document.querySelector('#aliyunCaptcha-img'), pz=document.querySelector('#aliyunCaptcha-puzzle'), sl=document.querySelector('#aliyunCaptcha-sliding-slider');
     const du=u=>u&&u.startsWith('data:image')?u.split(',')[1]:null;
     return {img:du(img&&img.src), pz:du(pz&&pz.src), slider:sl?sl.getBoundingClientRect().toJSON():null};
   });
 }
 if (!st.img || !st.pz || !st.slider) throw new Error('captcha images/slider not found');
 const bgPath=path.join(here,'.last_bg.png'), pzPath=path.join(here,'.last_pz.png');
 fs.writeFileSync(bgPath,Buffer.from(st.img,'base64')); fs.writeFileSync(pzPath,Buffer.from(st.pz,'base64'));
 const out=require('child_process').execSync(`python3 ${path.join(here,'compute_gap_yolo.py')} ${bgPath} ${pzPath}`).toString();
 console.log(out.trim());
 const gap=Number((out.match(/gap=(\d+)/)||[])[1]||0);
 const want=Math.max(0,Math.min(260,targetEnv||gap));
 const dragM=(-0.076525+Math.sqrt(0.076525*0.076525+4*0.00355125*want))/(2*0.00355125);
 const box=st.slider, sx=box.x+box.width/2, sy=box.y+box.height/2, targetX=box.x+20+dragM;
 for(let i=0;i<20;i++){ await page.mouse.move(100+i*25,150+Math.sin(i/4)*50,{steps:3}); await page.waitForTimeout(35); }
 await page.mouse.move(sx,sy); await page.mouse.down();
 const steps=45;
 for(let i=1;i<=steps;i++){
   const u=i/steps;
   let e = u<0.2 ? 0.5*(u/0.2)*(u/0.2)*0.2 : u<0.75 ? 0.1+0.65*((u-0.2)/0.55) : 0.75+0.25*Math.pow((u-0.75)/0.25,0.6);
   if (u>0.85) e -= 0.015*Math.sin((u-0.85)/0.15*Math.PI);
   await page.mouse.move(sx+(targetX-sx)*e, sy+Math.sin(u*Math.PI*3)*1.5);
   await page.waitForTimeout(12+Math.random()*16);
 }
 await page.mouse.up();
 await page.waitForTimeout(9000);
 const success = verifyResponses.find(r => r.includes('"VerifyCode":"T001"')) || null;
 const result = { sceneId, prefix, gap, target: want, pzLeft: await page.evaluate(()=>{const p=document.querySelector('#aliyunCaptcha-puzzle');return p?p.style.left:null}), verifyPosts, verifyResponses, success, initResp };
 const outFile=process.env.OUT || path.join(here,'.last_solve_result.json');
 fs.writeFileSync(outFile,JSON.stringify(result,null,2));
 console.log(JSON.stringify({gap, target:want, success:!!success, responses:verifyResponses.map(r=>{try{return JSON.parse(r).Result&&JSON.parse(r).Result.VerifyCode}catch(e){return '?'}}), outFile}));
 await browser.close(); chrome.kill('SIGTERM');
 if (!success) process.exitCode=2;
})().catch(e=>{ console.error('ERR', e); chrome.kill('SIGTERM'); process.exit(1); });
