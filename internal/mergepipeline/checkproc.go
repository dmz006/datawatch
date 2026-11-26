package mergepipeline

import (
	"bytes"
	"io"
	"os"
	"os/exec"
)

// checkOutputBytes is how much of the end of a check's output is kept.
const checkOutputBytes = 1 << 20

// runContained runs cmd so that nothing it starts outlives it. Its output
// goes to a file, not a pipe, so the run never waits on a process the command
// left behind; that process is stopped when the command ends.
func runContained(cmd *exec.Cmd, out *bytes.Buffer) error {
	f, err := os.CreateTemp("", "dw-merge-check-*.log")
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}()
	cmd.Stdout, cmd.Stderr = f, f
	stop := ownProcessGroup(cmd)
	err = cmd.Run()
	stop()
	if size, serr := f.Seek(0, io.SeekEnd); serr == nil {
		from := size - checkOutputBytes
		if from < 0 {
			from = 0
		}
		if _, serr = f.Seek(from, io.SeekStart); serr == nil {
			_, _ = io.Copy(out, f)
		}
	}
	return err
}
