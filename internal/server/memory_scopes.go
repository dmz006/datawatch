// v7.0.0 S5 — REST surface for the scope-hierarchy memory model.
//
//	GET  /api/memory/scopes/recall    list/walk merged across layers
//	GET  /api/memory/scopes/borrow    read-only cross-scope query
//	POST /api/memory/scopes/seed      copy entries (with filter) into a target scope
//	POST /api/memory/scopes/promote   move an entry up the hierarchy with breadcrumb
//
// Returns 503 when no memory backend is wired.

package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/memory"
)

// memoryBackend is the optional accessor a Server may expose for the
// scope endpoints. Implementations type-assert through s.linkStreams
// or via a dedicated setter — but for v7 alpha.5 we expose a
// minimal SetMemoryBackend method.
func (s *Server) SetMemoryBackend(b memory.Backend) { s.memoryBackend = b }

func (s *Server) handleMemoryScopes(w http.ResponseWriter, r *http.Request) {
	if s.memoryBackend == nil {
		http.Error(w, "memory backend disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/memory/scopes/")

	switch {
	case rest == "recall" && r.Method == http.MethodGet:
		s.memoryRecall(w, r)
	case rest == "borrow" && r.Method == http.MethodGet:
		s.memoryBorrow(w, r)
	case rest == "seed" && r.Method == http.MethodPost:
		s.memorySeed(w, r)
	case rest == "promote" && r.Method == http.MethodPost:
		s.memoryPromote(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) memoryRecall(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	topK := atoiDefault(q.Get("top_k"), 10)
	out, err := memory.ScopedRecall(s.memoryBackend, nil,
		q.Get("persona"), q.Get("project"), q.Get("session"), nil, topK)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{
		"results": out,
		"count":   len(out),
	})
}

func (s *Server) memoryBorrow(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	topK := atoiDefault(q.Get("top_k"), 10)
	from := memory.ScopeRef{
		Scope:     memory.Scope(q.Get("scope")),
		Persona:   q.Get("persona"),
		Project:   q.Get("project"),
		SessionID: q.Get("session"),
	}
	if from.Scope == "" {
		http.Error(w, "scope query param required", http.StatusBadRequest)
		return
	}
	hits, err := memory.BorrowReadOnly(s.memoryBackend, from, nil, topK)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{
		"from":    from,
		"results": hits,
		"count":   len(hits),
	})
}

func (s *Server) memorySeed(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From       memory.ScopeRef `json:"from"`
		To         memory.ScopeRef `json:"to"`
		Filter     memory.SeedFilter `json:"filter"`
		Limit      int              `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.From.Scope == "" || body.To.Scope == "" {
		http.Error(w, "from.scope + to.scope required", http.StatusBadRequest)
		return
	}
	if body.Limit == 0 {
		body.Limit = 100
	}
	n, err := memory.Seed(s.memoryBackend, body.From, body.To, body.Filter, body.Limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{
		"copied":  n,
		"from":    body.From,
		"to":      body.To,
	})
}

func (s *Server) memoryPromote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MemoryID   int64           `json:"memory_id"`
		From       memory.ScopeRef `json:"from"`
		To         memory.ScopeRef `json:"to"`
		Breadcrumb memory.Breadcrumb `json:"breadcrumb"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.MemoryID == 0 || body.From.Scope == "" || body.To.Scope == "" {
		http.Error(w, "memory_id + from.scope + to.scope required", http.StatusBadRequest)
		return
	}
	newID, bc, err := memory.Promote(s.memoryBackend, body.MemoryID, body.From, body.To, body.Breadcrumb)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{
		"new_memory_id": newID,
		"breadcrumb":    bc,
	})
}

func atoiDefault(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// silence unused-import in tiny patches.
var _ = time.Now
