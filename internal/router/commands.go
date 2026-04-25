package router

import (
	"fmt"
	"strconv"
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
	// F10 sprint 3: agent operations over chat. Spawn + list + show +
	// logs + kill all exposed — a typo here is recoverable (kill the
	// wrong agent, try again), unlike accidentally editing a profile.
	CmdAgent       CommandType = "agent"
	// F10 sprint 3.6: bind a session to a parent-spawned worker so
	// reads forward through the agent reverse proxy. Pass agent_id
	// of "" to unbind (we represent that on the wire as a literal
	// "-" or empty 2nd token; both accepted by the parser).
	CmdBind        CommandType = "bind"
	// BL93/BL94: orphan session reconciler + import.
	//   "session reconcile"        — dry-run list of orphans
	//   "session reconcile apply"  — import every orphan
	//   "session import <id-or-dir>" — import a single dir
	CmdSession     CommandType = "session"

	// Sprint Sx2 (v3.7.3) — comm-channel parity for v3.5–v3.7 REST
	// endpoints. The `rest` command pipes any HTTP verb+path through
	// the local loopback so every REST surface is reachable from chat.
	// Convenience shortcuts (cost / cooldown / audit / stale) pipe
	// through the same dispatcher with curated paths.
	CmdRest        CommandType = "rest"
	CmdCost        CommandType = "cost"
	CmdCooldown    CommandType = "cooldown"
	CmdStale       CommandType = "stale"
	CmdAudit       CommandType = "audit"

	// BL172 (S11) — federated observer peers. List + show + register +
	// delete a Shape B / C peer over chat. Mirror of the
	// /api/observer/peers/* REST surface so operators can manage
	// peers from Signal / Telegram without curl.
	//   "peers"                       — list
	//   "peers <name>"                — detail
	//   "peers <name> stats"          — last snapshot
	//   "peers register <name>[ shape] [version]"
	//   "peers delete <name>"
	CmdPeers       CommandType = "peers"

	CmdUnknown     CommandType = "unknown"
)

// AgentVerb values set on a CmdAgent command.
const (
	AgentVerbSpawn  = "spawn"
	AgentVerbList   = "list"
	AgentVerbShow   = "show"
	AgentVerbLogs   = "logs"
	AgentVerbKill   = "kill"
	AgentVerbAudit  = "audit" // BL107
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

	// F10 sprint 3 — CmdAgent fields.
	AgentVerb        string // "spawn" | "list" | "show" | "logs" | "kill"
	AgentID          string // for show / logs / kill
	AgentProject     string // for spawn: "agent spawn <project> <cluster> [<task>]"
	AgentClusterName string
	AgentTask        string

	// F10 sprint 3.6 — CmdBind fields.
	BindAgentID string // empty means unbind

	// BL93/BL94 — CmdSession fields.
	SessionVerb string // "reconcile" | "import"
	SessionArg  string // for reconcile: "apply" | ""; for import: the dir/id

	// Sprint Sx2 — CmdRest fields.
	//   Method:  GET | POST | PUT | DELETE
	//   Path:    /api/...
	//   Body:    raw JSON body (for POST/PUT)
	RestMethod string
	RestPath   string
	RestBody   string

	// Sprint Sx2 — CmdCooldown fields.
	//   CooldownVerb: "status" | "set" | "clear"
	//   CooldownSeconds: only for "set" — pause duration in seconds
	//   CooldownReason: optional operator note for "set"
	CooldownVerb    string
	CooldownSeconds int
	CooldownReason  string
}

// SessionVerb values.
const (
	SessionVerbReconcile = "reconcile"
	SessionVerbImport    = "import"
)

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

	case strings.HasPrefix(lower, "bind "):
		// format: "bind <session-id> <agent-id>" ; "-" or omitted
		// second arg means unbind.
		parts := strings.Fields(text[5:])
		cmd := Command{Type: CmdBind}
		if len(parts) >= 1 {
			cmd.SessionID = parts[0]
		}
		if len(parts) >= 2 && parts[1] != "-" {
			cmd.BindAgentID = parts[1]
		}
		return cmd

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
	// F10 sprint 3 — agent lifecycle from chat.
	//   agent list
	//   agent show <id>
	//   agent logs <id>
	//   agent kill <id>
	//   agent spawn <project> <cluster> [task…]
	case lower == "agent" || strings.HasPrefix(lower, "agent "):
		rest := strings.TrimSpace(strings.TrimPrefix(text, "agent"))
		parts := strings.Fields(rest)
		if len(parts) < 1 {
			return Command{Type: CmdAgent}
		}
		verb := strings.ToLower(parts[0])
		cmd := Command{Type: CmdAgent, AgentVerb: verb}
		switch verb {
		case AgentVerbList:
			return cmd
		case AgentVerbAudit:
			// optional: "agent audit <agent-id>" filters to one ID.
			if len(parts) >= 2 {
				cmd.AgentID = parts[1]
			}
			return cmd
		case AgentVerbShow, AgentVerbLogs, AgentVerbKill:
			if len(parts) < 2 {
				cmd.Text = verb + " requires an agent id"
				return cmd
			}
			cmd.AgentID = parts[1]
			return cmd
		case AgentVerbSpawn:
			if len(parts) < 3 {
				cmd.Text = "spawn requires <project> <cluster> [<task>]"
				return cmd
			}
			cmd.AgentProject = parts[1]
			cmd.AgentClusterName = parts[2]
			if len(parts) > 3 {
				cmd.AgentTask = strings.Join(parts[3:], " ")
			}
			return cmd
		default:
			cmd.Text = "unknown verb: " + verb
			return cmd
		}

	// BL93/BL94 — orphan session reconciler over chat.
	//   session reconcile         (dry-run)
	//   session reconcile apply   (import every orphan)
	//   session import <dir|id>
	case lower == "session" || strings.HasPrefix(lower, "session "):
		rest := strings.TrimSpace(strings.TrimPrefix(text, "session"))
		parts := strings.Fields(rest)
		if len(parts) < 1 {
			return Command{Type: CmdSession, Text: "usage: session reconcile [apply] | session import <dir|id>"}
		}
		verb := strings.ToLower(parts[0])
		cmd := Command{Type: CmdSession, SessionVerb: verb}
		switch verb {
		case SessionVerbReconcile:
			if len(parts) >= 2 {
				cmd.SessionArg = strings.ToLower(parts[1])
			}
			return cmd
		case SessionVerbImport:
			if len(parts) < 2 {
				cmd.Text = "import requires a session dir or id"
				return cmd
			}
			cmd.SessionArg = parts[1]
			return cmd
		default:
			cmd.Text = "unknown session verb: " + verb
			return cmd
		}

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

	// Sprint Sx2 — comm-channel parity for v3.5–v3.7 endpoints.
	case lower == "cost" || strings.HasPrefix(lower, "cost "):
		// "cost" → /api/cost; "cost <full_id>" → /api/cost?session=<id>
		rest := strings.TrimSpace(text[len("cost"):])
		return Command{Type: CmdCost, Text: rest}

	case lower == "stale" || strings.HasPrefix(lower, "stale "):
		rest := strings.TrimSpace(text[len("stale"):])
		return Command{Type: CmdStale, Text: rest}

	case lower == "audit" || strings.HasPrefix(lower, "audit "):
		rest := strings.TrimSpace(text[len("audit"):])
		return Command{Type: CmdAudit, Text: rest}

	case lower == "peers" || strings.HasPrefix(lower, "peers "):
		// BL172 (S11) — federated observer peers over chat. Forms:
		//   "peers"                       → list
		//   "peers <name>"                → detail
		//   "peers <name> stats"          → last snapshot
		//   "peers register <name> [shape] [version]"
		//   "peers delete <name>"
		rest := strings.TrimSpace(text[len("peers"):])
		return Command{Type: CmdPeers, Text: rest}

	case strings.HasPrefix(lower, "cooldown"):
		rest := strings.TrimSpace(text[len("cooldown"):])
		cmd := Command{Type: CmdCooldown, CooldownVerb: "status"}
		if rest == "" || strings.HasPrefix(strings.ToLower(rest), "status") {
			return cmd
		}
		if strings.HasPrefix(strings.ToLower(rest), "clear") {
			cmd.CooldownVerb = "clear"
			return cmd
		}
		// "cooldown set <seconds> [reason words]"
		if strings.HasPrefix(strings.ToLower(rest), "set ") {
			parts := strings.SplitN(strings.TrimSpace(rest[4:]), " ", 2)
			cmd.CooldownVerb = "set"
			if len(parts) > 0 {
				if n, err := strconv.Atoi(parts[0]); err == nil {
					cmd.CooldownSeconds = n
				}
			}
			if len(parts) > 1 {
				cmd.CooldownReason = parts[1]
			}
			return cmd
		}
		return cmd

	case strings.HasPrefix(lower, "rest "):
		// rest <METHOD> <PATH> [JSON body]
		rest := strings.TrimSpace(text[5:])
		parts := strings.SplitN(rest, " ", 3)
		if len(parts) < 2 {
			return Command{Type: CmdUnknown}
		}
		out := Command{
			Type:       CmdRest,
			RestMethod: strings.ToUpper(parts[0]),
			RestPath:   parts[1],
		}
		if len(parts) == 3 {
			out.RestBody = parts[2]
		}
		return out

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
profile project list            list project profiles
profile cluster list            list cluster profiles
profile <kind> show <name>      show a profile (kind: project|cluster)
profile <kind> smoke <name>     run profile validation smoke test
agent list                      list active agent workers
agent spawn <proj> <cluster> [task]   spawn a new agent
agent show <id>                 show agent detail
agent logs <id>                 tail agent container logs
agent kill <id>                 terminate an agent
bind <session-id> <agent-id>    bind a session to a worker agent (use - to unbind)
help                            show this help

— v3.7.x parity (Sx2) —
cost [<full_id>]                 token+USD rollup (or per-session)
cooldown                         show global rate-limit cooldown
cooldown set <seconds> [reason]  pause new sessions for N seconds
cooldown clear                   clear active cooldown
stale [<seconds>]                list stale running sessions
audit [actor=x action=y limit=N] query operator audit log
rest <METHOD> <PATH> [json]      raw REST passthrough (e.g. rest GET /api/templates)`, hostname)
}
