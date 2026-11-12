package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// laneFillChatDecisions is how many of the newest decisions a chat reply shows,
// so the reply fits one chat message.
const laneFillChatDecisions = 5

// laneFillChatLimitMax is the largest limit GET /api/autonomous/lane-fill accepts.
const laneFillChatLimitMax = 1000

const (
	laneFillUsage          = "usage: autonomous lane-fill [limit]"
	setLanePreferenceUsage = "usage: autonomous set-lane-preference <prd-id> <gpu|claude|any>"
)

// LaneFillCommVerbs lists the lane fill comm verbs, for the parity test.
func LaneFillCommVerbs() []string {
	return []string{"lane-fill", "set-lane-preference"}
}

// handleLaneFillVerb answers the lane fill verbs of the autonomous command.
// args[0] is the verb, as the autonomous handler passes it.
func (r *Router) handleLaneFillVerb(verb string, args []string) {
	if verb == "lane-fill" || verb == "lane_fill" {
		r.replyLaneFill(args)
		return
	}
	r.replySetLanePreference(args)
}

// replyLaneFill reads the lane fill status and replies with one line per lane,
// the queued eligible counts, then the newest decisions. limit is how many
// decisions are read; the reply shows at most laneFillChatDecisions of them.
func (r *Router) replyLaneFill(args []string) {
	limit := laneFillChatDecisions
	if len(args) > 2 {
		r.reply("autonomous lane-fill failed", laneFillUsage)
		return
	}
	if len(args) == 2 {
		n, err := strconv.Atoi(args[1])
		if err != nil || n < 1 || n > laneFillChatLimitMax {
			r.reply("autonomous lane-fill failed", laneFillUsage)
			return
		}
		limit = n
	}
	out, err := r.commGet("/api/autonomous/lane-fill", url.Values{"limit": {strconv.Itoa(limit)}})
	if err != nil {
		r.reply("autonomous lane-fill failed", err.Error())
		return
	}
	st, err := autonomous.ParseLaneFillStatus([]byte(out))
	if err != nil {
		r.reply("autonomous lane-fill failed", err.Error())
		return
	}
	r.reply("autonomous lane-fill", strings.TrimRight(autonomous.RenderLaneFillText(st, laneFillChatDecisions), "\n"))
}

// replySetLanePreference sets the lane an Automaton may take. A preference other
// than gpu, claude or any is refused before anything is sent.
func (r *Router) replySetLanePreference(args []string) {
	if len(args) < 3 {
		r.reply("autonomous set-lane-preference failed", setLanePreferenceUsage)
		return
	}
	pref := strings.ToLower(strings.TrimSpace(args[2]))
	switch pref {
	case autonomous.LanePreferenceGPU, autonomous.LanePreferenceClaude, autonomous.LanePreferenceAny:
	default:
		r.reply("autonomous set-lane-preference failed", "unknown lane preference "+strconv.Quote(args[2])+"; "+setLanePreferenceUsage)
		return
	}
	body, _ := json.Marshal(map[string]string{"lane_preference": pref})
	out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_lane_preference", string(body))
	if err != nil {
		r.reply("autonomous set-lane-preference failed", err.Error())
		return
	}
	r.reply("autonomous set-lane-preference", prettyJSON(out))
}
