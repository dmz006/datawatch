// Merge pipeline surfaces:
//
//	GET    /api/autonomous/merge-pipeline?project=NAME   the pipeline in effect
//	PUT    /api/autonomous/merge-pipeline/record         set a project's record
//	DELETE /api/autonomous/merge-pipeline/record         remove a project's record
//	POST   /api/autonomous/merge-pipeline/validate       check a pipeline, saving nothing
//
// and the autonomous.merge_pipeline block on GET /api/config and PUT
// /api/config.
//
// The read answers which pipeline would run for a project and why: the
// definition, the source it came from (the project's own file, the record the
// daemon keeps, or the built-in pipeline), the conflict policy and model, and
// the per-project overrides. Reads need the autonomous read capability, writes
// the autonomous write capability. PUT /api/config checks every key of the
// block before anything is applied, so a bad value refuses the whole PUT.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/mergepipeline"
)

// mergePipelineKeyPrefix is the config path every autonomous.merge_pipeline.*
// key shares.
const mergePipelineKeyPrefix = "autonomous.merge_pipeline."

// mergePipelineProjectsPrefix starts the per-project override keys:
// autonomous.merge_pipeline.projects.<project>.<key>.
const mergePipelineProjectsPrefix = mergePipelineKeyPrefix + "projects."

// The keys of the block, global and per project.
const (
	mergePipelineFieldFileName          = "file_name"
	mergePipelineFieldSourceOrder       = "source_order"
	mergePipelineFieldFallbackOnBadFile = "fallback_on_bad_file"
	mergePipelineFieldConflictPolicy    = "conflict_policy"
	mergePipelineFieldConflictModel     = "conflict_model"
	mergePipelineFieldVersionFiles      = "version_files"
)

// mergePipelineFields lists the keys in a fixed order, so the first bad value
// reported does not depend on map order.
var mergePipelineFields = []string{
	mergePipelineFieldFileName, mergePipelineFieldSourceOrder, mergePipelineFieldFallbackOnBadFile,
	mergePipelineFieldConflictPolicy, mergePipelineFieldConflictModel, mergePipelineFieldVersionFiles,
}

// The post-ship hook keys. The post_ship block is global: the settings keep
// one of them for all projects, so unlike the keys above they have no
// projects.<project>. form.
const (
	mergePipelineFieldPostShipEnabled             = "post_ship.enabled"
	mergePipelineFieldPostShipRegistryFile        = "post_ship.registry_file"
	mergePipelineFieldPostShipRegistryRowPattern  = "post_ship.registry_row_pattern"
	mergePipelineFieldPostShipCompanionRepository = "post_ship.companion_repository"
	mergePipelineFieldPostShipIssueTitleTemplate  = "post_ship.issue_title_template"
	mergePipelineFieldPostShipNotifyTemplate      = "post_ship.notify_template"
	mergePipelineFieldPostShipHooks               = "post_ship.hooks"
)

// mergePipelinePostShipFields lists the post-ship keys in a fixed order, for
// the error messages and MergePipelineConfigureKeys.
var mergePipelinePostShipFields = []string{
	mergePipelineFieldPostShipEnabled, mergePipelineFieldPostShipRegistryFile,
	mergePipelineFieldPostShipRegistryRowPattern, mergePipelineFieldPostShipCompanionRepository,
	mergePipelineFieldPostShipIssueTitleTemplate, mergePipelineFieldPostShipNotifyTemplate,
	mergePipelineFieldPostShipHooks,
}

func isMergePipelinePostShipField(field string) bool {
	for _, f := range mergePipelinePostShipFields {
		if field == f {
			return true
		}
	}
	return false
}

// Bounds on what the routes and PUT /api/config accept.
const (
	mergePipelineMaxBodyBytes = 1 << 20
	mergePipelineMaxListItems = 100
	mergePipelineMaxNameBytes = 200
)

// mergePipelineRecordMu serialises writes to the record store file.
var mergePipelineRecordMu sync.Mutex

// mergePipelineView is the body of GET /api/autonomous/merge-pipeline.
type mergePipelineView struct {
	Project string `json:"project"`
	// Definition is the pipeline in effect; null when none could be chosen
	// (the file is unusable and the setting allows no substitute).
	Definition *mergepipeline.Definition `json:"definition"`
	// Source is "file", "record" or "default".
	Source string `json:"source"`
	// Reason says in plain words why that source was chosen.
	Reason string `json:"reason"`
	// FileError is the problem found with the project's file, if any, even
	// when another source stands in for it.
	FileError string `json:"file_error,omitempty"`
	// Settings are the values in effect for the project: the project's
	// override, else the global value, else the default.
	Settings mergePipelineSettingsView `json:"settings"`
	// ConflictPolicy is the policy configured; ConflictPolicyInEffect is the
	// one acted on, which falls back to stop_and_ask when a model is needed
	// and is not available (ConflictDegradedReason says why).
	ConflictPolicy         string `json:"conflict_policy"`
	ConflictPolicyInEffect string `json:"conflict_policy_in_effect"`
	// ConflictModel is the model configured; ConflictModelInEffect is the one
	// that would be used, empty when none is.
	ConflictModel          string `json:"conflict_model"`
	ConflictModelInEffect  string `json:"conflict_model_in_effect"`
	ConflictDegradedReason string `json:"conflict_degraded_reason,omitempty"`
	// Overrides are the per-project values set for this project; null when
	// the project has none.
	Overrides *config.MergePipelineOverride `json:"overrides"`
	// Record is when the daemon's record for the project was last set; empty
	// when there is none.
	RecordUpdatedAt string `json:"record_updated_at,omitempty"`
	// PostShip is the post-ship hook state that applies to the project
	// regardless of the source: the block is global, so the same for every
	// project: on or off, how many hooks are defined, and which would run.
	PostShip mergePipelinePostShipView `json:"post_ship"`
}

// mergePipelineSettingsView is the resolution and version settings in effect.
type mergePipelineSettingsView struct {
	FileName          string   `json:"file_name"`
	SourceOrder       []string `json:"source_order"`
	FallbackOnBadFile string   `json:"fallback_on_bad_file"`
	VersionFiles      []string `json:"version_files"`
}

// mergePipelinePostShipView is the post_ship state in the view: whether the
// block is on, how many hooks are defined, and the names of the ones that
// would run.
type mergePipelinePostShipView struct {
	Enabled      bool     `json:"enabled"`
	HookCount    int      `json:"hook_count"`
	EnabledHooks []string `json:"enabled_hooks"`
}

// postShipView is the post_ship block seen from the view.
func postShipView(p config.PostShipConfig) mergePipelinePostShipView {
	enabled := make([]string, 0, len(p.Hooks))
	for _, h := range p.Hooks {
		if h.Enabled {
			enabled = append(enabled, h.Name)
		}
	}
	return mergePipelinePostShipView{Enabled: p.Enabled, HookCount: len(p.Hooks), EnabledHooks: enabled}
}

// mergePipelineRecordBody is the body of PUT .../record and POST .../validate.
// A pipeline is given as Definition (steps as JSON) or as YAML text, not both.
type mergePipelineRecordBody struct {
	Project    string                    `json:"project"`
	Definition *mergepipeline.Definition `json:"definition"`
	YAML       string                    `json:"yaml"`
	// File asks validate to check the project's repository file instead of a
	// body: true checks the configured file name, a string names the file.
	File json.RawMessage `json:"file"`
}

// mergePipelineValidation is the body of POST .../validate.
type mergePipelineValidation struct {
	Valid      bool                      `json:"valid"`
	Errors     []string                  `json:"errors"`
	Definition *mergepipeline.Definition `json:"definition,omitempty"`
	// Source says what was checked: "body" or "file".
	Source string `json:"source"`
	File   string `json:"file,omitempty"`
}

// mergePipelineProject returns the project a request is about: the project
// query value, else name, else the base name of the merge base repo.
func (s *Server) mergePipelineProject(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		if mr, ok := s.autonomousMgr.(mergeRunner); ok && s.autonomousMgr != nil {
			if repo := mr.MergeBaseRepo(); repo != "" {
				name = filepath.Base(repo)
			}
		}
	}
	if name == "" {
		return "", errors.New("project is required")
	}
	if len(name) > mergePipelineMaxNameBytes {
		return "", fmt.Errorf("project is longer than %d bytes", mergePipelineMaxNameBytes)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("project must not contain control characters")
		}
	}
	return name, nil
}

// mergePipelineProjectDir finds the repository directory of a project: the
// directory registered for it, else the merge base repo when the name is the
// base name of that repo.
func (s *Server) mergePipelineProjectDir(project string) (string, bool) {
	if s.cfg != nil {
		if e, ok := s.cfg.Projects[project]; ok && e.Dir != "" {
			return e.Dir, true
		}
	}
	if s.autonomousMgr != nil {
		if mr, ok := s.autonomousMgr.(mergeRunner); ok {
			if repo := mr.MergeBaseRepo(); repo != "" && filepath.Base(repo) == project {
				return repo, true
			}
		}
	}
	return "", false
}

// mergePipelineSettingsFor builds the resolution rules for a project from the
// operator's settings.
func mergePipelineSettingsFor(cfg config.MergePipelineConfig, project string) mergepipeline.Settings {
	return mergepipeline.Settings{
		FileName:          cfg.EffectiveFileName(project),
		SourceOrder:       cfg.EffectiveSourceOrder(project),
		FallbackOnBadFile: cfg.EffectiveFallbackOnBadFile(project),
	}
}

// mergePipelineRecords opens the daemon's record store.
func mergePipelineRecords() (*mergepipeline.RecordStore, error) {
	return mergepipeline.NewRecordStore(mergeDataDir())
}

// mergePipelineGate runs the checks every route shares: the method, the
// capability and the config. It reports whether the request may go on.
func (s *Server) mergePipelineGate(w http.ResponseWriter, r *http.Request, methods map[string]string) bool {
	capName, ok := methods[r.Method]
	if !ok {
		allow := make([]string, 0, len(methods))
		for m := range methods {
			allow = append(allow, m)
		}
		sort.Strings(allow)
		w.Header().Set("Allow", strings.Join(allow, ", "))
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if !s.fedCap(w, r, capName) {
		return false
	}
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// handleMergePipeline serves GET /api/autonomous/merge-pipeline.
func (s *Server) handleMergePipeline(w http.ResponseWriter, r *http.Request) {
	if !s.mergePipelineGate(w, r, map[string]string{http.MethodGet: federation.CapAutonomousRead}) {
		return
	}
	project, err := s.mergePipelineProject(r.URL.Query().Get("project"))
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	cfg := s.mergePipelineConfig()
	records, err := mergePipelineRecords()
	if err != nil {
		http.Error(w, "merge pipeline records unavailable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	resolver := &mergepipeline.Resolver{
		Records:    records,
		ProjectDir: func(p string) (string, bool) { return s.mergePipelineProjectDir(p) },
		Settings:   func(p string) mergepipeline.Settings { return mergePipelineSettingsFor(cfg, p) },
	}
	res, _ := resolver.Resolve(project) // an error here is reported through FileError and a null definition

	view := mergePipelineView{
		Project: project,
		Source:  string(res.Source),
		Reason:  res.Reason,
		Settings: mergePipelineSettingsView{
			FileName:          cfg.EffectiveFileName(project),
			SourceOrder:       cfg.EffectiveSourceOrder(project),
			FallbackOnBadFile: cfg.EffectiveFallbackOnBadFile(project),
			VersionFiles:      nonNilStrings(cfg.EffectiveVersionFiles(project)),
		},
		ConflictPolicy: cfg.EffectiveConflictPolicy(project),
		ConflictModel:  cfg.EffectiveConflictModel(project),
	}
	if len(res.Definition.Steps) > 0 {
		def := res.Definition
		view.Definition = &def
	}
	view.PostShip = postShipView(cfg.PostShip)
	if res.FileError != nil {
		view.FileError = res.FileError.Error()
	}
	if o, ok := cfg.Projects[project]; ok {
		view.Overrides = &o
	}
	var lookup config.LLMLookup
	if s.inferenceReg != nil {
		lookup = s.inferenceReg
	}
	view.ConflictPolicyInEffect, view.ConflictModelInEffect, view.ConflictDegradedReason =
		cfg.ResolveConflictPolicy(project, lookup)
	if res.Source == mergepipeline.SourceRecord {
		if rec, err := records.Get(project); err == nil {
			view.RecordUpdatedAt = rec.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
		}
	}
	writeJSONOK(w, view)
}

// definitionFromBody reads the pipeline a request body carries. It returns an
// error that is the plain-language problem when the body has none, has both,
// or the pipeline does not validate.
func definitionFromBody(b mergePipelineRecordBody) (mergepipeline.Definition, error) {
	hasDef := b.Definition != nil
	hasYAML := strings.TrimSpace(b.YAML) != ""
	switch {
	case hasDef && hasYAML:
		return mergepipeline.Definition{}, errors.New("give the pipeline as definition or as yaml, not both")
	case hasYAML:
		return mergepipeline.Parse([]byte(b.YAML), "request body")
	case hasDef:
		if err := b.Definition.Validate(); err != nil {
			return mergepipeline.Definition{}, err
		}
		return *b.Definition, nil
	}
	return mergepipeline.Definition{}, errors.New("a pipeline is required: give definition or yaml")
}

// decodeMergePipelineBody reads a JSON request body of bounded size.
func decodeMergePipelineBody(w http.ResponseWriter, r *http.Request) (mergePipelineRecordBody, bool) {
	var b mergePipelineRecordBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, mergePipelineMaxBodyBytes))
	if err := dec.Decode(&b); err != nil {
		http.Error(w, "bad request: invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return b, false
	}
	return b, true
}

// handleMergePipelineRecord serves PUT and DELETE
// /api/autonomous/merge-pipeline/record.
func (s *Server) handleMergePipelineRecord(w http.ResponseWriter, r *http.Request) {
	if !s.mergePipelineGate(w, r, map[string]string{
		http.MethodPut: federation.CapAutonomousWrite, http.MethodDelete: federation.CapAutonomousWrite,
	}) {
		return
	}
	if r.Method == http.MethodDelete {
		project, err := s.mergePipelineProject(r.URL.Query().Get("project"))
		if err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		mergePipelineRecordMu.Lock()
		defer mergePipelineRecordMu.Unlock()
		records, err := mergePipelineRecords()
		if err != nil {
			http.Error(w, "merge pipeline records unavailable: "+err.Error(), http.StatusInternalServerError)
			return
		}
		switch err := records.Delete(project); {
		case errors.Is(err, mergepipeline.ErrRecordNotFound):
			http.Error(w, "no merge pipeline record for project "+project, http.StatusNotFound)
		case err != nil:
			http.Error(w, "could not remove the record: "+err.Error(), http.StatusInternalServerError)
		default:
			writeJSONOK(w, map[string]string{"status": "deleted", "project": project})
		}
		return
	}

	body, ok := decodeMergePipelineBody(w, r)
	if !ok {
		return
	}
	name := r.URL.Query().Get("project")
	if strings.TrimSpace(name) == "" {
		name = body.Project
	}
	project, err := s.mergePipelineProject(name)
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	def, err := definitionFromBody(body)
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	mergePipelineRecordMu.Lock()
	defer mergePipelineRecordMu.Unlock()
	records, err := mergePipelineRecords()
	if err != nil {
		http.Error(w, "merge pipeline records unavailable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	rec, err := records.Set(project, def)
	if err != nil {
		var verr *mergepipeline.ValidationError
		if errors.As(err, &verr) {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "could not save the record: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, rec)
}

// handleMergePipelineValidate serves POST
// /api/autonomous/merge-pipeline/validate. A pipeline that does not validate
// is a normal answer ("valid": false with every problem), not an error status;
// only a request that cannot be read is one.
func (s *Server) handleMergePipelineValidate(w http.ResponseWriter, r *http.Request) {
	if !s.mergePipelineGate(w, r, map[string]string{http.MethodPost: federation.CapAutonomousRead}) {
		return
	}
	body, ok := decodeMergePipelineBody(w, r)
	if !ok {
		return
	}
	hasFile := len(body.File) > 0 && string(body.File) != "null" && string(body.File) != "false"
	hasBody := body.Definition != nil || strings.TrimSpace(body.YAML) != ""
	if hasFile && hasBody {
		http.Error(w, "bad request: check a body or a repository file, not both", http.StatusBadRequest)
		return
	}
	if !hasFile {
		def, err := definitionFromBody(body)
		out := mergePipelineValidation{Source: "body", Errors: []string{}}
		if err != nil {
			if !hasBody {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			out.Errors = validationProblems(err)
		} else {
			out.Valid, out.Definition = true, &def
		}
		writeJSONOK(w, out)
		return
	}

	project, err := s.mergePipelineProject(body.Project)
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	fileName := s.mergePipelineConfig().EffectiveFileName(project)
	if string(body.File) != "true" {
		var named string
		if err := json.Unmarshal(body.File, &named); err != nil {
			http.Error(w, `bad request: file must be true or a file name inside the project`, http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(named) == "" {
			http.Error(w, "bad request: file is empty", http.StatusBadRequest)
			return
		}
		if err := mergePipelineCheckFileName(named); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		fileName = named
	}
	dir, found := s.mergePipelineProjectDir(project)
	if !found {
		http.Error(w, "project not found: "+project+" has no directory on this daemon", http.StatusNotFound)
		return
	}
	out := mergePipelineValidation{Source: "file", File: fileName, Errors: []string{}}
	def, err := mergepipeline.LoadFile(dir, fileName, 0)
	if err != nil {
		out.Errors = validationProblems(err)
	} else {
		out.Valid, out.Definition = true, &def
	}
	writeJSONOK(w, out)
}

// mergePipelineCheckFileName refuses a file name that leaves the project.
func mergePipelineCheckFileName(name string) error {
	var c config.MergePipelineConfig
	c.FileName = name
	return c.Validate()
}

// validationProblems lists the problems an error carries, one entry each.
func validationProblems(err error) []string {
	var verr *mergepipeline.ValidationError
	if errors.As(err, &verr) {
		return append([]string{}, verr.Problems...)
	}
	var mal *mergepipeline.ErrMalformed
	if errors.As(err, &mal) {
		if errors.As(mal.Err, &verr) {
			return append([]string{}, verr.Problems...)
		}
	}
	return []string{err.Error()}
}

// ---- config surface and checked write --------------------------------------

// mergePipelineConfigSurface is the autonomous.merge_pipeline block GET
// /api/config serves: the values in effect, so an unset key shows its default,
// and the per-project overrides as set.
func mergePipelineConfigSurface(c config.MergePipelineConfig) map[string]any {
	files := c.EffectiveVersionFiles("")
	if files == nil {
		files = []string{} // an empty list reads as [], not null
	}
	projects := c.Projects
	if projects == nil {
		projects = map[string]config.MergePipelineOverride{}
	}
	return map[string]any{
		mergePipelineFieldFileName:          c.EffectiveFileName(""),
		mergePipelineFieldSourceOrder:       c.EffectiveSourceOrder(""),
		mergePipelineFieldFallbackOnBadFile: c.EffectiveFallbackOnBadFile(""),
		mergePipelineFieldConflictPolicy:    c.EffectiveConflictPolicy(""),
		mergePipelineFieldConflictModel:     c.EffectiveConflictModel(""),
		mergePipelineFieldVersionFiles:      files,
		"post_ship": map[string]any{
			"enabled":              c.PostShip.Enabled,
			"hooks":                c.PostShip.Hooks,
			"registry_file":        c.PostShip.RegistryFile,
			"registry_row_pattern": c.PostShip.RegistryRowPattern,
			"companion_repository": c.PostShip.CompanionRepository,
			"issue_title_template": c.PostShip.IssueTitleTemplate,
			"notify_template":      c.PostShip.NotifyTemplate,
		},
		"projects": projects,
	}
}

// mergePipelineKey is one parsed autonomous.merge_pipeline.* key.
type mergePipelineKey struct {
	project string // empty for a global key
	field   string // empty when the whole project override is the target
}

// parseMergePipelineKey splits key. known is false when key is outside the
// block; err is set when it is inside the block but not a key of it.
func parseMergePipelineKey(key string) (pk mergePipelineKey, known bool, err error) {
	if !strings.HasPrefix(key, mergePipelineKeyPrefix) {
		return pk, false, nil
	}
	if strings.HasPrefix(key, mergePipelineProjectsPrefix) {
		rest := strings.TrimPrefix(key, mergePipelineProjectsPrefix)
		for _, f := range mergePipelineFields {
			if strings.HasSuffix(rest, "."+f) {
				name := strings.TrimSuffix(rest, "."+f)
				if strings.TrimSpace(name) == "" {
					return pk, true, fmt.Errorf("%s: the project name is empty", key)
				}
				return mergePipelineKey{project: name, field: f}, true, nil
			}
		}
		if strings.Contains(rest, ".post_ship.") || strings.Contains(rest, ".projects.post_ship") {
			return pk, true, fmt.Errorf("%s: the post_ship keys have no per-project form; the block is global, so set one of %s", key, strings.Join(mergePipelinePostShipFields, ", "))
		}
		if strings.TrimSpace(rest) == "" {
			return pk, true, fmt.Errorf("%s: the project name is empty", key)
		}
		return mergePipelineKey{project: rest}, true, nil
	}
	f := strings.TrimPrefix(key, mergePipelineKeyPrefix)
	for _, known := range mergePipelinePostShipFields {
		if f == known {
			return mergePipelineKey{field: f}, true, nil
		}
	}
	for _, known := range mergePipelineFields {
		if f == known {
			return mergePipelineKey{field: f}, true, nil
		}
	}
	return pk, true, fmt.Errorf("%s: unknown key; the keys are %s, %s, and projects.<project>.<key> for one project",
		key, strings.Join(mergePipelineFields, ", "), strings.Join(mergePipelinePostShipFields, ", "))
}

// validateMergePipelinePatch checks the autonomous.merge_pipeline keys of a PUT
// /api/config body before anything is applied. A key that is absent is not
// checked. The error names the key and the valid values.
func validateMergePipelinePatch(patch map[string]any) error {
	return validateMergePipelinePatchBase(config.MergePipelineConfig{}, patch)
}

// validateMergePipelinePatchBase checks the patch against the block it applies
// on top of. It refuses the whole patch if any key is bad, naming the key; an
// enabled built-in hook is refused by name only when its target is unset in
// the applied block, so a hook whose target the block already holds is
// accepted, and a single PUT can set the target and the hook together.
func validateMergePipelinePatchBase(base config.MergePipelineConfig, patch map[string]any) error {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		if strings.HasPrefix(k, mergePipelineKeyPrefix) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	scratch := base.Clone()
	for _, k := range keys {
		if _, err := applyMergePipelineKey(&scratch, k, patch[k]); err != nil {
			return err
		}
	}
	return scratch.Validate()
}

// applyMergePipelinePatch writes one merge pipeline key into the daemon
// config. It reports whether key belongs to the block. The value was checked
// by validateMergePipelinePatch before the PUT reached this point, so a value
// that does not parse here is left unchanged.
func applyMergePipelinePatch(cfg *config.Config, key string, v any) bool {
	known, _ := applyMergePipelineKey(&cfg.Autonomous.MergePipeline, key, v)
	return known
}

// applyMergePipelineKey parses one key's value and stores it in c. known
// reports whether key is in the block; when err is not nil, c is unchanged.
func applyMergePipelineKey(c *config.MergePipelineConfig, key string, v any) (known bool, err error) {
	pk, known, err := parseMergePipelineKey(key)
	if !known || err != nil {
		return known, err
	}
	if pk.project != "" && pk.field == "" {
		if v != nil {
			return true, fmt.Errorf("%s: only null is accepted here, which removes the project's overrides; set one key at a time with %s<project>.<key>",
				key, mergePipelineProjectsPrefix)
		}
		delete(c.Projects, pk.project)
		return true, nil
	}
	if isMergePipelinePostShipField(pk.field) {
		if err := setMergePipelinePostShipKey(c, key, v); err != nil {
			return true, err
		}
		return true, nil
	}

	var o config.MergePipelineOverride
	target := &o
	if pk.project == "" {
		// A global key writes straight into the block through a view of it.
		o = config.MergePipelineOverride{
			FileName: c.FileName, SourceOrder: c.SourceOrder, FallbackOnBadFile: c.FallbackOnBadFile,
			ConflictPolicy: c.ConflictPolicy, ConflictModel: c.ConflictModel, VersionFiles: c.VersionFiles,
		}
	} else {
		o = c.Projects[pk.project]
	}
	if err := setMergePipelineField(target, key, pk.field, v); err != nil {
		return true, err
	}
	if pk.project == "" {
		c.FileName, c.SourceOrder, c.FallbackOnBadFile = o.FileName, o.SourceOrder, o.FallbackOnBadFile
		c.ConflictPolicy, c.ConflictModel, c.VersionFiles = o.ConflictPolicy, o.ConflictModel, o.VersionFiles
		return true, nil
	}
	if c.Projects == nil {
		c.Projects = map[string]config.MergePipelineOverride{}
	}
	if o.FileName == "" && o.SourceOrder == nil && o.FallbackOnBadFile == "" && o.ConflictPolicy == "" &&
		o.ConflictModel == nil && o.VersionFiles == nil {
		delete(c.Projects, pk.project)
		return true, nil
	}
	c.Projects[pk.project] = o
	return true, nil
}

// setMergePipelineField parses v for field and stores it in o. null clears the
// field back to unset.
func setMergePipelineField(o *config.MergePipelineOverride, key, field string, v any) error {
	switch field {
	case mergePipelineFieldFileName, mergePipelineFieldFallbackOnBadFile, mergePipelineFieldConflictPolicy:
		str, err := mergePipelineText(key, v)
		if err != nil {
			return err
		}
		switch field {
		case mergePipelineFieldFileName:
			o.FileName = str
		case mergePipelineFieldFallbackOnBadFile:
			o.FallbackOnBadFile = str
		default:
			o.ConflictPolicy = str
		}
	case mergePipelineFieldConflictModel:
		switch x := v.(type) {
		case nil:
			o.ConflictModel = nil
		case string:
			m := strings.TrimSpace(x)
			o.ConflictModel = &m
		default:
			return fmt.Errorf("%s: %v is invalid; use a model name from the LLM registry, or an empty string for no model", key, v)
		}
	case mergePipelineFieldSourceOrder, mergePipelineFieldVersionFiles:
		list, err := mergePipelineList(key, v)
		if err != nil {
			return err
		}
		if field == mergePipelineFieldSourceOrder {
			o.SourceOrder = list
		} else {
			o.VersionFiles = list
		}
	}
	return nil
}

// setMergePipelinePostShipKey sets one global post_ship key on the block; a
// bad value is refused with the key named.
func setMergePipelinePostShipKey(c *config.MergePipelineConfig, key string, v any) error {
	set := func(dst *string) error {
		s, err := mergePipelineText(key, v)
		if err != nil {
			return err
		}
		*dst = s
		return nil
	}
	switch key {
	case mergePipelineKeyPrefix + "post_ship.enabled":
		switch x := v.(type) {
		case nil:
			c.PostShip.Enabled = false
			return nil
		case bool:
			c.PostShip.Enabled = x
			return nil
		default:
			return fmt.Errorf("%s: %v is invalid; use true or false", key, v)
		}
	case mergePipelineKeyPrefix + "post_ship.registry_file":
		return set(&c.PostShip.RegistryFile)
	case mergePipelineKeyPrefix + "post_ship.registry_row_pattern":
		return set(&c.PostShip.RegistryRowPattern)
	case mergePipelineKeyPrefix + "post_ship.companion_repository":
		return set(&c.PostShip.CompanionRepository)
	case mergePipelineKeyPrefix + "post_ship.issue_title_template":
		return set(&c.PostShip.IssueTitleTemplate)
	case mergePipelineKeyPrefix + "post_ship.notify_template":
		return set(&c.PostShip.NotifyTemplate)
	case mergePipelineKeyPrefix + "post_ship.hooks":
		hooks, err := mergePipelinePostShipHooks(key, v)
		if err != nil {
			return err
		}
		c.PostShip.Hooks = hooks
		return nil
	default:
		return fmt.Errorf("%s: unknown post_ship key; use %s", key, strings.Join(mergePipelinePostShipFields, ", "))
	}
}

// mergePipelinePostShipHooks reads the post_ship.hooks value: the hook list
// straight from a REST body, or its JSON text as the chat channel sends it.
// nil or the empty text clears the list. Every form a hook can take in is
// checked by the block's Validate, which refuses a bad hook by its name, an
// unknown built-in by name, a hook without exactly one of builtin or command,
// an enabled built-in with an empty target in the block, and a list over
// PostShipMaxHooks.
func mergePipelinePostShipHooks(key string, v any) ([]config.PostShipHook, error) {
	var decoded []config.PostShipHook
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		text := strings.TrimSpace(x)
		if text == "" {
			return []config.PostShipHook{}, nil
		}
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			return nil, fmt.Errorf("%s: %q is invalid; use a JSON array of {name, builtin or command, enabled} objects", key, text)
		}
		return decoded, nil
	case []any:
		b, err := json.Marshal(x)
		if err != nil {
			return nil, fmt.Errorf("%s: %v is invalid; use a JSON array of {name, builtin or command, enabled} objects", key, v)
		}
		if err := json.Unmarshal(b, &decoded); err != nil {
			return nil, fmt.Errorf("%s: %v", key, err)
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("%s: %v is invalid; use a JSON array of {name, builtin or command, enabled} objects", key, v)
	}
}

// mergePipelineText reads a text value; null reads as unset (empty).
func mergePipelineText(key string, v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return strings.TrimSpace(x), nil
	}
	return "", fmt.Errorf("%s: %v is invalid; use text", key, v)
}

// mergePipelineList reads a list value: a JSON array of text or a comma list.
// null clears the key back to unset (nil). An empty array or string is an
// explicit empty list, which is not nil.
func mergePipelineList(key string, v any) ([]string, error) {
	var out []string
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		out = []string{}
		if strings.TrimSpace(x) == "" {
			return out, nil
		}
		for _, part := range strings.Split(x, ",") {
			out = append(out, strings.TrimSpace(part))
		}
	case []any:
		out = make([]string, 0, len(x))
		for i, it := range x {
			str, ok := it.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d]: %v is invalid; use text", key, i, it)
			}
			out = append(out, strings.TrimSpace(str))
		}
	default:
		return nil, fmt.Errorf("%s: must be a JSON array of text or a comma list", key)
	}
	if len(out) > mergePipelineMaxListItems {
		return nil, fmt.Errorf("%s: %d entries is too many; use at most %d", key, len(out), mergePipelineMaxListItems)
	}
	return out, nil
}

// nonNilStrings returns s, or an empty list when s is nil, so JSON shows [].
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
