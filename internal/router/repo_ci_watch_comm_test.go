package router

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// repoCIWatchCommSample is the repo CI watch read body for the comm
// tests: one failing run on the head of main and one hung run on a
// tag, both with a resolved classification and a run URL.
const repoCIWatchCommSample = `{"mode":"on","alert_minutes":30,` +
	`"repos":["/srv/git/datawatch"],"last_poll_at":"2026-11-01T12:00:00Z","records":[{` +
	`"repo":"dmz006/datawatch","ref_kind":"main","ref_name":"main","sha":"abc123",` +
	`"workflow":"ci.yml","run_id":"1001",` +
	`"run_url":"https://github.com/dmz006/datawatch/actions/runs/1001",` +
	`"status":"failing","classification":"code_or_test"},{` +
	`"repo":"dmz006/datawatch","ref_kind":"tag","ref_name":"v1.4.0","sha":"def456",` +
	`"workflow":"release.yml","run_id":"1002",` +
	`"run_url":"https://github.com/dmz006/datawatch/actions/runs/1002",` +
	`"status":"hung","classification":"infra"}]}`

// repoCIWatchCommBody splits the captured reply into its body lines —
// everything after the "[h] autonomous repo-ci-watch" header — so a
// test can check the first line and the record lines independently.
func repoCIWatchCommBody(t *testing.T, r *Router, text string) []string {
	t.Helper()
	out := strings.Join(r.HandleTestMessage(text), "\n")
	header := "[h] autonomous repo-ci-watch\n"
	i := strings.Index(out, header)
	if i == -1 {
		t.Fatalf("reply carries no repo CI watch header: %q", out)
	}
	return strings.Split(out[i+len(header):], "\n")
}

func TestRepoCIWatchComm_VerbDispatch(t *testing.T) {
	if !slices.Contains(RepoCIWatchCommVerbs(), "repo-ci-watch") {
		t.Fatalf("RepoCIWatchCommVerbs() = %v, must contain %q", RepoCIWatchCommVerbs(), "repo-ci-watch")
	}
	for _, verb := range RepoCIWatchCommVerbs() {
		c := Parse("autonomous " + verb)
		if c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
	}

	log, port := laneFillCommDaemon(t, http.StatusOK, repoCIWatchCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous repo-ci-watch"), " ")
	if log.path != "/api/autonomous/repo-ci-watch" || log.query != "" {
		t.Errorf("path=%s query=%q; want the repo CI watch route with no filters", log.path, log.query)
	}
	if log.calls != 1 {
		t.Errorf("the read is one call, got %d", log.calls)
	}
	for _, want := range []string{
		"mode: on",
		"dmz006/datawatch",
		"ci.yml",
		"failing",
		"code_or_test",
		"https://github.com/dmz006/datawatch/actions/runs/1001",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}

	// The underscore alias dispatches to the same read.
	_, port = laneFillCommDaemon(t, http.StatusOK, repoCIWatchCommSample)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous repo_ci_watch"), " ")
	if !strings.Contains(got, "mode: on") {
		t.Errorf("the underscore alias must answer the read, got: %q", got)
	}
}

func TestRepoCIWatchComm_ModeFirstLine(t *testing.T) {
	for _, mode := range []string{"on", "shadow", "off"} {
		body := strings.Replace(repoCIWatchCommSample, `"mode":"on"`, `"mode":"`+mode+`"`, 1)
		_, port := laneFillCommDaemon(t, http.StatusOK, body)
		r := setupPeerRouter(t, port)
		lines := repoCIWatchCommBody(t, r, "autonomous repo-ci-watch")
		if len(lines) < 1 || lines[0] != "mode: "+mode {
			t.Errorf("mode %s: first line = %q, want %q (full: %q)", mode, firstOrEmpty(lines), "mode: "+mode, lines)
		}
	}
}

func TestRepoCIWatchComm_LineFormat(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, repoCIWatchCommSample)
	r := setupPeerRouter(t, port)
	lines := repoCIWatchCommBody(t, r, "autonomous repo-ci-watch")
	if len(lines) != 3 {
		t.Fatalf("want mode line + 2 record lines, got %d: %q", len(lines), lines)
	}
	lines = lines[1:]
	if lines[0] != "dmz006/datawatch | main | ci.yml | failing | code_or_test | https://github.com/dmz006/datawatch/actions/runs/1001" {
		t.Errorf("record 1 line = %q; want repository, ref, workflow, status, classification, run URL", lines[0])
	}
	if lines[1] != "dmz006/datawatch | tag v1.4.0 | release.yml | hung | infra | https://github.com/dmz006/datawatch/actions/runs/1002" {
		t.Errorf("record 2 line = %q; want repository, ref, workflow, status, classification, run URL", lines[1])
	}

	// Every record line carries its six fields, each non-empty.
	for _, line := range lines {
		parts := strings.Split(line, " | ")
		if len(parts) != 6 {
			t.Fatalf("record line must carry six fields, got %d: %q", len(parts), line)
		}
		for i, p := range parts {
			if strings.TrimSpace(p) == "" {
				t.Errorf("record line field %d is empty: %q", i, line)
			}
		}
	}
}

func TestRepoCIWatchComm_EmptyReply(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, `{"mode":"on","alert_minutes":30,"repos":["x"],"last_poll_at":"2026-11-01T12:00:00Z","records":[]}`)
	r := setupPeerRouter(t, port)
	lines := repoCIWatchCommBody(t, r, "autonomous repo-ci-watch")
	if lines[0] != "mode: on" {
		t.Errorf("the first line still states the mode, got %q", lines[0])
	}
	if len(lines) != 2 || lines[1] != "no repo CI watch records" {
		t.Errorf("record-less store = mode line then no-records line, got %q", lines)
	}
}

func TestRepoCIWatchComm_NoPRDWording(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, repoCIWatchCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous repo-ci-watch"), "\n")
	for _, word := range []string{"PRD", "prd"} {
		if strings.Contains(got, word) {
			t.Errorf("the reply must be free of the word %q: %q", word, got)
		}
	}

	// And the record-less reply as well.
	_, port = laneFillCommDaemon(t, http.StatusOK, `{"mode":"off","records":[]}`)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous repo-ci-watch"), "\n")
	for _, word := range []string{"PRD", "prd"} {
		if strings.Contains(got, word) {
			t.Errorf("the empty reply must be free of the word %q: %q", word, got)
		}
	}
}

func TestRepoCIWatchComm_RefusesExtraArgs(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, repoCIWatchCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous repo-ci-watch extra"), " ")
	if !strings.Contains(got, repoCIWatchUsage) {
		t.Errorf("extra args must be refused with the usage, got: %q", got)
	}
	if log.calls != 0 {
		t.Errorf("the refusal must not reach the daemon, got %d calls", log.calls)
	}
}

func TestRepoCIWatchComm_DaemonRefusalPassesThrough(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous repo-ci-watch"), " ")
	if !strings.Contains(got, "HTTP 503") || !strings.Contains(got, "autonomous disabled") {
		t.Errorf("the daemon refusal must pass through, got: %q", got)
	}
}

func TestRepoCIWatchComm_HelpListsVerb(t *testing.T) {
	r := setupPeerRouter(t, 0)
	got := strings.Join(r.HandleTestMessage("autonomous"), " ")
	if !strings.Contains(got, "repo-ci-watch") {
		t.Errorf("the autonomous help must list the repo-ci-watch verb; got:\n%s", got)
	}
}

// firstOrEmpty returns the first line of lines, or "" when there is
// none — for the failure message in the first-line checks.
func firstOrEmpty(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}
