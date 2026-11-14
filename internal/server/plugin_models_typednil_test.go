package server

// Regression tests for the typed-nil model-use source crash: a nil
// *defaultModelUseSource stored in a PluginModelsSource / ModelUseSource
// interface is non-nil to the interface, so the nil checks never fired
// and the first ModelUse call dereferenced it. Per AGENT.md these carry
// the PluginModels test-name prefix.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/plugins"
)

// resetModelNodeEventsOnce re-arms the package-level
// modelNodeEventsOnce guard so a test can exercise StartModelNodeEvents
// even when an earlier test in the binary already consumed it. It
// re-arms again on cleanup so later tests are unaffected by this one
// having consumed it. Test-only: production code never resets the guard.
func resetModelNodeEventsOnce(t *testing.T) {
	t.Helper()
	modelNodeEventsOnce = sync.Once{}
	t.Cleanup(func() { modelNodeEventsOnce = sync.Once{} })
}

// TestPluginModels_TypedNil_DefaultSourceNilReceiver — ModelUse on a nil
// *defaultModelUseSource and on one with a nil manifest func returns
// ErrModelUseUnknownPlugin instead of dereferencing.
func TestPluginModels_TypedNil_DefaultSourceNilReceiver(t *testing.T) {
	var nilSrc *defaultModelUseSource
	cases := map[string]*defaultModelUseSource{
		"nil receiver":      nilSrc,
		"nil manifest func": {srv: pluginModelsServerBare(t), manifest: nil},
	}
	for name, d := range cases {
		t.Run(name, func(t *testing.T) {
			var rep *plugins.ModelUseReport
			var err error
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Fatalf("ModelUse panicked: %v", p)
					}
				}()
				rep, err = d.ModelUse(context.Background(), "imap-mcp")
			}()
			if !errors.Is(err, ErrModelUseUnknownPlugin) {
				t.Fatalf("err = %v, want ErrModelUseUnknownPlugin", err)
			}
			if rep != nil {
				t.Fatalf("report returned alongside the error: %v", rep)
			}
		})
	}
}

// TestPluginModels_TypedNil_DefaultSourceUnwired — with no plugin token
// source wired the server's default source is a true nil, so it is never
// stored as a typed nil in an interface.
func TestPluginModels_TypedNil_DefaultSourceUnwired(t *testing.T) {
	s := pluginModelsServerBare(t)
	if s.pluginTokensSrc != nil {
		t.Fatal("test precondition: pluginTokensSrc must be nil")
	}
	if d := s.defaultModelUseSource(); d != nil {
		t.Fatalf("defaultModelUseSource() = %v, want nil with no token source", d)
	}
}

// TestPluginModels_TypedNil_StartModelNodeEventsNoSource — a bare server
// with no plugin token source and no explicit SetPluginModels source
// starts the model-node events task without a panic and without
// recording any event: the task is handed a true nil source (it returns
// at once), never a typed nil that it would call into.
func TestPluginModels_TypedNil_StartModelNodeEventsNoSource(t *testing.T) {
	resetModelNodeEventsOnce(t)

	s := pluginModelsServerBare(t)
	if s.pluginTokensSrc != nil || s.pluginModelsSrc != nil {
		t.Fatal("test precondition: no token source and no explicit model-use source")
	}
	rec, cap := newModelUseRecorder(t)
	s.SetModelNodeRecorder(rec)
	const interval = 20 * time.Millisecond
	s.SetModelNodeEventInterval(interval)

	var rosterCalls atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	// A panic in the task goroutine would abort the test binary, so
	// reaching the assertions below is itself the no-panic proof.
	s.StartModelNodeEvents(ctx, func() []string {
		rosterCalls.Add(1)
		return []string{"imap-mcp"}
	})

	if s.modelNodeEvents == nil {
		t.Fatal("StartModelNodeEvents did not build the events task")
	}
	// The regression: the source must be a true nil interface, not a
	// non-nil interface holding a nil *defaultModelUseSource.
	if s.modelNodeEvents.Source != nil {
		t.Fatalf("events Source = %#v, want a true nil interface", s.modelNodeEvents.Source)
	}

	// Wait several intervals.
	time.Sleep(6 * interval)
	if n := cap.count(); n != 0 {
		t.Errorf("recorded %d events with no source: %s", n, cap.joined())
	}
	if n := rosterCalls.Load(); n != 0 {
		t.Errorf("roster resolved %d times; the task must not pass with no source", n)
	}
}

// TestPluginModels_TypedNil_EndpointNoSource503 — the model-use endpoint
// with no plugin token source and no explicit source answers 503
// "plugins not available" rather than panicking on a typed-nil source.
func TestPluginModels_TypedNil_EndpointNoSource503(t *testing.T) {
	s := pluginModelsServerBare(t)
	if s.pluginTokensSrc != nil || s.pluginModelsSrc != nil {
		t.Fatal("test precondition: no token source and no explicit model-use source")
	}

	// Directly through the handler.
	req := httptest.NewRequest(http.MethodGet, "/api/plugins/imap-mcp/models", nil)
	rr := httptest.NewRecorder()
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("handlePluginModels panicked: %v", p)
			}
		}()
		s.handlePluginModels(rr, req, "imap-mcp")
	}()
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body %q, want 503", rr.Code, rr.Body)
	}

	// And through the authenticated /api/plugins router.
	h := s.fedAuthMiddleware(http.HandlerFunc(s.handlePlugins))
	rr = pluginModelsDo(t, h, "/api/plugins/imap-mcp/models")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("routed status = %d, body %q, want 503", rr.Code, rr.Body)
	}
}
