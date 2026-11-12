// Cleanup REST surface:
//
//   GET /api/autonomous/cleanup — the full automation cleanup status:
//   both passes (the finished-worktrees pass and the daemon temp-file
//   sweep), their last run and owner-vs-net split, the two disk
//   readings, and the rate-limited disk-alert flag.
//
// It also carries the strict write checks for the four automation
// cleanup knobs that PUT /api/autonomous/config applies to
// automation.cleanup_mode, automation.cleanup_retention_hours,
// automation.cleanup_max_removals_per_pass and automation.
// disk_alert_percent: an absent key keeps the current value, but a
// present one must be a value the Effective* accessors can read back
// verbatim (off|report|on; whole numbers of at least 1; a whole percent
// in 1..100).

package server

import (
	"fmt"
	"math"
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// cleanupStatusReader is the narrow Manager surface the cleanup status
// route needs; asserted like the merge routes so minimal test doubles
// keep compiling.
type cleanupStatusReader interface {
	CleanupStatus() autonomous.CleanupStatus
}

// handleCleanupStatus serves GET /api/autonomous/cleanup: the status of
// both cleanup passes (Mode, LastRun, the Removed/WouldRemove/Skipped/
// Failed counts, BytesFreed, LastError, and the owner-vs-net split for
// each), the two disk readings behind the cleanup, and
// the DiskAlertPercent threshold with the DiskAlert flag and the last
// time the alert actually fired. Read-only, same gate as the merge read
// routes: only GET is served, the Automata must be enabled, a
// federated peer needs autonomous:read, and the Manager must expose the
// status. Any other method is a 405.
func (s *Server) handleCleanupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	cr, ok := s.autonomousMgr.(cleanupStatusReader)
	if !ok {
		http.Error(w, "Automaton cleanup status unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSONOK(w, cr.CleanupStatus())
}

// validateCleanupConfigMode checks automation.cleanup_mode: it must be
// one of off, report or on. Empty and unknown words are rejected here —
// unlike the merge-pending mode, the read side has no "unknown reads as
// default" tolerance for this one.
func validateCleanupConfigMode(v string) error {
	switch v {
	case config.CleanupModeOff, config.CleanupModeReport, config.CleanupModeOn:
		return nil
	}
	return fmt.Errorf("autonomous.automation.cleanup_mode: must be %q, %q or %q",
		config.CleanupModeOff, config.CleanupModeReport, config.CleanupModeOn)
}

// cleanupWholeAtLeast checks a *float64 automation knob: nil (absent)
// passes; a present value must be a whole number of at least 1.
func cleanupWholeAtLeast(key string, v *float64) error {
	if v == nil {
		return nil
	}
	f := *v
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 1 || f != math.Trunc(f) {
		return fmt.Errorf("autonomous.automation.%s: must be a whole number of at least 1", key)
	}
	return nil
}

// cleanupPercentWhole checks automation.disk_alert_percent: nil (absent)
// passes; a present value must be a whole number between 1 and 100.
func cleanupPercentWhole(v *float64) error {
	if v == nil {
		return nil
	}
	f := *v
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 1 || f > 100 || f != math.Trunc(f) {
		return fmt.Errorf("autonomous.automation.disk_alert_percent: must be a whole number between 1 and 100")
	}
	return nil
}
