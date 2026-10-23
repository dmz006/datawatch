// CLI commands for the Automata tag registry and per-Automaton tag
// assignment: autonomous tag-list|tag-create|tag-rename|tag-move|tag-color|
// tag-delete and prd-set-tags. Each maps onto the /api/autonomous/tags REST
// endpoints through daemonJSON.

package main

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

const autonomousTagsPath = "/api/autonomous/tags"

func tagPath(id string) string { return autonomousTagsPath + "/" + url.PathEscape(id) }

func newAutonomousTagListCmd() *cobra.Command {
	var includeSub bool
	cmd := &cobra.Command{
		Use:   "tag-list",
		Short: "List Automata tags (a tree via parent_id) with the number of Automata carrying each",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			path := autonomousTagsPath
			if includeSub {
				path += "?include_sub=true"
			}
			return daemonJSON(http.MethodGet, path, nil)
		},
	}
	cmd.Flags().BoolVar(&includeSub, "include-sub", false, "roll each count up to include descendant tags")
	return cmd
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
	var tags string
	cmd := &cobra.Command{
		Use:   "prd-set-tags <prd-id> --tags <id1,id2,...>",
		Short: "Replace the tags on an Automaton (an empty --tags value clears them)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("tags") {
				return cmd.Usage()
			}
			ids := []string{}
			for _, s := range strings.Split(tags, ",") {
				if s = strings.TrimSpace(s); s != "" {
					ids = append(ids, s)
				}
			}
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(args[0])+"/set_tags",
				map[string]any{"tag_ids": ids})
		},
	}
	cmd.Flags().StringVar(&tags, "tags", "", "comma-separated tag IDs (empty clears)")
	return cmd
}
