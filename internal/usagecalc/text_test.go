package usagecalc

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// Text-form tests for FormatCLI and FormatComm: the design doc's field order,
// the conditional lines, the no-reading empty state, the 80/50 column caps,
// and the comm form's closing reply line.

func advResp(t testing.TB, in Input) Response {
	t.Helper()
	a := Advise(in)
	return Response{
		Variant: VariantAll,
		Policy:  a.Advice.Policy,
		Advice:  a,
	}
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	var b strings.Builder
	for _, r := range s {
		if r == ' ' {
			if b.Len() == 0 || b.String()[b.Len()-1] != ' ' {
				b.WriteByte(' ')
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func mustContainAll(t *testing.T, name, got string, subs ...string) {
	t.Helper()
	norm := normalize(got)
	for _, sub := range subs {
		if !strings.Contains(norm, normalize(sub)) {
			t.Errorf("%s does not contain %q\nfull output:\n%s", name, sub, got)
		}
	}
}

func noSubs(t *testing.T, name, got string, subs ...string) {
	t.Helper()
	norm := normalize(got)
	for _, sub := range subs {
		if strings.Contains(norm, normalize(sub)) {
			t.Errorf("%s must not contain %q\nfull output:\n%s", name, sub, got)
		}
	}
}

func assertWidth(t *testing.T, name, got string, width int) {
	t.Helper()
	for i, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if len(line) > width {
			t.Errorf("%s: line %d is %d columns, want at most %d: %q", name, i, len(line), width, line)
		}
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("%s should end with a newline", name)
	}
}

func lastLine(s string) string {
	s = strings.TrimSuffix(s, "\n")
	i := strings.LastIndexByte(s, '\n')
	if i < 0 {
		return s
	}
	return s[i+1:]
}

func position(haystack, needle string) int {
	return strings.Index(normalize(haystack), needle)
}

func TestFormatCLIWorkedExample(t *testing.T) {
	resp := advResp(t, workedExample(t))
	cli := FormatCLI(resp)

	mustContainAll(t, "cli", cli,
		"usage: 72% read Fri 22:08 EDT, reset Sun 08:00 EDT (1.41 days left)",
		"pace line 79.8 points, headroom 7.8 points, 5.59 days since reset",
		"burn 24.1 points per 24h recent, 12.9 points per 24h week average",
		"projected end 106% recent (over budget), 90.2% week average (within budget)",
		"runs out Sun 02:02 EDT, about 5.96 hours before the reset (recent burn)",
		"verdict: over budget at the recent burn; within budget at the week average",
		"lanes 3 running",
		"policy Run until used up",
		"remaining work 20 points (typed); 0 queued tasks",
		"advice: Over budget at the recent burn:",
	)

	// Design doc field order: reading → reset → pace → burn → projected end →
	// run out → verdict → lanes & policy → advice.
	norm := normalize(cli)
	order := []string{
		"usage:", "reset", "pace line", "burn ", "projected end",
		"runs out", "verdict:", "lanes 3 running", "policy Run until used up", "advice:",
	}
	for i := 1; i < len(order); i++ {
		prev, cur := strings.Index(norm, order[i-1]), strings.Index(norm, order[i])
		if prev < 0 || cur < 0 || prev >= cur {
			t.Errorf("field order broken at %q vs %q (prev=%d cur=%d)\nfull output:\n%s",
				order[i-1], order[i], prev, cur, cli)
		}
	}
	assertWidth(t, "cli", cli, CLIWidth)

	// The comm form carries the same fields and closes with the reply line.
	comm := FormatComm(resp)
	for _, sub := range []string{"usage:", "pace line", "burn ", "projected end", "runs out", "verdict:", "advice:"} {
		if !strings.Contains(comm, sub) {
			t.Errorf("comm form missing %q\nfull output:\n%s", sub, comm)
		}
	}
	// ...and the same figures as the CLI form, not just its labels: 72
	// percent, the 79.8 pace line, the 24.1/12.9 burns, and the 106 percent
	// recent (90.2 percent average) projection.
	mustContainAll(t, "comm figures", comm,
		"72%",
		"79.8",
		"24.1",
		"12.9",
		"106",
		"90.2",
	)
	if got := strings.TrimSpace(lastLine(comm)); !strings.HasPrefix(got, "Reply:") {
		t.Errorf("comm form must end with the reply line, got %q", got)
	}
	assertWidth(t, "comm", comm, CommWidth)
}

// TestFormatWidths pins the column caps with the longest real inputs: the
// design doc's worked example, a stale copy with its network error, a
// non-zero extra credit, and a long zone name. No line in either form may
// exceed its cap.
func TestFormatWidths(t *testing.T) {
	t.Run("worked example", func(t *testing.T) {
		assertFormWidths(t, advResp(t, workedExample(t)))
	})
	t.Run("stale copy", func(t *testing.T) {
		in := workedExample(t)
		a := Advise(in)
		copiedAt := time.Date(2026, 10, 24, 9, 12, 25, 0, time.UTC)
		assertFormWidths(t, Response{
			Policy:      a.Advice.Policy,
			Stale:       true,
			StaleReason: `keeper "alpha" unreachable: dial tcp 10.0.0.5:9138: no route to host`,
			CopiedAt:    &copiedAt,
			Advice:      a,
		})
	})
	t.Run("extra credit", func(t *testing.T) {
		in := workedExample(t)
		in.ExtraCreditPoints = 7
		assertFormWidths(t, advResp(t, in))
	})
	t.Run("long zone name", func(t *testing.T) {
		_, err := time.LoadLocation("Australia/Broken_Hill")
		if err != nil {
			t.Skipf("Australia/Broken_Hill not available: %v", err)
		}
		loc, _ := time.LoadLocation("Australia/Broken_Hill")
		in := Input{
			Readings: []Reading{{
				ID:      "acdt",
				At:      time.Date(2026, 11, 15, 2, 0, 0, 0, loc),
				Percent: f64(28),
				Source:  "typed",
			}},
			Reset: ResetSpec{Weekday: time.Sunday, Clock: "08:00", Zone: "Australia/Broken_Hill"},
		}
		assertFormWidths(t, advResp(t, in))
	})
}

func assertFormWidths(t *testing.T, resp Response) {
	t.Helper()
	assertWidth(t, "cli", FormatCLI(resp), CLIWidth)
	assertWidth(t, "comm", FormatComm(resp), CommWidth)
}

func TestFormatExtraCreditLine(t *testing.T) {
	in := workedExample(t)
	in.ExtraCreditPoints = 7
	resp := advResp(t, in)
	cli := FormatCLI(resp)

	mustContainAll(t, "cli", cli, "extra credit +7 points, ceiling 107 points")
	if i := position(cli, "extra credit +7 points"); i < 0 || i >= position(cli, "pace line") {
		t.Errorf("the extra-credit line comes before the pace line\nfull output:\n%s", cli)
	}
}

func TestFormatRemainingWorkLines(t *testing.T) {
	// Typed figure wins, shown with the queued task count.
	typed := advResp(t, workedExample(t))
	mustContainAll(t, "typed", FormatCLI(typed),
		"remaining work 20 points (typed); 0 queued tasks",
	)

	// Estimate: queued tasks priced at the cost per task.
	in := workedExample(t)
	in.RemainingWork = RemainingWork{QueueTasks: 8, CostPerTask: 2}
	est := advResp(t, in)
	mustContainAll(t, "estimate", FormatCLI(est),
		"remaining work 16 points (estimate: 8 queued tasks at 2 points each)",
	)

	// Nothing typed or priced, but the count is still visible.
	in.RemainingWork = RemainingWork{QueueTasks: 1}
	none := advResp(t, in)
	mustContainAll(t, "none", FormatCLI(none), "remaining work not set; 1 queued task")
}

func TestFormatFallbackNotice(t *testing.T) {
	// A previous reading only 68 minutes back: the 2-hour rule sends the recent
	// burn on the week average, with the notice instead of a second silent figure.
	in := workedExample(t)
	in.Readings = []Reading{
		percentReading(at(t, 2026, 10, 23, 21, 0), 67),
		percentReading(at(t, 2026, 10, 23, 22, 8), 72),
	}
	resp := advResp(t, in)
	cli := FormatCLI(resp)

	mustContainAll(t, "cli", cli,
		"note: No earlier reading at least 2 hours back; the week average is used, so this figure is smoother than the recent burn.",
	)
	assertWidth(t, "cli", cli, CLIWidth)
}

func TestFormatSessionLine(t *testing.T) {
	in := workedExample(t)
	in.Readings[1].SessionPercent = f64(45)
	resp := advResp(t, in)

	cli := FormatCLI(resp)
	mustContainAll(t, "cli", cli, "session 45% (secondary, no verdict)")
	noSubs(t, "cli", cli, " (verdict")

	comm := FormatComm(resp)
	mustContainAll(t, "comm", comm, "(secondary, no verdict)")
	assertWidth(t, "comm", comm, CommWidth)
}

func TestFormatStaleCopyLine(t *testing.T) {
	in := workedExample(t)
	a := Advise(in)
	copiedAt := time.Date(2026, 10, 24, 9, 12, 25, 0, time.UTC)
	resp := Response{
		Policy:      a.Advice.Policy,
		Stale:       true,
		StaleReason: `keeper "alpha" unreachable: dial tcp 10.0.0.5:9138: no route to host`,
		CopiedAt:    &copiedAt,
		Advice:      a,
	}

	cli := FormatCLI(resp)
	mustContainAll(t, "cli", cli,
		`stale: using the last copy taken 2026-10-24 09:12:25; keeper "alpha" unreachable: dial tcp 10.0.0.5:9138: no route to host`,
	)

	comm := FormatComm(resp)
	mustContainAll(t, "comm", comm, `keeper "alpha" unreachable`)
	assertWidth(t, "comm", comm, CommWidth)
}

func TestFormatNoReadingEmptyState(t *testing.T) {
	in := workedExample(t)
	in.Readings = nil
	resp := advResp(t, in)
	if resp.Advice.Needs != NeedsReading {
		t.Fatalf("Needs = %q, want %q", resp.Advice.Needs, NeedsReading)
	}

	cli := FormatCLI(resp)
	mustContainAll(t, "cli", cli,
		"usage: no weekly readings yet",
		"verdict: none yet; no weekly reading",
		"advice: Enter a weekly reading to see advice.",
	)
	// No reading: no pace, burn, projection, run-out or work figures.
	noSubs(t, "cli", cli, "pace line", "burn ", "projected end", "runs out", "remaining work", "to spare")

	comm := FormatComm(resp)
	if got := strings.TrimSpace(lastLine(comm)); !strings.HasPrefix(got, "Reply:") {
		t.Errorf("empty-state comm form must end with the reply line, got %q", got)
	}
	assertWidth(t, "comm", comm, CommWidth)
}

func TestFormatResetNotSetState(t *testing.T) {
	in := workedExample(t)
	in.Reset = ResetSpec{}
	resp := advResp(t, in)
	if resp.Advice.Needs != NeedsReset {
		t.Fatalf("Needs = %q, want %q", resp.Advice.Needs, NeedsReset)
	}

	cli := FormatCLI(resp)
	mustContainAll(t, "cli", cli,
		"usage: the weekly reset is not set yet",
		"advice: Set the weekly reset as a weekday, an HH:MM time and a time zone name.",
	)
}

func TestFormatSpendOnlyState(t *testing.T) {
	in := workedExample(t)
	in.Readings = []Reading{{
		ID:       "spend-only",
		At:       at(t, 2026, 10, 23, 22, 8),
		SpendUSD: f64(12.4),
		Source:   "typed",
	}}
	resp := advResp(t, in)
	if resp.Advice.Needs != NeedsPercent {
		t.Fatalf("Needs = %q, want %q", resp.Advice.Needs, NeedsPercent)
	}

	cli := FormatCLI(resp)
	// The model still renders the reading (with its missing percent) and carries its
	// own spine: the verdict, the advice headline, and the work figure.
	mustContainAll(t, "cli", cli,
		"usage: 0% read Fri 22:08 EDT",
		"verdict: none yet; a weekly percentage is needed",
		"remaining work 20 points (typed); 0 queued tasks",
		"advice: A weekly percentage is needed: the newest reading carries only a spend figure, and spend readings are stored but not yet turned into a verdict.",
	)
	assertWidth(t, "cli", cli, CLIWidth)
}

func TestFormatFinishEarlyForm(t *testing.T) {
	in := workedExample(t)
	in.Policy = PolicyFinishEarly
	resp := advResp(t, in)
	if resp.Policy != PolicyFinishEarly {
		t.Fatalf("Policy = %q, want finish-early", resp.Policy)
	}

	cli := FormatCLI(resp)
	mustContainAll(t, "cli", cli,
		"policy Run flat out",
		"work of 20 points at 3 lanes finishes Sat 20:54 EDT, 5.2 points to spare, in about 22.77 hours",
		"advice: Within budget: the work finishes in about 22.8 hours with 5.2 points of allowance to spare.",
	)

	// The finish-early form keeps the fields: reading, pace line, burn, run out,
	// finish, remaining work, advice.
	for _, sub := range []string{"usage:", "pace line", "burn ", "runs out", "to spare", "remaining work", "advice:"} {
		if !strings.Contains(cli, sub) {
			t.Errorf("finish-early CLI form missing %q\nfull output:\n%s", sub, cli)
		}
	}
	assertWidth(t, "cli", cli, CLIWidth)

	comm := FormatComm(resp)
	mustContainAll(t, "comm", comm,
		"5.2 points to spare",
		"Reply: usage advice lanes=3",
	)
}

func TestFormatVariantDoesNotChangeText(t *testing.T) {
	in := workedExample(t)
	a := advResp(t, in)
	b := advResp(t, in)
	b.Variant = VariantBurnLedger // the variant is display state only
	if FormatCLI(a) != FormatCLI(b) {
		t.Error("variant changes the CLI text")
	}
	if FormatComm(a) != FormatComm(b) {
		t.Error("variant changes the comm text")
	}
}

func TestFormatLongZoneName(t *testing.T) {
	_, err := time.LoadLocation("Australia/Broken_Hill")
	if err != nil {
		t.Skipf("Australia/Broken_Hill not available: %v", err)
	}
	loc, _ := time.LoadLocation("Australia/Broken_Hill")
	in := Input{
		Readings: []Reading{{
			ID:      "acdt",
			At:      time.Date(2026, 11, 15, 2, 0, 0, 0, loc),
			Percent: f64(28),
			Source:  "typed",
		}},
		Reset: ResetSpec{Weekday: time.Sunday, Clock: "08:00", Zone: "Australia/Broken_Hill"},
	}
	resp := advResp(t, in)
	if resp.Advice.Needs != "" {
		t.Fatalf("Needs = %q, want none", resp.Advice.Needs)
	}

	cli := FormatCLI(resp)
	mustContainAll(t, "cli", cli, "reset Sun 08:00 ACDT")
	assertWidth(t, "cli", cli, CLIWidth)
	assertWidth(t, "comm", FormatComm(resp), CommWidth)
}

func TestFormatReplyLine(t *testing.T) {
	// With a usable advice the reply line proposes the lanes the model suggests.
	in := workedExample(t)
	resp := advResp(t, in)
	if resp.Advice.Advice.LanesSuggested <= 0 {
		t.Fatalf("LanesSuggested = 0, want 1 or more")
	}
	want := "Reply: usage advice lanes=" + strconv.Itoa(resp.Advice.Advice.LanesSuggested)
	if got := FormatComm(resp); !strings.Contains(got, want) {
		t.Errorf("comm form should propose the suggested lanes (%s):\n%s", want, got)
	}

	// With no readings it points at the first reading.
	in.Readings = nil
	empty := advResp(t, in)
	if got := FormatComm(empty); !strings.Contains(got, "Reply: usage reading add --percent N") {
		t.Errorf("empty-state comm form should propose the first reading:\n%s", got)
	}
}
