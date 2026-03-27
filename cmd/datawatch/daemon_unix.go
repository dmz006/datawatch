//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// daemonize re-invokes the current binary with --foreground appended, detaches
// it from the terminal (new session via Setsid), and writes its PID to
// ~/.datawatch/daemon.pid.
func daemonize() error {
	cfg, _ := loadConfig()
	dataDir := expandHome(cfg.DataDir)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	logPath := filepath.Join(dataDir, "daemon.log")
	pidPath := filepath.Join(dataDir, "daemon.pid")

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	args := appendForegroundFlag(os.Args[1:])

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	child := exec.Command(exe, args...)
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := child.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start daemon: %w", err)
	}
	logFile.Close()

	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", child.Process.Pid)), 0644); err != nil {
		fmt.Printf("[warn] could not write PID file: %v\n", err)
	}

	fmt.Printf("datawatch daemon started (PID %d)\n", child.Process.Pid)
	fmt.Printf("Logs: tail -f %s\n", logPath)
	fmt.Printf("Stop: datawatch stop\n")
	return nil
}
