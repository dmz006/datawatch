package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
)

// hostAuditFixture wires a fresh audit log for the test and returns it. The
// log file lives under the package directory rather than the system temp
// dir, and is removed when the test ends.
func hostAuditFixture(t *testing.T) (*audit.Log, string) {
	t.Helper()
	dir, err := os.MkdirTemp(".", "hostaudit-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "audit.log")
	l, err := audit.NewAt(path)
	if err != nil {
		t.Fatal(err)
	}
	SetHostAuditLog(l)
	t.Cleanup(func() {
		SetHostAuditLog(nil)
		_ = l.Close()
	})
	return l, path
}

// hostAuditEntries returns every host-git-operation entry, oldest first.
func hostAuditEntries(t *testing.T, l *audit.Log) []audit.Entry {
	t.Helper()
	all, err := l.Read(audit.QueryFilter{Action: audit.ActionHostGitOperation})
	if err != nil {
		t.Fatal(err)
	}
	// Read returns most-recent first; reverse for chronological order.
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return all
}

func TestHostAuditRecordsFacadeOperations(t *testing.T) {
	withFakeForges(t)
	l, _ := hostAuditFixture(t)
	ctx := context.Background()
	host := GitHostConfig{Name: "corp-gitlab", Kind: hostKindGitLab, BaseURL: "https://gitlab.corp.example.test", CredentialSource: "vault"}

	if _, err := OpenChangeRequest(ctx, host, PROptions{Repo: "grp/proj", HeadBranch: "feat/x", Title: "T"}); err != nil {
		t.Fatal(err)
	}
	if _, err := FileIssue(ctx, host, IssueOptions{Repo: "grp/proj", Title: "Bug title"}); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishRelease(ctx, host, ReleaseOptions{Repo: "grp/proj", Tag: "v1.2.3"}); err != nil {
		t.Fatal(err)
	}

	got := hostAuditEntries(t, l)
	if len(got) != 3 {
		t.Fatalf("want 3 host audit entries, got %d", len(got))
	}
	want := []struct{ op, ref string }{
		{HostAuditMR, "feat/x"},
		{HostAuditIssue, "Bug title"},
		{HostAuditRelease, "v1.2.3"},
	}
	for i, w := range want {
		e := got[i]
		if e.Action != audit.ActionHostGitOperation {
			t.Errorf("entry %d action = %q", i, e.Action)
		}
		if e.Actor != "daemon" {
			t.Errorf("entry %d actor = %q", i, e.Actor)
		}
		if e.Subject != "grp/proj" {
			t.Errorf("entry %d subject = %q", i, e.Subject)
		}
		checks := map[string]string{
			"host":              "corp-gitlab",
			"kind":              hostKindGitLab,
			"operation":         w.op,
			"repo":              "grp/proj",
			"ref":               w.ref,
			"credential_source": "vault",
		}
		for k, v := range checks {
			if e.Details[k] != v {
				t.Errorf("entry %d details[%s] = %v, want %q", i, k, e.Details[k], v)
			}
		}
	}
}

func TestHostAuditRecordsTag(t *testing.T) {
	l, _ := hostAuditFixture(t)
	dir, err := os.MkdirTemp(".", "hostaudit-repo-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=test", "-c", "user.email=test@example.test", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if _, err := Run(ctx, dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Default host credential source is empty, which the audit reads as host_login.
	host := GitHostConfig{Name: "gh", Kind: hostKindGitHub}
	if err := CreateTagForHost(ctx, dir, "v9.9.9", "release", host); err != nil {
		t.Fatal(err)
	}
	got := hostAuditEntries(t, l)
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if got[0].Details["operation"] != HostAuditTag || got[0].Details["ref"] != "v9.9.9" {
		t.Errorf("tag entry details = %v", got[0].Details)
	}
	if got[0].Details["credential_source"] != "host_login" {
		t.Errorf("credential_source = %v, want host_login", got[0].Details["credential_source"])
	}
}

func TestHostAuditSkipsFailedOperations(t *testing.T) {
	fakes := withFakeForges(t)
	fakes[hostKindGitHub].err = errors.New("boom")
	l, _ := hostAuditFixture(t)
	host := GitHostConfig{Name: "github.com", Kind: hostKindGitHub}
	if _, err := FileIssue(context.Background(), host, IssueOptions{Repo: "o/r", Title: "x"}); err == nil {
		t.Fatal("expected error")
	}
	if got := hostAuditEntries(t, l); len(got) != 0 {
		t.Errorf("failed operation recorded %d entries", len(got))
	}
}

func TestHostAuditRedactsURLCredentials(t *testing.T) {
	l, logPath := hostAuditFixture(t)
	err := RecordHostAudit(context.Background(), HostAuditEvent{
		Host:   "h",
		Kind:   hostKindGit,
		Action: HostAuditMR,
		Repo:   "https://bob:hunter2pass@git.example.test/o/r.git",
		Ref:    "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := hostAuditEntries(t, l)
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	repo, _ := got[0].Details["repo"].(string)
	if strings.Contains(repo, "hunter2pass") || strings.Contains(repo, "bob:") {
		t.Errorf("repo not redacted: %q", repo)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2pass") {
		t.Error("credential value reached the audit file")
	}
}

func TestHostAuditRecordsNoToken(t *testing.T) {
	const token = "ghp_TESTONLY_0123456789abcdef"
	t.Setenv("GITHUB_TOKEN", token)
	t.Setenv("GITLAB_TOKEN", token)
	t.Setenv("DATAWATCH_GIT_TOKEN", token)
	withFakeForges(t)
	l, logPath := hostAuditFixture(t)
	ctx := context.Background()
	host := GitHostConfig{Name: "github.com", Kind: hostKindGitHub, CredentialSource: "broker"}
	if _, err := OpenChangeRequest(ctx, host, PROptions{Repo: "o/r", HeadBranch: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishRelease(ctx, host, ReleaseOptions{Repo: "o/r", Tag: "v1.0.0"}); err != nil {
		t.Fatal(err)
	}
	for _, e := range hostAuditEntries(t, l) {
		for k, v := range e.Details {
			if strings.Contains(k, "token") || fmt.Sprint(v) == token || strings.Contains(fmt.Sprint(v), token) {
				t.Errorf("entry detail %s leaks a token: %v", k, v)
			}
		}
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), token) {
		t.Error("token value recorded in audit file")
	}
}

func TestHostAuditNoLogWiredIsNoop(t *testing.T) {
	SetHostAuditLog(nil)
	if err := RecordHostAudit(context.Background(), HostAuditEvent{Action: HostAuditTag, Ref: "v1"}); err != nil {
		t.Errorf("unwired RecordHostAudit returned %v, want nil", err)
	}
}
