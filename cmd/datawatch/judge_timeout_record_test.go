package main

import (
	"context"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// neverAnsweringJudge is a fake judge attempt that launches a fake session,
// blocks until its run context ends, and on timeout records the session id and
// the last output on the attempt, as the real attempt does.
func neverAnsweringJudge(clock *judgeTestClock) judgeAttemptFn {
	return func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
		var releases = func() {}
		admit := func(context.Context, string, string, string, string) (func(), string, error) {
			return releases, "node", nil
		}
		v, _, err := runJudgeTimed(context.Background(), admit, att, "", "u1", "p1", att.Limit, clock.now,
			func(runCtx context.Context, _ string) (autonomouspkg.GuardrailVerdict, error) {
				att.SessionID = "sess-" + backend
				att.LastOutput = "last output of " + backend
				<-runCtx.Done()
				clock.advance(att.Limit)
				return autonomouspkg.GuardrailVerdict{}, runCtx.Err()
			})
		return v, err
	}
}

func TestJudgeNeverAnswers(t *testing.T) {
	clock := &judgeTestClock{t: time.Now()}
	backends := []string{"alpha", "beta", "gamma"}
	var alerts []string
	req := autonomouspkg.GuardrailInvocation{PRDID: "p1", UnitID: "u1", Guardrail: "rules"}
	v, err := runJudgeAttempts(req, judgeChooserFromList(backends), 20*time.Millisecond,
		func(level, title, body string) { alerts = append(alerts, level+": "+title) },
		nil, nil, nil, neverAnsweringJudge(clock))
	if err != nil {
		t.Fatal(err)
	}
	if !autonomouspkg.IsJudgeTimeoutHold(v) {
		t.Fatalf("verdict = %+v, want a judge timeout hold", v)
	}
	if len(v.JudgeAttempts) != len(backends) {
		t.Fatalf("attempt records = %d, want %d", len(v.JudgeAttempts), len(backends))
	}
	for i, rec := range v.JudgeAttempts {
		b := backends[i]
		if rec.Backend != b || rec.Outcome != autonomouspkg.JudgeOutcomeTimedOut ||
			rec.SessionID != "sess-"+b || rec.LastOutput != "last output of "+b {
			t.Errorf("record %d = %+v", i, rec)
		}
	}
	if v.JudgeSessionID != "sess-gamma" || v.RawJudge != "last output of gamma" {
		t.Errorf("verdict session %q output %q, want the last attempt's", v.JudgeSessionID, v.RawJudge)
	}
	if len(alerts) == 0 || !strings.HasPrefix(alerts[len(alerts)-1], "error:") {
		t.Errorf("alerts = %v, want a final error alert", alerts)
	}
}

func TestTimedOutJudgeRecordsSession(t *testing.T) {
	long := strings.Repeat("x", 3*autonomouspkg.JudgeLastOutputMax) + "TAIL"
	attempts := []judgeAttempt{
		{Backend: "alpha", Started: time.Now(), Elapsed: time.Minute, TimedOut: true, SessionID: "s1", LastOutput: long},
		{Backend: "beta", Started: time.Now(), NoSlot: true},
	}
	recs := judgeAttemptRecords(attempts)
	if recs[0].SessionID != "s1" || !strings.HasSuffix(recs[0].LastOutput, "TAIL") || len(recs[0].LastOutput) > autonomouspkg.JudgeLastOutputMax {
		t.Errorf("timed out record = session %q, %d bytes", recs[0].SessionID, len(recs[0].LastOutput))
	}
	if recs[1].SessionID != "" || recs[1].LastOutput != "" {
		t.Errorf("no-slot record carries a session: %+v", recs[1])
	}
	v := judgeHoldVerdict(autonomouspkg.GuardrailInvocation{PRDID: "p1"}, attempts)
	if v.JudgeSessionID != "s1" || !strings.HasSuffix(v.RawJudge, "TAIL") {
		t.Errorf("hold verdict session %q output len %d", v.JudgeSessionID, len(v.RawJudge))
	}
}
