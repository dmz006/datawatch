package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPacksConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Packs.Enabled {
		t.Error("Packs.Enabled default = true, want false")
	}
	if want := []string{"builtin"}; !reflect.DeepEqual(cfg.Packs.Active, want) {
		t.Errorf("Packs.Active default = %v, want %v", cfg.Packs.Active, want)
	}
	if !reflect.DeepEqual(cfg.Packs, DefaultPacksConfig()) {
		t.Errorf("DefaultConfig().Packs = %+v, want DefaultPacksConfig() %+v", cfg.Packs, DefaultPacksConfig())
	}
}

func TestPacksConfigDefaultsAreIndependent(t *testing.T) {
	a := DefaultPacksConfig()
	a.Active[0] = "mutated"
	if got := DefaultPacksConfig().Active[0]; got != "builtin" {
		t.Errorf("default Active shared between calls: got %q", got)
	}
}

func TestPacksConfigValidate(t *testing.T) {
	cases := []struct {
		name      string
		active    []string
		installed []string
		wantErr   string
	}{
		{"default builtin, nothing installed", []string{"builtin"}, nil, ""},
		{"empty active", nil, nil, ""},
		{"installed pack", []string{"builtin", "acme"}, []string{"acme", "other"}, ""},
		{"installed only", []string{"acme"}, []string{"acme"}, ""},
		{"unknown pack", []string{"builtin", "ghost"}, []string{"acme"}, `"ghost"`},
		{"unknown with none installed", []string{"ghost"}, nil, `"ghost"`},
		{"case sensitive", []string{"Builtin"}, nil, `"Builtin"`},
		{"empty name", []string{""}, nil, "must not be empty"},
		{"blank name", []string{"builtin", "  "}, nil, "must not be empty"},
		{"duplicate", []string{"builtin", "builtin"}, nil, "more than once"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := PacksConfig{Active: tc.active}.Validate(tc.installed)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Validate error = %q, want it to contain %q", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), "packs.active") {
				t.Errorf("Validate error = %q, want it to name packs.active", err)
			}
		})
	}
}

func TestPacksConfigYAMLKeys(t *testing.T) {
	out, err := yaml.Marshal(Config{Packs: PacksConfig{Enabled: true, Active: []string{"builtin", "acme"}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	packs, ok := m["packs"].(map[string]any)
	if !ok {
		t.Fatalf("no packs section in:\n%s", out)
	}
	if packs["enabled"] != true {
		t.Errorf("packs.enabled = %v, want true", packs["enabled"])
	}
	if _, ok := packs["active"]; !ok {
		t.Errorf("packs.active missing in:\n%s", out)
	}
}

func TestPacksConfigYAMLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datawatch.yaml")
	cfg := DefaultConfig()
	cfg.Packs = PacksConfig{Enabled: true, Active: []string{"builtin", "acme"}}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got.Packs, cfg.Packs) {
		t.Errorf("round trip Packs = %+v, want %+v", got.Packs, cfg.Packs)
	}
}

func TestPacksConfigDefaultsSurviveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datawatch.yaml")
	if err := Save(DefaultConfig(), path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got.Packs, DefaultPacksConfig()) {
		t.Errorf("default round trip Packs = %+v, want %+v", got.Packs, DefaultPacksConfig())
	}
}

func TestPacksConfigLoadAbsentSectionKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datawatch.yaml")
	if err := os.WriteFile(path, []byte("hostname: testhost\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got.Packs, DefaultPacksConfig()) {
		t.Errorf("absent section Packs = %+v, want %+v", got.Packs, DefaultPacksConfig())
	}
}

func TestPacksConfigLoadExplicitValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datawatch.yaml")
	body := "packs:\n  enabled: true\n  active:\n    - builtin\n    - acme\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := PacksConfig{Enabled: true, Active: []string{"builtin", "acme"}}
	if !reflect.DeepEqual(got.Packs, want) {
		t.Errorf("Packs = %+v, want %+v", got.Packs, want)
	}
}
