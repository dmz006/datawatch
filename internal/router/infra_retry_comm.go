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
// resort cuts the text itself. Each cut is named in closing lines that survive
// every cut, so the reader always knows what was left out. Each task renders
// once, so the cost grows with what is shown, not with how many tasks exist.
func renderInfraRetriesForChat(st autonomous.InfraRetryStatus) string {
	total := len(st.Tasks)
	if total == 0 {
		return strings.TrimRight(autonomous.RenderInfraRetryText(st, 0), "\n")
	}
	newest := st.Tasks[0]
	head, block := infraRetryChatBlock(st, newest)
	fits := func(text, footer string) bool { return len(text)+len(footer) <= infraRetryChatMaxChars }

	footer := infraRetryChatFooter(0, total-1)
	if fits(head+block, footer) {
		body := head + block
		shown := 1
		for shown < total {
			_, next := infraRetryChatBlock(st, st.Tasks[shown])
			if !fits(body+next, infraRetryChatFooter(0, total-shown-1)) {
				break
			}
			body += next
			shown++
		}
		return strings.TrimRight(body, "\n") + infraRetryChatFooter(0, total-shown)
	}

	// The newest task alone is too long: drop its oldest retries, as few as
	// will fit, keeping at least the latest one.
	retries := newest.Retries
	render := func(dropped int) (string, string) {
		cut := newest
		cut.Retries = retries[dropped:]
		_, b := infraRetryChatBlock(st, cut)
		return strings.TrimRight(head+b, "\n"), infraRetryChatFooter(dropped, total-1)
	}
	maxDrop := len(retries) - 1
	if maxDrop < 0 {
		maxDrop = 0
	}
	lo, hi := 0, maxDrop
	for lo < hi {
		mid := (lo + hi) / 2
		if text, foot := render(mid); fits(text, foot) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	text, foot := render(lo)
	if fits(text, foot) {
		return text + foot
	}
	return cutInfraRetriesChat(text, foot)
}

// infraRetryChatBlock renders one task through the shared text and returns the
// settings line and the task's block separately.
func infraRetryChatBlock(st autonomous.InfraRetryStatus, t autonomous.InfraRetryTask) (head, block string) {
	view := st
	view.Tasks = []autonomous.InfraRetryTask{t}
	text := autonomous.RenderInfraRetryText(view, 0)
	i := strings.Index(text, "\n") + 1
	return text[:i], text[i:]
}

// infraRetryChatFooter is the closing lines that say what a cut left out: the
// oldest retries of the newest task, and the older tasks.
func infraRetryChatFooter(droppedRetries, leftTasks int) string {
	var b strings.Builder
	if droppedRetries > 0 {
		fmt.Fprintf(&b, "\n(%d earlier %s of the newest task not shown)", droppedRetries, retryWord(droppedRetries))
	}
	if leftTasks > 0 {
		fmt.Fprintf(&b, "\n(%d older %s not shown; add an Automaton id or a smaller limit to narrow the list)", leftTasks, taskWord(leftTasks))
	}
	return b.String()
}

// cutInfraRetriesChat cuts text so that text, a note that it was cut and the
// footer fit the chat budget. It cuts at a line break when it can.
func cutInfraRetriesChat(text, footer string) string {
	const note = "\n(reply cut to fit one message)"
	budget := infraRetryChatMaxChars - len(note) - len(footer)
	if budget < 0 {
		budget = 0
	}
	keep := text
	if len(keep) > budget {
		keep = keep[:budget]
		if i := strings.LastIndex(keep, "\n"); i > 0 {
			keep = keep[:i]
		}
	}
	return strings.ToValidUTF8(keep, "") + note + footer
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
