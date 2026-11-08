package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

// pullFakeOllama serves the node endpoints a pull-then-load Ensure walks:
// /api/ps (resident), /api/tags (downloaded), /api/pull and /api/generate.
// pullStatus / pullBody control what /api/pull says.
type pullFakeOllama struct {
	mu         sync.Mutex
	tags       []string // models the node has downloaded
	resident   []string // models currently in memory
	pullCalls  []string
	loaded     []string
	pullStatus int
	pullBody   string
}

func (f *pullFakeOllama) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/api/ps":
		_ = json.NewEncoder(w).Encode(map[string]any{"models": modelList(f.resident)})
	case "/api/tags":
		_ = json.NewEncoder(w).Encode(map[string]any{"models": modelList(f.tags)})
	case "/api/pull":
		var body struct {
			Name   string `json:"name"`
			Stream bool   `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.pullCalls = append(f.pullCalls, body.Name)
		status := f.pullStatus
		if status == 0 {
			status = http.StatusOK
		}
		bodyOut := f.pullBody
		if bodyOut == "" {
			bodyOut = `{"status":"success"}` + "\n"
		}
		if status >= 400 {
			w.WriteHeader(status)
		}
		_, _ = fmt.Fprint(w, bodyOut)
	case "/api/generate":
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.loaded = append(f.loaded, body.Model)
		f.resident = append(f.resident, body.Model)
		_, _ = fmt.Fprint(w, `{"done":true}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func modelList(names []string) []map[string]string {
	out := make([]map[string]string, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]string{"name": n, "model": n})
	}
	return out
}

func TestPullModelStreamsProgressToCompletion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pull" || r.Method != http.MethodPost {
			t.Errorf("want POST /api/pull, got %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Name   string `json:"name"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode pull body: %v", err)
		}
		if body.Name != "qwen3:8b" || !body.Stream {
			t.Errorf("pull body must name the model and ask for a stream; got %+v", body)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprint(w, `{"status":"pulling manifest"}`+"\n") //nolint:errcheck
		fmt.Fprint(w, `{"status":"downloading manifest","total":1000,"completed":400}`+"\n") //nolint:errcheck
		fmt.Fprint(w, `{"status":"success"}`+"\n")                                              //nolint:errcheck
	}))
	defer srv.Close()

	var reports []PullProgress
	err := PullModel(context.Background(), nil, srv.URL, "qwen3:8b", func(p PullProgress) {
		reports = append(reports, p)
	})
	if err != nil {
		t.Fatalf("PullModel: %v", err)
	}
	if len(reports) != 3 {
		t.Fatalf("want one report per progress line, got %d: %+v", len(reports), reports)
	}
	if reports[1].Status != "downloading manifest" || reports[1].BytesTotal != 1000 || reports[1].BytesDone != 400 {
		t.Errorf("second report should carry the byte counters; got %+v", reports[1])
	}
	if reports[1].Percent != 0.4 {
		t.Errorf("second report percent = %v, want 0.4", reports[1].Percent)
	}
	if !reports[2].Success || reports[2].Status != "success" {
		t.Errorf("final report should flag success; got %+v", reports[2])
	}
}

// A pull may end without an explicit success line (feed just closes). That is
// still a completion, not a decode failure.
func TestPullModelFeedEndingWithoutSuccessLineCompletes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"pulling manifest"}`+"\n") //nolint:errcheck
		fmt.Fprint(w, `{"status":"writing manifest","total":100,"completed":100}`+"\n") //nolint:errcheck
	}))
	defer srv.Close()
	if err := PullModel(context.Background(), nil, srv.URL, "qwen3:8b", nil); err != nil {
		t.Fatalf("a cleanly closed feed is a completion; got %v", err)
	}
}

func TestPullModelTransientHTTP500IsAFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error":"transient registry failure"}`)
	}))
	defer srv.Close()

	var reports int
	err := PullModel(context.Background(), nil, srv.URL, "qwen3:8b", func(PullProgress) {
		reports++
	})
	if err == nil {
		t.Fatal("want an error for an HTTP 500 pull response, got nil")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "transient registry failure") {
		t.Errorf("error should name the status and the body; got %v", err)
	}
	if reports != 0 {
		t.Errorf("a failed response must not report progress; got %d reports", reports)
	}
}

func TestPullModelWithoutAnAddressFails(t *testing.T) {
	err := PullModel(context.Background(), nil, "  ", "qwen3:8b", nil)
	if err == nil {
		t.Fatal("want an error when the node has no address, got nil")
	}
	if !strings.Contains(err.Error(), "no node address") {
		t.Errorf("error should name the missing address; got %v", err)
	}
}

func TestPullModelDecodingGarbageInStreamFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"pulling manifest"}`+"\n") //nolint:errcheck
		_, _ = fmt.Fprint(w, "this is not json at all")
	}))
	defer srv.Close()

	reports := 0
	err := PullModel(context.Background(), nil, srv.URL, "qwen3:8b", func(PullProgress) {
		reports++
	})
	if err == nil {
		t.Fatal("want an error when the stream holds a non-JSON line, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error should say the failure was a decode; got %v", err)
	}
	if reports != 1 {
		t.Errorf("the one good line should still have been reported; got %d", reports)
	}
}

func TestPullModelErrorLineInStreamFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"pulling manifest"}`+"\n") //nolint:errcheck
		_, _ = fmt.Fprint(w, `{"error":"manifest for model qwen3:8b not found"}`)
	}))
	defer srv.Close()

	reports := 0
	err := PullModel(context.Background(), nil, srv.URL, "qwen3:8b", func(PullProgress) {
		reports++
	})
	if err == nil {
		t.Fatal("want an error for an error line in the stream, got nil")
	}
	if !strings.Contains(err.Error(), "manifest for model qwen3:8b not found") {
		t.Errorf("error should carry the line's message; got %v", err)
	}
	if reports != 1 {
		t.Errorf("the line before the error should have been reported; got %d", reports)
	}
}

// marketplacePullState mirrors the PullTask updates the marketplace worker
// (runOllamaPull) makes around a PullModel call, so the marketplace
// consumption contract — the same status values, and the terminal progress —
// is exercised here without importing the server package.
type marketplacePullState struct {
	status     string
	progress   float64
	bytesTotal int64
	bytesDone  int64
	err        string
}

func runMarketplacePullPath(t *testing.T, handler http.HandlerFunc) *marketplacePullState {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	m := &marketplacePullState{status: "pending"} // as written by the registry before the worker starts
	m.status = "in_progress"                       // as runOllamaPull sets it before calling PullModel
	err := PullModel(context.Background(), nil, srv.URL, "qwen3:8b", func(p PullProgress) {
		if p.BytesTotal > 0 {
			m.bytesTotal = p.BytesTotal
			m.bytesDone = p.BytesDone
			m.progress = p.Percent
		}
		if p.Success {
			m.status = "done"
			m.progress = 1.0
		}
	})
	if err != nil {
		m.status = "failed"
		m.err = err.Error()
		return m
	}
	if m.status == "in_progress" && m.err == "" {
		m.status = "done"
		m.progress = 1.0
	}
	return m
}

func TestMarketplacePathReportsCompletionOnPullSuccess(t *testing.T) {
	m := runMarketplacePullPath(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"pulling manifest"}`+"\n") //nolint:errcheck
		fmt.Fprint(w, `{"status":"downloading","total":2000,"completed":600}`+"\n") //nolint:errcheck
		fmt.Fprint(w, `{"status":"success"}`+"\n")                                    //nolint:errcheck
	})
	if m.status != "done" {
		t.Fatalf("marketplace path should end done after a successful pull; got %q (err=%q)", m.status, m.err)
	}
	if m.bytesTotal != 2000 || m.bytesDone != 600 || m.progress != 1.0 {
		t.Errorf("terminal progress should carry the last byte counters and 1.0 progress; got total=%d done=%d progress=%v", m.bytesTotal, m.bytesDone, m.progress)
	}
}

func TestMarketplacePathReportsFailureOnTransient500(t *testing.T) {
	m := runMarketplacePullPath(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error":"transient registry failure"}`)
	})
	if m.status != "failed" {
		t.Fatalf("marketplace path should end failed on a 500; got %q (err=%q)", m.status, m.err)
	}
	if m.err == "" {
		t.Error("the failed pull must record the error text")
	}
}

func TestEnsurePullsAMissingModelThenLoadsIt(t *testing.T) {
	f := &pullFakeOllama{} // node has nothing downloaded and nothing resident
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	defer srv.Close()
	node := &compute.Node{Name: "alpha", Address: srv.URL}
	loader := &ModelLoader{Wait: time.Second, Poll: time.Millisecond}

	var reasons []string
	err := loader.Ensure(context.Background(), node, "qwen3:8b", func(r string) { reasons = append(reasons, r) })
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(f.pullCalls) != 1 || f.pullCalls[0] != "qwen3:8b" {
		t.Fatalf("Ensure must pull a model the node lacks; pullCalls=%v", f.pullCalls)
	}
	if len(f.loaded) != 1 || f.loaded[0] != "qwen3:8b" {
		t.Fatalf("Ensure must load the model after pulling; loaded=%v", f.loaded)
	}
	pullSeen := false
	for _, r := range reasons {
		if strings.Contains(r, "pulling qwen3:8b onto node alpha") {
			pullSeen = true
		}
	}
	if !pullSeen {
		t.Errorf("the wait callback must report the pull; reasons=%v", reasons)
	}
}

func TestEnsureSkipsPullWhenModelIsOnTheNode(t *testing.T) {
	f := &pullFakeOllama{tags: []string{"qwen3:8b"}} // downloaded, not resident
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	defer srv.Close()
	node := &compute.Node{Name: "alpha", Address: srv.URL}
	loader := &ModelLoader{Wait: time.Second, Poll: time.Millisecond}

	err := loader.Ensure(context.Background(), node, "qwen3:8b", nil)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(f.pullCalls) != 0 {
		t.Fatalf("a model the node already has must not be pulled; pullCalls=%v", f.pullCalls)
	}
	if len(f.loaded) != 1 || f.loaded[0] != "qwen3:8b" {
		t.Fatalf("Ensure must still load the model; loaded=%v", f.loaded)
	}
}

func TestEnsurePullFailureIsNodeLoadError(t *testing.T) {
	f := &pullFakeOllama{pullStatus: 200, pullBody: `{"error":"manifest for model qwen3:8b not found"}`}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	defer srv.Close()
	node := &compute.Node{Name: "alpha", Address: srv.URL}
	loader := &ModelLoader{Wait: time.Second, Poll: time.Millisecond}

	err := loader.Ensure(context.Background(), node, "qwen3:8b", nil)
	var nodeErr *NodeLoadError
	if !errors.As(err, &nodeErr) {
		t.Fatalf("want *NodeLoadError, got %v", err)
	}
	if nodeErr.Node != "alpha" || nodeErr.Model != "qwen3:8b" {
		t.Errorf("error should name the node and model, got %+v", nodeErr)
	}
	if !strings.Contains(err.Error(), "manifest for model qwen3:8b not found") {
		t.Errorf("error should carry the pull's message, got %v", err)
	}
	if len(f.loaded) != 0 {
		t.Fatalf("no load may follow a failed pull; loaded=%v", f.loaded)
	}
}

func TestEnsurePullsAPinnedModelWhenMissing(t *testing.T) {
	// Pins come first on the pull path too: a pinned node lacks the pin, so
	// Ensure pulls the pin for an implicit request and never the other model.
	f := &pullFakeOllama{tags: []string{"qwen3:27b"}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	defer srv.Close()
	node := &compute.Node{Name: "alpha", Address: srv.URL, NodePinnedModel: "qwen3:8b"}
	loader := &ModelLoader{Wait: time.Second, Poll: time.Millisecond}

	err := loader.Ensure(context.Background(), node, "qwen3:27b", nil)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(f.pullCalls) != 1 || f.pullCalls[0] != "qwen3:8b" {
		t.Fatalf("a pinned node must pull its pin for an implicit request; pullCalls=%v", f.pullCalls)
	}
	if len(f.loaded) != 1 || f.loaded[0] != "qwen3:8b" {
		t.Fatalf("a load never swaps a pinned model out; loaded=%v", f.loaded)
	}
}
