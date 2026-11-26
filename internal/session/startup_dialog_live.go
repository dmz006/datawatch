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
	// dialogChoiceRow is one numbered choice; group 1 is the selector.
	dialogChoiceRow = regexp.MustCompile(`^[\s│┃|]*(❯[\s\x{00a0}]*)?\d+[.)][\s\x{00a0}]+(\S.*?)[\s│┃|]*$`)
	// sessionMarks are drawn by a running session, never inside a dialog.
	sessionMarks = []string{"⏵⏵", "? for shortcuts", "esc to interrupt"}
)

// isBorderLine reports a line made only of box border characters.
func isBorderLine(l string) bool {
	l = strings.TrimSpace(l)
	return l != "" && strings.Trim(l, " ─━│┃╭╮╰╯└┘┌┐├┤") == ""
}

// liveDialogBlock returns the dialog that ends the screen: the lines from its
// start to its footer. opened reports that a rule or a box starts it. ok is
// false when the screen does not end with a dialog footer.
func liveDialogBlock(pane string) (block []string, opened, ok bool) {
	lines := strings.Split(strings.ReplaceAll(StripANSI(pane), "\r", ""), "\n")
	end := len(lines) - 1
	for end >= 0 && (strings.TrimSpace(lines[end]) == "" || isBorderLine(lines[end])) {
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

// liveStartupDialog returns the start-up dialog on the screen pane shows, and
// whether there is one. The kind and keys are classifyStartupDialog's, read
// from the dialog alone.
func liveStartupDialog(pane string) (StartupDialogAction, bool) {
	none := StartupDialogAction{Kind: StartupDialogNone, Keys: startupKeysNone}
	if PaneShowsApprovalPrompt(pane) {
		return none, false
	}
	block, opened, ok := liveDialogBlock(pane)
	if !ok {
		return none, false
	}
	text := strings.Join(block, "\n")
	if containsAnyPhrase(strings.ToLower(text), sessionMarks...) {
		return none, false
	}
	var choices []string
	selected := false
	for _, l := range block {
		if row := dialogChoiceRow.FindStringSubmatch(l); row != nil {
			choices = append(choices, strings.ToLower(row[2]))
			selected = selected || row[1] != ""
		}
	}
	menu := selected && len(choices) >= 2
	choice := func(has func(string) bool) bool {
		for _, c := range choices {
			if has(c) {
				return true
			}
		}
		return false
	}
	starts := func(p string) bool { return choice(func(c string) bool { return strings.HasPrefix(c, p) }) }
	holds := func(p string) bool { return choice(func(c string) bool { return strings.Contains(c, p) }) }

	a := classifyStartupDialog(text)
	shaped := false
	switch a.Kind {
	case StartupDialogTrustFolder:
		shaped = menu && starts("yes") && starts("no")
	case StartupDialogBypassAccept:
		shaped = menu && holds("yes, i accept") && holds("no, exit")
	case StartupDialogDevChannels:
		shaped = menu && holds("local development")
	case StartupDialogMCPTrust:
		shaped = menu
	case StartupDialogConfirmContinue, StartupDialogConfirmUnsafe,
		StartupDialogUnknownConfirm, StartupDialogUpdatePrompt:
		// A plain confirmation's choices are in its footer; its start is
		// marked instead.
		shaped = menu || opened
	}
	if !shaped {
		return none, false
	}
	return a, true
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
