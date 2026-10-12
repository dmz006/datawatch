// BL403 — REST surface for plugin widget values.
//
// Endpoints (dispatched from handlePlugins):
//
//	POST /api/plugins/{name}/widgets/token        mint/rotate the plugin's push token (operator auth; peers need plugin:write)
//	POST /api/plugins/{name}/widgets/{widget_id}  push a value (plugin push token)
//	GET  /api/plugins/{name}/widgets              declared widgets + latest values (operator auth; peers need plugin:read)
//	GET  /api/plugins/{name}/widgets/{widget_id}  one widget + latest value (operator auth; peers need plugin:read)
//
// Federation caps: widget data is gated on plugin:read / plugin:write, not
// the config:* caps that gate the plugin lifecycle routes in plugins.go.
//
// Push auth: `Authorization: Bearer <token>` where the token was minted by
// the token endpoint for the same plugin. fedAuthMiddleware accepts such a
// token on the push route alone (see pluginPushTarget), so a plugin process
// needs only its own token, never the operator token.
//
// Push body: the request body IS the widget value — any JSON document,
// stored as-is in WidgetValue.Data. This matches the pull side for service
// plugins, which stores the GET response body as-is.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/plugins"
)

// PluginWidgetSource looks up the widgets a plugin declares in its
// manifest. Implemented by plugins.API.
type PluginWidgetSource interface {
	Widgets(name string) ([]plugins.Widget, bool)
}

// PluginTokensAPI is the widget push-token registry surface the REST layer
// needs. Implemented by plugins.PluginTokenRegistry.
type PluginTokensAPI interface {
	Register(plugin string) (string, error)
	Verify(plugin, token string) error
}

// SetPluginWidgets wires the widget surface. Any nil argument leaves
// /api/plugins/{name}/widgets* returning 503.
func (s *Server) SetPluginWidgets(src PluginWidgetSource, store *plugins.WidgetStore, tokens PluginTokensAPI) {
	s.pluginWidgetSrc = src
	s.widgetStore = store
	s.pluginTokens = tokens
}

// pluginPushTarget reports the plugin name when path is a widget push
// route, /api/plugins/{name}/widgets/{widget_id}. The token-mint route
// (widget id "token") is not a push route.
func pluginPushTarget(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, "/api/plugins/")
	if !ok {
		return "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] != "widgets" ||
		parts[2] == "" || parts[2] == plugins.ReservedWidgetTokenID {
		return "", false
	}
	return parts[0], true
}

// pluginPushFromContext returns the plugin a request authenticated as via
// its widget push token in fedAuthMiddleware, or "".
func pluginPushFromContext(ctx context.Context) string {
	p, _ := ctx.Value(pluginPushKey).(string)
	return p
}

// verifyPluginPushToken checks a plugin push token for the push route of
// r. Used by fedAuthMiddleware's narrow fallback branch.
func (s *Server) verifyPluginPushToken(r *http.Request, tok string) (string, bool) {
	if s.pluginTokens == nil || tok == "" || r.Method != http.MethodPost {
		return "", false
	}
	name, ok := pluginPushTarget(r.URL.Path)
	if !ok || s.pluginTokens.Verify(name, tok) != nil {
		return "", false
	}
	return name, true
}

// handlePluginWidgets serves /api/plugins/{name}/widgets[/{widget_id}].
// sub is the part after "widgets" with its leading slash removed.
func (s *Server) handlePluginWidgets(w http.ResponseWriter, r *http.Request, name, sub string) {
	if s.pluginWidgetSrc == nil || s.widgetStore == nil || s.pluginTokens == nil {
		jsonError(w, http.StatusServiceUnavailable, "plugin widgets are not available")
		return
	}
	if strings.Contains(sub, "/") {
		jsonError(w, http.StatusNotFound, "unknown widgets path")
		return
	}
	switch sub {
	case "":
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handlePluginWidgetList(w, r, name)
	case plugins.ReservedWidgetTokenID:
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handlePluginWidgetToken(w, r, name)
	default:
		switch r.Method {
		case http.MethodGet:
			s.handlePluginWidgetGet(w, r, name, sub)
		case http.MethodPost:
			s.handlePluginWidgetPush(w, r, name, sub)
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// handlePluginWidgetToken mints (or rotates) the plugin's push token and
// returns it once.
func (s *Server) handlePluginWidgetToken(w http.ResponseWriter, r *http.Request, name string) {
	if !s.fedCap(w, r, federation.CapPluginWrite) {
		return
	}
	if _, ok := s.pluginWidgetSrc.Widgets(name); !ok {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("plugin %q not found", name))
		return
	}
	token, err := s.pluginTokens.Register(name)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r.Context(), "plugin_widget_token_issue", "plugin", name, nil)
	writeJSONOK(w, map[string]string{"token": token})
}

// handlePluginWidgetPush stores a pushed value for a push-mode widget.
func (s *Server) handlePluginWidgetPush(w http.ResponseWriter, r *http.Request, name, widgetID string) {
	if !s.authorizePluginPush(w, r, name) {
		return
	}
	widgets, ok := s.pluginWidgetSrc.Widgets(name)
	if !ok {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("plugin %q not found", name))
		return
	}
	wd, ok := plugins.Manifest{Widgets: widgets}.Widget(widgetID)
	if !ok {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("widget %q is not declared by plugin %q", widgetID, name))
		return
	}
	if wd.DataMode != plugins.WidgetDataPush {
		jsonError(w, http.StatusConflict, fmt.Sprintf("widget %q is pull-mode; push is not accepted", widgetID))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, plugins.MaxWidgetPayloadBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			jsonError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("widget payload is over the %d byte limit", plugins.MaxWidgetPayloadBytes))
			return
		}
		jsonError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	if !json.Valid(body) {
		jsonError(w, http.StatusBadRequest, "widget payload is not valid JSON")
		return
	}
	if err := s.widgetStore.Set(plugins.WidgetValue{
		Plugin: name, WidgetID: widgetID, Data: body, Source: plugins.WidgetSourcePush,
	}); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	v, _ := s.widgetStore.Get(name, widgetID)
	writeJSONOK(w, map[string]any{"updated_at": v.UpdatedAt})
}

// authorizePluginPush accepts the request when fedAuthMiddleware already
// verified a push token for this plugin, or when the Authorization bearer
// token verifies against the plugin's push token (e.g. when no operator
// token is configured, the middleware passes every request through).
// Writes a 401 and returns false otherwise.
func (s *Server) authorizePluginPush(w http.ResponseWriter, r *http.Request, name string) bool {
	if pluginPushFromContext(r.Context()) == name {
		return true
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		jsonError(w, http.StatusUnauthorized, "missing bearer token")
		return false
	}
	if err := s.pluginTokens.Verify(name, token); err != nil {
		jsonError(w, http.StatusUnauthorized, err.Error())
		return false
	}
	return true
}

// pluginWidgetView is one declared widget with its latest stored value
// (nil when none has arrived yet).
type pluginWidgetView struct {
	plugins.Widget
	Value *plugins.WidgetValue `json:"value"`
}

func (s *Server) widgetView(name string, wd plugins.Widget) pluginWidgetView {
	view := pluginWidgetView{Widget: wd}
	if v, ok := s.widgetStore.Get(name, wd.ID); ok {
		view.Value = &v
	}
	return view
}

func (s *Server) handlePluginWidgetList(w http.ResponseWriter, r *http.Request, name string) {
	if !s.fedCap(w, r, federation.CapPluginRead) {
		return
	}
	widgets, ok := s.pluginWidgetSrc.Widgets(name)
	if !ok {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("plugin %q not found", name))
		return
	}
	out := make([]pluginWidgetView, 0, len(widgets))
	for _, wd := range widgets {
		out = append(out, s.widgetView(name, wd))
	}
	writeJSONOK(w, map[string]any{"plugin": name, "widgets": out})
}

func (s *Server) handlePluginWidgetGet(w http.ResponseWriter, r *http.Request, name, widgetID string) {
	if !s.fedCap(w, r, federation.CapPluginRead) {
		return
	}
	widgets, ok := s.pluginWidgetSrc.Widgets(name)
	if !ok {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("plugin %q not found", name))
		return
	}
	wd, ok := plugins.Manifest{Widgets: widgets}.Widget(widgetID)
	if !ok {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("widget %q is not declared by plugin %q", widgetID, name))
		return
	}
	writeJSONOK(w, s.widgetView(name, wd))
}
