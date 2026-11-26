// Which files in a project are the installer's to write. It writes only its
// own, never through a link, and it makes git ignore the files it creates, in a
// repository now or in a folder that becomes one later.

package hookinstaller

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// scriptMarker is in the head of every script this installer has written.
	scriptMarker = "# datawatch hook event POST — auto-installed"
	// ignoreMarker heads the git ignore files it writes. Each names only the
	// files datawatch created, so nothing of the user's is hidden.
	ignoreMarker = "# datawatch session hooks: files it created, not project work"
	sprintIgnore = ignoreMarker + "\n/post-event.sh\n/.dw-env\n/platform-context.txt\n/.gitignore\n"
	claudeIgnore = ignoreMarker + "\n/settings.json\n/.gitignore\n"

	sprintIgnoreFile = ".claude/sprint/.gitignore"
)

var envLine = regexp.MustCompile(`^(DAEMON_URL|SESSION_ID|TOKEN)=`)

// ownEnvFile reports the content of a .dw-env this installer wrote.
func ownEnvFile(body []byte) bool {
	session := false
	for _, l := range strings.Split(string(body), "\n") {
		if l == "" {
			continue
		}
		if !envLine.MatchString(l) {
			return false
		}
		session = session || strings.HasPrefix(l, "SESSION_ID=")
	}
	return session
}

// regularFile reads path if it is an ordinary file. found is false when there
// is nothing there; anything else there, a link included, is an error.
func regularFile(path string) (body []byte, found bool, err error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, notInstalled("%w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, notInstalled("%s is a link or not an ordinary file", path)
	}
	body, err = os.ReadFile(path)
	if err != nil {
		return nil, false, notInstalled("%w", err)
	}
	return body, true, nil
}

// refuseOthersFiles refuses a project where a file the installer would write
// is a link, or was not written by it. It changes nothing.
func refuseOthersFiles(projectDir string) error {
	sprint := filepath.Join(projectDir, ".claude", "sprint")
	if info, err := os.Lstat(sprint); err == nil && !info.IsDir() {
		return notInstalled("%s is a link or not a folder", sprint)
	} else if err != nil && !os.IsNotExist(err) {
		return notInstalled("%w", err)
	}
	read := map[string][]byte{}
	for _, name := range []string{"post-event.sh", ".dw-env", "platform-context.txt", ".gitignore"} {
		body, found, err := regularFile(filepath.Join(sprint, name))
		if err != nil {
			return err
		}
		if found {
			read[name] = body
		}
	}
	notOurs := func(name string) error {
		return notInstalled("%s was not written by datawatch, so it is left as it is", filepath.Join(sprint, name))
	}
	if body, found := read[".gitignore"]; found && !strings.HasPrefix(string(body), ignoreMarker) {
		return notOurs(".gitignore")
	}
	if script, found := read["post-event.sh"]; found {
		if !strings.Contains(string(script), scriptMarker) {
			return notOurs("post-event.sh")
		}
		return nil // the files beside datawatch's script are its own
	}
	if _, found := read["platform-context.txt"]; found {
		return notOurs("platform-context.txt")
	}
	if env, found := read[".dw-env"]; found && !ownEnvFile(env) {
		return notOurs(".dw-env")
	}
	return nil
}

// writeOwn replaces path whole. It puts a new file in place, so it never
// writes into whatever a link at path points to.
func writeOwn(path string, body []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dw-hook-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // gone after the rename
	_, werr := tmp.Write(body)
	if err := tmp.Chmod(mode); werr == nil {
		werr = err
	}
	if err := tmp.Close(); werr == nil {
		werr = err
	}
	if werr != nil {
		return werr
	}
	return os.Rename(tmp.Name(), path)
}

// ignoreNewSettings makes git ignore a settings.json the installer is about to
// create. A .gitignore already there is the user's and is not touched.
func ignoreNewSettings(claudeDir string) error {
	path := filepath.Join(claudeDir, ".gitignore")
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return nil
	}
	return writeOwn(path, []byte(claudeIgnore), 0o644)
}
