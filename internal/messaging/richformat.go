// BL15 — rich-preview formatter for alert messages.
//
// Detects triple-backtick code blocks in plain alert text and emits
// the per-backend-flavoured equivalent. Pure helper; the messaging
// backends opt in by calling FormatAlert(text, backend.Name()).
//
// Recognised backends:
//   "telegram"  → MarkdownV2 fenced blocks, escapes Telegram metachars
//   "slack"     → Slack mrkdwn fenced (```...```) — already valid as-is
//   "discord"   → Discord Markdown — already valid as-is
//   "signal"    → plain text with leading "—" indents (Signal has no rich format)
//   anything else → plain text returned unchanged
//
// Operators turn the rich path on with session.alerts_rich_format: true.

package messaging

import "strings"

// FormatAlert returns the rich-formatted variant of the input for the
// given backend. enabled=false short-circuits to the input unchanged
// so backends can pass the operator's opt-in directly.
func FormatAlert(text, backend string, enabled bool) string {
	if !enabled || text == "" {
		return text
	}
	switch strings.ToLower(backend) {
	case "telegram":
		return formatTelegramMD(text)
	case "slack", "discord":
		return text // existing fenced blocks already render correctly
	case "signal":
		return formatSignalMono(text)
	default:
		return text
	}
}

// formatTelegramMD escapes a few MarkdownV2 metacharacters OUTSIDE
// fenced ``` ``` blocks. Inside fences, the text is delivered as-is
// (Telegram MarkdownV2 doesn't require escaping inside ```code```).
func formatTelegramMD(text string) string {
	var b strings.Builder
	b.Grow(len(text) + 16)

	inFence := false
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			b.WriteString(line)
			if i < len(lines)-1 {
				b.WriteByte('\n')
			}
			continue
		}
		if inFence {
			b.WriteString(line)
		} else {
			b.WriteString(escapeTelegramMD(line))
		}
		if i < len(lines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// escapeTelegramMD escapes the MarkdownV2 metacharacters that break
// rendering when used outside fenced code blocks. Conservative — we
// only escape the ones Telegram actually rejects.
func escapeTelegramMD(s string) string {
	const meta = "_*[]()~`>#+-=|{}.!"
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		if strings.ContainsRune(meta, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// formatSignalMono converts ``` fenced blocks to a Signal-friendly
// monospace approximation: prefix each line of the fenced region
// with " │ " so it visually stands out.
func formatSignalMono(text string) string {
	var b strings.Builder
	b.Grow(len(text) + 16)
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue // drop the fence line itself
		}
		if inFence {
			b.WriteString(" │ ")
			b.WriteString(line)
		} else {
			b.WriteString(line)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
