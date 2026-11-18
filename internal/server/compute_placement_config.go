// Compute placement configuration surface: the top-level compute_placement
// block on GET /api/config and PUT /api/config. Every key is writable at
// runtime and reads through its default when unset.
package server

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// The five compute_placement.* config keys (the top-level block).
const (
	keyComputePlacementMissingModel      = "compute_placement.missing_model"
	keyComputePlacementMemHeadroomPct    = "compute_placement.mem_headroom_pct"
	keyComputePlacementDiskHeadroomGB    = "compute_placement.disk_headroom_gb"
	keyComputePlacementAskTimeoutMinutes = "compute_placement.ask_timeout_minutes"
	keyComputePlacementAssumeModelSizeGB = "compute_placement.assume_model_size_gb"
)

// computePlacementKeys is the full list of compute_placement keys.
var computePlacementKeys = []string{
	keyComputePlacementMissingModel,
	keyComputePlacementMemHeadroomPct,
	keyComputePlacementDiskHeadroomGB,
	keyComputePlacementAskTimeoutMinutes,
	keyComputePlacementAssumeModelSizeGB,
}

// computePlacementSettings is the compute_placement block as it appears on
// the GET surface.
type computePlacementSettings struct {
	MissingModel      string `json:"missing_model"`
	MemHeadroomPct    int    `json:"mem_headroom_pct"`
	DiskHeadroomGB    int    `json:"disk_headroom_gb"`
	AskTimeoutMinutes int    `json:"ask_timeout_minutes"`
	AssumeModelSizeGB int    `json:"assume_model_size_gb"`

	MissingModelVocab []string `json:"missing_model_vocab"`
}

// computePlacementConfigSurface renders the live compute_placement block,
// with defaults read through where unset.
func computePlacementConfigSurface(c config.ComputePlacementConfig) computePlacementSettings {
	return computePlacementSettings{
		MissingModel:      c.EffectiveMissingModel(),
		MemHeadroomPct:    c.MemHeadroomPct,
		DiskHeadroomGB:    c.DiskHeadroomGB,
		AskTimeoutMinutes: c.AskTimeoutMinutes,
		AssumeModelSizeGB: c.AssumeModelSizeGB,
		MissingModelVocab: config.ComputePlacementMissingModels(),
	}
}

// validateComputePlacementPatch checks the compute_placement keys named in
// a config patch, as a group: the missing_model value, the headrooms and
// timeouts as whole numbers with per-key ranges (mem_headroom_pct 0-100,
// the rest 0 or more). A single invalid key rejects the whole PUT, and the
// error names the key.
func validateComputePlacementPatch(patch map[string]any) error {
	for _, key := range computePlacementKeys {
		v, ok := patch[key]
		if !ok {
			continue
		}
		c := config.ComputePlacementConfig{}
		if _, err := applyComputePlacementKey(&c, key, v); err != nil {
			return err
		}
	}
	return nil
}

// applyComputePlacementPatch applies one compute_placement key from a
// validated patch. It reports whether the key is a compute_placement key;
// for those it returns no error (validateComputePlacementPatch already
// rejected every invalid one, and handlePutConfig runs it before the apply
// loop).
func applyComputePlacementPatch(cfg *config.Config, key string, v any) bool {
	if known, _ := applyComputePlacementKey(&cfg.ComputePlacement, key, v); known {
		return true
	}
	return false
}

// applyComputePlacementKey applies one compute_placement key onto c and
// reports whether the key belongs to the block, or why the value is
// unacceptable.
func applyComputePlacementKey(c *config.ComputePlacementConfig, key string, v any) (bool, error) {
	switch key {
	case keyComputePlacementMissingModel:
		s, ok := v.(string)
		if !ok || !config.ComputePlacementValidMissingModel(s) {
			return true, fmt.Errorf("%s: %v is invalid; valid values: %s",
				key, v, strings.Join(config.ComputePlacementMissingModels(), ", "))
		}
		c.MissingModel = s
	case keyComputePlacementMemHeadroomPct:
		n, ok := infraRetryWhole(v)
		if !ok || n < 0 || n > 100 {
			return true, fmt.Errorf("%s: %v is invalid; use a whole number from 0 to 100 (the default is %d)",
				key, v, config.ComputePlacementDefaultMemHeadroomPct)
		}
		c.MemHeadroomPct = n
	case keyComputePlacementDiskHeadroomGB:
		n, ok := infraRetryWhole(v)
		if !ok || n < 0 {
			return true, fmt.Errorf("%s: %v is invalid; use a whole number of 0 or more (the default is %d)",
				key, v, config.ComputePlacementDefaultDiskHeadroomGB)
		}
		c.DiskHeadroomGB = n
	case keyComputePlacementAskTimeoutMinutes:
		n, ok := infraRetryWhole(v)
		if !ok || n < 0 {
			return true, fmt.Errorf("%s: %v is invalid; use a whole number of 0 or more (the default is %d)",
				key, v, config.ComputePlacementDefaultAskTimeoutMinutes)
		}
		c.AskTimeoutMinutes = n
	case keyComputePlacementAssumeModelSizeGB:
		n, ok := infraRetryWhole(v)
		if !ok || n < 0 {
			return true, fmt.Errorf("%s: %v is invalid; use a whole number of 0 or more (0 = size unknown)",
				key, v)
		}
		c.AssumeModelSizeGB = n
	default:
		return false, nil
	}
	return true, nil
}
