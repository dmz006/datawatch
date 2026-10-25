package autonomous

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func TestLifecycleNotifySettingsFromConfigMapsEveryField(t *testing.T) {
	n := config.AutonomousNotifyConfig{
		Webhook: config.AutonomousNotifyWebhookConfig{
			Enabled:                   true,
			AllowHosts:                []string{"hooks.example.net", "hooks.example.net:8443"},
			MaxAttempts:               7,
			BackoffSeconds:            []int{2, 6},
			JitterPercent:             30,
			AttemptTimeoutSeconds:     12,
			MaxBodyBytes:              8192,
			SignatureToleranceSeconds: 120,
		},
		Forwarding: config.AutonomousNotifyForwardingConfig{
			Enabled:               true,
			MaxHops:               4,
			MaxAttempts:           3,
			BackoffSeconds:        []int{5},
			JitterPercent:         15,
			AttemptTimeoutSeconds: 9,
		},
	}
	w := WebhookSettingsFromConfig(n)
	if !w.Enabled || w.MaxAttempts != 7 || w.JitterPercent != 30 || w.MaxBodyBytes != 8192 {
		t.Fatalf("webhook scalar fields = %+v", w)
	}
	if w.Timeout != 12*time.Second || w.SignatureTolerance != 120*time.Second {
		t.Fatalf("webhook durations: timeout=%v tolerance=%v", w.Timeout, w.SignatureTolerance)
	}
	if len(w.Backoff) != 2 || w.Backoff[0] != 2*time.Second || w.Backoff[1] != 6*time.Second {
		t.Fatalf("webhook backoff = %v", w.Backoff)
	}
	if len(w.AllowHosts) != 2 || w.AllowHosts[1] != "hooks.example.net:8443" {
		t.Fatalf("webhook allow-list = %v", w.AllowHosts)
	}
	f := ForwardSettingsFromConfig(n)
	if !f.Enabled || f.MaxHops != 4 || f.MaxAttempts != 3 || f.JitterPercent != 15 {
		t.Fatalf("forwarding fields = %+v", f)
	}
	if f.Timeout != 9*time.Second || len(f.Backoff) != 1 || f.Backoff[0] != 5*time.Second {
		t.Fatalf("forwarding timeout/backoff = %v / %v", f.Timeout, f.Backoff)
	}
	// The envelope cap is the webhook sink's, so a forwarded envelope obeys it.
	if f.MaxBodyBytes != 8192 {
		t.Fatalf("forwarding envelope cap = %d, want the webhook value 8192", f.MaxBodyBytes)
	}
}

func TestLifecycleNotifySettingsFromConfigZeroValuesAreDefaults(t *testing.T) {
	w := WebhookSettingsFromConfig(config.AutonomousNotifyConfig{})
	if w.Enabled {
		t.Fatalf("the webhook sink must ship disabled")
	}
	if w.MaxAttempts != config.DefaultNotifyMaxAttempts || w.JitterPercent != config.DefaultNotifyJitterPercent ||
		w.Timeout != config.DefaultNotifyAttemptTimeoutSeconds*time.Second ||
		w.MaxBodyBytes != config.DefaultNotifyMaxBodyBytes ||
		w.SignatureTolerance != config.DefaultNotifySignatureToleranceSeconds*time.Second {
		t.Fatalf("zero config did not read as defaults: %+v", w)
	}
	if len(w.Backoff) != 4 || w.Backoff[0] != time.Second || w.Backoff[3] != 64*time.Second {
		t.Fatalf("default backoff = %v", w.Backoff)
	}
	f := ForwardSettingsFromConfig(config.AutonomousNotifyConfig{})
	if f.Enabled {
		t.Fatalf("forwarding must ship disabled")
	}
	if f.MaxHops != config.DefaultNotifyMaxHops || f.MaxAttempts != config.DefaultNotifyMaxAttempts ||
		f.JitterPercent != config.DefaultNotifyJitterPercent || f.Timeout != DefaultWebhookTimeout ||
		f.MaxBodyBytes != config.DefaultNotifyMaxBodyBytes {
		t.Fatalf("zero forwarding config did not read as defaults: %+v", f)
	}
}

func TestLifecycleNotifySettingsAllowListIsCopied(t *testing.T) {
	n := config.AutonomousNotifyConfig{}
	n.Webhook.AllowHosts = []string{"hooks.example.net"}
	w := WebhookSettingsFromConfig(n)
	n.Webhook.AllowHosts[0] = "changed.example.net"
	if w.AllowHosts[0] != "hooks.example.net" {
		t.Fatalf("a later config write changed a settings snapshot: %v", w.AllowHosts)
	}
}

func TestValidateWebhookAllowHostAccepts(t *testing.T) {
	for _, ok := range []string{
		"hooks.example.net",
		"hooks.example.net:8443",
		"HOOKS.example.net",
		"10.0.0.5",
		"10.0.0.5:9000",
		"[2001:db8::1]:443",
		"  hooks.example.net  ",
	} {
		if err := ValidateWebhookAllowHost(ok); err != nil {
			t.Errorf("ValidateWebhookAllowHost(%q) = %v, want nil", ok, err)
		}
	}
}

func TestValidateWebhookAllowHostRejects(t *testing.T) {
	cases := map[string]string{
		"":                            "empty",
		"https://hooks.example.net":   "scheme",
		"http://hooks.example.net:80": "scheme",
		"hooks.example.net/hook":      "path or query",
		"hooks.example.net?x=1":       "path or query",
		"hooks.example.net#frag":      "path or query",
		"*.example.net":               "wildcard",
		"hooks.*":                     "wildcard",
		"user:pass@hooks.example.net": "userinfo",
		"user@hooks.example.net":      "userinfo",
		"hooks.example.net:99999":     "not a valid",
		"hooks.example.net:0":         "not a valid",
		"hooks.example.net:":          "not a valid",
		"hooks example.net":           "not a valid",
		"hooks.example.net:abc":       "not a valid",
		"0x7f.1":                      "not a valid",
	}
	for in, wantWord := range cases {
		err := ValidateWebhookAllowHost(in)
		if err == nil {
			t.Errorf("ValidateWebhookAllowHost(%q) = nil, want an error", in)
			continue
		}
		if !strings.Contains(err.Error(), wantWord) {
			t.Errorf("ValidateWebhookAllowHost(%q) = %q, want it to mention %q", in, err, wantWord)
		}
	}
}

// The validator and the matcher share one parse: every entry the validator
// accepts is one the matcher can match.
func TestValidateWebhookAllowHostMatchesWhatItAccepts(t *testing.T) {
	entry := "hooks.example.net:8443"
	if err := ValidateWebhookAllowHost(entry); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !WebhookHostAllowed("hooks.example.net:8443", []string{entry}) {
		t.Fatalf("accepted entry %q does not match its own host:port", entry)
	}
	if WebhookHostAllowed("hooks.example.net:9443", []string{entry}) {
		t.Fatalf("a host:port entry matched another port")
	}
}

func TestWebhookBackoffExplicitScheduleRepeatsLastEntry(t *testing.T) {
	c := retryCurve{
		schedule:      []time.Duration{1 * time.Second, 4 * time.Second, 16 * time.Second, 64 * time.Second},
		jitterPercent: 20,
	}
	want := []time.Duration{1, 4, 16, 64, 64, 64}
	for n := 1; n <= len(want); n++ {
		got := webhookBackoff(c, n, 0, 0.5) // jitter 0.5 is no spread
		if got != want[n-1]*time.Second {
			t.Errorf("attempt %d delay = %v, want %v", n, got, want[n-1]*time.Second)
		}
	}
}

func TestWebhookBackoffGeometricFallbackWhenNoSchedule(t *testing.T) {
	c := retryCurve{base: time.Second, ceiling: 64 * time.Second, jitterPercent: 20}
	want := []time.Duration{1, 4, 16, 64, 64}
	for n := 1; n <= len(want); n++ {
		if got := webhookBackoff(c, n, 0, 0.5); got != want[n-1]*time.Second {
			t.Errorf("geometric attempt %d = %v, want %v", n, got, want[n-1]*time.Second)
		}
	}
}

func TestWebhookBackoffJitterSpreadFollowsPercent(t *testing.T) {
	c := retryCurve{schedule: []time.Duration{4 * time.Second}, jitterPercent: 50}
	if got := webhookBackoff(c, 1, 0, 0); got != 2*time.Second { // -50%
		t.Errorf("jitter 0 with 50%% = %v, want 2s", got)
	}
	if got := webhookBackoff(c, 1, 0, 1); got != 6*time.Second { // +50%
		t.Errorf("jitter 1 with 50%% = %v, want 6s", got)
	}
	// Out-of-range jitter draws are clamped to the spread's edges.
	if got := webhookBackoff(c, 1, 0, -3); got != 2*time.Second {
		t.Errorf("negative jitter draw = %v, want the lower edge 2s", got)
	}
	if got := webhookBackoff(c, 1, 0, 9); got != 6*time.Second {
		t.Errorf("large jitter draw = %v, want the upper edge 6s", got)
	}
	// A zero jitter percent falls back to the default spread (20%).
	zero := retryCurve{schedule: []time.Duration{10 * time.Second}}
	if got := webhookBackoff(zero, 1, 0, 0); got != 8*time.Second {
		t.Errorf("zero jitter percent = %v, want 8s (the 20%% default)", got)
	}
}

func TestWebhookBackoffRetryAfterRaisesDelay(t *testing.T) {
	c := retryCurve{schedule: []time.Duration{time.Second}, jitterPercent: 20}
	if got := webhookBackoff(c, 1, 30*time.Second, 0.5); got != 30*time.Second {
		t.Fatalf("retry-after 30s over a 1s delay = %v, want 30s", got)
	}
	if got := webhookBackoff(c, 1, time.Millisecond, 0.5); got != time.Second {
		t.Fatalf("a shorter retry-after must not lower the delay: %v", got)
	}
}

func TestWebhookSettingsCurveCarriesTheConfiguredSchedule(t *testing.T) {
	set := WebhookSettings{
		Backoff:       []time.Duration{3 * time.Second},
		JitterPercent: 10,
	}.withDefaults()
	c := set.curve()
	if len(c.schedule) != 1 || c.schedule[0] != 3*time.Second || c.jitterPercent != 10 {
		t.Fatalf("curve = %+v", c)
	}
	fwd := ForwardSettings{Backoff: []time.Duration{7 * time.Second}}.withDefaults().curve()
	if len(fwd.schedule) != 1 || fwd.schedule[0] != 7*time.Second || fwd.jitterPercent != config.DefaultNotifyJitterPercent {
		t.Fatalf("forwarding curve = %+v", fwd)
	}
}

func TestMarshalWebhookEnvelopeHonoursTheLiveCap(t *testing.T) {
	env := WebhookEnvelope{Version: WebhookEnvelopeVersion, DeliveryID: "d1", Host: "h", Event: webhookTestEvent()}
	full, err := marshalWebhookEnvelope(env, 0)
	if err != nil {
		t.Fatalf("default cap: %v", err)
	}
	if _, err := marshalWebhookEnvelope(env, len(full)); err != nil {
		t.Fatalf("a cap equal to the body size must pass: %v", err)
	}
	if _, err := marshalWebhookEnvelope(env, 40); err == nil || !strings.Contains(err.Error(), "40 byte cap") {
		t.Fatalf("a cap of 40 bytes: err = %v, want the named cap in the error", err)
	}
}

func TestDefaultWebhookSettingsCarryEveryProposalValue(t *testing.T) {
	d := DefaultWebhookSettings()
	if d.Enabled || d.MaxAttempts != 5 || d.JitterPercent != 20 || d.MaxBodyBytes != 64*1024 ||
		d.SignatureTolerance != 300*time.Second || d.Timeout != 10*time.Second || len(d.Backoff) != 4 {
		t.Fatalf("DefaultWebhookSettings = %+v", d)
	}
	f := DefaultForwardSettings()
	if f.Enabled || f.MaxHops != 3 || f.MaxAttempts != 5 || f.JitterPercent != 20 || len(f.Backoff) != 4 {
		t.Fatalf("DefaultForwardSettings = %+v", f)
	}
}
