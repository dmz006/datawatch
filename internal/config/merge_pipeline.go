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
	FileName          string   `yaml:"file_name,omitempty" json:"file_name,omitempty"`
	SourceOrder       []string `yaml:"source_order,omitempty" json:"source_order,omitempty"`
	FallbackOnBadFile string   `yaml:"fallback_on_bad_file,omitempty" json:"fallback_on_bad_file,omitempty"`
	ConflictPolicy    string   `yaml:"conflict_policy,omitempty" json:"conflict_policy,omitempty"`
	ConflictModel     *string  `yaml:"conflict_model,omitempty" json:"conflict_model,omitempty"`
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
	// Projects holds per-project overrides keyed by project name.
	Projects map[string]MergePipelineOverride `yaml:"projects,omitempty" json:"projects,omitempty"`
}

// IsZero reports whether no key is set, so yaml.v3 can drop an unset block.
func (c MergePipelineConfig) IsZero() bool {
	return c.FileName == "" && c.SourceOrder == nil && c.FallbackOnBadFile == "" &&
		c.ConflictPolicy == "" && c.ConflictModel == nil && len(c.Projects) == 0
}

// Clone returns a deep copy.
func (c MergePipelineConfig) Clone() MergePipelineConfig {
	out := c
	out.SourceOrder = cloneStrings(c.SourceOrder)
	out.ConflictModel = cloneStringPtr(c.ConflictModel)
	if c.Projects != nil {
		out.Projects = make(map[string]MergePipelineOverride, len(c.Projects))
		for k, v := range c.Projects {
			v.SourceOrder = cloneStrings(v.SourceOrder)
			v.ConflictModel = cloneStringPtr(v.ConflictModel)
			out.Projects[k] = v
		}
	}
	return out
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
	if err := validFallback(p+"fallback_on_bad_file", c.FallbackOnBadFile); err != nil {
		return err
	}
	if err := validConflictPolicy(p+"conflict_policy", c.ConflictPolicy); err != nil {
		return err
	}
	if err := validModelName(p+"conflict_model", c.ConflictModel); err != nil {
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
		if err := validFallback(k+"fallback_on_bad_file", o.FallbackOnBadFile); err != nil {
			return err
		}
		if err := validConflictPolicy(k+"conflict_policy", o.ConflictPolicy); err != nil {
			return err
		}
		if err := validModelName(k+"conflict_model", o.ConflictModel); err != nil {
			return err
		}
	}
	return nil
}
