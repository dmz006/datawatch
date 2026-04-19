// BL12 — historical analytics endpoint.
//
// GET /api/analytics?range=30d returns a time-series of day buckets:
// session_count, completed/failed/killed splits, avg duration. Range
// values: "7d" (default), "14d", "30d", "90d".

package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/stats"
)

// sessionAdapter is the stats.Sessionish view of a session.Session.
type sessionAdapter struct{ s *session.Session }

func (a sessionAdapter) GetCreatedAt() time.Time { return a.s.CreatedAt }
func (a sessionAdapter) GetUpdatedAt() time.Time { return a.s.UpdatedAt }
func (a sessionAdapter) GetState() string        { return string(a.s.State) }

func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.manager == nil {
		http.Error(w, "manager not available", http.StatusServiceUnavailable)
		return
	}

	days := parseRangeDays(r.URL.Query().Get("range"))
	to := time.Now().UTC()
	from := to.Add(-time.Duration(days-1) * 24 * time.Hour)

	all := s.manager.ListSessions()
	wrapped := make([]stats.Sessionish, 0, len(all))
	for _, sess := range all {
		wrapped = append(wrapped, sessionAdapter{s: sess})
	}
	buckets := stats.Aggregate(wrapped, from, to)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"range_days":   days,
		"from":         from.Format("2006-01-02"),
		"to":           to.Format("2006-01-02"),
		"buckets":      buckets,
		"success_rate": stats.SuccessRate(buckets),
	})
}

// parseRangeDays accepts "7d", "30d", etc. Caps at 365. Defaults to 7.
func parseRangeDays(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 7
	}
	if !strings.HasSuffix(raw, "d") {
		return 7
	}
	n, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
	if err != nil || n < 1 {
		return 7
	}
	if n > 365 {
		return 365
	}
	return n
}
