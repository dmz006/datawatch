// CLI tests for `datawatch autonomous archive`, against an httptest daemon.

package main

import (
	"bytes"
	"strings"
	"testing"
)

func runArchiveCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newAutonomousArchiveCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestCLIAutonomousArchiveDefaultsToDryRun(t *testing.T) {
	c := &captureDaemon{replyBody: `{"dry_run":true,"eligible":[]}`}
	withCaptureServer(t, c)
	out, err := runArchiveCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	if c.gotMethod != "POST" || c.gotPath != "/api/autonomous/archive" {
		t.Fatalf("request = %s %s", c.gotMethod, c.gotPath)
	}
	if got := c.body(t)["dry_run"]; got != true {
		t.Fatalf("dry_run = %v, want true", got)
	}
	if !strings.Contains(out, `"dry_run": true`) {
		t.Fatalf("output: %s", out)
	}
	// An explicit --dry-run is the same request.
	if _, err := runArchiveCmd(t, "--dry-run"); err != nil || c.body(t)["dry_run"] != true {
		t.Fatalf("--dry-run: %v %v", err, c.body(t))
	}
}

func TestCLIAutonomousArchiveApplyAndShow(t *testing.T) {
	c := &captureDaemon{replyBody: `{"dry_run":false,"archived":[]}`}
	withCaptureServer(t, c)
	if _, err := runArchiveCmd(t, "--apply"); err != nil {
		t.Fatal(err)
	}
	if c.body(t)["dry_run"] != false {
		t.Fatalf("--apply must send dry_run=false: %v", c.body(t))
	}
	if _, err := runArchiveCmd(t, "--apply", "--dry-run"); err == nil {
		t.Fatal("--apply with --dry-run must be refused")
	}
	c.replyBody = `{"version":1,"id":"ab12"}`
	if _, err := runArchiveCmd(t, "--id", "ab12"); err != nil {
		t.Fatal(err)
	}
	if c.gotMethod != "GET" || c.gotPath != "/api/autonomous/prds/ab12/archive" {
		t.Fatalf("request = %s %s", c.gotMethod, c.gotPath)
	}
	if _, err := runArchiveCmd(t, "--id", "ab12", "--apply"); err == nil {
		t.Fatal("--id with --apply must be refused")
	}
}
