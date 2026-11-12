// Configuration surfaces for autonomous.inherited_tags_cache_seconds — the
// lifetime, in whole seconds, of the shared cache of tags inherited from the
// PRD (0 = the 15-second default, negative = cache off).
//
// The comm-channel configure verb and `datawatch config set` are both
// generic over dotted keys (cmd/datawatch/configure_body.go sends the given
// key straight to PUT /api/config, with no per-key list to register in), so
// the PUT/GET /api/config round-trip below is exactly what those verbs do;
// the router.go help text that names the key is checked for source parity.
// The MCP surface (autonomous_config_set) is covered in internal/mcp,
// because internal/mcp imports this package and cannot be imported back
// from here.

package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func newInheritedTagsCacheServer(t *testing.T) (*Server, *session.Manager, string) {
	t.Helper()
	cfgPath := t.TempDir() + "/config.yaml"
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       cfgPath,
		autonomousMgr: &autonomousConfigPropagationSpy{},
	}
	sm, err := session.NewManager("h", t.TempDir(), "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	t.Cleanup(func() { _ = sm.Close() })
	s.manager = sm
	return s, sm, cfgPath
}

func TestInheritedTagsCache_ConfigRoundTrip(t *testing.T) {
	s, sm, cfgPath := newInheritedTagsCacheServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.inherited_tags_cache_seconds": 60})

	if got := string(getAutonomousConfigMap(t, s)["inherited_tags_cache_seconds"]); got != "60" {
		t.Fatalf("GET /api/config autonomous.inherited_tags_cache_seconds = %q, want 60", got)
	}
	if got := sm.InheritedTagsCacheTTL(); got != 60*time.Second {
		t.Fatalf("live session manager TTL = %v, want 60s (hot reload, no restart)", got)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "inherited_tags_cache_seconds: 60") {
		t.Fatalf("config.yaml lacks inherited_tags_cache_seconds: 60:\n%s", raw)
	}
}

// Zero and negative are both valid values here (0 = the 15-second default,
// negative = cache off) and are stored as given — unlike
// ci_watch_timeout_seconds, where a negative value is refused.
func TestInheritedTagsCache_ZeroAndNegativeAreStoredAsGiven(t *testing.T) {
	s, sm, _ := newInheritedTagsCacheServer(t)

	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.inherited_tags_cache_seconds": 0})
	if got := s.cfg.Autonomous.InheritedTagsCacheSeconds; got != 0 {
		t.Fatalf("stored value = %d, want 0 (the 15-second default at load)", got)
	}
	if got := sm.InheritedTagsCacheTTL(); got != 15*time.Second {
		t.Fatalf("manager TTL = %v after 0, want the 15-second default", got)
	}

	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.inherited_tags_cache_seconds": -1})
	if got := s.cfg.Autonomous.InheritedTagsCacheSeconds; got != -1 {
		t.Fatalf("stored value = %d, want -1 (cache off)", got)
	}
	if got := sm.InheritedTagsCacheTTL(); got != -1*time.Second {
		t.Fatalf("manager TTL = %v after -1, want -1 (cache off)", got)
	}
}

// GET /api/autonomous/config includes the key (it lives in the daemon config,
// not the manager's), which is what the MCP autonomous_config_get tool
// returns.
func TestInheritedTagsCache_AutonomousConfigGetIncludesTheKey(t *testing.T) {
	s, _, _ := newInheritedTagsCacheServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.inherited_tags_cache_seconds": 45})

	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/autonomous/config: want 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"inherited_tags_cache_seconds":45`) {
		t.Fatalf("GET /api/autonomous/config lacks inherited_tags_cache_seconds:45: %s", rr.Body.String())
	}
}

// The comm-channel configure-verb help text (router.go) names the key; the
// verb itself is generic and needs no per-key registration.
func TestInheritedTagsCache_RouterHelpTextNamesTheKey(t *testing.T) {
	readRel(t, "internal/router/router.go", "autonomous.inherited_tags_cache_seconds")
}

// GET /api/config default: the unset daemon value is 0 (the 15-second
// default at load), surfaced as a plain number.
func TestInheritedTagsCache_ConfigGetDefaultIsZero(t *testing.T) {
	s, _, _ := newInheritedTagsCacheServer(t)
	if got := string(getAutonomousConfigMap(t, s)["inherited_tags_cache_seconds"]); got != "0" {
		t.Fatalf("default = %s, want 0 (the 15-second default at load)", got)
	}
}
