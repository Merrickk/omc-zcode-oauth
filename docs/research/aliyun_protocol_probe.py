#!/usr/bin/env python3
"""Decode AliyunCaptcha.js loader constants and exercise the device OpenAPI."""
import base64, datetime, uuid, hashlib, hmac, json, re, sys, urllib.parse, urllib.request
from pathlib import Path

SRC = Path(sys.argv[1] if len(sys.argv) > 1 else "aliyunCaptcha.js")
TEXT = SRC.read_text(encoding="utf-8", errors="replace")

ALPHA = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+/="

def obf_decode(s: str, uri: bool = False) -> str:
    r = o = 0
    out = bytearray()
    for ch in s:
        e = ALPHA.find(ch)
        if e >= 0:
            r = (64 * r + e) if o % 4 else e
            old = o
            o += 1
            # JS: if(o++%4) ...>>(-2*o&6) uses the post-increment o.
            if old % 4:
                out.append(255 & (r >> (-2 * o & 6)))
    if uri:
        return urllib.parse.unquote_to_bytes(bytes(out)).decode("utf-8", "replace")
    return bytes(out).decode("latin1")

def table(fn: str):
    m = re.search(r"function " + re.escape(fn) + r"\(\)\{var t=\[(?:\"[^\"]*\",?)+\](?=;return)", TEXT)
    if not m:
        raise ValueError(f"table {fn} not found")
    return re.findall(r'"([^"]*)"', m.group(0))

def decoder(fn: str, table_fn: str):
    m = re.search(r"function " + re.escape(fn) + r"\(\w+,\w+\)\{var e=" + re.escape(table_fn) + r"\(\);return " + re.escape(fn) + r"=function\(\w+,\w+\)\{var i=e\[\w+-=(\d+)\]", TEXT)
    if not m:
        raise ValueError(f"decoder {fn} not found")
    return table(table_fn), int(m.group(1))

# Nt table (q decoder): rotation shift 196 found by making ENDPOINTS a clean URL.
NT, NT_BASE = decoder("q", "Nt")
NT_SHIFT = 196
def U(n: int) -> str:
    return obf_decode(NT[((n - NT_BASE + NT_SHIFT) % len(NT) + len(NT)) % len(NT)])

def P(*idx: int) -> str:
    return "".join(U(i) for i in idx)

# ie table (ne decoder): rotation found by simulating the checksum IIFE (shift 6).
IE, IE_BASE = decoder("ne", "ie")
IE_SHIFT = 6
def V(n: int) -> str:
    return obf_decode(IE[((n - IE_BASE + IE_SHIFT) % len(IE) + len(IE)) % len(IE)], uri=True)

def decrypt(secret: str, ct: str) -> str:
    # ye(t,r): AES-CBC, key = Utf8(t), iv = Utf8(Base64(Hex(ht)))
    ht = P(234, 279, 208).encode()
    iv = base64.b64encode(bytes.fromhex(ht.decode())).decode().encode()
    from Crypto.Cipher import AES
    cipher = AES.new(secret.encode(), AES.MODE_CBC, iv)
    data = cipher.decrypt(base64.b64decode(ct))
    pad = data[-1]
    return (data[:-pad] if 1 <= pad <= 16 else data).decode("utf-8", "replace")

def constants():
    access_sec = P(354, 391)
    id_ct = P(287, 459, 448, 310, 401, 352)
    secret_ct = P(489, 444, 235, 458, 491, 408)
    return {
        "api_version": P(325) + "15",
        "version": P(276) + "05",
        "access_sec": access_sec,
        "key_id": decrypt(access_sec, id_ct),
        "key_secret": decrypt(access_sec, secret_ct),
        "app_key": P(479, 447, 481, 333),
        "app_name": P(486, 474),
        "endpoints": {
            "cn": P(223, 396, 356, 476, 242),
            "intl": P(223, 396, 356, 428, 420, 232, 253),
            "waf": P(223, 306, 265, 466, 314),
        },
    }

def esc(s):
    return urllib.parse.quote(str(s), safe="-_.~")

def sign_request(params: dict, key_id: str, key_secret: str, host: str = "https://cloudauth-device.aliyuncs.com/"):
    params = dict(params)
    params["AaduaneId"] = key_id
    params.setdefault("SignatureMethod", "HMAC-SHA1")
    params.setdefault("SignatureVersion", "1.0")
    params.setdefault("Format", "JSON")
    params.setdefault("Timestamp", datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))
    params.setdefault("SignatureNonce", str(uuid.uuid4()))
    qs = "&".join(f"{esc(k)}={esc(v)}" for k, v in sorted(params.items()))
    to_sign = "POST&%2F&" + esc(qs)
    sig = base64.b64encode(hmac.new((key_secret + "&").encode(), to_sign.encode(), hashlib.sha1).digest()).decode()
    params["Signature"] = sig
    body = urllib.parse.urlencode(params).encode()
    req = urllib.request.Request(host, data=body, headers={
        "Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
        "User-Agent": "Mozilla/5.0",
    })
    try:
        resp = urllib.request.urlopen(req, timeout=15)
        return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")

if __name__ == "__main__":
    import uuid
    c = constants()
    print(json.dumps(c, ensure_ascii=False, indent=2))
    print("\n--- InitCaptcha on WAF endpoint (version 2023-03-05) ---")
    status, body = sign_request(
        {"Version": c["version"], "Action": "InitCaptcha", "SceneId": "test"},
        c["key_id"], c["key_secret"], c["endpoints"]["waf"],
    )
    print(status, body[:800])

def aes_encrypt(key: str, plaintext: str) -> str:
    ht = P(234, 279, 208).encode()
    iv = base64.b64encode(bytes.fromhex(ht.decode())).decode().encode()
    from Crypto.Cipher import AES
    from Crypto.Util.Padding import pad
    c = AES.new(key.encode(), AES.MODE_CBC, iv)
    return base64.b64encode(c.encrypt(pad(plaintext.encode(), 16))).decode()

def action_constants():
    sec = P(354, 391)
    # WEB AES secret keys, decrypted from Yt table entries
    req_key = decrypt(sec, P(490, 493, 440, 224, 432, 405))   # Yt[REQ]
    res_key = decrypt(sec, P(400, 425, 298, 429, 470, 373))   # Yt[RES]
    flag_key = decrypt(sec, P(360, 278, 323, 395, 433, 291))  # Yt[FLAG]
    return {"req_key": req_key, "res_key": res_key, "flag_key": flag_key}

def init_request(scene_id: str, region: str = "cn", prefix: str = ""):
    """Replicate Ne(): encrypted Data= payload + POP signature, device API."""
    c = constants()
    ak = action_constants()
    # x = decrypt(ACCESS_SEC, WEB_AES_FLAG_SECRET_KEY[FLAG]) used inside Ne for the
    # inner flag encryption (de(s, plaintext)); s = ye(ACCESS_SEC, i.secretKey) || WEB_AES_FLAG_SECRET_KEY.
    # The device config i.secretKey is embedded as Yt[FLAG] ciphertext -> ak['flag_key'].
    flag = aes_encrypt(ak["flag_key"],
                       c["app_name"] + "#" + (c["app_name"]) + "#" + scene_id +
                       "#captcha-front#" + prefix + "#" + region)
    # Actually Ne uses i.PLATFORM+"#"+a+"#"+(i.sceneId||"")+"#captcha-front#"+i.prefix+"#"+i.region
    platform = "W.10001.c"  # U(436)+'c' from earlier
    inner = platform + "#" + c["app_name"] + "#" + scene_id + "#captcha-front#" + prefix + "#" + region
    flag = aes_encrypt(ak["flag_key"], inner)
    data = aes_encrypt(ak["req_key"],
                       c["app_key"] + "#WEB#" + flag + "#" + P(366) + "2" + "#CLOUD#")
    params = {
        "AaduaneId": c["key_id"],
        "Version": c["api_version"],
        "SignatureMethod": "HMAC-SHA1",
        "SignatureVersion": "1.0",
        "Format": "JSON",
        "Action": "InitCaptcha",
        "Data": data,
        "SignatureNonce": str(uuid.uuid4()),
    }
    # sign with the effective KEY_SECRET from Ne: ye(ACCESS_SEC, pt.secretKey) was invalid,
    # but o.KEY_SECRET passed to Ne is the LOG secret (o=_e): KEY_SECRET = Duane...
    key_secret = "DuanemHmyeE6LXCC46sJEDUw5DTlSZ"
    qs = "&".join(f"{esc(k)}={esc(v)}" for k, v in sorted(params.items()) if k != "Signature")
    sig = base64.b64encode(hmac.new((key_secret + "&").encode(), ("POST&%2F&" + esc(qs)).encode(), hashlib.sha1).digest()).decode()
    params["Signature"] = sig
    return params
