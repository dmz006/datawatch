// BL403 — plugin widget push/read REST tests.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/plugins"
)

const widgetAdminToken = "admin-token"

// fakeWidgetSource serves manifest widgets from a map; a missing key is an
// unknown plugin.
type fakeWidgetSource map[string][]plugins.Widget

func (f fakeWidgetSource) Widgets(name string) ([]plugins.Widget, bool) {
	ws, ok := f[name]
	return ws, ok
}

type widgetTestEnv struct {
	s       *Server
	src     fakeWidgetSource
	store   *plugins.WidgetStore
	handler http.Handler // handlePlugins behind fedAuthMiddleware
}

func newWidgetTestEnv(t *testing.T) *widgetTestEnv {
	t.Helper()
	s := bl90Server(t)
	s.token = widgetAdminToken
	src := fakeWidgetSource{
		"svc": {
			{ID: "inbox", Surface: plugins.WidgetSurfaceDashboard, Kind: plugins.WidgetKindList, DataMode: plugins.WidgetDataPush},
			{ID: "health", Surface: plugins.WidgetSurfaceDashboard, Kind: plugins.WidgetKindStatusBadge, DataMode: plugins.WidgetDataPull, PullPath: "/health"},
		},
		"other": {
			{ID: "count", Surface: plugins.WidgetSurfaceDashboard, Kind: plugins.WidgetKindStatTile, DataMode: plugins.WidgetDataPush},
		},
	}
	store, err := plugins.NewWidgetStore("")
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := plugins.NewPluginTokenRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	s.SetPluginsAPI(&fakePluginsAPI{known: map[string]bool{"svc": true, "other": true}})
	s.SetPluginWidgets(src, store, tokens)
	return &widgetTestEnv{s: s, src: src, store: store,
		handler: s.fedAuthMiddleware(http.HandlerFunc(s.handlePlugins))}
}

func (e *widgetTestEnv) do(t *testing.T, method, path, bearer, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	e.handler.ServeHTTP(rr, req)
	return rr
}

func (e *widgetTestEnv) mint(t *testing.T, plugin string) string {
	t.Helper()
	rr := e.do(t, http.MethodPost, "/api/plugins/"+plugin+"/widgets/token", widgetAdminToken, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("mint %s: %d %s", plugin, rr.Code, rr.Body)
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || resp.Token == "" {
		t.Fatalf("mint body %q: %v", rr.Body, err)
	}
	return resp.Token
}

func wantStatus(t *testing.T, rr *httptest.ResponseRecorder, code int, contains string) {
	t.Helper()
	if rr.Code != code {
		t.Fatalf("status = %d, want %d (body %q)", rr.Code, code, rr.Body)
	}
	if contains != "" && !strings.Contains(rr.Body.String(), contains) {
		t.Fatalf("body %q does not contain %q", rr.Body, contains)
	}
}

func TestPluginWidgets_PushThenGet(t *testing.T) {
	e := newWidgetTestEnv(t)
	tok := e.mint(t, "svc")

	// The plugin authenticates with only its own token.
	rr := e.do(t, http.MethodPost, "/api/plugins/svc/widgets/inbox", tok, `{"items":["a","b"]}`)
	wantStatus(t, rr, http.StatusOK, "updated_at")

	v, ok := e.store.Get("svc", "inbox")
	if !ok || string(v.Data) != `{"items":["a","b"]}` || v.Source != plugins.WidgetSourcePush {
		t.Fatalf("stored value = %+v, %v", v, ok)
	}

	rr = e.do(t, http.MethodGet, "/api/plugins/svc/widgets/inbox", widgetAdminToken, "")
	wantStatus(t, rr, http.StatusOK, "")
	var one struct {
		ID    string               `json:"id"`
		Value *plugins.WidgetValue `json:"value"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.ID != "inbox" || one.Value == nil || string(one.Value.Data) != `{"items":["a","b"]}` {
		t.Fatalf("GET one = %s", rr.Body)
	}

	rr = e.do(t, http.MethodGet, "/api/plugins/svc/widgets", widgetAdminToken, "")
	wantStatus(t, rr, http.StatusOK, "")
	var list struct {
		Plugin  string `json:"plugin"`
		Widgets []struct {
			ID       string               `json:"id"`
			DataMode string               `json:"data_mode"`
			Value    *plugins.WidgetValue `json:"value"`
		} `json:"widgets"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Plugin != "svc" || len(list.Widgets) != 2 {
		t.Fatalf("GET list = %s", rr.Body)
	}
	if list.Widgets[0].ID != "inbox" || list.Widgets[0].Value == nil {
		t.Fatalf("inbox entry = %+v", list.Widgets[0])
	}
	if list.Widgets[1].ID != "health" || list.Widgets[1].DataMode != "pull" || list.Widgets[1].Value != nil {
		t.Fatalf("health entry = %+v", list.Widgets[1])
	}
}

func TestPluginWidgets_ReadsNeedOperatorAuth(t *testing.T) {
	e := newWidgetTestEnv(t)
	tok := e.mint(t, "svc")
	// The plugin token only opens the push route.
	wantStatus(t, e.do(t, http.MethodGet, "/api/plugins/svc/widgets", tok, ""), http.StatusUnauthorized, "")
	wantStatus(t, e.do(t, http.MethodGet, "/api/plugins/svc/widgets/inbox", tok, ""), http.StatusUnauthorized, "")
	wantStatus(t, e.do(t, http.MethodPost, "/api/plugins/svc/widgets/token", tok, ""), http.StatusUnauthorized, "")
	wantStatus(t, e.do(t, http.MethodGet, "/api/plugins/svc", tok, ""), http.StatusUnauthorized, "")
}

func TestPluginWidgets_PushUnknownPlugin(t *testing.T) {
	e := newWidgetTestEnv(t)
	// No token can exist for a plugin that was never registered.
	wantStatus(t, e.do(t, http.MethodPost, "/api/plugins/ghost/widgets/inbox", "anything", `1`),
		http.StatusUnauthorized, "")
	wantStatus(t, e.do(t, http.MethodPost, "/api/plugins/ghost/widgets/token", widgetAdminToken, ""),
		http.StatusNotFound, `plugin \"ghost\" not found`)

	// A plugin removed after its token was minted.
	tok := e.mint(t, "other")
	delete(e.src, "other")
	wantStatus(t, e.do(t, http.MethodPost, "/api/plugins/other/widgets/count", tok, `1`),
		http.StatusNotFound, `plugin \"other\" not found`)
}

func TestPluginWidgets_PushPullModeWidget(t *testing.T) {
	e := newWidgetTestEnv(t)
	tok := e.mint(t, "svc")
	wantStatus(t, e.do(t, http.MethodPost, "/api/plugins/svc/widgets/health", tok, `"ok"`),
		http.StatusConflict, `widget \"health\" is pull-mode; push is not accepted`)
	if _, ok := e.store.Get("svc", "health"); ok {
		t.Fatal("pull-mode push was stored")
	}
}

func TestPluginWidgets_PushUndeclaredWidget(t *testing.T) {
	e := newWidgetTestEnv(t)
	tok := e.mint(t, "svc")
	wantStatus(t, e.do(t, http.MethodPost, "/api/plugins/svc/widgets/nope", tok, `1`),
		http.StatusNotFound, `widget \"nope\" is not declared by plugin \"svc\"`)
	wantStatus(t, e.do(t, http.MethodGet, "/api/plugins/svc/widgets/nope", widgetAdminToken, ""),
		http.StatusNotFound, `widget \"nope\" is not declared by plugin \"svc\"`)
}

func TestPluginWidgets_PushAuthRejected(t *testing.T) {
	e := newWidgetTestEnv(t)
	e.mint(t, "svc")
	otherTok := e.mint(t, "other")
	const path = "/api/plugins/svc/widgets/inbox"

	cases := []struct{ name, bearer string }{
		{"missing", ""},
		{"wrong token", "not-the-token"},
		{"other plugin's token", otherTok},
		// The operator token passes the middleware but is not svc's push token.
		{"operator token", widgetAdminToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantStatus(t, e.do(t, http.MethodPost, path, tc.bearer, `1`), http.StatusUnauthorized, "")
		})
	}
	// Malformed Authorization header (not a Bearer scheme).
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`1`))
	req.Header.Set("Authorization", "Basic Zm9vOmJhcg==")
	rr := httptest.NewRecorder()
	e.handler.ServeHTTP(rr, req)
	wantStatus(t, rr, http.StatusUnauthorized, "")

	if _, ok := e.store.Get("svc", "inbox"); ok {
		t.Fatal("rejected push was stored")
	}
}

// With no operator token configured the middleware passes everything; the
// handler must still enforce the plugin token itself.
func TestPluginWidgets_PushAuthWithoutOperatorToken(t *testing.T) {
	e := newWidgetTestEnv(t)
	tok := e.mint(t, "svc")
	e.s.token = ""
	wantStatus(t, e.do(t, http.MethodPost, "/api/plugins/svc/widgets/inbox", "", `1`),
		http.StatusUnauthorized, "missing bearer token")
	wantStatus(t, e.do(t, http.MethodPost, "/api/plugins/svc/widgets/inbox", "wrong", `1`),
		http.StatusUnauthorized, "")
	wantStatus(t, e.do(t, http.MethodPost, "/api/plugins/svc/widgets/inbox", tok, `1`),
		http.StatusOK, "updated_at")
}

func TestPluginWidgets_PushBodyRejected(t *testing.T) {
	e := newWidgetTestEnv(t)
	tok := e.mint(t, "svc")
	const path = "/api/plugins/svc/widgets/inbox"

	big := `"` + strings.Repeat("x", plugins.MaxWidgetPayloadBytes) + `"`
	wantStatus(t, e.do(t, http.MethodPost, path, tok, big), http.StatusRequestEntityTooLarge, "byte limit")
	wantStatus(t, e.do(t, http.MethodPost, path, tok, `{not json`), http.StatusBadRequest, "not valid JSON")
	wantStatus(t, e.do(t, http.MethodPost, path, tok, ``), http.StatusBadRequest, "not valid JSON")
	if _, ok := e.store.Get("svc", "inbox"); ok {
		t.Fatal("rejected body was stored")
	}

	// Exactly at the limit is accepted.
	atLimit := `"` + strings.Repeat("x", plugins.MaxWidgetPayloadBytes-2) + `"`
	wantStatus(t, e.do(t, http.MethodPost, path, tok, atLimit), http.StatusOK, "")
}

func TestPluginWidgets_TokenRotation(t *testing.T) {
	e := newWidgetTestEnv(t)
	old := e.mint(t, "svc")
	cur := e.mint(t, "svc")
	if old == cur {
		t.Fatal("rotation returned the same token")
	}
	wantStatus(t, e.do(t, http.MethodPost, "/api/plugins/svc/widgets/inbox", old, `1`), http.StatusUnauthorized, "")
	wantStatus(t, e.do(t, http.MethodPost, "/api/plugins/svc/widgets/inbox", cur, `2`), http.StatusOK, "")
}

func TestPluginWidgets_Unwired(t *testing.T) {
	s := bl90Server(t)
	s.SetPluginsAPI(&fakePluginsAPI{known: map[string]bool{"svc": true}})
	rr := httptest.NewRecorder()
	s.handlePlugins(rr, httptest.NewRequest(http.MethodGet, "/api/plugins/svc/widgets", nil))
	wantStatus(t, rr, http.StatusServiceUnavailable, "")
}

func TestPluginPushTarget(t *testing.T) {
	cases := []struct {
		path string
		want string
		ok   bool
	}{
		{"/api/plugins/svc/widgets/inbox", "svc", true},
		{"/api/plugins/svc/widgets/token", "", false},
		{"/api/plugins/svc/widgets", "", false},
		{"/api/plugins/svc/widgets/", "", false},
		{"/api/plugins/svc/widgets/a/b", "", false},
		{"/api/plugins//widgets/a", "", false},
		{"/api/plugins/svc/enable", "", false},
		{"/api/other/svc/widgets/a", "", false},
	}
	for _, tc := range cases {
		got, ok := pluginPushTarget(tc.path)
		if got != tc.want || ok != tc.ok {
			t.Errorf("pluginPushTarget(%q) = %q, %v; want %q, %v", tc.path, got, ok, tc.want, tc.ok)
		}
	}
}
