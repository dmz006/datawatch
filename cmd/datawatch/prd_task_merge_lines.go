package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// prdTaskMergeLines renders one line per task that carries a merge state, from
// the daemon's prd-get reply: "merge: <task> <state> [<short commit>]". It
// reads the tasks from the stories of the full view or from the flat tasks
// list of the tasks view, and returns nil when no task has a merge state (a
// task that works in the PRD worktree has none).
func prdTaskMergeLines(prdJSON []byte) []string {
	type task struct {
		ID          string `json:"id"`
		MergeState  string `json:"merge_state"`
		MergeCommit string `json:"merge_commit"`
	}
	var v struct {
		Stories []struct {
			Tasks []task `json:"tasks"`
		} `json:"stories"`
		Tasks []task `json:"tasks"`
	}
	if err := json.Unmarshal(prdJSON, &v); err != nil {
		return nil
	}
	all := append([]task(nil), v.Tasks...)
	for _, s := range v.Stories {
		all = append(all, s.Tasks...)
	}
	var out []string
	for _, t := range all {
		if strings.TrimSpace(t.MergeState) == "" {
			continue
		}
		line := fmt.Sprintf("merge: %s %s", t.ID, t.MergeState)
		if c := t.MergeCommit; c != "" {
			if len(c) > 7 {
				c = c[:7]
			}
			line += " " + c
		}
		out = append(out, line)
	}
	return out
}
