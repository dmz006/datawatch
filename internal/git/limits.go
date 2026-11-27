// A hard limit for the git processes of one piece of work.
//
// Git's memory use follows the shape of the repository it is given: a push
// from a shallow clone with very many refs needs memory that grows with the
// square of the number of refs times the history. Work that datawatch starts
// by itself over a project's repository carries a ProcessLimit in its
// context, and every git process started through Command under that context
// is held to it. Without a limit in the context Command is a plain git
// command.

package git

import (
	"context"
	"os/exec"
	"strconv"
	"time"
)

// ProcessLimit bounds git processes.
type ProcessLimit struct {
	// MemoryBytes is the most data memory (heap and private anonymous
	// mappings) one git process may hold. Every process git starts itself,
	// such as the receiving side of a local push, inherits it. The kernel
	// enforces it on Linux; mapped pack files are not counted.
	MemoryBytes int64
	// Time is the longest one git command may run. At the end of it the
	// command and everything it started are stopped.
	Time time.Duration
}

// ShadowProcessLimit holds the git steps of a merge shadow's engine, copy and
// push checks. The limit over each command's whole process tree, those of the
// shadow's other commands included, is package proclimit's.
var ShadowProcessLimit = ProcessLimit{MemoryBytes: 2 << 30, Time: 10 * time.Minute}

type processLimitKey struct{}

// WithProcessLimit returns a context under which Command applies l.
func WithProcessLimit(ctx context.Context, l ProcessLimit) context.Context {
	return context.WithValue(ctx, processLimitKey{}, l)
}

// ProcessLimitOf returns the limit ctx carries.
func ProcessLimitOf(ctx context.Context) (ProcessLimit, bool) {
	l, ok := ctx.Value(processLimitKey{}).(ProcessLimit)
	return l, ok
}

// limitStopDelay is how long a stopped command's output is waited for.
const limitStopDelay = 5 * time.Second

// Command builds "git args..." under ctx, held to the limit ctx carries. The
// caller sets the folder and environment, runs it and then calls done.
func Command(ctx context.Context, args ...string) (cmd *exec.Cmd, done context.CancelFunc) {
	l, ok := ProcessLimitOf(ctx)
	if !ok {
		return exec.CommandContext(ctx, "git", args...), func() {} // #nosec G204 -- fixed binary, arguments built by the caller
	}
	done = func() {}
	if l.Time > 0 {
		ctx, done = context.WithTimeout(ctx, l.Time)
	}
	// A packing thread's stack counts as data memory, so the number of
	// threads does not follow the number of cores.
	args = append([]string{"-c", "pack.threads=2"}, args...)
	cmd = limitedCommand(ctx, strconv.FormatInt(l.MemoryBytes/1024, 10), args)
	cmd.WaitDelay = limitStopDelay
	return cmd, done
}
