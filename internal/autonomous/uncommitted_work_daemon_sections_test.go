package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const uwdsBase = "# Agent\n\nBuild with go build.\n"

const uwdsBlock = "\n<!-- datawatch:platform-context v1 -->\nplatform text\n<!-- /datawatch:platform-context -->\n"

func uwdsRepo(t *testing.T) string {
	t.Helper()
	dir := uwRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(uwdsBase), 0o644); err != nil {
		t.Fatal(err)
	}
	uwGit(t, dir, "add", "AGENT.md")
	uwGit(t, dir, "commit", "-q", "-m", "agent")
	return dir
}

func uwdsWrite(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUncommittedWorkDaemonSectionOnlyChangeIsDropped(t *testing.T) {
	dir := uwdsRepo(t)
	uwdsWrite(t, dir, "AGENT.md", uwdsBase+uwdsBlock)
	got, err := DetectUncommittedWork(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want no entries, got %v", got)
	}
}

func TestUncommittedWorkDaemonSectionRealEditStillListed(t *testing.T) {
	dir := uwdsRepo(t)
	uwdsWrite(t, dir, "AGENT.md", uwdsBase+"Real operator edit.\n"+uwdsBlock)
	got, err := DetectUncommittedWork(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "AGENT.md" {
		t.Fatalf("want AGENT.md listed, got %v", got)
	}
}

func TestUncommittedWorkDaemonSectionUntrackedStillListed(t *testing.T) {
	dir := uwdsRepo(t)
	uwdsWrite(t, dir, "AGENT.md", uwdsBase+uwdsBlock)
	uwdsWrite(t, dir, "new.txt", "new\n")
	got, err := DetectUncommittedWork(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "new.txt" || got[0].Status != "??" {
		t.Fatalf("want only new.txt listed, got %v", got)
	}
}
