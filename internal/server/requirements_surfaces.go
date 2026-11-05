package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// This file wires the requirements engine (capability minimum-backend-
// versions + automation backend minimums), the model-pull consent policy and
// the compute node backend versions onto the REST surface. The engines
// themselves live in internal/autonomous / internal/compute; this file only
// adapts them for HTTP.

// requirementsReporter returns one requirements report for a backend
// summary. The autonomous manager implements it; tests use a fake.
type requirementsReporter interface {
	RequirementsReport(backends autonomous.AutomationBackends) autonomous.RequirementsReport
}

// RequirementsSurfacesRESTOperation is one requirements surface REST route,
// read by the parity test.
type RequirementsSurfacesRESTOperation struct {
	Name   string
	Method string
	Path   string
}

// RequirementsSurfacesRESTOperations is the single list of requirements
// surface REST operations (the capabilities report, the model-pull consent
// policy read and set, and the compute node backend versions).
func RequirementsSurfacesRESTOperations() []RequirementsSurfacesRESTOperation {
	return []RequirementsSurfacesRESTOperation{
		{"requirements_report", http.MethodGet, "/api/automation/requirements-report"},
		{"model_consent_get", http.MethodGet, "/api/automation/model-consent"},
		{"model_consent_set", http.MethodPut, "/api/automation/model-consent"},
		{"node_backend_versions", http.MethodGet, "/api/compute/nodes/{name}/versions"},
	}
}

// automationBackendsSummary returns the current automation backend summary
// (per-role backend lists plus Claude / local-pool flags).
type automationBackendsSummary func() autonomous.AutomationBackends

// SetRequirementsReporter wires the requirements engine in (usually the
// autonomous manager). A nil reporter unsets it (route reports 503).
func (s *Server) SetRequirementsReporter(rep requirementsReporter) {
	s.requirementsReporter = rep
}

// SetAutomationBackends wires the backend-summary provider used with the
// requirements reporter.
func (s *Server) SetAutomationBackends(f automationBackendsSummary) {
	s.automationBackends = f
}

// automationBackendsNow summarizes the current config: the per-role
// backends (execution, planning, verification), whether a Claude backend
// is reachable, and whether a local model pool (local LLM compute node)
// exists. The flags are heuristics — the source of truth for "configured"
// is the runtime state, this is the config-level picture the report shows.
func (s *Server) automationBackendsNow() autonomous.AutomationBackends {
	b := autonomous.AutomationBackends{}
	if s.cfg != nil {
		a := s.cfg.Autonomous
		if a.ExecutionBackend != "" {
			b.Execution = []string{a.ExecutionBackend}
		}
		if a.PlanningBackend != "" {
			b.Planning = []string{a.PlanningBackend}
		}
		b.Verification = a.VerificationBackends
		b.ClaudeConfigured = s.cfg.Session.ClaudeEnabled
		autobackends := append(append(append([]string{}, b.Execution...), b.Planning...), b.Verification...)
		for _, name := range autobackends {
			if strings.Contains(strings.ToLower(name), "claude") {
				b.ClaudeConfigured = true
				break
			}
		}
	}
	// A local model pool is any local LLM compute node (Ollama or an
	// OpenAI-compatible server) the local registry knows about.
	if s.computeReg != nil {
		for _, n := range s.computeReg.List() {
			if n.Kind == compute.KindOllama || n.Kind == compute.KindOpenAICompat {
				b.LocalPoolConfigured = true
				break
			}
		}
	}
	return b
}

// wireRequirementsFromAuto detects the requirements engine inside an
// already-wired autonomous API (the concrete autonomous manager
// implements it) and wires it plus a config-derived backend summary
// into the requirements surface. A nil API leaves the route at 503.
func (s *Server) wireRequirementsFromAuto(api AutonomousAPI) {
	if eng, ok := api.(interface {
		RequirementsReport(autonomous.AutomationBackends) autonomous.RequirementsReport
	}); ok {
		s.requirementsReporter = eng
		s.automationBackends = s.automationBackendsNow
	}
}

// requirementsReport builds the current report. ok is false when no engine
// is wired in.
func (s *Server) requirementsReport() (autonomous.RequirementsReport, bool) {
	if s.requirementsReporter == nil {
		return autonomous.RequirementsReport{}, false
	}
	var b autonomous.AutomationBackends
	if s.automationBackends != nil {
		b = s.automationBackends()
	}
	return s.requirementsReporter.RequirementsReport(b), true
}

// modelPullDecisionRule states the gate rule in words (docs + the GET
// response share this single statement).
const modelPullDecisionRule = "A model pull is allowed when the model name is listed with allowed: true, or when the model is not listed and its publisher is on the publisher allow-list; every other pull is refused."

// handleRequirementsReport serves GET /api/automation/requirements-report
// (format=json is the default, format=text returns the shared text
// rendering).
func (s *Server) handleRequirementsReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	rep, ok := s.requirementsReport()
	if !ok {
		http.Error(w, "the requirements engine is not available", http.StatusServiceUnavailable)
		return
	}
	if r.URL.Query().Get("format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := fmt.Fprint(w, rep.Text()); err != nil {
			return
		}
		return
	}
	writeJSONOK(w, rep)
}

// modelConsentView is the GET model-consent body: the policy in the form
// the gate decides on, plus the decision rule it implements.
type modelConsentView struct {
	Models       []config.ModelConsentEntry `json:"models"`
	Publishers   []string                   `json:"publishers"`
	DecisionRule string                     `json:"decision_rule"`
}

// handleModelConsent serves GET/PUT /api/automation/model-consent.
//
// GET: the current consent policy as JSON.
//
// PUT: body {"models":[...], "publishers":[...]} — the complete policy to
// store (same shape and same rejection rules as
// autonomous.model_pull_consent on PUT /api/config and PUT
// /api/autonomous/config). An item may be a model name (string, allowed:
// true) or an object {name, allowed, note}. On success: saved to config,
// synced into the autonomous manager (when wired), audited as
// model_pull_consent_changed, 200 {"status":"ok"}. Invalid input: 400.
func (s *Server) handleModelConsent(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		sel := config.ModelPullConsent{}
		if s.cfg != nil {
			sel = s.cfg.Autonomous.ModelPullConsent
		}
		if sel.Models == nil {
			sel.Models = []config.ModelConsentEntry{}
		}
		if sel.Publishers == nil {
			sel.Publishers = []string{}
		}
		writeJSONOK(w, modelConsentView{Models: sel.Models, Publishers: sel.Publishers, DecisionRule: modelPullDecisionRule})

	case http.MethodPut:
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		var mpc config.ModelPullConsent
		if err := json.NewDecoder(r.Body).Decode(&mpc); err != nil {
			http.Error(w, `model-consent: body must be an object with "models" and/or "publishers"`, http.StatusBadRequest)
			return
		}
		if verr := config.ValidateModelPullConsent(mpc); verr != nil {
			http.Error(w, "model-consent: "+verr.Error(), http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(mpc)
		if s.cfg == nil {
			http.Error(w, "config is not available", http.StatusInternalServerError)
			return
		}
		// Model names as "name=pull_allowed"/"name=pull_refused" for the
		// audit trail (same rule as the autonomous config surface).
		names := make([]string, 0, len(mpc.Models))
		for i := range mpc.Models {
			if mpc.Models[i].Allowed {
				names = append(names, mpc.Models[i].Name+"=pull_allowed")
			} else {
				names = append(names, mpc.Models[i].Name+"=pull_refused")
			}
		}
		s.cfg.Autonomous.ModelPullConsent = mpc
		_ = config.Save(s.cfg, s.cfgPath)
		if s.autonomousMgr != nil {
			patch, err := json.Marshal(map[string]any{"model_pull_consent": mpc})
			if err == nil {
				_ = s.autonomousMgr.SetConfig(patch)
			}
		}
		if s.auditLog != nil {
			s.auditAs(s.auditActor(r.Context()), audit.ActionModelPullConsentChanged, "model_pull_consent", "autonomous",
				map[string]any{"models": names, "publishers": mpc.Publishers, "payload": string(payload)})
		}
		writeJSONOK(w, map[string]any{"status": "ok"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// backendVersionLine is one capability's minimum applied to one node's
// reported backend version.
type backendVersionLine struct {
	Capability string `json:"capability"`
	Minimum    string `json:"minimum"`
	OK         *bool  `json:"ok"` // nil when the version is unknown
	Unknown    bool   `json:"unknown"`
	Reason     string `json:"reason,omitempty"`
}

// nodeBackendVersions is GET /api/compute/nodes/{name}/versions: the node
// identity that carries a backend version, the version as reported (with
// when it was recorded), and per-capability minimums applied to it.
type nodeBackendVersions struct {
	Name            string               `json:"name"`
	Kind            string               `json:"kind"`
	Address         string               `json:"address"`
	Backend         string               `json:"backend,omitempty"`
	BackendVersion  string               `json:"backend_version"`
	VersionProbedAt string               `json:"version_probed_at,omitempty"` // RFC3339, empty when never recorded
	Capabilities    []backendVersionLine `json:"capabilities"`
}

// handleNodeBackendVersions serves GET /api/compute/nodes/{name}/versions.
// Known nodes are required ("node not found" otherwise): versions must be
// attributed to a node.
func (s *Server) handleNodeBackendVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapComputeRead) {
		return
	}
	name := r.PathValue("name")
	if name == "" {
		// Direct handler invocation (tests) or non-pattern routing: derive
		// the name from the path.
		p := strings.TrimPrefix(r.URL.Path, "/api/compute/nodes/")
		p = strings.TrimSuffix(p, "/versions")
		if p != "" || strings.Contains(r.URL.Path, "/versions") {
			name = p
		}
	}
	if name == "" {
		http.Error(w, "missing node name", http.StatusBadRequest)
		return
	}
	n, err := s.computeReg.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	overrides := map[string]string{}
	if s.cfg != nil {
		for k, v := range s.cfg.Autonomous.Requirements.MinOllama {
			overrides[k] = v
		}
	}
	// The minimums target the Ollama backend: apply them when the node is
	// an Ollama node or its version was probed against Ollama (legacy-
	// shaped nodes, e.g. kind "remote".)
	ollamaBackend := n.Kind == compute.KindOllama || n.VersionBackend == "ollama"
	caps := []backendVersionLine{}
	if strings.TrimSpace(n.BackendVersion) != "" && ollamaBackend {
		for _, cap := range autonomous.KnownCapabilities() {
			chk, err := autonomous.CheckCapabilityNode(overrides, cap, name, n.BackendVersion)
			if err != nil {
				continue
			}
			line := backendVersionLine{
				Capability: chk.Capability,
				Minimum:    chk.Required,
				OK:         &chk.OK,
				Unknown:    chk.Unknown,
				Reason:     chk.Reason,
			}
			// Unknown version (failed closed): OK is meaningful only for
			// a known version; surface it as null + unknown.
			if chk.Unknown {
				ok := false
				line.OK = &ok
			}
			caps = append(caps, line)
		}
	}
	probed := ""
	if !n.VersionProbedAt.IsZero() {
		probed = n.VersionProbedAt.UTC().Format(time.RFC3339)
	}
	writeJSONOK(w, nodeBackendVersions{
		Name:            name,
		Kind:            string(n.Kind),
		Address:         n.Address,
		Backend:         n.VersionBackend,
		BackendVersion:  n.BackendVersion,
		VersionProbedAt: probed,
		Capabilities:    caps,
	})
}

// ── autonomous.requirements config surface (min-Ollama overrides) ───────────
//
// The requirements config block rides the standard config surface:
//   GET /api/config reads autonomous.requirements as
//   {"min_ollama":{capability:version}} plus the known capability list, and
//   PUT /api/config accepts the whole "autonomous.requirements" object or a
//   per-capability "autonomous.requirements.min_ollama.<capability>" key.
//   These helpers own the render/validate shape; api.go wires the routes.

// requirementsConfigGetSurface renders the autonomous.requirements config
// block: the stored min-Ollama overrides (empty = the in-code floor
// applies) plus the known capability list so every surface renders the same
// fields in the same order.
func requirementsConfigGetSurface(req config.AutonomousRequirementsConfig) map[string]any {
	min := map[string]string{}
	for cap, version := range req.MinOllama {
		version = strings.TrimSpace(version)
		if version == "" {
			continue
		}
		min[cap] = version
	}
	capabilities := autonomous.KnownCapabilities()
	out := make([]string, 0, len(capabilities))
	out = append(out, capabilities...)
	return map[string]any{
		"min_ollama":  min,
		"capabilities": out,
	}
}

// minOllamaMapFromAny reads a JSON "min_ollama" value (an object mapping
// capability → version, or an empty object) into a Go map. It rejects
// non-object values, non-string values and anything the shared validator
// rejects (unknown caps, bad versions) so the two config keys share one
// rule set.
func minOllamaMapFromAny(v any) (map[string]string, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		out := make(map[string]string, len(x))
		for cap, ver := range x {
			s, ok := ver.(string)
			if !ok {
				return nil, fmt.Errorf("autonomous.requirements.min_ollama: value for %q must be a version string", cap)
			}
			out[cap] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("autonomous.requirements.min_ollama: must be an object of capability → version strings")
	}
}

// validateRequirementsConfigBlock validates the whole
// autonomous.requirements PUT value and returns the config block it
// represents. v must be a JSON object whose only key is "min_ollama" (a
// capability→version object); "capabilities" is accepted but ignored, and a
// bare capability→version object is accepted as a convenience for comm
// string values — everything invalid still refuses the whole PUT.
func validateRequirementsConfigBlock(v any) (config.AutonomousRequirementsConfig, error) {
	switch x := v.(type) {
	case map[string]any:
		consolidated := map[string]string{}
		for key, val := range x {
			if key == "capabilities" {
				continue // read-only rendering field; accepted, ignored
			}
			if key == "min_ollama" {
				m, err := minOllamaMapFromAny(val)
				if err != nil {
					return config.AutonomousRequirementsConfig{}, err
				}
				for cap, version := range m {
					consolidated[cap] = version
				}
				continue
			}
			// convenience shape (comm sends strings): a bare capability→version
			s, ok := val.(string)
			if !ok {
				return config.AutonomousRequirementsConfig{},
					fmt.Errorf("autonomous.requirements: value for %q must be a version string", key)
			}
			consolidated[key] = s
		}
		normalized, err := autonomous.NormalizeMinOllamaOverrides(consolidated)
		if err != nil {
			return config.AutonomousRequirementsConfig{}, err
		}
		return config.AutonomousRequirementsConfig{MinOllama: normalized}, nil
	default:
		return config.AutonomousRequirementsConfig{},
			fmt.Errorf("autonomous.requirements: must be an object with a \"min_ollama\" capability → version map")
	}
}

// validateRequirementsMinOllamaKey validates one
// autonomous.requirements.min_ollama.<capability> PUT value: a version
// string (dotted semantic version) or the empty string, which clears the
// override for that capability. It returns the stored value.
func validateRequirementsMinOllamaKey(capability string, v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("autonomous.requirements.min_ollama.%s: must be a version string (the empty string clears the override)", capability)
	}
	version := strings.TrimSpace(s)
	// One shared rule set for every surface: validate a single-entry map.
	if _, err := autonomous.NormalizeMinOllamaOverrides(map[string]string{capability: version}); err != nil {
		return "", err
	}
	return version, nil
}
