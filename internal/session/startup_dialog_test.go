package session

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// expected fixture outcomes, one per file in testdata/startup.
var startupFixtureCases = []struct {
	file string
	want StartupDialogAction
}{
	{"trust-folder.txt", StartupDialogAction{
		Kind: StartupDialogTrustFolder,
		Keys: []string{"Down", "Enter"},
	}},
	{"bypass-accept.txt", StartupDialogAction{
		Kind: StartupDialogBypassAccept,
		Keys: []string{"2", "Enter"},
	}},
	{"dev-channels.txt", StartupDialogAction{
		Kind: StartupDialogDevChannels,
		Keys: []string{"Enter"},
	}},
	{"mcp-trust.txt", StartupDialogAction{
		Kind: StartupDialogMCPTrust,
		Keys: []string{"1", "Enter"},
	}},
	{"confirm-continue.txt", StartupDialogAction{
		Kind: StartupDialogConfirmContinue,
		Keys: []string{"Enter"},
	}},
	{"confirm-unsafe.txt", StartupDialogAction{
		Kind:  StartupDialogConfirmUnsafe,
		Keys:  []string{"Esc"},
		Retry: true,
	}},
	{"update-prompt.txt", StartupDialogAction{
		Kind:  StartupDialogUpdatePrompt,
		Keys:  []string{"Esc"},
		Retry: true,
	}},
	{"unknown-confirm.txt", StartupDialogAction{
		Kind: StartupDialogUnknownConfirm,
		Keys: []string{},
	}},
	{"idle-prompt.txt", StartupDialogAction{
		Kind: StartupDialogNone,
		Keys: []string{},
	}},
}

func TestClassifyStartupDialogFixtureVariants(t *testing.T) {
	for _, tc := range startupFixtureCases {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "startup", tc.file))
			if err != nil {
				t.Fatalf("reading fixture %s: %v", tc.file, err)
			}
			got := classifyStartupDialog(string(data))
			if got.Kind != tc.want.Kind {
				t.Errorf("Kind = %q, want %q", got.Kind, tc.want.Kind)
			}
			if !reflect.DeepEqual(got.Keys, tc.want.Keys) {
				t.Errorf("Keys = %#v, want %#v", got.Keys, tc.want.Keys)
			}
			if got.Retry != tc.want.Retry {
				t.Errorf("Retry = %v, want %v", got.Retry, tc.want.Retry)
			}
		})
	}
}

// Unsafe dialogs must be declined with Esc (never accepted with Enter),
// and both must ask the daemon to treat the session as a start failure.
func TestClassifyStartupDialogUnsafeVariants(t *testing.T) {
	for _, name := range []string{"confirm-unsafe.txt", "update-prompt.txt"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "startup", name))
			if err != nil {
				t.Fatalf("reading fixture %s: %v", name, err)
			}
			got := classifyStartupDialog(string(data))
			if !got.Retry {
				t.Errorf("Retry = false, want true")
			}
			for _, k := range got.Keys {
				if k == "Enter" {
					t.Fatalf("unsafe dialog %s maps to Enter: %#v", name, got.Keys)
				}
			}
			if !reflect.DeepEqual(got.Keys, []string{"Esc"}) {
				t.Errorf("Keys = %#v, want [Esc]", got.Keys)
			}
		})
	}
}

// The classifier must strip ANSI and match case-insensitively.
func TestClassifyStartupDialogANSIAndCase(t *testing.T) {
	ansiTrust := "\x1b[1m  ┌────────────────────────────────────────\x1b[0m\n" +
		"\x1b[7m  │ QUICK SAFETY CHECK \x1b[0m\n" +
		"  │ \x1b[32m1. YES, I TRUST THIS FOLDER\x1b[0m\n" +
		"  │ 2. No, exit\n" +
		"  └────────────────────────────────────────\n"
	got := classifyStartupDialog(ansiTrust)
	if got.Kind != StartupDialogTrustFolder {
		t.Fatalf("Kind = %q, want trust-folder", got.Kind)
	}
	if !reflect.DeepEqual(got.Keys, []string{"Down", "Enter"}) {
		t.Errorf("Keys = %#v, want [Down Enter]", got.Keys)
	}

	// A pane that is empty or pure whitespace is simply no dialog.
	for _, pane := range []string{"", "   \n\t\n"} {
		if got := classifyStartupDialog(pane); got.Kind != StartupDialogNone {
			t.Errorf("empty-ish pane: Kind = %q, want none", got.Kind)
		}
	}
}

// The arrow-key trust sequence must match SendTrustPromptAccept
// (Down then Enter) so both paths stay interchangeable.
func TestClassifyStartupDialogTrustFolderUsesArrowPath(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "startup", "trust-folder.txt"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	got := classifyStartupDialog(string(data))
	want := []string{"Down", "Enter"}
	if !reflect.DeepEqual(got.Keys, want) {
		t.Errorf("Keys = %#v, want %#v (matches SendTrustPromptAccept)", got.Keys, want)
	}
}
