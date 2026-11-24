package session

import (
	"context"
	"testing"
)

// A session whose task the daemon types itself (DaemonDeliversTask) is not
// started because its channel is ready or a claude process is running: both
// are true of an agent sitting at an empty prompt that never got its task.
// It counts as started once the task is delivered or the agent has answered.

func daemonDeliveredSession(t *testing.T, mgr *Manager, paneCmd string, fakePane map[string]string) *Session {
	t.Helper()
	sess := newAgentStartedSession(t, mgr)
	fakePane[sess.TmuxSession] = paneCmd
	sess.DaemonDeliversTask = true
	sess.ChannelReady = true
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestAgentStarted_DaemonDelivery_ChannelReadyWithoutTaskIsNotStarted(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := daemonDeliveredSession(t, mgr, "claude", fake.PaneCmd)

	if mgr.AgentStarted(sess.FullID) {
		t.Error("channel-ready session with a running agent but an undelivered task reported as started")
	}
}

func TestAgentStarted_DaemonDelivery_DeliveredTaskIsStarted(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := daemonDeliveredSession(t, mgr, "claude", fake.PaneCmd)
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("claim should succeed on a fresh session")
	}

	if !mgr.AgentStarted(sess.FullID) {
		t.Error("session whose task was delivered reported as not started")
	}
}

func TestAgentStarted_DaemonDelivery_AssistantTurnIsStarted(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := daemonDeliveredSession(t, mgr, "bash", fake.PaneCmd)
	writeAgentLog(t, sess, "Welcome banner\n❯ count the files\n• Listing the directory\n")

	if !mgr.AgentStarted(sess.FullID) {
		t.Error("recorded assistant turn reported as not started")
	}
}

type channelBackendStub struct{ on bool }

func (b channelBackendStub) ChannelEnabled() bool { return b.on }

// TestArmTaskDeliveryAfterStart_RecordsDaemonDelivery: the start of a
// channel-enabled claude-code session with a task records on the stored
// session that the daemon delivers the task; a launch without the channel, or
// without a task, does not.
func TestArmTaskDeliveryAfterStart_RecordsDaemonDelivery(t *testing.T) {
	for _, c := range []struct {
		name    string
		channel bool
		task    string
		want    bool
	}{
		{"channel launch with a task", true, "count the files", true},
		{"command-line launch", false, "count the files", false},
		{"channel launch without a task", true, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			mgr, fake := newTestManagerWithFake(t)
			sess := newAgentStartedSession(t, mgr)
			fake.PaneCmd[sess.TmuxSession] = "claude"
			sess.Task = c.task
			if err := mgr.SaveSession(sess); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // the background delivery trigger must not run
			mgr.armTaskDeliveryAfterStart(ctx, sess, channelBackendStub{on: c.channel}, "")
			got, _ := mgr.GetSession(sess.FullID)
			if got.DaemonDeliversTask != c.want {
				t.Errorf("DaemonDeliversTask = %v, want %v", got.DaemonDeliversTask, c.want)
			}
		})
	}
}
