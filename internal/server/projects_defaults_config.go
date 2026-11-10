package server

import (
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// projectsDefaultsForDisplay renders the projects_defaults block for GET
// /api/config with the effective values, so an unset field shows its default.
// Branch retention and weekly share read 0 when unset.
func projectsDefaultsForDisplay(d config.ProjectsDefaultsConfig) map[string]interface{} {
	return map[string]interface{}{
		"branch_retention_days": d.BranchRetentionDays,
		"weekly_share_percent":  d.WeeklySharePercent,
		"major_trigger":         d.EffectiveMajorTrigger(),
	}
}

// firstProjectsDefaultsSettingError returns the first refusal among the
// projects_defaults keys of a PUT /api/config patch, checked in key order so
// the reported error is stable. It returns nil when every such key is valid
// or none is present.
func firstProjectsDefaultsSettingError(patch map[string]interface{}) error {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		if strings.HasPrefix(k, config.ProjectsDefaultsSettingPrefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := config.ValidateProjectsDefaultsSetting(k, patch[k]); err != nil {
			return err
		}
	}
	return nil
}
