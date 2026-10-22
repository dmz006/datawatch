package session

import (
	"strings"
)

// StartupKind names the recognised startup dialog shapes that a fresh
// Claude Code session can present in its pane before it reaches an idle
// prompt. The values are a closed set; the daemon maps each one to a
// specific keystroke sequence via StartupDialogAction.
type StartupKind string

const (
	StartupDialogNone            StartupKind = "none"
	StartupDialogTrustFolder     StartupKind = "trust-folder"
	StartupDialogBypassAccept    StartupKind = "bypass-accept"
	StartupDialogDevChannels     StartupKind = "dev-channels"
	StartupDialogMCPTrust        StartupKind = "mcp-trust"
	StartupDialogConfirmContinue StartupKind = "confirm-continue"
	StartupDialogConfirmUnsafe   StartupKind = "confirm-unsafe"
	StartupDialogUpdatePrompt    StartupKind = "update-prompt"
	StartupDialogUnknownConfirm  StartupKind = "unknown-confirm"
)

// StartupDialogAction is the pure decision for one captured pane: which
// dialog kind is on screen, which keys to send in order (empty slice means
// send nothing), and whether the daemon must treat the session as a start
// failure after acting (currently: after sending Esc to an unsafe dialog).
type StartupDialogAction struct {
	Kind  StartupKind
	Keys  []string
	Retry bool
}

// Precomputed key sequences. Empty slice is the "send nothing" action.
var (
	startupKeysNone      = []string{}
	startupKeysDownEnter = []string{"Down", "Enter"}
	startupKeysEnter     = []string{"Enter"}
	startupKeys1Enter    = []string{"1", "Enter"}
	startupKeys2Enter    = []string{"2", "Enter"}
	startupKeysEsc       = []string{"Esc"}
)

// startupConfirmUnsafePhrases mark a confirm-footer question that must be
// declined rather than accepted: anything touching updates, settings or
// permission state is out of scope for silent auto-accept.
var startupConfirmUnsafePhrases = []string{
	"update", "upgrade", "install", "settings",
	"auto-update", "permission mode", "configuration",
}

// startupConfirmSafePhrases mark a confirm-footer question that is a
// normal startup continuation (trust/continue/accept) and may be accepted
// with Enter.
var startupConfirmSafePhrases = []string{
	"trust", "continue", "proceed", "accept", "channels",
}

// classifyStartupDialog is a pure classifier for a raw tmux pane capture.
// It strips ANSI (reusing StripANSI) and matches wording case-insensitively.
//
// Order of precedence:
//  1. folder-trust arrow dialog          -> trust-folder, Down then Enter
//     (same sequence as SendTrustPromptAccept)
//  2. bypass-permissions disclaimer      -> bypass-accept, "2" then Enter
//  3. development-channels consent       -> dev-channels, Enter
//  4. MCP server trust prompt            -> mcp-trust, "1" then Enter
//  5. "Enter to confirm / Esc to cancel" -> question above decides:
//     update/settings/permission wording -> confirm-unsafe, Esc, Retry
//     trust/continue/accept wording      -> confirm-continue, Enter
//     otherwise                          -> unknown-confirm, send nothing
//  6. visible update banner, no footer   -> update-prompt, Esc, Retry
//  7. anything else (incl. idle prompt)  -> none
func classifyStartupDialog(pane string) StartupDialogAction {
	plain := strings.ToLower(StripANSI(pane))

	switch {
	case containsAnyPhrase(plain,
		"quick safety check", "trust this folder", "yes, i trust"):
		// Arrow-key TUI with "No, exit" pre-selected: Down moves the
		// selector onto the accept option, Enter confirms. This mirrors
		// SendTrustPromptAccept so a caller can drive both with Keys.
		return StartupDialogAction{Kind: StartupDialogTrustFolder, Keys: startupKeysDownEnter}

	case strings.Contains(plain, "yes, i accept") && strings.Contains(plain, "no, exit"):
		// Numbered disclaimer: option 2 is the accept line.
		return StartupDialogAction{Kind: StartupDialogBypassAccept, Keys: startupKeys2Enter}

	case containsAnyPhrase(plain,
		"i am using this for local development", "loading development channels"):
		return StartupDialogAction{Kind: StartupDialogDevChannels, Keys: startupKeysEnter}

	case containsAnyPhrase(plain,
		"new mcp server found", "use this mcp server"):
		// Numbered trust prompt: option 1 is the trust line.
		return StartupDialogAction{Kind: StartupDialogMCPTrust, Keys: startupKeys1Enter}

	case strings.Contains(plain, "enter to confirm") && strings.Contains(plain, "esc to cancel"):
		question := textAboveConfirmFooter(plain)
		// Fail-safe: check the unsafe wording first so a question that
		// mentions both an accepting verb and an update/settings change is
		// still declined, never entered.
		if containsAnyPhrase(question, startupConfirmUnsafePhrases...) {
			return StartupDialogAction{Kind: StartupDialogConfirmUnsafe, Keys: startupKeysEsc, Retry: true}
		}
		if containsAnyPhrase(question, startupConfirmSafePhrases...) {
			return StartupDialogAction{Kind: StartupDialogConfirmContinue, Keys: startupKeysEnter}
		}
		// Do not guess: send nothing and let the silent-start detector
		// flag the session as a start failure later.
		return StartupDialogAction{Kind: StartupDialogUnknownConfirm, Keys: startupKeysNone}

	case containsAnyPhrase(plain, "update available", "update now", "new version"):
		return StartupDialogAction{Kind: StartupDialogUpdatePrompt, Keys: startupKeysEsc, Retry: true}

	default:
		return StartupDialogAction{Kind: StartupDialogNone, Keys: startupKeysNone}
	}
}

func containsAnyPhrase(text string, phrases ...string) bool {
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// dialogBoxDrawing is the set of Unicode box-drawing and decorative
// characters Claude Code frame lines are made of.
const dialogBoxDrawing = "─━│┃┌┍┎┏┐┑┒┓└┕┖┗┘┙┚┛├┝┞┟┠┡┢┣┤┥┦┧┨┩┪┫┬┭┮┯┰┱┲┳┴┵┶┷┸┹┺┻┼┽┾┿╀╁╂╃╄╅╆╇╈╉╊╋╌╍╎╭╮╯╰❯✦·"

// textAboveConfirmFooter returns the readable question text that sits above
// the first "enter to confirm" footer line, skipping purely decorative
// frame lines. Callers pass the already lower-cased, ANSI-stripped pane.
func textAboveConfirmFooter(plain string) string {
	lines := strings.Split(plain, "\n")
	footerIdx := -1
	for i, l := range lines {
		if strings.Contains(l, "enter to confirm") {
			footerIdx = i
			break
		}
	}
	if footerIdx <= 0 {
		return ""
	}
	const window = 8 // number of lines above the footer to consider
	var b strings.Builder
	for i := footerIdx - 1; i >= 0 && i >= footerIdx-window; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if isFrameOnlyLine(line) {
			continue
		}
		if strings.Contains(line, "enter to confirm") || strings.Contains(line, "esc to cancel") {
			continue
		}
		line = strings.TrimPrefix(line, "❯ ")
		line = strings.TrimSpace(line)
		if line != "" {
			b.WriteString(line)
			b.WriteString(" ")
		}
	}
	return strings.ToLower(b.String())
}

func isFrameOnlyLine(line string) bool {
	for _, r := range line {
		if r == ' ' || r == '\t' {
			continue
		}
		if !strings.ContainsRune(dialogBoxDrawing, r) {
			return false
		}
	}
	return true
}
