# Recorded Claude Code panes (task delivery fixtures)

Each `.txt` file is a real `tmux capture-pane -p` text of a Claude Code screen,
rendered from the session's recorded output (or captured from a live pane) and
sanitized: session ids, host names, user names and paths are replaced, and the
long wrapped launch command echoed above the banner is condensed to one line.
Nothing else is edited. The input row uses a no-break space after `❯` (U+00A0),
exactly as the CLI draws it; do not turn it into a plain space.

| File | Screen |
|------|--------|
| `claude-2.1.331-dev-channels-dialog.txt` | development-channels consent dialog, still waiting for an answer |
| `claude-2.1.331-startup-idle.txt` | startup done: Channels notice, empty box with the placeholder `Try "write a test for <filepath>"` |
| `claude-2.1.330-startup-idle-placeholder.txt` | same, other version, other placeholder text |
| `claude-2.1.330-unsubmitted-paste.txt` | the task is in the box as eight `[Pasted text #N +M lines]` chips, Enter never took effect |
| `claude-2.1.331-working.txt` | mid-turn: spinner, running tool line, `esc to interrupt` in the footer |
