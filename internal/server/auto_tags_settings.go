package server

// The autonomous.auto_tags block on the REST config surface. The comm
// configure, MCP config_set and CLI config set paths all send their value
// through PUT /api/config, so the message firstAutoTagsSettingError returns
// is the one every surface shows.

import (
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// autoTagsSettingsForDisplay renders the effective autonomous.auto_tags block
// for GET /api/config.
func autoTagsSettingsForDisplay(a config.AutoTagsConfig) map[string]interface{} {
	e := a.Effective()
	enabled := true
	if e.Enabled != nil {
		enabled = *e.Enabled
	}
	return map[string]interface{}{
		"enabled":             enabled,
		"automation_template": e.AutomationTemplate,
		"stage_template":      e.StageTemplate,
		"kind_template":       e.KindTemplate,
		"kind_labels":         e.KindLabels,
	}
}

// firstAutoTagsSettingError returns the first refusal among a config patch's
// autonomous.auto_tags keys, or nil.
func firstAutoTagsSettingError(patch map[string]interface{}) error {
	var first error
	for k, v := range patch {
		if !strings.HasPrefix(k, config.AutoTagsSettingPrefix) {
			continue
		}
		if err := config.ValidateAutoTagsSetting(k, v); err != nil {
			if first == nil {
				first = err
			}
		}
	}
	return first
}
