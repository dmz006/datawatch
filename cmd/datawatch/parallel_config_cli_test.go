package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPRDTaskMergeLinesFromStories(t *testing.T) {
	body := []byte(`{"stories":[{"tasks":[
		{"id":"t1","merge_state":"merged","merge_commit":"0123456789abcdef"},
		{"id":"t2","merge_state":"conflict"},
		{"id":"t3"}]}]}`)
	got := prdTaskMergeLines(body)
	want := []string{"merge: t1 merged 0123456", "merge: t2 conflict"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, want %q", got, want)
	}
}

func TestPRDTaskMergeLinesFromFlatTasks(t *testing.T) {
	got := prdTaskMergeLines([]byte(`{"tasks":[{"id":"t9","merge_state":"pending"}]}`))
	if len(got) != 1 || got[0] != "merge: t9 pending" {
		t.Fatalf("lines = %q", got)
	}
}

func TestPRDTaskMergeLinesNoneAndBadJSON(t *testing.T) {
	if got := prdTaskMergeLines([]byte(`{"stories":[{"tasks":[{"id":"t1"}]}]}`)); got != nil {
		t.Fatalf("lines = %q, want none", got)
	}
	if got := prdTaskMergeLines([]byte(`not json`)); got != nil {
		t.Fatalf("lines = %q, want none", got)
	}
}

func TestConfigRoundTripParallelKeys(t *testing.T) {
	full := map[string]interface{}{"autonomous": map[string]interface{}{"parallel": map[string]interface{}{
		"worktree_per_task": true, "merge_conflict_policy": "block", "merge_conflict_model": "",
	}}}
	if got := lookupConfigKey(full, "autonomous.parallel.merge_conflict_policy"); got != "block" {
		t.Fatalf("policy = %v", got)
	}
	if got := lookupConfigKey(full, "autonomous.parallel.worktree_per_task"); got != true {
		t.Fatalf("worktree_per_task = %v", got)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(configureRequestBody("autonomous.parallel.worktree_per_task", "false")), &m); err != nil {
		t.Fatal(err)
	}
	if m["autonomous.parallel.worktree_per_task"] != false {
		t.Fatalf("body = %v", m)
	}
	if err := json.Unmarshal([]byte(configureRequestBody("autonomous.parallel.merge_conflict_policy", "triage")), &m); err != nil {
		t.Fatal(err)
	}
	if m["autonomous.parallel.merge_conflict_policy"] != "triage" {
		t.Fatalf("body = %v", m)
	}
}

func TestConfigSetHelpNamesParallelKeys(t *testing.T) {
	long := newConfigSetCmd().Long
	for _, k := range []string{"autonomous.parallel.worktree_per_task", "autonomous.parallel.merge_conflict_policy", "autonomous.parallel.merge_conflict_model"} {
		if !strings.Contains(long, k) {
			t.Errorf("config set help lacks %s", k)
		}
	}
}

func TestConfigParallelMergeRetrySeconds(t *testing.T) {
	// help names the key
	if !strings.Contains(newConfigSetCmd().Long, "autonomous.parallel.merge_retry_seconds") {
		t.Error("config set help lacks autonomous.parallel.merge_retry_seconds")
	}
	// default: read back from GET /api/config
	full := map[string]interface{}{"autonomous": map[string]interface{}{"parallel": map[string]interface{}{
		"merge_retry_seconds": float64(60),
	}}}
	if got := lookupConfigKey(full, "autonomous.parallel.merge_retry_seconds"); got != float64(60) {
		t.Fatalf("merge_retry_seconds = %v", got)
	}
	// set and invalid: whole numbers, including 0 and a negative, go out as JSON numbers
	for val, want := range map[string]float64{"0": 0, "90": 90, "-5": -5} {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(configureRequestBody("autonomous.parallel.merge_retry_seconds", val)), &m); err != nil {
			t.Fatal(err)
		}
		if m["autonomous.parallel.merge_retry_seconds"] != want {
			t.Fatalf("body for %s = %v", val, m)
		}
	}
}
