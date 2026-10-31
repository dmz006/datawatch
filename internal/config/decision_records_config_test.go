// Decision record config keys (autonomous.decisions.*): every default, an
// explicit false or 0 surviving a round trip, each validator refusal, the file
// load refusing a bad value, and the follow_ups list.

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func decisionStrPtr(v string) *string { return &v }

func TestDecisionRecordConfig_Defaults(t *testing.T) {
	for name, d := range map[string]AutonomousDecisionsConfig{
		"zero value":     {},
		"default config": DefaultConfig().Autonomous.Decisions,
	} {
		if !d.EffectiveEnabled() {
			t.Errorf("%s: enabled should default true", name)
		}
		if got := d.EffectiveFolder(); got != "" {
			t.Errorf("%s: folder = %q, want empty (watch off)", name, got)
		}
		if got := d.EffectiveFolderGlob(); got != "*.md" {
			t.Errorf("%s: folder_glob = %q", name, got)
		}
		if got := d.EffectiveFrontMatterDelimiter(); got != "---" {
			t.Errorf("%s: front_matter_delimiter = %q", name, got)
		}
		if got := d.EffectiveFrontMatterSeparator(); got != ":" {
			t.Errorf("%s: front_matter_separator = %q", name, got)
		}
		if got := d.EffectiveFrontMatterFieldSeparator(); got != "|" {
			t.Errorf("%s: front_matter_field_separator = %q", name, got)
		}
		if got := d.EffectiveFrontMatterKeys(); got == nil || len(got) != 0 {
			t.Errorf("%s: front_matter_keys = %#v, want an empty map", name, got)
		}
		if !d.EffectiveFolderWithdrawOnDelete() {
			t.Errorf("%s: folder_withdraw_on_delete should default true", name)
		}
		if got := d.EffectiveClarityMode(); got != "warn" {
			t.Errorf("%s: clarity_mode = %q", name, got)
		}
		if got := d.EffectiveClarityMinTextChars(); got != 60 {
			t.Errorf("%s: clarity_min_text_chars = %d", name, got)
		}
		if !d.EffectiveClarityRequireTradeoffs() || !d.EffectiveClarityRequireLabel() {
			t.Errorf("%s: clarity_require_tradeoffs and clarity_require_label should default true", name)
		}
		if got := d.EffectiveClarityTrackerIDPattern(); got != DefaultQuestionQualityTrackerIDPattern {
			t.Errorf("%s: clarity_tracker_id_pattern = %q, want the built-in pattern", name, got)
		}
		if got := d.EffectiveDocRoot(); got != "" {
			t.Errorf("%s: doc_root = %q, want empty (working directory)", name, got)
		}
		if got := d.EffectiveDocStatusMode(); got != "report" {
			t.Errorf("%s: doc_status_mode = %q", name, got)
		}
		if got := d.EffectiveDocStatusPattern(); got != "^Confirmed:.*$" {
			t.Errorf("%s: doc_status_pattern = %q", name, got)
		}
		if got := d.EffectiveDocStatusTemplate(); got != `Confirmed: {choice} - "{quote}" ({actor}, {date})` {
			t.Errorf("%s: doc_status_template = %q", name, got)
		}
		if got := d.EffectiveFollowUps(); got == nil || len(got) != 0 {
			t.Errorf("%s: follow_ups = %#v, want an empty list", name, got)
		}
	}
	// A freshly marshalled default config stores no pointer value.
	d := DefaultConfig().Autonomous.Decisions
	if d.Enabled != nil || d.ClarityMode != nil || d.ClarityMinTextChars != nil || d.DocStatusMode != nil {
		t.Fatalf("defaults must be absent, not stored: %+v", d)
	}
}

// An explicit false and an explicit 0 are values, not an absent key: they
// survive a YAML and a JSON round trip.
func TestDecisionRecordConfig_RoundTrip(t *testing.T) {
	f := false
	zero := 0
	in := AutonomousDecisionsConfig{
		Enabled:                 &f,
		FolderWithdrawOnDelete:  &f,
		ClarityRequireTradeoffs: &f,
		ClarityRequireLabel:     &f,
		ClarityMinTextChars:     &zero,
		ClarityMode:             decisionStrPtr("refuse"),
		DocStatusMode:           decisionStrPtr("on"),
		Folder:                  "inbox/decisions",
		FolderGlob:              decisionStrPtr("*.txt"),
		DocRoot:                 "docs",
		FrontMatterKeys:         map[string]string{"title": "Title", "deciding": "Decide"},
	}
	check := func(label string, out AutonomousDecisionsConfig) {
		t.Helper()
		if out.EffectiveEnabled() || out.EffectiveFolderWithdrawOnDelete() ||
			out.EffectiveClarityRequireTradeoffs() || out.EffectiveClarityRequireLabel() {
			t.Errorf("%s: an explicit false came back true: %+v", label, out)
		}
		if out.ClarityMinTextChars == nil || *out.ClarityMinTextChars != 0 || out.EffectiveClarityMinTextChars() != 0 {
			t.Errorf("%s: an explicit 0 did not survive: %v", label, out.ClarityMinTextChars)
		}
		if out.EffectiveClarityMode() != "refuse" || out.EffectiveDocStatusMode() != "on" {
			t.Errorf("%s: modes = %q, %q", label, out.EffectiveClarityMode(), out.EffectiveDocStatusMode())
		}
		if out.EffectiveFolder() != "inbox/decisions" || out.EffectiveFolderGlob() != "*.txt" || out.EffectiveDocRoot() != "docs" {
			t.Errorf("%s: folder keys = %q %q %q", label, out.EffectiveFolder(), out.EffectiveFolderGlob(), out.EffectiveDocRoot())
		}
		if !reflect.DeepEqual(out.EffectiveFrontMatterKeys(), in.FrontMatterKeys) {
			t.Errorf("%s: front_matter_keys = %v", label, out.EffectiveFrontMatterKeys())
		}
	}

	y, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, frag := range []string{"enabled: false", "clarity_min_text_chars: 0", "folder_withdraw_on_delete: false"} {
		if !strings.Contains(string(y), frag) {
			t.Errorf("YAML lacks %q:\n%s", frag, y)
		}
	}
	var fromYAML AutonomousDecisionsConfig
	if err := yaml.Unmarshal(y, &fromYAML); err != nil {
		t.Fatal(err)
	}
	check("yaml", fromYAML)

	j, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, frag := range []string{`"enabled":false`, `"clarity_min_text_chars":0`} {
		if !strings.Contains(string(j), frag) {
			t.Errorf("JSON lacks %q: %s", frag, j)
		}
	}
	var fromJSON AutonomousDecisionsConfig
	if err := json.Unmarshal(j, &fromJSON); err != nil {
		t.Fatal(err)
	}
	check("json", fromJSON)

	// Through the whole config file: write, load, read back.
	cfg := DefaultConfig()
	cfg.Autonomous.Decisions = in
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	check("file", loaded.Autonomous.Decisions)
}

func TestDecisionRecordConfig_RejectsBadValues(t *testing.T) {
	neg := -1
	cases := []struct {
		name  string
		d     AutonomousDecisionsConfig
		named string
	}{
		{"clarity_mode outside its set", AutonomousDecisionsConfig{ClarityMode: decisionStrPtr("strict")}, "clarity_mode"},
		{"clarity_mode wrong case", AutonomousDecisionsConfig{ClarityMode: decisionStrPtr("WARN")}, "clarity_mode"},
		{"doc_status_mode outside its set", AutonomousDecisionsConfig{DocStatusMode: decisionStrPtr("apply")}, "doc_status_mode"},
		{"clarity_tracker_id_pattern does not compile", AutonomousDecisionsConfig{ClarityTrackerIDPattern: decisionStrPtr("[unclosed")}, "clarity_tracker_id_pattern"},
		{"doc_status_pattern does not compile", AutonomousDecisionsConfig{DocStatusPattern: decisionStrPtr("(?P<")}, "doc_status_pattern"},
		{"negative clarity_min_text_chars", AutonomousDecisionsConfig{ClarityMinTextChars: &neg}, "clarity_min_text_chars"},
		{"empty front_matter_delimiter", AutonomousDecisionsConfig{FrontMatterDelimiter: decisionStrPtr("")}, "front_matter_delimiter"},
		{"empty front_matter_separator", AutonomousDecisionsConfig{FrontMatterSeparator: decisionStrPtr("")}, "front_matter_separator"},
		{"empty front_matter_field_separator", AutonomousDecisionsConfig{FrontMatterFieldSeparator: decisionStrPtr("")}, "front_matter_field_separator"},
		{"follow-up with an empty name", AutonomousDecisionsConfig{FollowUps: []AutonomousDecisionFollowUp{{Kind: "start_template", Target: "t"}}}, "name is empty"},
		{"follow-up with another kind", AutonomousDecisionsConfig{FollowUps: []AutonomousDecisionFollowUp{{Name: "a", Kind: "run_shell", Target: "t"}}}, "invalid kind"},
		{"follow-up with an empty target", AutonomousDecisionsConfig{FollowUps: []AutonomousDecisionFollowUp{{Name: "a", Kind: "open_automaton"}}}, "target is empty"},
		{"follow-ups with a duplicate name", AutonomousDecisionsConfig{FollowUps: []AutonomousDecisionFollowUp{
			{Name: "a", Kind: "open_automaton", Target: "x"}, {Name: "a", Kind: "start_template", Target: "y"}}}, "duplicate name"},
	}
	for _, c := range cases {
		err := c.d.validate()
		if err == nil {
			t.Errorf("%s: validate accepted it", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.named) {
			t.Errorf("%s: error %q should name %q", c.name, err, c.named)
		}
	}

	// The exported validators the PUT path and the other surfaces call.
	for name, err := range map[string]error{
		"mode":            ValidateDecisionClarityMode("loud"),
		"doc status mode": ValidateDecisionDocStatusMode("loud"),
		"min chars":       ValidateDecisionClarityMinTextChars(-5),
		"tracker pattern": ValidateDecisionClarityTrackerIDPattern("("),
		"status pattern":  ValidateDecisionDocStatusPattern("("),
		"delimiter":       ValidateDecisionFrontMatterDelimiter(""),
		"separator":       ValidateDecisionFrontMatterSeparator(""),
		"field separator": ValidateDecisionFrontMatterFieldSeparator(""),
		"follow-ups":      ValidateDecisionFollowUps([]AutonomousDecisionFollowUp{{Name: "", Kind: "", Target: ""}}),
	} {
		if err == nil {
			t.Errorf("%s: validator accepted a bad value", name)
		}
	}
	// Every valid value is accepted.
	for _, mode := range []string{"", "off", "warn", "refuse"} {
		if err := ValidateDecisionClarityMode(mode); err != nil {
			t.Errorf("clarity_mode %q refused: %v", mode, err)
		}
	}
	for _, mode := range []string{"", "off", "report", "on"} {
		if err := ValidateDecisionDocStatusMode(mode); err != nil {
			t.Errorf("doc_status_mode %q refused: %v", mode, err)
		}
	}
	if err := ValidateDecisionClarityMinTextChars(0); err != nil {
		t.Errorf("0 refused: %v", err)
	}
	if err := ValidateDecisionClarityTrackerIDPattern(""); err != nil {
		t.Errorf("empty tracker pattern refused: %v", err)
	}
}

// The config file load refuses a bad value and names the key.
func TestDecisionRecordConfig_LoadRefusesBadFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	for _, c := range []struct{ body, named string }{
		{"clarity_mode: strict", "clarity_mode"},
		{"doc_status_mode: apply", "doc_status_mode"},
		{"doc_status_pattern: '[bad'", "doc_status_pattern"},
		{"clarity_min_text_chars: -3", "clarity_min_text_chars"},
		{"front_matter_delimiter: ''", "front_matter_delimiter"},
		{"follow_ups:\n      - {name: a, kind: nope, target: t}", "follow_ups"},
	} {
		if err := os.WriteFile(p, []byte("autonomous:\n  decisions:\n    "+c.body+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(p)
		if err == nil {
			t.Errorf("Load accepted %q", c.body)
			continue
		}
		if !strings.Contains(err.Error(), c.named) {
			t.Errorf("Load error for %q = %v, want it to name %q", c.body, err, c.named)
		}
	}

	good := "autonomous:\n  decisions:\n    clarity_mode: refuse\n    doc_status_mode: on\n    clarity_min_text_chars: 0\n"
	if err := os.WriteFile(p, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load refused a valid file: %v", err)
	}
	d := cfg.Autonomous.Decisions
	if d.EffectiveClarityMode() != "refuse" || d.EffectiveDocStatusMode() != "on" || d.EffectiveClarityMinTextChars() != 0 {
		t.Fatalf("loaded values wrong: %+v", d)
	}
}

func TestDecisionRecordConfig_FollowUpsRoundTrip(t *testing.T) {
	want := []AutonomousDecisionFollowUp{
		{Name: "kick off the build", Kind: "start_template", Target: "tmpl-build"},
		{Name: "open the review", Kind: "open_automaton", Target: "ab12cd34"},
	}
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultConfig()
	cfg.Autonomous.Decisions.FollowUps = want
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, frag := range []string{"follow_ups:", "kind: start_template", "target: ab12cd34"} {
		if !strings.Contains(string(raw), frag) {
			t.Errorf("config file lacks %q:\n%s", frag, raw)
		}
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := loaded.Autonomous.Decisions.EffectiveFollowUps(); !reflect.DeepEqual(got, want) {
		t.Fatalf("follow_ups = %#v, want %#v", got, want)
	}

	j, err := json.Marshal(cfg.Autonomous.Decisions)
	if err != nil {
		t.Fatal(err)
	}
	var back AutonomousDecisionsConfig
	if err := json.Unmarshal(j, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.FollowUps, want) {
		t.Fatalf("JSON follow_ups = %#v", back.FollowUps)
	}

	// The Effective helper hands out a copy: editing it leaves the config alone.
	got := loaded.Autonomous.Decisions.EffectiveFollowUps()
	got[0].Name = "changed"
	if loaded.Autonomous.Decisions.FollowUps[0].Name != want[0].Name {
		t.Fatal("EffectiveFollowUps returned the stored slice, not a copy")
	}
}

// Empty text on a key that has a default reads as the default, so a cleared
// field in a settings form does not leave the feature without a pattern.
func TestDecisionRecordConfig_EmptyTextReadsAsDefault(t *testing.T) {
	d := AutonomousDecisionsConfig{
		FolderGlob:              decisionStrPtr(""),
		ClarityMode:             decisionStrPtr(""),
		ClarityTrackerIDPattern: decisionStrPtr(""),
		DocStatusMode:           decisionStrPtr(""),
		DocStatusPattern:        decisionStrPtr(""),
		DocStatusTemplate:       decisionStrPtr(""),
	}
	if err := d.validate(); err != nil {
		t.Fatalf("empty text is valid for these keys: %v", err)
	}
	if d.EffectiveFolderGlob() != "*.md" || d.EffectiveClarityMode() != "warn" ||
		d.EffectiveClarityTrackerIDPattern() != DefaultQuestionQualityTrackerIDPattern ||
		d.EffectiveDocStatusMode() != "report" || d.EffectiveDocStatusPattern() != "^Confirmed:.*$" ||
		d.EffectiveDocStatusTemplate() != DefaultDecisionDocStatusTemplate {
		t.Fatalf("empty text did not read as the default: %+v", d)
	}
}
