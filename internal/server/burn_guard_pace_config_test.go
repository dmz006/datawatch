// The pace_line policy settings on REST /api/usage/burn-guard/config: GET carries
// them with their defaults, a valid PUT stores them (plain or prefixed keys), an
// invalid value is refused with a 400 naming the key and the allowed range, and
// nothing from a rejected PUT is stored.

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestBurnGuardPaceConfigDefaultGet(t *testing.T) {
	s := burnGuardConfigServer(t)
	rr := doBurnGuard(s, http.MethodGet, "/api/usage/burn-guard/config", "", s.handleBurnGuardConfig)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", rr.Code, rr.Body.String())
	}
	var got map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := map[string]interface{}{
		"mode":                      "shadow",
		"week_tolerance":            float64(config.BurnGuardWeekToleranceDefault),
		"points_to_week_factor":     config.BurnGuardPointsToWeekFactorDefault,
		"window_percent_per_point":  config.BurnGuardWindowPercentPerPointDefault,
		"window_stop_percent":       config.BurnGuardWindowStopPercentDefault,
		"reading_freshness_minutes": float64(config.BurnGuardReadingFreshnessMinutesDefault),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("GET %s = %v, want %v", k, got[k], v)
		}
	}
}

func TestBurnGuardPaceConfigPutStoresValidValues(t *testing.T) {
	s := burnGuardConfigServer(t)
	body := `{"mode":"on","week_tolerance":5,"points_to_week_factor":2.5,"window_percent_per_point":10,"window_stop_percent":90,"reading_freshness_minutes":30}`
	rr := doBurnGuard(s, http.MethodPut, "/api/usage/burn-guard/config", body, s.handleBurnGuardConfig)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rr.Code, rr.Body.String())
	}
	bg := s.cfg.Usage.BurnGuard
	if bg.PaceLineMode != "on" || bg.PaceLineWeekTolerance != 5 || bg.PaceLinePointsToWeekFactor != 2.5 ||
		bg.PaceLineWindowPercentPerPoint != 10 || bg.PaceLineWindowStopPercent != 90 || bg.PaceLineReadingFreshnessMinutes != 30 {
		t.Fatalf("stored = %+v", bg)
	}
	// A single value, under the prefix, replaces it and keeps the other pace values.
	rr = doBurnGuard(s, http.MethodPut, "/api/usage/burn-guard/config", `{"usage.burn_guard.mode":"off"}`, s.handleBurnGuardConfig)
	if rr.Code != http.StatusOK || s.cfg.Usage.BurnGuard.PaceLineMode != "off" || s.cfg.Usage.BurnGuard.PaceLineWeekTolerance != 5 {
		t.Fatalf("prefixed PUT = %d mode=%q tol=%d", rr.Code, s.cfg.Usage.BurnGuard.PaceLineMode, s.cfg.Usage.BurnGuard.PaceLineWeekTolerance)
	}
	// The range edges are accepted.
	rr = doBurnGuard(s, http.MethodPut, "/api/usage/burn-guard/config",
		`{"week_tolerance":0,"points_to_week_factor":1,"window_percent_per_point":1,"window_stop_percent":50,"reading_freshness_minutes":1}`, s.handleBurnGuardConfig)
	if rr.Code != http.StatusOK {
		t.Fatalf("edges PUT = %d: %s", rr.Code, rr.Body.String())
	}
}

func TestBurnGuardPaceConfigPutRefusesInvalidValues(t *testing.T) {
	cases := map[string]struct {
		body string
		key  string
	}{
		"mode not a mode":           {"{\"mode\":\"loud\"}", "mode"},
		"mode not a string":         {"{\"mode\":3}", "mode"},
		"tolerance negative":        {"{\"week_tolerance\":-1}", "week_tolerance"},
		"tolerance over ten":        {"{\"week_tolerance\":11}", "week_tolerance"},
		"tolerance fraction":        {"{\"week_tolerance\":2.5}", "week_tolerance"},
		"tolerance not a number":    {"{\"week_tolerance\":\"two\"}", "week_tolerance"},
		"factor under one":          {"{\"points_to_week_factor\":0.9}", "points_to_week_factor"},
		"factor over ten":           {"{\"points_to_week_factor\":11}", "points_to_week_factor"},
		"ppp under one":             {"{\"window_percent_per_point\":0.5}", "window_percent_per_point"},
		"ppp over twenty five":      {"{\"window_percent_per_point\":26}", "window_percent_per_point"},
		"stop under fifty":          {"{\"window_stop_percent\":49}", "window_stop_percent"},
		"stop over ninety five":     {"{\"window_stop_percent\":96}", "window_stop_percent"},
		"freshness zero":            {"{\"reading_freshness_minutes\":0}", "reading_freshness_minutes"},
		"freshness over a day":      {"{\"reading_freshness_minutes\":1441}", "reading_freshness_minutes"},
		"freshness fraction":        {"{\"reading_freshness_minutes\":60.5}", "reading_freshness_minutes"},
		"good plus bad stores none": {"{\"mode\":\"off\",\"week_tolerance\":-3}", "week_tolerance"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := burnGuardConfigServer(t)
			before := s.cfg.Usage.BurnGuard
			rr := doBurnGuard(s, http.MethodPut, "/api/usage/burn-guard/config", tc.body, s.handleBurnGuardConfig)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("%s: PUT = %d (%s), want 400", name, rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), tc.key) {
				t.Fatalf("%s: body %q does not name the key %q", name, rr.Body.String(), tc.key)
			}
			after := s.cfg.Usage.BurnGuard
			if after.PaceLineMode != before.PaceLineMode || after.PaceLineWeekTolerance != before.PaceLineWeekTolerance ||
				after.PaceLinePointsToWeekFactor != before.PaceLinePointsToWeekFactor || after.PaceLineWindowPercentPerPoint != before.PaceLineWindowPercentPerPoint ||
				after.PaceLineWindowStopPercent != before.PaceLineWindowStopPercent || after.PaceLineReadingFreshnessMinutes != before.PaceLineReadingFreshnessMinutes ||
				after.MarginPoints != before.MarginPoints {
				t.Fatalf("%s: a rejected PUT stored something", name)
			}
		})
	}
}
