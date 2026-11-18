// Task routing REST surface: the routing table, its profiles, the
// per-Automaton read and the no-write dry run. Every handler calls the same
// engine functions the MCP tools proxy to (internal/autonomous
// task_routing_*.go), so all surfaces answer from the same code.
//
//	GET    /api/autonomous/task-routing                          table + validation
//	PUT    /api/autonomous/task-routing                          validate and replace the table
//	GET    /api/autonomous/task-routing/profiles                 profile names + the active one
//	POST   /api/autonomous/task-routing/profiles                 save a profile ({name, rows?})
//	PUT    /api/autonomous/task-routing/active-profile           switch the active profile ({name})
//	DELETE /api/autonomous/task-routing/profiles/{name}          delete a profile
//	GET    /api/autonomous/task-routing/profiles/{name}/export   one profile as a JSON download
//	POST   /api/autonomous/task-routing/profiles/import          import an exported profile
//	GET    /api/autonomous/prds/{id}/task-routing                which row decided each task's LLM
//	POST   /api/autonomous/prds/{id}/task-routing/dry-run        what a profile would assign; writes nothing
//
// Reads take CapAutonomousRead and writes CapAutonomousWrite, like the
// planning routing routes. Changing the table only affects tasks created
// afterwards; no session is ever touched.

package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// taskRoutingSetter is the optional AutonomousAPI extension that replaces
// the task_routing block outright (autonomous.API implements it).
type taskRoutingSetter interface {
	SetTaskRouting(config.TaskRoutingConfig) error
}

// taskRoutingReader is the optional AutonomousAPI extension for the
// per-Automaton read and the dry run (autonomous.API implements it).
type taskRoutingReader interface {
	TaskRoutingForAutomaton(prdID string) (any, error)
	TaskRoutingDryRun(prdID, profile string) (any, error)
}

const taskRoutingImportMaxBytes = 1 << 20

// taskRoutingView is the GET/PUT body: the table plus its validation.
type taskRoutingView struct {
	Enabled       bool                                 `json:"enabled"`
	ActiveProfile string                               `json:"active_profile"`
	Profiles      map[string]config.TaskRoutingProfile `json:"profiles"`
	TaskTypes     []string                             `json:"task_types"`
	Validation    taskRoutingValidation                `json:"validation"`
}

type taskRoutingValidation struct {
	Valid    bool     `json:"valid"`
	Problems []string `json:"problems,omitempty"`
}

func taskRoutingProblems(errs []error) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	return out
}

func (s *Server) taskRoutingViewOf(cfg config.TaskRoutingConfig) taskRoutingView {
	cfg = config.TaskRoutingWithDefaults(cfg)
	v := taskRoutingView{
		Enabled: cfg.Enabled, ActiveProfile: cfg.ActiveProfile, Profiles: cfg.Profiles,
		TaskTypes: config.TaskRoutingTaskTypes(),
	}
	if v.Profiles == nil {
		v.Profiles = map[string]config.TaskRoutingProfile{}
	}
	if errs := config.ValidateTaskRouting(cfg, s.taskRoutingChecks()); len(errs) > 0 {
		v.Validation = taskRoutingValidation{Problems: taskRoutingProblems(errs)}
	} else {
		v.Validation = taskRoutingValidation{Valid: true}
	}
	return v
}

// taskRoutingCurrent reads the block from the live manager config.
func (s *Server) taskRoutingCurrent() (config.TaskRoutingConfig, error) {
	raw, err := json.Marshal(s.autonomousMgr.Config())
	if err != nil {
		return config.TaskRoutingConfig{}, err
	}
	var holder struct {
		TaskRouting config.TaskRoutingConfig `json:"task_routing"`
	}
	if err := json.Unmarshal(raw, &holder); err != nil {
		return config.TaskRoutingConfig{}, err
	}
	return config.TaskRoutingWithDefaults(holder.TaskRouting).Clone(), nil
}

func writeTaskRoutingError(w http.ResponseWriter, code int, msg string, problems []string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	body := map[string]any{"error": msg}
	if len(problems) > 0 {
		body["problems"] = problems
	}
	_ = json.NewEncoder(w).Encode(body)
}

// commitTaskRouting validates and applies a whole table, then persists it.
// It answers 400 listing every problem and changes nothing when the table
// is not valid. It reports whether the table was applied.
func (s *Server) commitTaskRouting(w http.ResponseWriter, r *http.Request, in config.TaskRoutingConfig, auditAction string) bool {
	in = config.TaskRoutingWithDefaults(in)
	if errs := config.ValidateTaskRouting(in, s.taskRoutingChecks()); len(errs) > 0 {
		writeTaskRoutingError(w, http.StatusBadRequest, "the routing table is not valid; nothing was changed", taskRoutingProblems(errs))
		return false
	}
	if setter, ok := s.autonomousMgr.(taskRoutingSetter); ok {
		if err := setter.SetTaskRouting(in); err != nil {
			writeTaskRoutingError(w, http.StatusBadRequest, err.Error(), nil)
			return false
		}
	} else {
		b, _ := json.Marshal(map[string]any{"task_routing": in})
		if err := s.autonomousMgr.SetConfig(json.RawMessage(b)); err != nil {
			writeTaskRoutingError(w, http.StatusBadRequest, err.Error(), nil)
			return false
		}
	}
	if s.cfg != nil {
		s.cfg.Autonomous.TaskRouting = in.Clone()
		if s.cfgPath != "" {
			if err := s.saveConfig(); err != nil {
				log.Printf("[task-routing] WARNING: applied live but YAML save failed, will not survive a restart: %v", err)
			}
		}
	}
	s.audit(r.Context(), auditAction, "autonomous_task_routing", "", nil)
	return true
}

func (s *Server) taskRoutingGate(w http.ResponseWriter, r *http.Request, write bool) bool {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return false
	}
	capName := federation.CapAutonomousRead
	if write {
		capName = federation.CapAutonomousWrite
	}
	return s.fedCap(w, r, capName)
}

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// handleTaskRouting serves GET and PUT on the table.
func (s *Server) handleTaskRouting(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.taskRoutingGate(w, r, false) {
			return
		}
		cur, err := s.taskRoutingCurrent()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, s.taskRoutingViewOf(cur))
	case http.MethodPut:
		if !s.taskRoutingGate(w, r, true) {
			return
		}
		var in config.TaskRoutingConfig
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			writeTaskRoutingError(w, http.StatusBadRequest, "bad request: "+err.Error(), nil)
			return
		}
		if !s.commitTaskRouting(w, r, in, "update") {
			return
		}
		cur, _ := s.taskRoutingCurrent()
		writeJSONOK(w, s.taskRoutingViewOf(cur))
	default:
		methodNotAllowed(w)
	}
}

// handleTaskRoutingProfiles serves GET (list) and POST (save) on the profiles.
func (s *Server) handleTaskRoutingProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.taskRoutingGate(w, r, false) {
			return
		}
		cur, err := s.taskRoutingCurrent()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]any{
			"active_profile": cur.ActiveProfile,
			"profiles":       autonomous.ListProfiles(cur),
		})
	case http.MethodPost:
		if !s.taskRoutingGate(w, r, true) {
			return
		}
		var body struct {
			Name string                    `json:"name"`
			Rows config.TaskRoutingProfile `json:"rows"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			writeTaskRoutingError(w, http.StatusBadRequest, "bad request: "+err.Error(), nil)
			return
		}
		cur, err := s.taskRoutingCurrent()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		next, err := autonomous.SaveProfile(cur, body.Name, body.Rows)
		if err != nil {
			writeTaskRoutingError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		if !s.commitTaskRouting(w, r, next, "save_profile") {
			return
		}
		writeJSONOK(w, map[string]any{"saved": strings.TrimSpace(body.Name), "profiles": autonomous.ListProfiles(next)})
	default:
		methodNotAllowed(w)
	}
}

// handleTaskRoutingActiveProfile serves PUT: switch the active profile.
func (s *Server) handleTaskRoutingActiveProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	if !s.taskRoutingGate(w, r, true) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeTaskRoutingError(w, http.StatusBadRequest, "bad request: "+err.Error(), nil)
		return
	}
	cur, err := s.taskRoutingCurrent()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	next, err := autonomous.SetActiveProfile(cur, body.Name)
	if err != nil {
		writeTaskRoutingError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if !s.commitTaskRouting(w, r, next, "activate_profile") {
		return
	}
	writeJSONOK(w, map[string]any{"active_profile": next.ActiveProfile})
}

// handleTaskRoutingProfile serves DELETE on one profile.
func (s *Server) handleTaskRoutingProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	if !s.taskRoutingGate(w, r, true) {
		return
	}
	name := r.PathValue("name")
	cur, err := s.taskRoutingCurrent()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	next, err := autonomous.DeleteProfile(cur, name)
	if err != nil {
		code := http.StatusBadRequest
		if _, ok := cur.Profiles[name]; !ok {
			code = http.StatusNotFound
		}
		writeTaskRoutingError(w, code, err.Error(), nil)
		return
	}
	if !s.commitTaskRouting(w, r, next, "delete_profile") {
		return
	}
	writeJSONOK(w, map[string]any{"deleted": name, "profiles": autonomous.ListProfiles(next)})
}

var taskRoutingFileUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// handleTaskRoutingProfileExport serves GET: one profile as a JSON download.
func (s *Server) handleTaskRoutingProfileExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.taskRoutingGate(w, r, false) {
		return
	}
	name := r.PathValue("name")
	cur, err := s.taskRoutingCurrent()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	doc, err := autonomous.ExportProfile(cur, name)
	if err != nil {
		writeTaskRoutingError(w, http.StatusNotFound, err.Error(), nil)
		return
	}
	file := taskRoutingFileUnsafe.ReplaceAllString(name, "_")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="task-routing-%s.json"`, file))
	_, _ = w.Write(doc)
}

// handleTaskRoutingProfileImport serves POST: import an exported profile.
// Query: name (optional, defaults to the document's name), overwrite=true.
func (s *Server) handleTaskRoutingProfileImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.taskRoutingGate(w, r, true) {
		return
	}
	doc, err := io.ReadAll(io.LimitReader(r.Body, taskRoutingImportMaxBytes+1))
	if err != nil {
		writeTaskRoutingError(w, http.StatusBadRequest, "bad request: "+err.Error(), nil)
		return
	}
	if len(doc) > taskRoutingImportMaxBytes {
		writeTaskRoutingError(w, http.StatusRequestEntityTooLarge, "the profile file is too large", nil)
		return
	}
	cur, err := s.taskRoutingCurrent()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	overwrite := r.URL.Query().Get("overwrite") == "true" || r.URL.Query().Get("overwrite") == "1"
	next, err := autonomous.ImportProfile(cur, doc, name, overwrite, s.taskRoutingChecks())
	if err != nil {
		var problems []string
		msg := err.Error()
		if strings.Contains(msg, "; ") {
			problems = strings.Split(msg, "; ")
		}
		writeTaskRoutingError(w, http.StatusBadRequest, msg, problems)
		return
	}
	if !s.commitTaskRouting(w, r, next, "import_profile") {
		return
	}
	writeJSONOK(w, map[string]any{"profiles": autonomous.ListProfiles(next)})
}

// handleAutomatonTaskRouting serves GET on /prds/{id}/task-routing (which
// row decided each task's LLM) and POST on /prds/{id}/task-routing/dry-run
// (what a profile would assign; nothing is written).
func (s *Server) handleAutomatonTaskRouting(w http.ResponseWriter, r *http.Request) {
	dry := strings.HasSuffix(r.URL.Path, "/dry-run")
	if dry && r.Method != http.MethodPost || !dry && r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	// The dry run only reads, so both need the read capability.
	if !s.taskRoutingGate(w, r, false) {
		return
	}
	rd, ok := s.autonomousMgr.(taskRoutingReader)
	if !ok {
		http.Error(w, "task routing is not available", http.StatusNotImplemented)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing Automaton id", http.StatusBadRequest)
		return
	}
	var (
		out any
		err error
	)
	if dry {
		var body struct {
			Profile string `json:"profile"`
		}
		if r.Body != nil {
			b, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
			if len(strings.TrimSpace(string(b))) > 0 {
				if jerr := json.Unmarshal(b, &body); jerr != nil {
					writeTaskRoutingError(w, http.StatusBadRequest, "bad request: "+jerr.Error(), nil)
					return
				}
			}
		}
		if body.Profile == "" {
			body.Profile = r.URL.Query().Get("profile")
		}
		out, err = rd.TaskRoutingDryRun(id, body.Profile)
	} else {
		out, err = rd.TaskRoutingForAutomaton(id)
	}
	if err != nil {
		code := http.StatusBadRequest
		msg := err.Error()
		if strings.Contains(msg, "not found") {
			code = http.StatusNotFound
			msg = "Automaton not found: " + id
		}
		writeTaskRoutingError(w, code, msg, nil)
		return
	}
	if out == nil {
		out = []any{}
	}
	key := "tasks"
	if dry {
		key = "entries"
	}
	writeJSONOK(w, map[string]any{"automaton_id": id, key: out})
}
