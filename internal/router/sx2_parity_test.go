// Sprint Sx2 — comm-channel parity parser tests.

package router

import "testing"

func TestSx2_Parse_Cost_Empty(t *testing.T) {
	c := Parse("cost")
	if c.Type != CmdCost || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestSx2_Parse_Cost_WithSession(t *testing.T) {
	c := Parse("cost host-aa01")
	if c.Type != CmdCost || c.Text != "host-aa01" {
		t.Errorf("got %+v", c)
	}
}

func TestSx2_Parse_Stale_Default(t *testing.T) {
	c := Parse("stale")
	if c.Type != CmdStale || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestSx2_Parse_Stale_WithSeconds(t *testing.T) {
	c := Parse("stale 600")
	if c.Type != CmdStale || c.Text != "600" {
		t.Errorf("got %+v", c)
	}
}

func TestSx2_Parse_Audit_Empty(t *testing.T) {
	c := Parse("audit")
	if c.Type != CmdAudit || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestSx2_Parse_Audit_WithFilters(t *testing.T) {
	c := Parse("audit actor=operator action=start limit=20")
	if c.Type != CmdAudit {
		t.Fatalf("type=%v", c.Type)
	}
	if c.Text != "actor=operator action=start limit=20" {
		t.Errorf("text=%q", c.Text)
	}
}

func TestSx2_Parse_Cooldown_Status(t *testing.T) {
	c := Parse("cooldown")
	if c.Type != CmdCooldown || c.CooldownVerb != "status" {
		t.Errorf("got %+v", c)
	}
	c2 := Parse("cooldown status")
	if c2.Type != CmdCooldown || c2.CooldownVerb != "status" {
		t.Errorf("got %+v", c2)
	}
}

func TestSx2_Parse_Cooldown_Set(t *testing.T) {
	c := Parse("cooldown set 60 anthropic 429")
	if c.Type != CmdCooldown {
		t.Fatalf("type=%v", c.Type)
	}
	if c.CooldownVerb != "set" {
		t.Errorf("verb=%q", c.CooldownVerb)
	}
	if c.CooldownSeconds != 60 {
		t.Errorf("seconds=%d", c.CooldownSeconds)
	}
	if c.CooldownReason != "anthropic 429" {
		t.Errorf("reason=%q", c.CooldownReason)
	}
}

func TestSx2_Parse_Cooldown_Clear(t *testing.T) {
	c := Parse("cooldown clear")
	if c.Type != CmdCooldown || c.CooldownVerb != "clear" {
		t.Errorf("got %+v", c)
	}
}

func TestSx2_Parse_Rest_GET(t *testing.T) {
	c := Parse("rest GET /api/cost")
	if c.Type != CmdRest {
		t.Fatalf("type=%v", c.Type)
	}
	if c.RestMethod != "GET" {
		t.Errorf("method=%q", c.RestMethod)
	}
	if c.RestPath != "/api/cost" {
		t.Errorf("path=%q", c.RestPath)
	}
	if c.RestBody != "" {
		t.Errorf("body should be empty: %q", c.RestBody)
	}
}

func TestSx2_Parse_Rest_POST_WithBody(t *testing.T) {
	c := Parse(`rest POST /api/projects {"name":"foo","dir":"/tmp"}`)
	if c.Type != CmdRest {
		t.Fatalf("type=%v", c.Type)
	}
	if c.RestMethod != "POST" || c.RestPath != "/api/projects" {
		t.Errorf("got %+v", c)
	}
	if c.RestBody != `{"name":"foo","dir":"/tmp"}` {
		t.Errorf("body=%q", c.RestBody)
	}
}

func TestSx2_Parse_Rest_Malformed(t *testing.T) {
	if c := Parse("rest"); c.Type != CmdUnknown {
		t.Errorf("missing args should be unknown: %+v", c)
	}
	if c := Parse("rest GET"); c.Type != CmdUnknown {
		t.Errorf("missing path should be unknown: %+v", c)
	}
}

func TestSx2_Router_SetWebPort(t *testing.T) {
	r := &Router{}
	r.SetWebPort(8080)
	if r.webPort != 8080 {
		t.Errorf("got %d want 8080", r.webPort)
	}
}

func TestSx2_Router_LoopbackUnconfigured(t *testing.T) {
	r := &Router{}
	if _, err := r.commGet("/api/cost", nil); err == nil {
		t.Error("expected loopback-not-configured error")
	}
	if _, err := r.commJSON("POST", "/api/cooldown", `{}`); err == nil {
		t.Error("expected loopback-not-configured error")
	}
}
