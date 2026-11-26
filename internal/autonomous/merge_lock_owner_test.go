//go:build linux

package autonomous

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// A lock is taken over only when its owner is provably gone.
func TestMergeLockIsTakenOverOnlyFromAProvenGoneOwner(t *testing.T) {
	ended := exec.Command("true")
	if err := ended.Run(); err != nil {
		t.Fatal(err)
	}
	live := currentMergeLockOwner()
	for name, c := range map[string]struct {
		owner string
		taken bool
	}{
		"no owner recorded":    {``, false},
		"the owner is running": {`,"owner":{"pid":` + strconv.Itoa(live.PID) + `,"started":"` + live.Started + `","boot":"` + live.Boot + `","instance":"` + live.Instance + `"}`, false},
		// The daemon restarts by replacing its own program, which keeps the
		// process id and start time.
		"this process before it restarted itself": {`,"owner":{"pid":` + strconv.Itoa(live.PID) + `,"started":"` + live.Started + `","boot":"` + live.Boot + `","instance":"before-the-restart"}`, true},
		"the owner's process ended":               {`,"owner":{"pid":` + strconv.Itoa(ended.Process.Pid) + `,"started":"1","boot":"` + live.Boot + `"}`, true},
		"the pid now names another process":       {`,"owner":{"pid":` + strconv.Itoa(live.PID) + `,"started":"1","boot":"` + live.Boot + `"}`, true},
		"the machine restarted since":             {`,"owner":{"pid":` + strconv.Itoa(live.PID) + `,"started":"` + live.Started + `","boot":"another-boot"}`, true},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			body := `{"/repo":{"repo":"/repo","run_id":"run-old","automaton_id":"a-old","branches":["b"],"since":"2026-01-01T00:00:00Z"` + c.owner + `}}`
			if err := os.WriteFile(filepath.Join(dir, mergeLocksFile), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			locks, err := NewMergeLocks(dir)
			if err != nil {
				t.Fatal(err)
			}
			_, err = locks.Acquire("/repo", MergeLockHolder{RunID: "run-new", AutomatonID: "a-new", Branches: []string{"c"}})
			if (err == nil) != c.taken {
				t.Fatalf("err = %v, want taken=%v", err, c.taken)
			}
			if err != nil && !errors.Is(err, ErrMergeLockHeld) {
				t.Errorf("err = %v, want ErrMergeLockHeld", err)
			}
			held, _ := locks.Status("/repo")
			if want := map[bool]string{true: "run-new", false: "run-old"}[c.taken]; held.RunID != want {
				t.Errorf("held by %s, want %s", held.RunID, want)
			}
		})
	}
}

// Where processes cannot be listed, nothing shows that what a gone run started
// has ended too, so its lock stays.
func TestMergeLockStaysWhenLeftoversCannotBeChecked(t *testing.T) {
	ended := exec.Command("true")
	if err := ended.Run(); err != nil {
		t.Fatal(err)
	}
	was := processTable
	processTable = filepath.Join(t.TempDir(), "no-process-table")
	t.Cleanup(func() { processTable = was })
	dir := t.TempDir()
	body := `{"/repo":{"repo":"/repo","run_id":"run-old","automaton_id":"a-old","branches":["b"],"since":"2026-01-01T00:00:00Z","owner":{"pid":` +
		strconv.Itoa(ended.Process.Pid) + `}}}`
	if err := os.WriteFile(filepath.Join(dir, mergeLocksFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	locks, err := NewMergeLocks(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = locks.Acquire("/repo", MergeLockHolder{RunID: "run-new", AutomatonID: "a-new", Branches: []string{"c"}})
	if !errors.Is(err, ErrMergeLockHeld) || !errors.Is(err, ErrMergeLockLeftovers) {
		t.Fatalf("err = %v, want the lock kept because nothing can be checked", err)
	}
}
