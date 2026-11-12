package platformcontext

import (
	"regexp"
	"strings"
	"testing"
)

func TestPlatformContextTokenBudget(t *testing.T) {
	if n := EstimateTokens(PlatformContext); n >= 200 {
		t.Fatalf("platform context estimated at %d tokens, want under 200", n)
	}
}

func TestPlatformContextRequiredContent(t *testing.T) {
	lower := strings.ToLower(PlatformContext)
	for _, w := range []string{
		"datawatch", "docs search", "docs read", "automata", "memory",
		"federation", "messaging", "secrets", "alerts", "many more tools",
	} {
		if !strings.Contains(lower, w) {
			t.Errorf("platform context missing %q", w)
		}
	}
}

func TestPlatformContextNoTrackerIDs(t *testing.T) {
	re := regexp.MustCompile(`\b(BL|GH|SEC)[-#]?\d+\b`)
	if m := re.FindString(PlatformContext); m != "" {
		t.Errorf("tracker identifier %q in platform context", m)
	}
	for _, w := range []string{"BL", "GH", "SEC", "PRD"} {
		if regexp.MustCompile(`\b` + w + `\b`).MatchString(PlatformContext) {
			t.Errorf("forbidden term %q in platform context", w)
		}
	}
}
