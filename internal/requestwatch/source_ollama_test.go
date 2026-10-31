package requestwatch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingNode is an httptest node that records every request it receives.
type recordingNode struct {
	mu   sync.Mutex
	reqs []string
	srv  *httptest.Server
}

func newRecordingNode(t *testing.T, status int, body string) *recordingNode {
	t.Helper()
	n := &recordingNode{}
	n.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.mu.Lock()
		n.reqs = append(n.reqs, r.Method+" "+r.URL.Path)
		n.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(n.srv.Close)
	return n
}

func (n *recordingNode) requests() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.reqs...)
}

const psLoaded = `{"models":[{"name":"m1:7b","model":"m1:7b","expires_at":"2026-01-02T03:14:05Z","size_vram":1000,"context_length":4096}]}`

func TestOllamaSourceNoModelCall(t *testing.T) {
	n := newRecordingNode(t, 200, psLoaded)
	src := &OllamaSource{}
	if _, err := src.Sample(context.Background(), NodeRef{Name: "n", Address: n.srv.URL}, "m1:7b"); err != nil {
		t.Fatal(err)
	}
	got := n.requests()
	if len(got) != 1 || got[0] != "GET /api/ps" {
		t.Fatalf("requests = %v, want exactly [GET /api/ps]", got)
	}
}

func TestOllamaSourceNoModelCallWhenNotLoadedOrFailing(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"not loaded": {200, `{"models":[]}`},
		"bad status": {500, `oops`},
		"bad body":   {200, `not json`},
	} {
		t.Run(name, func(t *testing.T) {
			n := newRecordingNode(t, tc.status, tc.body)
			_, _ = (&OllamaSource{}).Sample(context.Background(), NodeRef{Address: n.srv.URL}, "m1")
			got := n.requests()
			if len(got) != 1 || got[0] != "GET /api/ps" {
				t.Fatalf("requests = %v, want exactly [GET /api/ps]", got)
			}
		})
	}
}

func TestOllamaSourceLoadedFingerprint(t *testing.T) {
	n := newRecordingNode(t, 200, psLoaded)
	s, err := (&OllamaSource{}).Sample(context.Background(), NodeRef{Address: n.srv.URL}, "m1:7b")
	if err != nil {
		t.Fatal(err)
	}
	if !s.ModelLoaded || s.Fingerprint != "m1:7b|2026-01-02T03:14:05Z|1000|4096" {
		t.Fatalf("sample = %+v", s)
	}
	if s.GPUKnown {
		t.Fatal("gpu must be unknown without a GPU func")
	}
}

func TestOllamaSourceFingerprintChangesWithEntry(t *testing.T) {
	bodies := []string{
		`{"models":[{"name":"m1","expires_at":"A","size_vram":1,"context_length":1}]}`,
		`{"models":[{"name":"m1","expires_at":"B","size_vram":1,"context_length":1}]}`,
		`{"models":[{"name":"m1","expires_at":"B","size_vram":2,"context_length":1}]}`,
		`{"models":[{"name":"m1","expires_at":"B","size_vram":2,"context_length":2}]}`,
	}
	seen := map[string]bool{}
	for _, b := range bodies {
		n := newRecordingNode(t, 200, b)
		s, err := (&OllamaSource{}).Sample(context.Background(), NodeRef{Address: n.srv.URL}, "m1")
		if err != nil {
			t.Fatal(err)
		}
		if seen[s.Fingerprint] {
			t.Fatalf("fingerprint %q repeated", s.Fingerprint)
		}
		seen[s.Fingerprint] = true
	}
}

func TestOllamaSourceNotLoaded(t *testing.T) {
	n := newRecordingNode(t, 200, `{"models":[{"name":"other:1b","expires_at":"x"}]}`)
	s, err := (&OllamaSource{}).Sample(context.Background(), NodeRef{Address: n.srv.URL}, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if s.ModelLoaded || s.Fingerprint != FingerprintNotLoaded {
		t.Fatalf("sample = %+v", s)
	}
	empty := newRecordingNode(t, 200, `{}`)
	s, err = (&OllamaSource{}).Sample(context.Background(), NodeRef{Address: empty.srv.URL}, "m1")
	if err != nil || s.ModelLoaded || s.Fingerprint != FingerprintNotLoaded {
		t.Fatalf("empty listing: %+v err=%v", s, err)
	}
}

func TestOllamaSourceModelNameMatching(t *testing.T) {
	cases := []struct {
		name   string
		listed string
		model  string
		want   bool
	}{
		{"exact", "m1:7b", "m1:7b", true},
		{"provider prefix", "m1:7b", "ollama/m1:7b", true},
		{"other provider prefix", "m1:7b", "provider/m1:7b", true},
		{"case insensitive", "M1:7B", "m1:7b", true},
		{"untagged means latest", "m1:latest", "m1", true},
		{"untagged prefixed", "m1:latest", "ollama/m1", true},
		{"namespaced listing", "hub/user/m1:7b", "hub/user/m1:7b", true},
		{"different tag", "m1:7b", "m1:13b", false},
		{"different model", "m2:7b", "m1:7b", false},
		{"prefix is not a substring match", "xm1:7b", "m1:7b", false},
		{"empty model matches any", "m1:7b", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := `{"models":[{"name":"` + c.listed + `","model":"` + c.listed + `","expires_at":"e"}]}`
			n := newRecordingNode(t, 200, body)
			s, err := (&OllamaSource{}).Sample(context.Background(), NodeRef{Address: n.srv.URL}, c.model)
			if err != nil {
				t.Fatal(err)
			}
			if s.ModelLoaded != c.want {
				t.Fatalf("loaded = %v, want %v", s.ModelLoaded, c.want)
			}
		})
	}
}

func TestOllamaSourceMatchesModelField(t *testing.T) {
	n := newRecordingNode(t, 200, `{"models":[{"name":"alias","model":"m1:7b","expires_at":"e"}]}`)
	s, err := (&OllamaSource{}).Sample(context.Background(), NodeRef{Address: n.srv.URL}, "m1:7b")
	if err != nil || !s.ModelLoaded {
		t.Fatalf("model field not matched: %+v err=%v", s, err)
	}
}

func TestOllamaSourceAddsScheme(t *testing.T) {
	n := newRecordingNode(t, 200, psLoaded)
	bare := strings.TrimPrefix(n.srv.URL, "http://")
	if _, err := (&OllamaSource{}).Sample(context.Background(), NodeRef{Address: bare + "/"}, "m1:7b"); err != nil {
		t.Fatal(err)
	}
	if got := n.requests(); len(got) != 1 || got[0] != "GET /api/ps" {
		t.Fatalf("requests = %v", got)
	}
}

func TestOllamaSourceNoAddress(t *testing.T) {
	if _, err := (&OllamaSource{}).Sample(context.Background(), NodeRef{}, "m1"); err == nil {
		t.Fatal("expected an error for a node with no address")
	}
}

func TestOllamaSourceBadStatus(t *testing.T) {
	n := newRecordingNode(t, 503, `busy`)
	_, err := (&OllamaSource{}).Sample(context.Background(), NodeRef{Address: n.srv.URL}, "m1")
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %v, want a 503 error", err)
	}
}

func TestOllamaSourceBadBody(t *testing.T) {
	n := newRecordingNode(t, 200, `{"models": [`)
	if _, err := (&OllamaSource{}).Sample(context.Background(), NodeRef{Address: n.srv.URL}, "m1"); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestOllamaSourceBodyLimited(t *testing.T) {
	big := `{"models":[{"name":"m1","pad":"` + strings.Repeat("x", maxOllamaBody) + `"}]}`
	n := newRecordingNode(t, 200, big)
	if _, err := (&OllamaSource{}).Sample(context.Background(), NodeRef{Address: n.srv.URL}, "m1"); err == nil {
		t.Fatal("expected an error for a body over the size limit")
	}
}

func TestOllamaSourceClosedServerUnreachable(t *testing.T) {
	n := newRecordingNode(t, 200, psLoaded)
	addr := n.srv.URL
	n.srv.Close()
	if _, err := (&OllamaSource{}).Sample(context.Background(), NodeRef{Address: addr}, "m1:7b"); err == nil {
		t.Fatal("expected an error for a closed server")
	}
}

func TestOllamaSourceTimeoutBounded(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	src := &OllamaSource{Timeout: 50 * time.Millisecond}
	start := time.Now()
	if _, err := src.Sample(context.Background(), NodeRef{Address: srv.URL}, "m1"); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("request was not bounded by the timeout")
	}
}

func TestOllamaSourceGPU(t *testing.T) {
	n := newRecordingNode(t, 200, psLoaded)
	node := NodeRef{Name: "n", Address: n.srv.URL, ObserverPeer: "peer"}

	var got NodeRef
	src := &OllamaSource{GPU: func(_ context.Context, nr NodeRef) (float64, bool) {
		got = nr
		return 42.5, true
	}}
	s, err := src.Sample(context.Background(), node, "m1:7b")
	if err != nil {
		t.Fatal(err)
	}
	if !s.GPUKnown || s.GPUPercent != 42.5 || got.ObserverPeer != "peer" {
		t.Fatalf("gpu sample = %+v node=%+v", s, got)
	}

	notOK := &OllamaSource{GPU: func(context.Context, NodeRef) (float64, bool) { return 99, false }}
	s, err = notOK.Sample(context.Background(), node, "m1:7b")
	if err != nil || s.GPUKnown || s.GPUPercent != 0 {
		t.Fatalf("gpu not ok = %+v err=%v", s, err)
	}
}

func TestOllamaSourceImplementsSource(t *testing.T) {
	var _ Source = (*OllamaSource)(nil)
}
