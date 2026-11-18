#!/usr/bin/env python3
"""Pure-protocol prototype for Aliyun CaptchaV3.

Everything in this file is reconstructed from captured browser traffic and is
byte-verified against real requests. It has ONE unresolved input: the
behaviour telemetry that the server-side risk model scores. In headless runs
the SDK submitted empty tracks and a HeadlessChrome UA and got F001; a real
browser supplies real telemetry. See ALIYUN_PROTOCOL.md.

This module deliberately does NOT bypass a captcha or claim plans. It only
reproduces the client-side protocol for research/reproduction.
"""
import base64, hashlib, hmac, json, secrets, time, urllib.parse, urllib.request
import uuid
from datetime import datetime, timezone

try:
    from Crypto.Cipher import AES
    from Crypto.Util.Padding import pad
except ImportError:
    raise SystemExit("pip install pycryptodome")

IV = b"0123456789ABCDEF"           # Utf8(Base64(Hex(d35db7e39ebbf3d001083105)))
ACCESS_KEY = "FqJB6iRNVYdEGpwb"      # static key used to ship session keys / device id
REQ_KEY = "45f8ac1e1de14397"        # InitCaptcha DeviceData encryption
FLAG_KEY = "c175a358550d02e2"       # captcha-front flag encryption
APP_KEY = "ab034ec0643f91399eb33e062dc7fae1"
APP_NAME = "saf-captcha"
PLATFORM = "W.10001.c"
APP_VERSION = "W20220202"
AK_ID = "111jdk439dJJIjd023823201"
AK_SECRET = "222aiJodos2938JDdosko2djd82sf0"
ACTION_VERSION = "2023-03-05"
CLOUD_VERSION = "2020-10-15"        # used by the device/log channel

# scene config from GET /api/v1/client/configs (public)
SCENE_ID = "11xygtvd"
PREFIX = "no8xfe"
REGION = "cn"

def aes_b64(key: str, plaintext: str) -> str:
    ct = AES.new(key.encode(), AES.MODE_CBC, IV).encrypt(pad(plaintext.encode(), 16))
    return base64.b64encode(ct).decode()

def new_device_id() -> str:
    return f"{APP_KEY}-h-{int(time.time()*1000)}-{secrets.token_hex(16)}"

def new_session_key() -> str:
    return secrets.token_hex(8)      # 16 hex chars = 16-byte ASCII key

def captcha_flag(prefix=PREFIX, region=REGION, scene=SCENE_ID):
    inner = f"{PLATFORM}#{APP_NAME}#{scene}#captcha-normal#{prefix}#{region}"
    return aes_b64(FLAG_KEY, inner)

def init_device_data(scene=SCENE_ID):
    flag = aes_b64(FLAG_KEY, f"{PLATFORM}#{APP_NAME}#{scene}#captcha-front#{PREFIX}#{REGION}")
    return aes_b64(REQ_KEY, f"{APP_KEY}#W#{flag}#{APP_VERSION}#CLOUD#")

def make_device_token(device_id: str, session_key: str, fingerprint_fields: list, extra=()):
    """Assemble the exact deviceToken layout captured from window.um.getToken()."""
    # fingerprint_fields: ordered list of the 155 string values (empty string for absent)
    fp_plain = "#".join(str(v) for v in fingerprint_fields)
    fp_cipher = aes_b64(session_key, fp_plain)
    parts = ["WEB", device_id, fp_cipher, "505"]
    tail = hashlib.md5("#".join(parts).encode()).hexdigest()
    token = "#".join(parts + [tail])
    return base64.b64encode(token.encode()).decode()

def pop_sign(params: dict, secret=AK_SECRET) -> str:
    p = {k: v for k, v in params.items() if k != "Signature"}
    qs = "&".join(
        urllib.parse.quote(str(k), safe="-_.~") + "=" + urllib.parse.quote(str(v), safe="-_.~")
        for k, v in sorted(p.items())
    )
    sign_str = "POST&%2F&" + urllib.parse.quote(qs, safe="-_.~")
    return base64.b64encode(hmac.new((secret + "&").encode(), sign_str.encode(), hashlib.sha1).digest()).decode()

def post_form(host: str, params: dict) -> str:
    params = dict(params)
    params["Signature"] = pop_sign(params)
    data = urllib.parse.urlencode(params).encode()
    req = urllib.request.Request(host, data=data, headers={
        "Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
        "User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/131.0.0.0 Safari/537.36",
        "Origin": f"https://{PREFIX}.captcha-open.aliyuncs.com",
    })
    try:
        with urllib.request.urlopen(req, timeout=20) as r:
            return r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.read().decode("utf-8", "replace")

def init_captcha():
    params = {
        "AaduaneId": AK_ID,
        "SignatureMethod": "HMAC-SHA1",
        "SignatureVersion": "1.0",
        "Format": "JSON",
        "Timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "Version": ACTION_VERSION,
        "Action": "InitCaptchaV3",
        "SceneId": SCENE_ID,
        "Language": "cn",
        "Mode": "popup",
        "DeviceData": init_device_data(),
        "SignatureNonce": str(uuid.uuid4()),
    }
    host = f"https://{PREFIX}.captcha-open.aliyuncs.com/"
    return host, post_form(host, params)

def verify_captcha(certify_id, device_token, behavior_data_b64):
    """VerifyCaptchaV3. behavior_data_b64 is the encrypted 'data' blob.

    The plaintext of that blob (captured) is:
      appKey#W#<flag>#W20220202#CLOUD#162#<base64 behavior JSON>
    where behavior JSON has mousemove/pointerEvent/keyup/... arrays. Filling
    those arrays with server-accepted human-like telemetry is UNSOLVED.
    """
    param = json.dumps({
        "sceneId": SCENE_ID,
        "certifyId": certify_id,
        "deviceToken": device_token,
        "data": behavior_data_b64,
    }, separators=(",", ":"))
    params = {
        "AaduaneId": AK_ID,
        "SignatureMethod": "HMAC-SHA1",
        "SignatureVersion": "1.0",
        "Format": "JSON",
        "Timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "Version": ACTION_VERSION,
        "Action": "VerifyCaptchaV3",
        "SceneId": SCENE_ID,
        "CertifyId": certify_id,
        "CaptchaVerifyParam": param,
        "SignatureNonce": str(uuid.uuid4()),
    }
    host = f"https://{PREFIX}-verify.captcha-open.aliyuncs.com/"
    return host, post_form(host, params)

if __name__ == "__main__":
    host, resp = init_captcha()
    print("InitCaptcha", host, resp[:400])
    doc = json.loads(resp)
    if doc.get("Success"):
        print("CaptchaType:", doc.get("CaptchaType"), "CertifyId:", doc.get("CertifyId"))
        print("NOTE: to verify, supply a deviceToken with a server-accepted behaviour blob.")
