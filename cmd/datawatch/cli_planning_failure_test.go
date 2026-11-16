package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlanningFailureCLIGetAndListShowReasonAndPhrases(t *testing.T) {
	obj := `"planning_failure":{"reason":"spec holds a fixed version number","category":"content","source":"release-version-fixed","phrases":["v9.1.0"],"retryable":false,"retry_counts":{"content":1},"retry_budget":3}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/autonomous/prds" {
			_, _ = w.Write([]byte(`{"prds":[{"id":"p1","title":"Alpha",` + obj + `},{"id":"p2","title":"Beta"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"p1",` + obj + `}`))
	}))
	defer srv.Close()
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })

	get := newAutonomousPRDGetCmd()
	var out, notes bytes.Buffer
	get.SetOut(&out)
	get.SetErr(&notes)
	if err := get.RunE(get, []string{"p1"}); err != nil {
		t.Fatalf("prd-get: %v", err)
	}
	// The planning-failure block is a note: standard error, never the JSON stream.
	for _, want := range []string{"Planning failed", "reason:   spec holds a fixed version number", "category: content", "rule:     release-version-fixed", "phrases:  v9.1.0", "1 of 3"} {
		if !strings.Contains(notes.String(), want) {
			t.Errorf("get notes missing %q:\n%s", want, notes.String())
		}
		if strings.Contains(out.String(), want) {
			t.Errorf("get stdout must stay pure JSON, found %q:\n%s", want, out.String())
		}
	}
}
