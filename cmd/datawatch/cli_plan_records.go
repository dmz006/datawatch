// CLI for plan records: the registry rows (the numbered items of the plans
// registry) and the findings that the daemon keeps as records. Every
// subcommand calls the plan record routes on the local daemon
// (internal/server/plan_records.go):
//
//	datawatch plan-record create --kind registry-row|finding --project <alias> --title <t> [--body <text>] [--state <s>] [--json]
//	    → POST /api/plan-records/{registry-rows|findings}
//	datawatch plan-record get <ref> [--kind K] [--project P] [--json]
//	    → GET /api/plan-records/{registry-rows|findings}/{ref}
//	datawatch plan-record update <ref> [--kind K] [--project P] [--title … --body … --state …] [--json]
//	    → PUT /api/plan-records/{registry-rows|findings}/{ref}
//	datawatch plan-record list [--kind K] [--project P] [--state S] [--limit n] [--json]
//	    → GET /api/plan-records/{registry-rows|findings}
//	datawatch plan-record search <text> [--kind K] [--project P] [--state S] [--json]
//	    → GET /api/plan-records/search
//	datawatch plan-record import [--project P] [--source registry|findings|all] [--policy suffix|rename|skip] [--dry-run=false]
//	    → POST /api/plan-records/import
//
// A reference is an id, a uid, "row <n>", an exact title, an older title or an
// unambiguous title prefix. A title that matches more than one record prints
// the candidates and exits non-zero. Output shows the title first and the id
// second. The render functions are pure text builders over the response
// bodies, so the tests check the operator text without a daemon.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// ----- response shapes (the parts the CLI prints) ------------------------

type cliPlanRecord struct {
	Title        string `json:"title"`
	ID           string `json:"id"`
	UID          string `json:"uid"`
	Kind         string `json:"kind"`
	Project      string `json:"project"`
	State        string `json:"state"`
	Body         string `json:"body"`
	LegacyNumber int    `json:"legacy_number"`
	LegacySource string `json:"legacy_source"`
	ShippedBy    string `json:"shipped_by"`
	RaisedBy     string `json:"raised_by"`
	ShippedVer   string `json:"shipped_version"`
	Links        []struct {
		Kind string `json:"kind"`
		Ref  string `json:"ref"`
	} `json:"links"`
	Aliases      []string `json:"aliases"`
	NeedsReview  bool     `json:"needs_review"`
	ReviewReason string   `json:"review_reason"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	ClosedAt     string   `json:"closed_at"`
}

type cliPlanRecordImport struct {
	Note   string `json:"note"`
	Result struct {
		Project string `json:"project"`
		DryRun  bool   `json:"dry_run"`
		Policy  string `json:"policy"`
		Counts  struct {
			Create    int `json:"create"`
			Unchanged int `json:"unchanged"`
			Changed   int `json:"changed"`
			Conflicts int `json:"conflicts"`
			Skipped   int `json:"skipped"`
		} `json:"counts"`
		Conflicts []struct {
			Source   string `json:"source"`
			Title    string `json:"title"`
			Reason   string `json:"reason"`
			With     string `json:"with"`
			Action   string `json:"action"`
			NewTitle string `json:"new_title"`
			Note     string `json:"note"`
		} `json:"conflicts"`
		Unparseable []struct {
			Source string `json:"source"`
			Reason string `json:"reason"`
		} `json:"unparseable"`
		Failed []struct {
			Source string `json:"source"`
			Reason string `json:"reason"`
		} `json:"failed"`
		Files []string `json:"files"`
	} `json:"result"`
}

// ----- kinds ---------------------------------------------------------------

// planRecordKindSegment maps --kind to its REST collection segment.
func planRecordKindSegment(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "registry-row", "registry_row", "registry-rows":
		return "registry-rows", nil
	case "finding", "findings":
		return "findings", nil
	}
	return "", fmt.Errorf("bad value for --kind: %q — use registry-row or finding", kind)
}

// planRecordKindQuery maps --kind to the stored kind the filters take.
func planRecordKindQuery(kind string) (string, error) {
	seg, err := planRecordKindSegment(kind)
	if err != nil {
		return "", err
	}
	if seg == "findings" {
		return "finding", nil
	}
	return "registry_row", nil
}

// ----- rendering -----------------------------------------------------------

func planRecordPrintJSON(w io.Writer, body []byte) {
	var v any
	if json.Unmarshal(body, &v) != nil {
		_, _ = w.Write(body)
		return
	}
	out, _ := json.MarshalIndent(v, "", "  ")
	_, _ = fmt.Fprintln(w, string(out))
}

// planRecordLine is the one-line form: title first, id second.
func planRecordLine(r cliPlanRecord) string {
	var b strings.Builder
	b.WriteString(oneLineText(r.Title))
	b.WriteString("  [" + r.ID + "]")
	if r.Project != "" {
		b.WriteString("  " + r.Project)
	}
	if r.State != "" {
		b.WriteString("  " + r.State)
	}
	if r.LegacyNumber > 0 {
		b.WriteString("  row " + strconv.Itoa(r.LegacyNumber))
	}
	if r.NeedsReview {
		b.WriteString("  needs review")
	}
	return b.String()
}

// oneLineText collapses all white space so a title stays on one line.
func oneLineText(s string) string { return strings.Join(strings.Fields(s), " ") }

func renderPlanRecordList(w io.Writer, body []byte) error {
	var list struct {
		Records []cliPlanRecord `json:"records"`
		Total   int             `json:"total"`
		Offset  int             `json:"offset"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return fmt.Errorf("the daemon sent a list I cannot read: %w", err)
	}
	if len(list.Records) == 0 {
		_, _ = fmt.Fprintln(w, "No plan records.")
		return nil
	}
	for _, r := range list.Records {
		_, _ = fmt.Fprintln(w, planRecordLine(r))
	}
	if list.Total > len(list.Records)+list.Offset {
		_, _ = fmt.Fprintf(w, "(%d of %d shown; use --limit and --offset for more)\n", len(list.Records), list.Total)
	}
	return nil
}

func renderPlanRecordSearch(w io.Writer, body []byte) error {
	var res struct {
		Hits []struct {
			Record  cliPlanRecord `json:"record"`
			Matched string        `json:"matched"`
		} `json:"hits"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("the daemon sent a search result I cannot read: %w", err)
	}
	if len(res.Hits) == 0 {
		_, _ = fmt.Fprintln(w, "No plan records match.")
		return nil
	}
	for _, h := range res.Hits {
		line := planRecordLine(h.Record)
		if h.Matched != "" {
			line += "  (matched " + h.Matched + ")"
		}
		_, _ = fmt.Fprintln(w, line)
	}
	if res.Total > len(res.Hits) {
		_, _ = fmt.Fprintf(w, "(%d of %d shown)\n", len(res.Hits), res.Total)
	}
	return nil
}

// planRecordFromBody reads the {"record": …} envelope.
func planRecordFromBody(body []byte) (cliPlanRecord, error) {
	var env struct {
		Record cliPlanRecord `json:"record"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Record.ID == "" {
		return cliPlanRecord{}, fmt.Errorf("the daemon sent a record I cannot read")
	}
	return env.Record, nil
}

func renderPlanRecordShow(w io.Writer, body []byte) error {
	r, err := planRecordFromBody(body)
	if err != nil {
		return err
	}
	p := func(label, v string) {
		if strings.TrimSpace(v) != "" {
			_, _ = fmt.Fprintf(w, "%-13s %s\n", label+":", v)
		}
	}
	_, _ = fmt.Fprintln(w, oneLineText(r.Title))
	p("id", r.ID)
	p("uid", r.UID)
	p("kind", r.Kind)
	p("project", r.Project)
	p("state", r.State)
	if r.LegacyNumber > 0 {
		p("old row", strconv.Itoa(r.LegacyNumber))
	}
	p("imported from", r.LegacySource)
	p("shipped by", r.ShippedBy)
	p("raised by", r.RaisedBy)
	p("shipped", r.ShippedVer)
	if len(r.Aliases) > 0 {
		p("older titles", strings.Join(r.Aliases, "; "))
	}
	for _, l := range r.Links {
		p("link", l.Kind+" "+l.Ref)
	}
	if r.NeedsReview {
		reason := r.ReviewReason
		if reason == "" {
			reason = "yes"
		}
		p("needs review", reason)
	}
	p("created", r.CreatedAt)
	p("updated", r.UpdatedAt)
	p("closed", r.ClosedAt)
	if strings.TrimSpace(r.Body) != "" {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, strings.TrimRight(r.Body, "\n"))
	}
	return nil
}

func renderPlanRecordSaved(w io.Writer, verb string, body []byte) error {
	r, err := planRecordFromBody(body)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "%s: %s\n", verb, planRecordLine(r))
	return nil
}

func renderPlanRecordImport(w io.Writer, body []byte) error {
	var res cliPlanRecordImport
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("the daemon sent an import result I cannot read: %w", err)
	}
	r := res.Result
	mode := "Import"
	if r.DryRun {
		mode = "Preview"
	}
	_, _ = fmt.Fprintf(w, "%s for project %s (conflict policy: %s)\n", mode, r.Project, r.Policy)
	_, _ = fmt.Fprintf(w, "  create %d, unchanged %d, changed %d, conflicts %d, skipped %d\n",
		r.Counts.Create, r.Counts.Unchanged, r.Counts.Changed, r.Counts.Conflicts, r.Counts.Skipped)
	for _, f := range r.Files {
		_, _ = fmt.Fprintf(w, "  read %s\n", f)
	}
	for _, c := range r.Conflicts {
		line := fmt.Sprintf("  conflict (%s): %s — %s", c.Reason, oneLineText(c.Title), c.Action)
		if c.NewTitle != "" {
			line += " → " + oneLineText(c.NewTitle)
		}
		if c.With != "" {
			line += " (with " + c.With + ")"
		}
		if c.Source != "" {
			line += " [" + c.Source + "]"
		}
		_, _ = fmt.Fprintln(w, line)
		if c.Note != "" {
			_, _ = fmt.Fprintln(w, "    "+c.Note)
		}
	}
	for _, u := range r.Unparseable {
		_, _ = fmt.Fprintf(w, "  not imported: %s — %s\n", u.Source, u.Reason)
	}
	for _, f := range r.Failed {
		_, _ = fmt.Fprintf(w, "  failed: %s — %s\n", f.Source, f.Reason)
	}
	if res.Note != "" {
		_, _ = fmt.Fprintln(w, res.Note)
	}
	if r.DryRun {
		_, _ = fmt.Fprintln(w, "To write these records run the same command with --dry-run=false.")
	}
	return nil
}

// ----- ambiguous references -------------------------------------------------

// planRecordCandidates reads the candidates out of a 409 error from the
// daemon ("HTTP 409: {…}"). ok is false for any other failure.
func planRecordCandidates(err error) (msg string, cands []cliPlanRecord, ok bool) {
	if err == nil {
		return "", nil, false
	}
	text := err.Error()
	const prefix = "HTTP 409: "
	if !strings.HasPrefix(text, prefix) {
		return "", nil, false
	}
	var wire struct {
		Error      string          `json:"error"`
		Candidates []cliPlanRecord `json:"candidates"`
	}
	if json.Unmarshal([]byte(strings.TrimPrefix(text, prefix)), &wire) != nil || len(wire.Candidates) == 0 {
		return "", nil, false
	}
	return wire.Error, wire.Candidates, true
}

// planRecordAmbiguousError prints the candidates and returns the error the
// command exits with.
func planRecordAmbiguousError(w io.Writer, ref, msg string, cands []cliPlanRecord) error {
	if msg == "" {
		msg = fmt.Sprintf("%q matches more than one record", ref)
	}
	_, _ = fmt.Fprintln(w, msg)
	_, _ = fmt.Fprintln(w, "Candidates:")
	for _, c := range cands {
		kind := ""
		if c.Kind != "" {
			kind = "  " + c.Kind
		}
		_, _ = fmt.Fprintln(w, "  "+planRecordLine(c)+kind)
	}
	_, _ = fmt.Fprintln(w, "Say which one: pass its id instead of the title.")
	return errors.New("ambiguous reference: " + ref)
}

// planRecordFetchAcrossKinds reads one record by reference. With a kind it
// asks that collection only; without one it asks both and refuses a reference
// that resolves in both. method is GET (the update path calls it to find the
// kind, then PUTs).
func planRecordResolveKind(cmd *cobra.Command, ref, kind, project string) (seg string, body []byte, err error) {
	q := url.Values{}
	if project != "" {
		q.Set("project", project)
	}
	suffix := ""
	if enc := q.Encode(); enc != "" {
		suffix = "?" + enc
	}
	try := func(s string) ([]byte, error) {
		return daemonJSONBody(http.MethodGet, "/api/plan-records/"+s+"/"+url.PathEscape(ref)+suffix, nil)
	}
	if kind != "" {
		s, kerr := planRecordKindSegment(kind)
		if kerr != nil {
			return "", nil, kerr
		}
		b, gerr := try(s)
		if gerr != nil {
			if msg, cands, ok := planRecordCandidates(gerr); ok {
				return "", nil, planRecordAmbiguousError(cmd.ErrOrStderr(), ref, msg, cands)
			}
			return "", nil, gerr
		}
		return s, b, nil
	}
	var foundSeg string
	var found []byte
	var all []cliPlanRecord
	for _, s := range []string{"registry-rows", "findings"} {
		b, gerr := try(s)
		if gerr != nil {
			if msg, cands, ok := planRecordCandidates(gerr); ok {
				return "", nil, planRecordAmbiguousError(cmd.ErrOrStderr(), ref, msg, cands)
			}
			if strings.HasPrefix(gerr.Error(), "HTTP 404") {
				continue
			}
			return "", nil, gerr
		}
		if rec, perr := planRecordFromBody(b); perr == nil {
			all = append(all, rec)
		}
		foundSeg, found = s, b
	}
	switch len(all) {
	case 0:
		return "", nil, fmt.Errorf("no plan record matches %q", ref)
	case 1:
		return foundSeg, found, nil
	}
	return "", nil, planRecordAmbiguousError(cmd.ErrOrStderr(), ref,
		fmt.Sprintf("%q matches a registry row and a finding; say which with --kind", ref), all)
}

// ----- commands -------------------------------------------------------------

// newPlanRecordCmd is the plan-record group.
func newPlanRecordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "plan-record",
		Aliases: []string{"plan-records"},
		Short:   "Plan records: registry rows and findings kept by the daemon",
		Long: `Plan records are the numbered items of the plans registry (registry rows) and the
findings, kept by the daemon so Automata and operators read and change them
the same way. A record has a title (its human name), a short id and an old row
number when it came from the registry file.

  datawatch plan-record create --kind registry-row|finding --project <alias> --title <title> [--body <text>] [--state <state>]
  datawatch plan-record get <ref> [--kind K] [--project P]
  datawatch plan-record update <ref> [--kind K] [--project P] [--title … --body … --state …]
  datawatch plan-record list [--kind K] [--project P] [--state S] [--limit n]
  datawatch plan-record search <text> [--kind K] [--project P] [--state S]
  datawatch plan-record import [--project P] [--source registry|findings|all] [--policy suffix|rename|skip] [--dry-run=false]

A <ref> is an id, a uid, "row <n>" (the old row number), an exact title, an
older title or an unambiguous title prefix. A title that matches more than one
record prints the candidates and exits non-zero; repeat the command with an id.
Import previews by default: read the conflicts it lists, then add --dry-run=false.

When the daemon reports 404 for these routes, plan records are turned off: set
autonomous.plan_records.enabled to true in the daemon config.`,
	}
	cmd.AddCommand(newPlanRecordCreateCmd(), newPlanRecordGetCmd(), newPlanRecordUpdateCmd(),
		newPlanRecordListCmd(), newPlanRecordSearchCmd(), newPlanRecordImportCmd())
	return cmd
}

// planRecordWriteFlags are the content inputs of create and update.
type planRecordWriteFlags struct {
	title, body, bodyFile, state, source, shippedBy, raisedBy, shippedVersion, reviewReason string
	needsReview                                                                             bool
	aliases, links                                                                          []string
	legacyNumber                                                                            int
}

func planRecordWriteFlagSet(cmd *cobra.Command, f *planRecordWriteFlags, update bool) {
	fl := cmd.Flags()
	fl.StringVar(&f.title, "title", "", "The record's title (unique within a project)")
	fl.StringVar(&f.body, "body", "", "The record's text")
	fl.StringVar(&f.bodyFile, "body-file", "", "Read the body from this file (- for standard input)")
	fl.StringVar(&f.state, "state", "", "State. Registry rows: open, planned, in_progress, blocked, shipped, done, deferred, cancelled. Findings: open, triaged, fixed, wontfix, duplicate")
	fl.StringVar(&f.source, "legacy-source", "", "Where an imported record came from")
	fl.StringVar(&f.shippedBy, "shipped-by", "", "The Automaton that shipped it")
	fl.StringVar(&f.raisedBy, "raised-by", "", "The Automaton that raised it")
	fl.StringVar(&f.shippedVersion, "shipped-version", "", "What the archive said shipped it (free text)")
	fl.StringArrayVar(&f.links, "link", nil, "A link as kind=ref (repeatable; replaces the links on update)")
	fl.BoolVar(&f.needsReview, "needs-review", false, "Flag the record for a person to look at")
	fl.StringVar(&f.reviewReason, "review-reason", "", "Why the record needs review")
	if update {
		fl.StringArrayVar(&f.aliases, "add-alias", nil, "An older title that should still resolve (repeatable)")
	} else {
		fl.StringArrayVar(&f.aliases, "alias", nil, "An older title that should still resolve (repeatable)")
		fl.IntVar(&f.legacyNumber, "legacy-number", 0, "Registry rows: the old row number")
	}
}

// planRecordBody builds the create or update body from the flags the operator
// set. Only changed flags go in, so an update leaves the rest alone.
func planRecordBody(cmd *cobra.Command, f planRecordWriteFlags, update bool) (map[string]any, error) {
	body := map[string]any{}
	set := func(flag, key string, v any) {
		if cmd.Flags().Changed(flag) {
			body[key] = v
		}
	}
	set("title", "title", strings.TrimSpace(f.title))
	set("state", "state", strings.TrimSpace(f.state))
	set("legacy-source", "legacy_source", f.source)
	set("shipped-by", "shipped_by", f.shippedBy)
	set("raised-by", "raised_by", f.raisedBy)
	set("shipped-version", "shipped_version", f.shippedVersion)
	set("needs-review", "needs_review", f.needsReview)
	set("review-reason", "review_reason", f.reviewReason)
	if cmd.Flags().Changed("body") && cmd.Flags().Changed("body-file") {
		return nil, fmt.Errorf("give --body or --body-file, not both")
	}
	set("body", "body", f.body)
	if f.bodyFile != "" {
		var raw []byte
		var err error
		if f.bodyFile == "-" {
			raw, err = io.ReadAll(cmd.InOrStdin())
		} else {
			raw, err = os.ReadFile(f.bodyFile)
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read --body-file: %w", err)
		}
		body["body"] = string(raw)
	}
	if cmd.Flags().Changed("link") {
		links := []map[string]string{}
		for _, l := range f.links {
			k, ref, ok := strings.Cut(l, "=")
			if !ok || strings.TrimSpace(k) == "" || strings.TrimSpace(ref) == "" {
				return nil, fmt.Errorf("bad value for --link: %q — use kind=ref", l)
			}
			links = append(links, map[string]string{"kind": strings.TrimSpace(k), "ref": strings.TrimSpace(ref)})
		}
		body["links"] = links
	}
	if len(f.aliases) > 0 {
		if update {
			body["add_aliases"] = f.aliases
		} else {
			body["aliases"] = f.aliases
		}
	}
	if !update && cmd.Flags().Changed("legacy-number") {
		if f.legacyNumber < 1 {
			return nil, fmt.Errorf("--legacy-number must be 1 or larger")
		}
		body["legacy_number"] = f.legacyNumber
	}
	return body, nil
}

func newPlanRecordCreateCmd() *cobra.Command {
	var kind, project string
	var jsonOut bool
	var f planRecordWriteFlags
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a registry row or a finding",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(kind) == "" {
				return fmt.Errorf("--kind is required: registry-row or finding")
			}
			seg, err := planRecordKindSegment(kind)
			if err != nil {
				return err
			}
			if strings.TrimSpace(project) == "" {
				return fmt.Errorf("--project is required: a registered project alias")
			}
			if strings.TrimSpace(f.title) == "" {
				return fmt.Errorf("--title is required")
			}
			body, err := planRecordBody(cmd, f, false)
			if err != nil {
				return err
			}
			body["project"] = strings.TrimSpace(project)
			out, err := daemonJSONBody(http.MethodPost, "/api/plan-records/"+seg, body)
			if err != nil {
				return err
			}
			if jsonOut {
				planRecordPrintJSON(cmd.OutOrStdout(), out)
				return nil
			}
			return renderPlanRecordSaved(cmd.OutOrStdout(), "created", out)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "registry-row or finding (required)")
	cmd.Flags().StringVar(&project, "project", "", "A registered project alias (required)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	planRecordWriteFlagSet(cmd, &f, false)
	return cmd
}

func newPlanRecordGetCmd() *cobra.Command {
	var kind, project string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "get <ref>",
		Short: "Show one plan record (ref: id, row <n>, title or title prefix)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, body, err := planRecordResolveKind(cmd, strings.TrimSpace(args[0]), kind, strings.TrimSpace(project))
			if err != nil {
				return err
			}
			if jsonOut {
				planRecordPrintJSON(cmd.OutOrStdout(), body)
				return nil
			}
			return renderPlanRecordShow(cmd.OutOrStdout(), body)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "registry-row or finding (both are searched when left out)")
	cmd.Flags().StringVar(&project, "project", "", "Narrow a title reference to this project alias")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	return cmd
}

func newPlanRecordUpdateCmd() *cobra.Command {
	var kind, project string
	var jsonOut bool
	var f planRecordWriteFlags
	cmd := &cobra.Command{
		Use:   "update <ref>",
		Short: "Change a plan record; a flag you leave out leaves that field alone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := strings.TrimSpace(args[0])
			body, err := planRecordBody(cmd, f, true)
			if err != nil {
				return err
			}
			if len(body) == 0 {
				return fmt.Errorf("nothing to change: give at least one of --title, --body, --state and the other content flags")
			}
			seg, _, err := planRecordResolveKind(cmd, ref, kind, strings.TrimSpace(project))
			if err != nil {
				return err
			}
			path := "/api/plan-records/" + seg + "/" + url.PathEscape(ref)
			if p := strings.TrimSpace(project); p != "" {
				path += "?" + url.Values{"project": {p}}.Encode()
			}
			out, err := daemonJSONBody(http.MethodPut, path, body)
			if err != nil {
				return err
			}
			if jsonOut {
				planRecordPrintJSON(cmd.OutOrStdout(), out)
				return nil
			}
			return renderPlanRecordSaved(cmd.OutOrStdout(), "updated", out)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "registry-row or finding (both are searched when left out)")
	cmd.Flags().StringVar(&project, "project", "", "Narrow a title reference to this project alias")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	planRecordWriteFlagSet(cmd, &f, true)
	return cmd
}

// planRecordFilterValues builds the shared list and search query.
func planRecordFilterValues(project, state string, limit, offset int) (url.Values, error) {
	if limit < 0 || offset < 0 {
		return nil, fmt.Errorf("--limit and --offset must be 0 or larger")
	}
	q := url.Values{}
	if v := strings.TrimSpace(project); v != "" {
		q.Set("project", v)
	}
	if v := strings.TrimSpace(state); v != "" {
		q.Set("state", v)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	return q, nil
}

func newPlanRecordListCmd() *cobra.Command {
	var kind, project, state string
	var limit, offset int
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List plan records, most recently updated first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q, err := planRecordFilterValues(project, state, limit, offset)
			if err != nil {
				return err
			}
			segs := []string{"registry-rows", "findings"}
			if strings.TrimSpace(kind) != "" {
				s, kerr := planRecordKindSegment(kind)
				if kerr != nil {
					return kerr
				}
				segs = []string{s}
			}
			suffix := ""
			if enc := q.Encode(); enc != "" {
				suffix = "?" + enc
			}
			combined := map[string]json.RawMessage{}
			for _, s := range segs {
				out, derr := daemonJSONBody(http.MethodGet, "/api/plan-records/"+s+suffix, nil)
				if derr != nil {
					// A state that fits only one kind is refused by the other
					// collection; with no --kind that is not a failure.
					if len(segs) > 1 && state != "" && strings.HasPrefix(derr.Error(), "HTTP 400") {
						continue
					}
					return derr
				}
				if jsonOut {
					if len(segs) == 1 {
						planRecordPrintJSON(cmd.OutOrStdout(), out)
						return nil
					}
					combined[s] = out
					continue
				}
				if len(segs) > 1 {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "== %s ==\n", s)
				}
				if rerr := renderPlanRecordList(cmd.OutOrStdout(), out); rerr != nil {
					return rerr
				}
			}
			if jsonOut {
				raw, _ := json.MarshalIndent(combined, "", "  ")
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(raw))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "registry-row or finding (both when left out)")
	cmd.Flags().StringVar(&project, "project", "", "Only this project alias")
	cmd.Flags().StringVar(&state, "state", "", "Only this state")
	cmd.Flags().IntVar(&limit, "limit", 0, "How many records to return (the daemon caps at 1000)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Skip this many records")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	return cmd
}

func newPlanRecordSearchCmd() *cobra.Command {
	var kind, project, state string
	var limit, offset int
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "search <text>",
		Short: "Search registry rows and findings by words",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := strings.TrimSpace(strings.Join(args, " "))
			if text == "" {
				return fmt.Errorf("give the words to search for")
			}
			q, err := planRecordFilterValues(project, state, limit, offset)
			if err != nil {
				return err
			}
			if strings.TrimSpace(kind) != "" {
				k, kerr := planRecordKindQuery(kind)
				if kerr != nil {
					return kerr
				}
				q.Set("kind", k)
			}
			q.Set("q", text)
			out, err := daemonJSONBody(http.MethodGet, "/api/plan-records/search?"+q.Encode(), nil)
			if err != nil {
				return err
			}
			if jsonOut {
				planRecordPrintJSON(cmd.OutOrStdout(), out)
				return nil
			}
			return renderPlanRecordSearch(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "registry-row or finding (both when left out)")
	cmd.Flags().StringVar(&project, "project", "", "Only this project alias")
	cmd.Flags().StringVar(&state, "state", "", "Only this state")
	cmd.Flags().IntVar(&limit, "limit", 0, "How many hits to return (the daemon caps at 1000)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Skip this many hits")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	return cmd
}

func newPlanRecordImportCmd() *cobra.Command {
	var project, source, policy string
	var dryRun, jsonOut bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import a project's registry rows and findings files as plan records (previews by default)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch s := strings.ToLower(strings.TrimSpace(source)); s {
			case "", "registry", "findings", "all":
				source = s
			default:
				return fmt.Errorf("bad value for --source: %q — use registry, findings or all", source)
			}
			switch p := strings.ToLower(strings.TrimSpace(policy)); p {
			case "", "suffix", "rename", "skip":
				policy = p
			default:
				return fmt.Errorf("bad value for --policy: %q — use suffix, rename or skip", policy)
			}
			body := map[string]any{}
			if v := strings.TrimSpace(project); v != "" {
				body["project"] = v
			}
			if source != "" {
				body["source"] = source
			}
			if policy != "" {
				body["conflict_policy"] = policy
			}
			path := "/api/plan-records/import?" + url.Values{"dry_run": {strconv.FormatBool(dryRun)}}.Encode()
			out, err := daemonJSONBody(http.MethodPost, path, body)
			if err != nil {
				return err
			}
			if jsonOut {
				planRecordPrintJSON(cmd.OutOrStdout(), out)
				return nil
			}
			return renderPlanRecordImport(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "A registered project alias (default datawatch)")
	cmd.Flags().StringVar(&source, "source", "", "registry, findings or all (default all)")
	cmd.Flags().StringVar(&policy, "policy", "", "How to settle a conflict: suffix, rename or skip (default: the daemon's setting)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", true, "Preview only; pass --dry-run=false to write")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	return cmd
}
