// Merge pipeline keys (autonomous.merge_pipeline.*) over REST for the
// Settings rows under Automata: every key is written with PUT /api/config and
// read back with GET /api/config.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

func TestAutomataSettingsMoveMergePipelineSurfaces(t *testing.T) {
	enc := func(v any) any { return v }
	cases := []struct {
		key  string
		put  any
		path []string // path of the value inside the merge_pipeline map
		want any
	}{
		{"autonomous.merge_pipeline.file_name", "ci/pipeline.yaml", []string{"file_name"}, "ci/pipeline.yaml"},
		{"autonomous.merge_pipeline.source_order", []any{"record", "file"}, []string{"source_order"}, []any{"record", "file"}},
		{"autonomous.merge_pipeline.fallback_on_bad_file", "none", []string{"fallback_on_bad_file"}, "none"},
		{"autonomous.merge_pipeline.conflict_policy", "stop_and_ask", []string{"conflict_policy"}, "stop_and_ask"},
		{"autonomous.merge_pipeline.conflict_model", "some-model", []string{"conflict_model"}, "some-model"},
		{"autonomous.merge_pipeline.conflict_mode", "on", []string{"conflict_mode"}, "on"},
		{"autonomous.merge_pipeline.conflict_confirm_first_n", 0, []string{"conflict_confirm_first_n"}, float64(0)},
		{"autonomous.merge_pipeline.version_files", []any{"cmd/x/main.go"}, []string{"version_files"}, []any{"cmd/x/main.go"}},
		{"autonomous.merge_pipeline.release_size_map", map[string]any{"feature": "patch"}, []string{"release_size_map", "feature"}, "patch"},
		{"autonomous.merge_pipeline.major_trigger", "milestone", []string{"major_trigger"}, "milestone"},
		{"autonomous.merge_pipeline.release_cadence", "per_merge", []string{"release_cadence"}, "per_merge"},
		{"autonomous.merge_pipeline.version_scheme", "semver", []string{"version_scheme"}, "semver"},
		{"autonomous.merge_pipeline.release_approval.patch", "manual", []string{"release_approval", "patch"}, "manual"},
		{"autonomous.merge_pipeline.release_approval.minor", "ask", []string{"release_approval", "minor"}, "ask"},
		{"autonomous.merge_pipeline.release_approval.major", "auto", []string{"release_approval", "major"}, "auto"},
		{"autonomous.merge_pipeline.post_ship.enabled", true, []string{"post_ship", "enabled"}, true},
		{"autonomous.merge_pipeline.post_ship.registry_file", "docs/reg.md", []string{"post_ship", "registry_file"}, "docs/reg.md"},
		{"autonomous.merge_pipeline.post_ship.registry_row_pattern", "row {project}", []string{"post_ship", "registry_row_pattern"}, "row {project}"},
		{"autonomous.merge_pipeline.post_ship.companion_repository", "owner/repo", []string{"post_ship", "companion_repository"}, "owner/repo"},
		{"autonomous.merge_pipeline.post_ship.issue_title_template", "Parity {version}", []string{"post_ship", "issue_title_template"}, "Parity {version}"},
		{"autonomous.merge_pipeline.post_ship.notify_template", "Shipped {version}", []string{"post_ship", "notify_template"}, "Shipped {version}"},
		{"autonomous.merge_pipeline.post_ship.hooks",
			[]any{map[string]any{"name": "say", "command": "echo hi", "enabled": true}},
			[]string{"post_ship", "hooks", "0", "name"}, "say"},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			wireReleaseModeRecords(t, s)
			putJSON(t, s.handlePutConfig, "/api/config", map[string]any{c.key: enc(c.put)})
			raw, ok := getAutonomousConfigMap(t, s)["merge_pipeline"]
			if !ok {
				t.Fatal("GET /api/config autonomous map lacks merge_pipeline")
			}
			var cur any
			if err := json.Unmarshal(raw, &cur); err != nil {
				t.Fatalf("decode merge_pipeline: %v", err)
			}
			for _, p := range c.path {
				switch n := cur.(type) {
				case map[string]any:
					cur = n[p]
				case []any:
					if p != "0" || len(n) == 0 {
						t.Fatalf("path %v: no element %s", c.path, p)
					}
					cur = n[0]
				default:
					t.Fatalf("path %v: cannot descend into %T at %s", c.path, cur, p)
				}
			}
			if !reflect.DeepEqual(cur, c.want) {
				t.Fatalf("GET after PUT %s = %#v, want %#v", c.key, cur, c.want)
			}
		})
	}
}

// TestAutomataSettingsMoveDecisionSurfaces writes each decision record key
// with PUT /api/config and reads it back with GET /api/config.
func TestAutomataSettingsMoveDecisionSurfaces(t *testing.T) {
	cases := []struct {
		key  string
		put  any
		path string
		want any
	}{
		{"autonomous.decisions.enabled", false, "enabled", false},
		{"autonomous.decisions.folder", "docs/decisions", "folder", "docs/decisions"},
		{"autonomous.decisions.folder_glob", "*.md", "folder_glob", "*.md"},
		{"autonomous.decisions.clarity_mode", "refuse", "clarity_mode", "refuse"},
		{"autonomous.decisions.clarity_min_text_chars", 80, "clarity_min_text_chars", float64(80)},
		{"autonomous.decisions.doc_status_mode", "on", "doc_status_mode", "on"},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			putJSON(t, s.handlePutConfig, "/api/config", map[string]any{c.key: c.put})
			raw, ok := getAutonomousConfigMap(t, s)["decisions"]
			if !ok {
				t.Fatal("GET /api/config autonomous map lacks decisions")
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("decode decisions: %v", err)
			}
			if !reflect.DeepEqual(m[c.path], c.want) {
				t.Fatalf("GET after PUT %s = %#v, want %#v", c.key, m[c.path], c.want)
			}
		})
	}
}

// getUsageConfigMap returns the usage block of GET /api/config.
func getUsageConfigMap(t *testing.T, s *Server) map[string]map[string]any {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var got struct {
		Usage map[string]map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return got.Usage
}

// usageMoveCases lists every usage key that moved to Settings under
// Automata: the calculator keys and each burn guard key.
var usageMoveCases = []struct {
	key  string
	put  any
	want any
}{
	{"usage.calculator.policy", "pace", "pace"},
	{"usage.calculator.safety_points", 7.5, 7.5},
	{"usage.calculator.cost_per_task_points", 0.25, 0.25},
	{"usage.calculator.baseline_per_24h", 4, float64(4)},
	{"usage.calculator.max_lanes", 4, float64(4)},
	{"usage.calculator.auto_lanes", true, true},
	{"usage.calculator.auto_lanes_max", 3, float64(3)},
	{"usage.calculator.auto_lanes_special_priority", 2, float64(2)},
	{"usage.calculator.reset_weekday", "friday", "friday"},
	{"usage.calculator.reset_time", "09:30", "09:30"},
	{"usage.calculator.reset_zone", "America/New_York", "America/New_York"},
	{"usage.calculator.keeper", "", ""},
	{"usage.burn_guard.enabled", false, false},
	{"usage.burn_guard.interval_minutes", 30, float64(30)},
	{"usage.burn_guard.margin_points", 3.5, 3.5},
	{"usage.burn_guard.ladder", []any{"lower_inflight", "hold_claude"}, []any{"lower_inflight", "hold_claude"}},
	{"usage.burn_guard.step_back", false, false},
	{"usage.burn_guard.cache_write_points_per_million", 0.4, 0.4},
	{"usage.burn_guard.cache_read_points_per_million", 0.01, 0.01},
	{"usage.burn_guard.refit_on_reading", false, false},
	{"usage.burn_guard.retry_limit_claude", 3, float64(3)},
	{"usage.burn_guard.ask_on_change", true, true},
	{"usage.burn_guard.max_projection_gap_points", 10, float64(10)},
}

// TestAutomataSettingsMoveUsageRoundTrip writes each moved usage key with
// PUT /api/config and reads it back with GET /api/config.
func TestAutomataSettingsMoveUsageRoundTrip(t *testing.T) {
	for _, c := range usageMoveCases {
		t.Run(c.key, func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			putJSON(t, s.handlePutConfig, "/api/config", map[string]any{c.key: c.put})
			parts := strings.SplitN(strings.TrimPrefix(c.key, "usage."), ".", 2)
			block, ok := getUsageConfigMap(t, s)[parts[0]]
			if !ok {
				t.Fatalf("GET /api/config usage lacks %s", parts[0])
			}
			if got := block[parts[1]]; !reflect.DeepEqual(got, c.want) {
				t.Fatalf("GET after PUT %s = %#v, want %#v", c.key, got, c.want)
			}
		})
	}
}

// TestAutomataSettingsMoveOldKeysStillWork calls the usage and merge pipeline
// routes the removed cards used and checks each still answers with its
// previous shape; it also sends one key from each group through the comm
// channel configure verb, which ends in the same PUT /api/config validation
// that datawatch config set uses.
func TestAutomataSettingsMoveOldKeysStillWork(t *testing.T) {
	t.Run("usage routes", func(t *testing.T) {
		s := newUsageCalcServer(t)
		now := time.Now().UTC().Add(-time.Hour)
		rr := doUsage(t, s, http.MethodPost, "/api/usage/readings",
			jsonMarshalUsage(t, usagecalc.Reading{At: now, Percent: fptrUsage(40), Source: usagecalc.SourceTyped}), s.handleUsageReadings)
		if rr.Code != http.StatusOK {
			t.Fatalf("POST /api/usage/readings = %d: %s", rr.Code, rr.Body.String())
		}
		rr = doUsage(t, s, http.MethodGet, "/api/usage/readings", "", s.handleUsageReadings)
		var series map[string]any
		if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &series) != nil {
			t.Fatalf("GET /api/usage/readings = %d: %s", rr.Code, rr.Body.String())
		}
		for _, k := range []string{"readings", "extra_credit_points", "keeper", "is_keeper"} {
			if _, ok := series[k]; !ok {
				t.Errorf("GET /api/usage/readings lacks %q: %s", k, rr.Body.String())
			}
		}
		if rs, _ := series["readings"].([]any); len(rs) != 1 {
			t.Errorf("readings = %v, want 1", series["readings"])
		}
		rr = doUsage(t, s, http.MethodPut, "/api/usage/extra-credit", `{"points":12}`, s.handleUsageExtraCredit)
		if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{"ok":true}` {
			t.Fatalf("PUT /api/usage/extra-credit = %d: %s", rr.Code, rr.Body.String())
		}
		rr = doUsage(t, s, http.MethodGet, "/api/usage/readings", "", s.handleUsageReadings)
		series = nil
		_ = json.Unmarshal(rr.Body.Bytes(), &series)
		if series["extra_credit_points"] != float64(12) {
			t.Errorf("extra_credit_points = %v, want 12", series["extra_credit_points"])
		}
		rr = doUsage(t, s, http.MethodGet, "/api/usage/advice", "", s.handleUsageAdvice)
		var advice map[string]any
		if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &advice) != nil || len(advice) == 0 {
			t.Fatalf("GET /api/usage/advice = %d: %s", rr.Code, rr.Body.String())
		}
		// The old methods are still refused the same way.
		if rr := doUsage(t, s, http.MethodGet, "/api/usage/extra-credit", "", s.handleUsageExtraCredit); rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET extra-credit = %d, want 405", rr.Code)
		}
		if rr := doUsage(t, s, http.MethodPost, "/api/usage/advice", "", s.handleUsageAdvice); rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST advice = %d, want 405", rr.Code)
		}
	})

	t.Run("burn guard config route", func(t *testing.T) {
		s := burnGuardConfigServer(t)
		rr := doBurnGuard(s, http.MethodPut, "/api/usage/burn-guard/config", `{"margin_points": 4}`, s.handleBurnGuardConfig)
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT burn-guard/config = %d: %s", rr.Code, rr.Body.String())
		}
		rr = doBurnGuard(s, http.MethodGet, "/api/usage/burn-guard/config", "", s.handleBurnGuardConfig)
		var bg config.BurnGuardConfig
		if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &bg) != nil || bg.MarginPoints != 4 {
			t.Fatalf("GET burn-guard/config = %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("merge pipeline routes", func(t *testing.T) {
		s, _ := newMergePipelineServer(t)
		v := mpGet(t, s, "demo")
		if v.Source != "default" || v.Definition == nil || v.Project != "demo" {
			t.Fatalf("GET merge-pipeline = %+v", v)
		}
		rec := mpDo(s.handleMergePipelineRecord, http.MethodPut, "/api/autonomous/merge-pipeline/record?project=demo", mpValidRecordJSON)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT record = %d: %s", rec.Code, rec.Body.String())
		}
		if v := mpGet(t, s, "demo"); v.Source != "record" || v.RecordUpdatedAt == "" {
			t.Fatalf("GET after record = %+v", v)
		}
		rec = mpDo(s.handleMergePipelineValidate, http.MethodPost, "/api/autonomous/merge-pipeline/validate", mpValidRecordJSON)
		var val mergePipelineValidation
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &val) != nil || !val.Valid || val.Source != "body" {
			t.Fatalf("POST validate = %d: %s", rec.Code, rec.Body.String())
		}
		rec = mpDo(s.handleMergePipelineValidate, http.MethodPost, "/api/autonomous/merge-pipeline/validate", `{"definition":{"steps":[]}}`)
		val = mergePipelineValidation{}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &val) != nil || val.Valid || len(val.Errors) == 0 {
			t.Fatalf("POST validate bad = %d: %s", rec.Code, rec.Body.String())
		}
		rec = mpDo(s.handleMergePipelineRecord, http.MethodDelete, "/api/autonomous/merge-pipeline/record?project=demo", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("DELETE record = %d: %s", rec.Code, rec.Body.String())
		}
		if v := mpGet(t, s, "demo"); v.Source != "default" {
			t.Fatalf("GET after delete = %+v", v)
		}
	})

	// One key per group goes through the comm channel configure verb, whose
	// write is the same PUT /api/config validation datawatch config set uses.
	for _, c := range []struct{ key, val, group string }{
		{"usage.calculator.safety_points", "6", "usage calculator"},
		{"usage.burn_guard.margin_points", "3", "burn guard"},
		{"autonomous.merge_pipeline.conflict_policy", "stop_and_ask", "merge pipeline"},
		{"autonomous.decisions.clarity_mode", "refuse", "decision records"},
	} {
		t.Run("comm configure/"+c.group, func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			r := router.NewRouter("h", "", nil, nil, 0, nil)
			r.SetConfigureFunc(func(k, v string) error {
				var val any = v
				var n json.Number
				if json.Unmarshal([]byte(v), &n) == nil {
					val = n
				}
				b, _ := json.Marshal(map[string]any{k: val})
				rr := httptest.NewRecorder()
				s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
				if rr.Code != http.StatusOK {
					return &configureErr{rr.Body.String()}
				}
				return nil
			})
			resp := r.HandleTestMessage("configure " + c.key + "=" + c.val)
			if len(resp) == 0 || !strings.Contains(resp[0], "Set "+c.key) {
				t.Fatalf("configure response = %v", resp)
			}
		})
	}
}
