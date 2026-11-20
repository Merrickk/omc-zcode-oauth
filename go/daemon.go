package main

import (
	"os"
	"strings"
	"sync"
	"time"
)

var daemonOnce sync.Once

// startAutoClaimDaemon starts a background daemon that runs every 30 minutes
// to report activation events and auto-claim gifts for every ZCode account.
func startAutoClaimDaemon(dir string) {
	if dir == "" {
		return
	}
	daemonOnce.Do(func() {
		go func() {
			// Initial warm-up delay after startup
			time.Sleep(20 * time.Second)
			runAllAutoClaim(dir)

			ticker := time.NewTicker(30 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				runAllAutoClaim(dir)
			}
		}()
	})
}

// runAllAutoClaim inspects the credential directory, reports activation events,
// and claims any available gift plans for all ZCode accounts.
func runAllAutoClaim(dir string) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		// Match zcode credentials
		if !strings.HasPrefix(e.Name(), "zcode-") && !strings.Contains(e.Name(), "zcode") {
			continue
		}
		name := e.Name()
		func() {
			unlock := claimLock(name)
			defer unlock()
			_ = autoClaim(claimSender(), dir, autoClaimRequest{
				Auth:     name,
				Activate: true,
			})
		}()
		// Smooth pause between accounts to respect vendor rate limits
		time.Sleep(3 * time.Second)
	}
}
