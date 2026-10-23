//go:build linux

package shutdowncause

import (
	"encoding/binary"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestParentChain(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("parent chain requires /proc")
	}
	chain := buildParentChain()
	if len(chain) == 0 || len(chain) > maxChainDepth {
		t.Fatalf("chain length %d not in [1, %d]", len(chain), maxChainDepth)
	}
	if chain[0].PID != os.Getpid() {
		t.Fatalf("first entry pid = %d, want %d", chain[0].PID, os.Getpid())
	}
	for i, p := range chain {
		if p.PID <= 0 {
			t.Fatalf("chain[%d] has bad pid %d", i, p.PID)
		}
	}
}

func TestProcIdentity(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires /proc")
	}
	uid, _ := procIdentity(os.Getpid())
	if uid == "" {
		t.Fatal("expected a numeric uid for own process")
	}
	uidBad, _ := procIdentity(999999999)
	if uidBad != "" {
		t.Fatalf("expected empty uid for missing pid, got %q", uidBad)
	}
}

func TestScanCandidates(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires /proc")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	ownPID := os.Getpid()

	// Positive: a live process that looks like the systemctl stop of datawatch.
	// service-scope-ok: the cmdline is faked with `exec -a` on a child of this test; no service manager is ever invoked.
	pos := exec.Command(bash, "-c", `exec -a "systemctl --user stop datawatch" sleep 30`) // service-scope-ok fake cmdline child of this test, never touches a unit
	if err := pos.Start(); err != nil {
		t.Fatalf("start positive: %v", err)
	}
	defer func() {
		_ = pos.Process.Kill()
		_ = pos.Wait()
	}()

	// Negative controls: plain sleep, and a datawatch subcommand without a
	// stop intent, are not service-control stop invocations.
	neg := exec.Command("sleep", "30")
	if err := neg.Start(); err != nil {
		t.Fatalf("start negative: %v", err)
	}
	defer func() {
		_ = neg.Process.Kill()
		_ = neg.Wait()
	}()
	neg2 := exec.Command(bash, "-c", `exec -a "datawatch serve" sleep 30`)
	if err := neg2.Start(); err != nil {
		t.Fatalf("start negative 2: %v", err)
	}
	defer func() {
		_ = neg2.Process.Kill()
		_ = neg2.Wait()
	}()

	time.Sleep(150 * time.Millisecond) // let the children settle

	cands := scanCandidates(ownPID)
	var found *candidateProc
	for i := range cands {
		if cands[i].pid == pos.Process.Pid {
			found = &cands[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected candidate pid %d among %v", pos.Process.Pid, cands)
	}
	if !strings.Contains(found.cmdline, "datawatch") {
		t.Fatalf("candidate cmdline %q does not name the unit", found.cmdline)
	}
	if found.uid == "" {
		t.Fatalf("candidate uid empty: %+v", found)
	}
	for _, c := range cands {
		if c.pid == neg.Process.Pid {
			t.Fatalf("false positive on plain sleep pid %d", neg.Process.Pid)
		}
		if c.pid == neg2.Process.Pid {
			t.Fatalf("false positive on 'datawatch serve' pid %d", neg2.Process.Pid)
		}
		if c.pid == ownPID {
			t.Fatal("self must never be a candidate")
		}
	}
}

func TestSystemdJobGraceful(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemctl probing is Linux-only")
	}
	job, ok := systemdStopJob()
	if ok {
		if job == "" || strings.EqualFold(job, "n/a") {
			t.Fatalf("ok=true but job %q is unusable", job)
		}
	}
	// ok=false is acceptable: systemctl may be absent or report no job.
}

func TestSiginfoCause(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux siginfo layout only")
	}
	const senderPID = 777
	buf := make([]byte, int(unsafe.Sizeof(linuxSiginfo{})))
	binary.LittleEndian.PutUint32(buf[0:4], uint32(syscall.SIGTERM)) // Signo
	binary.LittleEndian.PutUint32(buf[12:16], senderPID)             // SiPid
	buid := os.Getuid()
	binary.LittleEndian.PutUint32(buf[16:20], uint32(buid)) // SiUid

	names := map[int]os.Signal{int(syscall.SIGTERM): syscall.SIGTERM}
	cause, sig, ok := siginfoCause(buf, names)
	if !ok {
		t.Fatal("siginfoCause: !ok on valid siginfo")
	}
	if sig != syscall.SIGTERM {
		t.Fatalf("signal = %v, want SIGTERM", sig)
	}
	if cause.Source != SourceSignalInfo {
		t.Fatalf("source = %q", cause.Source)
	}
	if cause.SenderPID != senderPID {
		t.Fatalf("sender pid = %d, want %d", cause.SenderPID, senderPID)
	}
	if cause.SenderUID != strconv.Itoa(buid) {
		t.Fatalf("sender uid = %q, want %d (from /etc/passwd lookup)", cause.SenderUID, buid)
	}
	if cause.SenderUser == "" {
		t.Fatal("expected a resolved sender user name")
	}
	if cause.Signal != syscall.SIGTERM.String() {
		t.Fatalf("signal name = %q", cause.Signal)
	}
	if len(cause.ParentChain) == 0 {
		t.Fatal("expected parent chain on linux siginfo path")
	}
	if _, _, ok := siginfoCause(buf[:10], names); ok {
		t.Fatal("short buffer should be rejected")
	}
}

func TestInstallBasics(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("signalfd requires Linux")
	}
	ch, err := Install(syscall.SIGINT)
	if err != nil {
		t.Skipf("signalfd setup failed: %v", err)
	}
	if ch == nil {
		t.Fatal("Install returned nil channel")
	}
	// Unmask immediately so normal test-run signal behavior is preserved.
	set := unix.Sigset_t{}
	set.Val[syscall.SIGINT/64] |= 1 << (uint32(syscall.SIGINT) % 64)
	_ = unix.PthreadSigmask(unix.SIG_UNBLOCK, &set, nil)
}
