package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mdp/qrterminal/v3"
)

// TestMain lets the test binary double as a fake signal-cli subprocess.
// When _FAKE_SIGNAL_CLI_MODE is set the binary acts as the fake and exits immediately;
// otherwise the normal test runner executes.
func TestMain(m *testing.M) {
	if mode := os.Getenv("_FAKE_SIGNAL_CLI_MODE"); mode != "" {
		os.Exit(runFakeSignalCLI(mode))
	}
	os.Exit(m.Run())
}

func runFakeSignalCLI(mode string) int {
	switch mode {
	case "ok_stderr":
		fmt.Fprintln(os.Stderr, "sgnl://linkinguri?uuid=test&pub_key=abc123")
		time.Sleep(20 * time.Millisecond) // simulate waiting for scan
		return 0
	case "ok_stdout":
		fmt.Fprintln(os.Stdout, "sgnl://linkinguri?uuid=test&pub_key=abc123")
		return 0
	case "fail":
		fmt.Fprintln(os.Stderr, "java.lang.RuntimeException: Failed to link device: already registered")
		return 1
	case "no_uri":
		// exits 0 but never outputs a URI
		return 0
	}
	return 1
}

// fakeCmd returns a cmd that re-invokes the test binary as a fake signal-cli.
// -test.run=^$ matches no tests so only TestMain runs (performing the fake role).
func fakeCmd(mode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "_FAKE_SIGNAL_CLI_MODE="+mode)
	return cmd
}

func TestLinkViaCommand_StderrURI(t *testing.T) {
	var got string
	err := linkViaCommand(fakeCmd("ok_stderr"), func(uri string) {
		got = uri
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got, "sgnl://") {
		t.Errorf("expected sgnl:// URI, got %q", got)
	}
}

func TestLinkViaCommand_StdoutURI(t *testing.T) {
	var got string
	err := linkViaCommand(fakeCmd("ok_stdout"), func(uri string) {
		got = uri
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got, "sgnl://") {
		t.Errorf("expected sgnl:// URI, got %q", got)
	}
}

func TestLinkViaCommand_Failure(t *testing.T) {
	called := false
	err := linkViaCommand(fakeCmd("fail"), func(uri string) {
		called = true
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if called {
		t.Error("onQR should not be called when signal-cli fails")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error should include signal-cli diagnostic output, got: %v", err)
	}
}

func TestLinkViaCommand_CalledOnceOnly(t *testing.T) {
	calls := 0
	err := linkViaCommand(fakeCmd("ok_stderr"), func(uri string) {
		calls++
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("onQR called %d times, want exactly 1", calls)
	}
}

func TestLinkViaCommand_QRCodeGeneration(t *testing.T) {
	// Validate that the URI received by onQR actually produces a non-empty QR code.
	var buf bytes.Buffer
	err := linkViaCommand(fakeCmd("ok_stderr"), func(uri string) {
		qrterminal.GenerateHalfBlock(uri, qrterminal.L, &buf)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("QR code generation produced empty output")
	}
	output := buf.String()
	// Half-block QR output contains Unicode block characters
	if !strings.Contains(output, "\u2580") && !strings.Contains(output, "\u2584") && !strings.Contains(output, "\u2588") {
		t.Log("Warning: QR output may not contain expected Unicode block chars (terminal may not support them)")
		t.Logf("QR output length: %d bytes", len(output))
	}
}

func TestLinkViaCommand_NoURINoCallback(t *testing.T) {
	called := false
	err := linkViaCommand(fakeCmd("no_uri"), func(uri string) {
		called = true
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("onQR should not be called when no sgnl:// URI was emitted")
	}
}
