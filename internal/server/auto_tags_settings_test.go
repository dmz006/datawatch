package server

// Tests for the autonomous.auto_tags block on the REST config surface, and
// for the refusal text the other surfaces must carry. The comm, MCP and CLI
// surfaces all send their value through PUT /api/config, so the REST
// handler's message is the one they show.

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

	"github.com/dmz006/datawatch/internal/config"
)

func newAutoTagsSettingsServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	return &Server{cfg: config.DefaultConfig(), cfgPath: cfgPath}, cfgPath
}

// getAutoTagsSettings returns the autonomous.auto_tags object of GET /api/config.
func getAutoTagsSettings(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var top struct {
		Autonomous struct {
			AutoTags map[string]interface{} `json:"auto_tags"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &top); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return top.Autonomous.AutoTags
}

// putAutoTagsBody sends a raw PUT /api/config body and returns the recorder.
func putAutoTagsBody(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
	return rr
}

// putAutoTagsPatch sends a PUT /api/config patch built from body.
func putAutoTagsPatch(t *testing.T, s *Server, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	return putAutoTagsBody(t, s, string(b))
}

func TestAutoTagsSettings_DefaultsOnGet(t *testing.T) {
	s, _ := newAutoTagsSettingsServer(t)
	want := map[string]interface{}{
		"enabled":             true,
		"automation_template": "automation:{automation}",
		"stage_template":      "stage:{stage}",
		"kind_template":       "kind:{kind}",
		"kind_labels":         map[string]interface{}{"feature": "feature", "bug": "bug", "other": "maintenance"},
	}
	if got := getAutoTagsSettings(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET autonomous.auto_tags = %v, want %v", got, want)
	}
}

func TestAutoTagsSettings_PutThenGetRoundTrip(t *testing.T) {
	s, _ := newAutoTagsSettingsServer(t)
	rr := putAutoTagsPatch(t, s, map[string]interface{}{
		"autonomous.auto_tags.enabled":             true,
		"autonomous.auto_tags.automation_template": "work/{automation}",
		"autonomous.auto_tags.stage_template":      "phase:{stage}",
		"autonomous.auto_tags.kind_template":       "kind:{kind}",
		"autonomous.auto_tags.kind_labels":         map[string]string{"other": "support"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	got := getAutoTagsSettings(t, s)
	if got["automation_template"] != "work/{automation}" || got["stage_template"] != "phase:{stage}" {
		t.Fatalf("round trip lost the templates: %v", got)
	}
	if labels, ok := got["kind_labels"].(map[string]interface{}); !ok || labels["other"] != "support" {
		t.Fatalf("round trip lost kind_labels: %v", got)
	}
}

// TestAutoTagsSettings_ConfigureBodyRoundTrip sends the bodies the comm
// channel and the CLI build for `configure <key>=<value>`: a bare value is a
// string, and a bool is sent as itself.
func TestAutoTagsSettings_ConfigureBodyRoundTrip(t *testing.T) {
	s, _ := newAutoTagsSettingsServer(t)
	for _, body := range []string{
		`{"autonomous.auto_tags.enabled":"false"}`,
		`{"autonomous.auto_tags.automation_template":"auto:{automation}"}`,
		`{"autonomous.auto_tags.kind_labels":"feature:feat,bug:fix"}`,
	} {
		if rr := putAutoTagsBody(t, s, body); rr.Code != http.StatusOK {
			t.Fatalf("PUT %s: want 200, got %d body=%s", body, rr.Code, rr.Body.String())
		}
	}
	got := getAutoTagsSettings(t, s)
	if got["enabled"] != false || got["automation_template"] != "auto:{automation}" {
		t.Fatalf("configure round trip lost a value: %v", got)
	}
	labels, ok := got["kind_labels"].(map[string]interface{})
	if !ok || labels["feature"] != "feat" || labels["bug"] != "fix" {
		t.Fatalf("configure round trip lost kind_labels: %v", got)
	}
}

func TestAutoTagsSettings_InvalidRefusedWithSameMessage(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		key   string
		value interface{}
	}{
		{"unknown placeholder", `{"autonomous.auto_tags.stage_template":"stage:{owner}"}`, "autonomous.auto_tags.stage_template", "stage:{owner}"},
		{"empty template", `{"autonomous.auto_tags.kind_template":""}`, "autonomous.auto_tags.kind_template", ""},
		{"bad kind", `{"autonomous.auto_tags.kind_labels":{"epic":"e"}}`, "autonomous.auto_tags.kind_labels", map[string]interface{}{"epic": "e"}},
		{"bad enabled", `{"autonomous.auto_tags.enabled":"maybe"}`, "autonomous.auto_tags.enabled", "maybe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newAutoTagsSettingsServer(t)
			before := s.cfg.Autonomous.AutoTags
			rr := putAutoTagsBody(t, s, tc.body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
			}
			want := config.ValidateAutoTagsSetting(tc.key, tc.value)
			if want == nil {
				t.Fatalf("validator accepted %v; the case is wrong", tc.value)
			}
			if got := strings.TrimSpace(rr.Body.String()); got != want.Error() {
				t.Fatalf("REST message = %q, want the shared validator text %q", got, want.Error())
			}
			if !reflect.DeepEqual(s.cfg.Autonomous.AutoTags, before) {
				t.Fatalf("a refused PUT changed the block: %+v", s.cfg.Autonomous.AutoTags)
			}
		})
	}
}

func TestAutoTagsSettings_RejectedPutChangesNothing(t *testing.T) {
	s, _ := newAutoTagsSettingsServer(t)
	rr := putAutoTagsPatch(t, s, map[string]interface{}{
		"autonomous.auto_tags.kind_template": "work/{kind}",
		"autonomous.auto_tags.kind_labels":   map[string]string{"epic": "e"},
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}
	if got := getAutoTagsSettings(t, s)["kind_template"]; got != "kind:{kind}" {
		t.Fatalf("a refused PUT still applied kind_template = %q", got)
	}
}

func TestAutoTagsSettings_ExplicitFalseSurvivesSaveAndLoad(t *testing.T) {
	s, cfgPath := newAutoTagsSettingsServer(t)
	if rr := putAutoTagsBody(t, s, `{"autonomous.auto_tags.enabled":true}`); rr.Code != http.StatusOK {
		t.Fatalf("PUT true: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if rr := putAutoTagsBody(t, s, `{"autonomous.auto_tags.enabled":false}`); rr.Code != http.StatusOK {
		t.Fatalf("PUT false: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "enabled: false") {
		t.Fatalf("config.yaml lacks an explicit enabled: false:\n%s", raw)
	}
	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if back.Autonomous.AutoTags.On() {
		t.Fatalf("an explicit enabled: false did not survive a reload: %+v", back.Autonomous.AutoTags)
	}
}

// TestAutoTagsSettings_MCPConfigSetDescriptionNamesTheKeys checks that the
// generic config_set tool, the MCP route for these keys, names them.
func TestAutoTagsSettings_MCPConfigSetDescriptionNamesTheKeys(t *testing.T) {
	readRel(t, "internal/mcp/memory_tools.go",
		"autonomous.auto_tags.enabled",
		"autonomous.auto_tags.automation_template",
		"autonomous.auto_tags.stage_template",
		"autonomous.auto_tags.kind_template",
		"autonomous.auto_tags.kind_labels",
	)
}

// TestAutoTagsSettings_PutRefusalIsPlainText checks the refusal is written as
// the bare message (http.Error adds a trailing newline), so every surface can
// show it without parsing.
func TestAutoTagsSettings_PutRefusalIsPlainText(t *testing.T) {
	s, _ := newAutoTagsSettingsServer(t)
	rr := putAutoTagsBody(t, s, `{"autonomous.auto_tags.kind_template":"stage:{owner}"}`)
	if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", got)
	}
	if !bytes.HasPrefix(rr.Body.Bytes(), []byte("autonomous.auto_tags.kind_template:")) {
		t.Fatalf("body = %q", rr.Body.String())
	}
}
