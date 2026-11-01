// set_git — optional push_on_complete override, applied via
// Manager.SetPRDGitPush (the fake below mirrors its validation:
// non-empty value must parse as never|branch|default_branch; "" clears).

package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/git"
)

// setGitPushTestOrch embeds fakeOrchAutonomous and records setter calls.
type setGitPushTestOrch struct {
	fakeOrchAutonomous
	gitCalls  int
	pushCalls []string
}

func (o *setGitPushTestOrch) SetPRDGit(prdID string, autoPR bool, baseBranch string) (any, error) {
	o.gitCalls++
	return map[string]any{"id": prdID, "git": map[string]any{"auto_pr": autoPR, "base_branch": baseBranch}}, nil
}
func (o *setGitPushTestOrch) SetPRDGitPush(prdID, policy string) (any, error) {
	want := strings.TrimSpace(policy)
	if want != "" {
		if _, ok := git.ParsePolicy(want); !ok {
			return nil, fmt.Errorf("invalid push-on-complete policy %q (allowed: never, branch, default_branch)", policy)
		}
	}
	o.pushCalls = append(o.pushCalls, want)
	return map[string]any{"id": prdID, "git": map[string]any{"push_on_complete": want}}, nil
}

func setGitPushPost(t *testing.T, orch *setGitPushTestOrch, body string) *httptest.ResponseRecorder {
	t.Helper()
	srv := &Server{autonomousMgr: orch}
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/p1/set_git", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr, req)
	return rr
}

func TestSetGitPushOnComplete_ValidPolicyApplied(t *testing.T) {
	orch := &setGitPushTestOrch{}
	rr := setGitPushPost(t, orch, `{"auto_pr":true,"base_branch":"main","push_on_complete":"branch"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(orch.pushCalls) != 1 || orch.pushCalls[0] != "branch" {
		t.Errorf("pushCalls = %v, want [branch]", orch.pushCalls)
	}
	if orch.gitCalls != 1 {
		t.Errorf("gitCalls = %d, want 1", orch.gitCalls)
	}
}

func TestSetGitPushOnComplete_WhitespaceTrimmed(t *testing.T) {
	orch := &setGitPushTestOrch{}
	rr := setGitPushPost(t, orch, `{"auto_pr":false,"base_branch":"","push_on_complete":"  default_branch "}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(orch.pushCalls) != 1 || orch.pushCalls[0] != "default_branch" {
		t.Errorf("pushCalls = %v, want [default_branch]", orch.pushCalls)
	}
}

func TestSetGitPushOnComplete_EmptyClearsOverride(t *testing.T) {
	orch := &setGitPushTestOrch{}
	rr := setGitPushPost(t, orch, `{"auto_pr":false,"base_branch":"main","push_on_complete":""}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(orch.pushCalls) != 1 || orch.pushCalls[0] != "" {
		t.Errorf("pushCalls = %v, want [\"\"] (clear)", orch.pushCalls)
	}
	if orch.gitCalls != 1 {
		t.Errorf("gitCalls = %d, want 1", orch.gitCalls)
	}
}

func TestSetGitPushOnComplete_InvalidRejectedBeforeGitWrite(t *testing.T) {
	orch := &setGitPushTestOrch{}
	rr := setGitPushPost(t, orch, `{"auto_pr":true,"base_branch":"main","push_on_complete":"sometimes"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "allowed: never, branch, default_branch") {
		t.Errorf("want allowed-values message in body, got %s", rr.Body.String())
	}
	if len(orch.pushCalls) != 0 {
		t.Errorf("pushCalls = %v, want none applied", orch.pushCalls)
	}
	if orch.gitCalls != 0 {
		t.Errorf("gitCalls = %d, want 0 — invalid push policy must reject before git fields are written", orch.gitCalls)
	}
}

func TestSetGitPushOnComplete_OmittedLeavesOverrideUnchanged(t *testing.T) {
	orch := &setGitPushTestOrch{}
	rr := setGitPushPost(t, orch, `{"auto_pr":false,"base_branch":"main"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(orch.pushCalls) != 0 {
		t.Errorf("pushCalls = %v, want none (field omitted)", orch.pushCalls)
	}
	if orch.gitCalls != 1 {
		t.Errorf("gitCalls = %d, want 1", orch.gitCalls)
	}
}
