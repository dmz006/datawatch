package router

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAutonomousArchiveCommIsDryRunOnly(t *testing.T) {
	var method, path, body string
	handler := func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		method, path, body = req.Method, req.URL.Path, string(b)
		_, _ = w.Write([]byte(`{"dry_run":true,"retention_days":30,"bytes_before":1000,"bytes_after":100,"archive_bytes":950,"already_archived":2,"eligible":[{"id":"aa11"},{"id":"bb22"}],"skipped":[{"id":"cc33","reason":"task t1 is in_progress"}]}`))
	}
	_, port := newFakePeerParent(t, handler)
	r := setupRetryRouter(t, port)

	out := joinReplies(r.HandleTestMessage("autonomous archive"))
	if method != http.MethodPost || path != "/api/autonomous/archive" || !strings.Contains(body, `"dry_run":true`) {
		t.Fatalf("proxied %s %s %s", method, path, body)
	}
	for _, want := range []string{"Dry run (nothing written)", "2 finished Automata", "1000 bytes now, 100 bytes after", "skipped cc33"} {
		if !strings.Contains(out, want) {
			t.Errorf("reply lacks %q:\n%s", want, out)
		}
	}
	// Anything after the verb is refused with the usage line; chat never applies.
	method = ""
	out = joinReplies(r.HandleTestMessage("autonomous archive apply=true"))
	if method != "" || !strings.Contains(out, "usage: autonomous archive") {
		t.Fatalf("extra argument: method=%q reply=%q", method, out)
	}
}
