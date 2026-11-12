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
//   - applySessionLaunchTags records the request's role/kind/stage on the
//     session record (SetLaunchFacts; the Automaton is already stored as the
//     session's PRDID by the start), then builds the session's placement
//     with autonomous.SessionPlacement and hands it to
//     autonomous.ApplyPlacementTags with the live placement-tag config
//     (autonomous.tags.placement). The tags land in the session's system
//     tags; nothing else is touched.
//
// Every started session gets its placement tags, including a plain start
// with no launch fields: it carries compute (the local node when it stored
// none), backend, lane and project tags, and no role, kind, automaton,
// automation or stage tag, because it records none of those facts.

package server

import (
	"fmt"
	"log"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// launchLocalNode is the node name a session that ran locally and stored no
// compute node is placed on (its tag reads compute:local). It is the same
// name the tag sweep uses for launch-fact sessions.
const launchLocalNode = "local"

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

// applySessionLaunchTags records the request's launch facts on a freshly
// started session and writes its placement tags. The role, kind and stage
// are stored with SetLaunchFacts; the session's own record supplies the
// rest (backend, node, project, and the Automaton as PRDID). The placement
// comes from autonomous.SessionPlacement and is applied with the live
// placement-tag config, so the master switch and per-axis on/off + template
// overrides apply exactly as on a worker launch.
//
// This runs for every started session, including a plain start with no
// launch fields. Applying facts and tags is best-effort after the session
// already exists: a failure is logged, never returned — the session stays
// usable without its tags.
func (s *Server) applySessionLaunchTags(sess *session.Session, f launchFacts) {
	if sess == nil || s.manager == nil {
		return
	}
	role := strings.TrimSpace(f.Role)
	kind := strings.TrimSpace(f.Kind)
	stage := strings.TrimSpace(f.Stage)
	automaton := strings.TrimSpace(f.Automaton)

	if err := s.manager.SetLaunchFacts(sess.FullID, role, kind, stage); err != nil {
		log.Printf("launch facts: session %s: %v", sess.FullID, err)
	}

	// Re-read the record so the placement sees the facts just stored.
	cur := sess
	if got, ok := s.manager.GetSession(sess.FullID); ok && got != nil {
		cur = got
	}
	p := autonomous.SessionPlacement(cur, launchLocalNode, s.launchAutomationID)
	// The start does not store launch_automaton as the record's PRDID, so
	// the request's Automaton is carried onto the placement here.
	if p.Automaton == "" && automaton != "" {
		p.Automaton = automaton
		p.Automation = s.launchAutomationID(automaton)
	}
	// autonomous.laneForBackend knows the "claude" backend but not
	// "claude-code", the family a Claude session records; fill the lane
	// for that case so every Claude session gets its lane tag.
	if p.Lane == "" {
		p.Lane = launchLaneForBackend(cur.BackendFamily)
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

	if err := autonomous.ApplyPlacementTags(s.manager, nil, "", cur.FullID, p, pc); err != nil {
		log.Printf("launch tags: session %s: %v", cur.FullID, err)
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
// autonomous.laneForBackend and adding the "claude-code" family it does not
// list. Other pool backends ride the gpu lane; anything unrecognized names no
// lane, so the lane axis stays unset.
func launchLaneForBackend(backend string) string {
	switch strings.TrimSpace(backend) {
	case "claude", "claude-code":
		return string(autonomous.LaneTypeClaude)
	case "pi", "pi-prompt", "ollama", "vllm":
		return string(autonomous.LaneTypeGPU)
	default:
		return ""
	}
}
