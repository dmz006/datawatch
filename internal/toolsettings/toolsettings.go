// Package toolsettings reads and writes the settings files of the tools
// datawatch launches (Claude Code, opencode, MCP lists) without losing any of
// them. A file that cannot be read and understood to its last byte is never
// written.
package toolsettings

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"unicode/utf8"
)

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

// AccountHome returns the home folder the user database names for this account,
// which HOME need not be; "" when it cannot say. It is a variable only so tests
// can name a folder of their own: the lookup ignores HOME, so without it a test
// would look at the real account's folders. The daemon never reassigns it.
var AccountHome = func() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return u.HomeDir
}

// RefuseUserClaude refuses a project folder where writing .claude/<name> would
// reach beyond the project: no folder at all, a home folder, a .claude folder
// that holds a user's own settings, or a .claude folder or settings file that
// is a link to somewhere else. For installers that write into a project.
func RefuseUserClaude(projectDir, name string) error {
	if projectDir == "" {
		return fmt.Errorf("there is no project folder")
	}
	var homes []string
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		homes = append(homes, h)
	}
	if h := AccountHome(); h != "" {
		homes = append(homes, h)
	}
	userDirs := []string{}
	for _, h := range homes {
		if samePlace(projectDir, h) {
			return fmt.Errorf("the project folder is a home folder, whose .claude settings apply to everything the user runs")
		}
		userDirs = append(userDirs, filepath.Join(h, ".claude"))
	}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		userDirs = append(userDirs, d)
	}
	claudeDir := filepath.Join(projectDir, ".claude")
	settings := filepath.Join(claudeDir, name)
	for _, p := range []string{claudeDir, settings} {
		if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a link, and what it points to may not belong to this project", p)
		}
	}
	for _, d := range userDirs {
		if samePlace(claudeDir, d) || samePlace(settings, filepath.Join(d, name)) {
			return fmt.Errorf("%s holds user-level settings", claudeDir)
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

// Read reads the JSON object in path, in full, or fails. A missing or empty
// file gives an empty object. mode is the file's mode, 0 when there is no file.
// A link, or anything else that is not an ordinary file, is refused.
func Read(path string) (doc map[string]any, mode os.FileMode, err error) {
	left := func(format string, args ...any) (map[string]any, os.FileMode, error) {
		return nil, 0, fmt.Errorf("%s "+format+", so it is left as it is", append([]any{path}, args...)...)
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return map[string]any{}, 0, nil
	}
	if err != nil {
		return left("cannot be read (%v)", err)
	}
	if !info.Mode().IsRegular() {
		return left("is not an ordinary file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return left("cannot be read (%v)", err)
	}
	mode = info.Mode().Perm()
	if len(raw) == 0 {
		return map[string]any{}, mode, nil
	}
	if !utf8.Valid(raw) {
		return left("is not valid UTF-8") // the decoder would replace the bytes
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // numbers are written back as they are
	if err := dec.Decode(&doc); err != nil {
		return left("cannot be parsed (%v)", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return left("has text after its JSON object")
	}
	if doc == nil {
		return left("is not a JSON object")
	}
	if key := duplicateKey(raw); key != "" {
		return left("holds the key %q twice", key)
	}
	return doc, mode, nil
}

// ReadThroughLink is Read for a file that may be a link on purpose, such as a
// user's settings kept with their other dotfiles. real is where to write it
// back, so the link stays a link.
func ReadThroughLink(path string) (real string, doc map[string]any, mode os.FileMode, err error) {
	real = path
	if info, lerr := os.Lstat(path); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		if real, err = filepath.EvalSymlinks(path); err != nil {
			return "", nil, 0, fmt.Errorf("%s is a link that cannot be followed (%v), so it is left as it is", path, err)
		}
	}
	doc, mode, err = Read(real)
	return real, doc, mode, err
}

// Object returns the object under key in parent, adding an empty one when
// there is none. Anything else under the key is an error: replacing it would
// lose it.
func Object(parent map[string]any, key string) (map[string]any, error) {
	v := parent[key]
	if v == nil {
		o := map[string]any{}
		parent[key] = o
		return o, nil
	}
	o, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%q is not an object, so the file is left as it is", key)
	}
	return o, nil
}

// newTemp makes the file Write fills beside path. When path exists the caller
// gives the temporary file the old mode. When it does not, the file is made the
// ordinary way, so the umask applies to mode as it would to any new file.
func newTemp(path string, mode os.FileMode, exists bool) (*os.File, error) {
	if exists {
		return os.CreateTemp(filepath.Dir(path), ".dw-settings-*")
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	name := filepath.Join(filepath.Dir(path), ".dw-settings-"+hex.EncodeToString(b[:]))
	return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode) // #nosec G304 -- beside the settings file
}

// Write replaces path whole with doc: an interrupted write leaves the old file.
// A file that could not be written in place (read-only, or another user's) is
// not replaced either.
func Write(path string, doc map[string]any, mode os.FileMode) error {
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, serr := os.Lstat(path)
	exists := serr == nil
	if exists {
		// Opened without truncating: this only asks whether it may be written.
		f, err := os.OpenFile(path, os.O_WRONLY, 0) // #nosec G304 -- the settings file
		if err != nil {
			return fmt.Errorf("%s cannot be written, so it is left as it is: %w", path, err)
		}
		_ = f.Close()
	}
	tmp, err := newTemp(path, mode, exists)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // gone after the rename
	_, werr := tmp.Write(body.Bytes())
	if exists {
		if err := tmp.Chmod(mode); werr == nil {
			werr = err
		}
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
		return fmt.Errorf("write %s: %w", path, werr)
	}
	return nil
}
