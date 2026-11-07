package autonomous

import (
	"context"
	"strings"
	"testing"
)

const tmdsBase = "# Agent\n\nBuild with go build.\n"

const tmdsBlock = "\n<!-- datawatch:platform-context v1 -->\nplatform text\n<!-- /datawatch:platform-context -->\n"

func tmdsFixture(t *testing.T) *mergeFixture {
	t.Helper()
	f := newMergeFixture(t, "", map[string]string{"AGENT.md": tmdsBase})
	f.addTask("t1", nil, "a.txt", "from a\n")
	f.seal()
	return f
}

func TestMergePreflightDaemonSectionOnlyChangePasses(t *testing.T) {
	f := tmdsFixture(t)
	writeMergeFile(t, f.prd.ProjectDir, "AGENT.md", tmdsBase+tmdsBlock)
	out, err := f.m.MergeTaskBranch(context.Background(), f.prd, f.task("t1"))
	if err != nil || out != MergeOutcomeMerged {
		t.Fatalf("outcome %s err %v", out, err)
	}
}

func TestMergePreflightDaemonSectionRealEditStillFails(t *testing.T) {
	f := tmdsFixture(t)
	writeMergeFile(t, f.prd.ProjectDir, "AGENT.md", tmdsBase+"Real operator edit.\n"+tmdsBlock)
	out, err := f.m.MergeTaskBranch(context.Background(), f.prd, f.task("t1"))
	if err == nil || out != MergeOutcomeDeferred {
		t.Fatalf("outcome %s err %v", out, err)
	}
	if !strings.Contains(err.Error(), "has uncommitted changes; not merging over them") {
		t.Fatalf("unexpected error text: %v", err)
	}
}
