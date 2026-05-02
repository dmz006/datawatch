// BL220 — parser and loopback-safety tests for the 9 new comm commands.

package router

import (
	"strings"
	"testing"
)

// ── Parser tests ──────────────────────────────────────────────────────────────

func TestBL220_Parse_Orchestrator_Bare(t *testing.T) {
	c := Parse("orchestrator")
	if c.Type != CmdOrchestrator || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Orchestrator_Config(t *testing.T) {
	c := Parse("orchestrator config")
	if c.Type != CmdOrchestrator || c.Text != "config" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Orchestrator_Get(t *testing.T) {
	c := Parse("orchestrator get abc123")
	if c.Type != CmdOrchestrator || c.Text != "get abc123" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Orchestrator_Run(t *testing.T) {
	c := Parse("orchestrator run abc123")
	if c.Type != CmdOrchestrator || c.Text != "run abc123" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Orchestrator_Verdicts(t *testing.T) {
	c := Parse("orchestrator verdicts")
	if c.Type != CmdOrchestrator || c.Text != "verdicts" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Plugins_Bare(t *testing.T) {
	c := Parse("plugins")
	if c.Type != CmdPlugins || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Plugins_Enable(t *testing.T) {
	c := Parse("plugins enable myplugin")
	if c.Type != CmdPlugins || c.Text != "enable myplugin" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Plugins_Disable(t *testing.T) {
	c := Parse("plugins disable myplugin")
	if c.Type != CmdPlugins || c.Text != "disable myplugin" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Plugins_Reload(t *testing.T) {
	c := Parse("plugins reload")
	if c.Type != CmdPlugins || c.Text != "reload" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Plugins_Test(t *testing.T) {
	c := Parse("plugins test myplugin on_session_start")
	if c.Type != CmdPlugins || c.Text != "test myplugin on_session_start" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Templates_Bare(t *testing.T) {
	c := Parse("templates")
	if c.Type != CmdTemplates || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Templates_Get(t *testing.T) {
	c := Parse("templates get my-template")
	if c.Type != CmdTemplates || c.Text != "get my-template" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Templates_Delete(t *testing.T) {
	c := Parse("templates delete my-template")
	if c.Type != CmdTemplates || c.Text != "delete my-template" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Routing_Bare(t *testing.T) {
	c := Parse("routing")
	if c.Type != CmdRouting || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Routing_Test(t *testing.T) {
	c := Parse("routing test fix the login bug")
	if c.Type != CmdRouting || c.Text != "test fix the login bug" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_DeviceAlias_Bare(t *testing.T) {
	c := Parse("device-alias")
	if c.Type != CmdDeviceAlias || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_DeviceAlias_Add(t *testing.T) {
	c := Parse("device-alias add ring-laptop prod-server")
	if c.Type != CmdDeviceAlias || c.Text != "add ring-laptop prod-server" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_DeviceAlias_Delete(t *testing.T) {
	c := Parse("device-alias delete ring-laptop")
	if c.Type != CmdDeviceAlias || c.Text != "delete ring-laptop" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Splash(t *testing.T) {
	c := Parse("splash")
	if c.Type != CmdSplash {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Detection(t *testing.T) {
	c := Parse("detection")
	if c.Type != CmdDetection {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Detection_WithArg(t *testing.T) {
	c := Parse("detection status")
	if c.Type != CmdDetection {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Observer_Bare(t *testing.T) {
	c := Parse("observer")
	if c.Type != CmdObserver || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Observer_Stats(t *testing.T) {
	c := Parse("observer stats")
	if c.Type != CmdObserver || c.Text != "stats" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Observer_Envelopes(t *testing.T) {
	c := Parse("observer envelopes")
	if c.Type != CmdObserver || c.Text != "envelopes" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Observer_EnvelopesAllPeers(t *testing.T) {
	c := Parse("observer envelopes all-peers")
	if c.Type != CmdObserver || c.Text != "envelopes all-peers" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Observer_Config(t *testing.T) {
	c := Parse("observer config")
	if c.Type != CmdObserver || c.Text != "config" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Analytics_Bare(t *testing.T) {
	c := Parse("analytics")
	if c.Type != CmdAnalytics || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestBL220_Parse_Analytics_WithRange(t *testing.T) {
	c := Parse("analytics 30d")
	if c.Type != CmdAnalytics || c.Text != "30d" {
		t.Errorf("got %+v", c)
	}
}

// ── Loopback-unconfigured safety ──────────────────────────────────────────────
// Each new handler must return an error reply, not panic, when webPort==0.

func newBL220Router(t *testing.T) *Router {
	t.Helper()
	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	// webPort stays 0 — triggers "loopback not configured"
	return r
}

func assertFailReply(t *testing.T, replies []string) {
	t.Helper()
	if len(replies) == 0 {
		t.Error("expected at least one reply, got none")
		return
	}
	for _, rep := range replies {
		if strings.Contains(rep, "failed") || strings.Contains(rep, "loopback") {
			return
		}
	}
	t.Errorf("expected failure reply, got: %v", replies)
}

func TestBL220_Loopback_Orchestrator(t *testing.T) {
	for _, msg := range []string{"orchestrator", "orchestrator config", "orchestrator get x", "orchestrator run x"} {
		r := newBL220Router(t)
		assertFailReply(t, r.HandleTestMessage(msg))
	}
}

func TestBL220_Loopback_Plugins(t *testing.T) {
	for _, msg := range []string{"plugins", "plugins enable x", "plugins disable x", "plugins reload"} {
		r := newBL220Router(t)
		assertFailReply(t, r.HandleTestMessage(msg))
	}
}

func TestBL220_Loopback_Templates(t *testing.T) {
	for _, msg := range []string{"templates", "templates get x", "templates delete x"} {
		r := newBL220Router(t)
		assertFailReply(t, r.HandleTestMessage(msg))
	}
}

func TestBL220_Loopback_Routing(t *testing.T) {
	for _, msg := range []string{"routing", "routing test some task"} {
		r := newBL220Router(t)
		assertFailReply(t, r.HandleTestMessage(msg))
	}
}

func TestBL220_Loopback_DeviceAlias(t *testing.T) {
	for _, msg := range []string{"device-alias", "device-alias add a b", "device-alias delete a"} {
		r := newBL220Router(t)
		assertFailReply(t, r.HandleTestMessage(msg))
	}
}

func TestBL220_Loopback_Splash(t *testing.T) {
	r := newBL220Router(t)
	assertFailReply(t, r.HandleTestMessage("splash"))
}

func TestBL220_Loopback_Detection(t *testing.T) {
	r := newBL220Router(t)
	assertFailReply(t, r.HandleTestMessage("detection"))
}

func TestBL220_Loopback_Observer(t *testing.T) {
	for _, msg := range []string{"observer", "observer stats", "observer config", "observer envelopes", "observer envelopes all-peers"} {
		r := newBL220Router(t)
		assertFailReply(t, r.HandleTestMessage(msg))
	}
}

func TestBL220_Loopback_Analytics(t *testing.T) {
	for _, msg := range []string{"analytics", "analytics 30d"} {
		r := newBL220Router(t)
		assertFailReply(t, r.HandleTestMessage(msg))
	}
}
