// BL41 — effort levels per task.

package session

import "testing"

func TestBL41_IsValidEffort(t *testing.T) {
	cases := map[string]bool{
		"":         true, // empty = use default
		"quick":    true,
		"normal":   true,
		"thorough": true,
		"QUICK":    false, // case-sensitive
		"medium":   false,
		"x":        false,
	}
	for in, want := range cases {
		if got := IsValidEffort(in); got != want {
			t.Errorf("IsValidEffort(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestBL41_Manager_DefaultEffort(t *testing.T) {
	m := &Manager{}
	if m.DefaultEffort() != "normal" {
		t.Errorf("default-of-default should be 'normal', got %q", m.DefaultEffort())
	}
	m.SetDefaultEffort("thorough")
	if m.DefaultEffort() != "thorough" {
		t.Errorf("after SetDefaultEffort('thorough'): got %q", m.DefaultEffort())
	}
	// Invalid falls back to normal.
	m.SetDefaultEffort("nope")
	if m.DefaultEffort() != "normal" {
		t.Errorf("invalid effort should fall back to 'normal', got %q", m.DefaultEffort())
	}
}

func TestBL41_ResolveEffort_OptWins(t *testing.T) {
	m := &Manager{}
	m.SetDefaultEffort("normal")
	if got := m.resolveEffort(&StartOptions{Effort: "thorough"}); got != "thorough" {
		t.Errorf("opt should win: got %q", got)
	}
}

func TestBL41_ResolveEffort_FallbackToManagerDefault(t *testing.T) {
	m := &Manager{}
	m.SetDefaultEffort("quick")
	if got := m.resolveEffort(nil); got != "quick" {
		t.Errorf("nil opt should use manager default: got %q", got)
	}
	if got := m.resolveEffort(&StartOptions{}); got != "quick" {
		t.Errorf("empty opt.Effort should use manager default: got %q", got)
	}
}

func TestBL41_ResolveEffort_InvalidOptIgnored(t *testing.T) {
	m := &Manager{}
	m.SetDefaultEffort("thorough")
	// Invalid opt.Effort falls through to manager default.
	if got := m.resolveEffort(&StartOptions{Effort: "garbage"}); got != "thorough" {
		t.Errorf("invalid opt.Effort should fall through: got %q", got)
	}
}
