// Pre-push secret scan wiring tests: effectiveSecretScan's live-config
// contract (unwired = off, empty mode = off, shadow/enforce pass through,
// the policy function is read on every push attempt) and
// recordPushOutcome's recording contract for scan findings (shadow
// Decision + single warn alert, enforce refusal path, clean push).
//
// A fake secret-shaped value that these tests need is assembled from
// fragments at runtime so no contiguous secret literal exists in this
// file; the git.SecretFinding type itself never holds a secret, and the
// assertions only prove the redacted summary (detector, path:line,
// fingerprint) is what lands on the PRD and in alerts.
package autonomous

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/git"
)

// newSecretScanTestPRD builds a Manager (see
// newGitPushTestManager in git_push_state_test.go) and a fresh PRD.
func newSecretScanTestPRD(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	m := newGitPushTestManager(t)
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	return m, prd
}

// secretScanFindings returns two fake findings and a secret-shaped
// string (assembled at runtime, fragments only in the source) that the
// redacted summary must never contain.
func secretScanFindings() (findings []git.SecretFinding, secret string) {
	secret = "gh" + "p_" + "fak" + "eScanValue9x"
	return []git.SecretFinding{
		{Commit: "aaa111", Detector: "github_token", Path: "env", Line: 1, Fingerprint: "0123456789abcdef"},
		{Commit: "aaa111", Detector: "aws_key", Path: "config.yaml", Line: 12, Fingerprint: "fedcba9876543210"},
	}, secret
}

// countDecisionRows counts Decision rows of one Kind, so a duplicate
// row appended by a regression is caught even if the last row is right.
func countDecisionRows(t *testing.T, prd *PRD, kind string) int {
	t.Helper()
	n := 0
	for _, d := range prd.Decisions {
		if d.Kind == kind {
			n++
		}
	}
	return n
}

// assertNoSecretInPRDOrAlerts proves the redaction contract: no
// persisted PRD field nor any alert line carries the fake secret.
func assertNoSecretInPRDOrAlerts(t *testing.T, prd *PRD, alerts [][]string, secret string) {
	t.Helper()
	dump, err := json.Marshal(prd)
	if err != nil {
		t.Fatalf("marshal prd: %v", err)
	}
	if strings.Contains(string(dump), secret) {
		t.Fatalf("persisted PRD carries the secret")
	}
	for _, a := range alerts {
		for _, s := range a {
			if s == "" {
				continue
			}
			if strings.Contains(s, secret) {
				t.Fatalf("alert carries the secret: %v", a)
			}
		}
	}
}

// sameSecretScanPolicy reports whether two policies hold identical
// values. SecretScanPolicy is not comparable (slice fields), so each
// field is compared individually.
func sameSecretScanPolicy(a, b git.SecretScanPolicy) bool {
	if a.Mode != b.Mode {
		return false
	}
	aeq := func(x, y []string) bool {
		if len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	return aeq(a.Detectors, b.Detectors) && aeq(a.PathPatterns, b.PathPatterns) && aeq(a.Fingerprints, b.Fingerprints)
}

// 1. effectiveSecretScan: unwired → off; empty mode → off; shadow and
// enforce pass through with detectors and allow-lists intact; the wired
// function is read on every call, so a runtime config change applies
// without a restart.
func TestEffectiveSecretScanPolicyResolution(t *testing.T) {
	// Unwired reads as off (legacy push behaviour).
	m := newGitPushTestManager(t)
	if p := m.effectiveSecretScan(); p.Mode != git.ScanOff {
		t.Errorf("unwired: got Mode %q, want %q", p.Mode, git.ScanOff)
	}

	// A wired function whose daemon side leaves Mode unset reads as off,
	// not as shadow.
	m.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{} })
	if p := m.effectiveSecretScan(); p.Mode != git.ScanOff {
		t.Errorf("empty mode: got %q, want %q", p.Mode, git.ScanOff)
	}

	// Shadow and enforce pass through with detectors and allow-lists.
	shadow := git.SecretScanPolicy{
		Mode:         git.ScanShadow,
		Detectors:    []string{"github_token", "aws_key"},
		PathPatterns: []string{"testdata/**"},
		Fingerprints: []string{"0123456789abcdef"},
	}
	m.SetSecretScanSettings(func() git.SecretScanPolicy { return shadow })
	if p := m.effectiveSecretScan(); !sameSecretScanPolicy(p, shadow) {
		t.Errorf("shadow pass-through: got %+v, want %+v", p, shadow)
	}
	enforce := git.SecretScanPolicy{
		Mode:         git.ScanEnforce,
		Detectors:    []string{"github_token"},
		Fingerprints: []string{"fedcba9876543210"},
	}
	m.SetSecretScanSettings(func() git.SecretScanPolicy { return enforce })
	if p := m.effectiveSecretScan(); !sameSecretScanPolicy(p, enforce) {
		t.Errorf("enforce pass-through: got %+v, want %+v", p, enforce)
	}

	// The closure is read on every call: changing what the fake returns
	// between two calls is seen immediately — a runtime config change
	// applies without a restart.
	cur := git.SecretScanPolicy{Mode: git.ScanShadow, Detectors: []string{"github_token"}}
	m.SetSecretScanSettings(func() git.SecretScanPolicy { return cur })
	if got := m.effectiveSecretScan().Mode; got != git.ScanShadow {
		t.Fatalf("first read: got %q, want %q", got, git.ScanShadow)
	}
	cur = git.SecretScanPolicy{
		Mode:         git.ScanEnforce,
		Detectors:    []string{"aws_key"},
		PathPatterns: []string{"env"},
	}
	got := m.effectiveSecretScan()
	if !sameSecretScanPolicy(got, cur) {
		t.Errorf("second read: got %+v, want %+v (closure must be read on every call)", got, cur)
	}
}

// 2. recordPushOutcome with shadow-mode findings on a successful push:
// PushState=pushed, warning cleared, exactly one git_secret_scan_shadow
// Decision row (count asserted to catch a duplicate), one warn alert
// naming Automaton and the finding count, both carrying the redacted
// summary and nothing secret-shaped.
//
// git.SecretFinding structurally cannot hold the matched secret (its
// fields are detector, commit, path, line, fingerprint — see internal/git
// secretscan.go), so the redaction contract asserted here is the one the
// wiring layer actually owns: the alert and the Decision record exactly
// the redacted summary, and no unrendered finding field leaks into them.
// Commit is the one field RedactedFindingsSummary does not render, so a
// sentinel planted there turns the render guard from a vacuous string
// check into an assertion that can fail if production ever switches to a
// raw finding dump (String() or %+v) in the alert or the PRD.
func TestRecordPushOutcomeSecretScanShadowFindings(t *testing.T) {
	findings, secret := secretScanFindings()
	raw := "raw" + "finding" + "field" + "7Q9x" // sentinel: assembled from fragments
	findings[0].Commit = raw
	summary := git.RedactedFindingsSummary(findings)
	if summary == "" {
		t.Fatal("RedactedFindingsSummary returned empty for two findings")
	}

	m, prd := newSecretScanTestPRD(t)
	var alerts [][]string
	m.SetAlertSink(func(level, title, body string) string {
		alerts = append(alerts, []string{level, title, body})
		return ""
	})

	before := len(prd.Decisions)
	m.recordPushOutcome(prd, git.PushResult{
		Pushed:         true,
		Remote:         "origin",
		Ref:            "refs/heads/prd/p1",
		SHA:            "abc123",
		SecretFindings: findings,
	}, git.PushBranch)

	// Push still records success; the warning is clear (the shadow path
	// does not set one).
	if prd.Git.PushState != PushStatePushed || prd.Git.PushedSHA != "abc123" || prd.Git.PushedAt == nil {
		t.Fatalf("shadow push must still record success: %+v", prd.Git)
	}
	if prd.Git.PushWarning != "" {
		t.Errorf("PushWarning = %q, want cleared", prd.Git.PushWarning)
	}

	// Exactly one shadow Decision row (counted across all rows — a
	// duplicate row is caught even if the last row is correct) plus the
	// git_pushed row.
	if got := len(prd.Decisions); got != before+2 {
		t.Fatalf("want %d decisions (+git_pushed, +shadow), got %d: %+v", before+2, got, prd.Decisions)
	}
	if got := countDecisionRows(t, prd, "git_secret_scan_shadow"); got != 1 {
		t.Errorf("git_secret_scan_shadow rows = %d, want exactly 1", got)
	}
	if got := countDecisionRows(t, prd, "git_pushed"); got != 1 {
		t.Errorf("git_pushed rows = %d, want exactly 1 (after one push)", got)
	}
	if note := prd.Decisions[before+1]; note.Kind != "git_secret_scan_shadow" || note.Note != summary {
		t.Errorf("shadow decision = %+v, want Kind=git_secret_scan_shadow Note=%q", note, summary)
	}

	// Exactly one warn alert: title names Automaton and the finding
	// count; body is exactly the redacted summary.
	if len(alerts) != 1 {
		t.Fatalf("want exactly 1 alert, got %d: %v", len(alerts), alerts)
	}
	lvl, title, body := alerts[0][0], alerts[0][1], alerts[0][2]
	if lvl != "warn" {
		t.Errorf("alert level = %q, want warn", lvl)
	}
	if !strings.Contains(title, "Automaton") || !strings.Contains(title, prd.ID) || !strings.Contains(title, "2") {
		t.Errorf("alert title = %q, want to mention Automaton, the PRD id and the finding count", title)
	}
	if body != summary {
		t.Errorf("alert body = %q, want the redacted summary %q", body, summary)
	}

	// The fake secret never appears on the PRD or in the alert.
	assertNoSecretInPRDOrAlerts(t, prd, alerts, secret)

	// Neither the sentinel from the unrendered field nor a raw dump of
	// the findings' other fields may leak into the warning, the decision
	// rows or the alert (a String()/struct-dump regression is caught here
	// even though the summary-equality checks above already pin the exact
	// redacted text).
	for _, s := range []string{title, body, prd.Git.PushWarning} {
		if strings.Contains(s, raw) {
			t.Errorf("shadow render leaked an unrendered finding field: %q", s)
		}
	}
	for _, d := range prd.Decisions {
		if strings.Contains(d.Note, raw) {
			t.Errorf("shadow decision row leaked an unrendered finding field: %q", d.Note)
		}
	}

	// Persisted, not just in memory.
	got, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD: not found after save")
	}
	if got.Git.PushState != PushStatePushed || countDecisionRows(t, got, "git_secret_scan_shadow") != 1 {
		t.Errorf("persisted state = PushState %q, shadow rows %d", got.Git.PushState, countDecisionRows(t, got, "git_secret_scan_shadow"))
	}
}

// 3. recordPushOutcome with an enforce refusal: the failure path —
// not_pushed state, PushWarning carrying the redacted reason, one warn
// alert "Automaton <id> not pushed", and no shadow Decision row.
func TestRecordPushOutcomeSecretScanEnforceRefusal(t *testing.T) {
	findings, secret := secretScanFindings()
	summary := git.RedactedFindingsSummary(findings)
	reason := "secret scan (enforce mode): " + summary + " refuses the push to origin/refs/heads/prd/p1"

	m, prd := newSecretScanTestPRD(t)
	var alerts [][]string
	m.SetAlertSink(func(level, title, body string) string {
		alerts = append(alerts, []string{level, title, body})
		return ""
	})

	before := len(prd.Decisions)
	m.recordPushOutcome(prd, git.PushResult{
		Remote:         "origin",
		Ref:            "refs/heads/prd/p1",
		Kind:           git.PushFailSecret,
		Reason:         reason,
		SecretFindings: findings,
	}, git.PushBranch)

	if prd.Git.PushState != PushStateNotPushed {
		t.Errorf("PushState = %q, want not_pushed", prd.Git.PushState)
	}
	wantWarn := "not pushed: " + reason
	if prd.Git.PushWarning != wantWarn {
		t.Errorf("PushWarning = %q, want %q", prd.Git.PushWarning, wantWarn)
	}
	// Failure path: one git_not_pushed row and no shadow row.
	if got := len(prd.Decisions); got != before+1 {
		t.Fatalf("want %d decisions (+git_not_pushed only), got %d: %+v", before+1, got, prd.Decisions)
	}
	if got := countDecisionRows(t, prd, "git_secret_scan_shadow"); got != 0 {
		t.Errorf("enforce refusal must not append a shadow row, got %d", got)
	}
	if last := prd.Decisions[len(prd.Decisions)-1]; last.Kind != "git_not_pushed" || last.Note != reason {
		t.Errorf("failure decision = %+v", last)
	}
	// Exactly one warn alert with the failure contract.
	if len(alerts) != 1 {
		t.Fatalf("want exactly 1 alert, got %d: %v", len(alerts), alerts)
	}
	lvl, title, body := alerts[0][0], alerts[0][1], alerts[0][2]
	if lvl != "warn" || title != "Automaton "+prd.ID+" not pushed" || body != reason {
		t.Errorf("alert = (%q,%q,%q); want (warn, %q, %q)", lvl, title, body, "Automaton "+prd.ID+" not pushed", reason)
	}
	assertNoSecretInPRDOrAlerts(t, prd, alerts, secret)
}

// 4. A clean push (no findings) records no shadow Decision and no
// secret alert — just the single git_pushed row.
func TestRecordPushOutcomeSecretScanCleanPush(t *testing.T) {
	m, prd := newSecretScanTestPRD(t)
	var alerts [][]string
	m.SetAlertSink(func(level, title, body string) string {
		alerts = append(alerts, []string{level, title, body})
		return ""
	})

	before := len(prd.Decisions)
	m.recordPushOutcome(prd, git.PushResult{
		Pushed: true,
		Remote: "origin",
		Ref:    "refs/heads/prd/p1",
		SHA:    "abc123",
	}, git.PushBranch)

	if prd.Git.PushState != PushStatePushed {
		t.Fatalf("clean push must record success: %+v", prd.Git)
	}
	if got := len(prd.Decisions); got != before+1 {
		t.Fatalf("clean push records exactly the git_pushed row: %+v", prd.Decisions)
	}
	if got := countDecisionRows(t, prd, "git_secret_scan_shadow"); got != 0 {
		t.Errorf("clean push must record no shadow row, got %d", got)
	}
	if len(alerts) != 0 {
		t.Errorf("clean push must raise no alert, got %v", alerts)
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

// recordingSecretScanAudit returns a SetSecretScanAuditFn reporter plus
// the slice of calls it recorded.
func recordingSecretScanAudit() (fn audit.SecretScanReporter, out *[]secretScanAuditCall) {
	out = &[]secretScanAuditCall{}
	fn = func(action, subject, remote, ref string, findings []map[string]any) {
		*out = append(*out, secretScanAuditCall{action: action, subject: subject, remote: remote, ref: ref, findings: findings})
	}
	return fn, out
}

// assertRedactedSecretScanAudit proves the audit-emission redaction
// contract: every emitted finding carries ONLY the four allowed fields
// (detector, path, line, fingerprint), and no emission anywhere carries
// the fake secret.
func assertRedactedSecretScanAudit(t *testing.T, calls *[]secretScanAuditCall, secret string) {
	t.Helper()
	if len(*calls) == 0 {
		t.Fatal("no audit emissions recorded")
	}
	for i, c := range *calls {
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
	if dump, err := json.Marshal(*calls); err == nil && strings.Contains(string(dump), secret) {
		t.Error("the audit emission must never carry the fake secret")
	}
}

// 5. recordPushOutcome with shadow findings and an audit reporter wired:
// EXACTLY ONE secret_scan_shadow emission per shadow report, named after
// the Automaton, carrying the push target and the redacted findings
// only; the Decision, alert and PRD state are unchanged by the wiring.
func TestRecordPushOutcomeSecretScanAuditEmitsOncePerShadowReport(t *testing.T) {
	findings, secret := secretScanFindings()

	m, prd := newSecretScanTestPRD(t)
	var alerts [][]string
	m.SetAlertSink(func(level, title, body string) string {
		alerts = append(alerts, []string{level, title, body})
		return ""
	})
	fnAudit, calls := recordingSecretScanAudit()
	m.SetSecretScanAuditFn(fnAudit)

	m.recordPushOutcome(prd, git.PushResult{
		Pushed:         true,
		Remote:         "origin",
		Ref:            "refs/heads/prd/p1",
		SHA:            "abc123",
		SecretFindings: findings,
	}, git.PushBranch)

	if len(*calls) != 1 {
		t.Fatalf("want exactly 1 audit emission per shadow report, got %d: %+v", len(*calls), *calls)
	}
	c := (*calls)[0]
	if c.action != audit.ActionSecretScanShadow {
		t.Errorf("audit action = %q, want %q", c.action, audit.ActionSecretScanShadow)
	}
	if c.subject != prd.ID {
		t.Errorf("audit subject = %q, want the Automaton ID %q", c.subject, prd.ID)
	}
	if c.remote != "origin" || c.ref != "refs/heads/prd/p1" {
		t.Errorf("audit target = %s/%s, want origin/refs/heads/prd/p1", c.remote, c.ref)
	}
	if len(c.findings) != len(findings) {
		t.Fatalf("audit findings = %d entries, want %d", len(c.findings), len(findings))
	}
	for i, want := range findings {
		got := c.findings[i]
		if got["detector"] != want.Detector || got["path"] != want.Path || got["line"] != want.Line || got["fingerprint"] != want.Fingerprint {
			t.Errorf("audit finding[%d] = %v, want the redacted %+v", i, got, want)
		}
	}
	assertRedactedSecretScanAudit(t, calls, secret)
	// The Decision + alert contract is unchanged by the audit wiring.
	if got := countDecisionRows(t, prd, "git_secret_scan_shadow"); got != 1 {
		t.Errorf("git_secret_scan_shadow rows = %d, want exactly 1", got)
	}
	if len(alerts) != 1 {
		t.Errorf("exactly 1 alert expected, got %d: %v", len(alerts), alerts)
	}
}

// 6. recordPushOutcome with an enforce refusal and an audit reporter
// wired: EXACTLY ONE secret_scan_refused emission per refusal, with the
// refused target and the redacted findings; the failure recording and
// alert are unchanged by the wiring.
func TestRecordPushOutcomeSecretScanAuditEmitsOncePerRefusal(t *testing.T) {
	findings, secret := secretScanFindings()
	reason := "secret scan (enforce mode): " + git.RedactedFindingsSummary(findings) + " refuses the push to origin/refs/heads/prd/p1"

	m, prd := newSecretScanTestPRD(t)
	var alerts [][]string
	m.SetAlertSink(func(level, title, body string) string {
		alerts = append(alerts, []string{level, title, body})
		return ""
	})
	fnAudit, calls := recordingSecretScanAudit()
	m.SetSecretScanAuditFn(fnAudit)

	m.recordPushOutcome(prd, git.PushResult{
		Remote:         "origin",
		Ref:            "refs/heads/prd/p1",
		Kind:           git.PushFailSecret,
		Reason:         reason,
		SecretFindings: findings,
	}, git.PushBranch)

	if len(*calls) != 1 {
		t.Fatalf("want exactly 1 audit emission per enforce refusal, got %d: %+v", len(*calls), *calls)
	}
	c := (*calls)[0]
	if c.action != audit.ActionSecretScanRefused {
		t.Errorf("audit action = %q, want %q", c.action, audit.ActionSecretScanRefused)
	}
	if c.subject != prd.ID {
		t.Errorf("audit subject = %q, want the Automaton ID %q", c.subject, prd.ID)
	}
	if c.remote != "origin" || c.ref != "refs/heads/prd/p1" {
		t.Errorf("audit target = %s/%s, want origin/refs/heads/prd/p1", c.remote, c.ref)
	}
	if len(c.findings) != len(findings) {
		t.Fatalf("audit findings = %d entries, want %d", len(c.findings), len(findings))
	}
	assertRedactedSecretScanAudit(t, calls, secret)
	// Failure recording and alert are unchanged by the audit wiring.
	if prd.Git.PushState != PushStateNotPushed {
		t.Errorf("PushState = %q, want not_pushed", prd.Git.PushState)
	}
	if len(alerts) != 1 {
		t.Errorf("exactly 1 alert expected on a refusal, got %d: %v", len(alerts), alerts)
	}
}

// 7. recordPushOutcome: a clean push (no findings) and a non-secret
// failure kind (auth) emit NO secret-scan audit entry at all — the sink
// is never called, whether or not it is wired.
func TestRecordPushOutcomeSecretScanAuditCleanPushAndOtherFailureEmitNothing(t *testing.T) {
	// Clean push: no emission.
	m, prd := newSecretScanTestPRD(t)
	fnAudit, calls := recordingSecretScanAudit()
	m.SetSecretScanAuditFn(fnAudit)
	m.recordPushOutcome(prd, git.PushResult{
		Pushed: true,
		Remote: "origin",
		Ref:    "refs/heads/prd/p1",
		SHA:    "abc123",
	}, git.PushBranch)
	if len(*calls) != 0 {
		t.Errorf("a clean push must emit no secret-scan audit entry, got: %+v", *calls)
	}

	// A non-secret failure kind (auth) must not emit either.
	m2, prd2 := newSecretScanTestPRD(t)
	fnAudit2, calls2 := recordingSecretScanAudit()
	m2.SetSecretScanAuditFn(fnAudit2)
	m2.recordPushOutcome(prd2, git.PushResult{
		Remote: "origin",
		Ref:    "refs/heads/prd/p1",
		Kind:   git.PushFailAuth,
		Reason: "auth failed",
	}, git.PushBranch)
	if len(*calls2) != 0 {
		t.Errorf("a non-secret failure must emit no secret-scan audit entry, got: %+v", *calls2)
	}
}

// 8. recordPushOutcome with shadow findings and NO audit reporter wired:
// nothing changes — no panic, no emission, and the shadow Decision +
// alert contract holds exactly as before.
func TestRecordPushOutcomeSecretScanAuditNilReporterChangesNothing(t *testing.T) {
	findings, secret := secretScanFindings()

	m, prd := newSecretScanTestPRD(t) // no SetSecretScanAuditFn call at all
	var alerts [][]string
	m.SetAlertSink(func(level, title, body string) string {
		alerts = append(alerts, []string{level, title, body})
		return ""
	})

	m.recordPushOutcome(prd, git.PushResult{
		Pushed:         true,
		Remote:         "origin",
		Ref:            "refs/heads/prd/p1",
		SHA:            "abc123",
		SecretFindings: findings,
	}, git.PushBranch)

	if prd.Git.PushState != PushStatePushed {
		t.Fatalf("shadow push must still record success: %+v", prd.Git)
	}
	if got := countDecisionRows(t, prd, "git_secret_scan_shadow"); got != 1 {
		t.Errorf("git_secret_scan_shadow rows = %d, want exactly 1 with a nil audit reporter", got)
	}
	if len(alerts) != 1 {
		t.Errorf("exactly 1 alert expected with a nil audit reporter, got %d: %v", len(alerts), alerts)
	}
	assertNoSecretInPRDOrAlerts(t, prd, alerts, secret)
}
