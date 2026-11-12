// CLI for the held-question report: one read of the daemon's
// held-question view (internal/server/held_questions.go).
//
//	datawatch question held [--prd <prd-id>] [--state <held state id>] [--json]
//	    → GET /api/autonomous/held-questions
//
// The renderer (renderHeldQuestions) is a pure text builder over the
// response body, so the tests check the operator text without a daemon.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

// ----- response shapes (the parts the CLI prints) ------------------------

// cliHeldOpen is one open held question, as the daemon reports it.
type cliHeldOpen struct {
	QuestionID        string `json:"question_id"`
	State             string `json:"state,omitempty"`
	PRDID             string `json:"prd_id,omitempty"`
	TaskID            string `json:"task_id,omitempty"`
	AutomationID      string `json:"automation_id,omitempty"`
	Title             string `json:"title,omitempty"`
	Reason            string `json:"reason,omitempty"`
	RecommendedOption string `json:"recommended_option,omitempty"`
	Default           string `json:"default,omitempty"`
	CreatedAt         string `json:"created_at,omitempty"`
}

// cliHeldShadow is one shadow entry: a hold the daemon would have raised if
// held-question mode were on.
type cliHeldShadow struct {
	PRDID             string `json:"prd_id"`
	State             string `json:"state"`
	RecommendedOption string `json:"recommended_option,omitempty"`
	At                string `json:"at"`
}

// cliHeldReport is the held-question report body (GET
// /api/autonomous/held-questions). Open and shadow are always arrays.
type cliHeldReport struct {
	Mode   string          `json:"mode"`
	States []string        `json:"states"`
	Open   []cliHeldOpen   `json:"open"`
	Shadow []cliHeldShadow `json:"shadow"`
}

// renderHeldQuestions writes the human-readable held-question report: the
// mode line, then one block per open held question ("on" mode) and one line
// per shadow entry ("shadow" mode). With neither, a single "nothing held"
// line.
func renderHeldQuestions(w io.Writer, body []byte) error {
	var r cliHeldReport
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	var modeNote string
	switch r.Mode {
	case "on":
		modeNote = "— held questions are raised now; answer them with `datawatch question answer`"
	case "shadow":
		modeNote = "— shadow mode: the daemon logs would-be holds instead of raising them"
	case "off":
		modeNote = "— the daemon neither raises nor logs held questions"
	}
	_, _ = fmt.Fprintf(w, "held mode: %s (%s)%s\n", r.Mode, heldStatesLabel(r.States), modeNote)

	if len(r.Open) == 0 && len(r.Shadow) == 0 {
		_, _ = fmt.Fprintln(w, "nothing on the daemon is held right now")
		return nil
	}

	for _, q := range r.Open {
		line := q.QuestionID
		if q.State != "" {
			line += "  held: " + q.State
		}
		if q.Title != "" || q.AutomationID != "" {
			who := q.Title
			if q.AutomationID != "" {
				who = q.AutomationID
				if q.Title != "" {
					who += " (" + q.Title + ")"
				}
			}
			line += "  " + who
		}
		if q.TaskID != "" {
			line += "  task " + q.TaskID
		}
		_, _ = fmt.Fprintln(w, line)
		if q.Reason != "" {
			_, _ = fmt.Fprintln(w, "    why: "+q.Reason)
		}
		reco := q.RecommendedOption
		switch {
		case reco != "" && reco == q.Default:
			_, _ = fmt.Fprintln(w, "    recommended: "+reco+" (forced default)")
		case reco != "" && q.Default != "":
			_, _ = fmt.Fprintln(w, "    recommended: "+reco+"  (default: "+q.Default+")")
		case reco != "":
			_, _ = fmt.Fprintln(w, "    recommended: "+reco)
		case q.Default != "":
			_, _ = fmt.Fprintln(w, "    default: "+q.Default)
		}
	}

	for _, s := range r.Shadow {
		line := "shadow  " + s.State
		if s.PRDID != "" {
			line += "  " + s.PRDID
		}
		if s.RecommendedOption != "" {
			line += "  recommended: " + s.RecommendedOption
		}
		if s.At != "" {
			line += "  " + s.At
		}
		_, _ = fmt.Fprintln(w, line)
	}
	return nil
}

// commaJoin joins with ", " ("no" when empty).
// heldStatesLabel renders the held state list for the mode line.
func heldStatesLabel(vals []string) string {
	if len(vals) == 0 {
		return "no states"
	}
	return commaJoin(vals) + " states"
}

func commaJoin(vals []string) string {
	out := ""
	for i, v := range vals {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	if out == "" {
		return "no"
	}
	return out
}

// newQuestionHeldCmd builds `question held`: the held-question report.
func newQuestionHeldCmd() *cobra.Command {
	var (
		prd    string
		state  string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "held [--prd <prd-id>] [--state <held state id>]",
		Short: "Read the held-question report (mode, open holds, shadow entries)",
		Long: `Read the daemon's held-question report: the held-question mode in
effect (off, shadow or on) and the held states it covers, the held
questions it holds on the Automaton right now (with each one's held
state, reason and recommended option) and what it would have held in
shadow mode.

Read-only: nothing here acts on a hold — answering one is
` + "`" + `datawatch question answer` + "`" + `. The state filter takes one of the
daemon's held state ids (it rejects anything else); the prd filter
narrows to one Automaton.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{}
			if prd != "" {
				q.Set("prd_id", prd)
			}
			if state != "" {
				q.Set("state", state)
			}
			body, err := daemonJSONBody(http.MethodGet, "/api/autonomous/held-questions?"+q.Encode(), nil)
			if err != nil {
				return err
			}
			if asJSON {
				prettyPrint(body)
				return nil
			}
			return renderHeldQuestions(cmd.OutOrStdout(), body)
		},
	}
	cmd.Flags().StringVar(&prd, "prd", "", "only this Automaton's held questions")
	cmd.Flags().StringVar(&state, "state", "", "one of the five held state ids (the daemon rejects anything else)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return cmd
}
