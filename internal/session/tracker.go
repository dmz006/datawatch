package session

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Tracker manages the per-session git-tracked folder.
type Tracker struct {
	sessionDir string // e.g. ~/.claude-signal/sessions/hal9000-a3f2
	session    *Session
}

// NewTracker creates and initializes the session tracking folder.
// Creates the directory, initializes git, writes initial files, makes first commit.
func NewTracker(dataDir string, sess *Session) (*Tracker, error) {
	sessionDir := filepath.Join(dataDir, "sessions", sess.FullID)
	t := &Tracker{sessionDir: sessionDir, session: sess}

	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}

	// Initialize git repo
	if err := t.git("init", "-b", "main"); err != nil {
		// Try without -b for older git versions
		if err2 := t.git("init"); err2 != nil {
			return nil, fmt.Errorf("git init: %w", err2)
		}
	}

	// Configure git identity for the repo (local only)
	_ = t.git("config", "user.email", "claude-signal@localhost")
	_ = t.git("config", "user.name", "claude-signal")

	// Write initial files
	if err := t.writeInitialFiles(); err != nil {
		return nil, fmt.Errorf("write initial files: %w", err)
	}

	// First commit
	if err := t.commitAll("session: start — " + truncateStr(sess.Task, 60)); err != nil {
		return nil, fmt.Errorf("initial commit: %w", err)
	}

	return t, nil
}

// ResumeTracker opens an existing tracker without re-initializing.
func ResumeTracker(dataDir string, sess *Session) *Tracker {
	return &Tracker{
		sessionDir: filepath.Join(dataDir, "sessions", sess.FullID),
		session:    sess,
	}
}

// RecordStateChange appends to timeline.md, updates README and session.json, commits.
func (t *Tracker) RecordStateChange(from, to State) error {
	event := fmt.Sprintf("%s | state-change | %s → %s", timestamp(), from, to)
	if err := t.appendTimeline(event); err != nil {
		return err
	}
	t.session.State = to
	t.session.UpdatedAt = time.Now()
	if err := t.updateReadme(); err != nil {
		return err
	}
	if err := t.writeSessionJSON(); err != nil {
		return err
	}
	return t.commitAll(fmt.Sprintf("session: state %s→%s", from, to))
}

// RecordNeedsInput appends to conversation.md and timeline.md, commits.
func (t *Tracker) RecordNeedsInput(prompt string) error {
	entry := fmt.Sprintf("\n## [%s] Claude needs input\n\n```\n%s\n```\n", timestamp(), prompt)
	if err := t.appendFile("conversation.md", entry); err != nil {
		return err
	}
	if err := t.appendTimeline(fmt.Sprintf("%s | needs-input | %s", timestamp(), truncateStr(prompt, 80))); err != nil {
		return err
	}
	if err := t.updateReadme(); err != nil {
		return err
	}
	return t.commitAll("session: waiting for input — " + truncateStr(prompt, 50))
}

// RecordInputSent appends the user's response to conversation.md, commits.
func (t *Tracker) RecordInputSent(input string) error {
	entry := fmt.Sprintf("\n## [%s] User response\n\n```\n%s\n```\n", timestamp(), input)
	if err := t.appendFile("conversation.md", entry); err != nil {
		return err
	}
	if err := t.appendTimeline(fmt.Sprintf("%s | input-sent | %s", timestamp(), truncateStr(input, 80))); err != nil {
		return err
	}
	return t.commitAll("session: input sent")
}

// RecordComplete marks the session done, makes final commit.
func (t *Tracker) RecordComplete(finalState State) error {
	t.session.State = finalState
	t.session.UpdatedAt = time.Now()
	if err := t.updateReadme(); err != nil {
		return err
	}
	if err := t.writeSessionJSON(); err != nil {
		return err
	}
	if err := t.appendTimeline(fmt.Sprintf("%s | %s | session ended", timestamp(), finalState)); err != nil {
		return err
	}
	return t.commitAll(fmt.Sprintf("session: %s", finalState))
}

// RecordRateLimit records that the session is paused due to API rate limiting.
// resetAt is the time when the limit resets (may be zero if unknown).
func (t *Tracker) RecordRateLimit(resetAt time.Time) error {
	var resetStr string
	if resetAt.IsZero() {
		resetStr = "unknown"
	} else {
		resetStr = resetAt.Format(time.RFC3339)
	}
	event := fmt.Sprintf("%s | rate-limited | resets at %s", timestamp(), resetStr)
	if err := t.appendTimeline(event); err != nil {
		return err
	}
	if err := t.updateReadme(); err != nil {
		return err
	}
	return t.commitAll("session: rate limited — waiting for quota reset")
}

// RecordResume records that the session is resuming after a rate limit.
func (t *Tracker) RecordResume() error {
	event := fmt.Sprintf("%s | resumed | rate limit reset", timestamp())
	if err := t.appendTimeline(event); err != nil {
		return err
	}
	return t.commitAll("session: resumed after rate limit reset")
}

// WriteCLAUDEMD writes a CLAUDE.md guardrails file to both the session tracking
// folder and the project directory, using the template at templatePath.
// Template variables: SessionID, Hostname, StartedAt, Task, ProjectDir, TrackingDir.
func (t *Tracker) WriteCLAUDEMD(templatePath string, sess *Session) error {
	// Read template
	tmplBytes, err := os.ReadFile(templatePath)
	if err != nil {
		// Template not found — write a minimal CLAUDE.md
		tmplBytes = []byte(minimalCLAUDEMD(sess))
	}

	// Simple token replacement (not using text/template to avoid import complexity)
	content := string(tmplBytes)
	content = strings.ReplaceAll(content, "{{.SessionID}}", sess.FullID)
	content = strings.ReplaceAll(content, "{{.Hostname}}", sess.Hostname)
	content = strings.ReplaceAll(content, "{{.StartedAt}}", sess.CreatedAt.Format(time.RFC3339))
	content = strings.ReplaceAll(content, "{{.Task}}", sess.Task)
	content = strings.ReplaceAll(content, "{{.ProjectDir}}", sess.ProjectDir)
	content = strings.ReplaceAll(content, "{{.TrackingDir}}", t.sessionDir)

	// Write to session tracking folder
	if err := os.WriteFile(filepath.Join(t.sessionDir, "CLAUDE.md"), []byte(content), 0644); err != nil {
		return err
	}

	// Write to project directory (if different from session dir)
	if sess.ProjectDir != "" && sess.ProjectDir != t.sessionDir {
		projectCLAUDE := filepath.Join(sess.ProjectDir, "CLAUDE.md")
		// Only write if it doesn't already exist (don't overwrite user's own CLAUDE.md)
		if _, err := os.Stat(projectCLAUDE); os.IsNotExist(err) {
			if err := os.WriteFile(projectCLAUDE, []byte(content), 0644); err != nil {
				return err
			}
		}
	}

	return nil
}

func minimalCLAUDEMD(sess *Session) string {
	return fmt.Sprintf(`# Session Guardrails

Session: %s | Task: %s

## Constraints
- Work only within: %s
- Commit changes to git frequently
- If rate limited: output CLAUDE_SIGNAL_RATE_LIMITED: resets at <time>
- If needing input: output CLAUDE_SIGNAL_NEEDS_INPUT: <question>
- When done: output CLAUDE_SIGNAL_COMPLETE: <summary>
`, sess.FullID, sess.Task, sess.ProjectDir)
}

// SessionDir returns the path to the session's tracking folder.
func (t *Tracker) SessionDir() string {
	return t.sessionDir
}

// OutputLogPath returns the path to the session's output log file.
// This is where tmux pipe-pane should write output.
func (t *Tracker) OutputLogPath() string {
	return filepath.Join(t.sessionDir, "output.log")
}

// writeInitialFiles creates all the initial tracking files.
func (t *Tracker) writeInitialFiles() error {
	files := map[string]string{
		"task.md":         t.renderTask(),
		"README.md":       t.renderReadme(),
		"conversation.md": t.renderConversationHeader(),
		"timeline.md":     t.renderTimelineHeader(),
		"output.log":      "",
	}

	for name, content := range files {
		path := filepath.Join(t.sessionDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}

	// Write proper session.json
	return t.writeSessionJSON()
}

func (t *Tracker) renderTask() string {
	return fmt.Sprintf("# Task\n\n**Session:** `%s`  \n**Created:** %s  \n**Host:** %s\n\n## Description\n\n%s\n",
		t.session.FullID,
		t.session.CreatedAt.Format(time.RFC3339),
		t.session.Hostname,
		t.session.Task,
	)
}

func (t *Tracker) renderReadme() string {
	return fmt.Sprintf(`# Session %s

| Field | Value |
|-------|-------|
| ID | %s |
| Host | %s |
| State | %s |
| Created | %s |
| Updated | %s |
| Tmux | %s |

## Task

%s

## Files

| File | Description |
|------|-------------|
| task.md | Original task description |
| output.log | Full claude-code output (live) |
| conversation.md | Human-readable Q&A history |
| timeline.md | Timestamped event log |
| session.json | Machine-readable session state |

## Git History

All state changes, inputs, and outputs are tracked as git commits.
View with: `+"`git log --oneline`"+`
`,
		t.session.FullID,
		t.session.FullID,
		t.session.Hostname,
		t.session.State,
		t.session.CreatedAt.Format(time.RFC3339),
		t.session.UpdatedAt.Format(time.RFC3339),
		t.session.TmuxSession,
		t.session.Task,
	)
}

func (t *Tracker) renderConversationHeader() string {
	return fmt.Sprintf("# Conversation — Session %s\n\nTask: %s  \nStarted: %s\n\n---\n",
		t.session.FullID, t.session.Task, t.session.CreatedAt.Format(time.RFC3339))
}

func (t *Tracker) renderTimelineHeader() string {
	return fmt.Sprintf("# Timeline — Session %s\n\nFormat: timestamp | event-type | details\n\n---\n\n%s | created | task: %s\n",
		t.session.FullID,
		timestamp(),
		truncateStr(t.session.Task, 80),
	)
}

func (t *Tracker) updateReadme() error {
	content := t.renderReadme()
	return os.WriteFile(filepath.Join(t.sessionDir, "README.md"), []byte(content), 0644)
}

func (t *Tracker) writeSessionJSON() error {
	data, err := marshalSession(t.session)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(t.sessionDir, "session.json"), data, 0644)
}

func (t *Tracker) appendTimeline(line string) error {
	return t.appendFile("timeline.md", line+"\n")
}

func (t *Tracker) appendFile(name, content string) error {
	path := filepath.Join(t.sessionDir, name)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

func (t *Tracker) git(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = t.sessionDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, out)
	}
	return nil
}

func (t *Tracker) commitAll(message string) error {
	// Stage all changes
	if err := t.git("add", "-A"); err != nil {
		return err
	}
	// Check if there is anything to commit
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = t.sessionDir
	out, _ := cmd.Output()
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil // nothing to commit
	}
	return t.git("commit", "-m", message, "--allow-empty")
}

func marshalSession(sess *Session) ([]byte, error) {
	return json.MarshalIndent(sess, "", "  ")
}

func timestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
