// Tests for the sensitive write gate (sensitiveWriteGateMW) composed with
// the config write audit middleware (configWriteAuditMW). Everything runs
// against fake handlers and a temp-dir audit file: no daemon, no network.
//
// The chain mirrors production registration: the audit middleware is the
// outer layer and the gate sits inside it, so the audit sees the refusal
// the gate returns.

package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// sensitiveGateTools is one tool from each family of the sensitive class:
// a secrets write, an identity write and a daemon control call.
var sensitiveGateTools = []struct {
	family string
	tool   string
	args   map[string]any
}{
	{"secrets", "secret_set", map[string]any{"name": "gate-test-key", "value": "super-secret-payload-123"}},
	{"identity", "set_identity", map[string]any{"field": "role", "value": "super-secret-payload-123"}},
	{"daemon", "restart_daemon", map[string]any{}},
}

// gateCaller names the kinds of caller a case runs as.
type gateCaller int

const (
	callerSession gateCaller = iota
	callerPeer
	callerAdmin
)

func (c gateCaller) String() string {
	switch c {
	case callerSession:
		return "session"
	case callerPeer:
		return "peer"
	}
	return "admin"
}

// ctxFor builds the request context for a caller kind and returns the
// identity the audit record is expected to carry ("" for admin).
func (c gateCaller) ctxFor() (context.Context, string) {
	ctx := context.Background()
	switch c {
	case callerSession:
		return federation.WithCallerSessionID(ctx, "sess-gate-1"), "sess-gate-1"
	case callerPeer:
		return context.WithValue(ctx, mcpFedPeerKey, &multiserver.Entry{Name: "peer-gate-1"}), "peer-gate-1"
	}
	return ctx, ""
}

// gateFixture is a server with a temp-dir audit sink, a fake downstream
// handler that counts its calls, and the audit(gate(handler)) chain.
type gateFixture struct {
	s         *Server
	auditPath string
	calls     int
	handler   server.ToolHandlerFunc
}

// newGateFixture builds the chain. cfg may be nil (a server with no MCP
// config at all). fakeErr, when set, makes the fake handler fail.
func newGateFixture(t *testing.T, cfg *config.MCPConfig) *gateFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit", "mcp-config-write.jsonl")
	f := &gateFixture{
		s:         &Server{cfg: cfg, configAudit: federation.NewConfigAuditor(path, true)},
		auditPath: path,
	}
	fake := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		f.calls++
		return mcpsdk.NewToolResultText("done: " + req.Params.Name), nil
	}
	f.handler = configWriteAuditMW(f.s)(sensitiveWriteGateMW(f.s)(fake))
	return f
}

// records returns the audit records written so far; a missing file means
// no record was written.
func (f *gateFixture) records(t *testing.T) []federation.ConfigAuditRecord {
	t.Helper()
	if _, err := os.Stat(f.auditPath); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return readAuditFile(t, f.auditPath)
}

// gateResultText returns the first text content of a tool result.
func gateResultText(res *mcpsdk.CallToolResult) string {
	if res == nil {
		return ""
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func TestSensitiveWriteGate_GateOnAllows(t *testing.T) {
	cases := []struct {
		name   string
		cfg    *config.MCPConfig
		caller gateCaller
	}{
		{"setting absent, session", &config.MCPConfig{}, callerSession},
		{"setting absent, peer", &config.MCPConfig{}, callerPeer},
		{"setting true, session", &config.MCPConfig{AllowSensitiveWrites: config.BoolPtr(true)}, callerSession},
		{"setting true, peer", &config.MCPConfig{AllowSensitiveWrites: config.BoolPtr(true)}, callerPeer},
	}
	for _, tool := range sensitiveGateTools {
		for _, tc := range cases {
			t.Run(tool.family+"/"+tool.tool+"/"+tc.name, func(t *testing.T) {
				f := newGateFixture(t, tc.cfg)
				ctx, wantCaller := tc.caller.ctxFor()
				res, err := f.handler(ctx, cfgCallReq(tool.tool, tool.args))
				if err != nil {
					t.Fatalf("call: %v", err)
				}
				if res == nil || res.IsError {
					t.Fatalf("gate on must allow, got %+v", res)
				}
				if f.calls != 1 {
					t.Errorf("downstream handler calls = %d, want 1", f.calls)
				}
				recs := f.records(t)
				if len(recs) != 1 {
					t.Fatalf("expected exactly 1 audit record, got %d: %+v", len(recs), recs)
				}
				rec := recs[0]
				if !rec.Allowed || rec.Reason != "" {
					t.Errorf("record must be allowed with no reason: %+v", rec)
				}
				if rec.Surface != "mcp" || rec.Tool != tool.tool || rec.Class != auditClassSensitive {
					t.Errorf("record identity: %+v", rec)
				}
				if rec.Caller != wantCaller {
					t.Errorf("caller = %q, want %q", rec.Caller, wantCaller)
				}
			})
		}
	}
}

func TestSensitiveWriteGate_GateOffRefusesSessionAndPeer(t *testing.T) {
	for _, tool := range sensitiveGateTools {
		for _, caller := range []gateCaller{callerSession, callerPeer} {
			t.Run(tool.family+"/"+tool.tool+"/"+caller.String(), func(t *testing.T) {
				f := newGateFixture(t, &config.MCPConfig{AllowSensitiveWrites: config.BoolPtr(false)})
				ctx, wantCaller := caller.ctxFor()
				res, err := f.handler(ctx, cfgCallReq(tool.tool, tool.args))
				if err != nil {
					t.Fatalf("a refusal is a tool error result, not a Go error: %v", err)
				}
				if res == nil || !res.IsError {
					t.Fatalf("gate off must refuse, got %+v", res)
				}
				if f.calls != 0 {
					t.Errorf("downstream handler must not run when refused, calls = %d", f.calls)
				}
				msg := gateResultText(res)
				if msg != sensitiveWriteDeniedMessage(tool.tool) {
					t.Errorf("refusal text = %q, want %q", msg, sensitiveWriteDeniedMessage(tool.tool))
				}
				for _, want := range []string{tool.tool, "mcp.allow_sensitive_writes", "disabled", "re-enable"} {
					if !strings.Contains(msg, want) {
						t.Errorf("refusal text %q does not mention %q", msg, want)
					}
				}
				recs := f.records(t)
				if len(recs) != 1 {
					t.Fatalf("expected exactly 1 audit record, got %d: %+v", len(recs), recs)
				}
				rec := recs[0]
				if rec.Allowed {
					t.Errorf("refused call must be recorded as denied: %+v", rec)
				}
				if rec.Surface != "mcp" || rec.Tool != tool.tool || rec.Class != auditClassSensitive {
					t.Errorf("record identity: %+v", rec)
				}
				if rec.Caller != wantCaller {
					t.Errorf("caller = %q, want %q", rec.Caller, wantCaller)
				}
				if !strings.Contains(rec.Reason, "mcp.allow_sensitive_writes") {
					t.Errorf("reason = %q, want it to name the setting", rec.Reason)
				}
			})
		}
	}
}

func TestSensitiveWriteGate_AdminUnaffectedWhenOff(t *testing.T) {
	for _, tool := range sensitiveGateTools {
		t.Run(tool.family+"/"+tool.tool, func(t *testing.T) {
			f := newGateFixture(t, &config.MCPConfig{AllowSensitiveWrites: config.BoolPtr(false)})
			ctx, _ := callerAdmin.ctxFor()
			res, err := f.handler(ctx, cfgCallReq(tool.tool, tool.args))
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if res == nil || res.IsError {
				t.Fatalf("admin must be unaffected with the gate off, got %+v", res)
			}
			if f.calls != 1 {
				t.Errorf("downstream handler calls = %d, want 1", f.calls)
			}
			// Admin calls are not audited as session writes (same as before).
			if recs := f.records(t); len(recs) != 0 {
				t.Errorf("admin call must not produce a session-write record, got %+v", recs)
			}
		})
	}
}

// TestSensitiveWriteGate_DefaultConfigUnchanged pins that a server with the
// setting absent behaves exactly as it did before the setting existed, and
// the same as an explicit true: a nil MCP config, an empty config and an
// explicit-true config all allow every caller kind.
func TestSensitiveWriteGate_DefaultConfigUnchanged(t *testing.T) {
	configs := []struct {
		name string
		cfg  *config.MCPConfig
	}{
		{"nil config", nil},
		{"empty config (setting absent)", &config.MCPConfig{}},
		{"explicit true", &config.MCPConfig{AllowSensitiveWrites: config.BoolPtr(true)}},
	}
	for _, tool := range sensitiveGateTools {
		for _, c := range configs {
			for _, caller := range []gateCaller{callerSession, callerPeer, callerAdmin} {
				t.Run(tool.family+"/"+tool.tool+"/"+c.name+"/"+caller.String(), func(t *testing.T) {
					f := newGateFixture(t, c.cfg)
					ctx, _ := caller.ctxFor()
					res, err := f.handler(ctx, cfgCallReq(tool.tool, tool.args))
					if err != nil || res == nil || res.IsError {
						t.Fatalf("default must allow: res=%+v err=%v", res, err)
					}
					if got := gateResultText(res); got != "done: "+tool.tool {
						t.Errorf("result text = %q, want the downstream handler's result", got)
					}
					if f.calls != 1 {
						t.Errorf("downstream handler calls = %d, want 1", f.calls)
					}
					wantRecs := 1
					if caller == callerAdmin {
						wantRecs = 0
					}
					if recs := f.records(t); len(recs) != wantRecs {
						t.Errorf("audit records = %d, want %d: %+v", len(recs), wantRecs, recs)
					}
				})
			}
		}
	}
}

// TestSensitiveWriteGate_EveryCallOneRecord runs allowed and refused calls
// back to back for each tool and checks the audit file holds exactly one
// record per call, in order, with the matching decision.
func TestSensitiveWriteGate_EveryCallOneRecord(t *testing.T) {
	for _, tool := range sensitiveGateTools {
		t.Run(tool.family+"/"+tool.tool, func(t *testing.T) {
			cfg := &config.MCPConfig{}
			f := newGateFixture(t, cfg)
			steps := []struct {
				enabled bool
				caller  gateCaller
			}{
				{true, callerSession},
				{false, callerSession},
				{true, callerPeer},
				{false, callerPeer},
				{false, callerSession},
				{true, callerSession},
			}
			for i, st := range steps {
				cfg.AllowSensitiveWrites = config.BoolPtr(st.enabled)
				ctx, _ := st.caller.ctxFor()
				res, err := f.handler(ctx, cfgCallReq(tool.tool, tool.args))
				if err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
				if refused := res != nil && res.IsError; refused == st.enabled {
					t.Fatalf("step %d: enabled=%v but refused=%v", i, st.enabled, refused)
				}
			}
			recs := f.records(t)
			if len(recs) != len(steps) {
				t.Fatalf("audit records = %d, want %d (one per call): %+v", len(recs), len(steps), recs)
			}
			for i, st := range steps {
				if recs[i].Allowed != st.enabled {
					t.Errorf("record %d: allowed = %v, want %v", i, recs[i].Allowed, st.enabled)
				}
				if recs[i].Tool != tool.tool {
					t.Errorf("record %d: tool = %q, want %q", i, recs[i].Tool, tool.tool)
				}
			}
			wantCalls := 0
			for _, st := range steps {
				if st.enabled {
					wantCalls++
				}
			}
			if f.calls != wantCalls {
				t.Errorf("downstream handler calls = %d, want %d (refused calls never reach it)", f.calls, wantCalls)
			}
		})
	}
}

// TestSensitiveWriteGate_AuditNeverRecordsValues checks that neither the
// allowed nor the refused record carries argument values for a sensitive
// tool: only the argument names are kept.
func TestSensitiveWriteGate_AuditNeverRecordsValues(t *testing.T) {
	const secret = "super-secret-payload-123"
	for _, enabled := range []bool{true, false} {
		for _, tool := range sensitiveGateTools[:2] { // secret_set, set_identity carry the value
			name := tool.tool + "/allowed"
			if !enabled {
				name = tool.tool + "/refused"
			}
			t.Run(name, func(t *testing.T) {
				f := newGateFixture(t, &config.MCPConfig{AllowSensitiveWrites: config.BoolPtr(enabled)})
				ctx, _ := callerSession.ctxFor()
				if _, err := f.handler(ctx, cfgCallReq(tool.tool, tool.args)); err != nil {
					t.Fatalf("call: %v", err)
				}
				recs := f.records(t)
				if len(recs) != 1 {
					t.Fatalf("audit records = %d, want 1", len(recs))
				}
				if recs[0].Values != nil {
					t.Errorf("values must not be recorded for a sensitive tool: %+v", recs[0].Values)
				}
				raw, err := os.ReadFile(f.auditPath)
				if err != nil {
					t.Fatalf("read audit file: %v", err)
				}
				if strings.Contains(string(raw), secret) {
					t.Errorf("audit file leaks the argument value: %s", raw)
				}
			})
		}
	}
}

// TestSensitiveWriteGate_OtherToolsUnaffected checks the gate only touches
// the sensitive class: a tool outside it passes through for a session
// caller with the gate off, and produces no sensitive-class record.
func TestSensitiveWriteGate_OtherToolsUnaffected(t *testing.T) {
	for _, tool := range []string{"list_sessions", "get_version"} {
		t.Run(tool, func(t *testing.T) {
			if federation.IsSensitiveWriteTool(tool) {
				t.Fatalf("%s is expected to be outside the sensitive class", tool)
			}
			f := newGateFixture(t, &config.MCPConfig{AllowSensitiveWrites: config.BoolPtr(false)})
			ctx, _ := callerSession.ctxFor()
			res, err := f.handler(ctx, cfgCallReq(tool, nil))
			if err != nil || res == nil || res.IsError {
				t.Fatalf("non-sensitive tool must pass: res=%+v err=%v", res, err)
			}
			if f.calls != 1 {
				t.Errorf("downstream handler calls = %d, want 1", f.calls)
			}
			for _, rec := range f.records(t) {
				if rec.Class == auditClassSensitive {
					t.Errorf("unexpected sensitive-class record: %+v", rec)
				}
			}
		})
	}
}

// TestSensitiveWriteGate_DirectMiddleware drives the gate alone (no audit
// layer) to pin the refusal result itself.
func TestSensitiveWriteGate_DirectMiddleware(t *testing.T) {
	s := &Server{cfg: &config.MCPConfig{AllowSensitiveWrites: config.BoolPtr(false)}}
	called := false
	next := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		called = true
		return okRes(), nil
	}
	h := sensitiveWriteGateMW(s)(next)
	for _, tool := range sensitiveGateTools {
		called = false
		ctx, _ := callerSession.ctxFor()
		res, err := h(ctx, cfgCallReq(tool.tool, tool.args))
		if err != nil || res == nil || !res.IsError {
			t.Fatalf("%s: want error result, got res=%+v err=%v", tool.tool, res, err)
		}
		if called {
			t.Errorf("%s: handler ran despite the refusal", tool.tool)
		}
	}
}
