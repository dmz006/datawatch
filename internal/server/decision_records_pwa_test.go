package server

// PWA-side surface for the decision records card and the two places it shows
// up: the card at the top of the Automata list, and the record id on the
// per-Automaton decision log line. Together with
// decision_records_config_surface_test.go this covers the whole
// autonomous.decisions.* surface; those tests cover the config values and
// these cover the view and its wiring.

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// decisionPWAKeys is the set of autonomous.decisions.* keys that the PWA
// settings panel exposes as rows (short suffix after the prefix).
var decisionPWAKeys = []string{
	"enabled", "folder", "folder_glob", "folder_withdraw_on_delete",
	"front_matter_delimiter", "front_matter_separator", "front_matter_field_separator", "front_matter_keys",
	"clarity_mode", "clarity_min_text_chars", "clarity_require_tradeoffs", "clarity_require_label", "clarity_tracker_id_pattern",
	"doc_root", "doc_status_mode", "doc_status_pattern", "doc_status_template", "follow_ups",
}

func decisionAppSource(t *testing.T) string {
	t.Helper()
	appJS, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("app.js missing from embed: %v", err)
	}
	return string(appJS)
}

func TestDecisionRecordPWA_CardFunctionsPresent(t *testing.T) {
	src := decisionAppSource(t)
	want := []string{
		"window.decisionRecordCardHtml",
		"window.decisionRecordCardFailedHtml",
		"window.loadDecisionRecordCard",
		"function _decisionRecordRow(",
		"function _decisionRecordFolderLine(",
		"_decisionRecordSlotHtml(",
		"'drCard'",
	}
	for _, s := range want {
		if !strings.Contains(src, s) {
			t.Errorf("app.js is missing %q", s)
		}
	}
}

func TestDecisionRecordPWA_CardWiredIntoAutomataPanel(t *testing.T) {
	src := decisionAppSource(t)
	if !strings.Contains(src, "_decisionRecordSlotHtml()") {
		t.Fatal("the Automata panel does not prepend the decision records slot")
	}
	if strings.Count(src, "window.loadDecisionRecordCard") < 2 {
		t.Fatal("the card loader is defined but never called from the Automata render callback")
	}
	// The fetch URL is the decision records list, not the prd-scoped
	// decision line read.
	if !strings.Contains(src, "'/api/autonomous/decision-records?limit=100'") {
		t.Fatal("the card does not fetch /api/autonomous/decision-records")
	}
}

func TestDecisionRecordPWA_RecordIDOnDecisionLine(t *testing.T) {
	src := decisionAppSource(t)
	for _, s := range []string{"decision-record-ref-id", "d.record_id"} {
		if !strings.Contains(src, s) {
			t.Errorf("decision log row does not show the record id (missing %q)", s)
		}
	}
}

// The settings rows use the settings_automata_decisions_ label and docs keys,
// and every one of them points at the definitions anchor.
func TestDecisionRecordPWA_SettingsRowsUseAutomataPrefix(t *testing.T) {
	src := decisionAppSource(t)
	for _, k := range decisionPWAKeys {
		row := "'autonomous.decisions." + k + "'"
		label := "settings_automata_decisions_" + k
		if !strings.Contains(src, row) {
			t.Errorf("GENERAL_CONFIG_FIELDS has no row for autonomous.decisions.%s", k)
			continue
		}
		if !strings.Contains(src, "docsKey: '"+label+"'") {
			t.Errorf("row for autonomous.decisions.%s has no docsKey settings_automata_decisions_%s", k, k)
		}
		// The folder row opens its own how-to; every other row opens the glossary anchor.
		if k == "folder" {
			if !strings.Contains(src, label+": 'howto/automaton-decision-records.md'") {
				t.Errorf("DOCS_LINKS_BY_KEY has no %s entry pointing at the decision records how-to", label)
			}
			continue
		}
		if !strings.Contains(src, label+": DOCS_DEFINITIONS") {
			t.Errorf("DOCS_LINKS_BY_KEY has no %s entry", label)
		}
	}
	if !strings.Contains(src, "decision_record_card_title: DOCS_DEFINITIONS") {
		t.Error("DOCS_LINKS_BY_KEY has no decision_record_card_title entry")
	}
}

func TestDecisionRecordPWA_RowLabelsLocalize(t *testing.T) {
	src := decisionAppSource(t)
	for _, k := range decisionPWAKeys {
		needle := "labelKey: 'settings_automata_decisions_" + k + "'"
		if !strings.Contains(src, needle) {
			t.Errorf("the row for autonomous.decisions.%s does not carry the labelKey settings_automata_decisions_%s", k, k)
		}
	}
}

func TestDecisionRecordPWA_DefinitionsDocHasTheSection(t *testing.T) {
	raw, err := os.ReadFile("../../docs/datawatch-definitions.md")
	if err != nil {
		t.Fatalf("docs/datawatch-definitions.md: %v", err)
	}
	doc := string(raw)
	if !strings.Contains(doc, "### Decision records in the PWA") {
		t.Fatal("docs/datawatch-definitions.md has no '### Decision records in the PWA' section")
	}
	if !strings.Contains(doc, "### Decision record settings") {
		t.Fatal("docs/datawatch-definitions.md lost the '### Decision record settings' section (the docs anchor target)")
	}
	if !strings.Contains(doc, "folder watch") || !strings.Contains(doc, "status line") {
		t.Error("the PWA section does not say what the folder watch status line shows")
	}
	for _, k := range decisionPWAKeys {
		if !strings.Contains(doc, "autonomous.decisions."+k) {
			t.Errorf("docs do not explain autonomous.decisions.%s", k)
		}
	}
}
