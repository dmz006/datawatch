//go:build linux

package git

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// zeros is an endless stream of zero bytes.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestCommandHoldsGitToItsMemoryLimit(t *testing.T) {
	dir := t.TempDir()
	// Git reads what it is to hash from a pipe into memory.
	hash := func(ctx context.Context, size int64) (string, error) {
		cmd, done := Command(ctx, "hash-object", "--stdin")
		defer done()
		var out bytes.Buffer
		cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = dir, io.LimitReader(zeros{}, size), &out, &out
		err := cmd.Run()
		return out.String(), err
	}
	ctx := WithProcessLimit(context.Background(), ProcessLimit{MemoryBytes: 64 << 20, Time: time.Minute})
	if out, err := hash(ctx, 1<<20); err != nil {
		t.Fatalf("a small input under the limit: %v: %s", err, out)
	}
	if out, err := hash(ctx, 200<<20); err == nil {
		t.Errorf("200 MB was read under a 64 MB limit: %s", out)
	}
	if out, err := hash(context.Background(), 200<<20); err != nil {
		t.Errorf("without a limit: %v: %s", err, out)
	}
}

func TestCommandPassesItsLimitToWhatGitStarts(t *testing.T) {
	ctx := WithProcessLimit(context.Background(), ProcessLimit{MemoryBytes: 512 << 20, Time: time.Minute})
	cmd, done := Command(ctx, "-c", "alias.limit=!ulimit -d", "limit")
	defer done()
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != strconv.Itoa(512<<10) {
		t.Errorf("a process git started has the data limit %q KiB", got)
	}
}

func TestCommandStopsGitAndWhatItStartedAtTheTimeLimit(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "outlived")
	ctx := WithProcessLimit(context.Background(), ProcessLimit{MemoryBytes: 512 << 20, Time: 300 * time.Millisecond})
	cmd, done := Command(ctx, "-c", "alias.wait=!sleep 2; touch '"+mark+"'", "wait")
	defer done()
	cmd.Dir = t.TempDir()
	start := time.Now()
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Errorf("the command was not stopped: %s", out)
	}
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Errorf("it took %s", took)
	}
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Errorf("what git started ran on: %v", err)
	}
}

func TestCommandWithoutALimitIsPlainGit(t *testing.T) {
	cmd, done := Command(context.Background(), "version")
	defer done()
	if got := strings.Join(cmd.Args, " "); got != "git version" || cmd.SysProcAttr != nil {
		t.Errorf("args=%q attr=%v", got, cmd.SysProcAttr)
	}
}
