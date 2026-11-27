//go:build linux

package proclimit

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// refusedTools ask a service outside the command's tree to run work: the
// service manager, or a container engine. What they start is not under the
// command's limit, so a command that asks for one gets a stand-in that runs
// nothing and notes that it was asked.
var refusedTools = []string{"systemd-run", "docker", "docker-compose", "podman", "nerdctl"}

// noEngine is the address of a container engine a command is given: nothing
// listens there.
const noEngine = "unix:///nonexistent/datawatch-no-container-engine"

// askedFile, beside the stand-ins, takes one name for each time one was asked.
const askedFile = "asked"

const refusalScript = `#!/bin/sh
printf '%s\n' "${0##*/}" >>"${0%/*}/` + askedFile + `"
echo "datawatch: ${0##*/} was refused: what it starts would run outside the memory limit of this command" >&2
exit 126
`

// makeRefusals makes a folder with a stand-in for each refused tool that is on
// path, and returns it, or "" when none is. A tool the command could not find
// stays one it cannot find.
func makeRefusals(path string) (dir string, err error) {
	var have []string
	for _, name := range refusedTools {
		for _, d := range filepath.SplitList(path) {
			if st, err := os.Stat(filepath.Join(d, name)); err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0 {
				have = append(have, name)
				break
			}
		}
	}
	if len(have) == 0 {
		return "", nil
	}
	if dir, err = os.MkdirTemp("", "dw-refuse-*"); err != nil {
		return "", err
	}
	for _, name := range have {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(refusalScript), 0o700); err != nil { // #nosec G306 -- a script of this process's own folder, which must run
			_ = os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}

// asked returns the refused tools that were asked for, each once.
func asked(dir string) []string {
	if dir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, askedFile)) // #nosec G304 -- the folder made above
	if err != nil {
		return nil
	}
	var out []string
	for _, name := range strings.Fields(string(data)) {
		if slices.Contains(refusedTools, name) && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func refusedWhy(names []string) string {
	return "refused: the command asked for " + strings.Join(names, ", ") +
		", which would run work outside its memory limit, so it was not run"
}

// ownCgroup is where Linux says which control group this process is in.
const ownCgroup = "/proc/self/cgroup"

// limitScopeOf returns the scope of a command's limit that holds a process
// whose control group data describes, "" when none does.
func limitScopeOf(data string) string {
	for _, line := range strings.Split(data, "\n") {
		group, ok := strings.CutPrefix(line, "0::")
		if !ok {
			continue
		}
		for _, part := range strings.Split(group, "/") {
			if strings.HasPrefix(part, UnitPrefix) && strings.HasSuffix(part, ".scope") {
				return part
			}
		}
	}
	return ""
}
