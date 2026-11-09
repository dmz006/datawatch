package autonomous

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func useRunRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "verify-runs")
	SetVerificationRunRoot(root)
	t.Cleanup(func() { SetVerificationRunRoot("") })
	return root
}

func readOnlyRecord(t *testing.T, root string) (verifyRunRecord, string) {
	t.Helper()
	ents, err := os.ReadDir(root)
	if err != nil || len(ents) != 1 {
		t.Fatalf("want one run dir, got %v err=%v", ents, err)
	}
	d := filepath.Join(root, ents[0].Name())
	b, err := os.ReadFile(filepath.Join(d, verifyRecordFile))
	if err != nil {
		t.Fatal(err)
	}
	var rec verifyRunRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	return rec, d
}

func TestVerificationScriptRecordHasOutputAndExitCode(t *testing.T) {
	root := useRunRoot(t)
	dir := t.TempDir()
	writeScript(t, dir, "ok.sh", "echo hello-record\n")
	out, code, err := runVerificationScript("ok.sh", dir)
	if err != nil || code != 0 || !strings.Contains(out, "hello-record") {
		t.Fatalf("out=%q code=%d err=%v", out, code, err)
	}
	rec, d := readOnlyRecord(t, root)
	if rec.ExitCode == nil || *rec.ExitCode != 0 || rec.PID <= 0 || rec.StartedAt.IsZero() || !strings.HasSuffix(rec.ScriptPath, "ok.sh") {
		t.Fatalf("bad record %+v", rec)
	}
	b, _ := os.ReadFile(filepath.Join(d, verifyOutputFile))
	if !strings.Contains(string(b), "hello-record") {
		t.Fatalf("output file %q", b)
	}
	if c, _ := os.ReadFile(filepath.Join(d, verifyExitFile)); strings.TrimSpace(string(c)) != "0" {
		t.Fatalf("exit file %q", c)
	}
}

func TestVerificationScriptRecordNonZeroExit(t *testing.T) {
	root := useRunRoot(t)
	dir := t.TempDir()
	writeScript(t, dir, "bad.sh", "echo nope; exit 7\n")
	out, code, err := runVerificationScript("bad.sh", dir)
	if err != nil || code != 7 || !strings.Contains(out, "nope") {
		t.Fatalf("out=%q code=%d err=%v", out, code, err)
	}
	rec, _ := readOnlyRecord(t, root)
	if rec.ExitCode == nil || *rec.ExitCode != 7 {
		t.Fatalf("bad record %+v", rec)
	}
}

func TestVerificationScriptRecordTimeoutKillsProcessGroup(t *testing.T) {
	root := useRunRoot(t)
	old := verificationScriptTimeout
	verificationScriptTimeout = 500 * time.Millisecond
	t.Cleanup(func() { verificationScriptTimeout = old })
	dir := t.TempDir()
	pidf := filepath.Join(dir, "child.pid")
	writeScript(t, dir, "hang.sh", "sleep 300 &\necho $! > "+pidf+"\nwait\n")
	start := time.Now()
	_, code, err := runVerificationScript("hang.sh", dir)
	if err == nil || code != -1 {
		t.Fatalf("code=%d err=%v, want timeout error", code, err)
	}
	if time.Since(start) > 20*time.Second {
		t.Fatal("timeout did not end the run")
	}
	rec, _ := readOnlyRecord(t, root)
	if !rec.TimedOut {
		t.Fatalf("record not marked timed out: %+v", rec)
	}
	b, _ := os.ReadFile(pidf)
	pid := strings.TrimSpace(string(b))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, serr := os.Stat("/proc/" + pid); os.IsNotExist(serr) {
			return
		}
		if st, _ := os.ReadFile("/proc/" + pid + "/stat"); strings.Contains(string(st), ") Z ") {
			return // killed, awaiting reaping
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("background child %s survived the timeout", pid)
}

func TestVerificationScriptRecordKeepsOutputCap(t *testing.T) {
	useRunRoot(t)
	dir := t.TempDir()
	writeScript(t, dir, "big.sh", "for i in $(seq 1 2000); do echo 0123456789; done; echo THE-END\n")
	out, code, err := runVerificationScript("big.sh", dir)
	if err != nil || code != 0 || !strings.HasPrefix(out, "[truncated: showing last 8192 of ") || !strings.HasSuffix(out, "THE-END\n") {
		t.Fatalf("code=%d err=%v out=%q", code, err, out[:60])
	}
}
