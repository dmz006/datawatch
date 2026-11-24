package server

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/config"
)

// forcedRunAPI is a manager whose Run and RunForced are recorded; RunForced
// fails with forceErr when it is set.
type forcedRunAPI struct {
	fakeOrchAutonomous
	runs     int
	forced   []forcedRunCall
	forceErr error
}

type forcedRunCall struct{ id, actor, reason string }

func (a *forcedRunAPI) Run(string) error { a.runs++; return nil }

func (a *forcedRunAPI) RunForced(id, actor, reason string) error {
	if a.forceErr != nil {
		return a.forceErr
	}
	a.forced = append(a.forced, forcedRunCall{id, actor, reason})
	return nil
}

func forcedRunFixture(t *testing.T) (*Server, *alerts.Store, *audit.Log, *forcedRunAPI) {
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
	api := &forcedRunAPI{}
	return &Server{cfg: &config.Config{}, autonomousMgr: api, alertStore: as, auditLog: al}, as, al, api
}

// A forced run of work that already shipped, made by a plain REST call with no
// client marker, raises one alert naming the caller, the Automaton and why.
func TestSafeguardAlert_ScriptShippedForceRunRaisesOneAlert(t *testing.T) {
	s, as, al, api := forcedRunFixture(t)
	rr := resetWith(s, scriptCall(http.MethodPost, "/api/autonomous/prds/prd-7/run",
		`{"force":true,"reason":"re-land the hotfix by hand"}`))
	if rr.Code != 200 {
		t.Fatalf("run = %d %s", rr.Code, rr.Body.String())
	}
	if len(api.forced) != 1 {
		t.Fatalf("RunForced calls = %d, want 1", len(api.forced))
	}
	got := as.List()
	if len(got) != 1 {
		t.Fatalf("alerts = %d, want 1: %+v", len(got), got)
	}
	text := got[0].Title + "\n" + got[0].Body
	for _, want := range []string{"127.0.0.1:41234", "curl/8.5.0", "prd-7", "re-land the hotfix by hand", "already shipped"} {
		if !strings.Contains(text, want) {
			t.Errorf("alert text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "super-secret-admin-token") {
		t.Errorf("the alert carries the credential:\n%s", text)
	}
	ents := safeguardEntries(t, al)
	if len(ents) != 1 || ents[0].Details["action"] != "shipped_work_force_run" || ents[0].Details["alert_raised"] != true {
		t.Fatalf("audit = %+v", ents)
	}
}

// A run without force lifts nothing, so it raises nothing.
func TestSafeguardAlert_PlainRunRaisesNothing(t *testing.T) {
	s, as, al, api := forcedRunFixture(t)
	if rr := resetWith(s, scriptCall(http.MethodPost, "/api/autonomous/prds/prd-7/run", `{}`)); rr.Code != 200 {
		t.Fatalf("run = %d %s", rr.Code, rr.Body.String())
	}
	if api.runs != 1 || len(api.forced) != 0 {
		t.Fatalf("runs=%d forced=%d, want a plain run only", api.runs, len(api.forced))
	}
	if n := len(as.List()); n != 0 {
		t.Fatalf("a plain run raised %d alerts", n)
	}
	if n := len(safeguardEntries(t, al)); n != 0 {
		t.Fatalf("a plain run wrote %d safeguard entries", n)
	}
}

// A forced run the manager refuses did not lift anything: no alert.
func TestSafeguardAlert_RefusedShippedForceRunRaisesNothing(t *testing.T) {
	s, as, al, api := forcedRunFixture(t)
	api.forceErr = errors.New("a reason is required")
	rr := resetWith(s, scriptCall(http.MethodPost, "/api/autonomous/prds/prd-7/run", `{"force":true}`))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("run = %d, want 400", rr.Code)
	}
	if n := len(as.List()); n != 0 {
		t.Fatalf("a refused forced run raised %d alerts", n)
	}
	if n := len(safeguardEntries(t, al)); n != 0 {
		t.Fatalf("a refused forced run wrote %d safeguard entries", n)
	}
}

// The web app raises no alert, but the action is still audited with the marker.
func TestSafeguardAlert_WebAppShippedForceRunNoAlertButAudited(t *testing.T) {
	s, as, al, _ := forcedRunFixture(t)
	req := scriptCall(http.MethodPost, "/api/autonomous/prds/prd-7/run", `{"force":true,"reason":"operator re-run"}`)
	req.Header.Set("X-Datawatch-Client", "pwa")
	if rr := resetWith(s, req); rr.Code != 200 {
		t.Fatalf("run = %d %s", rr.Code, rr.Body.String())
	}
	if n := len(as.List()); n != 0 {
		t.Fatalf("the web app raised %d alerts", n)
	}
	ents := safeguardEntries(t, al)
	if len(ents) != 1 || ents[0].Details["client_marker"] != "pwa" || ents[0].Details["alert_raised"] != false {
		t.Fatalf("audit = %+v", ents)
	}
}

// A session credential is neither alerted nor recorded here.
func TestSafeguardAlert_SessionCredentialShippedForceRunNeverAlerts(t *testing.T) {
	s, as, al, _ := forcedRunFixture(t)
	store, err := auth.NewSessionTokenStore(filepath.Join(t.TempDir(), "tok.json"))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := store.Mint("host-sess-7", []string{"autonomous:write"})
	if err != nil {
		t.Fatal(err)
	}
	s.sessionTokens = store
	req := scriptCall(http.MethodPost, "/api/autonomous/prds/prd-7/run", `{"force":true,"reason":"x"}`)
	ctx := context.WithValue(req.Context(), sessionCapsKey, []string{"autonomous:write"})
	req = req.WithContext(context.WithValue(ctx, callerTokenKey, tok))
	resetWith(s, req)
	if n := len(as.List()); n != 0 {
		t.Fatalf("a session credential raised %d alerts", n)
	}
	if n := len(safeguardEntries(t, al)); n != 0 {
		t.Fatalf("a session credential wrote %d safeguard entries", n)
	}
}
