// BL403 — federation capability tests for the plugin widget-data routes.
//
// Widget reads are gated on plugin:read and token minting on plugin:write.
// Neither cap is in any built-in group except full-control, so a peer
// holding read-only or config:* alone is refused. The plugin lifecycle
// routes in plugins.go stay on config:read / config:write.

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/plugins"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// newWidgetFedTestEnv wires the widget fixture from newWidgetTestEnv onto a
// federation-aware server and registers one peer per capability set.
func newWidgetFedTestEnv(t *testing.T) http.Handler {
	t.Helper()
	s, store, _ := newFedTestServer(t)
	src := fakeWidgetSource{
		"svc": {
			{ID: "inbox", Surface: plugins.WidgetSurfaceDashboard, Kind: plugins.WidgetKindList, DataMode: plugins.WidgetDataPush},
		},
	}
	ws, err := plugins.NewWidgetStore("")
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := plugins.NewPluginTokenRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	s.SetPluginsAPI(&fakePluginsAPI{known: map[string]bool{"svc": true}})
	s.SetPluginWidgets(src, ws, tokens)

	peers := []struct {
		token string
		caps  []string
	}{
		{"wtok-ro", []string{"read-only"}},
		{"wtok-config", []string{"config:read", "config:write"}},
		{"wtok-pread", []string{"plugin:read"}},
		{"wtok-pwrite", []string{"plugin:read", "plugin:write"}},
		{"wtok-full", []string{"full-control"}},
	}
	for _, p := range peers {
		name := "peer-" + strings.TrimPrefix(p.token, "wtok-")
		if err := store.Add(&multiserver.Entry{
			Name:         name,
			Token:        p.token,
			URL:          "http://" + name + ":8080",
			Enabled:      true,
			Federated:    true,
			Capabilities: p.caps,
		}); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	return s.fedAuthMiddleware(http.HandlerFunc(s.handlePlugins))
}

func widgetFedDo(t *testing.T, h http.Handler, method, path, token string) int {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Code
}

func TestPluginWidgetsFedCap(t *testing.T) {
	const (
		list  = "/api/plugins/svc/widgets"
		get   = "/api/plugins/svc/widgets/inbox"
		token = "/api/plugins/svc/widgets/token"
	)
	cases := []struct {
		peer                       string
		wantList, wantGet, wantTok int
	}{
		{"wtok-ro", http.StatusForbidden, http.StatusForbidden, http.StatusForbidden},
		{"wtok-config", http.StatusForbidden, http.StatusForbidden, http.StatusForbidden},
		{"wtok-pread", http.StatusOK, http.StatusOK, http.StatusForbidden},
		{"wtok-pwrite", http.StatusOK, http.StatusOK, http.StatusOK},
		{"wtok-full", http.StatusOK, http.StatusOK, http.StatusOK},
		{"admin-token", http.StatusOK, http.StatusOK, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.peer, func(t *testing.T) {
			h := newWidgetFedTestEnv(t)
			if got := widgetFedDo(t, h, http.MethodGet, list, c.peer); got != c.wantList {
				t.Errorf("GET %s = %d, want %d", list, got, c.wantList)
			}
			if got := widgetFedDo(t, h, http.MethodGet, get, c.peer); got != c.wantGet {
				t.Errorf("GET %s = %d, want %d", get, got, c.wantGet)
			}
			if got := widgetFedDo(t, h, http.MethodPost, token, c.peer); got != c.wantTok {
				t.Errorf("POST %s = %d, want %d", token, got, c.wantTok)
			}
		})
	}
}

// TestPluginLifecycleFedCapUnchanged — lifecycle routes still gate on
// config:*, so a config-only peer is allowed and a plugin-only peer is not.
func TestPluginLifecycleFedCapUnchanged(t *testing.T) {
	h := newWidgetFedTestEnv(t)
	if got := widgetFedDo(t, h, http.MethodGet, "/api/plugins/svc", "wtok-config"); got != http.StatusOK {
		t.Errorf("config peer GET /api/plugins/svc = %d, want 200", got)
	}
	if got := widgetFedDo(t, h, http.MethodGet, "/api/plugins/svc", "wtok-pwrite"); got != http.StatusForbidden {
		t.Errorf("plugin-only peer GET /api/plugins/svc = %d, want 403", got)
	}
}
