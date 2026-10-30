// CLI for the operator question channel on the Automaton. Every subcommand
// calls the existing REST routes on the local daemon
// (internal/server/questions.go):
//
//	datawatch question list [--state <state>] [--context <ctx>] [--asker <who>] [--text <search>] [--limit n] [--json]
//	    → GET /api/questions
//	datawatch question show <id>
//	    → GET /api/questions/{id} + GET /api/questions/{id}/answers
//	datawatch question answer <id> (--option <option id> and/or --text <verbatim words>) [--supersedes <answer id>]
//	    → POST /api/questions/{id}/answer
//	datawatch question withdraw <id> --reason <text>
//	    → POST /api/questions/{id}/withdraw
//	datawatch question raise --text <text> --option id=label [--option id=label …] …
//	    → POST /api/questions
//
// The render functions (renderQuestionList, renderQuestionShow, …) are pure
// text builders over the response bodies, so the tests check the operator
// text without a daemon.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// ----- response shapes (the parts the CLI prints) ------------------------

// cliQuestionOption is one option on a question.
type cliQuestionOption struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Tradeoffs string `json:"tradeoffs"`
}

// cliQuestionRecommendation is the asker's suggested option and why.
type cliQuestionRecommendation struct {
	OptionID  string `json:"option_id"`
	Rationale string `json:"rationale"`
}

// cliQuestion is the question record as the CLI reads it back.
type cliQuestion struct {
	ID             string                     `json:"id"`
	Label          string                     `json:"label"`
	AskedBy        string                     `json:"asked_by"`
	AskerKind      string                     `json:"asker_kind"`
	Context        string                     `json:"context"`
	Text           string                     `json:"text"`
	Options        []cliQuestionOption        `json:"options"`
	Recommendation *cliQuestionRecommendation `json:"recommendation"`
	Default        string                     `json:"default"`
	Risk           string                     `json:"risk"`
	Priority       string                     `json:"priority"`
	State          string                     `json:"state"`
	CreatedAt      string                     `json:"created_at"`
}

// cliAnswerView is an answer plus its derived superseded state, as in the
// answer log (GET /api/questions/{id}/answers).
type cliAnswerView struct {
	ID             string `json:"id"`
	ChosenOptionID string `json:"chosen_option_id"`
	Quote          string `json:"quote"`
	Actor          string `json:"actor"`
	Surface        string `json:"surface"`
	Supersedes     string `json:"supersedes"`
	CreatedAt      string `json:"created_at"`
	Superseded     bool   `json:"superseded"`
	SupersededBy   string `json:"superseded_by"`
}

// optionLabelFor looks up the label of the option id on q (the id itself
// when there is no option with that id, so an out-of-band id still shows).
func optionLabelFor(q cliQuestion, id string) string {
	for _, o := range q.Options {
		if o.ID == id {
			return o.Label
		}
	}
	return id
}

// questionStateText returns the question's state for printing (open is the
// state a fresh question is in; the daemon always sets it, but stay safe).
func questionStateText(q cliQuestion) string {
	if q.State == "" {
		return "open"
	}
	return q.State
}

// renderQuestionList writes the human-readable form of a GET /api/questions
// body: one block per question, or "no questions match".
func renderQuestionList(w io.Writer, body []byte) error {
	var resp struct {
		Count     int           `json:"count"`
		Questions []cliQuestion `json:"questions"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	if len(resp.Questions) == 0 {
		_, _ = fmt.Fprintln(w, "no questions match")
		return nil
	}
	for _, q := range resp.Questions {
		line := q.ID + "  " + questionStateText(q)
		if q.Label != "" {
			line += "  " + q.Label
		}
		if q.Priority != "" {
			line += "  priority=" + q.Priority
		}
		_, _ = fmt.Fprintln(w, line)
		_, _ = fmt.Fprintln(w, "    "+q.Text)
		if q.AskedBy != "" {
			askedBy := q.AskedBy
			if q.AskerKind != "" {
				askedBy += " (" + q.AskerKind + ")"
			}
			_, _ = fmt.Fprintln(w, "    asked by: "+askedBy)
		}
	}
	return nil
}

// renderQuestionShow writes the operator view of one question: the
// question (text, numbered options with trade-offs, recommendation,
// default, state, priority, asked by), then the current answer and the
// full answer log with its superseded flags.
func renderQuestionShow(w io.Writer, showBody, answersBody []byte) error {
	var show struct {
		Question        cliQuestion    `json:"question"`
		AnswerCount     int            `json:"answer_count"`
		CurrentAnswerID string         `json:"current_answer_id"`
		Answer          *cliAnswerView `json:"answer"`
	}
	if err := json.Unmarshal(showBody, &show); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	var answers struct {
		Answers         []cliAnswerView `json:"answers"`
		CurrentAnswerID string          `json:"current_answer_id"`
	}
	if err := json.Unmarshal(answersBody, &answers); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}

	q := show.Question
	_, _ = fmt.Fprintf(w, "%s  state: %s\n", q.ID, questionStateText(q))
	if q.Label != "" {
		_, _ = fmt.Fprintf(w, "label: %s\n", q.Label)
	}
	_, _ = fmt.Fprintln(w, q.Text)
	if q.AskedBy != "" {
		askedBy := q.AskedBy
		if q.AskerKind != "" {
			askedBy += " (" + q.AskerKind + ")"
		}
		_, _ = fmt.Fprintf(w, "asked by: %s\n", askedBy)
	}
	if q.Priority != "" {
		_, _ = fmt.Fprintf(w, "priority: %s\n", q.Priority)
	}
	if q.Risk != "" {
		_, _ = fmt.Fprintf(w, "risk: %s\n", q.Risk)
	}

	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "options:")
	for i, o := range q.Options {
		_, _ = fmt.Fprintf(w, "  %d. %s  %s\n", i+1, o.ID, o.Label)
		if o.Tradeoffs != "" {
			_, _ = fmt.Fprintf(w, "     trade-offs: %s\n", o.Tradeoffs)
		}
	}
	if q.Recommendation != nil {
		rec := optionLabelFor(q, q.Recommendation.OptionID) + " (id " + q.Recommendation.OptionID + ")"
		if q.Recommendation.Rationale != "" {
			rec += ": " + q.Recommendation.Rationale
		}
		_, _ = fmt.Fprintln(w, "recommendation: "+rec)
	}
	if q.Default != "" {
		_, _ = fmt.Fprintf(w, "default: %s\n", optionLabelFor(q, q.Default))
	}

	_, _ = fmt.Fprintln(w, "")
	if show.Answer != nil {
		a := show.Answer
		_, _ = fmt.Fprintln(w, "current answer:")
		_, _ = fmt.Fprintf(w, "  id: %s\n", a.ID)
		if a.ChosenOptionID != "" {
			_, _ = fmt.Fprintf(w, "  option: %s (id %s)\n", optionLabelFor(q, a.ChosenOptionID), a.ChosenOptionID)
		}
		_, _ = fmt.Fprintf(w, "  quote: %q\n", a.Quote)
		if a.Actor != "" {
			who := a.Actor
			if a.Surface != "" {
				who += " (" + a.Surface + ")"
			}
			_, _ = fmt.Fprintf(w, "  by: %s\n", who)
		}
		if a.CreatedAt != "" {
			_, _ = fmt.Fprintf(w, "  at: %s\n", a.CreatedAt)
		}
	} else {
		_, _ = fmt.Fprintln(w, "current answer: (none yet)")
	}

	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintf(w, "answer log (%d):\n", len(answers.Answers))
	if len(answers.Answers) == 0 {
		_, _ = fmt.Fprintln(w, "  (no answers recorded)")
	}
	currentID := answers.CurrentAnswerID
	for _, a := range answers.Answers {
		mark := "earlier"
		if a.Superseded {
			mark = "superseded"
			if a.SupersededBy != "" {
				mark += " (by " + a.SupersededBy + ")"
			}
		} else if a.ID == currentID {
			mark = "current"
		}
		line := "  " + a.ID + "  " + mark
		if a.ChosenOptionID != "" {
			line += "  option: " + optionLabelFor(q, a.ChosenOptionID)
		}
		_, _ = fmt.Fprintln(w, line)
		if a.Quote != "" {
			_, _ = fmt.Fprintf(w, "    quote: %q\n", a.Quote)
		}
		if a.Actor != "" {
			_, _ = fmt.Fprintf(w, "    by: %s\n", a.Actor)
		}
	}
	return nil
}

// renderQuestionAnswer writes the confirmation of a recorded answer: the
// stored option label (when one was chosen) and the stored quote verbatim.
func renderQuestionAnswer(w io.Writer, body []byte) error {
	var resp struct {
		Answer   cliAnswerView `json:"answer"`
		Question cliQuestion   `json:"question"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	_, _ = fmt.Fprintf(w, "answer recorded: %s\n", resp.Answer.ID)
	if resp.Answer.ChosenOptionID != "" {
		_, _ = fmt.Fprintf(w, "  option: %s\n", optionLabelFor(resp.Question, resp.Answer.ChosenOptionID))
	}
	if resp.Answer.Quote != "" {
		_, _ = fmt.Fprintf(w, "  quote: %q\n", resp.Answer.Quote)
	}
	if resp.Question.State != "" {
		_, _ = fmt.Fprintf(w, "question is now: %s\n", resp.Question.State)
	}
	return nil
}

// renderQuestionWithdraw writes the confirmation of a withdrawn question.
func renderQuestionWithdraw(w io.Writer, body []byte) error {
	var resp struct {
		Question cliQuestion `json:"question"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	_, _ = fmt.Fprintf(w, "question %s withdrawn", resp.Question.ID)
	if resp.Question.State != "" {
		_, _ = fmt.Fprintf(w, " (state: %s)", resp.Question.State)
	}
	_, _ = fmt.Fprintln(w)
	return nil
}

// renderQuestionRaise writes the confirmation of a raised question.
func renderQuestionRaise(w io.Writer, body []byte) error {
	var resp struct {
		Question cliQuestion `json:"question"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	q := resp.Question
	_, _ = fmt.Fprintf(w, "question raised: %s\n", q.ID)
	if q.Label != "" {
		_, _ = fmt.Fprintf(w, "  label: %s\n", q.Label)
	}
	_, _ = fmt.Fprintf(w, "  options: %d\n", len(q.Options))
	if q.Default != "" {
		_, _ = fmt.Fprintf(w, "  default: %s\n", optionLabelFor(q, q.Default))
	}
	if q.State != "" {
		_, _ = fmt.Fprintf(w, "  state: %s\n", q.State)
	}
	return nil
}

// questionActor is the local login name the CLI records as the actor of an
// answer or withdraw: the $USER or $LOGNAME environment, else the OS user
// database entry.
func questionActor() string {
	if n := strings.TrimSpace(os.Getenv("USER")); n != "" {
		return n
	}
	if n := strings.TrimSpace(os.Getenv("LOGNAME")); n != "" {
		return n
	}
	if u, err := user.Current(); err == nil {
		if n := strings.TrimSpace(u.Username); n != "" {
			return n
		}
	}
	return "unknown"
}

// validateQuestionContext matches the raise's context with its link: the
// context names what the question is about and carries its link — context
// prd takes --prd-id, task --task-id, proposal --proposal-path,
// registry_row --registry-row-key, and none takes no links at all.
func validateQuestionContext(context, prdID, taskID, proposalPath, registryRowKey string) error {
	if context == "" {
		context = "none"
	}
	switch context {
	case "none":
		if prdID != "" || taskID != "" || proposalPath != "" || registryRowKey != "" {
			return fmt.Errorf("context none takes no links (drop --prd-id, --task-id, --proposal-path or --registry-row-key)")
		}
		return nil
	case "prd":
		if prdID == "" {
			return fmt.Errorf("context prd requires --prd-id (the link to the prd)")
		}
		return nil
	case "task":
		if taskID == "" {
			return fmt.Errorf("context task requires --task-id (the link to the task)")
		}
		return nil
	case "proposal":
		if proposalPath == "" {
			return fmt.Errorf("context proposal requires --proposal-path (the link to the proposal)")
		}
		return nil
	case "registry_row":
		if registryRowKey == "" {
			return fmt.Errorf("context registry_row requires --registry-row-key (the link to the registry row)")
		}
		return nil
	default:
		return fmt.Errorf("--context must be one of none, prd, task, proposal or registry_row (got %q)", context)
	}
}

// ----- the question command group ----------------------------------------

// newQuestionCmd builds the `question` command group: the operator question
// channel on the Automaton (list, show, answer, withdraw, raise).
func newQuestionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "question",
		Short: "Operator questions on the Automaton: list, show, answer, withdraw, raise",
		Long: `Raise, read and decide the operator questions the Automaton puts to
its operator. Questions are a decision channel: each question is one text,
a numbered set of options with trade-offs, a recommendation, and a forced
default (an option or hold). An answer is immutable — a changed mind is a
new answer that supersedes the old one, and the full answer log is
preserved.

Every read and write is gated on the autonomous.questions.enabled daemon
switch; while it is off the daemon answers with a 503 naming the switch.`,
	}
	cmd.AddCommand(
		newQuestionListCmd(),
		newQuestionShowCmd(),
		newQuestionAnswerCmd(),
		newQuestionWithdrawCmd(),
		newQuestionRaiseCmd(),
	)
	return cmd
}

// newQuestionListCmd builds `question list`.
func newQuestionListCmd() *cobra.Command {
	var (
		state   string
		context string
		asker   string
		text    string
		limit   int
		asJSON  bool
	)
	cmd := &cobra.Command{
		Use:   "list [--state <state>] [--context <ctx>] [--asker <who>] [--text <search>] [--limit n] [--json]",
		Short: "List the questions on the Automaton (default: the open ones)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if state == "" {
				state = "open"
			}
			switch state {
			case "open", "answered", "withdrawn", "expired", "all":
			default:
				return fmt.Errorf("--state must be one of open, answered, withdrawn, expired or all (got %q)", state)
			}
			switch context {
			case "", "none", "prd", "task", "proposal", "registry_row", "all":
			default:
				return fmt.Errorf("--context must be one of none, prd, task, proposal, registry_row or all (got %q)", context)
			}
			if limit < 0 {
				return fmt.Errorf("--limit must be positive (got %d)", limit)
			}

			q := url.Values{}
			if state == "all" {
				q.Set("state", "all") // the CLI default is open; ask for "all" when the operator wants every state
			} else {
				q.Set("state", state)
			}
			if context != "" {
				q.Set("context", context)
			}
			if asker != "" {
				q.Set("asker", asker)
			}
			if text != "" {
				q.Set("text", text)
			}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}

			body, err := daemonJSONBody(http.MethodGet, "/api/questions?"+q.Encode(), nil)
			if err != nil {
				return err
			}
			if asJSON {
				prettyPrint(body)
				return nil
			}
			return renderQuestionList(cmd.OutOrStdout(), body)
		},
	}
	cmd.Flags().StringVar(&state, "state", "open", "state filter: open, answered, withdrawn, expired or all (default open)")
	cmd.Flags().StringVar(&context, "context", "", "context filter: none, prd, task, proposal, registry_row or all (default: no filter)")
	cmd.Flags().StringVar(&asker, "asker", "", "only questions asked by this asker")
	cmd.Flags().StringVar(&text, "text", "", "search this text in the question's words")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum questions to return")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return cmd
}

// newQuestionShowCmd builds `question show`.
func newQuestionShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a question, its current answer, and its full answer log",
		Long: `Show one question in operator form: the text, the numbered options
with their trade-offs, the recommendation, the forced default, the state,
priority and asker. Below that, the currently effective answer (option
label and the operator's verbatim quote) and the full answer log, oldest
first, each answer flagged when a later answer supersedes it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("a question id is required")
			}
			escaped := url.PathEscape(id)
			showBody, err := daemonJSONBody(http.MethodGet, "/api/questions/"+escaped, nil)
			if err != nil {
				return err
			}
			answersBody, err := daemonJSONBody(http.MethodGet, "/api/questions/"+escaped+"/answers", nil)
			if err != nil {
				return err
			}
			return renderQuestionShow(cmd.OutOrStdout(), showBody, answersBody)
		},
	}
	return cmd
}

// newQuestionAnswerCmd builds `question answer`.
func newQuestionAnswerCmd() *cobra.Command {
	var (
		option     string
		text       string
		supersedes string
	)
	cmd := &cobra.Command{
		Use:   "answer <id> (--option <option id> and/or --text <verbatim words>) [--supersedes <answer id>]",
		Short: "Record an answer to a question, in the operator's own words",
		Long: `Answer one question. Name an option with --option, and/or say the
operator's own words verbatim with --text (sent exactly as given; they are
kept in full, never trimmed or re-encoded). At least one of the two is
needed. An already-answered question accepts only a new answer that names
its current answer with --supersedes: an answer is immutable, and a
changed mind is a superseding answer, never an edit.

The answer is recorded with surface "cli" and the local login name as the
actor.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("a question id is required")
			}
			if option == "" && strings.TrimSpace(text) == "" {
				return fmt.Errorf("an answer needs --option <option id> or a non-blank --text (the operator's own words, verbatim)")
			}
			body := map[string]any{
				"surface": "cli",
				"actor":   questionActor(),
				"quote":   text, // sent exactly as given, never trimmed
			}
			if option != "" {
				body["chosen_option_id"] = option
			}
			if supersedes != "" {
				body["supersedes"] = supersedes
			}
			resp, err := daemonJSONBody(http.MethodPost, "/api/questions/"+url.PathEscape(id)+"/answer", body)
			if err != nil {
				return err
			}
			return renderQuestionAnswer(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().StringVar(&option, "option", "", "the chosen option id (one of the question's options)")
	cmd.Flags().StringVar(&text, "text", "", "the operator's own words, verbatim (never trimmed)")
	cmd.Flags().StringVar(&supersedes, "supersedes", "", "the current answer id being superseded (required when the question is already answered)")
	return cmd
}

// newQuestionWithdrawCmd builds `question withdraw`.
func newQuestionWithdrawCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "withdraw <id> --reason <text>",
		Short: "Withdraw an open question, with the reason of record",
		Long: `Withdraw one open question. The reason is required: it is the record
of why the question no longer stands, and it is stored verbatim with the
question. Withdrawn is terminal — the question accepts no answer after
that.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("a question id is required")
			}
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("a withdraw needs --reason <text> naming why the question no longer stands")
			}
			body := map[string]any{
				"reason": reason,
				"actor":  questionActor(),
			}
			resp, err := daemonJSONBody(http.MethodPost, "/api/questions/"+url.PathEscape(id)+"/withdraw", body)
			if err != nil {
				return err
			}
			return renderQuestionWithdraw(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the question no longer stands (required)")
	return cmd
}

// newQuestionRaiseCmd builds `question raise`.
func newQuestionRaiseCmd() *cobra.Command {
	var (
		text           string
		label          string
		options        []string
		recommend      string
		rationale      string
		def            string // --default
		risk           string
		priority       string
		context        string
		prdID          string
		taskID         string
		proposalPath   string
		registryRowKey string
		asker          string
		askerKind      string
		session        string
		returnPath     bool
	)
	cmd := &cobra.Command{
		Use:   "raise --text <text> --option id=label [--option id=label …] [--default <option id or hold>]",
		Short: "Raise a question to the operator of the Automaton",
		Long: `Raise one question: a line of text, at least two options
(--option id=label, repeatable), the asker's recommendation
(--recommend <option id> with --rationale), and a forced default
(--default, an option id or the word hold, so no choice is ever silent).

The context names what the question is about and carries its link:
prd + --prd-id, task + --task-id, proposal + --proposal-path,
registry_row + --registry-row-key, or none (no links).

--session names the asking session; --return-path opts this one
question in to the session line (one DATAWATCH_ANSWER: line into the
asking session when it is answered) and therefore requires --session.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(text) == "" {
				return fmt.Errorf("a question needs --text <text>")
			}
			if len(options) < 2 {
				return fmt.Errorf("a question needs at least two --option id=Label pairs (got %d)", len(options))
			}
			parsed := make([]map[string]any, 0, len(options))
			optionIDs := make([]string, 0, len(options))
			for _, pair := range options {
				id, lbl, ok := strings.Cut(pair, "=")
				if !ok || id == "" || lbl == "" {
					return fmt.Errorf("each --option must be an id=label pair (got %q)", pair)
				}
				parsed = append(parsed, map[string]any{"id": id, "label": lbl})
				optionIDs = append(optionIDs, id)
			}
			inOptions := func(id string) bool {
				for _, have := range optionIDs {
					if have == id {
						return true
					}
				}
				return false
			}
			if recommend != "" && !inOptions(recommend) {
				return fmt.Errorf("--recommend names option %q, which is not among the --option ids", recommend)
			}
			if def == "" {
				return fmt.Errorf("a question needs --default <option id or hold> — no choice is ever silent")
			}
			if def != "hold" && !inOptions(def) {
				return fmt.Errorf("--default %q is neither an --option id nor hold", def)
			}
			switch priority {
			case "":
			case "normal", "high":
			default:
				return fmt.Errorf("--priority must be normal or high (got %q)", priority)
			}
			switch risk {
			case "":
			case "low", "normal":
			default:
				return fmt.Errorf("--risk must be low or normal (got %q)", risk)
			}
			if returnPath && session == "" {
				return fmt.Errorf("--return-path requires --session <id>: the session line needs the asking session")
			}
			if askerKind != "" && asker == "" {
				return fmt.Errorf("--asker-kind requires --asker (who is asking, by name)")
			}
			if err := validateQuestionContext(context, prdID, taskID, proposalPath, registryRowKey); err != nil {
				return err
			}

			body := map[string]any{
				"text":        text,
				"context":     context,
				"options":     parsed,
				"default":     def,
				"return_path": returnPath,
			}
			if label != "" {
				body["label"] = label
			}
			if recommend != "" {
				rec := map[string]any{"option_id": recommend}
				if rationale != "" {
					rec["rationale"] = rationale
				}
				body["recommendation"] = rec
			}
			if risk != "" {
				body["risk"] = risk
			}
			if priority != "" {
				body["priority"] = priority
			}
			if prdID != "" {
				body["prd_id"] = prdID
			}
			if taskID != "" {
				body["task_id"] = taskID
			}
			if proposalPath != "" {
				body["proposal_path"] = proposalPath
			}
			if registryRowKey != "" {
				body["registry_row_key"] = registryRowKey
			}
			if asker != "" {
				body["asked_by"] = asker
			}
			if askerKind != "" {
				body["asker_kind"] = askerKind
			}
			if session != "" {
				body["session_id"] = session
			}

			resp, err := daemonJSONBody(http.MethodPost, "/api/questions", body)
			if err != nil {
				return err
			}
			return renderQuestionRaise(cmd.OutOrStdout(), resp)
		},
	}
	f := cmd.Flags()
	f.StringVar(&text, "text", "", "the question as a line of text (required)")
	f.StringVar(&label, "label", "", "the question's label (default: decision, or set it yourself)")
	f.StringArrayVar(&options, "option", nil, "an option as id=label (repeatable, at least two)")
	f.StringVar(&recommend, "recommend", "", "the recommended option id (one of the --option ids)")
	f.StringVar(&rationale, "rationale", "", "why the recommended option is the right one (with --recommend)")
	f.StringVar(&def, "default", "", "the forced default: an option id or the word hold (required)")
	f.StringVar(&risk, "risk", "", "how dangerous a wrong answer is: low or normal (default normal)")
	f.StringVar(&priority, "priority", "", "how urgent this is for the operator: normal or high (default normal)")
	f.StringVar(&context, "context", "none", "what the question is about: none, prd, task, proposal or registry_row (with the matching link)")
	f.StringVar(&prdID, "prd-id", "", "the prd link (context prd)")
	f.StringVar(&taskID, "task-id", "", "the task link (context task)")
	f.StringVar(&proposalPath, "proposal-path", "", "the proposal link (context proposal)")
	f.StringVar(&registryRowKey, "registry-row-key", "", "the registry_row link (context registry_row)")
	f.StringVar(&asker, "asker", "", "who is asking (default: the local login name)")
	f.StringVar(&askerKind, "asker-kind", "", "the asker's kind: session, supervisor, operator or system (default operator)")
	f.StringVar(&session, "session", "", "the asking session id")
	f.BoolVar(&returnPath, "return-path", false, "opt this question in to the answer line into the asking session (requires --session)")
	return cmd
}
