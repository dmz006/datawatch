// The comm-channel surface of the account usage calculator (the comm-channel
// section of docs/plans/2026-10-23-usage-calculator-design.md; decisions in
// docs/plans/2026-10-25-usage-calculator-decisions.md).
//
// The handler proxies this daemon's REST loopback, the same way the budget,
// cost and stale handlers do (sx2_parity.go), so the reply shows the exact
// data the other surfaces show:
//
//	usage
//	usage advice [variant=V] [policy=P] [lanes=N]
//	usage reading <percent> [lanes=N] [session=N]
//	usage extra <points>
//	usage work <points|clear>
//
// lanes=N on the advice is the what-if lane count (not saved). A reading
// still parses lanes=N for older callers but never sends it: the daemon
// stamps each reading with the Claude lanes running in the ledger.
//
// The advice is rendered with the comm-channel form of the shared text layer
// (usagecalc.FormatComm, 50 columns, ending in its own reply line), followed
// by this grammar's reply hint. An invalid form answers the usage lines
// without sending anything to the daemon.
package router

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

// usageUsageLines is the reply for a bare or invalid usage form: the grammar
// itself, one form per line.
const usageUsageLines = `usage: usage
usage: usage advice [variant=V] [policy=P] [lanes=N]
usage: usage reading <percent> [session=N]
usage: usage extra <points>
usage: usage work <points|clear>
usage: usage burn-guard
lanes=N on advice is a what-if, not saved.
A reading is stamped with the running lanes.`

// usageReplyHint closes every usage calculator reply: the forms the operator
// can send back, one per line, each within the comm channel's 50 columns.
const usageReplyHint = "reply with one of:\n  usage advice [variant=V] [policy=P] [lanes=N]\n  usage reading <pct> [session=N]\n  usage extra <pts>\n  usage work <pts|clear>\nlanes=N is a what-if, not saved."

// handleUsage serves the usage calculator grammar over the REST loopback.
func (r *Router) handleUsage(cmd Command) {
	switch cmd.UsageVerb {
	case "": // bare "usage" — the advice.
		fallthrough
	case "advice":
		q := url.Values{}
		if cmd.UsageVariant != "" {
			q.Set("variant", cmd.UsageVariant)
		}
		if cmd.UsagePolicy != "" {
			q.Set("policy", cmd.UsagePolicy)
		}
		if cmd.UsageLanesSet {
			q.Set("lanes", strconv.Itoa(cmd.UsageLanes))
		}
		r.replyUsageAdvice(q, "usage advice")
		return
	case "reading":
		body := map[string]any{
			"percent": cmd.UsagePercent,
			"source":  "typed",
		}
		// lanes=N is parsed for older callers but not sent: the daemon
		// stamps the reading with the Claude lanes running in the ledger.
		if cmd.UsageSession != nil {
			body["session_percent"] = *cmd.UsageSession
		}
		jsonBody, _ := json.Marshal(body)
		if _, err := r.commJSON("POST", "/api/usage/readings", string(jsonBody)); err != nil {
			r.reply("usage reading failed", err.Error())
			return
		}
		ack := fmt.Sprintf("reading stored: %s%%", formatUsageNumber(cmd.UsagePercent))
		r.replyUsageAdvice(nil, "usage reading", ack)
		return
	case "burn-guard":
		r.replyBurnGuard()
		return
	case "extra":
		jsonBody := fmt.Sprintf(`{"points":%s}`, formatJSONNumber(cmd.UsagePoints))
		if _, err := r.commJSON("PUT", "/api/usage/extra-credit", jsonBody); err != nil {
			r.reply("usage extra failed", err.Error())
			return
		}
		ack := fmt.Sprintf("extra credit set to %s points", formatUsageNumber(cmd.UsagePoints))
		r.replyUsageAdvice(nil, "usage extra", ack)
		return
	case "work":
		if !cmd.UsageClear {
			jsonBody := fmt.Sprintf(`{"points":%s}`, formatJSONNumber(cmd.UsagePoints))
			if _, err := r.commJSON("PUT", "/api/usage/remaining-work", jsonBody); err != nil {
				r.reply("usage work failed", err.Error())
				return
			}
			ack := fmt.Sprintf("remaining work set to %s points", formatUsageNumber(cmd.UsagePoints))
			r.replyUsageAdvice(nil, "usage work", ack)
			return
		}
		// POST /api/usage/readings / PUT /api/usage/remaining-work keep the
		// typed figure; DELETE clears it, after which the estimate stands in.
		if _, err := r.commJSON("DELETE", "/api/usage/remaining-work", ""); err != nil {
			r.reply("usage work failed", err.Error())
			return
		}
		ack := "remaining work cleared; the per-task estimate stands in"
		r.replyUsageAdvice(nil, "usage work", ack)
		return
	default: // "invalid"
		r.reply("usage", usageUsageLines)
	}
}

// replyUsageAdvice fetches the daemon's current advice (with the per-call
// overrides of the advice verb) and replies with the comm-channel form of
// the shared text layer, closing with the grammar's reply hint. Writes are
// acknowledged first, then shown by the refreshed advice.
func (r *Router) replyUsageAdvice(q url.Values, label string, acks ...string) {
	body, err := r.commGet("/api/usage/advice", q)
	if err != nil {
		r.reply(label, err.Error())
		return
	}
	var resp usagecalc.Response
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		r.reply(label, "advice reply could not be read: "+err.Error())
		return
	}
	text := usagecalc.FormatComm(resp)
	if len(acks) > 0 && acks[0] != "" {
		text = acks[0] + "\n" + text
	}
	text = text[:len(text)-1] + "\n" + usageReplyHint + "\n"
	r.reply(label, text)
}

// formatUsageNumber renders a points/percent figure the way the shared text
// layer does it (two significant fraction digits, no trailing zeros), so the
// acknowledgement line matches the advice's numbers (usagecalc/text.go).
func formatUsageNumber(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

// formatJSONNumber renders a figure for a JSON body (no locale, no trailing
// fraction surprises): integers without a fraction part.
func formatJSONNumber(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// burnGuardStatusView is the part of GET /api/usage/burn-guard the comm reply shows.
type burnGuardStatusView struct {
	Enabled bool     `json:"enabled"`
	State   string   `json:"state"`
	Level   int      `json:"level"`
	Rung    string   `json:"rung"`
	Applied []string `json:"applied"`
	Line    *float64 `json:"line"`
	Margin  *float64 `json:"margin"`

	LastProjection *struct {
		Needs         string  `json:"needs"`
		Projected     float64 `json:"projected"`
		PointsPerHour float64 `json:"points_per_hour"`
		Over          bool    `json:"over"`
		OverBy        float64 `json:"over_by"`
	} `json:"last_projection"`
	LastDecision *struct {
		Direction string `json:"direction"`
		Rung      string `json:"rung"`
	} `json:"last_decision"`
	LastError string `json:"last_error"`
}

// replyBurnGuard mirrors the burn guard status: state, rung, last projection against
// the line and margin, and the last decision. It proxies the same REST route the CLI
// and the MCP tool use.
func (r *Router) replyBurnGuard() {
	body, err := r.commGet("/api/usage/burn-guard", nil)
	if err != nil {
		r.reply("usage burn-guard failed", err.Error())
		return
	}
	var st burnGuardStatusView
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		r.reply("usage burn-guard", "status could not be read: "+err.Error())
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "burn guard: %s", st.State)
	if !st.Enabled {
		b.WriteString(" (disabled)")
	}
	b.WriteString("\n")
	if st.Rung != "" {
		fmt.Fprintf(&b, "rung: %s (level %d: %s)\n", st.Rung, st.Level, strings.Join(st.Applied, ", "))
	} else {
		b.WriteString("rung: none in force\n")
	}
	if p := st.LastProjection; p != nil && p.Needs == "" {
		fmt.Fprintf(&b, "projected: %s%% at reset\n", formatUsageNumber(p.Projected))
		if st.Line != nil && st.Margin != nil {
			fmt.Fprintf(&b, "line: %s%%, margin: %s pts\n", formatUsageNumber(*st.Line), formatUsageNumber(*st.Margin))
		}
		fmt.Fprintf(&b, "burn: %s pts/h, %s\n", formatUsageNumber(p.PointsPerHour), overUnder(p.Over, p.OverBy))
	} else if p != nil {
		fmt.Fprintf(&b, "no projection yet: %s\n", p.Needs)
	} else {
		b.WriteString("no projection yet\n")
	}
	if d := st.LastDecision; d != nil {
		fmt.Fprintf(&b, "last decision: %s", d.Direction)
		if d.Rung != "" {
			fmt.Fprintf(&b, " %s", d.Rung)
		}
		b.WriteString("\n")
	}
	if st.LastError != "" {
		fmt.Fprintf(&b, "last error: %s\n", st.LastError)
	}
	r.reply("usage burn-guard", b.String())
}

func overUnder(over bool, by float64) string {
	if over {
		return formatUsageNumber(by) + " pts over target"
	}
	return formatUsageNumber(-by) + " pts under target"
}
