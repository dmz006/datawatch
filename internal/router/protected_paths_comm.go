// autonomous protected-paths over the chat channel: the protected-paths status
// read and the per-Automaton authorised list, so the operator can ask from
// Signal / Telegram which Automaton tasks are held because their commit changed
// protected paths, which paths those are and which question waits for an
// answer — and authorise paths in advance — without curl.
//
//	autonomous protected-paths [automaton-id]
//	    GET /api/autonomous/protected-paths — a read; nothing is approved,
//	    rejected or released.
//	autonomous authorize-paths <automaton-id> <path> [<path>...]
//	autonomous authorize-paths <automaton-id> clear
//	    POST /api/autonomous/prds/{id}/set_authorized_paths — replaces the list
//	    of protected paths that Automaton may change without an operator
//	    decision; clear empties it.
//
// The held-for-protected-path alert and the question announcement reach the
// chat channel through the alert sink and the question channel. The alert body
// is built in internal/autonomous (recordProtectedPathsEvent), not in this
// package, so its wording is covered by the tests there and no alert-text test
// lives here. The three settings behind the gate are written with the existing
// `configure <key>=<value>`; there is no parallel configure verb.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// protectedPathsUsage is the usage line for the protected-paths read verb. The
// optional argument narrows the read to one Automaton.
const protectedPathsUsage = "usage: autonomous protected-paths [automaton-id]"

// authorizePathsUsage is the usage line for the authorize-paths verb.
const authorizePathsUsage = "usage: autonomous authorize-paths <automaton-id> <path> [<path>...] | autonomous authorize-paths <automaton-id> clear"

// protectedPathsCommListed caps the paths named in one comm list.
const protectedPathsCommListed = 20

// protectedPathsQuestionHint points at the question queue, where the operator
// answers a held task: approve, reject or change the spec.
const protectedPathsQuestionHint = "Answer a held task in the question queue: question list, then question answer <question-id> approve | reject | change_spec."

// ProtectedPathsCommVerbs lists the protected-paths comm verbs for the parity
// test.
func ProtectedPathsCommVerbs() []string {
	return []string{"protected-paths", "authorize-paths"}
}

// handleProtectedPathsVerb answers the autonomous command's protected-paths
// and authorize-paths verbs.
func (r *Router) handleProtectedPathsVerb(verb string, args []string) {
	switch verb {
	case "protected-paths", "protected_paths":
		r.replyProtectedPaths(args)
	case "authorize-paths", "authorize_paths":
		r.replyAuthorizePaths(args)
	default:
		r.reply("autonomous protected-paths failed", protectedPathsUsage)
	}
}

func (r *Router) replyProtectedPaths(args []string) {
	if len(args) > 2 {
		r.reply("autonomous protected-paths failed", protectedPathsUsage)
		return
	}
	q := url.Values{}
	if len(args) == 2 && strings.TrimSpace(args[1]) != "" {
		q.Set("automaton", strings.TrimSpace(args[1]))
	}
	out, err := r.commGet("/api/autonomous/protected-paths", q)
	if err != nil {
		r.reply("autonomous protected-paths failed", err.Error())
		return
	}
	var st autonomous.ProtectedPathsStatus
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		r.reply("autonomous protected-paths failed", "reading the Automaton protected-paths status: "+err.Error())
		return
	}
	r.reply("autonomous protected-paths", strings.TrimRight(renderProtectedPathsComm(st), "\n"))
}

// renderProtectedPathsComm formats the protected-paths read for a chat
// message: the settings in effect, the held tasks with their paths and
// question, the Automata that authorise paths, and where to answer.
func renderProtectedPathsComm(st autonomous.ProtectedPathsStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Automaton protected paths (action: %s, rules source: %s)\n", st.Action, st.RulesSource)
	if len(st.Paths) == 0 {
		b.WriteString("Protected list: none\n")
	} else {
		fmt.Fprintf(&b, "Protected list (%d): %s\n", len(st.Paths), boundedProtectedList(st.Paths))
	}
	if len(st.Held) == 0 {
		b.WriteString("Held: no Automaton task is held on protected paths.\n")
	} else {
		fmt.Fprintf(&b, "Held: %d\n", len(st.Held))
		for _, e := range st.Held {
			fmt.Fprintf(&b, "- Automaton %s (%s), task %s (%s): %s\n",
				protectedCommName(e.AutomatonTitle, e.AutomatonID), e.AutomatonID,
				protectedCommName(e.TaskTitle, e.TaskID), e.TaskID, boundedProtectedList(e.Paths))
			if e.Reason != "" {
				b.WriteString("    reason: " + e.Reason + "\n")
			}
			switch {
			case e.AwaitingSpecChange:
				b.WriteString("    waiting for the task spec to change\n")
			case e.QuestionID != "":
				b.WriteString("    question: " + e.QuestionID + " (approve, reject or change the spec)\n")
			}
		}
	}
	if len(st.Authorized) == 0 {
		b.WriteString("Authorised in advance: no Automaton authorises protected paths.\n")
	} else {
		fmt.Fprintf(&b, "Authorised in advance: %d\n", len(st.Authorized))
		for _, e := range st.Authorized {
			fmt.Fprintf(&b, "- Automaton %s (%s): %s\n",
				protectedCommName(e.AutomatonTitle, e.AutomatonID), e.AutomatonID, boundedProtectedList(e.Paths))
		}
	}
	if len(st.Held) > 0 {
		b.WriteString(protectedPathsQuestionHint + "\n")
	}
	return b.String()
}

func (r *Router) replyAuthorizePaths(args []string) {
	if len(args) < 3 {
		r.reply("autonomous authorize-paths failed", authorizePathsUsage)
		return
	}
	id := strings.TrimSpace(args[1])
	if id == "" || strings.ContainsAny(id, "/?#\\") {
		r.reply("autonomous authorize-paths failed", authorizePathsUsage)
		return
	}
	paths := []string{}
	if len(args) == 3 && strings.EqualFold(args[2], "clear") {
		// The word clear on its own empties the list.
	} else {
		paths = append(paths, args[2:]...)
	}
	body, _ := json.Marshal(map[string]any{"paths": paths})
	out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(id)+"/set_authorized_paths", string(body))
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			r.reply("autonomous authorize-paths failed", "Automaton "+id+" was not found")
			return
		}
		r.reply("autonomous authorize-paths failed", err.Error())
		return
	}
	var got struct {
		ID              string   `json:"id"`
		Title           string   `json:"title"`
		AuthorizedPaths []string `json:"authorized_paths"`
	}
	_ = json.Unmarshal([]byte(out), &got)
	if got.ID == "" {
		got.ID = id
	}
	who := fmt.Sprintf("Automaton %s (%s)", protectedCommName(got.Title, got.ID), got.ID)
	if len(got.AuthorizedPaths) == 0 {
		r.reply("autonomous authorize-paths", who+" authorises no protected paths: the list is empty.")
		return
	}
	r.reply("autonomous authorize-paths", fmt.Sprintf("%s authorises %d protected path(s): %s\n"+
		"This does not answer a task that is already held. %s",
		who, len(got.AuthorizedPaths), boundedProtectedList(got.AuthorizedPaths), protectedPathsQuestionHint))
}

// protectedCommName is a title, or the id when the title is empty.
func protectedCommName(title, id string) string {
	if title == "" {
		return id
	}
	return title
}

// boundedProtectedList joins paths, naming at most protectedPathsCommListed of
// them and counting the rest.
func boundedProtectedList(paths []string) string {
	if len(paths) <= protectedPathsCommListed {
		return strings.Join(paths, ", ")
	}
	return strings.Join(paths[:protectedPathsCommListed], ", ") +
		fmt.Sprintf(" and %d more", len(paths)-protectedPathsCommListed)
}
