package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// readAuditFile returns the decoded JSON-lines records from a config
// audit file.
func readAuditFile(t *testing.T, path string) []federation.ConfigAuditRecord {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file %q: %v", path, err)
	}
	var recs []federation.ConfigAuditRecord
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var rec federation.ConfigAuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unmarshal audit line %q: %v", line, err)
		}
		recs = append(recs, rec)
	}
	return recs
}

func cfgAuditServer(t *testing.T) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit", "mcp-config-write.jsonl")
	s := &Server{
		cfg:         &config.MCPConfig{},
		configAudit: federation.NewConfigAuditor(path, true),
	}
	return s, path
}

func cfgCallReq(name string, args map[string]any) mcpsdk.CallToolRequest {
	return mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{Name: name, Arguments: args}}
}

func okRes() *mcpsdk.CallToolResult { return mcpsdk.NewToolResultText("ok") }

func errRes(msg string) *mcpsdk.CallToolResult { return mcpsdk.NewToolResultError(msg) }

func TestConfigWriteAudit_Allowed_OneRecord(t *testing.T) {
	s, path := cfgAuditServer(t)
	next := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return okRes(), nil
	}
	mw := configWriteAuditMW(s)
	mw(next) // sanity: wrap
	h := mw(next)
	ctx := federation.WithCallerSessionID(context.Background(), "sess-test-1")
	_, err := h(ctx, cfgCallReq("config_set", map[string]any{"llm.default": "claude"}))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	recs := readAuditFile(t, path)
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 audit record, got %d: %+v", len(recs), recs)
	}
	rec := recs[0]
	if rec.Surface != "mcp" || rec.Tool != "config_set" || !rec.Allowed {
		t.Errorf("record fields: %+v", rec)
	}
	if rec.Caller != "sess-test-1" {
		t.Errorf("caller = %q, want sess-test-1", rec.Caller)
	}
	if len(rec.Keys) != 1 || rec.Keys[0] != "llm.default" {
		t.Errorf("keys = %v, want [llm.default]", rec.Keys)
	}
	if rec.Values["llm.default"] != "claude" {
		t.Errorf("values = %v, want llm.default=claude", rec.Values)
	}
}

func TestConfigWriteAudit_Denied_RecordsReason(t *testing.T) {
	s, path := cfgAuditServer(t)
	next := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return errRes("permission denied: self-modify is disabled (mcp.allow_self_config: false)"), nil
	}
	h := configWriteAuditMW(s)(next)
	ctx := federation.WithCallerSessionID(context.Background(), "sess-deny-1")
	res, _ := h(ctx, cfgCallReq("config_set", map[string]any{"llm.default": "claude"}))
	if res == nil || !res.IsError {
		t.Fatalf("expected error result, got %+v", res)
	}
	recs := readAuditFile(t, path)
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 audit record, got %d", len(recs))
	}
	rec := recs[0]
	if rec.Allowed {
		t.Errorf("allowed = true, want false")
	}
	if !strings.Contains(rec.Reason, "permission denied") {
		t.Errorf("reason = %q, want to contain 'permission denied'", rec.Reason)
	}
}

func TestConfigWriteAudit_BootstrapProtection_RefusalAudited(t *testing.T) {
	// The handler refuses mcp.allow_self_config (bootstrap-protected); the
	// audit middleware records the refusal as a denied record naming the key.
	s, path := cfgAuditServer(t)
	next := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.Params.Arguments.(map[string]any)
		if args["mcp.allow_self_config"] != nil {
			return errRes("refused: mcp.allow_self_config is bootstrap-protected; edit the YAML directly to change it"), nil
		}
		return okRes(), nil
	}
	h := configWriteAuditMW(s)(next)
	ctx := federation.WithCallerSessionID(context.Background(), "sess-boot-1")
	res, _ := h(ctx, cfgCallReq("config_set", map[string]any{"mcp.allow_self_config": true}))
	if res == nil || !res.IsError {
		t.Fatalf("bootstrap key must be refused, got %+v", res)
	}
	recs := readAuditFile(t, path)
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 audit record, got %d", len(recs))
	}
	rec := recs[0]
	if rec.Allowed {
		t.Errorf("bootstrap refusal must be allowed=false")
	}
	if len(rec.Keys) != 1 || rec.Keys[0] != "mcp.allow_self_config" {
		t.Errorf("keys = %v, want [mcp.allow_self_config]", rec.Keys)
	}
}

func TestConfigWriteAudit_PeerCaller_Audited(t *testing.T) {
	s, path := cfgAuditServer(t)
	next := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return okRes(), nil
	}
	h := configWriteAuditMW(s)(next)
	ctx := context.WithValue(context.Background(), mcpFedPeerKey, &multiserver.Entry{Name: "peer-a"})
	_, _ = h(ctx, cfgCallReq("council_config_set", map[string]any{"enabled": true}))
	recs := readAuditFile(t, path)
	if len(recs) != 1 || recs[0].Caller != "peer-a" {
		t.Errorf("peer record missing: %+v", recs)
	}
}

func TestConfigWriteAudit_Admin_NoRecord(t *testing.T) {
	s, path := cfgAuditServer(t)
	next := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return okRes(), nil
	}
	h := configWriteAuditMW(s)(next)
	// No peer, no session — admin write.
	_, _ = h(context.Background(), cfgCallReq("config_set", map[string]any{"llm.default": "claude"}))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("admin call must not produce an audit file; stat err = %v", err)
	}
}

func TestConfigWriteAudit_NonConfigTool_NoRecord(t *testing.T) {
	s, path := cfgAuditServer(t)
	next := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return okRes(), nil
	}
	h := configWriteAuditMW(s)(next)
	ctx := federation.WithCallerSessionID(context.Background(), "sess-x")
	_, _ = h(ctx, cfgCallReq("session_list", map[string]any{}))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("non-config tool must not produce an audit file; stat err = %v", err)
	}
}

func TestConfigWriteAudit_SecretValueRedacted(t *testing.T) {
	s, path := cfgAuditServer(t)
	next := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return okRes(), nil
	}
	h := configWriteAuditMW(s)(next)
	ctx := federation.WithCallerSessionID(context.Background(), "sess-sec")
	_, _ = h(ctx, cfgCallReq("config_set", map[string]any{"gitlab.token": "glpat-SECRET123", "llm.default": "claude"}))
	recs := readAuditFile(t, path)
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if got := recs[0].Values["gitlab.token"]; got != "<redacted>" {
		t.Errorf("secret value = %q, want <redacted>", got)
	}
	var valuesJoined string
	for _, vv := range recs[0].Values {
		valuesJoined += "," + vv
	}
	if strings.Contains(valuesJoined, "glpat-SECRET123") {
		t.Errorf("secret leaked into audit record: %+v", recs[0].Values)
	}
}

func TestConfigWriteAudit_NilAuditor_NoCrash(t *testing.T) {
	s := &Server{cfg: &config.MCPConfig{}} // configAudit nil
	next := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return okRes(), nil
	}
	h := configWriteAuditMW(s)(next)
	ctx := federation.WithCallerSessionID(context.Background(), "sess-nil")
	res, err := h(ctx, cfgCallReq("config_set", map[string]any{"a": "b"}))
	if err != nil || res == nil {
		t.Fatalf("nil auditor must be a no-op, got res=%+v err=%v", res, err)
	}
}

func TestConfigWriteAudit_RecordDeniedDisabled_Drop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.jsonl")
	s := &Server{cfg: &config.MCPConfig{}, configAudit: federation.NewConfigAuditor(path, false)}
	next := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return errRes("denied"), nil
	}
	h := configWriteAuditMW(s)(next)
	ctx := federation.WithCallerSessionID(context.Background(), "sess-drop")
	_, _ = h(ctx, cfgCallReq("config_set", map[string]any{"a": "b"}))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("denied record must be dropped when recordDenied=false; stat err = %v", err)
	}
	// But an allowed call still lands.
	h2 := configWriteAuditMW(s)(func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return okRes(), nil
	})
	_, _ = h2(ctx, cfgCallReq("config_set", map[string]any{"a": "b"}))
	recs := readAuditFile(t, path)
	if len(recs) != 1 || !recs[0].Allowed {
		t.Errorf("allowed record must still land, got %+v", recs)
	}
}

func TestConfigWriteAudit_SensitiveTool_RecordedWithClass(t *testing.T) {
	s, path := cfgAuditServer(t)
	allow := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return okRes(), nil
	}
	deny := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return errRes("refused"), nil
	}
	ctx := federation.WithCallerSessionID(context.Background(), "sess-sens-1")
	args := map[string]any{"name": "db", "value": "hunter2-plain"}
	if _, err := configWriteAuditMW(s)(allow)(ctx, cfgCallReq("secret_set", args)); err != nil {
		t.Fatalf("allowed call: %v", err)
	}
	if _, err := configWriteAuditMW(s)(deny)(ctx, cfgCallReq("secret_set", args)); err != nil {
		t.Fatalf("denied call: %v", err)
	}
	recs := readAuditFile(t, path)
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d: %+v", len(recs), recs)
	}
	for i, rec := range recs {
		if rec.Surface != "mcp" || rec.Tool != "secret_set" || rec.Class != "sensitive" || rec.Caller != "sess-sens-1" {
			t.Errorf("record %d fields: %+v", i, rec)
		}
		if len(rec.Values) != 0 {
			t.Errorf("record %d carries argument values: %+v", i, rec.Values)
		}
	}
	if !recs[0].Allowed || recs[1].Allowed || recs[1].Reason == "" {
		t.Errorf("allowed/denied mismatch: %+v", recs)
	}
}

func TestConfigWriteAudit_ConfigToolClass_AndUnclassifiedSkipped(t *testing.T) {
	s, path := cfgAuditServer(t)
	next := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return okRes(), nil
	}
	ctx := federation.WithCallerSessionID(context.Background(), "sess-c")
	h := configWriteAuditMW(s)(next)
	_, _ = h(ctx, cfgCallReq("config_set", map[string]any{"a.b": "c"}))
	_, _ = h(ctx, cfgCallReq("list_sessions", nil))
	recs := readAuditFile(t, path)
	if len(recs) != 1 || recs[0].Class != "config" {
		t.Fatalf("want one config-class record, got %+v", recs)
	}
}
