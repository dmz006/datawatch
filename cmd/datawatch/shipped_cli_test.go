// Pure (no daemon) tests for the `shipped` CLI command's request building:
// the flags are resolved into the four shipped fields for a PRD or an
// Automation, the path picks the right surface, and no status field is ever
// part of the body.

package main

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestShippedCLI_BodyAndPath(t *testing.T) {
	a := &shippedRecordArgs{
		id:      "p 1", // a space verifies path escaping
		release: "v1.2.0",
		commit:  "c0ffee42",
		at:      "2026-11-20T09:30:00Z",
		source:  "merge_step",
	}
	body, err := a.body()
	if err != nil {
		t.Fatalf("body(): %v", err)
	}
	if body["merge_commit"] != "c0ffee42" || body["merged_in_release"] != "v1.2.0" || body["source"] != "merge_step" {
		t.Errorf("body shipped fields = %+v; want commit c0ffee42 release v1.2.0 source merge_step", body)
	}
	if body["merged_at"] != "2026-11-20T09:30:00Z" {
		t.Errorf("body merged_at = %v; want 2026-11-20T09:30:00Z", body["merged_at"])
	}
	// The request never carries a status; recording cannot move it.
	if _, ok := body["status"]; ok {
		t.Errorf("body must not contain a status key: %+v", body)
	}

	if got := a.path(); got != "/api/autonomous/prds/p%201/shipped" {
		t.Errorf("PRD path = %q; want /api/autonomous/prds/p%%201/shipped", got)
	}
	a.automation = true
	if got := a.path(); got != "/api/automations/p%201/shipped" {
		t.Errorf("automation path = %q; want /api/automations/p%%201/shipped", got)
	}
}

func TestShippedCLI_RequiresCommit(t *testing.T) {
	for _, commit := range []string{"", "   "} {
		a := &shippedRecordArgs{id: "p1", release: "v1", commit: commit}
		body, err := a.body()
		if err == nil {
			t.Errorf("commit %q: want an error, got body %+v", commit, body)
		}
		if body != nil {
			t.Errorf("commit %q: body should be nil on error, got %+v", commit, body)
		}
	}
}

func TestShippedCLI_SourceDefaultAndValidation(t *testing.T) {
	// Empty source defaults to backfill.
	a := &shippedRecordArgs{id: "p1", commit: "c1"}
	body, err := a.body()
	if err != nil {
		t.Fatalf("body(): %v", err)
	}
	if body["source"] != autonomous.ShippedSourceBackfill {
		t.Errorf("default source = %v; want %v", body["source"], autonomous.ShippedSourceBackfill)
	}

	// An unknown source is refused before any request is built.
	bad := &shippedRecordArgs{id: "p1", commit: "c1", source: "chore"}
	if _, err := bad.body(); err == nil {
		t.Errorf("source 'chore': want an error, got nil")
	}
}

func TestShippedCLI_MergedAtNowWhenUnset(t *testing.T) {
	before := time.Now().UTC().Add(-2 * time.Second)
	a := &shippedRecordArgs{id: "p1", commit: "c1"}
	body, err := a.body()
	if err != nil {
		t.Fatalf("body(): %v", err)
	}
	raw, ok := body["merged_at"].(string)
	if !ok {
		t.Fatalf("merged_at not a string: %+v", body)
	}
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("merged_at %q is not RFC3339: %v", raw, err)
	}
	now := time.Now().UTC().Add(2 * time.Second)
	if at.Before(before) || at.After(now) {
		t.Errorf("merged_at %v is not 'now' (want within [%v, %v])", at, before, now)
	}
}

func TestShippedCLI_MergedAtInvalid(t *testing.T) {
	a := &shippedRecordArgs{id: "p1", commit: "c1", at: "not-a-time"}
	if _, err := a.body(); err == nil {
		t.Errorf("at 'not-a-time': want an error, got nil")
	}
}
