// Settings coverage for the question quality check and the queue preview:
// the four rows next to the decision gate rows in the Autonomous Automata
// planning card of app.js, their labelKeys, the help link table entry that
// points each row at the definitions manual, and the locale keys in all five
// bundles. app.js and the manual are read as text.

package server

import (
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

// questionQualitySettings pairs each config key with the label key and the
// control type its settings row must carry.
var questionQualitySettings = []struct {
	configKey string
	labelKey  string
	rowType   string
}{
	{"autonomous.questions.quality_mode", "settings_automata_questions_quality_mode", "select"},
	{"autonomous.questions.quality_min_text_chars", "settings_automata_questions_quality_min_text_chars", "number"},
	{"autonomous.questions.quality_tracker_id_pattern", "settings_automata_questions_quality_tracker_id_pattern", "text"},
	{"autonomous.questions.queue_preview_lines", "settings_automata_questions_queue_preview_lines", "number"},
}

const questionQualityDocsAnchor = "automata-question-quality-and-queue-settings"

func readQuestionQualityAppJS(t *testing.T) string {
	t.Helper()
	raw, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("app.js missing from embed: %v", err)
	}
	return string(raw)
}

// settingsRowLine returns the one source line of the settings row for key.
func settingsRowLine(t *testing.T, src, configKey string) string {
	t.Helper()
	marker := "{ key: '" + configKey + "',"
	var found []string
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, marker) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("app.js has %d settings rows for %q, want exactly 1", len(found), configKey)
	}
	return found[0]
}

func TestQuestionQuality_SettingsRows(t *testing.T) {
	src := readQuestionQualityAppJS(t)
	gate := strings.Index(src, "key: 'autonomous.questions.decision_gate_mode'")
	if gate < 0 {
		t.Fatal("app.js has no decision_gate_mode row to sit next to")
	}
	for _, s := range questionQualitySettings {
		row := settingsRowLine(t, src, s.configKey)
		if !strings.Contains(row, "labelKey: '"+s.labelKey+"'") {
			t.Errorf("row %s: missing labelKey %q", s.configKey, s.labelKey)
		}
		if !strings.Contains(row, "type: '"+s.rowType+"'") {
			t.Errorf("row %s: want type %q", s.configKey, s.rowType)
		}
		if !strings.Contains(row, "docsKey: '"+s.labelKey+"'") {
			t.Errorf("row %s: missing docsKey %q", s.configKey, s.labelKey)
		}
		if !strings.Contains(row, "Automata") {
			t.Errorf("row %s: the plain English label must say Automata: %s", s.configKey, row)
		}
		if prdWord.MatchString(row) {
			t.Errorf("row %s: label uses PRD", s.configKey)
		}
		// Next to the decision gate rows: within the same questions block.
		pos := strings.Index(src, "key: '"+s.configKey+"'")
		if pos < gate || pos-gate > 3000 {
			t.Errorf("row %s is not next to the decision_gate_mode row", s.configKey)
		}
	}

	mode := settingsRowLine(t, src, "autonomous.questions.quality_mode")
	if !strings.Contains(mode, "options: ['off', 'warn', 'refuse']") {
		t.Errorf("quality_mode row must offer off, warn and refuse: %s", mode)
	}
	if !strings.Contains(mode, "default: 'warn'") {
		t.Errorf("quality_mode row must default to warn: %s", mode)
	}
}

func TestQuestionQuality_SettingsLinkTable(t *testing.T) {
	src := readQuestionQualityAppJS(t)
	start := strings.Index(src, "const DOCS_LINKS_BY_KEY = {")
	if start < 0 {
		t.Fatal("app.js has no DOCS_LINKS_BY_KEY table")
	}
	end := strings.Index(src[start:], "\n};")
	if end < 0 {
		t.Fatal("DOCS_LINKS_BY_KEY table does not close")
	}
	table := src[start : start+end]
	for _, s := range questionQualitySettings {
		want := s.labelKey + ": DOCS_DEFINITIONS + '#" + questionQualityDocsAnchor + "'"
		if !strings.Contains(table, want) {
			t.Errorf("DOCS_LINKS_BY_KEY has no entry %q", want)
		}
	}
	// The generic select and input renderers must turn docsKey into a link.
	if got := strings.Count(src, "${f.docsKey ? defsLink(f.docsKey) : ''}"); got < 2 {
		t.Errorf("the select and input row renderers must append defsLink(f.docsKey); found %d", got)
	}

	// The anchor must be a real heading in the manual.
	manual, err := os.ReadFile("../../docs/datawatch-definitions.md")
	if err != nil {
		t.Fatalf("read the definitions manual: %v", err)
	}
	nonSlug := regexp.MustCompile(`[^a-z0-9]+`)
	found := false
	for _, line := range strings.Split(string(manual), "\n") {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-") == questionQualityDocsAnchor {
			found = true
		}
	}
	if !found {
		t.Errorf("datawatch-definitions.md has no heading for anchor #%s", questionQualityDocsAnchor)
	}
	for _, s := range questionQualitySettings {
		if !strings.Contains(string(manual), "`"+s.configKey+"`") {
			t.Errorf("datawatch-definitions.md does not define %s", s.configKey)
		}
	}
}

func TestQuestionQuality_SettingsLocaleKeys(t *testing.T) {
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, s := range questionQualitySettings {
			value, ok := bundle[s.labelKey]
			if !ok {
				t.Errorf("locale %s: missing key %q", lang, s.labelKey)
				continue
			}
			checkQuestionSettingValue(t, lang, s.labelKey, value)
			if !strings.Contains(value, "Automata") {
				t.Errorf("locale %s: key %q must say Automata: %q", lang, s.labelKey, value)
			}
		}
	}
}
