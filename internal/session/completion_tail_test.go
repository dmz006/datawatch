package session

import (
	"strings"
	"testing"
)

const tailBox = "\n────────────────────────────── build-job ─\n❯ \n──────────────────────────────\n" +
	"  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents                  ● high · /effort\n"

func TestOutputEndsWithCompletion(t *testing.T) {
	marker := "● DATAWATCH_COMPLETE: built the change and wrote the report"
	for name, c := range map[string]struct {
		pane string
		want bool
	}{
		"finished turn above the input box":  {marker + "\n\n✻ Worked for 4m 2s\n" + tailBox, true},
		"wrapped summary":                    {"● DATAWATCH_COMPLETE: built the change and\n  wrote the report\n" + tailBox, true},
		"marker at the end of a longer turn": {"● All done.\n\n  DATAWATCH_COMPLETE: wrote the report\n" + tailBox, true},
		"recap, tip and survey after it": {marker + "\n\n※ recap: the build is done and\n  the report is written\n\n" +
			"  ⎿  Tip: Use /clear\n\n● How is Claude doing this session? (optional)\n  1: Bad    2: Fine   3: Good   0: Dismiss\n" + tailBox, true},
		"agent exited to the shell": {"DATAWATCH_COMPLETE: claude done\nuser@host:~/work$\n", true},
		"new input after it":        {marker + "\n\n❯ supervisor wake: automaton.stuck_running automaton=a1\n\n● Looking now.\n" + tailBox, false},
		"new answer after it":       {marker + "\n\n● Continuing with the next job.\n" + tailBox, false},
		"short new input after it":  {marker + "\n\n❯ go\n" + tailBox, false},
		"working after it":          {marker + "\n\n✻ Crunching… (12s)\n" + tailBox, false},
		"quoted line, then prose":   {"● The build reported:\n\n  DATAWATCH_COMPLETE: built it\n\n  I will land it next.\n" + tailBox, false},
		// Both shapes below were seen on finished sessions: Claude Code draws
		// them itself, with no model turn.
		"local slash command after it": {marker + "\n\n❯ /effort high\n  ⎿  Set effort level to high (saved as your default for new sessions): Comprehensive\n" +
			"     testing and documentation\n\n✻ Cooked for 26m 48s · done 4:00 PM\n" + tailBox, true},
		"idle compaction after it": {marker + "\n\n✻ Baked for 1m 45s · done 4:20 PM\n  ⎿ \u00a0Read ../../scratchpad/long-folder-name-3\n" +
			"     7353d/test-run1.log (10 lines)\n  ⎿ \u00a0Referenced file CLAUDE.md\n  ⎿ \u00a0Referenced file ../../tmp/a-very-long-name-b\n     77a/notes.txt\n" +
			"  ⎿ \u00a0Read ../job.md (13 lines)\n\n● Compacted while idle, before the prompt cache expired · turn off with \"idleCompaction\": false in\n  settings.json\n" + tailBox, true},
		"slash command that starts a turn": {marker + "\n\n❯ /ship\n\n● Committing the work now.\n" + tailBox, false},
		"tool call after it":               {marker + "\n\n● Read(report.md)\n  ⎿  Read 49 lines\n" + tailBox, false},
		"tool result alone after it":       {marker + "\n\n  ⎿  Tests passed in 4 packages\n" + tailBox, false},
		"no marker":                        {"● Checking the lanes now.\n" + tailBox, false},
		"instruction placeholder":          {"● I will print\n  DATAWATCH_COMPLETE: <one-line summary of what was done>\n" + tailBox, false},
		"typed in the input box": {"● Ready.\n\n──────────────────────────────\n❯ DATAWATCH_COMPLETE: typed by hand\n" +
			"──────────────────────────────\n", false},
		"echoed launch command":                      {"$ claude; echo 'DATAWATCH_COMPLETE: claude done'\n", false},
		"task text wrapped to start with the marker": {"❯ end with a line that starts with\n  DATAWATCH_COMPLETE: in the report and the message\n\n● Reading the job file.\n" + tailBox, false},
		"empty": {"", false},
	} {
		if got := OutputEndsWithCompletion(c.pane); got != c.want {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
		if got := OutputEndsWithCompletion(strings.ReplaceAll(c.pane, "\n", "\r\n")); got != c.want {
			t.Errorf("%s (CRLF): got %v, want %v", name, got, c.want)
		}
	}
}
