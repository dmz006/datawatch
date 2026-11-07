// Launch tagging for operator-started sessions.
//
// POST /api/sessions/start (api.go handleStartSession) accepts four
// optional fields: launch_role, launch_kind, launch_automaton and
// launch_stage (see docs/plans/2026-11-06-automata-pages-redesign.md,
// section 12). api.go decodes them, calls validateSessionLaunch before
// starting the session (unknown role/kind value → 400), and calls
// applySessionLaunchTags once manager.Start has returned — everything
// else lives in this file:
//
//   - validateSessionLaunch rejects role/kind values outside the known
//     vocabulary before any session is started.
//   - applySessionLaunchTags builds an autonomous.Placement from the
//     request facts plus the session's own facts (backend, model, node,
//     project) and hands it to autonomous.ApplyPlacementTags with the
//     live placement-tag config (autonomous.tags.placement). The tags
//     land in the session's system tags; nothing else is touched.
//
// A request without any launch field takes the fast path in
// applySessionLaunchTags and is byte-for-byte the same as before this
// feature existed.

package server

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// launchRoles is the known launch-role vocabulary (docs/plans/
// 2026-11-06-automata-pages-redesign.md section 12).
var launchRoles = map[string]bool{
	"worker":     true,
	"verifier":   true,
	"planner":    true,
	"judge":      true,
	"supervisor": true,
}

// launchKinds is the known launch-kind vocabulary (same section).
var launchKinds = map[string]bool{
	"task":      true,
	"decompose": true,
	"review":    true,
	"rejudge":   true,
	"auto-fix":  true,
}

// validateSessionLaunch checks launch_role / launch_kind against the known
// vocabulary. Empty values are always accepted (the field is optional); an
// unknown non-empty value is an operator mistake in the request and must be
// refused with 400 before any session is started.
func validateSessionLaunch(role, kind string) error {
	role = strings.TrimSpace(role)
	if role != "" && !launchRoles[role] {
		return fmt.Errorf("launch_role %q is not a known role (worker|verifier|planner|judge|supervisor)", role)
	}
	kind = strings.TrimSpace(kind)
	if kind != "" && !launchKinds[kind] {
		return fmt.Errorf("launch_kind %q is not a known kind (task|decompose|review|rejudge|auto-fix)", kind)
	}
	return nil
}

// launchFacts carries the four optional launch fields of a start-request
// into applySessionLaunchTags.
type launchFacts struct {
	Role      string
	Kind      string
	Automaton string
	Stage     string
}

// applySessionLaunchTags writes the launch placement tags onto a freshly
// started session. Request facts (role/kind/automaton/stage) plus the
// session's own facts (backend, model, node, project) are folded into one
// autonomous.Placement and handed to autonomous.ApplyPlacementTags with
// the live placement-tag config, so the master switch and per-axis
// on/off + template overrides apply exactly as on a worker launch.
//
// A request without any launch field (all four facts empty) is a no-op and
// never touches the tag machinery. Applying tags is best-effort after the
// session already exists: a failure is logged, never returned — the session
// stays usable without its tags.
func (s *Server) applySessionLaunchTags(sess *session.Session, f launchFacts) {
	role := strings.TrimSpace(f.Role)
	kind := strings.TrimSpace(f.Kind)
	automaton := strings.TrimSpace(f.Automaton)
	stage := strings.TrimSpace(f.Stage)
	if sess == nil || s.manager == nil {
		return
	}
	if role == "" && kind == "" && automaton == "" && stage == "" {
		return // launch fields absent — behave exactly as before.
	}

	p := autonomous.Placement{
		Role:      role,
		Kind:      kind,
		Automaton: automaton,
		Automation: s.launchAutomationID(automaton),
		Stage:     stage,
		Project:   filepath.Base(sess.ProjectDir),
		Backend:   sess.BackendFamily,
		Model:     sess.Model,
		Lane:      launchLaneForBackend(sess.BackendFamily),
		Node:      sess.ComputeNodeRef,
	}

	// The live placement-tag config: whatever autonomous.tags.placement
	// says right now. Unset (nil cfg or zero block) falls back to the
	// defaults — master on, every axis on with its default template —
	// which is also what a worker launch gets.
	var pc config.PlacementConfig
	if s.cfg != nil {
		pc = s.cfg.Autonomous.Tags.Placement
	}
	pc = pc.EffectivePlacement()

	if err := autonomous.ApplyPlacementTags(s.manager, nil, "", sess.FullID, p, pc); err != nil {
		log.Printf("launch tags: session %s: %v", sess.FullID, err)
	}
}

// launchAutomationID resolves the Automation a launch_automaton PRD belongs
// to (empty when the PRD is unknown or the autonomous manager is not
// wired). Best-effort: an unresolvable automaton just leaves the automation
// axis unset rather than refusing the launch.
func (s *Server) launchAutomationID(prdID string) string {
	if prdID == "" || s.autonomousMgr == nil {
		return ""
	}
	got, ok := s.autonomousMgr.GetPRD(prdID)
	if !ok {
		return ""
	}
	prd, ok := got.(*autonomous.PRD)
	if !ok || prd == nil {
		return ""
	}
	return prd.AutomationID
}

// launchLaneForBackend names the lane a backend's session runs in, mirroring
// autonomous.laneForBackend: Claude backends ride the claude lane, every
// other pool backend rides the gpu lane, and anything else (no recognized
// backend) names no lane so the lane axis simply stays unset.
func launchLaneForBackend(backend string) string {
	switch strings.TrimSpace(backend) {
	case "claude", "claude-code":
		return "claude"
	case "pi", "pi-prompt", "ollama", "vllm":
		return "gpu"
	default:
		return ""
	}
}
