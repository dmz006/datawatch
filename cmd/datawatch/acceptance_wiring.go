// "Done when" lists: opens the store and wires it to the HTTP server.

package main

import (
	"fmt"
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

// wireAcceptance opens the lists and wires them to the web server. Only the
// request sources create lists from real requests, and datawatch runs no
// command check: command items read "cannot tell". A damaged store leaves the
// surfaces unavailable and is never overwritten.
func wireAcceptance(opts acceptance.Options, httpServer *server.HTTPServer) *acceptance.Service {
	if httpServer != nil {
		opts.Audit = httpServer.AuditAcceptance
	}
	svc, err := acceptance.Open(opts)
	if err != nil {
		fmt.Printf("[warn] %v\n", err)
		return nil
	}
	if httpServer != nil {
		httpServer.SetAcceptance(svc, opts.Key != nil)
	}
	return svc
}
