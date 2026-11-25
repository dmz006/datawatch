package main

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// The daemon's own loop hooks carry the merge run and deploy run switch, read
// live, and write each refusal to the daemon's audit log.
func TestSupervisorLoopDaemonHooks_HoldSwitchAndAudit(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir}
	m, err := autonomouspkg.NewManager(t.TempDir(), autonomouspkg.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(&cfg.Server, cfg, "", dir, nil, "test-host", nil)
	log, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	hooks := daemonSupervisorLoopHooks(m, cfg, supervisorloop.DefaultSettings, srv)
	// The daemon sets the audit log before it wires the loop; the hook reads
	// it when it writes, so either order works.
	srv.SetAuditLog(log)
	if hooks.MergeDeployRunEnabled == nil || hooks.MergeDeployRunEnabled() {
		t.Fatal("the daemon hooks must carry the switch, off by default")
	}
	cfg.Autonomous.MergeDeployRunEnabled = true
	if !hooks.MergeDeployRunEnabled() {
		t.Fatal("the switch must be read live")
	}
	if hooks.RunRefused == nil {
		t.Fatal("the daemon hooks must record refusals")
	}
	hooks.RunRefused(context.Background(), autonomouspkg.SupervisorRunHold{
		Job: "run_merge_until_conflict", Setting: config.MergeDeployRunKey, Message: config.MergeDeployRunRefusal,
	}, false)
	entries, err := log.Read(audit.QueryFilter{Action: audit.ActionMergeDeployRunRefused})
	if err != nil || len(entries) != 1 || entries[0].Subject != "merge_run" || entries[0].Actor != "supervisor-loop" {
		t.Fatalf("audit = %+v %v", entries, err)
	}
	// A daemon with no web server still gets the switch.
	if h := daemonSupervisorLoopHooks(m, cfg, supervisorloop.DefaultSettings, nil); h.MergeDeployRunEnabled == nil {
		t.Fatal("no server: the switch must still apply")
	}
}
