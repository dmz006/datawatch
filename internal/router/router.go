package router

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/messaging"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/wizard"
)

// Router dispatches incoming messages to the session manager
// and formats responses back to the messaging backend.
type Router struct {
	hostname    string
	groupID     string
	backend     messaging.Backend
	manager     *session.Manager
	tailLines   int
	wizardMgr   *wizard.Manager
	schedStore  *session.ScheduleStore
	alertStore  *alerts.Store
	cmdLib      *session.CmdLibrary
	version     string
	checkUpdate func() string // optional func that returns latest version string
	restartFn   func()        // optional func to restart the daemon
	statsFn     func() string // optional func returning system stats summary
}

// NewRouter creates a new Router.
func NewRouter(hostname, groupID string, backend messaging.Backend, manager *session.Manager, tailLines int, wm *wizard.Manager) *Router {
	return &Router{
		hostname:  hostname,
		groupID:   groupID,
		backend:   backend,
		manager:   manager,
		tailLines: tailLines,
		wizardMgr: wm,
	}
}

// SetScheduleStore wires a schedule store into the router for the schedule command.
func (r *Router) SetScheduleStore(s *session.ScheduleStore) { r.schedStore = s }

// SetAlertStore wires an alert store into the router for the alerts command and SendAlert.
func (r *Router) SetAlertStore(s *alerts.Store) { r.alertStore = s }

// SetCmdLibrary wires the saved command library into the router for alert quick-reply hints.
func (r *Router) SetCmdLibrary(l *session.CmdLibrary) { r.cmdLib = l }

// SetVersion sets the version string reported by the version command.
func (r *Router) SetVersion(v string) { r.version = v }

// SetUpdateChecker sets an optional function that returns the latest available version.
func (r *Router) SetUpdateChecker(fn func() string) { r.checkUpdate = fn }

// SetRestartFunc sets an optional function that restarts the daemon.
func (r *Router) SetRestartFunc(fn func()) { r.restartFn = fn }
func (r *Router) SetStatsFunc(fn func() string) { r.statsFn = fn }

func (r *Router) handleStats() {
	if r.statsFn == nil {
		r.send(fmt.Sprintf("[%s] Stats not available.", r.hostname))
		return
	}
	r.send(fmt.Sprintf("[%s] System Stats:\n%s", r.hostname, r.statsFn()))
}

// Run starts the router, subscribing to Signal messages and dispatching them.
// Blocks until ctx is cancelled.
func (r *Router) Run(ctx context.Context) error {
	fmt.Printf("[%s] Router (%s) listening on group: %q\n", r.hostname, r.backend.Name(), r.groupID)
	// Only set default callbacks if none have been wired up yet.
	// When the HTTP server is enabled, main.go sets combined callbacks before
	// calling Run, so we skip re-setting them here.
	if r.manager.StateChangeHandler() == nil {
		r.manager.SetStateChangeHandler(r.HandleStateChange)
	}
	if r.manager.NeedsInputHandler() == nil {
		r.manager.SetNeedsInputHandler(r.HandleNeedsInput)
	}

	// Subscribe to messages
	return r.backend.Subscribe(ctx, r.handleMessage)
}

// handleMessage processes an incoming message.
func (r *Router) handleMessage(msg messaging.Message) {
	// Only process messages from our configured group
	if msg.GroupID != r.groupID {
		// Log mismatches for debugging (use -v flag to see)
		if msg.GroupID != "" {
			fmt.Printf("[%s] [debug] Message from group %q (expected %q) — ignoring\n",
				r.hostname, msg.GroupID, r.groupID)
		}
		return
	}

	fmt.Printf("[%s] [%s] Received: %q\n", r.hostname, msg.Backend, truncate(msg.Text, 80))

	// Check if an active wizard is waiting for a response in this group
	if r.wizardMgr != nil && r.wizardMgr.HandleMessage(msg.GroupID, msg.Text) {
		return
	}

	cmd := Parse(msg.Text)

	switch cmd.Type {
	case CmdNew:
		r.handleNew(cmd)
	case CmdList:
		r.handleList(cmd.Text)
	case CmdStatus:
		r.handleStatus(cmd)
	case CmdSend:
		r.handleSend(cmd)
	case CmdKill:
		r.handleKill(cmd)
	case CmdTail:
		r.handleTail(cmd)
	case CmdAttach:
		r.handleAttach(cmd)
	case CmdSetup:
		r.handleSetup(cmd, msg.GroupID)
	case CmdVersion:
		r.handleVersion()
	case CmdRestart:
		r.handleRestart()
	case CmdUpdateCheck:
		r.handleUpdateCheck()
	case CmdSchedule:
		r.handleSchedule(cmd)
	case CmdAlerts:
		r.handleAlerts(cmd)
	case CmdStats:
		r.handleStats()
	case CmdHelp:
		r.send(HelpText(r.hostname))
	default:
		// If exactly one session on this host is waiting for input,
		// treat any unrecognised message as the reply.
		r.handleImplicitSend(msg.Text)
	}
}

func (r *Router) handleSetup(cmd Command, groupID string) {
	if r.wizardMgr == nil {
		r.send(fmt.Sprintf("[%s] Setup wizards are not available in this context.", r.hostname))
		return
	}
	service := strings.TrimSpace(cmd.Text)
	if service == "" {
		r.send(fmt.Sprintf("[%s] Usage: setup <service>\nAvailable: signal, telegram, discord, slack, matrix, twilio, ntfy, email, webhook, github, web, server, llm <backend>, session, mcp", r.hostname))
		return
	}
	if err := r.wizardMgr.StartWizard(groupID, service, r.send); err != nil {
		r.send(fmt.Sprintf("[%s] %v", r.hostname, err))
	}
}

func (r *Router) handleAlerts(cmd Command) {
	if r.alertStore == nil {
		r.send(fmt.Sprintf("[%s] Alert store not available.", r.hostname))
		return
	}
	n := cmd.TailN
	if n <= 0 {
		n = 5
	}
	all := r.alertStore.List()
	if len(all) == 0 {
		r.send(fmt.Sprintf("[%s] No alerts.", r.hostname))
		return
	}
	if n > len(all) {
		n = len(all)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] Last %d alert(s):\n", r.hostname, n))
	for i, a := range all[:n] {
		sessLabel := ""
		if a.SessionID != "" {
			parts := strings.Split(a.SessionID, "-")
			sessLabel = fmt.Sprintf("[%s] ", parts[len(parts)-1])
		}
		sb.WriteString(fmt.Sprintf("  %s%s %s — %s\n",
			sessLabel, a.CreatedAt.Format("15:04:05"), strings.ToUpper(string(a.Level)), a.Title))
		if a.Body != "" {
			sb.WriteString(fmt.Sprintf("    %s\n", truncate(a.Body, 100)))
		}
		if i < n-1 {
			sb.WriteString("  ────\n")
		}
	}
	r.send(sb.String())
}

// SendAlert formats an alert and broadcasts it to this router's backend group.
// Called by main.go's alert listener for each active messaging backend.
func (r *Router) SendAlert(a *alerts.Alert) {
	body := ""
	if a.Body != "" {
		body = "\n" + truncate(a.Body, 200)
	}
	// Append quick-reply hints when a session is waiting for input and saved commands exist.
	quickHints := ""
	if a.SessionID != "" && r.cmdLib != nil {
		sess, ok := r.manager.GetSession(a.SessionID)
		if ok && sess.State == session.StateWaitingInput {
			cmds := r.cmdLib.List()
			if len(cmds) > 0 {
				names := make([]string, 0, len(cmds))
				for _, c := range cmds {
					names = append(names, c.Name)
				}
				shortID := sess.ID
				quickHints = fmt.Sprintf("\nQuick reply: send %s: <cmd>  options: %s",
					shortID, strings.Join(names, " | "))
			}
		}
	}
	r.send(fmt.Sprintf("[%s] ALERT [%s] %s%s%s",
		r.hostname, strings.ToUpper(string(a.Level)), a.Title, body, quickHints))
}

func (r *Router) handleVersion() {
	r.send(r.aboutText())
}

// aboutText returns ASCII art logo with version and host info.
func (r *Router) aboutText() string {
	v := r.version
	if v == "" {
		v = "unknown"
	}
	sessions := r.manager.ListSessions()
	active := 0
	for _, s := range sessions {
		if s.State == session.StateRunning || s.State == session.StateWaitingInput {
			active++
		}
	}
	return fmt.Sprintf(`
    ╔═══════════════════════════════════╗
    ║         ░▒▓ DATAWATCH ▓▒░        ║
    ║      ┌──────────────────┐        ║
    ║      │   ◉  ◎  ◉  ◎    │        ║
    ║      │  ╔══╗  ╔══╗     │        ║
    ║      │  ║◉◉║──║◎◎║     │        ║
    ║      │  ╚══╝  ╚══╝     │        ║
    ║      │    ◎  ◉  ◎  ◉   │        ║
    ║      └──────────────────┘        ║
    ║   AI Session Monitor & Bridge    ║
    ╠═══════════════════════════════════╣
    ║  Version:  v%-22s ║
    ║  Host:     %-22s  ║
    ║  Sessions: %d active / %-10d ║
    ║  Backend:  %-22s  ║
    ╚═══════════════════════════════════╝`, v, r.hostname, active, len(sessions), r.manager.ActiveBackend())
}

func (r *Router) handleRestart() {
	if r.restartFn == nil {
		r.send(fmt.Sprintf("[%s] restart not available.", r.hostname))
		return
	}
	r.send(fmt.Sprintf("[%s] Restarting daemon…", r.hostname))
	go func() {
		time.Sleep(500 * time.Millisecond)
		r.restartFn()
	}()
}

func (r *Router) handleUpdateCheck() {
	if r.checkUpdate == nil {
		v := r.version
		if v == "" {
			v = "unknown"
		}
		r.send(fmt.Sprintf("[%s] datawatch v%s (update check not available)", r.hostname, v))
		return
	}
	latest := r.checkUpdate()
	current := r.version
	if current == "" {
		current = "unknown"
	}
	if latest == "" || latest == current {
		r.send(fmt.Sprintf("[%s] datawatch v%s — up to date", r.hostname, current))
	} else {
		r.send(fmt.Sprintf("[%s] datawatch v%s — update available: v%s\nRun `datawatch update` on the host to upgrade.", r.hostname, current, latest))
	}
}

func (r *Router) handleSchedule(cmd Command) {
	if r.schedStore == nil {
		r.send(fmt.Sprintf("[%s] Scheduling is not available (no schedule store).", r.hostname))
		return
	}
	if cmd.SessionID == "" || cmd.Text == "" {
		r.send(fmt.Sprintf("[%s] Usage: schedule <id>: <when> <command>\n  when: now | HH:MM | cancel <schedID>", r.hostname))
		return
	}

	// Split Text into "when" and "command"
	parts := strings.SplitN(strings.TrimSpace(cmd.Text), " ", 2)
	when := strings.ToLower(strings.TrimSpace(parts[0]))
	command := ""
	if len(parts) >= 2 {
		command = strings.TrimSpace(parts[1])
	}

	// Handle cancel
	if when == "cancel" {
		if command == "" {
			r.send(fmt.Sprintf("[%s] Usage: schedule <id>: cancel <schedID>", r.hostname))
			return
		}
		if err := r.schedStore.Cancel(command); err != nil {
			r.send(fmt.Sprintf("[%s] %v", r.hostname, err))
		} else {
			r.send(fmt.Sprintf("[%s] Scheduled command %s cancelled.", r.hostname, command))
		}
		return
	}

	if command == "" {
		r.send(fmt.Sprintf("[%s] Usage: schedule <id>: <when> <command>", r.hostname))
		return
	}

	// Handle "list" to show pending schedules
	if when == "list" {
		pending := r.schedStore.List(session.SchedPending)
		if len(pending) == 0 {
			r.send(fmt.Sprintf("[%s] No pending scheduled items.", r.hostname))
			return
		}
		lines := []string{fmt.Sprintf("[%s] Pending schedules:", r.hostname)}
		for _, sc := range pending {
			when2 := "on input"
			if !sc.RunAt.IsZero() {
				when2 = sc.RunAt.Format("2006-01-02 15:04")
			}
			label := sc.SessionID
			if sc.Type == session.SchedTypeNewSession && sc.DeferredSession != nil {
				label = "NEW: " + sc.DeferredSession.Name
			}
			lines = append(lines, fmt.Sprintf("  [%s] %s @ %s: %s", sc.ID, label, when2, sc.Command))
		}
		r.send(strings.Join(lines, "\n"))
		return
	}

	var runAt time.Time
	if when != "now" {
		// Use natural language time parser (supports "in 30m", "at 14:00", "tomorrow at 9am", etc.)
		var err error
		runAt, err = session.ParseScheduleTime(when+" "+command, time.Now())
		if err != nil {
			// Fallback: try just the "when" part
			runAt, err = session.ParseScheduleTime(when, time.Now())
			if err != nil {
				r.send(fmt.Sprintf("[%s] Invalid time %q — try: now, in 30m, at 14:00, tomorrow at 9am", r.hostname, when))
				return
			}
		}
	}

	sc, err := r.schedStore.Add(cmd.SessionID, command, runAt, "")
	if err != nil {
		r.send(fmt.Sprintf("[%s] Failed to schedule: %v", r.hostname, err))
		return
	}

	when2 := "on next input prompt"
	if !sc.RunAt.IsZero() {
		when2 = sc.RunAt.Format("2006-01-02 15:04")
	}
	r.send(fmt.Sprintf("[%s] Scheduled [%s] for session %s at %s:\n  %s", r.hostname, sc.ID, cmd.SessionID, when2, command))
}

func (r *Router) handleNew(cmd Command) {
	if cmd.Text == "" {
		r.send(fmt.Sprintf("[%s] Usage: new: <task description>", r.hostname))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sess, err := r.manager.Start(ctx, cmd.Text, r.groupID, cmd.ProjectDir)
	if err != nil {
		r.send(fmt.Sprintf("[%s] Failed to start session: %v", r.hostname, err))
		return
	}
	r.send(fmt.Sprintf("[%s][%s] Started session for: %s\nTmux: %s\nAttach: tmux attach -t %s",
		r.hostname, sess.ID, cmd.Text, sess.TmuxSession, sess.TmuxSession))
}

func (r *Router) handleList(filter string) {
	sessions := r.manager.ListSessions()
	doneStates := map[session.State]bool{
		session.StateComplete: true,
		session.StateFailed:   true,
		session.StateKilled:   true,
	}

	var mine []*session.Session
	for _, s := range sessions {
		if s.Hostname != r.hostname {
			continue
		}
		switch strings.TrimPrefix(filter, "--") {
		case "active":
			if doneStates[s.State] {
				continue
			}
		case "inactive":
			if !doneStates[s.State] {
				continue
			}
		} // "all" or "" shows everything
		mine = append(mine, s)
	}

	if len(mine) == 0 {
		label := "sessions"
		if filter != "" {
			label = filter + " sessions"
		}
		r.send(fmt.Sprintf("[%s] No %s.", r.hostname, label))
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] Sessions (%d):\n", r.hostname, len(mine)))
	for i, s := range mine {
		name := s.Name
		if name == "" {
			name = truncate(s.Task, 40)
		}
		if name == "" {
			name = "(no task)"
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s | %s | %s | %s",
			s.ID, s.State, s.LLMBackend, s.UpdatedAt.Format("15:04"), name))
		if s.State == session.StateWaitingInput {
			sb.WriteString(" ⚠ INPUT")
		}
		sb.WriteByte('\n')
		if i < len(mine)-1 {
			sb.WriteString("  ────\n")
		}
	}
	r.send(sb.String())
}

func (r *Router) handleStatus(cmd Command) {
	if cmd.SessionID == "" {
		r.send(fmt.Sprintf("[%s] Usage: status <id>", r.hostname))
		return
	}

	sess, ok := r.manager.GetSession(cmd.SessionID)
	if !ok {
		r.send(fmt.Sprintf("[%s] Session %s not found.", r.hostname, cmd.SessionID))
		return
	}

	out, err := r.manager.TailOutput(sess.FullID, r.tailLines)
	if err != nil {
		r.send(fmt.Sprintf("[%s][%s] Error reading output: %v", r.hostname, sess.ID, err))
		return
	}

	r.send(fmt.Sprintf("[%s][%s] State: %s\nTask: %s\n---\n%s",
		r.hostname, sess.ID, sess.State, sess.Task, out))
}

func (r *Router) handleSend(cmd Command) {
	if cmd.SessionID == "" || cmd.Text == "" {
		r.send(fmt.Sprintf("[%s] Usage: send <id>: <message>", r.hostname))
		return
	}

	sess, ok := r.manager.GetSession(cmd.SessionID)
	if !ok {
		r.send(fmt.Sprintf("[%s] Session %s not found.", r.hostname, cmd.SessionID))
		return
	}

	if err := r.manager.SendInput(sess.FullID, cmd.Text, r.backend.Name()); err != nil {
		r.send(fmt.Sprintf("[%s][%s] Failed to send input: %v", r.hostname, sess.ID, err))
		return
	}
	r.send(fmt.Sprintf("[%s][%s] Input sent.", r.hostname, sess.ID))
}

func (r *Router) handleKill(cmd Command) {
	if cmd.SessionID == "" {
		r.send(fmt.Sprintf("[%s] Usage: kill <id>", r.hostname))
		return
	}

	sess, ok := r.manager.GetSession(cmd.SessionID)
	if !ok {
		r.send(fmt.Sprintf("[%s] Session %s not found.", r.hostname, cmd.SessionID))
		return
	}

	if err := r.manager.Kill(sess.FullID); err != nil {
		r.send(fmt.Sprintf("[%s][%s] Failed to kill: %v", r.hostname, sess.ID, err))
		return
	}
	r.send(fmt.Sprintf("[%s][%s] Session killed.", r.hostname, sess.ID))
}

func (r *Router) handleTail(cmd Command) {
	if cmd.SessionID == "" {
		r.send(fmt.Sprintf("[%s] Usage: tail <id> [n]", r.hostname))
		return
	}

	n := cmd.TailN
	if n <= 0 {
		n = r.tailLines
	}

	sess, ok := r.manager.GetSession(cmd.SessionID)
	if !ok {
		r.send(fmt.Sprintf("[%s] Session %s not found.", r.hostname, cmd.SessionID))
		return
	}

	out, err := r.manager.TailOutput(sess.FullID, n)
	if err != nil {
		r.send(fmt.Sprintf("[%s][%s] Error reading output: %v", r.hostname, sess.ID, err))
		return
	}
	r.send(fmt.Sprintf("[%s][%s] Last %d lines:\n%s", r.hostname, sess.ID, n, out))
}

func (r *Router) handleAttach(cmd Command) {
	if cmd.SessionID == "" {
		r.send(fmt.Sprintf("[%s] Usage: attach <id>", r.hostname))
		return
	}

	sess, ok := r.manager.GetSession(cmd.SessionID)
	if !ok {
		r.send(fmt.Sprintf("[%s] Session %s not found.", r.hostname, cmd.SessionID))
		return
	}

	r.send(fmt.Sprintf("[%s][%s] Run on %s:\n  tmux attach -t %s",
		r.hostname, sess.ID, sess.Hostname, sess.TmuxSession))
}

// handleImplicitSend routes an unrecognised message to the single waiting session, if any.
func (r *Router) handleImplicitSend(text string) {
	var waiting []*session.Session
	for _, s := range r.manager.ListSessions() {
		if s.State == session.StateWaitingInput && s.Hostname == r.hostname {
			waiting = append(waiting, s)
		}
	}

	switch len(waiting) {
	case 0:
		// Nothing to do — message is noise
	case 1:
		if err := r.manager.SendInput(waiting[0].FullID, text, r.backend.Name()); err != nil {
			r.send(fmt.Sprintf("[%s][%s] Failed to send input: %v", r.hostname, waiting[0].ID, err))
		} else {
			r.send(fmt.Sprintf("[%s][%s] Input sent.", r.hostname, waiting[0].ID))
		}
	default:
		r.send(fmt.Sprintf("[%s] Multiple sessions waiting for input. Use: send <id>: <message>", r.hostname))
	}
}

// HandleStateChange is called by the session manager when session state changes.
// It is exported so that main.go can compose it with other callbacks (e.g. WS broadcast).
func (r *Router) HandleStateChange(sess *session.Session, oldState session.State) {
	if sess.Hostname != r.hostname {
		return
	}
	label := sess.ID
	if sess.Name != "" {
		label = sess.ID + " " + sess.Name
	}
	r.send(fmt.Sprintf("[%s][%s] State: %s → %s", r.hostname, label, oldState, sess.State))
}

// HandleNeedsInput is called when a session is waiting for user input.
// It is exported so that main.go can compose it with other callbacks (e.g. WS broadcast).
func (r *Router) HandleNeedsInput(sess *session.Session, prompt string) {
	if sess.Hostname != r.hostname {
		return
	}
	label := sess.ID
	if sess.Name != "" {
		label = sess.ID + " " + sess.Name
	}
	r.send(fmt.Sprintf("[%s][%s] Needs input:\n%s\n\nReply with: send %s: <your response>",
		r.hostname, label, prompt, sess.ID))
}

// send delivers a message to the messaging backend group asynchronously.
// Runs in a goroutine so the message handler is never blocked by a slow send.
func (r *Router) send(text string) {
	go func() {
		if err := r.backend.Send(r.groupID, text); err != nil {
			fmt.Printf("ERROR sending to %s: %v\n", r.backend.Name(), err)
		}
	}()
}

// truncate shortens s to at most n characters, appending "..." if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
