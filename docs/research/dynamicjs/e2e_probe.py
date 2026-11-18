#!/usr/bin/env python3
"""Local end-to-end pure-protocol captcha probe. Research only."""
import base64, json, os, subprocess, sys, time, uuid
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
import pure_protocol_prototype as p
from Crypto.Cipher import AES

FINGERPRINT = ROOT / "feilin_fingerprint_fields.json"
GEN = ROOT / "dynamicjs/data_blob_generator.js"


def dec_deviceconfig(value):
    raw = base64.b64decode(value)
    dec = AES.new(b"87f879f135f27da7", AES.MODE_CBC, p.IV).decrypt(raw)
    return dec[:-dec[-1]].decode()


def gen_data(digest, track, key):
    wrapped = {"__digest": digest, "track": track}
    inp = Path("/tmp/e2e_track_wrapped.json")
    inp.write_text(json.dumps(wrapped, separators=(",", ":")), encoding="utf-8")
    env = dict(os.environ)
    env["NODE_PATH"] = os.environ.get("NODE_PATH", "/home/merrick/node_modules")
    out = subprocess.check_output(["node", str(GEN), str(inp), key], env=env, text=True)
    return out.strip()


def main():
    now = int(time.time() * 1000)
    digest = base64.b64decode("ZGU2Mzg1YjFkYTE5ZmVkMg==").decode() if False else secrets_hex(16)
    key = secrets_hex(8)
    device_id = f"{p.APP_KEY}-h-{now}-{secrets_hex(16)}"
    fingerprint = json.loads(FINGERPRINT.read_text())
    # Fingerprint capture is a JSON object with obfuscated field names. Convert to ordered values.
    fields = [str(fingerprint[k]) for k in sorted(fingerprint.keys())]
    token = p.make_device_token(device_id, key, fields, preid=True)

    host, resp = p.init_captcha()
    init = json.loads(resp)
    print("INIT", init.get("Success"), init.get("CaptchaType"), init.get("CertifyId"))
    if not init.get("Success"):
        print(resp)
        return

    cfg = dec_deviceconfig(init["DeviceConfig"])
    parts = cfg.split("#")
    if len(parts) >= 3:
        digest = parts[0]

    track = {
        "TrackList": {"mc": "", "tc": "", "mu": "", "te": "", "mp": "", "tmv": "", "ks": "", "fi": "", "startTime": now},
        "TrackStartTime": now,
        "VerifyTime": now + 8,
        "arg": base64.b64encode(os.urandom(8)).decode(),
    }
    data = gen_data(digest, track, key)
    print("DATA", data[:80], "len", len(data))
    Path("/tmp/e2e_token.txt").write_text(token)
    Path("/tmp/e2e_data.txt").write_text(data)
    print("TOKEN", token[:80], "len", len(token))

    answer = p.verify_captcha(init["CertifyId"], token, data)
    print("VERIFY", answer[:1000])


def secrets_hex(n):
    import secrets
    return secrets.token_hex(n)


if __name__ == "__main__":
    main()
