package autonomous

import (
	"strings"
	"testing"
)

func TestSpecCheckFormat_CleanGivesNothing(t *testing.T) {
	f := SpecCheckFindings("Add a retry to the backup job.")
	if f == nil || len(f) != 0 {
		t.Fatalf("clean spec must give an empty non-nil slice, got %#v", f)
	}
	if got := FormatSpecCheckBlock(f); got != "" {
		t.Fatalf("clean block = %q", got)
	}
	if len(FormatSpecFindings(f)) != 0 {
		t.Fatal("clean spec must format to no lines")
	}
}

func TestSpecCheckFormat_LineCarriesPhraseAndSuggestion(t *testing.T) {
	f := SpecCheckFindings("Fix the crash described in BL123 for the Automaton.")
	if len(f) == 0 {
		t.Fatal("expected a finding for a fixed release number")
	}
	line := FormatSpecFinding(f[0])
	for _, want := range []string{f[0].Severity, f[0].Rule, f[0].Phrase, f[0].Suggestion, "daemon can fix"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q lacks %q", line, want)
		}
	}
	if strings.Contains(line, "\n") {
		t.Fatal("a finding must be one line")
	}
}

func TestSpecCheckFormat_ErrorFindingNotFixable(t *testing.T) {
	f := SpecCheckFindings("Call the host at 192.168.1.20 please.")
	if len(f) != 1 || f[0].Correctable {
		t.Fatalf("want one non-correctable finding, got %#v", f)
	}
	if strings.Contains(FormatSpecFinding(f[0]), "daemon can fix") {
		t.Fatal("an uncorrectable finding must not promise a daemon fix")
	}
}

func TestSpecCheckFormat_ReplyRoundTrip(t *testing.T) {
	body := []byte(`{"id":"x","spec_check":[{"rule":"names_tracker_id","phrase":"BL12","severity":"warning","correctable":true,"suggestion":"drop it"}]}`)
	block := SpecCheckBlockFromReply(body)
	if !strings.Contains(block, "1 finding(s)") || !strings.Contains(block, "BL12") {
		t.Fatalf("block = %q", block)
	}
	if SpecCheckFromReply([]byte("not json")) != nil || SpecCheckBlockFromReply([]byte(`{"id":"x"}`)) != "" {
		t.Fatal("a reply without spec_check must give nothing")
	}
}
