// Comm-channel verbs to update and test-fire an Automaton lifecycle-event
// subscription:
//
//	autonomous subscription-update <id> key=value...
//	    GET  /api/autonomous/subscriptions?owner=<me>  (ownership check + current record)
//	    PUT  /api/autonomous/subscriptions/<id>
//	autonomous subscription-test <id>
//	    GET  /api/autonomous/subscriptions?owner=<me>  (ownership check)
//	    POST /api/autonomous/subscriptions/<id>/test
//
// Owner-scoped exactly like unsubscribe (autonomous_subscriptions.go): the
// id must be in this channel's own list before anything is sent, and an id
// owned by another channel gets the same not-found answer as an id that
// does not exist. An update merges the key=value pairs into the stored
// record and keeps the comm sink, target and owner pinned to the calling
// channel, so a channel cannot redirect its subscription elsewhere or hand
// it to another owner.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// commSubUpdateUsage is the operator-facing syntax line for
// "subscription-update".
const commSubUpdateUsage = "usage: autonomous subscription-update <id> key=value...  (keys: kinds=<k1,k2> level=prd|task prd=<id> task=<id> coalesce=true|false one_shot=true|false expires=<seconds> sink=comm|webhook|peer target=<t> secret_ref=${secret:name}; prd= / task= / expires=0 clear)"

// commSubUpdate is the parsed key=value set of a subscription-update.
// A nil pointer / false "set" flag means the key was not given.
type commSubUpdate struct {
	kinds      []string
	setKinds   bool
	level      string
	setLevel   bool
	prdID      string
	setPRD     bool
	taskID     string
	setTask    bool
	coalesce   bool
	setCoal    bool
	oneShot    bool
	setOneShot bool
	expiresSec int
	setExpires bool
	sinkType   string
	setSink    bool
	target     string
	setTarget  bool
	secretRef  string
	setSecret  bool
}

// parseCommSubUpdate parses the key=value arguments of subscription-update.
func parseCommSubUpdate(args []string) (commSubUpdate, error) {
	var u commSubUpdate
	for _, a := range args {
		key, val, ok := strings.Cut(a, "=")
		if !ok {
			return commSubUpdate{}, fmt.Errorf("%q is not key=value  (%s)", a, commSubUpdateUsage)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "kinds", "kind":
			u.kinds = nil
			for _, p := range strings.Split(val, ",") {
				if p = strings.TrimSpace(p); p == "" {
					continue
				}
				k, err := resolveCommSubKind(p)
				if err != nil {
					return commSubUpdate{}, err
				}
				u.kinds = append(u.kinds, k)
			}
			u.setKinds = true
		case "level":
			if val != "" && val != "prd" && val != "task" {
				return commSubUpdate{}, fmt.Errorf("level must be prd or task, got %q", val)
			}
			u.level, u.setLevel = val, true
		case "prd":
			u.prdID, u.setPRD = val, true
		case "task":
			u.taskID, u.setTask = val, true
		case "coalesce", "one_shot", "one-shot":
			b, err := strconv.ParseBool(val)
			if err != nil {
				return commSubUpdate{}, fmt.Errorf("%s must be true or false, got %q", key, val)
			}
			if key == "coalesce" {
				u.coalesce, u.setCoal = b, true
			} else {
				u.oneShot, u.setOneShot = b, true
			}
		case "expires":
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 {
				return commSubUpdate{}, fmt.Errorf("expires must be a non-negative number of seconds, got %q", val)
			}
			u.expiresSec, u.setExpires = n, true
		case "sink":
			if !commSubSinkTypes[val] {
				return commSubUpdate{}, fmt.Errorf("sink must be comm, webhook or peer, got %q", val)
			}
			u.sinkType, u.setSink = val, true
		case "target":
			u.target, u.setTarget = val, true
		case "secret_ref":
			u.secretRef, u.setSecret = val, true
		default:
			return commSubUpdate{}, fmt.Errorf("unknown key %q  (%s)", key, commSubUpdateUsage)
		}
	}
	if !u.setKinds && !u.setLevel && !u.setPRD && !u.setTask && !u.setCoal && !u.setOneShot && !u.setExpires && !u.setSink && !u.setTarget && !u.setSecret {
		return commSubUpdate{}, fmt.Errorf("nothing to change  (%s)", commSubUpdateUsage)
	}
	return u, nil
}

// commSubOwnedRecord returns the raw record for id from this channel's
// owner-scoped list. found is false for an id that is not this channel's
// — whether it is unknown or owned by someone else.
func (r *Router) commSubOwnedRecord(id string) (rec map[string]any, found bool, err error) {
	body, err := r.commGet(commSubPath, url.Values{"owner": {r.commSubIdentity()}})
	if err != nil {
		return nil, false, err
	}
	var recs []map[string]any
	if err := json.Unmarshal([]byte(body), &recs); err != nil {
		return nil, false, fmt.Errorf("unexpected subscription list: %w", err)
	}
	for _, rc := range recs {
		if s, _ := rc["id"].(string); s == id {
			return rc, true, nil
		}
	}
	return nil, false, nil
}

// commSubNotFound is the reply for an id that is not this channel's.
func commSubNotFound(id, doing string) string {
	return fmt.Sprintf("id %q not found (a channel can only %s its own subscriptions)", id, doing)
}

// applyCommSubUpdate merges u into the stored record rec and returns the
// PUT body. The sink stays the record's own sink unless sink= changes it; a
// comm sink always targets this channel, and the owner stays this channel.
// The secret reference is kept only while the sink stays a webhook.
func applyCommSubUpdate(rec map[string]any, u commSubUpdate, identity string, now time.Time) (map[string]any, error) {
	curSink, _ := rec["sink"].(map[string]any)
	curType, _ := curSink["type"].(string)
	curTarget, _ := curSink["target"].(string)
	curSecret, _ := curSink["secret_ref"].(string)
	filter, _ := rec["filter"].(map[string]any)
	if filter == nil {
		filter = map[string]any{}
	}
	if u.setKinds {
		filter["kinds"] = u.kinds
	}
	if u.setLevel {
		filter["level"] = u.level
	}
	if u.setPRD {
		filter["prd_id"] = u.prdID
	}
	if u.setTask {
		filter["task_id"] = u.taskID
	}
	// Resolve the sink: sink= picks a new one, otherwise the record keeps its own.
	// Only the fields the new sink needs are carried over.
	sinkType := curType
	if u.setSink {
		sinkType = u.sinkType
	}
	target, secretRef := "", ""
	if sinkType == curType {
		target, secretRef = curTarget, curSecret
	}
	if u.setTarget {
		target = u.target
	}
	if u.setSecret {
		secretRef = u.secretRef
	}
	if sinkType == "comm" && !u.setTarget {
		target = "" // a comm sink always delivers to this channel; an explicit foreign target is refused
	}
	sink, err := commSubSinkBody(sinkType, target, secretRef, identity)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"filter": filter,
		"sink":   sink,
		"owner":  identity,
	}
	for _, k := range []string{"action", "coalesce", "one_shot", "expires_at"} {
		if v, ok := rec[k]; ok {
			body[k] = v
		}
	}
	if u.setCoal {
		body["coalesce"] = u.coalesce
	}
	if u.setOneShot {
		body["one_shot"] = u.oneShot
	}
	if u.setExpires {
		if u.expiresSec == 0 {
			delete(body, "expires_at")
		} else {
			body["expires_at"] = now.UTC().Add(time.Duration(u.expiresSec) * time.Second).Format(time.RFC3339)
		}
	}
	return body, nil
}

// ── subscription-update ────────────────────────────────────────────────────

// handleAutonomousSubsUpdate implements "autonomous subscription-update
// <id> key=value...": ownership check, merge into the stored record, PUT.
func (r *Router) handleAutonomousSubsUpdate(args []string) {
	if len(args) < 2 || strings.TrimSpace(args[0]) == "" {
		r.reply("autonomous subscription-update", commSubUpdateUsage)
		return
	}
	id := strings.TrimSpace(args[0])
	u, err := parseCommSubUpdate(args[1:])
	if err != nil {
		r.reply("autonomous subscription-update", err.Error())
		return
	}

	rec, found, err := r.commSubOwnedRecord(id)
	if err != nil {
		r.reply("autonomous subscription-update failed", err.Error())
		return
	}
	if !found {
		r.reply("autonomous subscription-update", commSubNotFound(id, "update"))
		return
	}

	next, err := applyCommSubUpdate(rec, u, r.commSubIdentity(), time.Now())
	if err != nil {
		r.reply("autonomous subscription-update", err.Error())
		return
	}
	body, err := json.Marshal(next)
	if err != nil {
		r.reply("autonomous subscription-update", err.Error())
		return
	}
	resp, err := r.commJSON(http.MethodPut, commSubPath+"/"+url.PathEscape(id), string(body))
	if err != nil {
		r.reply("autonomous subscription-update failed", err.Error())
		return
	}
	updated, err := decodeCommSubRecord(resp)
	if err != nil {
		r.reply("autonomous subscription-update", prettyJSON(resp))
		return
	}
	r.reply("autonomous subscription-update", "updated "+renderCommSubLine(updated))
}

// ── subscription-test ──────────────────────────────────────────────────────

// handleAutonomousSubsTest implements "autonomous subscription-test <id>":
// ownership check, then POST .../test.
func (r *Router) handleAutonomousSubsTest(args []string) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		r.reply("autonomous subscription-test", "usage: autonomous subscription-test <id>")
		return
	}
	id := strings.TrimSpace(args[0])

	_, found, err := r.commSubOwnedRecord(id)
	if err != nil {
		r.reply("autonomous subscription-test failed", err.Error())
		return
	}
	if !found {
		r.reply("autonomous subscription-test", commSubNotFound(id, "test"))
		return
	}

	resp, err := r.commJSON(http.MethodPost, commSubPath+"/"+url.PathEscape(id)+"/test", "")
	if err != nil {
		r.reply("autonomous subscription-test failed", err.Error())
		return
	}
	var res struct {
		OK       bool   `json:"ok"`
		Sink     string `json:"sink"`
		Error    string `json:"error"`
		Status   int    `json:"status"`
		Attempts int    `json:"attempts"`
	}
	if err := json.Unmarshal([]byte(resp), &res); err != nil {
		r.reply("autonomous subscription-test", prettyJSON(resp))
		return
	}
	// The webhook and peer sinks report the HTTP status and the attempt count.
	outcome := ""
	if res.Attempts > 0 {
		outcome = fmt.Sprintf(", status %d, attempts %d", res.Status, res.Attempts)
	}
	if !res.OK {
		r.reply("autonomous subscription-test", fmt.Sprintf("test for %s failed (sink %s%s): %s", id, res.Sink, outcome, res.Error))
		return
	}
	r.reply("autonomous subscription-test", fmt.Sprintf("test event delivered for %s (sink %s%s)", id, res.Sink, outcome))
}
