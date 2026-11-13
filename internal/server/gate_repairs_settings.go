package server

// The autonomous.gate_repair block on the REST config surface. The comm
// configure verb, MCP config_set, CLI config set and the Settings select
// fields all send their value through PUT /api/config, so the message
// firstGateRepairSettingError returns is the one every surface shows.

import (
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// gateRepairSettingsForDisplay renders the effective block for GET
// /api/config: the engine mode (off, report or on).
func gateRepairSettingsForDisplay(g config.GateRepairConfig) map[string]interface{} {
	return map[string]interface{}{
		"mode": g.Effective().Mode,
	}
}

// firstGateRepairSettingError returns the first refusal among a config patch's
// autonomous.gate_repair keys, or nil. Keys are checked in sorted order so
// the message is stable.
func firstGateRepairSettingError(patch map[string]interface{}) error {
	var keys []string
	for k := range patch {
		if strings.HasPrefix(k, config.GateRepairSettingPrefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := config.ValidateGateRepairSetting(k, patch[k]); err != nil {
			return err
		}
	}
	return nil
}

// applyGateRepairKey applies one validated key to the block.
func applyGateRepairKey(cfg *config.Config, key string, v interface{}) {
	field := strings.TrimPrefix(key, config.GateRepairSettingPrefix)
	if updated, ok := cfg.Autonomous.GateRepair.SetGateRepairField(field, v); ok {
		cfg.Autonomous.GateRepair = updated
	}
}
