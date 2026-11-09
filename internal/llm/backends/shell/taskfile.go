package shell

import (
	"fmt"
	"os"
	"path/filepath"
)

// taskFileDirName is the per-project folder that holds shell task files.
// It is gitignored, so task text written here is never committed.
const taskFileDirName = ".datawatch"

// taskFileName returns the file name used to hand a task to a shell script
// for the given session. The name is "shell-task-" followed by the session
// name, where every byte outside A-Z, a-z, 0-9, underscore and hyphen is
// replaced by an underscore, followed by ".txt". Replacement is per byte, so
// a multi-byte UTF-8 character becomes one underscore per byte.
func taskFileName(session string) string {
	b := []byte(session)
	for i, c := range b {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			b[i] = '_'
		}
	}
	return "shell-task-" + string(b) + ".txt"
}

// writeTaskFile writes task to the shell task file for session inside the
// project's .datawatch folder and returns the absolute path of that file.
//
// The folder is created with mode 0o700 if it is missing. The file is opened
// with O_CREATE, O_TRUNC and O_WRONLY at mode 0o600 and then chmod'ed to 0o600,
// so a file that already existed is also owner-only. The task bytes are written
// exactly as given. The .datawatch folder is gitignored, so the file is never
// committed. The command line removes the file after the script exits. If the
// daemon crashes first, the startup sweep removes it on the next start.
func writeTaskFile(projectDir, session, task string) (string, error) {
	dir := filepath.Join(projectDir, taskFileDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("task file: create %s: %w", dir, err)
	}
	path := filepath.Join(dir, taskFileName(session))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("task file: open %s: %w", path, err)
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return "", fmt.Errorf("task file: chmod %s: %w", path, err)
	}
	if _, err := f.WriteString(task); err != nil {
		f.Close()
		return "", fmt.Errorf("task file: write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("task file: close %s: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("task file: resolve %s: %w", path, err)
	}
	return abs, nil
}
