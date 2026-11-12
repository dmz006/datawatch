package main

import (
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

func TestJudgeVerdictWithSession(t *testing.T) {
	verdictAt := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	base := autonomouspkg.GuardrailVerdict{
		Guardrail: "security",
		Outcome:   "block",
		Severity:  "high",
		Summary:   "hardcoded credential",
		Issues:    []string{"config.go:12 literal token"},
		VerdictAt: verdictAt,
		Category:  "content",
	}

	tests := []struct {
		name       string
		sessionID  string
		outputPath string
		wantID     string
		wantPath   string
	}{
		{
			name:       "session id and path are set",
			sessionID:  "sess-judge-1",
			outputPath: "/proj/.guardrail-output-abcd1234.json",
			wantID:     "sess-judge-1",
			wantPath:   "/proj/.guardrail-output-abcd1234.json",
		},
		{
			name:       "empty session id stays empty",
			sessionID:  "",
			outputPath: "/proj/.guardrail-output-abcd1234.json",
			wantID:     "",
			wantPath:   "/proj/.guardrail-output-abcd1234.json",
		},
		{
			name:       "empty path stays empty",
			sessionID:  "sess-judge-2",
			outputPath: "",
			wantID:     "sess-judge-2",
			wantPath:   "",
		},
		{
			name:       "both empty stay empty",
			sessionID:  "",
			outputPath: "",
			wantID:     "",
			wantPath:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := judgeVerdictWithSession(base, tc.sessionID, tc.outputPath)

			if got.JudgeSessionID != tc.wantID {
				t.Errorf("JudgeSessionID = %q, want %q", got.JudgeSessionID, tc.wantID)
			}
			if got.JudgeOutputPath != tc.wantPath {
				t.Errorf("JudgeOutputPath = %q, want %q", got.JudgeOutputPath, tc.wantPath)
			}

			// Every other field must survive the stamp unchanged.
			if got.Guardrail != base.Guardrail || got.Outcome != base.Outcome ||
				got.Severity != base.Severity || got.Summary != base.Summary ||
				got.Category != base.Category || !got.VerdictAt.Equal(base.VerdictAt) {
				t.Errorf("stamp changed non-judge fields: got %+v, want fields of %+v", got, base)
			}
			if len(got.Issues) != len(base.Issues) || got.Issues[0] != base.Issues[0] {
				t.Errorf("Issues = %v, want %v", got.Issues, base.Issues)
			}
		})
	}
}

func TestJudgeVerdictWithSession_DoesNotMutateInput(t *testing.T) {
	base := autonomouspkg.GuardrailVerdict{Outcome: "block", Category: "infra"}

	_ = judgeVerdictWithSession(base, "sess-x", "/proj/out.json")

	if base.JudgeSessionID != "" || base.JudgeOutputPath != "" {
		t.Errorf("input verdict was mutated: %+v", base)
	}
}
