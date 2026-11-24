package main

import (
	"reflect"
	"testing"
)

func TestPauseBody(t *testing.T) {
	got, err := pauseBody("", "release freeze", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"scope": "global", "reason": "release freeze", "actor": "cli"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("global body = %v", got)
	}
	got, err = pauseBody("web", "review", []string{"laptop"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want = map[string]any{"scope": "project", "project": "web", "reason": "review", "actor": "cli", "peers": []string{"laptop"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("project body = %v", got)
	}
	got, _ = pauseBody("", "x", nil, true)
	if got["all_peers"] != true {
		t.Fatalf("all peers body = %v", got)
	}
}

func TestPauseBodyNeedsAReason(t *testing.T) {
	if _, err := pauseBody("", "  ", nil, false); err == nil {
		t.Fatal("a pause without a reason must be refused before it is sent")
	}
}

func TestResumeBody(t *testing.T) {
	got, err := resumeBody("", false, "all clear", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"scope": "global", "note": "all clear", "actor": "cli"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("global resume body = %v", got)
	}
	got, _ = resumeBody("web", false, "", nil, false)
	if !reflect.DeepEqual(got, map[string]any{"scope": "project", "project": "web", "actor": "cli"}) {
		t.Fatalf("project resume body = %v", got)
	}
	got, _ = resumeBody("", true, "", nil, true)
	if !reflect.DeepEqual(got, map[string]any{"all": true, "actor": "cli", "all_peers": true}) {
		t.Fatalf("resume-all body = %v", got)
	}
	if _, err := resumeBody("web", true, "", nil, false); err == nil {
		t.Fatal("--all with --project is ambiguous and must be refused")
	}
}

func TestPauseCommandsExist(t *testing.T) {
	p, r := newPauseCmd(), newResumeCmd()
	if p.Use == "" || r.Use == "" {
		t.Fatal("commands must be named")
	}
	found := false
	for _, c := range p.Commands() {
		if c.Name() == "status" {
			found = true
		}
	}
	if !found {
		t.Fatal("pause status must exist")
	}
	for _, f := range []string{"project", "reason", "peer", "peers"} {
		if p.Flags().Lookup(f) == nil {
			t.Errorf("pause lacks --%s", f)
		}
	}
	for _, f := range []string{"project", "all", "note", "peer", "peers"} {
		if r.Flags().Lookup(f) == nil {
			t.Errorf("resume lacks --%s", f)
		}
	}
}
