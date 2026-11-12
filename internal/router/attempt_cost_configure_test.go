package router

import (
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// attemptCostTest captures everything the configure verb sends and records
// every configureFn call, so tests can prove which writes were applied and
// which were refused before reaching the daemon. Sent values are only read
// through snapshot() (a copy under the lock) — never assert off a value
// taken while holding the lock, and never call methods that take the same
// lock from inside a t.Fatalf format call.
type attemptCostTest struct {
	sent  []string
	calls [][2]string
	mu    sync.Mutex
	wg    sync.WaitGroup
}

func (at *attemptCostTest) expectSends(n int) { at.wg.Add(n) }

func (at *attemptCostTest) snapshot() (sent []string, calls [][2]string) {
	at.mu.Lock()
	defer at.mu.Unlock()
	sent = append([]string(nil), at.sent...)
	calls = append([][2]string(nil), at.calls...)
	return sent, calls
}

func (at *attemptCostTest) backend() *captureBackend {
	return &captureBackend{name: "test", capture: func(m string) {
		at.mu.Lock()
		at.sent = append(at.sent, m)
		at.wg.Done()
		at.mu.Unlock()
	}}
}

func (at *attemptCostTest) configureFn() func(key, value string) error {
	return func(key, value string) error {
		at.mu.Lock()
		at.calls = append(at.calls, [2]string{key, value})
		at.mu.Unlock()
		return nil
	}
}

// The four new autonomous settings the configure verb must name in help.
var attemptCostHelpKeys = []string{
	"autonomous.first_attempt.<kind>.backend",
	"autonomous.first_attempt.<kind>.model",
	"autonomous.first_attempt_escalate_after_failures",
	"autonomous.retry_resume_max_context_tokens",
}

func TestConfigureAttemptCostHelpNamesKeysAndHowto(t *testing.T) {
	for name, text := range map[string]string{"blank": "", "help": "help", "list": "list"} {
		t.Run(name, func(t *testing.T) {
			at := &attemptCostTest{}
			at.expectSends(1)
			r := &Router{hostname: "h", backend: at.backend()}
			r.SetConfigureFunc(func(key, value string) error {
				t.Errorf("configure %q must not write", text)
				return nil
			})

			r.handleConfigure(Command{Type: CmdConfigure, Text: text})
			at.wg.Wait()

			sent, _ := at.snapshot()
			got := strings.Join(sent, "\n---\n")
			for _, key := range attemptCostHelpKeys {
				if !strings.Contains(got, key) {
					t.Errorf("help (%s) missing key %q", name, key)
				}
			}
			if !strings.Contains(got, "docs/howto/failed-attempt-cost.md") {
				t.Errorf("help (%s) does not name docs/howto/failed-attempt-cost.md", name)
			}
		})
	}
}

func TestConfigureAttemptCostAcceptsValidWrites(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
		state AttemptCostState
	}{
		{
			name:  "first_attempt_backend",
			key:   "autonomous.first_attempt.implementation.backend",
			value: "pi",
			state: AttemptCostState{FirstAttempt: map[string]config.FirstAttemptChoice{
				"implementation": {Model: "local-model"},
			}},
		},
		{
			name:  "first_attempt_model",
			key:   "autonomous.first_attempt.docs.model",
			value: "qwen3",
		},
		{
			name:  "escalate_after_failures_zero",
			key:   "autonomous.first_attempt_escalate_after_failures",
			value: "0",
		},
		{
			name:  "retry_resume_max_context_tokens",
			key:   "autonomous.retry_resume_max_context_tokens",
			value: "120000",
			state: AttemptCostState{FirstAttemptEscalateAfterFailures: 2, FirstAttempt: map[string]config.FirstAttemptChoice{
				"implementation": {Backend: "pi", Model: "local-model"},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := &attemptCostTest{}
			at.expectSends(1)
			r := &Router{hostname: "h", backend: at.backend()}
			r.SetConfigureFunc(at.configureFn())
			r.SetAttemptCostStateFn(func() AttemptCostState { return tc.state })

			r.handleConfigure(Command{Type: CmdConfigure, Text: tc.key + "=" + tc.value})
			at.wg.Wait()

			sent, calls := at.snapshot()
			if len(calls) != 1 || calls[0] != [2]string{tc.key, tc.value} {
				t.Fatalf("configureFn calls = %v; want exactly one of %q = %q", calls, tc.key, tc.value)
			}
			want := "[h] Set " + tc.key + " = " + tc.value + ". Restart may be required."
			if len(sent) != 1 || sent[0] != want {
				t.Fatalf("sent = %q; want %q", sent, want)
			}
		})
	}
}

// Each key must refuse a bad write through the shared validator: the
// refusal carries the validator's own message (as "Refused: ...") and
// configureFn is never called, so nothing is written.
func TestConfigureAttemptCostRefusesWrites(t *testing.T) {
	needsModelMsg := `backend "pi" needs a model; set a model or clear the backend`
	cases := []struct {
		name    string
		key     string
		value   string
		state   AttemptCostState
		wantErr string // substring that must appear after "Refused:"
	}{
		{
			name:  "backend_needs_model",
			key:   "autonomous.first_attempt.implementation.backend",
			value: "pi",
			state: AttemptCostState{FirstAttempt: map[string]config.FirstAttemptChoice{
				"implementation": {Model: ""},
			}},
			wantErr: needsModelMsg,
		},
		{
			name:  "clearing_model_under_model_backend",
			key:   "autonomous.first_attempt.implementation.model",
			value: "",
			state: AttemptCostState{FirstAttempt: map[string]config.FirstAttemptChoice{
				"implementation": {Backend: "pi", Model: "old-model"},
			}},
			wantErr: needsModelMsg,
		},
		{
			name:    "escalate_out_of_range",
			key:     "autonomous.first_attempt_escalate_after_failures",
			value:   "21",
			wantErr: "21 is invalid; use a whole number from 0 to 20",
		},
		{
			name:    "escalate_negative",
			key:     "autonomous.first_attempt_escalate_after_failures",
			value:   "-1",
			wantErr: "-1 is invalid; use a whole number from 0 to 20",
		},
		{
			name:    "resume_out_of_range",
			key:     "autonomous.retry_resume_max_context_tokens",
			value:   "2000001",
			wantErr: "2000001 is invalid; use a whole number from 0 to 2000000",
		},
		{
			name:    "unknown_task_kind",
			key:     "autonomous.first_attempt.bogus.backend",
			value:   "pi",
			wantErr: `unknown task kind "bogus"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := &attemptCostTest{}
			at.expectSends(1)
			r := &Router{hostname: "h", backend: at.backend()}
			r.SetConfigureFunc(at.configureFn())
			r.SetAttemptCostStateFn(func() AttemptCostState { return tc.state })

			r.handleConfigure(Command{Type: CmdConfigure, Text: tc.key + "=" + tc.value})
			at.wg.Wait()

			sent, calls := at.snapshot()
			if len(calls) != 0 {
				t.Fatalf("refused write still reached configureFn: %v", calls)
			}
			if len(sent) != 1 {
				t.Fatalf("sent %d message(s), want exactly 1: %v", len(sent), sent)
			}
			if !strings.Contains(sent[0], "Refused: ") {
				t.Fatalf("sent %q is not a refusal", sent[0])
			}
			if !strings.Contains(sent[0], tc.wantErr) {
				t.Fatalf("sent %q; want the validator's message containing %q", sent[0], tc.wantErr)
			}
			if strings.Contains(sent[0], "Set ") {
				t.Fatalf("sent %q looks like a success confirmation", sent[0])
			}
		})
	}
}

func TestConfigureAttemptCostRejectsMalformedKeysAndValues(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{name: "kindless_backend", key: "autonomous.first_attempt.backend", value: "pi", wantErr: "expected autonomous.first_attempt.<kind>.backend or .model with kind implementation, docs, note or final_gate"},
		{name: "kindless_model", key: "autonomous.first_attempt.model", value: "m", wantErr: "expected autonomous.first_attempt.<kind>.backend or .model with kind implementation, docs, note or final_gate"},
		{name: "esc_non_numeric", key: "autonomous.first_attempt_escalate_after_failures", value: "abc", wantErr: `"abc" is not a whole number`},
		{name: "resume_non_numeric", key: "autonomous.retry_resume_max_context_tokens", value: "lots", wantErr: `"lots" is not a whole number`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := &attemptCostTest{}
			at.expectSends(1)
			r := &Router{hostname: "h", backend: at.backend()}
			r.SetConfigureFunc(at.configureFn())
			r.SetAttemptCostStateFn(func() AttemptCostState { return AttemptCostState{} })

			r.handleConfigure(Command{Type: CmdConfigure, Text: tc.key + "=" + tc.value})
			at.wg.Wait()

			sent, calls := at.snapshot()
			if len(calls) != 0 {
				t.Fatalf("refused write still reached configureFn: %v", calls)
			}
			if len(sent) != 1 {
				t.Fatalf("sent %d message(s), want exactly 1: %v", len(sent), sent)
			}
			if !strings.Contains(sent[0], "Refused: ") || !strings.Contains(sent[0], tc.wantErr) {
				t.Fatalf("sent %q; want a refusal carrying %q", sent[0], tc.wantErr)
			}
		})
	}
}

// Ordinary keys never see the attempt-cost check: the write passes through
// to configureFn untouched.
func TestConfigureAttemptCostOrdinaryKeysBypassTheCheck(t *testing.T) {
	at := &attemptCostTest{}
	at.expectSends(1)
	r := &Router{hostname: "h", backend: at.backend()}
	r.SetConfigureFunc(at.configureFn())

	r.handleConfigure(Command{Type: CmdConfigure, Text: "session.max_sessions=4"})
	at.wg.Wait()

	sent, calls := at.snapshot()
	if len(calls) != 1 || calls[0] != [2]string{"session.max_sessions", "4"} {
		t.Fatalf("ordinary key passthrough broken: calls = %v", calls)
	}
	if len(sent) != 1 || sent[0] != "[h] Set session.max_sessions = 4. Restart may be required." {
		t.Fatalf("sent = %q", sent)
	}
}
