package server

import (
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// defectQueueSettingsForDisplay renders the autonomous.defect_queue block for
// GET /api/config: the effective block-level values (so an unset block shows
// its defaults) plus each project's raw overrides.
func defectQueueSettingsForDisplay(d config.DefectQueueConfig) map[string]interface{} {
	projects := map[string]interface{}{}
	for name, p := range d.Projects {
		projects[name] = p
	}
	return map[string]interface{}{
		"enabled":  d.EffectiveEnabled(),
		"batch":    d.EffectiveBatch(),
		"tiny_fix": d.EffectiveTinyFix(),
		"projects": projects,
	}
}

// firstDefectQueueSettingError returns the first refusal among the
// autonomous.defect_queue keys of a PUT /api/config patch, checked in key
// order so the reported error is stable. It returns nil when every such key
// is valid or none is present.
func firstDefectQueueSettingError(patch map[string]interface{}) error {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		if strings.HasPrefix(k, config.DefectQueueSettingPrefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := config.ValidateDefectQueueSetting(k, patch[k]); err != nil {
			return err
		}
	}
	return nil
}
