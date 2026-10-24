package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProgressKindNormalize(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"feature", "feature", true},
		{"bug", "bug", true},
		{"other", "other", true},
		{"", "", true},
		{"  BUG ", "bug", true},
		{"Feature", "feature", true},
		{"   ", "", true},
		{"chore", "chore", false},
		{"bugs", "bugs", false},
	}
	for _, c := range cases {
		got, ok := NormalizePRDKind(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("NormalizePRDKind(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestProgressKindDeriveTriggerWords(t *testing.T) {
	words := []string{
		"fix", "flak", "defect", "bug", "root cause", "root-cause", "root_cause", "stall", "regress",
		"race", "leak", "must not", "no longer", "crash", "broken", "fail", "fails", "failure",
		"blocker", "suppress", "cve", "trivy", "deterministic", "isolate", "refuse", "stuck",
	}
	for _, w := range words {
		for _, title := range []string{"Handle the " + w + " today", w, w + " first", "ends with " + w} {
			if got := DeriveKind(title, ""); got != PRDKindBug {
				t.Errorf("DeriveKind(%q) = %q, want bug", title, got)
			}
		}
	}
}

func TestProgressKindDeriveCaseInsensitive(t *testing.T) {
	for _, title := range []string{"FIX the thing", "Root Cause analysis", "A CVE", "Trivy scan", "MUST NOT drop"} {
		if got := DeriveKind(title, ""); got != PRDKindBug {
			t.Errorf("DeriveKind(%q) = %q, want bug", title, got)
		}
	}
}

func TestProgressKindDeriveWordBoundary(t *testing.T) {
	cases := []string{
		"prefix handling", "fixture loader", "fixes the thing", "debugger panel", "bugfix", "tracer",
		"embrace", "leakage", "racecar", "crashes", "failing", "failed", "stalled", "stuckup",
		"cves", "must nothing", "éfix", "fixé", "fix_it", "under_fix", "9fix", "fix9",
		"Add a new dashboard", "",
	}
	for _, title := range cases {
		if got := DeriveKind(title, ""); got != PRDKindFeature {
			t.Errorf("DeriveKind(%q) = %q, want feature", title, got)
		}
	}
	// A real match later in the text still counts after a near-miss.
	if got := DeriveKind("prefix then fix", ""); got != PRDKindBug {
		t.Errorf("near-miss then match = %q, want bug", got)
	}
	// Punctuation is a boundary.
	for _, title := range []string{"(fix)", "fix.", "a-fix-b", "[bug]", "race;"} {
		if got := DeriveKind(title, ""); got != PRDKindBug {
			t.Errorf("DeriveKind(%q) = %q, want bug", title, got)
		}
	}
	// root.cause: any single character between the two words.
	if got := DeriveKind("rootXcause", ""); got != PRDKindBug {
		t.Errorf("rootXcause = %q, want bug", got)
	}
	if got := DeriveKind("root\ncause", ""); got != PRDKindFeature {
		t.Errorf("root newline cause = %q, want feature (dot does not match newline)", got)
	}
	if got := DeriveKind("root  cause", ""); got != PRDKindFeature {
		t.Errorf("two characters between root and cause = %q, want feature", got)
	}
}

func TestProgressKindDeriveSpecWindow(t *testing.T) {
	// The spec counts only through its first 240 characters.
	filler := strings.Repeat("a ", 119) // 238 characters
	if got := DeriveKind("Title", filler+"fix"); got != PRDKindFeature {
		t.Errorf("fix starting at character 238 straddles the cut: got %q, want feature", got)
	}
	if got := DeriveKind("Title", filler[:236]+" fix"); got != PRDKindBug {
		t.Errorf("fix ending exactly at character 240: got %q, want bug", got)
	}
	long := strings.Repeat("x", 240) + " fix"
	if got := DeriveKind("Title", long); got != PRDKindFeature {
		t.Errorf("fix after the cut: got %q, want feature", got)
	}
	if got := DeriveKind("Title", "fix "+strings.Repeat("x", 500)); got != PRDKindBug {
		t.Errorf("fix at the start of a long spec: got %q, want bug", got)
	}
}

func TestProgressKindDeriveCutCountsCharacters(t *testing.T) {
	// 239 two-byte characters then " fix": with 240 characters kept the word is
	// cut after its first letter; counted in bytes it would be cut far earlier.
	spec := strings.Repeat("é", 239) + " fix"
	if got := DeriveKind("T", spec); got != PRDKindFeature {
		t.Errorf("cut inside the word: got %q, want feature", got)
	}
	// 236 multibyte characters then " fix" is 240 characters: the word survives
	// whole, although the byte length is well past 240.
	spec = strings.Repeat("é", 235) + " fix"
	if len(spec) <= 240 {
		t.Fatalf("test spec should be longer than 240 bytes, got %d", len(spec))
	}
	if got := DeriveKind("T", spec); got != PRDKindBug {
		t.Errorf("word kept whole within 240 characters: got %q, want bug", got)
	}
	// Four-byte characters.
	spec = strings.Repeat("😀", 236) + " bug"
	if got := DeriveKind("T", spec); got != PRDKindBug {
		t.Errorf("emoji prefix: got %q, want bug", got)
	}
	spec = strings.Repeat("😀", 238) + " bug"
	if got := DeriveKind("T", spec); got != PRDKindFeature {
		t.Errorf("emoji prefix past the cut: got %q, want feature", got)
	}
}

func TestProgressKindDeriveTitleAndSpecJoined(t *testing.T) {
	// Title and spec are joined by one space, so a word cannot form across them.
	if got := DeriveKind("pre", "fix"); got != PRDKindBug {
		t.Errorf("title then spec word: got %q, want bug", got)
	}
	if got := DeriveKind("must", "not do it"); got != PRDKindBug {
		t.Errorf("phrase spanning title and spec joined by one space: got %q, want bug", got)
	}
	if got := DeriveKind("", "no longer works"); got != PRDKindBug {
		t.Errorf("empty title: got %q, want bug", got)
	}
}

func TestProgressKindEffectivePrecedence(t *testing.T) {
	bugSpec := &PRD{ID: "aaaa1111", Title: "Fix the thing"}
	cases := []struct {
		name string
		p    *PRD
		rows map[string]string
		want string
	}{
		{"explicit feature beats row and derive", &PRD{ID: "aaaa1111", Title: "Fix it", Kind: "feature"}, map[string]string{"aaaa1111": "bug"}, "feature"},
		{"explicit other", &PRD{ID: "aaaa1111", Title: "x", Kind: "other"}, nil, "other"},
		{"explicit is normalized", &PRD{ID: "aaaa1111", Title: "x", Kind: " Bug "}, nil, "bug"},
		{"row beats derive", bugSpec, map[string]string{"aaaa1111": "other"}, "other"},
		{"row for another id is ignored", bugSpec, map[string]string{"bbbb2222": "other"}, "bug"},
		{"invalid explicit falls to row", &PRD{ID: "aaaa1111", Title: "x", Kind: "chore"}, map[string]string{"aaaa1111": "bug"}, "bug"},
		{"invalid explicit falls to derive", &PRD{ID: "aaaa1111", Title: "Fix it", Kind: "chore"}, nil, "bug"},
		{"invalid row falls to derive", &PRD{ID: "aaaa1111", Title: "Add a screen"}, map[string]string{"aaaa1111": "chore"}, "feature"},
		{"empty row falls to derive", bugSpec, map[string]string{"aaaa1111": ""}, "bug"},
		{"derive from spec", &PRD{ID: "aaaa1111", Title: "Title", Spec: "this is broken"}, nil, "bug"},
		{"derive feature", &PRD{ID: "aaaa1111", Title: "Add a screen", Spec: "nice"}, nil, "feature"},
		{"nil map", &PRD{ID: "aaaa1111", Title: "Add a screen"}, nil, "feature"},
		{"nil record", nil, nil, "feature"},
	}
	for _, c := range cases {
		if got := EffectiveKind(c.p, c.rows); got != c.want {
			t.Errorf("%s: EffectiveKind = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestProgressKindJSONRoundTrip(t *testing.T) {
	with := PRD{ID: "aaaa1111", Title: "T", Kind: PRDKindBug}
	data, err := json.Marshal(with)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"kind":"bug"`) {
		t.Errorf("kind missing from %s", data)
	}
	var back PRD
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Kind != PRDKindBug {
		t.Errorf("round trip kind = %q, want bug", back.Kind)
	}

	without := PRD{ID: "aaaa1111", Title: "T"}
	data, err = json.Marshal(without)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"kind"`) {
		t.Errorf("empty kind should be omitted, got %s", data)
	}

	var old PRD
	if err := json.Unmarshal([]byte(`{"id":"aaaa1111","title":"Old record","type":"software"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Kind != "" {
		t.Errorf("old record decoded kind = %q, want empty", old.Kind)
	}
	if got := EffectiveKind(&old, nil); got != PRDKindFeature {
		t.Errorf("old record effective kind = %q, want feature", got)
	}
}
