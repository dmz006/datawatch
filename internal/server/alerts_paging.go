// Cheap alert polling for mobile/PWA clients (BL432 / GH#242).
//
// GET /api/alerts keeps its original full-list response unless the caller
// opts in with limit=, unread=1 or before=; GET /api/alerts/count returns
// just the badge numbers; and every successful mark-read broadcasts an
// alerts_read WS message so clients can drop polling entirely.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/federation"
)

// alertsPagingRequested reports whether GET /api/alerts carries any of the
// paging params. Without them the legacy response shape is served unchanged.
func alertsPagingRequested(r *http.Request) bool {
	q := r.URL.Query()
	return q.Has("limit") || q.Has("unread") || q.Has("before")
}

type alertsPageResponse struct {
	Alerts      []*alerts.Alert `json:"alerts"`
	UnreadCount int             `json:"unread_count"`
	Total       int             `json:"total"`
	HasMore     bool            `json:"has_more"`
	NextBefore  string          `json:"next_before,omitempty"`
}

// serveAlertsPage serves the paged form of GET /api/alerts. The caller has
// already enforced CapAlertsList.
func (s *Server) serveAlertsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := alerts.PageOptions{Limit: -1, Source: q.Get("source"), BeforeID: q.Get("before")}
	if q.Has("limit") {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil || n < 0 {
			http.Error(w, "limit must be an integer >= 0", http.StatusBadRequest)
			return
		}
		opts.Limit = n
	}
	if q.Has("unread") {
		v, err := strconv.ParseBool(q.Get("unread"))
		if err != nil {
			http.Error(w, "unread must be 1/0 or true/false", http.StatusBadRequest)
			return
		}
		opts.UnreadOnly = v
	}
	if q.Has("before") && opts.BeforeID == "" {
		http.Error(w, "before must be an alert id", http.StatusBadRequest)
		return
	}
	items, hasMore, err := s.alertStore.Page(opts)
	if errors.Is(err, alerts.ErrUnknownCursor) {
		http.Error(w, "unknown alert id in before", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := alertsPageResponse{
		Alerts:      items,
		UnreadCount: s.alertStore.UnreadCount(),
		Total:       s.alertsTotal(opts.Source),
		HasMore:     hasMore,
	}
	if hasMore && len(items) > 0 {
		resp.NextBefore = items[len(items)-1].ID
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

// alertsTotal counts stored alerts, restricted to source when set.
func (s *Server) alertsTotal(source string) int {
	if source != "" {
		return s.alertStore.CountBySource(source)
	}
	return s.alertStore.Count()
}

// handleAlertsCount serves GET /api/alerts/count → {unread_count, total}.
func (s *Server) handleAlertsCount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.alertStore == nil {
		http.Error(w, "alert store not available", http.StatusServiceUnavailable)
		return
	}
	if !s.fedCap(w, r, federation.CapAlertsList) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{ //nolint:errcheck
		"unread_count": s.alertStore.UnreadCount(),
		"total":        s.alertStore.Count(),
	})
}

// alertsReadPayload is the data of a MsgAlertsRead WS message.
type alertsReadPayload struct {
	IDs         []string `json:"ids,omitempty"`
	All         bool     `json:"all,omitempty"`
	UnreadCount int      `json:"unread_count"`
}

// broadcastAlertsRead tells WS clients which alerts were just marked read.
func (s *Server) broadcastAlertsRead(id string, all bool) {
	if s.hub == nil {
		return
	}
	p := alertsReadPayload{All: all, UnreadCount: s.alertStore.UnreadCount()}
	if !all {
		p.IDs = []string{id}
	}
	go s.hub.Broadcast(MsgAlertsRead, p)
}
