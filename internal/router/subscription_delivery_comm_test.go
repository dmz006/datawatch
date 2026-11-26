// The chat subscription list shows when a one-time message has not been typed,
// with its reason code and never its text.

package router

import (
	"strings"
	"testing"
)

func TestCommSubscriptionLineShowsAnUndeliveredMessage(t *testing.T) {
	rec, err := decodeCommSubRecord(`{"id":"sub-1","filter":{"kinds":["task.failed"]},"sink":{"type":"session","target":"ops"},` +
		`"one_shot":true,"delivery":{"state":"not_typed","reason":"prompt_on_screen","target":"ops","text":"words that stay out of chat"}}`)
	if err != nil {
		t.Fatal(err)
	}
	got := renderCommSubLine(rec)
	for _, want := range []string{"delivery=not_typed", "reason=prompt_on_screen"} {
		if !strings.Contains(got, want) {
			t.Errorf("line lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "words that stay out of chat") {
		t.Errorf("line carries the message text: %s", got)
	}
	plain, err := decodeCommSubRecord(`{"id":"sub-2","filter":{"kinds":["task.failed"]},"sink":{"type":"comm","target":"c"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := renderCommSubLine(plain); strings.Contains(got, "delivery=") {
		t.Errorf("a subscription with nothing waiting shows a delivery state: %s", got)
	}
}
