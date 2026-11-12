// Package session provides the session manager. This file defines the
// built-in table of recognised Claude Code interstitial prompts — the
// settings nudges that appear mid-session, after a finished turn, while the
// session's stop hook waits. They are not startup dialogs (those are owned
// by startup_dialog.go) and they are not decision prompts (workspace trust,
// MCP server trust, bypass-permissions consent, permission requests and the
// usage-limit dialog all require an operator judgement and are deliberately
// recognised as none here, as they are there).
//
// Why a built-in table and not a DefaultDetection pattern list
// -------------------------------------------------------------
// The Detection Pattern Governance rule in AGENT.md puts every detection
// pattern in config.DefaultDetection(); the documented exception is the
// rate-limit table, which must carry backend and phrase together. The
// interstitial table is the same documented exception one level up: each
// kind must carry, besides its anchor phrases, the set of safe dismiss keys
// and a safety flag (which of the prompt's option rows would change a
// setting if Enter were sent on it). A flat
// detection.<list> of substrings cannot hold per-kind dismiss keys or
// safety flag: recognizing the phrase and then deciding what to send are
// two facts that must travel together, exactly as a rate-limit phrase and
// its backend family do. So, like the rate-limit table, the entries live
// in-tree, closed, and evidence-backed: adding an entry requires a
// recorded fixture in testdata/interstitial/ that carries the verbatim
// wording and names its source, and a dismiss key may enter only if the
// recorded capture's own hint line names it and a test proves it does not
// select an option (see interstitialSafeKeys below).
//
// Matching rules
// --------------
// ClassifyInterstitialPrompt is pure. It strips ANSI (StripANSI) and
// lower-cases for matching, then looks ONLY at the last 20 non-empty lines
// of the pane, so a worker that merely quotes a prompt's wording further up
// in its own output never matches. A kind matches only when ALL of the
// table entry's anchors are present in that window AND one of the window's
// lines is the prompt's footer hint line: each of the entry's footer words
// appears on that one line after letter-only normalization, which is what
// makes the match tolerant of arrow glyphs, commas, dots, middle dots and
// other separators between the hint's words. A partial or truncated pane —
// footer missing, one option row missing — matches nothing. When in doubt,
// the classifier returns InterstitialNone.
//
// Safety
// ------
// Dismiss keys are checked against interstitialSafeKeys, a closed
// allow-list that today holds exactly Esc (lower-cased for the
// case-insensitive check). Enter, Return, C-m, newline, carriage return,
// Tab, space, every digit and every arrow key are all rejected — they are
// not on the allow-list. On the autocompact prompt, Enter on the
// "Autocompact at ..." row changes a setting, so the action also reports
// ChangesSettingOnEnter: true when the cursor (the ❯ row in the pane) is
// on that row, and — conservatively — true when the cursor row cannot be
// read at all. A later task sends the dismiss key; this task only decides.
//
// Provenance
// ----------
// Every entry's wording is recorded verbatim in testdata/interstitial/
// (one capture per fixture, README names each file's source). The
// autocompact-threshold entry comes from the operator's recorded stall
// incident (the 80-minute stand-off on "Want to autocompact at 400k
// tokens instead?", documented in docs/plans/README.md: Escape / "not now"
// cleared the prompt without changing the setting; Enter on the highlighted
// row, "Autocompact at 400k", would have changed it). Candidate prompts
// that were checked and found WITHOUT verbatim in-tree or official
// release-note wording — so no entry is created for them — are recorded in
// the testdata/interstitial README: the session feedback survey (official
// release notes only name the survey, its prompt wording and hint line are
// unrecorded), the update-available notice (owned by the startup
// classifier, which must stay disjoint), and other "not now" settings
// nudges (no capture exists). When a live capture of one lands, add the
// fixture (README: source) and the table entry — the key-safety guard test
// pins the dismiss slice, and the fixture-consistency test pins the pair.

package session

import (
	"strings"
)

// InterstitialKind names a recognised interstitial prompt shape. The values
// are a closed set; a pane that matches no table entry is
// InterstitialNone, as are idle prompts, quoted prompt text, and every
// startup/decision dialog.
type InterstitialKind string

const (
	// InterstitialNone: no interstitial prompt in the pane's matching
	// window. The "when in doubt, none" outcome.
	InterstitialNone InterstitialKind = "none"
	// InterstitialAutocompactThreshold: "Want to autocompact at Nk tokens
	// instead?" with the Autocompact / Keep auto option pair and the
	// choose/confirm/esc "not now" hint line.
	InterstitialAutocompactThreshold InterstitialKind = "autocompact-threshold"
)

// InterstitialPromptAction is the pure decision for one captured pane:
// which interstitial kind is on screen, which key may dismiss it without
// side effects (empty slice means there is nothing to dismiss), the option
// row the pane's cursor is currently on ("" when the cursor row reads
// nothing), and whether Enter WOULD change a setting for this kind (true
// whenever the cursor is on the setting-changing row, or cannot be read at
// all — the conservative reading).
type InterstitialPromptAction struct {
	Kind                  InterstitialKind
	Dismiss               []string
	SelectedOption        string
	ChangesSettingOnEnter bool
}

// interstitialWindowLines is the number of non-empty pane lines, taken from
// the bottom of the pane, that the classifier looks at. The window is the
// guard against a worker that quoted the prompt's wording earlier in its
// own output: only a prompt rendered at the end of the pane matches.
const interstitialWindowLines = 20

// interstitialCursorGlyph is the heavy right-pointing angle glyph (❯,
// U+276F) the Claude Code TUI rows use to mark the selected/next-option
// row; it is the same selector the existing rate-limit and startup
// fixtures record.
const interstitialCursorGlyph = "❯"

// interstitialTableEntry is one row of the built-in interstitial table.
// Anchors are lower-cased substrings that must ALL appear in the pane
// window (question line first, then the option rows). FooterWords are the
// words of the prompt's own hint line that must appear on one single line
// after letter-only normalization (tolerant of arrows, commas, dots,
// middle dots, spaces). SettingAnchor is the lower-cased fragment of the
// option row whose selection changes a setting; a row matching it — or a
// cursor that cannot be read — makes ChangesSettingOnEnter true. Dismiss
// are the safe dismiss keys and must every pass IsSafeInterstitialKey.
// Evidence names the recorded source of the wording.
type interstitialTableEntry struct {
	kind          InterstitialKind
	anchors       []string
	footerWords   []string
	settingAnchor string
	dismiss       []string
	evidence      string
}

var interstitialTable = []interstitialTableEntry{
	{
		kind: InterstitialAutocompactThreshold,
		anchors: []string{
			"want to autocompact at", // question line
			"autocompact at",         // option row 1
			"keep auto",              // option row 2
		},
		footerWords:   []string{"choose", "confirm", "esc", "notnow"},
		settingAnchor: "autocompact at",
		dismiss:       []string{"Esc"},
		evidence:      "testdata/interstitial/autocompact-enter-would-pick.txt and the operator's recorded stall incident (docs/plans/README.md): 'Want to autocompact at 400k tokens instead?', options 'Autocompact at 400k' / 'Keep auto (1m tokens)', hint 'up down choose · enter confirm · esc not now'; Esc ('not now') cleared it without changing the setting, so the hint line itself names Esc as the dismiss key.",
	},
}

// ClassifyInterstitialPrompt is the pure entry point: given a raw tmux pane
// capture, it decides which interstitial prompt (if any) is on screen and
// what a safe dismissal would be. It performs no I/O and sends no keys.
// Empty or window-bare panes, idle prompts, quoted prompt text, and every
// startup/decision dialog classify as InterstitialNone.
func ClassifyInterstitialPrompt(pane string) InterstitialPromptAction {
	window := lastNonEmptyLines(StripANSI(pane), interstitialWindowLines)
	low := strings.ToLower(window)

	none := InterstitialPromptAction{Kind: InterstitialNone}
	for _, entry := range interstitialTable {
		if !allAnchorsPresent(low, entry.anchors) {
			continue
		}
		if !hasFooterLine(window, entry.footerWords) {
			// The hint line is the prompt's own safe-key evidence; a
			// capture truncated before it is a different, unreadable
			// shape, so it must not match.
			continue
		}
		action := InterstitialPromptAction{
			Kind:           entry.kind,
			Dismiss:        append([]string{}, entry.dismiss...),
			SelectedOption: selectedOptionRow(window),
		}
		action.ChangesSettingOnEnter = entry.changesSettingOnEnter(action.SelectedOption)
		return action
	}
	return none
}

// allAnchorsPresent reports whether every anchor phrase occurs somewhere in
// the (already lower-cased) window.
func allAnchorsPresent(window string, anchors []string) bool {
	for _, a := range anchors {
		if !strings.Contains(window, a) {
			return false
		}
	}
	return true
}

// hasFooterLine reports whether one of the window's lines, after keeping
// only letters, contains every footer word. Normalizing to letters alone
// is the tolerance the matching rule promises: the hint line's arrow
// glyphs, commas, dots, middle dots and other separators do not break the
// match, because none of them are letters.
func hasFooterLine(window string, footerWords []string) bool {
	for _, line := range strings.Split(window, "\n") {
		normalized := lettersOnly(line)
		if normalized == "" {
			continue
		}
		ok := true
		for _, w := range footerWords {
			if !strings.Contains(normalized, lettersOnly(w)) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// lettersOnly keeps the letters of s (ASCII a-z), lower-cased, in order
// and drops everything else — arrows, separators, digits and spaces
// included.
func lettersOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			if r >= 'A' && r <= 'Z' {
				r += 'a' - 'A'
			}
			b.WriteRune(r)
		default:
			// Non-ASCII characters (arrows, box-drawing, middle dots) are
			// separators from the match's point of view; dropping them is
			// the conservative reading and keeps the check byte-simple.
		}
	}
	return b.String()
}

// selectedOptionRow returns the text of the window's cursor row (the line
// carrying the ❯ selector), trimmed, or "" when no cursor row is present
// or readable.
func selectedOptionRow(window string) string {
	for _, line := range strings.Split(window, "\n") {
		if !strings.Contains(line, interstitialCursorGlyph) {
			continue
		}
		rest := strings.ReplaceAll(line, interstitialCursorGlyph, " ")
		rest = strings.TrimSpace(rest)
		if rest == "" {
			// A row carrying only the glyph (or glyph + separator)
			// reads nothing; report it as unreadable rather than as
			// the empty option.
			return ""
		}
		return rest
	}
	return ""
}

// changesSettingOnEnter answers, for this table entry, whether Enter at the
// current cursor position would change a setting. Conservative rule from
// the prompt's own semantics: a cursor on the setting-changing row → true;
// a cursor that cannot be read → true (the safe default); a cursor on a
// known non-setting row → false.
func (e interstitialTableEntry) changesSettingOnEnter(selected string) bool {
	sel := strings.ToLower(strings.TrimSpace(selected))
	if sel == "" {
		return true
	}
	return strings.Contains(sel, e.settingAnchor)
}

// lastNonEmptyLines returns the last n non-empty lines of pane, joined with
// newlines.
func lastNonEmptyLines(pane string, n int) string {
	kept := make([]string, 0, n)
	for _, line := range strings.Split(pane, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		kept = append(kept, line)
		if len(kept) > n {
			kept = kept[len(kept)-n:]
		}
	}
	return strings.Join(kept, "\n")
}

// interstitialSafeKeys is the closed allow-list of dismiss keys the daemon
// may send at a recognised interstitial prompt. Today it holds exactly
// Esc, the key the recorded autocompact capture's own hint line names as
// the "not now" dismissal. A further key may be added only when a
// recorded capture's hint line names it as the dismiss key AND a test
// proves it does not select an option — never on inference from
// neighbouring TUIs.
var interstitialSafeKeys = map[string]bool{
	"esc": true,
}

// IsSafeInterstitialKey reports whether key is on the dismiss allow-list.
// Matching is case-insensitive on the trimmed key name. Everything else —
// Enter, Return, C-m, newline, carriage return, Tab, space, every digit,
// arrow keys, letters that are not the allow-listed name — is rejected.
func IsSafeInterstitialKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	return interstitialSafeKeys[k]
}
