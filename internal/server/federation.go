// Server handler for GET /api/federation/sessions — closes #3.
//
// All-servers view: aggregates the local session list plus a
// parallel fan-out to every enabled remote server in cfg.Servers.
// Mobile clients (and any future aggregator) can hit one endpoint
// instead of per-profile looping — cuts request count + battery.

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// FederationResponse mirrors the shape promised in issue #3.
type FederationResponse struct {
	Primary  []*session.Session             `json:"primary"`
	Proxied  map[string][]*session.Session  `json:"proxied"`
	Errors   map[string]string              `json:"errors,omitempty"`
}

// handleFederationSessions implements the endpoint. Query params:
//
//   since=<unix_ms>           only return sessions with activity
//                             newer than the supplied timestamp
//   include=proxied           include proxied children (default true)
//   states=<csv>              filter by state set ("running,waiting")
//
// Proxied calls run in parallel with a 10s per-server timeout; per-
// server failures surface in `errors` without aborting the overall
// request — the caller still gets primary + whatever proxied data
// did return.
func (s *Server) handleFederationSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	since := parseSinceMs(r.URL.Query().Get("since"))
	includeProxied := r.URL.Query().Get("include") != "none"
	stateFilter := parseStateFilter(r.URL.Query().Get("states"))

	out := FederationResponse{
		Primary: filterSessions(s.manager.ListSessions(), since, stateFilter),
		Proxied: map[string][]*session.Session{},
		Errors:  map[string]string{},
	}

	if !includeProxied || s.cfg == nil || len(s.cfg.Servers) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
		return
	}

	// Parallel fan-out with per-server timeout.
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range s.cfg.Servers {
		srv := s.cfg.Servers[i]
		if !srv.Enabled || srv.URL == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sessions, err := fetchRemoteSessions(srv.URL, srv.Token)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.Errors[srv.Name] = err.Error()
				return
			}
			out.Proxied[srv.Name] = filterSessions(sessions, since, stateFilter)
		}()
	}
	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func fetchRemoteSessions(baseURL, token string) ([]*session.Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	endpoint := strings.TrimRight(baseURL, "/") + "/api/sessions"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("transport: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("auth_failed")
	case http.StatusOK:
	default:
		return nil, fmt.Errorf("http_%d", resp.StatusCode)
	}
	var sessions []*session.Session
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		// Fallback: /api/sessions may return the BL116 enriched shape
		// ({Session + scheduled_count}) — drop to io.ReadAll + custom
		// decode when straight []*Session fails.
		raw, _ := io.ReadAll(resp.Body)
		_ = raw
		return nil, fmt.Errorf("decode: %w", err)
	}
	return sessions, nil
}

func filterSessions(in []*session.Session, sinceMs int64, states map[string]bool) []*session.Session {
	if in == nil {
		return nil
	}
	out := make([]*session.Session, 0, len(in))
	for _, s := range in {
		if s == nil {
			continue
		}
		if sinceMs > 0 && s.UpdatedAt.UnixMilli() < sinceMs {
			continue
		}
		if len(states) > 0 && !states[string(s.State)] {
			continue
		}
		out = append(out, s)
	}
	return out
}

func parseSinceMs(s string) int64 {
	if s == "" {
		return 0
	}
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	return 0
}

func parseStateFilter(csv string) map[string]bool {
	if csv == "" {
		return nil
	}
	out := map[string]bool{}
	for _, s := range strings.Split(csv, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out[s] = true
		}
	}
	return out
}
