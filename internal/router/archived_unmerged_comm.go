// Comm verb: "autonomous archived-unmerged" — archived Automata whose work
// branch still holds unmerged commits, and carrying that work forward.
//
//	autonomous archived-unmerged                              list them
//	autonomous archived-unmerged carry-forward <id> [force]   start a new draft on the branch
//
// The REST routes do the work; this verb only parses the arguments and renders
// the reply, title first. Nothing here deletes or merges a branch.

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	commArchivedUnmergedPath  = "/api/autonomous/archived-unmerged"
	commArchivedUnmergedUsage = "usage: autonomous archived-unmerged | autonomous archived-unmerged carry-forward <id> [force]"
	commArchivedUnmergedMax   = 10
)

// ArchivedUnmergedCommVerbs lists the verb names for the parity test.
func ArchivedUnmergedCommVerbs() []string { return []string{"archived-unmerged"} }

// handleArchivedUnmergedVerb implements "autonomous archived-unmerged".
func (r *Router) handleArchivedUnmergedVerb(args []string) {
	const label = "autonomous archived-unmerged"
	if len(args) == 0 {
		body, err := r.commGet(commArchivedUnmergedPath, nil)
		if err != nil {
			r.reply(label+" failed", err.Error())
			return
		}
		text, err := renderCommArchivedUnmerged(body)
		if err != nil {
			r.reply(label+" failed", err.Error())
			return
		}
		r.reply(label, text)
		return
	}
	verb := strings.ToLower(args[0])
	if verb != "carry-forward" && verb != "carry_forward" {
		r.reply(label, "unknown option "+args[0]+"\n"+commArchivedUnmergedUsage)
		return
	}
	rest := args[1:]
	force := false
	var id string
	for _, a := range rest {
		switch strings.ToLower(a) {
		case "force", "force=true":
			force = true
		default:
			if id != "" {
				r.reply(label, commArchivedUnmergedUsage)
				return
			}
			id = strings.TrimSpace(a)
		}
	}
	if id == "" {
		r.reply(label, commArchivedUnmergedUsage)
		return
	}
	payload := "{}"
	if force {
		payload = `{"force":true}`
	}
	body, err := r.commJSON("POST", commArchivedUnmergedPath+"/"+url.PathEscape(id)+"/carry-forward", payload)
	if err != nil {
		r.reply(label+" carry-forward failed", err.Error())
		return
	}
	var draft struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
		Git    struct {
			Branch string `json:"branch"`
		} `json:"git"`
	}
	if err := json.Unmarshal([]byte(body), &draft); err != nil {
		r.reply(label+" carry-forward failed", "unexpected response: "+err.Error())
		return
	}
	r.reply(label+" carry-forward", fmt.Sprintf("%s (%s)\nnew %s draft on branch %s; review it, then approve it to run",
		draft.Title, draft.ID, draft.Status, draft.Git.Branch))
}

// renderCommArchivedUnmerged renders the list body as a short text list,
// title first, up to ten entries.
func renderCommArchivedUnmerged(body string) (string, error) {
	var resp struct {
		Entries []struct {
			ID           string `json:"id"`
			Title        string `json:"title"`
			FormerStatus string `json:"former_status"`
			Branch       string `json:"branch"`
			Ahead        int    `json:"ahead"`
			State        string `json:"state"`
			Detail       string `json:"detail"`
			CarriedTo    string `json:"carried_to"`
		} `json:"archived_unmerged"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return "", fmt.Errorf("reading archived-unmerged list: %w", err)
	}
	if len(resp.Entries) == 0 {
		return "no archived Automata hold unmerged work", nil
	}
	shown := resp.Entries
	if len(shown) > commArchivedUnmergedMax {
		shown = shown[:commArchivedUnmergedMax]
	}
	lines := []string{fmt.Sprintf("%d of %d archived Automata with unmerged work (newest first):", len(shown), len(resp.Entries))}
	for _, e := range shown {
		state := fmt.Sprintf("%d commits ahead", e.Ahead)
		if e.State == "unknown" {
			state = "comparison failed: " + e.Detail
		}
		line := fmt.Sprintf("%s (%s) — branch %s, %s", e.Title, e.ID, e.Branch, state)
		if e.FormerStatus != "" {
			line += ", was " + e.FormerStatus
		}
		if e.CarriedTo != "" {
			line += ", carried to " + e.CarriedTo
		}
		lines = append(lines, line)
	}
	lines = append(lines, "carry one forward: autonomous archived-unmerged carry-forward <id>")
	return strings.Join(lines, "\n"), nil
}
