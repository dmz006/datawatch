// BL332 T42c — CLI for Discussion Scopes.
//
// Subcommands nested under `datawatch memory discussion`:
//
//	datawatch memory discussion list
//	datawatch memory discussion write <id> <content>
//	datawatch memory discussion recall <id> [query]
//	datawatch memory discussion wal <id> [--n 20]
//	datawatch memory discussion participants <id> [--set peer1,peer2]

package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func newDiscussionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discussion",
		Short: "Manage per-discussion shared memory scopes (BL332)",
		Long: `Discussion scopes let multiple peers share a named memory namespace.
Entries are stored under the discussion/<id> role and synced to participant
peers via push-on-write WAL sync.

Subcommands:
  list                     List all local discussion scope IDs
  write <id> <content>     Write an entry to a discussion scope
  recall <id> [query]      Recall entries from a discussion scope
  wal <id>                 Show the WAL history for a discussion scope
  participants <id>        Get or set participant peers for a discussion scope`,
	}
	cmd.AddCommand(
		newDiscussionListCmd(),
		newDiscussionWriteCmd(),
		newDiscussionRecallCmd(),
		newDiscussionWALCmd(),
		newDiscussionParticipantsCmd(),
	)
	return cmd
}

func newDiscussionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all local discussion scope IDs",
		RunE: func(_ *cobra.Command, _ []string) error {
			return daemonGet("/api/memory/discussion")
		},
	}
}

func newDiscussionWriteCmd() *cobra.Command {
	var summary string
	var role string
	cmd := &cobra.Command{
		Use:   "write <id> <content>",
		Short: "Write an entry to a discussion scope",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			content := args[1]
			body := map[string]any{
				"content": content,
			}
			if summary != "" {
				body["summary"] = summary
			}
			if role != "" {
				body["role"] = role
			}
			return daemonJSON(http.MethodPost, fmt.Sprintf("/api/memory/discussion/%s", url.PathEscape(id)), body)
		},
	}
	cmd.Flags().StringVar(&summary, "summary", "", "Optional short summary for search indexing")
	cmd.Flags().StringVar(&role, "role", "", "Optional role override (default: 'discussion')")
	return cmd
}

func newDiscussionRecallCmd() *cobra.Command {
	var topK int
	cmd := &cobra.Command{
		Use:   "recall <id> [query]",
		Short: "Recall entries from a discussion scope",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			q := url.Values{}
			if len(args) > 1 && args[1] != "" {
				q.Set("q", args[1])
			}
			if topK > 0 {
				q.Set("top_k", fmt.Sprintf("%d", topK))
			}
			path := fmt.Sprintf("/api/memory/discussion/%s", url.PathEscape(id))
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().IntVar(&topK, "top-k", 10, "Maximum results to return")
	return cmd
}

func newDiscussionWALCmd() *cobra.Command {
	var n int
	cmd := &cobra.Command{
		Use:   "wal <id>",
		Short: "Show the WAL history for a discussion scope",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			q := url.Values{}
			if n > 0 {
				q.Set("n", fmt.Sprintf("%d", n))
			}
			path := fmt.Sprintf("/api/memory/discussion/%s/wal", url.PathEscape(id))
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().IntVar(&n, "n", 20, "Number of WAL entries to return")
	return cmd
}

func newDiscussionParticipantsCmd() *cobra.Command {
	var setPeers string
	cmd := &cobra.Command{
		Use:   "participants <id>",
		Short: "Get or set participant peers for a discussion scope",
		Long: `Without --set: shows the current participant peer list.
With --set: replaces the participant list with comma-separated peer hostnames.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			path := fmt.Sprintf("/api/memory/discussion/%s/participants", url.PathEscape(id))
			if setPeers != "" {
				peers := splitDiscussionCSV(setPeers)
				body := map[string]any{"peers": peers}
				return daemonJSON(http.MethodPut, path, body)
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().StringVar(&setPeers, "set", "", "Comma-separated peer hostnames to set as participants")
	return cmd
}

// splitDiscussionCSV splits a comma-separated string into trimmed, non-empty elements.
func splitDiscussionCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
