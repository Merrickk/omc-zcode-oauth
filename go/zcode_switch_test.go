package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func mustEncryptZcode(t *testing.T, secret, plain string) string {
	t.Helper()
	nonce := make([]byte, 12)
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	sealed := gcm.Seal(nil, nonce, []byte(plain), nil)
	ct, tag := sealed[:len(sealed)-16], sealed[len(sealed)-16:]
	return "enc:v1:" + base64.RawURLEncoding.EncodeToString(nonce) + "." +
		base64.RawURLEncoding.EncodeToString(tag) + "." +
		base64.RawURLEncoding.EncodeToString(ct)
}

func TestZcodeDecrypt(t *testing.T) {
	secret := "zcode-credential-fallback:linux:/home/u:user"
	enc := mustEncryptZcode(t, secret, "jwt-value")
	if got := zcodeDecrypt(enc, secret); got != "jwt-value" {
		t.Fatalf("zcodeDecrypt = %q", got)
	}
	if got := zcodeDecrypt("plain-jwt", secret); got != "plain-jwt" {
		t.Fatalf("plaintext passthrough = %q", got)
	}
	if got := zcodeDecrypt("enc:v1:bad.bad.bad", secret); got != "" {
		t.Fatalf("invalid ciphertext = %q", got)
	}
}

func TestZcodeSwitchMIDForJWT(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZCODE_CREDENTIAL_SECRET", "s")
	dir := filepath.Join(home, ".zcode-switch", "accounts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"user_id":"uid-1"}`))
	jwt := "header." + payload + ".sig"
	acc := map[string]any{
		"id":                 "a1",
		"virtual_device_mid": "mid-from-switch",
		"credentials":        map[string]any{"zcodejwttoken": jwt},
	}
	raw, _ := json.Marshal(acc)
	if err := os.WriteFile(filepath.Join(dir, "a1.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := zcodeSwitchMIDForJWT(jwt); got != "mid-from-switch" {
		t.Fatalf("zcodeSwitchMIDForJWT = %q, want mid-from-switch", got)
	}
	// A different JWT must not borrow the MID.
	if got := zcodeSwitchMIDForJWT("other.header.sig"); got != "" {
		t.Fatalf("unexpected MID for unknown JWT: %q", got)
	}
}

func TestTelemetryMIDFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZCODE_SWITCH_DATA_ROOT", "")
	t.Setenv("ZCODE_DATA_BASE_DIR", "")
	dir := filepath.Join(home, ".zcode", "v2")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "telemetry-state.json"), []byte(`{"deviceMid":"telemetry-mid"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := zcodeSwitchTelemetryMID(); got != "telemetry-mid" {
		t.Fatalf("zcodeSwitchTelemetryMID = %q", got)
	}
}
