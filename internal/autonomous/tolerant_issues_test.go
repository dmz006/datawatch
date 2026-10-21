package autonomous

import (
	"encoding/json"
	"testing"
)

func TestParseIssuesShapes(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{"absent", "", nil, false},
		{"null", "null", nil, false},
		{"whitespace", "  \n ", nil, false},
		{"empty string", `""`, nil, false},
		{"string array", `["a", "b"]`, []string{"a", "b"}, false},
		{"bare string", `"only one"`, []string{"only one"}, false},
		{"bare number", `42`, []string{"42"}, false},
		{"bare bool", `true`, []string{"true"}, false},
		{"object array", `[{"file":"a.go","line":42,"severity":"high","description":"boom"}]`,
			[]string{"[high] a.go:42: boom"}, false},
		{"mixed array", `["plain", {"message":"msg","file":"f.go","line":7}]`,
			[]string{"plain", "f.go:7: msg"}, false},
		{"fallback keys", `[{"issue":"from issue key"}]`, []string{"from issue key"}, false},
		{"text/detail keys", `[{"text":"t1"}, {"detail":"d1"}]`, []string{"t1", "d1"}, false},
		{"bare object", `{"severity":"low","detail":"low one"}`, []string{"[low] low one"}, false},
		{"unknown keys fall back to json", `[{"wat":1}]`, []string{`{"wat":1}`}, false},
		{"number line stays integral", `[{"file":"x.go","line":5,"message":"m"}]`, []string{"x.go:5: m"}, false},
		{"null element", `[null]`, []string{""}, false},
		{"malformed array", `["a`, nil, true},
		{"malformed object", `{`, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseIssues(json.RawMessage(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (issues=%v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("element %d: want %q, got %q", i, tc.want[i], got[i])
				}
			}
		})
	}
}

func TestGuardrailVerdictUnmarshalTolerantIssues(t *testing.T) {
	b := []byte(`{
		"guardrail": "rules",
		"outcome": "block",
		"severity": "high",
		"summary": "s",
		"issues": [{"file":"p/x.go","line":9,"severity":"high","description":"d"}, "plain issue"],
		"verdict_at": "2026-01-02T03:04:05Z",
		"raw_judge": "RAW"
	}`)
	var v GuardrailVerdict
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v.Guardrail != "rules" || v.Outcome != "block" {
		t.Fatalf("other fields not decoded strictly: %+v", v)
	}
	if v.Issues[0] != "[high] p/x.go:9: d" || v.Issues[1] != "plain issue" {
		t.Fatalf("issues: %v", v.Issues)
	}
	if v.RawJudge != "RAW" {
		t.Fatalf("raw_judge: %q", v.RawJudge)
	}

	// Marshal must emit issues as a plain string array.
	out, err := json.Marshal(&v)
	if err != nil {
		t.Fatal(err)
	}
	var round struct {
		Issues json.RawMessage `json:"issues"`
	}
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatal(err)
	}
	if len(round.Issues) < 2 || round.Issues[0] != '[' || round.Issues[1] != '"' {
		t.Fatalf("issues not serialized as string array: %s", round.Issues)
	}
}

func TestVerificationResultUnmarshalTolerantIssues(t *testing.T) {
	b := []byte(`{
		"ok": false,
		"severity": "medium",
		"summary": "s",
		"issues": {"message": "single object issue"},
		"verified_at": "2026-01-02T03:04:05Z",
		"raw_judge": "RAW"
	}`)
	var r VerificationResult
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(r.Issues) != 1 || r.Issues[0] != "single object issue" {
		t.Fatalf("issues: %v", r.Issues)
	}
	if r.RawJudge != "RAW" {
		t.Fatalf("raw_judge: %q", r.RawJudge)
	}
}

// Strictness on other fields must be preserved: a type error on a
// non-issues field still fails the whole unmarshal.
func TestUnmarshalStrictOnOtherFields(t *testing.T) {
	if err := json.Unmarshal([]byte(`{"guardrail": 5}`), &GuardrailVerdict{}); err == nil {
		t.Fatal("want error for non-string guardrail")
	}
	if err := json.Unmarshal([]byte(`{"ok": "yes"}`), &VerificationResult{}); err == nil {
		t.Fatal("want error for non-bool ok")
	}
}
