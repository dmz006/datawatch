// A repeating schedule aimed at a session by name is not ended by a send that
// fails. It stays scheduled, and the operator is alerted on the first failure
// in a row and then on every 24th, not on every one.

package session

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func failNextSend(f *FakeTmux) {
	f.mu.Lock()
	f.FailNextOp = map[string]int{"send-keys": 1, "send-keys-settle": 1}
	f.mu.Unlock()
}

func TestNameRecurringSendErrorsAlertOnTheFirstAndEvery24th(t *testing.T) {
	for _, kind := range []string{"cron", "every"} {
		t.Run(kind, func(t *testing.T) {
			mgr, f, sched := newNameSchedEnv(t)
			alerts := alertSink(mgr)
			holder := addNamedHolder(t, mgr, f, "aa01", "supervisor", StateRunning, idlePane)
			opts := AddOptions{SessionID: holder.FullID, SessionName: "supervisor", Command: "tick", RunAt: time.Now().Add(time.Hour)}
			if kind == "cron" {
				opts.CronExpr = "*/5 * * * *"
			} else {
				opts.RecurEvery = 60
			}
			sc, err := sched.AddFull(opts)
			if err != nil {
				t.Fatal(err)
			}
			var alertedAt []int
			fail := func(n int) {
				t.Helper()
				makeDue(t, sched, sc.ID)
				failNextSend(f)
				before := len(*alerts)
				cur, _ := sched.Get(sc.ID)
				if sent, _ := mgr.FireScheduled(sched, holder.FullID, cur); sent {
					t.Fatalf("failure %d was reported as sent", n)
				}
				got := wantState(t, sched, sc.ID, SchedPending)
				if got.ConsecutiveFailures != n || !got.RunAt.After(time.Now()) || got.LastError == "" {
					t.Fatalf("after failure %d: count=%d run_at=%s error=%q", n, got.ConsecutiveFailures, got.RunAt, got.LastError)
				}
				if len(*alerts) > before {
					alertedAt = append(alertedAt, n)
					if a := (*alerts)[len(*alerts)-1]; !a.SendFailed || a.Schedule.ID != sc.ID {
						t.Fatalf("alert at failure %d = %+v", n, a)
					}
				}
			}
			for n := 1; n <= 50; n++ {
				fail(n)
			}
			if fmt.Sprint(alertedAt) != "[1 24 48]" || len(*alerts) != 3 {
				t.Fatalf("alerted at failures %v (%d alerts), want [1 24 48]", alertedAt, len(*alerts))
			}
			// A send that works ends the run: the next failure is a first one.
			makeDue(t, sched, sc.ID)
			cur, _ := sched.Get(sc.ID)
			if sent, err := mgr.FireScheduled(sched, holder.FullID, cur); !sent || err != nil {
				t.Fatalf("sent=%v err=%v", sent, err)
			}
			if got := wantState(t, sched, sc.ID, SchedPending); got.ConsecutiveFailures != 0 || got.LastError != "" {
				t.Fatalf("failure not cleared: %+v", got)
			}
			alertedAt = nil
			fail(1)
			fail(2)
			if fmt.Sprint(alertedAt) != "[1]" {
				t.Fatalf("after a good send, alerted at %v, want [1]", alertedAt)
			}
		})
	}
}

func TestNameOneTimeSendErrorStillFails(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	alerts := alertSink(mgr)
	holder := addNamedHolder(t, mgr, f, "aa01", "supervisor", StateRunning, idlePane)
	sc, err := sched.AddFull(AddOptions{SessionID: holder.FullID, SessionName: "supervisor", Command: "once", RunAt: time.Now().Add(-time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	failNextSend(f)
	if sent, _ := mgr.FireScheduled(sched, holder.FullID, sc); sent {
		t.Fatal("reported as sent")
	}
	wantState(t, sched, sc.ID, SchedFailed)
	if len(*alerts) != 1 {
		t.Fatalf("%d alerts, want 1", len(*alerts))
	}
}

// The session can end between the send and the record of its failure. A name
// outlives its session; a recurrence past its deadline does not outlive that.
func TestNameRecurringSendErrorTargetAndDeadline(t *testing.T) {
	_, _, sched := newNameSchedEnv(t)
	sc := addCron(t, sched, "testhost-aa01", "supervisor")
	out, err := sched.recordSendFailure(sc.ID, errors.New("pane gone"), true, time.Now())
	if err != nil || !out.rearmed {
		t.Fatalf("rearmed=%v err=%v", out.rearmed, err)
	}
	wantState(t, sched, sc.ID, SchedPending)

	last, err := sched.AddFull(AddOptions{SessionName: "supervisor", Command: "last", RecurEvery: 3600,
		RunAt: time.Now(), RecurUntil: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if out, _ = sched.recordSendFailure(last.ID, errors.New("pane gone"), false, time.Now()); out.rearmed {
		t.Fatal("re-armed past its deadline")
	}
	wantState(t, sched, last.ID, SchedFailed)
}
