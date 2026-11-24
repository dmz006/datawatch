package server

// The interim safeguard alert: an operator pause resume, an attempt-count force
// reset (and, once it lands, a shipped-work force run) made by a caller that is
// neither the web app nor a session credential raises one alert naming the
// caller, the action, the target and the reason, and writes an audit record.
// The web app is told apart by the X-Datawatch-Client marker its fetch wrapper
// sends; a forged marker only suppresses the alert and is itself recorded.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/pause"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// safeguardFixture is a server with a pause store, an alert store and an audit
// log, all in temp dirs.
func safeguardFixture(t *testing.T) (*Server, *pause.Store, *alerts.Store, *audit.Log) {
	t.Helper()
	s, ps, al := pauseServer(t)
	as, err := alerts.NewStore(filepath.Join(t.TempDir(), "alerts.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.alertStore = as
	return s, ps, as, al
}

// scriptCall is a plain REST call: loopback address, curl, no marker.
func scriptCall(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:41234"
	req.Header.Set("User-Agent", "curl/8.5.0")
	req.Header.Set("Authorization", "Bearer super-secret-admin-token")
	return req
}

func safeguardEntries(t *testing.T, al *audit.Log) []audit.Entry {
	t.Helper()
	got, err := al.Read(audit.QueryFilter{Action: "safeguard_action"})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func resumeWith(s *Server, req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	s.handlePauseResume(rr, req)
	return rr
}

func TestSafeguardAlert_ScriptResumeRaisesOneAlertNamingTheCaller(t *testing.T) {
	s, ps, as, al := safeguardFixture(t)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "release freeze", "operator")

	rr := resumeWith(s, scriptCall(http.MethodPost, "/api/pause/resume", `{"scope":"global","actor":"nightly-script","note":"freeze is over"}`))
	if rr.Code != 200 {
		t.Fatalf("resume = %d %s", rr.Code, rr.Body.String())
	}
	got := as.List()
	if len(got) != 1 {
		t.Fatalf("alerts = %d, want exactly 1: %+v", len(got), got)
	}
	a := got[0]
	if a.Level != alerts.LevelWarn || a.Source != "system" {
		t.Errorf("alert level/source = %s/%s, want warn/system", a.Level, a.Source)
	}
	text := a.Title + "\n" + a.Body
	for _, want := range []string{"127.0.0.1:41234", "curl/8.5.0", "nightly-script", "resume", "global", "freeze is over"} {
		if !strings.Contains(text, want) {
			t.Errorf("alert text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "super-secret-admin-token") {
		t.Errorf("alert text leaks the credential:\n%s", text)
	}
	ents := safeguardEntries(t, al)
	if len(ents) != 1 {
		t.Fatalf("safeguard audit entries = %d, want 1", len(ents))
	}
	d := ents[0].Details
	if d["alert_raised"] != true || d["remote_addr"] != "127.0.0.1:41234" || d["user_agent"] != "curl/8.5.0" || d["claimed_actor"] != "nightly-script" || d["action"] != "pause_resume" {
		t.Errorf("audit details = %+v", d)
	}
	if _, blocked := ps.Blocked("/x"); blocked {
		t.Fatal("the resume itself must still take effect")
	}
}

func TestSafeguardAlert_OneAlertPerAction(t *testing.T) {
	s, ps, as, _ := safeguardFixture(t)
	for i := 0; i < 2; i++ {
		_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
		resumeWith(s, scriptCall(http.MethodPost, "/api/pause/resume", `{"scope":"global"}`))
	}
	if n := len(as.List()); n != 2 {
		t.Fatalf("two resumes raised %d alerts, want 2", n)
	}
}

func TestSafeguardAlert_ResumeAllAlertsOnceAndNamesTheScope(t *testing.T) {
	s, ps, as, _ := safeguardFixture(t)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "a", "operator")
	_, _ = ps.Pause(pause.ScopeProject, "/work/web", "b", "operator")
	resumeWith(s, scriptCall(http.MethodPost, "/api/pause/resume", `{"all":true}`))
	got := as.List()
	if len(got) != 1 || !strings.Contains(got[0].Title+got[0].Body, "all") {
		t.Fatalf("resume all alerts = %+v, want one naming all", got)
	}
}

func TestSafeguardAlert_ResumeThatClearsNothingRaisesNothing(t *testing.T) {
	s, _, as, _ := safeguardFixture(t)
	resumeWith(s, scriptCall(http.MethodPost, "/api/pause/resume", `{"scope":"global"}`))
	if n := len(as.List()); n != 0 {
		t.Fatalf("a resume that unpaused nothing raised %d alerts", n)
	}
}

func TestSafeguardAlert_WebAppMarkerRaisesNoAlertButIsAudited(t *testing.T) {
	s, ps, as, al := safeguardFixture(t)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	req := scriptCall(http.MethodPost, "/api/pause/resume", `{"scope":"global","actor":"web"}`)
	req.Header.Set("X-Datawatch-Client", "pwa")
	if rr := resumeWith(s, req); rr.Code != 200 {
		t.Fatalf("resume = %d %s", rr.Code, rr.Body.String())
	}
	if n := len(as.List()); n != 0 {
		t.Fatalf("the web app raised %d alerts, want none", n)
	}
	ents := safeguardEntries(t, al)
	if len(ents) != 1 {
		t.Fatalf("safeguard audit entries = %d, want 1 (marker use is recorded)", len(ents))
	}
	d := ents[0].Details
	if d["client_marker"] != "pwa" || d["alert_raised"] != false {
		t.Errorf("audit details = %+v, want client_marker pwa and alert_raised false", d)
	}
	if d["remote_addr"] != "127.0.0.1:41234" || d["user_agent"] != "curl/8.5.0" {
		t.Errorf("a marked call must still record where it came from: %+v", d)
	}
}

func TestSafeguardAlert_SessionCredentialIsRefusedAsBeforeWithNoAlert(t *testing.T) {
	s, ps, as, al := safeguardFixture(t)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	req := scriptCall(http.MethodPost, "/api/pause/resume", `{"scope":"global","actor":"operator"}`)
	req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, []string{"pause:write"}))
	rr := resumeWith(s, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("session resume = %d, want 403 as before", rr.Code)
	}
	if n := len(as.List()); n != 0 {
		t.Fatalf("a refused session resume raised %d alerts", n)
	}
	if n := len(safeguardEntries(t, al)); n != 0 {
		t.Fatalf("a refused resume wrote %d safeguard entries", n)
	}
	if _, blocked := ps.Blocked("/x"); !blocked {
		t.Fatal("the pause must still hold")
	}
}

func TestSafeguardAlert_FederationPeerIsNamedAndAlerts(t *testing.T) {
	s, ps, as, _ := safeguardFixture(t)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	req := scriptCall(http.MethodPost, "/api/pause/resume", `{"scope":"global"}`)
	req = req.WithContext(context.WithValue(req.Context(), fedPeerKey, &multiserver.Entry{Name: "lab-2", Capabilities: []string{"pause:write"}}))
	if rr := resumeWith(s, req); rr.Code != 200 {
		t.Fatalf("peer resume = %d %s", rr.Code, rr.Body.String())
	}
	got := as.List()
	if len(got) != 1 || !strings.Contains(got[0].Title+got[0].Body, "peer:lab-2") {
		t.Fatalf("peer alert = %+v, want one naming peer:lab-2", got)
	}
}

func TestSafeguardAlert_SwitchedOffRaisesNoAlertButStillAudits(t *testing.T) {
	s, ps, as, al := safeguardFixture(t)
	off := false
	s.cfg.Audit.SafeguardAlert = &off
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	resumeWith(s, scriptCall(http.MethodPost, "/api/pause/resume", `{"scope":"global"}`))
	if n := len(as.List()); n != 0 {
		t.Fatalf("switched off, still raised %d alerts", n)
	}
	ents := safeguardEntries(t, al)
	if len(ents) != 1 || ents[0].Details["alert_raised"] != false {
		t.Fatalf("audit = %+v, want one entry with alert_raised false", ents)
	}
}

func TestSafeguardAlert_NoAlertStoreIsNotAFailure(t *testing.T) {
	s, ps, _, al := safeguardFixture(t)
	s.alertStore = nil
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	if rr := resumeWith(s, scriptCall(http.MethodPost, "/api/pause/resume", `{"scope":"global"}`)); rr.Code != 200 {
		t.Fatalf("resume = %d", rr.Code)
	}
	if ents := safeguardEntries(t, al); len(ents) != 1 || ents[0].Details["alert_raised"] != false {
		t.Fatalf("audit = %+v", ents)
	}
}

// ---- attempt-count force reset ----

// resetAPI is a manager whose reset always succeeds.
type resetAPI struct {
	fakeOrchAutonomous
	calls []resetCall
}

type resetCall struct {
	actor string
	force bool
}

func (a *resetAPI) ResetTaskWithOptions(prdID, taskID, actor string, force, cleanRestart bool) (any, error) {
	a.calls = append(a.calls, resetCall{actor, force})
	return map[string]any{"id": prdID}, nil
}

func resetFixture(t *testing.T) (*Server, *alerts.Store, *audit.Log, *resetAPI) {
	t.Helper()
	as, err := alerts.NewStore(filepath.Join(t.TempDir(), "alerts.json"))
	if err != nil {
		t.Fatal(err)
	}
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	api := &resetAPI{}
	return &Server{cfg: &config.Config{}, autonomousMgr: api, alertStore: as, auditLog: al}, as, al, api
}

func resetWith(s *Server, req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)
	return rr
}

func TestSafeguardAlert_ScriptForceResetRaisesOneAlert(t *testing.T) {
	s, as, al, _ := resetFixture(t)
	rr := resetWith(s, scriptCall(http.MethodPost, "/api/autonomous/prds/prd-7/reset_task",
		`{"task_id":"task-3","force":true,"reason":"flaky node, retrying by hand"}`))
	if rr.Code != 200 {
		t.Fatalf("reset = %d %s", rr.Code, rr.Body.String())
	}
	got := as.List()
	if len(got) != 1 {
		t.Fatalf("alerts = %d, want 1: %+v", len(got), got)
	}
	text := got[0].Title + "\n" + got[0].Body
	for _, want := range []string{"127.0.0.1:41234", "curl/8.5.0", "operator", "prd-7", "task-3", "flaky node, retrying by hand", "attempt"} {
		if !strings.Contains(text, want) {
			t.Errorf("alert text lacks %q:\n%s", want, text)
		}
	}
	ents := safeguardEntries(t, al)
	if len(ents) != 1 || ents[0].Details["action"] != "attempt_count_force_reset" || ents[0].Details["alert_raised"] != true {
		t.Fatalf("audit = %+v", ents)
	}
}

func TestSafeguardAlert_ForceResetWithoutAReasonSaysSo(t *testing.T) {
	s, as, _, _ := resetFixture(t)
	resetWith(s, scriptCall(http.MethodPost, "/api/autonomous/prds/prd-7/reset_task", `{"task_id":"task-3","force":true}`))
	got := as.List()
	if len(got) != 1 || !strings.Contains(got[0].Body, "no reason given") {
		t.Fatalf("alerts = %+v, want one that says no reason given", got)
	}
}

func TestSafeguardAlert_PlainResetAndNonOperatorForceRaiseNothing(t *testing.T) {
	s, as, _, _ := resetFixture(t)
	// A reset without force does not clear the attempt count.
	resetWith(s, scriptCall(http.MethodPost, "/api/autonomous/prds/prd-7/reset_task", `{"task_id":"task-3"}`))
	// A force reset that names another actor does not clear it either.
	resetWith(s, scriptCall(http.MethodPost, "/api/autonomous/prds/prd-7/reset_task", `{"task_id":"task-3","force":true,"actor":"doer"}`))
	if n := len(as.List()); n != 0 {
		t.Fatalf("raised %d alerts for resets that do not clear the count", n)
	}
}

func TestSafeguardAlert_WebAppForceResetNoAlertButAudited(t *testing.T) {
	s, as, al, _ := resetFixture(t)
	req := scriptCall(http.MethodPost, "/api/autonomous/prds/prd-7/reset_task", `{"task_id":"task-3","actor":"operator","force":true}`)
	req.Header.Set("X-Datawatch-Client", "pwa")
	if rr := resetWith(s, req); rr.Code != 200 {
		t.Fatalf("reset = %d %s", rr.Code, rr.Body.String())
	}
	if n := len(as.List()); n != 0 {
		t.Fatalf("the web app raised %d alerts", n)
	}
	ents := safeguardEntries(t, al)
	if len(ents) != 1 || ents[0].Details["client_marker"] != "pwa" || ents[0].Details["alert_raised"] != false {
		t.Fatalf("audit = %+v", ents)
	}
}

func TestSafeguardAlert_SessionCredentialForceResetNeverClearsAndNeverAlerts(t *testing.T) {
	s, as, al, api := resetFixture(t)
	store, err := auth.NewSessionTokenStore(filepath.Join(t.TempDir(), "tok.json"))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := store.Mint("host-sess-7", []string{"autonomous:write"})
	if err != nil {
		t.Fatal(err)
	}
	s.sessionTokens = store
	req := scriptCall(http.MethodPost, "/api/autonomous/prds/prd-7/reset_task", `{"task_id":"task-3","actor":"operator","force":true}`)
	ctx := context.WithValue(req.Context(), sessionCapsKey, []string{"autonomous:write"})
	req = req.WithContext(context.WithValue(ctx, callerTokenKey, tok))
	resetWith(s, req)
	if len(api.calls) != 1 || !strings.HasPrefix(api.calls[0].actor, "session:") {
		t.Fatalf("the manager must see a session actor, got %+v", api.calls)
	}
	if n := len(as.List()); n != 0 {
		t.Fatalf("a session credential raised %d alerts", n)
	}
	if n := len(safeguardEntries(t, al)); n != 0 {
		t.Fatalf("a session credential wrote %d safeguard entries", n)
	}
}

// ---- the shared helper, for the shipped-work force run ----

func TestSafeguardAlert_HelperIsTheSharedEntryPoint(t *testing.T) {
	s, as, al, _ := resetFixture(t)
	req := scriptCall(http.MethodPost, "/api/autonomous/prds/prd-9/run", `{"force":true,"reason":"re-run the landing"}`)
	s.noteSafeguardAction(req, safeguardEvent{
		Action: safeguardShippedForceRun, Target: "automaton prd-9", Actor: "operator", Reason: "re-run the landing",
	})
	got := as.List()
	if len(got) != 1 || !strings.Contains(got[0].Title+got[0].Body, "prd-9") || !strings.Contains(got[0].Body, "re-run the landing") {
		t.Fatalf("alerts = %+v", got)
	}
	if ents := safeguardEntries(t, al); len(ents) != 1 || ents[0].Details["action"] != "shipped_work_force_run" {
		t.Fatalf("audit = %+v", ents)
	}
}

func TestSafeguardAlert_MarkerSpellingAndValue(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{{"pwa", true}, {"PWA", true}, {" pwa ", true}, {"curl", false}, {"", false}, {"pwa2", false}} {
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		if tc.val != "" {
			req.Header.Set("X-Datawatch-Client", tc.val)
		}
		if got := isWebAppCall(req); got != tc.want {
			t.Errorf("marker %q => %v, want %v", tc.val, got, tc.want)
		}
	}
}

func TestSafeguardAlert_CallerTextCannotForgeLinesOrFlood(t *testing.T) {
	s, ps, as, al := safeguardFixture(t)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	req := scriptCall(http.MethodPost, "/api/pause/resume", `{"scope":"global","note":"ok\nAction: nothing happened"}`)
	req.Header.Set("User-Agent", "evil\nReason: approved by the operator\r"+strings.Repeat("x", 5000))
	resumeWith(s, req)
	got := as.List()
	if len(got) != 1 {
		t.Fatalf("alerts = %d", len(got))
	}
	if n := strings.Count(got[0].Body, "\nReason:"); n != 1 {
		t.Errorf("a user agent or note forged a line; body:\n%s", got[0].Body)
	}
	if len(got[0].Body) > 2000 {
		t.Errorf("alert body is %d bytes; caller text must be bounded", len(got[0].Body))
	}
	if ents := safeguardEntries(t, al); len(ents) != 1 || len(ents[0].Details["user_agent"].(string)) > 210 {
		t.Errorf("audit user agent not bounded: %+v", ents)
	}
}

func TestSafeguardAlert_AuditNeverCarriesTheCredential(t *testing.T) {
	s, ps, _, al := safeguardFixture(t)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	resumeWith(s, scriptCall(http.MethodPost, "/api/pause/resume", `{"scope":"global"}`))
	for _, e := range safeguardEntries(t, al) {
		if strings.Contains(fmt.Sprint(e), "super-secret-admin-token") {
			t.Fatalf("audit entry leaks the credential: %+v", e)
		}
	}
}

func safeguardAlertSetting(t *testing.T, s *Server) bool {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var got struct {
		Audit map[string]any `json:"audit"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	v, ok := got.Audit["safeguard_alert"].(bool)
	if !ok {
		t.Fatalf("GET /api/config audit lacks safeguard_alert: %v", got.Audit)
	}
	return v
}

// TestSafeguardAlert_ConfigKeyRoundTripAndDefaultOn: the setting is on until
// someone turns it off, round-trips through PUT and GET /api/config, and is
// what the alert path reads, live.
func TestSafeguardAlert_ConfigKeyRoundTripAndDefaultOn(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	if !safeguardAlertSetting(t, s) {
		t.Fatal("audit.safeguard_alert must default to on")
	}
	if rec := putInfraRetryRaw(s, map[string]any{"audit.safeguard_alert": false}); rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body.String())
	}
	if safeguardAlertSetting(t, s) || s.cfg.Audit.SafeguardAlertOrDefault() {
		t.Fatal("the key must read false after PUT false")
	}
	if rec := putInfraRetryRaw(s, map[string]any{"audit.safeguard_alert": true}); rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d", rec.Code)
	}
	if !safeguardAlertSetting(t, s) {
		t.Fatal("the key must read true after PUT true")
	}
}
