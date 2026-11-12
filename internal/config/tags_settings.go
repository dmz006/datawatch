package config

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/placementtags"
)

// The autonomous.tags block configures the Automata tag browser: how it opens,
// which Automaton statuses join the attention group, how a tag selection
// matches, and how the shared tag registry is kept. Every configuration surface
// (PUT /api/config, the comm channel's configure, MCP config_set and the CLI's
// config set) reaches ValidateTagSetting through the REST handler, and Load
// validates the block at startup, so a refused value carries the same message
// everywhere.

// TagSettingPrefix is the dotted key prefix shared by every tag setting.
const TagSettingPrefix = "autonomous.tags."

// Accepted values of the tag browser settings.
const (
	TagDefaultViewAuto      = "auto"
	TagDefaultViewAttention = "attention"
	TagDefaultViewTags      = "tags"

	TagMatchAny = "any"
	TagMatchAll = "all"

	// TagRegistrySyncMinSeconds is the shortest registry sync interval accepted.
	TagRegistrySyncMinSeconds = 5
	// TagRegistrySyncDefaultSeconds is the registry sync interval when unset.
	TagRegistrySyncDefaultSeconds = 30
)

// DefaultTagAttentionStatuses are the Automaton statuses that put an Automaton
// in the attention group when attention_statuses is unset.
var DefaultTagAttentionStatuses = []string{"blocked", "needs_review", "paused"}

// TagAutomatonStatuses lists every Automaton status an attention list may name.
// internal/autonomous holds the status constants; its tests keep this list in
// step with them.
var TagAutomatonStatuses = []string{
	"draft", "planning", "needs_review", "approved", "revisions_asked",
	"running", "completed", "rejected", "cancelled", "archived",
	"blocked", "failed", "paused",
}

// TagsConfig is the autonomous.tags block. No field is omitempty, so an
// explicit false or empty value survives a save and load; Load merges over
// DefaultConfig, so an absent key keeps its default.
type TagsConfig struct {
	// DefaultView is the view the Automata tab opens on: auto (the attention
	// view when at least one item needs attention, else the tag browser),
	// attention, or tags. Default auto.
	DefaultView string `yaml:"default_view" json:"default_view"`
	// AttentionStatuses are the Automaton statuses that put an Automaton in
	// the attention group. Every entry must be an Automaton status. Default
	// blocked, needs_review and paused.
	AttentionStatuses []string `yaml:"attention_statuses" json:"attention_statuses"`
	// DefaultMatchMode is how a selection of several tags matches: any (the
	// list widens) or all (the list narrows). Default any.
	DefaultMatchMode string `yaml:"default_match_mode" json:"default_match_mode"`
	// DirectCountsVisible shows each tag's direct count as a tooltip beside
	// its subtree count. Default false.
	DirectCountsVisible bool `yaml:"direct_counts_visible" json:"direct_counts_visible"`
	// RegistryKeeper is the federation peer name that keeps the shared tag
	// registry. Empty means this daemon is its own keeper and nothing syncs.
	RegistryKeeper string `yaml:"registry_keeper" json:"registry_keeper"`
	// RegistrySyncSeconds is the whole number of seconds (>= 5) between
	// registry sync checks. Default 30.
	RegistrySyncSeconds int `yaml:"registry_sync_seconds" json:"registry_sync_seconds"`
	// Placement configures the placement tags: automatic tags that record
	// where a piece of work ran, one tag per enabled axis. See
	// PlacementConfig; an unset block takes DefaultPlacementConfig's values.
	Placement PlacementConfig `yaml:"placement" json:"placement"`
}

// PlacementAxis configures one built-in placement axis under
// autonomous.tags.placement: whether the axis is on, and the template its tag
// is built from. The template must contain the axis's placeholder ({node} for
// compute, {peer} for peer, {backend} for backend, {lane} for lane, {role} for
// role, {project} for project, {automaton} for automaton, {automation} for
// automation, {stage} for stage, {kind} for kind), which is replaced with the
// fact value.
type PlacementAxis struct {
	// Enabled turns the axis on or off. A disabled axis never yields a tag.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Template is the tag template; it must contain the axis's placeholder.
	Template string `yaml:"template" json:"template"`
}

// PlacementCustomAxis configures one user-defined placement axis. It always
// uses the {value} placeholder and reads its value from the session field
// named in Field.
type PlacementCustomAxis struct {
	// Name labels the axis. Lowercase letters, digits and hyphens only,
	// unique across custom axes, and not a built-in axis name.
	Name string `yaml:"name" json:"name"`
	// Field is the session field the axis reads. One of
	// placementtags.SessionFields: Name, Backend, Model, Node, PRDID, Role,
	// Project, Automaton, Automation, Stage, Kind.
	Field string `yaml:"field" json:"field"`
	// Template is the tag template; it must contain the {value} placeholder.
	Template string `yaml:"template" json:"template"`
	// Enabled turns the axis on or off.
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// PlacementConfig is the autonomous.tags.placement block.
//
// Enabled is the master switch: when false no placement tags are derived at
// all. One PlacementAxis covers each built-in axis; Custom holds
// user-defined axes. A piece of work that is missing an axis's fact value
// still yields no tag for that axis.
type PlacementConfig struct {
	// Enabled is the master switch. Default true.
	Enabled bool          `yaml:"enabled" json:"enabled"`
	Compute PlacementAxis `yaml:"compute" json:"compute"`
	Peer    PlacementAxis `yaml:"peer" json:"peer"`
	Backend PlacementAxis `yaml:"backend" json:"backend"`
	Lane    PlacementAxis `yaml:"lane" json:"lane"`
	Role    PlacementAxis `yaml:"role" json:"role"`
	Project PlacementAxis `yaml:"project" json:"project"`
	// Automaton tags the Automaton the work belongs to.
	Automaton PlacementAxis `yaml:"automaton" json:"automaton"`
	// Automation tags the Automation the work runs under.
	Automation PlacementAxis `yaml:"automation" json:"automation"`
	// Stage tags the pipeline stage the work is at.
	Stage PlacementAxis `yaml:"stage" json:"stage"`
	// Kind tags the work kind.
	Kind PlacementAxis `yaml:"kind" json:"kind"`
	// Custom holds user-defined axes, empty by default.
	Custom []PlacementCustomAxis `yaml:"custom" json:"custom"`
}

// defaultPlacementAxis builds one built-in axis's default: on with the
// conventional "<axis>:<placeholder>" template. Every built-in axis, project
// included, is on by default.
func defaultPlacementAxis(name string) PlacementAxis {
	template, _ := placementtags.DefaultTemplate(name)
	return PlacementAxis{Enabled: true, Template: template}
}

// DefaultPlacementConfig returns the placement block's defaults: the master
// switch on; every built-in axis (compute, peer, backend, lane, role,
// project, automaton, automation, stage and kind) on, each with its
// conventional template; no custom axes.
func DefaultPlacementConfig() PlacementConfig {
	return PlacementConfig{
		Enabled:    true,
		Compute:    defaultPlacementAxis("compute"),
		Peer:       defaultPlacementAxis("peer"),
		Backend:    defaultPlacementAxis("backend"),
		Lane:       defaultPlacementAxis("lane"),
		Role:       defaultPlacementAxis("role"),
		Project:    defaultPlacementAxis("project"),
		Automaton:  defaultPlacementAxis("automaton"),
		Automation: defaultPlacementAxis("automation"),
		Stage:      defaultPlacementAxis("stage"),
		Kind:       defaultPlacementAxis("kind"),
	}
}

// EffectivePlacement returns p with every axis that was never set (an empty
// template marks an unset axis) replaced by its default; an unset block (no
// axis template and no custom axes) takes all defaults, master switch
// included.
func (p PlacementConfig) EffectivePlacement() PlacementConfig {
	out := p
	if p.Compute.Template == "" && p.Peer.Template == "" && p.Backend.Template == "" &&
		p.Lane.Template == "" && p.Role.Template == "" && p.Project.Template == "" &&
		p.Automaton.Template == "" && p.Automation.Template == "" && p.Stage.Template == "" &&
		p.Kind.Template == "" && len(p.Custom) == 0 {
		return DefaultPlacementConfig()
	}
	defaultAxes := []struct {
		ax  *PlacementAxis
		def PlacementAxis
	}{
		{ax: &out.Compute, def: defaultPlacementAxis("compute")},
		{ax: &out.Peer, def: defaultPlacementAxis("peer")},
		{ax: &out.Backend, def: defaultPlacementAxis("backend")},
		{ax: &out.Lane, def: defaultPlacementAxis("lane")},
		{ax: &out.Role, def: defaultPlacementAxis("role")},
		{ax: &out.Project, def: defaultPlacementAxis("project")},
		{ax: &out.Automaton, def: defaultPlacementAxis("automaton")},
		{ax: &out.Automation, def: defaultPlacementAxis("automation")},
		{ax: &out.Stage, def: defaultPlacementAxis("stage")},
		{ax: &out.Kind, def: defaultPlacementAxis("kind")},
	}
	for _, e := range defaultAxes {
		if e.ax.Template == "" {
			*e.ax = e.def
		}
	}
	return out
}

// Effective returns t with each setting that has a default and is unset
// (empty or zero) replaced by that default. The stored value is not changed.
func (t TagsConfig) Effective() TagsConfig {
	out := t
	if out.DefaultView == "" {
		out.DefaultView = TagDefaultViewAuto
	}
	if len(out.AttentionStatuses) == 0 {
		out.AttentionStatuses = append([]string(nil), DefaultTagAttentionStatuses...)
	}
	if out.DefaultMatchMode == "" {
		out.DefaultMatchMode = TagMatchAny
	}
	if out.RegistrySyncSeconds <= 0 {
		out.RegistrySyncSeconds = TagRegistrySyncDefaultSeconds
	}
	out.Placement = out.Placement.EffectivePlacement()
	return out
}

// ValidateTagSetting checks one setting. key is the full dotted key, for example
// autonomous.tags.default_view; a key outside the tag block is not its concern
// and returns nil. value is what a PUT /api/config patch carries (a string, a
// number, a bool or a list), and the comm, MCP and CLI surfaces send the same
// values as strings, which are accepted too. The returned error text is the
// message every surface shows.
func ValidateTagSetting(key string, value interface{}) error {
	name, ok := strings.CutPrefix(key, TagSettingPrefix)
	if !ok {
		return nil
	}
	_, err := parseTagSetting(name, value)
	return err
}

// Set validates one setting and stores it. name is the key without the
// autonomous.tags. prefix. On error t is unchanged.
func (t *TagsConfig) Set(name string, value interface{}) error {
	v, err := parseTagSetting(name, value)
	if err != nil {
		return err
	}
	if name == "placement.enabled" {
		t.Placement.Enabled = v.(bool)
		return nil
	}
	if axis, field, ok := splitPlacementAxisKey(name); ok {
		setting := t.PlacementAxisSetting(axis)
		if setting == nil {
			return tagSettingErr(name, "unknown placement axis")
		}
		switch field {
		case "enabled":
			setting.Enabled = v.(bool)
		case "template":
			setting.Template = v.(string)
		}
		return nil
	}
	switch name {
	case "default_view":
		t.DefaultView = v.(string)
	case "attention_statuses":
		t.AttentionStatuses = v.([]string)
	case "default_match_mode":
		t.DefaultMatchMode = v.(string)
	case "direct_counts_visible":
		t.DirectCountsVisible = v.(bool)
	case "registry_keeper":
		t.RegistryKeeper = v.(string)
	case "registry_sync_seconds":
		t.RegistrySyncSeconds = v.(int)
	}
	return nil
}

// loadTags settles the tag block as Load read it: an empty or zero value takes
// its default (the same rule Effective applies everywhere else), then the
// block is checked, so a non-empty bad value in config.yaml refuses startup the
// way other config errors do. A config saved from a zero-valued struct therefore
// reloads cleanly.
func loadTags(cfg *Config) error {
	cfg.Autonomous.Tags = cfg.Autonomous.Tags.Effective()
	return validateLoadedTags(cfg.Autonomous.Tags)
}

// validateLoadedTags checks every setting of the block.
func validateLoadedTags(t TagsConfig) error {
	if err := validatePlacement(t.Placement); err != nil {
		return err
	}
	fields := map[string]interface{}{
		"default_view":          t.DefaultView,
		"attention_statuses":    t.AttentionStatuses,
		"default_match_mode":    t.DefaultMatchMode,
		"direct_counts_visible": t.DirectCountsVisible,
		"registry_keeper":       t.RegistryKeeper,
		"registry_sync_seconds": t.RegistrySyncSeconds,
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ValidateTagSetting(TagSettingPrefix+name, fields[name]); err != nil {
			return err
		}
	}
	return nil
}

// validatePlacement checks the placement block: every built-in template must
// carry its own placeholder, and every custom axis a well-formed unique name
// that is not a built-in axis, an allowed session field, and a template that
// carries {value}. The master switch does not gate these checks, so a bad
// value cannot wait in storage for the block to be switched back on.
func validatePlacement(p PlacementConfig) error {
	axes := []struct {
		name string
		ax   PlacementAxis
	}{
		{"compute", p.Compute},
		{"peer", p.Peer},
		{"backend", p.Backend},
		{"lane", p.Lane},
		{"role", p.Role},
		{"project", p.Project},
		{"automaton", p.Automaton},
		{"automation", p.Automation},
		{"stage", p.Stage},
		{"kind", p.Kind},
	}
	for _, a := range axes {
		placeholder := placementtags.AxisPlaceholders[a.name]
		if !strings.Contains(a.ax.Template, placeholder) {
			return fmt.Errorf("%splacement.%s: template %q must contain %s",
				TagSettingPrefix, a.name, a.ax.Template, placeholder)
		}
	}
	seen := map[string]bool{}
	for _, c := range p.Custom {
		if !placementCustomNameOK(c.Name) {
			return fmt.Errorf("%splacement.custom: name %q must be lowercase letters, digits and hyphens",
				TagSettingPrefix, c.Name)
		}
		if seen[c.Name] {
			return fmt.Errorf("%splacement.custom: name %q is used more than once",
				TagSettingPrefix, c.Name)
		}
		seen[c.Name] = true
		if _, ok := placementtags.AxisPlaceholders[c.Name]; ok {
			return fmt.Errorf("%splacement.custom: name %q collides with a built-in placement axis",
				TagSettingPrefix, c.Name)
		}
		if !placementtags.IsSessionField(c.Field) {
			return fmt.Errorf("%splacement.custom: field %q must be one of %s",
				TagSettingPrefix, c.Field, strings.Join(placementtags.SessionFields, ", "))
		}
		if !strings.Contains(c.Template, placementtags.CustomPlaceholder) {
			return fmt.Errorf("%splacement.custom: template %q must contain %s",
				TagSettingPrefix, c.Template, placementtags.CustomPlaceholder)
		}
	}
	return nil
}

// placementCustomNameOK accepts a name made of lowercase letters, digits and
// hyphens; an empty name or any other character is refused.
func placementCustomNameOK(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// parseTagSetting returns the stored form of one setting: a string, a []string,
// a bool or an int, depending on the setting.
func parseTagSetting(name string, value interface{}) (interface{}, error) {
	if name == "placement.enabled" {
		return parsePlacementBool(name, value)
	}
	if axis, field, ok := splitPlacementAxisKey(name); ok {
		return parsePlacementAxisSetting(name, axis, field, value)
	}
	switch name {
	case "default_view":
		if isBlankTagValue(value) {
			return TagDefaultViewAuto, nil
		}
		return tagChoice(name, value, "auto, attention or tags",
			TagDefaultViewAuto, TagDefaultViewAttention, TagDefaultViewTags)
	case "default_match_mode":
		if isBlankTagValue(value) {
			return TagMatchAny, nil
		}
		return tagChoice(name, value, "any or all", TagMatchAny, TagMatchAll)
	case "attention_statuses":
		return parseTagStatuses(value)
	case "direct_counts_visible":
		return parseTagBool(value)
	case "registry_keeper":
		return parseTagPeer(value)
	case "registry_sync_seconds":
		return parseTagSyncSeconds(value)
	}
	return nil, tagSettingErr(name, "unknown tag setting")
}

// splitPlacementAxisKey reads a per-axis placement key, one of
//
//	placement.<axis>.enabled — is <axis>'s placement tag derived at all
//	placement.<axis>.template — the tag name <axis> derives
//
// where <axis> is a built-in axis and <field> is enabled or template. The
// master key placement.enabled and the custom axes are not matched here (the
// custom axes are list-valued and take no per-key surface). Anything else
// reports false so the usual unknown-setting refusal applies.
func splitPlacementAxisKey(name string) (axis, field string, ok bool) {
	rest, found := strings.CutPrefix(name, "placement.")
	if !found || !strings.Contains(rest, ".") {
		return "", "", false
	}
	axis, field, _ = strings.Cut(rest, ".")
	if axis == "" || (field != "enabled" && field != "template") {
		return "", "", false
	}
	if _, known := placementtags.AxisPlaceholders[axis]; !known {
		return "", "", false
	}
	return axis, field, true
}

// PlacementAxisSetting returns the built-in axis config that name names, so
// Set can write placement.<axis>.<field> through one place. It is nil when
// name is not a built-in axis.
func (t *TagsConfig) PlacementAxisSetting(axis string) *PlacementAxis {
	switch axis {
	case "compute":
		return &t.Placement.Compute
	case "peer":
		return &t.Placement.Peer
	case "backend":
		return &t.Placement.Backend
	case "lane":
		return &t.Placement.Lane
	case "role":
		return &t.Placement.Role
	case "project":
		return &t.Placement.Project
	case "automaton":
		return &t.Placement.Automaton
	case "automation":
		return &t.Placement.Automation
	case "stage":
		return &t.Placement.Stage
	case "kind":
		return &t.Placement.Kind
	}
	return nil
}

// parsePlacementBool accepts a JSON bool, or the strings true and false. It
// is the placement-keyed twin of parseTagBool, carrying the key's own name in
// the refusal.
func parsePlacementBool(name string, value interface{}) (interface{}, error) {
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
	return nil, tagSettingErr(name, "must be true or false")
}

// parsePlacementAxisSetting parses one value of a placement.<axis>.<field>
// key. enabled accepts true or false; template must be a non-empty tag name
// carrying the axis's own placeholder. The refusal names the axis, the same
// way validatePlacement does, so a bad value is refused the same whether it
// arrives keyed or in the YAML block.
func parsePlacementAxisSetting(name, axis, field string, value interface{}) (interface{}, error) {
	if field == "enabled" {
		return parsePlacementBool(name, value)
	}
	s, _ := value.(string)
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, tagSettingErr(name, "must be a tag template")
	}
	placeholder := placementtags.AxisPlaceholders[axis]
	if !strings.Contains(s, placeholder) {
		return nil, fmt.Errorf("%splacement.%s: template %q must contain %s", TagSettingPrefix, axis, s, placeholder)
	}
	return s, nil
}

func tagSettingErr(name, reason string) error {
	return fmt.Errorf("%s%s: %s", TagSettingPrefix, name, reason)
}

// tagChoice accepts one of the allowed strings, case-insensitively.
func tagChoice(name string, value interface{}, want string, allowed ...string) (interface{}, error) {
	if s, ok := value.(string); ok {
		s = strings.ToLower(strings.TrimSpace(s))
		for _, a := range allowed {
			if s == a {
				return s, nil
			}
		}
	}
	return nil, tagSettingErr(name, "must be "+want)
}

// isBlankTagValue reports an empty string, which takes the setting's default.
func isBlankTagValue(value interface{}) bool {
	s, ok := value.(string)
	return ok && strings.TrimSpace(s) == ""
}

// parseTagStatuses accepts a list, or a comma-separated string. Entries are
// trimmed, lower-cased and de-duplicated; an empty entry is skipped. Any other
// entry is refused. A list with no entry left takes the default statuses, the
// same fallback the attention resolver applies.
func parseTagStatuses(value interface{}) (interface{}, error) {
	var raw []string
	switch v := value.(type) {
	case string:
		raw = strings.Split(v, ",")
	case []string:
		raw = v
	case []interface{}:
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return nil, tagSettingErr("attention_statuses", "must be a list of Automaton statuses")
			}
			raw = append(raw, s)
		}
	default:
		return nil, tagSettingErr("attention_statuses", "must be a list of Automaton statuses")
	}
	var out []string
	seen := map[string]bool{}
	for _, s := range raw {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		if !isTagAutomatonStatus(s) {
			return nil, tagSettingErr("attention_statuses",
				fmt.Sprintf("unknown Automaton status %q (valid: %s)", s, strings.Join(TagAutomatonStatuses, ", ")))
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		return append([]string(nil), DefaultTagAttentionStatuses...), nil
	}
	return out, nil
}

func isTagAutomatonStatus(s string) bool {
	for _, v := range TagAutomatonStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// parseTagBool accepts a JSON bool, or the strings true and false.
func parseTagBool(value interface{}) (interface{}, error) {
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
	return nil, tagSettingErr("direct_counts_visible", "must be true or false")
}

// parseTagPeer accepts a peer name. A number is taken as its digits, so a peer
// named 42 is not refused by a surface that sends it unquoted.
func parseTagPeer(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v), nil
	case float64:
		if v == math.Trunc(v) {
			return strconv.FormatFloat(v, 'f', -1, 64), nil
		}
	case int:
		return strconv.Itoa(v), nil
	}
	return nil, tagSettingErr("registry_keeper", "must be a federation peer name, or empty for this daemon")
}

// parseTagSyncSeconds accepts a whole number of seconds >= 5, as a number or a
// numeric string.
func parseTagSyncSeconds(value interface{}) (interface{}, error) {
	n, ok := wholeNumber(value)
	if !ok || n < TagRegistrySyncMinSeconds {
		return nil, tagSettingErr("registry_sync_seconds", "must be a whole number of seconds >= 5")
	}
	return n, nil
}

// wholeNumber reads a whole number from a JSON number, an int or a numeric string.
func wholeNumber(value interface{}) (int, bool) {
	switch v := value.(type) {
	case float64:
		if v != math.Trunc(v) || math.Abs(v) > math.MaxInt32 {
			return 0, false
		}
		return int(v), true
	case int:
		return v, true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil
	}
	return 0, false
}
