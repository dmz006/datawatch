// burn_guard_comm.go — the comm channel's configure path for the burn guard's
// usage.burn_guard.max_projection_gap_points key. The value is parsed and checked
// by config.ValidateMaxProjectionGapPoints, the same rule the REST config path
// and the CLI use, before configureFn is called, so a refused value is never written.

package router

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

const burnGuardMaxProjectionGapKey = "usage.burn_guard.max_projection_gap_points"

// checkBurnGuardConfigure refuses an invalid burn guard value. Other keys pass through.
func checkBurnGuardConfigure(key, value string) error {
	if strings.TrimSpace(key) != burnGuardMaxProjectionGapKey {
		return nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return fmt.Errorf("%s: %q is not a number", burnGuardMaxProjectionGapKey, value)
	}
	return config.ValidateMaxProjectionGapPoints(f)
}
