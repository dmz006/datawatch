package server

import (
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/inference"
)

// The pools a claude-code entry's start uses include the account pool, so the
// admission query and the Automaton capacity card agree with the real start.
func TestResolvePoolsForBackendAddsAccountPoolForClaudeCode(t *testing.T) {
	reg := inference.NewRegistry()
	for _, l := range []inference.LLM{
		{Name: "claude-code", Kind: inference.KindClaudeCode},
		{Name: "pi", Kind: inference.KindPi},
	} {
		l := l
		if err := reg.Add(&l); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{inferenceReg: reg}
	if got, want := s.resolvePoolsForBackend("claude-code"), []string{"llm:claude-code", "account:claude"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claude-code pools = %v, want %v", got, want)
	}
	if got, want := s.resolvePoolsForBackend("pi"), []string{"llm:pi"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pi pools = %v, want %v", got, want)
	}
}

// The admission query names the class a start would take: an interactive
// (operator) query is the operator class, and a supervisor query the
// supervisor class.
func TestAdmissionQueryRoleSetsClass(t *testing.T) {
	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "claude-code", Kind: inference.KindClaudeCode}); err != nil {
		t.Fatal(err)
	}
	s := &Server{inferenceReg: reg}
	for role, want := range map[string]capacity.Class{"interactive": capacity.ClassOperator, "operator": capacity.ClassOperator, "supervisor": capacity.ClassSupervisor, "worker": ""} {
		req, msg := s.admissionRequest(map[string][]string{"backend": {"claude-code"}, "role": {role}})
		if msg != "" {
			t.Fatalf("role %s: %s", role, msg)
		}
		if req.Class != want {
			t.Errorf("role %s: class %q, want %q", role, req.Class, want)
		}
	}
	if _, msg := s.admissionRequest(map[string][]string{"backend": {"claude-code"}, "role": {"bogus"}}); msg == "" {
		t.Fatal("a bogus role must be refused")
	}
}
