// BL242 Phase 3 — 1Password backend via the op CLI.
//
// Authentication is via a service account token supplied at construction
// time (from config or DATAWATCH_OP_TOKEN env var). The token is forwarded
// as OP_SERVICE_ACCOUNT_TOKEN in each subprocess environment; no stdin
// interaction is required.
//
// Storage layout:
//   title        → secret name
//   password     → secret value
//   notesPlain   → description
//   tags         → 1Password item tags (--tags flag)
//   created_at   → from op JSON response
//   updated_at   → from op JSON response

package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// OnePasswordStore implements Store using the 1Password CLI (op).
type OnePasswordStore struct {
	binary string
	vault  string
	token  string
}

// NewOnePasswordStore returns a OnePasswordStore. binary defaults to "op".
// vault is optional (empty = op's configured default vault). token is the
// service account token; DATAWATCH_OP_TOKEN env var may supply it instead.
func NewOnePasswordStore(binary, vault, token string) (*OnePasswordStore, error) {
	if binary == "" {
		binary = "op"
	}
	if _, err := exec.LookPath(binary); err != nil {
		return nil, fmt.Errorf("1Password CLI (op) not found in PATH: %w", err)
	}
	return &OnePasswordStore{binary: binary, vault: vault, token: token}, nil
}

// run executes op, injecting the service account token when set.
func (s *OnePasswordStore) run(args ...string) ([]byte, error) {
	cmd := exec.Command(s.binary, args...)
	if s.token != "" {
		cmd.Env = append(os.Environ(), "OP_SERVICE_ACCOUNT_TOKEN="+s.token)
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("op %s: %w — %s", args[0], err, strings.TrimSpace(errBuf.String()))
	}
	return out.Bytes(), nil
}

// vaultArgs returns --vault <name> when a vault is configured.
func (s *OnePasswordStore) vaultArgs() []string {
	if s.vault == "" {
		return nil
	}
	return []string{"--vault", s.vault}
}

// opItem is the subset of op's JSON item representation we need.
type opItem struct {
	Title     string    `json:"title"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Fields    []opField `json:"fields"`
}

type opField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

func (s *OnePasswordStore) List() ([]Secret, error) {
	args := append([]string{"item", "list", "--format", "json"}, s.vaultArgs()...)
	data, err := s.run(args...)
	if err != nil {
		return nil, err
	}
	var items []opItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("op list parse: %w", err)
	}
	list := make([]Secret, len(items))
	for i, it := range items {
		list[i] = Secret{
			Name:      it.Title,
			Tags:      it.Tags,
			Backend:   "onepassword",
			CreatedAt: it.CreatedAt,
			UpdatedAt: it.UpdatedAt,
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

func (s *OnePasswordStore) Get(name string) (Secret, error) {
	args := append([]string{"item", "get", name, "--format", "json"}, s.vaultArgs()...)
	data, err := s.run(args...)
	if err != nil {
		if isOPNotFound(err) {
			return Secret{}, ErrSecretNotFound
		}
		return Secret{}, err
	}
	var it opItem
	if err := json.Unmarshal(data, &it); err != nil {
		return Secret{}, fmt.Errorf("op get parse: %w", err)
	}
	sec := Secret{
		Name:      it.Title,
		Tags:      it.Tags,
		Backend:   "onepassword",
		CreatedAt: it.CreatedAt,
		UpdatedAt: it.UpdatedAt,
	}
	for _, f := range it.Fields {
		switch f.Label {
		case "password":
			sec.Value = f.Value
		case "notesPlain":
			sec.Description = f.Value
		case "datawatch-scopes":
			if f.Value != "" {
				sec.Scopes = strings.Split(f.Value, ",")
			}
		}
	}
	return sec, nil
}

func (s *OnePasswordStore) Set(name, value string, tags []string, description string, scopes []string) error {
	exists, err := s.Exists(name)
	if err != nil {
		return err
	}

	scopesStr := strings.Join(scopes, ",")
	var args []string
	if exists {
		args = []string{"item", "edit", name}
		args = append(args, s.vaultArgs()...)
		args = append(args, "password="+value, "notesPlain="+description,
			"datawatch-scopes[text]="+scopesStr)
		if len(tags) > 0 {
			args = append(args, "--tags", strings.Join(tags, ","))
		}
	} else {
		args = []string{"item", "create", "--category", "login", "--title", name}
		args = append(args, s.vaultArgs()...)
		args = append(args, "password="+value, "notesPlain="+description,
			"datawatch-scopes[text]="+scopesStr)
		if len(tags) > 0 {
			args = append(args, "--tags", strings.Join(tags, ","))
		}
	}
	_, err = s.run(args...)
	return err
}

func (s *OnePasswordStore) Delete(name string) error {
	exists, err := s.Exists(name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrSecretNotFound
	}
	args := append([]string{"item", "delete", name}, s.vaultArgs()...)
	_, err = s.run(args...)
	return err
}

func (s *OnePasswordStore) Exists(name string) (bool, error) {
	args := append([]string{"item", "get", name, "--fields", "label=title"}, s.vaultArgs()...)
	_, err := s.run(args...)
	if err != nil {
		if isOPNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// isOPNotFound returns true when the op error message indicates a missing item.
func isOPNotFound(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "isn't an item") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no item") ||
		strings.Contains(msg, "could not find") ||
		strings.Contains(msg, "does not exist")
}
