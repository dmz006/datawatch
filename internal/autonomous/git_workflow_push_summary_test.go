package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGitWorkflowPushSummary(t *testing.T) {
	sha := "0123456789abcdef"

	tests := []struct {
		name string
		git  GitWorkflow
		want string
	}{
		{
			name: "nothing attempted",
			git:  GitWorkflow{},
			want: "",
		},
		{
			name: "pushed with long sha",
			git: GitWorkflow{
				PushState: PushStatePushed,
				PushedRef: "refs/heads/feature",
				PushedSHA: sha,
			},
			want: "pushed refs/heads/feature@0123456",
		},
		{
			name: "pushed with short value",
			git: GitWorkflow{
				PushState: PushStatePushed,
				PushedRef: "main",
				PushedSHA: "abcde",
			},
			want: "pushed main@abcde",
		},
		{
			name: "not pushed with warning",
			git: GitWorkflow{
				PushState:   PushStateNotPushed,
				PushWarning: "no origin remote",
			},
			want: "not pushed: no origin remote",
		},
		{
			name: "unrecognized state",
			git:  GitWorkflow{PushState: "something-else"},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.git.PushSummary(); got != tc.want {
				t.Errorf("PushSummary() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSummaryViewPushStatus pins the push outcome on the trimmed summary
// view (list reads + MCP default detail) without leaking any other git
// state, and confirms both keys are absent when no push was attempted.
func TestSummaryViewPushStatus(t *testing.T) {
	t.Run("not_pushed surfaces state and warning", func(t *testing.T) {
		prd := &PRD{
			ID:     "abcd1234",
			Status: PRDRunning,
			Git: GitWorkflow{
				Branch:      "automaton/abcd1234",
				URL:         "https://example.invalid/org/repo",
				PushState:   PushStateNotPushed,
				PushWarning: "push refused by daemon policy",
			},
		}
		out, err := json.Marshal(BuildSummaryView(prd))
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		s := string(out)
		if !strings.Contains(s, `"push_state":"not_pushed"`) {
			t.Errorf("summary view missing push_state=not_pushed: %s", s)
		}
		if !strings.Contains(s, `"push_warning":"push refused by daemon policy"`) {
			t.Errorf("summary view missing push_warning: %s", s)
		}
	})

	t.Run("pushed surfaces state", func(t *testing.T) {
		prd := &PRD{
			ID:     "abcd1234",
			Status: PRDRunning,
			Git: GitWorkflow{
				PushState: PushStatePushed,
				PushedRef: "main",
				PushedSHA: "0123456789abcdef",
			},
		}
		view := BuildSummaryView(prd)
		if view.PushState != PushStatePushed || view.PushWarning != "" {
			t.Errorf("PushState/Warning = %q/%q, want pushed/empty", view.PushState, view.PushWarning)
		}
		out, _ := json.Marshal(view)
		if !strings.Contains(string(out), `"push_state":"pushed"`) {
			t.Errorf("summary view missing push_state=pushed: %s", out)
		}
	})

	t.Run("nothing attempted omits both keys", func(t *testing.T) {
		out, _ := json.Marshal(BuildSummaryView(&PRD{ID: "abcd1234"}))
		if strings.Contains(string(out), "push_state") || strings.Contains(string(out), "push_warning") {
			t.Errorf("summary view leaked push fields for an untouched PRD: %s", out)
		}
	})
}
