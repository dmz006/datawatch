// observerpeer.Client tests — ported + extended from the original
// cmd/datawatch-stats/peer_test.go when S13 hoisted the client into a
// shared package. New cases cover SetToken (the agent-as-peer flow)
// and Shape selection.

package observerpeer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dmz006/datawatch/internal/observer"
)

type fakeParent struct {
	srv          *httptest.Server
	mintedTokens map[string]string
	registers    int32
	pushes       int32
	lastBody     atomic.Value // string — last push body
	rejectNext   atomic.Bool
}

func newFakeParent(t *testing.T) *fakeParent {
	t.Helper()
	fp := &fakeParent{mintedTokens: map[string]string{}}
	fp.srv = httptest.NewServer(http.HandlerFunc(fp.handle))
	t.Cleanup(fp.srv.Close)
	return fp
}

func (f *fakeParent) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/observer/peers":
		atomic.AddInt32(&f.registers, 1)
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		token := "t-" + body.Name + "-" + strings.Repeat("x", int(atomic.LoadInt32(&f.registers)))
		f.mintedTokens[body.Name] = token
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":  body.Name,
			"shape": "B",
			"token": token,
		})
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/observer/peers/") && strings.HasSuffix(r.URL.Path, "/stats"):
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "no bearer", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/observer/peers/"), "/stats")
		if f.rejectNext.CompareAndSwap(true, false) {
			http.Error(w, "forced 401", http.StatusUnauthorized)
			return
		}
		want, ok := f.mintedTokens[name]
		if !ok || token != want {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		atomic.AddInt32(&f.pushes, 1)
		buf, _ := io.ReadAll(r.Body)
		f.lastBody.Store(string(buf))
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func newClient(t *testing.T, fp *fakeParent, name, tokenPath string) *Client {
	t.Helper()
	c, err := New(Config{
		ParentURL: fp.srv.URL,
		Name:      name,
		Shape:     "B",
		TokenPath: tokenPath,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNew_RequiresParentAndName(t *testing.T) {
	if _, err := New(Config{Name: "x"}); err == nil {
		t.Errorf("missing parent should error")
	}
	if _, err := New(Config{ParentURL: "http://x"}); err == nil {
		t.Errorf("missing name should error")
	}
}

func TestNew_DefaultShapeIsB(t *testing.T) {
	c, err := New(Config{ParentURL: "http://x", Name: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if c.shape != "B" {
		t.Errorf("default shape = %q want B", c.shape)
	}
}

func TestNew_ExplicitShapeRespected(t *testing.T) {
	c, _ := New(Config{ParentURL: "http://x", Name: "n", Shape: "a"})
	if c.shape != "A" {
		t.Errorf("shape = %q want A (uppercased)", c.shape)
	}
}

func TestRegister_PersistsTokenWithMode0600(t *testing.T) {
	fp := newFakeParent(t)
	tokenPath := filepath.Join(t.TempDir(), "nested", "peer.token")
	c := newClient(t, fp, "ollama", tokenPath)

	if err := c.Register(context.Background(), "0.1", HostInfo()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !c.HasToken() {
		t.Fatal("token missing in memory after Register")
	}
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != fp.mintedTokens["ollama"] {
		t.Errorf("on-disk token = %q want %q", got, fp.mintedTokens["ollama"])
	}
	info, _ := os.Stat(tokenPath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v want 0600", info.Mode().Perm())
	}
}

func TestLoadToken_SkipsRegister(t *testing.T) {
	fp := newFakeParent(t)
	tokenPath := filepath.Join(t.TempDir(), "peer.token")
	if err := os.WriteFile(tokenPath, []byte("preseeded\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newClient(t, fp, "ollama", tokenPath)
	c.LoadToken()
	if !c.HasToken() {
		t.Errorf("expected token loaded from disk")
	}
	if atomic.LoadInt32(&fp.registers) != 0 {
		t.Errorf("LoadToken should not register")
	}
}

func TestSetToken_AgentFlow_SkipsRegister(t *testing.T) {
	// S13 — agent gets token via env var; no register call.
	fp := newFakeParent(t)
	c := newClient(t, fp, "agt_a1b2", "")
	c.SetToken("preminted-token")
	if !c.HasToken() {
		t.Errorf("expected token in memory")
	}
	if atomic.LoadInt32(&fp.registers) != 0 {
		t.Errorf("SetToken should not register")
	}
}

func TestPush_HappyPath(t *testing.T) {
	fp := newFakeParent(t)
	c := newClient(t, fp, "ollama", filepath.Join(t.TempDir(), "p.tok"))
	if err := c.Register(context.Background(), "0.1", nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Push(context.Background(), &observer.StatsResponse{V: 2}, "0.1", nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if atomic.LoadInt32(&fp.pushes) != 1 {
		t.Errorf("push count = %d want 1", atomic.LoadInt32(&fp.pushes))
	}
	if got, _ := fp.lastBody.Load().(string); !strings.Contains(got, `"shape":"B"`) {
		t.Errorf("push body shape = ?? body=%s", got)
	}
}

func TestPush_ShapeAFromAgentClient(t *testing.T) {
	// S13 — agent peers push with shape "A"; verify the client's
	// configured shape lands in the wire body.
	fp := newFakeParent(t)
	c, _ := New(Config{ParentURL: fp.srv.URL, Name: "agt_a1", Shape: "A"})
	c.SetToken("agent-token")
	// Pre-mint matching token in fake parent so the push isn't 401.
	fp.mintedTokens["agt_a1"] = "agent-token"

	if err := c.Push(context.Background(), &observer.StatsResponse{V: 2}, "0.1", nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	body, _ := fp.lastBody.Load().(string)
	if !strings.Contains(body, `"shape":"A"`) {
		t.Errorf("expected shape:A in push body, got %s", body)
	}
}

func TestPush_AutoReregistersOn401(t *testing.T) {
	fp := newFakeParent(t)
	c := newClient(t, fp, "ollama", filepath.Join(t.TempDir(), "p.tok"))
	if err := c.Register(context.Background(), "0.1", nil); err != nil {
		t.Fatal(err)
	}
	registersBefore := atomic.LoadInt32(&fp.registers)
	fp.rejectNext.Store(true)

	if err := c.Push(context.Background(), &observer.StatsResponse{V: 2}, "0.1", nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if atomic.LoadInt32(&fp.registers) <= registersBefore {
		t.Errorf("expected re-register after 401")
	}
	if atomic.LoadInt32(&fp.pushes) != 1 {
		t.Errorf("push should have succeeded on retry")
	}
}

func TestPush_SurfacesNon401Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/observer/peers" {
			_, _ = w.Write([]byte(`{"token":"t"}`))
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c, _ := New(Config{ParentURL: srv.URL, Name: "x"})
	if err := c.Register(context.Background(), "0", nil); err != nil {
		t.Fatal(err)
	}
	err := c.Push(context.Background(), &observer.StatsResponse{V: 2}, "0", nil)
	if err == nil {
		t.Fatal("expected error from 500")
	}
	if errors.Is(err, ErrUnauthorized) {
		t.Errorf("500 should not be reported as ErrUnauthorized: %v", err)
	}
}

func TestHostInfo_PopulatesBasicFields(t *testing.T) {
	hi := HostInfo()
	for _, key := range []string{"hostname", "os", "arch"} {
		if hi[key] == nil || hi[key] == "" {
			t.Errorf("HostInfo missing %s: %+v", key, hi)
		}
	}
}
