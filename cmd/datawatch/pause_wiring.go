// Operator pause wiring: opens the persisted pause state, hands the read side
// to the autonomous manager and the whole store to the HTTP server.

package main

import (
	"fmt"
	"path/filepath"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/pause"
	"github.com/dmz006/datawatch/internal/server"
)

// pausePersistPath is the pause state file, in the data directory root.
func pausePersistPath(cfg *config.Config) string {
	return filepath.Join(expandHome(cfg.DataDir), "pause.json")
}

// wirePause opens the pause state and wires it. It runs before the autonomous
// boot-resume pass, so the first start after a restart already honours a pause
// the operator set before it. A damaged state file pauses globally and says so
// rather than reading as "not paused".
func wirePause(cfg *config.Config, amgr *autonomouspkg.Manager, httpServer *server.HTTPServer) *pause.Store {
	ps, err := pause.OpenFailClosed(pausePersistPath(cfg), cfg.Pause.StartPaused, nil)
	if err != nil {
		fmt.Printf("[warn] pause state: %v\n", err)
	}
	if snap := ps.Snapshot(); snap.Paused {
		fmt.Printf("[pause] new work is paused (global=%v, projects=%d); the operator resumes it\n", snap.Global != nil, len(snap.Projects))
	}
	if amgr != nil {
		amgr.SetPauseGate(ps)
	}
	if httpServer != nil {
		httpServer.SetPause(ps)
	}
	return ps
}
