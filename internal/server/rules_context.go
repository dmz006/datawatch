// Rules and context settings over REST: GET and PUT
// /api/autonomous/rules-context. GET returns the configured values, the
// policy resolved for a project directory and, when a PRD is named, the
// planning record (counts, names, hashes and sizes only — never file
// content). PUT replaces the configured values after validating them, then
// saves and installs them through the same steps the config PATCH uses, so
// the config file, REST and the in-memory policy agree.

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

const rulesContextMaxBodyBytes = 1 << 20

// rulesContextPolicyView is the resolved policy for one project directory.
type rulesContextPolicyView struct {
	ProjectDir     string   `json:"project_dir,omitempty"`
	RulesFile      string   `json:"rules_file"`
	ContextFiles   []string `json:"context_files"`
	DigestMode     string   `json:"digest_mode"`
	DigestMaxBytes int      `json:"digest_max_bytes"`
}

// rulesContextRecordView is the planning record without any file content.
type rulesContextRecordView struct {
	PRDID          string                           `json:"prd_id"`
	Recorded       bool                             `json:"recorded"`
	CapturedAt     string                           `json:"captured_at,omitempty"`
	FileCount      int                              `json:"file_count"`
	PresentCount   int                              `json:"present_count"`
	MissingCount   int                              `json:"missing_count"`
	Files          []autonomous.RulesContextFileRef `json:"files"`
	Missing        []string                         `json:"missing"`
	Notes          []string                         `json:"notes"`
	DigestMode     string                           `json:"digest_mode,omitempty"`
	DigestInjected bool                             `json:"digest_injected"`
	DigestBytes    int                              `json:"digest_bytes"`
}

// rulesContextView is the GET and PUT response body.
type rulesContextView struct {
	Config config.AutonomousRulesContextConfig `json:"config"`
	Policy rulesContextPolicyView              `json:"policy"`
	Record *rulesContextRecordView             `json:"record,omitempty"`
}

func rulesContextConfigView(c config.AutonomousRulesContextConfig) config.AutonomousRulesContextConfig {
	if c.ContextFiles == nil {
		c.ContextFiles = []string{}
	}
	if c.Projects == nil {
		c.Projects = []config.AutonomousRulesContextProject{}
	}
	return c
}

func rulesContextPolicyFor(dir string) rulesContextPolicyView {
	p := autonomous.RulesContextFor(dir)
	files := p.ContextFiles
	if files == nil {
		files = []string{}
	}
	return rulesContextPolicyView{ProjectDir: dir, RulesFile: p.RulesFile, ContextFiles: files,
		DigestMode: p.DigestMode, DigestMaxBytes: p.DigestMaxBytes}
}

// rulesContextRecordViewFor finds the PRD and summarises its record. ok is
// false when the PRD does not exist.
func (s *Server) rulesContextRecordViewFor(prdID string) (*rulesContextRecordView, bool) {
	if s.autonomousMgr == nil {
		return nil, false
	}
	raw, found := s.autonomousMgr.GetPRD(prdID)
	if !found {
		return nil, false
	}
	var rec *autonomous.RulesContextRecord
	switch p := raw.(type) {
	case *autonomous.PRD:
		if p == nil {
			return nil, false
		}
		rec = p.RulesContext
	case autonomous.PRD:
		rec = p.RulesContext
	default:
		return nil, false
	}
	v := &rulesContextRecordView{PRDID: prdID, Files: []autonomous.RulesContextFileRef{}, Missing: []string{}, Notes: []string{}}
	if rec == nil {
		return v, true
	}
	v.Recorded = true
	v.CapturedAt = rec.CapturedAt.UTC().Format("2006-01-02T15:04:05Z")
	if rec.Files != nil {
		v.Files = rec.Files
	}
	if rec.Missing != nil {
		v.Missing = rec.Missing
	}
	if rec.Notes != nil {
		v.Notes = rec.Notes
	}
	for _, f := range v.Files {
		if f.Present {
			v.PresentCount++
		}
	}
	v.FileCount = len(v.Files)
	v.MissingCount = len(v.Missing)
	v.DigestMode, v.DigestInjected, v.DigestBytes = rec.DigestMode, rec.DigestInjected, rec.DigestBytes
	return v, true
}

// handleAutonomousRulesContext serves GET and PUT /api/autonomous/rules-context.
func (s *Server) handleAutonomousRulesContext(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
	case http.MethodPut:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodPut {
		s.putRulesContext(w, r)
		return
	}
	dir := strings.TrimSpace(r.URL.Query().Get("project_dir"))
	out := rulesContextView{Config: rulesContextConfigView(s.cfg.Autonomous.RulesContext), Policy: rulesContextPolicyFor(dir)}
	if prdID := strings.TrimSpace(r.URL.Query().Get("prd")); prdID != "" {
		rec, ok := s.rulesContextRecordViewFor(prdID)
		if !ok {
			http.Error(w, "prd not found: "+prdID, http.StatusNotFound)
			return
		}
		out.Record = rec
	}
	writeRulesContextJSON(w, out)
}

func (s *Server) putRulesContext(w http.ResponseWriter, r *http.Request) {
	if s.cfgPath == "" {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	var next config.AutonomousRulesContextConfig
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, rulesContextMaxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&next); err != nil {
		http.Error(w, "bad request: invalid rules-context body: "+err.Error(), http.StatusBadRequest)
		return
	}
	next.RulesFile = strings.TrimSpace(next.RulesFile)
	next.DigestMode = config.NormalizeRulesContextMode(next.DigestMode)
	if err := next.Validate(); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	prev := s.cfg.Autonomous.RulesContext
	s.cfg.Autonomous.RulesContext = next
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		s.cfg.Autonomous.RulesContext = prev
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	autonomous.SetRulesContextConfig(next)
	s.auditConfigPatch(r.Context(), map[string]interface{}{"autonomous.rules_context": next}, nil)
	writeRulesContextJSON(w, rulesContextView{Config: rulesContextConfigView(next), Policy: rulesContextPolicyFor(strings.TrimSpace(r.URL.Query().Get("project_dir")))})
}

func writeRulesContextJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
