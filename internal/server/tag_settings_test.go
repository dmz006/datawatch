package server

// Tests for the autonomous.tags settings on the REST config surface, and for
// the refusal text the other surfaces must carry. The comm, MCP and CLI
// surfaces all send their value through PUT /api/config, so the REST handler's
// message is the one they show.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func newTagSettingsServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	return &Server{cfg: config.DefaultConfig(), cfgPath: cfgPath}, cfgPath
}

// getTagSettings returns the autonomous.tags object of GET /api/config.
func getTagSettings(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var top struct {
		Autonomous struct {
			Tags map[string]interface{} `json:"tags"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &top); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return top.Autonomous.Tags
}

// putTagBody sends a raw PUT /api/config body and returns the recorder.
func putTagBody(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
	return rr
}

// putTagPatch sends a PUT /api/config patch built from body.
func putTagPatch(t *testing.T, s *Server, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	return putTagBody(t, s, string(b))
}

func TestTagSettings_DefaultsOnGet(t *testing.T) {
	s, _ := newTagSettingsServer(t)
	want := map[string]interface{}{
		"default_view":          "auto",
		"attention_statuses":    []interface{}{"blocked", "needs_review", "paused"},
		"default_match_mode":    "any",
		"direct_counts_visible": false,
		"registry_keeper":       "",
		"registry_sync_seconds": float64(30),
	}
	if got := getTagSettings(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET autonomous.tags = %v, want %v", got, want)
	}
}

func TestTagSettings_PutThenGetRoundTrip(t *testing.T) {
	s, _ := newTagSettingsServer(t)
	rr := putTagPatch(t, s, map[string]interface{}{
		"autonomous.tags.default_view":          "tags",
		"autonomous.tags.attention_statuses":    []string{"failed", "blocked"},
		"autonomous.tags.default_match_mode":    "all",
		"autonomous.tags.direct_counts_visible": true,
		"autonomous.tags.registry_keeper":       "peer-b",
		"autonomous.tags.registry_sync_seconds": 60,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	want := map[string]interface{}{
		"default_view":          "tags",
		"attention_statuses":    []interface{}{"failed", "blocked"},
		"default_match_mode":    "all",
		"direct_counts_visible": true,
		"registry_keeper":       "peer-b",
		"registry_sync_seconds": float64(60),
	}
	if got := getTagSettings(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET after PUT = %v, want %v", got, want)
	}
	if got := s.cfg.Autonomous.Tags.RegistrySyncSeconds; got != 60 {
		t.Fatalf("stored registry_sync_seconds = %d, want 60", got)
	}
}

func TestTagSettings_ExplicitFalseAndEmptySurviveSaveAndLoad(t *testing.T) {
	s, cfgPath := newTagSettingsServer(t)
	putTagPatch(t, s, map[string]interface{}{
		"autonomous.tags.direct_counts_visible": true,
		"autonomous.tags.registry_keeper":       "peer-a",
	})
	rr := putTagPatch(t, s, map[string]interface{}{
		"autonomous.tags.direct_counts_visible": false,
		"autonomous.tags.registry_keeper":       "",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT explicit false/empty: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	for _, want := range []string{"direct_counts_visible: false", `registry_keeper: ""`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("config.yaml lacks explicit %q:\n%s", want, raw)
		}
	}
	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if back.Autonomous.Tags.DirectCountsVisible || back.Autonomous.Tags.RegistryKeeper != "" {
		t.Fatalf("explicit false/empty did not survive a reload: %+v", back.Autonomous.Tags)
	}
}

// TestTagSettings_ConfigureBodyRoundTrip sends the bodies the comm channel and
// the CLI build for `configure <key>=<value>`: a bare value is a string, and a
// number or a bool is sent as itself.
func TestTagSettings_ConfigureBodyRoundTrip(t *testing.T) {
	s, _ := newTagSettingsServer(t)
	for _, body := range []string{
		`{"autonomous.tags.attention_statuses":"blocked,paused"}`,
		`{"autonomous.tags.direct_counts_visible":true}`,
		`{"autonomous.tags.registry_sync_seconds":"90"}`,
		`{"autonomous.tags.default_view":"attention"}`,
	} {
		if rr := putTagBody(t, s, body); rr.Code != http.StatusOK {
			t.Fatalf("PUT %s: want 200, got %d body=%s", body, rr.Code, rr.Body.String())
		}
	}
	got := getTagSettings(t, s)
	if !reflect.DeepEqual(got["attention_statuses"], []interface{}{"blocked", "paused"}) {
		t.Fatalf("attention_statuses = %v, want [blocked paused]", got["attention_statuses"])
	}
	if got["direct_counts_visible"] != true || got["registry_sync_seconds"] != float64(90) || got["default_view"] != "attention" {
		t.Fatalf("configure round trip lost a value: %v", got)
	}
}

// TestTagSettings_InvalidValuesRefusedWithSameMessage checks that an invalid
// status or mode is refused with exactly the shared validator's text, whether
// the value arrives as a string (comm, MCP, CLI) or as a list or quoted value.
func TestTagSettings_InvalidValuesRefusedWithSameMessage(t *testing.T) {
	cases := []struct {
		name      string
		key       string
		body      string
		validated interface{}
	}{
		{"status as string", "autonomous.tags.attention_statuses", `{"autonomous.tags.attention_statuses":"bogus"}`, "bogus"},
		{"status as list", "autonomous.tags.attention_statuses", `{"autonomous.tags.attention_statuses":["blocked","bogus"]}`, []interface{}{"blocked", "bogus"}},
		{"mode as string", "autonomous.tags.default_match_mode", `{"autonomous.tags.default_match_mode":"sometimes"}`, "sometimes"},
		{"sync below minimum", "autonomous.tags.registry_sync_seconds", `{"autonomous.tags.registry_sync_seconds":4}`, float64(4)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTagSettingsServer(t)
			before := s.cfg.Autonomous.Tags
			rr := putTagBody(t, s, tc.body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
			}
			want := config.ValidateTagSetting(tc.key, tc.validated)
			if want == nil {
				t.Fatalf("validator accepted %v; the case is wrong", tc.validated)
			}
			if got := strings.TrimSpace(rr.Body.String()); got != want.Error() {
				t.Fatalf("REST message = %q, want the shared validator text %q", got, want.Error())
			}
			if !reflect.DeepEqual(s.cfg.Autonomous.Tags, before) {
				t.Fatalf("a refused PUT changed the block: %+v", s.cfg.Autonomous.Tags)
			}
		})
	}
}

func TestTagSettings_RejectedPutChangesNothing(t *testing.T) {
	s, _ := newTagSettingsServer(t)
	rr := putTagPatch(t, s, map[string]interface{}{
		"autonomous.tags.default_view":          "tags",
		"autonomous.tags.registry_sync_seconds": 2,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "autonomous.tags.registry_sync_seconds: must be a whole number of seconds >= 5") {
		t.Fatalf("message = %q", rr.Body.String())
	}
	if s.cfg.Autonomous.Tags.DefaultView != "auto" {
		t.Fatalf("a refused PUT still applied default_view = %q", s.cfg.Autonomous.Tags.DefaultView)
	}
}

func TestTagSettings_AttentionStatusesReachTheAttentionLookup(t *testing.T) {
	s, _ := newTagSettingsServer(t)
	if rr := putTagBody(t, s, `{"autonomous.tags.attention_statuses":"failed"}`); rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d", rr.Code)
	}
	if got := autonomous.ResolveAttentionStatuses(s.tagSettingLookup); !reflect.DeepEqual(got, []string{"failed"}) {
		t.Fatalf("attention statuses = %v, want [failed]", got)
	}
}

func TestTagSettings_ServerWiresTheAttentionLookup(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	srv := NewServer(NewHub(), nil, "h", "", nil, config.DefaultConfig(), cfgPath)
	if srv.autonomousSettings == nil {
		t.Fatal("NewServer did not wire autonomousSettings to the tag block")
	}
	got, ok := srv.autonomousSettings(autonomous.AttentionStatusesKey)
	if !ok || !reflect.DeepEqual(got, config.DefaultTagAttentionStatuses) {
		t.Fatalf("lookup = %v, %v; want the default attention statuses", got, ok)
	}
}

// TestTagSettings_CommHelpListsEveryKeyTwice follows the other configure help
// checks: each tag key must appear in both help blocks of the comm channel.
func TestTagSettings_CommHelpListsEveryKeyTwice(t *testing.T) {
	keys := []string{
		"autonomous.tags.default_view",
		"autonomous.tags.attention_statuses",
		"autonomous.tags.default_match_mode",
		"autonomous.tags.direct_counts_visible",
		"autonomous.tags.registry_keeper",
		"autonomous.tags.registry_sync_seconds",
	}
	help := readRel(t, "internal/router/router.go", keys...)
	for _, k := range keys {
		if c := strings.Count(help, k); c < 2 {
			t.Errorf("comm help documents %s %d time(s); want it in both Common keys and Configurable keys", k, c)
		}
	}
}

// TestTagSettings_MCPConfigSetDescriptionNamesTheKeys checks that the generic
// config_set tool, the MCP route for these keys, names them.
func TestTagSettings_MCPConfigSetDescriptionNamesTheKeys(t *testing.T) {
	readRel(t, "internal/mcp/memory_tools.go",
		"autonomous.tags.default_view",
		"autonomous.tags.attention_statuses",
		"autonomous.tags.default_match_mode",
		"autonomous.tags.direct_counts_visible",
		"autonomous.tags.registry_keeper",
		"autonomous.tags.registry_sync_seconds",
	)
}

// TestTagSettings_PutRefusalIsPlainText checks the refusal is written as the
// bare message (http.Error adds a trailing newline), so every surface can show
// it without parsing.
func TestTagSettings_PutRefusalIsPlainText(t *testing.T) {
	s, _ := newTagSettingsServer(t)
	rr := putTagBody(t, s, `{"autonomous.tags.default_match_mode":"sometimes"}`)
	if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", got)
	}
	if !bytes.HasPrefix(rr.Body.Bytes(), []byte("autonomous.tags.default_match_mode: must be any or all")) {
		t.Fatalf("body = %q", rr.Body.String())
	}
}
