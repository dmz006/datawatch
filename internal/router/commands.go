package router

import (
	"fmt"
	"strings"
)

// CommandType identifies the type of a parsed Signal command.
type CommandType string

const (
	CmdNew     CommandType = "new"
	CmdList    CommandType = "list"
	CmdStatus  CommandType = "status"
	CmdSend    CommandType = "send"
	CmdKill    CommandType = "kill"
	CmdTail    CommandType = "tail"
	CmdAttach  CommandType = "attach"
	CmdHistory CommandType = "history"
	CmdHelp    CommandType = "help"
	CmdUnknown CommandType = "unknown"
)

// Command is a parsed Signal message.
type Command struct {
	Type       CommandType
	SessionID  string // short or full ID
	Text       string // for new: and send:
	TailN      int    // for tail command
	ProjectDir string // for new: with explicit project directory
}

// Parse parses a Signal message text into a Command.
// Returns CmdUnknown if the message doesn't match any known command.
func Parse(text string) Command {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)

	switch {
	case strings.HasPrefix(lower, "new:"):
		rest := strings.TrimSpace(text[4:])
		// Support: "new: /absolute/path: task description"
		if strings.HasPrefix(rest, "/") {
			if idx := strings.Index(rest, ": "); idx > 0 {
				return Command{
					Type:       CmdNew,
					ProjectDir: strings.TrimSpace(rest[:idx]),
					Text:       strings.TrimSpace(rest[idx+2:]),
				}
			}
		}
		return Command{Type: CmdNew, Text: rest}

	case lower == "list":
		return Command{Type: CmdList}

	case strings.HasPrefix(lower, "status "):
		return Command{Type: CmdStatus, SessionID: strings.TrimSpace(text[7:])}

	case strings.HasPrefix(lower, "send "):
		// format: "send <id>: <text>"
		rest := text[5:]
		if idx := strings.Index(rest, ":"); idx >= 0 {
			return Command{
				Type:      CmdSend,
				SessionID: strings.TrimSpace(rest[:idx]),
				Text:      strings.TrimSpace(rest[idx+1:]),
			}
		}
		return Command{Type: CmdUnknown}

	case strings.HasPrefix(lower, "kill "):
		return Command{Type: CmdKill, SessionID: strings.TrimSpace(text[5:])}

	case strings.HasPrefix(lower, "tail "):
		// format: "tail <id> [n]"
		parts := strings.Fields(text[5:])
		cmd := Command{Type: CmdTail, TailN: 20}
		if len(parts) >= 1 {
			cmd.SessionID = parts[0]
		}
		if len(parts) >= 2 {
			fmt.Sscanf(parts[1], "%d", &cmd.TailN) //nolint:errcheck
		}
		return cmd

	case strings.HasPrefix(lower, "attach "):
		return Command{Type: CmdAttach, SessionID: strings.TrimSpace(text[7:])}

	case strings.HasPrefix(lower, "history "):
		return Command{Type: CmdHistory, SessionID: strings.TrimSpace(text[8:])}

	case lower == "help":
		return Command{Type: CmdHelp}

	default:
		return Command{Type: CmdUnknown}
	}
}

// HelpText returns the help message sent back to the messaging backend.
func HelpText(hostname string) string {
	return fmt.Sprintf(`[%s] datawatch commands:
new: <task>                     start session in default project dir
new: /path/to/project: <task>   start session in specific directory
list                            list sessions + status
status <id>                     recent output from session
send <id>: <msg>                send input to waiting session
kill <id>                       terminate session
tail <id> [n]                   last N lines of output (default 20)
attach <id>                     get tmux attach command
history <id>                    git log of session tracking folder
help                            show this help`, hostname)
}
