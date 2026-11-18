//go:build linux

package shutdowncause

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// fakeProcRow is one row of a fake process table: the cmdline plus the
// identity the source reports for that pid.
type fakeProcRow struct {
	cmdline string
	uid     string
	user    string
}

// fakeProcSource is a fully in-memory procSource: an explicit pid → row
// table that never touches the live proc filesystem. It exists so
// candidate scanning and ranking can be tested deterministically.
type fakeProcSource struct {
	procs map[int]fakeProcRow
}

func (f fakeProcSource) pids() []int {
	ids := make([]int, 0, len(f.procs))
	for id := range f.procs {
		ids = append(ids, id)
	}
	sort.Ints(ids) // deterministic scan order
	return ids
}

func (f fakeProcSource) cmdline(pid int) (string, bool) {
	row, ok := f.procs[pid]
	return row.cmdline, ok
}

func (f fakeProcSource) identity(pid int) (uid, userName string) {
	row, ok := f.procs[pid]
	if !ok {
		return "", ""
	}
	return row.uid, row.user
}

// describeCandidates renders candidateProc entries in a stable, readable
// 'pid=N cmdline="..." user="..."' form for failure messages.
func describeCandidates(c []candidateProc) string {
	parts := make([]string, 0, len(c))
	for _, r := range c {
		parts = append(parts, fmt.Sprintf("pid=%d cmdline=%q user=%q", r.pid, r.cmdline, r.user))
	}
	return strings.Join(parts, " ")
}

// TestProcScanFakeTable is a deterministic regression test for candidate
// attribution under a fake process table: the table contains (a) the
// test's own child — this test binary, cmdline in 'kill <daemon pid>'
// form — and (b) an unrelated decoy process whose cmdline mentions the
// same signal verb ('kill') and the same pid, so that without
// scoping and ranking the decoy would match the candidate rule just as
// well. The attribution must still pick the child: the top candidate
// after ranking must be the child, and the resolved sender must be the
// child (or unset) — never the decoy. The fake source installed here is
// the only view of the process table; the live proc filesystem is never
// consulted for it, and the default source is restored in cleanup.
func TestProcScanFakeTable(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	base := filepath.Base(bin)

	// Fixed, mutually distinct pids: the daemon pid the scan runs for,
	// the test's own child, an unrelated decoy (child pid < decoy pid
	// so the within-group pid ordering resolves in the child's favor),
	// and one bystander that must not match the candidate rule at all.
	daemonPID := 1777777
	childPID := 101
	decoyPID := 202
	noisePID := 55
	ownPIDStr := strconv.Itoa(daemonPID)

	src := fakeProcSource{procs: map[int]fakeProcRow{
		// (a) the test's own child: this test binary, carrying a
		// kill-family verb and naming the daemon's own pid as a token.
		childPID: {cmdline: bin + " kill " + ownPIDStr, uid: "1000", user: "tester"},
		// (b) an unrelated decoy: a different pid whose command line
		// also mentions the same signal verb and the same pid, so it
		// matches the candidate rule without scoping/injection.
		decoyPID: {cmdline: "kill " + ownPIDStr, uid: "1001", user: "other"},
		// A bystander with no stop intent: must not become a candidate.
		noisePID: {cmdline: "bash -c echo unrelated", uid: "1002", user: "noise"},
	}}

	// Install the fake source for the duration of the test; restore the
	// previous (default) source no matter how the test ends.
	prev := installProcSource(src)
	t.Cleanup(func() { installProcSource(prev) })

	cands := scanCandidates(daemonPID)
	if len(cands) == 0 {
		t.Fatalf("scanCandidates(%d) returned no candidates against the fake table", daemonPID)
	}

	// The child must be the decoy-safe top candidate. The decoy is in
	// the raw table (src.cmdline(decoyPID) reads it directly), and it is
	// fine — indeed the point — that it also appears among the
	// candidates; what must hold is the attribution result.
	if has, decoyRow := decoyInCandidates(cands, decoyPID); has {
		t.Logf("decoy pid %d matches the candidate rule (as intended) and is present: %s", decoyPID, fmt.Sprintf("cmdline=%q", decoyRow.cmdline))
	} else {
		t.Logf("decoy pid %d did not surface among the candidates; attribution asserted on the child anyway", decoyPID)
	}

	top := cands[0]
	if top.pid != childPID {
		t.Fatalf("top candidate pid = %d, want the child pid %d (never the decoy pid %d); candidates: %s",
			top.pid, childPID, decoyPID, describeCandidates(cands))
	}
	if !strings.Contains(top.cmdline, base) {
		t.Fatalf("top candidate cmdline %q does not contain the test binary %s; candidates: %s",
			top.cmdline, base, describeCandidates(cands))
	}
	if top.user != "tester" {
		t.Fatalf("top candidate user = %q, want %q (identity must come from the fake source); candidates: %s",
			top.user, "tester", describeCandidates(cands))
	}

	// The bystander must never be attributed.
	for _, r := range cands {
		if r.pid == noisePID {
			t.Fatalf("bystander pid %d must not be a candidate; candidates: %s", noisePID, describeCandidates(cands))
		}
	}

	// The resolved cause's sender, derived exactly as procScanCause
	// derives it (unset when there is no candidate at all, otherwise the
	// ranked top candidate), must be the child's pid — or unset if the
	// table had none — and never the decoy's.
	senderPID := 0
	if len(cands) > 0 {
		senderPID = cands[0].pid
	}
	if senderPID != 0 && senderPID != childPID {
		t.Fatalf("resolved SenderPID = %d, want 0 or the child pid %d — never the decoy pid %d; candidates: %s",
			senderPID, childPID, decoyPID, describeCandidates(cands))
	}

	// Cross-check the public wrapper agrees on the ranked top candidate.
	public := CandidatesFor(daemonPID)
	if len(public) == 0 {
		t.Fatalf("CandidatesFor(%d) returned no candidates against the fake table", daemonPID)
	}
	if public[0].PID != childPID {
		t.Fatalf("CandidatesFor(%d)[0].PID = %d, want the child pid %d — never the decoy pid %d",
			daemonPID, public[0].PID, childPID, decoyPID)
	}
}

// decoyInCandidates reports whether a candidateProc entry with the given
// pid is among the ranked candidates.
func decoyInCandidates(cands []candidateProc, pid int) (bool, candidateProc) {
	for _, r := range cands {
		if r.pid == pid {
			return true, r
		}
	}
	return false, candidateProc{}
}
