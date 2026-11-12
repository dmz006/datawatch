// Placement start wiring: stamps the placement (system) tags on every session
// the daemon starts, from the session manager's start hook.
//
// session.Manager.SetPlacementStartHook runs once per successful Start —
// scheduled starts, comm-channel starts and every other path that goes
// through Manager.Start. The function built here is that hook. It derives the
// session's placement with autonomous.SessionPlacement and writes the tags
// with autonomous.ApplyPlacementTags under the live tags.placement block, the
// same way the REST start path does in internal/server/session_launch_tags.go
// (applySessionLaunchTags). The REST path re-stamps after the start with the
// launch facts the request carried; this hook covers the starts that have no
// such request.

package main

import (
	"log"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// placementStartLocalNode is the node name a session that ran locally and
// stored no compute node is placed on (its tag reads compute:local). It is the
// same name the REST start path (launchLocalNode) and the tag sweep
// (sweepLocalNode) use.
const placementStartLocalNode = "local"

// placementStartFunc returns the function to install with
// session.Manager.SetPlacementStartHook. For each started session it:
//
//   - builds the Placement with autonomous.SessionPlacement, the local node
//     named placementStartLocalNode and automationOf for the Automation axis;
//   - fills the lane from the backend family when the record names none, for
//     a Claude Code family that autonomous.laneForBackend does not list (the
//     same fallback the REST start path applies);
//   - applies the tags with autonomous.ApplyPlacementTags, under placement()
//     with EffectivePlacement applied. A switch that is off clears the
//     session's system tags.
//
// sink is the session manager in production. placement is read on every
// session start, so a config edit takes effect without a restart. A nil
// placement reads as the zero block, which EffectivePlacement fills with the
// defaults. A tag write failure is logged, never returned: the session has
// already started.
func placementStartFunc(sink autonomous.SystemTagsSink, placement func() config.PlacementConfig, automationOf func(prdID string) string) func(*session.Session) {
	return func(s *session.Session) {
		if s == nil || sink == nil {
			return
		}
		p := autonomous.SessionPlacement(s, placementStartLocalNode, automationOf)
		if p.Lane == "" {
			p.Lane = placementStartLane(s.BackendFamily)
		}
		var pc config.PlacementConfig
		if placement != nil {
			pc = placement()
		}
		pc = pc.EffectivePlacement()
		if err := autonomous.ApplyPlacementTags(sink, nil, "", s.FullID, p, pc); err != nil {
			log.Printf("placement start tags: session %s: %v", s.FullID, err)
		}
	}
}

// placementAutomationOf returns the Automation lookup the Automation axis
// takes: the Automation a PRD (Automaton) belongs to, read from the stored
// PRD's AutomationID. It is the source the REST start path uses
// (launchAutomationID). The autonomous manager is looked up on every call, so
// a manager attached after startup is seen. A nil manager, an unknown PRD or a
// PRD in no Automation gives "".
func placementAutomationOf(amgr func() *autonomous.Manager) func(prdID string) string {
	return func(prdID string) string {
		if prdID == "" || amgr == nil {
			return ""
		}
		am := amgr()
		if am == nil || am.Store() == nil {
			return ""
		}
		prd, ok := am.Store().GetPRD(prdID)
		if !ok || prd == nil {
			return ""
		}
		return prd.AutomationID
	}
}

// placementStartLane names the lane a backend family serves, mirroring
// autonomous.laneForBackend and adding the "claude-code" family it does not
// list. Other pool backends ride the gpu lane; anything unrecognized names no
// lane.
func placementStartLane(backend string) string {
	switch backend {
	case "claude", "claude-code":
		return string(autonomous.LaneTypeClaude)
	case "pi", "pi-prompt", "ollama", "vllm":
		return string(autonomous.LaneTypeGPU)
	default:
		return ""
	}
}
