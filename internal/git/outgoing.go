// The outgoing check: the one way datawatch's own pushes leave the machine.
//
// Every push goes through checkedPush. It resolves each pushed revision to an
// object id once, asks the remote itself what it already has, and builds a
// manifest of exactly what the push would deliver: every new commit (its raw
// object, so message, author and committer), every line it adds (for a merge,
// the lines that are in no parent, which is where conflict resolutions are),
// every file name it adds, every new annotated tag object and the destination
// ref names. The manifest is scanned for secrets and handed to the project's
// own check, and the push then sends the checked object ids and nothing else:
// a branch that moved during the check, a configured push.followTags and
// submodule recursion cannot add anything.
//
// The manifest is built from state a checkout cannot forge: what the remote
// already has comes from the remote's own answer (never from the local
// refs/remotes tracking refs), replace refs, grafts and the commit graph are
// ignored, and diffs ignore attributes, textconv and external drivers.
//
// A check that cannot run refuses the push. A secret finding follows the
// secret scan mode: enforce refuses, shadow reports and pushes.
//
// The project's own check is the make target OutgoingCheckTarget in the
// project's top-level makefile, read and run from the base commit (the head of
// the remote's default branch), so the branch being pushed cannot weaken it.

package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/proclimit"
)

// OutgoingCheckTarget is the make target a project declares to check what is
// about to leave the machine.
const OutgoingCheckTarget = "check-outgoing"

// Environment variables the project's check target receives.
const (
	// OutgoingManifestEnv names the directory holding the manifest files.
	OutgoingManifestEnv = "OUTGOING_MANIFEST_DIR"
	// OutgoingProjectEnv names the project's main working tree, where files
	// that are not committed (a private pattern list) live.
	OutgoingProjectEnv = "OUTGOING_PROJECT_DIR"
)

// The most one push may deliver and still be checked (variables so a test can
// lower them).
var (
	outgoingMaxCommits   = 5000
	outgoingMaxDiffBytes = 256 << 20
)

const (
	outgoingCheckTimeout = 10 * time.Minute
	outgoingOutputTail   = 2000
	zeroObjectID         = "0000000000000000000000000000000000000000"
)

// OutgoingRef is one ref a push would create or update.
type OutgoingRef struct {
	// Src is the local revision to send: a full ref name or HEAD.
	Src string
	// Dst is the full destination ref name on the remote.
	Dst string
}

type checkedRef struct {
	dst, oldID, newID string
	objectType        string // commit or tag
}

type outgoingLine struct {
	commit, path string
	line         int
	text         string
	binary       bool
}

type outgoingCommit struct {
	id, raw string
	files   []string // file names the commit adds
}

type outgoingTag struct{ id, raw string }

type outgoingManifest struct {
	refs    []checkedRef
	commits []outgoingCommit
	tags    []outgoingTag
	lines   []outgoingLine
}

var (
	daemonScanMu sync.RWMutex
	daemonScanFn func() SecretScanPolicy
)

// SetDaemonScanPolicy wires the daemon's live secret scan settings for pushes
// whose caller passes no policy of its own (the ship step's tag push, the
// merge run's push, the worker pull request hook). A caller that passes a
// policy, including an explicit off, is not affected. Nil removes it.
func SetDaemonScanPolicy(fn func() SecretScanPolicy) {
	daemonScanMu.Lock()
	daemonScanFn = fn
	daemonScanMu.Unlock()
}

func effectiveScanPolicy(p SecretScanPolicy) SecretScanPolicy {
	if p.Mode != "" {
		return p
	}
	daemonScanMu.RLock()
	fn := daemonScanFn
	daemonScanMu.RUnlock()
	if fn != nil {
		return fn()
	}
	return p
}

// ---- git plumbing that ignores local rewriting ----

// IsObjectID reports whether s is a full object id: 40 or 64 lowercase hex
// digits. Such a value is never read by git as an option.
func IsObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	return strings.Trim(s, "0123456789abcdef") == ""
}

// outgoingEnv is the environment for every read the check does: replace refs
// and grafts are off, so the objects read are the objects a push sends.
func outgoingEnv() []string {
	return append(gitEnv(), "GIT_NO_REPLACE_OBJECTS=1", "GIT_GRAFT_FILE="+os.DevNull)
}

type limitedBuffer struct {
	buf      bytes.Buffer
	max      int
	overflow bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.buf.Len()+len(p) > l.max {
		l.overflow = true
		return 0, errors.New("output limit reached")
	}
	return l.buf.Write(p)
}

// readGit runs one git read and returns its stdout alone. stdin may be "".
func readGit(ctx context.Context, dir, stdin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	full := append([]string{"-c", "core.quotePath=false", "-c", "core.commitGraph=false"}, args...)
	cmd, done := Command(ctx, full...)
	defer done()
	cmd.Dir = dir
	cmd.Env = outgoingEnv()
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out := &limitedBuffer{max: outgoingMaxDiffBytes}
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = out, &stderr
	err := proclimit.Run(ctx, cmd)
	if out.overflow {
		return "", fmt.Errorf("git %s: more than %d bytes of output, too much to check", args[0], outgoingMaxDiffBytes)
	}
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", args[0], err, tailReason(stderr.String()))
	}
	return out.buf.String(), nil
}

// ---- the remote's own answer ----

type remoteState struct {
	refs        map[string]string // full ref name -> object id (peeled tags as name^{})
	defaultRef  string            // the ref the remote's HEAD names, "" when it names none
	remoteHeads int
}

// readRemote asks the remote the push goes to which refs it has. out is git's
// output for classifying a failure.
func readRemote(ctx context.Context, dir, remote string) (st remoteState, out string, err error) {
	urls, uerr := runGit(ctx, dir, "remote", "get-url", "--push", "--all", remote)
	if uerr != nil {
		return st, urls, fmt.Errorf("resolve the push address of %s: %w", remote, uerr)
	}
	list := strings.Fields(strings.TrimSpace(urls))
	if len(list) != 1 {
		return st, "", fmt.Errorf("remote %s has %d push addresses; the check needs exactly one", remote, len(list))
	}
	// By name when fetch and push go to the same place, so the remote's own
	// settings apply and an address holding a credential stays off the command
	// line; by the push address when they differ.
	address := list[0]
	if fetch, ferr := runGit(ctx, dir, "remote", "get-url", remote); ferr == nil && strings.TrimSpace(fetch) == address {
		address = remote
	}
	listing, lerr := runGit(ctx, dir, "ls-remote", "--symref", "--", address)
	if lerr != nil {
		return st, listing, fmt.Errorf("read the refs of %s: %s", remote, tailReason(listing))
	}
	st.refs = map[string]string{}
	for _, line := range strings.Split(listing, "\n") {
		left, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		if target, isSym := strings.CutPrefix(left, "ref: "); isSym {
			if name == "HEAD" {
				st.defaultRef = strings.TrimSpace(target)
			}
			continue
		}
		// Anything else, such as a warning on stderr, is not a ref. Both
		// values are given to git again later.
		if !IsObjectID(left) || (name != "HEAD" && !strings.HasPrefix(name, "refs/")) {
			continue
		}
		st.refs[name] = left
		if strings.HasPrefix(name, "refs/heads/") {
			st.remoteHeads++
		}
	}
	return st, "", nil
}

// protectedDestination names why dst may not be written by a push that is
// limited to work branches, or "".
func protectedDestination(ctx context.Context, dir, remote string, st remoteState, dst string) string {
	branch, isHead := strings.CutPrefix(dst, "refs/heads/")
	if !isHead {
		return "only a work branch may be pushed here, never " + dst
	}
	names := map[string]bool{"main": true, "master": true}
	if b, ok := strings.CutPrefix(st.defaultRef, "refs/heads/"); ok {
		names[b] = true
	}
	// The local record of the remote's default branch can only add a name.
	if out, err := runGit(ctx, dir, "symbolic-ref", "-q", "refs/remotes/"+remote+"/HEAD"); err == nil {
		if b, ok := strings.CutPrefix(strings.TrimSpace(out), "refs/remotes/"+remote+"/"); ok {
			names[b] = true
		}
	}
	if names[branch] {
		return branch + " is the remote's default branch (or a main branch name), which completion never moves"
	}
	return ""
}

// baseRef picks the remote branch whose head is the base commit.
func baseRef(st remoteState) string {
	if _, ok := st.refs[st.defaultRef]; ok && st.defaultRef != "" {
		return st.defaultRef
	}
	for _, r := range []string{"refs/heads/main", "refs/heads/master"} {
		if _, ok := st.refs[r]; ok {
			return r
		}
	}
	return ""
}

func haveObject(ctx context.Context, dir, id string) bool {
	_, err := readGit(ctx, dir, "", "cat-file", "-e", id)
	return err == nil
}

// ensureBase returns the base commit id, fetching it when this clone does not
// have it. "" with no error means the remote has no base branch.
func ensureBase(ctx context.Context, dir, remote string, st remoteState) (string, error) {
	ref := baseRef(st)
	if ref == "" {
		return "", nil
	}
	id := st.refs[ref]
	if haveObject(ctx, dir, id) {
		return id, nil
	}
	if out, err := runGit(ctx, dir, "fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--refmap=", remote, ref); err != nil {
		return "", fmt.Errorf("fetch the base commit from %s: %s", remote, tailReason(out))
	}
	if !haveObject(ctx, dir, id) {
		return "", fmt.Errorf("the base commit %s of %s is not available after a fetch (the remote moved; try again)", id, remote)
	}
	return id, nil
}

// ---- the project's declared check ----

var outgoingTargetRe = regexp.MustCompile(`(?m)^(?:[^\s:#=]+[ \t]+)*` + OutgoingCheckTarget + `(?:[ \t]+[^\s:#=]+)*[ \t]*::?(?:[^=]|$)`)

// declaresOutgoingCheck reports whether the top-level makefile of commit has
// the check target. The makefile is picked in make's own search order.
func declaresOutgoingCheck(ctx context.Context, dir, commit string) (bool, error) {
	out, err := readGit(ctx, dir, "", "ls-tree", "--name-only", commit, "--", "GNUmakefile", "makefile", "Makefile")
	if err != nil {
		return false, err
	}
	present := map[string]bool{}
	for _, n := range strings.Fields(out) {
		present[n] = true
	}
	for _, n := range []string{"GNUmakefile", "makefile", "Makefile"} {
		if !present[n] {
			continue
		}
		body, err := readGit(ctx, dir, "", "cat-file", "blob", commit+":"+n)
		if err != nil {
			return false, err
		}
		return outgoingTargetRe.MatchString(body), nil
	}
	return false, nil
}

// materialize writes the files of commit into dest straight from the objects,
// so no attribute, filter or local setting changes what the check runs.
func materialize(ctx context.Context, dir, commit, dest string) error {
	listing, err := readGit(ctx, dir, "", "ls-tree", "-r", "-z", "--full-tree", commit)
	if err != nil {
		return err
	}
	type entry struct{ mode, id, path string }
	var entries []entry
	for _, rec := range strings.Split(listing, "\x00") {
		meta, p, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 || f[1] != "blob" {
			continue // submodule links have no content here
		}
		if !filepath.IsLocal(p) {
			return fmt.Errorf("base commit holds an unsafe path")
		}
		entries = append(entries, entry{f[0], f[2], p})
	}
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	cmd, done := Command(ctx, "cat-file", "--batch")
	defer done()
	cmd.Dir = dir
	cmd.Env = outgoingEnv()
	var ids strings.Builder
	for _, e := range entries {
		ids.WriteString(e.id + "\n")
	}
	cmd.Stdin = strings.NewReader(ids.String())
	finish, err := proclimit.Prepare(ctx, cmd)
	if err != nil {
		return err
	}
	defer finish() // after the command has been waited for
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	r := bufio.NewReader(pipe)
	var links []entry
	for i := range entries {
		header, err := r.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read base files: %w", err)
		}
		f := strings.Fields(header)
		if len(f) != 3 {
			return fmt.Errorf("read base files: unexpected answer")
		}
		size, err := strconv.ParseInt(f[2], 10, 64)
		if err != nil {
			return fmt.Errorf("read base files: %w", err)
		}
		target := filepath.Join(dest, entries[i].path)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if entries[i].mode == "120000" {
			b := make([]byte, size)
			if _, err := io.ReadFull(r, b); err != nil {
				return err
			}
			e := entries[i]
			e.id = string(b) // the link target
			links = append(links, e)
		} else {
			mode := os.FileMode(0o600)
			if entries[i].mode == "100755" {
				mode = 0o700
			}
			w, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode) // #nosec G304 -- path checked local, inside a private temp dir
			if err != nil {
				return err
			}
			_, cerr := io.CopyN(w, r, size)
			if err := w.Close(); cerr == nil {
				cerr = err
			}
			if cerr != nil {
				return cerr
			}
		}
		if _, err := r.Discard(1); err != nil {
			return err
		}
	}
	// Links are made last so no file is ever written through one.
	for _, l := range links {
		if err := os.Symlink(l.id, filepath.Join(dest, l.path)); err != nil {
			return err
		}
	}
	return nil
}

// checkEnv is the environment for the project's check: the daemon's, without
// terminal-server, make and shell variables that would change what runs, and
// without anything that names a credential.
func checkEnv(manifestDir, projectDir string) []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		upper := strings.ToUpper(name)
		switch {
		case upper == "TMUX", upper == "TMUX_TMPDIR",
			upper == "MAKEFLAGS", upper == "MFLAGS", upper == "MAKEFILES", upper == "MAKELEVEL",
			upper == "SHELLOPTS", upper == "BASHOPTS", upper == "BASH_ENV", upper == "ENV",
			upper == "GIT_DIR", upper == "GIT_WORK_TREE", upper == "GIT_INDEX_FILE",
			strings.Contains(upper, "TOKEN"), strings.Contains(upper, "SECRET"), strings.Contains(upper, "PASSWORD"):
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", OutgoingManifestEnv+"="+manifestDir, OutgoingProjectEnv+"="+projectDir)
}

// mainWorkTree is the project's main working tree, also when dir is a linked
// worktree of it.
func mainWorkTree(ctx context.Context, dir string) (string, error) {
	out, err := readGit(ctx, dir, "", "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	common := strings.TrimSpace(out)
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	common, err = filepath.Abs(common)
	if err != nil {
		return "", err
	}
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common), nil
	}
	return common, nil
}

// writeManifest writes the files the project's check reads:
//
//	refs.tsv        destination ref, old object id, new object id
//	identities.tsv  object id, role (author, committer, tagger), name, email
//	commits/<id>    the raw commit object
//	tags/<id>       the raw annotated tag object
//	files.tsv       commit id, file name the commit adds
//	added.txt       one added line per line (NUL bytes removed)
//	added.idx       for the same line number: commit id, path, line, text|binary
func writeManifest(dir string, m *outgoingManifest) error {
	for _, sub := range []string{"commits", "tags"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return err
		}
	}
	var refs, ids, files, added, idx strings.Builder
	for _, r := range m.refs {
		fmt.Fprintf(&refs, "%s\t%s\t%s\n", r.dst, r.oldID, r.newID)
	}
	identities := func(id, raw string) {
		header, _, _ := strings.Cut(raw, "\n\n")
		for _, line := range strings.Split(header, "\n") {
			role, rest, _ := strings.Cut(line, " ")
			if role != "author" && role != "committer" && role != "tagger" {
				continue
			}
			open, shut := strings.LastIndex(rest, "<"), strings.LastIndex(rest, ">")
			name, email := rest, ""
			if open >= 0 && shut > open {
				name, email = strings.TrimSpace(rest[:open]), rest[open+1:shut]
			}
			clean := strings.NewReplacer("\t", " ", "\r", " ")
			fmt.Fprintf(&ids, "%s\t%s\t%s\t%s\n", id, role, clean.Replace(name), clean.Replace(email))
		}
	}
	write := func(name, body string) error {
		return os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600)
	}
	for _, c := range m.commits {
		identities(c.id, c.raw)
		if err := write(filepath.Join("commits", c.id), c.raw); err != nil {
			return err
		}
		for _, f := range c.files {
			fmt.Fprintf(&files, "%s\t%s\n", c.id, f)
		}
	}
	for _, t := range m.tags {
		identities(t.id, t.raw)
		if err := write(filepath.Join("tags", t.id), t.raw); err != nil {
			return err
		}
	}
	for _, l := range m.lines {
		kind := "text"
		if l.binary {
			kind = "binary"
		}
		added.WriteString(strings.ReplaceAll(l.text, "\x00", "") + "\n")
		fmt.Fprintf(&idx, "%s\t%s\t%d\t%s\n", l.commit, l.path, l.line, kind)
	}
	for name, body := range map[string]string{
		"refs.tsv": refs.String(), "identities.tsv": ids.String(), "files.tsv": files.String(),
		"added.txt": added.String(), "added.idx": idx.String(),
	} {
		if err := write(name, body); err != nil {
			return err
		}
	}
	return nil
}

// runProjectCheck runs the base commit's check target over the manifest. The
// check's output is returned (bounded, secrets redacted) only on failure; a
// check must not print what it matched.
func runProjectCheck(ctx context.Context, dir, base string, m *outgoingManifest, noProjectTree bool) (string, error) {
	tmp, err := os.MkdirTemp("", "dw-outgoing-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	tree, manifestDir := filepath.Join(tmp, "base"), filepath.Join(tmp, "manifest")
	if err := os.MkdirAll(tree, 0o700); err != nil {
		return "", err
	}
	if err := materialize(ctx, dir, base, tree); err != nil {
		return "", fmt.Errorf("lay out the base commit: %w", err)
	}
	if err := writeManifest(manifestDir, m); err != nil {
		return "", fmt.Errorf("write the manifest: %w", err)
	}
	project := filepath.Join(tmp, "no-project-tree")
	if noProjectTree {
		if err := os.Mkdir(project, 0o700); err != nil {
			return "", err
		}
	} else if project, err = mainWorkTree(ctx, dir); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, outgoingCheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "--no-print-directory", "-C", tree, OutgoingCheckTarget) // #nosec G204 -- fixed target name in a private temp dir
	cmd.Env = checkEnv(manifestDir, project)
	cmd.WaitDelay = 10 * time.Second
	configureCheckProcessGroup(cmd)
	finish, err := proclimit.Prepare(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("the project's %s target was not run: %w", OutgoingCheckTarget, err)
	}
	out, err := cmd.CombinedOutput()
	limit := finish()
	proclimit.NoteLeft(ctx, limit.Left)
	if limit.Failed != "" && ctx.Err() == nil {
		out = append(out, "\n"+limit.Failed...)
		err = errors.Join(&proclimit.LimitError{Why: limit.Failed}, err)
	}
	text, _ := RedactSecrets(redactCreds(strings.TrimSpace(string(out))))
	if len(text) > outgoingOutputTail {
		text = text[len(text)-outgoingOutputTail:]
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
	}
	if err == nil {
		return text, nil
	}
	return text, fmt.Errorf("the project's %s target (from base commit %.12s) did not pass: %v\n%s", OutgoingCheckTarget, base, err, text)
}

// ---- building the manifest ----

// outgoingCommits lists the commits reachable from the pushed objects that are
// reachable from nothing the remote advertises.
func outgoingCommits(ctx context.Context, dir string, st remoteState, tips []string) ([]string, error) {
	var ids []string
	for _, id := range st.refs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var spec strings.Builder
	for _, t := range tips {
		spec.WriteString(t + "\n")
	}
	if len(ids) > 0 {
		// Only objects this clone has can be excluded; leaving one out only
		// widens what is checked.
		out, err := readGit(ctx, dir, strings.Join(ids, "\n")+"\n", "cat-file", "--batch-check")
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) == 3 && (f[1] == "commit" || f[1] == "tag") {
				spec.WriteString("^" + f[0] + "\n")
			}
		}
	}
	out, err := readGit(ctx, dir, spec.String(), "rev-list", "--stdin")
	if err != nil {
		return nil, err
	}
	commits := strings.Fields(out)
	if len(commits) > outgoingMaxCommits {
		return nil, fmt.Errorf("%d commits would leave, more than the %d the check takes in one push", len(commits), outgoingMaxCommits)
	}
	return commits, nil
}

// commitParents returns the parents git itself counts: the consecutive
// "parent <id>" header lines directly after the "tree" line, nothing a
// hand-written object may add later in its header. It stops at the first
// other line.
func commitParents(raw string) []string {
	header, _, _ := strings.Cut(raw, "\n\n")
	lines := strings.Split(header, "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "tree ") {
		return nil
	}
	var parents []string
	for _, line := range lines[1:] {
		p, ok := strings.CutPrefix(line, "parent ")
		// git reads hex digits in either case.
		p = strings.ToLower(strings.TrimSpace(p))
		if !ok || !IsObjectID(p) {
			break
		}
		parents = append(parents, p)
	}
	return parents
}

// changedNames lists the paths of one diff, NUL separated so no name is
// quoted or split. parent "" diffs a root commit against nothing.
func changedNames(ctx context.Context, dir, parent, commit, filter string) ([]string, error) {
	args := []string{"diff-tree", "-r", "-z", "--no-commit-id", "--no-renames", "--name-only"}
	if filter != "" {
		args = append(args, "--diff-filter="+filter)
	}
	if parent == "" {
		args = append(args, "--root", commit)
	} else {
		args = append(args, parent, commit)
	}
	out, err := readGit(ctx, dir, "", args...)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, n := range strings.Split(out, "\x00") {
		if n != "" {
			names = append(names, n)
		}
	}
	return names, nil
}

func intersect(a, b []string) []string {
	in := map[string]bool{}
	for _, s := range b {
		in[s] = true
	}
	var out []string
	for _, s := range a {
		if in[s] {
			out = append(out, s)
		}
	}
	return out
}

// commitNames returns the file names commit adds: absent from every parent.
func commitNames(ctx context.Context, dir, commit string, parents []string) ([]string, error) {
	if len(parents) == 0 {
		return changedNames(ctx, dir, "", commit, "A")
	}
	var names []string
	for i, p := range parents {
		n, err := changedNames(ctx, dir, p, commit, "A")
		if err != nil {
			return nil, err
		}
		if i == 0 {
			names = n
		} else {
			names = intersect(names, n)
		}
	}
	return names, nil
}

type lineKey struct {
	path string
	line int
}

// patchLines returns the lines one diff adds, keyed by path and line number in
// the new version.
func patchLines(ctx context.Context, dir, parent, commit string, paths []string) (map[lineKey]string, []lineKey, error) {
	args := []string{"--literal-pathspecs", "diff-tree", "-r", "-p", "-U0", "--text", "--no-textconv", "--no-ext-diff",
		"--no-color", "--no-commit-id", "--no-renames", "--src-prefix=a/", "--dst-prefix=b/"}
	if parent == "" {
		args = append(args, "--root", commit)
	} else {
		args = append(args, parent, commit)
	}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	out, err := readGit(ctx, dir, "", args...)
	if err != nil {
		return nil, nil, err
	}
	lines := map[lineKey]string{}
	var order []lineKey
	path, newLine, inHunk := "", 0, false
	for _, raw := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(raw, "diff --git "):
			path, inHunk = "", false
		case !inHunk && strings.HasPrefix(raw, "+++ "):
			path = patchPath(strings.TrimPrefix(raw, "+++ "))
		case strings.HasPrefix(raw, "@@ "):
			newLine, inHunk = hunkNewStart(raw), true
		case inHunk && strings.HasPrefix(raw, "+"):
			if path == "" {
				return nil, nil, fmt.Errorf("diff of commit %s has added lines with no file name", commit)
			}
			k := lineKey{path, newLine}
			lines[k] = strings.TrimSuffix(raw[1:], "\r")
			order = append(order, k)
			newLine++
		case inHunk && strings.HasPrefix(raw, " "):
			newLine++
		}
	}
	return lines, order, nil
}

// patchPath reads the file name of a "+++ " patch header.
func patchPath(s string) string {
	s = strings.TrimSuffix(s, "\t")
	if strings.HasPrefix(s, `"`) {
		if u, err := strconv.Unquote(s); err == nil {
			s = u
		}
	}
	if s == os.DevNull || s == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(s, "b/")
}

// commitAddedLines returns the lines commit adds. For a merge these are the
// lines added against every parent: a line some parent already has either is
// on the remote or belongs to a commit that is checked itself, so what is left
// is what the merge wrote, conflict resolutions included.
func commitAddedLines(ctx context.Context, dir, commit string, parents []string) ([]outgoingLine, error) {
	var paths []string
	if len(parents) > 1 {
		for i, p := range parents {
			n, err := changedNames(ctx, dir, p, commit, "")
			if err != nil {
				return nil, err
			}
			if i == 0 {
				paths = n
			} else {
				paths = intersect(paths, n)
			}
		}
		if len(paths) == 0 {
			return nil, nil
		}
	}
	first := ""
	if len(parents) > 0 {
		first = parents[0]
	}
	keep, order, err := patchLines(ctx, dir, first, commit, paths)
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(parents); i++ {
		other, _, err := patchLines(ctx, dir, parents[i], commit, paths)
		if err != nil {
			return nil, err
		}
		for k := range keep {
			if _, ok := other[k]; !ok {
				delete(keep, k)
			}
		}
	}
	binary := map[string]bool{}
	for k, text := range keep {
		if strings.IndexByte(text, 0) >= 0 {
			binary[k.path] = true
		}
	}
	var lines []outgoingLine
	for _, k := range order {
		if text, ok := keep[k]; ok {
			lines = append(lines, outgoingLine{commit: commit, path: k.path, line: k.line, text: text, binary: binary[k.path]})
		}
	}
	return lines, nil
}

func buildManifest(ctx context.Context, dir string, st remoteState, refs []checkedRef) (*outgoingManifest, error) {
	m := &outgoingManifest{refs: refs}
	advertised := map[string]bool{}
	for _, id := range st.refs {
		advertised[id] = true
	}
	var tips []string
	seenTag := map[string]bool{}
	for _, r := range refs {
		if r.oldID == r.newID {
			continue
		}
		tips = append(tips, r.newID)
		// An annotated tag object carries its own message and tagger, and
		// may point at another tag object.
		id, kind := r.newID, r.objectType
		for kind == "tag" && !advertised[id] && !seenTag[id] {
			seenTag[id] = true
			raw, err := readGit(ctx, dir, "", "cat-file", "tag", id)
			if err != nil {
				return nil, err
			}
			m.tags = append(m.tags, outgoingTag{id: id, raw: raw})
			next, ok := strings.CutPrefix(strings.SplitN(raw, "\n", 2)[0], "object ")
			if !ok || !IsObjectID(strings.TrimSpace(next)) {
				return nil, fmt.Errorf("tag object %s names no object", id)
			}
			id = strings.TrimSpace(next)
			t, err := readGit(ctx, dir, "", "cat-file", "-t", id)
			if err != nil {
				return nil, err
			}
			kind = strings.TrimSpace(t)
			if kind != "tag" && kind != "commit" {
				return nil, fmt.Errorf("tag points at a %s, which the check cannot read as history", kind)
			}
		}
	}
	if len(tips) == 0 {
		return m, nil
	}
	commits, err := outgoingCommits(ctx, dir, st, tips)
	if err != nil {
		return nil, err
	}
	total := 0
	for _, id := range commits {
		raw, err := readGit(ctx, dir, "", "cat-file", "commit", id)
		if err != nil {
			return nil, err
		}
		parents := commitParents(raw)
		names, err := commitNames(ctx, dir, id, parents)
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			if strings.ContainsAny(n, "\n\r\t") {
				return nil, fmt.Errorf("commit %.12s adds a file name with a tab or line break, which the check cannot take", id)
			}
		}
		lines, err := commitAddedLines(ctx, dir, id, parents)
		if err != nil {
			return nil, err
		}
		for _, l := range lines {
			if strings.ContainsAny(l.path, "\n\r\t") {
				return nil, fmt.Errorf("commit %.12s changes a file name with a tab or line break, which the check cannot take", id)
			}
			total += len(l.text)
		}
		if total > outgoingMaxDiffBytes {
			return nil, fmt.Errorf("more than %d bytes would be added, too much to check", outgoingMaxDiffBytes)
		}
		m.commits = append(m.commits, outgoingCommit{id: id, raw: raw, files: names})
		m.lines = append(m.lines, lines...)
	}
	return m, nil
}

// ---- the push ----

type pushRules struct {
	// workBranchOnly refuses every destination that is not a branch other
	// than the remote's default branch.
	workBranchOnly bool
	// upstream is a local branch whose tracking is set after the push.
	upstream string
	// checkOnly runs every check and sends nothing.
	checkOnly bool
	// noProjectTree gives the project's check an empty project directory:
	// in a container worker there is no working tree of the project's owner,
	// and a file the task wrote into its own clone must not stand in for one.
	noProjectTree bool
}

// noProjectTreeNote explains a project check refusal from a container worker.
const noProjectTreeNote = "(run in a container worker, which has no project working tree: files that are never committed are not available to the check)"

func refused(remote, ref, id string, kind PushFailureKind, reason string) PushResult {
	return PushResult{Remote: remote, Ref: ref, SHA: id, Kind: kind, Reason: reason}
}

// checkedPush checks what refs would deliver to remote and, when it passes,
// sends exactly the checked object ids with a plain fast-forward push. label
// names the destination in results.
func checkedPush(ctx context.Context, dir, remote, label string, refs []OutgoingRef, policy SecretScanPolicy, rules pushRules) PushResult {
	if !remoteExists(ctx, dir, remote) {
		return refused(remote, label, "", PushFailNoRemote,
			fmt.Sprintf("remote %q is not configured in %s (try `git remote add %s <url>`)", remote, dir, remote))
	}
	policy = effectiveScanPolicy(policy)

	// Pin every source to an object id once.
	pinned := make([]checkedRef, 0, len(refs))
	for _, r := range refs {
		if err := checkRefName(r.Dst); err != nil {
			return refused(remote, label, "", PushFailOther, err.Error())
		}
		out, err := readGit(ctx, dir, "", "rev-parse", "--verify", "--end-of-options", r.Src)
		if err != nil {
			return refused(remote, label, "", PushFailOther, "cannot resolve "+r.Src+" to push it: "+tailReason(err.Error()))
		}
		id := strings.TrimSpace(out)
		t, err := readGit(ctx, dir, "", "cat-file", "-t", id)
		if err != nil {
			return refused(remote, label, id, PushFailOther, tailReason(err.Error()))
		}
		kind := strings.TrimSpace(t)
		if kind != "commit" && kind != "tag" {
			return refused(remote, label, id, PushFailCheck, "outgoing check: "+r.Src+" is a "+kind+", not a commit or a tag; nothing was pushed")
		}
		pinned = append(pinned, checkedRef{dst: r.Dst, newID: id, objectType: kind})
	}
	head := pinned[0].newID

	st, out, err := readRemote(ctx, dir, remote)
	if err != nil {
		res := refused(remote, label, head, PushFailCheck, "outgoing check could not read the remote, nothing was pushed: "+tailReason(err.Error()))
		if k := classifyPush(out); k != PushFailOther {
			res.Kind = k
		}
		return res
	}
	changed := false
	for i := range pinned {
		pinned[i].oldID = zeroObjectID
		if id, ok := st.refs[pinned[i].dst]; ok {
			pinned[i].oldID = id
		}
		changed = changed || pinned[i].oldID != pinned[i].newID
		if rules.workBranchOnly {
			if why := protectedDestination(ctx, dir, remote, st, pinned[i].dst); why != "" {
				if pinned[i].oldID == pinned[i].newID {
					continue // nothing would move
				}
				return refused(remote, label, head, PushFailProtected, "not pushed to "+remote+": "+why)
			}
		}
	}
	if !changed {
		setUpstream(ctx, dir, remote, rules.upstream)
		return PushResult{NothingToPush: true, Remote: remote, Ref: label, SHA: head}
	}

	findings, blocked := checkOutgoing(ctx, dir, remote, st, pinned, policy, rules.noProjectTree)
	if blocked.Kind != "" {
		blocked.Remote, blocked.Ref, blocked.SHA = remote, label, head
		log.Printf("git: push to %s/%s refused: %s", remote, label, blocked.Reason)
		return blocked
	}
	if rules.checkOnly {
		return PushResult{Remote: remote, Ref: label, SHA: head, SecretFindings: findings,
			ProjectCheckRan: blocked.ProjectCheckRan, ProjectCheckOutput: blocked.ProjectCheckOutput, SecretScanned: blocked.SecretScanned}
	}

	args := []string{"-c", "push.followTags=false", "push", "--no-follow-tags", "--recurse-submodules=no"}
	if len(pinned) > 1 {
		args = append(args, "--atomic")
	}
	args = append(args, remote)
	for _, r := range pinned {
		args = append(args, r.newID+":"+r.dst)
	}
	pushOut, err := runGit(ctx, dir, args...)
	if err != nil {
		res := failedPush(pushOut, remote, label)
		res.SHA = head
		return res
	}
	setUpstream(ctx, dir, remote, rules.upstream)
	if strings.Contains(pushOut, "Everything up-to-date") {
		return PushResult{NothingToPush: true, Remote: remote, Ref: label, SHA: head, SecretFindings: findings}
	}
	return PushResult{Pushed: true, Remote: remote, Ref: label, SHA: head, SecretFindings: findings}
}

// setUpstream records branch's tracking, which a push of an object id cannot.
func setUpstream(ctx context.Context, dir, remote, branch string) {
	if branch == "" {
		return
	}
	if _, err := runGit(ctx, dir, "rev-parse", "-q", "--verify", "refs/heads/"+branch); err != nil {
		return
	}
	for _, kv := range [][2]string{{"remote", remote}, {"merge", "refs/heads/" + branch}} {
		if out, err := runGit(ctx, dir, "config", "branch."+branch+"."+kv[0], kv[1]); err != nil {
			log.Printf("git: set upstream of %s: %s", branch, tailReason(out))
			return
		}
	}
}

// checkOutgoing runs the project's check and the secret scan over what refs
// would deliver. A non-empty Kind on the second result refuses the push.
func checkOutgoing(ctx context.Context, dir, remote string, st remoteState, refs []checkedRef, policy SecretScanPolicy, noProjectTree bool) ([]SecretFinding, PushResult) {
	cannot := func(format string, args ...any) ([]SecretFinding, PushResult) {
		return nil, PushResult{Kind: PushFailCheck, Reason: "outgoing check could not run, nothing was pushed: " + tailReason(fmt.Sprintf(format, args...))}
	}
	scan := policy.Mode != "" && policy.Mode != ScanOff
	var detectors []secretDetector
	if scan {
		RecordSecretScanRun()
		d, err := resolveDetectors(policy.Detectors)
		if err != nil {
			return cannot("%v", err)
		}
		detectors = d
	}

	base, err := ensureBase(ctx, dir, remote, st)
	if err != nil {
		return cannot("%v", err)
	}
	inBase := false
	if base != "" {
		if inBase, err = declaresOutgoingCheck(ctx, dir, base); err != nil {
			return cannot("%v", err)
		}
	}
	if !inBase {
		// A project that declares a check is never pushed without a trusted
		// copy of it.
		where := map[string]string{}
		for _, r := range refs {
			where[r.newID+"^{commit}"] = "the pushed commit"
		}
		local := strings.TrimPrefix(baseRef(st), "refs/heads/")
		for _, b := range []string{local, "main", "master"} {
			if b != "" {
				where["refs/heads/"+b+"^{commit}"] = "the local branch " + b
			}
		}
		for rev, what := range where {
			if _, err := readGit(ctx, dir, "", "rev-parse", "-q", "--verify", rev); err != nil {
				continue
			}
			declared, err := declaresOutgoingCheck(ctx, dir, rev)
			if err != nil {
				return cannot("%v", err)
			}
			if declared {
				return nil, PushResult{Kind: PushFailCheck, Reason: fmt.Sprintf(
					"outgoing check: %s declares the %s target but the base commit (the remote's default branch) does not, so there is no trusted copy to run; nothing was pushed. Put the target on the remote's default branch first",
					what, OutgoingCheckTarget)}
			}
		}
	}
	if !scan && !inBase {
		return nil, PushResult{}
	}

	m, err := buildManifest(ctx, dir, st, refs)
	if err != nil {
		return cannot("%v", err)
	}
	// The project's check goes first: when it refuses, nothing else about the
	// content (a file name it objects to, say) is printed.
	var ran PushResult
	if inBase {
		output, err := runProjectCheck(ctx, dir, base, m, noProjectTree)
		ran.ProjectCheckRan, ran.ProjectCheckOutput = true, output
		if err != nil {
			reason := "outgoing check refused the push, nothing was pushed: " + err.Error()
			if noProjectTree {
				reason += "\n" + noProjectTreeNote
			}
			ran.Kind, ran.Reason = PushFailCheck, reason
			return nil, ran
		}
	}
	if !scan {
		return nil, ran
	}
	ran.SecretScanned = true
	findings := scanManifest(m, detectors, policy.options())
	if len(findings) == 0 {
		return nil, ran
	}
	RecordSecretScanFindings(len(findings))
	summary := RedactedFindingsSummary(findings)
	if policy.Mode == ScanShadow {
		RecordSecretScanShadowReport()
		log.Printf("git: push to %s proceeds with shadow-mode secret findings: %s", remote, summary)
		return findings, ran
	}
	// Enforce, and any other mode value: refuse.
	RecordSecretScanRefusal()
	ran.Kind = PushFailSecret
	ran.Reason = "secret scan (enforce mode): " + summary + " refuses the push to " + remote
	ran.SecretFindings = findings
	return findings, ran
}

// EnforcedScanPolicy is the daemon's secret scan settings with the mode set to
// enforce: what a comparison that must not miss a secret scans with.
func EnforcedScanPolicy() SecretScanPolicy {
	p := effectiveScanPolicy(SecretScanPolicy{})
	p.Mode = ScanEnforce
	return p
}

// CheckOutgoing runs every check a push of refs to remote would run and sends
// nothing. A result with an empty Kind passed.
func CheckOutgoing(ctx context.Context, dir, remote string, refs []OutgoingRef, scan ...SecretScanPolicy) PushResult {
	if remote == "" {
		remote = "origin"
	}
	if len(refs) == 0 {
		return refused(remote, "", "", PushFailOther, "nothing to check")
	}
	return checkedPush(ctx, dir, remote, refs[0].Dst, refs, pushScanPolicy(scan), pushRules{checkOnly: true})
}

// PushCheckedRefs pushes several refs in one atomic, checked push.
func PushCheckedRefs(ctx context.Context, dir, remote string, refs []OutgoingRef, scan ...SecretScanPolicy) (res PushResult) {
	defer func() { RecordHostOp(HostOpPush, res.Kind == "" && (res.Pushed || res.NothingToPush)) }()
	if remote == "" {
		remote = "origin"
	}
	if len(refs) == 0 {
		return refused(remote, "", "", PushFailOther, "nothing to push")
	}
	return checkedPush(ctx, dir, remote, refs[0].Dst, refs, pushScanPolicy(scan), pushRules{})
}
