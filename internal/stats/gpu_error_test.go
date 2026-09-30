package stats

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeNvidiaSMI writes an executable "nvidia-smi" script into a fresh temp
// directory that always prints stderrMsg to stderr and exits non-zero
// (stdout is unused), then points PATH at only that directory so
// exec.LookPath finds nothing else — isolating the test from whatever the
// real host does or doesn't have installed. Returns the dir for cleanup via
// t.Setenv's automatic restore.
func fakeNvidiaSMI(t *testing.T, stderrMsg string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake binary needs a POSIX shell")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\necho " + shQuote(stderrMsg) + " >&2\nexit 1\n"
	path := filepath.Join(dir, "nvidia-smi")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func shQuote(s string) string {
	return "'" + s + "'"
}

func TestReadGPU_NvidiaSMIMissing_NoErrorNoData(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty dir: nvidia-smi not found
	c := NewCollector(t.TempDir())
	var s SystemStats
	c.readGPU(&s)
	if s.GPUError != "" || s.GPUName != "" {
		t.Fatalf("no nvidia-smi at all must mean no error and no data, got GPUError=%q GPUName=%q", s.GPUError, s.GPUName)
	}
}

func TestReadGPU_NvidiaSMIFails_SurfacesRealReason(t *testing.T) {
	fakeNvidiaSMI(t, "Failed to initialize NVML: Driver/library version mismatch")
	c := NewCollector(t.TempDir())
	var s SystemStats
	c.readGPU(&s)
	if s.GPUError == "" {
		t.Fatal("nvidia-smi present but failing must set GPUError")
	}
	if s.GPUError != "Failed to initialize NVML: Driver/library version mismatch" {
		t.Fatalf("GPUError should be nvidia-smi's own stderr text, got %q", s.GPUError)
	}
	if s.GPUName != "" {
		t.Fatalf("a failed probe must not report stale/fabricated GPU data, got %q", s.GPUName)
	}
}

func TestReadGPU_NvidiaSMISucceeds_ParsesDataAndClearsError(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'NVIDIA RTX 5090, 45, 12, 4096, 32768'\n"
	if err := os.WriteFile(filepath.Join(dir, "nvidia-smi"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	c := NewCollector(t.TempDir())
	var s SystemStats
	c.readGPU(&s)
	if s.GPUError != "" {
		t.Fatalf("a successful poll must not set GPUError, got %q", s.GPUError)
	}
	if s.GPUName != "NVIDIA RTX 5090" || s.GPUUtilPct != 12 {
		t.Fatalf("GPU data not parsed correctly: %+v", s)
	}
}
