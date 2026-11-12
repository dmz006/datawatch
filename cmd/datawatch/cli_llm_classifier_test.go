package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLLMClassifierCLI(t *testing.T) {
	run := func(args ...string) ([]llmCall, error) {
		seen := llmDaemon(t)
		c := newLLMClassifierCmd()
		c.SetArgs(args)
		err := runRunE(t, c)
		return seen(), err
	}

	calls, err := run("status")
	if err != nil || len(calls) != 1 || calls[0].method != "GET" || calls[0].path != "/api/llm/classifier" {
		t.Fatalf("status = %+v %v", calls, err)
	}
	calls, err = run("publishers")
	if err != nil || len(calls) != 1 || calls[0].path != "/api/llm/classifier/publishers" {
		t.Fatalf("publishers = %+v %v", calls, err)
	}

	calls, err = run("add-publisher", "qwen", "--confirm")
	if err != nil || len(calls) != 1 || calls[0].method != "POST" || calls[0].path != "/api/llm/classifier/publishers" ||
		calls[0].body["publisher"] != "qwen" || calls[0].body["confirm"] != true || calls[0].body["actor"] != "operator" {
		t.Fatalf("add-publisher = %+v %v", calls, err)
	}
	// Without --confirm nothing is sent.
	calls, err = run("add-publisher", "qwen")
	if err == nil || !strings.Contains(err.Error(), "--confirm") || len(calls) != 0 {
		t.Fatalf("unconfirmed add = %+v %v", calls, err)
	}
	if _, err = run("add-publisher"); err == nil {
		t.Fatal("add-publisher without a name must fail")
	}

	calls, err = run("verdicts", "p1")
	if err != nil || len(calls) != 1 || calls[0].path != "/api/llm/classifier/verdicts" {
		t.Fatalf("verdicts = %+v %v", calls, err)
	}
}

func TestLLMClassifierVerdictsQuery(t *testing.T) {
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })

	c := newLLMClassifierCmd()
	c.SetArgs([]string{"verdicts", "p 1", "--task", "t1", "--low-confidence"})
	if err := runRunE(t, c); err != nil {
		t.Fatal(err)
	}
	q, err := url.ParseQuery(rawQuery)
	if err != nil || q.Get("automaton") != "p 1" || q.Get("task") != "t1" || q.Get("low_confidence") != "true" {
		t.Fatalf("query = %q (%v)", rawQuery, err)
	}
	c = newLLMClassifierCmd()
	c.SetArgs([]string{"verdicts", "p1"})
	if err := runRunE(t, c); err != nil {
		t.Fatal(err)
	}
	if rawQuery != "automaton=p1" {
		t.Fatalf("unset filters leaked: %q", rawQuery)
	}
}

func TestLLMCmdRegistersClassifier(t *testing.T) {
	for _, sub := range newLLMCmd().Commands() {
		if sub.Name() == "classifier" {
			return
		}
	}
	t.Fatal("llm classifier is not registered under llm")
}
