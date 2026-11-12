// Question quality and answer-help comm-verb tests: the `question
// quality` subverb against a fake /api/questions/quality daemon, the
// answer verb's help wording, and the trade-off line under an option.

package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeQualityDaemon serves GET /api/questions/quality with a fixed policy
// (or the given error status) and returns the port and a close function.
func fakeQualityDaemon(t *testing.T, status int, paths *[]string) (int, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.Method+" "+r.URL.Path)
		if r.URL.Path != "/api/questions/quality" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		if status != 0 {
			http.Error(w, "operator questions are disabled", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"mode":"warn","min_text_chars":40,"tracker_id_pattern":"\\b[A-Z]{1,4}[0-9]{2,5}\\b","queue_preview_lines":3}`))
	}))
	idx := strings.LastIndex(srv.URL, ":")
	port := 0
	for i := idx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}
	return port, srv.Close
}

func TestQuestionQuality_Comm(t *testing.T) {
	v, err := parseQuestionVerb("quality")
	if err != nil || v.action != "quality" {
		t.Fatalf("quality: %+v %v", v, err)
	}
	if _, err := parseQuestionVerb("quality extra"); err == nil {
		t.Error("quality with an argument: want an error")
	}
	if got := Parse("question quality"); got.Type != CmdQuestion || got.Text != "quality" {
		t.Errorf("Parse(question quality) = %+v", got)
	}
	if !strings.Contains(questionUsage, "question quality") {
		t.Errorf("questionUsage = %q, want it to list question quality", questionUsage)
	}

	var paths []string
	port, closeFn := fakeQualityDaemon(t, 0, &paths)
	defer closeFn()
	r := questionRouter(t, port)
	out := joinReplies(r.HandleTestMessage("question quality"))
	for _, want := range []string{"mode: warn", "minimum text length: 40",
		`tracker-id pattern: \b[A-Z]{1,4}[0-9]{2,5}\b`, "queue preview lines: 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("reply %q is missing %q", out, want)
		}
	}
	if len(paths) != 1 || paths[0] != "GET /api/questions/quality" {
		t.Errorf("daemon requests = %v, want one GET /api/questions/quality", paths)
	}

	// A daemon refusal is reported, not swallowed.
	var paths2 []string
	port2, closeFn2 := fakeQualityDaemon(t, http.StatusServiceUnavailable, &paths2)
	defer closeFn2()
	out = joinReplies(questionRouter(t, port2).HandleTestMessage("question quality"))
	if !strings.Contains(out, "question quality failed") {
		t.Errorf("refusal reply = %q, want a failure message", out)
	}
}

func TestQuestionAnswerHelp_Comm(t *testing.T) {
	wants := []string{
		"optional when you pick an option",
		"saved exactly as written, with your choice, as the record of your decision",
		"the asker reads it as your answer",
	}
	// The malformed-answer reply, the chat help and the constant all carry it.
	r := &Router{
		hostname: "h",
		groupID:  "grp1",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	for name, text := range map[string]string{
		"constant":      questionAnswerHelp,
		"usage reply":   joinReplies(r.HandleTestMessage("question answer q_1")),
		"chat help":     HelpText("h"),
		"help via verb": joinReplies(r.HandleTestMessage("help")),
	} {
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s is missing %q", name, want)
			}
		}
	}
	if !strings.Contains(HelpText("h"), "question quality") {
		t.Error("chat help does not list question quality")
	}
}

func TestQuestionShowTradeoffs_Comm(t *testing.T) {
	opt := questionOption("opt_a", "Roll back")
	opt["tradeoffs"] = "safe but slow"
	f := &questionFixture{byID: map[string]map[string]any{
		"q_1": questionQuestion("q_1", "Rollback", "Roll back?", opt, questionOption("opt_b", "Keep going")),
	}}
	port, closeFn := fakeQuestionDaemon(t, f)
	defer closeFn()
	out := joinReplies(questionRouter(t, port).HandleTestMessage("question show q_1"))
	if !strings.Contains(out, "1. opt_a  Roll back\n   trade-offs: safe but slow") {
		t.Errorf("show = %q, want the trade-off line under the option", out)
	}
	if strings.Count(out, "trade-offs:") != 1 {
		t.Errorf("show = %q, want a trade-off line only for the option that has one", out)
	}
}
