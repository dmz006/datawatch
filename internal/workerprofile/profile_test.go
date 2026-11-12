package workerprofile

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuiltinMinimal(t *testing.T) {
	p, ok := Builtin("minimal")
	if !ok {
		t.Fatal("minimal: not found")
	}
	want := WorkerProfile{
		Name:    "minimal",
		MCP:     MCPSelection{Mode: MCPNone},
		Tools:   ToolSelection{Mode: ToolsAllow, Allow: []string{"Read", "Edit", "Write", "Bash", "Glob", "Grep"}},
		Skills:  false,
		Context: ContextMinimal,
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("minimal = %+v, want %+v", p, want)
	}
	if err := Validate(p); err != nil {
		t.Errorf("minimal fails Validate: %v", err)
	}
}

func TestBuiltinDigest(t *testing.T) {
	p, ok := Builtin("digest")
	if !ok {
		t.Fatal("digest: not found")
	}
	want := WorkerProfile{
		Name:    "digest",
		MCP:     MCPSelection{Mode: MCPDatawatch},
		Tools:   ToolSelection{Mode: ToolsAll},
		Skills:  false,
		Context: ContextDigest,
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("digest = %+v, want %+v", p, want)
	}
	if err := Validate(p); err != nil {
		t.Errorf("digest fails Validate: %v", err)
	}
}

func TestBuiltinFull(t *testing.T) {
	p, ok := Builtin("full")
	if !ok {
		t.Fatal("full: not found")
	}
	want := WorkerProfile{
		Name:    "full",
		MCP:     MCPSelection{Mode: MCPDatawatch},
		Tools:   ToolSelection{Mode: ToolsAll},
		Skills:  true,
		Context: ContextFull,
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("full = %+v, want %+v", p, want)
	}
	if err := Validate(p); err != nil {
		t.Errorf("full fails Validate: %v", err)
	}
}

func TestBuiltinDatawatchTools(t *testing.T) {
	p, ok := Builtin("datawatch-tools")
	if !ok {
		t.Fatal("datawatch-tools: not found")
	}
	want := WorkerProfile{
		Name:    "datawatch-tools",
		MCP:     MCPSelection{Mode: MCPDatawatch},
		Tools:   ToolSelection{Mode: ToolsAll},
		Skills:  false,
		Context: ContextDigest,
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("datawatch-tools = %+v, want %+v", p, want)
	}
	if err := Validate(p); err != nil {
		t.Errorf("datawatch-tools fails Validate: %v", err)
	}
}

func TestBuiltinUnknown(t *testing.T) {
	if _, ok := Builtin("nope"); ok {
		t.Error("unknown name reported as built-in")
	}
	if _, ok := Builtin(""); ok {
		t.Error("empty name reported as built-in")
	}
}

func TestBuiltinReturnsIndependentCopy(t *testing.T) {
	p, _ := Builtin("minimal")
	p.Tools.Allow[0] = "Changed"
	again, _ := Builtin("minimal")
	if again.Tools.Allow[0] != "Read" {
		t.Errorf("mutating a returned profile changed the built-in: got %q", again.Tools.Allow[0])
	}

	l := WorkerProfile{Name: "x", MCP: MCPSelection{Mode: MCPList, Servers: []string{"a"}}}
	c := clone(l)
	c.MCP.Servers[0] = "b"
	if l.MCP.Servers[0] != "a" {
		t.Error("clone shares the Servers slice with its source")
	}
	if clone(WorkerProfile{}).MCP.Servers != nil {
		t.Error("clone of nil Servers should stay nil")
	}
	if clone(WorkerProfile{}).Tools.Allow != nil {
		t.Error("clone of nil Allow should stay nil")
	}
}

func TestNamesSorted(t *testing.T) {
	want := []string{"datawatch-tools", "digest", "full", "minimal"}
	got := Names()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
	for _, n := range got {
		if _, ok := Builtin(n); !ok {
			t.Errorf("Names() lists %q but Builtin does not find it", n)
		}
	}
	// Each call returns a fresh slice.
	got[0] = "changed"
	if Names()[0] != "datawatch-tools" {
		t.Error("Names() shares its backing slice between calls")
	}
}

func TestValidateAcceptsValidProfiles(t *testing.T) {
	cases := map[string]WorkerProfile{
		"list mcp": {
			Name:    "a",
			MCP:     MCPSelection{Mode: MCPList, Servers: []string{"x"}},
			Tools:   ToolSelection{Mode: ToolsAll},
			Context: ContextFull,
		},
		"none mcp, allow tools": {
			Name:    "b",
			MCP:     MCPSelection{Mode: MCPNone},
			Tools:   ToolSelection{Mode: ToolsAllow, Allow: []string{"Read"}},
			Context: ContextMinimal,
		},
		"model and effort are not checked": {
			Name:    "c",
			MCP:     MCPSelection{Mode: MCPDatawatch},
			Tools:   ToolSelection{Mode: ToolsAll},
			Context: ContextDigest,
			Model:   "m",
			Effort:  "e",
		},
	}
	for name, p := range cases {
		if err := Validate(p); err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
	}
}

func TestValidateRefusals(t *testing.T) {
	valid := func() WorkerProfile {
		return WorkerProfile{
			Name:    "ok",
			MCP:     MCPSelection{Mode: MCPDatawatch},
			Tools:   ToolSelection{Mode: ToolsAll},
			Context: ContextDigest,
		}
	}
	cases := []struct {
		name    string
		mutate  func(*WorkerProfile)
		wantMsg string
	}{
		{"empty name", func(p *WorkerProfile) { p.Name = "" }, "name is required"},
		{"empty context", func(p *WorkerProfile) { p.Context = "" }, "context must be"},
		{"unknown context", func(p *WorkerProfile) { p.Context = "huge" }, "context must be"},
		{"empty mcp mode", func(p *WorkerProfile) { p.MCP.Mode = "" }, "mcp mode must be"},
		{"unknown mcp mode", func(p *WorkerProfile) { p.MCP.Mode = "all" }, "mcp mode must be"},
		{"list mcp without servers", func(p *WorkerProfile) {
			p.MCP = MCPSelection{Mode: MCPList}
		}, "needs at least one server"},
		{"empty tools mode", func(p *WorkerProfile) { p.Tools.Mode = "" }, "tools mode must be"},
		{"unknown tools mode", func(p *WorkerProfile) { p.Tools.Mode = "some" }, "tools mode must be"},
		{"allow tools without entries", func(p *WorkerProfile) {
			p.Tools = ToolSelection{Mode: ToolsAllow}
		}, "needs at least one entry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := valid()
			tc.mutate(&p)
			err := Validate(p)
			if err == nil {
				t.Fatal("expected a refusal, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

func TestValidateRefusesEveryBuiltinWhenNameCleared(t *testing.T) {
	for _, n := range Names() {
		p, _ := Builtin(n)
		p.Name = ""
		if err := Validate(p); err == nil {
			t.Errorf("%s with empty name: expected refusal", n)
		}
	}
}
