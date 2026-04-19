// BL96 — recursive wake-up (L0 overlay, L4, L5) tests.

package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakePeerLister returns a fixed roster.
type fakePeerLister struct{ list []PeerAgent }

func (f *fakePeerLister) ListAgents() []PeerAgent { return f.list }

// retrieverFixture is the reusable spin-up used by all the layers_recursive tests.
func retrieverFixture(t *testing.T) (*Retriever, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	emb := &fixedEmbedder{}
	return NewRetriever(store, emb, 5), dir
}

// fixedEmbedder is the simplest Embedder for the recall path.
type fixedEmbedder struct{}

func (f *fixedEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}
func (f *fixedEmbedder) Dimensions() int { return 3 }
func (f *fixedEmbedder) Name() string    { return "fixed" }

func TestL0ForAgent_FallsBackToHostFile(t *testing.T) {
	r, dir := retrieverFixture(t)
	_ = os.WriteFile(filepath.Join(dir, "identity.txt"), []byte("host id"), 0644)
	l := NewLayers(dir, r)
	if got := l.L0ForAgent(""); got != "host id" {
		t.Errorf("empty agentID got %q want host id", got)
	}
	if got := l.L0ForAgent("agent-x"); got != "host id" {
		t.Errorf("missing overlay got %q want host id", got)
	}
}

func TestL0ForAgent_Overlay(t *testing.T) {
	r, dir := retrieverFixture(t)
	_ = os.WriteFile(filepath.Join(dir, "identity.txt"), []byte("host id"), 0644)
	overlayDir := filepath.Join(dir, "agents", "agent-x")
	_ = os.MkdirAll(overlayDir, 0700)
	_ = os.WriteFile(filepath.Join(overlayDir, "identity.txt"),
		[]byte("validator role"), 0644)

	l := NewLayers(dir, r)
	if got := l.L0ForAgent("agent-x"); got != "validator role" {
		t.Errorf("overlay not used: got %q", got)
	}
	if got := l.L0ForAgent("agent-y"); got != "host id" {
		t.Errorf("non-overlay agent should fall back: got %q", got)
	}
}

func TestL5_ListsSiblingsExcludingSelf(t *testing.T) {
	r, dir := retrieverFixture(t)
	l := NewLayers(dir, r)
	l.SetPeerLister(&fakePeerLister{list: []PeerAgent{
		{ID: "a", ParentAgentID: "P", State: "running", Branch: "feat-a", Task: "t-a"},
		{ID: "b", ParentAgentID: "P", State: "ready", Branch: "feat-b", Task: "t-b"},
		{ID: "c", ParentAgentID: "OTHER", State: "running", Task: "unrelated"},
		{ID: "self", ParentAgentID: "P", State: "running", Task: "filtered"},
	}})
	out := l.L5("self", "P")
	if !strings.Contains(out, "a") || !strings.Contains(out, "b") {
		t.Errorf("L5 missing siblings: %s", out)
	}
	if strings.Contains(out, "self") || strings.Contains(out, "OTHER") || strings.Contains(out, "unrelated") {
		t.Errorf("L5 should exclude self + non-siblings: %s", out)
	}
}

func TestL5_NoPeersWiredReturnsEmpty(t *testing.T) {
	r, dir := retrieverFixture(t)
	l := NewLayers(dir, r)
	if got := l.L5("self", "P"); got != "" {
		t.Errorf("expected empty L5 with no peer lister, got %q", got)
	}
}

func TestL5_NoParentReturnsEmpty(t *testing.T) {
	r, dir := retrieverFixture(t)
	l := NewLayers(dir, r)
	l.SetPeerLister(&fakePeerLister{list: []PeerAgent{{ID: "a"}}})
	if got := l.L5("self", ""); got != "" {
		t.Errorf("L5 should return empty for top-level spawn: got %q", got)
	}
}

func TestL4_EmptyNamespace_NoOp(t *testing.T) {
	r, dir := retrieverFixture(t)
	l := NewLayers(dir, r)
	if got := l.L4("", 100); got != "" {
		t.Errorf("L4 with empty namespace must be empty, got %q", got)
	}
}

func TestWakeUpContextForAgent_Composes(t *testing.T) {
	r, dir := retrieverFixture(t)
	_ = os.WriteFile(filepath.Join(dir, "identity.txt"), []byte("host id"), 0644)
	l := NewLayers(dir, r)
	l.SetPeerLister(&fakePeerLister{list: []PeerAgent{
		{ID: "sib1", ParentAgentID: "P", State: "ready", Task: "lint"},
	}})
	out := l.WakeUpContextForAgent("self", "P", "", "/proj")
	if !strings.Contains(out, "Identity") {
		t.Errorf("missing Identity section: %s", out)
	}
	if !strings.Contains(out, "Sibling workers") {
		t.Errorf("missing Sibling workers section: %s", out)
	}
}
