// CLI for the decision records on the Automaton. Every subcommand calls the
// decision record routes on the local daemon
// (internal/server/decision_records.go):
//
//	datawatch autonomous decision-records list [--state <state>] [--automaton <id>] [--limit n] [--json]
//	    → GET /api/autonomous/decision-records
//	datawatch autonomous decision-records show <id>
//	    → GET /api/autonomous/decision-records/{id}
//	datawatch autonomous decision-records create (--title … --situation … --deciding … --option id=label … --option id=label … --default <id or hold> [more flags]) | (--file <path|->)
//	    → POST /api/autonomous/decision-records
//	datawatch autonomous decision-records update <id> [the same content inputs]
//	    → PUT /api/autonomous/decision-records/{id}
//	datawatch autonomous decision-records withdraw <id> [--reason <text>]
//	    → POST /api/autonomous/decision-records/{id}/withdraw
//
// The render functions (renderDecisionRecordList, renderDecisionRecordShow,…)
// are pure text builders over the response bodies, so the tests check the
// operator text without a daemon.

package main

import (
	"bytes"
	"encoding/json"
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

// cliDecRecordOption is one option on a decision record: a short label and
// one line of cost and risk.
type cliDecRecordOption struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Tradeoff string `json:"tradeoff"`
}

// cliDecRecordRecommendation names the option the author recommends and why.
type cliDecRecordRecommendation struct {
	OptionID string `json:"option_id"`
	Reason   string `json:"reason"`
}

// cliDecRecordLinks ties a record to the work it is about.
type cliDecRecordLinks struct {
	Doc         string `json:"doc"`
	Automaton   string `json:"automaton"`
	RegistryRow string `json:"registry_row"`
}

// cliDecOption is the option an answer chose, with what it cost.
type cliDecOption struct {
	AnswerID       string `json:"answer_id"`
	ChosenOptionID string `json:"chosen_option_id"`
	Quote          string `json:"quote"`
	Actor          string `json:"actor"`
	Surface        string `json:"surface"`
	AnsweredAt     string `json:"answered_at"`
}

// cliDecDocChange is what was done to the linked document after the answer.
type cliDecDocChange struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
	Status string `json:"status"`
	At     string `json:"at"`
}

// cliDecFollowUp is the follow-up the operator asked for and its outcome.
type cliDecFollowUp struct {
	Requested bool   `json:"requested"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Target    string `json:"target"`
	Status    string `json:"status"`
	Result    string `json:"result"`
	At        string `json:"at"`
}

// cliDecRecord is a decision record as the daemon sends it: the content, the
// bookkeeping, and the answer, document report and follow-up once they are
// set, plus the state of its queue question.
type cliDecRecord struct {
	ID             string                      `json:"id"`
	Label          string                      `json:"label"`
	Title          string                      `json:"title"`
	Situation      string                      `json:"situation"`
	Deciding       string                      `json:"deciding"`
	Options        []cliDecRecordOption        `json:"options"`
	Recommendation *cliDecRecordRecommendation `json:"recommendation"`
	Default        string                      `json:"default"`
	Links          cliDecRecordLinks           `json:"links"`
	State          string                      `json:"state"`
	QuestionID     string                      `json:"question_id"`
	Source         string                      `json:"source"`
	Answer         *cliDecOption               `json:"answer"`
	DocChange      *cliDecDocChange            `json:"doc_change"`
	FollowUp       *cliDecFollowUp             `json:"follow_up"`
	CreatedAt      string                      `json:"created_at"`
	UpdatedAt      string                      `json:"updated_at"`
	QuestionState  string                      `json:"question_state"`
}

// cliDecFolderProblem is one file (or the folder itself) that a scan could
// not use, or a note about one.
type cliDecFolderProblem struct {
	File    string `json:"file"`
	Message string `json:"message"`
}

// cliDecFolder is the folder watch status that rides on every list answer.
type cliDecFolder struct {
	Enabled   bool                  `json:"enabled"`
	Path      string                `json:"path"`
	FilesSeen int                   `json:"files_seen"`
	Errors    []cliDecFolderProblem `json:"errors"`
	Notes     []cliDecFolderProblem `json:"notes"`
	LastScan  string                `json:"last_scan"`
}

// decRecordOptionLabel looks up the label of an option id on r (the id
// itself when there is no option with that id, so an out-of-band id still
// shows).
func decRecordOptionLabel(r cliDecRecord, id string) string {
	for _, o := range r.Options {
		if o.ID == id {
			return o.Label
		}
	}
	return id
}

// oneLine cuts s to a single line so a report line stays one line.
func decOneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

// ----- rendering ----------------------------------------------------------

// renderDecisionRecordList writes the list: one block per record with the id,
// the state, the title, the queue question's state and, for an answered
// record, who answered and on what surface, the quote in full, the document
// report and the follow-up's outcome. When the folder watch is on, the
// folder's status line comes last. Empty matches print a plain line.
func decRecordPrintJSON(w io.Writer, body []byte) {
	var v any
	if err := json.Unmarshal(body, &v); err == nil {
		out, _ := json.MarshalIndent(v, "", "  ")
		_, _ = fmt.Fprintln(w, string(out))
		return
	}
	_, _ = w.Write(body)
}

func renderDecisionRecordList(w io.Writer, body []byte) error {
	var resp struct {
		DecisionRecords []cliDecRecord `json:"decision_records"`
		Folder          cliDecFolder   `json:"folder"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	if len(resp.DecisionRecords) == 0 {
		_, _ = fmt.Fprintln(w, "no decision records match")
	}
	for _, r := range resp.DecisionRecords {
		state := r.State
		if state == "" {
			state = "open"
		}
		_, _ = fmt.Fprintf(w, "%s  %s  %s\n", r.ID, state, r.Title)
		if r.Label != "" && r.Label != "decision" {
			_, _ = fmt.Fprintf(w, "    label: %s\n", r.Label)
		}
		qstate := r.QuestionState
		if qstate == "" {
			qstate = "open"
		}
		_, _ = fmt.Fprintf(w, "    question: %s", qstate)
		if r.QuestionID != "" {
			_, _ = fmt.Fprintf(w, " (%s)", r.QuestionID)
		}
		_, _ = fmt.Fprintln(w)
		if r.Answer != nil {
			a := r.Answer
			if a.ChosenOptionID != "" {
				_, _ = fmt.Fprintf(w, "    answered with: %s (id %s)\n", decRecordOptionLabel(r, a.ChosenOptionID), a.ChosenOptionID)
			}
			if a.Quote != "" {
				_, _ = fmt.Fprintf(w, "    quote: %q\n", a.Quote)
			}
			who := a.Actor
			if a.Surface != "" {
				who += " (" + a.Surface + ")"
			}
			if who != "" {
				_, _ = fmt.Fprintf(w, "    by: %s\n", who)
			}
			if a.AnsweredAt != "" {
				_, _ = fmt.Fprintf(w, "    at: %s\n", a.AnsweredAt)
			}
		}
		if r.DocChange != nil {
			d := r.DocChange
			_, _ = fmt.Fprintf(w, "    document: %s", d.Status)
			if d.Path != "" {
				_, _ = fmt.Fprintf(w, " — %s", d.Path)
			}
			if d.Before != "" {
				_, _ = fmt.Fprintf(w, "\n      before: %q", decOneLine(d.Before))
			}
			if d.After != "" {
				_, _ = fmt.Fprintf(w, "\n      after:  %q", decOneLine(d.After))
			}
			_, _ = fmt.Fprintln(w)
		}
		if r.FollowUp != nil && r.FollowUp.Requested {
			f := r.FollowUp
			line := "    follow-up: "
			if f.Name == "" {
				line += "(requested)"
			} else {
				line += f.Name
			}
			if f.Kind != "" || f.Target != "" {
				line += " [kind " + f.Kind + ", target " + f.Target + "]"
			}
			if f.Status != "" {
				line += " — " + f.Status
			}
			if f.Result != "" {
				line += ": " + decOneLine(f.Result)
			}
			_, _ = fmt.Fprintln(w, line)
		}
		_, _ = fmt.Fprintln(w)
	}

	if resp.Folder.Enabled {
		_, _ = fmt.Fprintf(w, "folder watch: on, %d file(s) seen", resp.Folder.FilesSeen)
		if resp.Folder.Path != "" {
			_, _ = fmt.Fprintf(w, ", path %s", resp.Folder.Path)
		}
		if resp.Folder.LastScan != "" {
			_, _ = fmt.Fprintf(w, ", last scan %s", resp.Folder.LastScan)
		}
		_, _ = fmt.Fprintln(w)
		for _, p := range resp.Folder.Errors {
			_, _ = fmt.Fprintf(w, "    error: %s: %s\n", orDash(p.File), p.Message)
		}
		for _, p := range resp.Folder.Notes {
			_, _ = fmt.Fprintf(w, "    note: %s: %s\n", orDash(p.File), p.Message)
		}
	}
	return nil
}

// orDash gives a file-less problem a dash so the line still reads.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// renderDecisionRecordShow writes one record in full: the content (title,
// situation, deciding, options with their cost line, recommendation, default,
// links), the state and bookkeeping, and below that the answer, the document
// report and the follow-up.
func renderDecisionRecordShow(w io.Writer, body []byte) error {
	var resp struct {
		Record     cliDecRecord `json:"record"`
		QuestionID string       `json:"question_id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	r := resp.Record
	state := r.State
	if state == "" {
		state = "open"
	}
	_, _ = fmt.Fprintf(w, "%s  state: %s\n", r.ID, state)
	if r.Label != "" && r.Label != "decision" {
		_, _ = fmt.Fprintf(w, "label: %s\n", r.Label)
	}
	_, _ = fmt.Fprintf(w, "title: %s\n", r.Title)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, r.Situation)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "deciding: %s\n", r.Deciding)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "options:")
	for i, o := range r.Options {
		_, _ = fmt.Fprintf(w, "  %d. %s  %s\n", i+1, o.ID, o.Label)
		if o.Tradeoff != "" {
			_, _ = fmt.Fprintf(w, "     cost: %s\n", o.Tradeoff)
		}
	}
	if r.Recommendation != nil {
		rec := decRecordOptionLabel(r, r.Recommendation.OptionID) + " (id " + r.Recommendation.OptionID + ")"
		if r.Recommendation.Reason != "" {
			rec += ": " + r.Recommendation.Reason
		}
		_, _ = fmt.Fprintln(w, "recommendation: "+rec)
	}
	if r.Default != "" {
		_, _ = fmt.Fprintf(w, "default: %s\n", decRecordOptionLabel(r, r.Default))
	}
	if l := r.Links; l.Doc != "" || l.Automaton != "" || l.RegistryRow != "" {
		parts := make([]string, 0, 3)
		if l.Doc != "" {
			parts = append(parts, "doc "+l.Doc)
		}
		if l.Automaton != "" {
			parts = append(parts, "automaton "+l.Automaton)
		}
		if l.RegistryRow != "" {
			parts = append(parts, "registry_row "+l.RegistryRow)
		}
		_, _ = fmt.Fprintln(w, "links: "+strings.Join(parts, ", "))
	}
	if r.Source != "" {
		_, _ = fmt.Fprintf(w, "source: %s\n", r.Source)
	}
	qid := r.QuestionID
	if qid == "" {
		qid = resp.QuestionID
	}
	qstate := r.QuestionState
	if qstate == "" {
		qstate = state
	}
	if qid != "" {
		_, _ = fmt.Fprintf(w, "question: %s (state: %s)\n", qid, qstate)
	}

	if r.Answer != nil {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "answer:")
		if r.Answer.AnswerID != "" {
			_, _ = fmt.Fprintf(w, "  id: %s\n", r.Answer.AnswerID)
		}
		if r.Answer.ChosenOptionID != "" {
			_, _ = fmt.Fprintf(w, "  option: %s (id %s)\n", decRecordOptionLabel(r, r.Answer.ChosenOptionID), r.Answer.ChosenOptionID)
		}
		_, _ = fmt.Fprintf(w, "  quote: %q\n", r.Answer.Quote)
		who := r.Answer.Actor
		if r.Answer.Surface != "" {
			who += " (" + r.Answer.Surface + ")"
		}
		if who != "" {
			_, _ = fmt.Fprintf(w, "  by: %s\n", who)
		}
		if r.Answer.AnsweredAt != "" {
			_, _ = fmt.Fprintf(w, "  at: %s\n", r.Answer.AnsweredAt)
		}
	}

	if r.DocChange != nil {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintf(w, "document: %s\n", r.DocChange.Status)
		if r.DocChange.Path != "" {
			_, _ = fmt.Fprintf(w, "  path: %s\n", r.DocChange.Path)
		}
		if r.DocChange.Before != "" {
			_, _ = fmt.Fprintf(w, "  before: %s\n", r.DocChange.Before)
		}
		if r.DocChange.After != "" {
			_, _ = fmt.Fprintf(w, "  after:  %s\n", r.DocChange.After)
		}
		if r.DocChange.At != "" {
			_, _ = fmt.Fprintf(w, "  at: %s\n", r.DocChange.At)
		}
	}

	if r.FollowUp != nil && r.FollowUp.Requested {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "follow-up:")
		if r.FollowUp.Name != "" {
			_, _ = fmt.Fprintf(w, "  requested: %s\n", r.FollowUp.Name)
		} else {
			_, _ = fmt.Fprintln(w, "  requested (no entry named)")
		}
		if r.FollowUp.Kind != "" {
			_, _ = fmt.Fprintf(w, "  kind: %s\n", r.FollowUp.Kind)
		}
		if r.FollowUp.Target != "" {
			_, _ = fmt.Fprintf(w, "  target: %s\n", r.FollowUp.Target)
		}
		if r.FollowUp.Status != "" {
			_, _ = fmt.Fprintf(w, "  status: %s\n", r.FollowUp.Status)
		}
		if r.FollowUp.Result != "" {
			_, _ = fmt.Fprintf(w, "  result: %s\n", r.FollowUp.Result)
		}
	}
	return nil
}

// renderDecisionRecordSaved writes the create/update confirmation: the record
// id with its verb, and the queue question id the daemon raised (or holds)
// for it.
func renderDecisionRecordSaved(w io.Writer, verb string, body []byte) error {
	var resp struct {
		Record     cliDecRecord `json:"record"`
		QuestionID string       `json:"question_id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	_, _ = fmt.Fprintf(w, "%s: %s\n", verb, resp.Record.ID)
	q := resp.QuestionID
	if q == "" {
		q = resp.Record.QuestionID
	}
	if q != "" {
		_, _ = fmt.Fprintf(w, "  question: %s\n", q)
	}
	if resp.Record.State != "" {
		_, _ = fmt.Fprintf(w, "  state: %s\n", resp.Record.State)
	}
	return nil
}

// renderDecisionRecordWarnings writes the warnings of a stored-with-warning
// answer to w, one plain line each, so the caller can send them to standard
// error. A response without warnings writes nothing.
func renderDecisionRecordWarnings(w io.Writer, body []byte) {
	var resp struct {
		Warnings []cliQuestionFinding `json:"warnings"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return
	}
	for _, f := range resp.Warnings {
		_, _ = fmt.Fprintf(w, "warning: %s\n", f.line())
	}
}

// renderDecisionRecordWithdraw writes the withdraw confirmation.
func renderDecisionRecordWithdraw(w io.Writer, reason string, body []byte) error {
	var resp struct {
		Record     cliDecRecord `json:"record"`
		QuestionID string       `json:"question_id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	r := resp.Record
	state := r.State
	if state == "" {
		state = "withdrawn"
	}
	_, _ = fmt.Fprintf(w, "decision record %s withdrawn (state: %s)\n", r.ID, state)
	if reason != "" {
		_, _ = fmt.Fprintf(w, "  reason: %s\n", reason)
	}
	q := r.QuestionID
	if q == "" {
		q = resp.QuestionID
	}
	if q != "" {
		_, _ = fmt.Fprintf(w, "  question: %s\n", q)
	}
	return nil
}

// ----- content: the create and update inputs ------------------------------

// decRecordContent is one record's content as the flags or a --file give it:
// the same fields the daemon's body accepts, nothing else.
type decRecordContent struct {
	Label          string
	Title          string
	Situation      string
	Deciding       string
	Options        []cliDecRecordOption
	Recommendation *cliDecRecordRecommendation
	Default        string
	Doc            string
	Automaton      string
	RegistryRow    string
}

// decRecordFileJSON is the --file shape: the record's content fields exactly
// as the daemon names them. A field outside this set is an error.
type decRecordFileJSON struct {
	Label          string                      `json:"label"`
	Title          string                      `json:"title"`
	Situation      string                      `json:"situation"`
	Deciding       string                      `json:"deciding"`
	Options        []cliDecRecordOption        `json:"options"`
	Recommendation *cliDecRecordRecommendation `json:"recommendation"`
	Default        string                      `json:"default"`
	Links          struct {
		Doc         string `json:"doc"`
		Automaton   string `json:"automaton"`
		RegistryRow string `json:"registry_row"`
	} `json:"links"`
}

// readDecRecordFile reads the content from --file (a path, or stdin on -).
func readDecRecordFile(p string) (decRecordContent, error) {
	var data []byte
	var err error
	if p == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(p)
	}
	if err != nil {
		return decRecordContent{}, fmt.Errorf("decision file: %w", err)
	}
	var f decRecordFileJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return decRecordContent{}, fmt.Errorf("decision file: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return decRecordContent{}, fmt.Errorf("decision file: expected exactly one JSON object")
	}
	return decRecordContent{
		Label:          f.Label,
		Title:          f.Title,
		Situation:      f.Situation,
		Deciding:       f.Deciding,
		Options:        f.Options,
		Recommendation: f.Recommendation,
		Default:        f.Default,
		Doc:            f.Links.Doc,
		Automaton:      f.Links.Automaton,
		RegistryRow:    f.Links.RegistryRow,
	}, nil
}

// decRecordFromFlags builds the content from the command-line flags. The
// flag names and value formats follow `question raise`: --option id=label
// (repeatable), --tradeoff id=text (repeatable, one per option), --recommend
// option id, --rationale text, --default id or hold.
func decRecordFromFlags(label, title, situation, deciding, def string, option, tradeoff []string, recommend, rationale, doc, automaton, registryRow string) (decRecordContent, error) {
	if strings.TrimSpace(title) == "" {
		return decRecordContent{}, fmt.Errorf("a decision record needs --title <short name>")
	}
	if strings.TrimSpace(situation) == "" {
		return decRecordContent{}, fmt.Errorf("a decision record needs --situation <what is true>")
	}
	if strings.TrimSpace(deciding) == "" {
		return decRecordContent{}, fmt.Errorf("a decision record needs --deciding <what this decides>")
	}
	opts := []cliDecRecordOption{}
	seen := map[string]bool{}
	for _, v := range option {
		id, lbl, ok := strings.Cut(v, "=")
		if !ok || strings.TrimSpace(id) == "" || lbl == "" {
			return decRecordContent{}, fmt.Errorf("each --option must be an id=label pair (got %q)", v)
		}
		if seen[id] {
			return decRecordContent{}, fmt.Errorf("option id %q appears twice", id)
		}
		seen[id] = true
		opts = append(opts, cliDecRecordOption{ID: id, Label: lbl})
	}
	if len(opts) < 2 {
		return decRecordContent{}, fmt.Errorf("a decision record needs at least two --option id=label pairs (got %d)", len(opts))
	}
	for _, v := range tradeoff {
		id, text, ok := strings.Cut(v, "=")
		if !ok || strings.TrimSpace(id) == "" {
			return decRecordContent{}, fmt.Errorf("each --tradeoff must be an id=text pair, with a non-empty option id before the = (got %q)", v)
		}
		if !seen[id] {
			return decRecordContent{}, fmt.Errorf("--tradeoff names option %q, a decision option is %s", id, strings.Join(idsOf(opts), ", "))
		}
		for i := range opts {
			if opts[i].ID == id {
				if opts[i].Tradeoff != "" {
					return decRecordContent{}, fmt.Errorf("option id %q has two --tradeoff values; give each option one", id)
				}
				opts[i].Tradeoff = text
				break
			}
		}
	}
	if def == "" {
		return decRecordContent{}, fmt.Errorf("a decision record needs --default <option id or hold> — no choice is ever silent")
	}
	if def != "hold" && !seen[def] {
		return decRecordContent{}, fmt.Errorf("--default %q is neither an --option id nor hold", def)
	}
	var rec *cliDecRecordRecommendation
	if recommend != "" {
		if !seen[recommend] {
			return decRecordContent{}, fmt.Errorf("--recommend names option %q, which is not among the --option ids", recommend)
		}
		if rationale == "" {
			return decRecordContent{}, fmt.Errorf("--recommend %q needs --rationale: say why this option is the recommendation", recommend)
		}
		rec = &cliDecRecordRecommendation{OptionID: recommend, Reason: rationale}
	}
	return decRecordContent{
		Label:          label,
		Title:          title,
		Situation:      situation,
		Deciding:       deciding,
		Options:        opts,
		Recommendation: rec,
		Default:        def,
		Doc:            doc,
		Automaton:      automaton,
		RegistryRow:    registryRow,
	}, nil
}

// idsOf lists the option ids of opts in order.
func idsOf(opts []cliDecRecordOption) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.ID)
	}
	return out
}

// validate checks the rules the daemon refuses (title, situation and
// deciding are required for flag and file inputs alike, as are the other
// content rules), so a bad input does not make a round trip.
func (c decRecordContent) validate() error {
	if strings.TrimSpace(c.Title) == "" {
		return fmt.Errorf("a decision record needs a title")
	}
	if strings.TrimSpace(c.Situation) == "" {
		return fmt.Errorf("a decision record needs a situation")
	}
	if strings.TrimSpace(c.Deciding) == "" {
		return fmt.Errorf("a decision record needs a deciding line")
	}
	seen := map[string]bool{}
	for _, o := range c.Options {
		if o.ID == "" {
			return fmt.Errorf("every option needs an id")
		}
		if seen[o.ID] {
			return fmt.Errorf("option id %q appears twice", o.ID)
		}
		seen[o.ID] = true
	}
	if len(c.Options) < 2 {
		return fmt.Errorf("a decision record needs at least two options")
	}
	if c.Default == "" {
		return fmt.Errorf("a decision record needs a default: an option id, or hold")
	}
	if c.Default != "hold" && !seen[c.Default] {
		return fmt.Errorf("the default %q is not one of the option ids (%s) or hold", c.Default, strings.Join(idsOf(c.Options), ", "))
	}
	if c.Recommendation != nil && !seen[c.Recommendation.OptionID] {
		return fmt.Errorf("the recommendation names option %q, which is not among the option ids", c.Recommendation.OptionID)
	}
	return nil
}

// body is the decision record body for the create and update routes: the
// content fields exactly as the daemon names them.
func (c decRecordContent) body() map[string]any {
	b := map[string]any{
		"title":     c.Title,
		"situation": c.Situation,
		"deciding":  c.Deciding,
		"options":   c.Options,
		"default":   c.Default,
	}
	if c.Label != "" {
		b["label"] = c.Label
	}
	if c.Recommendation != nil {
		b["recommendation"] = c.Recommendation
	}
	links := map[string]any{}
	if c.Doc != "" {
		links["doc"] = c.Doc
	}
	if c.Automaton != "" {
		links["automaton"] = c.Automaton
	}
	if c.RegistryRow != "" {
		links["registry_row"] = c.RegistryRow
	}
	if len(links) > 0 {
		b["links"] = links
	}
	return b
}

// ----- commands -----------------------------------------------------------

// newDecisionRecordsCmd is the decision-records group under `autonomous`:
//
//	list, show <id>, create, update <id>, withdraw <id>.
func newDecisionRecordsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decision-records",
		Short: "Decision records: ask, answer, document, follow up",
		Long: `Decision records on the Automaton: a question with real trade-offs, answered by an
operator, the record of the answer, what changed in the linked document and the
follow-up that was asked for.

  datawatch autonomous decision-records list [--state open|answered|withdrawn] [--automaton <prd id>] [--limit n] [--json]
  datawatch autonomous decision-records show <id>
  datawatch autonomous decision-records create (--title … --situation … --deciding … --option id=label --option id=label --default <id or hold> …) | (--file <path|->)
  datawatch autonomous decision-records update <id> [the same content inputs]
  datawatch autonomous decision-records withdraw <id> [--reason <text>]

When the daemon reports 503 for these routes, decision records are turned off:
set autonomous.decisions.enabled to true in the daemon config and restart it.`,
	}
	cmd.AddCommand(newDecisionRecordListCmd(), newDecisionRecordShowCmd(), newDecisionRecordCreateCmd(), newDecisionRecordUpdateCmd(), newDecisionRecordWithdrawCmd())
	return cmd
}

// ----- list ---------------------------------------------------------------

func newDecisionRecordListCmd() *cobra.Command {
	var state, automaton string
	var limit int
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the Automaton's decision records",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if state != "" && state != "open" && state != "answered" && state != "withdrawn" {
				return fmt.Errorf("bad value for --state: %q — use open, answered or withdrawn", state)
			}
			if limit < 0 {
				return fmt.Errorf("--limit must be 0 (all) or larger")
			}
			q := url.Values{}
			if state != "" {
				q.Set("state", state)
			}
			if automaton != "" {
				q.Set("prd_id", automaton)
			}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}
			suffix := ""
			if enc := q.Encode(); enc != "" {
				suffix = "?" + enc
			}
			path := "/api/autonomous/decision-records" + suffix

			body, err := daemonJSONBody(http.MethodGet, path, nil)
			if err != nil {
				return err
			}
			if jsonOut {
				decRecordPrintJSON(cmd.OutOrStdout(), body)
				return nil
			}
			return renderDecisionRecordList(cmd.OutOrStdout(), body)
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "Filter by state: open, answered, withdrawn")
	cmd.Flags().StringVar(&automaton, "automaton", "", "Only records that name this Automaton (links.automaton; daemon filter prd_id)")
	cmd.Flags().IntVar(&limit, "limit", 0, "How many records to return (positive; the daemon caps at 1000)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON instead of the rendered list")
	return cmd
}

// ----- show ---------------------------------------------------------------

func newDecisionRecordShowCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show one decision record: content, answer, document report, follow-up",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			body, err := daemonJSONBody(http.MethodGet, "/api/autonomous/decision-records/"+url.PathEscape(id), nil)
			if err != nil {
				return err
			}
			if jsonOut {
				decRecordPrintJSON(cmd.OutOrStdout(), body)
				return nil
			}
			return renderDecisionRecordShow(cmd.OutOrStdout(), body)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON instead of the rendered record")
	return cmd
}

// ----- create / update ------------------------------------------------------

// decRecordContentFlags are the content inputs of create and update.
type decRecordContentFlags struct {
	label, title, situation, deciding, def string
	option, tradeoff                       []string
	recommend, rationale, file             string
	doc, automaton, registryRow            string
}

func contentFromFlagsOrFile(f decRecordContentFlags) (decRecordContent, error) {
	if f.file != "" {
		if f.option != nil || f.tradeoff != nil {
			return decRecordContent{}, fmt.Errorf("--file takes the whole content; --option and --tradeoff are for flag mode")
		}
		return readDecRecordFile(f.file)
	}
	return decRecordFromFlags(f.label, f.title, f.situation, f.deciding, f.def, f.option, f.tradeoff, f.recommend, f.rationale, f.doc, f.automaton, f.registryRow)
}

func newDecisionRecordCreateCmd() *cobra.Command {
	var f decRecordContentFlags
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Raise a decision: a question with real trade-offs, stored for an operator's answer",
		Long: `Create a decision record: the situation and what is at stake, each option with what it
costs, a recommendation and a default.

Give the content as flags, or the whole record as one JSON file with --file (or
--file - on standard input):

  datawatch autonomous decision-records create \
    --title "Build engine" \
    --situation "We must pick a build engine before the launch." \
    --deciding "What the Automaton builds and how it ships." \
    --option go="plain Go, no toolchain" --option zig="Zig, one toolchain" \
    --tradeoff go="familiar, but two toolchains to keep" \
    --tradeoff zig="one toolchain, but less battle-tested" \
    --recommend go --rationale "the team knows it" \
    --default hold
`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			content, err := contentFromFlagsOrFile(f)
			if err != nil {
				return err
			}
			if err := content.validate(); err != nil {
				return err
			}
			body, err := daemonJSONBody(http.MethodPost, "/api/autonomous/decision-records", content.body())
			if err != nil {
				return questionRefusalError(err)
			}
			if err := renderDecisionRecordSaved(cmd.OutOrStdout(), "decision record raised", body); err != nil {
				return err
			}
			renderDecisionRecordWarnings(cmd.ErrOrStderr(), body)
			return nil
		},
	}
	decRecordContentFlagSet(cmd, &f)
	return cmd
}

func newDecisionRecordUpdateCmd() *cobra.Command {
	var f decRecordContentFlags
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Change the content of an open decision record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			content, err := contentFromFlagsOrFile(f)
			if err != nil {
				return err
			}
			if err := content.validate(); err != nil {
				return err
			}
			body, err := daemonJSONBody(http.MethodPut, "/api/autonomous/decision-records/"+url.PathEscape(id), content.body())
			if err != nil {
				return questionRefusalError(err)
			}
			if err := renderDecisionRecordSaved(cmd.OutOrStdout(), "decision record updated", body); err != nil {
				return err
			}
			renderDecisionRecordWarnings(cmd.ErrOrStderr(), body)
			return nil
		},
	}
	decRecordContentFlagSet(cmd, &f)
	return cmd
}

// decRecordContentFlagSet registers the content flags of create and update on cmd.
func decRecordContentFlagSet(cmd *cobra.Command, f *decRecordContentFlags) {
	fs := cmd.Flags()
	fs.StringVar(&f.label, "label", "decision", `"decision" (default) or "finding"`)
	fs.StringVar(&f.title, "title", "", "A short name for the decision")
	fs.StringVar(&f.situation, "situation", "", "The situation: what is true that makes this question necessary")
	fs.StringVar(&f.deciding, "deciding", "", "What this decision decides")
	fs.StringArrayVar(&f.option, "option", nil, "Option: id, then label. Repeat (id=label; short, safe with = and spaces)")
	fs.StringArrayVar(&f.tradeoff, "tradeoff", nil, "What an option costs: id, then the line. Repeat (id=text)")
	fs.StringVar(&f.recommend, "recommend", "", "Option id the author recommends")
	fs.StringVar(&f.rationale, "rationale", "", "Why: sent with --recommend")
	fs.StringVar(&f.def, "default", "", "Option id to answer with when nobody answers, or hold for no auto-answer")
	fs.StringVar(&f.file, "file", "", "Take the whole content from a JSON file (- = standard input) instead of the flags")
	fs.StringVar(&f.doc, "doc", "", "Document path the decision is about (links)")
	fs.StringVar(&f.automaton, "automaton", "", "Automaton id the decision belongs to (links; also the list filter)")
	fs.StringVar(&f.registryRow, "registry-row", "", "Registry row the decision belongs to (links)")
}

// ----- withdraw -------------------------------------------------------------

func newDecisionRecordWithdrawCmd() *cobra.Command {
	reason := ""
	cmd := &cobra.Command{
		Use:   "withdraw <id>",
		Short: "Withdraw a decision record and its queue question",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			body := map[string]any{}
			if reason != "" {
				body["reason"] = reason
			}
			resp, err := daemonJSONBody(http.MethodPost, "/api/autonomous/decision-records/"+url.PathEscape(id)+"/withdraw", body)
			if err != nil {
				return err
			}
			return renderDecisionRecordWithdraw(cmd.OutOrStdout(), reason, resp)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Why the decision is withdrawn (recorded)")
	return cmd
}
