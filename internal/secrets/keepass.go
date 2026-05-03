// BL242 Phase 2 — KeePass backend via keepassxc-cli.
//
// The value is stored in the KeePass entry's Password field.
// Description maps to Notes. Tags are stored as a comma-separated
// custom attribute named "datawatch-tags".
//
// The database master password is supplied at construction time.
// It is written to the subprocess's stdin (first line); when
// keepassxc-cli prompts for an entry password (-p flag), the
// entry value is written on the next line.

package secrets

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const keepassTagsAttr = "datawatch-tags"

// KeePassStore implements Store using keepassxc-cli as the backend.
type KeePassStore struct {
	binary   string
	dbPath   string
	password string
	group    string
}

// NewKeePassStore returns a KeePassStore. binary defaults to "keepassxc-cli"
// when empty. dbPath and password are required.
func NewKeePassStore(binary, dbPath, password, group string) (*KeePassStore, error) {
	if binary == "" {
		binary = "keepassxc-cli"
	}
	if _, err := exec.LookPath(binary); err != nil {
		return nil, fmt.Errorf("keepassxc-cli not found in PATH: %w", err)
	}
	if dbPath == "" {
		return nil, fmt.Errorf("keepass: db_path is required")
	}
	return &KeePassStore{binary: binary, dbPath: dbPath, password: password, group: group}, nil
}

// entryPath prepends the configured group if set.
func (k *KeePassStore) entryPath(name string) string {
	if k.group == "" {
		return name
	}
	return k.group + "/" + name
}

// run executes keepassxc-cli with stdin as supplied (DB password on line 1,
// optionally followed by an entry password on line 2 for -p commands).
func (k *KeePassStore) run(stdin string, args ...string) (string, error) {
	cmd := exec.Command(k.binary, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("keepassxc-cli %s: %w — %s", args[0], err, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}

// List returns all entries in the configured group, sorted by name. Values
// are omitted (KeePass list doesn't reveal passwords).
func (k *KeePassStore) List() ([]Secret, error) {
	args := []string{"ls", k.dbPath}
	if k.group != "" {
		args = append(args, k.group)
	}
	out, err := k.run(k.password+"\n", args...)
	if err != nil {
		return nil, err
	}
	var list []Secret
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// keepassxc-cli ls marks sub-groups with a trailing slash; skip them
		if line == "" || strings.HasSuffix(line, "/") {
			continue
		}
		list = append(list, Secret{Name: line, Backend: "keepass"})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// Get returns the full secret including value.
func (k *KeePassStore) Get(name string) (Secret, error) {
	out, err := k.run(k.password+"\n",
		"show", "-s", "-a", keepassTagsAttr, k.dbPath, k.entryPath(name))
	if err != nil {
		if isNotFound(err) {
			return Secret{}, ErrSecretNotFound
		}
		return Secret{}, err
	}
	return parseKeePassShow(name, out), nil
}

// Set creates or updates an entry. For an update the KeePass timestamps are
// preserved by the database; for a new entry they are set by keepassxc-cli.
func (k *KeePassStore) Set(name, value string, tags []string, description string) error {
	exists, err := k.Exists(name)
	if err != nil {
		return err
	}
	tagsStr := strings.Join(tags, ",")

	var args []string
	if exists {
		args = []string{
			"edit", k.dbPath, k.entryPath(name),
			"-p",
			"--notes", description,
			"-a", keepassTagsAttr + "=" + tagsStr,
		}
	} else {
		args = []string{
			"add", k.dbPath, k.entryPath(name),
			"-p",
			"--username", "",
			"--notes", description,
			"-a", keepassTagsAttr + "=" + tagsStr,
		}
	}
	// keepassxc-cli reads DB password on line 1, then entry password (-p) on line 2.
	_, err = k.run(k.password+"\n"+value+"\n", args...)
	return err
}

// Delete removes an entry. Returns ErrSecretNotFound if missing.
func (k *KeePassStore) Delete(name string) error {
	exists, err := k.Exists(name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrSecretNotFound
	}
	_, err = k.run(k.password+"\n", "rm", k.dbPath, k.entryPath(name))
	return err
}

// Exists reports whether a named entry is present in the database.
func (k *KeePassStore) Exists(name string) (bool, error) {
	_, err := k.run(k.password+"\n", "show", k.dbPath, k.entryPath(name))
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// isNotFound returns true when the keepassxc-cli error message indicates the
// entry was not found (vs. a real IO/auth failure).
func isNotFound(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no results") ||
		strings.Contains(msg, "entry not") ||
		strings.Contains(msg, "does not exist")
}

// parseKeePassShow converts keepassxc-cli show output to a Secret.
// Expected output format (keepassxc-cli 2.7.x):
//
//	Title: name
//	UserName:
//	Password: value     ← only with -s
//	URL:
//	Notes: description
//	Modified: 2006-01-02T15:04:05
//	 datawatch-tags: tag1,tag2   ← custom attribute, indented
func parseKeePassShow(name, out string) Secret {
	sec := Secret{Name: name, Backend: "keepass"}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Password: "):
			sec.Value = strings.TrimPrefix(line, "Password: ")
		case strings.HasPrefix(line, "Notes: "):
			sec.Description = strings.TrimPrefix(line, "Notes: ")
		case strings.HasPrefix(line, "Created: "):
			sec.CreatedAt = parseKeePassTime(strings.TrimPrefix(line, "Created: "))
		case strings.HasPrefix(line, "Modified: "):
			sec.UpdatedAt = parseKeePassTime(strings.TrimPrefix(line, "Modified: "))
		case strings.HasPrefix(line, " "+keepassTagsAttr+": "):
			raw := strings.TrimPrefix(line, " "+keepassTagsAttr+": ")
			if raw != "" {
				sec.Tags = strings.Split(raw, ",")
			}
		}
	}
	now := time.Now()
	if sec.CreatedAt.IsZero() {
		sec.CreatedAt = now
	}
	if sec.UpdatedAt.IsZero() {
		sec.UpdatedAt = now
	}
	return sec
}

func parseKeePassTime(s string) time.Time {
	for _, layout := range []string{
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
