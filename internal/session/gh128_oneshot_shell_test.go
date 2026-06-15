// GH#128 — one-shot shell task wrapping.
// When a spawn is created with OneShot=true and a task starting with "!",
// the launch function must receive the task with "; echo "DATAWATCH_COMPLETE: shell task done""
// appended so the session terminates after the command exits instead of
// leaking in waiting_input forever.

package session

import (
	"context"
	"strings"
	"testing"
)

func TestGH128_OneShotShellTaskWrapped(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)

	var receivedTask string
	mgr.SetLLMBackend("test", func(_ context.Context, task, tmuxSession, _, _ string) error {
		receivedTask = task
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})

	_, err := mgr.Start(context.Background(), "!/usr/bin/env my-cmd --flag", "", t.TempDir(), &StartOptions{
		OneShot: true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !strings.HasSuffix(receivedTask, `; echo "DATAWATCH_COMPLETE: shell task done"`) {
		t.Errorf("launchFn received task %q — expected DATAWATCH_COMPLETE suffix", receivedTask)
	}
	if !strings.HasPrefix(receivedTask, "!/usr/bin/env my-cmd --flag") {
		t.Errorf("launchFn received task %q — original command must be preserved", receivedTask)
	}
}

func TestGH128_NonOneShotShellTaskNotWrapped(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)

	var receivedTask string
	mgr.SetLLMBackend("test", func(_ context.Context, task, tmuxSession, _, _ string) error {
		receivedTask = task
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})

	_, err := mgr.Start(context.Background(), "!/usr/bin/env my-cmd", "", t.TempDir(), &StartOptions{
		OneShot: false,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if receivedTask != "!/usr/bin/env my-cmd" {
		t.Errorf("non-one-shot task must not be wrapped; got %q", receivedTask)
	}
}

func TestGH128_OneShotAITaskNotWrapped(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)

	var receivedTask string
	mgr.SetLLMBackend("test", func(_ context.Context, task, tmuxSession, _, _ string) error {
		receivedTask = task
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})

	_, err := mgr.Start(context.Background(), "run the audit and output DATAWATCH_COMPLETE: done", "", t.TempDir(), &StartOptions{
		OneShot: true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if receivedTask != "run the audit and output DATAWATCH_COMPLETE: done" {
		t.Errorf("AI task (no ! prefix) must not be wrapped; got %q", receivedTask)
	}
}
