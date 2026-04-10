package session

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// Tracker manages the per-session git-tracked folder.
type Tracker struct {
	sessionDir string // e.g. ~/.datawatch/sessions/hal9000-a3f2
	session    *Session
	encKey     []byte // optional: encrypts tracker files when set (secure_tracking: full)
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
	_ = t.git("config", "user.email", "datawatch@localhost")
	_ = t.git("config", "user.name", "datawatch")

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

// SetEncKey enables encryption for tracker file writes.
// When set, appendFile and writeSessionJSON encrypt content before writing.
func (t *Tracker) SetEncKey(key []byte) { t.encKey = key }

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
	backend := t.session.LLMBackend
	if backend == "" {
		backend = "LLM"
	}
	entry := fmt.Sprintf("\n## [%s] %s needs input\n\n```\n%s\n```\n", timestamp(), backend, prompt)
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
// source identifies where the input came from (e.g. "signal", "web", "mcp", "filter", "schedule").
func (t *Tracker) RecordInputSent(input, source string) error {
	sourceLabel := ""
	if source != "" {
		sourceLabel = fmt.Sprintf(" (via %s)", source)
	}
	entry := fmt.Sprintf("\n## [%s] User response%s\n\n```\n%s\n```\n", timestamp(), sourceLabel, input)
	if err := t.appendFile("conversation.md", entry); err != nil {
		return err
	}
	timelineSource := ""
	if source != "" {
		timelineSource = " | source:" + source
	}
	if err := t.appendTimeline(fmt.Sprintf("%s | input-sent%s | %s", timestamp(), timelineSource, truncateStr(input, 80))); err != nil {
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

// GuardrailsOptions controls which sections are included in session guardrails.
type GuardrailsOptions struct {
	MemoryEnabled bool
	RTKEnabled    bool
}

// WriteSessionGuardrails writes a guardrails file to the session tracking folder and,
// for claude-code sessions, merges memory/RTK sections into the project's existing
// CLAUDE.md or AGENT.md (creating it if it doesn't exist).
// Template variables: SessionID, Hostname, StartedAt, Task, ProjectDir, TrackingDir.
func (t *Tracker) WriteSessionGuardrails(templatePath string, sess *Session, opts ...GuardrailsOptions) error {
	var opt GuardrailsOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	isClaudeCode := sess.LLMBackend == "claude-code"

	// Determine guardrails filename: claude-code reads CLAUDE.md; all others use AGENT.md.
	guardrailsFile := "AGENT.md"
	if isClaudeCode {
		guardrailsFile = "CLAUDE.md"
	}

	// Read template (try backend-specific, then generic)
	tmplBytes, err := os.ReadFile(templatePath)
	if err != nil {
		tmplBytes = []byte(minimalSessionGuardrails(sess))
	}

	// Simple token replacement
	content := string(tmplBytes)
	content = strings.ReplaceAll(content, "{{.SessionID}}", sess.FullID)
	content = strings.ReplaceAll(content, "{{.Hostname}}", sess.Hostname)
	content = strings.ReplaceAll(content, "{{.StartedAt}}", sess.CreatedAt.Format(time.RFC3339))
	content = strings.ReplaceAll(content, "{{.Task}}", sess.Task)
	content = strings.ReplaceAll(content, "{{.ProjectDir}}", sess.ProjectDir)
	content = strings.ReplaceAll(content, "{{.TrackingDir}}", t.sessionDir)

	// Remove memory section from template if memory is not enabled
	if !opt.MemoryEnabled {
		if idx := strings.Index(content, "## Memory & Knowledge"); idx >= 0 {
			endIdx := strings.Index(content[idx+1:], "\n## ")
			if endIdx > 0 {
				content = content[:idx] + content[idx+1+endIdx:]
			}
		}
	}

	// Write to session tracking folder (always full template)
	if err := os.WriteFile(filepath.Join(t.sessionDir, guardrailsFile), []byte(content), 0644); err != nil {
		return err
	}

	// For project dir: merge sections into existing file (don't overwrite)
	if isClaudeCode && sess.ProjectDir != "" && sess.ProjectDir != t.sessionDir {
		targetFile := filepath.Join(sess.ProjectDir, "CLAUDE.md")
		// Prefer AGENT.md if it exists
		if agentPath := filepath.Join(sess.ProjectDir, "AGENT.md"); fileExists(agentPath) {
			targetFile = agentPath
		}

		if fileExists(targetFile) {
			// File exists — merge missing sections
			existing, _ := os.ReadFile(targetFile)
			existingStr := string(existing)
			modified := false

			// Add memory section if enabled and not present
			if opt.MemoryEnabled && !strings.Contains(existingStr, "Memory & Knowledge") && !strings.Contains(existingStr, "memory_recall") {
				existingStr += "\n\n" + memoryInstructions()
				modified = true
			}

			// Add RTK section if enabled and not present
			if opt.RTKEnabled && !strings.Contains(existingStr, "RTK") && !strings.Contains(existingStr, "rtk") {
				// RTK instructions are managed by rtk init, don't add here
			}

			if modified {
				os.WriteFile(targetFile, []byte(existingStr), 0644) //nolint:errcheck
			}
		} else {
			// No file exists — write the full template
			os.WriteFile(targetFile, []byte(content), 0644) //nolint:errcheck
		}
	}

	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// memoryInstructions returns the memory section to append to existing CLAUDE.md/AGENT.md.
func memoryInstructions() string {
	return `# Memory & Knowledge (datawatch)

Use the datawatch memory system proactively during this session.

## Before starting work
- Use ` + "`memory_recall`" + ` to check if similar work has been done
- Use ` + "`kg_query`" + ` to understand entity relationships
- Use ` + "`research_sessions`" + ` for deep cross-session search

## During work
- Use ` + "`memory_remember`" + ` to save key decisions and patterns
- Use ` + "`kg_add`" + ` to record relationships

## When asked about project history
Always check memory first with ` + "`memory_recall`" + ` before answering from training data.

## Available tools
| Tool | Purpose |
|------|---------|
| ` + "`memory_recall`" + ` | Semantic search across project memories |
| ` + "`memory_remember`" + ` | Save decisions, patterns, context |
| ` + "`kg_query`" + ` | Entity relationship queries |
| ` + "`kg_add`" + ` | Record new relationships |
| ` + "`research_sessions`" + ` | Cross-session research |
| ` + "`copy_response`" + ` | Last LLM response from any session |
| ` + "`get_prompt`" + ` | Last user prompt from any session |`
}

func minimalSessionGuardrails(sess *Session) string {
	return fmt.Sprintf(`# Session Guardrails

Session: %s | Task: %s

## Constraints
- Work only within: %s
- Commit changes to git frequently
- If rate limited: output DATAWATCH_RATE_LIMITED: resets at <time>
- If needing input: output DATAWATCH_NEEDS_INPUT: <question>
- When done: output DATAWATCH_COMPLETE: <summary>
`, sess.FullID, sess.Task, sess.ProjectDir)
}

// ReadTimeline returns the raw lines of timeline.md (excluding the header section).
// Lines beginning with "#" or "---" or blank are skipped.
func (t *Tracker) ReadTimeline() ([]string, error) {
	data, err := os.ReadFile(filepath.Join(t.sessionDir, "timeline.md"))
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "Format:") {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines, nil
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
	if t.encKey != nil {
		data, err = secfile.Encrypt(data, t.encKey)
		if err != nil {
			return fmt.Errorf("encrypt session.json: %w", err)
		}
	}
	return os.WriteFile(filepath.Join(t.sessionDir, "session.json"), data, 0644)
}

func (t *Tracker) appendTimeline(line string) error {
	return t.appendFile("timeline.md", line+"\n")
}

func (t *Tracker) appendFile(name, content string) error {
	path := filepath.Join(t.sessionDir, name)
	if t.encKey != nil {
		// Read-decrypt-append-encrypt-write for encrypted files
		existing := []byte{}
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			if secfile.IsEncrypted(data) {
				existing, err = secfile.Decrypt(data, t.encKey)
				if err != nil {
					return fmt.Errorf("decrypt %s for append: %w", name, err)
				}
			} else {
				existing = data
			}
		}
		combined := append(existing, []byte(content)...)
		enc, err := secfile.Encrypt(combined, t.encKey)
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", name, err)
		}
		return os.WriteFile(path, enc, 0644)
	}
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
