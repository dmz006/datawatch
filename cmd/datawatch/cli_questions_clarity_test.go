// CLI tests for the question clarity additions: the repeatable --tradeoff
// flag on `question raise`, the quality warnings and refusal, the
// `question quality` read command, and the plain answer help. They use the
// fake daemon of cli_questions_test.go.

package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runQuestionCmdSplit is runQuestionCmd with standard output and standard
// error kept apart, for the tests that check which stream a line is on.
func runQuestionCmdSplit(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "datawatch"}
	root.AddCommand(newQuestionCmd())
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"question"}, args...))
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// raiseArgs are the arguments of a valid two-option raise, to which a test
// appends its own flags.
func raiseArgs(extra ...string) []string {
	base := []string{"raise",
		"--text", "Which recovery path should the Automaton take?",
		"--option", "o1=Retry now",
		"--option", "o2=Escalate to an operator",
		"--default", "hold",
	}
	return append(base, extra...)
}

const raisedQuestionJSON = `{"id":"q_new","label":"decision","state":"open","default":"hold","options":[{"id":"o1","label":"Retry now"},{"id":"o2","label":"Escalate to an operator"}]}`

func TestQuestionRaiseTradeoff(t *testing.T) {
	t.Run("one flag is sent in the tradeoffs object", func(t *testing.T) {
		d := &questionRouteDaemon{routes: map[string]questionRouteReply{
			"POST /api/questions": {code: http.StatusCreated, body: `{"question":` + raisedQuestionJSON + `}`},
		}}
		withQuestionRouteServer(t, d)

		out, err := runQuestionCmd(t, raiseArgs("--tradeoff", "o1=fast, but it may double the work")...)
		if err != nil {
			t.Fatalf("err: %v (output: %s)", err, out)
		}
		tradeoffs, _ := d.bodyJSON(t, 0)["tradeoffs"].(map[string]any)
		if len(tradeoffs) != 1 || tradeoffs["o1"] != "fast, but it may double the work" {
			t.Errorf("tradeoffs = %v, want only o1 with its text", tradeoffs)
		}
	})

	t.Run("the value is split on the first = only", func(t *testing.T) {
		d := &questionRouteDaemon{routes: map[string]questionRouteReply{
			"POST /api/questions": {code: http.StatusCreated, body: `{"question":` + raisedQuestionJSON + `}`},
		}}
		withQuestionRouteServer(t, d)

		if out, err := runQuestionCmd(t, raiseArgs("--tradeoff", "o2=safe, but a=b waits for a person")...); err != nil {
			t.Fatalf("err: %v (output: %s)", err, out)
		}
		tradeoffs, _ := d.bodyJSON(t, 0)["tradeoffs"].(map[string]any)
		if tradeoffs["o2"] != "safe, but a=b waits for a person" {
			t.Errorf("o2 trade-off = %q, want everything after the first =", tradeoffs["o2"])
		}
	})

	t.Run("the repeated flag collects one entry per id", func(t *testing.T) {
		d := &questionRouteDaemon{routes: map[string]questionRouteReply{
			"POST /api/questions": {code: http.StatusCreated, body: `{"question":` + raisedQuestionJSON + `}`},
		}}
		withQuestionRouteServer(t, d)

		out, err := runQuestionCmd(t, raiseArgs(
			"--tradeoff", "o1=fast, but it may double the work",
			"--tradeoff", "o2=safe, but it waits for a person",
		)...)
		if err != nil {
			t.Fatalf("err: %v (output: %s)", err, out)
		}
		tradeoffs, _ := d.bodyJSON(t, 0)["tradeoffs"].(map[string]any)
		if len(tradeoffs) != 2 ||
			tradeoffs["o1"] != "fast, but it may double the work" ||
			tradeoffs["o2"] != "safe, but it waits for a person" {
			t.Errorf("tradeoffs = %v, want both o1 and o2", tradeoffs)
		}
	})

	t.Run("no flag sends no tradeoffs object", func(t *testing.T) {
		d := &questionRouteDaemon{routes: map[string]questionRouteReply{
			"POST /api/questions": {code: http.StatusCreated, body: `{"question":` + raisedQuestionJSON + `}`},
		}}
		withQuestionRouteServer(t, d)

		if out, err := runQuestionCmd(t, raiseArgs()...); err != nil {
			t.Fatalf("err: %v (output: %s)", err, out)
		}
		if _, present := d.bodyJSON(t, 0)["tradeoffs"]; present {
			t.Error("a raise without --tradeoff must not send a tradeoffs object")
		}
	})

	for name, tc := range map[string]struct{ value, want string }{
		"no equals sign": {"o1 is fast", "id=text"},
		"empty id":       {"=fast, but risky", "option id"},
		"blank id":       {"  =fast, but risky", "option id"},
	} {
		t.Run("rejects a value with "+name, func(t *testing.T) {
			d := &questionRouteDaemon{}
			withQuestionRouteServer(t, d)

			_, err := runQuestionCmd(t, raiseArgs("--tradeoff", tc.value)...)
			if err == nil {
				t.Fatalf("--tradeoff %q must be refused", tc.value)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), tc.value) {
				t.Errorf("error %q should name the bad value %q and say %q", err, tc.value, tc.want)
			}
			if len(d.requests) != 0 {
				t.Error("a bad --tradeoff reached the daemon")
			}
		})
	}

	t.Run("rejects the same id twice", func(t *testing.T) {
		d := &questionRouteDaemon{}
		withQuestionRouteServer(t, d)

		_, err := runQuestionCmd(t, raiseArgs("--tradeoff", "o1=fast", "--tradeoff", "o1=cheap")...)
		if err == nil || !strings.Contains(err.Error(), `"o1"`) {
			t.Fatalf("want an error naming o1, got %v", err)
		}
		if len(d.requests) != 0 {
			t.Error("a duplicate --tradeoff reached the daemon")
		}
	})
}

func TestQuestionQuality_CLI(t *testing.T) {
	t.Run("warnings on a 201 are printed to standard error", func(t *testing.T) {
		resp := `{"question":` + raisedQuestionJSON + `,"warnings":[` +
			`{"code":"no_tradeoffs","message":"Option o1 has no trade-off; say what it costs."},` +
			`{"code":"text_too_short","message":"The question text is shorter than 60 characters."}]}`
		d := &questionRouteDaemon{routes: map[string]questionRouteReply{
			"POST /api/questions": {code: http.StatusCreated, body: resp},
		}}
		withQuestionRouteServer(t, d)

		stdout, stderr, err := runQuestionCmdSplit(t, raiseArgs()...)
		if err != nil {
			t.Fatalf("a warned raise still succeeds, got %v", err)
		}
		wantContains(t, stderr,
			"warning: no_tradeoffs: Option o1 has no trade-off; say what it costs.",
			"warning: text_too_short: The question text is shorter than 60 characters.")
		if got := strings.Count(strings.TrimSpace(stderr), "\n") + 1; got != 2 {
			t.Errorf("want one line per warning (2), got %d:\n%s", got, stderr)
		}
		if strings.Contains(stdout, "warning") {
			t.Errorf("warnings belong on standard error, not standard output:\n%s", stdout)
		}
		wantContains(t, stdout, "question raised: q_new")
	})

	t.Run("a 201 without warnings prints none", func(t *testing.T) {
		d := &questionRouteDaemon{routes: map[string]questionRouteReply{
			"POST /api/questions": {code: http.StatusCreated, body: `{"question":` + raisedQuestionJSON + `}`},
		}}
		withQuestionRouteServer(t, d)

		_, stderr, err := runQuestionCmdSplit(t, raiseArgs()...)
		if err != nil || stderr != "" {
			t.Errorf("err = %v, stderr = %q, want a clean raise", err, stderr)
		}
	})

	t.Run("a refusal prints the findings and exits non-zero", func(t *testing.T) {
		refusal := `{"error":"question refused: it does not meet the question quality policy; fix the findings and send it again",` +
			`"findings":[{"code":"tracker_id","message":"The text names an internal planning id."},` +
			`{"code":"no_tradeoffs","message":"Option o2 has no trade-off."}]}`
		d := &questionRouteDaemon{routes: map[string]questionRouteReply{
			"POST /api/questions": {code: http.StatusUnprocessableEntity, body: refusal},
		}}
		withQuestionRouteServer(t, d)

		stdout, _, err := runQuestionCmdSplit(t, raiseArgs()...)
		if err == nil {
			t.Fatal("a 422 refusal must make the command fail (non-zero exit)")
		}
		wantContains(t, err.Error(),
			"question refused: it does not meet the question quality policy; fix the findings and send it again",
			"tracker_id: The text names an internal planning id.",
			"no_tradeoffs: Option o2 has no trade-off.")
		if strings.Contains(stdout, "question raised") {
			t.Errorf("a refused question must not print a raise confirmation:\n%s", stdout)
		}
	})

	t.Run("a 422 that is not a findings list keeps the daemon's text", func(t *testing.T) {
		d := &questionRouteDaemon{routes: map[string]questionRouteReply{
			"POST /api/questions": {code: http.StatusUnprocessableEntity, body: "plain refusal text"},
		}}
		withQuestionRouteServer(t, d)

		_, err := runQuestionCmd(t, raiseArgs()...)
		if err == nil || !strings.Contains(err.Error(), "plain refusal text") {
			t.Fatalf("want the daemon's text in the error, got %v", err)
		}
	})

	t.Run("the read command prints the effective policy", func(t *testing.T) {
		d := &questionRouteDaemon{routes: map[string]questionRouteReply{
			"GET /api/questions/quality": {body: `{"mode":"refuse","min_text_chars":80,"tracker_id_pattern":"\\b[A-Z]{1,3}[0-9]{2,4}\\b","queue_preview_lines":5}`},
		}}
		withQuestionRouteServer(t, d)

		out, err := runQuestionCmd(t, "quality")
		if err != nil {
			t.Fatalf("err: %v (output: %s)", err, out)
		}
		if d.requests[0].method != "GET" || d.requests[0].path != "/api/questions/quality" {
			t.Fatalf("request = %+v, want GET /api/questions/quality", d.requests[0])
		}
		wantContains(t, out,
			"mode: refuse",
			"minimum text length: 80 characters",
			`\b[A-Z]{1,3}[0-9]{2,4}\b`,
			"queue preview: 5 lines")
	})

	t.Run("the read command with --json prints the raw body", func(t *testing.T) {
		d := &questionRouteDaemon{routes: map[string]questionRouteReply{
			"GET /api/questions/quality": {body: `{"mode":"warn","min_text_chars":60,"tracker_id_pattern":"x","queue_preview_lines":3}`},
		}}
		withQuestionRouteServer(t, d)

		// prettyPrint writes to the process standard output, so this only
		// checks that the flag is accepted and the call succeeds.
		if _, err := runQuestionCmd(t, "quality", "--json"); err != nil {
			t.Fatalf("err: %v", err)
		}
	})

	t.Run("the read command surfaces the disabled switch", func(t *testing.T) {
		msg := "operator questions are disabled: set autonomous.questions.enabled to true"
		d := &questionRouteDaemon{routes: map[string]questionRouteReply{
			"GET /api/questions/quality": {code: http.StatusServiceUnavailable, body: msg},
		}}
		withQuestionRouteServer(t, d)

		_, err := runQuestionCmd(t, "quality")
		if err == nil || !strings.Contains(err.Error(), msg) {
			t.Fatalf("want the daemon's message %q, got %v", msg, err)
		}
	})
}

func TestQuestionAnswerHelp(t *testing.T) {
	answer := newQuestionAnswerCmd()
	// Collapse line wraps so the check is on the words, not the layout.
	help := strings.Join(strings.Fields(answer.Long+" "+answer.Short+" "+answer.Flags().Lookup("text").Usage), " ")

	wantContains(t, help,
		"saved exactly as written",
		"optional when you pick an option",
		"with your choice, as the record of your decision",
		"the asker reads it as your answer",
	)
	if strings.Contains(help, "verbatim") {
		t.Errorf("the help should say it plainly, without the word verbatim:\n%s", help)
	}

	out, err := runQuestionCmd(t, "answer", "--help")
	if err != nil {
		t.Fatalf("answer --help: %v", err)
	}
	wantContains(t, out, "saved exactly as written", "optional when you pick an option")
}

func TestQuestionGroupHasTheQualityCommand(t *testing.T) {
	have := map[string]bool{}
	for _, sub := range newQuestionCmd().Commands() {
		have[sub.Name()] = true
	}
	if !have["quality"] {
		t.Error("the question group lacks the quality subcommand")
	}
}
