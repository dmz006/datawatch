package router

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// infraRetryCommTask builds one task block of the status body, with retries
// numbered 1..retries.
func infraRetryCommTask(automaton, task string, retries int, at time.Time) string {
	var rs []string
	for i := 1; i <= retries; i++ {
		rs = append(rs, fmt.Sprintf(`{"number":%d,"class":"spawn_error","reason":"tmux spawn failed %s","at":%q,"delay_seconds":60,"next_at":%q}`,
			i, strings.Repeat("x", 120), at.Format(time.RFC3339), at.Format(time.RFC3339)))
	}
	return fmt.Sprintf(`{"automaton_id":%q,"automaton_title":"Title %s","task_id":%q,"task_title":"Task %s","status":"pending","retry_count":%d,"max_tries":3,"retries":[%s],"held":false}`,
		automaton, automaton, task, task, retries, strings.Join(rs, ","))
}

func infraRetryCommBody(tasks ...string) string {
	return `{"enabled":true,"max_tries":3,"backoff_seconds":[60,300,540],"categories":["spawn_error"],"exhausted_action":"hold","retry_dependents":true,"raise_question":true,` +
		`"tasks":[` + strings.Join(tasks, ",") + `],"count":` + fmt.Sprint(len(tasks)) + `}`
}

func TestInfraRetryComm_ReplyTextHasSettingsAndBlocks(t *testing.T) {
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	log, port := laneFillCommDaemon(t, http.StatusOK, infraRetryCommBody(infraRetryCommTask("a1", "t1", 2, at)))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous infra-retries"), " ")
	if log.path != "/api/autonomous/infra-retries" || log.query != "limit=10" {
		t.Errorf("path=%s query=%q, want the infra-retries route with limit=10", log.path, log.query)
	}
	for _, want := range []string{
		"Infrastructure retry: on, 3 tries, back-off 60s/300s/540s, hold when used up",
		`Automaton a1 "Title a1", task t1 "Task t1": pending, retry 2 of 3`,
		"retry 1: spawn_error",
		"retry 2: spawn_error",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestInfraRetryComm_AutomatonAndLimitAreSent(t *testing.T) {
	cases := map[string]string{
		"autonomous infra-retries a1":       "automaton=a1&limit=10",
		"autonomous infra-retries a1 4":     "automaton=a1&limit=4",
		"autonomous infra-retries 25":       "limit=25",
		"autonomous infra_retries":          "limit=10",
		"autonomous infra-retries ced90001": "automaton=ced90001&limit=10",
	}
	for msg, wantQuery := range cases {
		log, port := laneFillCommDaemon(t, http.StatusOK, infraRetryCommBody())
		r := setupPeerRouter(t, port)
		r.HandleTestMessage(msg)
		if log.query != wantQuery {
			t.Errorf("%q: query=%q, want %q", msg, log.query, wantQuery)
		}
	}
}

func TestInfraRetryComm_BadLimitShowsUsageAndSendsNothing(t *testing.T) {
	for _, msg := range []string{"autonomous infra-retries a1 abc", "autonomous infra-retries a1 0", "autonomous infra-retries a1 1001", "autonomous infra-retries a1 3 extra"} {
		log, port := laneFillCommDaemon(t, http.StatusOK, infraRetryCommBody())
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, "usage: autonomous infra-retries [automaton-id] [limit]") {
			t.Errorf("%q: expected the usage, got %q", msg, got)
		}
		if log.calls != 0 {
			t.Errorf("%q: a bad limit must not reach the daemon, got %d calls", msg, log.calls)
		}
	}
}

func TestInfraRetryComm_EmptyState(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, infraRetryCommBody())
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous infra-retries"), " ")
	if !strings.Contains(got, "Infrastructure retry: on") || !strings.Contains(got, "No infrastructure retries yet.") {
		t.Errorf("expected the settings line and the empty-state line, got %q", got)
	}
	if strings.Contains(got, "not shown") {
		t.Errorf("an empty state must not report left-out tasks: %q", got)
	}
}

func TestInfraRetryComm_LongListIsTrimmedKeepingNewest(t *testing.T) {
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	var tasks []string
	for i := 1; i <= 8; i++ {
		tasks = append(tasks, infraRetryCommTask("a1", fmt.Sprintf("task%d", i), 3, at))
	}
	_, port := laneFillCommDaemon(t, http.StatusOK, infraRetryCommBody(tasks...))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous infra-retries"), " ")
	if len(got) > infraRetryChatMaxChars+500 {
		t.Errorf("reply is %d characters, over the chat budget", len(got))
	}
	if !strings.Contains(got, "task1") {
		t.Errorf("the newest task (first in the list) must be kept: %q", got)
	}
	if strings.Contains(got, "task8") {
		t.Errorf("the oldest task must be dropped first: %q", got)
	}
	if !strings.Contains(got, "older tasks not shown") {
		t.Errorf("the reply must say how many tasks were left out: %q", got)
	}
}

func TestInfraRetryComm_TrimCountsLeftOutTasks(t *testing.T) {
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	var tasks []string
	for i := 1; i <= 8; i++ {
		tasks = append(tasks, infraRetryCommTask("a1", fmt.Sprintf("tk%02d", i), 3, at))
	}
	st := mustParseInfraRetryStatus(t, infraRetryCommBody(tasks...))
	text := renderInfraRetriesForChat(st)
	shown := strings.Count(text, "Automaton a1 ")
	if shown == 0 || shown == 8 {
		t.Fatalf("expected a partial list, %d of 8 shown: %q", shown, text)
	}
	for i := 1; i <= shown; i++ {
		if !strings.Contains(text, fmt.Sprintf("task tk%02d ", i)) {
			t.Errorf("the newest tasks must be the ones kept; tk%02d is missing: %q", i, text)
		}
	}
	want := fmt.Sprintf("(%d older tasks not shown", 8-shown)
	if !strings.Contains(text, want) {
		t.Errorf("reply lacks %q: %q", want, text)
	}
	if len(text) > infraRetryChatMaxChars {
		t.Errorf("reply is %d characters, over the %d budget", len(text), infraRetryChatMaxChars)
	}
}

func TestInfraRetryComm_OneOversizeBlockIsTrimmedToFit(t *testing.T) {
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	st := mustParseInfraRetryStatus(t, infraRetryCommBody(infraRetryCommTask("a1", "t1", 40, at)))
	text := renderInfraRetriesForChat(st)
	if len(text) > infraRetryChatMaxChars {
		t.Fatalf("reply is %d characters, over the %d budget", len(text), infraRetryChatMaxChars)
	}
	for _, want := range []string{
		"Infrastructure retry: on",
		`Automaton a1 "Title a1", task t1 "Task t1": pending, retry 40 of 3`,
		"retry 40: spawn_error",
		"earlier retries of the newest task not shown)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("reply lacks %q: %q", want, text)
		}
	}
	if strings.Contains(text, "retry 1: spawn_error") {
		t.Errorf("the oldest retry must be the one dropped: %q", text)
	}
}

func TestInfraRetryComm_OneOversizeRetryLineIsCut(t *testing.T) {
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	body := fmt.Sprintf(`{"enabled":true,"max_tries":3,"backoff_seconds":[60],"categories":["spawn_error"],"exhausted_action":"hold","retry_dependents":true,"raise_question":true,`+
		`"tasks":[{"automaton_id":"a1","automaton_title":"A","task_id":"t1","task_title":"T","status":"pending","retry_count":1,"max_tries":3,`+
		`"retries":[{"number":1,"class":"spawn_error","reason":%q,"at":%q,"delay_seconds":60,"next_at":%q}],"held":false}],"count":1}`,
		strings.Repeat("é", 6000), at.Format(time.RFC3339), at.Format(time.RFC3339))
	text := renderInfraRetriesForChat(mustParseInfraRetryStatus(t, body))
	if len(text) > infraRetryChatMaxChars {
		t.Fatalf("reply is %d characters, over the %d budget", len(text), infraRetryChatMaxChars)
	}
	if !utf8.ValidString(text) || !strings.HasSuffix(text, "(reply cut to fit one message)") {
		t.Errorf("a cut reply must stay valid text and say it was cut: %q", text[len(text)-60:])
	}
}

func TestInfraRetryComm_DaemonErrorIsReported(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusBadRequest, "bad request: limit must be a whole number from 1 to 1000")
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous infra-retries"), " ")
	if !strings.Contains(got, "infra-retries failed") || !strings.Contains(got, "HTTP 400") {
		t.Errorf("expected a failure reply carrying the refusal, got %q", got)
	}

	_, port = laneFillCommDaemon(t, http.StatusOK, "not json")
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous infra-retries"), " ")
	if !strings.Contains(got, "infra-retries failed") {
		t.Errorf("an unreadable body must give a failure reply, got %q", got)
	}
}

func TestInfraRetryComm_HelpListsVerb(t *testing.T) {
	r := &Router{}
	help := strings.Join(r.HandleTestMessage("autonomous"), " ")
	if !strings.Contains(help, "infra-retries [automaton-id] [limit]") {
		t.Errorf("autonomous help lacks the verb: %q", help)
	}
}

func TestInfraRetryComm_ParityListContainsVerb(t *testing.T) {
	found := false
	for _, v := range InfraRetryCommVerbs() {
		if v == "infra-retries" {
			found = true
		}
	}
	if !found {
		t.Errorf("InfraRetryCommVerbs() = %v, want it to contain infra-retries", InfraRetryCommVerbs())
	}
}

// infraRetryConfigDaemon stands in for PUT /api/config: it records each body and
// refuses a categories value that names an unknown category. The router's
// configure function sends one key per PUT and shows the daemon's refusal text
// as it is, the way the daemon's own configure function does.
func infraRetryConfigDaemon(t *testing.T) (*Router, *[]map[string]string) {
	t.Helper()
	var bodies []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		var body map[string]string
		_ = json.Unmarshal(raw, &body)
		if req.Method != http.MethodPut || req.URL.Path != "/api/config" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		bodies = append(bodies, body)
		if strings.Contains(body["autonomous.infra_retry.categories"], "bogus") {
			http.Error(w, `autonomous.infra_retry.categories: unknown category "bogus"; valid: spawn_error, storage_write, session_killed_before_delivery, daemon_restart`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	r := &Router{hostname: "h"}
	r.SetConfigureFunc(func(key, value string) error {
		b, _ := json.Marshal(map[string]string{key: value})
		req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/config", strings.NewReader(string(b)))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode != http.StatusOK {
			msg, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("%s", strings.TrimSpace(string(msg)))
		}
		return nil
	})
	return r, &bodies
}

func TestInfraRetryComm_ConfigureRoundTripSendsKeysUnchanged(t *testing.T) {
	r, bodies := infraRetryConfigDaemon(t)
	for key, value := range map[string]string{
		"autonomous.infra_retry.max_tries":        "5",
		"autonomous.infra_retry.enabled":          "false",
		"autonomous.infra_retry.backoff_seconds":  "30,120",
		"autonomous.infra_retry.categories":       "spawn_error,daemon_restart",
		"autonomous.infra_retry.exhausted_action": "count",
		"autonomous.infra_retry.retry_dependents": "false",
		"autonomous.infra_retry.raise_question":   "false",
	} {
		before := len(*bodies)
		reply := strings.Join(r.HandleTestMessage("configure "+key+"="+value), " ")
		if !strings.Contains(reply, "Set "+key+" = "+value) {
			t.Errorf("%s: expected a confirmation, got %q", key, reply)
		}
		if len(*bodies) != before+1 || (*bodies)[before][key] != value || len((*bodies)[before]) != 1 {
			t.Errorf("%s: PUT /api/config bodies = %v, want one body {%s: %s}", key, *bodies, key, value)
		}
	}
}

func TestInfraRetryComm_ConfigureRefusalIsShown(t *testing.T) {
	r, bodies := infraRetryConfigDaemon(t)
	reply := strings.Join(r.HandleTestMessage("configure autonomous.infra_retry.categories=spawn_error,bogus"), " ")
	if len(*bodies) != 1 {
		t.Fatalf("the router must pass the value on for the daemon to judge, got %d PUTs", len(*bodies))
	}
	for _, want := range []string{"Configure failed", `unknown category "bogus"`, "valid: spawn_error"} {
		if !strings.Contains(reply, want) {
			t.Errorf("reply lacks %q: %q", want, reply)
		}
	}
	if strings.Contains(reply, "Set autonomous.infra_retry.categories") {
		t.Errorf("a refused value must not be confirmed: %q", reply)
	}
}

func TestInfraRetryComm_ConfigureHelpListsAllKeys(t *testing.T) {
	r := &Router{}
	r.SetConfigureFunc(func(string, string) error { return nil })
	keys := []string{
		"autonomous.infra_retry.enabled", "autonomous.infra_retry.max_tries",
		"autonomous.infra_retry.backoff_seconds", "autonomous.infra_retry.categories",
		"autonomous.infra_retry.exhausted_action", "autonomous.infra_retry.retry_dependents",
		"autonomous.infra_retry.raise_question",
		"spawn_error, storage_write, session_killed_before_delivery, daemon_restart",
		"default 60,300,540", "hold|count",
	}
	for _, reply := range [][]string{r.HandleTestMessage("configure help"), r.HandleTestMessage("configure list")} {
		text := strings.Join(reply, " ")
		for _, k := range keys {
			if !strings.Contains(text, k) {
				t.Errorf("configure usage lacks %s", k)
			}
		}
	}
}

func TestInfraRetryComm_StatusNamesInfrastructureHold(t *testing.T) {
	r, d := newEscalationStoreRouter(t)
	d.status = `{"review_needed":[` +
		`{"automaton_id":"p1","automaton_title":"First","task_id":"t1","task_title":"task one","failure_count":0,"last_backend":"pi-prompt","held_at":"2026-10-30T09:00:00Z","held_cause":"spawn_error: tmux spawn failed"},` +
		`{"automaton_id":"p2","automaton_title":"Second","task_id":"t2","task_title":"task two","failure_count":3,"last_backend":"claude-code","held_at":"2026-10-30T10:00:00Z"}]}`
	out := strings.Join(r.HandleTestMessage("autonomous status"), "\n")
	if !strings.Contains(out, "held after infrastructure retries: spawn_error: tmux spawn failed") {
		t.Errorf("status lacks the infrastructure hold cause:\n%s", out)
	}
	if n := strings.Count(out, "held after infrastructure retries"); n != 1 {
		t.Errorf("only the infrastructure hold carries the cause, found %d mentions:\n%s", n, out)
	}
}

func mustParseInfraRetryStatus(t *testing.T, body string) autonomous.InfraRetryStatus {
	t.Helper()
	st, err := autonomous.ParseInfraRetryStatus([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// infraRetryCommHugeReasonBody has one newest task whose only retry reason is
// far longer than a chat message, followed by older small tasks.
func infraRetryCommHugeReasonBody(older int, at time.Time) string {
	huge := fmt.Sprintf(`{"automaton_id":"a1","automaton_title":"A","task_id":"t0","task_title":"T","status":"pending","retry_count":1,"max_tries":3,`+
		`"retries":[{"number":1,"class":"spawn_error","reason":%q,"at":%q,"delay_seconds":60,"next_at":%q}],"held":false}`,
		strings.Repeat("é", 6000), at.Format(time.RFC3339), at.Format(time.RFC3339))
	tasks := []string{huge}
	for i := 1; i <= older; i++ {
		tasks = append(tasks, infraRetryCommTask("a1", fmt.Sprintf("old%d", i), 1, at))
	}
	return infraRetryCommBody(tasks...)
}

func TestInfraRetryComm_RawCutStillCountsLeftOutTasks(t *testing.T) {
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	text := renderInfraRetriesForChat(mustParseInfraRetryStatus(t, infraRetryCommHugeReasonBody(1, at)))
	if len(text) > infraRetryChatMaxChars {
		t.Fatalf("reply is %d characters, over the %d budget", len(text), infraRetryChatMaxChars)
	}
	if !utf8.ValidString(text) {
		t.Errorf("a cut reply must stay valid text")
	}
	for _, want := range []string{"(reply cut to fit one message)", "(1 older task not shown"} {
		if !strings.Contains(text, want) {
			t.Errorf("reply lacks %q: %q", want, text[len(text)-200:])
		}
	}
	if !strings.HasSuffix(text, "to narrow the list)") {
		t.Errorf("the left-out count must close the reply: %q", text[len(text)-120:])
	}
	if strings.Contains(text, "task old1 ") {
		t.Errorf("an older task must not be shown when the newest one fills the message")
	}
}

func TestInfraRetryComm_RawCutCountsManyLeftOutTasks(t *testing.T) {
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	text := renderInfraRetriesForChat(mustParseInfraRetryStatus(t, infraRetryCommHugeReasonBody(25, at)))
	if len(text) > infraRetryChatMaxChars {
		t.Fatalf("reply is %d characters, over the %d budget", len(text), infraRetryChatMaxChars)
	}
	if !strings.Contains(text, "(25 older tasks not shown") {
		t.Errorf("reply must count all 25 left-out tasks: %q", text[len(text)-200:])
	}
}

func TestInfraRetryComm_ManyTasksRenderQuickly(t *testing.T) {
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	var tasks []string
	for i := 1; i <= 1000; i++ {
		tasks = append(tasks, infraRetryCommTask("a1", fmt.Sprintf("t%04d", i), 3, at))
	}
	st := mustParseInfraRetryStatus(t, infraRetryCommBody(tasks...))
	start := time.Now()
	text := renderInfraRetriesForChat(st)
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("rendering 1000 tasks took %s; the cost must follow what is shown, not the task count", d)
	}
	if len(text) > infraRetryChatMaxChars || !strings.Contains(text, " older tasks not shown") {
		t.Errorf("reply must fit and count the left-out tasks: %d characters", len(text))
	}
}
