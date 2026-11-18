package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The autonomous config MCP tools for autonomous.task_templates.mode,
// autonomous.script_gate_timeout_seconds and autonomous.project_gate_command:
// get returns the stored values, a valid set sends one config PUT, and each
// refused value comes back as a tool error with no request sent to the daemon.

// daemonTaskKeysServer serves the two reads the get tool makes and counts
// every request, so a refusal can be shown to have written nothing.
func daemonTaskKeysServer(t *testing.T, calls *int, lastBody *map[string]any) *Server {
	t.Helper()
	return capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		*calls++
		switch {
		case r.URL.Path == "/api/autonomous/config":
			_, _ = w.Write([]byte(`{"enabled":true,"task_templates":{"enabled":true,"templates":[]}}`))
		case r.URL.Path == "/api/config" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"autonomous":{` +
				`"task_templates":{"mode":{"changelog_entry":"daemon"}},` +
				`"script_gate_timeout_seconds":120,` +
				`"project_gate_command":"make check"}}`))
		case r.URL.Path == "/api/config" && r.Method == http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			m := map[string]any{}
			_ = json.Unmarshal(b, &m)
			*lastBody = m
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	})
}

func TestAutonomousConfigDaemonTaskKeysGetReturnsStoredValues(t *testing.T) {
	calls := 0
	var body map[string]any
	s := daemonTaskKeysServer(t, &calls, &body)
	res, err := s.handleAutonomousConfigGet(context.Background(), callReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("get: err=%v res=%+v", err, res)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(resContentText(t, res)), &got); err != nil {
		t.Fatal(err)
	}
	if got["script_gate_timeout_seconds"] != float64(120) {
		t.Errorf("script_gate_timeout_seconds = %v, want 120", got["script_gate_timeout_seconds"])
	}
	if got["project_gate_command"] != "make check" {
		t.Errorf("project_gate_command = %v, want \"make check\"", got["project_gate_command"])
	}
	tt, ok := got["task_templates"].(map[string]any)
	if !ok {
		t.Fatalf("task_templates = %v, want an object", got["task_templates"])
	}
	mode, _ := tt["mode"].(map[string]any)
	if mode["changelog_entry"] != "daemon" {
		t.Errorf("task_templates.mode = %v, want changelog_entry=daemon", tt["mode"])
	}
	// The manager's own task_templates fields must survive the merge.
	if tt["enabled"] != true {
		t.Errorf("task_templates.enabled lost in merge: %v", tt)
	}
	if got["enabled"] != true {
		t.Errorf("existing manager key lost in merge: %v", got)
	}
}

func TestAutonomousConfigDaemonTaskKeysSetValidWritesOneConfigPut(t *testing.T) {
	calls := 0
	var body map[string]any
	s := daemonTaskKeysServer(t, &calls, &body)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		daemonTaskModeArg:             "changelog=off, final_gate=llm",
		"script_gate_timeout_seconds": float64(0),
		"project_gate_command":        "go test ./...",
	}))
	if err != nil || res.IsError {
		t.Fatalf("set: err=%v res=%+v", err, res)
	}
	if calls != 1 {
		t.Fatalf("requests = %d, want 1 (config PUT only)", calls)
	}
	want := map[string]any{
		"autonomous.task_templates.mode.changelog":  "off",
		"autonomous.task_templates.mode.final_gate": "llm",
		"autonomous.script_gate_timeout_seconds":    float64(0),
		"autonomous.project_gate_command":           "go test ./...",
	}
	for k, v := range want {
		if got, ok := body[k]; !ok || got != v {
			t.Errorf("PUT body %s = %v (present=%v), want %v", k, got, ok, v)
		}
	}
}

func TestAutonomousConfigDaemonTaskKeysSetAcceptsJSONModeObject(t *testing.T) {
	calls := 0
	var body map[string]any
	s := daemonTaskKeysServer(t, &calls, &body)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		daemonTaskModeArg: map[string]any{"changelog_entry": "daemon"},
	}))
	if err != nil || res.IsError {
		t.Fatalf("set: err=%v res=%+v", err, res)
	}
	if got := body["autonomous.task_templates.mode.changelog_entry"]; got != "daemon" {
		t.Errorf("changelog_entry mode = %v, want daemon", got)
	}
}

func TestAutonomousConfigDaemonTaskKeysRefuseUnknownMode(t *testing.T) {
	calls := 0
	var body map[string]any
	s := daemonTaskKeysServer(t, &calls, &body)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		daemonTaskModeArg: "changelog=sometimes",
	}))
	assertDaemonTaskRefusal(t, res, err, calls, "autonomous.task_templates.mode")
}

func TestAutonomousConfigDaemonTaskKeysRefuseDaemonOnFinalGate(t *testing.T) {
	calls := 0
	var body map[string]any
	s := daemonTaskKeysServer(t, &calls, &body)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		daemonTaskModeArg: "final_gate=daemon",
	}))
	assertDaemonTaskRefusal(t, res, err, calls, "final_gate")
}

func TestAutonomousConfigDaemonTaskKeysRefuseNegativeScriptGateTimeout(t *testing.T) {
	calls := 0
	var body map[string]any
	s := daemonTaskKeysServer(t, &calls, &body)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"script_gate_timeout_seconds": float64(-5),
	}))
	assertDaemonTaskRefusal(t, res, err, calls, "script_gate_timeout_seconds")
}

func TestAutonomousConfigDaemonTaskKeysRefuseFractionalScriptGateTimeout(t *testing.T) {
	calls := 0
	var body map[string]any
	s := daemonTaskKeysServer(t, &calls, &body)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"script_gate_timeout_seconds": 1.5,
	}))
	assertDaemonTaskRefusal(t, res, err, calls, "script_gate_timeout_seconds")
}

func TestAutonomousConfigDaemonTaskKeysRefuseMultilineGateCommand(t *testing.T) {
	calls := 0
	var body map[string]any
	s := daemonTaskKeysServer(t, &calls, &body)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"project_gate_command": "make test\nmake lint",
	}))
	assertDaemonTaskRefusal(t, res, err, calls, "project_gate_command")
}

// A refusal in one of the three keys must not let the valid ones through:
// the whole set is refused and nothing is sent.
func TestAutonomousConfigDaemonTaskKeysRefusalStoresNothing(t *testing.T) {
	calls := 0
	var body map[string]any
	s := daemonTaskKeysServer(t, &calls, &body)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"project_gate_command":        "make test",
		"script_gate_timeout_seconds": float64(-1),
	}))
	assertDaemonTaskRefusal(t, res, err, calls, "script_gate_timeout_seconds")
}

func TestAutonomousConfigDaemonTaskKeysSchemaNamesNewKeys(t *testing.T) {
	s := &Server{}
	b, err := json.Marshal(s.toolAutonomousConfigSet())
	if err != nil {
		t.Fatal(err)
	}
	var tool struct {
		Description string `json:"description"`
		InputSchema struct {
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		} `json:"inputSchema"`
	}
	if err := json.Unmarshal(b, &tool); err != nil {
		t.Fatal(err)
	}
	for k, typ := range map[string]string{
		daemonTaskModeArg:             "string",
		"script_gate_timeout_seconds": "number",
		"project_gate_command":        "string",
	} {
		p, ok := tool.InputSchema.Properties[k]
		if !ok || p.Type != typ {
			t.Errorf("autonomous_config_set schema: %s = %+v, want %s", k, p, typ)
		}
	}
	for _, want := range []string{"autonomous.task_templates.mode", "llm, daemon or off", "autonomous.script_gate_timeout_seconds", "autonomous.project_gate_command"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("autonomous_config_set description does not name %q", want)
		}
	}
}

// assertDaemonTaskRefusal checks a refused set: a tool error that names the
// refused key, and no request reached the daemon.
func assertDaemonTaskRefusal(t *testing.T, res *mcpsdk.CallToolResult, err error, calls int, wantKey string) {
	t.Helper()
	if err != nil {
		t.Fatalf("refusal returned a Go error, want a tool error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("refusal result = %+v, want a tool error", res)
	}
	if calls != 0 {
		t.Errorf("refused set sent %d request(s), want 0", calls)
	}
	if msg := resContentText(t, res); !strings.Contains(msg, wantKey) {
		t.Errorf("refusal message %q does not name %q", msg, wantKey)
	}
}

func TestAutonomousConfigGateDrainSetWritesOnePut(t *testing.T) {
	calls := 0
	var body map[string]any
	s := daemonTaskKeysServer(t, &calls, &body)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"gate_drain_minutes": float64(0),
	}))
	if err != nil || res.IsError {
		t.Fatalf("set: err=%v res=%+v", err, res)
	}
	if calls != 1 || body["autonomous.gate_drain_minutes"] != float64(0) {
		t.Fatalf("calls=%d body=%v", calls, body)
	}
}

func TestAutonomousConfigGateDrainRefusals(t *testing.T) {
	for _, v := range []any{float64(-3), 1.5, "ten"} {
		calls := 0
		var body map[string]any
		s := daemonTaskKeysServer(t, &calls, &body)
		res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"gate_drain_minutes": v}))
		assertDaemonTaskRefusal(t, res, err, calls, "gate_drain_minutes")
	}
}

func TestAutonomousConfigGateDrainGetMerged(t *testing.T) {
	merged, err := mergeDaemonTaskSettings([]byte(`{"enabled":true}`), map[string]json.RawMessage{"gate_drain_minutes": json.RawMessage(`10`)})
	if err != nil || !strings.Contains(string(merged), `"gate_drain_minutes":10`) {
		t.Fatalf("merged=%s err=%v", merged, err)
	}
}
