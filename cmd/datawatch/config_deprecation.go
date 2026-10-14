package main

import (
	"fmt"
	"sync"
	"time"

	auditpkg "github.com/dmz006/datawatch/internal/audit"
)

// deprecatedConfigReplacements maps each deprecated config key to the key
// that replaces it, for the startup warning and audit entry.
var deprecatedConfigReplacements = map[string]string{
	"autonomous.max_parallel_tasks": "autonomous.max_concurrent_tasks",
}

var deprecatedConfigWarnOnce sync.Once

// warnDeprecatedConfigKeys logs one warning line and writes one audit
// entry per deprecated key set in the loaded config. It runs at most once
// per process so a config reload does not repeat it. A nil audit log
// (open failed) still gets the warning lines.
func warnDeprecatedConfigKeys(keys []string, auditLog *auditpkg.Log) {
	deprecatedConfigWarnOnce.Do(func() {
		for _, key := range keys {
			replacement := deprecatedConfigReplacements[key]
			fmt.Printf("[warn] config: %s is deprecated and ignored; use %s (removed in a future release)\n", key, replacement)
			if auditLog == nil {
				continue
			}
			if err := auditLog.Write(auditpkg.Entry{
				Timestamp: time.Now(),
				Actor:     "system",
				Action:    "config_deprecated_key",
				Details:   map[string]any{"key": key, "replacement": replacement},
			}); err != nil {
				fmt.Printf("[warn] audit write for deprecated config key %s failed: %v\n", key, err)
			}
		}
	})
}
