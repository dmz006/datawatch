// Reading a project's .claude/settings.json without losing any of it, and
// never the settings that apply to everything a user runs. The reading and
// writing rules are shared with every other settings writer (toolsettings).

package hookinstaller

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dmz006/datawatch/internal/toolsettings"
)

const settingsFile = ".claude/settings.json"

// hookEvents are the events Install registers a hook for.
var hookEvents = []string{"Stop", "PostToolUse", "UserPromptSubmit", "SubagentStop", "SessionStart"}

func notInstalled(format string, args ...any) error {
	return fmt.Errorf("hookinstaller: not installed: "+format, args...)
}

// refuseUserSettings refuses a project folder whose hooks would apply beyond
// the project.
func refuseUserSettings(projectDir string) error {
	if err := toolsettings.RefuseUserClaude(projectDir, filepath.Base(settingsFile)); err != nil {
		return notInstalled("%w", err)
	}
	return nil
}

// readSettings reads the project's settings file in full, or fails. mode is the
// mode to write it with.
func readSettings(path string) (doc map[string]any, mode os.FileMode, err error) {
	doc, mode, err = toolsettings.Read(path)
	if err != nil {
		return nil, 0, notInstalled("%w", err)
	}
	if mode == 0 {
		mode = 0o644
	}
	if v, present := doc["hooks"]; present {
		hooks, ok := v.(map[string]any)
		if !ok {
			return nil, 0, notInstalled("%s: \"hooks\" is not an object, so the file is left as it is", path)
		}
		for _, event := range hookEvents {
			if list, present := hooks[event]; present {
				if _, ok := list.([]any); !ok {
					return nil, 0, notInstalled("%s: \"hooks.%s\" is not a list, so the file is left as it is", path, event)
				}
			}
		}
	}
	return doc, mode, nil
}

// writeSettings replaces the settings file whole.
func writeSettings(path string, doc map[string]any, mode os.FileMode) error {
	if err := toolsettings.Write(path, doc, mode); err != nil {
		return notInstalled("%w", err)
	}
	return nil
}
