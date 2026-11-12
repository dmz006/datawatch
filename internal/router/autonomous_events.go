// Comm-channel verb for the read side of the mcp_queue lifecycle
// subscription sink:
//
//	autonomous events [queue=<name>] [since=<cursor>]
//	    GET /api/autonomous/subscriptions?owner=<me>   (ownership check)
//	    GET /api/autonomous/events?queue=<name>&since=<cursor>
//
// Owner-scoped like the subscription verbs: the queue defaults to a name
// derived from the calling channel's identity, and a channel may only
// read a queue that one of its own subscriptions feeds (an mcp_queue sink
// whose owner is this channel). The ownership check goes through the
// owner-filtered subscription list, so another channel's queue is never
// read, even when its name is known.

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// commEventsPath is the REST route the verb proxies.
const commEventsPath = "/api/autonomous/events"

// commEventsUsage is the operator-facing syntax line for "events".
const commEventsUsage = "usage: autonomous events [queue=<name>] [since=<cursor>]  (the queue defaults to this channel's own queue)"

// commEventsQueueName derives the default queue name from a channel
// identity: queue names allow only letters, digits and . _ : -, so every
// other rune of the identity ("$", "@", "!") becomes "_".
func commEventsQueueName(identity string) string {
	var b strings.Builder
	for _, c := range identity {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '.', c == '_', c == ':', c == '-':
			b.WriteRune(c)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// commEventsArgs is the parsed form of "events [queue=<name>] [since=<cursor>]".
type commEventsArgs struct {
	queue string
	since uint64
}

// parseCommEvents parses the verb's arguments; queue is left empty when
// absent so the caller can default it.
func parseCommEvents(args []string) (commEventsArgs, error) {
	var out commEventsArgs
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "queue="); ok {
			if strings.TrimSpace(v) == "" {
				return commEventsArgs{}, fmt.Errorf("queue= has an empty value  (%s)", commEventsUsage)
			}
			out.queue = strings.TrimSpace(v)
			continue
		}
		if v, ok := strings.CutPrefix(a, "since="); ok {
			n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return commEventsArgs{}, fmt.Errorf("since=%q is not a numeric cursor  (%s)", v, commEventsUsage)
			}
			out.since = n
			continue
		}
		return commEventsArgs{}, fmt.Errorf("unexpected argument %q  (%s)", a, commEventsUsage)
	}
	return out, nil
}

// commOwnsQueue reports whether the owner-scoped subscription list body
// has an mcp_queue sink feeding the given queue.
func commOwnsQueue(body, queue string) bool {
	var recs []struct {
		Sink struct {
			Type   string `json:"type"`
			Target string `json:"target"`
		} `json:"sink"`
	}
	if err := json.Unmarshal([]byte(body), &recs); err != nil {
		return false
	}
	for _, rec := range recs {
		if rec.Sink.Type == "mcp_queue" && strings.TrimSpace(rec.Sink.Target) == queue {
			return true
		}
	}
	return false
}

// handleAutonomousEvents implements "autonomous events".
func (r *Router) handleAutonomousEvents(args []string) {
	p, err := parseCommEvents(args)
	if err != nil {
		r.reply("autonomous events", err.Error())
		return
	}
	identity := r.commSubIdentity()
	if p.queue == "" {
		p.queue = commEventsQueueName(identity)
	}

	// Ownership isolation: the queue must be fed by one of THIS channel's
	// subscriptions before anything is read from it.
	subs, err := r.commGet(commSubPath, url.Values{"owner": {identity}})
	if err != nil {
		r.reply("autonomous events failed", err.Error())
		return
	}
	if !commOwnsQueue(subs, p.queue) {
		r.reply("autonomous events",
			fmt.Sprintf("queue %q not found (a channel can only read queues of its own mcp_queue subscriptions)", p.queue))
		return
	}

	body, err := r.commGet(commEventsPath, url.Values{
		"queue": {p.queue},
		"since": {strconv.FormatUint(p.since, 10)},
	})
	if err != nil {
		r.reply("autonomous events failed", err.Error())
		return
	}
	r.reply("autonomous events", renderCommEvents(body, p.queue))
}

// renderCommEvents renders the events body as one line per event plus the
// cursor to pass as since= on the next call.
func renderCommEvents(body, queue string) string {
	var resp struct {
		Events []struct {
			At        string `json:"at"`
			Kind      string `json:"kind"`
			PRDID     string `json:"prd_id"`
			TaskID    string `json:"task_id"`
			NewStatus string `json:"new_status"`
		} `json:"events"`
		NextCursor uint64 `json:"next_cursor"`
		Dropped    bool   `json:"dropped"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return prettyJSON(body)
	}
	var lines []string
	if resp.Dropped {
		lines = append(lines, "warning: events were dropped (the queue overflowed before you read them)")
	}
	if len(resp.Events) == 0 {
		lines = append(lines, "no new events in "+queue)
	}
	for _, e := range resp.Events {
		line := e.At + " " + e.Kind
		if e.PRDID != "" {
			line += " prd=" + e.PRDID
		}
		if e.TaskID != "" {
			line += " task=" + e.TaskID
		}
		if e.NewStatus != "" {
			line += " status=" + e.NewStatus
		}
		lines = append(lines, line)
	}
	lines = append(lines, fmt.Sprintf("next: autonomous events queue=%s since=%d", queue, resp.NextCursor))
	return strings.Join(lines, "\n")
}
