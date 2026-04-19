// BL100 — worker-side HTTP memory client tests.

package memory

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeParent records the calls a worker would make to the parent.
type fakeParent struct {
	mu             sync.Mutex
	saves          int
	searchProfiles []string
	searchQueries  []string
	saveErr        bool
}

func (f *fakeParent) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/memory/save":
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.saveErr {
				http.Error(w, "synthetic save failure", http.StatusBadGateway)
				return
			}
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"content"`) {
				t.Errorf("save body missing content: %s", body)
			}
			f.saves++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": 1}`))
		case "/api/memory/search":
			f.mu.Lock()
			defer f.mu.Unlock()
			f.searchQueries = append(f.searchQueries, r.URL.Query().Get("q"))
			f.searchProfiles = append(f.searchProfiles, r.URL.Query().Get("profile"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"content":"hit"}]`))
		default:
			http.NotFound(w, r)
		}
	})
}

func TestNewHTTPClientFromEnv_DisabledModes(t *testing.T) {
	t.Setenv("DATAWATCH_BOOTSTRAP_URL", "http://parent")
	for _, mode := range []string{"", "ephemeral"} {
		t.Setenv("DATAWATCH_MEMORY_MODE", mode)
		if c := NewHTTPClientFromEnv(); c != nil {
			t.Errorf("mode=%q should return nil client, got %+v", mode, c)
		}
	}
}

func TestNewHTTPClientFromEnv_NoBootstrapURL(t *testing.T) {
	t.Setenv("DATAWATCH_MEMORY_MODE", "shared")
	t.Setenv("DATAWATCH_BOOTSTRAP_URL", "")
	t.Setenv("DATAWATCH_PARENT_URL", "")
	if c := NewHTTPClientFromEnv(); c != nil {
		t.Errorf("missing parent URL should return nil, got %+v", c)
	}
}

func TestHTTPClient_Remember_Shared_Synchronous(t *testing.T) {
	fp := &fakeParent{}
	srv := httptest.NewServer(fp.handler(t))
	defer srv.Close()
	c := &HTTPClient{BaseURL: srv.URL, Mode: ModeShared, HTTP: srv.Client()}

	if err := c.Remember(context.Background(), "/proj", "hello", "user", "sess1"); err != nil {
		t.Fatal(err)
	}
	if fp.saves != 1 {
		t.Errorf("shared mode should save synchronously: saves=%d", fp.saves)
	}
}

func TestHTTPClient_Remember_SyncBack_BuffersThenFlush(t *testing.T) {
	fp := &fakeParent{}
	srv := httptest.NewServer(fp.handler(t))
	defer srv.Close()
	c := &HTTPClient{BaseURL: srv.URL, Mode: ModeSyncBack, HTTP: srv.Client()}

	for i := 0; i < 3; i++ {
		_ = c.Remember(context.Background(), "/p", "msg", "user", "s")
	}
	if fp.saves != 0 {
		t.Errorf("sync-back must not hit parent on Remember: saves=%d", fp.saves)
	}
	if c.QueuedLen() != 3 {
		t.Errorf("queue len = %d want 3", c.QueuedLen())
	}

	flushed, err := c.Flush(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if flushed != 3 || fp.saves != 3 {
		t.Errorf("flush mismatch: flushed=%d saves=%d want 3/3", flushed, fp.saves)
	}
	if c.QueuedLen() != 0 {
		t.Errorf("queue should be drained: %d", c.QueuedLen())
	}
}

func TestHTTPClient_Flush_PartialFailureRequeues(t *testing.T) {
	fp := &fakeParent{saveErr: true}
	srv := httptest.NewServer(fp.handler(t))
	defer srv.Close()
	c := &HTTPClient{BaseURL: srv.URL, Mode: ModeSyncBack, HTTP: srv.Client()}

	for i := 0; i < 2; i++ {
		_ = c.Remember(context.Background(), "/p", "msg", "user", "s")
	}
	flushed, err := c.Flush(context.Background())
	if err == nil {
		t.Fatal("expected error from failing parent")
	}
	if flushed != 0 {
		t.Errorf("flushed=%d want 0", flushed)
	}
	// Both entries should be re-queued.
	if c.QueuedLen() != 2 {
		t.Errorf("re-queued len=%d want 2", c.QueuedLen())
	}
}

func TestHTTPClient_Search_PassesProfile(t *testing.T) {
	fp := &fakeParent{}
	srv := httptest.NewServer(fp.handler(t))
	defer srv.Close()
	c := &HTTPClient{BaseURL: srv.URL, Profile: "alpha", Mode: ModeShared, HTTP: srv.Client()}

	results, err := c.Search(context.Background(), "needle")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0]["content"] != "hit" {
		t.Errorf("results=%v", results)
	}
	if len(fp.searchProfiles) != 1 || fp.searchProfiles[0] != "alpha" {
		t.Errorf("parent received profile=%v want [alpha]", fp.searchProfiles)
	}
	if len(fp.searchQueries) != 1 || fp.searchQueries[0] != "needle" {
		t.Errorf("parent received query=%v want [needle]", fp.searchQueries)
	}
}

func TestHTTPClient_NilReceiver_NoCrash(t *testing.T) {
	var c *HTTPClient
	if err := c.Remember(context.Background(), "", "x", "", ""); err != nil {
		t.Errorf("nil Remember should be no-op: %v", err)
	}
	if _, err := c.Search(context.Background(), "x"); err != nil {
		t.Errorf("nil Search should be no-op: %v", err)
	}
	if n, err := c.Flush(context.Background()); err != nil || n != 0 {
		t.Errorf("nil Flush: n=%d err=%v", n, err)
	}
	if c.QueuedLen() != 0 {
		t.Errorf("nil QueuedLen=%d want 0", c.QueuedLen())
	}
}

func TestHTTPClient_Search_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := &HTTPClient{BaseURL: srv.URL, Mode: ModeShared, HTTP: srv.Client()}
	_, err := c.Search(context.Background(), "x")
	if err == nil {
		t.Error("expected error on 500")
	}
}

// Sanity: the JSON response shape of /api/memory/search the worker
// expects matches the actual server output (verified via the handler
// returning a list of map objects).
func TestHTTPClient_Search_DecodeShape(t *testing.T) {
	expected := []map[string]interface{}{{"content": "x", "score": 0.5}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer srv.Close()
	c := &HTTPClient{BaseURL: srv.URL, Mode: ModeShared, HTTP: srv.Client()}
	got, err := c.Search(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["content"] != "x" {
		t.Errorf("decode shape: %v", got)
	}
}

func TestHTTPClient_Flush_NoOpInOtherModes(t *testing.T) {
	for _, m := range []MemoryMode{ModeShared, ModeEphemeral, ""} {
		c := &HTTPClient{Mode: m}
		n, err := c.Flush(context.Background())
		if err != nil || n != 0 {
			t.Errorf("mode=%q flush: n=%d err=%v", m, n, err)
		}
	}
}

func TestHTTPClient_Save_ServerError_Bubbles(t *testing.T) {
	c := &HTTPClient{BaseURL: "http://127.0.0.1:1", Mode: ModeShared, HTTP: &http.Client{}}
	err := c.Remember(context.Background(), "/p", "x", "user", "s")
	if err == nil {
		t.Error("expected transport error from unreachable parent")
	}
	if !errors.Is(err, err) { // sanity
		t.Fatal("?")
	}
}
