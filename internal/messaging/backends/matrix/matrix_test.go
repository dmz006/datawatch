package matrix

import (
	"context"
	"testing"

	"maunium.net/go/mautrix/id"
)

// ── classify.go ──────────────────────────────────────────────────────────────

func TestClassify(t *testing.T) {
	cases := []struct {
		mxid string
		want Origin
	}{
		{"@alice:matrix.org", OriginNative},
		{"@bot:matrix.org", OriginNative},
		{"@signal_+15555550100:signal.matrix.org", OriginBridgeSignal},
		{"@SIGNAL_+1555:server", OriginBridgeSignal},
		{"@telegram_123456789:t2bot.io", OriginBridgeTelegram},
		{"@whatsapp_15555550100:matrix.org", OriginBridgeWhatsApp},
		{"@slack_U012AB3CD:slack.example.com", OriginBridgeSlack},
		{"@discord_123456789012345678:discord.example.com", OriginBridgeDiscord},
		{"@irc_nick:matrix.org", OriginBridgeIRC},
		{"@_irc_nick:matrix.org", OriginBridgeIRC},
		{"@_puppet_user:matrix.org", OriginBridgeUnknown},
		// MXID without server part
		{"@signal_nodomain", OriginBridgeSignal},
		{"@regular", OriginNative},
	}
	for _, c := range cases {
		got := Classify(c.mxid)
		if got != c.want {
			t.Errorf("Classify(%q) = %v; want %v", c.mxid, got, c.want)
		}
	}
}

func TestIsBridge(t *testing.T) {
	if IsBridge(OriginNative) {
		t.Error("IsBridge(OriginNative) should be false")
	}
	for _, o := range []Origin{OriginBridgeSignal, OriginBridgeTelegram, OriginBridgeWhatsApp, OriginBridgeSlack, OriginBridgeDiscord, OriginBridgeIRC, OriginBridgeUnknown} {
		if !IsBridge(o) {
			t.Errorf("IsBridge(%v) should be true", o)
		}
	}
}

func TestOriginString(t *testing.T) {
	cases := []struct {
		o    Origin
		want string
	}{
		{OriginNative, "native"},
		{OriginBridgeSignal, "signal-bridge"},
		{OriginBridgeTelegram, "telegram-bridge"},
		{OriginBridgeWhatsApp, "whatsapp-bridge"},
		{OriginBridgeSlack, "slack-bridge"},
		{OriginBridgeDiscord, "discord-bridge"},
		{OriginBridgeIRC, "irc-bridge"},
		{OriginBridgeUnknown, "unknown-bridge"},
	}
	for _, c := range cases {
		if got := c.o.String(); got != c.want {
			t.Errorf("Origin(%d).String() = %q; want %q", c.o, got, c.want)
		}
	}
}

// ── render.go ────────────────────────────────────────────────────────────────

func TestNormaliseSender(t *testing.T) {
	cases := []struct {
		mxid string
		want string
	}{
		{"@alice:matrix.org", "@alice:matrix.org"},
		{"@bot:server", "@bot:server"},
		{"@signal_+15555550100:server", "Signal: +15555550100"},
		{"@telegram_123:t2bot.io", "Telegram: 123"},
		{"@whatsapp_15555550100:matrix.org", "WhatsApp: 15555550100"},
		{"@slack_U012AB3CD:server", "Slack: U012AB3CD"},
		{"@discord_123456789:server", "Discord: 123456789"},
		{"@irc_nick:server", "IRC: nick"},
		{"@_irc_nick:server", "IRC: nick"},
		{"@_puppet_user:server", "Bridge: user"},
	}
	for _, c := range cases {
		got := NormaliseSender(c.mxid)
		if got != c.want {
			t.Errorf("NormaliseSender(%q) = %q; want %q", c.mxid, got, c.want)
		}
	}
}

// ── aliases.go ───────────────────────────────────────────────────────────────

func TestIsAlias(t *testing.T) {
	if !isAlias("#room:matrix.org") {
		t.Error("isAlias('#room:matrix.org') should be true")
	}
	if isAlias("!roomid:matrix.org") {
		t.Error("isAlias('!roomid:matrix.org') should be false")
	}
	if isAlias("") {
		t.Error("isAlias('') should be false")
	}
}

func TestAliasResolverPassthrough(t *testing.T) {
	// A room ID (starting with '!') should pass through without a network call.
	r := &aliasResolver{cache: make(map[string]id.RoomID)}
	got, err := r.Resolve(context.Background(), "!abc123:matrix.org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "!abc123:matrix.org" {
		t.Errorf("got %q; want %q", got, "!abc123:matrix.org")
	}
}

func TestAliasResolverCache(t *testing.T) {
	r := &aliasResolver{cache: make(map[string]id.RoomID)}
	// Pre-seed the cache (bypasses network)
	r.cache["#test:matrix.org"] = "!resolved:matrix.org"
	got, err := r.Resolve(context.Background(), "#test:matrix.org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "!resolved:matrix.org" {
		t.Errorf("got %q; want %q", got, "!resolved:matrix.org")
	}
}

func TestAliasResolverInvalidate(t *testing.T) {
	r := &aliasResolver{cache: make(map[string]id.RoomID)}
	r.cache["#test:matrix.org"] = "!old:matrix.org"
	r.Invalidate("#test:matrix.org")
	if _, ok := r.cache["#test:matrix.org"]; ok {
		t.Error("Invalidate did not remove alias from cache")
	}
}
