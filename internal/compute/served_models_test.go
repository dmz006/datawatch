package compute

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type fakeLister struct {
	mu     sync.Mutex
	calls  int
	models []string
	err    error
}

func (f *fakeLister) List(_ context.Context, _ *Node, _ time.Duration) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.models, f.err
}
func (f *fakeLister) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newTestCache(f *fakeLister, c *fakeClock, age time.Duration) *ServedModelsCache {
	return NewServedModelsCache(f.List,
		func() time.Duration { return age },
		func() time.Duration { return 8 * time.Second },
		c.Now)
}

func testNode(name string) *Node {
	return &Node{Name: name, Kind: KindOllama, Address: "http://127.0.0.1:1"}
}

func TestServedModels_CachesWithinAge(t *testing.T) {
	f := &fakeLister{models: []string{"a:1", "b:2"}}
	clk := newFakeClock()
	c := newTestCache(f, clk, 300*time.Second)
	n := testNode("gpu-a")

	first := c.Get(context.Background(), n, false)
	if first.Cached || !first.Known || !reflect.DeepEqual(first.Models, []string{"a:1", "b:2"}) {
		t.Fatalf("first read wrong: %+v", first)
	}
	clk.Advance(100 * time.Second)
	second := c.Get(context.Background(), n, false)
	if !second.Cached || second.AgeSeconds != 100 {
		t.Fatalf("second read should be cached at 100s: %+v", second)
	}
	if f.Calls() != 1 {
		t.Fatalf("lister called %d times, want 1", f.Calls())
	}
}

func TestServedModels_RefetchesAfterAge(t *testing.T) {
	f := &fakeLister{models: []string{"a:1"}}
	clk := newFakeClock()
	c := newTestCache(f, clk, 60*time.Second)
	n := testNode("gpu-a")

	c.Get(context.Background(), n, false)
	clk.Advance(61 * time.Second)
	f.mu.Lock()
	f.models = []string{"a:1", "c:3"}
	f.mu.Unlock()
	got := c.Get(context.Background(), n, false)
	if got.Cached || len(got.Models) != 2 {
		t.Fatalf("expected a fresh read after the age: %+v", got)
	}
	if f.Calls() != 2 {
		t.Fatalf("lister called %d times, want 2", f.Calls())
	}
}

func TestServedModels_RefreshBypassesCache(t *testing.T) {
	f := &fakeLister{models: []string{"a:1"}}
	clk := newFakeClock()
	c := newTestCache(f, clk, 300*time.Second)
	n := testNode("gpu-a")

	c.Get(context.Background(), n, false)
	f.mu.Lock()
	f.models = []string{"z:9"}
	f.mu.Unlock()
	got := c.Get(context.Background(), n, true)
	if got.Cached || !reflect.DeepEqual(got.Models, []string{"z:9"}) {
		t.Fatalf("refresh should read the node: %+v", got)
	}
	again := c.Get(context.Background(), n, false)
	if !again.Cached || !reflect.DeepEqual(again.Models, []string{"z:9"}) {
		t.Fatalf("refresh should replace the cache entry: %+v", again)
	}
	if f.Calls() != 2 {
		t.Fatalf("lister called %d times, want 2", f.Calls())
	}
}

func TestServedModels_ZeroAgeNeverCaches(t *testing.T) {
	f := &fakeLister{models: []string{"a:1"}}
	clk := newFakeClock()
	c := newTestCache(f, clk, 0)
	n := testNode("gpu-a")

	for i := 0; i < 3; i++ {
		if got := c.Get(context.Background(), n, false); got.Cached {
			t.Fatalf("read %d was cached with age 0", i)
		}
	}
	if f.Calls() != 3 {
		t.Fatalf("lister called %d times, want 3", f.Calls())
	}
}

func TestServedModels_ReportsUnreachableNode(t *testing.T) {
	f := &fakeLister{err: errors.New("connection refused")}
	clk := newFakeClock()
	c := newTestCache(f, clk, 300*time.Second)
	n := testNode("gpu-b")

	got := c.Get(context.Background(), n, false)
	if got.Known || got.Error != "connection refused" || got.Node != "gpu-b" {
		t.Fatalf("unreachable node not reported: %+v", got)
	}
	if got.Models == nil {
		t.Fatalf("models should be an empty list, not null")
	}
	clk.Advance(10 * time.Second)
	again := c.Get(context.Background(), n, false)
	if !again.Cached || again.Known || again.Error == "" {
		t.Fatalf("a failed read should be cached for the age: %+v", again)
	}
	if f.Calls() != 1 {
		t.Fatalf("dead node was read %d times, want 1", f.Calls())
	}
}

func TestServedModels_GetAllOrdersByNodeName(t *testing.T) {
	f := &fakeLister{models: []string{"a:1"}}
	c := newTestCache(f, newFakeClock(), 300*time.Second)
	got := c.GetAll(context.Background(), []*Node{testNode("zeta"), testNode("alpha"), testNode("mid")}, false)
	names := []string{got[0].Node, got[1].Node, got[2].Node}
	if !reflect.DeepEqual(names, []string{"alpha", "mid", "zeta"}) {
		t.Fatalf("order = %v", names)
	}
}

func TestServedModels_AgeAndTimeoutReadLive(t *testing.T) {
	f := &fakeLister{models: []string{"a:1"}}
	clk := newFakeClock()
	age := 300 * time.Second
	c := NewServedModelsCache(f.List, func() time.Duration { return age }, func() time.Duration { return time.Second }, clk.Now)
	n := testNode("gpu-a")
	c.Get(context.Background(), n, false)
	age = 0
	c.Get(context.Background(), n, false)
	if f.Calls() != 2 {
		t.Fatalf("age change was not picked up: %d calls", f.Calls())
	}
}

func TestParseOllamaTags(t *testing.T) {
	got := ParseOllamaTags([]byte(`{"models":[{"name":"llama3:70b"},{"name":""},{"name":"qwen3:8b"}]}`))
	if !reflect.DeepEqual(got, []string{"llama3:70b", "qwen3:8b"}) {
		t.Fatalf("got %v", got)
	}
	if got := ParseOllamaTags([]byte(`{"models":[]}`)); got == nil || len(got) != 0 {
		t.Fatalf("empty list should be empty and non-nil, got %#v", got)
	}
	if got := ParseOllamaTags([]byte(`not json`)); got != nil {
		t.Fatalf("bad json should be nil, got %v", got)
	}
}

func TestParseOpenAIModels(t *testing.T) {
	got := ParseOpenAIModels([]byte(`{"data":[{"id":"gpt-x"},{"id":""},{"id":"m2"}]}`))
	if !reflect.DeepEqual(got, []string{"gpt-x", "m2"}) {
		t.Fatalf("got %v", got)
	}
	if got := ParseOpenAIModels([]byte(`[`)); got != nil {
		t.Fatalf("bad json should be nil, got %v", got)
	}
}

func TestHTTPModelLister_ReadsFakeOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3-coder:30b"},{"name":"llama3:latest"}]}`))
	}))
	defer srv.Close()

	lister := HTTPModelLister()
	n := &Node{Name: "fake", Kind: KindOllama, Address: srv.URL}
	got, err := lister(context.Background(), n, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"qwen3-coder:30b", "llama3:latest"}) {
		t.Fatalf("got %v", got)
	}

	// openai-compat nodes and nodes without an address have no model list.
	if _, err := lister(context.Background(), &Node{Name: "x", Kind: KindOpenAICompat, Address: srv.URL}, time.Second); !errors.Is(err, ErrNoModelList) {
		t.Fatalf("openai-compat err = %v, want ErrNoModelList", err)
	}
	if _, err := lister(context.Background(), &Node{Name: "x", Kind: KindOllama}, time.Second); !errors.Is(err, ErrNoModelList) {
		t.Fatalf("no address err = %v, want ErrNoModelList", err)
	}
}

func TestHTTPModelLister_OpenWebUIAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	lister := HTTPModelLister()
	got, err := lister(context.Background(), &Node{Name: "w", Kind: "openwebui", Address: srv.URL}, time.Second)
	if err != nil || !reflect.DeepEqual(got, []string{"m1"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := lister(context.Background(), &Node{Name: "o", Kind: KindOllama, Address: srv.URL}, time.Second); err == nil {
		t.Fatal("expected an error for HTTP 500")
	}
}
