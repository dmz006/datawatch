// autonomous host-load over the chat channel: the host load check read, so
// the operator can ask from Signal / Telegram whether the check is off,
// alert-only or also reaping, and at what threshold and for how long —
// without curl. It reads the same effective values GET /api/config serves.
// A read: it raises no alert, ends nothing.

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// hostLoadUsage is the usage line for the host-load comm verb.
const hostLoadUsage = "usage: autonomous host-load"

// hostLoadChatSettings is the host_load_alert block of GET /api/config.
type hostLoadChatSettings struct {
	Mode     string  `json:"mode"`
	Multiple float64 `json:"multiple"`
	Minutes  int     `json:"minutes"`
}

// HostLoadCommVerbs lists the host-load comm verbs for the parity test.
func HostLoadCommVerbs() []string { return []string{"host-load"} }

// handleHostLoadVerb answers the autonomous command's host-load verb.
func (r *Router) handleHostLoadVerb(verb string, args []string) {
	switch verb {
	case "host-load", "host_load":
		r.replyHostLoad(args)
	default:
		r.reply("autonomous host-load failed", hostLoadUsage)
	}
}

// replyHostLoad reads the host_load_alert block of GET /api/config and
// replies with the effective settings in plain text. args[0] is the verb;
// nothing else is accepted.
func (r *Router) replyHostLoad(args []string) {
	const label = "autonomous host-load"
	if len(args) != 1 {
		r.reply(label+" failed", hostLoadUsage)
		return
	}
	out, err := r.commGet("/api/config", url.Values{})
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	var st struct {
		Autonomous struct {
			HostLoadAlert hostLoadChatSettings `json:"host_load_alert"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		r.reply(label+" failed", "unreadable reply: "+err.Error())
		return
	}
	r.reply(label, renderHostLoadForChat(st.Autonomous.HostLoadAlert))
}

// renderHostLoadForChat renders the effective host load check settings as
// one or two sentences, naming what the check does on a sustained breach.
func renderHostLoadForChat(hl hostLoadChatSettings) string {
	mode := strings.TrimSpace(hl.Mode)
	fmtMultiple := strconv.FormatFloat(hl.Multiple, 'f', -1, 64)
	switch mode {
	case "off":
		return "Host load check is off (autonomous.host_load_alert.mode = off): load above the limit raises nothing and reaps nothing."
	case "alert":
		return fmt.Sprintf(
			"Host load check: mode alert — one warning alert when the host load stays above %s × the core count for %d minutes.",
			fmtMultiple, hl.Minutes)
	case "reap":
		return fmt.Sprintf(
			"Host load check: mode reap — one warning alert when the host load stays above %s × the core count for %d minutes, and it ends processes of ended sessions that the orphan reaper's evidence rule proves by exact process id.",
			fmtMultiple, hl.Minutes)
	default:
		return fmt.Sprintf("Host load check: mode %q at %s × cores for %d minutes.", mode, fmtMultiple, hl.Minutes)
	}
}
