// Defect queue configuration: the autonomous.defect_queue block.
//
// The block tunes how queued defects are grouped into batches and when a
// defect qualifies as a "tiny fix" (a small, low-risk change that needs no
// operator review). Every setting has a built-in default, so an absent or
// empty block behaves exactly like the documented defaults; a per-project
// entry overrides the batch and tiny-fix settings for one project only.
//
// Every configuration surface (PUT /api/config, the comm channel's
// configure verb, MCP config_set and the CLI's config set) reaches
// ValidateDefectQueueSetting through the REST handler, and Load validates
// the block at startup, so a refused value carries the same message
// everywhere. Precedence for a setting is: project override, then the
// block-level value, then the built-in default.
package config

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// DefectQueueSettingPrefix is the dotted key prefix shared by every
// autonomous.defect_queue.* setting.
const DefectQueueSettingPrefix = "autonomous.defect_queue."

// Legal batch grouping keys: the attributes defects are grouped on.
const (
	DefectGroupProjectArea    = "project_area"
	DefectGroupSharedFiles    = "shared_files"
	DefectGroupSharedSurfaces = "shared_surfaces"
)

// Legal batch priority rules: how a batch takes its priority from members.
const (
	// DefectPriorityHighestMember — the batch carries its highest-priority
	// member's priority. This is the default.
	DefectPriorityHighestMember = "highest_member"
	// DefectPriorityFirstMember — the batch carries the priority of the
	// member that joined it first.
	DefectPriorityFirstMember = "first_member"
)

// Legal tiny-fix kinds: the categories of change a tiny fix may be.
const (
	DefectKindTestOnly   = "test_only"
	DefectKindDocsOnly   = "docs_only"
	DefectKindScriptOnly = "script_only"
	DefectKindRuleTweak  = "rule_tweak"
	DefectKindLintTweak  = "lint_tweak"
)

// Built-in numeric defaults.
const (
	// DefaultDefectBatchSizeCap is the most defects one batch holds.
	DefaultDefectBatchSizeCap = 5
	// DefaultDefectTinyFixMaxFiles is the most files a tiny fix may touch.
	DefaultDefectTinyFixMaxFiles = 3
)

// Names of the settings below a block or a project entry. A project entry
// accepts every batch.* and tiny_fix.* name; "enabled" at block level
// switches the whole defect queue.
const (
	defectFieldEnabled        = "enabled"
	defectFieldSizeCap        = "batch.size_cap"
	defectFieldGroupingKeys   = "batch.grouping_keys"
	defectFieldAllowUnrelated = "batch.allow_unrelated"
	defectFieldPriorityRule   = "batch.priority_rule"
	defectFieldTinyEnabled    = "tiny_fix.enabled"
	defectFieldMaxFiles       = "tiny_fix.max_files"
	defectFieldPathGlobs      = "tiny_fix.allowed_path_globs"
	defectFieldKinds          = "tiny_fix.allowed_kinds"
	defectFieldVisibleGlobs   = "tiny_fix.operator_visible_path_globs"
)

// defectProjectFields are the settings a per-project entry may override.
var defectProjectFields = []string{
	defectFieldSizeCap, defectFieldGroupingKeys, defectFieldAllowUnrelated,
	defectFieldPriorityRule, defectFieldTinyEnabled, defectFieldMaxFiles,
	defectFieldPathGlobs, defectFieldKinds, defectFieldVisibleGlobs,
}

// DefaultDefectGroupingKeys returns the built-in batch grouping keys.
func DefaultDefectGroupingKeys() []string {
	return []string{DefectGroupProjectArea, DefectGroupSharedFiles, DefectGroupSharedSurfaces}
}

// DefaultDefectTinyFixKinds returns the built-in tiny-fix kinds.
func DefaultDefectTinyFixKinds() []string {
	return []string{DefectKindTestOnly, DefectKindDocsOnly, DefectKindScriptOnly, DefectKindRuleTweak, DefectKindLintTweak}
}

// DefaultDefectTinyFixPathGlobs returns the built-in paths a tiny fix may
// touch: test files, docs, scripts and lint / rule configuration.
func DefaultDefectTinyFixPathGlobs() []string {
	return []string{
		"**/*_test.go", "**/testdata/**", "tests/**", "test/**",
		"docs/**", "**/*.md",
		"scripts/**",
		".golangci.yml", ".golangci.yaml", ".trivyignore", ".gitleaks.toml", ".markdownlint*",
	}
}

// DefaultDefectOperatorVisibleGlobs returns the built-in paths whose change
// the operator can see, which disqualifies a change from being a tiny fix.
func DefaultDefectOperatorVisibleGlobs() []string {
	return []string{"internal/server/web/**", "cmd/**", "internal/server/api.go"}
}

// DefectQueueBatch holds the batching settings. A zero or nil field is
// unset: it reads the next level down (block level, then the default).
type DefectQueueBatch struct {
	// SizeCap is the most defects one batch holds; must be at least 1.
	SizeCap int `yaml:"size_cap,omitempty" json:"size_cap,omitempty"`
	// GroupingKeys are the attributes defects are grouped on; each must be
	// DefectGroupProjectArea, DefectGroupSharedFiles or DefectGroupSharedSurfaces.
	GroupingKeys []string `yaml:"grouping_keys,omitempty" json:"grouping_keys,omitempty"`
	// AllowUnrelated lets defects with no shared grouping key share a batch.
	AllowUnrelated *bool `yaml:"allow_unrelated,omitempty" json:"allow_unrelated,omitempty"`
	// PriorityRule is DefectPriorityHighestMember or DefectPriorityFirstMember.
	PriorityRule string `yaml:"priority_rule,omitempty" json:"priority_rule,omitempty"`
}

// DefectQueueTinyFix holds the tiny-fix criteria. A zero or nil field is
// unset: it reads the next level down.
type DefectQueueTinyFix struct {
	// Enabled switches tiny-fix handling.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// MaxFiles is the most files a tiny fix may touch; must be at least 1.
	MaxFiles int `yaml:"max_files,omitempty" json:"max_files,omitempty"`
	// AllowedPathGlobs are the paths a tiny fix may touch.
	AllowedPathGlobs []string `yaml:"allowed_path_globs,omitempty" json:"allowed_path_globs,omitempty"`
	// AllowedKinds are the change kinds that qualify as a tiny fix.
	AllowedKinds []string `yaml:"allowed_kinds,omitempty" json:"allowed_kinds,omitempty"`
	// OperatorVisiblePathGlobs are paths that mean a change is
	// operator-visible and so is never a tiny fix.
	OperatorVisiblePathGlobs []string `yaml:"operator_visible_path_globs,omitempty" json:"operator_visible_path_globs,omitempty"`
}

// DefectQueueProject is one per-project override: only the fields that are
// set replace the block-level value.
type DefectQueueProject struct {
	Batch   DefectQueueBatch   `yaml:"batch,omitempty" json:"batch,omitempty"`
	TinyFix DefectQueueTinyFix `yaml:"tiny_fix,omitempty" json:"tiny_fix,omitempty"`
}

// DefectQueueConfig is the autonomous.defect_queue block. Enabled is a
// *bool (nil = unset = true) so an explicit false survives a save and load.
type DefectQueueConfig struct {
	// Enabled is the master switch for the defect queue. Unset reads true.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// Batch holds the block-level batching settings.
	Batch DefectQueueBatch `yaml:"batch,omitempty" json:"batch,omitempty"`
	// TinyFix holds the block-level tiny-fix criteria.
	TinyFix DefectQueueTinyFix `yaml:"tiny_fix,omitempty" json:"tiny_fix,omitempty"`
	// Projects maps a project name to its overrides.
	Projects map[string]DefectQueueProject `yaml:"projects,omitempty" json:"projects,omitempty"`
}

// DefectQueueBatchSettings is the resolved batching settings, every field set.
type DefectQueueBatchSettings struct {
	SizeCap        int      `json:"size_cap"`
	GroupingKeys   []string `json:"grouping_keys"`
	AllowUnrelated bool     `json:"allow_unrelated"`
	PriorityRule   string   `json:"priority_rule"`
}

// DefectQueueTinyFixSettings is the resolved tiny-fix criteria, every field set.
type DefectQueueTinyFixSettings struct {
	Enabled                  bool     `json:"enabled"`
	MaxFiles                 int      `json:"max_files"`
	AllowedPathGlobs         []string `json:"allowed_path_globs"`
	AllowedKinds             []string `json:"allowed_kinds"`
	OperatorVisiblePathGlobs []string `json:"operator_visible_path_globs"`
}

// DefectQueueSettings is everything in force for one project.
type DefectQueueSettings struct {
	Enabled bool                       `json:"enabled"`
	Batch   DefectQueueBatchSettings   `json:"batch"`
	TinyFix DefectQueueTinyFixSettings `json:"tiny_fix"`
}

// EffectiveEnabled reports whether the defect queue is on (unset reads true).
func (d DefectQueueConfig) EffectiveEnabled() bool {
	return d.Enabled == nil || *d.Enabled
}

// EffectiveBatch resolves the block-level batching settings over the defaults.
func (d DefectQueueConfig) EffectiveBatch() DefectQueueBatchSettings {
	return resolveDefectBatch(d.Batch, nil)
}

// EffectiveTinyFix resolves the block-level tiny-fix criteria over the defaults.
func (d DefectQueueConfig) EffectiveTinyFix() DefectQueueTinyFixSettings {
	return resolveDefectTinyFix(d.TinyFix, nil)
}

// EffectiveForProject resolves every setting for one project: the project
// entry first, then the block-level value, then the built-in default. A
// project with no entry gets the block-level view.
func (d DefectQueueConfig) EffectiveForProject(project string) DefectQueueSettings {
	var p *DefectQueueProject
	if e, ok := d.Projects[strings.TrimSpace(project)]; ok {
		p = &e
	}
	out := DefectQueueSettings{Enabled: d.EffectiveEnabled()}
	if p == nil {
		out.Batch = d.EffectiveBatch()
		out.TinyFix = d.EffectiveTinyFix()
		return out
	}
	out.Batch = resolveDefectBatch(d.Batch, &p.Batch)
	out.TinyFix = resolveDefectTinyFix(d.TinyFix, &p.TinyFix)
	return out
}

func resolveDefectBatch(base DefectQueueBatch, over *DefectQueueBatch) DefectQueueBatchSettings {
	out := DefectQueueBatchSettings{
		SizeCap:      DefaultDefectBatchSizeCap,
		GroupingKeys: DefaultDefectGroupingKeys(),
		PriorityRule: DefectPriorityHighestMember,
	}
	for _, b := range []*DefectQueueBatch{&base, over} {
		if b == nil {
			continue
		}
		if b.SizeCap > 0 {
			out.SizeCap = b.SizeCap
		}
		if len(b.GroupingKeys) > 0 {
			out.GroupingKeys = append([]string(nil), b.GroupingKeys...)
		}
		if b.AllowUnrelated != nil {
			out.AllowUnrelated = *b.AllowUnrelated
		}
		if r := strings.ToLower(strings.TrimSpace(b.PriorityRule)); r != "" {
			out.PriorityRule = r
		}
	}
	return out
}

func resolveDefectTinyFix(base DefectQueueTinyFix, over *DefectQueueTinyFix) DefectQueueTinyFixSettings {
	out := DefectQueueTinyFixSettings{
		Enabled:                  true,
		MaxFiles:                 DefaultDefectTinyFixMaxFiles,
		AllowedPathGlobs:         DefaultDefectTinyFixPathGlobs(),
		AllowedKinds:             DefaultDefectTinyFixKinds(),
		OperatorVisiblePathGlobs: DefaultDefectOperatorVisibleGlobs(),
	}
	for _, t := range []*DefectQueueTinyFix{&base, over} {
		if t == nil {
			continue
		}
		if t.Enabled != nil {
			out.Enabled = *t.Enabled
		}
		if t.MaxFiles > 0 {
			out.MaxFiles = t.MaxFiles
		}
		if len(t.AllowedPathGlobs) > 0 {
			out.AllowedPathGlobs = append([]string(nil), t.AllowedPathGlobs...)
		}
		if len(t.AllowedKinds) > 0 {
			out.AllowedKinds = append([]string(nil), t.AllowedKinds...)
		}
		if len(t.OperatorVisiblePathGlobs) > 0 {
			out.OperatorVisiblePathGlobs = append([]string(nil), t.OperatorVisiblePathGlobs...)
		}
	}
	return out
}

// DefectQueueSettingKeys lists every settable key of the block (full dotted
// form). Per-project overrides are addressed as
// autonomous.defect_queue.projects.<project>.<batch.* | tiny_fix.*>.
func DefectQueueSettingKeys() []string {
	keys := []string{DefectQueueSettingPrefix + defectFieldEnabled}
	for _, f := range defectProjectFields {
		keys = append(keys, DefectQueueSettingPrefix+f)
	}
	sort.Strings(keys)
	return keys
}

// ValidateDefectQueueSetting checks one autonomous.defect_queue.* setting.
// key is the full dotted key; a key outside the block returns nil. value is
// what a config write carries (a bool, a number, a list, or a string — lists
// also accept a comma-separated string). A nil value is accepted only for a
// per-project key, where it clears the override.
func ValidateDefectQueueSetting(key string, value interface{}) error {
	name, ok := strings.CutPrefix(key, DefectQueueSettingPrefix)
	if !ok {
		return nil
	}
	_, _, err := parseDefectQueueKey(name, value)
	return err
}

// Set validates one setting and stores it on the block. name is the key
// without the autonomous.defect_queue. prefix. On error the block is
// unchanged. A nil value on a project key clears that override; clearing
// the last one removes the project entry.
func (d *DefectQueueConfig) Set(name string, value interface{}) error {
	field, project, err := splitDefectQueueKey(name)
	if err != nil {
		return err
	}
	v, err := parseDefectQueueValue(name, field, project != "", value)
	if err != nil {
		return err
	}
	if project == "" {
		if field == defectFieldEnabled {
			d.Enabled = v.(*bool)
			return nil
		}
		storeDefectField(&d.Batch, &d.TinyFix, field, v)
		return nil
	}
	p := d.Projects[project]
	storeDefectField(&p.Batch, &p.TinyFix, field, v)
	if p.isZero() {
		delete(d.Projects, project)
		return nil
	}
	if d.Projects == nil {
		d.Projects = map[string]DefectQueueProject{}
	}
	d.Projects[project] = p
	return nil
}

func (p DefectQueueProject) isZero() bool {
	b, t := p.Batch, p.TinyFix
	return b.SizeCap == 0 && b.GroupingKeys == nil && b.AllowUnrelated == nil && b.PriorityRule == "" &&
		t.Enabled == nil && t.MaxFiles == 0 && t.AllowedPathGlobs == nil && t.AllowedKinds == nil &&
		t.OperatorVisiblePathGlobs == nil
}

// storeDefectField writes a parsed value (nil clears) into a batch/tiny-fix pair.
func storeDefectField(b *DefectQueueBatch, t *DefectQueueTinyFix, field string, v interface{}) {
	switch field {
	case defectFieldSizeCap:
		b.SizeCap, _ = v.(int)
	case defectFieldGroupingKeys:
		b.GroupingKeys, _ = v.([]string)
	case defectFieldAllowUnrelated:
		b.AllowUnrelated, _ = v.(*bool)
	case defectFieldPriorityRule:
		b.PriorityRule, _ = v.(string)
	case defectFieldTinyEnabled:
		t.Enabled, _ = v.(*bool)
	case defectFieldMaxFiles:
		t.MaxFiles, _ = v.(int)
	case defectFieldPathGlobs:
		t.AllowedPathGlobs, _ = v.([]string)
	case defectFieldKinds:
		t.AllowedKinds, _ = v.([]string)
	case defectFieldVisibleGlobs:
		t.OperatorVisiblePathGlobs, _ = v.([]string)
	}
}

func defectQueueErr(name, reason string) error {
	return fmt.Errorf("%s%s: %s", DefectQueueSettingPrefix, name, reason)
}

// splitDefectQueueKey splits a key (without the block prefix) into the
// setting field and the project name ("" for a block-level key).
func splitDefectQueueKey(name string) (field, project string, err error) {
	if name == defectFieldEnabled {
		return name, "", nil
	}
	for _, f := range defectProjectFields {
		if name == f {
			return f, "", nil
		}
	}
	rest, ok := strings.CutPrefix(name, "projects.")
	if !ok {
		return "", "", defectQueueErr(name, "unknown defect queue setting")
	}
	for _, f := range defectProjectFields {
		if p, ok := strings.CutSuffix(rest, "."+f); ok {
			p = strings.TrimSpace(p)
			if p == "" {
				return "", "", defectQueueErr(name, "project name must not be empty")
			}
			return f, p, nil
		}
	}
	return "", "", defectQueueErr(name, "unknown defect queue project setting")
}

func parseDefectQueueKey(name string, value interface{}) (field, project string, err error) {
	field, project, err = splitDefectQueueKey(name)
	if err != nil {
		return "", "", err
	}
	_, err = parseDefectQueueValue(name, field, project != "", value)
	return field, project, err
}

// parseDefectQueueValue returns the stored form of one setting: *bool, int,
// []string or string. nil is the clear request, legal only for a project key.
func parseDefectQueueValue(name, field string, isProject bool, value interface{}) (interface{}, error) {
	if value == nil {
		if !isProject {
			return nil, defectQueueErr(name, "a value is required")
		}
		return nil, nil
	}
	switch field {
	case defectFieldEnabled, defectFieldAllowUnrelated, defectFieldTinyEnabled:
		b, err := parseDefectBool(name, value)
		if err != nil {
			return nil, err
		}
		return &b, nil
	case defectFieldSizeCap, defectFieldMaxFiles:
		n, err := parseDefectInt(name, value)
		if err != nil {
			return nil, err
		}
		if n < 1 {
			return nil, defectQueueErr(name, fmt.Sprintf("must be at least 1, got %d", n))
		}
		return n, nil
	case defectFieldPriorityRule:
		s, ok := value.(string)
		r := strings.ToLower(strings.TrimSpace(s))
		if !ok || (r != DefectPriorityHighestMember && r != DefectPriorityFirstMember) {
			return nil, defectQueueErr(name, fmt.Sprintf("must be %q or %q, got %v", DefectPriorityHighestMember, DefectPriorityFirstMember, value))
		}
		return r, nil
	case defectFieldGroupingKeys:
		return parseDefectList(name, value, []string{DefectGroupProjectArea, DefectGroupSharedFiles, DefectGroupSharedSurfaces})
	case defectFieldKinds:
		return parseDefectList(name, value, DefaultDefectTinyFixKinds())
	case defectFieldPathGlobs, defectFieldVisibleGlobs:
		return parseDefectList(name, value, nil)
	}
	return nil, defectQueueErr(name, "unknown defect queue setting")
}

func parseDefectBool(name string, value interface{}) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, defectQueueErr(name, "must be true or false")
}

func parseDefectInt(name string, value interface{}) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		if v == math.Trunc(v) && math.Abs(v) < 1<<31 {
			return int(v), nil
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n, nil
		}
	}
	return 0, defectQueueErr(name, "must be a whole number")
}

// parseDefectList accepts a list ([]string or []interface{} of strings) or a
// comma-separated string. Entries are trimmed and must be non-empty; when
// allowed is non-nil each entry must be one of its values. The list must
// hold at least one entry.
func parseDefectList(name string, value interface{}, allowed []string) ([]string, error) {
	var raw []string
	switch v := value.(type) {
	case []string:
		raw = v
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, defectQueueErr(name, "must be a list of strings")
			}
			raw = append(raw, s)
		}
	case string:
		raw = strings.Split(v, ",")
	default:
		return nil, defectQueueErr(name, "must be a list of strings or a comma-separated string")
	}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			if _, isStr := value.(string); isStr {
				continue // tolerate stray commas in the CSV form
			}
			return nil, defectQueueErr(name, "entries must not be blank")
		}
		if allowed != nil {
			ok := false
			for _, a := range allowed {
				if s == a {
					ok = true
					break
				}
			}
			if !ok {
				return nil, defectQueueErr(name, fmt.Sprintf("unknown entry %q, expected one of %s", s, strings.Join(allowed, ", ")))
			}
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, defectQueueErr(name, "must hold at least one entry")
	}
	return out, nil
}

// ValidateDefectQueue checks a whole block as Load read it: every set field
// is re-run through the setting validator, so a bad value in config.yaml
// refuses startup with the same message a bad write gets.
func (d DefectQueueConfig) ValidateDefectQueue() error {
	var probe DefectQueueConfig
	if d.Enabled != nil {
		if err := probe.Set(defectFieldEnabled, *d.Enabled); err != nil {
			return err
		}
	}
	if err := validateDefectFields("", d.Batch, d.TinyFix, &probe); err != nil {
		return err
	}
	names := make([]string, 0, len(d.Projects))
	for n := range d.Projects {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			return defectQueueErr("projects", "project name must not be empty")
		}
		p := d.Projects[n]
		if err := validateDefectFields("projects."+n+".", p.Batch, p.TinyFix, &probe); err != nil {
			return err
		}
	}
	return nil
}

func validateDefectFields(prefix string, b DefectQueueBatch, t DefectQueueTinyFix, probe *DefectQueueConfig) error {
	set := func(field string, v interface{}) error { return probe.Set(prefix+field, v) }
	if b.SizeCap != 0 {
		if err := set(defectFieldSizeCap, b.SizeCap); err != nil {
			return err
		}
	}
	if b.GroupingKeys != nil {
		if err := set(defectFieldGroupingKeys, b.GroupingKeys); err != nil {
			return err
		}
	}
	if b.AllowUnrelated != nil {
		if err := set(defectFieldAllowUnrelated, *b.AllowUnrelated); err != nil {
			return err
		}
	}
	if b.PriorityRule != "" {
		if err := set(defectFieldPriorityRule, b.PriorityRule); err != nil {
			return err
		}
	}
	if t.Enabled != nil {
		if err := set(defectFieldTinyEnabled, *t.Enabled); err != nil {
			return err
		}
	}
	if t.MaxFiles != 0 {
		if err := set(defectFieldMaxFiles, t.MaxFiles); err != nil {
			return err
		}
	}
	if t.AllowedPathGlobs != nil {
		if err := set(defectFieldPathGlobs, t.AllowedPathGlobs); err != nil {
			return err
		}
	}
	if t.AllowedKinds != nil {
		if err := set(defectFieldKinds, t.AllowedKinds); err != nil {
			return err
		}
	}
	if t.OperatorVisiblePathGlobs != nil {
		if err := set(defectFieldVisibleGlobs, t.OperatorVisiblePathGlobs); err != nil {
			return err
		}
	}
	return nil
}

// loadDefectQueue validates the defect queue block at load time the way
// loadShipped does for its block.
func loadDefectQueue(cfg *Config) error {
	return cfg.Autonomous.DefectQueue.ValidateDefectQueue()
}
