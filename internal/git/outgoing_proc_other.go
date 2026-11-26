//go:build !unix

package git

import "os/exec"

func configureCheckProcessGroup(*exec.Cmd) {}
