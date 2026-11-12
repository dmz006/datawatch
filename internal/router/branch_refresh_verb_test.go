package router

import "testing"

func TestParseBranchRefreshMode(t *testing.T) {
	for in, want := range map[string]string{"enabled": "true", "ON": "true", "disabled": "false", "off": "false", "inherit": "null", "clear": "null"} {
		got, ok := parseBranchRefreshMode(in)
		if !ok || got != want {
			t.Errorf("%q = %q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := parseBranchRefreshMode("maybe"); ok {
		t.Error("maybe must be refused")
	}
}

func TestBranchRefreshCommVerbs(t *testing.T) {
	if v := BranchRefreshCommVerbs(); len(v) != 2 || v[0] != "branch-refresh" || v[1] != "branch-refresh-stats" {
		t.Fatalf("got %v", v)
	}
}
