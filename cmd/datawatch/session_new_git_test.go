package main

import (
	"encoding/json"
	"testing"
)

// TestSessionNewStartBodyGitFlag verifies the `--git` flag lands in the
// POST /api/sessions/start request body as `git: true`, and that the field
// is absent (omitempty) when the flag is not set.
func TestSessionNewStartBodyGitFlag(t *testing.T) {
	body, err := sessionNewStartBody(
		"do the thing", "/tmp/proj", "my-sess", "", "", "",
		false, "", false, false, nil, true,
	)
	if err != nil {
		t.Fatalf("sessionNewStartBody: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body is not valid JSON: %v: %s", err, body)
	}
	if got["git"] != true {
		t.Errorf("expected git:true in request body, got %v (body: %s)", got["git"], body)
	}
	if got["task"] != "do the thing" {
		t.Errorf("expected task field preserved, got %v", got["task"])
	}
	if got["project_dir"] != "/tmp/proj" {
		t.Errorf("expected project_dir field preserved, got %v", got["project_dir"])
	}
}

func TestSessionNewStartBodyGitAbsentByDefault(t *testing.T) {
	body, err := sessionNewStartBody(
		"do the thing", "/tmp/proj", "", "", "", "",
		false, "", false, false, nil, false,
	)
	if err != nil {
		t.Fatalf("sessionNewStartBody: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body is not valid JSON: %v: %s", err, body)
	}
	if _, ok := got["git"]; ok {
		t.Errorf("git should be omitted by default (omitempty), body: %s", body)
	}
}
