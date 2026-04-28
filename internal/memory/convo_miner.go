// v5.26.72 — Mempalace convo_miner.py / convo_scanner.py port,
// extra sources beyond the existing memory_import (Claude Code,
// ChatGPT, generic JSON).
//
// Three additional source parsers ship together so an operator
// can mine an entire history into the memory store without
// hand-rolling a converter:
//
//   - Slack export (channel JSON files, one message per object)
//   - IRC log (weechat / irssi / hexchat plain-text format)
//   - mbox email archive (RFC 4155 boundary-separated)
//
// Each parser returns []ImportedMessage which the caller hands
// to Retriever.SaveConvoSlice — same contract memory_import uses
// for the existing sources.

package memory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// ImportedMessage is the cross-source common shape. Each parser
// fills what it has; missing fields are zero-valued.
type ImportedMessage struct {
	Timestamp time.Time
	Author    string
	Channel   string
	Text      string
	Source    string // "slack", "irc", "email"
}

// ParseSlackExport reads a single Slack-channel export JSON file.
// Slack exports a JSON array per day per channel; this parser
// accepts either an array-of-messages or NDJSON form.
func ParseSlackExport(r io.Reader, channel string) ([]ImportedMessage, error) {
	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read slack export: %w", err)
	}
	body := bytes.TrimSpace(buf)
	if len(body) == 0 {
		return nil, nil
	}
	type slackMsg struct {
		Type     string `json:"type"`
		User     string `json:"user"`
		Username string `json:"username"`
		Text     string `json:"text"`
		Ts       string `json:"ts"`
	}
	var msgs []slackMsg
	if body[0] == '[' {
		if err := json.Unmarshal(body, &msgs); err != nil {
			return nil, fmt.Errorf("parse slack json array: %w", err)
		}
	} else {
		// NDJSON
		sc := bufio.NewScanner(bytes.NewReader(body))
		sc.Buffer(make([]byte, 1<<20), 1<<22)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var m slackMsg
			if err := json.Unmarshal([]byte(line), &m); err == nil {
				msgs = append(msgs, m)
			}
		}
	}
	out := make([]ImportedMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Type != "" && m.Type != "message" {
			continue
		}
		if strings.TrimSpace(m.Text) == "" {
			continue
		}
		ts := time.Time{}
		if m.Ts != "" {
			// Slack ts is "1754399999.000200" — float seconds.
			parts := strings.SplitN(m.Ts, ".", 2)
			if sec, err := time.ParseDuration(parts[0] + "s"); err == nil {
				ts = time.Unix(int64(sec.Seconds()), 0)
			}
		}
		author := m.User
		if m.Username != "" {
			author = m.Username
		}
		out = append(out, ImportedMessage{
			Timestamp: ts, Author: author, Channel: channel,
			Text: m.Text, Source: "slack",
		})
	}
	return out, nil
}

// ircLineRe matches the common weechat/irssi/hexchat shape:
//   "2026-04-28 14:32:11 <nick> message body"
//   "Apr 28 14:32:11 <nick> message body"
var ircLineRe = regexp.MustCompile(
	`^(?:(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})|(\w{3}\s+\d+\s+\d{2}:\d{2}:\d{2}))\s+<([^>]+)>\s+(.*)$`,
)

// ParseIRCLog reads a plaintext IRC log and emits one message per
// matched line. Non-matching lines (joins/parts/notices) are
// skipped. Channel is supplied by the caller since most IRC log
// formats embed it in the filename, not the content.
func ParseIRCLog(r io.Reader, channel string) ([]ImportedMessage, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	var out []ImportedMessage
	for sc.Scan() {
		line := sc.Text()
		m := ircLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var ts time.Time
		if m[1] != "" {
			if t, err := time.Parse("2006-01-02 15:04:05", m[1]); err == nil {
				ts = t
			}
		} else if m[2] != "" {
			// "Apr 28 14:32:11" — assume current year for the import pass.
			if t, err := time.Parse("Jan 2 15:04:05 2006",
				m[2]+" "+fmt.Sprintf("%d", time.Now().Year())); err == nil {
				ts = t
			}
		}
		out = append(out, ImportedMessage{
			Timestamp: ts, Author: m[3], Channel: channel,
			Text: strings.TrimSpace(m[4]), Source: "irc",
		})
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("scan irc log: %w", err)
	}
	return out, nil
}

// ParseMboxEmail reads an mbox archive and emits one message per
// email. Body extraction prefers text/plain over text/html. Each
// email becomes one ImportedMessage; threading reconstruction is
// out of scope (mempalace's port doesn't do it either).
func ParseMboxEmail(r io.Reader) ([]ImportedMessage, error) {
	br := bufio.NewReader(r)
	var (
		out     []ImportedMessage
		current bytes.Buffer
	)
	flush := func() {
		raw := strings.TrimSpace(current.String())
		current.Reset()
		if raw == "" {
			return
		}
		msg, err := mail.ReadMessage(strings.NewReader(raw))
		if err != nil {
			return
		}
		body, _ := io.ReadAll(msg.Body)
		text := string(body)
		ts, _ := mail.ParseDate(msg.Header.Get("Date"))
		from := msg.Header.Get("From")
		if addr, err := mail.ParseAddress(from); err == nil {
			from = addr.Address
		}
		out = append(out, ImportedMessage{
			Timestamp: ts, Author: from,
			Channel: msg.Header.Get("Subject"),
			Text:    strings.TrimSpace(text),
			Source:  "email",
		})
	}
	for {
		line, err := br.ReadString('\n')
		if strings.HasPrefix(line, "From ") && current.Len() > 0 {
			flush()
		}
		if !strings.HasPrefix(line, "From ") || current.Len() == 0 {
			current.WriteString(line)
		} else {
			current.WriteString(line)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, fmt.Errorf("scan mbox: %w", err)
		}
	}
	flush()
	return out, nil
}

// SaveImportedMessages persists a slice of ImportedMessage rows into
// the memory store under the supplied projectDir + namespace. Each
// row gets role="manual" with Source set to "channel:slack" /
// "channel:irc" / "channel:email" so the corpus_origin path picks
// them up. Returns the number of rows actually written (post-dedup).
func (s *Store) SaveImportedMessages(projectDir, namespace string, msgs []ImportedMessage) (int, error) {
	written := 0
	for _, m := range msgs {
		if strings.TrimSpace(m.Text) == "" {
			continue
		}
		summary := ""
		if m.Author != "" {
			summary = m.Author
		}
		if m.Channel != "" {
			if summary == "" {
				summary = m.Channel
			} else {
				summary = m.Author + " in " + m.Channel
			}
		}
		id, err := s.SaveWithNamespaceAndSource(projectDir, m.Text, summary,
			"manual", "", "channel:"+m.Source, namespace, "", "", "", nil)
		if err != nil {
			continue
		}
		if id > 0 {
			written++
		}
	}
	return written, nil
}
