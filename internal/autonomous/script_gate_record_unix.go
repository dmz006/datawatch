//go:build unix

package autonomous

import (
	"os/exec"
	"syscall"
)

// launchGateProcess starts argv in its own session under the shared wrapper,
// which writes the combined output to outFile and the exit code to exitFile.
// The session keeps the gate running when the daemon exits. The daemon reaps
// the wrapper while it lives; after a restart init does.
func launchGateProcess(argv []string, dir, outFile, exitFile string) (int, error) {
	full := append([]string{"-c", verifyRecordWrapper, "bash", outFile, exitFile}, argv...)
	cmd := exec.Command("bash", full...) // #nosec G204 -- argv built by gateArgv from validated input
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	go func() { _ = cmd.Wait() }()
	return cmd.Process.Pid, nil
}

// killGateGroup ends the whole process group led by pid at the gate's deadline.
func killGateGroup(pid int) {
	if pid > 0 {
		// end-reason-ok: ends a gate check's own OS process group at its deadline, not a datawatch session
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}
