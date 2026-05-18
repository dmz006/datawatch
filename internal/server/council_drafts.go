// BL297 (v6.22.3) — Council Mode "Add Persona" wizard REST surface.
//
// Endpoints (all under /api/council/personas/draft*):
//
//   POST   /api/council/personas/draft         — start a draft wizard
//                                                  body: {operator_ref, channel_ref}
//                                                  returns: Draft (with id + first question)
//   GET    /api/council/personas/draft/{id}    — fetch current draft state
//   PATCH  /api/council/personas/draft/{id}    — update operator-supplied answers
//                                                  body: {field_name: value, ...}
//   POST   /api/council/personas/draft/{id}/refine
//                                                  body: {instruction, backend?}
//                                                  re-asks LLM with current state
//   POST   /api/council/personas/draft/{id}/save
//                                                  finalize → POST /api/council/personas
//   POST   /api/council/personas/draft/{id}/abandon
//                                                  marks draft as abandoned
//
//   GET    /api/council/personas/drafts        — list every draft (operator inspection)
//   DELETE /api/council/personas/drafts/{id}   — selective cleanup
//   DELETE /api/council/personas/drafts        — purge ALL drafts
//
// Plus a one-shot path for CLI / one-way channels:
//
//   POST   /api/council/personas/oneshot       — body: {name,role,focus,stance,tone,
//                                                       anti_patterns,examples,backend}
//                                                  returns: drafted persona YAML
//                                                  (does NOT save; operator follows up
//                                                  with /api/council/personas POST)
//
// Operator-decision references: 2026-05-08 BL297 interview Q1-Q10 + Q-final-A/B/C.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/council"
	"github.com/dmz006/datawatch/internal/federation"
)

// SetCouncilDrafts wires the drafts store + LLM caller for the wizard.
func (s *Server) SetCouncilDrafts(store *council.DraftsStore) {
	s.councilDrafts = store
}

// handleCouncilDrafts is the dispatcher for /api/council/personas/draft*
// AND /api/council/personas/drafts*. Mounted under existing
// handleCouncilPersonas — see council.go for the routing precedence.
func (s *Server) handleCouncilDrafts(w http.ResponseWriter, r *http.Request, rest string) bool {
	if s.councilDrafts == nil {
		http.Error(w, "council drafts disabled (no store wired)", http.StatusServiceUnavailable)
		return true
	}
	// Routing — every path here starts with "draft" or "drafts" (after
	// the /api/council/personas/ prefix has been trimmed).
	switch {
	case rest == "draft" && r.Method == http.MethodPost:
		s.councilDraftStart(w, r)
		return true
	case rest == "drafts" && r.Method == http.MethodGet:
		s.councilDraftsList(w, r)
		return true
	case rest == "drafts" && r.Method == http.MethodDelete:
		s.councilDraftsPurge(w, r)
		return true
	case rest == "oneshot" && r.Method == http.MethodPost:
		s.councilDraftOneShot(w, r)
		return true
	case strings.HasPrefix(rest, "draft/"):
		tail := strings.TrimPrefix(rest, "draft/")
		// {id} | {id}/refine | {id}/save | {id}/abandon
		if i := strings.Index(tail, "/"); i >= 0 {
			id := tail[:i]
			action := tail[i+1:]
			s.councilDraftAction(w, r, id, action)
		} else {
			s.councilDraftIDOp(w, r, tail)
		}
		return true
	case strings.HasPrefix(rest, "drafts/"):
		id := strings.TrimPrefix(rest, "drafts/")
		if r.Method == http.MethodDelete {
			s.councilDraftDelete(w, r, id)
			return true
		}
	}
	return false
}

func (s *Server) councilDraftStart(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCouncilRun) {
		return
	}
	var body struct {
		OperatorRef string `json:"operator_ref"`
		ChannelRef  string `json:"channel_ref"`
		Backend     string `json:"backend"`
		// Optional pre-fill from the entry form.
		Name string `json:"name"`
		Role string `json:"role"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	d, err := s.councilDrafts.New(body.OperatorRef, body.ChannelRef)
	if err != nil {
		http.Error(w, "draft new: "+err.Error(), http.StatusInternalServerError)
		return
	}
	d.Backend = body.Backend
	d.Name = strings.TrimSpace(body.Name)
	d.Role = strings.TrimSpace(body.Role)
	if d.Name != "" || d.Role != "" {
		d.CurrentStep = "focus"
	}
	if err := s.councilDrafts.Update(d); err != nil {
		http.Error(w, "draft update: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, d)
}

func (s *Server) councilDraftIDOp(w http.ResponseWriter, r *http.Request, id string) {
	if !s.fedCap(w, r, federation.CapCouncilRun) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		d, err := s.councilDrafts.Get(id)
		if err != nil {
			http.Error(w, "draft "+err.Error(), http.StatusNotFound)
			return
		}
		writeJSONOK(w, d)
	case http.MethodPatch:
		d, err := s.councilDrafts.Get(id)
		if err != nil {
			http.Error(w, "draft "+err.Error(), http.StatusNotFound)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		applyDraftPatch(d, body)
		if err := s.councilDrafts.Update(d); err != nil {
			http.Error(w, "draft update: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, d)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) councilDraftAction(w http.ResponseWriter, r *http.Request, id, action string) {
	if !s.fedCap(w, r, federation.CapCouncilRun) {
		return
	}
	d, err := s.councilDrafts.Get(id)
	if err != nil {
		http.Error(w, "draft "+err.Error(), http.StatusNotFound)
		return
	}
	switch action {
	case "refine":
		var body struct {
			Instruction string `json:"instruction"`
			Backend     string `json:"backend"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Backend != "" {
			d.Backend = body.Backend
		}
		out, tags, err := s.councilDraftLLM(d, body.Instruction)
		if err != nil {
			http.Error(w, "draft refine: "+err.Error(), http.StatusBadGateway)
			return
		}
		d.DraftPersona = out
		d.DraftTags = tags
		d.Status = council.DraftDrafted
		if err := s.councilDrafts.Update(d); err != nil {
			http.Error(w, "draft update: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, d)
	case "save":
		if d.DraftPersona == "" {
			http.Error(w, "no draft persona to save (run refine first)", http.StatusBadRequest)
			return
		}
		// Parse the YAML to extract name / role / system_prompt + tags.
		p, parseErr := council.ParsePersonaYAML(d.DraftPersona)
		if parseErr != nil {
			http.Error(w, "draft save: invalid persona yaml: "+parseErr.Error(), http.StatusBadRequest)
			return
		}
		if s.councilOrch == nil {
			http.Error(w, "council orchestrator unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := s.councilOrch.AddPersona(p); err != nil {
			http.Error(w, "council add: "+err.Error(), http.StatusInternalServerError)
			return
		}
		d.Status = council.DraftSaved
		_ = s.councilDrafts.Update(d)
		writeJSONOK(w, map[string]any{"persona": p, "draft_id": d.ID, "status": "saved"})
	case "abandon":
		d.Status = council.DraftAbandoned
		if err := s.councilDrafts.Update(d); err != nil {
			http.Error(w, "draft update: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, d)
	default:
		http.Error(w, "unknown action: "+action, http.StatusBadRequest)
	}
}

func (s *Server) councilDraftsList(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCouncilRun) {
		return
	}
	drafts, err := s.councilDrafts.List()
	if err != nil {
		http.Error(w, "drafts list: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{"drafts": drafts})
}

func (s *Server) councilDraftDelete(w http.ResponseWriter, r *http.Request, id string) {
	if !s.fedCap(w, r, federation.CapCouncilRun) {
		return
	}
	if err := s.councilDrafts.Delete(id); err != nil {
		http.Error(w, "drafts delete: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{"id": id, "status": "deleted"})
}

func (s *Server) councilDraftsPurge(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCouncilRun) {
		return
	}
	n, err := s.councilDrafts.Purge()
	if err != nil {
		http.Error(w, "drafts purge: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{"deleted": n})
}

// councilDraftOneShot accepts all 7 fields + backend, runs the LLM, and
// returns the drafted persona YAML inline. No persistence; for CLI +
// one-way channels per BL297 Q9 design.
func (s *Server) councilDraftOneShot(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCouncilRun) {
		return
	}
	var body struct {
		Name         string `json:"name"`
		Role         string `json:"role"`
		Focus        string `json:"focus"`
		Stance       string `json:"stance"`
		Tone         string `json:"tone"`
		AntiPatterns string `json:"anti_patterns"`
		Examples     string `json:"examples"`
		Backend      string `json:"backend"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	d := &council.Draft{
		Name:         body.Name,
		Role:         body.Role,
		Focus:        body.Focus,
		Stance:       body.Stance,
		Tone:         body.Tone,
		AntiPatterns: body.AntiPatterns,
		Examples:     body.Examples,
		Backend:      body.Backend,
	}
	yaml, tags, err := s.councilDraftLLM(d, "")
	if err != nil {
		http.Error(w, "oneshot llm: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSONOK(w, map[string]any{
		"persona_yaml": yaml,
		"tags":         strings.Split(tags, ","),
	})
}

// councilDraftLLM drives the LLM call. Reuses /api/ask plumbing.
func (s *Server) councilDraftLLM(d *council.Draft, refineInstruction string) (string, string, error) {
	if s.cfg == nil {
		return "", "", fmt.Errorf("server config unavailable")
	}
	backend := strings.ToLower(strings.TrimSpace(d.Backend))
	if backend == "" {
		// Default policy: ollama if configured, else openwebui.
		if s.cfg.Ollama.Host != "" {
			backend = "ollama"
		} else if s.cfg.OpenWebUI.URL != "" {
			backend = "openwebui"
		} else {
			return "", "", fmt.Errorf("no LLM backend configured (set ollama.host or openwebui.url)")
		}
	}
	prompt := buildCouncilDrafterPrompt(d, refineInstruction)
	req := AskRequest{Question: prompt, Backend: backend}
	var (
		answer string
		err    error
	)
	switch backend {
	case "ollama":
		answer, err = askOllama(s, req)
	case "openwebui":
		answer, err = askOpenWebUI(s, req)
	default:
		return "", "", fmt.Errorf("unsupported backend for council drafter: %s", backend)
	}
	if err != nil {
		return "", "", err
	}
	yaml, tags := council.ExtractPersonaYAMLAndTags(answer)
	if yaml == "" {
		return "", "", fmt.Errorf("LLM returned no parseable persona YAML; raw output: %s", truncate(answer, 200))
	}
	return yaml, tags, nil
}

// applyDraftPatch — partial update of operator-supplied interview fields.
func applyDraftPatch(d *council.Draft, body map[string]string) {
	if v, ok := body["name"]; ok {
		d.Name = v
	}
	if v, ok := body["role"]; ok {
		d.Role = v
	}
	if v, ok := body["focus"]; ok {
		d.Focus = v
	}
	if v, ok := body["stance"]; ok {
		d.Stance = v
	}
	if v, ok := body["tone"]; ok {
		d.Tone = v
	}
	if v, ok := body["anti_patterns"]; ok {
		d.AntiPatterns = v
	}
	if v, ok := body["examples"]; ok {
		d.Examples = v
	}
	if v, ok := body["backend"]; ok {
		d.Backend = v
	}
	if v, ok := body["current_step"]; ok {
		d.CurrentStep = v
	}
	if v, ok := body["draft_persona"]; ok {
		d.DraftPersona = v
	}
}

// buildCouncilDrafterPrompt composes the system + user prompt for the
// drafter LLM. The model's output must be parseable persona YAML by
// council.ParsePersonaYAML.
func buildCouncilDrafterPrompt(d *council.Draft, refineInstruction string) string {
	var b strings.Builder
	b.WriteString(`You are a Council Mode persona drafter for the datawatch operator-control-plane.

You produce a YAML persona definition for a council debate participant from
the operator's interview answers. Output STRICTLY a single YAML document:

  name: <kebab-case-name>
  role: <Title — One-sentence "what they do">
  system_prompt: |
    You are a {role}. For each proposal, evaluate:
    * <bullet 1>
    * <bullet 2>
    * <bullet 3-5>
    Be specific; reference the proposal's actual surfaces.
  tags: [tag1, tag2, tag3]

Rules:
- name must be kebab-case (lowercase + hyphens). Derive from operator-supplied name.
- role MUST be "<Title> — <one-sentence description of what they do>" combining the operator-supplied title + a clear description.
- system_prompt must be 5-12 lines, in second person ("You are…"), action-oriented.
- tags is 2-4 short lowercase words capturing the persona's domain.
- No prose around the YAML. No code-fence wrapping. Just the YAML.
`)
	b.WriteString("\nOperator-supplied interview answers:\n")
	b.WriteString(d.SerializeFields())
	if refineInstruction != "" {
		b.WriteString("\nThe operator has reviewed the previous draft and asked:\n  ")
		b.WriteString(refineInstruction)
		b.WriteString("\n\nProduce an updated YAML accordingly.\n")
	}
	if d.DraftPersona != "" && refineInstruction != "" {
		b.WriteString("\nPrevious draft (for context — apply the operator's instruction to this):\n")
		b.WriteString(d.DraftPersona)
	}
	return b.String()
}
