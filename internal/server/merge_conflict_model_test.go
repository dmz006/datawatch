package server

import (
	"context"
	"errors"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// conflictProposerSpy is a mergeRunner that also proposes conflict answers,
// recording the prompt and model name it was asked with.
type conflictProposerSpy struct {
	mergeRunSpy
	reply     string
	err       error
	gotPrompt string
	gotModel  string
}

func (c *conflictProposerSpy) ProposeMergeConflict(prompt, model string) (string, error) {
	c.gotPrompt, c.gotModel = prompt, model
	return c.reply, c.err
}

func TestConflictModelAdapter(t *testing.T) {
	t.Run("fake reply passes through", func(t *testing.T) {
		spy := &conflictProposerSpy{reply: `{"a.go":"resolved"}`}
		m := newConflictModel(spy, "model-x")
		if m == nil {
			t.Fatal("newConflictModel returned nil for a named model")
		}
		got, err := m.Propose(context.Background(), "the prompt")
		if err != nil {
			t.Fatalf("Propose: %v", err)
		}
		if got != `{"a.go":"resolved"}` {
			t.Errorf("reply = %q, want the fake reply unchanged", got)
		}
		if spy.gotPrompt != "the prompt" || spy.gotModel != "model-x" {
			t.Errorf("proposer got prompt=%q model=%q", spy.gotPrompt, spy.gotModel)
		}
	})
	t.Run("proposer error passes through", func(t *testing.T) {
		want := errors.New("model down")
		spy := &conflictProposerSpy{err: want}
		m := newConflictModel(spy, "model-x")
		if _, err := m.Propose(context.Background(), "p"); !errors.Is(err, want) {
			t.Errorf("err = %v, want %v", err, want)
		}
	})
	t.Run("cancelled context is returned without calling the model", func(t *testing.T) {
		spy := &conflictProposerSpy{reply: "x"}
		m := newConflictModel(spy, "model-x")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := m.Propose(ctx, "p"); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if spy.gotModel != "" {
			t.Errorf("model was called after cancel: %q", spy.gotModel)
		}
	})
	t.Run("empty name gives no model", func(t *testing.T) {
		spy := &conflictProposerSpy{reply: "x"}
		if m := newConflictModel(spy, ""); m != nil {
			t.Errorf("newConflictModel(\"\") = %#v, want nil", m)
		}
	})
	t.Run("runner without a proposer gives no model", func(t *testing.T) {
		if m := newConflictModel(&mergeRunSpy{}, "model-x"); m != nil {
			t.Errorf("newConflictModel without a proposer = %#v, want nil", m)
		}
	})
}

func TestNewMergeRunnerConflictSettings(t *testing.T) {
	t.Run("named model, mode and confirm count are wired", func(t *testing.T) {
		name, mode, n := "model-x", "on", 2
		cfg := config.MergePipelineConfig{ConflictModel: &name, ConflictMode: mode, ConflictConfirmFirstN: &n}
		spy := &conflictProposerSpy{reply: "ok"}
		runner := newMergeRunner(spy, cfg, t.TempDir(), "proj", t.TempDir(), nil)
		if runner.Model == nil {
			t.Fatal("runner.Model is nil for a named model")
		}
		if _, err := runner.Model.Propose(context.Background(), "p"); err != nil || spy.gotModel != "model-x" {
			t.Errorf("runner.Model proposed with model %q (err %v), want model-x", spy.gotModel, err)
		}
		s := runner.ConflictSettings()
		if s.Mode != "on" {
			t.Errorf("Mode = %q, want on", s.Mode)
		}
		if s.ConfirmFirstN != 2 {
			t.Errorf("ConfirmFirstN = %d, want 2", s.ConfirmFirstN)
		}
	})
	t.Run("empty model name leaves runner.Model nil", func(t *testing.T) {
		empty := ""
		cfg := config.MergePipelineConfig{ConflictModel: &empty}
		runner := newMergeRunner(&conflictProposerSpy{}, cfg, t.TempDir(), "proj", t.TempDir(), nil)
		if runner.Model != nil {
			t.Errorf("runner.Model = %#v, want nil for an empty model name", runner.Model)
		}
	})
	t.Run("defaults are shadow mode, five confirmations and the default model", func(t *testing.T) {
		spy := &conflictProposerSpy{reply: "ok"}
		runner := newMergeRunner(spy, config.MergePipelineConfig{}, t.TempDir(), "proj", t.TempDir(), nil)
		if _, err := runner.Model.Propose(context.Background(), "p"); err != nil || spy.gotModel != config.MergePipelineDefaultConflictModel {
			t.Errorf("default model = %q (err %v), want %q", spy.gotModel, err, config.MergePipelineDefaultConflictModel)
		}
		s := runner.ConflictSettings()
		if s.Mode != config.ConflictModeShadow {
			t.Errorf("Mode = %q, want shadow", s.Mode)
		}
		if s.ConfirmFirstN != config.MergeConflictConfirmFirstNDefault {
			t.Errorf("ConfirmFirstN = %d, want %d", s.ConfirmFirstN, config.MergeConflictConfirmFirstNDefault)
		}
	})
}
