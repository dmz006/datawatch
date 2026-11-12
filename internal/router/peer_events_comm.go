// Comm verb for the peer event subscriber: autonomous peer-events
// [status | start <peer> <queue> [since=N] | stop <peer>]. It calls
// /api/autonomous/peer-events.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const peerEventsUsage = "usage: autonomous peer-events [status | start <peer> <queue> [since=N] | stop <peer>]"

// PeerEventsCommVerbs lists the peer event comm verbs for the parity test.
func PeerEventsCommVerbs() []string { return []string{"peer-events", "peer_events"} }

func (r *Router) handlePeerEventsVerb(args []string) {
	const label = "autonomous peer-events"
	rest := args[1:]
	sub := "status"
	if len(rest) > 0 {
		sub, rest = rest[0], rest[1:]
	}
	switch sub {
	case "status":
		if len(rest) > 0 {
			r.reply(label+" failed", peerEventsUsage)
			return
		}
		out, err := r.commGet("/api/autonomous/peer-events", nil)
		if err != nil {
			r.reply(label+" failed", err.Error())
			return
		}
		var v struct {
			Enabled bool     `json:"enabled"`
			Running []string `json:"running"`
		}
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			r.reply(label+" failed", "unparseable body: "+err.Error())
			return
		}
		state := "off"
		if v.Enabled {
			state = "on"
		}
		reading := "no peers"
		if len(v.Running) > 0 {
			reading = strings.Join(v.Running, ", ")
		}
		r.reply(label, fmt.Sprintf("peer event reading: %s\nreading: %s", state, reading))
	case "start":
		var since uint64
		var pos []string
		for _, a := range rest {
			if v, ok := strings.CutPrefix(a, "since="); ok {
				n, err := strconv.ParseUint(v, 10, 64)
				if err != nil {
					r.reply(label+" failed", "since must be a whole number\n"+peerEventsUsage)
					return
				}
				since = n
				continue
			}
			pos = append(pos, a)
		}
		if len(pos) != 2 {
			r.reply(label+" failed", peerEventsUsage)
			return
		}
		b, _ := json.Marshal(map[string]any{"peer": pos[0], "queue": pos[1], "since": since})
		if _, err := r.commJSON(http.MethodPost, "/api/autonomous/peer-events/start", string(b)); err != nil {
			r.reply(label+" failed", err.Error())
			return
		}
		r.reply(label, fmt.Sprintf("reading events from peer %s (queue %s)", pos[0], pos[1]))
	case "stop":
		if len(rest) != 1 {
			r.reply(label+" failed", peerEventsUsage)
			return
		}
		b, _ := json.Marshal(map[string]any{"peer": rest[0]})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/peer-events/stop", string(b))
		if err != nil {
			r.reply(label+" failed", err.Error())
			return
		}
		var v struct {
			Stopped bool `json:"stopped"`
		}
		_ = json.Unmarshal([]byte(out), &v)
		if !v.Stopped {
			r.reply(label, "no read was running for peer "+rest[0])
			return
		}
		r.reply(label, "stopped reading events from peer "+rest[0])
	default:
		r.reply(label+" failed", "unknown sub-verb "+sub+"\n"+peerEventsUsage)
	}
}
