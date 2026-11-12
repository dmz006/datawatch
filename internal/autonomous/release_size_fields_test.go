package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
)

// Release size fields on the Automaton record: JSON round trip, omission when
// unset, and rejection of unknown sizes.

func TestReleaseSizeFieldsJSONRoundTrip(t *testing.T) {
	in := PRD{ID: "p1", ReleaseSize: ReleaseSizeMinor, BreakingChange: true}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"release_size":"minor"`) {
		t.Errorf("release_size not encoded: %s", s)
	}
	if !strings.Contains(s, `"breaking_change":true`) {
		t.Errorf("breaking_change not encoded: %s", s)
	}
	var out PRD
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ReleaseSize != ReleaseSizeMinor || !out.BreakingChange {
		t.Errorf("round trip lost fields: %+v", out)
	}
}

func TestReleaseSizeFieldsOmittedWhenEmpty(t *testing.T) {
	b, err := json.Marshal(PRD{ID: "p2"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "release_size") {
		t.Errorf("empty release_size should be omitted: %s", s)
	}
	if strings.Contains(s, "breaking_change") {
		t.Errorf("false breaking_change should be omitted: %s", s)
	}
}

func TestValidReleaseSize(t *testing.T) {
	for _, v := range []string{"", "patch", "minor", "major", "none", " Major "} {
		if !ValidReleaseSize(v) {
			t.Errorf("ValidReleaseSize(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"huge", "milestone", "patch,minor", "1.0.0"} {
		if ValidReleaseSize(v) {
			t.Errorf("ValidReleaseSize(%q) = true, want false", v)
		}
	}
}

func TestNormalizeReleaseSize(t *testing.T) {
	if v, ok := NormalizeReleaseSize("  MINOR "); !ok || v != ReleaseSizeMinor {
		t.Errorf("NormalizeReleaseSize(MINOR) = %q, %v", v, ok)
	}
	if v, ok := NormalizeReleaseSize(""); !ok || v != "" {
		t.Errorf("NormalizeReleaseSize(empty) = %q, %v; want empty, true", v, ok)
	}
	if _, ok := NormalizeReleaseSize("bogus"); ok {
		t.Error("NormalizeReleaseSize(bogus) accepted an unknown size")
	}
}
