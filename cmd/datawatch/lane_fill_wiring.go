package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
)

// federationSessionLister lists the running sessions of this host and of every
// federation peer, through the same view the federation sessions endpoint
// serves.
type federationSessionLister struct {
	sessions func() server.FederationResponse
}

func (l federationSessionLister) RunningSessions(ctx context.Context) ([]autonomous.PoolSession, []autonomous.PoolHostFault) {
	if l.sessions == nil {
		return nil, []autonomous.PoolHostFault{{Host: "local", Err: errors.New("session list unavailable")}}
	}
	resp := l.sessions()
	var out []autonomous.PoolSession
	add := func(host string, list []*session.Session) {
		for _, s := range list {
			if s == nil || s.State != session.StateRunning {
				continue
			}
			out = append(out, autonomous.PoolSession{ID: s.FullID, Backend: s.BackendFamily, Host: host, Node: s.ComputeNodeRef})
		}
	}
	add("", resp.Primary)
	var faults []autonomous.PoolHostFault
	for name, list := range resp.Proxied {
		add(name, list)
	}
	for name, msg := range resp.Errors {
		faults = append(faults, autonomous.PoolHostFault{Host: name, Err: errors.New(msg)})
	}
	sort.Slice(faults, func(i, j int) bool { return faults[i].Host < faults[j].Host })
	return out, faults
}

// computePoolHosts lists the Ollama endpoints of this host and of the enabled
// compute nodes. An endpoint shared by several entries is listed once.
type computePoolHosts struct {
	reg       *compute.Registry
	localHost func() string
}

func (h computePoolHosts) PoolHosts(ctx context.Context) []autonomous.PoolHost {
	var out []autonomous.PoolHost
	seen := map[string]bool{}
	add := func(name, node, endpoint string) {
		endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
		if endpoint == "" || seen[endpoint] {
			return
		}
		seen[endpoint] = true
		out = append(out, autonomous.PoolHost{Name: name, Endpoint: endpoint, Node: node})
	}
	if h.reg != nil {
		for _, n := range h.reg.List() {
			if n.Disabled || n.Kind != compute.KindOllama {
				continue
			}
			add(n.Name, n.Name, n.Address)
		}
	}
	if h.localHost != nil {
		if host := strings.TrimSpace(h.localHost()); host != "" {
			name := "local"
			if u, err := url.Parse(host); err == nil && u.Hostname() != "" {
				name = u.Hostname()
			}
			add(name, "", host)
		}
	}
	return out
}

// registryNodeLookup resolves a registered compute node by name, read live so
// an edit to the node's TLS settings applies to the next read. A nil registry
// resolves nothing.
func registryNodeLookup(reg *compute.Registry) autonomous.NodeLookup {
	if reg == nil {
		return nil
	}
	return func(name string) (*compute.Node, bool) {
		n, err := reg.Get(name)
		if err != nil || n == nil {
			return nil, false
		}
		return n, true
	}
}

// laneFillHostHolders names the pool hosts that hold work: the nodes of the
// capacity ledger's leases, the nodes of running sessions, and the nodes of
// Automaton tasks that are running or pinned to a node.
type laneFillHostHolders struct {
	leases   func() []capacity.Lease
	sessions autonomous.SessionLister
	prds     func() []*autonomous.PRD
}

func (h laneFillHostHolders) HeldHosts(ctx context.Context) map[string]bool {
	held := map[string]bool{}
	add := func(name string) {
		if name = strings.TrimSpace(name); name != "" {
			held[name] = true
		}
	}
	if h.leases != nil {
		for _, l := range h.leases() {
			add(l.Node)
			for _, p := range l.Pools {
				if n, ok := strings.CutPrefix(p, "node:"); ok {
					add(n)
				}
			}
		}
	}
	if h.sessions != nil {
		sessions, _ := h.sessions.RunningSessions(ctx)
		for _, s := range sessions {
			add(s.Node)
		}
	}
	if h.prds != nil {
		for _, prd := range h.prds() {
			if prd == nil {
				continue
			}
			for _, st := range prd.Story {
				for _, t := range st.Tasks {
					if t.Status == autonomous.TaskInProgress {
						add(t.ComputeNode)
					}
				}
			}
		}
	}
	return held
}

// newLaneFillEvidenceSource builds the pool evidence source lane fill plans
// with, over the real session list and the compute registry.
func newLaneFillEvidenceSource(mgr *autonomous.Manager, httpServer *server.HTTPServer, reg *compute.Registry, ollamaHost string, ledger *capacity.Ledger) autonomous.PoolEvidenceSource {
	lister := federationSessionLister{sessions: httpServer.FederationSessions}
	holders := laneFillHostHolders{
		sessions: lister,
		prds:     func() []*autonomous.PRD { return mgr.Store().ListPRDs() },
	}
	if ledger != nil {
		holders.leases = ledger.Leases
	}
	return autonomous.NewPoolEvidenceSource(
		lister,
		computePoolHosts{reg: reg, localHost: func() string { return ollamaHost }},
		autonomous.NewHTTPModelFetcherForNodes(nil, registryNodeLookup(reg)),
		autonomous.PoolEvidenceGPUBackend(func() string { return mgr.Config().LaneFill.EffectiveGPUBackend() }),
		autonomous.PoolEvidenceHostDownAfter(func() int { return mgr.Config().LaneFill.EffectiveHostDownAfterMinutes() }),
		autonomous.PoolEvidenceHolders(holders),
	)
}

// laneFillProposalHeadLimit caps how many bytes of a proposal file are read;
// the rule looks only at the first 200 lines.
const laneFillProposalHeadLimit = 64 * 1024

// repoProposalReader reads proposal files for the confirmed-proposal rule,
// under the Automaton base repository only. A reference that is absolute,
// holds a parent-directory element or resolves (through symbolic links) to a
// place outside the repository is reported as not existing, and nothing
// outside the repository is opened.
type repoProposalReader struct {
	root func() string
}

// ReadHead returns up to the first 64 KiB of the file at rel, a
// repository-relative path.
func (r repoProposalReader) ReadHead(rel string) ([]byte, error) {
	notFound := func(why string) error { return fmt.Errorf("proposal %q: %s: %w", rel, why, fs.ErrNotExist) }
	if rel == "" || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return nil, notFound("absolute path")
	}
	for _, el := range strings.FieldsFunc(rel, func(c rune) bool { return c == '/' || c == '\\' }) {
		if el == ".." {
			return nil, notFound("parent-directory element")
		}
	}
	base := ""
	if r.root != nil {
		base = strings.TrimSpace(r.root())
	}
	if base == "" {
		return nil, errors.New("no Automaton base repository is configured")
	}
	realRoot, err := filepath.EvalSymlinks(base)
	if err != nil {
		return nil, err
	}
	realFile, err := filepath.EvalSymlinks(filepath.Join(realRoot, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err // a missing file wraps fs.ErrNotExist
	}
	within, err := filepath.Rel(realRoot, realFile)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) || filepath.IsAbs(within) {
		return nil, notFound("resolves outside the repository")
	}
	f, err := os.Open(realFile)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, notFound("not a regular file")
	}
	return io.ReadAll(io.LimitReader(f, laneFillProposalHeadLimit))
}

// newLaneFillProposalReader builds the proposal reader over the Automaton base
// repository setting, read on every call so a config change applies at once.
func newLaneFillProposalReader(mgr *autonomous.Manager) autonomous.LaneFillProposalReader {
	return repoProposalReader{root: func() string {
		if mgr == nil {
			return ""
		}
		return mgr.Config().WorktreeBaseRepo
	}}
}

// repoMainProbe is the real main-carries probe. It answers true when the base
// branch of the Automaton base repository already carries the work of an
// Automaton, by either of two signals:
//
//   - the base branch history holds a commit whose subject names
//     automaton/<id> (a merge) or task(<id>) (a task commit);
//   - the Automaton has a branch with commits, and the branch-versus-base
//     comparison the merge pass uses (patch ids plus the subject check) says
//     every one of them is already on the base branch.
//
// It only reads: log, for-each-ref, rev-parse, rev-list and cherry. Every git
// call runs under the caller's context, so the poll's timeout ends a slow one.
type repoMainProbe struct {
	root func() string
	git  autonomous.MergeGitRunner
}

// newLaneFillMainProbe builds the probe over the Automaton base repository
// setting, read on every call so a config change applies at once.
func newLaneFillMainProbe(mgr *autonomous.Manager) *repoMainProbe {
	return &repoMainProbe{
		root: func() string {
			if mgr == nil {
				return ""
			}
			return mgr.Config().WorktreeBaseRepo
		},
		git: autonomous.NewMergeGitRunner(),
	}
}

var laneFillIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}$`)

func (p *repoMainProbe) repo() (string, error) {
	repo := ""
	if p.root != nil {
		repo = strings.TrimSpace(p.root())
	}
	if repo == "" {
		return "", errors.New("no Automaton base repository is configured")
	}
	return repo, nil
}

func (p *repoMainProbe) runner() autonomous.MergeGitRunner {
	if p.git != nil {
		return p.git
	}
	return autonomous.NewMergeGitRunner()
}

// MainHead returns the head commit of the base branch, which keys the cache.
func (p *repoMainProbe) MainHead(ctx context.Context) (string, error) {
	repo, err := p.repo()
	if err != nil {
		return "", err
	}
	base, err := autonomous.LaneFillBaseBranch(ctx, p.runner(), repo)
	if err != nil {
		return "", err
	}
	out, err := p.runner().Run(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+base)
	if err != nil {
		out, err = p.runner().Run(ctx, repo, "rev-parse", "--verify", "--quiet", base)
		if err != nil {
			return "", fmt.Errorf("base branch head: %v: %s", err, strings.TrimSpace(out))
		}
	}
	head := strings.TrimSpace(out)
	if head == "" {
		return "", errors.New("base branch head: empty")
	}
	return head, nil
}

// Carries implements autonomous.LaneFillMainProbe.
func (p *repoMainProbe) Carries(ctx context.Context, id, branch string) (bool, string, error) {
	if !laneFillIDPattern.MatchString(id) {
		return false, "", fmt.Errorf("automaton id %q is not 8 hex characters", id)
	}
	repo, err := p.repo()
	if err != nil {
		return false, "", err
	}
	git := p.runner()
	base, err := autonomous.LaneFillBaseBranch(ctx, git, repo)
	if err != nil {
		return false, "", err
	}

	// Signal (a): a merge or task commit for the Automaton on the base branch.
	out, err := git.Run(ctx, repo, "log", "--fixed-strings", "-n", "200", "--format=%h\t%s",
		"--grep=automaton/"+id, "--grep=task("+id+")", "refs/heads/"+base, "--")
	if err != nil {
		// The base branch may exist only as a remote-tracking ref.
		out, err = git.Run(ctx, repo, "log", "--fixed-strings", "-n", "200", "--format=%h\t%s",
			"--grep=automaton/"+id, "--grep=task("+id+")", base, "--")
		if err != nil {
			return false, "", fmt.Errorf("log of %s: %v: %s", base, err, strings.TrimSpace(out))
		}
	}
	for _, line := range strings.Split(out, "\n") {
		sha, subject, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		if strings.Contains(subject, "automaton/"+id) || strings.Contains(subject, "task("+id+")") {
			return true, fmt.Sprintf("commit %s on %s: %s", sha, base, subject), nil
		}
	}

	// Signal (b): the Automaton branch is entirely on the base branch.
	branch = strings.TrimSpace(branch)
	if branch == "" {
		refs, err := git.Run(ctx, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/automaton/"+id+"*")
		if err != nil {
			return false, "", fmt.Errorf("list branches of Automaton %s: %v: %s", id, err, strings.TrimSpace(refs))
		}
		for _, l := range strings.Split(refs, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				branch = l
				break
			}
		}
	}
	if branch == "" {
		return false, "", nil // no branch, no commits: nothing to compare
	}
	if strings.HasPrefix(branch, "-") || strings.ContainsAny(branch, " \t\n") {
		return false, "", fmt.Errorf("automaton branch %q is not a usable branch name", branch)
	}
	if _, err := git.Run(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		return false, "", nil // the branch is not in the repository: nothing to compare
	}
	cmp := autonomous.CompareBranchToMain(ctx, git, repo, branch, base)
	switch cmp.State {
	case autonomous.MergeStateMerged:
		return true, fmt.Sprintf("Automaton branch %s: all %d commit(s) are already on %s (every patch id matches and every subject is on %s)",
			branch, cmp.Ahead, base, base), nil
	case autonomous.MergeStateUnknown:
		return false, "", errors.New(cmp.Error)
	}
	return false, "", nil
}

// llmLaneCapSource is the real lane count cap source: the enabled-state and
// effective in-flight cap of every entry in the inference registry. The cap is
// inference.EffectiveMaxInflight, the same function syncLLMLimits sets the
// capacity ledger limit llm:<name> from, so lane counts and admission agree.
type llmLaneCapSource struct {
	reg   func() *inference.Registry
	table func() map[string]int
}

func (s llmLaneCapSource) LaneCapEntries() []autonomous.LaneCapEntry {
	if s.reg == nil {
		return nil
	}
	reg := s.reg()
	if reg == nil {
		return nil
	}
	var table map[string]int
	if s.table != nil {
		table = s.table()
	}
	var out []autonomous.LaneCapEntry
	for _, l := range reg.List() {
		out = append(out, autonomous.LaneCapEntry{
			Backend: l.Name,
			Enabled: l.Enabled(),
			Cap:     inference.EffectiveMaxInflight(*l, table),
		})
	}
	return out
}

// newLaneFillCapSource builds the cap source over a registry and the live
// plan tier table, both read on every poll so a changed cap applies to the
// next poll.
func newLaneFillCapSource(reg func() *inference.Registry, table func() map[string]int) autonomous.LaneCapSource {
	return llmLaneCapSource{reg: reg, table: table}
}
