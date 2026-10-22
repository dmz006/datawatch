// BL428 Phase 4 — per-backend usage back-off REST surface.
//
//   GET    /api/usage-backoff   active flags + effective config
//   POST   /api/usage-backoff   set { backend, minutes | until, reason? }
//   DELETE /api/usage-backoff   clear { backend? } — omitted/empty clears all
//
// Same capability gates as /api/cooldown. A manual set works even when
// autonomous.usage_backoff is "off": that mode only disables the AUTOMATIC
// path from rate-limited sessions, never the operator's escape hatch.

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

const (
	// usageBackoffDefaultMinutes is the back-off applied when a manual set
	// names neither minutes nor until.
	usageBackoffDefaultMinutes = 30
	// usageBackoffMaxMinutes bounds a manual back-off at one year.
	usageBackoffMaxMinutes = 525600
)

// SetUsageBackoff wires the per-backend usage back-off registry exposed at
// /api/usage-backoff and in the capacity response.
func (s *Server) SetUsageBackoff(b *autonomous.Backoff) { s.usageBackoff = b }

// SetUsageBackoff wires the usage back-off registry on the HTTP server.
func (s *HTTPServer) SetUsageBackoff(b *autonomous.Backoff) {
	if s.api != nil {
		s.api.SetUsageBackoff(b)
	}
}

// usageBackoffStatus snapshots the live flags with the effective config.
func (s *Server) usageBackoffStatus() autonomous.UsageBackoffStatus {
	var mode string
	var maxWait int
	if s.cfg != nil {
		mode, maxWait = s.cfg.Autonomous.UsageBackoff, s.cfg.Autonomous.BackoffMaxWaitMinutes
	}
	mode, maxWait = config.EffectiveUsageBackoff(mode, maxWait)
	st := autonomous.BuildUsageBackoffStatus(s.usageBackoff, mode, maxWait)
	st.PatternsVersion, st.PatternsStale = s.rateLimitPatternsStatus()
	return st
}

// rateLimitPatternsStatus reports the rate-limit pattern fingerprint and
// whether it is stale; empty and not stale when no session manager is wired.
func (s *Server) rateLimitPatternsStatus() (string, bool) {
	if s.manager == nil {
		return "", false
	}
	return s.manager.RateLimitPatternsStatus()
}

// handleUsageBackoffRecheck serves POST /api/usage-backoff/recheck: the
// operator confirms the current rate-limit patterns still match what the
// backends print, which records their fingerprint and clears patterns_stale.
func (s *Server) handleUsageBackoffRecheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if s.manager == nil {
		http.Error(w, "session manager not available", http.StatusServiceUnavailable)
		return
	}
	version, err := s.manager.RecheckRateLimitPatterns()
	if err != nil {
		http.Error(w, "recheck failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r.Context(), audit.ActionUsageBackoffPatternsRechecked, "usage_backoff", "patterns", map[string]any{
		"patterns_version": version,
	})
	writeJSONOK(w, s.usageBackoffStatus())
}

// usageBackoffSetRequest is the POST body.
type usageBackoffSetRequest struct {
	Backend string `json:"backend"`
	Minutes *int   `json:"minutes,omitempty"`
	Until   string `json:"until,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// resolve validates the request and returns the backend and deadline.
func (r usageBackoffSetRequest) resolve(now time.Time) (string, time.Time, error) {
	backend := strings.TrimSpace(r.Backend)
	if backend == "" {
		return "", time.Time{}, errors.New("backend: required")
	}
	if r.Minutes != nil && strings.TrimSpace(r.Until) != "" {
		return "", time.Time{}, errors.New("minutes and until: specify exactly one")
	}
	if r.Minutes != nil {
		if *r.Minutes < 1 {
			return "", time.Time{}, errors.New("minutes: must be >= 1")
		}
		if *r.Minutes > usageBackoffMaxMinutes {
			return "", time.Time{}, errors.New("minutes: must be <= 525600")
		}
		return backend, now.Add(time.Duration(*r.Minutes) * time.Minute), nil
	}
	if u := strings.TrimSpace(r.Until); u != "" {
		until, err := time.Parse(time.RFC3339, u)
		if err != nil {
			return "", time.Time{}, errors.New("until: must be an RFC3339 time")
		}
		if !until.After(now) {
			return "", time.Time{}, errors.New("until: must be in the future")
		}
		return backend, until, nil
	}
	return backend, now.Add(usageBackoffDefaultMinutes * time.Minute), nil
}

func (s *Server) handleUsageBackoff(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
		if s.usageBackoff == nil {
			http.Error(w, "usage back-off not available", http.StatusServiceUnavailable)
			return
		}
		writeJSONOK(w, s.usageBackoffStatus())
	case http.MethodPost:
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		if s.usageBackoff == nil {
			http.Error(w, "usage back-off not available", http.StatusServiceUnavailable)
			return
		}
		var req usageBackoffSetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		backend, until, err := req.resolve(time.Now())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reason := strings.TrimSpace(req.Reason)
		if reason == "" {
			reason = "operator back-off until " + until.Format("15:04 MST")
		}
		s.usageBackoff.Set(backend, until, reason, autonomous.BackoffSourceOperator)
		s.audit(r.Context(), audit.ActionUsageBackoffSet, "usage_backoff", backend, map[string]any{
			"backend": backend,
			"until":   until.UTC().Format(time.RFC3339),
			"source":  autonomous.BackoffSourceOperator,
		})
		writeJSONOK(w, s.usageBackoffStatus())
	case http.MethodDelete:
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		if s.usageBackoff == nil {
			http.Error(w, "usage back-off not available", http.StatusServiceUnavailable)
			return
		}
		var req struct {
			Backend string `json:"backend"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		backend := strings.TrimSpace(req.Backend)
		target := backend
		cleared := 0
		if backend == "" || strings.EqualFold(backend, "all") {
			target = "all"
			cleared = s.usageBackoff.ClearAll()
		} else if s.usageBackoff.Clear(backend) {
			cleared = 1
		}
		s.audit(r.Context(), audit.ActionUsageBackoffCleared, "usage_backoff", target, map[string]any{
			"backend": target,
		})
		st := s.usageBackoffStatus()
		writeJSONOK(w, struct {
			Cleared int `json:"cleared"`
			autonomous.UsageBackoffStatus
		}{cleared, st})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// validateUsageBackoffMode checks a patch value for autonomous.usage_backoff.
func validateUsageBackoffMode(v interface{}) error {
	str, ok := v.(string)
	if !ok {
		return fmt.Errorf("usage_backoff: must be a string (%q or %q)", config.UsageBackoffPauseBackend, config.UsageBackoffOff)
	}
	return config.ValidateUsageBackoff(str)
}

// parseBackoffMaxWaitMinutes checks a patch value for
// autonomous.backoff_max_wait_minutes: a whole number >= 1.
func parseBackoffMaxWaitMinutes(v interface{}) (int, error) {
	f, ok := v.(float64)
	if i, isInt := v.(int); isInt {
		f, ok = float64(i), true
	}
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || f < 1 || f > usageBackoffMaxMinutes {
		return 0, fmt.Errorf("backoff_max_wait_minutes: must be a whole number of minutes in 1..%d", usageBackoffMaxMinutes)
	}
	return int(f), nil
}

// usageBackoffModeForDisplay renders the effective mode for the config
// surface so an unset value shows its default instead of a blank.
func usageBackoffModeForDisplay(raw string) string {
	mode, _ := config.EffectiveUsageBackoff(raw, 0)
	return mode
}

// usageBackoffMaxWaitForDisplay renders the effective maximum back-off.
func usageBackoffMaxWaitForDisplay(raw int) int {
	_, maxWait := config.EffectiveUsageBackoff("", raw)
	return maxWait
}
