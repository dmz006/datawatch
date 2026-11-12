package planrecords

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatesAndClosed(t *testing.T) {
	if len(States(KindRegistryRow)) != 8 || len(States(KindFinding)) != 5 || States("x") != nil {
		t.Fatal("state sets wrong")
	}
	if !ValidState(KindRegistryRow, StateDeferred) || ValidState(KindFinding, StateDeferred) || ValidState("x", StateOpen) {
		t.Fatal("ValidState wrong")
	}
	closed := map[Kind][]State{KindRegistryRow: {StateShipped, StateDone, StateCancelled}, KindFinding: {StateFixed, StateWontfix, StateDuplicate}}
	for k, ss := range closed {
		for _, s := range ss {
			if !IsClosed(k, s) {
				t.Errorf("%s/%s should be closed", k, s)
			}
		}
		if IsClosed(k, StateOpen) {
			t.Errorf("%s open should not be closed", k)
		}
	}
	if IsClosed("x", StateOpen) || !ValidKind(KindFinding) || ValidKind("x") {
		t.Fatal("kind checks wrong")
	}
}

func TestNormTitleAndRowRef(t *testing.T) {
	if NormTitle("  Hello   WORLD ") != "hello world" {
		t.Fatal("NormTitle")
	}
	for in, want := range map[string]int{"row 12": 12, "Row #7": 7, " ROW   3 ": 3} {
		if n, ok := ParseRowRef(in); !ok || n != want {
			t.Errorf("ParseRowRef(%q)=%d,%v", in, n, ok)
		}
	}
	for _, in := range []string{"row", "row 0", "row x", "12", "rows 3"} {
		if _, ok := ParseRowRef(in); ok {
			t.Errorf("ParseRowRef(%q) should fail", in)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := Record{Kind: KindRegistryRow, Project: "datawatch", State: StateOpen, Title: "T"}
	if err := ok.validate(); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("x", MaxTitleRunes+1)
	bad := map[string]func(r *Record){
		"kind":       func(r *Record) { r.Kind = "x" },
		"project":    func(r *Record) { r.Project = "" },
		"project2":   func(r *Record) { r.Project = "a b" },
		"projectln":  func(r *Record) { r.Project = strings.Repeat("a", MaxProjectRunes+1) },
		"state":      func(r *Record) { r.State = StateFixed },
		"title":      func(r *Record) { r.Title = "  " },
		"titlelen":   func(r *Record) { r.Title = long },
		"body":       func(r *Record) { r.Body = strings.Repeat("x", MaxBodyBytes+1) },
		"id":         func(r *Record) { r.ID = "Bad ID" },
		"idlen":      func(r *Record) { r.ID = strings.Repeat("a", MaxIDRunes+1) },
		"legneg":     func(r *Record) { r.LegacyNumber = -1 },
		"legfind":    func(r *Record) { r.Kind, r.State, r.LegacyNumber = KindFinding, StateOpen, 3 },
		"source":     func(r *Record) { r.LegacySource = strings.Repeat("x", MaxSourceRunes+1) },
		"version":    func(r *Record) { r.ShippedVersion = strings.Repeat("x", MaxVersionRunes+1) },
		"shipped":    func(r *Record) { r.ShippedBy = strings.Repeat("x", MaxAutomatonRunes+1) },
		"reason":     func(r *Record) { r.ReviewReason = strings.Repeat("x", MaxReasonRunes+1) },
		"aliases":    func(r *Record) { r.Aliases = make([]string, MaxAliases+1) },
		"aliasempty": func(r *Record) { r.Aliases = []string{" "} },
		"links":      func(r *Record) { r.Links = make([]Link, MaxLinks+1) },
		"linkempty":  func(r *Record) { r.Links = []Link{{Kind: "a"}} },
		"linklong":   func(r *Record) { r.Links = []Link{{Kind: "a", Ref: strings.Repeat("x", MaxLinkRefRunes+1)}} },
	}
	for name, mut := range bad {
		r := ok
		mut(&r)
		err := r.validate()
		var inv *ErrInvalid
		if !errors.As(err, &inv) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		} else if !strings.Contains(inv.Error(), "invalid record") {
			t.Errorf("%s: message %q", name, inv.Error())
		}
	}
}

func TestCloneIsDeep(t *testing.T) {
	r := Record{Links: []Link{{"a", "b"}}, Aliases: []string{"x"}}
	c := r.clone()
	c.Links[0].Ref, c.Aliases[0] = "z", "z"
	if r.Links[0].Ref != "b" || r.Aliases[0] != "x" {
		t.Fatal("clone shares memory")
	}
}

func TestSlugify(t *testing.T) {
	if got := slugify("  Hello, World! 2x ", 40); got != "hello-world-2x" {
		t.Fatalf("slug %q", got)
	}
	if got := slugify("!!!", 40); got != "" {
		t.Fatalf("slug %q", got)
	}
	if got := slugify(strings.Repeat("ab ", 30), 10); len(got) > 10 {
		t.Fatalf("slug too long %q", got)
	}
}

// The package is a store: it must never start a process or send a signal.
func TestNoProcessOrSignalCode(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	banned := []string{"os/exec", "os/signal", "syscall", "exec.Command", ".Kill(", "tmux", "kill-session"}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range banned {
			if strings.Contains(string(b), w) {
				t.Errorf("%s contains %q", f, w)
			}
		}
	}
}
