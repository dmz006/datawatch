package resources

import (
	"errors"
	"testing"
	"time"
)

type fakeProc struct {
	exists bool
	seq    [][]ProcInfo
	i      int
}

func (f *fakeProc) Exists() bool { return f.exists }
func (f *fakeProc) Group(int) ([]ProcInfo, error) {
	if len(f.seq) == 0 {
		return nil, errors.New("none")
	}
	s := f.seq[f.i]
	if f.i < len(f.seq)-1 {
		f.i++
	}
	return s, nil
}

func TestSelectStatus(t *testing.T) {
	cases := []struct {
		scope, proc bool
		want        Status
	}{
		{true, true, StatusScope}, {true, false, StatusScope},
		{false, true, StatusWatchdog}, {false, false, StatusNone},
	}
	for _, c := range cases {
		if got := SelectStatus(c.scope, c.proc); got != c.want {
			t.Errorf("SelectStatus(%v,%v)=%s want %s", c.scope, c.proc, got, c.want)
		}
	}
	if !StatusNone.NeedsEvent() || StatusWatchdog.NeedsEvent() || StatusScope.NeedsEvent() {
		t.Error("only StatusNone needs an Events entry")
	}
	if StatusNone.Message() != NoCeilingMessage {
		t.Error("none message mismatch")
	}
}

func TestSafeKillTarget(t *testing.T) {
	if SafeKillTarget(0, 500) || SafeKillTarget(1, 500) || SafeKillTarget(500, 500) || SafeKillTarget(-3, 500) {
		t.Error("unsafe target accepted")
	}
	if !SafeKillTarget(700, 500) {
		t.Error("separate group refused")
	}
}

func TestWatchGroupKillsGroupOnly(t *testing.T) {
	f := &fakeProc{exists: true, seq: [][]ProcInfo{
		{{PID: 700, Name: "a", RSSKB: 100 * 1024, HWMKB: 100 * 1024}},
		{{PID: 700, Name: "a", RSSKB: 100 * 1024, HWMKB: 100 * 1024}, {PID: 701, Name: "big", RSSKB: 900 * 1024, HWMKB: 950 * 1024}},
	}}
	var killed []int
	res := WatchGroup(make(chan struct{}), f, 700, 500, 512, time.Millisecond, func(g int) error {
		killed = append(killed, g)
		return nil
	})
	if !res.Killed || len(killed) != 1 || killed[0] != 700 {
		t.Fatalf("killed=%v res=%v", killed, res)
	}
	if res.LargestName != "big" || res.PeakKB < 1000*1024 {
		t.Errorf("peak/largest wrong: %v", res)
	}
}

func TestWatchGroupNoKillUnderCeiling(t *testing.T) {
	f := &fakeProc{exists: true, seq: [][]ProcInfo{{{PID: 700, Name: "a", RSSKB: 1024}}}}
	done := make(chan struct{})
	go func() { time.Sleep(20 * time.Millisecond); close(done) }()
	res := WatchGroup(done, f, 700, 500, 512, time.Millisecond, func(int) error {
		t.Error("kill called")
		return nil
	})
	if res.Killed || res.PeakKB != 1024 || res.LargestName != "a" {
		t.Errorf("res=%v", res)
	}
}

func TestWatchGroupRefusesDaemonGroup(t *testing.T) {
	f := &fakeProc{exists: true, seq: [][]ProcInfo{{{PID: 500, Name: "d", RSSKB: 9999 * 1024}}}}
	res := WatchGroup(make(chan struct{}), f, 500, 500, 512, time.Millisecond, func(int) error {
		t.Error("daemon group must never be killed")
		return nil
	})
	if res.Killed {
		t.Error("daemon group reported killed")
	}
}

func TestShouldKillBoundary(t *testing.T) {
	if ShouldKill(512*1024, 512) || !ShouldKill(512*1024+1, 512) || ShouldKill(1<<40, 0) {
		t.Error("boundary wrong")
	}
}
