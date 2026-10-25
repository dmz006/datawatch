package config

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
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

// parseTagSetting returns the stored form of one setting: a string, a []string,
// a bool or an int, depending on the setting.
func parseTagSetting(name string, value interface{}) (interface{}, error) {
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
