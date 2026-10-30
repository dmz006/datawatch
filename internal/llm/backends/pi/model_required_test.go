package pi

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/testhome"
)

// regressionPinnedModel is the exact pinned model the 2026-10-20 22:29
// PRD shape resolved to (autonomous.verification_model /
// autonomous.planning_model = "ollama/qwen3.8-64k:27b"). Every pi launch
// path that names an explicit model must put this value on the command
// line verbatim — never truncated, swapped, or replaced by the harness's
// own host-default model (the "pi default" label).
const regressionPinnedModel = "ollama/qwen3.8-64k:27b"

// truncatedRegressionModel is the wrong model the same PRD shape must
// never resolve to. It is not a substring of the pinned name (the "-64k"
// sits between "qwen3.8" and ":27b"), so a Contains check is a valid
// negative assertion.
const truncatedRegressionModel = "ollama/qwen3.8:27b"

// assertExactModelFlag checks the shell command line: exactly one
// --model flag, its value byte-identical to want, the truncated variant
// absent, and no "pi default" fallback label anywhere.
func assertExactModelFlag(t *testing.T, cmd, want string) {
	t.Helper()
	if n := strings.Count(cmd, " --model "); n != 1 {
		t.Fatalf("command line has %d --model flag(s), want exactly 1: %q", n, cmd)
	}
	wantFlag := " --model '" + shellEscape(want) + "' "
	if !strings.Contains(cmd, wantFlag) {
		t.Fatalf("--model flag value not exactly %q in command line: %q", want, cmd)
	}
	if strings.Contains(cmd, truncatedRegressionModel) {
		t.Fatalf("command line contains the truncated model %q: %q", truncatedRegressionModel, cmd)
	}
	if strings.Contains(cmd, "pi default") {
		t.Fatalf("command line contains the 'pi default' fallback label: %q", cmd)
	}
}

func TestBuildInteractiveCmd_ExplicitModelPinnedExact(t *testing.T) {
	testhome.PrivateTmp(t) // any scratch file this build makes lands in the test's temp dir
	cmd, err := buildInteractiveCmd("/usr/local/bin/pi", regressionPinnedModel, "sess_judge_1", "", "/work", "tmux_judge_1")
	if err != nil {
		t.Fatalf("buildInteractiveCmd: %v", err)
	}
	assertExactModelFlag(t, cmd, regressionPinnedModel)
}

func TestBuildInteractiveCmd_EmptyModelFailsLoudly(t *testing.T) {
	// An empty explicit-model argument is a configuration failure, not a
	// silent "let the harness pick its own default" case.
	testhome.PrivateTmp(t) // boundary pin: the refuse path creates no scratch file
	cmd, err := buildInteractiveCmd("/usr/local/bin/pi", "", "sess_judge_2", "", "/work", "tmux_judge_2")
	if !errors.Is(err, ErrNoModel) {
		t.Fatalf("empty model: err = %v, want ErrNoModel (fail loudly, never fall back to the harness default)", err)
	}
	if cmd != "" {
		t.Fatalf("empty model must not produce a command line: %q", cmd)
	}
}

func TestBuildLaunchCmd_ExplicitModelPinnedExact(t *testing.T) {
	testhome.PrivateTmp(t) // the out/status scratch files are removed with the test's temp dir
	cmd, err := buildLaunchCmd("/usr/local/bin/pi", regressionPinnedModel, "", "/work", "tmux_judge_3")
	if err != nil {
		t.Fatalf("buildLaunchCmd: %v", err)
	}
	assertExactModelFlag(t, cmd, regressionPinnedModel)
}

func TestBuildLaunchCmd_EmptyModelFailsLoudly(t *testing.T) {
	testhome.PrivateTmp(t) // boundary pin: the refuse path creates no scratch file
	cmd, err := buildLaunchCmd("/usr/local/bin/pi", "  ", "", "/work", "tmux_judge_4")
	if !errors.Is(err, ErrNoModel) {
		t.Fatalf("blank model: err = %v, want ErrNoModel", err)
	}
	if cmd != "" {
		t.Fatalf("blank model must not produce a command line: %q", cmd)
	}
}

// forceTempDirUnusable points TMPDIR (read by os.TempDir on Unix) at a
// regular file so os.CreateTemp fails, exercising the scratch-file
// failure branches deterministically.
func forceTempDirUnusable(t *testing.T) {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatalf("set up unreachable tempdir: %v", err)
	}
	t.Setenv("TMPDIR", blocker)
}

// TestBuildLaunchCmd_ScratchFileFailureStaysLoud covers
// buildLaunchCmd's scratch-file failure branch: the fallback command
// still exists and still fails visibly rather than launching pi with a
// missing output-capture path.
func TestBuildLaunchCmd_ScratchFileFailureStaysLoud(t *testing.T) {
	forceTempDirUnusable(t)
	cmd, err := buildLaunchCmd("/usr/local/bin/pi", regressionPinnedModel, "task", "/work", "tmux_judge_5")
	if err != nil {
		t.Fatalf("scratch failure is handled by the fallback command, not an error: %v", err)
	}
	if !strings.Contains(cmd, "DATAWATCH_PI_FAILED") || !strings.Contains(cmd, "exit 1") {
		t.Fatalf("scratch-failure command must fail loudly, got: %q", cmd)
	}
	if strings.Contains(cmd, " --model ") {
		t.Fatalf("scratch-failure fallback must not launch pi at all: %q", cmd)
	}
}

// TestTaskArgExpr_InlinesTextWhenTaskFileUnavailable covers
// taskArgExpr's fallback (writeTaskFile error): the task text is
// shipped inline, single-quoted exactly by shellEscape, with no
// cleanup step.
func TestTaskArgExpr_InlinesTextWhenTaskFileUnavailable(t *testing.T) {
	forceTempDirUnusable(t)
	task := "do 'quoted' work"
	argExpr, cleanup := taskArgExpr("tmux_judge_6", task)
	want := fmt.Sprintf(" '%s'", shellEscape(task))
	if argExpr != want {
		t.Fatalf("taskArgExpr = %q, want exactly %q", argExpr, want)
	}
	if cleanup != "" {
		t.Fatalf("inline task has nothing to clean up, got %q", cleanup)
	}
}

// TestWriteTaskFile_WritesAndCleans covers writeTaskFile's success
// path: the file holds the task text verbatim and is removable by the
// caller.
func TestWriteTaskFile_WritesAndCleans(t *testing.T) {
	task := "line 1\nline 2\n"
	path, err := writeTaskFile("tmux_judge_7", task)
	if err != nil {
		t.Fatalf("writeTaskFile: %v", err)
	}
	t.Cleanup(func() { os.Remove(path) }) //nolint:errcheck
	body, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("reading task file: %v", rerr)
	}
	if string(body) != task {
		t.Fatalf("task file body = %q, want exactly %q", body, task)
	}
}

// TestReserveScratchFile_NamesAndFailure covers reserveScratchFile's
// success path and its CreateTemp error branch.
func TestReserveScratchFile_NamesAndFailure(t *testing.T) {
	testhome.PrivateTmp(t) // the scratch file is removed with the test's temp dir
	if _, err := reserveScratchFile("tmux_judge_8", "out"); err != nil {
		t.Fatalf("reserveScratchFile success path: %v", err)
	}
	forceTempDirUnusable(t)
	if _, err := reserveScratchFile("tmux_judge_8", "status"); err == nil {
		t.Fatal("reserveScratchFile with an unusable tempdir: want an error, got nil")
	}
}
