// Package pilog reads pi coding-agent session logs.
//
// pi writes one JSONL file per session under a per-project directory of its
// sessions root: <root>/--<workdir with separators replaced by "-">--/
// <start time>_<session id>.jsonl. LocateLog finds the file for a session
// started in a given working directory and Read condenses it into a Progress
// snapshot. Both are read-only; the log is written by a live pi process, so
// Read tolerates a partially written last line and event types it does not
// know about.
package pilog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// summaryRunes is the maximum length of Progress.LastSummary in characters.
const summaryRunes = 200

// Progress is a condensed view of a pi session log.
type Progress struct {
	// LastEventAt is the timestamp of the newest event that carries one.
	LastEventAt time.Time
	// AssistantTurns counts assistant messages.
	AssistantTurns int
	// ToolCalls counts tool calls by tool name.
	ToolCalls map[string]int
	// LastToolName and LastToolArgs describe the most recent tool call;
	// LastToolArgs is the call's arguments as compact JSON.
	LastToolName string
	LastToolArgs string
	// LastSummary is the first 200 characters of the last thinking or text block.
	LastSummary string
	// Tokens is the total token usage reported by assistant messages, 0 when
	// the log carries none.
	Tokens int
}

// ProjectDirName maps a working directory to the name of pi's per-project
// directory: the leading separator is dropped, path separators and colons
// become "-", and the result is wrapped in "--".
func ProjectDirName(workDir string) string {
	s := strings.TrimLeft(workDir, `/\`)
	s = strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(s)
	return "--" + s + "--"
}

// LocateLog returns the path of the session log under sessionsRoot for a
// session started in workDir at started: the .jsonl file in the project's
// directory whose <time>_<id> name carries the earliest start time at or
// after started. It returns an error wrapping os.ErrNotExist when there is
// no such file.
func LocateLog(sessionsRoot, workDir string, started time.Time) (string, error) {
	dir := filepath.Join(sessionsRoot, ProjectDirName(filepath.Clean(workDir)))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("pilog: read project dir: %w", err)
	}
	var best string
	var bestAt time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		at, ok := parseLogTime(e.Name())
		if !ok || at.Before(started) {
			continue
		}
		if best == "" || at.Before(bestAt) {
			best, bestAt = e.Name(), at
		}
	}
	if best == "" {
		return "", fmt.Errorf("pilog: no session log in %s at or after %s: %w",
			dir, started.Format(time.RFC3339), os.ErrNotExist)
	}
	return filepath.Join(dir, best), nil
}

// parseLogTime extracts the start time from a log file name of the form
// 2026-10-27T14-00-00-000Z_<id>.jsonl.
func parseLogTime(name string) (time.Time, bool) {
	stamp, _, ok := strings.Cut(name, "_")
	if !ok {
		return time.Time{}, false
	}
	date, clock, ok := strings.Cut(stamp, "T")
	if !ok {
		return time.Time{}, false
	}
	// clock is HH-MM-SS-mmmZ; restore the ':' and '.' pi replaced with '-'.
	parts := strings.SplitN(strings.TrimSuffix(clock, "Z"), "-", 4)
	if len(parts) < 3 {
		return time.Time{}, false
	}
	s := date + "T" + parts[0] + ":" + parts[1] + ":" + parts[2]
	if len(parts) == 4 {
		s += "." + parts[3]
	}
	t, err := time.Parse(time.RFC3339Nano, s+"Z")
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

type event struct {
	Type      string          `json:"type"`
	Timestamp json.RawMessage `json:"timestamp"`
	Message   *message        `json:"message"`
}

type message struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Usage     *usage          `json:"usage"`
	Timestamp json.RawMessage `json:"timestamp"`
}

type usage struct {
	Input       int `json:"input"`
	Output      int `json:"output"`
	CacheRead   int `json:"cacheRead"`
	CacheWrite  int `json:"cacheWrite"`
	TotalTokens int `json:"totalTokens"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Read parses the session log at path. Lines that are not valid JSON (such as
// a last line still being written) and events of unknown types are skipped.
func Read(path string) (Progress, error) {
	p := Progress{ToolCalls: map[string]int{}}
	f, err := os.Open(path)
	if err != nil {
		return p, fmt.Errorf("pilog: %w", err)
	}
	defer f.Close()

	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			p.apply(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return p, nil
			}
			return p, fmt.Errorf("pilog: read %s: %w", path, err)
		}
	}
}

// apply folds one JSONL line into p, ignoring anything it cannot parse.
func (p *Progress) apply(line []byte) {
	var ev event
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	if at, ok := eventTime(ev); ok && at.After(p.LastEventAt) {
		p.LastEventAt = at
	}
	if ev.Type != "message" || ev.Message == nil || ev.Message.Role != "assistant" {
		return
	}
	m := ev.Message
	p.AssistantTurns++
	if m.Usage != nil {
		p.Tokens += m.Usage.total()
	}
	var blocks []block
	if json.Unmarshal(m.Content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		switch b.Type {
		case "thinking":
			p.setSummary(b.Thinking)
		case "text":
			p.setSummary(b.Text)
		case "toolCall":
			p.ToolCalls[b.Name]++
			p.LastToolName = b.Name
			p.LastToolArgs = compactJSON(b.Arguments)
		}
	}
}

func (p *Progress) setSummary(s string) {
	if s = strings.TrimSpace(s); s != "" {
		p.LastSummary = truncateRunes(s, summaryRunes)
	}
}

func (u usage) total() int {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.Input + u.Output + u.CacheRead + u.CacheWrite
}

// eventTime returns the event's RFC 3339 timestamp, or the message's epoch
// millisecond timestamp when the event has none.
func eventTime(ev event) (time.Time, bool) {
	var s string
	if json.Unmarshal(ev.Timestamp, &s) == nil && s != "" {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t, true
		}
	}
	if ev.Message != nil {
		var ms int64
		if json.Unmarshal(ev.Message.Timestamp, &ms) == nil && ms > 0 {
			return time.UnixMilli(ms).UTC(), true
		}
	}
	return time.Time{}, false
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return string(raw)
	}
	return buf.String()
}

// truncateRunes returns the first n characters of s without splitting a rune.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
