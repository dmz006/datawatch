package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

func TestSessionBackend(t *testing.T) {
	sessions := map[string]*session.Session{
		"pi":     {BackendFamily: "pi-prompt", Model: "qwen3.8:27b"},
		"claude": {BackendFamily: "claude-code", Model: "claude-opus-5-5"},
		"nomod":  {BackendFamily: "claude-code"},
		"empty":  {Model: "orphan-model"},
	}
	lookup := func(id string) (*session.Session, bool) { s, ok := sessions[id]; return s, ok }
	cases := []struct {
		name, id, backend, model string
	}{
		{"pi-prompt session", "pi", "pi-prompt", "qwen3.8:27b"},
		{"claude-code session", "claude", "claude-code", "claude-opus-5-5"},
		{"no recorded model", "nomod", "claude-code", ""},
		{"missing session", "gone", "", ""},
		{"empty BackendFamily", "empty", "", ""},
	}
	fn := sessionBackend(lookup)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, m := fn(c.id)
			if b != c.backend || m != c.model {
				t.Fatalf("got (%q,%q), want (%q,%q)", b, m, c.backend, c.model)
			}
		})
	}
}

func TestSessionBackendNilLookup(t *testing.T) {
	if b, m := sessionBackend(nil)("x"); b != "" || m != "" {
		t.Fatalf("nil lookup should give empty, got (%q,%q)", b, m)
	}
}
