// text.go — the CLI and comm-channel text forms of one advice.
//
// Design (docs/plans/2026-10-23-usage-calculator-design.md, "The three forms"):
// the CLI prints the full text, the comm channel a compact text form that
// ends with the reply line the operator can send back. Both render the same
// Response the GET /api/usage/advice response carries, in the design doc's
// field order:
//
//	reading → reset → pace line → burn → projected end → run out →
//	verdict → lanes & policy → advice
//
// with the conditional lines that the response carries:
//
//   - the extra-credit line, with the ceiling, when extra credit is on top of
//     the 100-point weekly allowance;
//   - the remaining-work line: the typed figure, or the per-task estimate with
//     the queued task count, or "not set" with the count;
//   - the 2-hour fallback notice when the recent burn fell back to the week
//     average (one burn figure, with the reason, not a second silent number);
//   - the session line, always secondary and never a verdict of its own;
//   - the stale-copy line with the keeper name and when the copy was taken.
//
// Neither form invents verdict wording: the verdict, headline and notes are
// the reference model's own, verbatim. The display variant picks only the web
// page's rendering; it changes none of these texts.
//
// Widths: the CLI form wraps at CLIWidth (80) columns, the comm form at
// CommWidth (50). Numbers use the package's one-decimal display precision;
// clock figures (days, hours) keep the two-decimal precision the design's
// worked example shows.
package usagecalc

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// CLIWidth and CommWidth cap the rendered line widths of the two forms.
const (
	CLIWidth  = 80
	CommWidth = 50
)

// Response is the shape GET /api/usage/advice serves: one advice with its
// resolved selection and the series provenance (stale copy, session).
type Response = AdviceResult

// FormatCLI renders the CLI form of resp: one sentence per line, in the
// design doc's field order, every line at or under CLIWidth columns.
func FormatCLI(resp Response) string {
	return renderText(resp, CLIWidth, false)
}

// FormatComm renders the comm-channel form of resp: the same fields and order
// as FormatCLI, every line at or under CommWidth columns, closing with the
// reply line the operator can send back.
func FormatComm(resp Response) string {
	return renderText(resp, CommWidth, true)
}

func renderText(resp Response, width int, comm bool) string {
	lines := textLines(resp)
	if comm {
		lines = append(lines, replyLine(resp))
	}
	var b strings.Builder
	first := true
	for _, l := range lines {
		for _, wl := range wordWrap(l, width) {
			if !first {
				b.WriteByte('\n')
			}
			b.WriteString(wl)
			first = false
		}
	}
	b.WriteByte('\n')
	return b.String()
}

// textLines is the shared field list in the design doc's order. The two forms
// differ only in wrapping and the comm form's final reply line.
func textLines(resp Response) []string {
	ad := resp.Advice
	var lines []string

	// reading and reset, with the reset time in the reset zone.
	switch {
	case ad.ReadAt != nil:
		lines = append(lines, readLine(ad))
	case ad.Needs == NeedsReset:
		lines = append(lines, "usage: the weekly reset is not set yet (weekday, HH:MM clock time, zone name)")
	default:
		lines = append(lines, "usage: no weekly readings yet")
	}

	if ad.ExtraCreditPoints > 0 {
		lines = append(lines, fmt.Sprintf("extra credit +%s points, ceiling %s points",
			txtPoints(ad.ExtraCreditPoints), txtPoints(ad.Ceiling)))
	}

	full := ad.ReadAt != nil

	if full {
		lines = append(lines, paceLine(ad))
		lines = append(lines, fmt.Sprintf("burn %s points per 24h recent, %s points per 24h week average",
			txtPoints(ad.RecentBurnPer24h), txtPoints(ad.WeekAvgBurnPer24h)))
		if ad.FallbackNotice != "" {
			lines = append(lines, "note: "+ad.FallbackNotice)
		}
		lines = append(lines, fmt.Sprintf("projected end %s%% recent (%s), %s%% week average (%s)",
			txtPoints(ad.ProjectedEndRecent), verdictLabel(ad.Verdict),
			txtPoints(ad.ProjectedEndAvg), verdictLabel(ad.VerdictAvg)))
		lines = append(lines, runOutLine(ad))
	}
	lines = append(lines, verdictLine(ad))
	lines = append(lines, laneLines(ad)...)

	if full {
		if ad.Lanes > 0 {
			lines = append(lines, fmt.Sprintf("lanes %d running", ad.Lanes))
		}
		lines = append(lines, "policy "+policyLabel(ad.Advice.Policy))
		lines = append(lines, workLine(ad.RemainingWork))
		if line := finishLine(ad); line != "" {
			lines = append(lines, line)
		}
	}
	lines = append(lines, "advice: "+ad.Advice.Headline)
	for _, n := range ad.Advice.Notes {
		lines = append(lines, "  - "+n)
	}

	if ad.Session != nil {
		lines = append(lines, fmt.Sprintf("session %s%% (secondary, no verdict)", txtPoints(ad.Session.Percent)))
	}

	if resp.Stale {
		lines = append(lines, staleLine(resp))
	}

	return lines
}

// laneLines states the three lane numbers and says what each is: the Claude lanes running
// now (the ledger count), the lanes stamped on the newest usable reading, and, when the
// caller set one, the what-if lanes, marked as not saved. The lane table and the headline
// work off the chosen count; these lines never change with the policy.
func laneLines(ad Advice) []string {
	lines := []string{
		"Claude lanes running now (ledger count): " + laneCount(ad.LanesRunningNow, "no ledger"),
		"Lanes stamped on the newest reading: " + laneCount(ad.LanesStamped, "none stamped"),
	}
	if ad.WhatIfLanes != nil {
		lines = append(lines, "What-if lanes (not saved): "+strconv.Itoa(*ad.WhatIfLanes))
	}
	return lines
}

func laneCount(n *int, missing string) string {
	if n == nil {
		return "unknown (" + missing + ")"
	}
	return strconv.Itoa(*n)
}

// readLine is the opening line: the latest reading, the reset time in the
// reset zone, and the days left.
func readLine(ad Advice) string {
	reset := "not set"
	if ad.NextResetAt != nil {
		reset = clockStr(*ad.NextResetAt)
		if ad.ServerZone {
			reset += " (server zone)"
		}
	}
	return fmt.Sprintf("usage: %s%% read %s, reset %s (%s days left)",
		txtPoints(ad.Percent),
		clockStr(*ad.ReadAt),
		reset,
		txtHours(ad.DaysLeft))
}

func paceLine(ad Advice) string {
	ahead := ""
	if ad.AheadOfPace {
		ahead = ", " + txtPoints(ad.Percent-ad.PaceLine) + " points ahead of the pace line"
	} else {
		ahead = ", headroom " + txtPoints(ad.Headroom) + " points"
	}
	return fmt.Sprintf("pace line %s points%s, %s days since reset",
		txtPoints(ad.PaceLine), ahead, txtHours(ad.DaysSinceReset))
}

func runOutLine(ad Advice) string {
	switch {
	case ad.RunOutAtRecent != nil:
		return fmt.Sprintf("runs out %s, about %s hours before the reset (recent burn)",
			clockStr(*ad.RunOutAtRecent), txtHours(ad.HoursEarlyRecent))
	case ad.RunOutAtAvg != nil:
		return fmt.Sprintf("runs out %s, about %s hours before the reset (week average)",
			clockStr(*ad.RunOutAtAvg), txtHours(ad.HoursEarlyAvg))
	default:
		return "runs out: not before the reset"
	}
}

// finishLine is the Run flat out finish: the work points, all the lanes, the
// finish time, the points to spare and the hours, the design doc's line "Work
// of 20 points at 3 lanes finishes Sat 20:54, 5.2 points to spare."
func finishLine(ad Advice) string {
	f := ad.Advice.Finish
	if f == nil || f.NoBurn || f.At == nil {
		return ""
	}
	lanes := ad.Advice.LanesSuggested
	if lanes <= 0 {
		lanes = ad.Lanes
	}
	line := "work of " + txtPoints(ad.RemainingWork.Points) + " points at " +
		lanesWord(lanes) + " finishes " + clockStr(*f.At)
	if f.SparePoints != nil {
		line += ", " + txtPoints(*f.SparePoints) + " points to spare"
	}
	if f.Hours != nil {
		line += ", in about " + txtHours(*f.Hours) + " hours"
	}
	return line
}

// verdictLine shows only the verdict vocabulary the reference model uses.
func verdictLine(ad Advice) string {
	var parts []string
	if ad.Verdict != "" {
		parts = append(parts, verdictLabel(ad.Verdict)+" at the recent burn")
	}
	if ad.VerdictAvg != "" {
		parts = append(parts, verdictLabel(ad.VerdictAvg)+" at the week average")
	}
	if len(parts) > 0 {
		return "verdict: " + strings.Join(parts, "; ")
	}
	switch ad.Needs {
	case NeedsReset:
		return "verdict: none yet; the weekly reset is not set"
	case NeedsPercent:
		return "verdict: none yet; a weekly percentage is needed"
	default:
		return "verdict: none yet; no weekly reading"
	}
}

func verdictLabel(v string) string {
	switch v {
	case VerdictOver:
		return "over budget"
	case VerdictWithin:
		return "within budget"
	default:
		return "none"
	}
}

// workLine is the remaining-work line: the typed figure when the operator
// typed one, otherwise the per-task estimate, always with the queued count.
func workLine(w WorkResult) string {
	queue := fmt.Sprintf("%d queued task%s", w.QueueTasks, pluralS(w.QueueTasks))
	switch w.InUse {
	case WorkInUseTyped:
		return fmt.Sprintf("remaining work %s points (typed); %s", txtPoints(w.Points), queue)
	case WorkInUseEstimate:
		return fmt.Sprintf("remaining work %s points (estimate: %s at %s points each)",
			txtPoints(w.Points), queue, txtCost(w.CostPerTask))
	default:
		return fmt.Sprintf("remaining work not set; %s", queue)
	}
}

func policyLabel(id string) string {
	if l := PolicyLabel(id); l != "" {
		return l
	}
	if id == "" {
		return PolicyLabel(PolicyUseAll)
	}
	return id
}

// staleLine names the keeper and when the last copy was taken.
func staleLine(resp Response) string {
	s := "stale: using the last copy"
	if resp.CopiedAt != nil {
		s += " taken " + resp.CopiedAt.Format("2006-01-02 15:04:05")
	}
	if r := strings.TrimSpace(resp.StaleReason); r != "" {
		s += "; " + r
	}
	return s
}

// replyLine is the comm form's closing line: the next command the operator
// can send back.
func replyLine(resp Response) string {
	if resp.Advice.Advice.LanesSuggested > 0 {
		return "Reply: usage advice lanes=" + strconv.Itoa(resp.Advice.Advice.LanesSuggested)
	}
	return "Reply: usage reading add --percent N"
}

// ----- display helpers -----------------------------------------------------

// txtPoints is the package's one-decimal display precision (no trailing .0),
// the same precision the reference model's headlines use.
func txtPoints(v float64) string {
	if !finite(v) {
		return "0"
	}
	return formatPoints(v)
}

// txtCost prints a per-task cost as typed: up to three decimals with trailing
// zeros dropped (0.05, 0.1, 0.025, 1, 0). One decimal would round 0.05 up to
// 0.1 and misprice the estimate; costs are small figures the operator chose.
// A non-finite value prints 0, like the other display helpers.
func txtCost(v float64) string {
	if !finite(v) {
		return "0"
	}
	s := strconv.FormatFloat(v, 'f', 3, 64)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		j := len(s)
		for j > i+1 && s[j-1] == '0' {
			j--
		}
		s = s[:j]
		if j == i+1 {
			s = s[:i]
		}
	}
	return s
}

// txtHours keeps the two-decimal precision the design's worked example uses
// for clock figures (days, hours), dropping trailing zeros.
func txtHours(v float64) string {
	if !finite(v) || v < 0 {
		return "0"
	}
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// clockStr renders a local time with its zone name (EDT, ART, ...); zone
// names that carry no name (UTC) are omitted.
func clockStr(t time.Time) string {
	name, _ := t.Zone()
	if name == "" || name == "UTC" {
		return t.Format("Mon 15:04")
	}
	return t.Format("Mon 15:04") + " " + name
}

// wordWrap wraps text at width columns on word boundaries, force-splitting
// words longer than the width.
func wordWrap(text string, width int) []string {
	if width < 8 {
		width = 8
	}
	runes := []rune(text)
	var lines []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			lines = append(lines, string(cur))
			cur = nil
		}
	}
	i := 0
	for i < len(runes) {
		for i < len(runes) && runes[i] == ' ' {
			i++
		}
		if i >= len(runes) {
			break
		}
		j := i
		for j < len(runes) && runes[j] != ' ' {
			j++
		}
		word := runes[i:j]
		i = j
		if len(word) > width {
			flush()
			for len(word) > width {
				lines = append(lines, string(word[:width]))
				word = word[width:]
			}
			cur = word
			continue
		}
		if len(cur) > 0 && len(cur)+1+len(word) > width {
			flush()
		}
		if len(cur) == 0 {
			cur = append(cur, word...)
		} else {
			cur = append(cur, ' ')
			cur = append(cur, word...)
		}
	}
	flush()
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}
