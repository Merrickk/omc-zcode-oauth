package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// virtualDeviceMID mirrors zcode-switch's per-account stable virtual device id.
// It lives next to the credential store as a small sidecar file so credentials
// remain byte-for-byte unchanged.
func virtualDeviceMID(dir, authName string) (string, error) {
	id, err := claimAuthID(authName)
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", fmt.Errorf("auth directory unavailable")
	}
	sum := sha256.Sum256([]byte(id))
	path := filepath.Join(dir, "omc-zcode-"+hex.EncodeToString(sum[:8])+".mid")
	if raw, err := os.ReadFile(path); err == nil {
		if mid := strings.TrimSpace(string(raw)); mid != "" {
			return mid, nil
		}
	}
	mid, err := randomUUID()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(mid+"\n"), 0o600); err != nil {
		return "", err
	}
	return mid, nil
}

func credentialWithVirtualDevice(dir, name string, c credential) (credential, error) {
	mid, err := virtualDeviceMID(dir, name)
	if err != nil {
		return c, err
	}
	c.Device = mid
	return c, nil
}

var _ = pluginapi.ManagementRequest{}
