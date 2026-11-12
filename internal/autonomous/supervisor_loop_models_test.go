package autonomous

import (
	"context"
	"errors"
	"testing"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

func TestSupervisorLoopModelClientRouting(t *testing.T) {
	var one, sess int
	c := &SupervisorModelClient{
		Oneshot: func(_ context.Context, b, m, p string) (string, error) { one++; return "o:" + b + m + p, nil },
		Session: func(_ context.Context, b, m, p string) (string, error) { sess++; return "s", nil },
	}
	ctx := context.Background()
	if out, err := c.Ask(ctx, supervisorloop.Request{Style: supervisorloop.StyleOneshot, Backend: "b", Model: "m", Prompt: "p"}); err != nil || out != "o:bmp" {
		t.Fatalf("%q %v", out, err)
	}
	if out, err := c.Ask(ctx, supervisorloop.Request{Style: supervisorloop.StyleSession, Backend: "b", Prompt: "p"}); err != nil || out != "s" {
		t.Fatalf("%q %v", out, err)
	}
	if one != 1 || sess != 1 {
		t.Fatalf("one=%d sess=%d", one, sess)
	}
}

func TestSupervisorLoopModelClientNoModel(t *testing.T) {
	called := false
	f := func(context.Context, string, string, string) (string, error) { called = true; return "x", nil }
	c := &SupervisorModelClient{Oneshot: f, Session: f}
	_, err := c.Ask(context.Background(), supervisorloop.Request{Style: supervisorloop.StyleOneshot, Prompt: "p"})
	if !errors.Is(err, ErrSupervisorNoModel) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestSupervisorLoopModelClientUsesDecisionPath(t *testing.T) {
	m := &Manager{}
	m.SetDecisionAssistantModelFn(func(_ context.Context, b, mo, p string) (string, error) { return "via:" + b, nil })
	c := NewSupervisorModelClient(m, nil)
	out, err := c.Ask(context.Background(), supervisorloop.Request{Style: supervisorloop.StyleOneshot, Backend: "pi", Prompt: "p"})
	if err != nil || out != "via:pi" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := c.Ask(context.Background(), supervisorloop.Request{Style: supervisorloop.StyleSession, Backend: "pi"}); err == nil {
		t.Fatal("unwired session should error")
	}
}

func TestSupervisorLoopModelJobsAccessor(t *testing.T) {
	m := &Manager{}
	if m.SupervisorModelJobs() != nil {
		t.Fatal("expected none before wiring")
	}
	j := &supervisorloop.ModelJobs{}
	m.SetSupervisorModelJobs(j)
	if m.SupervisorModelJobs() != j {
		t.Fatal("not stored")
	}
}
