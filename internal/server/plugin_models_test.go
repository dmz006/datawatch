package server

// Tests for the per-service model-use report endpoint
// (GET /api/plugins/{name}/models). Per AGENT.md these carry the
// PluginModels test-name prefix.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/plugins"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// fakeModelUseSource returns the given report for the named plugins and
// the given error otherwise.
type fakeModelUseSource struct {
	reports map[string]*plugins.ModelUseReport
	errs    map[string]error
}

func (f fakeModelUseSource) ModelUse(_ context.Context, name string) (*plugins.ModelUseReport, error) {
	if f.errs[name] != nil {
		return nil, f.errs[name]
	}
	if rep, ok := f.reports[name]; ok {
		return rep, nil
	}
	return nil, ErrModelUseUnknownPlugin
}

// swappableModelUseSource serves one report for every plugin and lets a
// test swap it while the background events task is reading it.
type swappableModelUseSource struct {
	mu     sync.Mutex
	report *plugins.ModelUseReport
}

func (f *swappableModelUseSource) set(r *plugins.ModelUseReport) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.report = r
}

func (f *swappableModelUseSource) ModelUse(context.Context, string) (*plugins.ModelUseReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.report, nil
}

// recorderCapture collects alert/audit sink calls.
type recorderCapture struct {
	mu   sync.Mutex
	seen []string
}

func (c *recorderCapture) add(kind, what string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, kind+" "+what)
}

func (c *recorderCapture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen)
}

func (c *recorderCapture) joined() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.seen, "\n")
}

func newModelUseRecorder(t *testing.T) (*plugins.ServiceEventRecorder, *recorderCapture) {
	t.Helper()
	cap := &recorderCapture{}
	rec := plugins.NewServiceEventRecorder(plugins.ServiceEventOptions{
		Alert: func(_ alerts.Level, title, body string) { cap.add("alert", title+"/"+body) },
		Audit: func(action, subject string, _ map[string]any) { cap.add("audit", action+" "+subject) },
	})
	return rec, cap
}

func pluginModelsServer(t *testing.T, src PluginModelsSource) (*Server, http.Handler) {
	t.Helper()
	s := pluginModelsServerBare(t)
	if src != nil {
		s.SetPluginModels(src)
	}
	return s, s.fedAuthMiddleware(http.HandlerFunc(s.handlePlugins))
}

// pluginModelsServerBare returns a *Server ready for model-use endpoint
// tests: operator token, known service plugin.
func pluginModelsServerBare(t *testing.T) *Server {
	t.Helper()
	s := bl90Server(t)
	s.token = pluginTokenAdmin
	s.SetPluginsAPI(&fakePluginsAPI{known: map[string]bool{"imap-mcp": true}})
	return s
}

// waitFor polls ok until true or times out.
func waitFor(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func pluginModelsDo(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+pluginTokenAdmin)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func decodeReport(t *testing.T, rr *httptest.ResponseRecorder) plugins.ModelUseReport {
	t.Helper()
	var rep plugins.ModelUseReport
	if err := json.Unmarshal(rr.Body.Bytes(), &rep); err != nil {
		t.Fatalf("decode report: %v\nbody: %s", err, rr.Body.String())
	}
	return rep
}

// TestPluginModels_Report — the endpoint reports which models the
// service calls, on which Ollama node each runs, the memory each holds,
// the documented calls, and the findings.
func TestPluginModels_Report(t *testing.T) {
	report := plugins.CollectModelUse("imap-mcp", plugins.ModelUseInputs{
		Nodes: []plugins.ModelNode{{
			Name: "johnnyjohnny",
			Loaded: []plugins.LoadedModel{
				{Name: "qwen3:1.7b", RAMBytes: 512 << 20, VRAMBytes: 512 << 20, ContextTokens: 32768},
			},
		}},
		Service: plugins.ServiceUsage{Documented: true, Models: []plugins.ServiceModel{
			{Name: "qwen3:1.7b", Calls: 1234},
		}},
	}, plugins.ModelUseThresholds{})
	s, h := pluginModelsServer(t, fakeModelUseSource{reports: map[string]*plugins.ModelUseReport{"imap-mcp": report}})
	rr := pluginModelsDo(t, h, "/api/plugins/imap-mcp/models")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
	got := decodeReport(t, rr)
	if got.Plugin != "imap-mcp" {
		t.Errorf("plugin = %q", got.Plugin)
	}
	if len(got.Models) != 1 {
		t.Fatalf("models = %+v, want 1 row", got.Models)
	}
	e := got.Models[0]
	if e.Model != "qwen3:1.7b" || e.Node != "johnnyjohnny" || e.HeldBytes != 1024<<20 || e.Calls != 1234 {
		t.Errorf("entry = %+v", e)
	}
	has := map[string]bool{}
	for _, f := range got.Findings {
		has[f.ID] = true
	}
	if !has[plugins.FindingSmallModelLargeContext] {
		t.Errorf("findings lack small_model_large_context: %+v", got.Findings)
	}
	_ = s
}

// TestPluginModels_UndocumentedCounts — a service whose contract does
// not document per-model call counts reports what the nodes report and
// says so.
func TestPluginModels_UndocumentedCounts(t *testing.T) {
	report := plugins.CollectModelUse("imap-mcp", plugins.ModelUseInputs{
		Nodes: []plugins.ModelNode{{
			Name:   "johnnyjohnny",
			Loaded: []plugins.LoadedModel{{Name: "qwen3:1.7b", VRAMBytes: 256 << 20}},
		}},
		Service: plugins.ServiceUsage{Documented: false},
	}, plugins.ModelUseThresholds{})
	_, h := pluginModelsServer(t, fakeModelUseSource{reports: map[string]*plugins.ModelUseReport{"imap-mcp": report}})
	rr := pluginModelsDo(t, h, "/api/plugins/imap-mcp/models")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
	got := decodeReport(t, rr)
	if len(got.Models) != 1 || got.Models[0].Calls != 0 || got.Models[0].CallsDocumented {
		t.Fatalf("entry = %+v, want calls 0 and undocumenting", got.Models)
	}
	joined := strings.Join(got.Notes, " ")
	if !strings.Contains(joined, "does not document per-model call counts") {
		t.Errorf("notes do not say counts are undocumented: %q", joined)
	}
	if len(got.Findings) != 0 {
		t.Errorf("findings must not fire off these inputs: %+v", got.Findings)
	}
}

// TestPluginModels_NodeUnreachable — a node that does not answer is
// marked in the report and noted by the request; the
// model_node_unreachable condition itself is raised (and cleared) by
// the background events task through the recorder, once per transition,
// not by requests to the endpoint.
func TestPluginModels_NodeUnreachable(t *testing.T) {
	bad := plugins.CollectModelUse("imap-mcp", plugins.ModelUseInputs{
		Nodes: []plugins.ModelNode{{Name: "johnnyjohnny", Error: "connection refused"}},
	}, plugins.ModelUseThresholds{})

	good := plugins.CollectModelUse("imap-mcp", plugins.ModelUseInputs{
		Nodes: []plugins.ModelNode{{Name: "johnnyjohnny", Loaded: []plugins.LoadedModel{{Name: "qwen3:1.7b", VRAMBytes: 1 << 20}}}},
	}, plugins.ModelUseThresholds{})

	rec, cap := newModelUseRecorder(t)
	src := &swappableModelUseSource{report: bad}
	s := pluginModelsServerBare(t)
	s.SetModelNodeRecorder(rec)
	s.SetPluginModels(src)
	// The events task raises from the background at a short interval.
	s.SetModelNodeEventInterval(50 * time.Millisecond)
	s.StartModelNodeEvents(context.Background(), func() []string { return []string{"imap-mcp"} })

	h := s.fedAuthMiddleware(http.HandlerFunc(s.handlePlugins))

	// A request to the endpoint names + notes the down node and raises
	// nothing itself.
	rr := pluginModelsDo(t, h, "/api/plugins/imap-mcp/models")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
	got := decodeReport(t, rr)
	joined := strings.Join(got.Notes, " ")
	if !strings.Contains(joined, "johnnyjohnny") || !strings.Contains(joined, "did not answer") {
		t.Errorf("notes do not name the node that did not answer: %q", joined)
	}

	// The background task raises once (alert + audit), and a second
	// failing pass must not re-raise.
	waitFor(t, 5*time.Second, "task to raise", func() bool { return cap.count() >= 2 })
	waitFor(t, 5*time.Second, "second failing pass to settle", func() bool { return cap.count() == 2 })

	// The node answers again → the task clears the condition (1 clear)
	// without re-raising.
	src.set(good)
	waitFor(t, 5*time.Second, "task to clear after recovery", func() bool { return cap.count() >= 4 })
	if !strings.Contains(cap.joined(), "reachable again") {
		t.Errorf("no clear recorded: %s", cap.joined())
	}
}

// TestPluginModels_UnknownPlugin — a plugin the registry does not know
// is a 404 naming the plugin.
func TestPluginModels_UnknownPlugin(t *testing.T) {
	_, h := pluginModelsServer(t, fakeModelUseSource{})
	rr := pluginModelsDo(t, h, "/api/plugins/ghost/models")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), `plugin "ghost"`) {
		t.Errorf("body does not name the plugin: %q", rr.Body.String())
	}
}

// TestPluginModels_NotService — a registered plugin that is not a
// service-kind plugin gets a 422 naming the plugin.
func TestPluginModels_NotService(t *testing.T) {
	_, h := pluginModelsServer(t, fakeModelUseSource{
		errs: map[string]error{"lib": ErrModelUseNotService},
	})
	// The fake returns the not-service error for the named plugin.
	rr := pluginModelsDo(t, h, "/api/plugins/lib/models")
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
}

// TestPluginModels_MethodNotAllowed — only GET is allowed.
func TestPluginModels_MethodNotAllowed(t *testing.T) {
	_, h := pluginModelsServer(t, fakeModelUseSource{})
	req := httptest.NewRequest(http.MethodPost, "/api/plugins/imap-mcp/models", nil)
	req.Header.Set("Authorization", "Bearer "+pluginTokenAdmin)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
}

// TestPluginModels_OperatorRequired and _FederationPeerRefused — the
// endpoint is operator-token only; peers and session-scoped callers are
// refused.
func TestPluginModels_OperatorRequired(t *testing.T) {
	_, h := pluginModelsServer(t, fakeModelUseSource{})
	req := httptest.NewRequest(http.MethodGet, "/api/plugins/imap-mcp/models", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
}

func TestPluginModels_FederationPeerRefused(t *testing.T) {
	s, store, _ := newFedTestServer(t)
	if err := store.Add(&multiserver.Entry{
		Name: "peer-pm", Token: "pm-full", URL: "http://peer:8080",
		Enabled: true, Federated: true, Capabilities: []string{"full-control"},
	}); err != nil {
		t.Fatal(err)
	}
	s.SetPluginsAPI(&fakePluginsAPI{known: map[string]bool{"imap-mcp": true}})
	s.SetPluginModels(fakeModelUseSource{reports: map[string]*plugins.ModelUseReport{}})
	h := s.fedAuthMiddleware(http.HandlerFunc(s.handlePlugins))
	req := httptest.NewRequest(http.MethodGet, "/api/plugins/imap-mcp/models", nil)
	req.Header.Set("Authorization", "Bearer pm-full")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body %q", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), "operator token required") {
		t.Errorf("body does not name the refusal: %q", rr.Body.String())
	}
}

// TestPluginModels_DefaultSource — with no fake source the endpoint
// resolves the report through the real readers: served models +
// resident memory from one httptest "Ollama node", the service's models
// + documented call counts from one httptest "service", bearer token
// resolved from the manifest's ${secret:name} reference.
func TestPluginModels_DefaultSource(t *testing.T) {
	// Fake Ollama node: serves qwen3:1.7b, resident with 512 MiB VRAM.
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen3:1.7b"}]}`))
		case "/api/ps":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen3:1.7b","size":0,"size_vram":536870912,"details":{"num_ctx":131072}}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer node.Close()

	// Fake imap-mcp service: documents the models it calls and the call
	// counts; both require the bearer token (the fake secret store
	// hands out its sentinel value).
	sentinel := "SENTINEL-SECRET-VALUE-imapmcp_token"
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+sentinel {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/models":
			_, _ = w.Write([]byte(`{"models":["qwen3:1.7b"]}`))
		case "/api/stats":
			_, _ = w.Write([]byte(`{"models":{"qwen3:1.7b":7}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svc.Close()

	s := bl90Server(t)
	s.token = pluginTokenAdmin
	s.SetPluginsAPI(&fakePluginsAPI{known: map[string]bool{"imap-mcp": true}})
	s.SetPluginTokens(fakeManifestSource{"imap-mcp": {
		Name: "imap-mcp",
		Kind: plugins.KindService,
		Connection: &plugins.PluginConnection{
			BaseURL: svc.URL,
			Auth:    &plugins.PluginAuth{Type: "bearer", Token: "${secret:imapmcp_token}"},
		},
	}})
	s.secretsStore = fakeSecretStore{names: map[string]bool{"imapmcp_token": true}}

	reg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(&compute.Node{
		Name:    "fake-node",
		Kind:    compute.KindOllama,
		Address: node.URL,
	}); err != nil {
		t.Fatal(err)
	}
	s.computeReg = reg

	h := s.fedAuthMiddleware(http.HandlerFunc(s.handlePlugins))
	rr := pluginModelsDo(t, h, "/api/plugins/imap-mcp/models")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
	got := decodeReport(t, rr)
	if len(got.Models) != 1 {
		t.Fatalf("models = %+v, want 1 row", got.Models)
	}
	e := got.Models[0]
	if e.Model != "qwen3:1.7b" || e.Node != "fake-node" || e.HeldBytes != 512<<20 {
		t.Errorf("entry = %+v", e)
	}
	if e.ContextTokens != 131072 {
		t.Errorf("entry.ContextTokens = %d, want 131072 from the Ollama reader: %+v", e.ContextTokens, e)
	}
	if !e.UsedByService || !e.CallsDocumented || e.Calls != 7 {
		t.Errorf("entry = %+v, want service-documented 7 calls", e)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].Error != "" {
		t.Errorf("nodes = %+v, want one healthy node", got.Nodes)
	}
}

// TestPluginModels_DefaultSourceNodeDown — the default source marks a
// node that does not answer and notes it; the model_node_unreachable
// raise is owned by the events task, not by this request.
func TestPluginModels_DefaultSourceNodeDown(t *testing.T) {
	s := bl90Server(t)
	s.token = pluginTokenAdmin
	s.SetPluginsAPI(&fakePluginsAPI{known: map[string]bool{"imap-mcp": true}})
	s.SetPluginTokens(fakeManifestSource{"imap-mcp": {
		Name: "imap-mcp",
		Kind: plugins.KindService,
		Connection: &plugins.PluginConnection{
			// Point at a closed port: the node does not answer.
			BaseURL: "http://127.0.0.1:1",
		},
	}})
	reg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(&compute.Node{
		Name:    "down",
		Kind:    compute.KindOllama,
		Address: "http://127.0.0.1:1",
	}); err != nil {
		t.Fatal(err)
	}
	s.computeReg = reg
	rec, cap := newModelUseRecorder(t)
	s.SetModelNodeRecorder(rec)

	h := s.fedAuthMiddleware(http.HandlerFunc(s.handlePlugins))
	rr := pluginModelsDo(t, h, "/api/plugins/imap-mcp/models")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
	got := decodeReport(t, rr)
	if len(got.Nodes) != 1 || got.Nodes[0].Name != "down" || got.Nodes[0].Error == "" {
		t.Fatalf("nodes = %+v, want the down node marked with an error", got.Nodes)
	}
	joined := strings.Join(got.Notes, " ")
	if !strings.Contains(joined, "down") || !strings.Contains(joined, "did not answer") {
		t.Errorf("notes do not name the node that did not answer: %q", joined)
	}
	if len(got.Findings) != 0 {
		t.Errorf("no finding may fire from a node that did not answer: %+v", got.Findings)
	}
	if n := cap.count(); n != 0 {
		t.Fatalf("saw %d sink calls for a mere request (the model_node_unreachable raise is owned by the events task): %s", n, cap.joined())
	}
}

// A nil default source must never become a typed nil inside the
// PluginModelsSource interface: ModelUse is safe on a nil receiver and
// nil manifest func, and the handler answers 503 when no source exists.
func TestDefaultModelUseSource_NilSafe(t *testing.T) {
	var d *defaultModelUseSource
	if _, err := d.ModelUse(context.Background(), "imap-mcp"); !errors.Is(err, ErrModelUseUnknownPlugin) {
		t.Errorf("nil receiver: err = %v, want ErrModelUseUnknownPlugin", err)
	}
	d = &defaultModelUseSource{}
	if _, err := d.ModelUse(context.Background(), "imap-mcp"); !errors.Is(err, ErrModelUseUnknownPlugin) {
		t.Errorf("nil manifest: err = %v, want ErrModelUseUnknownPlugin", err)
	}

	s := &Server{}
	if got := s.defaultModelUseSource(); got != nil {
		t.Fatalf("defaultModelUseSource without tokens source = %v, want nil", got)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/plugins/imap-mcp/models", nil)
	rr := httptest.NewRecorder()
	s.handlePluginModels(rr, req, "imap-mcp")
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("handler status = %d, want 503", rr.Code)
	}
}
