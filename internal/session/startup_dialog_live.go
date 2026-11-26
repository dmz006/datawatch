// Whether a start-up dialog is really on the screen. classifyStartupDialog
// says which dialog a text names and which key answers it; it cannot tell a
// dialog from output or a task that quotes one. A key is sent only for a
// dialog with a dialog's shape at the bottom of the live screen.

package session

import (
	"regexp"
	"strings"
)

// startupDialogMaxLines is how many lines above its footer a dialog may be.
const startupDialogMaxLines = 40

var (
	// dialogFooterLine is the whole last line of a Claude Code dialog, with an
	// optional key hint on either side and nothing else.
	dialogFooterLine = regexp.MustCompile(`^[\s│┃|]*(?:[↑↓←→][^·•|]{0,30}[·•|]\s*)?\(?(?:press )?enter to confirm\)?\s*(?:[·•|,]|or)?\s*\(?(?:esc to (?:cancel|exit|reject all|reject|go back|quit|skip|dismiss)|esc not now)\)?(?:\s*[·•|]\s*[↑↓←→a-z+ -]{0,30})*[\s│┃|]*$`)
	// dialogOptionLine splits one line of a dialog: group 1 is the selector,
	// group 2 the option's number, group 3 its text.
	dialogOptionLine = regexp.MustCompile(`^[\s│┃|]*(❯[\s\x{00a0}]*)?(\d+[.)][\s\x{00a0}]+)?(.*?)[\s│┃|]*$`)
	// boxPromptLine is the older input prompt, drawn inside a box.
	boxPromptLine = regexp.MustCompile(`^\s*[│┃|]\s*>(\s|$)`)
	// sessionMarks are drawn by a running session, never inside a dialog.
	sessionMarks = []string{"⏵⏵", "? for shortcuts", "esc to interrupt"}
)

// isBoxBottom reports the lower edge of a drawn box. A plain rule is not one:
// the input box ends with a rule, so a rule below a footer means the footer is
// text in that box.
func isBoxBottom(l string) bool {
	l = strings.TrimSpace(l)
	return (strings.HasPrefix(l, "╰") || strings.HasPrefix(l, "└")) && strings.Trim(l, " ─━╰╯└┘┴") == ""
}

// liveDialogBlock returns the dialog that ends the screen: the lines from its
// start to its footer. opened reports that a rule or a box starts it. ok is
// false when the screen does not end with a dialog footer.
func liveDialogBlock(pane string) (block []string, opened, ok bool) {
	lines := strings.Split(strings.ReplaceAll(StripANSI(pane), "\r", ""), "\n")
	end := len(lines) - 1
	for end >= 0 && (strings.TrimSpace(lines[end]) == "" || isBoxBottom(lines[end])) {
		end--
	}
	if end < 0 || !dialogFooterLine.MatchString(strings.ToLower(lines[end])) {
		return nil, false, false
	}
	start, seen := end, 0
	for start > 0 && seen < startupDialogMaxLines {
		above := strings.TrimSpace(lines[start-1])
		if isBoxSeparator(above) {
			opened = true
			break
		}
		start--
		if above != "" {
			seen++
		}
		if strings.HasPrefix(above, "╭") || strings.HasPrefix(above, "┌") {
			opened = true
		}
	}
	return lines[start : end+1], opened, true
}

// startupScreen is a start-up dialog as the screen shows it.
type startupScreen struct {
	action StartupDialogAction
	// text is the dialog's own lines; two readings of an unchanged dialog have
	// the same text.
	text string
	// selected is the option under the cursor, lower case, "" when the dialog
	// lists none; selectedNumber is its number, "" when it has none.
	selected, selectedNumber string
}

// liveStartupDialog returns the start-up dialog on the screen pane shows, and
// whether there is one. The kind and keys are classifyStartupDialog's, read
// from the dialog alone.
func liveStartupDialog(pane string) (StartupDialogAction, bool) {
	d, ok := readStartupScreen(pane)
	if !ok {
		return StartupDialogAction{Kind: StartupDialogNone, Keys: startupKeysNone}, false
	}
	return d.action, true
}

// readStartupScreen is liveStartupDialog with what the screen shows of it.
func readStartupScreen(pane string) (startupScreen, bool) {
	var none startupScreen
	if PaneShowsApprovalPrompt(pane) {
		return none, false
	}
	block, opened, ok := liveDialogBlock(pane)
	if !ok {
		return none, false
	}
	trimmed := make([]string, len(block))
	for i, l := range block {
		trimmed[i] = strings.TrimRight(l, " \t")
	}
	text := strings.Join(trimmed, "\n")
	if containsAnyPhrase(strings.ToLower(text), sessionMarks...) {
		return none, false
	}
	// Options may be numbered or not. numbered counts the numbered ones;
	// selected is the text under the cursor, and cursors counts the cursors
	// that are not on a numbered option, as an input prompt's is not.
	var options []string
	selected, selectedNumber, numbered, cursors, bare := "", "", 0, 0, 0
	for _, l := range block {
		if boxPromptLine.MatchString(l) {
			return none, false
		}
		part := dialogOptionLine.FindStringSubmatch(l)
		if part == nil {
			continue
		}
		option := strings.ToLower(strings.Trim(part[3], " \t\u00a0"))
		if option == "" {
			continue
		}
		options = append(options, option)
		if part[2] != "" {
			numbered++
		}
		if part[1] != "" {
			selected = option
			selectedNumber = strings.TrimRight(strings.Trim(part[2], " \t\u00a0"), ".)")
			cursors++
			if part[2] == "" {
				bare++
			}
		}
	}
	has := func(prefix string) bool {
		for _, o := range options {
			if strings.HasPrefix(o, prefix) {
				return true
			}
		}
		return false
	}
	// on reports one cursor, on an option that starts with one of prefixes.
	on := func(prefixes ...string) bool {
		for _, p := range prefixes {
			if cursors == 1 && strings.HasPrefix(selected, p) {
				return true
			}
		}
		return false
	}
	menu := cursors == 1 && bare == 0 && numbered >= 2

	a := classifyStartupDialog(text)
	shaped := false
	switch a.Kind {
	case StartupDialogTrustFolder:
		shaped = has("yes") && has("no") && on("yes", "no")
	case StartupDialogBypassAccept:
		shaped = has("yes, i accept") && has("no, exit") && on("yes, i accept", "no, exit")
	case StartupDialogDevChannels:
		shaped = has("i am using this for local development") && on("i am using this for local development", "exit", "no, exit")
	case StartupDialogMCPTrust:
		shaped = menu
	case StartupDialogConfirmContinue, StartupDialogConfirmUnsafe,
		StartupDialogUnknownConfirm, StartupDialogUpdatePrompt:
		// A plain confirmation's choices are in its footer; its start is
		// marked instead. A cursor on unnumbered text is an input prompt.
		shaped = menu || (opened && bare == 0)
	}
	if !shaped {
		return none, false
	}
	return startupScreen{action: a, text: text, selected: selected, selectedNumber: selectedNumber}, true
}

// LiveStartupDialog returns the start-up dialog on the live screen of session
// fullID, from one reading of it. The live screen is read even when someone has
// scrolled the pane back.
func (m *Manager) LiveStartupDialog(fullID string) (StartupDialogAction, bool) {
	sess, ok := m.store.Get(fullID)
	if !ok || sess.TmuxSession == "" {
		return StartupDialogAction{Kind: StartupDialogNone}, false
	}
	return liveStartupDialog(m.liveScreen(sess.TmuxSession))
}

func (m *Manager) liveScreen(tmuxSession string) string {
	pane, err := m.tmux.CapturePaneLiveTail(tmuxSession)
	if err != nil {
		pane, _ = m.tmux.CapturePaneVisible(tmuxSession)
	}
	return pane
}
