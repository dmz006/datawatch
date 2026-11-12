// Request watch: the alert and the operator question raised when the model
// request of a worker or verifier shows no progress.
//
// The request watch (internal/requestwatch) observes the model request of a
// session whose model runs on a compute node. When a request passes the
// no-progress threshold the watcher calls RaiseRequestWatchNoProgress, which
// sends one alert and, when the operator question channel is on, raises one
// question that offers the configured ways to act. The question is not
// blocking and nothing here changes the task or the session: this code only
// alerts, asks and records. An answer is written to the Automaton's decision
// log as a request_watch_answer entry and nothing else happens; the operator
// acts by hand.
package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/requestwatch"
)

const (
	// requestWatchQuestionAsker is the asked_by of the operator question raised
	// for a request with no progress; it is also how the question is found
	// again.
	requestWatchQuestionAsker = "request-watch"
	// requestWatchQuestionLabel is the question's label.
	requestWatchQuestionLabel = "decision"
	// requestWatchAnswerDecisionKind is the Decision.Kind written when the
	// operator answers the question.
	requestWatchAnswerDecisionKind = "request_watch_answer"
	// requestWatchWithdrawActor is the actor of a withdrawal no operator made.
	requestWatchWithdrawActor = "autonomous"
	// requestWatchAlertTitle is the title of the no-progress alert.
	requestWatchAlertTitle = "Automaton model request is not progressing"
	// requestWatchNothingPerformed is the sentence every action option carries:
	// this release records the choice and performs nothing.
	requestWatchNothingPerformed = "This release only records your choice and performs nothing, so you act by hand."
)

// requestWatchOptionFor returns the question option for a remediation option
// id. ok is false for an id the feature does not know.
func requestWatchOptionFor(id string) (QuestionOption, bool) {
	switch id {
	case "keep_waiting":
		return QuestionOption{ID: id, Label: "Keep waiting",
			Tradeoffs: "Nothing changes and a slow request may still finish."}, true
	case "end_worker":
		return QuestionOption{ID: id, Label: "End only the stuck worker",
			Tradeoffs: "Leaves the model on the node alone. " + requestWatchNothingPerformed}, true
	case "retry_other_backend":
		return QuestionOption{ID: id, Label: "Retry on a stronger backend",
			Tradeoffs: "Runs the work again on another backend. " + requestWatchNothingPerformed}, true
	case "unload_reload_model":
		return QuestionOption{ID: id, Label: "Unload and reload the model on the node",
			Tradeoffs: "Clears a model that may be stuck on the node. " + requestWatchNothingPerformed}, true
	}
	return QuestionOption{}, false
}

// requestWatchOptions returns the question options the configuration offers,
// in order, keep_waiting first. An id the feature does not know is skipped.
func requestWatchOptions(cfg config.RequestWatchConfig) []QuestionOption {
	var out []QuestionOption
	for _, id := range cfg.EffectiveRemediationOptions() {
		if opt, ok := requestWatchOptionFor(id); ok {
			out = append(out, opt)
		}
	}
	return out
}

// SetRequestWatchConfig wires the reader of the live autonomous.request_watch
// block. It is read on every call, so a config change applies with no
// restart. A nil function reads the defaults.
func (m *Manager) SetRequestWatchConfig(fn func() config.RequestWatchConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestWatchCfg = fn
}

// requestWatchConfig returns the request watch policy in effect.
func (m *Manager) requestWatchConfig() config.RequestWatchConfig {
	m.mu.Lock()
	fn := m.requestWatchCfg
	m.mu.Unlock()
	if fn == nil {
		return config.RequestWatchConfig{}
	}
	return fn().Clone()
}

// requestWatchEpisodeKey is the dedupe key of one no-progress episode: a hash
// of the session, the node and the time the episode began, so the same episode
// read twice raises once and a later episode of the same session raises again.
func requestWatchEpisodeKey(o requestwatch.Observation) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s", o.SessionID, o.Node, o.LastChange)))
	return hex.EncodeToString(sum[:])
}

// claimRequestWatchEpisode records key as the episode of the session and
// reports whether it is new. A key already recorded for the session is not
// handled twice.
func (m *Manager) claimRequestWatchEpisode(sessionID, key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.requestWatchEpisodes == nil {
		m.requestWatchEpisodes = map[string]string{}
	}
	if m.requestWatchEpisodes[sessionID] == key {
		return false
	}
	m.requestWatchEpisodes[sessionID] = key
	return true
}

// ForgetRequestWatchEpisode drops the episode recorded for a session, once the
// request recovered or the session ended.
func (m *Manager) ForgetRequestWatchEpisode(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.requestWatchEpisodes, sessionID)
}

// requestWatchSubject names the task a request belongs to: the Automaton's
// label (its title, or its id when it has none) and the task's title (or its
// id when the task cannot be found).
func (m *Manager) requestWatchSubject(o requestwatch.Observation) (automaton, task string) {
	automaton, task = o.AutomatonID, o.TaskID
	prd, ok := m.store.GetPRD(o.AutomatonID)
	if !ok || prd == nil {
		return automaton, task
	}
	m.store.WithLock(func() {
		automaton = automatonLabel(prd)
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				if t := &prd.Story[si].Tasks[ti]; t.ID == o.TaskID && strings.TrimSpace(t.Title) != "" {
					task = t.Title
				}
			}
		}
	})
	return automaton, task
}

// requestWatchMinutes renders the observation's no-progress time in whole
// minutes.
func requestWatchMinutes(o requestwatch.Observation) int {
	return int(o.NoProgressSeconds / 60)
}

// requestWatchMinutesText renders a number of minutes with the right noun: "1
// minute", "30 minutes".
func requestWatchMinutesText(n int) string {
	if n == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", n)
}

// RaiseRequestWatchNoProgress acts on a model request that passed the
// no-progress threshold. With autonomous.request_watch.raise_alert on it sends
// one alert; with raise_question on and the operator question channel enabled
// it raises one non-blocking question. The same episode, identified by the
// session, the node and the time the episode began, is handled once. When no
// question is asked the alert lists the options in plain words, so it alone
// carries the choices. It changes no task and no session.
func (m *Manager) RaiseRequestWatchNoProgress(o requestwatch.Observation) {
	if !m.claimRequestWatchEpisode(o.SessionID, requestWatchEpisodeKey(o)) {
		return
	}
	cfg := m.requestWatchConfig()
	options := requestWatchOptions(cfg)
	store, channelOn := m.operatorQuestionChannel()
	asking := cfg.EffectiveRaiseQuestion() && channelOn && len(options) >= 2

	if cfg.EffectiveRaiseAlert() {
		m.raiseRequestWatchAlert(o, cfg, options, asking)
	}
	if asking {
		m.raiseRequestWatchQuestion(store, o, cfg, options)
	}
}

// raiseRequestWatchAlert sends the one no-progress alert through the sink the
// other Automaton alerts use. Nothing is sent when no sink is wired.
func (m *Manager) raiseRequestWatchAlert(o requestwatch.Observation, cfg config.RequestWatchConfig, options []QuestionOption, asked bool) {
	alert := m.AlertSink()
	if alert == nil {
		return
	}
	automaton, task := m.requestWatchSubject(o)
	var b strings.Builder
	fmt.Fprintf(&b, "Automaton: %s (%s)\nTask: %s (%s)\nRole: %s\nNode: %s\nModel: %s\n",
		automaton, o.AutomatonID, task, o.TaskID, o.Role, o.Node, o.Model)
	fmt.Fprintf(&b, "No progress for: %s (limit %s)\n",
		requestWatchMinutesText(requestWatchMinutes(o)), requestWatchMinutesText(cfg.EffectiveNoProgressMinutes()))
	b.WriteString("Ways to act: keep waiting, since a slow request may still finish")
	if asked {
		b.WriteString(", or answer the operator question for the task. " +
			"Answering only records your choice; this release performs nothing, so you act by hand.")
	} else {
		b.WriteString(". The options are: ")
		for i, opt := range options {
			if i > 0 {
				b.WriteString("; ")
			}
			b.WriteString(opt.Label)
		}
		b.WriteString(". This release only reports the situation and performs nothing, so you act by hand.")
	}
	alert("warn", requestWatchAlertTitle, b.String())
}

// raiseRequestWatchQuestion raises the one non-blocking question for a request
// with no progress. A question already open for the same episode is not raised
// again. A failure is logged and changes nothing.
func (m *Manager) raiseRequestWatchQuestion(store *QuestionStore, o requestwatch.Observation, cfg config.RequestWatchConfig, options []QuestionOption) {
	automaton, task := m.requestWatchSubject(o)
	q := Question{
		Label:     requestWatchQuestionLabel,
		AskedBy:   requestWatchQuestionAsker,
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextTask,
		PRDID:     o.AutomatonID,
		TaskID:    o.TaskID,
		Text: fmt.Sprintf("The model request of the %s of task %s in Automaton %s on node %s has shown no progress for %s (limit %d). How do you want to proceed?",
			o.Role, task, automaton, o.Node, requestWatchMinutesText(requestWatchMinutes(o)), cfg.EffectiveNoProgressMinutes()),
		Options:          options,
		Default:          "keep_waiting",
		Blocking:         false,
		SourceLineSHA256: requestWatchEpisodeKey(o),
	}
	if _, _, err := store.RaiseUnlessOpenSourceLine(q); err != nil {
		log.Printf("[autonomous] prd=%s task=%s: raise request watch question: %v", o.AutomatonID, o.TaskID, err)
	}
}

// WithdrawRequestWatchQuestions withdraws the open request watch questions of
// a task, with reason. A question never waits for an answer that no longer
// matters once the request recovered or the session ended. It does nothing
// when no question store is wired or no such question is open.
func (m *Manager) WithdrawRequestWatchQuestions(automatonID, taskID, actor, reason string) {
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil {
		return
	}
	if actor == "" {
		actor = requestWatchWithdrawActor
	}
	open := store.List(QuestionFilter{State: QuestionOpen, Asker: requestWatchQuestionAsker, PRDID: automatonID, Limit: 1000})
	for _, q := range open {
		if q.TaskID != taskID {
			continue
		}
		if _, err := store.Withdraw(q.ID, actor, reason); err != nil && !errors.Is(err, ErrQuestionNotOpen) {
			log.Printf("[autonomous] prd=%s task=%s: withdraw request watch question %s: %v", automatonID, taskID, q.ID, err)
		}
	}
}

// recordRequestWatchAnswer writes the answer to a request watch question on
// the Automaton's decision log as one request_watch_answer entry that names the
// chosen option and states that the daemon performed no remediation. It
// reports whether the change was such an answer. It changes no task: a task
// keeps the status it has. The same answer is recorded once.
func (m *Manager) recordRequestWatchAnswer(c QuestionChange) bool {
	q := c.Question
	if c.Kind != QuestionChangeAnswered || c.Answer == nil ||
		q.AskedBy != requestWatchQuestionAsker || q.AskerKind != QuestionAskerSystem {
		return false
	}
	if q.PRDID == "" {
		return true
	}
	p, ok := m.store.GetPRD(q.PRDID)
	if !ok {
		return true
	}
	chosen := c.Answer.ChosenOptionID
	if opt, known := requestWatchOptionFor(chosen); known {
		chosen = fmt.Sprintf("%s (%s)", opt.ID, opt.Label)
	}
	if chosen == "" {
		chosen = "no listed option"
	}
	actor := c.Answer.Actor
	if actor == "" {
		actor = reviewDecisionActor
	}
	dec := Decision{
		At:            m.attemptNow(),
		Kind:          requestWatchAnswerDecisionKind,
		Actor:         actor,
		QuestionID:    q.ID,
		AnswerID:      c.Answer.ID,
		DecisionState: string(QuestionAnswered),
		Note: fmt.Sprintf("task %s: the operator chose %s for the stuck model request; the daemon performed no remediation, so nothing was ended, retried or unloaded",
			q.TaskID, chosen),
	}
	appended := false
	m.store.WithLock(func() {
		for _, d := range p.Decisions {
			if d.Kind == dec.Kind && d.QuestionID == dec.QuestionID && d.AnswerID == dec.AnswerID {
				return
			}
		}
		p.Decisions = append(p.Decisions, dec)
		appended = true
	})
	if appended {
		if err := m.store.SavePRD(p); err != nil {
			log.Printf("[autonomous] prd=%s save %s decision: %v", p.ID, dec.Kind, err)
		}
	}
	return true
}
