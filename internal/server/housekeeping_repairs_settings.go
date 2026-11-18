package server

// The autonomous.housekeeping_repairs block on the REST config surface. The
// comm configure verb, MCP config_set, CLI config set and the Settings select
// fields all send their value through PUT /api/config, so the message
// firstHousekeepingRepairsSettingError returns is the one every surface shows.

import (
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// housekeepingRepairsSettingsForDisplay renders the effective block for
// GET /api/config: each group's mode and the grace seconds.
func housekeepingRepairsSettingsForDisplay(h config.HousekeepingRepairsConfig) map[string]interface{} {
	e := h.Effective()
	return map[string]interface{}{
		"task_chain":    e.TaskChain,
		"advance_judge": e.AdvanceJudge,
		"lane_fill":     e.LaneFill,
		"leftovers":     e.Leftovers,
		"planning":      e.Planning,
		"grace_seconds": e.GraceSecondsOrDefault(),
	}
}

// firstHousekeepingRepairsSettingError returns the first refusal among a
// config patch's autonomous.housekeeping_repairs keys, or nil. Keys are
// checked in sorted order so the message is stable.
func firstHousekeepingRepairsSettingError(patch map[string]interface{}) error {
	var keys []string
	for k := range patch {
		if strings.HasPrefix(k, config.HousekeepingRepairsSettingPrefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := config.ValidateHousekeepingRepairsSetting(k, patch[k]); err != nil {
			return err
		}
	}
	return nil
}

// applyHousekeepingRepairsKey applies one validated key to the block.
func applyHousekeepingRepairsKey(cfg *config.Config, key string, v interface{}) {
	field := strings.TrimPrefix(key, config.HousekeepingRepairsSettingPrefix)
	if updated, ok := config.SetHousekeepingRepairsField(cfg.Autonomous.HousekeepingRepairs, field, v); ok {
		cfg.Autonomous.HousekeepingRepairs = updated
	}
}
