package router

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAutomationImportGraphComm(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	replyCode := http.StatusOK
	replyBody := ""
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, nil
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(replyCode)
		_, _ = w.Write([]byte(replyBody))
	})
	r := setupRetryRouter(t, port)

	reply := func(t *testing.T, in string) string {
		t.Helper()
		gotPath = ""
		resp := strings.Join(r.HandleTestMessage(in), "\n")
		if gotMethod != http.MethodPost || gotPath != "/api/automations/import-graph/g1" {
			t.Fatalf("%q proxied %s %s, want POST /api/automations/import-graph/g1", in, gotMethod, gotPath)
		}
		return resp
	}

	t.Run("live import", func(t *testing.T) {
		replyCode = http.StatusOK
		replyBody = `{"id":"ab12","status":"draft","members":[{"kind":"prd"},{"kind":"fork"},{"kind":"prd"},{"kind":"join"}]}`
		got := reply(t, "automation import-graph g1")
		if gotBody["dry_run"] != false {
			t.Errorf("dry_run = %v, want false", gotBody["dry_run"])
		}
		for _, want := range []string{"Automation ab12", "members: 4, forks: 1, joins: 1", "warnings: none"} {
			if !strings.Contains(got, want) {
				t.Errorf("reply lacks %q:\n%s", want, got)
			}
		}
	})

	t.Run("dry run", func(t *testing.T) {
		replyCode = http.StatusOK
		replyBody = `{"input":{"members":[{"kind":"prd"},{"kind":"fork"},{"kind":"join"}]},"warnings":["node n2 has no task"],"dry_run":true}`
		got := reply(t, "automation import-graph g1 dry-run")
		if gotBody["dry_run"] != true {
			t.Errorf("dry_run = %v, want true", gotBody["dry_run"])
		}
		for _, want := range []string{"dry run", "members: 3, forks: 1, joins: 1", "warnings: 1", "node n2 has no task"} {
			if !strings.Contains(got, want) {
				t.Errorf("reply lacks %q:\n%s", want, got)
			}
		}
	})

	t.Run("refusal", func(t *testing.T) {
		replyCode = http.StatusUnprocessableEntity
		replyBody = "node n3 has two parents"
		got := reply(t, "automation import-graph g1")
		if !strings.Contains(got, "cannot be imported") || !strings.Contains(got, "node n3 has two parents") {
			t.Errorf("reply = %q, want the refusal reason", got)
		}
	})

	t.Run("usage", func(t *testing.T) {
		if got := strings.Join(r.HandleTestMessage("automation import-graph"), "\n"); !strings.Contains(got, "usage: automation import-graph") {
			t.Errorf("reply = %q, want the usage line", got)
		}
	})
}
