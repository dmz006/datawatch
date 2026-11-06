// BL110 extension — the comm-channel `configure` path records exactly one
// audit record per attempted write from a SESSION-attributed invocation
// (surface "comm", caller = session/peer, denied (capability) or failed
// writes included with the reason). Operator traffic (zero attribution —
// the normal comm channel, whose loopback REST PUT is admin) is executed
// but NOT audited as a session write, consistent with the REST and MCP
// surfaces which skip admin callers.

package router

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

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

func newAuditTestRouter(t *testing.T, configure func(key, value string) error, sent *[]string, wg *sync.WaitGroup, mu *sync.Mutex) *Router {
	t.Helper()
	r := &Router{hostname: "h", groupID: "grp-1", backend: &captureBackend{name: "test", capture: func(s string) {
		mu.Lock()
		*sent = append(*sent, s)
		mu.Unlock()
		if wg != nil {
			wg.Done()
		}
	}}}
	r.SetConfigureFunc(configure)
	return r
}

func TestConfigureAudit_SessionAllowed_OneRecord(t *testing.T) {
	auditPath := t.TempDir() + "/configure-audit.jsonl"
	var mu sync.Mutex
	var sent []string
	var wg sync.WaitGroup
	calls := 0
	r := newAuditTestRouter(t, func(key, value string) error { calls++; return nil }, &sent, &wg, &mu)
	r.SetConfigAuditor(federation.NewConfigAuditor(auditPath, true))

	at := ConfigureAttribution{
		Caller:             "session:s1",
		ConfigWriteAllowed: true,
	}
	wg.Add(1)
	r.handleConfigureAttributed(Command{Type: CmdConfigure, Text: "usage.burn_guard.ask_on_change=true"}, at)
	wg.Wait()

	if calls != 1 {
		t.Fatalf("configure write ran %d times, want 1", calls)
	}
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
	if rec.Caller != "session:s1" {
		t.Errorf("caller = %q, want session:s1", rec.Caller)
	}
	if !rec.Allowed {
		t.Errorf("allowed configure must be recorded with allowed=true: %+v", rec)
	}
	if rec.Values["usage.burn_guard.ask_on_change"] != "true" {
		t.Errorf("changed key missing/wrong in record: %+v", rec.Values)
	}
	// And the confirmation still reached the channel.
	mu.Lock()
	if len(sent) < 1 || !strings.Contains(sent[0], "Set usage.burn_guard.ask_on_change = true") {
		t.Fatalf("confirmation not sent: %v", sent)
	}
	mu.Unlock()
}

// TestConfigureAudit_SessionDenied_MissingCapability is the capability
// REMOVAL case: the session credential lacks config:write (the BL412
// built-in-group removal), so the write is refused before it runs —
// once — with the exact missing capability named. This is the comm
// analogue of the REST fedCap denial and the MCP middleware denial.
func TestConfigureAudit_SessionDenied_MissingCapability(t *testing.T) {
	auditPath := t.TempDir() + "/configure-audit.jsonl"
	var mu sync.Mutex
	var sent []string
	calls := 0
	r := newAuditTestRouter(t, func(key, value string) error { calls++; return nil }, &sent, nil, &mu)
	r.SetConfigAuditor(federation.NewConfigAuditor(auditPath, true))

	at := ConfigureAttribution{
		Caller:             "session:s2",
		ConfigWriteAllowed: false,
		DenyReason:         "session token lacks capability: " + federation.CapConfigWrite,
	}
	r.handleConfigureAttributed(Command{Type: CmdConfigure, Text: "usage.burn_guard.ask_on_change=true"}, at)

	// Router.send dispatches asynchronously — wait for the channel reply
	// before asserting on it.
	joined := ""
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		done := false
		for _, s := range sent {
			if strings.Contains(s, "config:write") || strings.Contains(s, "capabilit") {
				done = true
				break
			}
		}
		joined = strings.Join(sent, "\n")
		mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("denied configure did not report to the channel within 2s, got: %q", joined)
		}
		time.Sleep(5 * time.Millisecond)
	}

	if calls != 0 {
		t.Fatalf("denied configure must NOT run the write, configureFn was called %d times", calls)
	}
	recs := readConfigureAudit(t, auditPath)
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 audit record for the denied configure, got %d", len(recs))
	}
	if recs[0].Allowed {
		t.Errorf("capability-denied configure must be recorded with allowed=false: %+v", recs[0])
	}
	if !strings.Contains(recs[0].Reason, federation.CapConfigWrite) {
		t.Errorf("reason = %q, want the missing capability %q named", recs[0].Reason, federation.CapConfigWrite)
	}
	if recs[0].Caller != "session:s2" {
		t.Errorf("caller = %q, want session:s2", recs[0].Caller)
	}
}

// TestConfigureAudit_OperatorTraffic_NotAudited: a normal comm message
// arrives without a session credential (zero attribution); the write runs
// fine, but NO audit record is produced — the daemon records session
// writes (REST, MCP, and this session-attributed comm path), and the
// loopback PUT this path makes is admin, so the REST point skips it.
func TestConfigureAudit_OperatorTraffic_NotAudited(t *testing.T) {
	auditPath := t.TempDir() + "/configure-audit.jsonl"
	var mu sync.Mutex
	var sent []string
	var wg sync.WaitGroup
	boom := 0
	fail := false
	r := newAuditTestRouter(t, func(key, value string) error {
		boom++
		if fail {
			return errConfigureBoom
		}
		return nil
	}, &sent, &wg, &mu)
	r.SetConfigAuditor(federation.NewConfigAuditor(auditPath, true))

	// 1) allowed operator write — audited zero times
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "usage.burn_guard.ask_on_change=true"})
	wg.Wait()
	// 2) failed operator write — equally not a session write
	fail = true
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "usage.burn_guard.ask_on_change=false"})
	wg.Wait()

	if boom != 2 {
		t.Fatalf("both operator writes should have run, got %d", boom)
	}
	if recs := readConfigureAudit(t, auditPath); len(recs) != 0 {
		t.Fatalf("operator configure must not be audited as a session write, got %d record(s): %+v", len(recs), recs)
	}
}

// TestConfigureAudit_HandlesTestMessage is the daemon's
// session-attributed test-injection path: POST /api/test/message →
// HandleAttributedTestMessage → configure. A session with config:write
// lands exactly one allowed record; a session without it is refused and
// exactly one denied record (naming the missing capability) lands, and
// the write did not run.
func TestConfigureAudit_HandlesTestMessage(t *testing.T) {
	t.Run("session_with_capability_is_allowed", func(t *testing.T) {
		auditPath := t.TempDir() + "/configure-audit.jsonl"
		calls := 0
		r := &Router{hostname: "h", groupID: "grp-test", backend: &captureBackend{name: "test", capture: func(string) {}}}
		r.SetConfigureFunc(func(key, value string) error { calls++; return nil })
		r.SetConfigAuditor(federation.NewConfigAuditor(auditPath, true))

		r.HandleAttributedTestMessage(
			"configure usage.burn_guard.ask_on_change=true",
			ConfigureAttribution{Caller: "session:s3", ConfigWriteAllowed: true})

		if calls != 1 {
			t.Fatalf("configure write ran %d times, want 1", calls)
		}
		recs := readConfigureAudit(t, auditPath)
		if len(recs) != 1 {
			t.Fatalf("expected exactly 1 audit record, got %d", len(recs))
		}
		if recs[0].Surface != "comm" || !recs[0].Allowed || recs[0].Caller != "session:s3" {
			t.Fatalf("unexpected record: %+v", recs[0])
		}
	})
	t.Run("session_without_capability_is_denied", func(t *testing.T) {
		auditPath := t.TempDir() + "/configure-audit.jsonl"
		calls := 0
		r := &Router{hostname: "h", groupID: "grp-test", backend: &captureBackend{name: "test", capture: func(string) {}}}
		r.SetConfigureFunc(func(key, value string) error { calls++; return nil })
		r.SetConfigAuditor(federation.NewConfigAuditor(auditPath, true))

		resps := r.HandleAttributedTestMessage(
			"configure usage.burn_guard.ask_on_change=true",
			ConfigureAttribution{
				Caller:     "session:s4",
				DenyReason: "session token lacks capability: " + federation.CapConfigWrite,
			})

		if calls != 0 {
			t.Fatalf("denied configure must NOT run the write, ran %d times", calls)
		}
		recs := readConfigureAudit(t, auditPath)
		if len(recs) != 1 {
			t.Fatalf("expected exactly 1 audit record, got %d", len(recs))
		}
		if recs[0].Allowed {
			t.Fatalf("denied configure must be allowed=false: %+v", recs[0])
		}
		if !strings.Contains(recs[0].Reason, federation.CapConfigWrite) {
			t.Fatalf("reason must name the missing capability, got %q", recs[0].Reason)
		}
		if len(resps) == 0 {
			t.Errorf("denied configure should still send a channel response, got none")
		}
	})
}
