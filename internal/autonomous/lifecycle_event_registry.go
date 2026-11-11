// Registry of the built-in lifecycle event kinds, each with a one-line
// description and a group. KnownEventKinds and IsKnownEventKind derive
// from it, so the set a subscription may filter on has one source.

package autonomous

import "sort"

// Event kind groups.
const (
	EventGroupAutomaton = "automaton"
	EventGroupTask      = "task"
	EventGroupReview    = "review"
	EventGroupCapacity  = "capacity"
	EventGroupBudget    = "budget"
)

// EventKindInfo describes one built-in lifecycle event kind.
type EventKindInfo struct {
	Kind        string `json:"kind"`
	Description string `json:"description"` // one line
	Group       string `json:"group"`       // one of the EventGroup* values
}

// validEventGroup reports whether g is one of the defined groups.
func validEventGroup(g string) bool {
	switch g {
	case EventGroupAutomaton, EventGroupTask, EventGroupReview,
		EventGroupCapacity, EventGroupBudget:
		return true
	}
	return false
}

// notSubscribableKinds are registry kinds a subscription filter does not
// accept: task.looping is emitted and listed, but was never part of the
// accepted set, and the stored format must not change.
var notSubscribableKinds = map[string]bool{
	KindTaskLooping: true,
}

var eventKindRegistry = buildEventKindRegistry()

func buildEventKindRegistry() []EventKindInfo {
	var out []EventKindInfo
	add := func(kind, group, desc string) {
		out = append(out, EventKindInfo{Kind: kind, Description: desc, Group: group})
	}

	autoDesc := map[PRDStatus]string{
		PRDDraft:          "An Automaton was created or returned to draft.",
		PRDPlanning:       "An Automaton started decomposing its spec into tasks.",
		PRDApproved:       "An Automaton was approved and may run.",
		PRDRevisionsAsked: "Revisions were requested and the Automaton will be decomposed again.",
		PRDActive:         "An Automaton entered the legacy active state.",
		PRDRunning:        "An Automaton started running its tasks.",
		PRDCompleted:      "An Automaton finished with every task done.",
		PRDRejected:       "The decomposition was rejected and the Automaton is closed.",
		PRDCancelled:      "An Automaton was cancelled while running.",
		PRDArchived:       "An Automaton was archived.",
		PRDBlocked:        "A guardrail blocked an Automaton until the operator acts.",
		PRDFailed:         "An Automaton ended because tasks failed after all retries.",
		PRDPaused:         "An Automaton was paused; running tasks finish and no new ones start.",
	}
	for s, d := range autoDesc {
		add(PRDStatusKind(s), EventGroupAutomaton, d)
	}
	add(PRDStatusKind(PRDNeedsReview), EventGroupReview,
		"An Automaton was decomposed and waits for operator review.")
	add(KindPRDUpdated, EventGroupAutomaton,
		"An Automaton was saved; an internal refresh signal, not a notification.")

	taskDesc := map[TaskStatus]string{
		TaskPending:      "A task was created and is not yet queued.",
		TaskQueued:       "A task was queued to start.",
		TaskInProgress:   "A task started working.",
		TaskRunningTests: "A task started running its tests.",
		TaskVerifying:    "A task started its verification step.",
		TaskCompleted:    "A task finished and passed verification.",
		TaskFailed:       "A task failed after all retries.",
		TaskBlocked:      "A task is blocked and needs attention.",
		TaskCancelled:    "A task was cancelled.",
	}
	for s, d := range taskDesc {
		add(TaskStatusKind(s), EventGroupTask, d)
	}
	add(TaskStatusKind(TaskWaitingCapacity), EventGroupCapacity,
		"A task is waiting for a free worker lane.")
	add(TaskStatusKind(TaskAwaitingApproval), EventGroupReview,
		"A task is waiting for operator approval before it starts.")
	add(TaskStatusKind(TaskReviewNeeded), EventGroupReview,
		"A task result needs operator review.")

	add(KindTaskSessionStarted, EventGroupTask, "A task got a working session.")
	add(KindTaskSessionCleared, EventGroupTask, "A task's working session was cleared.")
	add(KindTaskLooping, EventGroupTask, "A task's session appears to be repeating itself.")
	add(KindScopeWarning, EventGroupTask, "A task touched files outside its allowed paths.")
	add(KindGuardrailWarn, EventGroupTask, "A guardrail returned a warning for a task.")
	add(KindQuestionAnswered, EventGroupReview, "An operator question was answered.")
	add(KindQuestionWithdrawn, EventGroupReview, "An operator question was withdrawn.")

	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// ListEventKinds returns every built-in event kind, sorted by group and
// then kind. The slice is a fresh copy.
func ListEventKinds() []EventKindInfo {
	out := make([]EventKindInfo, len(eventKindRegistry))
	copy(out, eventKindRegistry)
	return out
}
