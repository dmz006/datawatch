// Merge pipeline config: the autonomous.merge_pipeline block.
//
// The merge pipeline is the ordered list of steps that takes finished
// branches to shipped. This block says where a project's pipeline is read
// from (a file in its repository, a record the daemon keeps, or the built-in
// default), what happens when the file is bad, and how a merge conflict is
// handled. Every key can be overridden per project; the Effective* accessors
// give the project value when set, else the global value, else the default.
//
// ConflictModel is a *string so an unset key (reads the default model) stays
// distinct from an explicit empty string (no model: the policy degrades to
// stop_and_ask). The model is a name in the LLM registry; this block never
// adds, edits or deletes a registry entry.
package config

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// Where a project's merge pipeline can come from.
const (
	MergePipelineSourceFile   = "file"
	MergePipelineSourceRecord = "record"
)

// What happens when the project's pipeline file exists but cannot be used.
const (
	// MergePipelineFallbackRecord reports the error and uses the daemon record.
	MergePipelineFallbackRecord = "record"
	// MergePipelineFallbackDefault reports the error and uses the built-in pipeline.
	MergePipelineFallbackDefault = "default"
	// MergePipelineFallbackNone reports the error and substitutes nothing.
	MergePipelineFallbackNone = "none"
)

// How a merge conflict is handled.
const (
	ConflictPolicyStopAndAsk         = "stop_and_ask"
	ConflictPolicyMechanical         = "mechanical"
	ConflictPolicyMechanicalAndModel = "mechanical_and_model"
)

// How the conflict model's proposals are used.
const (
	// ConflictModeShadow runs the model without acting on its proposal.
	ConflictModeShadow = "shadow"
	// ConflictModeOn acts on the model's proposal.
	ConflictModeOn = "on"
	// MergeConflictModeDefault is the mode read when conflict_mode is unset.
	MergeConflictModeDefault = ConflictModeShadow
)

// MergeConflictConfirmFirstNDefault is how many of a project's first
// conflict proposals need confirmation when conflict_confirm_first_n is unset.
// An explicit 0 turns confirmation off.
const MergeConflictConfirmFirstNDefault = 5

// Post-ship hook built-in names. A hook uses exactly one of a built-in and
// a command; the built-in says which setting provides its target.
const (
	// PostShipHookBuiltinRegistryRow appends or updates a row in the
	// registry file the project names (targets: registry_file,
	// registry_row_pattern).
	PostShipHookBuiltinRegistryRow = "registry_row"
	// PostShipHookBuiltinParityIssue files an issue in the companion
	// repository (targets: companion_repository, issue_title_template).
	PostShipHookBuiltinParityIssue = "parity_issue"
	// PostShipHookBuiltinTag tags the release (no target setting).
	PostShipHookBuiltinTag = "tag"
	// PostShipHookBuiltinNotify sends a message (target: notify_template).
	PostShipHookBuiltinNotify = "notify"
)

// PostShipMaxHooks bounds the hook list so a project cannot configure an
// unbounded tail after deploy.
const PostShipMaxHooks = 20

const (
	// MergePipelineDefaultFileName is the pipeline file looked for in a project.
	MergePipelineDefaultFileName = ".datawatch/merge-pipeline.yaml"
	// MergePipelineDefaultConflictModel is the strong model that proposes a
	// conflict resolution unless the operator names another. It is a name in
	// the LLM registry.
	MergePipelineDefaultConflictModel = "claude-opus-5-5"
)

// MergePipelineDefaultSourceOrder returns the default: the file, then the record.
func MergePipelineDefaultSourceOrder() []string {
	return []string{MergePipelineSourceFile, MergePipelineSourceRecord}
}

// MergePipelineOverride holds one project's values; an unset key inherits.
type MergePipelineOverride struct {
	FileName              string   `yaml:"file_name,omitempty" json:"file_name,omitempty"`
	SourceOrder           []string `yaml:"source_order,omitempty" json:"source_order,omitempty"`
	FallbackOnBadFile     string   `yaml:"fallback_on_bad_file,omitempty" json:"fallback_on_bad_file,omitempty"`
	ConflictPolicy        string   `yaml:"conflict_policy,omitempty" json:"conflict_policy,omitempty"`
	ConflictModel         *string  `yaml:"conflict_model,omitempty" json:"conflict_model,omitempty"`
	ConflictMode          string   `yaml:"conflict_mode,omitempty" json:"conflict_mode,omitempty"`
	ConflictConfirmFirstN *int     `yaml:"conflict_confirm_first_n,omitempty" json:"conflict_confirm_first_n,omitempty"`
	VersionFiles          []string `yaml:"version_files,omitempty" json:"version_files,omitempty"`
}

// MergePipelineConfig is the autonomous.merge_pipeline block.
type MergePipelineConfig struct {
	// FileName is the pipeline file, relative to the project. Unset reads
	// ".datawatch/merge-pipeline.yaml".
	FileName string `yaml:"file_name,omitempty" json:"file_name,omitempty"`
	// SourceOrder lists the sources tried first to last: "file", "record".
	// Unset reads file then record; the built-in default always applies last.
	SourceOrder []string `yaml:"source_order,omitempty" json:"source_order,omitempty"`
	// FallbackOnBadFile is "record" (default), "default" or "none".
	FallbackOnBadFile string `yaml:"fallback_on_bad_file,omitempty" json:"fallback_on_bad_file,omitempty"`
	// ConflictPolicy is "stop_and_ask", "mechanical" or "mechanical_and_model"
	// (default).
	ConflictPolicy string `yaml:"conflict_policy,omitempty" json:"conflict_policy,omitempty"`
	// ConflictModel names the LLM registry entry that proposes a resolution.
	// Unset reads MergePipelineDefaultConflictModel; an explicit empty string
	// means no model.
	ConflictModel *string `yaml:"conflict_model,omitempty" json:"conflict_model,omitempty"`
	// ConflictMode is "shadow" or "on". Unset reads "shadow".
	ConflictMode string `yaml:"conflict_mode,omitempty" json:"conflict_mode,omitempty"`
	// ConflictConfirmFirstN is how many of a project's first conflict
	// proposals need confirmation. Unset reads 5; an explicit 0 turns
	// confirmation off; a negative value is invalid.
	ConflictConfirmFirstN *int `yaml:"conflict_confirm_first_n,omitempty" json:"conflict_confirm_first_n,omitempty"`
	// VersionFiles lists the project files, relative to the project, that
	// carry the `var Version = "X.Y.Z"` line the release-version step sets.
	// Unset means none: a pipeline that bumps the version fails that step
	// until the operator names the files.
	VersionFiles []string `yaml:"version_files,omitempty" json:"version_files,omitempty"`
	// PostShip holds the post-ship hook list: the small steps that run
	// after the ship's deploy step. Off by default; a hook is a named
	// built-in or a shell command, and every target a built-in touches is
	// a setting in this block, never a value in code.
	PostShip PostShipConfig `yaml:"post_ship,omitempty" json:"post_ship,omitempty"`
	// Projects holds per-project overrides keyed by project name.
	Projects map[string]MergePipelineOverride `yaml:"projects,omitempty" json:"projects,omitempty"`
}

// IsZero reports whether no key is set, so yaml.v3 can drop an unset block.
func (c MergePipelineConfig) IsZero() bool {
	return c.FileName == "" && c.SourceOrder == nil && c.FallbackOnBadFile == "" &&
		c.ConflictPolicy == "" && c.ConflictModel == nil && c.ConflictMode == "" &&
		c.ConflictConfirmFirstN == nil && c.VersionFiles == nil &&
		c.PostShip.IsZero() && len(c.Projects) == 0
}

// Clone returns a deep copy.
func (c MergePipelineConfig) Clone() MergePipelineConfig {
	out := c
	out.SourceOrder = cloneStrings(c.SourceOrder)
	out.VersionFiles = cloneStrings(c.VersionFiles)
	out.ConflictModel = cloneStringPtr(c.ConflictModel)
	out.ConflictConfirmFirstN = cloneIntPtr(c.ConflictConfirmFirstN)
	out.PostShip = c.PostShip.Clone()
	if c.Projects != nil {
		out.Projects = make(map[string]MergePipelineOverride, len(c.Projects))
		for k, v := range c.Projects {
			v.SourceOrder = cloneStrings(v.SourceOrder)
			v.VersionFiles = cloneStrings(v.VersionFiles)
			v.ConflictModel = cloneStringPtr(v.ConflictModel)
			v.ConflictConfirmFirstN = cloneIntPtr(v.ConflictConfirmFirstN)
			out.Projects[k] = v
		}
	}
	return out
}

func cloneIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneStrings(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string{}, s...)
}

func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func (c MergePipelineConfig) override(project string) MergePipelineOverride {
	return c.Projects[project]
}

// EffectiveFileName returns the pipeline file name for project.
func (c MergePipelineConfig) EffectiveFileName(project string) string {
	if v := c.override(project).FileName; v != "" {
		return v
	}
	if c.FileName != "" {
		return c.FileName
	}
	return MergePipelineDefaultFileName
}

// EffectiveSourceOrder returns the source order for project (a copy).
func (c MergePipelineConfig) EffectiveSourceOrder(project string) []string {
	if v := c.override(project).SourceOrder; len(v) > 0 {
		return cloneStrings(v)
	}
	if len(c.SourceOrder) > 0 {
		return cloneStrings(c.SourceOrder)
	}
	return MergePipelineDefaultSourceOrder()
}

// EffectiveVersionFiles returns the version files for project (a copy); empty
// when the operator named none.
func (c MergePipelineConfig) EffectiveVersionFiles(project string) []string {
	if v := c.override(project).VersionFiles; len(v) > 0 {
		return cloneStrings(v)
	}
	return cloneStrings(c.VersionFiles)
}

// EffectiveFallbackOnBadFile returns what to do with a bad file for project.
func (c MergePipelineConfig) EffectiveFallbackOnBadFile(project string) string {
	if v := c.override(project).FallbackOnBadFile; v != "" {
		return v
	}
	if c.FallbackOnBadFile != "" {
		return c.FallbackOnBadFile
	}
	return MergePipelineFallbackRecord
}

// EffectiveConflictPolicy returns the configured conflict policy for project,
// before the model's availability is considered.
func (c MergePipelineConfig) EffectiveConflictPolicy(project string) string {
	if v := c.override(project).ConflictPolicy; v != "" {
		return v
	}
	if c.ConflictPolicy != "" {
		return c.ConflictPolicy
	}
	return ConflictPolicyMechanicalAndModel
}

// EffectivePostShip returns the post-ship block as a copy. An unset
// block reads as: disabled, no hooks, no targets.
func (c MergePipelineConfig) EffectivePostShip() PostShipConfig {
	return c.PostShip.Clone()
}

// EffectiveConflictModel returns the model name for project. An explicit
// empty string is returned as is, meaning no model.
func (c MergePipelineConfig) EffectiveConflictModel(project string) string {
	if v := c.override(project).ConflictModel; v != nil {
		return *v
	}
	if c.ConflictModel != nil {
		return *c.ConflictModel
	}
	return MergePipelineDefaultConflictModel
}

// EffectiveConflictMode returns "shadow" or "on" for project: the project
// value, else the global value, else MergeConflictModeDefault.
func (c MergePipelineConfig) EffectiveConflictMode(project string) string {
	if v := c.override(project).ConflictMode; v != "" {
		return v
	}
	if c.ConflictMode != "" {
		return c.ConflictMode
	}
	return MergeConflictModeDefault
}

// EffectiveConflictConfirmFirstN returns how many first conflict proposals
// need confirmation for project: the project value, else the global value,
// else MergeConflictConfirmFirstNDefault. An explicit 0 is returned as is.
func (c MergePipelineConfig) EffectiveConflictConfirmFirstN(project string) int {
	if v := c.override(project).ConflictConfirmFirstN; v != nil {
		return *v
	}
	if c.ConflictConfirmFirstN != nil {
		return *c.ConflictConfirmFirstN
	}
	return MergeConflictConfirmFirstNDefault
}

// PostShipHook is one entry of the post-ship hook list. Exactly one of
// Builtin and Command is set: a built-in hook runs against the target
// setting named by its built-in, a command hook runs the shell line.
// Enabled is false by default: listing a hook does not turn it on.
type PostShipHook struct {
	// Name identifies the hook inside the list; required and unique.
	Name string `yaml:"name" json:"name"`
	// Builtin is one of the post-ship built-in names. Empty when Command
	// is set.
	Builtin string `yaml:"builtin,omitempty" json:"builtin,omitempty"`
	// Command is a shell command line. Empty when Builtin is set.
	Command string `yaml:"command,omitempty" json:"command,omitempty"`
	// Enabled turns the hook on. False by default.
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// PostShipConfig is the autonomous.merge_pipeline.post_ship block: the
// hook list that runs after the ship's deploy step, and the target
// settings its built-ins use. Off by default (Enabled false, no hooks),
// and no target has a hard-coded default: a target that is unset is
// empty, and an enabled built-in whose target is empty is rejected by
// validation rather than guessed.
type PostShipConfig struct {
	// Enabled turns the whole block on. False by default: with the block
	// off, not one hook runs.
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// Hooks lists the steps, run in order after a hook's predecessor.
	Hooks []PostShipHook `yaml:"hooks,omitempty" json:"hooks,omitempty"`
	// RegistryFile is the registry file the registry_row hook writes,
	// relative to the project. It may contain the {project} placeholder.
	// No default.
	RegistryFile string `yaml:"registry_file,omitempty" json:"registry_file,omitempty"`
	// RegistryRowPattern is the line-matching text template the
	// registry_row hook writes. It may contain the {version} placeholder.
	// No default.
	RegistryRowPattern string `yaml:"registry_row_pattern,omitempty" json:"registry_row_pattern,omitempty"`
	// CompanionRepository is the owner/name the parity_issue hook files
	// its issue in. No default.
	CompanionRepository string `yaml:"companion_repository,omitempty" json:"companion_repository,omitempty"`
	// IssueTitleTemplate is the parity_issue hook's issue title. It may
	// contain the {project} and {version} placeholders. No default.
	IssueTitleTemplate string `yaml:"issue_title_template,omitempty" json:"issue_title_template,omitempty"`
	// NotifyTemplate is the notify hook's message body. It may contain
	// the {version} placeholder. No default.
	NotifyTemplate string `yaml:"notify_template,omitempty" json:"notify_template,omitempty"`
}

// IsZero reports whether no key is set, so yaml.v3 can drop an unset
// block.
func (s PostShipConfig) IsZero() bool {
	return !s.Enabled && len(s.Hooks) == 0 && s.RegistryFile == "" &&
		s.RegistryRowPattern == "" && s.CompanionRepository == "" &&
		s.IssueTitleTemplate == "" && s.NotifyTemplate == ""
}

// Clone returns a deep copy; callers may mutate the copy's hooks.
func (s PostShipConfig) Clone() PostShipConfig {
	out := s
	if s.Hooks != nil {
		out.Hooks = append([]PostShipHook{}, s.Hooks...)
	}
	return out
}

// EffectiveHooks returns the hook list as a copy; empty when unset.
func (s PostShipConfig) EffectiveHooks() []PostShipHook {
	return s.Clone().Hooks
}

// HookEnabled reports whether the hook called name is listed and enabled.
func (s PostShipConfig) HookEnabled(name string) bool {
	for _, h := range s.Hooks {
		if h.Name == name {
			return h.Enabled
		}
	}
	return false
}

// EffectiveRegistryFile returns the registry_file setting; empty when
// unset, so nothing names a registry path of its own.
func (s PostShipConfig) EffectiveRegistryFile() string { return s.RegistryFile }

// EffectiveRegistryRowPattern returns the registry_row_pattern setting;
// empty when unset.
func (s PostShipConfig) EffectiveRegistryRowPattern() string {
	return s.RegistryRowPattern
}

// EffectiveCompanionRepository returns the companion_repository setting;
// empty when unset, so nothing names a repository of its own.
func (s PostShipConfig) EffectiveCompanionRepository() string {
	return s.CompanionRepository
}

// EffectiveIssueTitleTemplate returns the issue_title_template setting;
// empty when unset, so nothing names a title of its own.
func (s PostShipConfig) EffectiveIssueTitleTemplate() string {
	return s.IssueTitleTemplate
}

// EffectiveNotifyTemplate returns the notify_template setting; empty when
// unset, so nothing names a message body of its own.
func (s PostShipConfig) EffectiveNotifyTemplate() string { return s.NotifyTemplate }

func validPostShipBuiltin(v string) bool {
	switch v {
	case PostShipHookBuiltinRegistryRow, PostShipHookBuiltinParityIssue, PostShipHookBuiltinTag, PostShipHookBuiltinNotify:
		return true
	}
	return false
}

// Validate rejects an invalid post-ship block, naming the offending key in
// every error. Unset keys are valid and read as "off, no hooks".
func (s PostShipConfig) Validate() error {
	const p = "autonomous.merge_pipeline.post_ship."
	if err := validFileName(p+"registry_file", s.RegistryFile); err != nil {
		return err
	}
	if len(s.Hooks) > PostShipMaxHooks {
		return fmt.Errorf("%shooks: at most %d hooks are allowed", p, PostShipMaxHooks)
	}
	first := make(map[string]int, len(s.Hooks))
	for i, h := range s.Hooks {
		k := fmt.Sprintf("%shooks[%d]", p, i)
		if strings.TrimSpace(h.Name) == "" {
			return fmt.Errorf("%s.name: a hook name is required", k)
		}
		if at, dup := first[h.Name]; dup {
			return fmt.Errorf("%s.name: hook %q is listed twice (first at hooks[%d])", k, h.Name, at)
		}
		first[h.Name] = i
		switch {
		case h.Builtin != "" && h.Command != "":
			return fmt.Errorf("%s: set either builtin or command, not both", k)
		case h.Builtin == "" && h.Command == "":
			return fmt.Errorf("%s: set either builtin or command", k)
		}
		if h.Builtin != "" && !validPostShipBuiltin(h.Builtin) {
			return fmt.Errorf("%s.builtin: %q is unknown; use %q, %q, %q or %q", k, h.Builtin,
				PostShipHookBuiltinRegistryRow, PostShipHookBuiltinParityIssue, PostShipHookBuiltinTag,
				PostShipHookBuiltinNotify)
		}
		if !h.Enabled || h.Builtin == "" {
			continue
		}
		need := func(key, value, hook string) error {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%s%s: required because hook %q is enabled", p, key, hook)
			}
			return nil
		}
		switch h.Builtin {
		case PostShipHookBuiltinRegistryRow:
			if err := need("registry_file", s.RegistryFile, h.Name); err != nil {
				return err
			}
			if err := need("registry_row_pattern", s.RegistryRowPattern, h.Name); err != nil {
				return err
			}
		case PostShipHookBuiltinParityIssue:
			if err := need("companion_repository", s.CompanionRepository, h.Name); err != nil {
				return err
			}
			if err := need("issue_title_template", s.IssueTitleTemplate, h.Name); err != nil {
				return err
			}
		case PostShipHookBuiltinNotify:
			if err := need("notify_template", s.NotifyTemplate, h.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// LLMLookup is the part of the LLM registry the conflict policy needs: it
// reports whether a named entry exists and is enabled.
type LLMLookup interface {
	ResolveKind(name string) (string, bool)
}

// ResolveConflictPolicy returns the policy to act on for project and the
// model to use. When the policy needs a model and the configured entry is
// empty, missing from the registry or disabled, the policy degrades to
// stop_and_ask and the model is empty; it never falls to a different model.
// degraded says why, in plain words, or is empty when nothing degraded.
func (c MergePipelineConfig) ResolveConflictPolicy(project string, reg LLMLookup) (policy, model, degraded string) {
	policy = c.EffectiveConflictPolicy(project)
	if policy != ConflictPolicyMechanicalAndModel {
		return policy, "", ""
	}
	model = c.EffectiveConflictModel(project)
	switch {
	case model == "":
		return ConflictPolicyStopAndAsk, "", "no conflict model is set, so conflicts stop and ask"
	case reg == nil:
		return ConflictPolicyStopAndAsk, "", "no model registry is available, so conflicts stop and ask"
	}
	if _, ok := reg.ResolveKind(model); !ok {
		return ConflictPolicyStopAndAsk, "", fmt.Sprintf("the conflict model %q is not available, so conflicts stop and ask", model)
	}
	return policy, model, ""
}

func validSourceOrder(key string, order []string) error {
	seen := map[string]bool{}
	for i, s := range order {
		if s != MergePipelineSourceFile && s != MergePipelineSourceRecord {
			return fmt.Errorf("%s[%d]: unknown source %q; use %q or %q", key, i, s,
				MergePipelineSourceFile, MergePipelineSourceRecord)
		}
		if seen[s] {
			return fmt.Errorf("%s[%d]: source %q is listed twice", key, i, s)
		}
		seen[s] = true
	}
	return nil
}

func validFileName(key, name string) error {
	if name == "" {
		return nil
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) || strings.Contains(name, `\`) ||
		(len(name) >= 2 && name[1] == ':') {
		return fmt.Errorf("%s: %q must be a path inside the project, not an absolute one", key, name)
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return fmt.Errorf("%s: %q must stay inside the project", key, name)
		}
	}
	if clean := path.Clean(name); clean == "." || strings.HasSuffix(name, "/") {
		return fmt.Errorf("%s: %q must name a file", key, name)
	}
	return nil
}

func validFallback(key, v string) error {
	switch v {
	case "", MergePipelineFallbackRecord, MergePipelineFallbackDefault, MergePipelineFallbackNone:
		return nil
	}
	return fmt.Errorf("%s: %q is invalid; use %q, %q or %q", key, v,
		MergePipelineFallbackRecord, MergePipelineFallbackDefault, MergePipelineFallbackNone)
}

func validConflictPolicy(key, v string) error {
	switch v {
	case "", ConflictPolicyStopAndAsk, ConflictPolicyMechanical, ConflictPolicyMechanicalAndModel:
		return nil
	}
	return fmt.Errorf("%s: %q is invalid; use %q, %q or %q", key, v,
		ConflictPolicyStopAndAsk, ConflictPolicyMechanical, ConflictPolicyMechanicalAndModel)
}

func validConflictMode(key, v string) error {
	switch v {
	case "", ConflictModeShadow, ConflictModeOn:
		return nil
	}
	return fmt.Errorf("%s: %q is invalid; use %q or %q", key, v, ConflictModeShadow, ConflictModeOn)
}

func validConfirmFirstN(key string, v *int) error {
	if v == nil || *v >= 0 {
		return nil
	}
	return fmt.Errorf("%s: %d is invalid; use 0 or more (0 turns confirmation off)", key, *v)
}

func validModelName(key string, v *string) error {
	if v == nil || *v == "" {
		return nil
	}
	if *v != strings.TrimSpace(*v) || strings.ContainsAny(*v, " \t\r\n") {
		return fmt.Errorf("%s: %q must be a registry entry name with no spaces", key, *v)
	}
	return nil
}

// Validate rejects an invalid value, naming the key. An unset key is valid.
func (c MergePipelineConfig) Validate() error {
	const p = "autonomous.merge_pipeline."
	if err := validFileName(p+"file_name", c.FileName); err != nil {
		return err
	}
	if err := validSourceOrder(p+"source_order", c.SourceOrder); err != nil {
		return err
	}
	for _, f := range c.VersionFiles {
		if f == "" {
			return fmt.Errorf("%sversion_files: a file name is empty", p)
		}
		if err := validFileName(p+"version_files", f); err != nil {
			return err
		}
	}
	if err := validFallback(p+"fallback_on_bad_file", c.FallbackOnBadFile); err != nil {
		return err
	}
	if err := validConflictPolicy(p+"conflict_policy", c.ConflictPolicy); err != nil {
		return err
	}
	if err := validModelName(p+"conflict_model", c.ConflictModel); err != nil {
		return err
	}
	if err := validConflictMode(p+"conflict_mode", c.ConflictMode); err != nil {
		return err
	}
	if err := validConfirmFirstN(p+"conflict_confirm_first_n", c.ConflictConfirmFirstN); err != nil {
		return err
	}
	if err := c.PostShip.Validate(); err != nil {
		return err
	}
	names := make([]string, 0, len(c.Projects))
	for name := range c.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%sprojects: a project name is empty", p)
		}
		o := c.Projects[name]
		k := fmt.Sprintf("%sprojects.%s.", p, name)
		if err := validFileName(k+"file_name", o.FileName); err != nil {
			return err
		}
		if err := validSourceOrder(k+"source_order", o.SourceOrder); err != nil {
			return err
		}
		for _, f := range o.VersionFiles {
			if f == "" {
				return fmt.Errorf("%sversion_files: a file name is empty", k)
			}
			if err := validFileName(k+"version_files", f); err != nil {
				return err
			}
		}
		if err := validFallback(k+"fallback_on_bad_file", o.FallbackOnBadFile); err != nil {
			return err
		}
		if err := validConflictPolicy(k+"conflict_policy", o.ConflictPolicy); err != nil {
			return err
		}
		if err := validModelName(k+"conflict_model", o.ConflictModel); err != nil {
			return err
		}
		if err := validConflictMode(k+"conflict_mode", o.ConflictMode); err != nil {
			return err
		}
		if err := validConfirmFirstN(k+"conflict_confirm_first_n", o.ConflictConfirmFirstN); err != nil {
			return err
		}
	}
	return nil
}
