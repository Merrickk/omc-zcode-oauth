#!/usr/bin/env python3
"""Decrypt a zcode-switch .zsb bundle and probe billing/preview with its JWT.

Usage:
  python3 tools/claim_probe.py Merrick.zsb 111111 [--mid DEVICE_MID] [--ver 3.16.3]

Prints the JWT identity and the RAW preview response so you can tell whether
an empty plan list comes from the server, the device context, or parsing.
"""
import argparse
import base64
import hashlib
import json
import sys
import urllib.error
import urllib.request
import uuid

from cryptography.hazmat.primitives.ciphers.aead import AESGCM

PREVIEW = "https://zcode.z.ai/api/v1/zcode-plan/billing/preview"


def open_bundle(path, password):
    env = json.load(open(path, encoding="utf-8"))
    salt = base64.b64decode(env["kdf"]["salt"])
    nonce = base64.b64decode(env["cipher"]["nonce"])
    tag = base64.b64decode(env["cipher"]["tag"])
    data = base64.b64decode(env["cipher"]["data"])
    key = hashlib.pbkdf2_hmac("sha256", password.encode(), salt, env["kdf"]["iters"], 32)
    plain = AESGCM(key).decrypt(nonce, data + tag, None)
    return json.loads(plain)


def jwt_payload(jwt):
    p = jwt.split(".")[1]
    p += "=" * (-len(p) % 4)
    return json.loads(base64.urlsafe_b64decode(p))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("bundle")
    ap.add_argument("password")
    ap.add_argument("--mid", default="")
    ap.add_argument("--ver", default="3.16.3")
    ap.add_argument("--platform", default="linux-x64")
    args = ap.parse_args()

    bundle = open_bundle(args.bundle, args.password)
    for i, acc in enumerate(bundle.get("accounts", [])):
        creds = acc.get("credentials", {})
        jwt = creds.get("zcodejwttoken")
        if not jwt:
            continue
        uid = jwt_payload(jwt).get("user_id") or jwt_payload(jwt).get("sub")
        mid = args.mid or str(uuid.uuid4())
        url = f"{PREVIEW}?app_version={args.ver}&platform={args.platform}"
        h = {
            "Content-Type": "application/json",
            "Accept": "application/json",
            "User-Agent": f"ZCode/{args.ver}",
            "HTTP-Referer": "https://zcode.z.ai",
            "X-Title": "Z Code@electron",
            "X-ZCode-App-Version": args.ver,
            "X-Platform": args.platform,
            "X-Release-Channel": "stable",
            "X-Client-Language": "zh-CN",
            "X-Client-Timezone": "Asia/Shanghai",
            "X-Os-Category": "linux",
            "X-Os-Version": "6.1.0",
            "Authorization": "Bearer " + jwt,
            "X-Request-Id": str(uuid.uuid4()),
            "X-Device-Mid": mid,
        }
        req = urllib.request.Request(url, headers=h, method="GET")
        try:
            r = urllib.request.urlopen(req, timeout=25)
            raw, code = r.read().decode(), r.status
        except urllib.error.HTTPError as e:
            raw, code = e.read().decode(), e.code
        except Exception as e:
            raw, code = f"request error: {e}", -1
        print(f"[{i}] {acc.get('name')} uid={uid} mid={mid} ver={args.ver}")
        print(f"    HTTP {code} raw={raw[:600]}")
        try:
            j = json.loads(raw)
            data = j.get("data")
            plans = data.get("plans") if isinstance(data, dict) else None
            print(f"    code={j.get('code')} has_data={data is not None} plans={len(plans) if plans is not None else 'N/A'}")
        except Exception:
            pass


if __name__ == "__main__":
    main()
