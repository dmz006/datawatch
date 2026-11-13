// plans_cli.go — `datawatch plans migrate [--apply]`: build plan records from
// the project arcs and the registry rows through POST /api/plans/migrate.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

// cliPlansMigrate is the part of the migrate answer the CLI prints.
type cliPlansMigrate struct {
	Note   string `json:"note"`
	Result struct {
		Applied         bool                `json:"applied"`
		WouldCreate     []cliPlansMigrateIt `json:"would_create"`
		WouldLink       []cliPlansMigrateIt `json:"would_link"`
		SkippedExisting []cliPlansMigrateIt `json:"skipped_existing"`
		Conflicts       []cliPlansMigrateIt `json:"conflicts"`
	} `json:"result"`
}

type cliPlansMigrateIt struct {
	Source   string `json:"source"`
	Title    string `json:"title"`
	Project  string `json:"project"`
	LinkKind string `json:"link_kind"`
	LinkID   string `json:"link_id"`
	Order    int    `json:"order"`
	Reason   string `json:"reason"`
}

func renderPlansMigrate(w io.Writer, body []byte) error {
	var res cliPlansMigrate
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("the daemon sent a migration result I cannot read: %w", err)
	}
	r := res.Result
	head, verbCreate, verbLink := "Preview", "would create", "would link"
	if r.Applied {
		head, verbCreate, verbLink = "Migration", "created", "linked"
	}
	_, _ = fmt.Fprintf(w, "%s: %s %d, %s %d, already there %d, conflicts %d\n", head,
		verbCreate, len(r.WouldCreate), verbLink, len(r.WouldLink), len(r.SkippedExisting), len(r.Conflicts))
	line := func(prefix string, it cliPlansMigrateIt) {
		s := fmt.Sprintf("  %s: %s (%s", prefix, oneLineText(it.Title), it.Source)
		if it.Project != "" {
			s += ", " + it.Project
		}
		if it.Order > 0 {
			s += fmt.Sprintf(", order %d", it.Order)
		}
		s += ")"
		if it.LinkID != "" {
			s += " → " + it.LinkKind + " " + it.LinkID
		}
		if it.Reason != "" {
			s += " — " + it.Reason
		}
		_, _ = fmt.Fprintln(w, s)
	}
	for _, it := range r.WouldCreate {
		line(verbCreate, it)
	}
	for _, it := range r.WouldLink {
		line(verbLink, it)
	}
	for _, it := range r.SkippedExisting {
		line("left alone", it)
	}
	for _, it := range r.Conflicts {
		line("conflict", it)
	}
	if res.Note != "" {
		_, _ = fmt.Fprintln(w, res.Note)
	}
	if !r.Applied {
		_, _ = fmt.Fprintln(w, "To write these plans run the same command with --apply.")
	}
	return nil
}

func newPlansCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plans",
		Short: "Plans: the work to come, linked to Automations and Automata",
		Long: `Plans group the work to come. A plan links down to Automations and Automata.

  datawatch plans migrate [--apply] [--json]

migrate builds plans from the project arcs and from the registry rows that name
an Automaton or Automation (a row that names none becomes a backlog plan, in
the old row order). It previews by default; add --apply to write. A plan or
link that already exists is never changed, so a second --apply changes nothing.
No arc, row or config key is deleted.

When the daemon reports 404, plan records are turned off: set
autonomous.plan_records.enabled to true in the daemon config.`,
	}
	cmd.AddCommand(newPlansMigrateCmd())
	return cmd
}

func newPlansMigrateCmd() *cobra.Command {
	var apply, jsonOut bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Build plans from project arcs and registry rows (previews unless --apply)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := daemonJSONBody(http.MethodPost, "/api/plans/migrate", map[string]any{"apply": apply})
			if err != nil {
				return err
			}
			if jsonOut {
				planRecordPrintJSON(cmd.OutOrStdout(), out)
				return nil
			}
			return renderPlansMigrate(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "Write the plans and links; without it only a preview prints")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	return cmd
}
