package config

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The proposal's recommended values, pinned so a changed decision is a
// deliberate edit to one constant block (and to this table).
func TestNotifyConfigDefaultsMatchProposal(t *testing.T) {
	if DefaultNotifyMaxAttempts != 5 || DefaultNotifyJitterPercent != 20 ||
		DefaultNotifyAttemptTimeoutSeconds != 10 || DefaultNotifyMaxBodyBytes != 64*1024 ||
		DefaultNotifySignatureToleranceSeconds != 300 || DefaultNotifyMaxHops != 3 {
		t.Fatalf("a proposal default changed; update docs and the proposal checklist with it")
	}
	want := []int{1, 4, 16, 64}
	got := DefaultNotifyBackoffSeconds()
	if len(got) != len(want) {
		t.Fatalf("default backoff = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("default backoff = %v, want %v", got, want)
		}
	}
}

func TestNotifyConfigYAMLRoundTripKeepsExplicitFalseAndEmptyList(t *testing.T) {
	in := AutonomousNotifyConfig{
		Webhook:    AutonomousNotifyWebhookConfig{Enabled: false, AllowHosts: []string{}, MaxAttempts: 7},
		Forwarding: AutonomousNotifyForwardingConfig{Enabled: false, MaxHops: 2},
	}
	b, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(b)
	for _, want := range []string{"enabled: false", "allow_hosts: []", "max_attempts: 7", "max_hops: 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("YAML lacks %q (an explicit value was elided):\n%s", want, text)
		}
	}
	// Unmarshal over a struct that holds the opposite values: the explicit
	// false and the empty list must win, not leave the previous value.
	out := AutonomousNotifyConfig{
		Webhook:    AutonomousNotifyWebhookConfig{Enabled: true, AllowHosts: []string{"old.example.net"}},
		Forwarding: AutonomousNotifyForwardingConfig{Enabled: true},
	}
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Webhook.Enabled || len(out.Webhook.AllowHosts) != 0 || out.Webhook.MaxAttempts != 7 {
		t.Fatalf("webhook after YAML round trip = %+v", out.Webhook)
	}
	if out.Forwarding.Enabled || out.Forwarding.MaxHops != 2 {
		t.Fatalf("forwarding after YAML round trip = %+v", out.Forwarding)
	}
}

func TestNotifyConfigYAMLKeyNamesAreTheProposalKeys(t *testing.T) {
	in := AutonomousNotifyConfig{
		Webhook: AutonomousNotifyWebhookConfig{
			BackoffSeconds: []int{1, 2}, JitterPercent: 10, AttemptTimeoutSeconds: 3,
			MaxBodyBytes: 4096, SignatureToleranceSeconds: 60, AllowHosts: []string{"a.example.net:443"},
		},
		Forwarding: AutonomousNotifyForwardingConfig{BackoffSeconds: []int{5}, JitterPercent: 15, AttemptTimeoutSeconds: 6},
	}
	b, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	webhook, _ := raw["webhook"].(map[string]any)
	forwarding, _ := raw["forwarding"].(map[string]any)
	wantWebhook := []string{"enabled", "allow_hosts", "max_attempts", "backoff_seconds", "jitter_percent",
		"attempt_timeout_seconds", "max_body_bytes", "signature_tolerance_seconds"}
	wantForwarding := []string{"enabled", "max_hops", "max_attempts", "backoff_seconds", "jitter_percent",
		"attempt_timeout_seconds"}
	for _, k := range wantWebhook {
		if _, ok := webhook[k]; !ok {
			t.Errorf("webhook YAML lacks key %q", k)
		}
	}
	for _, k := range wantForwarding {
		if _, ok := forwarding[k]; !ok {
			t.Errorf("forwarding YAML lacks key %q", k)
		}
	}
	if len(webhook) != len(wantWebhook) || len(forwarding) != len(wantForwarding) {
		t.Errorf("unexpected keys: webhook=%v forwarding=%v", webhook, forwarding)
	}
}

func TestNotifyConfigJSONRoundTripKeepsExplicitFalseAndEmptyList(t *testing.T) {
	in := AutonomousNotifyConfig{
		Webhook:    AutonomousNotifyWebhookConfig{Enabled: false, AllowHosts: []string{}},
		Forwarding: AutonomousNotifyForwardingConfig{Enabled: false},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(b)
	for _, want := range []string{`"enabled":false`, `"allow_hosts":[]`} {
		if !strings.Contains(text, want) {
			t.Fatalf("JSON lacks %s: %s", want, text)
		}
	}
	out := AutonomousNotifyConfig{
		Webhook:    AutonomousNotifyWebhookConfig{Enabled: true, AllowHosts: []string{"old.example.net"}},
		Forwarding: AutonomousNotifyForwardingConfig{Enabled: true},
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Webhook.Enabled || len(out.Webhook.AllowHosts) != 0 || out.Forwarding.Enabled {
		t.Fatalf("after JSON round trip webhook=%+v forwarding=%+v", out.Webhook, out.Forwarding)
	}
}

func TestNotifyConfigZeroValuesReadAsDefaults(t *testing.T) {
	var w AutonomousNotifyWebhookConfig
	var f AutonomousNotifyForwardingConfig
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"webhook.max_attempts", w.EffectiveMaxAttempts(), DefaultNotifyMaxAttempts},
		{"webhook.jitter_percent", w.EffectiveJitterPercent(), DefaultNotifyJitterPercent},
		{"webhook.attempt_timeout_seconds", w.EffectiveAttemptTimeoutSeconds(), DefaultNotifyAttemptTimeoutSeconds},
		{"webhook.max_body_bytes", w.EffectiveMaxBodyBytes(), DefaultNotifyMaxBodyBytes},
		{"webhook.signature_tolerance_seconds", w.EffectiveSignatureToleranceSeconds(), DefaultNotifySignatureToleranceSeconds},
		{"forwarding.max_hops", f.EffectiveMaxHops(), DefaultNotifyMaxHops},
		{"forwarding.max_attempts", f.EffectiveMaxAttempts(), DefaultNotifyMaxAttempts},
		{"forwarding.jitter_percent", f.EffectiveJitterPercent(), DefaultNotifyJitterPercent},
		{"forwarding.attempt_timeout_seconds", f.EffectiveAttemptTimeoutSeconds(), DefaultNotifyAttemptTimeoutSeconds},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s zero value = %d, want default %d", c.name, c.got, c.want)
		}
	}
	for _, ints := range [][]int{w.EffectiveBackoffSeconds(), f.EffectiveBackoffSeconds()} {
		if len(ints) != 4 || ints[0] != 1 || ints[3] != 64 {
			t.Errorf("zero backoff reads as %v, want the default schedule", ints)
		}
	}
}

func TestNotifyConfigEffectiveValuesKeepPositiveSettings(t *testing.T) {
	w := AutonomousNotifyWebhookConfig{
		MaxAttempts: 3, JitterPercent: 40, AttemptTimeoutSeconds: 2,
		MaxBodyBytes: 2048, SignatureToleranceSeconds: 90, BackoffSeconds: []int{2, 8},
	}
	if w.EffectiveMaxAttempts() != 3 || w.EffectiveJitterPercent() != 40 ||
		w.EffectiveAttemptTimeoutSeconds() != 2 || w.EffectiveMaxBodyBytes() != 2048 ||
		w.EffectiveSignatureToleranceSeconds() != 90 {
		t.Fatalf("positive settings were not kept: %+v", w)
	}
	if got := w.EffectiveBackoffSeconds(); len(got) != 2 || got[0] != 2 || got[1] != 8 {
		t.Fatalf("backoff = %v, want [2 8]", got)
	}
	f := AutonomousNotifyForwardingConfig{MaxHops: 5, MaxAttempts: 2, JitterPercent: 10, AttemptTimeoutSeconds: 4}
	if f.EffectiveMaxHops() != 5 || f.EffectiveMaxAttempts() != 2 || f.EffectiveJitterPercent() != 10 ||
		f.EffectiveAttemptTimeoutSeconds() != 4 {
		t.Fatalf("forwarding positive settings were not kept: %+v", f)
	}
}

func TestNotifyConfigNegativeValuesReadAsDefaults(t *testing.T) {
	w := AutonomousNotifyWebhookConfig{MaxAttempts: -1, JitterPercent: -5, AttemptTimeoutSeconds: 0, MaxBodyBytes: -9}
	if w.EffectiveMaxAttempts() != DefaultNotifyMaxAttempts || w.EffectiveJitterPercent() != DefaultNotifyJitterPercent ||
		w.EffectiveAttemptTimeoutSeconds() != DefaultNotifyAttemptTimeoutSeconds || w.EffectiveMaxBodyBytes() != DefaultNotifyMaxBodyBytes {
		t.Fatalf("negative values did not fall back: %+v", w)
	}
}

func TestNotifyConfigJitterIsCappedAt100(t *testing.T) {
	w := AutonomousNotifyWebhookConfig{JitterPercent: 250}
	if got := w.EffectiveJitterPercent(); got != 100 {
		t.Fatalf("webhook jitter 250 = %d, want 100", got)
	}
	f := AutonomousNotifyForwardingConfig{JitterPercent: 101}
	if got := f.EffectiveJitterPercent(); got != 100 {
		t.Fatalf("forwarding jitter 101 = %d, want 100", got)
	}
}

func TestNotifyConfigBackoffDropsNonPositiveEntries(t *testing.T) {
	w := AutonomousNotifyWebhookConfig{BackoffSeconds: []int{0, 3, -2, 9}}
	if got := w.EffectiveBackoffSeconds(); len(got) != 2 || got[0] != 3 || got[1] != 9 {
		t.Fatalf("backoff = %v, want [3 9]", got)
	}
	allBad := AutonomousNotifyForwardingConfig{BackoffSeconds: []int{0, -1}}
	if got := allBad.EffectiveBackoffSeconds(); len(got) != 4 {
		t.Fatalf("all-invalid backoff = %v, want the default schedule", got)
	}
}

func TestNotifyConfigDefaultBackoffIsAFreshSlice(t *testing.T) {
	a := DefaultNotifyBackoffSeconds()
	a[0] = 999
	if b := DefaultNotifyBackoffSeconds(); b[0] != 1 {
		t.Fatalf("mutating a returned default changed the next one: %v", b)
	}
}
