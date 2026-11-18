package mergeshadow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeLint builds options with a fake version reader (versions by binary path)
// and a fake installer that writes the binary and records its version.
func fakeLint(t *testing.T, versions *sync.Map, calls *int32, installErr error) LintToolOptions {
	t.Helper()
	return LintToolOptions{
		LookPath: func(string) (string, error) { return "/host/bin/golangci-lint", nil },
		ReadVersion: func(_ context.Context, bin string) (string, error) {
			v, ok := versions.Load(bin)
			if !ok {
				return "", errors.New("no such binary")
			}
			return "golangci-lint has version " + v.(string), nil
		},
		Install: func(_ context.Context, toolsDir, version string) error {
			atomic.AddInt32(calls, 1)
			if installErr != nil {
				return installErr
			}
			bin := filepath.Join(toolsDir, "golangci-lint")
			if err := os.WriteFile(bin, []byte("x"), 0o600); err != nil {
				return err
			}
			versions.Store(bin, version)
			return nil
		},
	}
}

func TestResolveLintToolNewerHostUsesToolsFolder(t *testing.T) {
	data := t.TempDir()
	var versions sync.Map
	versions.Store("/host/bin/golangci-lint", "2.20.0")
	var calls int32
	tool, err := ResolveLintTool(context.Background(), data, "2.12.2", fakeLint(t, &versions, &calls, nil))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(data, "tools", "golangci-lint", "2.12.2", "golangci-lint")
	if tool.Path != want || tool.Source != LintSourceInstalled {
		t.Fatalf("tool %+v, want %s installed", tool, want)
	}
	res := applyResolvedLint(GateResult{Name: "lint", Status: GatePass}, "2.12.2", tool, nil)
	if res.Status != GatePass || len(res.Findings) != 0 {
		t.Fatalf("status %q findings %+v, want pass with none", res.Status, res.Findings)
	}
	for _, s := range []string{"2.12.2", want, LintSourceInstalled} {
		if !strings.Contains(res.Detail, s) {
			t.Errorf("detail %q lacks %q", res.Detail, s)
		}
	}
	// A second resolve finds it in the tools folder without installing.
	tool, err = ResolveLintTool(context.Background(), data, "2.12.2", fakeLint(t, &versions, &calls, nil))
	if err != nil || tool.Source != LintSourceToolsDir || calls != 1 {
		t.Fatalf("second resolve %+v err %v calls %d", tool, err, calls)
	}
}

func TestResolveLintToolMatchingHostNoInstall(t *testing.T) {
	var versions sync.Map
	versions.Store("/host/bin/golangci-lint", "v2.12.2")
	var calls int32
	tool, err := ResolveLintTool(context.Background(), t.TempDir(), "2.12.2", fakeLint(t, &versions, &calls, nil))
	if err != nil || tool.Source != LintSourcePath || tool.Path != "/host/bin/golangci-lint" || calls != 0 {
		t.Fatalf("tool %+v err %v calls %d", tool, err, calls)
	}
}

func TestResolveLintToolInstallFailureIsUnavailable(t *testing.T) {
	var versions sync.Map
	var calls int32
	_, err := ResolveLintTool(context.Background(), t.TempDir(), "2.12.2",
		fakeLint(t, &versions, &calls, errors.New("network down")))
	var te *LintToolError
	if !errors.As(err, &te) {
		t.Fatalf("err %v, want *LintToolError", err)
	}
	res := applyResolvedLint(GateResult{Name: "lint", Status: GatePass}, "2.12.2", LintTool{}, err)
	if res.Status != GateFail || len(res.Findings) != 1 ||
		res.Findings[0].Code != LintToolUnavailableCode || !res.Findings[0].Blocking ||
		!strings.Contains(res.Findings[0].Message, "network down") {
		t.Fatalf("status %q findings %+v", res.Status, res.Findings)
	}
}

func TestResolveLintToolConcurrentInstallsOnce(t *testing.T) {
	data := t.TempDir()
	var versions sync.Map
	var calls int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ResolveLintTool(context.Background(), data, "2.12.2", fakeLint(t, &versions, &calls, nil)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("installer called %d times, want 1", calls)
	}
}

func TestResolveLintToolNoPinReturnsPathBinary(t *testing.T) {
	var versions sync.Map
	versions.Store("/host/bin/golangci-lint", "2.20.0")
	var calls int32
	tool, err := ResolveLintTool(context.Background(), t.TempDir(), "", fakeLint(t, &versions, &calls, nil))
	if err != nil || tool.Path != "/host/bin/golangci-lint" || calls != 0 {
		t.Fatalf("tool %+v err %v calls %d", tool, err, calls)
	}
}
