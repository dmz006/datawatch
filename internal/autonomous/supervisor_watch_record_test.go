// Supervisor watch record — kind vocabulary, key and store tests.
package autonomous

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testSupervisorWatchFinding(kind SupervisorWatchKind, subject, text string, firstSeen time.Time) *SupervisorWatchFinding {
	return &SupervisorWatchFinding{
		Kind:        kind,
		Subject:     subject,
		Text:        text,
		FirstSeenAt: firstSeen,
	}
}

// TestSupervisorWatchRecordKinds pins the stable finding vocabulary:
// exactly the fourteen kinds, each with its stable string name.
func TestSupervisorWatchRecordKinds(t *testing.T) {
	want := map[SupervisorWatchKind]string{
		SupervisorWatchTmuxRestart:               "tmux_restart",
		SupervisorWatchDaemonInactive:            "daemon_inactive",
		SupervisorWatchModelNotResident:          "model_not_resident",
		SupervisorWatchTaskFailed:                "task_failed",
		SupervisorWatchTaskReviewNeeded:          "task_review_needed",
		SupervisorWatchAutomatonNotMerged:        "automaton_not_merged",
		SupervisorWatchIdleLaneEligibleWork:      "idle_lane_eligible_work",
		SupervisorWatchWorktreeNoGomod:           "worktree_no_gomod",
		SupervisorWatchWorktreeNotRegistered:     "worktree_not_registered",
		SupervisorWatchWorktreeSilent:            "worktree_silent",
		SupervisorWatchVerifyScriptNotExecutable: "verify_script_not_executable",
		SupervisorWatchMainCheckoutDirty:         "main_checkout_dirty",
		SupervisorWatchCIRed:                     "ci_red",
		SupervisorWatchCISTuck:                   "ci_stuck",
	}
	if len(SupervisorWatchKinds) != len(want) {
		t.Fatalf("len(SupervisorWatchKinds) = %d, want %d", len(SupervisorWatchKinds), len(want))
	}
	seen := map[SupervisorWatchKind]bool{}
	for _, k := range SupervisorWatchKinds {
		if seen[k] {
			t.Fatalf("duplicate kind %q in SupervisorWatchKinds", k)
		}
		seen[k] = true
		wantName, ok := want[k]
		if !ok {
			t.Fatalf("unexpected kind %q in the vocabulary", k)
		}
		if string(k) != wantName {
			t.Fatalf("kind string = %q, want %q", k, wantName)
		}
		if !ValidSupervisorWatchKind(k) {
			t.Fatalf("ValidSupervisorWatchKind(%q) = false, want true", k)
		}
	}
	if ValidSupervisorWatchKind("not_a_real_kind") {
		t.Fatal("ValidSupervisorWatchKind(unknown) = true, want false")
	}
	if ValidSupervisorWatchKind("") {
		t.Fatal("ValidSupervisorWatchKind(\"\") = true, want false")
	}
}

// TestSupervisorWatchRecordKeyStability checks that the composite key
// of a finding is stable across instances (a finding round-tripped
// through JSON keys exactly as the fresh one does) and that distinct
// kind+subject pairs never collide.
func TestSupervisorWatchRecordKeyStability(t *testing.T) {
	a := testSupervisorWatchFinding(SupervisorWatchTaskFailed, "auto-one", "", time.Now())
	if a.Key() != SupervisorWatchKey(a.Kind, a.Subject) {
		t.Fatalf("Key() = %q, want SupervisorWatchKey(%q, %q)", a.Key(), a.Kind, a.Subject)
	}
	if !strings.Contains(a.Key(), "\x1f") {
		t.Fatalf("key %q must carry the unit separator so the join is lossless", a.Key())
	}

	// A JSON round trip must key to exactly the same value.
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var a2 SupervisorWatchFinding
	if err := json.Unmarshal(b, &a2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if a2.Key() != a.Key() {
		t.Fatalf("key after round trip = %q, want %q", a2.Key(), a.Key())
	}

	// Distinct kinds and distinct subjects must never share a key.
	if SupervisorWatchKey(SupervisorWatchCIRed, "run-1") == SupervisorWatchKey(SupervisorWatchCISTuck, "run-1") {
		t.Fatal("two kinds with the same subject collide")
	}
	if SupervisorWatchKey(SupervisorWatchTaskFailed, "auto-one") == SupervisorWatchKey(SupervisorWatchTaskFailed, "auto-two") {
		t.Fatal("two subjects with the same kind collide")
	}
	if SupervisorWatchKey(SupervisorWatchTaskFailed, "a") == SupervisorWatchKey(SupervisorWatchTaskFailed, "") {
		t.Fatal("subject and the empty subject collide")
	}
}

// TestSupervisorWatchRecordUpsertKeepsFirstSeenAt checks the upsert
// refresh contract: a second observation of the same kind+subject
// updates Text and LastSeenAt but keeps FirstSeenAt and the earlier
// AlertedAt, and re-opens a resolved finding in place.
func TestSupervisorWatchRecordUpsertKeepsFirstSeenAt(t *testing.T) {
	store, err := NewSupervisorWatchStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSupervisorWatchStore: %v", err)
	}

	firstSeen := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	alerted := firstSeen.Add(30 * time.Minute)
	f1 := testSupervisorWatchFinding(SupervisorWatchTaskFailed, "auto-one", "first line", firstSeen)
	out1, err := store.Upsert(f1)
	if err != nil {
		t.Fatalf("Upsert (first): %v", err)
	}
	if !out1.FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("FirstSeenAt = %v, want %v", out1.FirstSeenAt, firstSeen)
	}
	if out1.AlertedAt != nil {
		t.Fatalf("AlertedAt = %v, want nil on first upsert", *out1.AlertedAt)
	}
	firstLastSeen := out1.LastSeenAt

	// Simulate a later poll: same condition observed again, with an
	// alert already raised by then.
	f2 := testSupervisorWatchFinding(SupervisorWatchTaskFailed, "auto-one", "second line", firstSeen.Add(2*time.Hour))
	f2.AlertedAt = &alerted
	out2, err := store.Upsert(f2)
	if err != nil {
		t.Fatalf("Upsert (second): %v", err)
	}
	if !out2.FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("refreshed FirstSeenAt = %v, want %v preserved", out2.FirstSeenAt, firstSeen)
	}
	if out2.Text != "second line" {
		t.Fatalf("refreshed Text = %q, want %q", out2.Text, "second line")
	}
	if out2.LastSeenAt.Before(firstLastSeen) {
		t.Fatalf("LastSeenAt = %v, want >= first LastSeenAt %v", out2.LastSeenAt, firstLastSeen)
	}
	if out2.AlertedAt == nil || !out2.AlertedAt.Equal(alerted) {
		t.Fatalf("refreshed AlertedAt = %v, want %v preserved", out2.AlertedAt, alerted)
	}
	if out2.Resolved {
		t.Fatal("refreshed finding must be open")
	}

	// Resolving then re-observing re-opens in place, still with the
	// original FirstSeenAt.
	res, err := store.Resolve(out1.Key())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.Resolved {
		t.Fatal("Resolved = false after Resolve")
	}
	out3, err := store.Upsert(testSupervisorWatchFinding(SupervisorWatchTaskFailed, "auto-one", "again", time.Time{}))
	if err != nil {
		t.Fatalf("Upsert (third): %v", err)
	}
	if out3.Resolved {
		t.Fatal("re-observed finding must be open after a fresh observation")
	}
	if !out3.FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("re-opened FirstSeenAt = %v, want %v preserved", out3.FirstSeenAt, firstSeen)
	}

	// The caller's input must not have been mutated by the store.
	if f2.FirstSeenAt.IsZero() || (f2.AlertedAt == nil) {
		t.Fatalf("Upsert mutated its input: %+v", f2)
	}

	// Validation: unknown kind and blank subject are refused.
	if _, err := store.Upsert(testSupervisorWatchFinding("not_a_real_kind", "auto-one", "", time.Time{})); err == nil {
		t.Fatal("Upsert(unknown kind) succeeded, want an error")
	}
	if _, err := store.Upsert(testSupervisorWatchFinding(SupervisorWatchTaskFailed, "  ", "", time.Time{})); err == nil {
		t.Fatal("Upsert(blank subject) succeeded, want an error")
	}
	if _, err := store.Upsert(nil); err == nil {
		t.Fatal("Upsert(nil) succeeded, want an error")
	}
}

// TestSupervisorWatchRecordResolve checks resolve semantics: the flag
// sticks, LastSeenAt moves, a missing key is refused, and Delete
// removes the finding.
func TestSupervisorWatchRecordResolve(t *testing.T) {
	store, err := NewSupervisorWatchStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSupervisorWatchStore: %v", err)
	}
	f := testSupervisorWatchFinding(SupervisorWatchMainCheckoutDirty, "main-checkout", "dirty", time.Time{})
	out, err := store.Upsert(f)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if out.Resolved {
		t.Fatal("fresh finding must be open")
	}

	res, err := store.Resolve(out.Key())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.Resolved {
		t.Fatal("Resolved = false, want true")
	}
	if res.LastSeenAt.Before(out.LastSeenAt) {
		t.Fatalf("resolved LastSeenAt = %v, want >= %v", res.LastSeenAt, out.LastSeenAt)
	}
	if got, ok := store.Get(out.Key()); !ok || !got.Resolved {
		t.Fatalf("Get after Resolve: ok=%v resolved=%v, want ok and resolved", ok, got)
	}

	if _, err := store.Resolve(SupervisorWatchKey(SupervisorWatchCIRed, "missing")); err == nil {
		t.Fatal("Resolve(missing) succeeded, want an error")
	}
	if err := store.Delete(out.Key()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := store.Get(out.Key()); ok {
		t.Fatal("Get after Delete: found, want gone")
	}
	if err := store.Delete(out.Key()); err == nil {
		t.Fatal("Delete(missing) succeeded, want an error")
	}
}

// TestSupervisorWatchRecordPersistenceRoundTrip checks that a finding
// store reloaded from disk carries the same findings, FirstSeenAt,
// AlertedAt and Resolved state as the in-memory store left it.
func TestSupervisorWatchRecordPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSupervisorWatchStore(dir)
	if err != nil {
		t.Fatalf("NewSupervisorWatchStore: %v", err)
	}

	firstSeen := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	alerted := firstSeen.Add(15 * time.Minute)
	a := testSupervisorWatchFinding(SupervisorWatchWorktreeSilent, "auto-silent", "silent", firstSeen)
	a.AlertedAt = &alerted
	if _, err := store.Upsert(a); err != nil {
		t.Fatalf("Upsert(a): %v", err)
	}
	b := testSupervisorWatchFinding(SupervisorWatchCIRed, "run-42", "red", firstSeen)
	if _, err := store.Upsert(b); err != nil {
		t.Fatalf("Upsert(b): %v", err)
	}
	if _, err := store.Resolve(b.Key()); err != nil {
		t.Fatalf("Resolve(b): %v", err)
	}

	// The file must live under the data dir with the stable name.
	if fi, err := os.Stat(filepath.Join(dir, SupervisorWatchFileName)); err != nil {
		t.Fatalf("supervisor_watch.jsonl missing under the data dir: %v", err)
	} else if fi.Size() == 0 {
		t.Fatal("supervisor_watch.jsonl is empty after two upserts and a resolve")
	}

	// Reload and compare.
	reloaded, err := NewSupervisorWatchStore(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	gotA, ok := reloaded.Get(a.Key())
	if !ok {
		t.Fatal("reloaded store lost finding a")
	}
	if !gotA.FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("reloaded FirstSeenAt = %v, want %v", gotA.FirstSeenAt, firstSeen)
	}
	if gotA.AlertedAt == nil || !gotA.AlertedAt.Equal(alerted) {
		t.Fatalf("reloaded AlertedAt = %v, want %v", gotA.AlertedAt, alerted)
	}
	if gotA.Resolved {
		t.Fatal("reloaded a must stay open")
	}
	gotB, ok := reloaded.Get(b.Key())
	if !ok {
		t.Fatal("reloaded store lost finding b")
	}
	if !gotB.Resolved {
		t.Fatal("reloaded b must stay resolved")
	}
	list := reloaded.List()
	if len(list) != 2 {
		t.Fatalf("List after reload = %d findings, want 2", len(list))
	}
	// Open findings must come before resolved ones in List.
	if list[0].Key() != a.Key() {
		t.Fatalf("List order: first = %q, want the open finding %q", list[0].Key(), a.Key())
	}
}

// TestSupervisorWatchRecordMissingFileIsEmpty checks the empty-store
// contract: a missing file reads as an empty store, not an error.
func TestSupervisorWatchRecordMissingFileIsEmpty(t *testing.T) {
	store, err := NewSupervisorWatchStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSupervisorWatchStore on a fresh dir: %v", err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("List on a fresh store = %d findings, want 0", len(got))
	}
}
