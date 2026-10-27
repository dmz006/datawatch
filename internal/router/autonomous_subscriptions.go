// BL427 (phase 3) — comm-channel verbs for Automaton lifecycle-event
// subscriptions. Design: docs/plans/2026-10-13-prd-lifecycle-events.md
// (Phases item 3).
//
// The daemon already owns the subscription store, the dispatcher engine
// and the REST routes:
//
//	internal/autonomous/lifecycle_subscription.go      Subscription shape + validation
//	internal/autonomous/lifecycle_subscription_store.go  persisted store
//	internal/autonomous/lifecycle_session_push.go        dispatcher engine (comm fan-out)
//	internal/server/autonomous_subscriptions.go          /api/autonomous/subscriptions
//
// These verbs proxy that REST surface through the in-process loopback
// — the same pattern the other autonomous comm verbs use (the case
// bodies of handleAutonomous in sx2_parity.go: commGet/commJSON +
// prettyJSON + reply). They live in this file because owner-scoped
// listing and ownership-checked delete are shared by all three verbs.
//
//	autonomous subscribe <kinds|short-names> [prd=<id>] [task=<id>]
//	    POST /api/autonomous/subscriptions
//	autonomous unsubscribe <id>
//	    GET  /api/autonomous/subscriptions?owner=<me>  (ownership check)
//	    DELETE /api/autonomous/subscriptions/<id>
//	autonomous subscriptions
//	    GET  /api/autonomous/subscriptions?owner=<me>
//
// A subscription created from a comm channel is a COMM sink (events are
// delivered back over the daemon's notify pipeline — routers / ntfy /
// email — and coalesce per Automaton in a quiet window): the calling
// channel/backend identity is BOTH the sink target and the owner, so the
// channel receives its own events. Everything is scoped by owner:
// "subscriptions" lists only this channel's records, and "unsubscribe"
// verifies the id against this channel's list before the DELETE — a
// channel can never list or delete a subscription another channel created.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// commSubPath is the REST route the three verbs proxy.
const commSubPath = "/api/autonomous/subscriptions"

// commSubIdentity returns the calling channel's backend identity:
// "<backendname>:<group-id>" (e.g. "telegram:-1001234", "signal:$abc",
// "github:github"). The group id already distinguishes channels within a
// daemon; the backend prefix keeps identities unambiguous when one daemon
// runs several backends. Falls back to the hostname alone when neither is
// set (unit tests without a real backend).
func (r *Router) commSubIdentity() string {
	chanID := strings.TrimSpace(r.groupID)
	if chanID == "" {
		chanID = strings.TrimSpace(r.hostname)
	}
	backend := ""
	if r.backend != nil {
		backend = r.backend.Name()
	}
	if backend != "" && chanID != "" {
		return backend + ":" + chanID
	}
	return chanID
}

// commSubShortKinds maps the bare shorthand words (the CLI's --on
// shorthand, cmd/datawatch/cli_autonomous_subscriptions.go) to their
// full task-level event kinds. Kinds passed with a dot (prd.blocked,
// task.completed) always pass through unchanged. The two opt-in warning
// kinds (scope_warning, guardrail_warn) are bare lifecycle kinds, not task
// statuses, so they map to themselves instead of task.<word>.
var commSubShortKinds = map[string]string{
	"session_started": "task.session_started",
	"session_cleared": "task.session_cleared",
	"prd_updated":     "task.prd_updated",
	// The warning kinds are bare lifecycle kinds, not task statuses.
	"scope_warning":  "scope_warning",
	"guardrail_warn": "guardrail_warn",
}

// commSubUsage is the operator-facing syntax line for "subscribe".
const commSubUsage = "usage: autonomous subscribe <kinds|short-names> [prd=<id>] [task=<id>]  (e.g. autonomous subscribe session_started,prd.blocked prd=prd_a3f9 | autonomous subscribe scope_warning,guardrail_warn task=<id>)"

// commSubArgs is the parsed form of "subscribe <kinds…> [prd=<id>] [task=<id>]".
type commSubArgs struct {
	kinds  []string
	prdID  string
	taskID string
}

// resolveCommSubKind normalizes one kind token: full kinds (containing
// ".") pass through, shorthand words map to task.<kind> when known, and
// any other bare word is a task status kind (task.<word>) — the same
// behavior as the CLI's --on shorthand. The opt-in warning kinds
// scope_warning and guardrail_warn carry no dot but map to themselves.
func resolveCommSubKind(kind string) (string, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "", fmt.Errorf("event kind is empty")
	}
	if strings.Contains(kind, ".") {
		return kind, nil
	}
	if full, ok := commSubShortKinds[kind]; ok {
		return full, nil
	}
	return "task." + kind, nil
}

// parseCommSubscribe parses the subscribe verb's arguments. Kinds may be
// comma- or space-separated; "prd=<id>" and "task=<id>" scope the filter
// to one Automaton or task. The daemon's ValidateSubscription remains
// authoritative for what gets stored (unknown kinds come back verbatim
// over the loopback).
func parseCommSubscribe(args []string) (commSubArgs, error) {
	var out commSubArgs

	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "prd="); ok {
			if out.prdID != "" {
				return commSubArgs{}, fmt.Errorf("prd=<id> is repeated (already %q)", out.prdID)
			}
			if strings.TrimSpace(v) == "" {
				return commSubArgs{}, fmt.Errorf("prd= has an empty value  (%s)", commSubUsage)
			}
			out.prdID = strings.TrimSpace(v)
			continue
		}
		if v, ok := strings.CutPrefix(a, "task="); ok {
			if out.taskID != "" {
				return commSubArgs{}, fmt.Errorf("task=<id> is repeated (already %q)", out.taskID)
			}
			if strings.TrimSpace(v) == "" {
				return commSubArgs{}, fmt.Errorf("task= has an empty value  (%s)", commSubUsage)
			}
			out.taskID = strings.TrimSpace(v)
			continue
		}
		// Kind token(s): one or more, comma-separated.
		for _, p := range strings.Split(a, ",") {
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			k, err := resolveCommSubKind(p)
			if err != nil {
				return commSubArgs{}, err
			}
			out.kinds = append(out.kinds, k)
		}
	}

	if len(out.kinds) == 0 {
		return commSubArgs{}, fmt.Errorf("at least one event kind is required  (%s)", commSubUsage)
	}
	return out, nil
}

// commSubRecord is one persisted subscription in the shape these verbs
// need (a projection of autonomous.Subscription's JSON).
type commSubRecord struct {
	id       string
	kindList []string
	PRDID    string
	TaskID   string
	SinkType string
	Target   string
	Owner    string
	Coalesce bool
}

// decodeCommSubRecord parses one record from the daemon (the 201 body of
// subscribe, or one entry of the owner-scoped list).
func decodeCommSubRecord(body string) (commSubRecord, error) {
	var raw struct {
		ID       string `json:"id"`
		Owner    string `json:"owner"`
		Coalesce bool   `json:"coalesce"`
		Filter   struct {
			Kinds  []string `json:"kinds"`
			PRDID  string   `json:"prd_id"`
			TaskID string   `json:"task_id"`
		} `json:"filter"`
		Sink struct {
			Type   string `json:"type"`
			Target string `json:"target"`
		} `json:"sink"`
	}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return commSubRecord{}, err
	}
	if raw.ID == "" {
		return commSubRecord{}, fmt.Errorf("record has no id")
	}
	return commSubRecord{
		id:       raw.ID,
		kindList: raw.Filter.Kinds,
		PRDID:    raw.Filter.PRDID,
		TaskID:   raw.Filter.TaskID,
		SinkType: raw.Sink.Type,
		Target:   raw.Sink.Target,
		Owner:    raw.Owner,
		Coalesce: raw.Coalesce,
	}, nil
}

// renderCommSubLine renders one subscription for the list reply.
func renderCommSubLine(rec commSubRecord) string {
	parts := []string{rec.id}
	if len(rec.kindList) > 0 {
		parts = append(parts, "kinds="+strings.Join(rec.kindList, ","))
	}
	if rec.PRDID != "" {
		parts = append(parts, "prd="+rec.PRDID)
	}
	if rec.TaskID != "" {
		parts = append(parts, "task="+rec.TaskID)
	}
	parts = append(parts,
		fmt.Sprintf("sink=%s -> %s", rec.SinkType, rec.Target),
		fmt.Sprintf("coalesce=%t", rec.Coalesce))
	return strings.Join(parts, " ")
}

// commSubListContainsID reports whether the owner-scoped list body
// carries a record with the given id.
func commSubListContainsID(body, id string) bool {
	var recs []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &recs); err != nil {
		return false
	}
	for _, r := range recs {
		if r.ID == id {
			return true
		}
	}
	return false
}

// ── subscribe ──────────────────────────────────────────────────────────────

// handleAutonomousSubsSubscribe implements "autonomous subscribe <kinds…>
// [prd=<id>] [task=<id>]": a comm-sink subscription owned by (and
// delivering to) the calling channel, coalesced.
func (r *Router) handleAutonomousSubsSubscribe(args []string) {
	if len(args) == 0 {
		r.reply("autonomous subscribe", commSubUsage)
		return
	}
	p, err := parseCommSubscribe(args)
	if err != nil {
		r.reply("autonomous subscribe", err.Error())
		return
	}

	identity := r.commSubIdentity()
	body, err := json.Marshal(map[string]any{
		"filter": map[string]any{
			"kinds":   p.kinds,
			"prd_id":  p.prdID,
			"task_id": p.taskID,
		},
		"sink":     map[string]any{"type": "comm", "target": identity},
		"owner":    identity,
		"coalesce": true, // comm channels batch per Automaton during the quiet window
	})
	if err != nil {
		r.reply("autonomous subscribe", err.Error())
		return
	}

	resp, err := r.commJSON(http.MethodPost, commSubPath, string(body))
	if err != nil {
		r.reply("autonomous subscribe failed", err.Error())
		return
	}

	rec, err := decodeCommSubRecord(resp)
	if err != nil {
		r.reply("autonomous subscribe", prettyJSON(resp))
		return
	}
	r.reply("autonomous subscribe", fmt.Sprintf(
		"subscribed %s  kinds=%s%s%s  sink=comm -> %s  coalesce=yes  (owner %s)",
		rec.id, strings.Join(rec.kindList, ","),
		scopePart("prd=", rec.PRDID), scopePart("task=", rec.TaskID),
		rec.Target, rec.Owner))
}

// scopePart renders " <k>v" when v is non-empty, else "".
func scopePart(k, v string) string {
	if v == "" {
		return ""
	}
	return " " + k + v
}

// ── unsubscribe ────────────────────────────────────────────────────────────

// handleAutonomousSubsUnsubscribe implements "autonomous unsubscribe
// <id>": ownership check (owner-scoped list) then DELETE.
func (r *Router) handleAutonomousSubsUnsubscribe(args []string) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		r.reply("autonomous unsubscribe", "usage: autonomous unsubscribe <id>")
		return
	}
	id := strings.TrimSpace(args[0])
	identity := r.commSubIdentity()

	// Ownership isolation: the id must be one of THIS channel's
	// subscriptions before the DELETE goes out.
	body, err := r.commGet(commSubPath, url.Values{"owner": {identity}})
	if err != nil {
		r.reply("autonomous unsubscribe failed", err.Error())
		return
	}
	if !commSubListContainsID(body, id) {
		r.reply("autonomous unsubscribe",
			fmt.Sprintf("id %q not found (a channel can only delete its own subscriptions)", id))
		return
	}

	if _, err := r.commJSON(http.MethodDelete, commSubPath+"/"+url.PathEscape(id), ""); err != nil {
		r.reply("autonomous unsubscribe failed", err.Error())
		return
	}
	r.reply("autonomous unsubscribe", "deleted "+id)
}

// ── subscriptions (list) ───────────────────────────────────────────────────

// handleAutonomousSubsList implements "autonomous subscriptions": the
// owner-scoped list, one record per line.
func (r *Router) handleAutonomousSubsList() {
	identity := r.commSubIdentity()
	body, err := r.commGet(commSubPath, url.Values{"owner": {identity}})
	if err != nil {
		r.reply("autonomous subscriptions", err.Error())
		return
	}

	if strings.TrimSpace(body) == "[]" {
		r.reply("autonomous subscriptions", "(none for this channel)")
		return
	}
	var ids []json.RawMessage
	if err := json.Unmarshal([]byte(body), &ids); err != nil {
		r.reply("autonomous subscriptions", prettyJSON(body))
		return
	}
	var lines []string
	for _, raw := range ids {
		rec, err := decodeCommSubRecord(string(raw))
		if err != nil {
			r.reply("autonomous subscriptions", prettyJSON(body))
			return
		}
		lines = append(lines, renderCommSubLine(rec))
	}
	r.reply("autonomous subscriptions", strings.Join(lines, "\n"))
}
