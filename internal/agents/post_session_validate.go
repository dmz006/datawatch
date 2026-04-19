// F10 sprint 7 (S7.5) — validator-on-session-end trigger.
//
// When a worker session reaches a terminal state and its Project
// Profile has AutoValidate=true, the parent spawns a small
// read-only validator agent against ValidateProfile (defaults to
// "validator"). The validator reads the worker's diff + memory
// writes + declared task and emits a pass/fail signal. This file
// owns the trigger; the validator's actual check logic lives in
// the validator image (BL103).
//
// Pairs with the existing PostSessionPRHook (S5.4) — both fire on
// session-end via Manager.SetOnSessionEnd; main.go composes them.

package agents

import (
	"context"
	"fmt"
	"time"
)

// ValidatorSpawnerConfig groups the dependencies the trigger needs.
// Spawner is typically Manager.Spawn wrapped to fix the
// ParentAgentID + auto-derived branch.
type ValidatorSpawnerConfig struct {
	Manager *Manager
	Now     func() time.Time
	// SpawnFunc is overridable for testing; production uses
	// Manager.Spawn.
	SpawnFunc func(ctx context.Context, req SpawnRequest) (*Agent, error)
}

// PostSessionValidateHook returns a callback compatible with
// session.Manager.SetOnSessionEnd that spawns the validator agent
// when the worker's profile has AutoValidate=true. Best-effort:
// spawn failures are logged via the supplied log func but never
// block the session-end callback chain.
func PostSessionValidateHook(cfg ValidatorSpawnerConfig, log func(string, ...interface{})) func(SessionLike) {
	if log == nil {
		log = func(string, ...interface{}) {}
	}
	if cfg.SpawnFunc == nil && cfg.Manager != nil {
		cfg.SpawnFunc = cfg.Manager.Spawn
	}
	return func(s SessionLike) {
		agentID := s.GetAgentID()
		if agentID == "" {
			return // host session, not a worker
		}
		if cfg.Manager == nil || cfg.SpawnFunc == nil {
			log("[validate-hook] config incomplete; skipping (sess=%s agent=%s)", s.GetID(), agentID)
			return
		}
		proj := cfg.Manager.GetProjectFor(agentID)
		if proj == nil {
			log("[validate-hook] no project profile for agent=%s", agentID)
			return
		}
		if !proj.AutoValidate {
			return // explicit opt-in
		}
		validateProfile := proj.ValidateProfile
		if validateProfile == "" {
			validateProfile = "validator"
		}

		// Reuse the worker's cluster profile so the validator lands
		// in the same docker host / k8s cluster — minimises network
		// hops + lets the validator inspect the worker's pod when
		// useful (BL103 may add a follow-up to read the worker's
		// /workspace via the agent reverse proxy).
		clusterProfile := getClusterProfileForAgent(cfg.Manager, agentID)
		if clusterProfile == "" {
			log("[validate-hook] no cluster profile for agent=%s", agentID)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Tag the validator's branch with the parent agent + a
		// validate suffix so it doesn't collide with the worker's
		// branch under the workspace lock.
		now := time.Now().UTC()
		if cfg.Now != nil {
			now = cfg.Now()
		}
		branch := fmt.Sprintf("validate-%s-%d", agentID[:8], now.Unix())

		req := SpawnRequest{
			ProjectProfile: validateProfile,
			ClusterProfile: clusterProfile,
			Task:           fmt.Sprintf("validate session %s on agent %s", s.GetID(), agentID),
			ParentAgentID:  agentID,
			Branch:         branch,
		}
		v, err := cfg.SpawnFunc(ctx, req)
		if err != nil {
			log("[validate-hook] spawn validator failed: %v", err)
			return
		}
		log("[validate-hook] validator spawned id=%s for worker=%s", v.ID, agentID)
	}
}

// getClusterProfileForAgent reads the agent's ClusterProfile name
// without exposing internal manager state. Returns "" when unknown.
func getClusterProfileForAgent(m *Manager, agentID string) string {
	a := m.Get(agentID)
	if a == nil {
		return ""
	}
	return a.ClusterProfile
}
