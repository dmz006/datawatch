package resources

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// ScopeBinary is the launcher binary name, resolved through PATH.
const ScopeBinary = "systemd-run"

// ScopeSpec describes one wrapped command.
type ScopeSpec struct {
	// ID is the session or attempt id used in the unit name.
	ID string
	// Kind is a generic kind word used in the unit name.
	Kind string
	// CeilingMB is the memory ceiling in MB; must be positive.
	CeilingMB int
	// Command is the wrapped command and its arguments.
	Command []string
}

var unitSafe = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func sanitizeUnitPart(s string) string {
	return strings.Trim(unitSafe.ReplaceAllString(s, "-"), "-")
}

// UnitName builds the scope unit name from the id and kind word.
func UnitName(id, kind string) string {
	id, kind = sanitizeUnitPart(id), sanitizeUnitPart(kind)
	if kind == "" {
		kind = "task"
	}
	return fmt.Sprintf("datawatch-%s-%s", kind, id)
}

// ScopeArgs builds the argument list (without the binary name) for the
// systemd-run command.
func ScopeArgs(spec ScopeSpec) ([]string, error) {
	if sanitizeUnitPart(spec.ID) == "" {
		return nil, errors.New("scope: id is required")
	}
	if spec.CeilingMB <= 0 {
		return nil, errors.New("scope: ceiling must be a positive MB number")
	}
	if len(spec.Command) == 0 {
		return nil, errors.New("scope: wrapped command is required")
	}
	args := []string{
		"--user", "--scope",
		"-p", fmt.Sprintf("MemoryMax=%dM", spec.CeilingMB),
		"-p", "MemorySwapMax=0",
		"-p", "MemoryAccounting=yes",
		"--unit=" + UnitName(spec.ID, spec.Kind),
		"--",
	}
	return append(args, spec.Command...), nil
}

// ScopeCommand returns the binary path (resolved via PATH with
// exec.LookPath) and the full argument list.
func ScopeCommand(spec ScopeSpec) (string, []string, error) {
	args, err := ScopeArgs(spec)
	if err != nil {
		return "", nil, err
	}
	bin, err := exec.LookPath(ScopeBinary)
	if err != nil {
		return "", nil, fmt.Errorf("scope: %s not found in PATH: %w", ScopeBinary, err)
	}
	return bin, args, nil
}

// ScopeAvailable reports whether a user scope can be created on this host.
func ScopeAvailable() bool { return probeScope() }
