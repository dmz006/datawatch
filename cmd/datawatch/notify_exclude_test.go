package main

import "testing"

func TestNotifyExcludeSet_EmptyIsNil(t *testing.T) {
	if s := notifyExcludeSet(nil); s != nil {
		t.Fatalf("expected nil for no names, got %v", s)
	}
	if s := notifyExcludeSet([]string{}); s != nil {
		t.Fatalf("expected nil for empty slice, got %v", s)
	}
}

func TestNotifyExcludeSet_ContainsGivenNames(t *testing.T) {
	s := notifyExcludeSet([]string{"imap_mcp", "ntfy"})
	if s == nil || !s["imap_mcp"] || !s["ntfy"] || s["signal"] {
		t.Fatalf("got %v", s)
	}
}

func TestIsNotifyExcluded(t *testing.T) {
	if isNotifyExcluded(nil, "imap_mcp") {
		t.Fatal("nil set must never exclude anything")
	}
	s := notifyExcludeSet([]string{"imap_mcp"})
	if !isNotifyExcluded(s, "imap_mcp") {
		t.Fatal("expected imap_mcp to be excluded")
	}
	if isNotifyExcluded(s, "signal") {
		t.Fatal("signal was not in the exclude list")
	}
}
