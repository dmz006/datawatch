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

// renderInfraRetriesForChat renders the status and cuts it to fit one chat
// message. It drops the oldest tasks first and always keeps the newest one; if
// that one task is still too long it drops its oldest retries, and as a last
// resort cuts the text itself. Each cut is named in a closing line, so the
// reader knows what was left out.
func renderInfraRetriesForChat(st autonomous.InfraRetryStatus) string {
	total := len(st.Tasks)
	shown := total
	droppedRetries := 0
	for {
		text := composeInfraRetriesChat(st, shown, droppedRetries)
		if len(text) <= infraRetryChatMaxChars {
			return text
		}
		switch {
		case shown > 1:
			shown--
		case shown == 1 && len(st.Tasks[0].Retries)-droppedRetries > 1:
			droppedRetries++
		default:
			return cutInfraRetriesChat(text)
		}
	}
}

// composeInfraRetriesChat renders the first shown tasks, with the oldest
// droppedRetries retries of the first task left out, and appends the lines that
// say what was left out.
func composeInfraRetriesChat(st autonomous.InfraRetryStatus, shown, droppedRetries int) string {
	view := st
	view.Tasks = append([]autonomous.InfraRetryTask(nil), st.Tasks[:shown]...)
	if droppedRetries > 0 {
		first := view.Tasks[0]
		first.Retries = first.Retries[droppedRetries:]
		view.Tasks[0] = first
	}
	text := strings.TrimRight(autonomous.RenderInfraRetryText(view, 0), "\n")
	if droppedRetries > 0 {
		text += fmt.Sprintf("\n(%d earlier %s of the newest task not shown)", droppedRetries, retryWord(droppedRetries))
	}
	if left := len(st.Tasks) - shown; left > 0 {
		text += fmt.Sprintf("\n(%d older %s not shown; add an Automaton id or a smaller limit to narrow the list)", left, taskWord(left))
	}
	return text
}

// cutInfraRetriesChat cuts text to the chat budget at a line break when it can,
// and says it was cut.
func cutInfraRetriesChat(text string) string {
	const note = "\n(reply cut to fit one message)"
	keep := text[:infraRetryChatMaxChars-len(note)]
	if i := strings.LastIndex(keep, "\n"); i > 0 {
		keep = keep[:i]
	}
	return strings.ToValidUTF8(keep, "") + note
}

// taskWord is "task" for one and "tasks" otherwise.
func taskWord(n int) string {
	if n == 1 {
		return "task"
	}
	return "tasks"
}

// retryWord is "retry" for one and "retries" otherwise.
func retryWord(n int) string {
	if n == 1 {
		return "retry"
	}
	return "retries"
}
