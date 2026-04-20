// BL15 — rich-preview formatter tests.

package messaging

import (
	"strings"
	"testing"
)

func TestBL15_FormatAlert_Disabled_Passthrough(t *testing.T) {
	in := "Hello *world*"
	if got := FormatAlert(in, "telegram", false); got != in {
		t.Errorf("disabled should pass through; got %q", got)
	}
}

func TestBL15_FormatAlert_UnknownBackend_Passthrough(t *testing.T) {
	in := "Hello"
	if got := FormatAlert(in, "carrier-pigeon", true); got != in {
		t.Errorf("unknown backend should pass through; got %q", got)
	}
}

func TestBL15_FormatAlert_Telegram_EscapesMeta(t *testing.T) {
	got := FormatAlert("hello *world*", "telegram", true)
	// `*` outside a fence must be escaped.
	if !strings.Contains(got, `\*world\*`) {
		t.Errorf("telegram should escape *: %q", got)
	}
}

func TestBL15_FormatAlert_Telegram_PreservesFenceContent(t *testing.T) {
	in := "before\n```\nplain *star*\n```\nafter"
	got := FormatAlert(in, "telegram", true)
	// Inside the fence, `*` is NOT escaped.
	if !strings.Contains(got, "plain *star*") {
		t.Errorf("fence content corrupted: %q", got)
	}
	// Outside, `before` and `after` have no metas.
	if strings.Contains(got, `\*`) {
		// Only metas from the surrounding text would produce escapes;
		// neither "before" nor "after" contains them.
		t.Errorf("over-escaped: %q", got)
	}
}

func TestBL15_FormatAlert_Signal_Mono(t *testing.T) {
	in := "before\n```\nx = 1\ny = 2\n```\nafter"
	got := FormatAlert(in, "signal", true)
	// Fence lines are converted to " │ " prefixed lines; fence markers themselves are dropped.
	if !strings.Contains(got, " │ x = 1") {
		t.Errorf("missing mono prefix: %q", got)
	}
	if strings.Contains(got, "```") {
		t.Errorf("fence markers should be dropped for signal: %q", got)
	}
}

func TestBL15_FormatAlert_Slack_Passthrough(t *testing.T) {
	in := "before\n```code\nx = 1\n```\nafter"
	if got := FormatAlert(in, "slack", true); got != in {
		t.Errorf("slack should pass through: %q", got)
	}
}

func TestBL15_FormatAlert_EmptyInput(t *testing.T) {
	if got := FormatAlert("", "telegram", true); got != "" {
		t.Errorf("empty in → empty out, got %q", got)
	}
}
