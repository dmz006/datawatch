// Reading and writing a project's .claude/settings.json without losing any of
// it, and never the settings that apply to everything a user runs.

package hookinstaller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
)

// hookEvents are the events Install registers a hook for.
var hookEvents = []string{"Stop", "PostToolUse", "UserPromptSubmit", "SubagentStop", "SessionStart"}

func notInstalled(format string, args ...any) error {
	return fmt.Errorf("hookinstaller: not installed: "+format, args...)
}

// resolved is path with symlinks followed as far as it exists.
func resolved(path string) string {
	path = filepath.Clean(path)
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(resolved(parent), filepath.Base(path))
}

func samePlace(a, b string) bool {
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	if aerr == nil && berr == nil {
		return os.SameFile(ai, bi)
	}
	return resolved(a) == resolved(b)
}

// refuseUserSettings refuses a project folder whose hooks would apply beyond
// the project: a home folder, a .claude folder that is a user's own, or a
// .claude folder or settings file that is a link to somewhere else.
func refuseUserSettings(projectDir string) error {
	var homes []string
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		homes = append(homes, h)
	}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		homes = append(homes, u.HomeDir)
	}
	userDirs := []string{}
	for _, h := range homes {
		if samePlace(projectDir, h) {
			return notInstalled("the project folder is a home folder, whose .claude settings apply to everything the user runs")
		}
		userDirs = append(userDirs, filepath.Join(h, ".claude"))
	}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		userDirs = append(userDirs, d)
	}
	claudeDir := filepath.Join(projectDir, ".claude")
	settings := filepath.Join(projectDir, settingsFile)
	for _, p := range []string{claudeDir, settings} {
		if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return notInstalled("%s is a link, and what it points to may not belong to this project", p)
		}
	}
	for _, d := range userDirs {
		if samePlace(claudeDir, d) || samePlace(settings, filepath.Join(d, "settings.json")) {
			return notInstalled("%s holds user-level settings", claudeDir)
		}
	}
	return nil
}

// duplicateKey returns a key an object holds twice, or "". Reading such a file
// keeps one of the two, so writing it back would drop the other.
func duplicateKey(raw []byte) string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var walk func() string
	walk = func() string {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		switch tok {
		case json.Delim('{'):
			seen := map[string]bool{}
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return ""
				}
				key, _ := k.(string)
				if seen[key] {
					return key
				}
				seen[key] = true
				if d := walk(); d != "" {
					return d
				}
			}
			_, _ = dec.Token()
		case json.Delim('['):
			for dec.More() {
				if d := walk(); d != "" {
					return d
				}
			}
			_, _ = dec.Token()
		}
		return ""
	}
	return walk()
}

// readSettings reads the project's settings file in full, or fails: a file that
// cannot be read and understood to the last byte is never written. A missing or
// empty file gives an empty document. mode is the mode to write it with.
func readSettings(path string) (doc map[string]any, mode os.FileMode, err error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return map[string]any{}, 0o644, nil
	}
	if err != nil {
		return nil, 0, notInstalled("%s cannot be read: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, notInstalled("%s is not an ordinary file", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, notInstalled("%s cannot be read: %w", path, err)
	}
	mode = info.Mode().Perm()
	if len(raw) == 0 {
		return map[string]any{}, mode, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // numbers are written back as they are
	if err := dec.Decode(&doc); err != nil {
		return nil, 0, notInstalled("%s cannot be parsed, so it is left as it is: %w", path, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, 0, notInstalled("%s has text after its JSON object, so it is left as it is", path)
	}
	if doc == nil {
		return nil, 0, notInstalled("%s is not a JSON object, so it is left as it is", path)
	}
	if key := duplicateKey(raw); key != "" {
		return nil, 0, notInstalled("%s holds the key %q twice, so it is left as it is", path, key)
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
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return notInstalled("%w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dw-settings-*")
	if err != nil {
		return notInstalled("%w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // gone after the rename
	_, werr := tmp.Write(body.Bytes())
	if err := tmp.Chmod(mode); werr == nil {
		werr = err
	}
	if err := tmp.Sync(); werr == nil {
		werr = err
	}
	if err := tmp.Close(); werr == nil {
		werr = err
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		return notInstalled("write %s: %w", path, werr)
	}
	return nil
}
