package autonomous

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// AutoTagsOverride is the per-Automation override of the daemon-level
// autonomous.auto_tags block. A nil override on an Automation means the
// daemon's block applies whole; when set, each unset field inherits from the
// daemon's. kind_labels stay daemon-global: kind labels are a shared
// vocabulary (feature → feature, bug → bug, other → maintenance) that should
// not differ between Automations, so the override carries only the three
// templates and the enable flag.

// AutoTagsOverride — the per-Automation override (see AutoTagsOverride above).
type AutoTagsOverride struct {
	// Enabled — when false, auto tagging is off for this Automation no matter
	// the daemon's block; nil (unset) inherits the daemon's setting.
	Enabled *bool `json:"enabled,omitempty"`
	// AutomationTemplate — override for the daemon's automation_template.
	AutomationTemplate string `json:"automation_template,omitempty"`
	// StageTemplate — override for the daemon's stage_template.
	StageTemplate string `json:"stage_template,omitempty"`
	// KindTemplate — override for the daemon's kind_template.
	KindTemplate string `json:"kind_template,omitempty"`
}

// ResolvedAutoTags is a fully-resolved auto-tagging setting: the daemon's
// autonomous.auto_tags block with an Automation's override layered on top.
type ResolvedAutoTags struct {
	Enabled            bool
	AutomationTemplate string
	StageTemplate      string
	KindTemplate       string
	KindLabels         map[string]string
}

// ResolveAutoTags merges the daemon's autonomous.auto_tags block and an
// Automation's override: an override that is nil keeps the daemon's block,
// and each of its unset fields (empty template, nil Enabled) inherits the
// daemon's. kind_labels are always the daemon's.
func ResolveAutoTags(daemonCfg config.AutoTagsConfig, override *AutoTagsOverride) ResolvedAutoTags {
	d := daemonCfg.Effective()
	out := ResolvedAutoTags{
		Enabled:            d.On(),
		AutomationTemplate: d.AutomationTemplate,
		StageTemplate:      d.StageTemplate,
		KindTemplate:       d.KindTemplate,
		KindLabels:         d.KindLabels,
	}
	if override == nil {
		return out
	}
	if override.Enabled != nil {
		out.Enabled = *override.Enabled
	}
	if override.AutomationTemplate != "" {
		out.AutomationTemplate = override.AutomationTemplate
	}
	if override.StageTemplate != "" {
		out.StageTemplate = override.StageTemplate
	}
	if override.KindTemplate != "" {
		out.KindTemplate = override.KindTemplate
	}
	if out.KindLabels == nil {
		out.KindLabels = map[string]string{}
	}
	return out
}

// validateAutoTagsOverride reports the override's problems; called from
// validateAutomation for the Automation and its members. An empty template
// is fine (it inherits the daemon's); a template that is present must be a
// usable one, so a typo in the placeholder is stored-loud, not applied-quiet.
func validateAutoTagsOverride(o *AutoTagsOverride) error {
	if o == nil {
		return nil
	}
	for field, tpl := range map[string]string{
		"automation_template": o.AutomationTemplate,
		"stage_template":      o.StageTemplate,
		"kind_template":       o.KindTemplate,
	} {
		if tpl == "" {
			continue
		}
		if err := config.ValidateAutoTagsTemplate(tpl); err != nil {
			return fmt.Errorf("auto_tags.%s: %v", field, err)
		}
	}
	return nil
}

// autoTagKindLabel is the tag value {kind} renders as for an Automaton kind:
// the daemon's kind_labels for that kind, then the kind's default label
// (feature → feature, bug → bug, other → maintenance), then the kind itself.
func autoTagKindLabel(resolved ResolvedAutoTags, kind string) string {
	if v, ok := resolved.KindLabels[kind]; ok {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	if v, ok := config.AutoTagsKindDefaultLabels[kind]; ok {
		return v
	}
	return kind
}

// RenderAutoTag renders one auto tag: what is "automation" (the Automation),
// "stage" or "kind". vars supplies the concrete automation/stage/kind names;
// a missing variable renders as the empty string, and the rendered name must
// then still be non-empty and newline-free (validated with the same rules as
// the config block, so a template that only renders empty when the concrete
// value is empty is stored-loud here as well).
func RenderAutoTag(resolved ResolvedAutoTags, what string, vars map[string]string) (string, error) {
	if !resolved.Enabled {
		return "", fmt.Errorf("auto tagging is not enabled")
	}
	if vars == nil {
		vars = map[string]string{}
	}
	var tpl string
	switch what {
	case "automation":
		tpl = resolved.AutomationTemplate
	case "stage":
		tpl = resolved.StageTemplate
	case "kind":
		tpl = resolved.KindTemplate
		vars["kind"] = autoTagKindLabel(resolved, vars["kind"])
	default:
		return "", fmt.Errorf("%q is not an auto tag (use automation, stage or kind)", what)
	}
	return config.RenderAutoTagsTemplate(tpl, vars)
}
