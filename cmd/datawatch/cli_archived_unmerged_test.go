package main

import (
	"bytes"
	"strings"
	"testing"
)

const aumCLIList = `{"archived_unmerged":[
 {"id":"done-1","title":"Parser rewrite","former_status":"completed","branch":"work/parser","ahead":3,"state":"ahead","carried_to":"draft-9"},
 {"id":"bad-2","title":"Cache layer","branch":"work/cache","state":"unknown","detail":"git failed"}
],"total":2}`

func runAumCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newAutonomousArchivedUnmergedCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestArchivedUnmergedCLIListTitleFirst(t *testing.T) {
	c := &captureDaemon{replyBody: aumCLIList}
	withCaptureServer(t, c)
	out, err := runAumCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	if c.gotMethod != "GET" || c.gotPath != "/api/autonomous/archived-unmerged" {
		t.Errorf("request = %s %s", c.gotMethod, c.gotPath)
	}
	for _, want := range []string{"Parser rewrite  (done-1)", "3 commits ahead", "was completed", "already carried to draft-9", "Cache layer  (bad-2)", "comparison failed: git failed", "total: 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "Parser rewrite") > strings.Index(out, "done-1") {
		t.Errorf("title must come before the id:\n%s", out)
	}
}

func TestArchivedUnmergedCLIEmptyAndJSON(t *testing.T) {
	c := &captureDaemon{replyBody: `{"archived_unmerged":[],"total":0}`}
	withCaptureServer(t, c)
	out, err := runAumCmd(t)
	if err != nil || !strings.Contains(out, "no archived Automata hold unmerged work") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	out, err = runAumCmd(t, "--json")
	if err != nil || !strings.Contains(out, `"total": 0`) {
		t.Fatalf("json out=%q err=%v", out, err)
	}
}

func TestArchivedUnmergedCLICarryForward(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"draft-1","title":"Parser rewrite","status":"draft","git":{"branch":"work/parser"}}`}
	withCaptureServer(t, c)
	out, err := runAumCmd(t, "carry-forward", "done-1", "--force")
	if err != nil {
		t.Fatal(err)
	}
	if c.gotMethod != "POST" || c.gotPath != "/api/autonomous/archived-unmerged/done-1/carry-forward" || !strings.Contains(string(c.gotRaw), `"force":true`) {
		t.Errorf("request = %s %s %s", c.gotMethod, c.gotPath, c.gotRaw)
	}
	if !strings.HasPrefix(out, "Parser rewrite  (draft-1)") || !strings.Contains(out, "work/parser") {
		t.Errorf("output = %q", out)
	}
}

func TestArchivedUnmergedCLIRefusals(t *testing.T) {
	if _, err := runAumCmd(t, "carry-forward"); err == nil {
		t.Error("carry-forward without an id must fail")
	}
	c := &captureDaemon{replyCode: 409, replyBody: "carry forward: branch work/x no longer exists"}
	withCaptureServer(t, c)
	if _, err := runAumCmd(t, "carry-forward", "x"); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("daemon refusal not surfaced: %v", err)
	}
}
