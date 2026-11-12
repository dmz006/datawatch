package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// The three attempt-cost keys on the autonomous config MCP tools and the
// attempt-record fields on autonomous_status: get returns the stored values
// and set writes them to PUT /api/config, a refused set writes nothing and
// names the key, and the status body carries the per-attempt backend, model,
// backend reason, resumed flag, resume reason and no-retry reason.

// attemptCostDaemon serves the two GETs the get and set paths read and
// records every PUT body. stored is the autonomous block of GET /api/config.
type attemptCostDaemon struct {
	stored string
	puts   []map[string]any
	calls  int
}

func (d *attemptCostDaemon) handler(w http.ResponseWriter, r *http.Request) {
	d.calls++
	switch {
	case r.URL.Path == "/api/autonomous/config":
		_, _ = w.Write([]byte(`{"enabled":true}`))
	case r.URL.Path == "/api/config" && r.Method == http.MethodGet:
		_, _ = w.Write([]byte(`{"autonomous":` + d.stored + `}`))
	case r.URL.Path == "/api/config" && r.Method == http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		d.puts = append(d.puts, body)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	case r.URL.Path == "/api/autonomous/config" && r.Method == http.MethodPut:
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	default:
		http.NotFound(w, r)
	}
}

const attemptCostStoredBlock = `{
	"enabled": true,
	"first_attempt": {"implementation": {"backend": "pi-prompt", "model": "qwen3.8:27b"}},
	"first_attempt_escalate_after_failures": 2,
	"retry_resume_max_context_tokens": 90000
}`

func TestAttemptCostTools_GetReturnsThreeKeys(t *testing.T) {
	d := &attemptCostDaemon{stored: attemptCostStoredBlock}
	s := capacityTestServer(t, d.handler)
	res, err := s.handleAutonomousConfigGet(context.Background(), callReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("get: err=%v res=%+v", err, res)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(resContentText(t, res)), &got); err != nil {
		t.Fatal(err)
	}
	if got["first_attempt_escalate_after_failures"] != float64(2) {
		t.Errorf("first_attempt_escalate_after_failures = %v, want 2", got["first_attempt_escalate_after_failures"])
	}
	if got["retry_resume_max_context_tokens"] != float64(90000) {
		t.Errorf("retry_resume_max_context_tokens = %v, want 90000", got["retry_resume_max_context_tokens"])
	}
	fa, ok := got["first_attempt"].(map[string]any)
	if !ok {
		t.Fatalf("first_attempt = %v, want an object", got["first_attempt"])
	}
	impl, _ := fa["implementation"].(map[string]any)
	if impl["backend"] != "pi-prompt" || impl["model"] != "qwen3.8:27b" {
		t.Errorf("first_attempt.implementation = %v", impl)
	}
	if got["enabled"] != true {
		t.Errorf("existing key lost in merge: %v", got)
	}
}

func TestAttemptCostTools_SchemaNamesEachKey(t *testing.T) {
	s := &Server{}
	b, err := json.Marshal(s.toolAutonomousConfigSet())
	if err != nil {
		t.Fatal(err)
	}
	var tool struct {
		Description string `json:"description"`
		InputSchema struct {
			Properties map[string]struct {
				Type        string `json:"type"`
				Description string `json:"description"`
			} `json:"properties"`
		} `json:"inputSchema"`
	}
	if err := json.Unmarshal(b, &tool); err != nil {
		t.Fatal(err)
	}
	wantTypes := map[string]string{
		attemptCostFirstAttemptKey: "object",
		attemptCostEscalateKey:     "number",
		attemptCostResumeKey:       "number",
	}
	for key, typ := range wantTypes {
		p, ok := tool.InputSchema.Properties[key]
		if !ok || p.Type != typ {
			t.Errorf("autonomous_config_set schema %s = %+v, want type %s", key, p, typ)
		}
		if !strings.Contains(tool.Description, key) {
			t.Errorf("autonomous_config_set description does not name %s", key)
		}
	}
	// Each argument states its range or accepted kinds.
	if d := tool.InputSchema.Properties[attemptCostEscalateKey].Description; !strings.Contains(d, "0 to 20") {
		t.Errorf("escalate description missing range: %s", d)
	}
	if d := tool.InputSchema.Properties[attemptCostResumeKey].Description; !strings.Contains(d, "0 to 2000000") {
		t.Errorf("resume description missing range: %s", d)
	}
	if d := tool.InputSchema.Properties[attemptCostFirstAttemptKey].Description; !strings.Contains(d, "implementation, docs, note or final_gate") {
		t.Errorf("first_attempt description missing kinds: %s", d)
	}
}

func TestAttemptCostTools_SetWritesValidValues(t *testing.T) {
	d := &attemptCostDaemon{stored: attemptCostStoredBlock}
	s := capacityTestServer(t, d.handler)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"first_attempt": map[string]any{
			"docs": map[string]any{"backend": "pi-prompt", "model": "qwen3.8:27b"},
			"note": map[string]any{},
		},
		"first_attempt_escalate_after_failures": float64(0),
		"retry_resume_max_context_tokens":       float64(0),
	}))
	if err != nil || res.IsError {
		t.Fatalf("set: err=%v res=%+v", err, res)
	}
	if len(d.puts) != 1 {
		t.Fatalf("PUT /api/config calls = %d, want 1", len(d.puts))
	}
	put := d.puts[0]
	if put["autonomous.first_attempt_escalate_after_failures"] != float64(0) {
		t.Errorf("escalate sent = %v, want 0 (zero is a value)", put["autonomous.first_attempt_escalate_after_failures"])
	}
	if put["autonomous.retry_resume_max_context_tokens"] != float64(0) {
		t.Errorf("resume sent = %v, want 0 (zero means a fresh session)", put["autonomous.retry_resume_max_context_tokens"])
	}
	fa, ok := put["autonomous.first_attempt"].(map[string]any)
	if !ok {
		t.Fatalf("first_attempt sent = %v", put["autonomous.first_attempt"])
	}
	if docs, _ := fa["docs"].(map[string]any); docs["backend"] != "pi-prompt" || docs["model"] != "qwen3.8:27b" {
		t.Errorf("first_attempt.docs sent = %v", fa["docs"])
	}
	if _, ok := fa["note"].(map[string]any); !ok {
		t.Errorf("empty note entry not sent: %v", fa)
	}
}

func TestAttemptCostTools_SetOmitsAbsentKeys(t *testing.T) {
	d := &attemptCostDaemon{stored: attemptCostStoredBlock}
	s := capacityTestServer(t, d.handler)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"retry_resume_max_context_tokens": float64(1000),
	})); err != nil {
		t.Fatal(err)
	}
	if len(d.puts) != 1 {
		t.Fatalf("PUT calls = %d, want 1", len(d.puts))
	}
	for _, k := range []string{"autonomous.first_attempt", "autonomous.first_attempt_escalate_after_failures"} {
		if _, ok := d.puts[0][k]; ok {
			t.Errorf("%s sent though the caller never gave it: %v", k, d.puts[0])
		}
	}
	if d.puts[0]["autonomous.retry_resume_max_context_tokens"] != float64(1000) {
		t.Errorf("resume not sent: %v", d.puts[0])
	}
}

// A refused set returns the validator's message, names the key, and makes no
// write (no PUT anywhere).
func TestAttemptCostTools_RefusedSetWritesNothing(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		key  string
	}{
		{"unknown kind", map[string]any{"first_attempt": map[string]any{"review": map[string]any{"backend": "claude-code"}}}, "autonomous.first_attempt"},
		{"model-needing backend without model", map[string]any{"first_attempt": map[string]any{"docs": map[string]any{"backend": "ollama"}}}, "autonomous.first_attempt.docs.model"},
		{"escalate above 20", map[string]any{attemptCostEscalateKey: float64(21)}, "autonomous.first_attempt_escalate_after_failures"},
		{"escalate negative", map[string]any{attemptCostEscalateKey: float64(-1)}, "autonomous.first_attempt_escalate_after_failures"},
		{"resume above 2000000", map[string]any{attemptCostResumeKey: float64(2000001)}, "autonomous.retry_resume_max_context_tokens"},
		{"resume negative", map[string]any{attemptCostResumeKey: float64(-5)}, "autonomous.retry_resume_max_context_tokens"},
		{"escalate fractional", map[string]any{attemptCostEscalateKey: 2.5}, attemptCostEscalateKey},
		{"resume as string", map[string]any{attemptCostResumeKey: "100"}, attemptCostResumeKey},
		{"first_attempt not an object", map[string]any{attemptCostFirstAttemptKey: float64(3)}, attemptCostFirstAttemptKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &attemptCostDaemon{stored: attemptCostStoredBlock}
			s := capacityTestServer(t, d.handler)
			res, err := s.handleAutonomousConfigSet(context.Background(), callReq(tc.args))
			if err != nil || res == nil || !res.IsError {
				t.Fatalf("want an error result, got err=%v res=%+v", err, res)
			}
			msg := resContentText(t, res)
			if !strings.Contains(msg, tc.key) {
				t.Errorf("error does not name %s: %s", tc.key, msg)
			}
			for _, p := range d.puts {
				t.Errorf("a PUT was sent for a refused value: %v", p)
			}
		})
	}
}

// Refusal checks the whole candidate, so a value that is valid alone but not
// with the stored keys is also refused: an entry with an empty model for a
// model-needing backend stored elsewhere is still caught.
func TestAttemptCostTools_RefusalUsesStoredValues(t *testing.T) {
	d := &attemptCostDaemon{stored: `{"first_attempt":{"note":{"backend":"pi"}}}`}
	s := capacityTestServer(t, d.handler)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		attemptCostEscalateKey: float64(1),
	}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("want refusal of stored pi-without-model, got err=%v res=%+v", err, res)
	}
	if !strings.Contains(resContentText(t, res), "autonomous.first_attempt.note.model") {
		t.Errorf("error does not name the stored key: %s", resContentText(t, res))
	}
	if len(d.puts) != 0 {
		t.Errorf("PUT sent for a refused candidate")
	}
}

// Boundaries: 20 and 2000000 and 0 are accepted; the range is the validator's.
func TestAttemptCostTools_BoundariesAccepted(t *testing.T) {
	for _, args := range []map[string]any{
		{attemptCostEscalateKey: float64(20)},
		{attemptCostEscalateKey: float64(0)},
		{attemptCostResumeKey: float64(2000000)},
		{attemptCostResumeKey: float64(0)},
	} {
		d := &attemptCostDaemon{stored: attemptCostStoredBlock}
		s := capacityTestServer(t, d.handler)
		res, err := s.handleAutonomousConfigSet(context.Background(), callReq(args))
		if err != nil || res.IsError {
			t.Errorf("%v: want accepted, got err=%v res=%+v", args, err, res)
		}
		if len(d.puts) != 1 {
			t.Errorf("%v: PUT calls = %d, want 1", args, len(d.puts))
		}
	}
}

// The status tool returns the daemon's status body verbatim, so the attempt
// record fields on each review_needed row must come through. The row is
// built from the exported autonomous type, so the field names are the real
// JSON tags, not a copy.
func TestAttemptCostTools_StatusCarriesAttemptRecordFields(t *testing.T) {
	held := autonomous.HeldTaskStatus{
		AutomatonID:    "a1",
		AutomatonTitle: "Automaton one",
		ReviewNeededEntry: autonomous.ReviewNeededEntry{
			TaskID:            "t1",
			StoryID:           "s1",
			FailureCount:      2,
			LastBackend:       "pi-prompt",
			LastModel:         "qwen3.8:27b",
			LastBackendReason: "first attempt for implementation",
			LastResumed:       true,
			LastResumeReason:  "context under cap",
			NoRetryReason:     "the task writes nothing",
			HeldAt:            time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC),
		},
	}
	body, err := json.Marshal(map[string]any{"review_needed": []autonomous.HeldTaskStatus{held}})
	if err != nil {
		t.Fatal(err)
	}
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autonomous/status" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	})
	res, err := s.handleAutonomousStatus(context.Background(), callReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("status: err=%v res=%+v", err, res)
	}
	var got struct {
		ReviewNeeded []map[string]any `json:"review_needed"`
	}
	if err := json.Unmarshal([]byte(resContentText(t, res)), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.ReviewNeeded) != 1 {
		t.Fatalf("review_needed rows = %d, want 1", len(got.ReviewNeeded))
	}
	row := got.ReviewNeeded[0]
	want := map[string]any{
		"last_backend":        "pi-prompt",
		"last_model":          "qwen3.8:27b",
		"last_backend_reason": "first attempt for implementation",
		"last_resumed":        true,
		"last_resume_reason":  "context under cap",
		"no_retry_reason":     "the task writes nothing",
	}
	for k, v := range want {
		if row[k] != v {
			t.Errorf("review_needed[%s] = %v, want %v", k, row[k], v)
		}
	}
}

// The status description names the attempt-record fields so a caller can find them.
func TestAttemptCostTools_StatusDescriptionNamesAttemptFields(t *testing.T) {
	s := &Server{}
	b, err := json.Marshal(s.toolAutonomousStatus())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"last_backend_reason", "last_resumed", "last_resume_reason", "no_retry_reason"} {
		if !strings.Contains(string(b), f) {
			t.Errorf("autonomous_status description does not name %s", f)
		}
	}
}
