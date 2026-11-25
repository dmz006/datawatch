// Failure escalation — the evidence Accept needs.
//
// A completion marker is only the worker's own word, so it never completes a
// held task by itself. checkAcceptEvidence runs before acceptHeldTask and
// refuses, naming what is missing, unless:
//
//   - the task has a real task commit (a pre-task snapshot commit does not
//     count). A read-only task needs none, but then needs a report path or a
//     verification script;
//   - every file the task names (files, files_generated, report path) is in
//     that commit or in the Automaton worktree;
//   - the task's own verification script, when it names one, passes.
//
// Files the task commit changes outside the named ones are a warning only.
// There is no override: a refused task stays held for retry or cancel.
package autonomous

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	// acceptGitTimeout bounds all the git reads of one accept.
	acceptGitTimeout = 60 * time.Second
	// acceptLogMax bounds the commits read per ref.
	acceptLogMax = "5000"
	// acceptOutputTail is how much of a failed check's output the refusal keeps.
	acceptOutputTail = 600
	// acceptListMax bounds the file names a refusal or warning lists.
	acceptListMax = 10
	// Commit subject limits of the daemon's task commits (internal/session).
	acceptShortIDRunes  = 8
	acceptSubjectRunes  = 120
	acceptBlankTitle    = "session work"
	acceptSnapshotStart = "snapshot before "
)

var acceptTaskSubject = regexp.MustCompile(`^task\(([^)]*)\): (.*)$`)

// acceptInput is a copy of what the checks read from the Automaton and the
// held task, taken under the store lock so the checks run without it.
type acceptInput struct {
	prdID, prdDir, prdBranch          string
	created                           time.Time
	title                             string
	taskDir, taskProjectDir, worktree string
	taskBranch                        string
	readOnly                          bool
	reportPath, script                string
	files, generated                  []string
}

// acceptProof is what the checks found: the task commit (empty for a
// read-only task) and a warning for the decision log, or empty.
type acceptProof struct {
	commit  string
	warning string
}

// acceptCommit is one task commit and the directory it was read from.
type acceptCommit struct{ dir, sha string }

func (m *Manager) checkAcceptEvidence(prd *PRD, t *Task) (acceptProof, error) {
	var in acceptInput
	m.store.WithLock(func() {
		in = acceptInput{
			prdID: prd.ID, prdDir: prd.ProjectDir, prdBranch: prd.Git.Branch, created: prd.CreatedAt,
			title: t.Title, taskDir: ResolveTaskProjectDir(prd, t),
			taskProjectDir: t.ProjectDir, worktree: t.WorktreePath, taskBranch: t.WorktreeBranch,
			readOnly: t.ReadOnly, reportPath: reportPathOf(t), script: strings.TrimSpace(t.VerificationScript),
			files:     append([]string(nil), t.FilesPlanned...),
			generated: append([]string(nil), t.FilesGenerated...),
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), acceptGitTimeout)
	defer cancel()

	var proof acceptProof
	var commits []acceptCommit
	if in.readOnly {
		if in.reportPath == "" && in.script == "" {
			return proof, errors.New("it is read-only and has no report path and no verification script, so only the completion marker would prove it")
		}
	} else {
		var snapshot string
		commits, snapshot = findTaskCommits(ctx, in)
		if len(commits) == 0 {
			if snapshot != "" {
				return proof, fmt.Errorf("it has no task commit; the only commit found for it is a snapshot: %s", snapshot)
			}
			return proof, errors.New("it has no task commit in its task worktree or on the Automaton branch")
		}
		proof.commit = commits[0].sha
	}

	if missing := missingNamedFiles(ctx, in, commits); len(missing) > 0 {
		return proof, fmt.Errorf("named files are missing from the task commit and the Automaton worktree: %s", acceptList(missing))
	}
	if stray := strayCommitFiles(ctx, in, commits); len(stray) > 0 {
		proof.warning = "the task commit also changes files the task does not name: " + acceptList(stray)
	}
	if in.script != "" {
		if err := m.runAcceptScript(in); err != nil {
			return proof, err
		}
	}
	return proof, nil
}

// findTaskCommits returns the task's own commits, newest first per ref, from
// the task worktree and the Automaton branch. It matches by subject and never
// by a range from the pre-task sha, which moves forward on a retry. snapshot
// describes a pre-task snapshot commit of this task, when one was seen.
func findTaskCommits(ctx context.Context, in acceptInput) (found []acceptCommit, snapshot string) {
	title := acceptCommitTitle(in.title)
	short := firstRunes(in.prdID, acceptShortIDRunes)
	seen := map[string]bool{}
	read := func(dir, ref string) {
		if dir == "" || ref == "" || strings.HasPrefix(ref, "-") {
			return
		}
		args := []string{"log", "--no-merges", "--format=%H%x09%s", "-n", acceptLogMax}
		if !in.created.IsZero() {
			// A commit older than the Automaton is never this task's work.
			args = append(args, "--since="+in.created.Format(time.RFC3339))
		}
		out, err := taskGitOut(ctx, dir, append(args, ref, "--")...)
		if err != nil {
			return
		}
		for _, line := range strings.Split(out, "\n") {
			sha, subject, ok := strings.Cut(line, "\t")
			if !ok || seen[sha] {
				continue
			}
			seen[sha] = true
			if strings.HasPrefix(subject, acceptSnapshotStart) {
				if snapshot == "" && strings.Contains(subject, title) {
					snapshot = fmt.Sprintf("%s %q", firstRunes(sha, 10), subject)
				}
				continue
			}
			if isTaskCommitSubject(subject, short, title) {
				found = append(found, acceptCommit{dir: dir, sha: sha})
			}
		}
	}
	read(in.worktree, "HEAD")
	read(in.taskProjectDir, "HEAD")
	read(in.prdDir, in.taskBranch)
	read(in.prdDir, "HEAD")
	read(in.prdDir, in.prdBranch)
	return found, snapshot
}

// isTaskCommitSubject reports whether subject names this task. A
// "task(<id>): " subject must carry this Automaton's id and exactly this
// title. Any other subject must be the title, alone or after a "prefix: ".
func isTaskCommitSubject(subject, short, title string) bool {
	if g := acceptTaskSubject.FindStringSubmatch(subject); g != nil {
		return g[1] == short && g[2] == title
	}
	return subject == title || strings.HasSuffix(subject, ": "+title)
}

// acceptCommitTitle is title as the daemon writes it into a commit subject.
func acceptCommitTitle(title string) string {
	one := strings.Join(strings.Fields(title), " ")
	if one == "" {
		return acceptBlankTitle
	}
	return strings.TrimRight(firstRunes(one, acceptSubjectRunes), " ")
}

// missingNamedFiles returns the files the task names that are neither in a task
// commit nor in the Automaton worktree (or the task's own project dir). A
// .datawatch/ file is never committed, so the task worktree counts for it too.
func missingNamedFiles(ctx context.Context, in acceptInput, commits []acceptCommit) []string {
	var named []string
	named = append(named, in.files...)
	named = append(named, in.generated...)
	if in.reportPath != "" {
		named = append(named, in.reportPath)
	}
	var missing []string
	checked := map[string]bool{}
	for _, f := range named {
		if f = strings.TrimSpace(f); f == "" || checked[f] {
			continue
		}
		checked[f] = true
		if !namedFileExists(ctx, in, commits, path.Clean(f)) {
			missing = append(missing, f)
		}
	}
	return missing
}

func namedFileExists(ctx context.Context, in acceptInput, commits []acceptCommit, rel string) bool {
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return false
	}
	trees := []string{in.prdDir, in.taskProjectDir}
	if strings.HasPrefix(rel, ".datawatch/") {
		trees = append(trees, in.worktree)
	}
	for _, dir := range trees {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
			return true
		}
	}
	for _, c := range commits {
		if _, err := taskGitOut(ctx, c.dir, "cat-file", "-e", c.sha+":"+rel); err == nil {
			return true
		}
	}
	return false
}

// strayCommitFiles returns the files the task commits change outside the files
// the task names. A task that names none has nothing to compare against.
func strayCommitFiles(ctx context.Context, in acceptInput, commits []acceptCommit) []string {
	var named []string
	for _, f := range append(append([]string(nil), in.files...), in.generated...) {
		if f = strings.TrimSpace(f); f != "" {
			named = append(named, path.Clean(f))
		}
	}
	if len(named) == 0 {
		return nil
	}
	stray := map[string]bool{}
	for _, c := range commits {
		out, err := taskGitOut(ctx, c.dir, "show", "--name-only", "--format=", c.sha, "--")
		if err != nil {
			continue
		}
	files:
		for _, f := range strings.Split(out, "\n") {
			if f = strings.TrimSpace(f); f == "" || strings.HasPrefix(f, ".datawatch/") {
				continue
			}
			for _, n := range named {
				if f == n || strings.HasPrefix(f, n+"/") {
					continue files
				}
			}
			stray[f] = true
		}
	}
	list := make([]string, 0, len(stray))
	for f := range stray {
		list = append(list, f)
	}
	sort.Strings(list)
	return list
}

func acceptList(files []string) string {
	if n := len(files); n > acceptListMax {
		return fmt.Sprintf("%s and %d more", strings.Join(files[:acceptListMax], ", "), n-acceptListMax)
	}
	return strings.Join(files, ", ")
}

// runAcceptScript runs the task's verification script afresh, in the task's
// directory or else the Automaton worktree: niced, outside tmux, in its own
// process group that is killed at the limit. The limit is
// script_gate_timeout_seconds; this run is never uncapped, so 0 there means
// the shipped default. An earlier run's stored result is never reused.
func (m *Manager) runAcceptScript(in acceptInput) error {
	var dir, scriptPath string
	for _, d := range []string{in.taskDir, in.prdDir} {
		if d == "" {
			continue
		}
		p, err := resolveVerificationScriptPath(in.script, d)
		if err != nil {
			return err
		}
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			dir, scriptPath = d, p
			break
		}
	}
	if scriptPath == "" {
		return fmt.Errorf("its verification script %s is missing from the worktree", in.script)
	}
	seconds := m.Config().ScriptGateTimeoutSeconds
	if seconds <= 0 {
		seconds = config.DefaultScriptGateTimeoutSeconds
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "env", "-u", "TMUX", "-u", "TMUX_TMPDIR", "nice", "-n", "19", "bash", scriptPath) // #nosec G204 -- script path validated to resolve inside the task or Automaton worktree
	cmd.Dir = dir
	configureGateProcessGroup(cmd)
	cmd.WaitDelay = gateWaitDelay
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err == nil {
		return nil
	}
	why := err.Error()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		why = fmt.Sprintf("it ran past its %d-second limit", seconds)
	}
	tail := strings.TrimSpace(out.String())
	if len(tail) > acceptOutputTail {
		tail = "..." + strings.ToValidUTF8(tail[len(tail)-acceptOutputTail:], "")
	}
	if tail != "" {
		why += "; output: " + tail
	}
	return fmt.Errorf("its verification script %s did not pass: %s", in.script, why)
}
