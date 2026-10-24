package session

import "testing"

// Output between two OSC sequences closed with ESC \ (shell-integration marks)
// must survive stripping, so a completion marker printed there is still found.
func TestStripANSIKeepsOutputBetweenOSCSequences(t *testing.T) {
	raw := "\x1b]3008;start=a;type=command\x1b\\worker tick\r\n" +
		"DATAWATCH_COMPLETE: worker finished\r\n" +
		"\x1b]3008;end=a;exit=success\x1b\\\x1b]3008;start=b;type=shell\x1b\\prompt$ "
	got := StripANSI(raw)
	if want := "worker tick\r\nDATAWATCH_COMPLETE: worker finished\r\nprompt$ "; got != want {
		t.Fatalf("StripANSI() = %q, want %q", got, want)
	}
	if sum := ExtractCompletionSummary(raw); sum != "worker finished" {
		t.Fatalf("ExtractCompletionSummary() = %q, want %q", sum, "worker finished")
	}
}

func TestStripANSIOSCWithBellTerminator(t *testing.T) {
	if got := StripANSI("\x1b]0;title\x07text"); got != "text" {
		t.Fatalf("StripANSI() = %q, want %q", got, "text")
	}
}
