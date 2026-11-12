package server

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestDefectQueueConfigPatchRoundTrip(t *testing.T) {
	patch := map[string]interface{}{
		"autonomous.defect_queue.enabled":                              false,
		"autonomous.defect_queue.batch.size_cap":                       float64(4),
		"autonomous.defect_queue.batch.grouping_keys":                  []interface{}{"shared_files"},
		"autonomous.defect_queue.batch.allow_unrelated":                true,
		"autonomous.defect_queue.batch.priority_rule":                  "first_member",
		"autonomous.defect_queue.tiny_fix.enabled":                     false,
		"autonomous.defect_queue.tiny_fix.max_files":                   float64(2),
		"autonomous.defect_queue.tiny_fix.allowed_path_globs":          "docs/**,scripts/**",
		"autonomous.defect_queue.tiny_fix.allowed_kinds":               []interface{}{"docs_only"},
		"autonomous.defect_queue.tiny_fix.operator_visible_path_globs": []interface{}{"web/**"},
		"autonomous.defect_queue.projects.app.batch.size_cap":          float64(2),
	}
	if err := firstDefectQueueSettingError(patch); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	applyConfigPatch(cfg, patch)
	view := defectQueueSettingsForDisplay(cfg.Autonomous.DefectQueue)
	if view["enabled"] != false {
		t.Fatalf("enabled: %v", view["enabled"])
	}
	b := view["batch"].(config.DefectQueueBatchSettings)
	if b.SizeCap != 4 || !b.AllowUnrelated || b.PriorityRule != "first_member" || !reflect.DeepEqual(b.GroupingKeys, []string{"shared_files"}) {
		t.Fatalf("batch: %+v", b)
	}
	tf := view["tiny_fix"].(config.DefectQueueTinyFixSettings)
	if tf.Enabled || tf.MaxFiles != 2 || !reflect.DeepEqual(tf.AllowedPathGlobs, []string{"docs/**", "scripts/**"}) ||
		!reflect.DeepEqual(tf.AllowedKinds, []string{"docs_only"}) || !reflect.DeepEqual(tf.OperatorVisiblePathGlobs, []string{"web/**"}) {
		t.Fatalf("tiny_fix: %+v", tf)
	}
	if _, ok := view["projects"].(map[string]interface{})["app"]; !ok {
		t.Fatalf("projects: %v", view["projects"])
	}
}

func TestDefectQueueConfigPatchRejectsBadValue(t *testing.T) {
	err := firstDefectQueueSettingError(map[string]interface{}{
		"autonomous.defect_queue.batch.size_cap": float64(0),
		"autonomous.tags.default_view":           "auto",
	})
	if err == nil || !strings.Contains(err.Error(), "autonomous.defect_queue.batch.size_cap") {
		t.Fatalf("got %v", err)
	}
}
