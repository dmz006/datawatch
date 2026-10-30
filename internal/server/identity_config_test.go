// identity.backups_to_keep over the config API: GET /api/config returns the
// effective value, a PUT stores it and reaches config.yaml, a bad value is a
// 400 that names the key and changes nothing, and an identity manager wired
// to the same config prunes to the new count on its next write.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/identity"
)

func getIdentityBackupsToKeep(t *testing.T, s *Server) int {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var got struct {
		Identity struct {
			BackupsToKeep *int `json:"backups_to_keep"`
		} `json:"identity"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Identity.BackupsToKeep == nil {
		t.Fatal("GET /api/config lacks identity.backups_to_keep")
	}
	return *got.Identity.BackupsToKeep
}

func putIdentityRaw(t *testing.T, s *Server, rawValue string) (int, string) {
	t.Helper()
	body := bytes.NewBufferString(`{"identity.backups_to_keep": ` + rawValue + `}`)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", body))
	return rr.Code, rr.Body.String()
}

func TestIdentityBackupsToKeep_ConfigAPI(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)

	if got := getIdentityBackupsToKeep(t, s); got != 10 {
		t.Fatalf("GET default = %d, want 10", got)
	}
	if s.cfg.Identity.BackupsToKeep != nil {
		t.Fatalf("stored value must be absent by default, got %d", *s.cfg.Identity.BackupsToKeep)
	}

	// Identity manager wired the way main.go does it: the provider reads the
	// same config the server mutates.
	mgr, err := identity.NewManager(filepath.Join(t.TempDir(), "identity.yaml"))
	if err != nil {
		t.Fatalf("identity.NewManager: %v", err)
	}
	mgr.SetBackupsToKeep(func() int { return s.cfg.Identity.EffectiveBackupsToKeep() })
	roles := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	for _, r := range roles {
		if _, err := mgr.Set(identity.Identity{Role: r}); err != nil {
			t.Fatalf("Set %s: %v", r, err)
		}
	}
	if bs, err := mgr.ListBackups(); err != nil || len(bs) != len(roles)-1 {
		t.Fatalf("before the PUT want %d backups (limit 10), got %d err=%v", len(roles)-1, len(bs), err)
	}

	// An explicit value survives a PUT, a GET and reaches config.yaml.
	if code, body := putIdentityRaw(t, s, `3`); code != http.StatusOK {
		t.Fatalf("PUT 3: want 200, got %d %s", code, body)
	}
	if got := getIdentityBackupsToKeep(t, s); got != 3 {
		t.Fatalf("GET after PUT 3 = %d, want 3", got)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "backups_to_keep: 3") {
		t.Errorf("config.yaml lacks backups_to_keep: 3:\n%s", raw)
	}

	// Each bad value is refused, names the key, and leaves the 3 in place.
	for _, bad := range []string{`0`, `-1`, `"abc"`, `1.5`, `null`, `true`} {
		code, body := putIdentityRaw(t, s, bad)
		if code != http.StatusBadRequest {
			t.Errorf("PUT %s: want 400, got %d %s", bad, code, body)
			continue
		}
		if !strings.Contains(body, "identity.backups_to_keep") || !strings.Contains(body, ">= 1") {
			t.Errorf("PUT %s: message %q should name the key and the rule", bad, body)
		}
		if got := getIdentityBackupsToKeep(t, s); got != 3 {
			t.Errorf("after refused PUT %s the value is %d, want 3", bad, got)
		}
	}

	// The next identity write prunes to 3, with no restart.
	if _, err := mgr.Set(identity.Identity{Role: "z"}); err != nil {
		t.Fatalf("Set after PUT: %v", err)
	}
	bs, err := mgr.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(bs) != 3 {
		t.Fatalf("after the PUT and one write want 3 backups, got %d", len(bs))
	}
}
