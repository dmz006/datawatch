// Configuration round-trip for rtk.write_rules_files on the surfaces the
// project keeps in parity with the other rtk.* settings: REST (GET and PUT,
// with the booleans and strings that the CLI, chat and MCP send), the Settings
// card and its five locale bundles (label and help text), and the docs.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func newRTKRulesServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	return &Server{cfg: config.DefaultConfig(), cfgPath: cfgPath}, cfgPath
}

// getRTKBlock returns the "rtk" object GET /api/config serves.
func getRTKBlock(t *testing.T, s *Server) map[string]json.RawMessage {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: %d", rr.Code)
	}
	var got struct {
		RTK map[string]json.RawMessage `json:"rtk"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.RTK == nil {
		t.Fatalf("GET /api/config lacks the rtk block: %s", rr.Body.String())
	}
	return got.RTK
}

func getWriteRulesFiles(t *testing.T, s *Server) bool {
	t.Helper()
	raw, ok := getRTKBlock(t, s)["write_rules_files"]
	if !ok {
		t.Fatal("GET /api/config rtk block lacks write_rules_files")
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("write_rules_files is not a boolean: %s", raw)
	}
	return v
}

func TestRTKWriteRulesFiles_DefaultIsFalse(t *testing.T) {
	s, _ := newRTKRulesServer(t)
	if s.cfg.RTK.WriteRulesFiles {
		t.Fatal("daemon config default must be false")
	}
	if getWriteRulesFiles(t, s) {
		t.Fatal("GET /api/config must report rtk.write_rules_files false by default")
	}
}

func TestRTKWriteRulesFiles_RESTRoundTrip(t *testing.T) {
	s, cfgPath := newRTKRulesServer(t)

	// A JSON boolean (what the Settings card sends).
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"rtk.write_rules_files": true})
	if !s.cfg.RTK.WriteRulesFiles || !getWriteRulesFiles(t, s) {
		t.Fatal("PUT true must turn the setting on and GET must show it")
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "write_rules_files: true") {
		t.Fatalf("config.yaml lacks write_rules_files: true:\n%s", raw)
	}

	// Turning it off again writes false back.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"rtk.write_rules_files": false})
	if s.cfg.RTK.WriteRulesFiles || getWriteRulesFiles(t, s) {
		t.Fatal("PUT false must turn the setting off")
	}
	raw, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "write_rules_files: false") {
		t.Fatalf("config.yaml lacks write_rules_files: false:\n%s", raw)
	}

	// A string (what datawatch config set, chat configure and MCP send).
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"rtk.write_rules_files": "true"})
	if !s.cfg.RTK.WriteRulesFiles || !getWriteRulesFiles(t, s) {
		t.Fatal(`PUT "true" must turn the setting on`)
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"rtk.write_rules_files": "false"})
	if s.cfg.RTK.WriteRulesFiles || getWriteRulesFiles(t, s) {
		t.Fatal(`PUT "false" must turn the setting off`)
	}
}

// Setting rtk.write_rules_files changes only that key: the neighbouring rtk
// settings keep their values.
func TestRTKWriteRulesFiles_DoesNotTouchOtherRTKSettings(t *testing.T) {
	s, _ := newRTKRulesServer(t)
	s.cfg.RTK.AutoInit = true
	s.cfg.RTK.Binary = "rtk-custom"
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"rtk.write_rules_files": true})
	if !s.cfg.RTK.AutoInit || s.cfg.RTK.Binary != "rtk-custom" {
		t.Fatalf("other rtk settings changed: auto_init=%v binary=%q", s.cfg.RTK.AutoInit, s.cfg.RTK.Binary)
	}
	block := getRTKBlock(t, s)
	for _, k := range []string{"enabled", "binary", "show_savings", "auto_init", "discover_interval", "auto_update", "update_check_interval", "write_rules_files"} {
		if _, ok := block[k]; !ok {
			t.Errorf("GET /api/config rtk block lacks %s", k)
		}
	}
}

func TestRTKWriteRulesFiles_SettingsCardAndLocales(t *testing.T) {
	app, err := os.ReadFile(filepath.Join("web", "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(app), "key: 'rtk.write_rules_files'") ||
		!strings.Contains(string(app), "labelKey: 'settings_rtk_write_rules_files'") ||
		!strings.Contains(string(app), "helpKey: 'settings_rtk_write_rules_files_help'") {
		t.Fatal("Settings card lacks rtk.write_rules_files with label and help keys")
	}
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		raw, err := os.ReadFile(filepath.Join("web", "locales", lang+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var bundle map[string]string
		if err := json.Unmarshal(raw, &bundle); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		for _, k := range []string{"settings_rtk_write_rules_files", "settings_rtk_write_rules_files_help"} {
			if strings.TrimSpace(bundle[k]) == "" {
				t.Errorf("%s locale lacks %s", lang, k)
			}
		}
		help := bundle["settings_rtk_write_rules_files_help"]
		for _, banned := range []string{"BL", "GH#"} {
			if strings.Contains(help, banned) {
				t.Errorf("%s help text carries a tracker ID (%q): %s", lang, banned, help)
			}
		}
	}

	// The English help text says what the setting does.
	raw, _ := os.ReadFile(filepath.Join("web", "locales", "en.json"))
	var en map[string]string
	if err := json.Unmarshal(raw, &en); err != nil {
		t.Fatal(err)
	}
	help := en["settings_rtk_write_rules_files_help"]
	for _, want := range []string{"AGENT.md", "CLAUDE.md", "never", "untracked", "people"} {
		if !strings.Contains(help, want) {
			t.Errorf("English help text lacks %q: %s", want, help)
		}
	}
}

func TestRTKWriteRulesFiles_Documented(t *testing.T) {
	for _, p := range []string{"../../docs/rtk-integration.md", "../../docs/config-reference.yaml"} {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "write_rules_files") {
			t.Errorf("%s does not document write_rules_files", p)
		}
	}
}
