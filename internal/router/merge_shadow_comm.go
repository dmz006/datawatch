// autonomous merge-shadow over the chat channel: one summary line of the
// matched and different merge comparison counts per release size, read from
// the daemon's merge shadow endpoint.
//
//	autonomous merge-shadow
package router

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	mergeShadowCommPath = "/api/autonomous/merge/shadow"
	// mergeShadowCommEmpty is the reply when no comparison is stored. An
	// explicit verb always gets an answer.
	mergeShadowCommEmpty = "No merge comparisons yet"
)

// mergeShadowCommSizes is the order release sizes are listed in.
var mergeShadowCommSizes = []string{"patch", "minor", "major", "none"}

// MergeShadowCommVerbs lists the merge shadow comm verbs, for the parity test.
func MergeShadowCommVerbs() []string { return []string{"merge-shadow"} }

// mergeShadowCommSummary builds the one summary line from the merge shadow
// endpoint's JSON. It returns the empty-state line when no comparison is
// stored.
func mergeShadowCommSummary(raw string) (string, error) {
	var v struct {
		Projects []struct {
			Comparisons []json.RawMessage `json:"comparisons"`
			Counts      map[string]struct {
				Matched   int `json:"matched"`
				Different int `json:"different"`
			} `json:"counts"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", fmt.Errorf("the daemon's reply could not be read: %w", err)
	}
	type tally struct{ matched, different int }
	sums := map[string]tally{}
	total := 0
	for _, p := range v.Projects {
		total += len(p.Comparisons)
		for size, c := range p.Counts {
			t := sums[size]
			t.matched += c.Matched
			t.different += c.Different
			sums[size] = t
		}
	}
	if total == 0 {
		return mergeShadowCommEmpty, nil
	}
	parts := make([]string, 0, len(mergeShadowCommSizes))
	for _, size := range mergeShadowCommSizes {
		t := sums[size]
		parts = append(parts, fmt.Sprintf("%s %d matched, %d different", size, t.matched, t.different))
	}
	return "Merge shadow: " + strings.Join(parts, "; "), nil
}

// replyMergeShadow answers the merge-shadow verb.
func (r *Router) replyMergeShadow(args []string) {
	const label = "autonomous merge-shadow"
	if len(args) > 1 {
		r.reply(label+" failed", "usage: autonomous merge-shadow")
		return
	}
	out, err := r.commGet(mergeShadowCommPath, nil)
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	line, err := mergeShadowCommSummary(out)
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	r.reply(label, line)
}
