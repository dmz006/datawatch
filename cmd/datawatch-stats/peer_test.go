package main

import (
	"context"
	"encoding/json"
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

// fakeParent simulates the parent's /api/observer/peers* surface.
// Tracks how many times each route was hit and what tokens it has
// minted, so tests can assert both side-effects and wire fidelity.
type fakeParent struct {
	srv          *httptest.Server
	mintedTokens map[string]string // name → current token
	registers    int32
	pushes       int32
	rejectNext   atomic.Bool // when true, next push gets 401 (and toggles back)
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
		// Auth check + optional 401 forcing.
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
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func TestPeerClient_RegisterPersistsTokenToDisk(t *testing.T) {
	fp := newFakeParent(t)
	tokenPath := filepath.Join(t.TempDir(), "nested", "peer.token")

	pc, err := newPeerClient(fp.srv.URL, "ollama", tokenPath, false)
	if err != nil {
		t.Fatalf("newPeerClient: %v", err)
	}
	if err := pc.register(context.Background(), "B", "0.1", runtimeHostInfo()); err != nil {
		t.Fatalf("register: %v", err)
	}
	if !pc.hasToken() {
		t.Fatal("token not held in memory after register")
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
		t.Errorf("token-file perm = %v want 0600", info.Mode().Perm())
	}
}

func TestPeerClient_LoadTokenSkipsRegister(t *testing.T) {
	fp := newFakeParent(t)
	tokenPath := filepath.Join(t.TempDir(), "peer.token")
	// Pre-seed the file.
	if err := os.WriteFile(tokenPath, []byte("preseeded-token\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	pc, err := newPeerClient(fp.srv.URL, "ollama", tokenPath, false)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	pc.loadToken()
	if !pc.hasToken() {
		t.Errorf("expected token loaded from disk")
	}
	if atomic.LoadInt32(&fp.registers) != 0 {
		t.Errorf("loadToken should not register")
	}
}

func TestPeerClient_PushHappyPath(t *testing.T) {
	fp := newFakeParent(t)
	pc, _ := newPeerClient(fp.srv.URL, "ollama", filepath.Join(t.TempDir(), "p.tok"), false)
	if err := pc.register(context.Background(), "B", "0.1", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	snap := &observer.StatsResponse{V: 2}
	if err := pc.push(context.Background(), snap, "B", "0.1", nil); err != nil {
		t.Fatalf("push: %v", err)
	}
	if got := atomic.LoadInt32(&fp.pushes); got != 1 {
		t.Errorf("push count = %d want 1", got)
	}
}

func TestPeerClient_PushAutoReregistersOn401(t *testing.T) {
	fp := newFakeParent(t)
	pc, _ := newPeerClient(fp.srv.URL, "ollama", filepath.Join(t.TempDir(), "p.tok"), false)
	if err := pc.register(context.Background(), "B", "0.1", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	registersBefore := atomic.LoadInt32(&fp.registers)

	// Force the next push to 401, then verify the client re-registers
	// and retries successfully.
	fp.rejectNext.Store(true)
	snap := &observer.StatsResponse{V: 2}
	if err := pc.push(context.Background(), snap, "B", "0.1", nil); err != nil {
		t.Fatalf("push: %v", err)
	}
	if atomic.LoadInt32(&fp.registers) <= registersBefore {
		t.Errorf("expected re-register after 401, register count unchanged")
	}
	if got := atomic.LoadInt32(&fp.pushes); got != 1 {
		t.Errorf("push count = %d want 1", got)
	}
}

func TestPeerClient_PushSurfacesNon401Errors(t *testing.T) {
	// Custom server that always returns 500 to push.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/observer/peers" && r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"token":"t"}`))
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	pc, _ := newPeerClient(srv.URL, "x", "", false)
	if err := pc.register(context.Background(), "B", "0", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	err := pc.push(context.Background(), &observer.StatsResponse{V: 2}, "B", "0", nil)
	if err == nil {
		t.Fatal("expected error from 500")
	}
}

func TestPeerClient_NewRequiresParentAndName(t *testing.T) {
	if _, err := newPeerClient("", "x", "", false); err == nil {
		t.Errorf("missing parent should error")
	}
	if _, err := newPeerClient("http://x", "", "", false); err == nil {
		t.Errorf("missing name should error")
	}
}
