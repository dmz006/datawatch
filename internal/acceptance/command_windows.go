//go:build windows

package acceptance

import "os/exec"

func setProcessGroup(*exec.Cmd) {}

func killProcessGroup(*exec.Cmd) {}
