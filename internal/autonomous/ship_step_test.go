package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func sgit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func shipRepo(t *testing.T) (work, remote string) {
	t.Helper()
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	work = filepath.Join(root, "work")
	sgit(t, root, "init", "--bare", "-b", "main", remote)
	sgit(t, root, "init", "-b", "main", work)
	sgit(t, work, "config", "user.name", "t")
	sgit(t, work, "config", "user.email", "t@t")
	sgit(t, work, "remote", "add", "origin", remote)
	if err := os.MkdirAll(filepath.Join(work, "cmd/datawatch"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package main\n\nvar Version = \"1.2.3\"\n"
	if err := os.WriteFile(filepath.Join(work, "cmd/datawatch/main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sgit(t, work, "add", ".")
	sgit(t, work, "commit", "-m", "release")
	return work, remote
}

func onPolicy() ShipPolicy {
	return ShipPolicy{Push: "on", Tag: "on", CIWatch: "off", TagFormat: "v{version}", Remote: "origin"}
}

func TestShipStep_OnPushesAndTags(t *testing.T) {
	work, remote := shipRepo(t)
	r := RunShipStep(context.Background(), work, "main", onPolicy())
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Tag != "v1.2.3" {
		t.Errorf("tag %q", r.Tag)
	}
	if sgit(t, remote, "tag", "-l", "v1.2.3") != "v1.2.3" {
		t.Error("tag not on remote")
	}
	if sgit(t, remote, "rev-parse", "main") != sgit(t, work, "rev-parse", "HEAD") {
		t.Error("branch not pushed")
	}
	if sgit(t, work, "cat-file", "-t", "refs/tags/v1.2.3") != "tag" {
		t.Error("tag must be annotated")
	}
}

func TestShipStep_IdempotentSameCommit(t *testing.T) {
	work, _ := shipRepo(t)
	if r := RunShipStep(context.Background(), work, "main", onPolicy()); r.Err != nil {
		t.Fatal(r.Err)
	}
	r := RunShipStep(context.Background(), work, "main", onPolicy())
	if r.Err != nil {
		t.Fatalf("rerun must succeed: %v", r.Err)
	}
	if !strings.Contains(strings.Join(r.Notes, " "), "already exists") {
		t.Errorf("rerun must record the existing tag: %v", r.Notes)
	}
}

func TestShipStep_TagOnDifferentCommitFails(t *testing.T) {
	work, remote := shipRepo(t)
	sgit(t, work, "tag", "-a", "v1.2.3", "-m", "old")
	_ = os.WriteFile(filepath.Join(work, "x.txt"), []byte("x"), 0o644)
	sgit(t, work, "add", ".")
	sgit(t, work, "commit", "-m", "later")
	r := RunShipStep(context.Background(), work, "main", onPolicy())
	if r.Err == nil || !strings.Contains(r.Err.Error(), "different commit") {
		t.Fatalf("want different-commit failure, got %v", r.Err)
	}
	if sgit(t, remote, "tag", "-l") != "" {
		t.Error("tag must not be pushed")
	}
}

func TestShipStep_DirtyTreeRefused(t *testing.T) {
	work, remote := shipRepo(t)
	_ = os.WriteFile(filepath.Join(work, "dirty.txt"), []byte("d"), 0o644)
	r := RunShipStep(context.Background(), work, "main", onPolicy())
	if r.Err == nil || !strings.Contains(r.Err.Error(), "dirty") {
		t.Fatalf("want dirty refusal, got %v", r.Err)
	}
	if sgit(t, remote, "branch", "--list") != "" {
		t.Error("nothing may be pushed from a dirty tree")
	}
}

func TestShipStep_ShadowRecordsCommandsOnly(t *testing.T) {
	work, remote := shipRepo(t)
	p := onPolicy()
	p.Push, p.Tag = "shadow", "shadow"
	_ = os.WriteFile(filepath.Join(work, "dirty.txt"), []byte("d"), 0o644) // shadow must not care
	r := RunShipStep(context.Background(), work, "main", p)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	want := []string{
		"git push origin refs/heads/main:refs/heads/main",
		"git tag -a v1.2.3 -m Release 1.2.3",
		"git push origin refs/tags/v1.2.3:refs/tags/v1.2.3",
	}
	if strings.Join(r.Commands, "|") != strings.Join(want, "|") {
		t.Errorf("commands %v", r.Commands)
	}
	if sgit(t, remote, "branch", "--list") != "" || sgit(t, work, "tag", "-l") != "" {
		t.Error("shadow must run nothing")
	}
}

func TestShipStep_OffSkips(t *testing.T) {
	work, remote := shipRepo(t)
	p := onPolicy()
	p.Push, p.Tag = "off", "off"
	r := RunShipStep(context.Background(), work, "main", p)
	if r.Err != nil || len(r.Commands) != 0 {
		t.Fatalf("off must skip: %+v", r)
	}
	if sgit(t, remote, "branch", "--list") != "" {
		t.Error("off must push nothing")
	}
}

func TestShipStep_PushFailureStopsBeforeTag(t *testing.T) {
	work, _ := shipRepo(t)
	sgit(t, work, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	r := RunShipStep(context.Background(), work, "main", onPolicy())
	if r.Err == nil {
		t.Fatal("want push failure")
	}
	if sgit(t, work, "tag", "-l") != "" {
		t.Error("no tag may be created after a failed push")
	}
}

func TestRunShipStep_RecordsFailureInTriage(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	SetShipConfig(config.AutonomousShipConfig{Push: "on", Tag: "on"})
	t.Cleanup(func() { SetShipConfig(config.AutonomousShipConfig{}) })

	work, _ := shipRepo(t)
	_ = os.WriteFile(filepath.Join(work, "dirty.txt"), []byte("d"), 0o644)
	prd, err := m.CreatePRD("spec", work, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd.Git.Branch = "main"
	res := m.runShipStep(prd)
	if res == nil || res.Err == nil {
		t.Fatalf("want failure result, got %+v", res)
	}
	snap, ok := m.store.SnapshotPRD(prd.ID)
	if !ok || snap.Triage == nil {
		t.Fatal("triage record missing")
	}
	found := false
	for _, n := range snap.Triage.Notes {
		if n.Source == shipTriageSource && strings.Contains(n.Text, "FAILED") && strings.Contains(n.Text, "dirty") {
			found = true
		}
	}
	if !found {
		t.Errorf("failure not recorded: %+v", snap.Triage.Notes)
	}
}
