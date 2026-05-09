// v7.0.0 S2 — Dispatcher tests with mock adapter.

package inference

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

type mockAdapter struct {
	kind       Kind
	calls      []callRecord
	failTimes  int
	failError  error
	transient  bool
	respText   string
}

type callRecord struct {
	NodeName string
	Prompt   string
}

func (m *mockAdapter) Kind() Kind { return m.kind }

func (m *mockAdapter) Infer(_ context.Context, node *compute.Node, llm *LLM, req Request) (Response, error) {
	rec := callRecord{Prompt: req.Prompt}
	if node != nil {
		rec.NodeName = node.Name
	}
	m.calls = append(m.calls, rec)
	if m.failTimes > 0 {
		m.failTimes--
		err := m.failError
		if err == nil {
			err = errors.New("mock failure")
		}
		if m.transient {
			return Response{}, &ErrTransient{Err: err}
		}
		return Response{}, err
	}
	return Response{Text: m.respText}, nil
}

func nodeFn(nodes map[string]*compute.Node) func(string) (*compute.Node, error) {
	return func(name string) (*compute.Node, error) {
		n, ok := nodes[name]
		if !ok {
			return nil, compute.ErrNotFound
		}
		return n, nil
	}
}

func TestDispatcher_HappyPath(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "llama", Kind: KindOllama, Model: "llama3:8b", ComputeNodes: []string{"gpu-1"}})
	nodes := map[string]*compute.Node{
		"gpu-1": {Name: "gpu-1", Kind: compute.KindRemote, Address: "http://gpu-1:11434"},
	}
	d := NewDispatcher(reg, nodeFn(nodes))
	d.RegisterAdapter(&mockAdapter{kind: KindOllama, respText: "hi"})

	resp, err := d.Call(context.Background(), "llama", Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.Text != "hi" || resp.UsedNode != "gpu-1" || resp.Backend != KindOllama {
		t.Fatalf("response: %+v", resp)
	}
}

func TestDispatcher_FailoverOnTransient(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "llama", Kind: KindOllama, Model: "llama3:8b", ComputeNodes: []string{"gpu-1", "gpu-2"}})
	nodes := map[string]*compute.Node{
		"gpu-1": {Name: "gpu-1", Kind: compute.KindRemote, Address: "http://gpu-1:11434"},
		"gpu-2": {Name: "gpu-2", Kind: compute.KindRemote, Address: "http://gpu-2:11434"},
	}
	d := NewDispatcher(reg, nodeFn(nodes))
	mock := &mockAdapter{kind: KindOllama, respText: "ok-from-gpu-2", failTimes: 1, transient: true}
	d.RegisterAdapter(mock)

	resp, err := d.Call(context.Background(), "llama", Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 calls (one transient fail + one success), got %d", len(mock.calls))
	}
	if mock.calls[0].NodeName != "gpu-1" || mock.calls[1].NodeName != "gpu-2" {
		t.Fatalf("failover order: %+v", mock.calls)
	}
	if resp.UsedNode != "gpu-2" {
		t.Fatalf("used node: %s", resp.UsedNode)
	}
}

func TestDispatcher_NoFailoverOnFinal(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "llama", Kind: KindOllama, ComputeNodes: []string{"gpu-1", "gpu-2"}})
	nodes := map[string]*compute.Node{
		"gpu-1": {Name: "gpu-1", Kind: compute.KindRemote, Address: "http://gpu-1:11434"},
		"gpu-2": {Name: "gpu-2", Kind: compute.KindRemote, Address: "http://gpu-2:11434"},
	}
	d := NewDispatcher(reg, nodeFn(nodes))
	mock := &mockAdapter{kind: KindOllama, failTimes: 1, transient: false, failError: errors.New("HTTP 400 bad model")}
	d.RegisterAdapter(mock)

	_, err := d.Call(context.Background(), "llama", Request{Prompt: "ping"})
	if err == nil {
		t.Fatal("expected error on final failure")
	}
	if len(mock.calls) != 1 {
		t.Fatalf("final error should NOT failover; got %d calls", len(mock.calls))
	}
}

func TestDispatcher_RBACDeny(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "llama", Kind: KindOllama, ComputeNodes: []string{"gpu-1", "gpu-2"}})
	nodes := map[string]*compute.Node{
		"gpu-1": {
			Name: "gpu-1", Kind: compute.KindRemote, Address: "http://gpu-1:11434",
			Permissions: compute.Permissions{DeniedConsumers: []string{"council"}},
		},
		"gpu-2": {Name: "gpu-2", Kind: compute.KindRemote, Address: "http://gpu-2:11434"},
	}
	d := NewDispatcher(reg, nodeFn(nodes))
	mock := &mockAdapter{kind: KindOllama, respText: "ok"}
	d.RegisterAdapter(mock)

	_, err := d.Call(context.Background(), "llama", Request{Prompt: "ping", Consumer: "council"})
	if err != nil {
		t.Fatalf("expected to skip denied node and succeed on next: %v", err)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("RBAC denied should skip without invoking adapter; got %d", len(mock.calls))
	}
	if mock.calls[0].NodeName != "gpu-2" {
		t.Fatalf("RBAC: ran on wrong node %q", mock.calls[0].NodeName)
	}
}

func TestDispatcher_NoNodesError(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "llama", Kind: KindOllama})
	d := NewDispatcher(reg, nodeFn(map[string]*compute.Node{}))
	d.RegisterAdapter(&mockAdapter{kind: KindOllama})
	_, err := d.Call(context.Background(), "llama", Request{Prompt: "ping"})
	if err == nil || !errors.Is(err, ErrNoBackend) {
		t.Fatalf("expected ErrNoBackend, got %v", err)
	}
}

func TestDispatcher_CloudKindBypassesNodes(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "claude", Kind: KindClaude, Model: "claude-sonnet-4-6", APIKeyRef: "sk-test"})
	d := NewDispatcher(reg, nodeFn(map[string]*compute.Node{}))
	mock := &mockAdapter{kind: KindClaude, respText: "hi from claude"}
	d.RegisterAdapter(mock)

	resp, err := d.Call(context.Background(), "claude", Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.Text != "hi from claude" || resp.Backend != KindClaude {
		t.Fatalf("response: %+v", resp)
	}
	if mock.calls[0].NodeName != "" {
		t.Fatalf("cloud kind should pass nil node, got NodeName=%q", mock.calls[0].NodeName)
	}
}

func TestRegistryPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llms.json")
	r1, err := NewRegistryFromFile(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_ = r1.Add(&LLM{Name: "llama", Kind: KindOllama, Model: "llama3:8b"})
	_ = r1.Add(&LLM{Name: "claude", Kind: KindClaude, Model: "claude-sonnet-4-6", APIKeyRef: "sk"})

	r2, err := NewRegistryFromFile(path)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	if got := r2.List(); len(got) != 2 {
		t.Fatalf("re-load: got %d", len(got))
	}
}

func TestMigrateLegacyConfig(t *testing.T) {
	r := NewRegistry()
	created := MigrateLegacyConfig(r, "http://localhost:11434", "llama3:8b", "http://owui:8080", "gpt-4o", "sk-owui")
	if len(created) != 2 {
		t.Fatalf("expected 2 migrated, got %v", created)
	}
	got, _ := r.Get("ollama-default")
	if got.Kind != KindOllama || got.Model != "llama3:8b" || !got.AutoCreated {
		t.Fatalf("ollama-default: %+v", got)
	}
	got2, _ := r.Get("openwebui-default")
	if got2.Kind != KindOpenWebUI || got2.APIKeyRef != "sk-owui" {
		t.Fatalf("openwebui-default: %+v", got2)
	}
	// Re-running shouldn't duplicate.
	created2 := MigrateLegacyConfig(r, "http://localhost:11434", "llama3:8b", "http://owui:8080", "gpt-4o", "sk-owui")
	if len(created2) != 0 {
		t.Fatalf("re-migrate created %v", created2)
	}
}

func TestLLMValidate(t *testing.T) {
	cases := []struct {
		name string
		l    *LLM
		err  string
	}{
		{"empty name", &LLM{Kind: KindOllama}, "name required"},
		{"bad name chars", &LLM{Name: "BAD NAME", Kind: KindOllama}, "invalid name"},
		{"unknown kind", &LLM{Name: "x", Kind: "void"}, "unknown kind"},
		{"negative timeout", &LLM{Name: "x", Kind: KindOllama, TimeoutSeconds: -1}, ">= 0"},
		{"happy", &LLM{Name: "ollama-default", Kind: KindOllama, Model: "llama3:8b"}, ""},
	}
	for _, tc := range cases {
		err := tc.l.Validate()
		if tc.err == "" {
			if err != nil {
				t.Errorf("%s: unexpected err: %v", tc.name, err)
			}
		} else {
			if err == nil || !contains(err.Error(), tc.err) {
				t.Errorf("%s: want %q, got %v", tc.name, tc.err, err)
			}
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
