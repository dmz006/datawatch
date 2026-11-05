package config

import (
	"fmt"
	"strings"
)

// Autonomous auto-tagging. The autonomous.auto_tags block defines the tag
// names datawatch derives for an Automaton: one per Automation, one per
// stage, one per PRD kind. Each is a template that may use the
// {automation}, {stage} and {kind} placeholders (and nothing else), and the
// name rendered from one is always a single non-empty tag name with no
// newline. Kind labels map an Automaton kind (feature, bug, other — see
// autonomous.PRDKindFeature, PRDKindBug and PRDKindOther; the names are
// repeated here so this package does not import internal/autonomous, which
// imports this one) to the tag value {kind} renders as: feature → feature,
// bug → bug and other → maintenance by default, so the automaton-kind
// vocabulary never leaks into tag names.
//
// The block is settable on every configuration channel: PUT /api/config
// {"autonomous.auto_tags.<name>": <value>} (GET /api/config shows the
// effective values under autonomous.auto_tags), datawatch config set
// autonomous.auto_tags.<name> <value>, MCP config_set with key
// autonomous.auto_tags.<name>, comm configure
// autonomous.auto_tags.<name>=<value>, and the web app Settings →
// Automata group. Every surface reaches ValidateAutoTagsSetting, so an
// invalid value is refused with the same message everywhere and nothing is
// written.
//
// An Automation overrides the block field by field with its own auto_tags
// setting (autonomous.AutoTagsOverride): unset fields inherit the daemon's,
// and a nil setting means "the daemon's block applies whole".

// AutoTagsSettingPrefix — the config keys this block owns.
const AutoTagsSettingPrefix = "autonomous.auto_tags."

// AutoTagsSettingFields — the block's fields in order, for help text.
var AutoTagsSettingFields = []string{"enabled", "automation_template", "stage_template", "kind_template", "kind_labels"}

// AutoTags placeholder names.
const (
	autoTagsPlaceholderAutomation = "automation"
	autoTagsPlaceholderStage      = "stage"
	autoTagsPlaceholderKind       = "kind"
)

// AutoTags defaults.
const (
	AutoTagsAutomationTemplateDefault = "automation:{automation}"
	AutoTagsStageTemplateDefault      = "stage:{stage}"
	AutoTagsKindTemplateDefault       = "kind:{kind}"
)

// AutoTagsKindDefaultLabels — the default kind_labels mapping. The
// feature and bug kinds render as themselves; other renders as
// maintenance.
var AutoTagsKindDefaultLabels = map[string]string{
	"feature": "feature",
	"bug":     "bug",
	"other":   "maintenance",
}

// autoTagsPlaceholderNames — the only placeholders a template may contain.
var autoTagsPlaceholderNames = map[string]string{
	autoTagsPlaceholderAutomation: autoTagsPlaceholderAutomation,
	autoTagsPlaceholderStage:      autoTagsPlaceholderStage,
	autoTagsPlaceholderKind:       autoTagsPlaceholderKind,
}

// AutoTagsConfig — the autonomous.auto_tags block.
type AutoTagsConfig struct {
	// Enabled — nil (unset) or true = on (the default); an explicit false
	// persists through Save/Load and turns auto tagging off.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// AutomationTemplate — the tag name per Automation.
	AutomationTemplate string `yaml:"automation_template" json:"automation_template"`
	// StageTemplate — the tag name per stage.
	StageTemplate string `yaml:"stage_template" json:"stage_template"`
	// KindTemplate — the tag name per kind.
	KindTemplate string `yaml:"kind_template" json:"kind_template"`
	// KindLabels — Automaton kind → the tag value {kind} renders as.
	KindLabels map[string]string `yaml:"kind_labels" json:"kind_labels"`
}

// On reports whether auto tagging is enabled (unmarked = true).
func (a AutoTagsConfig) On() bool { return a.Enabled == nil || *a.Enabled }

// Effective returns the block with unset fields filled in.
func (a AutoTagsConfig) Effective() AutoTagsConfig {
	out := a
	if out.AutomationTemplate == "" {
		out.AutomationTemplate = AutoTagsAutomationTemplateDefault
	}
	if out.StageTemplate == "" {
		out.StageTemplate = AutoTagsStageTemplateDefault
	}
	if out.KindTemplate == "" {
		out.KindTemplate = AutoTagsKindTemplateDefault
	}
	labels := make(map[string]string, len(AutoTagsKindDefaultLabels)+len(a.KindLabels))
	for k, v := range AutoTagsKindDefaultLabels {
		labels[k] = v
	}
	for k, v := range a.KindLabels {
		labels[k] = v
	}
	if len(labels) == 0 {
		labels = nil
	}
	out.KindLabels = labels
	return out
}

// Set stores one normalized autonomous.auto_tags field in the config (for
// the PUT /api/config and `datawatch config set` paths); the rejection is the
// reason the value must not be kept. field is the key with the
// autonomous.auto_tags. prefix already stripped; when it is not the block's,
// or it cannot be parsed, ok is false and out is a.
func (a AutoTagsConfig) Set(field string, value interface{}) (out AutoTagsConfig, ok bool) {
	out = a
	switch field {
	case "enabled":
		b, err := parseAutoTagsEnabled(value)
		if err != nil {
			return a, false
		}
		out.Enabled = &b
	case "automation_template":
		s, err := parseAutoTagsTemplate(value)
		if err != nil {
			return a, false
		}
		out.AutomationTemplate = s
	case "stage_template", "kind_template":
		s, err := parseAutoTagsTemplate(value)
		if err != nil {
			return a, false
		}
		if field == "stage_template" {
			out.StageTemplate = s
		} else {
			out.KindTemplate = s
		}
	case "kind_labels":
		m, err := parseAutoTagsKindLabels(value)
		if err != nil {
			return a, false
		}
		out.KindLabels = m
	default:
		return a, false
	}
	return out, true
}

// ValidateAutoTagsSetting checks the one autonomous.auto_tags key a single
// surface is about to set. Rejection is the message the other surfaces show:
// the comm configure, MCP config_set and CLI config set paths all send the
// value through PUT /api/config, so refusing it here keeps every surface on
// the same rule.
func ValidateAutoTagsSetting(key string, value interface{}) error {
	field, ok := strings.CutPrefix(key, AutoTagsSettingPrefix)
	if !ok {
		return fmt.Errorf("%s: not an %s key", key, AutoTagsSettingPrefix+"*")
	}
	if field == "" {
		return fmt.Errorf("%s: name the field to set (or one of %s)", key, strings.Join(AutoTagsSettingFields, ", "))
	}
	switch field {
	case "enabled":
		if _, err := parseAutoTagsEnabled(value); err != nil {
			return fmt.Errorf("%s: %v", key, err)
		}
	case "automation_template", "stage_template", "kind_template":
		if _, err := parseAutoTagsTemplate(value); err != nil {
			return fmt.Errorf("%s: %v", key, err)
		}
	case "kind_labels":
		if _, err := parseAutoTagsKindLabels(value); err != nil {
			return fmt.Errorf("%s: %v", key, err)
		}
	default:
		return fmt.Errorf("%s: must be one of %s", key, strings.Join(AutoTagsSettingFields, ", "))
	}
	return nil
}

// ValidateAutoTagsConfig checks a whole block as read from config or a
// live-patch; called from Load, LoadSecure and the live-patch path.
func ValidateAutoTagsConfig(a AutoTagsConfig) error {
	e := a.Effective()
	for field, tpl := range map[string]string{
		"automation_template": e.AutomationTemplate,
		"stage_template":      e.StageTemplate,
		"kind_template":       e.KindTemplate,
	} {
		if err := ValidateAutoTagsTemplate(tpl); err != nil {
			return fmt.Errorf("autonomous.auto_tags.%s: %v", field, err)
		}
	}
	if err := validateAutoTagsKindLabels(e.KindLabels); err != nil {
		return fmt.Errorf("autonomous.auto_tags.kind_labels: %v", err)
	}
	return nil
}

// loadAutoTags normalizes the block; the live-patch path (PUT /api/config)
// re-uses Set + ValidateAutoTagsSetting so both land on the same rules.
func loadAutoTags(cfg *Config) error {
	cfg.Autonomous.AutoTags = cfg.Autonomous.AutoTags.Effective()
	return ValidateAutoTagsConfig(cfg.Autonomous.AutoTags)
}

// autoTagsValidationVars — standard values for a template's placeholders,
// used to check that a template renders a non-empty, newline-free name.
var autoTagsValidationVars = map[string]string{
	autoTagsPlaceholderAutomation: autoTagsPlaceholderAutomation,
	autoTagsPlaceholderStage:      autoTagsPlaceholderStage,
	autoTagsPlaceholderKind:       autoTagsPlaceholderKind,
}

// RenderAutoTagsTemplate fills a template's {automation}, {stage} and
// {kind} placeholders with vars["automation"], vars["stage"] and
// vars["kind"]. It is deliberately strict: a stray or unknown placeholder is
// an error (templates may only name those three), and the rendered name must
// be non-empty and free of newlines — a tag name is a single-line, non-blank
// string everywhere it is allowed.
func RenderAutoTagsTemplate(tpl string, vars map[string]string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(tpl); {
		if tpl[i] != '{' {
			b.WriteByte(tpl[i])
			i++
			continue
		}
		rest := tpl[i:]
		idx := strings.IndexByte(rest, '}')
		if idx < 0 {
			return "", fmt.Errorf("unmatched '{' in template %q", tpl)
		}
		name := rest[1:idx]
		if _, known := autoTagsPlaceholderNames[name]; !known {
			return "", fmt.Errorf("placeholder {%s} is not one of {automation}, {stage}, {kind}", name)
		}
		b.WriteString(vars[name])
		i += idx + 1
	}
	out := b.String()
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("renders an empty tag name")
	}
	if strings.ContainsAny(out, "\n\r") {
		return "", fmt.Errorf("tag names must not contain a newline")
	}
	return out, nil
}

// ValidateAutoTagsTemplate reports whether tpl is a usable auto-tag
// template: only the {automation}, {stage} and {kind} placeholders appear,
// and it renders a non-empty, newline-free tag name.
func ValidateAutoTagsTemplate(tpl string) error {
	if strings.TrimSpace(tpl) == "" {
		return fmt.Errorf("the template is empty and would render an empty tag name")
	}
	if _, err := RenderAutoTagsTemplate(tpl, autoTagsValidationVars); err != nil {
		return err
	}
	return nil
}

// parseAutoTagsEnabled reports the enabled value a surface is about to set
// (booleans from JSON; the text forms from CLI/comm). Rejection is the
// message the other surfaces show.
func parseAutoTagsEnabled(value interface{}) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	default:
		s, ok := value.(string)
		if !ok {
			return false, fmt.Errorf("enabled must be true or false")
		}
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off":
			return false, nil
		default:
			return false, fmt.Errorf("enabled must be true or false, got %q", s)
		}
	}
}

// parseAutoTagsTemplate reports the template a surface is about to set.
func parseAutoTagsTemplate(value interface{}) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("must be a template string")
	}
	if err := ValidateAutoTagsTemplate(s); err != nil {
		return "", err
	}
	return s, nil
}

// autoTagsKindNames — the known Automaton kinds; the same names live in
// internal/autonomous (PRDKindFeature, PRDKindBug, PRDKindOther) and this
// package must not import that package.
var autoTagsKindNames = map[string]bool{
	"feature": true,
	"bug":     true,
	"other":   true,
}

// validateAutoTagsKindLabels reports whether labels is a usable kind_labels
// mapping: known kinds only, each value non-empty and newline-free.
func validateAutoTagsKindLabels(labels map[string]string) error {
	for k, v := range labels {
		if !autoTagsKindNames[k] {
			return fmt.Errorf("%q is not a kind (use feature, bug or other)", k)
		}
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("the label for %s must not be empty", k)
		}
		if strings.ContainsAny(v, "\n\r") {
			return fmt.Errorf("the label for %s must not contain a newline", k)
		}
	}
	return nil
}

// parseAutoTagsKindLabels reports the kind_labels mapping a surface is about
// to set. It accepts the map form (JSON) and the "kind:value,kind:value" form
// the text surfaces send.
func parseAutoTagsKindLabels(value interface{}) (map[string]string, error) {
	var raw map[string]interface{}
	switch v := value.(type) {
	case map[string]string:
		raw = make(map[string]interface{}, len(v))
		for k, s := range v {
			raw[k] = s
		}
	case map[interface{}]interface{}:
		raw = make(map[string]interface{}, len(v))
		for k, s := range v {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("kind names must be strings")
			}
			raw[ks] = s
		}
	case map[string]interface{}:
		raw = v
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return map[string]string{}, nil
		}
		if !strings.ContainsRune(s, ':') {
			return nil, fmt.Errorf("kind_labels must be a mapping of kinds to label strings")
		}
		raw = make(map[string]interface{})
		for _, part := range strings.Split(s, ",") {
			k, v, ok := strings.Cut(strings.TrimSpace(part), ":")
			if !ok || strings.TrimSpace(k) == "" {
				return nil, fmt.Errorf("kind_labels entry %q must look like feature:feature", strings.TrimSpace(part))
			}
			raw[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	default:
		return nil, fmt.Errorf("kind_labels must be a mapping of kinds to label strings")
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("the label for %s must be a string", k)
		}
		out[k] = s
	}
	if err := validateAutoTagsKindLabels(out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return map[string]string{}, nil
	}
	return out, nil
}
