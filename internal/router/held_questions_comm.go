// held_questions_comm.go — the `question held` chat verb: the held-question
// report over comm (decision item 23, companion to the `question` verbs in
// questions_comm.go). Mirrors the /api/autonomous/held-questions REST
// surface (internal/server/held_questions.go):
//
//	question held                                    — GET /api/autonomous/held-questions
//	question held prd <prd-id>                       — ?prd_id=<id>
//	question held state <held state id>              — ?state=<id>
//	question held prd <prd-id> state <held state id> — both
//
// Read-only — no verb here writes. The loopback uses the same commGet
// helper as the other comm-parity commands. The daemon answers an
// unknown state filter with a 400 naming the five held state ids; that
// text passes straight through to the channel.

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// heldQuestionsPath is the held-question report read for `question held`.
const heldQuestionsPath = "/api/autonomous/held-questions"

// heldWireOpen is one open held question, as the daemon reports it.
type heldWireOpen struct {
	QuestionID        string `json:"question_id"`
	State             string `json:"state"`
	PRDID             string `json:"prd_id"`
	TaskID            string `json:"task_id"`
	AutomationID      string `json:"automation_id"`
	Title             string `json:"title"`
	Reason            string `json:"reason"`
	RecommendedOption string `json:"recommended_option"`
	Default           string `json:"default"`
	CreatedAt         string `json:"created_at"`
}

// heldWireShadow is one shadow entry the daemon logged (instead of
// raising) in shadow mode.
type heldWireShadow struct {
	PRDID             string `json:"prd_id"`
	State             string `json:"state"`
	RecommendedOption string `json:"recommended_option"`
	At                string `json:"at"`
}

// heldQuestionsBody is the GET /api/autonomous/held-questions response.
type heldQuestionsBody struct {
	Mode   string           `json:"mode"`
	States []string         `json:"states"`
	Open   []heldWireOpen   `json:"open"`
	Shadow []heldWireShadow `json:"shadow"`
}

// parseHeldVerb parses the optional `question held` filters. At most one
// `prd <id>` and one `state <held state id>` pair, in either order.
func parseHeldVerb(parts []string) (questionVerb, error) {
	v := questionVerb{action: "held"}
	for i := 0; i < len(parts); i++ {
		switch parts[i] {
		case "prd":
			if v.heldPRD != "" {
				return questionVerb{}, fmt.Errorf("question held takes one prd filter (%s)", questionUsage)
			}
			if i+1 >= len(parts) {
				return questionVerb{}, fmt.Errorf("question held prd needs an id (%s)", questionUsage)
			}
			v.heldPRD = strings.TrimSpace(parts[i+1])
			i++
		case "state":
			if v.heldState != "" {
				return questionVerb{}, fmt.Errorf("question held takes one state filter (%s)", questionUsage)
			}
			if i+1 >= len(parts) {
				return questionVerb{}, fmt.Errorf("question held state needs a held state id (%s)", questionUsage)
			}
			v.heldState = strings.TrimSpace(parts[i+1])
			i++
		default:
			return questionVerb{}, fmt.Errorf("question held expects prd <id> or state <held state id> (%s)", questionUsage)
		}
	}
	return v, nil
}

// handleQuestionHeld GETs the held-question report and sends it: the mode
// (off / shadow / on) with the states it covers, one block per open held
// question its own, one line per shadow entry. With neither list the
// reply says so.
func (r *Router) handleQuestionHeld(prd, state string) {
	q := url.Values{}
	if prd != "" {
		q.Set("prd_id", prd)
	}
	if state != "" {
		q.Set("state", state)
	}
	body, err := r.commGet(heldQuestionsPath, q)
	if err != nil {
		r.reply("question held failed", err.Error())
		return
	}
	var vb heldQuestionsBody
	if err := json.Unmarshal([]byte(body), &vb); err != nil {
		r.reply("question held failed", "cannot read daemon response: "+err.Error())
		return
	}

	var b strings.Builder
	var modeNote string
	switch vb.Mode {
	case "on":
		modeNote = " — held questions are raised now; answer them with `question answer`"
	case "shadow":
		modeNote = " — shadow mode: the daemon logs would-be holds instead of raising them"
	case "off":
		modeNote = " — the daemon neither raises nor logs held questions"
	}
	states := strings.Join(vb.States, ", ")
	if states == "" {
		states = "(no states reported)"
	}
	fmt.Fprintf(&b, "held mode: %s (%s)%s", vb.Mode, states, modeNote)

	if len(vb.Open) == 0 && len(vb.Shadow) == 0 {
		r.reply("question held", b.String()+"\nnothing on the daemon is held right now")
		return
	}

	for _, o := range vb.Open {
		b.WriteString("\n")
		line := o.QuestionID
		if o.State != "" {
			line += "  held: " + o.State
		}
		who := o.AutomationID
		if o.Title != "" {
			if who != "" {
				who += " (" + o.Title + ")"
			} else {
				who = o.Title
			}
		}
		if who != "" {
			line += "  " + who
		}
		if o.TaskID != "" {
			line += "  task " + o.TaskID
		}
		b.WriteString(line)
		if o.Reason != "" {
			fmt.Fprintf(&b, "\n   why: %s", o.Reason)
		}
		switch {
		case o.RecommendedOption != "" && o.RecommendedOption == o.Default:
			fmt.Fprintf(&b, "\n   recommended: %s (forced default)", o.RecommendedOption)
		case o.RecommendedOption != "" && o.Default != "":
			fmt.Fprintf(&b, "\n   recommended: %s  (default: %s)", o.RecommendedOption, o.Default)
		case o.RecommendedOption != "":
			fmt.Fprintf(&b, "\n   recommended: %s", o.RecommendedOption)
		case o.Default != "":
			fmt.Fprintf(&b, "\n   default: %s", o.Default)
		}
	}

	for _, s := range vb.Shadow {
		line := "\nshadow  " + s.State
		if s.PRDID != "" {
			line += "  " + s.PRDID
		}
		if s.RecommendedOption != "" {
			line += "  recommended: " + s.RecommendedOption
		}
		if s.At != "" {
			line += "  " + s.At
		}
		b.WriteString(line)
	}
	r.reply("question held", b.String())
}
