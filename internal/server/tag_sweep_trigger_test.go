package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// tagSweepTriggerSpy is a fake autonomous manager that records the tag
// sweeps handlePutConfig triggers. It embeds the package's standing fake
// (which satisfies AutonomousAPI) and adds the SweepTags method, so it
// passes the tagSweepAPI assertion the trigger makes.
type tagSweepTriggerSpy struct {
	fakeOrchAutonomous
	mu       sync.Mutex
	sweeps   int
	dryRuns  []bool
	lastCall autonomous.SweepResult
}

func (s *tagSweepTriggerSpy) SweepTags(dryRun bool) autonomous.SweepResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweeps++
	s.dryRuns = append(s.dryRuns, dryRun)
	res := autonomous.SweepResult{
		Sessions: autonomous.SweepCount{Examined: 2, Changed: 1},
		Automata: autonomous.SweepCount{Examined: 1, Changed: 1},
	}
	s.lastCall = res
	return res
}

func (s *tagSweepTriggerSpy) sweepCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sweeps
}

func (s *tagSweepTriggerSpy) sweepDryRuns() []bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]bool(nil), s.dryRuns...)
}

// waitForSweeps polls the spy until it recorded want sweeps or the test
// times out; the trigger runs in the background after the PUT returns.
func (s *tagSweepTriggerSpy) waitForSweeps(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.sweepCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d sweep call(s), got %d", want, s.sweepCount())
}

func putConfigPatch(t *testing.T, s *Server, patch map[string]any) int {
	t.Helper()
	body, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /api/config: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	return rr.Code
}

// TestHandlePutConfig_PlacementChangeTriggersOneSweep is the
// config-change trigger: a PUT that touches any
// autonomous.tags.placement.* key runs one background tag sweep (real run,
// not dry) after the config is saved.
func TestHandlePutConfig_PlacementChangeTriggersOneSweep(t *testing.T) {
	spy := &tagSweepTriggerSpy{}
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: spy,
	}

	putConfigPatch(t, s, map[string]any{
		"autonomous.tags.placement.automaton.enabled": false,
	})

	spy.waitForSweeps(t, 1)
	if drs := spy.sweepDryRuns(); len(drs) != 1 || drs[0] {
		t.Fatalf("the placement-change sweep is a real run: dryRun calls = %v, want one false", drs)
	}
}

// TestHandlePutConfig_NestedPlacementKeyAlsoTriggers: any key under the
// block triggers, including a per-axis nested one (not just the top-level
// enabled/template keys an axis has).
func TestHandlePutConfig_NestedPlacementKeyAlsoTriggers(t *testing.T) {
	spy := &tagSweepTriggerSpy{}
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: spy,
	}

	putConfigPatch(t, s, map[string]any{
		"autonomous.tags.placement.lane.template": "lane:{lane}",
	})

	spy.waitForSweeps(t, 1)
}

// TestHandlePutConfig_UnrelatedKeyTriggersNoSweep: a PUT that changes no
// placement key never sweeps — even another autonomous.tags.* key (the tag
// registry, which the sweep never touches) or an unrelated autonomous
// knob.
func TestHandlePutConfig_UnrelatedKeyTriggersNoSweep(t *testing.T) {
	spy := &tagSweepTriggerSpy{}
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: spy,
	}

	putConfigPatch(t, s, map[string]any{
		"autonomous.tags.direct_counts_visible": true,
		"autonomous.worktree_base_repo":         "/tmp/repo",
		"autonomous.max_concurrent_tasks":       2,
	})

	// Give any (mistaken) background sweep a chance to fire before deciding.
	time.Sleep(100 * time.Millisecond)
	if got := spy.sweepCount(); got != 0 {
		t.Fatalf("a PUT with no placement key ran %d sweep(s), want none", got)
	}
}

// TestHandlePutConfig_PlacementSweepNeedsSweepAPI: the trigger only fires
// when the manager actually implements the sweep (the tagSweepAPI
// assertion), so a manager without it gets a clean 200 and no panic.
func TestHandlePutConfig_PlacementSweepNeedsSweepAPI(t *testing.T) {
	spy := &autonomousConfigPropagationSpy{} // no SweepTags method
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: spy,
	}

	putConfigPatch(t, s, map[string]any{
		"autonomous.tags.placement.backend.enabled": false,
	})
	// No assertion needed beyond the 200 above: without the method the
	// trigger is a no-op by construction.
}
