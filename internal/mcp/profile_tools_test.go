package mcp

import (
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// profilePathForKind is the single interesting pure function in
// profile_tools.go; the HTTP handlers are thin proxies covered
// end-to-end by the server package's integration tests.
func TestProfilePathForKind(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"project", "projects", false},
		{"cluster", "clusters", false},
		{"", "", true},
		{"unknown", "", true},
		{"PROJECT", "", true}, // case-sensitive by design
	}
	for _, c := range cases {
		got, err := profilePathForKind(c.in)
		if c.wantErr && err == nil {
			t.Errorf("%q: want error", c.in)
			continue
		}
		if !c.wantErr && err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q → %q, want %q", c.in, got, c.want)
		}
	}
}

// Lock in the required-args list for each tool so accidental schema
// regressions cause an immediate test failure rather than confusing
// the LLM at runtime.
func TestProfileToolDeclarations_HaveRequiredArgs(t *testing.T) {
	s := &Server{}
	cases := []struct {
		name     string
		tool     mcpsdk.Tool
		required []string
	}{
		{"profile_list", s.toolProfileList(), []string{"kind"}},
		{"profile_get", s.toolProfileGet(), []string{"kind", "name"}},
		{"profile_create", s.toolProfileCreate(), []string{"kind", "body"}},
		{"profile_update", s.toolProfileUpdate(), []string{"kind", "name", "body"}},
		{"profile_delete", s.toolProfileDelete(), []string{"kind", "name"}},
		{"profile_smoke", s.toolProfileSmoke(), []string{"kind", "name"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.tool.Name != c.name {
				t.Errorf("tool.Name=%q want %q", c.tool.Name, c.name)
			}
			set := make(map[string]bool)
			for _, r := range c.tool.InputSchema.Required {
				set[r] = true
			}
			for _, r := range c.required {
				if !set[r] {
					t.Errorf("%s: required list %v missing %q", c.name, c.tool.InputSchema.Required, r)
				}
			}
		})
	}
}
