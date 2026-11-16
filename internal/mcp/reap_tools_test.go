// MCP branch reaper tools: the report proxies GET /api/autonomous/branch-reap
// and is marked read-only; the run and the restore proxy the two POST routes
// and are the operator writes.

package mcp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

const reapMCPReport = `{"reaped":[{"automaton_id":"prd-a","branch":"automaton-prd-a--tasks-fix-t1","commits":3,"reason":"settled"}],
"waiting":[],"backups":[{"branch":"automaton-prd-a--tasks-fix-t1","ref":"refs/datawatch/reap-backup/prd-a/1757220000","sha":"3f2a"}],
"backup_days":30}`

const reapMCPRun = `{"reaped":[{"branch":"automaton-prd-a--tasks-fix-t1","backup_ref":"refs/datawatch/reap-backup/prd-a/1757220000","commits":3,"reason":"settled"}],"waiting":[]}`

const reapMCPRestore = `{"branch":"automaton-prd-a--tasks-fix-t1","backup_ref":"refs/datawatch/reap-backup/prd-a/1757220000","sha":"3f2a"}`

func TestBranchReapMCP_ToolsRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range mergeTestToolNames(t) {
		have[n] = true
	}
	for _, want := range ReapToolNames() {
		if !have[want] {
			t.Errorf("tool %q is not registered", want)
		}
	}
}

func TestBranchReapMCP_ReportReadsRoute(t *testing.T) {
	var method, path, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, rawQuery = r.Method, r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(reapMCPReport))
	})
	res, err := s.handleAutonomousBranchReap(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/branch-reap" || rawQuery != "" {
		t.Errorf("proxied %s %s?%s", method, path, rawQuery)
	}
	if text := automationText(t, res); !strings.Contains(text, "Would be reaped") || !strings.Contains(text, "Backup refs") {
		t.Errorf("default format should be the rendered report:\n%s", text)
	}

	// format json returns the daemon body untouched.
	res, err = s.handleAutonomousBranchReap(context.Background(), callReq(map[string]any{"format": "json"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if text := automationText(t, res); !strings.Contains(text, `"backup_days":30`) {
		t.Errorf("json format should pass the body through:\n%s", text)
	}

	// A bad format is refused before the proxy.
	res, _ = s.handleAutonomousBranchReap(context.Background(), callReq(map[string]any{"format": "yaml"}))
	if res == nil || !res.IsError {
		t.Error("format yaml must be refused")
	}
}

func TestBranchReapMCP_RunPostsRoute(t *testing.T) {
	var method, path, rawQuery string
	var body io.Reader
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, rawQuery = r.Method, r.URL.Path, r.URL.RawQuery
		body = r.Body
		_, _ = w.Write([]byte(reapMCPRun))
	})
	res, err := s.handleAutonomousBranchReapRun(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/autonomous/branch-reap/run" || rawQuery != "" {
		t.Errorf("proxied %s %s?%s", method, path, rawQuery)
	}
	if raw, _ := io.ReadAll(body); len(raw) != 0 {
		t.Errorf("the run must send no body, got %q", raw)
	}
	if text := automationText(t, res); !strings.Contains(text, "Branch reap pass done") {
		t.Errorf("default format should render the pass:\n%s", text)
	}
}

func TestBranchReapMCP_RestorePostsBranch(t *testing.T) {
	var method, path string
	var rawBody []byte
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		rawBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(reapMCPRestore))
	})
	res, err := s.handleAutonomousBranchReapRestore(context.Background(), callReq(map[string]any{"branch": "automaton-prd-a--tasks-fix-t1"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/autonomous/branch-reap/restore" {
		t.Errorf("proxied %s %s", method, path)
	}
	if !strings.Contains(string(rawBody), `"branch":"automaton-prd-a--tasks-fix-t1"`) {
		t.Errorf("body must carry the branch, got %q", rawBody)
	}
	if text := automationText(t, res); !strings.Contains(text, "Branch restored") {
		t.Errorf("default format should render the restore:\n%s", text)
	}
}

// A restore without a branch is refused before the daemon is hit.
func TestBranchReapMCP_RestoreRefusesWithoutBranch(t *testing.T) {
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
		res, err := s.handleAutonomousBranchReapRestore(context.Background(), callReq(args))
		if err != nil || res == nil || !res.IsError {
			t.Fatalf("args=%v: must be refused as a tool error", args)
		}
	}
}

// The report is marked read-only; the run and the restore are the operator
// writes and must not carry the read-only hint.
func TestBranchReapMCP_ReadOnlyAnnotations(t *testing.T) {
	s := &Server{}
	if hint := s.toolAutonomousBranchReap().Annotations.ReadOnlyHint; hint == nil || !*hint {
		t.Error("autonomous_branch_reap must be marked read-only")
	}
	for _, pair := range []struct {
		name string
		hint *bool
	}{
		{s.toolAutonomousBranchReapRun().Name, s.toolAutonomousBranchReapRun().Annotations.ReadOnlyHint},
		{s.toolAutonomousBranchReapRestore().Name, s.toolAutonomousBranchReapRestore().Annotations.ReadOnlyHint},
	} {
		if pair.hint != nil && *pair.hint {
			t.Errorf("%s must not be marked read-only", pair.name)
		}
	}
}
