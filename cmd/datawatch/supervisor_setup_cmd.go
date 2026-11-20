// datawatch supervisor-setup — set up a named project's supervisor and edit
// its state record, over the daemon's /api/supervisor-setup routes:
//
//	datawatch supervisor-setup run --project P --model M [--schedule name[=cron]]...
//	datawatch supervisor-setup state get|set|change|remove --project P ...
//	datawatch supervisor-setup correction list|record|promote --project P ...
//
// Thin REST proxies; the daemon validates every value. The set-up command is
// idempotent: running it again adds no schedule that already exists.

package main

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newSupervisorSetupCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "supervisor-setup",
		Short: "Set up a project's supervisor; edit its state record and corrections",
		Long: `Set up a named project's supervisor and edit what it remembers.

  run         idempotent set-up: launch role supervisor, the project
              directory, an explicit Sonnet or Haiku model, a generated
              handover and one recurring spawn schedule per name
  state       get, set, change or remove an entry of the pinned state record
  correction  list, record or promote the corrections the operator gave

Every command prints the daemon's JSON answer.`,
	}
	c.AddCommand(newSupervisorSetupRunCmd(), newSupervisorStateCmd(), newSupervisorCorrectionCmd())
	return c
}

func supervisorSetupPrint(cmd *cobra.Command, body []byte, err error) error {
	if err != nil {
		return err
	}
	prettyPrintTo(cmd.OutOrStdout(), body)
	return nil
}

func newSupervisorSetupRunCmd() *cobra.Command {
	var project, model string
	var schedules []string
	c := &cobra.Command{
		Use:   "run",
		Short: "Idempotently set up a project's supervisor",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			list := []map[string]string{}
			for _, sc := range schedules {
				name, cron, _ := strings.Cut(sc, "=")
				list = append(list, map[string]string{"name": strings.TrimSpace(name), "cron": strings.TrimSpace(cron)})
			}
			body := map[string]any{"project": project, "model": model}
			if len(list) > 0 {
				body["schedules"] = list
			}
			out, err := daemonJSONBody(http.MethodPost, "/api/supervisor-setup", body)
			return supervisorSetupPrint(cmd, out, err)
		},
	}
	c.Flags().StringVar(&project, "project", "", "project name (required)")
	c.Flags().StringVar(&model, "model", "", "explicit Sonnet or Haiku model (required)")
	c.Flags().StringArrayVar(&schedules, "schedule", nil, "recurring schedule as name[=cron]; repeatable (default: one watch schedule)")
	return c
}

// supervisorEditFlags are the flags the state and correction edits share.
type supervisorEditFlags struct {
	project, role, id, section, text, owner, nextStep string
	class, evidence, workaround, who, why             string
}

func (f *supervisorEditFlags) bind(fl *pflag.FlagSet) {
	fl.StringVar(&f.project, "project", "", "project name (required)")
	fl.StringVar(&f.role, "role", "", "record role (default supervisor)")
	fl.StringVar(&f.id, "id", "", "entry or correction id")
	fl.StringVar(&f.who, "who", "", "who is making the edit")
	fl.StringVar(&f.why, "why", "", "why the edit is made")
}

func (f *supervisorEditFlags) fields(cmd *cobra.Command, names map[string]*string) map[string]any {
	body := map[string]any{"project": f.project, "role": f.role, "id": f.id, "who": f.who, "why": f.why}
	for n, p := range names {
		// Only fields the caller passed are sent, so a change leaves the rest.
		if cmd.Flags().Changed(n) {
			body[strings.ReplaceAll(n, "-", "_")] = *p
		}
	}
	return body
}

func newSupervisorStateCmd() *cobra.Command {
	f := &supervisorEditFlags{}
	c := &cobra.Command{Use: "state", Short: "Read or edit the pinned state record"}
	get := &cobra.Command{Use: "get", Short: "Read the state record", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{"project": {f.project}}
			if f.role != "" {
				q.Set("role", f.role)
			}
			out, err := progressGet("/api/supervisor-setup/state", q)
			return supervisorSetupPrint(cmd, out, err)
		}}
	edit := func(use, short, method string, withSection bool) *cobra.Command {
		sub := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs}
		names := map[string]*string{}
		if method != http.MethodDelete {
			sub.Flags().StringVar(&f.text, "text", "", "entry text")
			sub.Flags().StringVar(&f.owner, "owner", "", "entry owner")
			sub.Flags().StringVar(&f.nextStep, "next-step", "", "entry next step")
			names["text"], names["owner"], names["next-step"] = &f.text, &f.owner, &f.nextStep
		}
		if withSection {
			sub.Flags().StringVar(&f.section, "section", "", "decision, work, checked or correction")
			names["section"] = &f.section
		}
		sub.RunE = func(cmd *cobra.Command, _ []string) error {
			out, err := daemonJSONBody(method, "/api/supervisor-setup/state", f.fields(cmd, names))
			return supervisorSetupPrint(cmd, out, err)
		}
		return sub
	}
	c.AddCommand(get,
		edit("set", "Set an entry (create or replace)", http.MethodPost, true),
		edit("change", "Change fields of an entry", http.MethodPatch, false),
		edit("remove", "Remove an entry", http.MethodDelete, false))
	f.bind(c.PersistentFlags())
	return c
}

func newSupervisorCorrectionCmd() *cobra.Command {
	f := &supervisorEditFlags{}
	c := &cobra.Command{Use: "correction", Short: "List, record or promote corrections"}
	list := &cobra.Command{Use: "list", Short: "List recorded corrections", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{"project": {f.project}}
			if f.role != "" {
				q.Set("role", f.role)
			}
			out, err := progressGet("/api/supervisor-setup/corrections", q)
			return supervisorSetupPrint(cmd, out, err)
		}}
	record := &cobra.Command{Use: "record", Short: "Record a correction", Args: cobra.NoArgs}
	record.Flags().StringVar(&f.text, "text", "", "correction text")
	record.Flags().StringVar(&f.class, "class", "", "state, standing_rule or product_defect")
	record.Flags().StringVar(&f.evidence, "evidence", "", "evidence for the correction")
	record.Flags().StringVar(&f.workaround, "workaround", "", "workaround until fixed")
	record.RunE = func(cmd *cobra.Command, _ []string) error {
		body := f.fields(cmd, map[string]*string{"text": &f.text, "class": &f.class, "evidence": &f.evidence, "workaround": &f.workaround})
		out, err := daemonJSONBody(http.MethodPost, "/api/supervisor-setup/corrections", body)
		return supervisorSetupPrint(cmd, out, err)
	}
	promote := &cobra.Command{Use: "promote", Short: "Promote a standing-rule correction to the rules pack", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := daemonJSONBody(http.MethodPost, "/api/supervisor-setup/corrections/promote", f.fields(cmd, nil))
			return supervisorSetupPrint(cmd, out, err)
		}}
	c.AddCommand(list, record, promote)
	f.bind(c.PersistentFlags())
	return c
}
