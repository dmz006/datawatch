// Package main (cmd/datawatch) — split-out state path helpers.
//
// state_paths_startup.go holds path construction used by the startup
// migration block in main.go (discussion WAL migration and friends).

package main

import "path/filepath"

// startupDiscussionsDir returns the discussions folder under the expanded
// configured data directory, e.g. <expandHome(cfg.DataDir)>/discussions.
func startupDiscussionsDir(dataDir string) string {
	return filepath.Join(expandHome(dataDir), "discussions")
}
