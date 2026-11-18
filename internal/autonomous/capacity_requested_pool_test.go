package autonomous

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

const (
	requestedPoolClaude = "llm:claude-code"
	requestedPoolPi     = "llm:pi-prompt"
)

// requestedPoolKeys maps each backend to its own pool and records every
// backend the capacity request asked about.
type requestedPoolKeys struct {
	mu    sync.Mutex
	asked []string
}

func (k *requestedPoolKeys) keys(backend, model string) ([]string, string) {
	k.mu.Lock()
	k.asked = append(k.asked, backend)
	k.mu.Unlock()
	return []string{"llm:" + backend}, ""
}

func (k *requestedPoolKeys) askedBackends() map[string]bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := map[string]bool{}
	for _, b := range k.asked {
		out[b] = true
	}
	return out
}

// requestedPoolRun runs a one-task Automaton through the executor with the
// daemon default worker set to pi-prompt, and returns the pools the task's
// worker lease held when it spawned and the backends capacity was asked about.
// setup builds the Automaton's stories and settings.
func requestedPoolRun(t *testing.T, prdBackend, taskBackend, lanePref string) (spawnPools []string, asked map[string]bool, spawned SpawnRequest) {
	t.Helper()
	m, api, _, verify := apiFixture(t)
	cfg := m.Config()
	cfg.ExecutionBackend = "pi-prompt"
	m.SetConfig(cfg)

	led := capacity.New(capacity.Options{PollInterval: time.Second, Clock: newFakeCapClock()})
	led.SetLimit(requestedPoolClaude, 1)
	led.SetLimit(requestedPoolPi, 1)
	keys := &requestedPoolKeys{}
	m.SetCapacity(led, keys.keys)

	prd, _ := m.CreatePRD("s", t.TempDir(), prdBackend, "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T0", Spec: "write docs/x.md", Backend: taskBackend}}}})
	got, _ := m.Store().GetPRD(prd.ID)
	got.MaxConcurrentTasks = 1
	got.LanePreference = lanePref
	_ = m.Store().SavePRD(got)

	var mu sync.Mutex
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		if ls := led.LeaseOf(req.TaskID); ls != nil {
			mu.Lock()
			spawnPools = append([]string(nil), ls.Pools...)
			spawned = req
			mu.Unlock()
		}
		return SpawnResult{SessionID: "s-" + req.TaskID}, nil
	}
	final := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if final.Status != PRDCompleted {
		t.Fatalf("Automaton ended %s, want completed", final.Status)
	}
	mu.Lock()
	defer mu.Unlock()
	return spawnPools, keys.askedBackends(), spawned
}

// A task whose own worker, Automaton and lane preference are all Claude
// requests the Claude pool, never the pool of the daemon default.
func TestRequestedPool_ExplicitClaudeRequestsClaudePool(t *testing.T) {
	pools, asked, req := requestedPoolRun(t, "claude-code", "claude-code", LanePreferenceClaude)
	if len(pools) != 1 || pools[0] != requestedPoolClaude {
		t.Fatalf("lease pools = %v, want [%s]", pools, requestedPoolClaude)
	}
	if asked["pi-prompt"] {
		t.Fatalf("capacity was asked about pi-prompt: %v", asked)
	}
	if req.Backend != "claude-code" {
		t.Fatalf("spawned backend = %q, want claude-code", req.Backend)
	}
}

// An Automaton that prefers the Claude lane and names no worker anywhere
// requests the Claude pool, not the pool of the daemon default.
func TestRequestedPool_ClaudeLanePreferenceDecidesPoolOverDefault(t *testing.T) {
	pools, asked, req := requestedPoolRun(t, "", "", LanePreferenceClaude)
	if len(pools) != 1 || pools[0] != requestedPoolClaude {
		t.Fatalf("lease pools = %v, want [%s]", pools, requestedPoolClaude)
	}
	if asked["pi-prompt"] {
		t.Fatalf("capacity was asked about pi-prompt: %v", asked)
	}
	if req.Backend != "claude-code" {
		t.Fatalf("spawned backend = %q, want claude-code", req.Backend)
	}
}

// A task with no Claude setting keeps the pool of the daemon default.
func TestRequestedPool_NoClaudeSettingKeepsExistingPool(t *testing.T) {
	pools, asked, req := requestedPoolRun(t, "", "", "")
	if len(pools) != 1 || pools[0] != requestedPoolPi {
		t.Fatalf("lease pools = %v, want [%s]", pools, requestedPoolPi)
	}
	if asked["claude-code"] {
		t.Fatalf("capacity was asked about claude-code: %v", asked)
	}
	if req.Backend != "pi-prompt" {
		t.Fatalf("spawned backend = %q, want pi-prompt", req.Backend)
	}
}

// The fallback level of the resolution order: only a claude or gpu lane
// preference moves it off the daemon default.
func TestRequestedPool_ExecutionFallbackBackend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ExecutionBackend = "opencode"
	cases := []struct {
		name string
		prd  *PRD
		want string
	}{
		{"nil Automaton", nil, "opencode"},
		{"no preference", &PRD{}, "opencode"},
		{"any", &PRD{LanePreference: LanePreferenceAny}, "opencode"},
		{"claude", &PRD{LanePreference: LanePreferenceClaude}, cfg.LaneFill.EffectiveClaudeBackend()},
		{"gpu", &PRD{LanePreference: LanePreferenceGPU}, cfg.LaneFill.EffectiveGPUBackend()},
	}
	for _, c := range cases {
		if got := executionFallbackBackend(cfg, c.prd); got != c.want {
			t.Errorf("%s: fallback = %q, want %q", c.name, got, c.want)
		}
	}
	// A named worker still beats the lane preference.
	prd := &PRD{LanePreference: LanePreferenceClaude, Backend: "pi-prompt"}
	b, _, src := resolveTaskExecutionLLM(&Task{}, nil, prd, nil, executionFallbackBackend(cfg, prd))
	if b != "pi-prompt" || src != ExecutorSourcePRD {
		t.Fatalf("PRD pin: got %s/%s, want pi-prompt/%s", b, src, ExecutorSourcePRD)
	}
}
