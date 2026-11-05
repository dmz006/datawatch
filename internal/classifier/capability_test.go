package classifier

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func serve(t *testing.T, status int, fixture string, hits *int32) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		w.WriteHeader(status)
		if fixture != "" {
			b, _ := fixtureFS.ReadFile("testdata/" + fixture)
			_, _ = w.Write(b)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// probeBody is the native fixture re-keyed to the probe's question name.
func probeServer(t *testing.T, native bool, hits *int32) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		if native {
			_, _ = w.Write([]byte(`{"questions":{"probe":{"choice":"A","probabilities":{"A":0.9,"B":0.1}}}}`))
			return
		}
		b, _ := fixtureFS.ReadFile("testdata/" + FixtureLogprobs)
		_, _ = w.Write(b)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestProbeTable(t *testing.T) {
	tests := []struct {
		name   string
		path   Path
		status int
		body   string
		want   State
	}{
		{"native ok", PathNative, 200, `{"questions":{"probe":{"choice":"A","probabilities":{"A":1}}}}`, StateSupported},
		{"native 404", PathNative, 404, "", StateUnsupported},
		{"native 405", PathNative, 405, "", StateUnsupported},
		{"native 200 wrong shape", PathNative, 200, `{"choices":[]}`, StateUnsupported},
		{"native 200 not json", PathNative, 200, `nope`, StateUnsupported},
		{"native 500", PathNative, 500, "", StateUnknown},
		{"logprobs ok", PathLogprobs, 200, mustFixture(t, FixtureLogprobs), StateSupported},
		{"logprobs absent", PathLogprobs, 200, `{"choices":[{"message":{"content":"A"}}]}`, StateUnsupported},
		{"logprobs 400", PathLogprobs, 400, "", StateUnsupported},
		{"logprobs 503", PathLogprobs, 503, "", StateUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer s.Close()
			p := NewProber(s.Client(), func(string) (string, error) { return s.URL, nil }, Config{}, nil)
			got, err := p.Probe(context.Background(), "n1", "m", tc.path)
			if got != tc.want {
				t.Fatalf("state %s, want %s (err %v)", got, tc.want, err)
			}
			if tc.want == StateUnknown && err == nil {
				t.Error("unknown must carry a cause")
			}
			rerr := p.Require(context.Background(), "n1", "m", tc.path)
			if (tc.want == StateSupported) != (rerr == nil) {
				t.Errorf("Require err = %v for %s", rerr, tc.want)
			}
			if rerr != nil {
				var ce *CapabilityError
				if !errors.As(rerr, &ce) || ce.State != tc.want || !IsCapabilityError(rerr) || ce.Error() == "" {
					t.Errorf("bad typed error: %v", rerr)
				}
				if FailureCategory(rerr) != "infra" {
					t.Error("capability failure must be infra")
				}
			}
		})
	}
}

func mustFixture(t *testing.T, name string) string {
	b, err := fixtureFS.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestProbeCachePerModelNodeAndTTL(t *testing.T) {
	var hits int32
	s := probeServer(t, true, &hits)
	now := time.Unix(1000, 0)
	p := NewProber(s.Client(), func(string) (string, error) { return s.URL, nil }, Config{ProbeTTL: time.Minute}, func() time.Time { return now })
	ctx := context.Background()
	probe := func(node, model string) {
		if st, _ := p.Probe(ctx, node, model, PathNative); st != StateSupported {
			t.Fatalf("state %s", st)
		}
	}
	probe("a", "m1")
	probe("a", "m1")
	if hits != 1 {
		t.Fatalf("cache miss: %d hits", hits)
	}
	probe("a", "m2") // other model
	probe("b", "m1") // other node
	if hits != 3 {
		t.Fatalf("per model/node keys: %d hits", hits)
	}
	now = now.Add(2 * time.Minute) // expired
	probe("a", "m1")
	if hits != 4 {
		t.Fatalf("ttl expiry: %d hits", hits)
	}
	p.Invalidate("a")
	probe("a", "m1")
	probe("b", "m1")
	if hits != 6 { // a re-probed after invalidate; b expired with the clock
		t.Fatalf("invalidate: %d hits", hits)
	}
}

func TestUnknownNotCached(t *testing.T) {
	var hits int32
	s := serve(t, 500, "", &hits)
	p := NewProber(s.Client(), func(string) (string, error) { return s.URL, nil }, Config{}, nil)
	for i := 0; i < 2; i++ {
		if st, _ := p.Probe(context.Background(), "n", "m", PathNative); st != StateUnknown {
			t.Fatal(st)
		}
	}
	if hits != 2 {
		t.Errorf("unknown was cached: %d hits", hits)
	}
}

func TestProbeFailures(t *testing.T) {
	ctx := context.Background()
	bad := NewProber(nil, func(string) (string, error) { return "", errors.New("no node") }, Config{}, nil)
	if st, err := bad.Probe(ctx, "n", "m", PathNative); st != StateUnknown || err == nil {
		t.Error("resolver error must be unknown with cause")
	}
	p := NewProber(nil, func(string) (string, error) { return "http://x", nil }, Config{}, nil)
	if st, err := p.Probe(ctx, "n", "m", Path("bogus")); st != StateUnknown || err == nil {
		t.Error("bogus path must be unknown")
	}
	// bad URL: request construction fails
	p = NewProber(nil, func(string) (string, error) { return "http://[::1", nil }, Config{}, nil)
	if st, _ := p.Probe(ctx, "n", "m", PathNative); st != StateUnknown {
		t.Error("bad url must be unknown")
	}
	// transport error
	s := httptest.NewServer(http.NotFoundHandler())
	url := s.URL
	s.Close()
	p = NewProber(nil, func(string) (string, error) { return url, nil }, Config{}, nil)
	if st, _ := p.Probe(ctx, "n", "m", PathLogprobs); st != StateUnknown {
		t.Error("connection refused must be unknown")
	}
}

func TestProbeTimeout(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer s.Close()
	p := NewProber(s.Client(), func(string) (string, error) { return s.URL, nil }, Config{ProbeTimeout: 50 * time.Millisecond}, nil)
	st, err := p.Probe(context.Background(), "n", "m", PathNative)
	if st != StateUnknown || err == nil {
		t.Errorf("timeout must be unknown with cause, got %s %v", st, err)
	}
	if !strings.Contains(p.Require(context.Background(), "n", "m", PathNative).Error(), "unknown") {
		t.Error("typed error must name state")
	}
}

func TestConfigDefaults(t *testing.T) {
	c := Config{}.withDefaults()
	if c.ProbeTTL != DefaultProbeTTL || c.ProbeTimeout != DefaultProbeTimeout {
		t.Errorf("%+v", c)
	}
}

type fakeGate struct {
	err      error
	acquired int
	released int
	node     string
}

func (g *fakeGate) Acquire(_ context.Context, node, _ string) (func(), error) {
	if g.err != nil {
		return nil, g.err
	}
	g.acquired++
	g.node = node
	return func() { g.released++ }, nil
}

func TestWithGate(t *testing.T) {
	ctx := context.Background()
	g := &fakeGate{}
	ran := false
	if err := WithGate(ctx, g, "n1", "m", func(context.Context) error { ran = true; return nil }); err != nil || !ran || g.acquired != 1 || g.released != 1 || g.node != "n1" {
		t.Fatalf("happy path: %v %+v", err, g)
	}
	sentinel := errors.New("fn failed")
	g = &fakeGate{}
	if err := WithGate(ctx, g, "n", "m", func(context.Context) error { return sentinel }); !errors.Is(err, sentinel) || g.released != 1 {
		t.Errorf("fn error must pass through and release: %v %+v", err, g)
	}
	refuse := errors.New("pool full")
	ran = false
	err := WithGate(ctx, &fakeGate{err: refuse}, "n", "m", func(context.Context) error { ran = true; return nil })
	var ge *GateError
	if ran || !errors.As(err, &ge) || !errors.Is(err, refuse) || ge.Error() == "" || FailureCategory(err) != "infra" {
		t.Errorf("refused gate must block with typed infra error: %v ran=%v", err, ran)
	}
	if err := WithGate(ctx, nil, "n", "m", func(context.Context) error { ran = true; return nil }); !errors.As(err, &ge) {
		t.Error("nil gate must error, not bypass")
	}
	if FailureCategory(nil) != "" {
		t.Error("nil error has no category")
	}
}
