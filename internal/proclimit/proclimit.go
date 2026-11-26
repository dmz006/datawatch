// Package proclimit holds a command, and everything it starts, to one hard
// memory limit, and stops all of it together.
//
// Work that datawatch starts by itself over a project, such as the shadow of
// a merge, runs commands the project supplies. A limit on each process does
// not bound what such a command starts. A Starter puts the whole process tree
// under one limit; the context of the work carries it, and every place that
// starts a command for that work runs it through Prepare.
package proclimit

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"
)

// None is the Starter where the limit cannot be had: it starts nothing under
// it and says why.
func None(why error) Starter { return none{why} }

type none struct{ why error }

func (n none) Available(context.Context) error { return n.why }

func (n none) Prepare(*exec.Cmd, time.Duration) (func() Result, error) { return nil, n.why }

func (n none) Describe() string { return "no hard memory limit can be had here: " + n.why.Error() }

// RunWhereHad runs cmd under the limit ctx carries, and without one where ctx
// carries None. It is for datawatch's own reads, not for a project's commands.
func RunWhereHad(ctx context.Context, cmd *exec.Cmd) error {
	if s, ok := From(ctx); ok {
		if _, isNone := s.(none); isNone {
			return cmd.Run()
		}
	}
	return Run(ctx, cmd)
}

// CombinedOutput is Run with the command's output and error output together.
// What the limit said is added to it.
func CombinedOutput(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := Run(ctx, cmd)
	var over *LimitError
	if errors.As(err, &over) {
		out.WriteString("\n" + over.Why)
	}
	return out.Bytes(), err
}

// ShadowTreeBytes is the most memory one command of a shadow may use together
// with everything it starts.
const ShadowTreeBytes int64 = 16 << 30

// Starter puts a command and its whole process tree under a limit.
type Starter interface {
	// Available returns why the limit cannot be had here, nil when it can.
	Available(ctx context.Context) error
	// Prepare changes cmd, not yet started, so that it runs under the limit.
	// maxRun, when above zero, is how long the command may run: the tree is
	// stopped after it even if this process is gone. finish is called once
	// the command has ended or failed to start.
	Prepare(cmd *exec.Cmd, maxRun time.Duration) (finish func() Result, err error)
	// Describe says what the limit covers.
	Describe() string
}

// Result is what the limit saw of one command.
type Result struct {
	// Failed says why the command must be read as failed because of the
	// limit; "" when the limit ended nothing.
	Failed string
	// Left is how many processes the command started that were still running
	// when it ended. They have been stopped.
	Left int
}

// LimitError is a command the limit made fail.
type LimitError struct{ Why string }

func (e *LimitError) Error() string { return e.Why }

type starterKey struct{}

type held struct {
	s    Starter
	left *Tally
}

// Tally counts the processes that commands left running.
type Tally struct {
	mu sync.Mutex
	n  int
}

// Add counts n more.
func (t *Tally) Add(n int) {
	if t == nil || n <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.n += n
}

// Count returns how many were counted.
func (t *Tally) Count() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.n
}

// With returns a context whose commands run through s. left, when not nil,
// counts what checked commands leave running.
func With(ctx context.Context, s Starter, left *Tally) context.Context {
	return context.WithValue(ctx, starterKey{}, held{s, left})
}

// From returns the Starter ctx carries.
func From(ctx context.Context) (Starter, bool) {
	h, ok := ctx.Value(starterKey{}).(held)
	return h.s, ok && h.s != nil
}

// NoteLeft counts processes a checked command left running.
func NoteLeft(ctx context.Context, n int) {
	if h, ok := ctx.Value(starterKey{}).(held); ok {
		h.left.Add(n)
	}
}

// Prepare puts cmd, built under ctx and not yet started, under the limit ctx
// carries. Without one it changes nothing. When the limit cannot be applied
// the command must not be started.
func Prepare(ctx context.Context, cmd *exec.Cmd) (finish func() Result, err error) {
	s, ok := From(ctx)
	if !ok {
		return func() Result { return Result{} }, nil
	}
	var maxRun time.Duration
	if d, ok := ctx.Deadline(); ok {
		maxRun = max(time.Until(d), time.Second)
	}
	return s.Prepare(cmd, maxRun)
}

// Run runs cmd under the limit ctx carries. A command the limit made fail
// returns a *LimitError, whatever its own exit status was.
func Run(ctx context.Context, cmd *exec.Cmd) error {
	finish, err := Prepare(ctx, cmd)
	if err != nil {
		return err
	}
	err = cmd.Run()
	if r := finish(); r.Failed != "" && ctx.Err() == nil {
		return errors.Join(&LimitError{r.Failed}, err)
	}
	return err
}
