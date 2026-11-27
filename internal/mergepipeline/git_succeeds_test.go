package mergepipeline

import "os/exec"

func gitSucceeds(dir string, args ...string) bool {
	return exec.Command("git", append([]string{"-C", dir}, args...)...).Run() == nil
}
