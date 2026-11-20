package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// zcode-switch persists one stable virtual_device_mid per account under
// ~/.zcode-switch/accounts/<id>.json. Claim eligibility is bound to that MID:
// a plugin that invents its own MID looks like a different device and gets an
// empty preview even with the same JWT. These helpers read the very same MID
// so the plugin shares the desktop tool's device identity.

type zcodeSwitchAccount struct {
	ID               string                     `json:"id"`
	Name             string                     `json:"name"`
	VirtualDeviceMID string                     `json:"virtual_device_mid"`
	Credentials      map[string]json.RawMessage `json:"credentials"`
}

func zcodeSwitchHome() string {
	if s := strings.TrimSpace(os.Getenv("ZCODE_SWITCH_HOME")); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv("HOME"))
}

func zcodeSwitchDataRoot(home string) string {
	if s := strings.TrimSpace(os.Getenv("ZCODE_SWITCH_DATA_ROOT")); s != "" {
		return s
	}
	if s := strings.TrimSpace(os.Getenv("ZCODE_DATA_BASE_DIR")); s != "" {
		return s
	}
	return home
}

func zcodeCredentialSecret(home string) string {
	if s := os.Getenv("ZCODE_CREDENTIAL_SECRET"); s != "" {
		return s
	}
	username := strings.TrimSpace(os.Getenv("USER"))
	if username == "" {
		username = strings.TrimSpace(os.Getenv("LOGNAME"))
	}
	if username == "" {
		if out, err := exec.Command("id", "-un").Output(); err == nil {
			username = strings.TrimSpace(string(out))
		}
	}
	if username == "" {
		username = "unknown"
	}
	return "zcode-credential-fallback:linux:" + home + ":" + username
}

// zcodeDecrypt mirrors zcrypto.decrypt_with_secret: enc:v1:<nonce>.<tag>.<ct>
// with AES-256-GCM, key = sha256(secret), base64url without padding.
func zcodeDecrypt(value, secret string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "enc:v1:") {
		return value
	}
	parts := strings.Split(strings.TrimPrefix(value, "enc:v1:"), ".")
	if len(parts) != 3 {
		return ""
	}
	nonce, e1 := base64.RawURLEncoding.DecodeString(parts[0])
	tag, e2 := base64.RawURLEncoding.DecodeString(parts[1])
	ct, e3 := base64.RawURLEncoding.DecodeString(parts[2])
	if e1 != nil || e2 != nil || e3 != nil || len(nonce) != 12 {
		return ""
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	sealed := append(append([]byte{}, ct...), tag...)
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

func jwtUserID(jwt string) string {
	parts := strings.Split(strings.TrimSpace(jwt), ".")
	if len(parts) < 2 {
		return ""
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var p struct {
		UserID string `json:"user_id"`
		Sub    string `json:"sub"`
		ID     string `json:"id"`
	}
	if json.Unmarshal(b, &p) != nil {
		return ""
	}
	for _, s := range []string{p.UserID, p.Sub, p.ID} {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

// zcodeSwitchMIDForJWT finds the zcode-switch account whose JWT matches the
// credential (or whose user id matches), then returns its virtual_device_mid.
func zcodeSwitchMIDForJWT(jwt string) string {
	jwt = strings.TrimSpace(jwt)
	if jwt == "" {
		return ""
	}
	home := zcodeSwitchHome()
	if home == "" {
		return ""
	}
	dir := filepath.Join(home, ".zcode-switch", "accounts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	juid := jwtUserID(jwt)
	secret := zcodeCredentialSecret(home)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var acc zcodeSwitchAccount
		if json.Unmarshal(raw, &acc) != nil || strings.TrimSpace(acc.VirtualDeviceMID) == "" {
			continue
		}
		token := zcodeDecrypt(rawString(acc.Credentials["zcodejwttoken"]), secret)
		if token == jwt {
			return acc.VirtualDeviceMID
		}
		if juid != "" && jwtUserID(token) == juid {
			return acc.VirtualDeviceMID
		}
	}
	return ""
}

// zcodeSwitchTelemetryMID is the deviceMid written by zcode-switch when it
// switches to an account. It is a fallback when no per-account match exists.
func zcodeSwitchTelemetryMID() string {
	home := zcodeSwitchHome()
	if home == "" {
		return ""
	}
	p := filepath.Join(zcodeSwitchDataRoot(home), ".zcode", "v2", "telemetry-state.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var v struct {
		DeviceMID string `json:"deviceMid"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return strings.TrimSpace(v.DeviceMID)
}
