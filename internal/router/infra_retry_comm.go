package router

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// infraRetryChatTasks is how many tasks a chat reply reads when the caller gives
// no limit.
const infraRetryChatTasks = 10

// infraRetryChatLimitMax is the largest limit GET /api/autonomous/infra-retries accepts.
const infraRetryChatLimitMax = 1000

// infraRetryChatMaxChars is the most text a chat reply carries, so it fits one
// chat message.
const infraRetryChatMaxChars = 3000

const infraRetriesUsage = "usage: autonomous infra-retries [automaton-id] [limit]"

// InfraRetryCommVerbs lists the infrastructure retry comm verbs, for the parity
// test.
func InfraRetryCommVerbs() []string {
	return []string{"infra-retries"}
}

// replyInfraRetries reads the infrastructure retry status and replies with the
// settings line and one block per task, newest first, cut to fit one chat
// message. args[0] is the verb; then an optional Automaton ID and an optional
// limit. A single word that is a whole number from 1 to 1000 is the limit.
func (r *Router) replyInfraRetries(args []string) {
	const label = "autonomous infra-retries"
	words := args[1:]
	automaton, limitWord := "", ""
	switch {
	case len(words) > 2:
		r.reply(label+" failed", infraRetriesUsage)
		return
	case len(words) == 2:
		automaton, limitWord = words[0], words[1]
	case len(words) == 1:
		if n, err := strconv.Atoi(words[0]); err == nil && n >= 1 && n <= infraRetryChatLimitMax {
			limitWord = words[0]
		} else {
			automaton = words[0]
		}
	}
	limit := infraRetryChatTasks
	if limitWord != "" {
		n, err := strconv.Atoi(limitWord)
		if err != nil || n < 1 || n > infraRetryChatLimitMax {
			r.reply(label+" failed", infraRetriesUsage)
			return
		}
		limit = n
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if automaton != "" {
		q.Set("automaton", automaton)
	}
	out, err := r.commGet("/api/autonomous/infra-retries", q)
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	st, err := autonomous.ParseInfraRetryStatus([]byte(out))
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	r.reply(label, renderInfraRetriesForChat(st))
}

// renderInfraRetriesForChat renders the status and drops the oldest blocks
// until the text fits one chat message, always keeping the newest block. It ends
// with a line saying how many tasks were left out.
func renderInfraRetriesForChat(st autonomous.InfraRetryStatus) string {
	total := len(st.Tasks)
	shown := total
	text := ""
	for {
		view := st
		view.Tasks = st.Tasks[:shown]
		text = strings.TrimRight(autonomous.RenderInfraRetryText(view, 0), "\n")
		if shown <= 1 || len(text) <= infraRetryChatMaxChars {
			break
		}
		shown--
	}
	if left := total - shown; left > 0 {
		text += fmt.Sprintf("\n(%d older %s not shown; add an Automaton id or a smaller limit to narrow the list)", left, taskWord(left))
	}
	return text
}

// taskWord is "task" for one and "tasks" otherwise.
func taskWord(n int) string {
	if n == 1 {
		return "task"
	}
	return "tasks"
}
