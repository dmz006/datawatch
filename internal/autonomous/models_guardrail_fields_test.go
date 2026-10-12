package autonomous

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// BL416 Phase 1 — schema-only fields: VerificationScript,
// GuardrailBackend/GuardrailModel, FilesGenerated.

var bl416NewKeys = []string{
	"verification_script",
	"guardrail_backend",
	"guardrail_model",
	"files_generated",
}

func assertNoBL416Keys(t *testing.T, label string, raw []byte) {
	t.Helper()
	for _, k := range bl416NewKeys {
		if strings.Contains(string(raw), `"`+k+`"`) {
			t.Errorf("%s: zero-valued marshal contains key %q: %s", label, k, raw)
		}
	}
}

func TestBL416Fields_ZeroValueOmitted(t *testing.T) {
	for label, v := range map[string]any{
		"Task":  Task{},
		"Story": Story{},
		"PRD":   PRD{},
	} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: marshal: %v", label, err)
		}
		assertNoBL416Keys(t, label, raw)
	}
}

const bl416LegacyPRDJSON = `{
  "id": "abcd1234",
  "spec": "legacy spec",
  "title": "Legacy PRD",
  "project_dir": "/tmp/proj",
  "backend": "claude-code",
  "model": "sonnet",
  "status": "approved",
  "created_at": "2026-01-02T03:04:05Z",
  "updated_at": "2026-01-02T03:04:05Z",
  "stories": [
    {
      "id": "s1",
      "prd_id": "abcd1234",
      "title": "Story one",
      "status": "pending",
      "files": ["a.go"],
      "created_at": "2026-01-02T03:04:05Z",
      "updated_at": "2026-01-02T03:04:05Z",
      "tasks": [
        {
          "id": "t1",
          "story_id": "s1",
          "prd_id": "abcd1234",
          "title": "Task one",
          "spec": "do the thing",
          "status": "completed",
          "backend": "opencode",
          "model": "qwen",
          "files": ["a.go", "b.go"],
          "files_touched": ["a.go"],
          "created_at": "2026-01-02T03:04:05Z",
          "updated_at": "2026-01-02T03:04:05Z"
        }
      ]
    }
  ]
}`

func TestBL416Fields_LegacyRoundTrip(t *testing.T) {
	var p PRD
	if err := json.Unmarshal([]byte(bl416LegacyPRDJSON), &p); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	assertNoBL416Keys(t, "legacy PRD", out)

	var want, got map[string]any
	if err := json.Unmarshal([]byte(bl416LegacyPRDJSON), &want); err != nil {
		t.Fatalf("unmarshal want map: %v", err)
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal got map: %v", err)
	}
	// Every legacy key/value must survive the round-trip unchanged
	// (the re-marshal may add pre-existing non-omitempty keys, which
	// are not under test here).
	assertSubset(t, "prd", want, got)
}

// assertSubset checks every key in want exists in got with an equal
// value, recursing into nested objects and arrays of objects.
func assertSubset(t *testing.T, path string, want, got any) {
	t.Helper()
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			t.Errorf("%s: want object, got %T", path, got)
			return
		}
		for k, wv := range w {
			gv, ok := g[k]
			if !ok {
				t.Errorf("%s.%s: key missing after round-trip", path, k)
				continue
			}
			assertSubset(t, path+"."+k, wv, gv)
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			t.Errorf("%s: want %v, got %v", path, w, got)
			return
		}
		for i := range w {
			assertSubset(t, path, w[i], g[i])
		}
	default:
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s: want %v, got %v", path, want, got)
		}
	}
}

func TestBL416Fields_PopulatedRoundTrip(t *testing.T) {
	in := PRD{
		ID:               "abcd1234",
		GuardrailBackend: "ollama",
		GuardrailModel:   "qwen3",
		Story: []Story{{
			ID:                 "s1",
			VerificationScript: ".datawatch/verify/s1.sh",
			FilesGenerated:     []string{".datawatch/verify/s1.sh"},
			Tasks: []Task{{
				ID:                 "t1",
				VerificationScript: ".datawatch/verify/t1.sh",
				GuardrailBackend:   "claude-code",
				GuardrailModel:     "sonnet",
				FilesTouched:       []string{"a.go"},
				FilesGenerated:     []string{".datawatch/verify/t1.sh"},
			}},
		}},
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, k := range bl416NewKeys {
		if !strings.Contains(string(raw), `"`+k+`"`) {
			t.Errorf("populated marshal missing key %q", k)
		}
	}
	var out PRD
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.GuardrailBackend != in.GuardrailBackend || out.GuardrailModel != in.GuardrailModel {
		t.Errorf("PRD guardrail fields: got %q/%q", out.GuardrailBackend, out.GuardrailModel)
	}
	if len(out.Story) != 1 || len(out.Story[0].Tasks) != 1 {
		t.Fatalf("structure lost: %+v", out)
	}
	gs, ws := out.Story[0], in.Story[0]
	if gs.VerificationScript != ws.VerificationScript || !reflect.DeepEqual(gs.FilesGenerated, ws.FilesGenerated) {
		t.Errorf("Story fields: got %q %v", gs.VerificationScript, gs.FilesGenerated)
	}
	gt, wt := gs.Tasks[0], ws.Tasks[0]
	if gt.VerificationScript != wt.VerificationScript ||
		gt.GuardrailBackend != wt.GuardrailBackend ||
		gt.GuardrailModel != wt.GuardrailModel ||
		!reflect.DeepEqual(gt.FilesGenerated, wt.FilesGenerated) ||
		!reflect.DeepEqual(gt.FilesTouched, wt.FilesTouched) {
		t.Errorf("Task fields: got %+v", gt)
	}
}
