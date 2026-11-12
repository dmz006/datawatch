package shutdowncause

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	MarkAPIInitiated("") // ensure a clean slate before any test
	// Re-exec helper role: TestSignalChildProcScan starts a copy of this
	// test binary; it signals its parent (this process) by PID. Exiting
	// here keeps the helper from running the real test suite.
	if os.Getenv("DW_SHUTDOWNCAUSE_ROLE") == signalChildRole {
		os.Exit(runSignalParentRole())
	}
	os.Exit(m.Run())
}

func sampleCause() Cause {
	tm, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")
	return Cause{
		Signal:         "SIGTERM",
		SenderPID:      4242,
		SenderUID:      "0",
		SenderUser:     "root",
		Cmdline:        "systemctl --user stop datawatch", // service-scope-ok fixture string: expected value in Cause parsing tests, never executed
		ParentChain:    []ProcInfo{{PID: 1, Cmdline: "/sbin/init", User: "root"}},
		Source:         SourceProcScan,
		ServiceStopJob: "datawatch: stop",
		Time:           tm,
	}
}

func TestJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := sampleCause()
	if err := Write(dir, original); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, ok := ReadLast(dir)
	if !ok {
		t.Fatal("ReadLast: not ok")
	}
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, original)
	}
}

func TestReadLastMissingAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadLast(dir); ok {
		t.Fatal("expected missing file to return !ok")
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	if _, ok := ReadLast(dir); ok {
		t.Fatal("expected corrupt file to return !ok")
	}
}

func TestWriteAtomicAndPermissions(t *testing.T) {
	dir := t.TempDir()
	c1 := sampleCause()
	c1.SenderPID = 1
	if err := Write(dir, c1); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	c2 := sampleCause()
	c2.SenderPID = 2
	if err := Write(dir, c2); err != nil {
		t.Fatalf("Write 2: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != FileName {
		t.Fatalf("expected exactly one file %q, got %v", FileName, entries)
	}
	st, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := st.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected mode 0600, got %v", mode)
	}
	// The rename must have fully replaced the previous contents.
	got, ok := ReadLast(dir)
	if !ok || !reflect.DeepEqual(got, c2) {
		t.Fatalf("after second Write, got %+v ok=%v, want c2", got, ok)
	}
}

func TestWriteConcurrent(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := sampleCause()
			c.SenderPID = i
			if err := Write(dir, c); err != nil {
				t.Errorf("concurrent Write %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != FileName {
		t.Fatalf("expected one file, got %v", entries)
	}
	got, ok := ReadLast(dir)
	if !ok {
		t.Fatal("ReadLast: !ok")
	}
	if got.SenderPID < 0 || got.SenderPID > 7 {
		t.Fatalf("concurrent write left incoherent state: %+v", got)
	}
}

func TestString(t *testing.T) {
	tm, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")
	c := Cause{
		Signal:         "SIGTERM",
		SenderPID:      4242,
		SenderUID:      "0",
		SenderUser:     "root",
		Cmdline:        "systemctl --user stop datawatch", // service-scope-ok fixture string: expected-value test data, never executed
		Source:         SourceProcScan,
		ServiceStopJob: "datawatch: stop",
		Time:           tm,
		Via:            "procscan: 2 candidates",
	}
	// service-scope-ok fixture string: expected String() output, never executed
	want := `shutdown: signal=SIGTERM source=procscan sender_pid=4242 sender_user=root cmdline="systemctl --user stop datawatch" service_stop_job=datawatch: stop via="procscan: 2 candidates" at 2006-01-02T15:04:05Z` // service-scope-ok
	if got := c.String(); got != want {
		t.Fatalf("String() = %q\nwant        %q", got, want)
	}
	if strings.Contains(c.String(), "\n") {
		t.Fatal("String() must be a single line")
	}

	// Minimal cause: only the known fields appear.
	min := Cause{Signal: "SIGTERM", Source: SourceUnknown, Time: tm}
	wantMin := "shutdown: signal=SIGTERM source=unknown at 2006-01-02T15:04:05Z"
	if got := min.String(); got != wantMin {
		t.Fatalf("minimal String() = %q, want %q", got, wantMin)
	}
}

func TestMarkAPIInitiated(t *testing.T) {
	t.Cleanup(func() { MarkAPIInitiated("") })

	MarkAPIInitiated("restart request via daemon API")
	c := Capture(os.Interrupt)
	if c.Source != SourceAPIRestart {
		t.Fatalf("restart via source = %q, want %q", c.Source, SourceAPIRestart)
	}
	if c.Via != "restart request via daemon API" {
		t.Fatalf("via = %q", c.Via)
	}
	if c.SenderPID != 0 || c.Cmdline != "" || len(c.ParentChain) != 0 || c.ServiceStopJob != "" {
		t.Fatalf("API-initiated cause must not carry fabricated fields: %+v", c)
	}

	MarkAPIInitiated("stop request via daemon API")
	c = Capture(syscall.SIGTERM)
	if c.Source != SourceAPIStop {
		t.Fatalf("stop via source = %q, want %q", c.Source, SourceAPIStop)
	}

	MarkAPIInitiated("") // reset; capture goes back to platform detection
	c = Capture(syscall.SIGTERM)
	if strings.HasPrefix(c.Source, "api-") {
		t.Fatalf("after reset, source = %q, want non-api source", c.Source)
	}
}

func TestCaptureFillsSignalAndTime(t *testing.T) {
	t.Cleanup(func() { MarkAPIInitiated("") })
	MarkAPIInitiated("stop probe")
	c := Capture(nil)
	if c.Signal != "" {
		t.Fatalf("nil signal should leave Signal empty, got %q", c.Signal)
	}
	if c.Time.IsZero() {
		t.Fatal("Time must always be set")
	}
}

func TestCaptureFallback(t *testing.T) {
	c := Capture(os.Interrupt)
	if c.Time.IsZero() {
		t.Fatal("Time must be set")
	}
	if runtime.GOOS != "linux" {
		if c.Source != SourceUnknown {
			t.Fatalf("stub source = %q, want %q", c.Source, SourceUnknown)
		}
		return
	}
	if c.Source != SourceProcScan && c.Source != SourceServiceManager {
		t.Fatalf("fallback source = %q, want procscan or service-manager", c.Source)
	}
	if len(c.ParentChain) == 0 {
		t.Fatal("expected a non-empty parent chain")
	}
}
