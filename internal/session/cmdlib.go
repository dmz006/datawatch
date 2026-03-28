package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// SavedCommand is a named reusable command string.
type SavedCommand struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	Seeded    bool      `json:"seeded,omitempty"` // true for pre-populated commands
	CreatedAt time.Time `json:"created_at"`
}

// CmdLibrary stores named reusable commands in a JSON file.
type CmdLibrary struct {
	mu     sync.Mutex
	path   string
	encKey []byte
	cmds   []SavedCommand
}

// NewCmdLibrary creates a new CmdLibrary backed by the given file path (no encryption).
// If the file does not exist it is created on first write.
func NewCmdLibrary(path string) (*CmdLibrary, error) {
	return newCmdLibraryWithKey(path, nil)
}

// NewCmdLibraryEncrypted creates a CmdLibrary with AES-256-GCM encryption at rest.
func NewCmdLibraryEncrypted(path string, key []byte) (*CmdLibrary, error) {
	return newCmdLibraryWithKey(path, key)
}

func newCmdLibraryWithKey(path string, key []byte) (*CmdLibrary, error) {
	lib := &CmdLibrary{path: path, encKey: key}
	if err := lib.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load command library: %w", err)
	}
	return lib, nil
}

func (l *CmdLibrary) load() error {
	data, err := secfile.ReadFile(l.path, l.encKey)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &l.cmds)
}

func (l *CmdLibrary) save() error {
	data, err := json.MarshalIndent(l.cmds, "", "  ")
	if err != nil {
		return err
	}
	return secfile.WriteFile(l.path, data, 0600, l.encKey)
}

// Add creates a new named command. Returns an error if the name already exists.
func (l *CmdLibrary) Add(name, command string) (*SavedCommand, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range l.cmds {
		if c.Name == name {
			return nil, fmt.Errorf("command %q already exists (use delete first)", name)
		}
	}
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	cmd := SavedCommand{
		ID:        hex.EncodeToString(b),
		Name:      name,
		Command:   command,
		CreatedAt: time.Now(),
	}
	l.cmds = append(l.cmds, cmd)
	return &cmd, l.save()
}

// Delete removes a command by name or ID.
func (l *CmdLibrary) Delete(nameOrID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, c := range l.cmds {
		if c.Name == nameOrID || c.ID == nameOrID {
			l.cmds = append(l.cmds[:i], l.cmds[i+1:]...)
			return l.save()
		}
	}
	return fmt.Errorf("command %q not found", nameOrID)
}

// Update changes the name and/or command text of an existing entry.
// oldName identifies the entry; name/command are the new values.
func (l *CmdLibrary) Update(oldName, name, command string) (*SavedCommand, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, c := range l.cmds {
		if c.Name == oldName || c.ID == oldName {
			if name != "" {
				l.cmds[i].Name = name
			}
			if command != "" {
				l.cmds[i].Command = command
			}
			if err := l.save(); err != nil {
				return nil, err
			}
			updated := l.cmds[i]
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("command %q not found", oldName)
}

// List returns all saved commands.
func (l *CmdLibrary) List() []SavedCommand {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]SavedCommand, len(l.cmds))
	copy(out, l.cmds)
	return out
}

// Get looks up a command by name or ID.
func (l *CmdLibrary) Get(nameOrID string) (*SavedCommand, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range l.cmds {
		if c.Name == nameOrID || c.ID == nameOrID {
			cp := c
			return &cp, true
		}
	}
	return nil, false
}

// Seed adds the given commands if they don't already exist.
// Seeded commands are marked with Seeded=true and can be listed separately.
func (l *CmdLibrary) Seed(seeds []SavedCommand) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	changed := false
	for _, s := range seeds {
		found := false
		for _, c := range l.cmds {
			if c.Name == s.Name {
				found = true
				break
			}
		}
		if !found {
			b := make([]byte, 2)
			rand.Read(b) //nolint:errcheck
			s.ID = hex.EncodeToString(b)
			if s.CreatedAt.IsZero() {
				s.CreatedAt = time.Now()
			}
			s.Seeded = true
			l.cmds = append(l.cmds, s)
			changed = true
		}
	}
	if changed {
		return l.save()
	}
	return nil
}
