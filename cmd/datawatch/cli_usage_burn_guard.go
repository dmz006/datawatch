// cli_usage_burn_guard.go — the burn guard CLI, under the usage command:
//
//	usage burn-guard status
//	usage burn-guard run
//	usage burn-guard config [key=value ...]
//
// status GETs /api/usage/burn-guard, run POSTs /api/usage/burn-guard/run, and config
// GETs /api/usage/burn-guard/config, or PUTs the given key=value settings to it. The
// keys are validated by the daemon through the config set path, so one bad value
// stores nothing. A value of the ladder key is a comma-separated list of rungs.

package main

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/spf13/cobra"
)

func newUsageBurnGuardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "burn-guard",
		Short: "The burn guard: status, run now, settings",
		Long: `The burn guard compares the projected weekly burn with the pace line and steps
down a ladder of admission changes when the projection runs over. It never ends
a running session.`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show the guard's state, rung, last projection (line and margin), last decision and the pace policy",
		Long: `Show the guard's state, rung, last projection (line and margin) and last decision.
The pace block is the pace policy: its mode (off, shadow or on), whether new work
is held now and on which rule (window_pause_line or weekly_pace_line), and the
recent decisions that changed the verdict or the hold, each with the evidence it
was made on. In shadow the policy records what it would hold and holds nothing.
After a weekly reset with no new reading, the projection and the weekly line use an
estimate from the tokens since the reset (estimated, week_from_tokens); enter a
reading to correct it.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return daemonJSON(http.MethodGet, "/api/usage/burn-guard", nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "run",
		Short: "Evaluate the guard now and show the decision",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return daemonJSON(http.MethodPost, "/api/usage/burn-guard/run", map[string]any{})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "config [key=value ...]",
		Short: "Show the guard's settings, or set some (enabled, interval_minutes, margin_points, max_projection_gap_points, ladder, step_back, mode, week_tolerance, points_to_week_factor, window_percent_per_point, window_stop_percent, reading_freshness_minutes, ...)",
		Long: `Show the guard's settings (a GET; the output includes max_projection_gap_points),
or set some with key=value arguments. max_projection_gap_points is the largest gap,
in points, allowed between the guard's projection and the usage advice before the
guard takes the lower projection: a number from 0 to 100; 0 never compares them.
The pace_line policy settings sit beside them: mode (off, shadow, or on; unset
defaults to shadow), week_tolerance (a whole number from 0 to 10, default 2),
points_to_week_factor (1 to 10, default 1.3), window_percent_per_point (1 to 25,
default 4), window_stop_percent (50 to 95, default 85) and
reading_freshness_minutes (1 to 1440, default 60).
An invalid value is refused here and nothing is stored.`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return daemonJSON(http.MethodGet, "/api/usage/burn-guard/config", nil)
			}
			body, err := parseBurnGuardSettings(args)
			if err != nil {
				return err
			}
			return daemonJSON(http.MethodPut, "/api/usage/burn-guard/config", body)
		},
	})
	return cmd
}

// parseBurnGuardSettings turns key=value arguments into the PUT body.
func parseBurnGuardSettings(args []string) (map[string]any, error) {
	body := map[string]any{}
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !ok || k == "" {
			return nil, fmt.Errorf("a setting is key=value: %q", a)
		}
		switch k {
		case "enabled", "step_back", "refit_on_reading", "ask_on_change":
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("%s must be true or false: %q", k, v)
			}
			body[k] = b
		case "max_projection_gap_points":
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("%s must be a number: %q", k, v)
			}
			if err := config.ValidateMaxProjectionGapPoints(f); err != nil {
				return nil, err
			}
			body[k] = f
		case "mode":
			if !config.IsBurnGuardMode(v) {
				return nil, fmt.Errorf("usage.burn_guard.mode: %q is not a mode (allowed: %s)", v, strings.Join(config.BurnGuardModes(), ", "))
			}
			body[k] = v
		case "week_tolerance":
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f != math.Trunc(f) || f < 0 || f > config.BurnGuardWeekToleranceMax {
				return nil, fmt.Errorf("usage.burn_guard.week_tolerance: must be a whole number from 0 to %d", config.BurnGuardWeekToleranceMax)
			}
			body[k] = f
		case "points_to_week_factor":
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f < config.BurnGuardPointsToWeekFactorMin || f > config.BurnGuardPointsToWeekFactorMax {
				return nil, fmt.Errorf("usage.burn_guard.points_to_week_factor: must be from %g to %g", config.BurnGuardPointsToWeekFactorMin, config.BurnGuardPointsToWeekFactorMax)
			}
			body[k] = f
		case "window_percent_per_point":
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f < config.BurnGuardWindowPercentPerPointMin || f > config.BurnGuardWindowPercentPerPointMax {
				return nil, fmt.Errorf("usage.burn_guard.window_percent_per_point: must be from %g to %g", config.BurnGuardWindowPercentPerPointMin, config.BurnGuardWindowPercentPerPointMax)
			}
			body[k] = f
		case "window_stop_percent":
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f < config.BurnGuardWindowStopPercentMin || f > config.BurnGuardWindowStopPercentMax {
				return nil, fmt.Errorf("usage.burn_guard.window_stop_percent: must be from %g to %g", config.BurnGuardWindowStopPercentMin, config.BurnGuardWindowStopPercentMax)
			}
			body[k] = f
		case "reading_freshness_minutes":
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f != math.Trunc(f) || f < 1 || f > config.BurnGuardReadingFreshnessMinutesMax {
				return nil, fmt.Errorf("usage.burn_guard.reading_freshness_minutes: must be a whole number from 1 to %d", config.BurnGuardReadingFreshnessMinutesMax)
			}
			body[k] = f
		case "ladder":
			var rungs []string
			for _, r := range strings.Split(v, ",") {
				if r = strings.TrimSpace(r); r != "" {
					rungs = append(rungs, r)
				}
			}
			body[k] = rungs
		default:
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("%s must be a number: %q", k, v)
			}
			body[k] = f
		}
	}
	return body, nil
}
