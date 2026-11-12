package session

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const interstitialFixturesDir = "testdata/interstitial"

// interstitialNone is the "no prompt" action shape every negative fixture
// must produce.
func interstitialNone(t *testing.T, action InterstitialPromptAction) bool {
	t.Helper()
	if action.Kind != InterstitialNone {
		t.Errorf("Kind = %q, want none", action.Kind)
		return false
	}
	if len(action.Dismiss) != 0 {
		t.Errorf("Dismiss = %#v, want empty for none", action.Dismiss)
		return false
	}
	if action.ChangesSettingOnEnter {
		t.Errorf("ChangesSettingOnEnter = true for none, want false")
		return false
	}
	return true
}

// interstitialFixtureKinds pins each fixture to the kind its README row
// states (testdata/interstitial/README.md).
var interstitialFixtureKinds = map[string]InterstitialKind{
	"autocompact-enter-would-pick.txt":      InterstitialAutocompactThreshold,
	"autocompact-keep-auto-selected.txt":    InterstitialAutocompactThreshold,
	"autocompact-enter-would-pick-ansi.txt": InterstitialAutocompactThreshold,
	"partial-autocompact-no-footer.txt":     InterstitialNone,
	"idle-prompt.txt":                       InterstitialNone,
	"quoted-autocompact-in-output.txt":      InterstitialNone,
}

// readInterstitialFixture loads one fixture from testdata/interstitial.
func readInterstitialFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(interstitialFixturesDir, name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return string(data)
}

// TestInterstitialFixtureClassification: every fixture classifies
// (case-sensitively, byte-for-byte) to the kind its README row states, and
// the action carries the safe dismiss key for a recognised prompt
// and an empty one for none.
func TestInterstitialFixtureClassification(t *testing.T) {
	for name, wantKind := range interstitialFixtureKinds {
		t.Run(name, func(t *testing.T) {
			action := ClassifyInterstitialPrompt(readInterstitialFixture(t, name))
			if action.Kind != wantKind {
				t.Fatalf("Kind = %q, want %q", action.Kind, wantKind)
			}
			switch wantKind {
			case InterstitialNone:
				if len(action.Dismiss) != 0 {
					t.Errorf("Dismiss = %#v, want empty for none", action.Dismiss)
				}
				if action.ChangesSettingOnEnter {
					t.Errorf("ChangesSettingOnEnter = true for none, want false")
				}
			default:
				if !reflect.DeepEqual(action.Dismiss, []string{"Esc"}) {
					t.Errorf("Dismiss = %#v, want [Esc]", action.Dismiss)
				}
			}
		})
	}
}

// TestInterstitialEnterNeverReturned: across every fixture and every table
// entry, no returned dismiss key is Enter (or any other key
// IsSafeInterstitialKey rejects: newline, carriage return, C-m, a digit,
// Tab, space, an arrow).
func TestInterstitialEnterNeverReturned(t *testing.T) {
	unsafe := []string{"Enter", "Return", "return", "C-m", "c-m", "\n", "\r",
		"Tab", "\t", " ", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
		"Up", "Down", "Left", "Right", "ArrowUp", "ArrowDown", "Space"}
	check := func(t *testing.T, got []string, where string) {
		t.Helper()
		for _, k := range got {
			if !IsSafeInterstitialKey(k) {
				t.Errorf("%s: returned key %q is not on the safe-key allow-list", where, k)
			}
			for _, u := range unsafe {
				if strings.EqualFold(k, u) {
					t.Errorf("%s: returned key %q is explicitly unsafe (%q)", where, k, u)
				}
			}
		}
	}
	for name := range interstitialFixtureKinds {
		action := ClassifyInterstitialPrompt(readInterstitialFixture(t, name))
		if action.Kind != InterstitialNone {
			check(t, action.Dismiss, "fixture "+name)
		}
	}
	for _, entry := range interstitialTable {
		check(t, entry.dismiss, "table entry "+string(entry.kind))
	}
}

// TestInterstitialFixturesEnterWouldPick: the 400k capture is exactly the
// pane where Enter WOULD pick "Autocompact at 400k" — the classifier must
// say so; the keep-auto capture is the same prompt with the cursor on the
// neutral row, so it must say the opposite. Both offer exactly the safe
// dismiss key.
func TestInterstitialFixturesEnterWouldPick(t *testing.T) {
	pick := ClassifyInterstitialPrompt(readInterstitialFixture(t, "autocompact-enter-would-pick.txt"))
	if pick.Kind != InterstitialAutocompactThreshold {
		t.Fatalf("Kind = %q, want autocompact-threshold", pick.Kind)
	}
	if !pick.ChangesSettingOnEnter {
		t.Errorf("ChangesSettingOnEnter = false, want true (Enter would pick Autocompact at 400k)")
	}
	if !strings.Contains(pick.SelectedOption, "Autocompact at 400k") {
		t.Errorf("SelectedOption = %q, want it to contain %q", pick.SelectedOption, "Autocompact at 400k")
	}
	if !reflect.DeepEqual(pick.Dismiss, []string{"Esc"}) {
		t.Errorf("Dismiss = %#v, want exactly [Esc]", pick.Dismiss)
	}

	keep := ClassifyInterstitialPrompt(readInterstitialFixture(t, "autocompact-keep-auto-selected.txt"))
	if keep.Kind != InterstitialAutocompactThreshold {
		t.Fatalf("Kind = %q, want autocompact-threshold", keep.Kind)
	}
	if keep.ChangesSettingOnEnter {
		t.Errorf("ChangesSettingOnEnter = true, want false (cursor is on the Keep auto row)")
	}
	if !strings.Contains(keep.SelectedOption, "Keep auto") {
		t.Errorf("SelectedOption = %q, want it to contain %q", keep.SelectedOption, "Keep auto")
	}
	if !reflect.DeepEqual(keep.Dismiss, []string{"Esc"}) {
		t.Errorf("Dismiss = %#v, want exactly [Esc]", keep.Dismiss)
	}

	// The ANSI capture must classify exactly like its plain twin.
	ansi := ClassifyInterstitialPrompt(readInterstitialFixture(t, "autocompact-enter-would-pick-ansi.txt"))
	if ansi.Kind != InterstitialAutocompactThreshold || !ansi.ChangesSettingOnEnter {
		t.Fatalf("ANSI capture: Kind=%q ChangesSettingOnEnter=%v, want autocompact-threshold true", ansi.Kind, ansi.ChangesSettingOnEnter)
	}
	if ansi.SelectedOption != pick.SelectedOption {
		t.Errorf("ANSI SelectedOption = %q, want it to equal the plain capture's %q", ansi.SelectedOption, pick.SelectedOption)
	}
	if !reflect.DeepEqual(ansi.Dismiss, pick.Dismiss) {
		t.Errorf("ANSI Dismiss = %#v, want %#v", ansi.Dismiss, pick.Dismiss)
	}
}

// TestInterstitialNonPromptsReturnNone: the negative shapes — the partial
// capture, the idle prompt, the quoted-in-output pane — all classify as
// none, because a missing anchor or a missing hint line is the "when in
// doubt, none" rule working.
func TestInterstitialNonPromptsReturnNone(t *testing.T) {
	for _, name := range []string{
		"partial-autocompact-no-footer.txt",
		"idle-prompt.txt",
		"quoted-autocompact-in-output.txt",
	} {
		t.Run(name, func(t *testing.T) {
			action := ClassifyInterstitialPrompt(readInterstitialFixture(t, name))
			if !interstitialNone(t, action) {
				t.Fatalf("fixture %s should classify as none", name)
			}
		})
	}
}

// TestInterstitialDisjointFromStartupClassifier: every startup fixture and
// every decision-style dialog (folder trust, bypass-permissions consent,
// MCP trust, the "enter to confirm" footer dialogs, the usage-limit
// dialog) is NOT an interstitial prompt. The two classifiers must not
// overlap: these panes are decisions an operator must make, and the
// interstitial classifier must stay out of them.
func TestInterstitialDisjointFromStartupClassifier(t *testing.T) {
	entries, err := os.ReadDir("testdata/startup")
	if err != nil {
		t.Fatalf("reading testdata/startup: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		data, err := os.ReadFile(filepath.Join("testdata", "startup", e.Name()))
		if err != nil {
			t.Fatalf("reading startup fixture %s: %v", e.Name(), err)
		}
		if action := ClassifyInterstitialPrompt(string(data)); action.Kind != InterstitialNone {
			t.Errorf("startup fixture %s classified interstitial %#v, want none", e.Name(), action)
		}
	}

	decisionPanels := map[string]string{
		"folder-trust": "  Quick safety check\n\n" +
			"  ┌─────────────────────────────────────────┐\n" +
			"  │ Trust this folder?                       │\n" +
			"  │ 1. Yes, I trust this folder              │\n" +
			"  │ 2. No, exit                              │\n" +
			"  └─────────────────────────────────────────┘\n",
		"bypass-consent": "  Bypass permissions?\n" +
			"  1. Yes, I accept\n" +
			"  2. No, exit\n",
		"mcp-trust": "  New MCP server found.\n" +
			"  Use this MCP server?\n" +
			"  1. Yes\n" +
			"  2. No\n",
		"usage-limit-dialog": "  ⎿  5-hour limit reached ∙ resets 2pm\n" +
			"  Your usage limit will reset at 2pm (America/New_York).\n" +
			"  ❯ 1. Stop and wait for limit to reset\n" +
			"  2. Resume automatically at 2pm\n",
		"permission-request": "  Claude requested permission to run rm -rf build/\n" +
			"  ❯ 1. Allow once\n" +
			"  2. Allow always\n" +
			"  3. Deny\n",
	}
	for name, pane := range decisionPanels {
		t.Run(name, func(t *testing.T) {
			if action := ClassifyInterstitialPrompt(pane); action.Kind != InterstitialNone {
				t.Errorf("decision dialog %s classified interstitial %#v, want none", name, action)
			}
		})
	}
}

// TestInterstitialPromptScrolledAboveWindow: the prompt rendered earlier in
// the pane, with more than 20 non-empty lines of output after it, must not
// match — the classifier only looks at the bottom of the pane.
func TestInterstitialPromptScrolledAboveWindow(t *testing.T) {
	prompt := readInterstitialFixture(t, "autocompact-enter-would-pick.txt")
	var b strings.Builder
	b.WriteString(prompt)
	b.WriteString("\n")
	for i := 1; i <= 25; i++ {
		b.WriteString("  Follow-up output line number ")
		b.WriteString(strings.Repeat("x", i%7+1))
		b.WriteString(" that keeps scrolling the prompt upward.\n")
		if i%4 == 0 {
			b.WriteString("\n")
		}
	}
	if action := ClassifyInterstitialPrompt(b.String()); action.Kind != InterstitialNone {
		t.Fatalf("scrolled prompt classified %#v, want none (prompt is above the 20-line window)", action)
	}

	// And the same prompt, still at the bottom, still matches — the
	// window is about relative position, not about pane length.
	tail := ClassifyInterstitialPrompt(b.String() + "\n" + prompt)
	if tail.Kind != InterstitialAutocompactThreshold {
		t.Fatalf("re-drawn prompt at the bottom classified %q, want autocompact-threshold", tail.Kind)
	}
}

// TestInterstitialMatchingIsTolerant: case-insensitive matching, separator
// tolerance on the hint line (arrows, commas, dots, middle dots, plain
// spaces all stand in for each other), and the conservative
// ChangesSettingOnEnter=true when the cursor row cannot be read.
func TestInterstitialMatchingIsTolerant(t *testing.T) {
	uppercase := "  WANT TO AUTOCOMPACT AT 400K TOKENS INSTEAD?\n" +
		"  ❯ AUTOCOMPACT AT 400K\n" +
		"  KEEP AUTO (1M TOKENS)\n" +
		"  UP DOWN CHOOSE, ENTER CONFIRM, ESC NOT NOW\n"
	action := ClassifyInterstitialPrompt(uppercase)
	if action.Kind != InterstitialAutocompactThreshold {
		t.Fatalf("uppercase prompt classified %q, want autocompact-threshold", action.Kind)
	}

	commaHint := "  Want to autocompact at 400k tokens instead?\n" +
		"  Autocompact at 400k\n" +
		"  Keep auto (1m tokens)\n" +
		"  up down choose, enter confirm, esc: not now.\n"
	if action = ClassifyInterstitialPrompt(commaHint); action.Kind != InterstitialAutocompactThreshold {
		t.Fatalf("comma/dot hint prompt classified %q, want autocompact-threshold", action.Kind)
	}

	// No cursor anywhere: the option row cannot be read, so the
	// setting-on-enter answer must be the conservative one.
	noCursor := "  Want to autocompact at 400k tokens instead?\n" +
		"  Autocompact at 400k\n" +
		"  Keep auto (1m tokens)\n" +
		"  ↑ ↓ to choose · Enter to confirm · Esc not now\n"
	action = ClassifyInterstitialPrompt(noCursor)
	if action.Kind != InterstitialAutocompactThreshold {
		t.Fatalf("cursor-less prompt classified %q, want autocompact-threshold", action.Kind)
	}
	if action.SelectedOption != "" {
		t.Errorf("SelectedOption = %q, want empty (no cursor row)", action.SelectedOption)
	}
	if !action.ChangesSettingOnEnter {
		t.Errorf("ChangesSettingOnEnter = false with an unreadable cursor, want true (conservative)")
	}
}

// TestInterstitialSafeKeyAllowList: the allow-list guard. Every table
// entry's dismiss slice passes IsSafeInterstitialKey, and the function
// itself rejects the whole family of decision keys (Enter, Return, C-m,
// newline, carriage return, Tab, space, every digit, every arrow) no
// matter how they are spelled.
func TestInterstitialSafeKeyAllowList(t *testing.T) {
	for i, entry := range interstitialTable {
		if len(entry.dismiss) == 0 {
			t.Errorf("table entry %d (%s) has no dismiss keys", i, entry.kind)
		}
		for _, k := range entry.dismiss {
			if !IsSafeInterstitialKey(k) {
				t.Errorf("table entry %s dismiss key %q fails IsSafeInterstitialKey", entry.kind, k)
			}
		}
	}

	if !IsSafeInterstitialKey("Esc") {
		t.Error("Esc must be on the allow-list")
	}
	if !IsSafeInterstitialKey("ESC") {
		t.Error("allow-list matching must be case-insensitive")
	}

	unsafe := []string{
		"Enter", "enter", "ENTER", "Return", "return",
		"C-m", "c-m", "C-M", "\n", "\r", "\x1b",
		"Tab", "\t", "space", " ",
		"0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
		"Up", "Down", "Left", "Right", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight",
		"x", "y", "a", "b", "zq", "Escape-key", "esc2",
	}
	for _, k := range unsafe {
		if IsSafeInterstitialKey(k) {
			t.Errorf("IsSafeInterstitialKey(%q) = true, want false", k)
		}
	}
	if IsSafeInterstitialKey("") {
		t.Error("blank key must not be on the allow-list")
	}
}

// TestInterstitialReadmeAndFixturesConsistent: the README and the fixture
// directory name the same set of .txt files, in both directions. A fixture
// with no README row is an undocumented capture; a README row with no file
// is a broken reference.
func TestInterstitialReadmeAndFixturesConsistent(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join(interstitialFixturesDir, "README.md"))
	if err != nil {
		t.Fatalf("reading README: %v", err)
	}
	readmeText := string(readme)

	entries, err := os.ReadDir(interstitialFixturesDir)
	if err != nil {
		t.Fatalf("reading fixture dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		if !strings.Contains(readmeText, e.Name()) {
			t.Errorf("fixture %s is not named in the README", e.Name())
		}
	}

	wanted := []string{
		"autocompact-enter-would-pick.txt",
		"autocompact-keep-auto-selected.txt",
		"autocompact-enter-would-pick-ansi.txt",
		"partial-autocompact-no-footer.txt",
		"idle-prompt.txt",
		"quoted-autocompact-in-output.txt",
	}
	for _, name := range wanted {
		if !strings.Contains(readmeText, name) {
			t.Errorf("README does not name fixture %s", name)
			continue
		}
		if _, err := os.Stat(filepath.Join(interstitialFixturesDir, name)); err != nil {
			t.Errorf("README names %s but the file is missing: %v", name, err)
		}
	}
}
