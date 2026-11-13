package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderPlansMigrate(t *testing.T) {
	body := []byte(`{"note":"Preview only.","result":{"applied":false,
"would_create":[{"source":"arc","title":"Plans tab","project":"datawatch","link_kind":"automation","link_id":"aaaa1111","order":1}],
"would_link":[],"skipped_existing":[{"source":"registry-row","title":"X","reason":"already linked"}],
"conflicts":[{"source":"arc","title":"Y","reason":"title taken"}]}}`)
	var out bytes.Buffer
	if err := renderPlansMigrate(&out, body); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Preview: would create 1, would link 0, already there 1, conflicts 1",
		"would create: Plans tab (arc, datawatch, order 1) → automation aaaa1111",
		"left alone: X", "conflict: Y", "--apply"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if err := renderPlansMigrate(&out, []byte("nope")); err == nil {
		t.Error("bad JSON must be an error")
	}
}

func TestPlansMigrateCommandDefaultsToDryRun(t *testing.T) {
	cmd := newPlansMigrateCmd()
	f := cmd.Flags().Lookup("apply")
	if f == nil || f.DefValue != "false" {
		t.Fatalf("--apply must exist and default to false: %+v", f)
	}
}
