// Project resolution for the Sessions grouping (project axis).
//
// The project of a session is read in one fixed order, shared by the grouped
// read (GET /api/sessions?group_by=project — which the MCP list_sessions
// params, the CLI --group-by flag and the `sessions groups` comm verb all
// call) and by the `project` field of the session list rows the PWA groups
// from:
//
//  1. the project of the owning Automaton: its working folder, mapped back
//     to the repository it is a worktree of and matched against the
//     registered project directories (config `projects`, name to dir). When
//     the Automaton has no folder any more, the recorded base repository
//     (autonomous.worktree_base_repo) is matched instead;
//  2. a `project:` tag on the session itself (several tags: sorted, first);
//  3. the registered-project match of the session's own working folder,
//     mapped the same way, so a session in a worktree of a registered
//     repository lands with the repository's sessions;
//  4. when an Automaton is known but no project resolves: the Automaton's
//     title — never the worktree folder;
//  5. only when neither a project nor an Automaton is known: the working
//     folder, else ungrouped.
//
// Candidates are always sorted, so the result never depends on map or tag
// order.

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// groupProjectResolver resolves session rows to project keys. It is built per
// request: the registry is read once and the Automaton lookups are cached.
type groupProjectResolver struct {
	names    []string          // registered project names, sorted
	byDir    map[string]string // normalised dir -> first sorted name
	baseRepo string            // autonomous.worktree_base_repo
	prd      func(id string) (title, dir string, ok bool)
	ownerTag func(id string) []string
	owners   map[string]groupOwner
}

// groupOwner is what is known about one owning Automaton.
type groupOwner struct {
	known   bool
	title   string
	project string
}

// newGroupProjectResolver builds the resolver from the daemon config and the
// Automaton readers. Any of them may be nil.
func newGroupProjectResolver(cfg *config.Config, prd func(string) (string, string, bool), ownerTag func(string) []string) *groupProjectResolver {
	r := &groupProjectResolver{
		byDir: map[string]string{}, prd: prd, ownerTag: ownerTag,
		owners: map[string]groupOwner{},
	}
	if cfg == nil {
		return r
	}
	r.baseRepo = strings.TrimSpace(cfg.Autonomous.WorktreeBaseRepo)
	for name := range cfg.Projects {
		r.names = append(r.names, name)
	}
	sort.Strings(r.names)
	for _, name := range r.names { // sorted: the first name owning a dir wins
		dir := strings.TrimSpace(cfg.Projects[name].Dir)
		if dir == "" {
			continue
		}
		if n := normalizeGroupDir(dir); n != "" {
			if _, dup := r.byDir[n]; !dup {
				r.byDir[n] = name
			}
		}
	}
	return r
}

// normalizeGroupDir cleans a directory and resolves symlinks when it exists.
func normalizeGroupDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	return dir
}

// matchDir returns the registered project owning dir, or "". The folder
// itself is tried first, then the repository it is a worktree (or checkout)
// of, then the longest registered directory containing it.
func (r *groupProjectResolver) matchDir(dir string) string {
	n := normalizeGroupDir(dir)
	if n == "" || len(r.byDir) == 0 {
		return ""
	}
	if name, ok := r.byDir[n]; ok {
		return name
	}
	if root := groupRepoRoot(n); root != "" {
		if name, ok := r.byDir[normalizeGroupDir(root)]; ok {
			return name
		}
	}
	best, bestLen := "", -1
	for d, name := range r.byDir {
		if strings.HasPrefix(n, d+string(filepath.Separator)) &&
			(len(d) > bestLen || (len(d) == bestLen && name < best)) {
			best, bestLen = name, len(d)
		}
	}
	return best
}

// groupRepoRoot returns the repository root a folder belongs to: for a plain
// checkout the folder holding `.git`, for a linked worktree the folder of the
// repository that owns it (the git common directory's parent). "" when the
// folder is not inside a repository (or no longer exists).
func groupRepoRoot(dir string) string {
	for p := dir; ; {
		gitPath := filepath.Join(p, ".git")
		if st, err := os.Stat(gitPath); err == nil {
			if st.IsDir() {
				return p
			}
			raw, err := os.ReadFile(gitPath) // #nosec G304 -- a .git file under a session folder
			if err != nil {
				return ""
			}
			gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), "gitdir:"))
			if gitdir == "" {
				return ""
			}
			if !filepath.IsAbs(gitdir) {
				gitdir = filepath.Join(p, gitdir)
			}
			common := gitdir
			if c, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil { // #nosec G304 -- inside the worktree's git dir
				common = strings.TrimSpace(string(c))
				if !filepath.IsAbs(common) {
					common = filepath.Join(gitdir, common)
				}
			}
			common = filepath.Clean(common)
			if filepath.Base(common) == ".git" {
				return filepath.Dir(common)
			}
			return common
		}
		parent := filepath.Dir(p)
		if parent == p {
			return ""
		}
		p = parent
	}
}

// owner returns what is known about the Automaton prdID, cached.
func (r *groupProjectResolver) owner(prdID string) groupOwner {
	if prdID == "" {
		return groupOwner{}
	}
	if o, ok := r.owners[prdID]; ok {
		return o
	}
	o := groupOwner{known: true}
	dir := ""
	if r.prd != nil {
		if title, d, ok := r.prd(prdID); ok {
			o.title, dir = strings.TrimSpace(title), strings.TrimSpace(d)
		}
	}
	if dir != "" {
		o.project = r.matchDir(dir)
	}
	if o.project == "" && r.baseRepo != "" {
		if _, err := os.Stat(dir); dir == "" || err != nil { // no folder left: the recorded base repository
			o.project = r.matchDir(r.baseRepo)
		}
	}
	if o.project == "" && r.ownerTag != nil {
		// Tags the Automaton's sessions carry count only when they name a
		// registered project: a worktree-derived tag must not become a group.
		for _, name := range sortedProjectTagNames(r.ownerTag(prdID)) {
			if _, ok := r.cfgHas(name); ok {
				o.project = name
				break
			}
		}
	}
	r.owners[prdID] = o
	return o
}

// cfgHas reports whether name is a registered project.
func (r *groupProjectResolver) cfgHas(name string) (string, bool) {
	i := sort.SearchStrings(r.names, name)
	if i < len(r.names) && r.names[i] == name {
		return name, true
	}
	return "", false
}

// sortedProjectTagNames returns the sorted, de-duplicated names of the
// `project:` tags in tags.
func sortedProjectTagNames(tags []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if !strings.HasPrefix(t, "project:") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(t, "project:"))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// fill sets the resolved fields on a grouping row from the session.
func (r *groupProjectResolver) fill(row *sessionGroupRow, sess *session.Session) {
	row.PRDID = sess.PRDID
	row.ProjectDir = sess.ProjectDir
	row.SystemTags = sess.SystemTags
	if sess.PRDID != "" {
		o := r.owner(sess.PRDID)
		row.OwnerProject, row.OwnerTitle = o.project, o.title
	}
	if sess.ProjectDir != "" {
		row.FolderProject = r.matchDir(sess.ProjectDir)
	}
}

// sessionProjectKey is the project-axis key for a session: the one value the
// grouped read and the list rows share.
func (r *groupProjectResolver) sessionProjectKey(sess *session.Session) string {
	var row sessionGroupRow
	r.fill(&row, sess)
	key := resolveGroupProject(row)
	if key == groupUngroupedKey {
		return ""
	}
	return key
}

// autonomousPRDReader adapts the Automaton manager's GetPRD to the resolver:
// it reads only the title and the working folder from the record.
func (s *Server) autonomousPRDReader() func(string) (string, string, bool) {
	if s.autonomousMgr == nil {
		return nil
	}
	return func(id string) (string, string, bool) {
		raw, ok := s.autonomousMgr.GetPRD(id)
		if !ok || raw == nil {
			return "", "", false
		}
		b, err := json.Marshal(raw)
		if err != nil {
			return "", "", false
		}
		var v struct {
			Title      string `json:"title"`
			ProjectDir string `json:"project_dir"`
		}
		if json.Unmarshal(b, &v) != nil {
			return "", "", false
		}
		return v.Title, v.ProjectDir, true
	}
}

// groupProjectResolver builds the per-request resolver for this server.
func (s *Server) groupProjectResolver() *groupProjectResolver {
	var ownerTag func(string) []string
	if s.manager != nil {
		ownerTag = s.manager.SystemTagsByPRD
	}
	return newGroupProjectResolver(s.cfg, s.autonomousPRDReader(), ownerTag)
}
