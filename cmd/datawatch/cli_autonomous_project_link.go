package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// projectLinkBody builds the set_project request body. An empty project
// clears the link.
func projectLinkBody(project string) map[string]string {
	return map[string]string{"project": strings.TrimSpace(project)}
}

// projectLinkBackfillBody builds the backfill request body: apply false is a
// dry run.
func projectLinkBackfillBody(apply bool) map[string]bool {
	return map[string]bool{"apply": apply}
}

func newAutonomousPRDSetProjectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   `prd-set-project <id> <project|"">`,
		Short: "Link an Automaton to a configured project (an empty name clears the link)",
		Long: `Set the explicit project link of one Automaton. The project must be a
configured project name; an unknown name is refused and the message lists the
configured names. An empty name ("" or clear) removes the link, after which the
Automaton is matched through its Automation, its parent or its directory. The
change is written to the Automaton's decision log. No session is touched.`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			project := args[1]
			if strings.EqualFold(strings.TrimSpace(project), "clear") {
				project = ""
			}
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(args[0])+"/set_project", projectLinkBody(project))
		},
	}
}

func newAutonomousProjectLinkBackfillCmd() *cobra.Command {
	var apply bool
	cmd := &cobra.Command{
		Use:   "project-link-backfill [--apply]",
		Short: "Report (and with --apply write) project links for Automata that have none",
		Long: `Run one project link backfill pass. Each Automaton with no project link is
matched to the datawatch project through its Automation, its parent or its
repository directory. The default is a dry run that prints the report table;
--apply writes the links. Automata that already have a link are never changed,
and Automata that match nothing are listed as skipped. Only Automaton records
are read and written; no session is touched.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resp, err := daemonJSONBody(http.MethodPost, "/api/autonomous/project-link-backfill", projectLinkBackfillBody(apply))
			if err != nil {
				return err
			}
			var rep autonomous.ProjectLinkBackfillReport
			if err := json.Unmarshal(resp, &rep); err != nil {
				return fmt.Errorf("decode report: %w", err)
			}
			out := cmd.OutOrStdout()
			if out == nil {
				out = os.Stdout
			}
			fmt.Fprint(out, formatProjectLinkReport(rep))
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "Write the links (default is a dry run that only reports)")
	return cmd
}

// formatProjectLinkReport renders a backfill report as a table followed by
// the counts line.
func formatProjectLinkReport(rep autonomous.ProjectLinkBackfillReport) string {
	var b strings.Builder
	mode := "dry run"
	if rep.Apply {
		mode = "applied"
	}
	fmt.Fprintf(&b, "Project link backfill (%s) — project: %s\n", mode, orDash(rep.Project))
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "AUTOMATON\tTITLE\tPROJECT\tSOURCE\tRESULT")
	for _, e := range rep.Entries {
		result := "would link"
		switch {
		case e.Error != "":
			result = "failed: " + e.Error
		case e.Skipped:
			result = "skipped: " + e.Reason
		case e.Applied:
			result = "linked"
		case rep.Apply:
			result = "not written"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.ID, orDash(e.Title), orDash(e.Project), orDash(e.Source), result)
	}
	_ = tw.Flush()
	c := rep.Counts
	fmt.Fprintf(&b, "scanned %d, matched %d, applied %d, skipped %d, failed %d\n", c.Scanned, c.Matched, c.Applied, c.Skipped, c.Failed)
	return b.String()
}
