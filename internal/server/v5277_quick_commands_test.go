// v5.27.7 (BL209, datawatch#28) — tests for /api/quick_commands.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestHandleQuickCommands_DefaultBaseline(t *testing.T) {
	s := bl90Server(t)
	// cfg.Session.QuickCommands deliberately left empty to exercise
	// the default-fallback branch.

	req := httptest.NewRequest(http.MethodGet, "/api/quick_commands", nil)
	rr := httptest.NewRecorder()
	s.handleQuickCommands(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Commands []config.QuickCommand `json:"commands"`
		Source   string                `json:"source"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Source != "default" {
		t.Errorf("source=%q want default", got.Source)
	}
	if len(got.Commands) < 5 {
		t.Errorf("default list too short (%d) — should have at least yes/no/continue/skip/Esc", len(got.Commands))
	}
	wantLabels := map[string]bool{"yes": false, "no": false, "Esc": false}
	for _, c := range got.Commands {
		if _, ok := wantLabels[c.Label]; ok {
			wantLabels[c.Label] = true
		}
	}
	for label, present := range wantLabels {
		if !present {
			t.Errorf("default list missing %q", label)
		}
	}
}

func TestHandleQuickCommands_OperatorOverride(t *testing.T) {
	s := bl90Server(t)
	s.cfg.Session.QuickCommands = []config.QuickCommand{
		{Label: "approve", Value: "approve\n", Category: "project"},
		{Label: "reject", Value: "reject\n", Category: "project"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/quick_commands", nil)
	rr := httptest.NewRecorder()
	s.handleQuickCommands(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var got struct {
		Commands []config.QuickCommand `json:"commands"`
		Source   string                `json:"source"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Source != "config" {
		t.Errorf("source=%q want config", got.Source)
	}
	if len(got.Commands) != 2 {
		t.Errorf("got %d commands want 2 (operator override should NOT include defaults)", len(got.Commands))
	}
	if got.Commands[0].Label != "approve" {
		t.Errorf("[0].Label=%q want approve", got.Commands[0].Label)
	}
}

func TestHandleQuickCommands_RejectsPost(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/quick_commands", nil)
	rr := httptest.NewRecorder()
	s.handleQuickCommands(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST should be 405, got %d", rr.Code)
	}
}

func TestDefaultQuickCommands_ContainsKeyShortcutsWithKeyPrefix(t *testing.T) {
	defaults := defaultQuickCommands()
	keyShortcutsFound := 0
	for _, c := range defaults {
		if c.Category == "keys" {
			if len(c.Value) < 4 || c.Value[:4] != "key:" {
				t.Errorf("category=keys entry %q has Value=%q — must start with `key:`", c.Label, c.Value)
			}
			keyShortcutsFound++
		}
	}
	if keyShortcutsFound < 5 {
		t.Errorf("expected at least 5 key-prefix entries, got %d", keyShortcutsFound)
	}
}
