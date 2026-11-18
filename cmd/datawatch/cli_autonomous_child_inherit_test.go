// `datawatch autonomous prd-create --parent <id>` sends parent_prd_id in the
// same request body the REST, MCP and comm surfaces use.

package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func runChildInheritCreateCmd(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newAutonomousPRDCreateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestChildInheritParity_CLI_ForwardsParent(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"c1"}`}
	withCaptureServer(t, c)
	if err := runChildInheritCreateCmd(t, "--parent", " p1 ", "--project-dir", "/explicit", "--backend", "b", "child", "work"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if c.gotMethod != "POST" || c.gotPath != "/api/autonomous/prds" {
		t.Fatalf("request = %s %s", c.gotMethod, c.gotPath)
	}
	var body map[string]any
	if err := json.Unmarshal(c.gotRaw, &body); err != nil {
		t.Fatal(err)
	}
	if body["parent_prd_id"] != "p1" || body["project_dir"] != "/explicit" || body["backend"] != "b" || body["spec"] != "child work" {
		t.Fatalf("body = %v", body)
	}
}

func TestChildInheritParity_CLI_NoParentOmitsField(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"c1"}`}
	withCaptureServer(t, c)
	if err := runChildInheritCreateCmd(t, "plain work"); err != nil {
		t.Fatalf("err: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(c.gotRaw, &body)
	if _, ok := body["parent_prd_id"]; ok {
		t.Fatalf("parent_prd_id must be omitted: %v", body)
	}
}
