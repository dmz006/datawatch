// burn_guard_comm.go — the comm channel's configure path for the burn guard's
// usage.burn_guard keys. The max_projection_gap_points value is checked by
// config.ValidateMaxProjectionGapPoints and the pace_line values by
// config.ValidateBurnGuardPaceLine — the same rules the REST config path, the
// CLI and the MCP tools use — before configureFn is called, so a refused value
// is never written.

package router

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

const burnGuardMaxProjectionGapKey = "usage.burn_guard.max_projection_gap_points"

var burnGuardPaceLineNumericKeys = map[string]string{
	"usage.burn_guard.week_tolerance":            "whole number 0 to 10",
	"usage.burn_guard.points_to_week_factor":     "number 1 to 10",
	"usage.burn_guard.window_percent_per_point":  "number 1 to 25",
	"usage.burn_guard.window_stop_percent":       "number 50 to 95",
	"usage.burn_guard.reading_freshness_minutes": "whole number 1 to 1440",
}

// checkBurnGuardConfigure refuses an invalid burn guard value. Other keys pass through.
func checkBurnGuardConfigure(key, value string) error {
	key, value = strings.TrimSpace(key), strings.TrimSpace(value)
	if key == burnGuardMaxProjectionGapKey {
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("%s: %q is not a number", burnGuardMaxProjectionGapKey, value)
		}
		return config.ValidateMaxProjectionGapPoints(f)
	}
	switch key {
	case "usage.burn_guard.mode":
		if !config.IsBurnGuardMode(value) {
			return fmt.Errorf("usage.burn_guard.mode: %q is not a mode (allowed: %s)", value, strings.Join(config.BurnGuardModes(), ", "))
		}
		c := config.DefaultBurnGuard()
		c.PaceLineMode = value
		return config.ValidateBurnGuardPaceLine(c)
	case "usage.burn_guard.week_tolerance",
		"usage.burn_guard.points_to_week_factor",
		"usage.burn_guard.window_percent_per_point",
		"usage.burn_guard.window_stop_percent",
		"usage.burn_guard.reading_freshness_minutes":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("%s: %q is not a number", key, value)
		}
		// week_tolerance and reading_freshness_minutes are whole numbers; the three
		// percent/factor settings accept fractions inside their ranges.
		if key == "usage.burn_guard.week_tolerance" || key == "usage.burn_guard.reading_freshness_minutes" {
			if f != math.Trunc(f) {
				return fmt.Errorf("%s: %s", key, burnGuardPaceLineNumericKeys[key])
			}
		}
		c := config.DefaultBurnGuard()
		switch key {
		case "usage.burn_guard.week_tolerance":
			c.PaceLineWeekTolerance = int(f)
		case "usage.burn_guard.points_to_week_factor":
			c.PaceLinePointsToWeekFactor = f
		case "usage.burn_guard.window_percent_per_point":
			c.PaceLineWindowPercentPerPoint = f
		case "usage.burn_guard.window_stop_percent":
			c.PaceLineWindowStopPercent = f
		case "usage.burn_guard.reading_freshness_minutes":
			c.PaceLineReadingFreshnessMinutes = int(f)
		}
		return config.ValidateBurnGuardPaceLine(c)
	}
	return nil
}
