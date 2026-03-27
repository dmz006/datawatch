package router

import (
	"fmt"
	"strings"
)

// CommandType identifies the type of a parsed Signal command.
type CommandType string

const (
	CmdNew         CommandType = "new"
	CmdList        CommandType = "list"
	CmdStatus      CommandType = "status"
	CmdSend        CommandType = "send"
	CmdKill        CommandType = "kill"
	CmdTail        CommandType = "tail"
	CmdAttach      CommandType = "attach"
	CmdHistory     CommandType = "history"
	CmdSetup       CommandType = "setup"
	CmdVersion     CommandType = "version"
	CmdUpdateCheck CommandType = "update"
	CmdSchedule    CommandType = "schedule"
	CmdAlerts      CommandType = "alerts"
	CmdHelp        CommandType = "help"
	CmdUnknown     CommandType = "unknown"
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

	case strings.HasPrefix(lower, "setup ") || lower == "setup":
		return Command{Type: CmdSetup, Text: strings.TrimSpace(text[5:])}

	case lower == "version":
		return Command{Type: CmdVersion}

	case lower == "update check" || lower == "update":
		return Command{Type: CmdUpdateCheck}

	case strings.HasPrefix(lower, "schedule "):
		// format: "schedule <id>: <when> <command>"
		// e.g. "schedule a3f2: now yes" or "schedule a3f2: 14:00 run tests"
		rest := text[9:]
		if idx := strings.Index(rest, ":"); idx >= 0 {
			sessionID := strings.TrimSpace(rest[:idx])
			remainder := strings.TrimSpace(rest[idx+1:])
			// Split remainder into when and command
			parts := strings.SplitN(remainder, " ", 2)
			when := ""
			cmd := ""
			if len(parts) >= 1 {
				when = strings.TrimSpace(parts[0])
			}
			if len(parts) >= 2 {
				cmd = strings.TrimSpace(parts[1])
			}
			return Command{Type: CmdSchedule, SessionID: sessionID, Text: when + " " + cmd}
		}
		return Command{Type: CmdUnknown}

	case lower == "alerts" || strings.HasPrefix(lower, "alerts "):
		n := 5
		if lower != "alerts" {
			fmt.Sscanf(strings.TrimSpace(text[6:]), "%d", &n) //nolint:errcheck
		}
		return Command{Type: CmdAlerts, TailN: n}

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
schedule <id>: <when> <cmd>     schedule a command (when: now, HH:MM, or cancel <schedID>)
alerts [n]                      show last N alerts (default 5)
setup <service>                 configure a backend (telegram/discord/.../llm/session/mcp)
version                         show datawatch version
update check                    check for available updates
help                            show this help`, hostname)
}
