// v7.0.0 S5 — REST surface for the scope-hierarchy memory model.
//
//	GET  /api/memory/scopes/recall    list/walk merged across layers
//	GET  /api/memory/scopes/borrow    read-only cross-scope query
//	POST /api/memory/scopes/seed      copy entries (with filter) into a target scope
//	POST /api/memory/scopes/promote   move an entry up the hierarchy with breadcrumb
//	POST /api/memory/scopes/save      write a memory directly to a scope (BL385)
//	POST /api/memory/scopes/delete    delete a memory from a scope by id (BL385)
//
// Returns 503 when no memory backend is wired.

package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/memory"
)

// memoryBackend is the optional accessor a Server may expose for the
// scope endpoints. Implementations type-assert through s.linkStreams
// or via a dedicated setter — but for v7 alpha.5 we expose a
// minimal SetMemoryBackend method.
func (s *Server) SetMemoryBackend(b memory.Backend) { s.memoryBackend = b }

func (s *Server) handleMemoryScopes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/memory/scopes/")

	switch {
	case rest == "recall" && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
		if s.memoryBackend == nil {
			http.Error(w, "memory backend disabled", http.StatusServiceUnavailable)
			return
		}
		s.memoryRecall(w, r)
	case rest == "borrow" && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
		if s.memoryBackend == nil {
			http.Error(w, "memory backend disabled", http.StatusServiceUnavailable)
			return
		}
		s.memoryBorrow(w, r)
	case rest == "seed" && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		if s.memoryBackend == nil {
			http.Error(w, "memory backend disabled", http.StatusServiceUnavailable)
			return
		}
		s.memorySeed(w, r)
	case rest == "promote" && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		if s.memoryBackend == nil {
			http.Error(w, "memory backend disabled", http.StatusServiceUnavailable)
			return
		}
		s.memoryPromote(w, r)
	case rest == "save" && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		if s.memoryBackend == nil {
			http.Error(w, "memory backend disabled", http.StatusServiceUnavailable)
			return
		}
		s.memoryScopesSave(w, r)
	case rest == "delete" && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		if s.memoryBackend == nil {
			http.Error(w, "memory backend disabled", http.StatusServiceUnavailable)
			return
		}
		s.memoryScopesDelete(w, r)
	// BL386 Phase 5 — scope inventory: per-(role,session) row counts.
	case rest == "inventory" && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
		if s.memoryBackend == nil {
			http.Error(w, "memory backend disabled", http.StatusServiceUnavailable)
			return
		}
		s.memoryScopeInventory(w, r)
	// BL386 Phase 3 — archive-import: seed a scope from previously archived memories.
	case rest == "archive-import" && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		if s.memoryBackend == nil {
			http.Error(w, "memory backend disabled", http.StatusServiceUnavailable)
			return
		}
		s.memoryArchiveImport(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) memoryRecall(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	topK := atoiDefault(q.Get("top_k"), 10)
	out, err := memory.ScopedRecall(s.memoryBackend, nil,
		q.Get("persona"), q.Get("project"), q.Get("session"), q.Get("prd_id"), q.Get("story_id"), nil, topK)
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

// memoryScopesSave writes a memory entry directly into a named scope
// (BL385). The caller supplies a ScopeRef (including PRDID/StoryID
// for the new layers) plus the content and optional summary.
func (s *Server) memoryScopesSave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope   memory.ScopeRef `json:"scope"`
		Content string          `json:"content"`
		Summary string          `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Scope.Scope == "" {
		http.Error(w, "scope.scope required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		http.Error(w, "content required", http.StatusBadRequest)
		return
	}
	dir, role, sessID := body.Scope.Resolve()
	id, err := s.memoryBackend.Save(dir, body.Content, body.Summary, role, sessID, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{
		"id":    id,
		"scope": body.Scope,
	})
}

// memoryScopesDelete removes a memory entry by id (BL385). The scope
// field is accepted for auditing but deletion is by id only — the
// Backend.Delete method takes the global row id.
func (s *Server) memoryScopesDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope    memory.ScopeRef `json:"scope"`
		MemoryID int64           `json:"memory_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.MemoryID == 0 {
		http.Error(w, "memory_id required", http.StatusBadRequest)
		return
	}
	if err := s.memoryBackend.Delete(body.MemoryID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{
		"deleted":   body.MemoryID,
		"scope":     body.Scope,
	})
}

// memoryArchiveImport (BL386 Phase 3) — seeds a PRD's prd-shared scope (or
// project-shared) from memories previously archived from a deleted/completed PRD.
// Archived memories are identified by the "archived from prd:<source_prd_id>"
// breadcrumb embedded in their content.
//
//	POST /api/memory/scopes/archive-import
//	{
//	  "project_dir":   "/home/user/proj",
//	  "source_prd_id": "0fb4e302",
//	  "target_prd_id": "a1b2c3d4",   // optional; absent = seed into project-shared
//	  "role_filter":   ["learning"],  // optional
//	  "max":           30,            // optional; default 50
//	  "dry_run":       false
//	}
func (s *Server) memoryArchiveImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectDir   string   `json:"project_dir"`
		SourcePRDID  string   `json:"source_prd_id"`
		TargetPRDID  string   `json:"target_prd_id"`
		RoleFilter   []string `json:"role_filter"`
		Max          int      `json:"max"`
		DryRun       bool     `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.SourcePRDID == "" {
		http.Error(w, "source_prd_id required", http.StatusBadRequest)
		return
	}
	max := body.Max
	if max <= 0 {
		max = 50
	}
	// source is project-shared (where archives land after PRD deletion)
	from := memory.ScopeRef{Scope: memory.ScopeProjectShared, Project: body.ProjectDir}
	var to memory.ScopeRef
	if body.TargetPRDID != "" {
		to = memory.ScopeRef{Scope: memory.ScopePRDShared, Project: body.ProjectDir, PRDID: body.TargetPRDID}
	} else {
		to = memory.ScopeRef{Scope: memory.ScopeProjectShared, Project: body.ProjectDir}
	}
	filter := memory.SeedFilter{
		ContentSubstring: "archived from prd:" + body.SourcePRDID,
	}
	if len(body.RoleFilter) > 0 {
		filter.RolePrefix = body.RoleFilter[0]
	}
	if body.DryRun {
		// Dry run: count what would be seeded without writing
		srcDir, srcRole, _ := from.Resolve()
		var rows []memory.Memory
		var lErr error
		if srcRole != "" {
			rows, lErr = s.memoryBackend.ListByRole(srcDir, srcRole, max*10)
		} else {
			rows, lErr = s.memoryBackend.ListRecent(srcDir, max*10)
		}
		if lErr != nil {
			http.Error(w, lErr.Error(), http.StatusInternalServerError)
			return
		}
		count := 0
		for _, m := range rows {
			if filter.ContentSubstring != "" && !strings.Contains(strings.ToLower(m.Content), strings.ToLower(filter.ContentSubstring)) {
				continue
			}
			count++
			if count >= max {
				break
			}
		}
		writeJSONOK(w, map[string]any{
			"dry_run":        true,
			"would_seed":     count,
			"source_prd_id":  body.SourcePRDID,
			"target_prd_id":  body.TargetPRDID,
		})
		return
	}
	seeded, err := memory.Seed(s.memoryBackend, from, to, filter, max)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{
		"seeded":        seeded,
		"source_prd_id": body.SourcePRDID,
		"target_prd_id": body.TargetPRDID,
		"dry_run":       false,
	})
}

// memoryScopeInventory (BL386 Phase 5) returns per-scope row counts for a
// project dir. Uses the optional InventoryBackend capability; returns 501 if
// the backend doesn't support it.
//
//	GET /api/memory/scopes/inventory?project=...
func (s *Server) memoryScopeInventory(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.memoryBackend.(memory.InventoryBackend)
	if !ok {
		http.Error(w, "inventory not supported by this backend", http.StatusNotImplemented)
		return
	}
	projectDir := r.URL.Query().Get("project")
	entries, err := inv.Inventory(projectDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{
		"project":  projectDir,
		"scopes":   entries,
		"count":    len(entries),
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
