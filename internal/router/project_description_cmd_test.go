// Comm-channel (chat) surface test: `project show` prints the description and
// `project set <name> description <value>` posts the updated description with
// the rest of the stored record.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectDescription_ShowPrintsDescription(t *testing.T) {
	out := formatProjectShow(projectRecordBody)
	if !strings.Contains(out, "Acme platform") {
		t.Errorf("show output missing the stored description:\n%s", out)
	}
	for _, want := range []string{
		"Project acme",
		"dir:",
		"/work/acme",
		"owner:",
		"platform",
		"plans:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// A record with no description must not crash the formatter.
func TestProjectDescription_ShowWithoutDescription(t *testing.T) {
	out := formatProjectShow(`{"name":"acme","dir":"/work/acme","default_backend":"claude-code"}`)
	if !strings.Contains(out, "Project acme") {
		t.Errorf("show output missing the project:\n%s", out)
	}
}

func TestProjectDescription_SetRoundTrip(t *testing.T) {
	var posted map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/projects/acme", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(projectRecordBody))
	})
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &posted); err != nil {
			t.Errorf("POST body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	r := loopbackRouter(t, srv)
	out := captureSends(r, func() {
		r.handleProjectCmd(Command{Type: CmdProject, Text: "set acme description the billing service of the Automata"})
	})
	if strings.Contains(out, "error") || strings.Contains(out, "failed") {
		t.Errorf("set failed:\n%s", out)
	}
	if posted == nil {
		t.Fatal("no POST /api/projects was sent")
	}
	if got := posted["description"]; got != "the billing service of the Automata" {
		t.Errorf("posted description = %v, want the billing service of the Automata", got)
	}
	if got := posted["dir"]; got != "/work/acme" {
		t.Errorf("posted dir = %v, want /work/acme", got)
	}
	if got := posted["default_backend"]; got != "claude-code" {
		t.Errorf("posted default_backend = %v, want claude-code", got)
	}
	if owner, _ := posted["owner"].(string); owner != "platform" {
		t.Errorf("posted owner = %v, want platform (stored value preserved)", posted["owner"])
	}
}
