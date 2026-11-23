// Package tmuxscope holds the unscoped tmux kill check. It is a leaf package
// (standard library only) so both internal/config and internal/mergepipeline
// can use it without an import cycle.
package tmuxscope

import "regexp"

var unscopedTmuxKill = regexp.MustCompile(`tmux([ \t]+[^|;&]*)?[ \t]+(kill-server|kill-session([ \t]+[^|;&]*)?[ \t]-[A-Za-z]*a)`)

// tmuxKillScoped matches the accepted scopings within one simple command: an
// explicit socket on the tmux call, or a scrubbed environment prefixing it.
var tmuxKillScoped = regexp.MustCompile(`tmux[ \t]+(-[A-Za-z]*S[ \t]|-L[ \t])|env[ \t]+(-u[ \t]+TMUX[ \t]+-u[ \t]+TMUX_TMPDIR|-u[ \t]+TMUX_TMPDIR[ \t]+-u[ \t]+TMUX)[ \t][^|;&\n]*tmux`)

// commandSeparators splits a command line into simple commands.
var commandSeparators = regexp.MustCompile(`[;&|\n]+`)

// CallsUnscopedTmuxKill reports whether any simple command in command kills
// tmux without an explicit throwaway socket or a scrubbed environment on that
// same command. A scoping marker on another command does not count.
func CallsUnscopedTmuxKill(command string) bool {
	for _, seg := range commandSeparators.Split(command, -1) {
		if unscopedTmuxKill.MatchString(seg) && !tmuxKillScoped.MatchString(seg) {
			return true
		}
	}
	return false
}
