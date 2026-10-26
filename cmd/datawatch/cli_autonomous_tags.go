// CLI commands for the Automata tag registry and per-Automaton tag
// assignment: autonomous tag-list|tag-create|tag-rename|tag-move|tag-color|
// tag-delete and prd-set-tags. Each maps onto the /api/autonomous/tags REST
// endpoints through daemonJSON.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

const autonomousTagsPath = "/api/autonomous/tags"

func tagPath(id string) string { return autonomousTagsPath + "/" + url.PathEscape(id) }

func newAutonomousTagListCmd() *cobra.Command {
	var includeSub, counts bool
	cmd := &cobra.Command{
		Use:     "tag-list",
		Aliases: []string{"tags"},
		Short:   "List Automata tags (a tree via parent_id) with the number of Automata carrying each",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			q := url.Values{}
			if includeSub {
				q.Set("include_sub", "true")
			}
			if counts {
				q.Set("counts", "true")
			}
			path := autonomousTagsPath
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			return daemonJSON(http.MethodGet, path, nil)
		},
	}
	cmd.Flags().BoolVar(&includeSub, "include-sub", false, "roll each count up to include descendant tags")
	cmd.Flags().BoolVar(&counts, "counts", false, "add subtree and direct counts (Automata and sessions) per tag, plus an untagged total")
	return cmd
}

func newAutonomousAttentionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attention",
		Short: "List the Automata that need attention (blocked, needs_review and paused by default, plus any with a task held for review)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return daemonJSON(http.MethodGet, "/api/autonomous/attention", nil)
		},
	}
}

func newAutonomousTagCreateCmd() *cobra.Command {
	var parent, color, description string
	cmd := &cobra.Command{
		Use:   "tag-create <name>",
		Short: "Create an Automata tag, optionally nested under a parent tag",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, autonomousTagsPath, map[string]string{
				"name": args[0], "parent_id": parent, "color": color, "description": description,
			})
		},
	}
	cmd.Flags().StringVar(&parent, "parent", "", "parent tag ID (empty = root tag)")
	cmd.Flags().StringVar(&color, "color", "", "CSS color, e.g. #6366f1")
	cmd.Flags().StringVar(&description, "description", "", "tag description")
	return cmd
}

func newAutonomousTagRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tag-rename <id> <new-name>",
		Short: "Rename an Automata tag",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPatch, tagPath(args[0]), map[string]string{"name": args[1]})
		},
	}
}

func newAutonomousTagMoveCmd() *cobra.Command {
	var parent string
	cmd := &cobra.Command{
		Use:   "tag-move <id> --parent <new-parent-id|root>",
		Short: "Move an Automata tag under a new parent (root = top level); refused if it would make the tag its own ancestor",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("parent") {
				return cmd.Usage()
			}
			if strings.EqualFold(parent, "root") {
				parent = ""
			}
			return daemonJSON(http.MethodPatch, tagPath(args[0]), map[string]string{"parent_id": parent})
		},
	}
	cmd.Flags().StringVar(&parent, "parent", "", "new parent tag ID, or root")
	return cmd
}

func newAutonomousTagColorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tag-color <id> <color>",
		Short: "Set an Automata tag's color (empty string clears it)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPatch, tagPath(args[0]), map[string]string{"color": args[1]})
		},
	}
}

func newAutonomousTagDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tag-delete <id>",
		Short: "Delete an Automata tag; refused while it has child tags or is assigned to Automata",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, tagPath(args[0]), nil)
		},
	}
}

func newAutonomousPRDSetTagsCmd() *cobra.Command {
	var tags, primary string
	cmd := &cobra.Command{
		Use:   "prd-set-tags <prd-id> [--tags <id1,id2,...>] [--primary <tag-id>]",
		Short: "Replace the tags on an Automaton (an empty --tags value clears them) and/or set its primary tag (an empty --primary clears it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tagsSet := cmd.Flags().Changed("tags")
			primarySet := cmd.Flags().Changed("primary")
			if !tagsSet && !primarySet {
				return cmd.Usage()
			}
			body := map[string]any{}
			if tagsSet {
				ids := []string{}
				for _, s := range strings.Split(tags, ",") {
					if s = strings.TrimSpace(s); s != "" {
						ids = append(ids, s)
					}
				}
				body["tag_ids"] = ids
			}
			if primarySet {
				body["primary_tag_id"] = primary
			}
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(args[0])+"/set_tags", body)
		},
	}
	cmd.Flags().StringVar(&tags, "tags", "", "comma-separated tag IDs (empty clears)")
	cmd.Flags().StringVar(&primary, "primary", "", "primary tag ID, one of the Automaton's tags, used for sorting (empty clears)")
	return cmd
}

// newSessionTagsCmd sets the tags a session carries itself. Inherited tags
// come from the owning Automaton and are not set here.
func newSessionTagsCmd() *cobra.Command {
	var tags string
	cmd := &cobra.Command{
		Use:   "tags <session-id> --tags <id1,id2,...>",
		Short: "Replace the tags a session carries itself (an empty --tags value clears them)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("tags") {
				return cmd.Usage()
			}
			return daemonJSON(http.MethodPost, "/api/sessions/tags",
				map[string]any{"id": args[0], "tag_ids": parseTagIDList(tags)})
		},
	}
	cmd.Flags().StringVar(&tags, "tags", "", "comma-separated tag IDs (empty clears)")
	return cmd
}

// parseTagIDList splits a comma-separated tag-ID list, dropping blanks. An
// empty list is returned as a non-nil slice so it serialises as [].
func parseTagIDList(list string) []string {
	ids := []string{}
	for _, s := range strings.Split(list, ",") {
		if s = strings.TrimSpace(s); s != "" {
			ids = append(ids, s)
		}
	}
	return ids
}

// newAutonomousTagRegistryCmd prints the tag registry status: the registry
// object of GET /api/autonomous/tags, without the tag list.
func newAutonomousTagRegistryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tag-registry",
		Short: "Show the Automata tag registry status (role, keeper, staleness, revision, notes and last error)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			raw, err := daemonJSONBody(http.MethodGet, autonomousTagsPath, nil)
			if err != nil {
				return err
			}
			registry, err := registryObjectFromTagsReply(raw)
			if err != nil {
				return err
			}
			prettyPrint(registry)
			return nil
		},
	}
}

// newAutonomousTagRegistryDismissCmd dismisses one registry note.
func newAutonomousTagRegistryDismissCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tag-registry-dismiss <note-id>",
		Short: "Dismiss one tag registry note (a merge or removal notice from the keeper)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, autonomousTagsPath+"/registry/notes/"+url.PathEscape(args[0])+"/dismiss", nil)
		},
	}
}

// registryObjectFromTagsReply extracts the "registry" object from a
// GET /api/autonomous/tags reply.
func registryObjectFromTagsReply(body []byte) ([]byte, error) {
	var reply struct {
		Registry json.RawMessage `json:"registry"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return nil, fmt.Errorf("decode tags reply: %w", err)
	}
	if len(reply.Registry) == 0 {
		return nil, fmt.Errorf("tags reply has no registry object")
	}
	return reply.Registry, nil
}
