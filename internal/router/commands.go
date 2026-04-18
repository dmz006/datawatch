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
	CmdRestart     CommandType = "restart"
	CmdSchedule    CommandType = "schedule"
	CmdAlerts      CommandType = "alerts"
	CmdStats       CommandType = "stats"
	CmdConfigure   CommandType = "configure"
	CmdCopy        CommandType = "copy"
	CmdPrompt      CommandType = "prompt"
	CmdRemember    CommandType = "remember"
	CmdRecall      CommandType = "recall"
	CmdMemories    CommandType = "memories"
	CmdForget      CommandType = "forget"
	CmdLearnings   CommandType = "learnings"
	CmdKG          CommandType = "kg"
	CmdMemReindex  CommandType = "mem_reindex"
	CmdResearch    CommandType = "research"
	CmdPipeline    CommandType = "pipeline"
	CmdHelp        CommandType = "help"
	// F10 sprint 2: read-only profile access over chat. Create/update/
	// delete deliberately NOT exposed here — too risky for a chat channel
	// to mint profiles; those stay on the API/MCP/CLI/UI paths.
	CmdProfile     CommandType = "profile"
	CmdUnknown     CommandType = "unknown"
)

// ProfileKind / ProfileVerb values set on a CmdProfile command.
const (
	ProfileKindProject = "project"
	ProfileKindCluster = "cluster"
	ProfileVerbList    = "list"
	ProfileVerbShow    = "show"
	ProfileVerbSmoke   = "smoke"
)

// Command is a parsed Signal message.
type Command struct {
	Type       CommandType
	SessionID  string // short or full ID
	Text       string // for new: and send:
	TailN      int    // for tail command
	ProjectDir string // for new: with explicit project directory
	Profile    string // named profile for "new <profile>: <task>"
	Server     string // target remote server for "new @server: <task>"

	// F10 sprint 2 — CmdProfile fields.
	ProfileKind string // "project" | "cluster"
	ProfileVerb string // "list" | "show" | "smoke"
	ProfileName string // required for show / smoke
}

// Parse parses a Signal message text into a Command.
// Returns CmdUnknown if the message doesn't match any known command.
func Parse(text string) Command {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)

	switch {
	case strings.HasPrefix(lower, "new:"):
		rest := strings.TrimSpace(text[4:])
		// Support: "new: @server: task" — route to remote server
		if strings.HasPrefix(rest, "@") {
			if idx := strings.Index(rest, ": "); idx > 0 {
				return Command{
					Type:   CmdNew,
					Server: strings.TrimSpace(rest[1:idx]),
					Text:   strings.TrimSpace(rest[idx+2:]),
				}
			}
		}
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

	case lower == "list" || strings.HasPrefix(lower, "list "):
		cmd := Command{Type: CmdList}
		if lower != "list" {
			cmd.Text = strings.TrimSpace(text[5:])
		}
		return cmd

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

	case lower == "version" || lower == "about":
		return Command{Type: CmdVersion}

	case lower == "restart":
		return Command{Type: CmdRestart}

	case lower == "update check" || lower == "update":
		return Command{Type: CmdUpdateCheck}

	// F10: read-only profile access
	//   "profile project list"
	//   "profile cluster list"
	//   "profile project show <name>"
	//   "profile cluster smoke <name>"
	case lower == "profile" || strings.HasPrefix(lower, "profile "):
		rest := strings.TrimSpace(strings.TrimPrefix(text, "profile"))
		parts := strings.Fields(rest)
		if len(parts) < 1 {
			return Command{Type: CmdProfile} // will render help text
		}
		if len(parts) < 2 {
			// Just "profile project" → remember the kind but no verb
			if k := strings.ToLower(parts[0]); k == ProfileKindProject || k == ProfileKindCluster {
				return Command{Type: CmdProfile, ProfileKind: k}
			}
			return Command{Type: CmdProfile, Text: "invalid kind: " + parts[0]}
		}
		kind := strings.ToLower(parts[0])
		verb := strings.ToLower(parts[1])
		if kind != ProfileKindProject && kind != ProfileKindCluster {
			return Command{Type: CmdProfile, Text: "invalid kind: " + kind}
		}
		cmd := Command{Type: CmdProfile, ProfileKind: kind, ProfileVerb: verb}
		if verb == ProfileVerbShow || verb == ProfileVerbSmoke {
			if len(parts) < 3 {
				cmd.Text = verb + " requires a profile name"
				return cmd
			}
			cmd.ProfileName = parts[2]
		}
		return cmd

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

	case lower == "stats":
		return Command{Type: CmdStats}

	case strings.HasPrefix(lower, "configure ") || strings.HasPrefix(lower, "config ") || strings.HasPrefix(lower, "set "):
		rest := text[strings.Index(lower, " ")+1:]
		return Command{Type: CmdConfigure, Text: strings.TrimSpace(rest)}

	case lower == "copy" || strings.HasPrefix(lower, "copy "):
		id := ""
		if lower != "copy" {
			id = strings.TrimSpace(text[5:])
		}
		return Command{Type: CmdCopy, SessionID: id}

	case lower == "prompt" || strings.HasPrefix(lower, "prompt "):
		id := ""
		if lower != "prompt" {
			id = strings.TrimSpace(text[7:])
		}
		return Command{Type: CmdPrompt, SessionID: id}

	case strings.HasPrefix(lower, "remember:") || strings.HasPrefix(lower, "remember "):
		rest := strings.TrimSpace(text[strings.Index(lower, " ")+1:])
		if strings.HasPrefix(lower, "remember:") {
			rest = strings.TrimSpace(text[9:])
		}
		return Command{Type: CmdRemember, Text: rest}

	case strings.HasPrefix(lower, "recall:") || strings.HasPrefix(lower, "recall "):
		rest := strings.TrimSpace(text[strings.Index(lower, " ")+1:])
		if strings.HasPrefix(lower, "recall:") {
			rest = strings.TrimSpace(text[7:])
		}
		return Command{Type: CmdRecall, Text: rest}

	case lower == "memories reindex":
		return Command{Type: CmdMemReindex}

	case lower == "memories tunnels":
		return Command{Type: CmdMemories, Text: "__tunnels__"}

	case lower == "memories" || strings.HasPrefix(lower, "memories "):
		n := 10
		rest := ""
		if lower != "memories" {
			rest = strings.TrimSpace(text[9:])
		}
		// Subcommands: stats, export, tunnels, reindex, or a number
		if rest == "stats" || rest == "export" || rest == "tunnels" || rest == "reindex" {
			return Command{Type: CmdMemories, Text: rest}
		}
		if rest != "" {
			fmt.Sscanf(rest, "%d", &n) //nolint:errcheck
		}
		return Command{Type: CmdMemories, TailN: n}

	case strings.HasPrefix(lower, "forget "):
		return Command{Type: CmdForget, Text: strings.TrimSpace(text[7:])}

	case lower == "learnings" || strings.HasPrefix(lower, "learnings "):
		rest := ""
		if lower != "learnings" {
			rest = strings.TrimSpace(text[10:])
		}
		return Command{Type: CmdLearnings, Text: rest}

	case strings.HasPrefix(lower, "kg ") || lower == "kg":
		rest := ""
		if lower != "kg" {
			rest = strings.TrimSpace(text[3:])
		}
		return Command{Type: CmdKG, Text: rest}

	case strings.HasPrefix(lower, "research:") || strings.HasPrefix(lower, "research "):
		rest := strings.TrimSpace(text[9:])
		if strings.HasPrefix(lower, "research ") { rest = strings.TrimSpace(text[9:]) }
		return Command{Type: CmdResearch, Text: rest}

	case strings.HasPrefix(lower, "pipeline:") || strings.HasPrefix(lower, "pipeline "):
		sep := 9
		if strings.HasPrefix(lower, "pipeline ") { sep = 9 }
		return Command{Type: CmdPipeline, Text: strings.TrimSpace(text[sep:])}

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
list [--active|--inactive|--all] list sessions (default: all)
status <id>                     recent output from session
send <id>: <msg>                send input to waiting session
kill <id>                       terminate session
tail <id> [n]                   last N lines of output (default 20)
attach <id>                     get tmux attach command
history <id>                    git log of session tracking folder
schedule <id>: <when> <cmd>     schedule a command (when: now, HH:MM, or cancel <schedID>)
alerts [n]                      show last N alerts (default 5)
stats                           show system statistics (CPU, memory, disk, sessions)
configure <key>=<value>         set a config value (e.g. session.console_cols=120)
configure list                  show common configurable settings
setup <service>                 configure a backend (telegram/discord/.../llm/session/mcp)
version / about                 show datawatch version and info
restart                         restart the datawatch daemon
update check                    check for available updates
copy [id]                       get last LLM response (default: most recent session)
prompt [id]                     get last user prompt sent to a session
remember: <text>                save a memory for the current project
recall: <query>                 semantic search across memories
memories [n]                    list recent memories (default 10)
forget <id>                     delete a memory by ID
learnings [search: <query>]     list or search task learnings
memories reindex                re-embed all memories after model change
memories tunnels                show cross-project room connections
kg query <entity>               knowledge graph — query entity relationships
kg add <subj> <pred> <obj>      add a relationship triple
kg timeline <entity>            chronological entity history
kg stats                        knowledge graph statistics
research: <query>               deep search across all sessions, memories, and KG
pipeline: t1 -> t2 -> t3        chain tasks in a pipeline
pipeline status [id]             show pipeline status
pipeline cancel <id>             cancel a pipeline
help                            show this help`, hostname)
}
