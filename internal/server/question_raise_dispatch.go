// question_raise_dispatch.go — the question-raise notification fan-out.
// On a successful POST /api/questions the Server fans the event out to
// the three notification surfaces: the WebSocket hub (the return-path
// event for the PWA), the messaging group (through the
// QuestionRaiseNotifier implementation main.go wires), and — for
// high-priority questions only — the alert store. The operator-questions
// switch and the notify_* toggles are read at dispatch time, so a live
// PUT /api/config change takes effect on the next raise; and when the
// switch itself is off, every part here is a no-op. A slow or failing
// part runs in its own goroutine and cannot delay or fail the raise —
// the raise (persist + 201 response) is already done before the fan-out
// starts.

package server

import (
	"strings"
	"sync"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/autonomous"
)

// questionNotifyGate is a sync.WaitGroup whose Add is serialized with
// Wait. A bare WaitGroup panics with "sync: WaitGroup misuse: Add called
// concurrently with Wait" when Add(d>0) runs while the counter is zero
// and another goroutine is inside Wait. That was reachable on the
// daemon-exit path: a still-arriving raise calls Add(1) through
// notifyQuestionRaised in the same instant the shutdown flush is blocked
// in WaitQuestionNotifies (counter zero). Holding one mutex across the
// whole Wait blocks a racing Add until the counter drains, so the Add
// can never interleave with an in-flight Wait.
type questionNotifyGate struct {
	mu sync.Mutex
	wg sync.WaitGroup
}

func (g *questionNotifyGate) add() {
	g.mu.Lock()
	g.wg.Add(1)
	g.mu.Unlock()
}

func (g *questionNotifyGate) done() {
	g.wg.Done()
}

func (g *questionNotifyGate) wait() {
	g.mu.Lock()
	g.wg.Wait()
	g.mu.Unlock()
}

// QuestionRaiseNotifier is the seam the raise flow uses to hand a
// freshly raised question to the messaging group. main.go implements it
// as a fan-out over the active messaging routers (the same async
// r.send path HandleNeedsInput uses), so the server package does not
// depend on the router package.
type QuestionRaiseNotifier interface {
	// NotifyQuestionRaised is called once per successful raise, off the
	// request path. Implementations must not assume anything is wired on
	// their end — a no-op here is safe.
	NotifyQuestionRaised(q autonomous.Question)
}

// SetQuestionRaiseNotifier wires the messaging fan-out. Nil disables the
// messaging part (the WS broadcast and the high-priority alert still
// fire while the switch is on — they do not need a router).
func (s *Server) SetQuestionRaiseNotifier(n QuestionRaiseNotifier) {
	if s == nil {
		return
	}
	s.questionRaiseNotifier = n
}

// HTTPServer delegate, so main.go can wire through the same variable it
// already uses for SetQuestions.
func (s *HTTPServer) SetQuestionRaiseNotifier(n QuestionRaiseNotifier) {
	if s == nil || s.api == nil {
		return
	}
	s.api.SetQuestionRaiseNotifier(n)
}

// NotifyQuestionRaised runs the raise fan-out for a question that was raised
// outside POST /api/questions (a worker question marker), so it reaches the
// same WebSocket, messaging and alert legs, under the same switch and toggles.
func (s *HTTPServer) NotifyQuestionRaised(q autonomous.Question) {
	if s == nil || s.api == nil {
		return
	}
	s.api.notifyQuestionRaised(q)
}

// notifyQuestionRaised fires the three parts for one successful raise.
// It reads the switch live (off = nothing below runs) and the two
// notify toggles live (absent keys count as on). The parts run in one
// goroutine, and a panic inside any of them is recovered — the raise
// itself already succeeded, and the operator still has the return path
// (the 201 response + the polled GET). The goroutine counts itself on
// the Server's questionNotifies WaitGroup so WaitQuestionNotifies can
// block until this raise's fan-out (including the alert-store write)
// has run to completion.
func (s *Server) notifyQuestionRaised(q autonomous.Question) {
	if s.cfg != nil && !s.cfg.Autonomous.Questions.Enabled {
		return // switch off: none of this work fires
	}
	s.questionNotifies.add()
	go func() {
		defer func() { _ = recover() }()
		defer s.questionNotifies.done()

		// (1) WebSocket — the return-path event, always part of the
		// switch (event plus polled GET is the default return path).
		if s.hub != nil {
			s.hub.BroadcastQuestionRaised(QuestionRaisedData{
				ID:       q.ID,
				Label:    q.Label,
				Text:     q.Text,
				Options:  q.Options,
				Priority: q.Priority,
				Context:  q.Context,
			})
		}

		// (2) Messaging group — gated on notify_messaging (absent key
		// counts as on).
		if s.cfg == nil || s.cfg.Autonomous.Questions.EffectiveNotifyMessaging() {
			if n := s.questionRaiseNotifier; n != nil {
				n.NotifyQuestionRaised(q)
			}
		}

		// (3) Alert — high-priority questions only, gated on
		// notify_alert (absent key counts as on). The alert store is
		// the existing alert path; the WS broadcast below is how
		// connected clients learn of it (same as the other alert
		// producers in this package).
		if q.Priority == autonomous.QuestionPriorityHigh &&
			(s.cfg == nil || s.cfg.Autonomous.Questions.EffectiveNotifyAlert()) &&
			s.alertStore != nil {
			title := "Operator question raised (high priority): " + q.ID
			if q.Label != "" {
				title += " — " + q.Label
			}
			body := q.Text
			if len(q.Options) > 0 {
				var parts []string
				for _, o := range q.Options {
					p := o.Label
					if p == "" {
						p = o.ID
					}
					parts = append(parts, p)
				}
				body = strings.TrimSpace(q.Text + "  Options: " + strings.Join(parts, " / "))
			}
			a := s.alertStore.Add(alerts.LevelWarn, title, body, q.SessionID)
			if s.hub != nil {
				s.hub.BroadcastAlert(a)
			}
		}
	}()
}

// WaitQuestionNotifies blocks until every raise fan-out goroutine that
// was in flight at the time of the call has finished. It exists so a
// caller that must be certain no fan-out leg is still running — notably
// the high-priority alert store's Add, which rewrites the alerts JSON
// file — can block before shutting down or reading that state; nothing
// else in the raise path waits for the goroutine, so without this a
// process exit or file read could race the write. Nil-safe: calling on
// a nil Server is a no-op.
func (s *Server) WaitQuestionNotifies() {
	if s == nil {
		return
	}
	s.questionNotifies.wait()
}

// HTTPServer delegate for WaitQuestionNotifies, so main.go and the
// daemon can wait through the same variable they wire the rest of the
// question channel on. Same nil-check pattern as the other delegates in
// this file.
func (s *HTTPServer) WaitQuestionNotifies() {
	if s == nil || s.api == nil {
		return
	}
	s.api.WaitQuestionNotifies()
}
