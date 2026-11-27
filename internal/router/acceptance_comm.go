// "Done when" lists on the comm channel, through the /api/acceptance
// loopback:
//
//	acceptance [list]
//	acceptance show <request>
//	acceptance confirm <request> <item> yes|no <words…>
//	acceptance confirm <request> list yes|no <list hash> <words…>
//	acceptance accept <request> <item> <why…>
//	acceptance edit <request> <json>
//	acceptance freeze <request> <list hash>
//	acceptance report
//	acceptance catalog
//
// On an install with encrypted stores, replies carry ids and results only.
// The word is a command only while acceptance.mode is report: otherwise a
// message that starts with it is an ordinary message.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/acceptance"
)

// acceptanceActorChannel names a change sent from a messaging channel.
const acceptanceActorChannel = "channel"

const acceptanceCommUsage = "usage: acceptance [list] | show <id> | confirm <id> <item> yes|no <words…> | accept <id> <item> <why…> | edit <id> <json> | freeze <id> <hash> | report | catalog"

// parseMessage is Parse plus the "acceptance" command, which exists only while
// the setting is on.
func (r *Router) parseMessage(text string) Command {
	if r.cfg != nil && r.cfg.Acceptance.Mode == acceptance.ModeReport {
		trimmed := strings.TrimSpace(text)
		const verb = "acceptance"
		if lower := strings.ToLower(trimmed); lower == verb || strings.HasPrefix(lower, verb+" ") {
			return parseAcceptance(trimmed[len(verb):])
		}
	}
	return Parse(text)
}

// parseAcceptance keeps the rest of the line; the handler splits it, so spoken
// words keep their case and spacing.
func parseAcceptance(rest string) Command {
	return Command{Type: CmdAcceptance, Text: strings.TrimSpace(rest)}
}

// cutWord splits off the first word.
func cutWord(s string) (word, rest string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t\n"); i >= 0 {
		return s[:i], strings.TrimSpace(s[i:])
	}
	return s, ""
}

type acceptanceCommView struct {
	Mode   string `json:"mode"`
	Secure bool   `json:"secure"`
	Hash   string `json:"hash"`
	List   struct {
		RequestID string `json:"request_id"`
		Items     []struct {
			ID        string `json:"id"`
			Statement string `json:"statement"`
		} `json:"items"`
	} `json:"list"`
	Rollup struct {
		Status string `json:"status"`
		Label  string `json:"label"`
		Items  []struct {
			ID     string `json:"id"`
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
			Accepted bool   `json:"accepted_without_proof"`
			Claim    string `json:"claim"`
		} `json:"items"`
	} `json:"rollup"`
}

// formatAcceptanceView writes one list for a chat. A secure install gets ids
// and results only.
func formatAcceptanceView(body string) string {
	var v acceptanceCommView
	if err := json.Unmarshal([]byte(body), &v); err != nil || v.List.RequestID == "" {
		return body
	}
	statements := map[string]string{}
	if !v.Secure {
		for _, it := range v.List.Items {
			statements[it.ID] = it.Statement
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s", v.List.RequestID, v.Rollup.Label)
	for _, it := range v.Rollup.Items {
		result := strings.ReplaceAll(it.Result.Value, "_", " ")
		if it.Accepted {
			result = "accepted without proof"
		}
		if it.Claim != "" {
			result += " (an answer is shown as a claim and does not count)"
		}
		fmt.Fprintf(&b, "\n  %s: %s", it.ID, result)
		if s := statements[it.ID]; s != "" {
			fmt.Fprintf(&b, " — %s", s)
		}
	}
	fmt.Fprintf(&b, "\nlist hash: %s", v.Hash)
	return b.String()
}

// formatAcceptanceList writes every list in short, ids and results only.
func formatAcceptanceList(body string) string {
	var v struct {
		Mode     string `json:"mode"`
		Overview struct {
			Lists []struct {
				RequestID string `json:"request_id"`
				Label     string `json:"label"`
				Open      int    `json:"open_items"`
				Due       string `json:"due"`
			} `json:"lists"`
		} `json:"overview"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return body
	}
	if v.Mode == "off" {
		return "done-when lists are off (set acceptance.mode to report)"
	}
	if len(v.Overview.Lists) == 0 {
		return "no done-when lists"
	}
	var b strings.Builder
	for _, l := range v.Overview.Lists {
		fmt.Fprintf(&b, "%s: %s, %d open", l.RequestID, l.Label, l.Open)
		if l.Due != "" {
			fmt.Fprintf(&b, " (%s)", strings.ReplaceAll(l.Due, "_", " "))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// acceptanceCommRequest builds the write for confirm, accept, freeze and edit.
func acceptanceCommRequest(verb, rest string) (path string, body map[string]any, ok bool) {
	id, rest := cutWord(rest)
	if id == "" || rest == "" {
		return "", nil, false
	}
	body = map[string]any{}
	switch verb {
	case "confirm":
		item, rest := cutWord(rest)
		answer, words := cutWord(rest)
		if item == "list" {
			var hash string
			hash, words = cutWord(words)
			body["hash"] = hash
		}
		if words == "" {
			return "", nil, false
		}
		body["item"], body["answer"], body["words"] = item, strings.ToLower(answer), words
		path = "/api/acceptance/confirm"
	case "accept":
		item, why := cutWord(rest)
		if why == "" {
			return "", nil, false
		}
		body["item"], body["reason"], body["words"] = item, why, why
		path = "/api/acceptance/accept"
	case "freeze":
		body["freeze"], body["hash"] = true, rest
		path = "/api/acceptance/edit"
	case "edit":
		if err := json.Unmarshal([]byte(rest), &body); err != nil || body == nil {
			return "", nil, false
		}
		path = "/api/acceptance/edit"
	default:
		return "", nil, false
	}
	// Set last, so a message cannot choose another id or caller name.
	body["id"], body["actor"] = id, acceptanceActorChannel
	delete(body, "via")
	delete(body, "via_session")
	return path, body, true
}

func (r *Router) handleAcceptance(cmd Command) {
	verb, rest := cutWord(cmd.Text)
	verb = strings.ToLower(verb)
	fail := func(msg string) { r.reply("acceptance failed", msg) }
	read := func(path string, q url.Values, format func(string) string) {
		body, err := r.commGet(path, q)
		if err != nil {
			fail(err.Error())
			return
		}
		r.reply("acceptance", format(body))
	}
	same := func(s string) string { return s }
	switch verb {
	case "", "list":
		read("/api/acceptance", nil, formatAcceptanceList)
	case "show":
		if rest == "" {
			fail(acceptanceCommUsage)
			return
		}
		read("/api/acceptance", url.Values{"id": {rest}}, formatAcceptanceView)
	case "catalog":
		read("/api/acceptance/catalog", nil, same)
	case "report":
		read("/api/acceptance/report", nil, formatAcceptanceReport)
	default:
		path, body, ok := acceptanceCommRequest(verb, rest)
		if !ok {
			fail(acceptanceCommUsage)
			return
		}
		payload, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, path, string(payload))
		if err != nil {
			fail(err.Error())
			return
		}
		r.reply("acceptance", formatAcceptanceView(out))
	}
}

// formatAcceptanceReport writes the standing check, ids and fixed texts only.
func formatAcceptanceReport(body string) string {
	var v struct {
		Starts   int `json:"starts_seen"`
		Findings []struct {
			Check     string `json:"check"`
			RequestID string `json:"request_id"`
			Item      string `json:"item"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return body
	}
	var b strings.Builder
	fmt.Fprintf(&b, "standing check (report only): %d finding(s), %d request start(s) seen", len(v.Findings), v.Starts)
	for _, f := range v.Findings {
		fmt.Fprintf(&b, "\n  %s %s %s", f.Check, f.RequestID, f.Item)
	}
	return b.String()
}
