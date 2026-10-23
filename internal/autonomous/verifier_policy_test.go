package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// fakeVerifierLoad is a fake capacity ledger: a backend admits while its
// in-use count is under its capacity.
type fakeVerifierLoad struct {
	capacity map[string]int
	inUse    map[string]int
}

func (f fakeVerifierLoad) Free(b string) int {
	c, ok := f.capacity[b]
	if !ok {
		return -1
	}
	if free := c - f.inUse[b]; free > 0 {
		return free
	}
	return 0
}

func (f fakeVerifierLoad) TryAdmit(b string) bool { return f.Free(b) != 0 }

func (f fakeVerifierLoad) RelativeLoad(b string) float64 {
	c := f.capacity[b]
	if c <= 0 {
		return 0
	}
	return float64(f.inUse[b]) / float64(c)
}

func TestVerifierPolicySelect(t *testing.T) {
	const strongModel = "claude-sonnet-5-5"
	list := []string{"ollama-a", "ollama-b", "claude-code"}
	full := func(a, b int) fakeVerifierLoad {
		return fakeVerifierLoad{
			capacity: map[string]int{"ollama-a": 2, "ollama-b": 2, "claude-code": 3},
			inUse:    map[string]int{"ollama-a": a, "ollama-b": b},
		}
	}
	tests := []struct {
		name       string
		policy     config.VerifierPolicyConfig
		candidates []string
		load       VerifierLoad
		flags      VerifierSelectFlags
		wantBack   string
		wantModel  string
	}{
		{
			name:     "prefer-free falls back when the first backend is full",
			policy:   config.VerifierPolicyConfig{Mode: "prefer-free", Backends: list},
			load:     full(2, 0),
			wantBack: "ollama-b",
		},
		{
			name:     "prefer-free uses the first when it has room",
			policy:   config.VerifierPolicyConfig{Mode: "prefer-free", Backends: list},
			load:     full(1, 0),
			wantBack: "ollama-a",
		},
		{
			name:     "prefer-free waits on the first when every backend is full",
			policy:   config.VerifierPolicyConfig{Mode: "prefer-free", Backends: list[:2]},
			load:     full(2, 2),
			wantBack: "ollama-a",
		},
		{
			name:     "balance picks the lighter backend",
			policy:   config.VerifierPolicyConfig{Mode: "balance", Backends: list},
			load:     full(1, 0),
			wantBack: "ollama-b",
		},
		{
			name:     "balance ties go to list order",
			policy:   config.VerifierPolicyConfig{Mode: "balance", Backends: list},
			load:     full(1, 1),
			wantBack: "claude-code",
		},
		{
			name:     "balance ties between equal backends go to list order",
			policy:   config.VerifierPolicyConfig{Mode: "balance", Backends: list[:2]},
			load:     full(1, 1),
			wantBack: "ollama-a",
		},
		{
			name:     "gpu-only never picks Claude",
			policy:   config.VerifierPolicyConfig{Mode: "gpu-only", Backends: []string{"claude-code", "ollama-a"}},
			load:     full(2, 0),
			wantBack: "ollama-a",
		},
		{
			name:     "gpu-only with only Claude backends has nothing eligible",
			policy:   config.VerifierPolicyConfig{Mode: "gpu-only", Backends: []string{"claude-code"}},
			wantBack: "",
		},
		{
			name:     "claude-only never picks a local backend",
			policy:   config.VerifierPolicyConfig{Mode: "claude-only", Backends: list},
			wantBack: "claude-code",
		},
		{
			name:     "a hold removes Claude",
			policy:   config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"claude-code", "claude-haiku", "ollama-a"}},
			flags:    VerifierSelectFlags{ClaudeHold: map[string]bool{"claude-code": true}},
			wantBack: "ollama-a",
		},
		{
			name:     "a hold with only Claude backends has nothing eligible",
			policy:   config.VerifierPolicyConfig{Mode: "claude-only", Backends: list},
			flags:    VerifierSelectFlags{ClaudeHold: map[string]bool{"claude-code": true}},
			wantBack: "",
		},
		{
			name:     "window cap removes Claude",
			policy:   config.VerifierPolicyConfig{Mode: "prefer-free", Backends: []string{"claude-code", "ollama-a"}, ClaudeSessionCap: 2},
			flags:    VerifierSelectFlags{ClaudeSessionsInWindow: 2},
			wantBack: "ollama-a",
		},
		{
			name:     "under the window cap Claude stays eligible",
			policy:   config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"claude-code", "ollama-a"}, ClaudeSessionCap: 2},
			flags:    VerifierSelectFlags{ClaudeSessionsInWindow: 1},
			wantBack: "claude-code",
		},
		{
			name:     "a zero cap is unlimited",
			policy:   config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"claude-code"}},
			flags:    VerifierSelectFlags{ClaudeSessionsInWindow: 99},
			wantBack: "claude-code",
		},
		{
			name:      "a retry forces the stronger verifier",
			policy:    config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"ollama-a"}, ForceStrongerOnRetry: true},
			flags:     VerifierSelectFlags{Retry: true},
			wantBack:  "claude-code",
			wantModel: strongModel,
		},
		{
			name:     "a retry without the setting keeps normal selection",
			policy:   config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"ollama-a"}},
			flags:    VerifierSelectFlags{Retry: true},
			wantBack: "ollama-a",
		},
		{
			name:      "the final gate forces the stronger verifier",
			policy:    config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"ollama-a"}, FinalGateStronger: true},
			flags:     VerifierSelectFlags{FinalGate: true},
			wantBack:  "claude-code",
			wantModel: strongModel,
		},
		{
			name: "a configured stronger backend and model are honoured",
			policy: config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"ollama-a"}, ForceStrongerOnRetry: true,
				StrongerBackend: "claude-alt", StrongerModel: "claude-opus-5-5"},
			flags:     VerifierSelectFlags{Retry: true},
			wantBack:  "claude-alt",
			wantModel: "claude-opus-5-5",
		},
		{
			name:     "a held stronger verifier falls back to normal selection",
			policy:   config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"ollama-a"}, ForceStrongerOnRetry: true},
			flags:    VerifierSelectFlags{Retry: true, ClaudeHold: map[string]bool{"claude-code": true}},
			wantBack: "ollama-a",
		},
		{
			name:     "a capped stronger verifier falls back to normal selection",
			policy:   config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"ollama-a"}, ForceStrongerOnRetry: true, ClaudeSessionCap: 1},
			flags:    VerifierSelectFlags{Retry: true, ClaudeSessionsInWindow: 1},
			wantBack: "ollama-a",
		},
		{
			name:     "gpu-only is not overridden by the stronger verifier",
			policy:   config.VerifierPolicyConfig{Mode: "gpu-only", Backends: []string{"ollama-a"}, ForceStrongerOnRetry: true},
			flags:    VerifierSelectFlags{Retry: true},
			wantBack: "ollama-a",
		},
		{
			name:     "fixed uses only the first entry",
			policy:   config.VerifierPolicyConfig{Mode: "fixed", Backends: list},
			load:     full(2, 0),
			wantBack: "ollama-a",
		},
		{
			name:       "empty mode is fixed over the candidate pool",
			policy:     config.VerifierPolicyConfig{},
			candidates: []string{"ollama-b", "ollama-a"},
			wantBack:   "ollama-b",
		},
		{
			name:       "the policy list wins over candidates",
			policy:     config.VerifierPolicyConfig{Backends: []string{"ollama-a"}},
			candidates: []string{"ollama-b"},
			wantBack:   "ollama-a",
		},
		{
			name:     "nothing configured selects nothing",
			policy:   config.VerifierPolicyConfig{},
			wantBack: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, model, reason := SelectVerifierBackend(tc.policy, tc.candidates, tc.load, tc.flags)
			if got != tc.wantBack || model != tc.wantModel {
				t.Fatalf("got (%q, %q), want (%q, %q); reason %q", got, model, tc.wantBack, tc.wantModel, reason)
			}
			if reason == "" {
				t.Fatalf("empty reason for (%q, %q)", got, model)
			}
		})
	}
}

func TestVerifierPolicySelectNilLoad(t *testing.T) {
	got, _, _ := SelectVerifierBackend(config.VerifierPolicyConfig{Mode: "balance", Backends: []string{"a", "b"}}, nil, nil, VerifierSelectFlags{})
	if got != "a" {
		t.Fatalf("got %q, want a", got)
	}
}

func TestVerifierPolicyOverridePrecedence(t *testing.T) {
	global := config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"g1", "g2"}, ClaudeSessionCap: 4}
	prd := &PRD{VerifierPolicy: &VerifierPolicyOverride{Mode: "balance", Backends: []string{"p1"}}}
	task := &Task{VerifierPolicy: &VerifierPolicyOverride{Mode: "gpu-only", Backends: []string{"t1", "t2"}}}

	tests := []struct {
		name     string
		prd      *PRD
		task     *Task
		wantMode string
		wantBack []string
	}{
		{"global only", nil, nil, "fixed", []string{"g1", "g2"}},
		{"nil overrides inherit", &PRD{}, &Task{}, "fixed", []string{"g1", "g2"}},
		{"PRD over global", prd, &Task{}, "balance", []string{"p1"}},
		{"task over PRD over global", prd, task, "gpu-only", []string{"t1", "t2"}},
		{"task alone over global", nil, task, "gpu-only", []string{"t1", "t2"}},
		{
			"fields resolve independently",
			prd, &Task{VerifierPolicy: &VerifierPolicyOverride{Mode: "prefer-free"}},
			"prefer-free", []string{"p1"},
		},
		{
			"empty override fields inherit",
			prd, &Task{VerifierPolicy: &VerifierPolicyOverride{Backends: []string{"t9"}}},
			"balance", []string{"t9"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveVerifierPolicy(tc.prd, tc.task, global)
			if got.Mode != tc.wantMode {
				t.Errorf("mode = %q, want %q", got.Mode, tc.wantMode)
			}
			if len(got.Backends) != len(tc.wantBack) {
				t.Fatalf("backends = %v, want %v", got.Backends, tc.wantBack)
			}
			for i := range tc.wantBack {
				if got.Backends[i] != tc.wantBack[i] {
					t.Fatalf("backends = %v, want %v", got.Backends, tc.wantBack)
				}
			}
			if got.ClaudeSessionCap != 4 {
				t.Errorf("non-overridable field changed: cap = %d", got.ClaudeSessionCap)
			}
		})
	}
	// The resolver must not alias the override's slice into the result.
	got := ResolveVerifierPolicy(prd, nil, global)
	got.Backends[0] = "changed"
	if prd.VerifierPolicy.Backends[0] != "p1" {
		t.Fatal("resolved policy aliases the PRD override")
	}
}
