// CLI for archived Automata whose work branch still holds unmerged commits.
//
//	datawatch autonomous archived-unmerged [--json]
//	    → GET /api/autonomous/archived-unmerged
//	datawatch autonomous archived-unmerged carry-forward <id> [--force] [--json]
//	    → POST /api/autonomous/archived-unmerged/{id}/carry-forward
//
// Output puts the title first and the id after it. Neither command deletes or
// merges a branch; carry-forward only creates a new, unapproved draft.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

const cliArchivedUnmergedPath = "/api/autonomous/archived-unmerged"

// archivedUnmergedListBody is the list route body.
type archivedUnmergedListBody struct {
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
	Total int `json:"total"`
}

// renderArchivedUnmerged writes one block per entry, title first.
func renderArchivedUnmerged(w io.Writer, body []byte) error {
	var resp archivedUnmergedListBody
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	if len(resp.Entries) == 0 {
		_, _ = fmt.Fprintln(w, "no archived Automata hold unmerged work")
		return nil
	}
	for _, e := range resp.Entries {
		title := e.Title
		if title == "" {
			title = "(untitled)"
		}
		_, _ = fmt.Fprintf(w, "%s  (%s)\n", title, e.ID)
		state := fmt.Sprintf("%d commits ahead", e.Ahead)
		if e.State == "unknown" {
			state = "comparison failed: " + e.Detail
		}
		line := fmt.Sprintf("    branch %s, %s", e.Branch, state)
		if e.FormerStatus != "" {
			line += ", was " + e.FormerStatus
		}
		if e.CarriedTo != "" {
			line += ", already carried to " + e.CarriedTo
		}
		_, _ = fmt.Fprintln(w, line)
	}
	_, _ = fmt.Fprintf(w, "total: %d\n", len(resp.Entries))
	return nil
}

func printArchivedUnmergedJSON(w io.Writer, body []byte) error {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	_, _ = fmt.Fprintln(w, pretty.String())
	return nil
}

// newAutonomousArchivedUnmergedCmd builds `autonomous archived-unmerged`.
func newAutonomousArchivedUnmergedCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "archived-unmerged",
		Short: "List archived Automata whose branch still holds unmerged commits",
		Long: `List archived Automata whose work branch still holds commits that never reached
the base branch, newest archive first. Each shows the title, then the id, the
branch and how many commits are ahead. Git is only read; nothing is deleted or
merged.

  --json  Print the raw JSON response instead.

To keep the work, carry it into a new draft:
  datawatch autonomous archived-unmerged carry-forward <id>`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := daemonJSONBody(http.MethodGet, cliArchivedUnmergedPath, nil)
			if err != nil {
				return err
			}
			if asJSON {
				return printArchivedUnmergedJSON(cmd.OutOrStdout(), body)
			}
			return renderArchivedUnmerged(cmd.OutOrStdout(), body)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	cmd.AddCommand(newAutonomousArchivedUnmergedCarryCmd())
	return cmd
}

// newAutonomousArchivedUnmergedCarryCmd builds `archived-unmerged carry-forward <id>`.
func newAutonomousArchivedUnmergedCarryCmd() *cobra.Command {
	var (
		force  bool
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "carry-forward <id>",
		Short: "Start a new draft that carries an archived Automaton's unmerged work",
		Long: `Create a new draft with the archived Automaton's title, spec and branch so its
unmerged work is not lost. The draft is not approved and has run nothing; the
archived record and its branch are not changed, and nothing is merged or
deleted. It is refused when the record is not archived, has no branch, the
branch is gone or has nothing ahead, or a draft was already carried from it.

  --force  Carry again even though a draft was already carried.
  --json   Print the raw JSON response instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("id is required")
			}
			payload := map[string]any{}
			if force {
				payload["force"] = true
			}
			body, err := daemonJSONBody(http.MethodPost, cliArchivedUnmergedPath+"/"+url.PathEscape(id)+"/carry-forward", payload)
			if err != nil {
				return err
			}
			if asJSON {
				return printArchivedUnmergedJSON(cmd.OutOrStdout(), body)
			}
			var draft struct {
				ID            string `json:"id"`
				Title         string `json:"title"`
				Status        string `json:"status"`
				CarriedFromID string `json:"carried_from_id"`
				Git           struct {
					Branch string `json:"branch"`
				} `json:"git"`
			}
			if err := json.Unmarshal(body, &draft); err != nil {
				return fmt.Errorf("unexpected response: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  (%s)\n    new %s draft on branch %s, carried from %s; review it, then approve it to run\n",
				draft.Title, draft.ID, draft.Status, draft.Git.Branch, id)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "carry again even though a draft was already carried")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return cmd
}
