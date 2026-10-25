// PWA↔Go parity for the notify-events closed kind set.
//
// The PWA (web/app.js) exposes NOTIFY_EVENT_KINDS as the exhaustive set of
// selectable lifecycle events and NOTIFY_EVENT_DEFAULTS as the default
// subset (rendered with a 'default' marker). app.js is loaded unmodified
// in the browser suite (app-notify-events.test.js); this Go test extracts
// both arrays from the source text and checks them against the backend
// ground truth, so the PWA cannot drift from:
//   - valid kinds: every autonomous.PRDStatus / autonomous.TaskStatus
//     lifecycle kind ("prd."/"task." + status) plus the two
//     task session kinds (KindTaskSessionStarted / KindTaskSessionCleared);
//   - defaults: config.DefaultConfig().Autonomous.Notify.DefaultEvents
//     (the global notify baseline).
package server

import (
	"os"
	"regexp"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// jsStringArray pulls the quoted string list out of a top-level
// `const NAME = ['a', 'b'];` declaration in the given source.
func jsStringArray(t *testing.T, src, varName string) []string {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(varName) + `\s*=\s*\[([^\]]*)\]`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("could not locate %s = [...] in web/app.js", varName)
	}
	valRe := regexp.MustCompile(`'([^']*)'`)
	matches := valRe.FindAllStringSubmatch(m[1], -1)
	out := make([]string, 0, len(matches))
	for _, p := range matches {
		out = append(out, p[1])
	}
	return out
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, i := range items {
		s[i] = true
	}
	return s
}

func TestNotifyEventKinds_MatchPWA(t *testing.T) {
	b, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatalf("reading web/app.js: %v", err)
	}
	src := string(b)

	jsKinds := jsStringArray(t, src, "NOTIFY_EVENT_KINDS")
	jsDefaults := jsStringArray(t, src, "NOTIFY_EVENT_DEFAULTS")
	if len(jsKinds) == 0 || len(jsDefaults) == 0 {
		t.Fatalf("extraction failed: kinds=%v defaults=%v", jsKinds, jsDefaults)
	}

	// --- PWA kinds must be exactly the backend's valid lifecycle kinds ---
	valid := toSet([]string{
		autonomous.KindTaskSessionStarted,
		autonomous.KindTaskSessionCleared,
	})
	for _, st := range []autonomous.PRDStatus{
		autonomous.PRDDraft, autonomous.PRDPlanning, autonomous.PRDNeedsReview,
		autonomous.PRDApproved, autonomous.PRDRevisionsAsked, autonomous.PRDActive,
		autonomous.PRDRunning, autonomous.PRDCompleted, autonomous.PRDRejected,
		autonomous.PRDCancelled, autonomous.PRDArchived, autonomous.PRDBlocked,
		autonomous.PRDFailed, autonomous.PRDPaused,
	} {
		valid[autonomous.PRDStatusKind(st)] = true
	}
	for _, st := range []autonomous.TaskStatus{
		autonomous.TaskPending, autonomous.TaskQueued, autonomous.TaskInProgress,
		autonomous.TaskRunningTests, autonomous.TaskVerifying, autonomous.TaskCompleted,
		autonomous.TaskFailed, autonomous.TaskBlocked, autonomous.TaskCancelled,
		autonomous.TaskWaitingCapacity, autonomous.TaskAwaitingApproval,
	} {
		valid[autonomous.TaskStatusKind(st)] = true
	}

	pwaSet := toSet(jsKinds)
	// Each PWA kind must be a real backend kind (no invented kinds).
	for k := range pwaSet {
		if !valid[k] {
			t.Errorf("PWA kind %q is not a valid backend lifecycle kind", k)
		}
	}
	// Each backend selectable kind must be offered in the PWA (no omissions).
	if len(pwaSet) != len(valid) {
		t.Errorf("kind count mismatch: PWA has %d kinds (%v), backend valid set has %d (%v)",
			len(pwaSet), jsKinds, len(valid), setKeys(valid))
	}
	for k := range valid {
		if !pwaSet[k] {
			t.Errorf("backend kind %q is missing from PWA NOTIFY_EVENT_KINDS", k)
		}
	}

	// --- No duplicates inside the PWA list ---
	seen := toSet(nil)
	for _, k := range jsKinds {
		if seen[k] {
			t.Errorf("duplicate kind %q in PWA NOTIFY_EVENT_KINDS", k)
		}
		seen[k] = true
	}

	// --- Defaults: PWA NOTIFY_EVENT_DEFAULTS == config global baseline ---
	goDefaults := config.DefaultConfig().Autonomous.Notify.DefaultEvents
	if len(goDefaults) == 0 {
		t.Fatal("config default events unexpectedly empty")
	}
	goDefaultSet := toSet(goDefaults)
	pwaDefaultSet := toSet(jsDefaults)
	if len(pwaDefaultSet) != len(goDefaultSet) {
		t.Errorf("default count mismatch: PWA=%v config=%v", jsDefaults, goDefaults)
	}
	for _, d := range goDefaults {
		if !pwaDefaultSet[d] {
			t.Errorf("config default %q missing from PWA NOTIFY_EVENT_DEFAULTS", d)
		}
	}
	for _, d := range jsDefaults {
		if !goDefaultSet[d] {
			t.Errorf("PWA default %q missing from config default events", d)
		}
		// Every default must itself be a selectable kind.
		if !pwaSet[d] {
			t.Errorf("default %q is not in the PWA kind set", d)
		}
	}
}

func setKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
