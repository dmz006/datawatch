// Decision record config keys (autonomous.decisions.*) over REST. GET
// /api/config returns the effective values, an explicit value (false, 0, a
// list, a map) survives a PUT and reaches config.yaml, and a bad value is a 400
// that names the key, applies nothing and writes nothing.

package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

type decisionRecordSurfaceView struct {
	Enabled                   bool                                `json:"enabled"`
	Folder                    string                              `json:"folder"`
	FolderGlob                string                              `json:"folder_glob"`
	FrontMatterDelimiter      string                              `json:"front_matter_delimiter"`
	FrontMatterSeparator      string                              `json:"front_matter_separator"`
	FrontMatterFieldSeparator string                              `json:"front_matter_field_separator"`
	FrontMatterKeys           map[string]string                   `json:"front_matter_keys"`
	FolderWithdrawOnDelete    bool                                `json:"folder_withdraw_on_delete"`
	ClarityMode               string                              `json:"clarity_mode"`
	ClarityMinTextChars       int                                 `json:"clarity_min_text_chars"`
	ClarityRequireTradeoffs   bool                                `json:"clarity_require_tradeoffs"`
	ClarityRequireLabel       bool                                `json:"clarity_require_label"`
	ClarityTrackerIDPattern   string                              `json:"clarity_tracker_id_pattern"`
	DocRoot                   string                              `json:"doc_root"`
	DocStatusMode             string                              `json:"doc_status_mode"`
	DocStatusPattern          string                              `json:"doc_status_pattern"`
	DocStatusTemplate         string                              `json:"doc_status_template"`
	FollowUps                 []config.AutonomousDecisionFollowUp `json:"follow_ups"`
}

func getDecisionRecordSurface(t *testing.T, s *Server) decisionRecordSurfaceView {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["decisions"]
	if !ok {
		t.Fatal("GET /api/config autonomous map lacks decisions")
	}
	var got decisionRecordSurfaceView
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode decisions: %v", err)
	}
	return got
}

func TestDecisionRecordConfigSurface_PutGet(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)

	// Defaults come out of GET with nothing stored.
	got := getDecisionRecordSurface(t, s)
	want := decisionRecordSurfaceView{
		Enabled: true, FolderGlob: "*.md",
		FrontMatterDelimiter: "---", FrontMatterSeparator: ":", FrontMatterFieldSeparator: "|",
		FrontMatterKeys: map[string]string{}, FolderWithdrawOnDelete: true,
		ClarityMode: "warn", ClarityMinTextChars: 60, ClarityRequireTradeoffs: true, ClarityRequireLabel: true,
		ClarityTrackerIDPattern: autonomous.DefaultQualityTrackerIDPattern,
		DocStatusMode:           "report", DocStatusPattern: "^Confirmed:.*$",
		DocStatusTemplate: `Confirmed: {choice} - "{quote}" ({actor}, {date})`,
		FollowUps:         []config.AutonomousDecisionFollowUp{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GET defaults = %+v\nwant %+v", got, want)
	}
	if d := s.cfg.Autonomous.Decisions; d.Enabled != nil || d.ClarityMode != nil || d.ClarityMinTextChars != nil || d.DocStatusMode != nil {
		t.Fatalf("stored values must be absent by default, got %+v", d)
	}

	// Explicit values, including false and 0, survive a PUT and a GET.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.decisions.enabled":                      false,
		"autonomous.decisions.folder":                       "inbox/decisions",
		"autonomous.decisions.folder_glob":                  "*.txt",
		"autonomous.decisions.front_matter_delimiter":       "+++",
		"autonomous.decisions.front_matter_separator":       "=",
		"autonomous.decisions.front_matter_field_separator": ";",
		"autonomous.decisions.front_matter_keys":            map[string]any{"title": "Title"},
		"autonomous.decisions.folder_withdraw_on_delete":    false,
		"autonomous.decisions.clarity_mode":                 "refuse",
		"autonomous.decisions.clarity_min_text_chars":       0,
		"autonomous.decisions.clarity_require_tradeoffs":    false,
		"autonomous.decisions.clarity_require_label":        false,
		"autonomous.decisions.clarity_tracker_id_pattern":   `\bQ-[0-9]+\b`,
		"autonomous.decisions.doc_root":                     "docs",
		"autonomous.decisions.doc_status_mode":              "on",
		"autonomous.decisions.doc_status_pattern":           `^Status:.*$`,
		"autonomous.decisions.doc_status_template":          "Status: {choice_label} ({decision_id})",
		"autonomous.decisions.follow_ups": []any{
			map[string]any{"name": "build", "kind": "start_template", "target": "tmpl-1"},
			map[string]any{"name": "review", "kind": "open_automaton", "target": "ab12cd34"},
		},
	})
	got = getDecisionRecordSurface(t, s)
	want = decisionRecordSurfaceView{
		Folder: "inbox/decisions", FolderGlob: "*.txt",
		FrontMatterDelimiter: "+++", FrontMatterSeparator: "=", FrontMatterFieldSeparator: ";",
		FrontMatterKeys: map[string]string{"title": "Title"},
		ClarityMode:     "refuse", ClarityMinTextChars: 0,
		ClarityTrackerIDPattern: `\bQ-[0-9]+\b`, DocRoot: "docs",
		DocStatusMode: "on", DocStatusPattern: `^Status:.*$`, DocStatusTemplate: "Status: {choice_label} ({decision_id})",
		FollowUps: []config.AutonomousDecisionFollowUp{
			{Name: "build", Kind: "start_template", Target: "tmpl-1"},
			{Name: "review", Kind: "open_automaton", Target: "ab12cd34"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GET after PUT = %+v\nwant %+v", got, want)
	}
	d := s.cfg.Autonomous.Decisions
	if d.Enabled == nil || *d.Enabled || d.ClarityMinTextChars == nil || *d.ClarityMinTextChars != 0 {
		t.Fatalf("an explicit false and 0 must be stored as such, got %+v", d)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	for _, frag := range []string{"decisions:", "clarity_mode: refuse", "clarity_min_text_chars: 0", "doc_status_mode: \"on\"", "follow_ups:", "kind: open_automaton"} {
		if !strings.Contains(string(raw), frag) {
			t.Errorf("config.yaml lacks %q:\n%s", frag, raw)
		}
	}

	// The comm channel and the CLI send text: a JSON text for the two
	// structured keys is accepted, and an empty list clears the follow-ups.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.decisions.follow_ups":        `[{"name":"only","kind":"open_automaton","target":"x1"}]`,
		"autonomous.decisions.front_matter_keys": `{"deciding":"Decide"}`,
	})
	got = getDecisionRecordSurface(t, s)
	if len(got.FollowUps) != 1 || got.FollowUps[0].Name != "only" || !reflect.DeepEqual(got.FrontMatterKeys, map[string]string{"deciding": "Decide"}) {
		t.Fatalf("text form of the structured keys not applied: %+v", got)
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.decisions.follow_ups": []any{}})
	if got = getDecisionRecordSurface(t, s); len(got.FollowUps) != 0 {
		t.Fatalf("an empty list must clear the follow-ups: %+v", got.FollowUps)
	}
}

func TestDecisionRecordConfigSurface_PutRefusesBad(t *testing.T) {
	cases := []struct {
		key, value, named string
	}{
		{"autonomous.decisions.enabled", `"yes"`, "boolean"},
		{"autonomous.decisions.folder_withdraw_on_delete", `1`, "boolean"},
		{"autonomous.decisions.clarity_require_label", `"false"`, "boolean"},
		{"autonomous.decisions.clarity_mode", `"strict"`, "refuse"},
		{"autonomous.decisions.clarity_mode", `true`, "refuse"},
		{"autonomous.decisions.doc_status_mode", `"apply"`, "report"},
		{"autonomous.decisions.doc_status_mode", `3`, "report"},
		{"autonomous.decisions.clarity_min_text_chars", `-1`, ">= 0"},
		{"autonomous.decisions.clarity_min_text_chars", `"many"`, ">= 0"},
		{"autonomous.decisions.clarity_min_text_chars", `12.5`, ">= 0"},
		{"autonomous.decisions.clarity_tracker_id_pattern", `"[unclosed"`, "regular expression"},
		{"autonomous.decisions.doc_status_pattern", `"(?P<"`, "regular expression"},
		{"autonomous.decisions.doc_status_pattern", `7`, "regular expression"},
		{"autonomous.decisions.front_matter_delimiter", `""`, "empty"},
		{"autonomous.decisions.front_matter_separator", `""`, "empty"},
		{"autonomous.decisions.front_matter_field_separator", `""`, "empty"},
		{"autonomous.decisions.folder", `5`, "text"},
		{"autonomous.decisions.front_matter_keys", `{"title": 4}`, "must be text"},
		{"autonomous.decisions.front_matter_keys", `"not json"`, "front_matter_keys"},
		{"autonomous.decisions.follow_ups", `[{"name":"","kind":"start_template","target":"t"}]`, "name is empty"},
		{"autonomous.decisions.follow_ups", `[{"name":"a","kind":"run_shell","target":"t"}]`, "invalid kind"},
		{"autonomous.decisions.follow_ups", `[{"name":"a","kind":"open_automaton","target":""}]`, "target is empty"},
		{"autonomous.decisions.follow_ups", `[{"name":"a","kind":"open_automaton","target":"x"},{"name":"a","kind":"start_template","target":"y"}]`, "duplicate name"},
		{"autonomous.decisions.follow_ups", `{"name":"a"}`, "follow_ups"},
	}
	for _, c := range cases {
		s, _, cfgPath := newMaxConcurrentTasksServer(t)
		// Sent beside a valid key: the whole PUT is refused, so that key must
		// not land either.
		body := `{"autonomous.decisions.folder": "keep", "` + c.key + `": ` + c.value + `}`
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s=%s: want 400, got %d (%s)", c.key, c.value, rr.Code, rr.Body.String())
		}
		if msg := rr.Body.String(); !strings.Contains(msg, c.key) || !strings.Contains(msg, c.named) {
			t.Errorf("400 for %s=%s should name the key and %q; got %q", c.key, c.value, c.named, msg)
		}
		d := s.cfg.Autonomous.Decisions
		if d.Folder != "" || d.Enabled != nil || d.ClarityMode != nil || d.DocStatusMode != nil ||
			d.ClarityMinTextChars != nil || len(d.FollowUps) != 0 || len(d.FrontMatterKeys) != 0 {
			t.Fatalf("a rejected PUT (%s=%s) changed the stored config: %+v", c.key, c.value, d)
		}
		if got := getDecisionRecordSurface(t, s); got.ClarityMode != "warn" || got.ClarityMinTextChars != 60 || got.Folder != "" {
			t.Fatalf("GET after rejected PUT %s=%s: %+v", c.key, c.value, got)
		}
		if _, err := os.Stat(cfgPath); err == nil {
			t.Fatalf("a rejected PUT (%s=%s) wrote config.yaml", c.key, c.value)
		}
	}
}

// The PWA Settings list names every autonomous.decisions key, each with a
// label in every locale bundle, and no label uses the PRD word.
func TestDecisionRecordConfigSurface_PWASettings(t *testing.T) {
	keys := []string{
		"enabled", "folder", "folder_glob", "front_matter_delimiter",
		"front_matter_separator", "front_matter_field_separator", "front_matter_keys",
		"folder_withdraw_on_delete", "clarity_mode", "clarity_min_text_chars",
		"clarity_require_tradeoffs", "clarity_require_label", "clarity_tracker_id_pattern",
		"doc_root", "doc_status_mode", "doc_status_pattern", "doc_status_template", "follow_ups",
	}
	appJS, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("app.js missing from embed: %v", err)
	}
	src := string(appJS)
	for _, k := range keys {
		if !strings.Contains(src, "key: 'autonomous.decisions."+k+"'") {
			t.Errorf("app.js settings list has no entry for autonomous.decisions.%s", k)
		}
		if !strings.Contains(src, "labelKey: 'settings_automata_decisions_"+k+"'") {
			t.Errorf("app.js entry for autonomous.decisions.%s has no labelKey", k)
		}
	}
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, k := range keys {
			v, ok := bundle["settings_automata_decisions_"+k]
			if !ok || strings.TrimSpace(v) == "" {
				t.Errorf("locale %s: missing or empty settings_automata_decisions_%s", lang, k)
				continue
			}
			if prdWord.MatchString(v) || strings.Contains(v, "Automation") {
				t.Errorf("locale %s: settings_automata_decisions_%s uses a reserved word: %q", lang, k, v)
			}
		}
	}
}
