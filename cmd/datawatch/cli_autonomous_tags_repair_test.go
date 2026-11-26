package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

const tagsRepairReply = `{"dry_run":true,"copies_read":2,"missing":1,"restore":1,"merge":0,"unresolved":0,"automata":1,"automata_left":0,"items":[{"tag_id":"t1","action":"restore","source":"copy","name":"made-up","automaton_ids":["a1"],"sessions":0}],"unused_copies":[]}`

// The command is a dry run unless --apply is given. --from sends the records
// of saved copies.
func TestTagsRepairRequest(t *testing.T) {
	dir := t.TempDir()
	list := filepath.Join(dir, "list.json")
	lines := filepath.Join(dir, "tags.jsonl")
	if err := os.WriteFile(list, []byte(`{"tags":[{"id":"t1","name":"made-up","count":2}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lines, []byte("{\"id\":\"t2\",\"name\":\"second\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		args   []string
		dryRun bool
		copies int
	}{
		{"plain is a dry run", []string{"tags", "repair"}, true, 0},
		{"two copies", []string{"tags", "repair", "--from", list, "--from", lines}, true, 2},
		{"apply", []string{"tags", "repair", "--apply", "--from", list}, false, 1},
		{"json", []string{"tag-list", "repair", "--json"}, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &captureDaemon{replyBody: tagsRepairReply}
			withCaptureServer(t, c)
			if err := runSubCmd(t, newAutonomousCmd(), tc.args...); err != nil {
				t.Fatalf("%v: %v", tc.args, err)
			}
			if c.gotMethod != http.MethodPost || c.gotPath != "/api/autonomous/tags/repair" {
				t.Fatalf("request = %s %s", c.gotMethod, c.gotPath)
			}
			raw := c.gotRaw
			var body struct {
				DryRun *bool            `json:"dry_run"`
				Copies []map[string]any `json:"copies"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("body %s: %v", raw, err)
			}
			if body.DryRun == nil || *body.DryRun != tc.dryRun || len(body.Copies) != tc.copies {
				t.Errorf("body = %s, want dry_run %v with %d copies", raw, tc.dryRun, tc.copies)
			}
		})
	}
}

func TestTagsRepairRefusesABadCopy(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("not a copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, from := range []string{bad, bad + ".missing"} {
		c := &captureDaemon{replyBody: tagsRepairReply}
		withCaptureServer(t, c)
		if err := runSubCmd(t, newAutonomousCmd(), "tags", "repair", "--from", from); err == nil {
			t.Errorf("%s: no error", from)
		}
		if c.gotMethod != "" {
			t.Errorf("%s: the daemon was called", from)
		}
	}
}
