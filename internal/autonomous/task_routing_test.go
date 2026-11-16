package autonomous

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
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

func trWantRow(t *testing.T, err error, profile, row, field string) {
	t.Helper()
	if err == nil {
		t.Fatal("bad row accepted")
	}
	for _, w := range []string{`profile "` + profile + `"`, `row "` + row + `"`, field} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("message does not name %s: %v", w, err)
		}
	}
}

func TestTaskRoutingUnknownBackendAndModelRefusedOnImportAndSave(t *testing.T) {
	cases := []struct{ name, row, field, want string }{
		{"backend", `{"worker_backend":"ghost-llm","worker_model":"m"}`, "worker", "ghost-llm"},
		{"verifier", `{"worker_backend":"pi-prompt","verification_backend":"ghost-v"}`, "verification", "ghost-v"},
		{"second", `{"worker_backend":"pi-prompt","second_choice":{"backend":"ghost-s"}}`, "second choice", "ghost-s"},
		{"model", `{"worker_backend":"claude-code","worker_model":"ollama/qwen3.8:27b"}`, "worker", "ollama/qwen3.8:27b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := config.DefaultTaskRouting()
			before := cfg.Clone()
			doc := []byte(`{"format_version":1,"name":"imp","rows":{"docs_set":` + c.row + `}}`)
			out, err := ImportProfile(cfg, doc, "", false, trChecks())
			trWantRow(t, err, "imp", "docs_set", c.field)
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("message does not name %s: %v", c.want, err)
			}
			if !reflect.DeepEqual(out, before) || !reflect.DeepEqual(cfg, before) {
				t.Error("config changed on refusal")
			}
			// Save stores rows as given; the whole-table check that follows
			// (the one the surfaces run before they commit) refuses it.
			var d TaskRoutingDocument
			if e := json.Unmarshal(doc, &d); e != nil {
				t.Fatal(e)
			}
			saved, err := SaveProfile(cfg, "bad", d.Rows)
			if err != nil {
				t.Fatal(err)
			}
			errs := config.ValidateTaskRouting(saved, trChecks())
			if len(errs) == 0 {
				t.Fatal("saved bad row passes validation")
			}
			trWantRow(t, errs[0], "bad", "docs_set", c.field)
			if !reflect.DeepEqual(cfg, before) {
				t.Error("SaveProfile modified its input")
			}
		})
	}
}

func TestTaskRoutingPremiumModelNeedsFlagOnImportAndSave(t *testing.T) {
	for _, model := range []string{"claude-opus-5-5", "claude-fable-5-2", "CLAUDE-OPUS-5-5"} {
		t.Run(model, func(t *testing.T) {
			cfg := config.DefaultTaskRouting()
			before := cfg.Clone()
			doc := func(flag string) []byte {
				return []byte(`{"format_version":1,"name":"prem","rows":{"final_gate":{"worker_backend":"claude-code","worker_model":"` + model + `"` + flag + `}}}`)
			}
			out, err := ImportProfile(cfg, doc(""), "", false, trChecks())
			trWantRow(t, err, "prem", "final_gate", "allow_premium_model")
			if !reflect.DeepEqual(out, before) || !reflect.DeepEqual(cfg, before) {
				t.Error("config changed on refusal")
			}
			ok, err := ImportProfile(cfg, doc(`,"allow_premium_model":true`), "", false, trChecks())
			if err != nil {
				t.Fatalf("flagged premium row refused: %v", err)
			}
			if !ok.Profiles["prem"]["final_gate"].AllowPremiumModel {
				t.Error("flag lost on import")
			}
			// Save: the same table check refuses an unflagged row and
			// accepts a flagged one.
			rows := config.TaskRoutingProfile{"final_gate": {WorkerBackend: "claude-code", WorkerModel: model}}
			saved, _ := SaveProfile(cfg, "prem", rows)
			errs := config.ValidateTaskRouting(saved, trChecks())
			if len(errs) == 0 {
				t.Fatal("unflagged premium row passes validation")
			}
			trWantRow(t, errs[0], "prem", "final_gate", "allow_premium_model")
			rows["final_gate"] = config.TaskRoutingRow{WorkerBackend: "claude-code", WorkerModel: model, AllowPremiumModel: true}
			saved, _ = SaveProfile(cfg, "prem", rows)
			if errs := config.ValidateTaskRouting(saved, trChecks()); len(errs) != 0 {
				t.Errorf("flagged premium row refused: %v", errs)
			}
		})
	}
}

func TestTaskRoutingModelPairingUnknownMessageNamesRow(t *testing.T) {
	cfg := config.DefaultTaskRouting()
	cfg.Profiles["default"][config.TaskTypeUICode] = config.TaskRoutingRow{WorkerBackend: "claude-code", WorkerModel: "ollama/qwen3.8:27b"}
	errs := config.ValidateTaskRouting(cfg, trChecks())
	if len(errs) == 0 {
		t.Fatal("mismatched pair accepted")
	}
	trWantRow(t, errs[0], "default", "ui_code", "worker")
}
