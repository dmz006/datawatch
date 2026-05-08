// BL274 Sprint 1, v6.16.0 — REST endpoints for the Docs-as-MCP-Interface.
//
//   GET  /api/docs/search?q=...&limit=10&sources=core,skill:foo
//   GET  /api/docs/read?path=howto/secrets-manager.md&anchor=rotating-a-secret
//   GET  /api/docs/list-howtos
//   POST /api/docs/apply        body: {howto_id, params, mode: "plan"|"execute", approval_token, risk_gate}
//
// Plus trust + pending-trust sub-paths:
//   GET    /api/docs/trust              → list trusted sources
//   POST   /api/docs/trust              body: {source, granted_by, note}   → add
//   DELETE /api/docs/trust/{source}     → remove
//   GET    /api/docs/trust/pending      → list pending sources
//   POST   /api/docs/trust/accept       body: {sources: [...]}
//   POST   /api/docs/trust/dismiss      body: {sources: [...]}
//   GET    /api/docs/trust/export       → flat list of trusted sources for config copy

package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/docsindex"
)

// handleDocs is the top-level dispatcher for /api/docs/*.
func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	rt := docsindex.Default()
	if rt == nil {
		http.Error(w, "docsindex not initialized", http.StatusServiceUnavailable)
		return
	}
	path := r.URL.Path
	switch {
	case path == "/api/docs/search":
		s.handleDocsSearch(w, r, rt)
	case path == "/api/docs/read":
		s.handleDocsRead(w, r, rt)
	case path == "/api/docs/list-howtos":
		s.handleDocsListHowtos(w, r, rt)
	case path == "/api/docs/apply":
		s.handleDocsApply(w, r, rt)
	case path == "/api/docs/trust":
		s.handleDocsTrust(w, r, rt)
	case path == "/api/docs/trust/pending":
		s.handleDocsTrustPending(w, r, rt)
	case path == "/api/docs/trust/accept":
		s.handleDocsTrustAccept(w, r, rt)
	case path == "/api/docs/trust/dismiss":
		s.handleDocsTrustDismiss(w, r, rt)
	case path == "/api/docs/trust/export":
		s.handleDocsTrustExport(w, r, rt)
	case strings.HasPrefix(path, "/api/docs/trust/"):
		// /api/docs/trust/{source} for DELETE
		source := strings.TrimPrefix(path, "/api/docs/trust/")
		s.handleDocsTrustDelete(w, r, rt, source)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (s *Server) handleDocsSearch(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, "q required", http.StatusBadRequest)
		return
	}
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	var sources []string
	if s := r.URL.Query().Get("sources"); s != "" {
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				sources = append(sources, p)
			}
		}
	}
	hits := rt.Search(q, limit, sources)
	type apiHit struct {
		ChunkID   string  `json:"chunk_id"`
		Source    string  `json:"source"`
		Path      string  `json:"path"`
		Anchor    string  `json:"anchor"`
		Title     string  `json:"title"`
		Heading   string  `json:"heading,omitempty"`
		Excerpt   string  `json:"excerpt"`
		Score     float64 `json:"score"`
		IndexKind string  `json:"index_kind"`
	}
	out := make([]apiHit, len(hits))
	for i, h := range hits {
		out[i] = apiHit{
			ChunkID:   h.Chunk.ChunkID(),
			Source:    h.Chunk.Source,
			Path:      h.Chunk.Path,
			Anchor:    h.Chunk.Anchor,
			Title:     h.Chunk.Title,
			Heading:   h.Chunk.Heading,
			Excerpt:   docsindex.Excerpt(h.Chunk.Body, 280),
			Score:     h.Score,
			IndexKind: h.Kind,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"hits": out})
}

func (s *Server) handleDocsRead(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	path := strings.TrimSpace(q.Get("path"))
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	anchor, _ := url.QueryUnescape(q.Get("anchor"))
	c, err := rt.Read(path, anchor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"path":     c.Path,
		"anchor":   c.Anchor,
		"title":    c.Title,
		"heading":  c.Heading,
		"content":  c.Body,
		"see_also": c.SeeAlso,
		"source":   c.Source,
	})
}

func (s *Server) handleDocsListHowtos(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"howtos": rt.ListHowtos()})
}

func (s *Server) handleDocsApply(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		HowtoID       string            `json:"howto_id"`
		Params        map[string]string `json:"params"`
		Mode          string            `json:"mode"`
		RiskGate      string            `json:"risk_gate"`
		ApprovalToken string            `json:"approval_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.HowtoID == "" {
		http.Error(w, "howto_id required", http.StatusBadRequest)
		return
	}
	if body.Mode == "" {
		body.Mode = "plan"
	}
	// Sprint 1 ships plan-only. Execute mode lands in Sprint 3.
	if body.Mode != "plan" {
		http.Error(w, "mode=execute not implemented in v6.16.0 (planned for v6.18.0; track BL274 sprint 3)", http.StatusNotImplemented)
		return
	}
	// Resolve howto_id → path. Format: "<path>" or "<path>#<anchor-tag>"
	// Sprint 1 only supports authored exec_steps; LLM-translation in Sprint 3.
	path := body.HowtoID
	if i := strings.Index(path, "#"); i >= 0 {
		path = path[:i]
	}
	c, err := rt.Read(path, "") // reads first chunk; we look at the doc body for frontmatter
	if err != nil {
		http.Error(w, "howto not found: "+err.Error(), http.StatusNotFound)
		return
	}
	fm, err := docsindex.ParseFrontMatter(c.Body)
	if err != nil {
		http.Error(w, "frontmatter parse: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !fm.HasExecSteps() {
		// Sprint 3 will route to LLM-translation here.
		http.Error(w, "howto has no authored exec_steps; LLM-translation fallback ships in v6.18.0 (BL274 sprint 3)", http.StatusNotImplemented)
		return
	}
	steps, err := fm.ResolveExecSteps(body.Params)
	if err != nil {
		http.Error(w, "resolve params: "+err.Error(), http.StatusBadRequest)
		return
	}
	out := map[string]interface{}{
		"howto_id":          body.HowtoID,
		"steps":             steps,
		"approval_required": true,
		// Sprint 3 issues an approval_token here for the execute round-trip.
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleDocsTrust(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"trusted": rt.Trust().List()})
	case http.MethodPost:
		var body struct {
			Source    string `json:"source"`
			GrantedBy string `json:"granted_by"`
			Note      string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if body.Source == "" {
			http.Error(w, "source required", http.StatusBadRequest)
			return
		}
		if body.GrantedBy == "" {
			body.GrantedBy = "operator"
		}
		added, err := rt.Trust().Trust(body.Source, body.GrantedBy, body.Note)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Auto-clear from pending queue when accepted.
		_ = rt.Pending().Remove(body.Source)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"added": added})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDocsTrustDelete(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime, source string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if source == "" {
		http.Error(w, "source required", http.StatusBadRequest)
		return
	}
	removed, err := rt.Trust().Untrust(source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"removed": removed})
}

func (s *Server) handleDocsTrustPending(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"pending": rt.Pending().List(),
		"count":   rt.Pending().Count(),
	})
}

func (s *Server) handleDocsTrustAccept(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Sources []string `json:"sources"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	added := 0
	for _, src := range body.Sources {
		if was, _ := rt.Trust().Trust(src, "operator", ""); was {
			added++
		}
		_ = rt.Pending().Remove(src)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"added": added})
}

func (s *Server) handleDocsTrustDismiss(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Sources []string `json:"sources"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	dismissed := 0
	for _, src := range body.Sources {
		_ = rt.Pending().Remove(src)
		dismissed++
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"dismissed": dismissed})
}

func (s *Server) handleDocsTrustExport(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"sources":      rt.Trust().Export(),
		"yaml_snippet": "docs_search:\n  trust:\n" + indentList(rt.Trust().Export()),
	})
}

// indentList formats a slice as YAML "  - item" lines.
func indentList(items []string) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString("    - ")
		b.WriteString(it)
		b.WriteByte('\n')
	}
	return b.String()
}
