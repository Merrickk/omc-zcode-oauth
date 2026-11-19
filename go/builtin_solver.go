package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed assets/captcha_solver/*
var builtinSolverFS embed.FS

var solverGit struct {
	sync.Mutex
	dir string
}

type builtinSolverResult struct {
	SceneID         string   `json:"scene_id"`
	Prefix          string   `json:"prefix"`
	Gap             int      `json:"gap"`
	Target          int      `json:"target"`
	Success         bool     `json:"success"`
	Responses       []string `json:"responses"`
	CaptchaVerifyParam string `json:"captcha_verify_param"`
}

func materializeBuiltinSolver() (string, error) {
	solverGit.Lock()
	defer solverGit.Unlock()
	if solverGit.dir != "" {
		return solverGit.dir, nil
	}
	base, err := os.MkdirTemp("", "omc-zcode-solver-")
	if err != nil {
		return "", err
	}
	entries, err := builtinSolverFS.ReadDir("assets/captcha_solver")
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		raw, err := builtinSolverFS.ReadFile("assets/captcha_solver/" + entry.Name())
		if err != nil {
			return "", err
		}
		mode := os.FileMode(0644)
		if strings.HasSuffix(entry.Name(), ".js") || strings.HasSuffix(entry.Name(), ".py") {
			mode = 0755
		}
		if err := os.WriteFile(filepath.Join(base, entry.Name()), raw, mode); err != nil {
			return "", err
		}
	}
	solverGit.dir = base
	return base, nil
}

func builtinSolverAvailable() error {
	for _, bin := range []string{"node", "python3", "xvfb-run"} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("built-in solver requires %s on PATH", bin)
		}
	}
	if runtime.GOOS != "linux" {
		return errors.New("built-in captcha solver is currently supported on Linux hosts only")
	}
	return nil
}

func runBuiltinCaptchaSolver(sceneID, prefix, region string) (string, error) {
	if err := builtinSolverAvailable(); err != nil {
		return "", err
	}
	dir, err := materializeBuiltinSolver()
	if err != nil {
		return "", err
	}
	outPath := filepath.Join(os.TempDir(), fmt.Sprintf("omc-zcode-solve-%d.json", time.Now().UnixNano()))
	cmd := exec.Command("xvfb-run", "-a", "node", filepath.Join(dir, "solve_captcha_e2e.js"))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"OUT="+outPath,
		"SCENE_ID="+sceneID,
		"PREFIX="+prefix,
		"NODE_PATH="+os.Getenv("NODE_PATH"),
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("captcha solver failed: %v: %s", err, tail(stderr.String(), 1200))
		}
	case <-time.After(3 * time.Minute):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", errors.New("captcha solver timed out")
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		return "", fmt.Errorf("captcha solver result missing: %v", err)
	}
	var result builtinSolverResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", errors.New("invalid captcha solver result")
	}
	if !result.Success {
		return "", fmt.Errorf("captcha solver did not pass: %s", tail(stderr.String(), 800))
	}
	for _, post := range result.Responses {
		values, err := url.ParseQuery(post)
		if err != nil {
			continue
		}
		if param := strings.TrimSpace(values.Get("CaptchaVerifyParam")); param != "" {
			return param, nil
		}
	}
	return "", errors.New("captcha solver result has no CaptchaVerifyParam")
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
