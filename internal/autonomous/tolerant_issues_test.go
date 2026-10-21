package autonomous

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The exact error the pre-tolerance code produced when a judge/verifier
// backend emitted an array of issues objects. The new code must never
// produce it.
const legacyIssuesError = `json: cannot unmarshal object into GuardrailVerdict.issues.0 of type string`

// objectIssueFragment is the canonical issue-object shape from the
// incident: description, message, file, line, severity.
const objectIssueFragment = `{"description":"d1","message":"m1","file":"f.go","line":7,"severity":"high"}`

type issuesTestCase struct {
	name string
	// issues is the JSON spliced into the "issues" slot; empty means
	// the field is absent entirely.
	issues string
	// wantExact is compared element-wise when non-nil.
	wantExact []string
	// wantContains requires each string to appear in at least one
	// rendered issue.
	wantContains []string
	// wantLen pins the exact number of rendered issues.
	wantLen int
}

// wrapGuardrail / wrapVerification splice the issues fragment into the
// minimal valid JSON for each struct (every strict field present, so a
// failure can only come from issues handling).
func wrapGuardrail(issues string) string {
	if issues == "" {
		return `{"guardrail":"rules","outcome":"block","severity":"high","summary":"verdict summary","verdict_at":"2026-01-02T03:04:05Z"}`
	}
	return `{"guardrail":"rules","outcome":"block","severity":"high","summary":"verdict summary","verdict_at":"2026-01-02T03:04:05Z","issues":` + issues + `}`
}

func wrapVerification(issues string) string {
	if issues == "" {
		return `{"ok":false,"severity":"medium","summary":"verification summary","verified_at":"2026-01-02T03:04:05Z"}`
	}
	return `{"ok":false,"severity":"medium","summary":"verification summary","verified_at":"2026-01-02T03:04:05Z","issues":` + issues + `}`
}

func checkIssues(t *testing.T, got []string, tc issuesTestCase) {
	t.Helper()
	if tc.wantLen >= 0 && len(got) != tc.wantLen {
		t.Fatalf("want %d issue(s), got %d (%v)", tc.wantLen, len(got), got)
	}
	if tc.wantExact != nil && !reflect.DeepEqual(got, tc.wantExact) {
		t.Fatalf("want %v, got %v", tc.wantExact, got)
	}
	for _, part := range tc.wantContains {
		found := false
		for _, g := range got {
			if strings.Contains(g, part) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no rendered issue contains %q (got %v)", part, got)
		}
	}
}

// TestTolerantIssuesShapes table-tests GuardrailVerdict and
// VerificationResult unmarshalling across the issues shapes seen in
// the wild: string arrays, object arrays, mixed arrays, null, absent,
// bare string, bare object, and the unknown-keys compact-JSON
// fallback. The rendered string for an issue object must contain every
// present part (description, message, file, line, severity), and the
// verdict's outcome/summary/severity must survive object issues.
func TestTolerantIssuesShapes(t *testing.T) {
	wrappers := []struct {
		name   string
		decode func(string) (issues []string, err error, outcome, summary string)
	}{
		{
			name: "guardrail verdict",
			decode: func(raw string) ([]string, error, string, string) {
				var v GuardrailVerdict
				err := json.Unmarshal([]byte(raw), &v)
				return v.Issues, err, v.Outcome, v.Summary
			},
		},
		{
			name: "verification result",
			decode: func(raw string) ([]string, error, string, string) {
				var r VerificationResult
				err := json.Unmarshal([]byte(raw), &r)
				return r.Issues, err, "", r.Summary
			},
		},
	}
	cases := []issuesTestCase{
		{
			name:      "issues as strings",
			issues:    `["a", "b"]`,
			wantExact: []string{"a", "b"},
			wantLen:   2,
		},
		{
			name:     "issues as objects with description/message/file/line/severity", 
			issues:   `[` + objectIssueFragment + `]`, 
			wantLen:  1, 
			// every present part must appear in the rendered string
			wantContains: []string{"high", "f.go", "7", "d1", "m1"},
		},
		{
			name:      "mixed array of strings and objects",
			issues:    `["plain string issue", ` + objectIssueFragment + `]`,
			wantLen:   2,
			wantExact: []string{"plain string issue", "[high] f.go:7: d1 / m1"},
			wantContains: []string{"plain string issue"},
		},
		{name: "empty array", issues: `[]`, wantLen: 0},
		{name: "null", issues: `null`, wantLen: 0},
		{name: "absent", issues: ``, wantLen: 0},
		{name: "bare string", issues: `"only one issue"`, wantExact: []string{"only one issue"}, wantLen: 1},
		{
			name:         "bare object",
			issues:       `{"severity":"low","message":"low problem","line":4}`,
			wantLen:      1,
			wantContains: []string{"low", "low problem", "4"},
		},
		{
			name:      "object with only unknown keys falls back to compact JSON",
			issues:    `{"wat":1}`, 
			wantExact: []string{`{"wat":1}`}, 
			wantLen:   1,
		},
	}
	for _, w := range wrappers {
		for _, tc := range cases {
			t.Run(w.name+" "+tc.name, func(t *testing.T) {
				issues, err, outcome, summary := w.decode(spliceIssues(w.name, tc.issues))
				if err != nil {
					if strings.Contains(err.Error(), legacyIssuesError) {
						t.Fatalf("legacy error text must never be produced: %v", err)
					}
					t.Fatalf("unmarshal shape %q as %s: %v", tc.issues, w.name, err)
				}
				checkIssues(t, issues, tc)
				if w.name == "guardrail verdict" {
					// outcome/summary/severity survive object issues
					if outcome != "block" || summary != "verdict summary" {
						t.Fatalf("outcome/summary not preserved: outcome=%q summary=%q", outcome, summary)
					}
				} else if summary != "verification summary" {
					t.Fatalf("summary not preserved: %q", summary)
				}
			})
		}
	}
}

func spliceIssues(kind, issues string) string {
	if kind == "guardrail verdict" {
		return wrapGuardrail(issues)
	}
	return wrapVerification(issues)
}

// TestTolerantIssuesStrictOtherFields: strict shape is kept for every
// field other than issues. A wrong type on another field (e.g.
// "outcome": 5) still fails the whole unmarshal — tolerant issues
// never weakened strictness elsewhere.
func TestTolerantIssuesStrictOtherFields(t *testing.T) {
	bad := []struct {
		name   string
		target interface{}
		raw    string
	}{
		{"outcome number", &GuardrailVerdict{},
			`{"guardrail":"rules","outcome":5,"summary":"s","verdict_at":"2026-01-02T03:04:05Z"}`},
		{"guardrail number", &GuardrailVerdict{},
			`{"guardrail":5,"outcome":"block","summary":"s","verdict_at":"2026-01-02T03:04:05Z"}`},
		{"ok string", &VerificationResult{},
			`{"ok":"yes","summary":"s","verified_at":"2026-01-02T03:04:05Z"}`},
		{"issues truncated", &GuardrailVerdict{},
			`{"guardrail":"rules","outcome":"block","summary":"s","verdict_at":"2026-01-02T03:04:05Z","issues":["a`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			err := json.Unmarshal([]byte(tc.raw), tc.target)
			if err == nil {
				t.Fatalf("want error for %s, got none", tc.name)
			}
			if strings.Contains(err.Error(), legacyIssuesError) {
				t.Fatalf("legacy error text must never be produced: %v", err)
			}
		})
	}

	// And the valid combination (right types everywhere, object
	// issues) must still decode.
	var v GuardrailVerdict
	if err := json.Unmarshal([]byte(wrapGuardrail(`[` + objectIssueFragment + `]`)), &v); err != nil {
		t.Fatalf("valid verdict with object issues must decode: %v", err)
	}
	if v.Outcome != "block" || v.Severity != "high" || v.Summary != "verdict summary" {
		t.Fatalf("fields not decoded: %+v", v)
	}
}

func issuesSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestIssuesRoundTrip: Marshal then Unmarshal yields the same []string
// for both types.
func TestIssuesRoundTrip(t *testing.T) {
	v := GuardrailVerdict{
		Guardrail: "rules",
		Outcome:   "block",
		Severity:  "high",
		Summary:   "s",
		Issues:    []string{"one", "two with spaces", "three"},
	}
	out, err := json.Marshal(&v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var w GuardrailVerdict
	if err := json.Unmarshal(out, &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !issuesSliceEqual(w.Issues, v.Issues) {
		t.Fatalf("round trip mismatch: want %v, got %v", v.Issues, w.Issues)
	}
	if w.Outcome != "block" || w.Summary != "s" || w.Severity != "high" {
		t.Fatalf("fields lost in round trip: %+v", w)
	}

	// VerificationResult round trip too.
	r := VerificationResult{OK: false, Summary: "s", Issues: []string{"v1", "v2"}}
	rout, err := json.Marshal(&r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var r2 VerificationResult
	if err := json.Unmarshal(rout, &r2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r2.OK || !issuesSliceEqual(r2.Issues, r.Issues) || r2.Summary != "s" {
		t.Fatalf("round trip mismatch: ok=%v issues=%v (want %v)", r2.OK, r2.Issues, r.Issues)
	}

	// Decoding an object-shaped issues fixture, marshalling, and
	// decoding again must be stable on the []string.
	var fx GuardrailVerdict
	if err := json.Unmarshal([]byte(wrapGuardrail(`[` + objectIssueFragment + `]`)), &fx); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	out2, err := json.Marshal(&fx)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	var fx2 GuardrailVerdict
	if err := json.Unmarshal(out2, &fx2); err != nil {
		t.Fatalf("redecode fixture: %v", err)
	}
	if !issuesSliceEqual(fx2.Issues, fx.Issues) {
		t.Fatalf("fixture round trip mismatch: want %v, got %v", fx.Issues, fx2.Issues)
	}
}

// padToTarget sizes the fixture so its total length is exactly target
// bytes: the placeholder is an ASCII-only token in the summary text,
// so the padding length is a pure byte arithmetic.
func padToTarget(t *testing.T, template, placeholder string, target int) string {
	t.Helper()
	base := len(template) - len(placeholder)
	if target <= base {
		t.Fatalf("target %d is not above the %d-byte template base", target, base)
	}
	return strings.Replace(template, placeholder, strings.Repeat("x", target-base), 1)
}

const incidentIssueTemplate = `    {
      "description": "DESC",
      "message": "MSG",
      "file": "FILE",
      "line": LINE,
      "severity": "SEV"
    }`

func incidentIssue(desc, msg, file string, line int, sev string) string {
	s := incidentIssueTemplate
	s = strings.Replace(s, "DESC", desc, 1)
	s = strings.Replace(s, "MSG", msg, 1)
	s = strings.Replace(s, "FILE", file, 1)
	s = strings.Replace(s, "LINE", strconv.Itoa(line), 1)
	return strings.Replace(s, "SEV", sev, 1)
}

// TestIncidentFixtureSizes is the regression test for the incident: a
// 3364-byte guardrail verdict and a 4740-byte verification result whose
// issues were arrays of objects (description, message, file, line,
// severity). The old decoder rejected both with the legacy error text;
// fixture generators here reproduce exactly those byte sizes (padded
// via the summary text) and both must now parse without error,
// preserve outcome/ok, and yield a non-empty []string.
func TestIncidentFixtureSizes(t *testing.T) {
	// Incident shape 1: guardrail verdict, exactly 3364 bytes.
	issue1 := incidentIssue(
		"guardrail step 1 failed the check",
		"detailed problem one was recorded by the judge",
		"internal/autonomous/tolerant_issues.go", 12, "high")
	issue2 := incidentIssue(
		"guardrail step 2 failed the check",
		"detailed problem two was recorded by the judge",
		"internal/autonomous/models.go", 874, "medium")
	guardrailTemplate := `{
  "guardrail": "rules",
  "outcome": "block",
  "severity": "high",
  "summary": "PADDING_PLACEHOLDER",
  "issues": [
` + issue1 + ",\n" + issue2 + `
  ],
  "verdict_at": "2026-10-12T04:05:06Z"
}`
	guardrailFixture := padToTarget(t, guardrailTemplate, "PADDING_PLACEHOLDER", 3364)
	if got := len(guardrailFixture); got != 3364 {
		t.Fatalf("guardrail fixture is %d bytes, want exactly 3364", got)
	}

	var v GuardrailVerdict
	err := json.Unmarshal([]byte(guardrailFixture), &v)
	if err != nil {
		if strings.Contains(err.Error(), legacyIssuesError) {
			t.Fatalf("legacy error text must never be produced: %v", err)
		}
		t.Fatalf("3364-byte incident guardrail verdict must parse, got: %v", err)
	}
	if v.Outcome != "block" || v.Severity != "high" {
		t.Fatalf("outcome/severity not preserved: outcome=%q severity=%q", v.Outcome, v.Severity)
	}
	if len(v.Summary) < 100 {
		t.Fatalf("padded summary unexpectedly short: %q", v.Summary)
	}
	if len(v.Issues) == 0 {
		t.Fatal("guardrail verdict issues must be non-empty")
	}
	for _, part := range []string{"high", "tolerant_issues.go", "12", "guardrail step 1", "detailed problem one"} {
		if !strings.Contains(v.Issues[0], part) {
			t.Fatalf("first issue %q missing part %q", v.Issues[0], part)
		}
	}

	// Incident shape 2: verification result, exactly 4740 bytes.
	vissue1 := incidentIssue(
		"verification step 1 found a regression",
		"the diff introduces a failing guardrail",
		"internal/autonomous/executor.go", 321, "high")
	vissue2 := incidentIssue(
		"verification step 2 flagged stale state",
		"checkpoint references a removed key",
		"internal/autonomous/store.go", 15, "low")
	verifTemplate := `{
  "ok": false,
  "severity": "high",
  "summary": "PADDING_PLACEHOLDER",
  "issues": [
` + vissue1 + ",\n" + vissue2 + `
  ],
  "verified_at": "2026-10-12T04:06:07Z"
}`
	verifFixture := padToTarget(t, verifTemplate, "PADDING_PLACEHOLDER", 4740)
	if got := len(verifFixture); got != 4740 {
		t.Fatalf("verification fixture is %d bytes, want exactly 4740", got)
	}

	var r VerificationResult
	verr := json.Unmarshal([]byte(verifFixture), &r)
	if verr != nil {
		if strings.Contains(verr.Error(), legacyIssuesError) {
			t.Fatalf("legacy error text must never be produced: %v", verr)
		}
		t.Fatalf("4740-byte incident verification result must parse, got: %v", verr)
	}
	if r.OK {
		t.Fatal("ok flag must stay false")
	}
	if len(r.Issues) == 0 {
		t.Fatal("verification result issues must be non-empty")
	}
	for _, part := range []string{"high", "executor.go", "321", "verification step 1", "failing guardrail"} {
		if !strings.Contains(r.Issues[0], part) {
			t.Fatalf("first issue %q missing part %q", r.Issues[0], part)
		}
	}
}
