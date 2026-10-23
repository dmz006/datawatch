package inference

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// RoleSnapshot maps each claimed role to the entry it currently resolves to.
type RoleSnapshot map[string]string

// SnapshotRoles resolves every role claimed by an enabled entry.
func SnapshotRoles(entries []*LLM) RoleSnapshot {
	snap := RoleSnapshot{}
	for _, role := range allRoles(entries) {
		if c := roleClaimants(entries, role); len(c) > 0 {
			snap[role] = c[0].Name
		}
	}
	return snap
}

func allRoles(entries []*LLM) []string {
	set := map[string]bool{}
	for _, e := range entries {
		if e == nil {
			continue
		}
		for _, r := range e.Roles {
			if r = strings.ToLower(strings.TrimSpace(r)); r != "" {
				set[r] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// CheckRoleContention reports (a) roles claimed by two or more enabled entries
// and (b) roles that resolved to different entries in previous but now resolve
// to the same one. It returns warnings only, never an error.
func CheckRoleContention(entries []*LLM, previous RoleSnapshot) []string {
	var warns []string
	roles := allRoles(entries)
	for _, role := range roles {
		if c := roleClaimants(entries, role); len(c) > 1 {
			warns = append(warns, ambiguityWarning(role, c))
		}
	}
	current := SnapshotRoles(entries)
	byEntry := map[string][]string{}
	for _, role := range roles {
		if name, ok := current[role]; ok {
			byEntry[name] = append(byEntry[name], role)
		}
	}
	names := make([]string, 0, len(byEntry))
	for n := range byEntry {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		rs := byEntry[name]
		if len(rs) < 2 {
			continue
		}
		// converged: some pair of these roles pointed at different entries before.
		converged := false
		for i := 0; i < len(rs) && !converged; i++ {
			for j := i + 1; j < len(rs); j++ {
				a, aok := previous[rs[i]]
				b, bok := previous[rs[j]]
				if aok && bok && a != b {
					converged = true
					break
				}
			}
		}
		if converged {
			warns = append(warns, fmt.Sprintf("roles %s have converged on entry %q since the previous check",
				strings.Join(rs, ", "), name))
		}
	}
	return warns
}

// RoleContentionMonitor remembers the previous snapshot between checks so a
// config write and the periodic scan share one view of "since last time".
type RoleContentionMonitor struct {
	mu       sync.Mutex
	previous RoleSnapshot
	last     []string
}

// Check runs CheckRoleContention against the remembered snapshot, stores the
// new one and returns the warnings.
func (m *RoleContentionMonitor) Check(entries []*LLM) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := CheckRoleContention(entries, m.previous)
	m.previous = SnapshotRoles(entries)
	m.last = w
	return w
}

// Warnings returns the warnings from the most recent Check.
func (m *RoleContentionMonitor) Warnings() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.last...)
}
