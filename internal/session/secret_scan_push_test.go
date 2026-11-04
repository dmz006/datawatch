// Pre-push secret scan (git.secret_scan) for the one-shot push surfaces:
// effectiveSecretScan policy resolution, recordShadowSecretFindings
// recording, and the shadow / enforce / off outcomes of both push paths
// (pushAfterCommit and pushUnpushedBeforeTeardown), driven end to end
// against a temp bare remote. No daemon, no gh, nothing outside
// t.TempDir, and no real secret: the token-shaped material is built from
// fragments at runtime, so no contiguous secret-shaped literal exists in
// this source file.

package session

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/git"
)

// fixedTestTime is the fixed timestamp for any session fields the tests
// touch; these paths carry no clock hook, so nothing in the assertions
// depends on wall-clock time — the fixed value just keeps the records
// deterministic.
var fixedTestTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// isolatedGitEnv points git's global configuration at a throwaway file so
// the tests never read the operator's git configuration, and refuses any
// terminal prompt.
func isolatedGitEnv(t *testing.T) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig.global")
	if err := os.WriteFile(cfg, []byte("[user]\n\tname = datawatch-test\n\temail = t@t.test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
}

// secretTokenMaterial builds a GitHub-Token-shaped value from fragments
// at runtime (the ghp_ prefix and the 36 word chars are never stored as
// one literal). It matches the github_token detector (gh[pousr]_ plus
// 20..255 word chars, no entropy gate).
func secretTokenMaterial() string {
	return "gh" + "p_" + strings.Repeat("a", 36)
}

// addSecretCommit commits one file that carries a token-shaped line on
// HEAD.
func addSecretCommit(t *testing.T, dir, msg string) {
	t.Helper()
	body := "service = billing\ntoken = " + secretTokenMaterial() + "\n"
	if err := os.WriteFile(filepath.Join(dir, "creds.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", msg)
}

// secretScanAlert is one recorded SetPushAlertFn invocation.
type secretScanAlert struct{ level, title, body string }

// recordingAlerts returns a SetPushAlertFn sink plus the slice of calls
// it recorded.
func recordingAlerts() (fn func(level, title, body string), out *[]secretScanAlert) {
	out = &[]secretScanAlert{}
	fn = func(level, title, body string) {
		*out = append(*out, secretScanAlert{level: level, title: title, body: body})
	}
	return fn, out
}

// pushSecretScanFixture builds a temp bare remote + working clone (seed
// pushed), a bare Manager over a real temp store with a running one-shot
// session bound to the clone, a recording alert sink, and branch-policy
// push settings pointing at origin.
func pushSecretScanFixture(t *testing.T) (bare, dir string, mgr *Manager, sess *Session, alerts *[]secretScanAlert) {
	t.Helper()
	isolatedGitEnv(t)
	bare, dir = setupBareClone(t)
	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	mgr = &Manager{store: store, hostname: "testhost"}
	sess = &Session{
		ID: "abcd", FullID: "testhost-abcd", Hostname: "testhost",
		State: StateRunning, OneShot: true, ProjectDir: dir, PRDID: "",
		TmuxSession: "", CreatedAt: fixedTestTime, UpdatedAt: fixedTestTime,
	}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}
	fn, alerts := recordingAlerts()
	mgr.SetPushAlertFn(fn)
	mgr.SetGitPushSettings(func() (string, string) { return "branch", "origin" })
	return bare, dir, mgr, sess, alerts
}

// Test 1 — effectiveSecretScan: unwired reads as off, an empty mode is
// normalised to off (never shadow), and shadow/enforce pass through with
// their detector, path and fingerprint lists intact.
func TestEffectiveSecretScan_ResolvesTheLivePolicy(t *testing.T) {
	shadow := git.SecretScanPolicy{
		Mode:         git.ScanShadow,
		Detectors:    []string{git.DetectorPrivateKey, git.DetectorSlackToken},
		PathPatterns: []string{"vendor/**"},
		Fingerprints: []string{"0123456789abcdef"},
	}
	enforce := git.SecretScanPolicy{
		Mode:         git.ScanEnforce,
		Detectors:    []string{git.DetectorGitHubToken},
		PathPatterns: []string{"testdata"},
		Fingerprints: []string{"fedcba9876543210"},
	}
	for _, tc := range []struct {
		name string
		wire func(m *Manager)
		want git.SecretScanPolicy
	}{
		{"unwired-reads-as-off", func(m *Manager) {}, git.SecretScanPolicy{Mode: git.ScanOff}},
		{"empty-mode-normalised-to-off-not-shadow",
			func(m *Manager) {
				m.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: ""} })
			},
			git.SecretScanPolicy{Mode: git.ScanOff}},
		{"off-mode-reads-as-off",
			func(m *Manager) {
				m.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: git.ScanOff} })
			},
			git.SecretScanPolicy{Mode: git.ScanOff}},
		{"shadow-passes-through-with-detectors-and-allowlists",
			func(m *Manager) { m.SetSecretScanSettings(func() git.SecretScanPolicy { return shadow }) },
			shadow},
		{"enforce-passes-through-with-detectors-and-allowlists",
			func(m *Manager) { m.SetSecretScanSettings(func() git.SecretScanPolicy { return enforce }) },
			enforce},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manager{}
			tc.wire(m)
			got := m.effectiveSecretScan()
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("effectiveSecretScan() = %+v — want %+v", got, tc.want)
			}
		})
	}
}

// Test 2 — recordShadowSecretFindings: two findings (no SecretFinding
// field holds a secret) produce the shadow-mode warning prefix plus the
// redacted summary on the session and exactly ONE warn alert whose title
// carries the session ID and the finding count; the alert body and the
// warning carry nothing but detectors, file:line and fingerprints.
func TestRecordShadowSecretScanFindings_OneWarnAlertAndRedactedSessionWarning(t *testing.T) {
	findings := []git.SecretFinding{
		{Detector: git.DetectorGitHubToken, Commit: strings.Repeat("c", 40), Path: "creds.txt", Line: 2, Fingerprint: "0123456789abcdef"},
		{Detector: git.DetectorSlackToken, Commit: strings.Repeat("c", 40), Path: "deploy/.env", Line: 7, Fingerprint: "fedcba9876543210"},
	}
	fn, alerts := recordingAlerts()
	m := &Manager{}
	m.SetPushAlertFn(fn)
	sess := &Session{ID: "abcd", CreatedAt: fixedTestTime, UpdatedAt: fixedTestTime}

	m.recordShadowSecretFindings(sess, "origin", "main", findings)

	// The session warning: the shadow-mode prefix plus the redacted
	// summary (count, detector, file:line, fingerprint — nothing else).
	wantWarning := "pushed; pre-push secret scan (shadow mode) reported: " + git.RedactedFindingsSummary(findings)
	if sess.PushWarning != wantWarning {
		t.Fatalf("PushWarning = %q — want %q", sess.PushWarning, wantWarning)
	}
	for _, f := range findings {
		for _, want := range []string{f.Detector, f.Path + ":" + strconv.Itoa(f.Line), f.Fingerprint} {
			if !strings.Contains(sess.PushWarning, want) {
				t.Errorf("PushWarning %q — must carry %q", sess.PushWarning, want)
			}
		}
	}
	if strings.Contains(sess.PushWarning, secretTokenMaterial()) {
		t.Error("PushWarning must never carry the matched token material")
	}
	if len(*alerts) != 1 {
		t.Fatalf("exactly ONE alert expected, got %d: %+v", len(*alerts), *alerts)
	}
	a := (*alerts)[0]
	if a.level != "warn" {
		t.Errorf("alert level %q — want warn", a.level)
	}
	if !strings.Contains(a.title, "abcd") {
		t.Errorf("alert title %q — must carry the session ID", a.title)
	}
	if !strings.Contains(a.title, "2") {
		t.Errorf("alert title %q — must carry the finding count", a.title)
	}
	if a.body != git.RedactedFindingsSummary(findings) {
		t.Errorf("alert body %q — want the redacted summary %q", a.body, git.RedactedFindingsSummary(findings))
	}
	if strings.Contains(a.title, secretTokenMaterial()) || strings.Contains(a.body, secretTokenMaterial()) {
		t.Error("the alert must never carry the matched token material")
	}
}

// Test 2b — recordShadowSecretFindings with NO alert sink wired must not
// panic; it still records the redacted warning on the session.
func TestRecordShadowSecretScanFindings_NoAlertSinkDoesNotPanic(t *testing.T) {
	m := &Manager{}
	sess := &Session{ID: "abcd"}
	m.recordShadowSecretFindings(sess, "origin", "main", []git.SecretFinding{
		{Detector: git.DetectorGitHubToken, Commit: strings.Repeat("9", 40), Path: "creds.txt", Line: 1, Fingerprint: "0123456789abcdef"},
		{Detector: git.DetectorSlackToken, Commit: strings.Repeat("9", 40), Path: "x.yml", Line: 3, Fingerprint: "fedcba9876543210"},
	})
	if !strings.HasPrefix(sess.PushWarning, "pushed; pre-push secret scan (shadow mode) reported: ") {
		t.Fatalf("PushWarning %q — want the shadow-mode warning prefix", sess.PushWarning)
	}
}

// Test 3a — pushAfterCommit, shadow: the scan runs, the push still
// succeeds, the session records PushState=pushed with the shadow-mode
// warning, and exactly one warn alert fired whose body never carries the
// token material.
func TestPushAfterCommit_SecretScanShadowPushesRecordsWarningFiresOneWarnAlert(t *testing.T) {
	bare, dir, mgr, sess, alerts := pushSecretScanFixture(t)
	addSecretCommit(t, dir, "add creds")
	head := localHead(t, dir)
	mgr.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: git.ScanShadow} })

	mgr.pushAfterCommit(sess, head)

	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.PushState != "pushed" {
		t.Errorf("PushState %q — want \"pushed\" even in shadow mode", got.PushState)
	}
	if !strings.HasPrefix(got.PushWarning, "pushed; pre-push secret scan (shadow mode) reported: ") {
		t.Fatalf("PushWarning %q — want the shadow-mode warning prefix", got.PushWarning)
	}
	for _, want := range []string{git.DetectorGitHubToken, "creds.txt:", "fingerprint"} {
		if !strings.Contains(got.PushWarning, want) {
			t.Errorf("PushWarning %q — must carry %q", got.PushWarning, want)
		}
	}
	if strings.Contains(got.PushWarning, secretTokenMaterial()) {
		t.Error("PushWarning must never carry the matched token material")
	}
	if got.PushedSHA != head {
		t.Errorf("PushedSHA %q — want the pushed commit %q", got.PushedSHA, head)
	}
	if r := bareHead(t, bare, "main"); r != head {
		t.Errorf("bare remote main %q — shadow mode must not refuse the push (want %q)", r, head)
	}
	if len(*alerts) != 1 {
		t.Fatalf("exactly one warn alert expected in shadow mode, got %d: %+v", len(*alerts), *alerts)
	}
	a := (*alerts)[0]
	if a.level != "warn" {
		t.Errorf("alert level %q — want warn", a.level)
	}
	if !strings.Contains(a.title, sess.ID) || !strings.Contains(a.title, "1") {
		t.Errorf("alert title %q — must carry the session ID and the finding count", a.title)
	}
	for _, want := range []string{git.DetectorGitHubToken, "creds.txt:", "fingerprint"} {
		if !strings.Contains(a.body, want) {
			t.Errorf("alert body %q — must carry %q", a.body, want)
		}
	}
	if strings.Contains(a.body, secretTokenMaterial()) || strings.Contains(a.title, secretTokenMaterial()) {
		t.Error("the alert must never carry the matched token material")
	}
}

// Test 3b — pushAfterCommit, enforce: any finding refuses the push
// BEFORE it runs — PushState=not_pushed, PushWarning starts "not
// pushed: secret scan (enforce mode)", the remote ref is untouched, the
// commit stays local, and exactly one warn alert fired.
func TestPushAfterCommit_SecretScanEnforceRefusesPushAndKeepsRemoteUntouched(t *testing.T) {
	bare, dir, mgr, sess, alerts := pushSecretScanFixture(t)
	seed := bareHead(t, bare, "main")
	addSecretCommit(t, dir, "add creds")
	head := localHead(t, dir)
	mgr.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: git.ScanEnforce} })

	mgr.pushAfterCommit(sess, head)

	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.PushState != "not_pushed" {
		t.Errorf("PushState %q — want \"not_pushed\" under enforce", got.PushState)
	}
	if !strings.HasPrefix(got.PushWarning, "not pushed: secret scan (enforce mode)") {
		t.Fatalf("PushWarning %q — want \"not pushed: secret scan (enforce mode)...\"", got.PushWarning)
	}
	for _, want := range []string{git.DetectorGitHubToken, "creds.txt:", "fingerprint"} {
		if !strings.Contains(got.PushWarning, want) {
			t.Errorf("PushWarning %q — must carry the redacted finding %q", got.PushWarning, want)
		}
	}
	if strings.Contains(got.PushWarning, secretTokenMaterial()) {
		t.Error("PushWarning must never carry the matched token material")
	}
	if got.PushedSHA != "" {
		t.Errorf("PushedSHA %q — nothing reached the remote", got.PushedSHA)
	}
	if r := bareHead(t, bare, "main"); r != seed {
		t.Errorf("bare remote main %q — enforce mode must leave the remote untouched (want seed %q)", r, seed)
	}
	if localHead(t, dir) != head {
		t.Error("the commit must stay committed locally")
	}
	if len(*alerts) != 1 {
		t.Fatalf("exactly one warn alert expected on a refused push, got %d: %+v", len(*alerts), *alerts)
	}
	a := (*alerts)[0]
	if a.level != "warn" {
		t.Errorf("alert level %q — want warn", a.level)
	}
	if !strings.Contains(a.title, sess.ID) {
		t.Errorf("alert title %q — must carry the session ID", a.title)
	}
	if strings.Contains(a.title, secretTokenMaterial()) || strings.Contains(a.body, secretTokenMaterial()) {
		t.Error("the alert must never carry the matched token material")
	}
}

// Test 3c — pushAfterCommit with the scan off or unwired: legacy
// behaviour — the push is plain, the scan never runs, nothing is
// recorded beyond the success, and no alert fires.
func TestPushAfterCommit_SecretScanOffOrUnwiredKeepsLegacyPush(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire bool
	}{
		{"off-mode", true},
		{"unwired", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bare, dir, mgr, sess, alerts := pushSecretScanFixture(t)
			addSecretCommit(t, dir, "add creds")
			head := localHead(t, dir)
			if tc.wire {
				mgr.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: git.ScanOff} })
			}

			mgr.pushAfterCommit(sess, head)

			got, ok := mgr.store.Get(sess.FullID)
			if !ok {
				t.Fatal("session disappeared")
			}
			if got.PushState != "pushed" {
				t.Errorf("PushState %q — want \"pushed\"", got.PushState)
			}
			if got.PushWarning != "" {
				t.Errorf("PushWarning %q — no scan, no warning expected", got.PushWarning)
			}
			if got.PushedSHA != head {
				t.Errorf("PushedSHA %q — want the pushed commit %q", got.PushedSHA, head)
			}
			if r := bareHead(t, bare, "main"); r != head {
				t.Errorf("bare remote main %q — want the pushed commit %q", r, head)
			}
			if len(*alerts) != 0 {
				t.Errorf("no alert expected without a scan or a failure, got: %+v", *alerts)
			}
		})
	}
}

// Test 4a — pushUnpushedBeforeTeardown, shadow, on a side branch (not
// the remote default): the push proceeds, the session records
// PushState=pushed with the shadow-mode warning, and exactly one warn
// alert fired.
func TestPushUnpushedBeforeTeardown_SecretScanShadowPushesRecordsWarningFiresOneWarnAlert(t *testing.T) {
	bare, dir, mgr, sess, alerts := pushSecretScanFixture(t)
	gitRunT(t, dir, "checkout", "-q", "-b", "work") // side branch: not the remote default
	addSecretCommit(t, dir, "add creds on work")
	head := localHead(t, dir)
	mgr.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: git.ScanShadow} })

	rep := mgr.pushUnpushedBeforeTeardown(sess)
	if !rep.Attempted || !rep.Pushed || rep.Reason != "" {
		t.Fatalf("report = %+v — shadow mode must push despite findings", rep)
	}

	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.PushState != "pushed" {
		t.Errorf("PushState %q — want \"pushed\" even in shadow mode", got.PushState)
	}
	if !strings.HasPrefix(got.PushWarning, "pushed; pre-push secret scan (shadow mode) reported: ") {
		t.Fatalf("PushWarning %q — want the shadow-mode warning prefix", got.PushWarning)
	}
	for _, want := range []string{git.DetectorGitHubToken, "creds.txt:", "fingerprint"} {
		if !strings.Contains(got.PushWarning, want) {
			t.Errorf("PushWarning %q — must carry %q", got.PushWarning, want)
		}
	}
	if strings.Contains(got.PushWarning, secretTokenMaterial()) {
		t.Error("PushWarning must never carry the matched token material")
	}
	if got.PushedSHA != head {
		t.Errorf("PushedSHA %q — want the pushed commit %q", got.PushedSHA, head)
	}
	if r := bareHead(t, bare, "work"); r != head {
		t.Errorf("bare remote work %q — shadow mode must not refuse the push (want %q)", r, head)
	}
	if len(*alerts) != 1 {
		t.Fatalf("exactly one warn alert expected in shadow mode, got %d: %+v", len(*alerts), *alerts)
	}
	a := (*alerts)[0]
	if a.level != "warn" {
		t.Errorf("alert level %q — want warn", a.level)
	}
	if !strings.Contains(a.title, sess.ID) || !strings.Contains(a.title, "1") {
		t.Errorf("alert title %q — must carry the session ID and the finding count", a.title)
	}
	if strings.Contains(a.body, secretTokenMaterial()) || strings.Contains(a.title, secretTokenMaterial()) {
		t.Error("the alert must never carry the matched token material")
	}
}

// Test 4b — pushUnpushedBeforeTeardown, enforce, on a side branch: the
// scan refuses the push before it runs — PushState=not_pushed,
// PushWarning starts "not pushed: secret scan (enforce mode)", the
// remote branch is untouched, the commits stay local, and exactly one
// warn alert fired.
func TestPushUnpushedBeforeTeardown_SecretScanEnforceRefusesPush(t *testing.T) {
	bare, dir, mgr, sess, alerts := pushSecretScanFixture(t)
	gitRunT(t, dir, "checkout", "-q", "-b", "work") // side branch: not the remote default
	addSecretCommit(t, dir, "add creds on work")
	head := localHead(t, dir)
	mgr.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: git.ScanEnforce} })

	rep := mgr.pushUnpushedBeforeTeardown(sess)
	if !rep.Attempted || rep.Pushed || rep.Reason == "" {
		t.Fatalf("report = %+v — enforce mode must refuse the push with a reason", rep)
	}

	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.PushState != "not_pushed" {
		t.Errorf("PushState %q — want \"not_pushed\" under enforce", got.PushState)
	}
	if !strings.HasPrefix(got.PushWarning, "not pushed: secret scan (enforce mode)") {
		t.Fatalf("PushWarning %q — want \"not pushed: secret scan (enforce mode)...\"", got.PushWarning)
	}
	for _, want := range []string{git.DetectorGitHubToken, "creds.txt:", "fingerprint"} {
		if !strings.Contains(got.PushWarning, want) {
			t.Errorf("PushWarning %q — must carry the redacted finding %q", got.PushWarning, want)
		}
	}
	if strings.Contains(got.PushWarning, secretTokenMaterial()) {
		t.Error("PushWarning must never carry the matched token material")
	}
	if got.PushedSHA != "" {
		t.Errorf("PushedSHA %q — nothing reached the remote", got.PushedSHA)
	}
	if r := bareHead(t, bare, "work"); r != "" {
		t.Errorf("bare remote work %q — enforce mode must leave the remote untouched", r)
	}
	if localHead(t, dir) != head {
		t.Error("the commits must stay committed locally")
	}
	if len(*alerts) != 1 {
		t.Fatalf("exactly one warn alert expected on a refused teardown push, got %d: %+v", len(*alerts), *alerts)
	}
	a := (*alerts)[0]
	if a.level != "warn" {
		t.Errorf("alert level %q — want warn", a.level)
	}
	if !strings.Contains(a.title, sess.ID) {
		t.Errorf("alert title %q — must carry the session ID", a.title)
	}
	if strings.Contains(a.title, secretTokenMaterial()) || strings.Contains(a.body, secretTokenMaterial()) {
		t.Error("the alert must never carry the matched token material")
	}
}

// Test 4c — pushUnpushedBeforeTeardown, shadow, clean history (no
// findings): the push proceeds and records NO warning at all — no
// shadow-mode summary, no alert.
func TestPushUnpushedBeforeTeardown_SecretScanCleanPushRecordsNoWarning(t *testing.T) {
	bare, dir, mgr, sess, alerts := pushSecretScanFixture(t)
	gitRunT(t, dir, "checkout", "-q", "-b", "work") // side branch: not the remote default
	if err := os.WriteFile(filepath.Join(dir, "f1.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "benign work")
	head := localHead(t, dir)
	mgr.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: git.ScanShadow} })

	rep := mgr.pushUnpushedBeforeTeardown(sess)
	if !rep.Attempted || !rep.Pushed || rep.Reason != "" {
		t.Fatalf("report = %+v — a clean history must push", rep)
	}

	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session disappeared")
	}
	if got.PushState != "pushed" {
		t.Errorf("PushState %q — want \"pushed\"", got.PushState)
	}
	if got.PushWarning != "" {
		t.Errorf("PushWarning %q — a clean scan records no warning", got.PushWarning)
	}
	if got.PushedSHA != head {
		t.Errorf("PushedSHA %q — want the pushed commit %q", got.PushedSHA, head)
	}
	if r := bareHead(t, bare, "work"); r != head {
		t.Errorf("bare remote work %q — want the pushed commit %q", r, head)
	}
	if len(*alerts) != 0 {
		t.Errorf("no alert expected on a clean shadow scan, got: %+v", *alerts)
	}
}

// --- Audit emission tests (operator audit log, git.secret_scan) ---------

// secretScanAuditCall is one recorded SetSecretScanAuditFn invocation.
type secretScanAuditCall struct {
	action   string
	subject  string
	remote   string
	ref      string
	findings []map[string]any
}

// recordingAudit returns a SetSecretScanAuditFn reporter plus the slice
// of calls it recorded.
func recordingAudit() (fn audit.SecretScanReporter, out *[]secretScanAuditCall) {
	out = &[]secretScanAuditCall{}
	fn = func(action, subject, remote, ref string, findings []map[string]any) {
		*out = append(*out, secretScanAuditCall{action: action, subject: subject, remote: remote, ref: ref, findings: findings})
	}
	return fn, out
}

// assertRedactedAuditFindings proves the audit-emission redaction
// contract: every emitted finding carries ONLY the four allowed fields
// (detector, path, line, fingerprint), and no emission anywhere carries
// the matched token material.
func assertRedactedAuditFindings(t *testing.T, calls *[]secretScanAuditCall) {
	t.Helper()
	if len(*calls) == 0 {
		t.Fatal("no audit emissions recorded")
	}
	for i, c := range *calls {
		if len(c.findings) == 0 {
			t.Errorf("emission %d carries no findings", i)
		}
		for j, f := range c.findings {
			for _, k := range []string{"detector", "path", "line", "fingerprint"} {
				if _, ok := f[k]; !ok {
					t.Errorf("emission %d finding %d — missing redacted field %q: %v", i, j, k, f)
				}
			}
			for k := range f {
				if k != "detector" && k != "path" && k != "line" && k != "fingerprint" {
					t.Errorf("emission %d finding %d — carries a non-redacted field %q: %v", i, j, k, f)
				}
			}
		}
	}
	if dump := fmt.Sprintf("%+v", *calls); strings.Contains(dump, secretTokenMaterial()) {
		t.Error("the audit emission must never carry the matched token material")
	}
}

// Test 5a — recordShadowSecretFindings with an audit reporter wired:
// EXACTLY ONE secret_scan_shadow emission for the shadow report, named
// after the session, carrying the push target and the redacted findings
// only.
func TestRecordShadowSecretScanFindings_SecretScanAuditEmitsOnceShadow(t *testing.T) {
	findings := []git.SecretFinding{
		{Detector: git.DetectorGitHubToken, Commit: strings.Repeat("c", 40), Path: "creds.txt", Line: 2, Fingerprint: "0123456789abcdef"},
		{Detector: git.DetectorSlackToken, Commit: strings.Repeat("c", 40), Path: "deploy/.env", Line: 7, Fingerprint: "fedcba9876543210"},
	}
	fn, alerts := recordingAlerts()
	fnAudit, calls := recordingAudit()
	m := &Manager{}
	m.SetPushAlertFn(fn)
	m.SetSecretScanAuditFn(fnAudit)
	sess := &Session{ID: "abcd", CreatedAt: fixedTestTime, UpdatedAt: fixedTestTime}

	m.recordShadowSecretFindings(sess, "origin", "main", findings)

	if len(*calls) != 1 {
		t.Fatalf("exactly ONE audit emission expected for one shadow report, got %d: %+v", len(*calls), *calls)
	}
	c := (*calls)[0]
	if c.action != audit.ActionSecretScanShadow {
		t.Errorf("audit action %q — want %q", c.action, audit.ActionSecretScanShadow)
	}
	if c.subject != "abcd" {
		t.Errorf("audit subject %q — want the session ID", c.subject)
	}
	if c.remote != "origin" || c.ref != "main" {
		t.Errorf("audit target = %s/%s — want origin/main", c.remote, c.ref)
	}
	if len(c.findings) != len(findings) {
		t.Fatalf("audit findings = %d entries — want %d", len(c.findings), len(findings))
	}
	for i, want := range findings {
		got := c.findings[i]
		if got["detector"] != want.Detector || got["path"] != want.Path || got["line"] != want.Line || got["fingerprint"] != want.Fingerprint {
			t.Errorf("audit finding[%d] = %v — want the redacted %+v", i, got, want)
		}
	}
	assertRedactedAuditFindings(t, calls)
	// The alert behaviour is unchanged by the audit wiring.
	if len(*alerts) != 1 {
		t.Errorf("exactly one alert expected, got %d", len(*alerts))
	}
}

// Test 5b — recordShadowSecretFindings with NO audit reporter wired:
// nothing changes — no panic, no emission, the recording and alert
// contract hold exactly as before.
func TestRecordShadowSecretScanFindings_SecretScanAuditNilFnChangesNothing(t *testing.T) {
	findings := []git.SecretFinding{
		{Detector: git.DetectorGitHubToken, Commit: strings.Repeat("9", 40), Path: "creds.txt", Line: 1, Fingerprint: "0123456789abcdef"},
	}
	fn, alerts := recordingAlerts()
	m := &Manager{} // no SetSecretScanAuditFn call at all
	m.SetPushAlertFn(fn)
	sess := &Session{ID: "abcd"}

	m.recordShadowSecretFindings(sess, "origin", "main", findings)

	if !strings.HasPrefix(sess.PushWarning, "pushed; pre-push secret scan (shadow mode) reported: ") {
		t.Fatalf("PushWarning %q — recording must hold even with no audit reporter", sess.PushWarning)
	}
	if len(*alerts) != 1 {
		t.Errorf("exactly one alert expected with a nil audit fn, got %d", len(*alerts))
	}
}

// Test 6a — pushAfterCommit, shadow, end to end: EXACTLY ONE
// secret_scan_shadow audit emission (one per shadow report), with the
// pushed target and the redacted findings; the push outcome, session
// state and alert are unchanged by the wiring.
func TestPushAfterCommit_SecretScanAuditEmitsOncePerShadowReport(t *testing.T) {
	bare, dir, mgr, sess, alerts := pushSecretScanFixture(t)
	addSecretCommit(t, dir, "add creds")
	head := localHead(t, dir)
	fnAudit, calls := recordingAudit()
	mgr.SetSecretScanAuditFn(fnAudit)
	mgr.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: git.ScanShadow} })

	mgr.pushAfterCommit(sess, head)

	if r := bareHead(t, bare, "main"); r != head {
		t.Errorf("bare remote main %q — shadow mode must push (want %q)", r, head)
	}
	if len(*calls) != 1 {
		t.Fatalf("exactly ONE audit emission expected per shadow report, got %d: %+v", len(*calls), *calls)
	}
	c := (*calls)[0]
	if c.action != audit.ActionSecretScanShadow {
		t.Errorf("audit action %q — want %q", c.action, audit.ActionSecretScanShadow)
	}
	if c.subject != sess.ID {
		t.Errorf("audit subject %q — want the session ID %q", c.subject, sess.ID)
	}
	if c.remote != "origin" {
		t.Errorf("audit remote %q — want the push target %q", c.remote, "origin")
	}
	if !strings.Contains(c.ref, "main") {
		t.Errorf("audit ref %q — must name the pushed branch", c.ref)
	}
	for _, f := range c.findings {
		if f["path"] == "" || f["detector"] == "" || f["line"] == 0 || f["fingerprint"] == "" {
			t.Errorf("audit finding %v — must carry detector, path, line, fingerprint", f)
		}
	}
	assertRedactedAuditFindings(t, calls)
	if len(*alerts) != 1 {
		t.Errorf("exactly one alert expected, got %d: %+v", len(*alerts), *alerts)
	}
}

// Test 6b — pushAfterCommit, enforce, end to end: EXACTLY ONE
// secret_scan_refused audit emission (one per refusal), with the
// refused target and the redacted findings; the remote stays untouched.
func TestPushAfterCommit_SecretScanAuditEmitsOncePerRefusal(t *testing.T) {
	bare, dir, mgr, sess, alerts := pushSecretScanFixture(t)
	seed := bareHead(t, bare, "main")
	addSecretCommit(t, dir, "add creds")
	fnAudit, calls := recordingAudit()
	mgr.SetSecretScanAuditFn(fnAudit)
	mgr.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: git.ScanEnforce} })

	mgr.pushAfterCommit(sess, localHead(t, dir))

	if r := bareHead(t, bare, "main"); r != seed {
		t.Errorf("bare remote main %q — enforce mode must leave the remote untouched (want seed %q)", r, seed)
	}
	if len(*calls) != 1 {
		t.Fatalf("exactly ONE audit emission expected per enforce refusal, got %d: %+v", len(*calls), *calls)
	}
	c := (*calls)[0]
	if c.action != audit.ActionSecretScanRefused {
		t.Errorf("audit action %q — want %q", c.action, audit.ActionSecretScanRefused)
	}
	if c.subject != sess.ID {
		t.Errorf("audit subject %q — want the session ID %q", c.subject, sess.ID)
	}
	if c.remote != "origin" {
		t.Errorf("audit remote %q — want the refused target %q", c.remote, "origin")
	}
	if len(c.findings) == 0 {
		t.Error("the refusal emission must carry the redacted findings")
	}
	assertRedactedAuditFindings(t, calls)
	if len(*alerts) != 1 {
		t.Errorf("exactly one alert expected on a refused push, got %d: %+v", len(*alerts), *alerts)
	}
}

// Test 6c — pushAfterCommit, shadow, clean history: a clean scan
// produces NO audit emission at all (the sink sees nothing), and no
// alert.
func TestPushAfterCommit_SecretScanCleanPushEmitsNoAuditEntry(t *testing.T) {
	bare, dir, mgr, sess, alerts := pushSecretScanFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "f1.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "benign work")
	head := localHead(t, dir)
	fnAudit, calls := recordingAudit()
	mgr.SetSecretScanAuditFn(fnAudit)
	mgr.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: git.ScanShadow} })

	mgr.pushAfterCommit(sess, head)

	if r := bareHead(t, bare, "main"); r != head {
		t.Errorf("bare remote main %q — want the pushed commit %q", r, head)
	}
	if len(*calls) != 0 {
		t.Errorf("a clean push must emit no audit entry, got: %+v", *calls)
	}
	if len(*alerts) != 0 {
		t.Errorf("a clean push must raise no alert, got: %+v", *alerts)
	}
}

// Test 7 — pushUnpushedBeforeTeardown, enforce, end to end: EXACTLY ONE
// secret_scan_refused audit emission for the refused teardown push, and
// the session record is unchanged by the wiring.
func TestPushUnpushedBeforeTeardown_SecretScanAuditEmitsOncePerRefusal(t *testing.T) {
	bare, dir, mgr, sess, alerts := pushSecretScanFixture(t)
	gitRunT(t, dir, "checkout", "-q", "-b", "work") // side branch: not the remote default
	addSecretCommit(t, dir, "add creds on work")
	fnAudit, calls := recordingAudit()
	mgr.SetSecretScanAuditFn(fnAudit)
	mgr.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: git.ScanEnforce} })

	rep := mgr.pushUnpushedBeforeTeardown(sess)
	if !rep.Attempted || rep.Pushed || rep.Reason == "" {
		t.Fatalf("report = %+v — enforce mode must refuse the teardown push", rep)
	}
	if r := bareHead(t, bare, "work"); r != "" {
		t.Errorf("bare remote work %q — enforce mode must leave the remote untouched", r)
	}
	if len(*calls) != 1 {
		t.Fatalf("exactly ONE audit emission expected per refused teardown push, got %d: %+v", len(*calls), *calls)
	}
	c := (*calls)[0]
	if c.action != audit.ActionSecretScanRefused {
		t.Errorf("audit action %q — want %q", c.action, audit.ActionSecretScanRefused)
	}
	if c.subject != sess.ID {
		t.Errorf("audit subject %q — want the session ID %q", c.subject, sess.ID)
	}
	if c.remote != "origin" {
		t.Errorf("audit remote %q — want the refused target %q", c.remote, "origin")
	}
	if len(c.findings) == 0 {
		t.Error("the refusal emission must carry the redacted findings")
	}
	assertRedactedAuditFindings(t, calls)
	if len(*alerts) != 1 {
		t.Errorf("exactly one alert expected on a refused teardown push, got %d: %+v", len(*alerts), *alerts)
	}
}
