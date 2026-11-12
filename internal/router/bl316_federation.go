// BL316 S2 — comm-channel verbs for federation peer and group management.
//
//	federation peers                               → list peers
//	federation peer add <name> <url> [token=<t>]   → register peer
//	federation peer delete <name>                  → remove peer
//	federation peer test <name>                    → ping peer /api/health
//	federation peer fingerprint <name> on|off      → toggle fingerprint exchange
//	federation groups                              → list builtin + custom groups

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const federationUsage = `Usage:
  federation peers                                    list federation peers
  federation peer add <name> <url> [token=<t>]        register a peer
  federation peer delete <name>                       remove a peer
  federation peer test <name>                         ping peer /api/health
  federation peer fingerprint <name> on|off           opt the peer into / out of the resource-fingerprint exchange
  federation groups                                   list groups (builtins + custom)
  federation fingerprint list [state]                 list fingerprint matches (state: pending, confirmed, rejected)
  federation fingerprint confirm <id>                 confirm a match — the two resources become one shared pool
  federation fingerprint reject <id>                  reject a match — the resources stay separate (confirm undone)`

// fedFingerprintMatch mirrors the daemon's /api/federation/fingerprint-matches
// record (the fields sufficient to identify the pair in a chat reply).
type fedFingerprintMatch struct {
	ID        string `json:"id"`
	Peer      string `json:"peer"`
	Kind      string `json:"kind"`
	LocalName string `json:"local_name"`
	PeerName  string `json:"peer_name"`
	State     string `json:"state"`
}

func (r *Router) handleFederationCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	lower := strings.ToLower(text)

	if lower == "help" {
		r.reply("federation", federationUsage)
		return
	}

	// "federation peers" or "federation peer list"
	if lower == "peers" || lower == "peer list" || lower == "peer" {
		out, err := r.commGet("/api/federation/peers", nil)
		if err != nil {
			r.reply("federation peers failed", err.Error())
			return
		}
		r.reply("federation peers", prettyJSON(out))
		return
	}

	// "federation groups"
	if lower == "groups" || lower == "group list" || lower == "group" {
		out, err := r.commGet("/api/federation/groups", nil)
		if err != nil {
			r.reply("federation groups failed", err.Error())
			return
		}
		r.reply("federation groups", prettyJSON(out))
		return
	}

	// "federation peer add <name> <url> [token=<t>]"
	if strings.HasPrefix(lower, "peer add ") {
		parts := strings.Fields(text[len("peer add "):])
		if len(parts) < 2 {
			r.reply("federation peer add", "Usage: federation peer add <name> <url> [token=<t>]")
			return
		}
		name, url := parts[0], parts[1]
		body := map[string]any{"name": name, "url": url, "enabled": true}
		for _, kv := range parts[2:] {
			if strings.HasPrefix(strings.ToLower(kv), "token=") {
				body["token"] = kv[len("token="):]
			}
		}
		data, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, "/api/federation/peers", string(data))
		if err != nil {
			r.reply("federation peer add failed", err.Error())
			return
		}
		r.reply("federation peer add", prettyJSON(out))
		return
	}

	// "federation peer delete <name>"
	if strings.HasPrefix(lower, "peer delete ") || strings.HasPrefix(lower, "peer del ") {
		var name string
		if strings.HasPrefix(lower, "peer delete ") {
			name = strings.TrimSpace(text[len("peer delete "):])
		} else {
			name = strings.TrimSpace(text[len("peer del "):])
		}
		out, err := r.commJSON(http.MethodDelete, "/api/federation/peers/"+name, "")
		if err != nil {
			r.reply("federation peer delete failed", err.Error())
			return
		}
		r.reply("federation peer delete", prettyJSON(out))
		return
	}

	// "federation peer test <name>"
	if strings.HasPrefix(lower, "peer test ") {
		name := strings.TrimSpace(text[len("peer test "):])
		out, err := r.commJSON(http.MethodPost, "/api/federation/peers/"+name+"/test", "")
		if err != nil {
			r.reply("federation peer test failed", err.Error())
			return
		}
		r.reply("federation peer test: "+name, prettyJSON(out))
		return
	}

	// "federation peer fingerprint <name> on|off" — opt the peer into (on)
	// or out of (off) the resource-fingerprint exchange. fingerprint_sync
	// is boolean, so the verb takes an explicit on/off: "off" sends an
	// explicit false, which would be indistinguishable from "unset" (no
	// change) if the key were simply dropped from the body.
	if strings.HasPrefix(lower, "peer fingerprint ") {
		bits := strings.Fields(text[len("peer fingerprint "):])
		if len(bits) != 2 {
			r.reply("federation peer fingerprint", "Usage: federation peer fingerprint <name> on|off")
			return
		}
		on := false
		switch strings.ToLower(bits[1]) {
		case "on", "true", "1", "yes", "enable":
			on = true
		case "off", "false", "0", "no", "disable":
			on = false
		default:
			r.reply("federation peer fingerprint", "fingerprint: expected on or off, got "+bits[1])
			return
		}
		body, _ := json.Marshal(map[string]any{"name": bits[0], "fingerprint_sync": on})
		out, err := r.commJSON(http.MethodPut, "/api/federation/peers/"+bits[0], string(body))
		if err != nil {
			r.reply("federation peer fingerprint failed", err.Error())
			return
		}
		r.reply("federation peer fingerprint", prettyJSON(out))
		return
	}

	// "federation fingerprint list [state]", "federation fingerprint
	// confirm <id>", "federation fingerprint reject <id>" — the match
	// records found during fingerprint reconciliation. Confirm merges the
	// pair into one shared pool; reject keeps (or makes) them separate.
	if lower == "fingerprint" || strings.HasPrefix(lower, "fingerprint ") {
		bits := strings.Fields(text[len("fingerprint"):])
		switch {
		// "fingerprint" alone lists everything, like "peers" alone.
		case len(bits) == 0 || bits[0] == "list":
			if len(bits) > 2 {
				r.reply("federation fingerprint list", "Usage: federation fingerprint list [pending|confirmed|rejected]")
				return
			}
			q := url.Values{}
			if len(bits) == 2 {
				state := bits[1]
				if state != "pending" && state != "confirmed" && state != "rejected" {
					r.reply("federation fingerprint list", "state must be pending, confirmed or rejected")
					return
				}
				q.Set("state", state)
			}
			out, err := r.commGet("/api/federation/fingerprint-matches", q)
			if err != nil {
				r.reply("federation fingerprint list failed", err.Error())
				return
			}
			var body struct {
				Matches []fedFingerprintMatch `json:"matches"`
			}
			if jsonErr := json.Unmarshal([]byte(out), &body); jsonErr != nil {
				r.reply("federation fingerprint list", prettyJSON(out))
				return
			}
			if len(body.Matches) == 0 {
				note := "no fingerprint matches"
				if len(bits) == 2 {
					note += " (state filter: " + bits[1] + ")"
				}
				r.reply("federation fingerprint list", note)
				return
			}
			lines := make([]string, 0, len(body.Matches)+1)
			for _, m := range body.Matches {
				lines = append(lines, fmt.Sprintf("%s  state=%s  kind=%s  local=%s  peer=%s/%s",
					m.ID, m.State, m.Kind, m.LocalName, m.Peer, m.PeerName))
			}
			lines = append(lines, "NOTE: confirming a match makes the two resources one shared pool; rejecting keeps them separate.")
			r.reply("federation fingerprint list", strings.Join(lines, "\n"))
			return
		case bits[0] == "confirm" && len(bits) == 2:
			out, err := r.commJSON(http.MethodPost, "/api/federation/fingerprint-matches/"+bits[1]+"/confirm", "")
			if err != nil {
				r.reply("federation fingerprint confirm failed", err.Error())
				return
			}
			var m fedFingerprintMatch
			if jsonErr := json.Unmarshal([]byte(out), &m); jsonErr != nil {
				r.reply("federation fingerprint confirm", prettyJSON(out))
				return
			}
			r.reply("federation fingerprint confirm",
				fmt.Sprintf("confirmed %s: %s and %s/%s are now one shared pool", m.ID, m.LocalName, m.Peer, m.PeerName))
			return
		case bits[0] == "reject" && len(bits) == 2:
			out, err := r.commJSON(http.MethodPost, "/api/federation/fingerprint-matches/"+bits[1]+"/reject", "")
			if err != nil {
				r.reply("federation fingerprint reject failed", err.Error())
				return
			}
			var m fedFingerprintMatch
			if jsonErr := json.Unmarshal([]byte(out), &m); jsonErr != nil {
				r.reply("federation fingerprint reject", prettyJSON(out))
				return
			}
			r.reply("federation fingerprint reject",
				fmt.Sprintf("rejected %s: %s and %s/%s are separate resources again (confirm undone if it had been confirmed)", m.ID, m.LocalName, m.Peer, m.PeerName))
			return
		}
		r.reply("federation fingerprint", federationUsage)
		return
	}

	r.reply("federation", federationUsage)
}
