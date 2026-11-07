package config

import "testing"

func TestPlatformContext_DefaultTrue(t *testing.T) {
	var c Config
	if !c.PlatformContext.IsEnabled() {
		t.Fatal("default should be enabled")
	}
	if !c.PlatformContext.EffectiveEnabled(nil) {
		t.Fatal("effective default should be true")
	}
}

func TestPlatformContext_GlobalFalse(t *testing.T) {
	p := PlatformContextConfig{Enabled: boolPtr(false)}
	if p.IsEnabled() || p.EffectiveEnabled(nil) || p.EffectiveEnabled(&ProjectPolicy{}) {
		t.Fatal("global false should disable")
	}
}

func TestPlatformContext_ProjectOverride(t *testing.T) {
	p := PlatformContextConfig{Enabled: boolPtr(false)}
	if !p.EffectiveEnabled(&ProjectPolicy{PlatformContextEnabled: boolPtr(true)}) {
		t.Fatal("project true should override global false")
	}
	p = PlatformContextConfig{Enabled: boolPtr(true)}
	if p.EffectiveEnabled(&ProjectPolicy{PlatformContextEnabled: boolPtr(false)}) {
		t.Fatal("project false should override global true")
	}
}

func TestPlatformContext_Note(t *testing.T) {
	if got := (PlatformContextConfig{}).AppendNote("base"); got != "base" {
		t.Fatalf("empty note changed text: %q", got)
	}
	if got := (PlatformContextConfig{Note: "  \n"}).AppendNote("base"); got != "base" {
		t.Fatalf("blank note changed text: %q", got)
	}
	got := PlatformContextConfig{Note: "extra"}.AppendNote("base\n")
	if got != "base\n\nextra" {
		t.Fatalf("got %q", got)
	}
}
