package acceptance

import (
	"context"
	"strings"
	"testing"
)

func TestNoAskTextVanishes(t *testing.T) {
	for ask, want := range map[string]string{
		"# Fix the export crash":                       "Fix the export crash",
		"## Fix the export crash\nand log it":          "Fix the export crash|and log it",
		"#1234 export still crashes, fix it":           "#1234 export still crashes, fix it",
		"Fix the export crash:":                        "Fix the export crash:",
		"Please do these:\n- keep logins working:":     "Please do these:|keep logins working:",
		"Set the limit to > 5.\nSet the limit to < 5.": "Set the limit to > 5|Set the limit to < 5",
		"Raise it by 5.\nRaise it by 5%.":              "Raise it by 5|Raise it by 5%",
		"Move it by 5.\nMove it by -5.":                "Move it by 5|Move it by -5",
		"Use x == 1.\nUse x != 1.":                     "Use x == 1|Use x != 1",
		"Keep it.  keep   IT!":                         "Keep it",
		"---\n===\n#\nDo it":                           "Do it",
	} {
		if got := strings.Join(AskSentences(ask), "|"); got != want {
			t.Errorf("%q -> %q, want %q", ask, got, want)
		}
	}
	// Quoting one of two opposite sentences does not cover the other.
	if covers("Set the limit to > 5", "Set the limit to < 5") {
		t.Error("\"> 5\" covers \"< 5\"")
	}
	if !covers("Please do these: keep logins working", "Please do these:") {
		t.Error("quoting the words without their punctuation does not cover them")
	}
}

func TestALineStartingWithAHashStillNeedsItsItem(t *testing.T) {
	w := newWorld(t)
	const ask = "#1234 export still crashes, fix it\nAlso add a note"
	w.frozen("r", "ticket", ask, Item{ID: "note", Statement: "a note exists", From: "Also add a note", Method: MethodConfirm})
	w.cover("r")
	l := must[*List](t)(w.svc.Confirm("r", "note", true, "yes", "", "", operator))
	if got := Read(l); got.Done || read(l, askItemID("#1234 export still crashes, fix it")).ID == "" {
		t.Fatalf("%s done=%v %+v", got.Status, got.Done, got.Items)
	}
}

func TestAProofLineDoesNotStopInsideALongerName(t *testing.T) {
	for _, c := range []struct {
		out, marker string
		want        bool
	}{{"aaa", "aa", false}, {"aa done", "aa", true}, {"--- PASS: TestAll", "--- PASS: TestA", false}, {"--- PASS: TestA/sub", "--- PASS: TestA", false}, {"  --- PASS: TestA (0.0s)", "--- PASS: TestA", true}} {
		if got := lineShows(c.out, c.marker); got != c.want {
			t.Errorf("%q shows %q = %v", c.out, c.marker, got)
		}
	}
}

func TestASkippedOrFailedLineIsNotAPass(t *testing.T) {
	const pass = "ok 1 - export works"
	check := Item{Check: &CheckSpec{Expect: []Expect{{Pass: pass}}}}
	for out, met := range map[string]bool{
		pass:                            true,
		"TAP version 13\n    " + pass:   true,
		pass + " # time=2ms":            true,
		pass + " # SKIP no database":    false,
		pass + " # skip":                false,
		pass + "# SKIP":                 false,
		pass + " (skipped)":             false,
		pass + " # TODO":                false,
		pass + " SKIPPED":               false,
		"not " + pass + " # TODO":       false,
		"not " + pass:                   false,
		"would run: " + pass:            false,
		"# " + pass:                     false,
		pass + " # SKIP\nwould " + pass: false,
	} {
		r := judgeRun(check, &ItemState{}, RunOutcome{Finished: true, Output: out})
		if (r.Value == ResultMet) != met {
			t.Errorf("%q -> %s (%s)", out, r.Value, r.Reason)
		}
	}
}

// A line that only mentions the failure is not the failing start run a change
// item needs.
func TestAMentionedFailureIsNoBaseline(t *testing.T) {
	w := newWorld(t)
	w.runner.out["c0"] = RunOutcome{Finished: true, ExitCode: 1, Output: "would report: " + failLine}
	w.frozen("r", "bug", oneAsk, fix())
	wantNotMet(t, w.work("r", "c1"), "fix", "did not show")
}

// A passing start run is still seen as already passing when its lines are not
// at the start of a line.
func TestAnAlreadyPassingStartRunIsStillSeen(t *testing.T) {
	w := newWorld(t)
	w.runner.out["c0"] = RunOutcome{Finished: true, Output: "[unit] " + passLine}
	w.frozen("r", "bug", oneAsk, fix())
	l := w.work("r", "c1")
	if got := read(l, "fix").Result; got.Value == ResultMet || !got.AlreadyTrue {
		t.Fatalf("%+v", got)
	}
}

func TestReplacingTheUnitDropsAcceptances(t *testing.T) {
	w := newWorld(t)
	w.frozen("r", "bug", oneAsk, fix())
	w.cover("r")
	must[*List](t)(w.svc.Accept("r", "fix", "later", "ok", "", operator))
	l := must[*List](t)(w.svc.Evaluate(context.Background(), "r"))
	wantStatus(t, l, StatusDoneAccepted)
	l = must[*List](t)(w.svc.NoteUnitReplaced("r", "reset", "", operator))
	if got := Read(l); got.Done || read(l, "fix").Accepted || l.State["fix"].Accept != nil {
		t.Fatalf("%s done=%v %+v", got.Status, got.Done, got.Items)
	}
	if got := Read(must[*List](t)(w.svc.Evaluate(context.Background(), "r"))); got.Done {
		t.Fatalf("a run brought the acceptance back: %s", got.Status)
	}
}
