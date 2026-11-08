package mergepipeline

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func okStep(name string) Step {
	return Step{Name: name, Kind: KindTest, Command: "go test ./..."}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		steps   []Step
		wantErr string // substring; empty means valid
	}{
		{"valid", []Step{okStep("a"), okStep("b")}, ""},
		{"no steps", nil, "no steps"},
		{"empty name", []Step{{Kind: KindTest, Command: "x"}}, "empty name"},
		{"blank name", []Step{{Name: "  ", Kind: KindTest, Command: "x"}}, "empty name"},
		{"duplicate name", []Step{okStep("a"), okStep("a")}, "duplicate name"},
		{"unknown kind", []Step{{Name: "a", Kind: "bogus", Command: "x"}}, "unknown kind"},
		{"empty kind", []Step{{Name: "a", Command: "x"}}, "unknown kind"},
		{"no command or action", []Step{{Name: "a", Kind: KindTest}}, "needs a command or an action"},
		{"both command and action", []Step{{Name: "a", Kind: KindTest, Command: "x", Action: ActionTagRelease}}, "not both"},
		{"unknown action", []Step{{Name: "a", Kind: KindTest, Action: "nope"}}, "unknown action"},
		{"absolute dir", []Step{{Name: "a", Kind: KindTest, Command: "x", Dir: "/etc"}}, "relative"},
		{"windows drive dir", []Step{{Name: "a", Kind: KindTest, Command: "x", Dir: "C:/x"}}, "relative"},
		{"dotdot dir", []Step{{Name: "a", Kind: KindTest, Command: "x", Dir: "../x"}}, "'..'"},
		{"nested dotdot dir", []Step{{Name: "a", Kind: KindTest, Command: "x", Dir: "a/../../b"}}, "'..'"},
		{"backslash dotdot dir", []Step{{Name: "a", Kind: KindTest, Command: "x", Dir: `a\..\b`}}, "'..'"},
		{"relative dir ok", []Step{{Name: "a", Kind: KindTest, Command: "x", Dir: "sub/dir"}}, ""},
		{"dotted name dir ok", []Step{{Name: "a", Kind: KindTest, Command: "x", Dir: "a..b"}}, ""},
		{"bad on_failure", []Step{{Name: "a", Kind: KindTest, Command: "x", OnFailure: "retry"}}, "on_failure"},
		{"continue ok", []Step{{Name: "a", Kind: KindTest, Command: "x", OnFailure: OnFailureContinue}}, ""},
		{"negative timeout", []Step{{Name: "a", Kind: KindTest, Command: "x", Timeout: -time.Second}}, "negative timeout"},
		{"unscoped kill-server", []Step{{Name: "a", Kind: KindCustom, Command: "tmux kill-server"}}, "unscoped tmux kill"},
		{"unscoped kill-session -a", []Step{{Name: "a", Kind: KindCustom, Command: "echo hi && tmux kill-session -a"}}, "unscoped tmux kill"},
		{"tmpdir only still unscoped", []Step{{Name: "a", Kind: KindCustom, Command: "TMUX_TMPDIR=/x tmux kill-server"}}, "unscoped tmux kill"},
		{"socket scoped kill ok", []Step{{Name: "a", Kind: KindCustom, Command: `tmux -S "$(mktemp -d)/s" kill-server`}}, ""},
		{"env scrubbed kill ok", []Step{{Name: "a", Kind: KindCustom, Command: "env -u TMUX -u TMUX_TMPDIR tmux kill-server"}}, ""},
		{"env scrub on other command", []Step{{Name: "a", Kind: KindCustom, Command: "env -u TMUX -u TMUX_TMPDIR go test ./...; tmux kill-server"}}, "unscoped tmux kill"},
		{"socket on other command", []Step{{Name: "a", Kind: KindCustom, Command: "tmux -L scratch list-sessions; tmux kill-server"}}, "unscoped tmux kill"},
		{"colon in dir ok", []Step{{Name: "a", Kind: KindTest, Command: "x", Dir: "a:b"}}, ""},
		{"bad make target", []Step{{Name: "a", Kind: KindTest, Command: "x", RequiresMakeTarget: "a b"}}, "requires_make_target"},
		{"kill-session by name ok", []Step{{Name: "a", Kind: KindCustom, Command: "tmux kill-session -t mine"}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Definition{Steps: c.steps}.Validate()
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("want ValidationError, got %v", err)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error %q lacks %q", err, c.wantErr)
			}
		})
	}
}

func TestValidateReportsAllProblems(t *testing.T) {
	err := Definition{Steps: []Step{{Kind: "x"}, {Name: "b", Kind: KindTest}}}.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) || len(ve.Problems) < 3 {
		t.Fatalf("want several problems, got %v", err)
	}
}

func TestEveryKindAccepted(t *testing.T) {
	for _, k := range Kinds() {
		d := Definition{Steps: []Step{{Name: "s", Kind: k, Command: "true"}}}
		if err := d.Validate(); err != nil {
			t.Errorf("kind %s: %v", k, err)
		}
	}
}

func TestEffectiveOnFailure(t *testing.T) {
	if (Step{}).EffectiveOnFailure() != OnFailureStop {
		t.Error("default should be stop")
	}
	if (Step{OnFailure: OnFailureContinue}).EffectiveOnFailure() != OnFailureContinue {
		t.Error("continue lost")
	}
}

func TestValidateReleaseBumpActionIsAccepted(t *testing.T) {
	d := Definition{Steps: []Step{{Name: "validate", Kind: KindCustom, Action: ActionValidateReleaseBump}}}
	if err := d.Validate(); err != nil {
		t.Fatalf("validate_release_bump rejected: %v", err)
	}
}
