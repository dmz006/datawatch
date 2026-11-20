package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

// Env vars the self re-exec child uses to know it is the crash child and
// where to point the crash output.
const (
	crashChildEnv     = "DW_CRASH_OUTPUT_CHILD"
	crashChildFileEnv = "DW_CRASH_OUTPUT_FILE"
)

// TestCrashOutputNilFile checks that passing nil is accepted (it disables the
// extra crash-output file and restores stderr-only output).
func TestCrashOutputNilFile(t *testing.T) {
	if err := installCrashOutput(nil); err != nil {
		t.Fatalf("installCrashOutput(nil) = %v, want nil", err)
	}
}

// TestCrashOutputTempFile checks that installCrashOutput on a real file
// returns nil and that the file is usable afterwards.
func TestCrashOutputTempFile(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "crash.log"))
	if err != nil {
		t.Fatalf("os.Create: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("close crash file: %v", err)
		}
	}()

	if err := installCrashOutput(f); err != nil {
		t.Fatalf("installCrashOutput = %v, want nil", err)
	}

	// The runtime duplicates the fd, so writing through the original handle
	// after SetCrashOutput returned must still work.
	if _, err := f.WriteString("ok\n"); err != nil {
		t.Fatalf("write after install: %v", err)
	}
}

// TestCrashOutputReExec proves the end-to-end behaviour: when the process
// dies from an unrecovered panic, the runtime's fatal output (goroutine
// dumps) lands in BOTH stderr (fd 2, untouched) and the crash file.
//
// The parent re-executes this test binary as a child (via os.Args[0]); the
// child installs the crash output on a temp file, then lets a goroutine panic
// without a recover. The parent then asserts both streams carry the dump.
func TestCrashOutputReExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self re-exec fatal-error test not run on windows")
	}

	// --- Child mode: install crash output, then crash. ---
	if os.Getenv(crashChildEnv) != "" {
		path := os.Getenv(crashChildFileEnv)
		f, err := os.Create(path)
		if err != nil {
			panic("crash-output child: open crash file: " + err.Error())
		}
		if err := installCrashOutput(f); err != nil {
			panic("crash-output child: installCrashOutput: " + err.Error())
		}
		debug.SetTraceback("crash")
		go func() {
			panic("crash-output re-exec test: intentional unrecovered panic in goroutine")
		}()
		// No recover anywhere: the fatal must take the process down.
		select {}
	}

	// --- Parent mode: run the child and check both outputs. ---
	dir := t.TempDir()
	crashFile := filepath.Join(dir, "daemon-crash.log")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run", "^TestCrashOutputReExec$")
	cmd.Env = append(os.Environ(),
		crashChildEnv+"=1",
		crashChildFileEnv+"="+crashFile,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == context.DeadlineExceeded {
		t.Fatalf("crash child did not die within 20s (stderr tail: %q)", stderr.String()[-min(512, stderr.Len()):])
	}
	// Any other error is the expected non-zero exit from the unrecovered panic.

	stderrOut := stderr.String()
	if !strings.Contains(stderrOut, "goroutine") {
		t.Errorf("child stderr missing goroutine dump (fd 2 was touched?)\ngot (%d bytes):\n%s", len(stderrOut), stderrOut)
	}

	crashOut, err := os.ReadFile(crashFile)
	if err != nil {
		t.Fatalf("crash file not written: %v (stderr tail: %q)", err, stderrOut[-min(512, stderr.Len()):])
	}
	if !bytes.Contains(crashOut, []byte("goroutine")) {
		t.Errorf("crash file missing goroutine dump\ngot (%d bytes):\n%s", len(crashOut), crashOut)
	}
	if !bytes.Contains(crashOut, []byte("crash-output re-exec test: intentional unrecovered panic")) {
		t.Errorf("crash file missing the panic message\ngot:\n%s", crashOut)
	}
}
