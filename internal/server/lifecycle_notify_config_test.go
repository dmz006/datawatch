// Config surface tests for the outbound webhook sink and peer forwarding
// (autonomous.notify.webhook.* and autonomous.notify.forwarding.*): the REST
// round trip, the rejection rules, the live effect with no restart, and a
// surface-parity check that every key is reachable and named on each operator
// surface (REST, MCP config_set, CLI config set, the comm configure verb, the
// Settings card, all five locale bundles, config-reference.yaml and
// implementation.md).
//
// The defaults and the wire decisions are in
// docs/plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/router"
)

// lifecycleNotifyWebhookFields and lifecycleNotifyForwardingFields are the
// config keys under each section. The full key is "autonomous.notify.<section>.<field>".
var (
	lifecycleNotifyWebhookFields = []string{
		"enabled", "allow_hosts", "max_attempts", "backoff_seconds", "jitter_percent",
		"attempt_timeout_seconds", "max_body_bytes", "signature_tolerance_seconds",
	}
	lifecycleNotifyForwardingFields = []string{
		"enabled", "max_hops", "max_attempts", "backoff_seconds", "jitter_percent",
		"attempt_timeout_seconds",
	}
)

// lifecycleNotifySample is one valid value per full key, in the form an
// operator's surface sends: a JSON value on REST, and a quoted string on the
// comm channel and the CLI. The list keys take both forms in the round trip.
var lifecycleNotifySample = map[string]any{
	"autonomous.notify.webhook.enabled":                     true,
	"autonomous.notify.webhook.allow_hosts":                 "hooks.example.net, hooks.example.net:8443",
	"autonomous.notify.webhook.max_attempts":                4,
	"autonomous.notify.webhook.backoff_seconds":             []int{2, 8, 32},
	"autonomous.notify.webhook.jitter_percent":              10,
	"autonomous.notify.webhook.attempt_timeout_seconds":     6,
	"autonomous.notify.webhook.max_body_bytes":              32768,
	"autonomous.notify.webhook.signature_tolerance_seconds": 90,
	"autonomous.notify.forwarding.enabled":                  true,
	"autonomous.notify.forwarding.max_hops":                 2,
	"autonomous.notify.forwarding.max_attempts":             3,
	"autonomous.notify.forwarding.backoff_seconds":          "5,10",
	"autonomous.notify.forwarding.jitter_percent":           25,
	"autonomous.notify.forwarding.attempt_timeout_seconds":  7,
}

// lifecycleNotifyWant is what GET /api/config must read back for each sample.
var lifecycleNotifyWant = map[string]string{
	"autonomous.notify.webhook.enabled":                     `true`,
	"autonomous.notify.webhook.allow_hosts":                 `["hooks.example.net","hooks.example.net:8443"]`,
	"autonomous.notify.webhook.max_attempts":                `4`,
	"autonomous.notify.webhook.backoff_seconds":             `[2,8,32]`,
	"autonomous.notify.webhook.jitter_percent":              `10`,
	"autonomous.notify.webhook.attempt_timeout_seconds":     `6`,
	"autonomous.notify.webhook.max_body_bytes":              `32768`,
	"autonomous.notify.webhook.signature_tolerance_seconds": `90`,
	"autonomous.notify.forwarding.enabled":                  `true`,
	"autonomous.notify.forwarding.max_hops":                 `2`,
	"autonomous.notify.forwarding.max_attempts":             `3`,
	"autonomous.notify.forwarding.backoff_seconds":          `[5,10]`,
	"autonomous.notify.forwarding.jitter_percent":           `25`,
	"autonomous.notify.forwarding.attempt_timeout_seconds":  `7`,
}

// lifecycleNotifyAllKeys lists every full key, webhook first, in the order above.
func lifecycleNotifyAllKeys() []string {
	var out []string
	for _, f := range lifecycleNotifyWebhookFields {
		out = append(out, "autonomous.notify.webhook."+f)
	}
	for _, f := range lifecycleNotifyForwardingFields {
		out = append(out, "autonomous.notify.forwarding."+f)
	}
	return out
}

// lifecycleNotifySection decodes the autonomous.notify GET surface into its two
// sections. Each section maps a field name to its raw JSON value.
func lifecycleNotifySection(t *testing.T, s *Server) (webhook, forwarding map[string]json.RawMessage) {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["notify"]
	if !ok {
		t.Fatalf("GET /api/config autonomous lacks notify")
	}
	var notify struct {
		Webhook    map[string]json.RawMessage `json:"webhook"`
		Forwarding map[string]json.RawMessage `json:"forwarding"`
	}
	if err := json.Unmarshal(raw, &notify); err != nil {
		t.Fatalf("decode notify: %v", err)
	}
	return notify.Webhook, notify.Forwarding
}

// lifecycleNotifyGetField reads one full key from the GET surface as compact JSON.
func lifecycleNotifyGetField(t *testing.T, s *Server, fullKey string) string {
	t.Helper()
	webhook, forwarding := lifecycleNotifySection(t, s)
	field := fullKey[strings.LastIndex(fullKey, ".")+1:]
	section := webhook
	if strings.Contains(fullKey, ".forwarding.") {
		section = forwarding
	}
	v, ok := section[field]
	if !ok {
		t.Fatalf("GET /api/config notify lacks %s", fullKey)
	}
	return string(v)
}

func TestLifecycleNotifyConfig_PutGetRoundTrip(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", lifecycleNotifySample)
	for _, key := range lifecycleNotifyAllKeys() {
		if got := lifecycleNotifyGetField(t, s, key); got != lifecycleNotifyWant[key] {
			t.Errorf("GET %s after PUT = %s, want %s", key, got, lifecycleNotifyWant[key])
		}
	}
	// The live config holds the same values the GET reports.
	n := s.cfg.Autonomous.Notify
	if !n.Webhook.Enabled || n.Webhook.MaxAttempts != 4 || len(n.Webhook.AllowHosts) != 2 ||
		n.Webhook.AllowHosts[1] != "hooks.example.net:8443" {
		t.Fatalf("live webhook config = %+v", n.Webhook)
	}
	if !n.Forwarding.Enabled || n.Forwarding.MaxHops != 2 || len(n.Forwarding.BackoffSeconds) != 2 {
		t.Fatalf("live forwarding config = %+v", n.Forwarding)
	}
}

func TestLifecycleNotifyConfig_ExplicitFalseAndEmptyListRoundTrip(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.notify.webhook.enabled":     true,
		"autonomous.notify.webhook.allow_hosts": "hooks.example.net",
	})
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.notify.webhook.enabled":     false,
		"autonomous.notify.webhook.allow_hosts": []string{},
	})
	if got := lifecycleNotifyGetField(t, s, "autonomous.notify.webhook.enabled"); got != "false" {
		t.Errorf("explicit false read back as %s", got)
	}
	if got := lifecycleNotifyGetField(t, s, "autonomous.notify.webhook.allow_hosts"); got != "[]" {
		t.Errorf("an explicit empty allow-list read back as %s, want []", got)
	}
}

func TestLifecycleNotifyConfig_ZeroAndNegativeReadBackAsDefaults(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.notify.webhook.max_attempts":       0,
		"autonomous.notify.webhook.backoff_seconds":    []int{},
		"autonomous.notify.webhook.max_body_bytes":     -3,
		"autonomous.notify.forwarding.max_hops":        0,
		"autonomous.notify.forwarding.jitter_percent":  0,
		"autonomous.notify.forwarding.backoff_seconds": "",
	})
	checks := map[string]string{
		"autonomous.notify.webhook.max_attempts":       `5`,
		"autonomous.notify.webhook.backoff_seconds":    `[1,4,16,64]`,
		"autonomous.notify.webhook.max_body_bytes":     `65536`,
		"autonomous.notify.forwarding.max_hops":        `3`,
		"autonomous.notify.forwarding.jitter_percent":  `20`,
		"autonomous.notify.forwarding.backoff_seconds": `[1,4,16,64]`,
	}
	for key, want := range checks {
		if got := lifecycleNotifyGetField(t, s, key); got != want {
			t.Errorf("GET %s = %s, want the default %s", key, got, want)
		}
	}
}

func TestLifecycleNotifyConfig_RejectsInvalidValuesWithoutPartialWrite(t *testing.T) {
	cases := []struct {
		name string
		key  string
		val  any
		msg  string
	}{
		{"allow-list with a scheme", "autonomous.notify.webhook.allow_hosts", "https://hooks.example.net", "scheme"},
		{"allow-list with a path", "autonomous.notify.webhook.allow_hosts", []string{"hooks.example.net/hook"}, "path"},
		{"allow-list with a wildcard", "autonomous.notify.webhook.allow_hosts", "*.example.net", "wildcard"},
		{"allow-list with userinfo", "autonomous.notify.webhook.allow_hosts", []string{"user@hooks.example.net"}, "userinfo"},
		{"allow-list with a bad port", "autonomous.notify.webhook.allow_hosts", "hooks.example.net:99999", "not a valid"},
		{"allow-list not a list", "autonomous.notify.webhook.allow_hosts", 42, "list of hosts"},
		{"max_attempts non-numeric", "autonomous.notify.webhook.max_attempts", "abc", "whole number"},
		{"max_attempts fractional", "autonomous.notify.webhook.max_attempts", 2.5, "whole number"},
		{"jitter above 100", "autonomous.notify.webhook.jitter_percent", 150, "at most 100"},
		{"backoff non-numeric entry", "autonomous.notify.webhook.backoff_seconds", "1,x,4", "whole number"},
		{"backoff zero entry", "autonomous.notify.webhook.backoff_seconds", []int{0, 4}, "at least 1"},
		{"backoff fractional entry", "autonomous.notify.webhook.backoff_seconds", []float64{1.5}, "whole number"},
		{"backoff is a bare number", "autonomous.notify.webhook.backoff_seconds", 5, "list of whole seconds"},
		{"max_body_bytes beyond int32", "autonomous.notify.webhook.max_body_bytes", 1e12, "whole number"},
		{"max_body_bytes boolean", "autonomous.notify.webhook.max_body_bytes", true, "whole number"},
		{"forwarding hops non-numeric", "autonomous.notify.forwarding.max_hops", "many", "whole number"},
		{"forwarding jitter above 100", "autonomous.notify.forwarding.jitter_percent", 101, "at most 100"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			// A valid enabled=true rides in the same PUT: a rejected PUT must not apply it.
			b, _ := json.Marshal(map[string]any{
				"autonomous.notify.webhook.enabled": true,
				c.key:                               c.val,
			})
			rr := httptest.NewRecorder()
			s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("PUT %s: want 400, got %d body=%s", b, rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), c.msg) {
				t.Errorf("error %q does not mention %q", rr.Body.String(), c.msg)
			}
			if got := lifecycleNotifyGetField(t, s, "autonomous.notify.webhook.enabled"); got != "false" {
				t.Errorf("a rejected PUT still applied enabled: %s", got)
			}
		})
	}
}

// The number reader accepts a JSON number, a Go int and a numeric string, and
// refuses everything else, including a value past the 32-bit range.
func TestLifecycleNotifyConfig_NumberReader(t *testing.T) {
	ok := map[string]any{"7": 7, "float": float64(9), "int": 11, "padded": " 13 ", "zero": "0"}
	want := map[string]int{"7": 7, "float": 9, "int": 11, "padded": 13, "zero": 0}
	for name, in := range ok {
		got, err := notifyInt(in)
		if err != nil || got != want[name] {
			t.Errorf("notifyInt(%s) = %d, %v; want %d", name, got, err, want[name])
		}
	}
	for _, in := range []any{"2.5", "", float64(1 << 40), 1.5, true, nil, []int{1}} {
		if got, err := notifyInt(in); err == nil {
			t.Errorf("notifyInt(%v) = %d, want an error", in, got)
		}
	}
}

// The sink reads its settings through the same closure main.go builds, so a
// PUT changes the next delivery with no restart.
func TestLifecycleNotifyConfig_LiveEffectWithoutRestart(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	var sig string
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		sig = r.Header.Get(autonomous.WebhookSignatureHeader)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer recv.Close()
	u, err := url.Parse(recv.URL)
	if err != nil {
		t.Fatalf("parse receiver URL: %v", err)
	}
	hostPort := u.Host // 127.0.0.1:port, an explicit allow-list entry

	s, _, _ := newMaxConcurrentTasksServer(t)
	sink := autonomous.NewWebhookSink(autonomous.WebhookSinkConfig{
		Settings: func() autonomous.WebhookSettings {
			return autonomous.WebhookSettingsFromConfig(s.cfg.Autonomous.Notify)
		},
		ResolveSecret: func(ref string) (string, error) { return "fake-test-signing-value", nil },
	})
	sub := autonomous.Subscription{
		ID: "sub-live",
		Sink: autonomous.SubscriptionSink{
			Type:      "webhook",
			Target:    recv.URL + "/hook",
			SecretRef: "${secret:test-live-hook}",
		},
	}
	ev := autonomous.LifecycleEvent{
		ID: "ev-live", Kind: "prd.completed", Level: autonomous.LevelPRD,
		PRDID: "prd-live", NewStatus: "completed", Host: "test-host",
	}
	hitCount := func() int {
		mu.Lock()
		defer mu.Unlock()
		return hits
	}
	deliver := func() {
		_ = sink.Deliver(context.Background(), sub, ev) // a refusal is not a test error here
	}

	// Disabled by default: nothing is sent.
	deliver()
	if n := hitCount(); n != 0 {
		t.Fatalf("a disabled sink sent %d request(s)", n)
	}

	// Enable and allow the receiver: the next delivery arrives, signed.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.notify.webhook.enabled":     true,
		"autonomous.notify.webhook.allow_hosts": hostPort,
	})
	if got := autonomous.WebhookSettingsFromConfig(s.cfg.Autonomous.Notify).AllowHosts; len(got) != 1 || got[0] != hostPort {
		t.Fatalf("the sink's live settings do not show the new host: %v", got)
	}
	deliver()
	if n := hitCount(); n != 1 {
		t.Fatalf("after enabling, the receiver got %d request(s), want 1", n)
	}
	mu.Lock()
	gotSig := sig
	mu.Unlock()
	if !strings.HasPrefix(gotSig, "t=") || !strings.Contains(gotSig, ",v1=") {
		t.Fatalf("delivered request signature header = %q", gotSig)
	}

	// Empty the allow-list: the host is no longer allowed, so nothing is sent.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.notify.webhook.allow_hosts": []string{},
	})
	deliver()
	if n := hitCount(); n != 1 {
		t.Fatalf("after clearing the allow-list, the receiver got %d request(s), want still 1", n)
	}
}

// lifecycleNotifyParityLocaleKeys are the locale strings the Settings card
// reads for the new fields (labelKey and helpKey). Every one must exist, be
// non-empty and carry no tracker id, in all five bundles.
var lifecycleNotifyParityLocaleKeys = []string{
	"settings_notify_webhook_enabled", "settings_notify_webhook_enabled_help",
	"settings_notify_webhook_allow_hosts", "settings_notify_webhook_allow_hosts_help",
	"settings_notify_webhook_max_attempts", "settings_notify_webhook_backoff_seconds",
	"settings_notify_webhook_jitter_percent", "settings_notify_webhook_attempt_timeout",
	"settings_notify_webhook_max_body_bytes", "settings_notify_webhook_signature_tolerance",
	"settings_notify_forwarding_enabled", "settings_notify_forwarding_enabled_help",
	"settings_notify_forwarding_max_hops", "settings_notify_forwarding_max_attempts",
	"settings_notify_forwarding_backoff_seconds", "settings_notify_forwarding_jitter_percent",
	"settings_notify_forwarding_attempt_timeout",
}

// TestLifecycleNotifyConfig_SurfacesParity proves each key is reachable and
// consistent on every operator surface. REST is exercised for real (PUT then
// GET); the other surfaces are pass-throughs over the same REST contract, so
// their source must name each key or the path they forward to.
func TestLifecycleNotifyConfig_SurfacesParity(t *testing.T) {
	keys := lifecycleNotifyAllKeys()

	t.Run("REST config: every key settable and readable", func(t *testing.T) {
		s, _, _ := newMaxConcurrentTasksServer(t)
		putJSON(t, s.handlePutConfig, "/api/config", lifecycleNotifySample)
		for _, key := range keys {
			if got := lifecycleNotifyGetField(t, s, key); got != lifecycleNotifyWant[key] {
				t.Errorf("REST %s = %s, want %s", key, got, lifecycleNotifyWant[key])
			}
		}
		// The PUT switch names every key literally.
		readRel(t, "internal/server/api.go", keys...)
	})

	t.Run("comm configure verb: every key parses, and help lists it", func(t *testing.T) {
		for _, key := range keys {
			text := "configure " + key + "=true"
			cmd := router.Parse(text)
			if cmd.Type != router.CmdConfigure {
				t.Fatalf("Parse(%q) type = %v, want configure", text, cmd.Type)
			}
			if cmd.Text != key+"=true" {
				t.Fatalf("Parse(%q) text = %q, want %q", text, cmd.Text, key+"=true")
			}
		}
		readRel(t, "internal/router/router.go", keys...)
		// The daemon wires comm configure to PUT /api/config.
		readRel(t, "cmd/datawatch/main.go", "SetConfigureFunc", "/api/config")
	})

	t.Run("MCP config_set: generic pass-through to PUT /api/config", func(t *testing.T) {
		readRel(t, "internal/mcp/memory_tools.go", `"config_set"`, `"/api/config"`)
	})

	t.Run("CLI config set: generic pass-through to PUT /api/config", func(t *testing.T) {
		readRel(t, "cmd/datawatch/main.go", "newConfigSetCmd", "runConfigSet", `"/api/config"`)
	})

	t.Run("Settings card: every key has a field", func(t *testing.T) {
		readRel(t, "internal/server/web/app.js", keys...)
		readRel(t, "internal/server/web/app.js", lifecycleNotifyParityLocaleKeys...)
	})

	t.Run("locale bundles: every label carries all five locales", func(t *testing.T) {
		for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
			raw := readRepoFile(t, "internal/server/web/locales/"+lang+".json")
			var m map[string]any
			if err := json.Unmarshal([]byte(raw), &m); err != nil {
				t.Fatalf("%s.json: invalid JSON: %v", lang, err)
			}
			for _, k := range lifecycleNotifyParityLocaleKeys {
				v, _ := m[k].(string)
				if strings.TrimSpace(v) == "" {
					t.Errorf("%s.json lacks a non-empty %s", lang, k)
				}
				if strings.Contains(v, "PRD") {
					t.Errorf("%s.json %s uses the internal term PRD: %q", lang, k, v)
				}
			}
		}
	})

	t.Run("config-reference.yaml names every key", func(t *testing.T) {
		readRel(t, "docs/config-reference.yaml", keys...)
	})

	t.Run("implementation.md names every key", func(t *testing.T) {
		readRel(t, "docs/implementation.md", keys...)
	})
}
