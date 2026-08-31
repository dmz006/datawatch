// BL368 — unit tests for manifest.go AcceptsImages field.
//
// TS-BL368-M1: AcceptsImages true parses correctly
// TS-BL368-M2: AcceptsImages absent defaults to false
// TS-BL368-M3: AcceptsImages false explicit
// TS-BL368-M4: AcceptsImages not in Extra when parsed as explicit field

package skills

import "testing"

// TS-BL368-M1
func TestManifest_AcceptsImages_True(t *testing.T) {
	yaml := `---
name: screenshot-reviewer
accepts_images: true
---`
	m, err := ParseManifestBytes([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if !m.AcceptsImages {
		t.Error("AcceptsImages: want true, got false")
	}
	// must not also appear in Extra
	if _, inExtra := m.Extra["accepts_images"]; inExtra {
		t.Error("accepts_images leaked into Extra map")
	}
}

// TS-BL368-M2
func TestManifest_AcceptsImages_DefaultFalse(t *testing.T) {
	yaml := `---
name: go-review
description: Reviews Go code
---`
	m, err := ParseManifestBytes([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if m.AcceptsImages {
		t.Error("AcceptsImages: want false (default), got true")
	}
}

// TS-BL368-M3
func TestManifest_AcceptsImages_ExplicitFalse(t *testing.T) {
	yaml := `---
name: code-audit
accepts_images: false
---`
	m, err := ParseManifestBytes([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if m.AcceptsImages {
		t.Error("AcceptsImages: want false (explicit), got true")
	}
}

// TS-BL368-M4: known fields don't end up in Extra
func TestManifest_AcceptsImages_KnownFieldsNotInExtra(t *testing.T) {
	yaml := `---
name: full-skill
description: test
version: 1.0.0
accepts_images: true
sampling_hook: "summarize"
guardrail_profile: strict
---`
	m, err := ParseManifestBytes([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"accepts_images", "sampling_hook", "guardrail_profile"} {
		if _, ok := m.Extra[field]; ok {
			t.Errorf("field %q leaked into Extra", field)
		}
	}
	if !m.AcceptsImages {
		t.Error("AcceptsImages: want true")
	}
}
