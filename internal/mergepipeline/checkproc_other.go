//go:build !unix

package mergepipeline

import "os/exec"

// ownProcessGroup has no process groups to use here: a timeout stops the
// command itself.
func ownProcessGroup(*exec.Cmd) func() { return func() {} }
