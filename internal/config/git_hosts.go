// Package config — git.hosts: the list of git hosts datawatch may push to.
//
// Each entry names a git host (GitHub, GitLab, Gitea, Bitbucket or a plain
// git remote), how datawatch authenticates to it, and whether tags pushed to
// it are signed. Defaults are applied at config load and ValidateGitHosts
// reports every problem in one pass. The dotted-key helpers
// (GetGitHostField / SetGitHostField) give later surfaces (REST, MCP, CLI,
// comm channel) one addressing scheme: git.hosts.<name>.<field>.
package config

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Git host kinds (git.hosts[].kind).
const (
	GitHostKindGitHub    = "github"
	GitHostKindGitLab    = "gitlab"
	GitHostKindGitea     = "gitea"
	GitHostKindBitbucket = "bitbucket"
	GitHostKindGit       = "git"
)

// Git host credential sources (git.hosts[].credential_source).
const (
	// GitCredentialHostLogin uses the login already configured on the host
	// (the default).
	GitCredentialHostLogin = "host_login"
	// GitCredentialVault reads the token from the secrets vault entry named
	// by VaultSecret.
	GitCredentialVault = "vault"
	// GitCredentialBroker asks the credential broker for a token.
	GitCredentialBroker = "broker"
)

// gitHostPrefix is the prefix of every dotted git host key.
const gitHostPrefix = "git.hosts."

// gitHostNameRE is the slug a host name must match: lowercase letters and
// digits, with single hyphens between parts. Dots are excluded so the dotted
// key git.hosts.<name>.<field> stays unambiguous.
var gitHostNameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// GitHostConfig is one entry of git.hosts.
type GitHostConfig struct {
	// Name is the lowercase slug that identifies the host. It must be unique
	// within git.hosts and is the <name> part of the dotted key
	// git.hosts.<name>.<field>. It cannot be changed through SetGitHostField.
	Name string `yaml:"name" json:"name"`
	// Kind is the host type: github, gitlab, gitea, bitbucket or git.
	Kind string `yaml:"kind" json:"kind"`
	// BaseURL is the host's web/API base URL. Required for gitea, bitbucket
	// and git. For gitlab it is required only when self-hosted (empty means
	// gitlab.com). For github empty means github.com.
	BaseURL string `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	// CredentialSource is how the token is obtained: host_login (default),
	// vault or broker.
	CredentialSource string `yaml:"credential_source,omitempty" json:"credential_source,omitempty"`
	// VaultSecret is the name of the secrets vault entry holding the token.
	// Required when CredentialSource is vault. It is the entry NAME only —
	// the secret value is never stored in the config.
	VaultSecret string `yaml:"vault_secret,omitempty" json:"vault_secret,omitempty"`
	// SignTags makes datawatch sign the tags it creates for this host.
	// Default false.
	SignTags bool `yaml:"sign_tags,omitempty" json:"sign_tags,omitempty"`
	// SigningKey is the optional key id used when signing tags. Empty uses
	// the git default signing key.
	SigningKey string `yaml:"signing_key,omitempty" json:"signing_key,omitempty"`
}

// gitHostFields lists the field names accepted in a dotted git host key.
var gitHostFields = []string{
	"kind", "base_url", "credential_source", "vault_secret", "sign_tags", "signing_key",
}

// applyGitHostDefaults fills defaults on every git.hosts entry. Only
// credential_source has a stored default (host_login); base_url defaults are
// resolved by the host kind at use time, so they stay empty here.
func applyGitHostDefaults(g *GitConfig) {
	for i := range g.Hosts {
		if g.Hosts[i].CredentialSource == "" {
			g.Hosts[i].CredentialSource = GitCredentialHostLogin
		}
	}
}

// loadGitHosts applies the git.hosts defaults and validates the list. It is
// called from both config load paths, so a malformed host list fails the
// load with every problem listed.
func loadGitHosts(cfg *Config) error {
	applyGitHostDefaults(&cfg.Git)
	return ValidateGitHosts(cfg.Git.Hosts)
}

// ValidateGitHosts checks a git.hosts list and returns every problem found,
// joined into one error (nil when the list is valid). Checked per entry:
// name is a non-empty unique lowercase slug; kind is one of the five known
// kinds; credential_source is one of the three known sources (empty reads as
// host_login); a vault source has a vault_secret name; and gitea, bitbucket
// and git have a base_url. The check never reads or prints a secret value.
func ValidateGitHosts(hosts []GitHostConfig) error {
	var errs []error
	seen := make(map[string]bool, len(hosts))
	for i, h := range hosts {
		label := fmt.Sprintf("git.hosts[%d]", i)
		if h.Name != "" {
			label = fmt.Sprintf("git.hosts[%d] (%s)", i, h.Name)
		}

		switch {
		case h.Name == "":
			errs = append(errs, fmt.Errorf("%s: name is required", label))
		case !gitHostNameRE.MatchString(h.Name):
			errs = append(errs, fmt.Errorf("%s: name must be a lowercase slug (a-z, 0-9, single hyphens)", label))
		case seen[h.Name]:
			errs = append(errs, fmt.Errorf("%s: duplicate name %q", label, h.Name))
		}
		if h.Name != "" {
			seen[h.Name] = true
		}

		switch h.Kind {
		case GitHostKindGitHub, GitHostKindGitLab, GitHostKindGitea, GitHostKindBitbucket, GitHostKindGit:
		default:
			errs = append(errs, fmt.Errorf("%s: unknown kind %q (want github, gitlab, gitea, bitbucket or git)", label, h.Kind))
		}

		source := h.CredentialSource
		if source == "" {
			source = GitCredentialHostLogin
		}
		switch source {
		case GitCredentialHostLogin, GitCredentialBroker:
		case GitCredentialVault:
			if strings.TrimSpace(h.VaultSecret) == "" {
				errs = append(errs, fmt.Errorf("%s: credential_source vault requires vault_secret (the vault entry name)", label))
			}
		default:
			errs = append(errs, fmt.Errorf("%s: unknown credential_source %q (want host_login, vault or broker)", label, h.CredentialSource))
		}

		switch h.Kind {
		case GitHostKindGitea, GitHostKindBitbucket, GitHostKindGit:
			if strings.TrimSpace(h.BaseURL) == "" {
				errs = append(errs, fmt.Errorf("%s: kind %s is self-hosted and requires base_url", label, h.Kind))
			}
		}
	}
	return errors.Join(errs...)
}

// GetGitHostField returns the value of the dotted key git.hosts.<name>.<field>
// as a string. Booleans read as "true" or "false". An unknown host, an unknown
// field or a malformed key is an error. The key is the only addressing the
// later surfaces need. Fields: name, kind, base_url, credential_source,
// vault_secret, sign_tags, signing_key.
func GetGitHostField(g GitConfig, key string) (string, error) {
	name, field, err := parseGitHostKey(key)
	if err != nil {
		return "", err
	}
	h, ok := findGitHost(g.Hosts, name)
	if !ok {
		return "", fmt.Errorf("git host %q not found", name)
	}
	switch field {
	case "name":
		return h.Name, nil
	case "kind":
		return h.Kind, nil
	case "base_url":
		return h.BaseURL, nil
	case "credential_source":
		return h.CredentialSource, nil
	case "vault_secret":
		return h.VaultSecret, nil
	case "sign_tags":
		return strconv.FormatBool(h.SignTags), nil
	case "signing_key":
		return h.SigningKey, nil
	}
	return "", fmt.Errorf("unknown git host field %q", field)
}

// SetGitHostField sets the dotted key git.hosts.<name>.<field> to value. The
// host must already exist; a host is created by writing the whole list, not
// by this helper. The name part cannot be changed. sign_tags accepts any value
// strconv.ParseBool reads. An empty credential_source resets to host_login.
// The change is applied to a copy, validated with ValidateGitHosts, and kept
// only when the whole list is still valid — on error g is unchanged.
func SetGitHostField(g *GitConfig, key, value string) error {
	name, field, err := parseGitHostKey(key)
	if err != nil {
		return err
	}
	if field == "name" {
		return fmt.Errorf("git host name cannot be changed; it is the key")
	}
	idx := -1
	for i := range g.Hosts {
		if g.Hosts[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("git host %q not found", name)
	}

	next := make([]GitHostConfig, len(g.Hosts))
	copy(next, g.Hosts)
	h := &next[idx]
	switch field {
	case "kind":
		h.Kind = value
	case "base_url":
		h.BaseURL = value
	case "credential_source":
		h.CredentialSource = value
	case "vault_secret":
		h.VaultSecret = value
	case "sign_tags":
		b, perr := strconv.ParseBool(value)
		if perr != nil {
			return fmt.Errorf("git.hosts.%s.sign_tags must be true or false, got %q", name, value)
		}
		h.SignTags = b
	case "signing_key":
		h.SigningKey = value
	default:
		return fmt.Errorf("unknown git host field %q (want one of %s)", field, strings.Join(gitHostFields, ", "))
	}
	applyGitHostDefaults(&GitConfig{Hosts: next})
	if err := ValidateGitHosts(next); err != nil {
		return err
	}
	g.Hosts = next
	return nil
}

// parseGitHostKey splits git.hosts.<name>.<field> into its name and field.
// The name is a slug without dots, so the first dot after the prefix ends it.
func parseGitHostKey(key string) (name, field string, err error) {
	if !strings.HasPrefix(key, gitHostPrefix) {
		return "", "", fmt.Errorf("git host key %q must start with %s", key, gitHostPrefix)
	}
	name, field, ok := strings.Cut(key[len(gitHostPrefix):], ".")
	if !ok || name == "" || field == "" {
		return "", "", fmt.Errorf("git host key %q must be git.hosts.<name>.<field>", key)
	}
	return name, field, nil
}

// findGitHost returns the entry named name, if any.
func findGitHost(hosts []GitHostConfig, name string) (GitHostConfig, bool) {
	for _, h := range hosts {
		if h.Name == name {
			return h, true
		}
	}
	return GitHostConfig{}, false
}

// ValidateGitHostsPatch applies the whole host list (when present) and the
// per-field keys git.hosts.<name>.<field> onto a copy of the current list,
// then validates the final combined state ONCE — so one write can set
// credential_source=vault together with the vault_secret it needs, and a
// batch that would leave a host inconsistent is rejected as a whole. A
// malformed key, unknown host, unknown field or non-boolean sign_tags is
// reported with the same clear message as SetGitHostField. The inputs are
// never mutated; the new list and nil are returned on success.
func ValidateGitHostsPatch(current, listValue []GitHostConfig, fieldPatches map[string]string) ([]GitHostConfig, error) {
	nextHosts := make([]GitHostConfig, len(current))
	copy(nextHosts, current)
	if listValue != nil {
		nextHosts = append(make([]GitHostConfig, 0, len(listValue)), listValue...)
	}
	names := make([]string, 0, len(fieldPatches))
	for k := range fieldPatches {
		if strings.HasPrefix(k, gitHostPrefix) {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	for _, k := range names {
		name, field, err := parseGitHostKey(k)
		if err != nil {
			return nil, err
		}
		if field == "name" {
			return nil, fmt.Errorf("git host name cannot be changed; it is the key")
		}
		idx := -1
		for i := range nextHosts {
			if nextHosts[i].Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("git host %q not found", name)
		}
		h := &nextHosts[idx]
		value := fieldPatches[k]
		switch field {
		case "kind":
			h.Kind = value
		case "base_url":
			h.BaseURL = value
		case "credential_source":
			h.CredentialSource = value
		case "vault_secret":
			h.VaultSecret = value
		case "sign_tags":
			b, perr := strconv.ParseBool(value)
			if perr != nil {
				return nil, fmt.Errorf("git.hosts.%s.sign_tags must be true or false, got %q", name, value)
			}
			h.SignTags = b
		case "signing_key":
			h.SigningKey = value
		default:
			return nil, fmt.Errorf("unknown git host field %q (want one of %s)", field, strings.Join(gitHostFields, ", "))
		}
	}
	applyGitHostDefaults(&GitConfig{Hosts: nextHosts})
	if err := ValidateGitHosts(nextHosts); err != nil {
		return nil, err
	}
	return nextHosts, nil
}
