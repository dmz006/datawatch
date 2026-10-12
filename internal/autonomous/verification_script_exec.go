// BL416 Phase 3 — guardrail execution path (plan:
// docs/plans/2026-10-12-tooled-deterministic-guardrails.md, Decisions
// 1, 5 and 6). Phase 2 (v9.0.37) taught tooled planners to write a real
// verification script per task/story and record its path in
// VerificationScript; this file is the runner that executes it so the
// guardrail can rely on real output instead of judgment alone.
//
// Reuse: same exec.Command + CombinedOutput + *exec.ExitError pattern
// as pipeline.RunTests, but not RunTests itself — its strings.Fields
// split is wrong for this fixed `bash <path>` invocation.
//
// Trust boundary: the script path comes from LLM-written PRD state, so
// it is validated to resolve inside projectDir before anything runs.
package autonomous

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxVerificationScriptOutput caps the combined output carried into
// GuardrailInvocation.ScriptOutput.
const maxVerificationScriptOutput = 8 * 1024

// runVerificationScript executes scriptRelPath (relative to projectDir)
// with bash, cwd = projectDir. A script's own non-zero exit is reported
// via exitCode with err == nil; err is non-nil (exitCode -1) only for an
// unsafe path or a failure to run bash at all.
func runVerificationScript(scriptRelPath, projectDir string) (output string, exitCode int, err error) {
	if scriptRelPath == "" {
		return "", -1, errors.New("verification script path is empty")
	}
	if filepath.IsAbs(scriptRelPath) {
		return "", -1, fmt.Errorf("verification script path %q must be relative to the project dir", scriptRelPath)
	}
	scriptPath := filepath.Clean(filepath.Join(projectDir, scriptRelPath))
	rel, relErr := filepath.Rel(filepath.Clean(projectDir), scriptPath)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", -1, fmt.Errorf("verification script path %q escapes the project dir", scriptRelPath)
	}

	cmd := exec.Command("bash", scriptPath) // #nosec G204 -- scriptPath validated above to resolve inside projectDir
	cmd.Dir = projectDir
	out, runErr := cmd.CombinedOutput()
	output = truncateScriptOutput(out)
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return output, exitErr.ExitCode(), nil
		}
		return output, -1, runErr
	}
	return output, 0, nil
}

// truncateScriptOutput keeps the LAST maxVerificationScriptOutput bytes
// (failures are usually at the end) behind a one-line truncation note.
func truncateScriptOutput(out []byte) string {
	if len(out) <= maxVerificationScriptOutput {
		return string(out)
	}
	return fmt.Sprintf("[truncated: showing last %d of %d bytes]\n", maxVerificationScriptOutput, len(out)) +
		string(out[len(out)-maxVerificationScriptOutput:])
}
