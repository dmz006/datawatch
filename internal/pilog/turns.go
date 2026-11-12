package pilog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// ToolCall is a single tool invocation inside an assistant turn; Args is the
// call's arguments as compact JSON.
type ToolCall struct {
	Name string
	Args string
}

// Turn is one assistant message from a session log: its concatenated text
// (thinking blocks excluded) and its tool calls in block order.
type Turn struct {
	Text      string
	ToolCalls []ToolCall
}

// ReadTurns parses the session log at path and returns its assistant turns in
// file order. Lines that are not valid JSON (such as a last line still being
// written) and events of unknown types are skipped, like in Read.
func ReadTurns(path string) ([]Turn, error) {
	var turns []Turn
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("pilog: %w", err)
	}
	defer f.Close() //nolint:errcheck

	r := bufio.NewReader(f)
	for {
		line, rerr := r.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			if ev, ok := parseEvent(line); ok {
				if t, ok := turnFromEvent(ev); ok {
					turns = append(turns, t)
				}
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return turns, nil
			}
			return nil, fmt.Errorf("pilog: read %s: %w", path, rerr)
		}
	}
}

// Recent returns the last n turns. When n is <= 0 or larger than len(turns)
// it returns all turns.
func Recent(turns []Turn, n int) []Turn {
	if n <= 0 || n > len(turns) {
		return turns
	}
	return turns[len(turns)-n:]
}

// parseEvent decodes one JSONL line into an event, reporting ok=false for
// lines that are not valid events.
func parseEvent(line []byte) (event, bool) {
	var ev event
	if json.Unmarshal(line, &ev) != nil {
		return ev, false
	}
	return ev, true
}

// turnFromEvent extracts the assistant turn from a message event, reporting
// ok=false for any other event.
func turnFromEvent(ev event) (Turn, bool) {
	if ev.Type != "message" || ev.Message == nil || ev.Message.Role != "assistant" {
		return Turn{}, false
	}
	var blocks []block
	if json.Unmarshal(ev.Message.Content, &blocks) != nil {
		return Turn{}, false
	}
	var turn Turn
	var text []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text = append(text, b.Text)
		case "toolCall":
			turn.ToolCalls = append(turn.ToolCalls, ToolCall{Name: b.Name, Args: compactJSON(b.Arguments)})
		}
	}
	turn.Text = textOf(text)
	return turn, true
}

func textOf(parts []string) string {
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += " "
		}
		s += p
	}
	return s
}

// TimedTurn is an assistant turn with the log's own timing and token usage.
type TimedTurn struct {
	Turn
	// At is the event time; zero when the log carried none.
	At time.Time
	// TokensIn and TokensOut are the turn's usage (cache reads and writes
	// count as input); zero when the log carried no usage.
	TokensIn, TokensOut int
}

// ParseTimedTurns reads JSONL from r and returns the assistant turns, only
// consuming complete (newline-terminated) lines. consumed is the number of
// bytes of r that were used, so a caller can resume at that offset once the
// log has grown. Invalid lines and unknown events are skipped.
func ParseTimedTurns(r io.Reader) (turns []TimedTurn, consumed int64, err error) {
	br := bufio.NewReader(r)
	for {
		line, rerr := br.ReadBytes('\n')
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return turns, consumed, nil
			}
			return turns, consumed, fmt.Errorf("pilog: read: %w", rerr)
		}
		consumed += int64(len(line))
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		ev, ok := parseEvent(line)
		if !ok {
			continue
		}
		t, ok := turnFromEvent(ev)
		if !ok {
			continue
		}
		tt := TimedTurn{Turn: t}
		tt.At, _ = eventTime(ev)
		if u := ev.Message.Usage; u != nil {
			tt.TokensIn = u.Input + u.CacheRead + u.CacheWrite
			tt.TokensOut = u.Output
		}
		turns = append(turns, tt)
	}
}
