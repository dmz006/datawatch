package autonomous

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Derived project alias. An Automaton, an Automation and a plan record can be
// shown under a project named by a registered project alias (the registry
// behind GET /api/projects: alias name -> directory). The alias is derived
// when a record is read; no stored record carries it and no stored record is
// rewritten.
//
// It is consistent with ProjectKey (progress_project.go), which keeps counting
// a record with no project as ProjectKeyNone: that function is unchanged.

// ProjectAliasResolver resolves records against one alias set. Build it once
// per list read (NewProjectAliasResolver) so the alias directories are
// canonicalised once, not once per record.
type ProjectAliasResolver struct {
	names map[string]bool
	dirs  []aliasDir
}

type aliasDir struct {
	name string
	dir  string
}

// NewProjectAliasResolver canonicalises each alias directory (absolute,
// symlink-resolved, cleaned) and orders them longest first, so the first
// containing directory found is the longest. Aliases with a blank name or a
// blank directory still count as registered names but never match a directory.
func NewProjectAliasResolver(aliases map[string]string) *ProjectAliasResolver {
	r := &ProjectAliasResolver{names: make(map[string]bool, len(aliases))}
	for name, dir := range aliases {
		r.names[name] = true
		if strings.TrimSpace(name) == "" || strings.TrimSpace(dir) == "" {
			continue
		}
		r.dirs = append(r.dirs, aliasDir{name: name, dir: absClean(dir)})
	}
	sort.Slice(r.dirs, func(i, j int) bool {
		if len(r.dirs[i].dir) != len(r.dirs[j].dir) {
			return len(r.dirs[i].dir) > len(r.dirs[j].dir)
		}
		return r.dirs[i].name < r.dirs[j].name // deterministic on a tie
	})
	return r
}

// Registered reports whether name is a registered alias.
func (r *ProjectAliasResolver) Registered(name string) bool {
	return r != nil && r.names[name]
}

// Resolve returns the alias for a record: the explicit profile when it is a
// registered alias; otherwise the longest registered alias directory that
// contains dir; otherwise "".
func (r *ProjectAliasResolver) Resolve(profile, dir string) string {
	if r == nil {
		return ""
	}
	if p := strings.TrimSpace(profile); p != "" && r.names[p] {
		return p
	}
	if strings.TrimSpace(dir) == "" || len(r.dirs) == 0 {
		return ""
	}
	d := absClean(dir)
	for _, a := range r.dirs {
		if pathContains(a.dir, d) {
			return a.name
		}
	}
	return ""
}

// pathContains reports whether child is root or lies under it. Both are
// already cleaned absolute paths.
func pathContains(root, child string) bool {
	if child == root {
		return true
	}
	if root == string(filepath.Separator) {
		return true
	}
	return strings.HasPrefix(child, root+string(filepath.Separator))
}

// ResolveProjectAlias returns the registered project alias a record belongs
// to, given its explicit project profile, its project directory and the alias
// set (alias name -> directory). The explicit profile wins when it is a
// registered alias; otherwise the longest registered alias directory that
// contains dir (cleaned, symlinks resolved) wins; otherwise the result is "".
func ResolveProjectAlias(profile, dir string, aliases map[string]string) string {
	return NewProjectAliasResolver(aliases).Resolve(profile, dir)
}

// ProjectAliasOfPRD returns the derived project alias of an Automaton.
func ProjectAliasOfPRD(p *PRD, aliases map[string]string) string {
	if p == nil {
		return ""
	}
	return ResolveProjectAlias(p.ProjectProfile, p.ProjectDir, aliases)
}

// ProjectAliasOfAutomation returns the derived project alias of an Automation,
// which carries a project directory but no project profile.
func ProjectAliasOfAutomation(a *Automation, aliases map[string]string) string {
	if a == nil {
		return ""
	}
	return ResolveProjectAlias("", a.ProjectDir, aliases)
}

// ValidateProjectAlias is the check a record store calls to confirm a record's
// project is a registered alias. An empty project is valid (the record has no
// project); a non-empty one must name a key of aliases.
func ValidateProjectAlias(project string, aliases map[string]string) error {
	project = strings.TrimSpace(project)
	if project == "" {
		return nil
	}
	if _, ok := aliases[project]; !ok {
		return fmt.Errorf("project %q is not a registered project alias", project)
	}
	return nil
}
