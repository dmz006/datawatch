package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTagSettingsDefaults(t *testing.T) {
	got := DefaultConfig().Autonomous.Tags
	want := TagsConfig{
		DefaultView:         "auto",
		AttentionStatuses:   []string{"blocked", "needs_review", "paused"},
		DefaultMatchMode:    "any",
		DirectCountsVisible: false,
		RegistryKeeper:      "",
		RegistrySyncSeconds: 30,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultConfig tags = %+v, want %+v", got, want)
	}
}

func TestTagSettingsDefaultStatusesAreAutomatonStatuses(t *testing.T) {
	for _, s := range DefaultTagAttentionStatuses {
		if !isTagAutomatonStatus(s) {
			t.Fatalf("default attention status %q is not an Automaton status", s)
		}
	}
}

func TestTagSettingsValidateAcceptsValidValues(t *testing.T) {
	cases := []struct {
		key   string
		value interface{}
	}{
		{"autonomous.tags.default_view", "auto"},
		{"autonomous.tags.default_view", "Attention"},
		{"autonomous.tags.default_view", "tags"},
		{"autonomous.tags.attention_statuses", "blocked,paused"},
		{"autonomous.tags.attention_statuses", []interface{}{"needs_review", "failed"}},
		{"autonomous.tags.default_match_mode", "all"},
		{"autonomous.tags.default_match_mode", "any"},
		{"autonomous.tags.direct_counts_visible", true},
		{"autonomous.tags.direct_counts_visible", "false"},
		{"autonomous.tags.registry_keeper", ""},
		{"autonomous.tags.registry_keeper", "peer-a"},
		{"autonomous.tags.registry_keeper", float64(42)},
		{"autonomous.tags.registry_sync_seconds", float64(5)},
		{"autonomous.tags.registry_sync_seconds", "120"},
	}
	for _, tc := range cases {
		if err := ValidateTagSetting(tc.key, tc.value); err != nil {
			t.Errorf("ValidateTagSetting(%q, %v) = %v, want nil", tc.key, tc.value, err)
		}
	}
}

func TestTagSettingsValidateRefusesInvalidValues(t *testing.T) {
	cases := []struct {
		key   string
		value interface{}
		want  string
	}{
		{"autonomous.tags.default_view", "sometimes", "autonomous.tags.default_view: must be auto, attention or tags"},
		{"autonomous.tags.default_match_mode", "sometimes", "autonomous.tags.default_match_mode: must be any or all"},
		{"autonomous.tags.attention_statuses", "bogus", unknownStatusMessage("bogus")},
		{"autonomous.tags.attention_statuses", []interface{}{"blocked", "active"}, unknownStatusMessage("active")},
		{"autonomous.tags.attention_statuses", []interface{}{float64(1)}, "autonomous.tags.attention_statuses: must be a list of Automaton statuses"},
		{"autonomous.tags.attention_statuses", float64(3), "autonomous.tags.attention_statuses: must be a list of Automaton statuses"},
		{"autonomous.tags.direct_counts_visible", "yes", "autonomous.tags.direct_counts_visible: must be true or false"},
		{"autonomous.tags.registry_keeper", []interface{}{"x"}, "autonomous.tags.registry_keeper: must be a federation peer name, or empty for this daemon"},
		{"autonomous.tags.registry_sync_seconds", float64(4), "autonomous.tags.registry_sync_seconds: must be a whole number of seconds >= 5"},
		{"autonomous.tags.registry_sync_seconds", float64(7.5), "autonomous.tags.registry_sync_seconds: must be a whole number of seconds >= 5"},
		{"autonomous.tags.registry_sync_seconds", "soon", "autonomous.tags.registry_sync_seconds: must be a whole number of seconds >= 5"},
		{"autonomous.tags.unknown_setting", "x", "autonomous.tags.unknown_setting: unknown tag setting"},
	}
	for _, tc := range cases {
		err := ValidateTagSetting(tc.key, tc.value)
		if err == nil {
			t.Errorf("ValidateTagSetting(%q, %v) = nil, want %q", tc.key, tc.value, tc.want)
			continue
		}
		if err.Error() != tc.want {
			t.Errorf("ValidateTagSetting(%q, %v) = %q, want %q", tc.key, tc.value, err.Error(), tc.want)
		}
	}
}

// unknownStatusMessage is the refusal for one status that is not an Automaton status.
func unknownStatusMessage(s string) string {
	return `autonomous.tags.attention_statuses: unknown Automaton status "` + s +
		`" (valid: ` + strings.Join(TagAutomatonStatuses, ", ") + `)`
}

func TestTagSettingsValidateIgnoresKeysOutsideTheBlock(t *testing.T) {
	if err := ValidateTagSetting("autonomous.plan_coverage_check", "bogus"); err != nil {
		t.Fatalf("a key outside autonomous.tags must not be refused here: %v", err)
	}
}

func TestTagSettingsSetStoresNormalizedValues(t *testing.T) {
	var tags TagsConfig
	if err := tags.Set("attention_statuses", " Blocked, paused ,blocked,"); err != nil {
		t.Fatalf("Set attention_statuses: %v", err)
	}
	if want := []string{"blocked", "paused"}; !reflect.DeepEqual(tags.AttentionStatuses, want) {
		t.Fatalf("attention_statuses = %v, want %v", tags.AttentionStatuses, want)
	}
	if err := tags.Set("registry_sync_seconds", "45"); err != nil {
		t.Fatalf("Set registry_sync_seconds: %v", err)
	}
	if tags.RegistrySyncSeconds != 45 {
		t.Fatalf("registry_sync_seconds = %d, want 45", tags.RegistrySyncSeconds)
	}
}

func TestTagSettingsSetRefusalLeavesValueUnchanged(t *testing.T) {
	tags := TagsConfig{DefaultMatchMode: "all"}
	if err := tags.Set("default_match_mode", "sometimes"); err == nil {
		t.Fatal("Set with an invalid mode returned nil")
	}
	if tags.DefaultMatchMode != "all" {
		t.Fatalf("a refused Set changed default_match_mode to %q", tags.DefaultMatchMode)
	}
}

func TestTagSettingsEffectiveFillsOnlyUnsetDefaults(t *testing.T) {
	got := TagsConfig{RegistryKeeper: "peer-a", DirectCountsVisible: true}.Effective()
	if got.DefaultView != "auto" || got.DefaultMatchMode != "any" || got.RegistrySyncSeconds != 30 {
		t.Fatalf("Effective did not fill the defaults: %+v", got)
	}
	if got.RegistryKeeper != "peer-a" || !got.DirectCountsVisible {
		t.Fatalf("Effective changed a set value: %+v", got)
	}
	if want := []string{"blocked", "needs_review", "paused"}; !reflect.DeepEqual(got.AttentionStatuses, want) {
		t.Fatalf("Effective attention_statuses = %v, want %v", got.AttentionStatuses, want)
	}
}

func TestTagSettingsLoadRefusesInvalidValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "autonomous:\n  tags:\n    default_match_mode: sometimes\n"
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "autonomous.tags.default_match_mode: must be any or all") {
		t.Fatalf("Load error = %v, want the default_match_mode refusal", err)
	}
}

func TestTagSettingsLoadKeepsExplicitFalseAndEmptyAcrossSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultConfig()
	cfg.Autonomous.Tags.DirectCountsVisible = false
	cfg.Autonomous.Tags.RegistryKeeper = ""
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	for _, want := range []string{"direct_counts_visible: false", `registry_keeper: ""`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("saved config lacks explicit %q:\n%s", want, raw)
		}
	}
	back, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if back.Autonomous.Tags.DirectCountsVisible || back.Autonomous.Tags.RegistryKeeper != "" {
		t.Fatalf("explicit false/empty did not survive: %+v", back.Autonomous.Tags)
	}
}

// TestTagSettingsEmptyMeansDefault checks the empty and zero values a surface
// may send, or a zero-valued config may save, take their defaults and reload.
func TestTagSettingsEmptyMeansDefault(t *testing.T) {
	var tags TagsConfig
	for _, v := range []struct {
		name  string
		value interface{}
	}{
		{"default_view", ""},
		{"default_match_mode", " "},
		{"attention_statuses", " , "},
	} {
		if err := tags.Set(v.name, v.value); err != nil {
			t.Fatalf("Set %s to an empty value: %v", v.name, err)
		}
	}
	if tags.DefaultView != "auto" || tags.DefaultMatchMode != "any" {
		t.Fatalf("empty values did not take defaults: %+v", tags)
	}
	if want := []string{"blocked", "needs_review", "paused"}; !reflect.DeepEqual(tags.AttentionStatuses, want) {
		t.Fatalf("empty attention statuses = %v, want the defaults %v", tags.AttentionStatuses, want)
	}
}

func TestTagSettingsLoadReloadsAZeroValuedBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultConfig()
	cfg.Autonomous.Tags = TagsConfig{}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatalf("a zero-valued block must reload, got %v", err)
	}
	if got := back.Autonomous.Tags.Effective(); !reflect.DeepEqual(got, DefaultConfig().Autonomous.Tags) {
		t.Fatalf("zero-valued block reloaded as %+v, want the defaults", got)
	}
}

func TestTagSettingsLoadAbsentBlockKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("autonomous:\n  enabled: true\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Autonomous.Tags.RegistrySyncSeconds; got != 30 {
		t.Fatalf("absent registry_sync_seconds loaded as %d, want the default 30", got)
	}
	if got := cfg.Autonomous.Tags.DefaultView; got != "auto" {
		t.Fatalf("absent default_view loaded as %q, want auto", got)
	}
}
