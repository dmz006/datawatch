package inference

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// NormalizeRoles trims, lower-cases and de-duplicates roles, keeping first-seen
// order. An empty (or whitespace-only) role is an error; a nil/empty input
// yields nil.
func NormalizeRoles(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, r := range in {
		r = strings.ToLower(strings.TrimSpace(r))
		if r == "" {
			return nil, errors.New("roles must not contain an empty role")
		}
		if seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out, nil
}

// hasRole reports whether l claims role (role already normalised).
func (l *LLM) hasRole(role string) bool {
	for _, r := range l.Roles {
		if strings.ToLower(strings.TrimSpace(r)) == role {
			return true
		}
	}
	return false
}

// roleClaimants returns the enabled, non-tombstoned entries claiming role,
// ordered by name.
func roleClaimants(entries []*LLM, role string) []*LLM {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		return nil
	}
	var out []*LLM
	for _, e := range entries {
		if e == nil || e.Disabled || e.Tombstoned || !e.hasRole(role) {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ResolveRole picks the registry entry for a workload role. An explicit
// per-call-site backend or model always wins and the role lookup is skipped
// (nil entry, no warnings: the caller uses its own override). Otherwise the
// entry claiming role is returned; when several enabled entries claim it the
// first by name wins and an ambiguity warning names the candidates. An unknown
// role, or one no entry claims, returns a nil entry and no warnings.
func ResolveRole(entries []*LLM, role, explicitBackend, explicitModel string) (*LLM, []string) {
	if strings.TrimSpace(explicitBackend) != "" || strings.TrimSpace(explicitModel) != "" {
		return nil, nil
	}
	c := roleClaimants(entries, role)
	if len(c) == 0 {
		return nil, nil
	}
	var warns []string
	if len(c) > 1 {
		warns = append(warns, ambiguityWarning(strings.ToLower(strings.TrimSpace(role)), c))
	}
	return c[0], warns
}

func ambiguityWarning(role string, c []*LLM) string {
	names := make([]string, len(c))
	for i, e := range c {
		names[i] = e.Name
	}
	return fmt.Sprintf("role %q is claimed by %d enabled entries (%s); using %q",
		role, len(c), strings.Join(names, ", "), names[0])
}
