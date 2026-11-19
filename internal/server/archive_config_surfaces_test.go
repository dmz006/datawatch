// autonomous.archive.* must be readable and writable through GET and PUT
// /api/config, and a bad value must reject the whole PUT.

package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

func archiveConfigField(t *testing.T, s *Server, field string) string {
	t.Helper()
	var archive map[string]json.RawMessage
	raw, ok := getAutonomousConfigMap(t, s)["archive"]
	if !ok {
		t.Fatalf("GET /api/config autonomous lacks archive")
	}
	if err := json.Unmarshal(raw, &archive); err != nil {
		t.Fatalf("decode archive: %v", err)
	}
	return strings.Trim(string(archive[field]), `"`)
}

func TestArchiveConfigDefaultsOnGet(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	if got := archiveConfigField(t, s, "enabled"); got != "true" {
		t.Errorf("default enabled = %q, want true", got)
	}
	if got := archiveConfigField(t, s, "retention_days"); got != "0" {
		t.Errorf("default retention_days = %q, want 0", got)
	}
}

func TestArchiveConfigRESTSettable(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.archive.retention_days": 45,
		"autonomous.archive.enabled":        false,
	})
	if got := archiveConfigField(t, s, "retention_days"); got != "45" {
		t.Errorf("retention_days = %q, want 45", got)
	}
	if got := archiveConfigField(t, s, "enabled"); got != "false" {
		t.Errorf("enabled = %q, want false", got)
	}
	saved, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "retention_days: 45") {
		t.Fatalf("config file lacks retention_days:\n%s", saved)
	}
	// Strings from the CLI and chat channel are accepted.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.archive.retention_days": "10", "autonomous.archive.enabled": "true",
	})
	if got := archiveConfigField(t, s, "retention_days"); got != "10" {
		t.Errorf("retention_days from string = %q", got)
	}
	if got := archiveConfigField(t, s, "enabled"); got != "true" {
		t.Errorf("enabled from string = %q", got)
	}
}

func TestArchiveConfigRESTRejectsBadValues(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.archive.retention_days": 20})
	for key, bads := range map[string][]any{
		"autonomous.archive.retention_days": {-1, 2.5, "soon"},
		"autonomous.archive.enabled":        {"maybe", 3},
	} {
		for _, bad := range bads {
			if code := progressPutStatus(t, s, map[string]any{key: bad}); code != http.StatusBadRequest {
				t.Errorf("PUT %s=%v: want 400, got %d", key, bad, code)
			}
		}
	}
	if got := archiveConfigField(t, s, "retention_days"); got != "20" {
		t.Errorf("retention_days after rejected PUTs = %q, want 20", got)
	}
}
