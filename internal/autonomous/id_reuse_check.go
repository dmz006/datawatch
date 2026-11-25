// The startup check for ids issued twice before the id check existed: a record
// whose id already had leftover work older than the record itself. It only
// reads and reports; it repairs nothing.

package autonomous

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// idReuseSlack absorbs timestamp rounding: a record is created before its
	// own branch and folder, so only clearly older work counts.
	idReuseSlack = time.Minute
	// idReuseAlertMax bounds the ids named in one alert; the log names all.
	idReuseAlertMax = 20
	idReuseDoc      = "docs/howto/autonomous-recovery.md"
)

// IDReuseArtifact is a branch or folder older than the record carrying its id.
type IDReuseArtifact struct {
	// Name is the ref name of a branch, or the path of a folder.
	Name string
	// Repo is the repository of a branch.
	Repo string
	// Since is the earliest time the branch or folder is known to have existed.
	Since time.Time
}

// IDReuseConflict is a record whose id was already in use before it existed.
type IDReuseConflict struct {
	ID        string
	Title     string
	CreatedAt time.Time
	Artifacts []IDReuseArtifact
}

// CheckIDReuse reports every record that has leftover work older than its own
// creation time. A branch's age comes from its reflog (reflogSince), because
// its newest commit may be older than the branch; a branch without a reflog has
// no known age and is not reported. A folder's age is the oldest change time of the
// folder and of the .git entry of each worktree in it.
func (m *Manager) CheckIDReuse() []IDReuseConflict {
	leftovers := m.store.idLeftovers()
	if len(leftovers) == 0 {
		return nil
	}
	type record struct {
		id, title string
		created   time.Time
	}
	var records []record
	m.store.mu.Lock()
	for id, p := range m.store.prds {
		if id != "" && !p.CreatedAt.IsZero() {
			records = append(records, record{id: id, title: p.Title, created: p.CreatedAt})
		}
	}
	m.store.mu.Unlock()

	gitDirs := map[string]string{}
	since := map[string]time.Time{}
	age := func(l idLeftover) time.Time {
		key := l.repo + "\x00" + l.full
		if at, ok := since[key]; ok {
			return at
		}
		var at time.Time
		if l.repo == "" {
			at = folderSince(l.full)
		} else {
			dir, ok := gitDirs[l.repo]
			if !ok {
				var err error
				if dir, err = gitCommonDir(l.repo); err != nil {
					log.Printf("[autonomous] id check: git directory of %s: %v (branch ages there are unknown)", l.repo, err)
					dir = ""
				}
				gitDirs[l.repo] = dir
			}
			at = reflogSince(dir, l.full)
		}
		since[key] = at
		return at
	}

	var out []IDReuseConflict
	for _, r := range records {
		c := IDReuseConflict{ID: r.id, Title: r.title, CreatedAt: r.created}
		seen := map[string]bool{}
		for _, l := range leftovers {
			// A task branch is named for its Automaton first, so it counts. A
			// task folder is named for its task: its parent folder, which is
			// the Automaton's, is checked instead.
			if (l.task && l.repo == "") || !l.ownedBy(r.id) || seen[l.full] {
				continue
			}
			seen[l.full] = true
			if at := age(l); !at.IsZero() && at.Before(r.created.Add(-idReuseSlack)) {
				c.Artifacts = append(c.Artifacts, IDReuseArtifact{Name: l.full, Repo: l.repo, Since: at})
			}
		}
		if len(c.Artifacts) > 0 {
			sort.Slice(c.Artifacts, func(i, j int) bool { return c.Artifacts[i].Since.Before(c.Artifacts[j].Since) })
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// folderSince is the oldest change time seen on dir, on its .git entry and, for
// a folder of task worktrees, on each of those.
func folderSince(dir string) time.Time {
	var oldest time.Time
	note := func(path string) {
		st, err := os.Lstat(path)
		if err == nil && (oldest.IsZero() || st.ModTime().Before(oldest)) {
			oldest = st.ModTime()
		}
	}
	note(dir)
	note(filepath.Join(dir, ".git"))
	if strings.HasSuffix(dir, taskFoldersSuffix) {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				note(filepath.Join(dir, e.Name()))
				note(filepath.Join(dir, e.Name(), ".git"))
			}
		}
	}
	return oldest
}

// reflogSince is the time ref is known to have had its name: the last rename
// onto it, else its oldest reflog entry; zero when the reflog does not say. A
// branch made under another name keeps its older entries when it is renamed.
func reflogSince(gitDir, ref string) time.Time {
	if gitDir == "" || !filepath.IsLocal(ref) {
		return time.Time{}
	}
	// Read through a root on the git directory: neither the ref name nor a
	// link inside the directory can lead the read out of it.
	root, err := os.OpenRoot(gitDir)
	if err != nil {
		return time.Time{}
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(filepath.Join("logs", ref))
	if err != nil {
		return time.Time{}
	}
	var since time.Time
	for i, line := range strings.Split(string(data), "\n") {
		head, msg, _ := strings.Cut(line, "\t")
		renamed := strings.HasPrefix(msg, "Branch: renamed ") && strings.HasSuffix(msg, " to "+ref)
		if i > 0 && !renamed {
			continue
		}
		fields := strings.Fields(head) // old new name <email> seconds zone
		if len(fields) < 2 {
			continue
		}
		if secs, err := strconv.ParseInt(fields[len(fields)-2], 10, 64); err == nil {
			since = time.Unix(secs, 0)
		}
	}
	return since
}

// ReportIDReuse runs CheckIDReuse, logs every finding and raises one operator
// alert when there is any. It changes nothing else.
func (m *Manager) ReportIDReuse() {
	found := m.CheckIDReuse()
	if len(found) == 0 {
		return
	}
	var body strings.Builder
	fmt.Fprintf(&body, "%d record(s) have a branch or worktree folder that is older than the record, so the id was in use before. Nothing was changed. Do not run, delete or clean up these records until the older work has been saved (see %s).", len(found), idReuseDoc)
	for i, c := range found {
		oldest := c.Artifacts[0]
		line := fmt.Sprintf("%s: record created %s, but %s dates from %s (%d older item(s))",
			c.ID, c.CreatedAt.Format(time.RFC3339), oldest.Name, oldest.Since.Format(time.RFC3339), len(c.Artifacts))
		log.Printf("[autonomous] id check: %s", line)
		if i < idReuseAlertMax {
			body.WriteString("\n" + line)
		}
	}
	if extra := len(found) - idReuseAlertMax; extra > 0 {
		fmt.Fprintf(&body, "\n%d more are listed in the daemon log.", extra)
	}
	if sink := m.AlertSink(); sink != nil {
		sink("warn", "An Automaton id was issued more than once", body.String())
	}
}

// StartIDReuseCheck runs ReportIDReuse in the background at daemon start.
func (m *Manager) StartIDReuseCheck() { go m.ReportIDReuse() }
