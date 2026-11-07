package server

import "testing"

func TestSessionMemoryCeilingNilWithoutManager(t *testing.T) {
	s := &Server{}
	if got := s.sessionMemoryCeiling(); got != nil {
		t.Fatalf("want nil with no autonomous manager, got %+v", got)
	}
}
