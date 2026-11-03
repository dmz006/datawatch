// `datawatch autonomous defect intake|batches|split|tiny-fix-check` — the
// defect queue from the CLI. Each sub-command calls one REST endpoint:
//
//	intake         POST /api/autonomous/defects/intake
//	batches        GET  /api/autonomous/defects/batches
//	split          POST /api/autonomous/defects/batches/{id}/split
//	tiny-fix-check POST /api/autonomous/defects/tiny-fix/check
//
// The daemon decides the route and the reason; the CLI only builds the
// request and prints the reply.

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

var (
	errDefectTitle   = errors.New("defect intake: --title is required")
	errDefectProject = errors.New("defect intake: --project is required")
	errDefectID      = errors.New("defect split: batch id and --story are required")
	errDefectFiles   = errors.New("defect tiny-fix-check: at least one --file is required")
	errDefectKind    = errors.New("defect tiny-fix-check: --kind is required")
)

const defectBasePath = "/api/autonomous/defects"

// defectIntakeArgs holds the parsed flags of `defect intake`.
type defectIntakeArgs struct {
	id, title, description, kind, area, project string
	priority                                    int
	files                                       []string
}

func (a *defectIntakeArgs) path() string { return defectBasePath + "/intake" }

func (a *defectIntakeArgs) body() (map[string]any, error) {
	if strings.TrimSpace(a.title) == "" {
		return nil, errDefectTitle
	}
	if strings.TrimSpace(a.project) == "" {
		return nil, errDefectProject
	}
	id := strings.TrimSpace(a.id)
	if id == "" {
		var b [6]byte
		_, _ = rand.Read(b[:])
		id = "defect-" + hex.EncodeToString(b[:])
	}
	body := map[string]any{
		"id": id, "title": a.title, "description": a.description,
		"project": a.project, "priority": a.priority,
	}
	if a.kind != "" {
		body["kind"] = a.kind
	}
	if a.area != "" {
		body["area"] = a.area
	}
	if len(a.files) > 0 {
		body["files"] = a.files
	}
	return body, nil
}

func (a *defectIntakeArgs) run() error {
	body, err := a.body()
	if err != nil {
		return err
	}
	return daemonJSON(http.MethodPost, a.path(), body)
}

// defectBatchesArgs holds the parsed flags of `defect batches`.
type defectBatchesArgs struct{ project string }

func (a *defectBatchesArgs) path() string {
	if strings.TrimSpace(a.project) == "" {
		return defectBasePath + "/batches"
	}
	return defectBasePath + "/batches?project=" + url.QueryEscape(a.project)
}

// defectSplitArgs holds the parsed args of `defect split`.
type defectSplitArgs struct{ batch, story string }

func (a *defectSplitArgs) path() string {
	return defectBasePath + "/batches/" + url.PathEscape(a.batch) + "/split"
}

func (a *defectSplitArgs) run() error {
	if strings.TrimSpace(a.batch) == "" || strings.TrimSpace(a.story) == "" {
		return errDefectID
	}
	return daemonJSON(http.MethodPost, a.path(), map[string]any{"story_id": a.story})
}

// defectTinyFixArgs holds the parsed flags of `defect tiny-fix-check`.
type defectTinyFixArgs struct {
	files         []string
	kind, project string
}

func (a *defectTinyFixArgs) path() string { return defectBasePath + "/tiny-fix/check" }

func (a *defectTinyFixArgs) run() error {
	if len(a.files) == 0 {
		return errDefectFiles
	}
	if strings.TrimSpace(a.kind) == "" {
		return errDefectKind
	}
	body := map[string]any{"files": a.files, "kind": a.kind}
	if a.project != "" {
		body["project"] = a.project
	}
	return daemonJSON(http.MethodPost, a.path(), body)
}

func newAutonomousDefectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "defect",
		Short: "Defect queue: intake, open batches, split-out, tiny-fix check",
		Long: `Work with the defect queue. The daemon routes each defect (tiny fix,
an open batch or a new batch) and replies with the route and the reason.`,
	}

	in := &defectIntakeArgs{}
	intake := &cobra.Command{
		Use:   "intake",
		Short: "Route one defect (--title, --project required); prints the route and the reason",
		Args:  cobra.NoArgs,
		RunE:  func(_ *cobra.Command, _ []string) error { return in.run() },
	}
	intake.Flags().StringVar(&in.id, "id", "", "Defect id (default: generated)")
	intake.Flags().StringVar(&in.title, "title", "", "Defect title (required)")
	intake.Flags().StringVar(&in.description, "description", "", "Defect description")
	intake.Flags().StringVar(&in.project, "project", "", "Project (required)")
	intake.Flags().StringVar(&in.kind, "kind", "", "Change kind, e.g. bug")
	intake.Flags().StringVar(&in.area, "area", "", "Code area used for batching")
	intake.Flags().IntVar(&in.priority, "priority", 0, "Priority")
	intake.Flags().StringSliceVar(&in.files, "file", nil, "File the fix touches (repeatable)")
	cmd.AddCommand(intake)

	bt := &defectBatchesArgs{}
	batches := &cobra.Command{
		Use:   "batches",
		Short: "List open defect batches with their story-to-defect map",
		Args:  cobra.NoArgs,
		RunE:  func(_ *cobra.Command, _ []string) error { return daemonGet(bt.path()) },
	}
	batches.Flags().StringVar(&bt.project, "project", "", "Only this project")
	cmd.AddCommand(batches)

	sp := &defectSplitArgs{}
	split := &cobra.Command{
		Use:   "split <batch-id> --story <story-id>",
		Short: "Move one story out of a batch into its own Automaton",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			sp.batch = args[0]
			return sp.run()
		},
	}
	split.Flags().StringVar(&sp.story, "story", "", "Story id to split out (required)")
	cmd.AddCommand(split)

	tf := &defectTinyFixArgs{}
	tiny := &cobra.Command{
		Use:   "tiny-fix-check",
		Short: "Would this change be a tiny fix? (--file, --kind required; read-only)",
		Args:  cobra.NoArgs,
		RunE:  func(_ *cobra.Command, _ []string) error { return tf.run() },
	}
	tiny.Flags().StringSliceVar(&tf.files, "file", nil, "File the change touches (repeatable, required)")
	tiny.Flags().StringVar(&tf.kind, "kind", "", "Change kind (required)")
	tiny.Flags().StringVar(&tf.project, "project", "", "Project (applies per-project settings)")
	cmd.AddCommand(tiny)
	return cmd
}
