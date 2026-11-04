package adapters

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/trace"
)

// ClaudeCodeName is the backend name of the claude-code adapter.
const ClaudeCodeName = "claude-code"

// ClaudeCode parses Claude Code JSON-lines transcripts.
type ClaudeCode struct{}

// NewClaudeCode returns the claude-code adapter.
func NewClaudeCode() *ClaudeCode { return &ClaudeCode{} }

// Name implements Adapter.
func (*ClaudeCode) Name() string { return ClaudeCodeName }

// Coverage implements Adapter.
func (*ClaudeCode) Coverage() trace.Coverage { return trace.CoverageFull }

// ClaudeProjectsDir returns the projects folder under the Claude config
// directory. An empty configDir reads the home directory's .claude.
func ClaudeProjectsDir(configDir string) string {
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".claude")
	}
	return filepath.Join(configDir, "projects")
}

// ClaudeProjectDirName maps a working directory to Claude Code's
// per-project directory name: every character other than a letter or digit
// becomes "-".
func ClaudeProjectDirName(workDir string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		}
		return '-'
	}, workDir)
}

// ClaudeTranscriptPath returns where Claude Code writes the transcript of a
// session started in workDir.
func ClaudeTranscriptPath(configDir, workDir, sessionID string) string {
	return filepath.Join(ClaudeProjectsDir(configDir), ClaudeProjectDirName(workDir), sessionID+".jsonl")
}

type ccLine struct {
	Type      string     `json:"type"`
	Timestamp string     `json:"timestamp"`
	SessionID string     `json:"sessionId"`
	Message   *ccMessage `json:"message"`
}

type ccMessage struct {
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *struct {
		Input      int64 `json:"input_tokens"`
		Output     int64 `json:"output_tokens"`
		CacheRead  int64 `json:"cache_read_input_tokens"`
		CacheWrite int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

type ccBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
}

// Parse implements Adapter. A tool_use becomes a tool_call event; its
// tool_result becomes a tool_result event whose duration is the timestamp
// difference. Each assistant message with usage yields a model_request.
func (a *ClaudeCode) Parse(r io.Reader, ctx *Context) ([]trace.Event, error) {
	var out []trace.Event
	br := bufio.NewReader(r)
	for {
		line, rerr := br.ReadBytes('\n')
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return out, nil
			}
			return out, rerr
		}
		ctx.Offset += int64(len(line))
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var l ccLine
		if json.Unmarshal(line, &l) != nil || l.Message == nil {
			continue
		}
		at := ctx.now()
		if t, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
			at = t.UTC()
		}
		var blocks []ccBlock
		if json.Unmarshal(l.Message.Content, &blocks) != nil {
			blocks = nil // plain string content: no tool blocks
		}
		switch l.Type {
		case "assistant":
			for _, b := range blocks {
				if b.Type != "tool_use" {
					continue
				}
				ctx.pending()[b.ID] = PendingCall{Name: b.Name, At: at}
				out = append(out, stamp(trace.Event{
					Kind: trace.KindToolCall, Name: b.Name,
					ArgSummary: compact(b.Input), Timestamp: at,
				}, ClaudeCodeName, ctx))
			}
			if u := l.Message.Usage; u != nil {
				out = append(out, stamp(trace.Event{
					Kind: trace.KindModelRequest, Name: l.Message.Model,
					TokensIn:  u.Input + u.CacheRead + u.CacheWrite,
					TokensOut: u.Output, Timestamp: at,
				}, ClaudeCodeName, ctx))
			}
		case "user":
			for _, b := range blocks {
				if b.Type != "tool_result" {
					continue
				}
				ev := trace.Event{Kind: trace.KindToolResult, Outcome: "ok", Timestamp: at}
				if b.IsError {
					ev.Outcome = "error"
				}
				if p, ok := ctx.pending()[b.ToolUseID]; ok {
					ev.Name = p.Name
					if d := at.Sub(p.At); d > 0 {
						ev.DurationMS = d.Milliseconds()
					}
					delete(ctx.Pending, b.ToolUseID)
				}
				out = append(out, stamp(ev, ClaudeCodeName, ctx))
			}
		}
	}
}

func compact(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return string(raw)
	}
	return buf.String()
}
