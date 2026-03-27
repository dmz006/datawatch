package router

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/messaging"
	"github.com/dmz006/datawatch/internal/session"
)

// Router dispatches incoming messages to the session manager
// and formats responses back to the messaging backend.
type Router struct {
	hostname  string
	groupID   string
	backend   messaging.Backend
	manager   *session.Manager
	tailLines int
}

// NewRouter creates a new Router.
func NewRouter(hostname, groupID string, backend messaging.Backend, manager *session.Manager, tailLines int) *Router {
	return &Router{
		hostname:  hostname,
		groupID:   groupID,
		backend:   backend,
		manager:   manager,
		tailLines: tailLines,
	}
}

// Run starts the router, subscribing to Signal messages and dispatching them.
// Blocks until ctx is cancelled.
// Note: if callbacks have already been set externally (e.g. by main.go when
// composing with the HTTP server), this will overwrite them. Call
// SetDefaultCallbacks before Run if you want the Signal-only behaviour.
func (r *Router) Run(ctx context.Context) error {
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
		return
	}

	cmd := Parse(msg.Text)

	switch cmd.Type {
	case CmdNew:
		r.handleNew(cmd)
	case CmdList:
		r.handleList()
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
	case CmdHelp:
		r.send(HelpText(r.hostname))
	default:
		// If exactly one session on this host is waiting for input,
		// treat any unrecognised message as the reply.
		r.handleImplicitSend(msg.Text)
	}
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

func (r *Router) handleList() {
	sessions := r.manager.ListSessions()
	// Filter to sessions on this host
	var mine []*session.Session
	for _, s := range sessions {
		if s.Hostname == r.hostname {
			mine = append(mine, s)
		}
	}

	if len(mine) == 0 {
		r.send(fmt.Sprintf("[%s] No sessions.", r.hostname))
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] Sessions:\n", r.hostname))
	for _, s := range mine {
		sb.WriteString(fmt.Sprintf("  [%s] %-14s %s\n    Task: %s\n",
			s.ID, s.State, s.UpdatedAt.Format("15:04:05"), truncate(s.Task, 60)))
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

	if err := r.manager.SendInput(sess.FullID, cmd.Text); err != nil {
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
		if err := r.manager.SendInput(waiting[0].FullID, text); err != nil {
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
	r.send(fmt.Sprintf("[%s][%s] State: %s → %s", r.hostname, sess.ID, oldState, sess.State))
}

// HandleNeedsInput is called when a session is waiting for user input.
// It is exported so that main.go can compose it with other callbacks (e.g. WS broadcast).
func (r *Router) HandleNeedsInput(sess *session.Session, prompt string) {
	if sess.Hostname != r.hostname {
		return
	}
	r.send(fmt.Sprintf("[%s][%s] Needs input:\n%s\n\nReply with: send %s: <your response>",
		r.hostname, sess.ID, prompt, sess.ID))
}

// send delivers a message to the Signal group, logging any error.
func (r *Router) send(text string) {
	if err := r.backend.Send(r.groupID, text); err != nil {
		fmt.Printf("ERROR sending to Signal: %v\n", err)
	}
}

// truncate shortens s to at most n characters, appending "..." if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
