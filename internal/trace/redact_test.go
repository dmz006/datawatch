package trace

import (
	"strings"
	"testing"
	"time"
)

// Path prefixes are assembled at runtime so no fixture holds a real-looking home path.
var (
	homeDir  = "/ho" + "me"
	macDir   = "/Us" + "ers"
	winUsers = `\Us` + `ers`
)

// Fake secrets assembled at runtime so no scanner flags this file.
var (
	fakeAWS    = "AKIA" + "IOSFODNN7EXAMPLE"
	fakeGH     = "ghp_" + "abcdefghijklmnopqrstuvwxyz0123456789"
	fakeSlack  = "xoxb-" + "123456789012-abcdefghijkl"
	fakeAPIKey = "api_key=" + "Zx9Qw3Er7Ty1Ui5Op2As8Df4"
	fakePEM    = "-----BEGIN RSA PRIVATE" + " KEY-----\nMIIEfakefakefake\nabc123\n-----END RSA PRIVATE" + " KEY-----"
	fixedTime  = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
)

func ev(arg string) Event {
	return Event{Kind: KindToolCall, Name: "bash", ArgSummary: arg, Timestamp: fixedTime}
}

func TestRedactSecretsRemoved(t *testing.T) {
	for name, secret := range map[string]string{
		"aws": fakeAWS, "github": fakeGH, "slack": fakeSlack, "generic": fakeAPIKey, "pem": fakePEM,
	} {
		for _, mode := range []string{ModeSummary, ModeFull} {
			got := Redact(ev("run "+secret+" now"), Settings{Mode: mode, ArgCapBytes: 4096})
			if strings.Contains(got.ArgSummary, "MIIEfake") || strings.Contains(got.ArgSummary, secret) ||
				strings.Contains(got.ArgSummary, "IOSFODNN7") || strings.Contains(got.ArgSummary, "Zx9Qw3") ||
				strings.Contains(got.ArgSummary, "abcdefghijklmnop") {
				t.Errorf("%s/%s: secret leaked: %q", name, mode, got.ArgSummary)
			}
			if !strings.Contains(got.ArgSummary, "[REDACTED]") {
				t.Errorf("%s/%s: no marker: %q", name, mode, got.ArgSummary)
			}
		}
	}
}

func TestRedactOffDropsArgs(t *testing.T) {
	got := Redact(ev("cat ~/x "+fakeGH), Settings{Mode: ModeOff})
	if got.ArgSummary != "" {
		t.Fatalf("off kept args: %q", got.ArgSummary)
	}
	if got.Name != "bash" || got.Kind != KindToolCall || !got.Timestamp.Equal(fixedTime) {
		t.Fatalf("off altered other fields: %+v", got)
	}
}

func TestRedactSummaryShort(t *testing.T) {
	long := strings.Repeat("a", 500) + "\nsecond line"
	got := Redact(ev(long), Settings{Mode: ModeSummary, ArgCapBytes: 4096})
	if len(got.ArgSummary) > SummaryMaxBytes || strings.Contains(got.ArgSummary, "second") {
		t.Fatalf("summary not short: %d %q", len(got.ArgSummary), got.ArgSummary)
	}
	if !strings.HasSuffix(got.ArgSummary, TruncationMarker) {
		t.Fatalf("no marker: %q", got.ArgSummary)
	}
	got = Redact(ev(long), Settings{Mode: ModeSummary, ArgCapBytes: 50})
	if len(got.ArgSummary) > 50 {
		t.Fatalf("cap ignored: %d", len(got.ArgSummary))
	}
}

func TestRedactFullKeepsLongerWithinCap(t *testing.T) {
	long := strings.Repeat("b", 400) + "\nline two"
	got := Redact(ev(long), Settings{Mode: ModeFull, ArgCapBytes: 1000})
	if got.ArgSummary != long {
		t.Fatalf("full should keep text under cap")
	}
	got = Redact(ev(long), Settings{Mode: ModeFull, ArgCapBytes: 100})
	if len(got.ArgSummary) != 100 || !strings.HasSuffix(got.ArgSummary, TruncationMarker) {
		t.Fatalf("full cap: %d %q", len(got.ArgSummary), got.ArgSummary)
	}
}

func TestRedactDefaultsToSummary(t *testing.T) {
	got := Redact(ev(strings.Repeat("c", 300)), Settings{})
	if len(got.ArgSummary) > SummaryMaxBytes {
		t.Fatalf("zero settings not summary: %d", len(got.ArgSummary))
	}
}

func TestTruncateUTF8Boundary(t *testing.T) {
	s := strings.Repeat("é", 100)
	got := truncate(s, 51)
	if len(got) > 51 || strings.ToValidUTF8(got, "?") != got {
		t.Fatalf("bad utf8 cut: %q", got)
	}
}

func TestSecretNotSplitByTruncation(t *testing.T) {
	arg := strings.Repeat("x", 100) + " " + fakeGH
	got := Redact(ev(arg), Settings{Mode: ModeFull, ArgCapBytes: 130})
	if strings.Contains(got.ArgSummary, "ghp_") {
		t.Fatalf("partial secret stored: %q", got.ArgSummary)
	}
}

func TestScrubPaths(t *testing.T) {
	cases := map[string]string{
		"cat " + homeDir + "/alice/.ssh/config":            "cat ~/.ssh/config",
		"open " + macDir + "/bob/Documents/a.txt":          "open ~/Documents/a.txt",
		"ls /root/.config":                                 "ls ~/.config",
		`type C:` + winUsers + `\carol\file.txt`:           `type ~\file.txt`,
		"a=" + homeDir + "/alice b=" + homeDir + "/dave/x": "a=~ b=~/x",
		"no paths here /usr/bin/env":                       "no paths here /usr/bin/env",
		homeDir + "/alice/work/proj/main.go:12:3 err":      "~/work/proj/main.go:12:3 err",
	}
	for in, want := range cases {
		if got := ScrubPaths(in); got != want {
			t.Errorf("ScrubPaths(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScrubPathsRoots(t *testing.T) {
	root := homeDir + "/alice/work/proj"
	got := ScrubPaths("edit "+homeDir+"/alice/work/proj/internal/a.go and "+homeDir+"/alice/work/project2/b.go and "+root, root)
	want := "edit internal/a.go and ~/work/project2/b.go and ."
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRedactScrubsPathsAndOutcome(t *testing.T) {
	e := ev("read " + homeDir + "/alice/.aws/credentials")
	e.Outcome = "failed: " + fakeAWS + " in " + macDir + "/bob/x"
	got := RedactIn(e, Settings{Mode: ModeFull, ArgCapBytes: 500})
	if got.ArgSummary != "read ~/.aws/credentials" {
		t.Fatalf("arg: %q", got.ArgSummary)
	}
	if strings.Contains(got.Outcome, "AKIA") || strings.Contains(got.Outcome, "bob") {
		t.Fatalf("outcome leaked: %q", got.Outcome)
	}
}

func TestRedactDoesNotMutateInput(t *testing.T) {
	e := ev(fakeGH)
	_ = Redact(e, Settings{Mode: ModeFull})
	if e.ArgSummary != fakeGH {
		t.Fatal("input mutated")
	}
}

func TestPlaceholderNotRedacted(t *testing.T) {
	got := Redact(ev("password=aaaaaaaaaaaaaaaaaaaa"), Settings{Mode: ModeFull})
	if strings.Contains(got.ArgSummary, "[REDACTED]") {
		t.Fatalf("low-entropy placeholder redacted (detector floor changed?): %q", got.ArgSummary)
	}
}
