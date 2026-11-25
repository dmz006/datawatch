// Supervisor set-up and state-record routes.
//
//	POST   /api/supervisor-setup               idempotent set-up for a named project
//	GET    /api/supervisor-setup/state         read the pinned state record
//	POST   /api/supervisor-setup/state         set an entry (create or replace)
//	PATCH  /api/supervisor-setup/state         change fields of an entry
//	DELETE /api/supervisor-setup/state         remove an entry
//	GET    /api/supervisor-setup/corrections   list recorded corrections
//	POST   /api/supervisor-setup/corrections   record a correction
//	POST   /api/supervisor-setup/corrections/promote  promote a standing rule
//
// Set-up launches nothing itself. It validates the explicit model (Sonnet or
// Haiku only), resolves the project directory, generates the handover and
// makes sure one recurring spawn schedule exists per schedule name, each
// carrying the supervisor launch role. Calling it again for the same project
// and names adds nothing: existing schedules are reported, not duplicated.
// Every time comes from the injected clock.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/pathsafe"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// DefaultSupervisorSetupCron is the cadence of the one schedule set-up adds
// when the request names none: every twenty minutes.
const DefaultSupervisorSetupCron = "*/20 * * * *"

// supervisorSetupRole is the state-record role used when a request names none.
const supervisorSetupRole = supervisorloop.LaunchRoleSupervisor

// SupervisorScheduleEnsurer is the schedule seam: it adds a pending spawn
// schedule under a name only when none exists. *session.ScheduleStore
// satisfies it.
type SupervisorScheduleEnsurer interface {
	EnsureNamedSpawn(opts session.AddSpawnOptions) (*session.ScheduledCommand, bool, error)
}

// SupervisorSetupDeps are the seams the set-up routes use; tests inject fakes.
type SupervisorSetupDeps struct {
	Session     supervisorloop.SessionLayer
	Rules       supervisorloop.RulesSource
	Plans       supervisorloop.PlanSource
	Overrides   supervisorloop.OverridesSource
	Schedules   SupervisorScheduleEnsurer
	Records     *supervisorloop.StateRecords
	Corrections *supervisorloop.Corrections
	Clock       supervisorloop.Clock
}

// SetSupervisorSetup wires the set-up routes. Nil leaves them answering 503.
func (s *Server) SetSupervisorSetup(d *SupervisorSetupDeps) { s.supervisorSetup = d }

// SetSupervisorSetup wires the set-up routes on the HTTP server.
func (s *HTTPServer) SetSupervisorSetup(d *SupervisorSetupDeps) { s.api.SetSupervisorSetup(d) }

// SupervisorSetupSchedule names one recurring spawn schedule.
type SupervisorSetupSchedule struct {
	Name string `json:"name"`
	Cron string `json:"cron,omitempty"`
}

// SupervisorSetupRequest is the POST /api/supervisor-setup body.
type SupervisorSetupRequest struct {
	Project   string                    `json:"project"`
	Model     string                    `json:"model"`
	Schedules []SupervisorSetupSchedule `json:"schedules,omitempty"`
}

// SupervisorSetupScheduleResult reports one schedule after set-up.
type SupervisorSetupScheduleResult struct {
	Name    string `json:"name"`
	ID      string `json:"id"`
	Cron    string `json:"cron"`
	Created bool   `json:"created"`
}

// SupervisorSetupResult is the POST /api/supervisor-setup response.
type SupervisorSetupResult struct {
	Project     string                          `json:"project"`
	LaunchRole  string                          `json:"launch_role"`
	ProjectDir  string                          `json:"project_dir"`
	Model       string                          `json:"model"`
	HandoverAt  string                          `json:"handover_assembled_at"`
	Schedules   []SupervisorSetupScheduleResult `json:"schedules"`
	AllExisting bool                            `json:"already_set_up"`
}

type supervisorStateBody struct {
	Project  string  `json:"project"`
	Role     string  `json:"role"`
	ID       string  `json:"id"`
	Section  string  `json:"section"`
	Text     *string `json:"text"`
	Owner    *string `json:"owner"`
	NextStep *string `json:"next_step"`
	Who      string  `json:"who"`
	Why      string  `json:"why"`
}

type supervisorCorrectionBody struct {
	Project    string `json:"project"`
	Role       string `json:"role"`
	ID         string `json:"id"`
	Text       string `json:"text"`
	Evidence   string `json:"evidence"`
	Class      string `json:"class"`
	Workaround string `json:"workaround"`
	Who        string `json:"who"`
	Why        string `json:"why"`
}

func (s *Server) supervisorSetupDeps(w http.ResponseWriter) *SupervisorSetupDeps {
	if s.supervisorSetup == nil {
		http.Error(w, "supervisor set-up unavailable", http.StatusServiceUnavailable)
		return nil
	}
	return s.supervisorSetup
}

func supervisorSetupBadRequest(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusBadRequest)
}

// supervisorSetupStatus maps a package error to its HTTP status.
func supervisorSetupStatus(err error) int {
	switch {
	case errors.Is(err, supervisorloop.ErrEntryNotFound), errors.Is(err, supervisorloop.ErrCorrectionNotFound):
		return http.StatusNotFound
	case errors.Is(err, supervisorloop.ErrBadEntry), errors.Is(err, supervisorloop.ErrCorrection):
		return http.StatusBadRequest
	case errors.Is(err, supervisorloop.ErrCorrectionClass):
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

func decodeSupervisorBody(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

// handleSupervisorSetup implements POST /api/supervisor-setup.
func (s *Server) handleSupervisorSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	d := s.supervisorSetupDeps(w)
	if d == nil {
		return
	}
	var req SupervisorSetupRequest
	if err := decodeSupervisorBody(r, &req); err != nil {
		supervisorSetupBadRequest(w, err)
		return
	}
	res, err := d.setUp(req)
	if err != nil {
		var be *supervisorSetupBadInput
		if errors.As(err, &be) {
			supervisorSetupBadRequest(w, err)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, res)
}

type supervisorSetupBadInput struct{ msg string }

func (e *supervisorSetupBadInput) Error() string { return e.msg }

func badSetupInput(format string, a ...any) error {
	return &supervisorSetupBadInput{msg: fmt.Sprintf(format, a...)}
}

// setUp is the idempotent set-up body.
func (d *SupervisorSetupDeps) setUp(req SupervisorSetupRequest) (*SupervisorSetupResult, error) {
	project := strings.TrimSpace(req.Project)
	if project == "" {
		return nil, badSetupInput("project is required")
	}
	if err := supervisorloop.ValidateSupervisorModel(req.Model); err != nil {
		return nil, badSetupInput("%v", err)
	}
	if d.Session == nil || d.Rules == nil || d.Plans == nil || d.Schedules == nil || d.Clock == nil {
		return nil, errors.New("supervisor set-up is not fully wired")
	}
	scheds := req.Schedules
	if len(scheds) == 0 {
		scheds = []SupervisorSetupSchedule{{Name: string(supervisorloop.TickWatch), Cron: DefaultSupervisorSetupCron}}
	}
	seen := map[string]bool{}
	for i := range scheds {
		n := strings.TrimSpace(scheds[i].Name)
		if n == "" {
			return nil, badSetupInput("every schedule needs a name")
		}
		// The name becomes part of the handover file name under the project.
		if err := pathsafe.ValidateRecordName(n); err != nil || strings.IndexFunc(n, unicode.IsControl) >= 0 {
			return nil, badSetupInput("schedule name %q must be one plain name: no path separator, \"..\" or control character", n)
		}
		if seen[n] {
			return nil, badSetupInput("schedule name %q is listed twice", n)
		}
		seen[n] = true
		scheds[i].Name = n
		if strings.TrimSpace(scheds[i].Cron) == "" {
			scheds[i].Cron = DefaultSupervisorSetupCron
		}
		if _, err := session.CronNext(scheds[i].Cron, d.Clock.Now()); err != nil {
			return nil, badSetupInput("schedule %q: %v", n, err)
		}
	}
	dir, err := d.Session.ProjectDir(project)
	if err != nil {
		return nil, badSetupInput("project %q: %v", project, err)
	}
	_, assembledAt, err := d.SpawnTask(project, req.Model, "", "")
	if err != nil {
		return nil, err
	}
	out := &SupervisorSetupResult{
		Project: project, LaunchRole: supervisorloop.LaunchRoleSupervisor, ProjectDir: dir,
		Model: req.Model, HandoverAt: assembledAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Schedules: []SupervisorSetupScheduleResult{}, AllExisting: true,
	}
	for _, sc := range scheds {
		full := "supervisor-" + project + "-" + sc.Name
		task, _, err := d.SpawnTask(project, req.Model, sc.Name, full)
		if err != nil {
			return nil, err
		}
		e, created, err := d.Schedules.EnsureNamedSpawn(session.AddSpawnOptions{
			Task:         task,
			ProjectDir:   dir,
			Model:        req.Model,
			SessionName:  full,
			ScheduleName: full,
			CronExpr:     sc.Cron,
			OneShot:      true,
			Ephemeral:    false,
			LaunchRole:   supervisorloop.LaunchRoleSupervisor,
		})
		if err != nil {
			return nil, fmt.Errorf("schedule %q: %w", full, err)
		}
		if created {
			out.AllExisting = false
		}
		out.Schedules = append(out.Schedules, SupervisorSetupScheduleResult{Name: full, ID: e.ID, Cron: e.CronExpr, Created: created})
	}
	return out, nil
}

// SpawnTask assembles the project's handover and returns the task a
// supervisor spawn starts with: the handover itself when it is small, else a
// short bootstrap that names the handover file, which is rewritten here. An
// empty scheduleName only assembles (no file is written, the task is the
// bare handover). The schedule fire path calls it again at each spawn so a
// fresh session reads current state.
func (d *SupervisorSetupDeps) SpawnTask(project, model, pass, scheduleName string) (string, time.Time, error) {
	// The handover carries the explicit model: override the stored settings.
	asm := &supervisorloop.HandoverAssembler{
		Session: d.Session, Rules: d.Rules, Plans: d.Plans, Clock: d.Clock,
		Overrides: modelOverride{base: d.Overrides, model: model},
	}
	ho, err := asm.AssembleHandover(project)
	if err != nil {
		return "", time.Time{}, err
	}
	text := ho.Render()
	if scheduleName == "" {
		return text, ho.AssembledAt, nil
	}
	text += "\nPass: " + pass + "\n"
	task, err := supervisorloop.DeliverablePrompt(ho.Build.ProjectDir, scheduleName, text)
	return task, ho.AssembledAt, err
}

// modelOverride makes the handover show the model set-up was asked for.
type modelOverride struct {
	base  supervisorloop.OverridesSource
	model string
}

func (m modelOverride) SupervisorOverrides(project string) (supervisorloop.SupervisorOverrides, bool, error) {
	var o supervisorloop.SupervisorOverrides
	if m.base != nil {
		got, ok, err := m.base.SupervisorOverrides(project)
		if err != nil {
			return o, false, err
		}
		if ok {
			o = got
		}
	}
	model := m.model
	o.Model = &model
	return o, true, nil
}

func stateRole(role string) string {
	if strings.TrimSpace(role) == "" {
		return supervisorSetupRole
	}
	return strings.TrimSpace(role)
}

// handleSupervisorSetupState implements the /api/supervisor-setup/state routes.
func (s *Server) handleSupervisorSetupState(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
	case http.MethodPost, http.MethodPatch, http.MethodDelete:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
	default:
		w.Header().Set("Allow", "GET, POST, PATCH, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d := s.supervisorSetupDeps(w)
	if d == nil {
		return
	}
	if d.Records == nil {
		http.Error(w, "state records unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodGet {
		rec, err := d.Records.Get(strings.TrimSpace(r.URL.Query().Get("project")), stateRole(r.URL.Query().Get("role")))
		if err != nil {
			http.Error(w, err.Error(), supervisorSetupStatus(err))
			return
		}
		if rec.Entries == nil {
			rec.Entries = []supervisorloop.StateEntry{}
		}
		writeJSONOK(w, rec)
		return
	}
	var b supervisorStateBody
	if err := decodeSupervisorBody(r, &b); err != nil {
		supervisorSetupBadRequest(w, err)
		return
	}
	project, role := strings.TrimSpace(b.Project), stateRole(b.Role)
	var err error
	action := ""
	switch r.Method {
	case http.MethodPost:
		action = "set"
		e := supervisorloop.StateEntry{ID: b.ID, Section: b.Section}
		if b.Text != nil {
			e.Text = *b.Text
		}
		if b.Owner != nil {
			e.Owner = *b.Owner
		}
		if b.NextStep != nil {
			e.NextStep = *b.NextStep
		}
		err = d.Records.SetEntry(project, role, e, b.Who, b.Why)
	case http.MethodPatch:
		action = "change"
		err = d.Records.ChangeEntry(project, role, b.ID, supervisorloop.EntryChange{Text: b.Text, Owner: b.Owner, NextStep: b.NextStep}, b.Who, b.Why)
	case http.MethodDelete:
		action = "remove"
		err = d.Records.RemoveEntry(project, role, b.ID, b.Who, b.Why)
	}
	if err != nil {
		http.Error(w, err.Error(), supervisorSetupStatus(err))
		return
	}
	writeJSONOK(w, map[string]any{"project": project, "role": role, "id": b.ID, "action": action, "ok": true})
}

// handleSupervisorSetupCorrections implements /api/supervisor-setup/corrections.
func (s *Server) handleSupervisorSetupCorrections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
	case http.MethodPost:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d := s.supervisorSetupDeps(w)
	if d == nil {
		return
	}
	if d.Corrections == nil {
		http.Error(w, "corrections unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodGet {
		list, err := d.Corrections.List(strings.TrimSpace(r.URL.Query().Get("project")), stateRole(r.URL.Query().Get("role")))
		if err != nil {
			http.Error(w, err.Error(), supervisorSetupStatus(err))
			return
		}
		if list == nil {
			list = []supervisorloop.Correction{}
		}
		writeJSONOK(w, map[string]any{"corrections": list})
		return
	}
	var b supervisorCorrectionBody
	if err := decodeSupervisorBody(r, &b); err != nil {
		supervisorSetupBadRequest(w, err)
		return
	}
	project, role := strings.TrimSpace(b.Project), stateRole(b.Role)
	c := supervisorloop.Correction{ID: b.ID, Text: b.Text, Evidence: b.Evidence, Class: b.Class, Workaround: b.Workaround}
	if err := d.Corrections.Record(project, role, c, b.Who, b.Why); err != nil {
		http.Error(w, err.Error(), supervisorSetupStatus(err))
		return
	}
	got, _, err := d.Corrections.Get(project, role, b.ID)
	if err != nil {
		http.Error(w, err.Error(), supervisorSetupStatus(err))
		return
	}
	writeJSONOK(w, got)
}

// handleSupervisorSetupPromote implements POST /api/supervisor-setup/corrections/promote.
func (s *Server) handleSupervisorSetupPromote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	d := s.supervisorSetupDeps(w)
	if d == nil {
		return
	}
	if d.Corrections == nil {
		http.Error(w, "corrections unavailable", http.StatusServiceUnavailable)
		return
	}
	var b supervisorCorrectionBody
	if err := decodeSupervisorBody(r, &b); err != nil {
		supervisorSetupBadRequest(w, err)
		return
	}
	got, err := d.Corrections.Promote(strings.TrimSpace(b.Project), stateRole(b.Role), b.ID, b.Who, b.Why)
	if err != nil {
		http.Error(w, err.Error(), supervisorSetupStatus(err))
		return
	}
	writeJSONOK(w, got)
}
