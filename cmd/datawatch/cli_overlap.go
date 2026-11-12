// datawatch autonomous overlap [--title T] [--spec S | <spec>] [--json] — the
// search-before-create read over the CLI. The body of GET /api/autonomous/overlap
// is rendered by the shared formatter (internal/autonomous/overlap_text.go), the
// same text the comm verb shows, so the operator sees one wording wherever they
// look. A read: nothing is written.

package main

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newAutonomousOverlapCmd is the `datawatch autonomous overlap` subcommand:
// the Automata, registry rows and unmerged branches that already cover a
// plan's title and spec, best match first.
func newAutonomousOverlapCmd() *cobra.Command {
	var title, spec string
	var asJSON bool
	c := &cobra.Command{
		Use:   "overlap [spec]",
		Short: "Search before you create: find the Automata, registry rows and branches that cover a plan",
		Long: "Show the Automata, registry rows and unmerged branches that already cover a plan's " +
			"title and spec, best match first. Give the spec with --spec or as the argument, " +
			"and the title with --title; either one alone is enough.\n\n" +
			"A read: nothing is written, and the search runs whatever the create-search mode is.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if spec != "" {
					return fmt.Errorf("give the spec with --spec or as an argument, not both")
				}
				spec = args[0]
			}
			if title == "" && spec == "" {
				return fmt.Errorf("give a --title, a spec, or both")
			}
			q := url.Values{}
			if title != "" {
				q.Set("title", title)
			}
			if spec != "" {
				q.Set("spec", spec)
			}
			body, err := progressGet("/api/autonomous/overlap", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			ws, err := autonomous.ParseOverlapWarnings(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderOverlapText(ws))
			return err
		},
	}
	c.Flags().StringVar(&title, "title", "", "the title of the plan you are about to create")
	c.Flags().StringVar(&spec, "spec", "", "the spec of the plan you are about to create")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
