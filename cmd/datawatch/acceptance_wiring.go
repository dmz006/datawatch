// "Done when" lists: opens the store and wires it to the HTTP server.

package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/dmz006/datawatch/internal/acceptance"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server"
)

// acceptanceSettings reads the live acceptance block, so a settings change
// applies without a restart.
func acceptanceSettings(cfg *config.Config) acceptance.Settings {
	c := cfg.Acceptance
	checks := map[string]acceptance.ProjectCheck{}
	for name, pc := range c.ProjectChecks {
		out := acceptance.ProjectCheck{Command: pc.Command, Inputs: pc.Inputs, TimeoutSec: pc.TimeoutSeconds}
		for _, x := range pc.Expect {
			out.Expect = append(out.Expect, acceptance.Expect{Pass: x.Pass, Fail: x.Fail})
		}
		checks[name] = out
	}
	return acceptance.Settings{
		ReworkRounds:  c.ReworkRounds,
		InfraRetries:  c.InfraRetries,
		CheckTimeout:  time.Duration(c.CheckTimeoutSeconds) * time.Second,
		Freshness:     time.Duration(c.FreshnessSeconds) * time.Second,
		WarnBefore:    time.Duration(c.WarnHours) * time.Hour,
		ProjectChecks: checks,
		DefaultsFile:  expandHome(c.DefaultsFile),
	}
}

// wireAcceptance wires the lists. Nothing here creates, fixes or runs a list
// from a real request: the request sources do that when they land, so no
// version resolver, source probe or reviewer is set and the items that need
// them read "cannot tell". A damaged store leaves the surfaces unavailable and
// is never overwritten.
func wireAcceptance(cfg *config.Config, encKey []byte, httpServer *server.HTTPServer) {
	if httpServer == nil {
		return
	}
	var key []byte
	if encKey != nil {
		key = append(key, encKey...)
	}
	svc, err := acceptance.Open(acceptance.Options{
		Path:     filepath.Join(expandHome(cfg.DataDir), "acceptance.json"),
		Key:      key,
		Mode:     func() string { return cfg.Acceptance.Mode },
		Settings: func() acceptance.Settings { return acceptanceSettings(cfg) },
		Audit:    httpServer.AuditAcceptance,
	})
	if err != nil {
		fmt.Printf("[warn] %v\n", err)
		return
	}
	httpServer.SetAcceptance(svc, key != nil)
}
