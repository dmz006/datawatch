// MCP remote branch reaper tools: the report proxies
// GET /api/autonomous/remote-reap and is marked read-only; the run and the
// restore proxy the two POST routes and are the operator writes.

package mcp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

const remoteReapMCPReport = `{"reaped":[{"automaton_id":"prd-a","branch":"automaton/prd-a","base":"main","tip":"3f2a","age_days":40,"reason":"would be reaped"}],
"held":[],"report_only":true,"backups":[],"retention_days":30,"computed_at":"2025-10-09T00:13:20Z"}`

const remoteReapMCPRun = `{"reaped":[{"automaton_id":"prd-a","branch":"automaton/prd-a","base":"main","backup_ref":"refs/datawatch/remote-reap-backup/automaton-prd-a/1760000000","reason":"reaped"}],"held":[],"report_only":false}`

const remoteReapMCPRestore = `{"branch":"automaton/prd-a","backup_ref":"refs/datawatch/remote-reap-backup/automaton-prd-a/1760000000","sha":"3f2a"}`

func TestRemoteReapMCP_ToolsRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range mergeTestToolNames(t) {
		have[n] = true
	}
	for _, want := range RemoteReapToolNames() {
		if !have[want] {
			t.Errorf("tool %q is not registered", want)
		}
	}
}

func TestRemoteReapMCP_ReportReadsRoute(t *testing.T) {
	var method, path, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, rawQuery = r.Method, r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(remoteReapMCPReport))
	})
	res, err := s.handleAutonomousRemoteReap(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/remote-reap" || rawQuery != "" {
		t.Errorf("proxied %s %s?%s", method, path, rawQuery)
	}
	if text := automationText(t, res); !strings.Contains(text, "Remote branch reaper") || !strings.Contains(text, "would be reaped") {
		t.Errorf("default format should be the rendered report:\n%s", text)
	}

	// format json returns the daemon body untouched.
	res, err = s.handleAutonomousRemoteReap(context.Background(), callReq(map[string]any{"format": "json"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if text := automationText(t, res); !strings.Contains(text, `"retention_days":30`) {
		t.Errorf("json format should pass the body through:\n%s", text)
	}

	// A bad format is refused before the proxy.
	res, _ = s.handleAutonomousRemoteReap(context.Background(), callReq(map[string]any{"format": "yaml"}))
	if res == nil || !res.IsError {
		t.Error("format yaml must be refused")
	}
}

func TestRemoteReapMCP_RunPostsRoute(t *testing.T) {
	var method, path string
	var rawBody []byte
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		rawBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(remoteReapMCPRun))
	})
	res, err := s.handleAutonomousRemoteReapRun(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/autonomous/remote-reap/run" {
		t.Errorf("proxied %s %s", method, path)
	}
	if len(rawBody) != 0 {
		t.Errorf("the run must send no body, got %q", rawBody)
	}
	if text := automationText(t, res); !strings.Contains(text, "Remote reap pass done") {
		t.Errorf("default format should render the pass:\n%s", text)
	}
}

func TestRemoteReapMCP_RestorePostsBranch(t *testing.T) {
	var method, path string
	var rawBody []byte
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		rawBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(remoteReapMCPRestore))
	})
	res, err := s.handleAutonomousRemoteReapRestore(context.Background(), callReq(map[string]any{"branch": "automaton/prd-a"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/autonomous/remote-reap/restore" {
		t.Errorf("proxied %s %s", method, path)
	}
	if !strings.Contains(string(rawBody), `"branch":"automaton/prd-a"`) {
		t.Errorf("body must carry the branch, got %q", rawBody)
	}
	if text := automationText(t, res); !strings.Contains(text, "restored") {
		t.Errorf("default format should render the restore:\n%s", text)
	}
}

// A restore without a branch is refused before the daemon is hit.
func TestRemoteReapMCP_RestoreRefusesWithoutBranch(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the daemon must not be hit when branch is missing")
		w.WriteHeader(http.StatusUnauthorized)
	})
	for _, args := range []map[string]any{
		nil,
		{"branch": ""},
		{"branch": "   "},
		{"branch": 42},
	} {
		res, err := s.handleAutonomousRemoteReapRestore(context.Background(), callReq(args))
		if err != nil || res == nil || !res.IsError {
			t.Fatalf("args=%v: must be refused as a tool error", args)
		}
	}
}

// The report is marked read-only; the run and the restore are the operator
// writes and must not carry the read-only hint.
func TestRemoteReapMCP_ReadOnlyAnnotations(t *testing.T) {
	s := &Server{}
	if hint := s.toolAutonomousRemoteReap().Annotations.ReadOnlyHint; hint == nil || !*hint {
		t.Error("autonomous_remote_reap must be marked read-only")
	}
	for _, pair := range []struct {
		name string
		hint *bool
	}{
		{s.toolAutonomousRemoteReapRun().Name, s.toolAutonomousRemoteReapRun().Annotations.ReadOnlyHint},
		{s.toolAutonomousRemoteReapRestore().Name, s.toolAutonomousRemoteReapRestore().Annotations.ReadOnlyHint},
	} {
		if pair.hint != nil && *pair.hint {
			t.Errorf("%s must not be marked read-only", pair.name)
		}
	}
}
