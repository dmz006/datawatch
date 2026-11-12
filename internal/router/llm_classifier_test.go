package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type classifierReq struct {
	method, path, query string
	body                map[string]any
}

// newClassifierRouter returns a router whose loopback REST stand-in records
// every request and answers the verdicts route with verdictsBody.
func newClassifierRouter(t *testing.T, verdictsBody string) (*Router, *[]classifierReq) {
	t.Helper()
	var reqs []classifierReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		reqs = append(reqs, classifierReq{r.Method, r.URL.Path, r.URL.RawQuery, body})
		if r.URL.Path == "/api/llm/classifier/verdicts" {
			_, _ = w.Write([]byte(verdictsBody))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	port, err := strconv.Atoi(srv.URL[strings.LastIndexByte(srv.URL, ':')+1:])
	if err != nil {
		t.Fatal(err)
	}
	r := NewRouter("h", "g", nil, nil, 10, nil)
	r.SetWebPort(port)
	return r, &reqs
}

func TestLLMClassifierCommReads(t *testing.T) {
	r, reqs := newClassifierRouter(t, `{"verdicts":[]}`)
	for _, c := range []struct{ msg, path string }{
		{"llm classifier status", "/api/llm/classifier"},
		{"llm classifier publishers", "/api/llm/classifier/publishers"},
	} {
		*reqs = nil
		out := strings.Join(r.HandleTestMessage(c.msg), "\n")
		if len(*reqs) != 1 || (*reqs)[0].method != "GET" || (*reqs)[0].path != c.path || strings.Contains(out, "failed") {
			t.Fatalf("%q → %+v reply=%q", c.msg, *reqs, out)
		}
	}
	*reqs = nil
	r.HandleTestMessage("llm classifier verdicts p1 task=t2 low")
	if len(*reqs) != 1 || (*reqs)[0].path != "/api/llm/classifier/verdicts" {
		t.Fatalf("verdicts → %+v", *reqs)
	}
	for _, want := range []string{"automaton=p1", "task=t2", "low_confidence=true"} {
		if !strings.Contains((*reqs)[0].query, want) {
			t.Errorf("query %q lacks %q", (*reqs)[0].query, want)
		}
	}
	*reqs = nil
	r.HandleTestMessage("llm classifier verdicts p1")
	if (*reqs)[0].query != "automaton=p1" {
		t.Errorf("unset filters leaked: %q", (*reqs)[0].query)
	}
}

func TestLLMClassifierCommAddPublisher(t *testing.T) {
	r, reqs := newClassifierRouter(t, "")
	r.HandleTestMessage("llm classifier add-publisher qwen confirm=true")
	if len(*reqs) != 1 || (*reqs)[0].method != "POST" || (*reqs)[0].path != "/api/llm/classifier/publishers" ||
		(*reqs)[0].body["publisher"] != "qwen" || (*reqs)[0].body["confirm"] != true {
		t.Fatalf("confirmed add → %+v", *reqs)
	}
	// Without the word the request still goes out with confirm=false: the
	// daemon owns the refusal rule.
	*reqs = nil
	r.HandleTestMessage("llm classifier add-publisher qwen")
	if len(*reqs) != 1 || (*reqs)[0].body["confirm"] != false {
		t.Fatalf("unconfirmed add → %+v", *reqs)
	}
	for _, msg := range []string{"llm classifier add-publisher", "llm classifier add-publisher confirm=true"} {
		*reqs = nil
		out := strings.Join(r.HandleTestMessage(msg), "\n")
		if len(*reqs) != 0 || !strings.Contains(out, "usage: llm classifier add-publisher") {
			t.Fatalf("%q → %+v reply=%q", msg, *reqs, out)
		}
	}
}

func TestLLMClassifierCommReview(t *testing.T) {
	r, reqs := newClassifierRouter(t, `{"verdicts":[{"scope":"task","summary":"low"}]}`)
	out := strings.Join(r.HandleTestMessage("llm classifier review p1 t1 retry note=looks fine after reading"), "\n")
	if strings.Contains(out, "failed") || len(*reqs) != 2 {
		t.Fatalf("review reply=%q reqs=%+v", out, *reqs)
	}
	if !strings.Contains((*reqs)[0].query, "low_confidence=true") || !strings.Contains((*reqs)[0].query, "task=t1") {
		t.Errorf("review must check for a low-confidence block first: %+v", (*reqs)[0])
	}
	post := (*reqs)[1]
	if post.method != "POST" || post.path != "/api/autonomous/prds/p1/resolve_task_review" ||
		post.body["task_id"] != "t1" || post.body["action"] != "retry" || post.body["note"] != "looks fine after reading" {
		t.Fatalf("decision → %+v", post)
	}
}

func TestLLMClassifierCommReviewRefusals(t *testing.T) {
	r, reqs := newClassifierRouter(t, `{"verdicts":[]}`)
	out := strings.Join(r.HandleTestMessage("llm classifier review p1 t1 cancel"), "\n")
	if !strings.Contains(out, "no low-confidence block") || len(*reqs) != 1 {
		t.Fatalf("no block: reply=%q reqs=%+v (must not post a decision)", out, *reqs)
	}
	*reqs = nil
	out = strings.Join(r.HandleTestMessage("llm classifier review p1 t1 approve"), "\n")
	if !strings.Contains(out, "retry or cancel") || len(*reqs) != 0 {
		t.Fatalf("bad action: %q %+v", out, *reqs)
	}
	out = strings.Join(r.HandleTestMessage("llm classifier review p1"), "\n")
	if !strings.Contains(out, "usage: llm classifier review") || len(*reqs) != 0 {
		t.Fatalf("short: %q", out)
	}
	out = strings.Join(r.HandleTestMessage("llm classifier"), "\n")
	if !strings.Contains(out, "llm classifier status") {
		t.Fatalf("bare verb = %q", out)
	}
	out = strings.Join(r.HandleTestMessage("llm classifier bogus"), "\n")
	if !strings.Contains(out, "llm classifier publishers") {
		t.Fatalf("unknown verb = %q", out)
	}
	out = strings.Join(r.HandleTestMessage("llm classifier verdicts"), "\n")
	if !strings.Contains(out, "usage: llm classifier verdicts") {
		t.Fatalf("verdicts without automaton = %q", out)
	}
}

func TestLLMClassifierCommRESTErrors(t *testing.T) {
	// No web port: every verb reports the failure instead of panicking.
	r := NewRouter("h", "g", nil, nil, 10, nil)
	for _, msg := range []string{
		"llm classifier status", "llm classifier add-publisher q confirm=true",
		"llm classifier verdicts p1", "llm classifier review p1 t1 retry",
	} {
		if out := strings.Join(r.HandleTestMessage(msg), "\n"); !strings.Contains(out, "failed") {
			t.Errorf("%q reply = %q, want a failure", msg, out)
		}
	}
	// A verdict list that is not JSON stops the review.
	r2, reqs := newClassifierRouter(t, `not json`)
	out := strings.Join(r2.HandleTestMessage("llm classifier review p1 t1 retry"), "\n")
	if !strings.Contains(out, "could not be read") || len(*reqs) != 1 {
		t.Fatalf("garbled list: %q %+v", out, *reqs)
	}
}

func TestNoteFromWords(t *testing.T) {
	if got := noteFromWords([]string{"x", "note=a", "b", "c"}); got != "a b c" {
		t.Errorf("got %q", got)
	}
	if got := noteFromWords([]string{"x"}); got != "" {
		t.Errorf("got %q", got)
	}
}
