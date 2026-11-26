//go:build linux

package mergeshadow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/proclimit"
)

func gateProject(t *testing.T, recipe string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\t"+recipe+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A gate of the finish-time plan whose process tree passes the limit fails
// with that reason, even when make itself succeeds.
func TestGateOverTheMemoryLimitFailsWithThatReason(t *testing.T) {
	limit := proclimit.Scope{MemoryBytes: 64 << 20}
	if err := limit.Available(context.Background()); err != nil {
		t.Skipf("no systemd scope here: %v", err)
	}
	dir := gateProject(t, "@head -c 200000000 /dev/zero | tail | wc -c >/dev/null; true")
	res := runGate(proclimit.With(context.Background(), limit, nil), dir, "test", time.Minute, "")
	if res.Status != GateFail || !strings.Contains(res.Detail, "memory limit") {
		t.Errorf("%+v", res)
	}
}

// Where a limit is asked for and cannot be had, the gate is not run.
func TestGateWithNoLimitToBeHadIsNotRun(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "ran")
	dir := gateProject(t, "@touch '"+mark+"'")
	ctx := proclimit.With(context.Background(), proclimit.None(os.ErrPermission), nil)
	res := runGate(ctx, dir, "test", time.Minute, "")
	if res.Status != GateSkipped || !strings.Contains(res.Detail, "no hard memory limit") {
		t.Errorf("%+v", res)
	}
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Errorf("the gate's command ran: %v", err)
	}
}
