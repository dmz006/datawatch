// BL110 extension — the comm-channel `configure` path writes exactly one
// audit record per call (surface "comm"), with the changed key, "comm:"
// attribution for the session credential the channel carries, and an
// allowed=false record (with the reason) when the configure function
// fails.

package router

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

var errConfigureBoom = errors.New("configure boom")

func readConfigureAudit(t *testing.T, path string) []federation.ConfigAuditRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []federation.ConfigAuditRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec federation.ConfigAuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad audit line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func TestConfigureAudit_OneRecordPerConfigure(t *testing.T) {
	auditPath := t.TempDir() + "/configure-audit.jsonl"
	var mu sync.Mutex
	var sent []string
	var wg sync.WaitGroup
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	r := &Router{hostname: "h", groupID: "grp-1", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error { return nil })
	r.SetConfigAuditor(federation.NewConfigAuditor(auditPath, true))

	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "usage.burn_guard.ask_on_change=true"})
	wg.Wait()

	recs := readConfigureAudit(t, auditPath)
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 audit record, got %d", len(recs))
	}
	rec := recs[0]
	if rec.Surface != "comm" {
		t.Errorf("surface = %q, want comm", rec.Surface)
	}
	if rec.Tool != "configure" {
		t.Errorf("tool = %q, want configure", rec.Tool)
	}
	if !strings.Contains(rec.Caller, "comm:") {
		t.Errorf("caller = %q, want comm:<session> attribution", rec.Caller)
	}
	if !rec.Allowed {
		t.Errorf("allowed configure must be recorded with allowed=true: %+v", rec)
	}
	found := false
	for k := range rec.Values {
		if k == "usage.burn_guard.ask_on_change" {
			found = true
		}
	}
	if !found {
		t.Errorf("changed key missing from record: %+v", rec.Values)
	}
	// And the confirmation still reached the channel.
	mu.Lock()
	if len(sent) < 1 || !strings.Contains(sent[0], "Set usage.burn_guard.ask_on_change = true") {
		t.Fatalf("confirmation not sent: %v", sent)
	}
	mu.Unlock()
}

func TestConfigureAudit_Failure_RecordsDeniedWithReason(t *testing.T) {
	auditPath := t.TempDir() + "/configure-audit.jsonl"
	var sent []string
	var wg sync.WaitGroup
	capture := func(s string) {
		sent = append(sent, s)
		wg.Done()
	}
	r := &Router{hostname: "h", groupID: "grp-2", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error { return errConfigureBoom })
	r.SetConfigAuditor(federation.NewConfigAuditor(auditPath, true))

	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "usage.burn_guard.ask_on_change=true"})
	wg.Wait()

	recs := readConfigureAudit(t, auditPath)
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 audit record for the failed configure, got %d", len(recs))
	}
	if recs[0].Allowed {
		t.Fatalf("failed configure must be recorded with allowed=false: %+v", recs[0])
	}
	if !strings.Contains(recs[0].Reason, "boom") {
		t.Errorf("reason = %q, want to contain boom", recs[0].Reason)
	}
}
