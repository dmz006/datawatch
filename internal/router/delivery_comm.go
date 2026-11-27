// The delivery check over chat: "delivery [status] | check | ack | accept |
// drop | test", through the /api/delivery loopback. The loopback uses the
// daemon's admin token, so a message sent with a session or peer credential
// names that caller in a header and the daemon records it as who acted.

package router

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/federation"
)

// deliveryActorChannel labels an action sent from a messaging channel.
const deliveryActorChannel = "channel"

// deliveryChatItems bounds the entry lines of each group in a chat reply.
const deliveryChatItems = 20

const deliveryUsage = "usage: delivery [status] | delivery check | delivery ack <key…>|backlog | " +
	"delivery accept <key…>|backlog|kind=<k> reason=<code> until=<date> | " +
	"delivery drop <key…>|backlog reason=<code> | delivery test " +
	"(ack, accept and drop also take state=<s> and match_reason=<code>)"

// parseDelivery reads the words after "delivery".
func parseDelivery(rest string) Command {
	cmd := Command{Type: CmdDelivery, DeliveryVerb: "status"}
	if fields := strings.Fields(rest); len(fields) > 0 {
		cmd.DeliveryVerb = strings.ToLower(fields[0])
		cmd.Text = strings.Join(fields[1:], " ")
	}
	return cmd
}

// deliveryActionBody builds the body of ack, accept or drop. A word it does not
// know is refused: dropping it would widen what the action applies to.
func deliveryActionBody(verb string, args []string) (map[string]any, error) {
	allowed := []string{"kind", "state", "match_reason"}
	var required []string
	switch verb {
	case "accept":
		required = []string{"reason", "until"}
	case "drop":
		required = []string{"reason"}
	}
	allowed = append(allowed, required...)

	body := map[string]any{}
	var keys []string
	selected := false
	for _, word := range args {
		name, value, isOption := strings.Cut(word, "=")
		switch {
		case isOption && !strings.Contains(name, ":"):
			name = strings.ToLower(name)
			if !slices.Contains(allowed, name) {
				return nil, fmt.Errorf("delivery %s does not take %q", verb, name+"=")
			}
			if _, twice := body[name]; twice || value == "" {
				return nil, fmt.Errorf("give %s= once, with a value", name)
			}
			body[name] = value
			if name != "reason" && name != "until" {
				selected = true
			}
		case strings.EqualFold(word, "backlog"):
			body["backlog"], selected = true, true
		case strings.Contains(word, ":"):
			keys, selected = append(keys, word), true
		default:
			return nil, fmt.Errorf("%q is not an entry key, backlog or an option", word)
		}
	}
	if !selected {
		return nil, errors.New("name the entries")
	}
	for _, name := range required {
		if _, ok := body[name]; !ok {
			return nil, fmt.Errorf("delivery %s needs %s=", verb, name)
		}
	}
	if len(keys) > 0 {
		body["keys"] = keys
	}
	body["actor"] = deliveryActorChannel
	return body, nil
}

// deliveryDo sends one loopback request for caller ("" is the operator).
func (r *Router) deliveryDo(method, target, jsonBody, caller string) (string, error) {
	if r.webPort == 0 {
		return "", fmt.Errorf("REST loopback not configured")
	}
	var rdr io.Reader
	if jsonBody != "" {
		rdr = bytes.NewReader([]byte(jsonBody))
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", r.webPort, target), rdr)
	if err != nil {
		return "", err
	}
	if jsonBody != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if caller != "" {
		req.Header.Set(federation.HeaderDeliveryCaller, caller)
	}
	return r.commDo(req)
}

// handleDelivery serves the delivery commands. caller is the session or peer
// the message was sent for, or "".
func (r *Router) handleDelivery(cmd Command, caller string) {
	verb := cmd.DeliveryVerb
	label := "delivery"
	if verb != "status" {
		label += " " + verb
	}
	args := strings.Fields(cmd.Text)
	refuse := func(why string) { r.reply(label+" not run", why+"\n"+deliveryUsage) }

	method, target, body := http.MethodPost, "", map[string]any(nil)
	switch verb {
	case "status", "check", "test":
		if len(args) > 0 {
			refuse("delivery " + verb + " takes nothing more")
			return
		}
		switch verb {
		case "status":
			method, target = http.MethodGet, "/api/delivery"
		case "check":
			target = "/api/delivery/check"
		default:
			target = "/api/delivery/test-message"
		}
	case "ack", "accept", "drop":
		var err error
		if body, err = deliveryActionBody(verb, args); err != nil {
			refuse(err.Error())
			return
		}
		target = "/api/delivery/" + verb
	default:
		refuse("unknown delivery command")
		return
	}

	var payload string
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			r.reply(label+" failed", err.Error())
			return
		}
		payload = string(b)
	}
	out, err := r.deliveryDo(method, target, payload, caller)
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	text, err := deliveryReply(verb, body, []byte(out))
	if err != nil {
		r.reply(label+" failed", "unreadable answer: "+err.Error())
		return
	}
	r.reply(label, text)
}

// deliveryReply turns the daemon's answer into the chat reply.
func deliveryReply(verb string, body map[string]any, out []byte) (string, error) {
	switch verb {
	case "status", "check":
		var report delivery.Report
		if err := json.Unmarshal(out, &report); err != nil {
			return "", err
		}
		return delivery.RenderReport(report, deliveryChatItems), nil
	case "test":
		var answer struct {
			Results []delivery.SendResult `json:"results"`
		}
		if err := json.Unmarshal(out, &answer); err != nil {
			return "", err
		}
		if len(answer.Results) == 0 {
			return "no message channel can reach a person", nil
		}
		lines := make([]string, len(answer.Results))
		for i, res := range answer.Results {
			state := "sent"
			if !res.OK {
				state = "failed (" + res.Code + ")"
			}
			lines[i] = res.Channel + ": " + state
		}
		return strings.Join(lines, "\n"), nil
	}
	var answer struct {
		Changed *int `json:"changed"`
	}
	if err := json.Unmarshal(out, &answer); err != nil {
		return "", err
	}
	if answer.Changed == nil {
		return "", errors.New("no count of changed entries")
	}
	switch verb {
	case "ack":
		return fmt.Sprintf("%d marked as seen; they stay listed", *answer.Changed), nil
	case "accept":
		return fmt.Sprintf("%d accepted until %v", *answer.Changed, body["until"]), nil
	}
	return fmt.Sprintf("%d dropped", *answer.Changed), nil
}

// handleDeliveryRest serves a "rest" command on a delivery route sent for a
// session or peer, so it too is recorded under that caller. It reports false
// for every other rest command.
func (r *Router) handleDeliveryRest(cmd Command, caller string) bool {
	if caller == "" {
		return false
	}
	u, err := url.Parse(cmd.RestPath)
	if err != nil {
		return false
	}
	if p := path.Clean("/" + u.Path); p != "/api/delivery" && !strings.HasPrefix(p, "/api/delivery/") {
		return false
	}
	label := "rest " + cmd.RestMethod + " " + cmd.RestPath
	body, err := r.deliveryDo(cmd.RestMethod, cmd.RestPath, cmd.RestBody, caller)
	if err != nil {
		r.reply(label+" failed", err.Error())
		return true
	}
	r.reply(label, body)
	return true
}

// deliveryConfigKeys are the delivery settings, in the order they are shown.
var deliveryConfigKeys = []string{"enabled", "notify", "interval_seconds"}

const deliveryConfigHelp = "delivery.enabled (true|false, default true: the list of unfinished and undelivered work and its report-only check), " +
	"delivery.notify (true|false, default true: send listed entries to the message channels), " +
	"delivery.interval_seconds (whole seconds, 60 to 86400; 0 = the default, 300), " +
	"delivery.request_take_by_seconds / delivery.request_due_seconds / delivery.reminder_interval_seconds (whole seconds; 0 = the default), " +
	"delivery.request_due_seconds_by_kind (kind=seconds pairs; empty value as {} clears it), " +
	"delivery.reminders_per_session_hour (default 2) and delivery.reminders_per_host_hour (default 20; 0 = no reminders); configure delivery reads them back"

// isDeliveryConfigRead reports whether a bare configure argument asks for the
// delivery settings.
func isDeliveryConfigRead(text string) bool {
	if text == "delivery" {
		return true
	}
	key, ok := strings.CutPrefix(text, "delivery.")
	return ok && (slices.Contains(deliveryConfigKeys, key) || slices.Contains(delivery.RequestSettingKeys, key))
}

// handleDeliveryConfigRead replies with the effective value of one delivery
// setting, or of all of them for "delivery".
func (r *Router) handleDeliveryConfigRead(text string) {
	out, err := r.commGet("/api/config", nil)
	if err != nil {
		r.reply("configure "+text+" failed", err.Error())
		return
	}
	var view struct {
		Delivery map[string]any `json:"delivery"`
		Requests map[string]any `json:"delivery_requests"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil || view.Delivery == nil {
		r.reply("configure "+text+" failed", "the daemon reported no delivery settings")
		return
	}
	var lines []string
	for _, key := range deliveryConfigKeys {
		if text == "delivery" || text == "delivery."+key {
			lines = append(lines, fmt.Sprintf("delivery.%s = %v", key, view.Delivery[key]))
		}
	}
	for _, key := range delivery.RequestSettingKeys {
		if v, ok := view.Requests[key]; ok && (text == "delivery" || text == "delivery."+key) {
			lines = append(lines, fmt.Sprintf("delivery.%s = %s", key, delivery.RenderSetting(v)))
		}
	}
	r.reply("configure", strings.Join(lines, "\n"))
}
