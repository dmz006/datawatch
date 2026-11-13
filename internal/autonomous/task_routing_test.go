package autonomous

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func trChecks() config.TaskRoutingChecks {
	return TaskRoutingChecksFor(func(b string) bool { return b == "pi-prompt" || b == "claude-code" })
}

func TestTaskRoutingProfileSaveSwitchDelete(t *testing.T) {
	cfg := config.DefaultTaskRouting()
	cfg2, err := SaveProfile(cfg, "mine", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg2.Profiles["mine"], cfg.Profiles["default"]) {
		t.Error("saved profile is not a copy of the current table")
	}
	if _, ok := cfg.Profiles["mine"]; ok {
		t.Error("SaveProfile modified its input")
	}
	// The copy is deep.
	r := cfg2.Profiles["mine"][config.TaskTypeTests]
	r.WorkerModel = "changed"
	cfg2.Profiles["mine"][config.TaskTypeTests] = r
	if cfg2.Profiles["default"][config.TaskTypeTests].WorkerModel == "changed" {
		t.Error("profiles share rows")
	}
	if _, err := SetActiveProfile(cfg2, "nope"); err == nil {
		t.Error("switch to missing profile accepted")
	}
	cfg3, err := SetActiveProfile(cfg2, "mine")
	if err != nil || cfg3.ActiveProfile != "mine" {
		t.Fatalf("switch failed: %v", err)
	}
	if _, err := DeleteProfile(cfg3, "mine"); err == nil {
		t.Error("active profile deleted")
	}
	cfg4, err := DeleteProfile(cfg3, "default")
	if err != nil || len(ListProfiles(cfg4)) != 1 {
		t.Errorf("delete inactive failed: %v %v", err, ListProfiles(cfg4))
	}
	if got := ListProfiles(cfg2); !reflect.DeepEqual(got, []string{"default", "mine"}) {
		t.Errorf("list = %v", got)
	}
}

func TestTaskRoutingExportImportRoundTrip(t *testing.T) {
	cfg := config.DefaultTaskRouting()
	doc, err := ExportProfile(cfg, "default")
	if err != nil {
		t.Fatal(err)
	}
	doc2, _ := ExportProfile(cfg, "default")
	if !bytes.Equal(doc, doc2) {
		t.Error("export is not deterministic")
	}
	if !bytes.Contains(doc, []byte(`"format_version": 1`)) {
		t.Error("no format version in export")
	}
	out, err := ImportProfile(cfg, doc, "copy", false, trChecks())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Profiles["copy"], cfg.Profiles["default"]) {
		t.Error("round trip changed the table")
	}
	if _, err := ImportProfile(out, doc, "copy", false, trChecks()); err == nil {
		t.Error("import over an existing profile without overwrite accepted")
	}
	if _, err := ImportProfile(out, doc, "copy", true, trChecks()); err != nil {
		t.Errorf("overwrite import refused: %v", err)
	}
}

func TestTaskRoutingImportRefusedLeavesConfigUnchanged(t *testing.T) {
	cfg := config.DefaultTaskRouting()
	before := cfg.Clone()
	bad := []byte(`{"format_version":1,"name":"x","rows":{"docs_set":{"worker_backend":"ghost-llm","worker_model":"m"}}}`)
	out, err := ImportProfile(cfg, bad, "x", false, trChecks())
	if err == nil {
		t.Fatal("unknown LLM accepted")
	}
	if !reflect.DeepEqual(out, before) || !reflect.DeepEqual(cfg, before) {
		t.Error("config changed on refusal")
	}
	opus := []byte(`{"format_version":1,"name":"y","rows":{"ui_code":{"worker_backend":"claude-code","worker_model":"claude-opus-5-5"}}}`)
	if _, err := ImportProfile(cfg, opus, "y", false, trChecks()); err == nil {
		t.Error("implicit opus accepted on import")
	}
	unk := []byte(`{"format_version":1,"name":"z","rows":{"made_up":{"worker_backend":"pi-prompt"}}}`)
	if _, err := ImportProfile(cfg, unk, "z", false, trChecks()); err == nil {
		t.Error("unknown task type accepted on import")
	}
	if _, err := ImportProfile(cfg, []byte(`{"format_version":9}`), "w", false, trChecks()); err == nil {
		t.Error("unsupported format version accepted")
	}
}

func TestTaskRoutingModelPairing(t *testing.T) {
	cfg := config.DefaultTaskRouting()
	cfg.Profiles["default"][config.TaskTypeUICode] = config.TaskRoutingRow{WorkerBackend: "claude-code", WorkerModel: "ollama/qwen3.8:27b"}
	if errs := config.ValidateTaskRouting(cfg, trChecks()); len(errs) == 0 {
		t.Error("mismatched pair accepted")
	}
}
