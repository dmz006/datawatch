package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
)

func TestLaneFillSessionListerKeepsRunningLocalAndRemote(t *testing.T) {
	l := federationSessionLister{sessions: func() server.FederationResponse {
		return server.FederationResponse{
			Primary: []*session.Session{
				{FullID: "h-1", State: session.StateRunning, BackendFamily: "pi-prompt"},
				{FullID: "h-2", State: session.StateComplete, BackendFamily: "pi-prompt"},
				nil,
			},
			Proxied: map[string][]*session.Session{
				"peer1": {{FullID: "p-1", State: session.StateRunning, BackendFamily: "opencode", ComputeNodeRef: "gpu-box"}},
			},
			Errors: map[string]string{"peer2": "transport: timeout"},
		}
	}}
	got, faults := l.RunningSessions(context.Background())
	if len(got) != 2 {
		t.Fatalf("sessions = %+v, want the two running ones", got)
	}
	var remote bool
	for _, s := range got {
		if s.ID == "p-1" {
			remote = s.Host == "peer1" && s.Node == "gpu-box" && s.Backend == "opencode"
		}
	}
	if !remote {
		t.Errorf("remote session lost its host or node: %+v", got)
	}
	if len(faults) != 1 || faults[0].Host != "peer2" {
		t.Errorf("faults = %+v", faults)
	}
}

func TestLaneFillPoolHostsListsOllamaNodesAndLocalOnce(t *testing.T) {
	reg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []*compute.Node{
		{Name: "gpu-box", Kind: compute.KindOllama, Address: "http://192.0.2.5:11434/"},
		{Name: "off", Kind: compute.KindOllama, Address: "http://192.0.2.6:11434", Disabled: true},
		{Name: "api", Kind: compute.KindOpenAICompat, Address: "http://192.0.2.7:8000"},
	} {
		if err := reg.Add(n); err != nil {
			t.Fatal(err)
		}
	}
	h := computePoolHosts{reg: reg, localHost: func() string { return "http://192.0.2.5:11434" }}
	got := h.PoolHosts(context.Background())
	if len(got) != 1 || got[0].Name != "gpu-box" || got[0].Node != "gpu-box" || got[0].Endpoint != "http://192.0.2.5:11434" {
		t.Fatalf("hosts = %+v, want gpu-box once", got)
	}

	h.localHost = func() string { return "http://localhost:11434" }
	if got := h.PoolHosts(context.Background()); len(got) != 2 || got[1].Name != "localhost" || got[1].Node != "" {
		t.Fatalf("hosts = %+v, want gpu-box and the local host", got)
	}
}

func newProposalReaderRepo(t *testing.T) (repo, outside string, rd repoProposalReader) {
	t.Helper()
	base := t.TempDir()
	repo = filepath.Join(base, "repo")
	outside = filepath.Join(base, "outside")
	for _, d := range []string{filepath.Join(repo, "docs", "plans"), outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return repo, outside, repoProposalReader{root: func() string { return repo }}
}

func TestLaneFillProposalReaderReadsUnderTheRepository(t *testing.T) {
	repo, _, rd := newProposalReaderRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "docs", "plans", "a.md"), []byte("Confirmed: yes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := rd.ReadHead("docs/plans/a.md")
	if err != nil || !strings.Contains(string(got), "Confirmed: yes") {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := rd.ReadHead("docs/plans/none.md"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing file must wrap fs.ErrNotExist, got %v", err)
	}
}

func TestLaneFillProposalReaderLimitsTheHead(t *testing.T) {
	repo, _, rd := newProposalReaderRepo(t)
	big := strings.Repeat("x", laneFillProposalHeadLimit+5000)
	if err := os.WriteFile(filepath.Join(repo, "docs", "plans", "big.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := rd.ReadHead("docs/plans/big.md")
	if err != nil || len(got) != laneFillProposalHeadLimit {
		t.Fatalf("len = %d err %v, want %d", len(got), err, laneFillProposalHeadLimit)
	}
}

func TestLaneFillProposalReaderRefusesParentAbsoluteAndEscapingLinks(t *testing.T) {
	repo, outside, rd := newProposalReaderRepo(t)
	secret := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(secret, []byte("Confirmed: yes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A symbolic link inside the repository that points at a file outside it,
	// and one that points at a directory outside it.
	if err := os.Symlink(secret, filepath.Join(repo, "docs", "plans", "link.md")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "docs", "plans", "dir")); err != nil {
		t.Fatal(err)
	}
	// A link that stays inside the repository is fine.
	inside := filepath.Join(repo, "docs", "plans", "real.md")
	if err := os.WriteFile(inside, []byte("Confirmed: yes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(inside, filepath.Join(repo, "docs", "plans", "ok.md")); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"docs/plans/../../../outside/secret.md",
		"../outside/secret.md",
		secret,
		"/etc/passwd",
		"docs/plans/link.md",
		"docs/plans/dir/secret.md",
	} {
		got, err := rd.ReadHead(rel)
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%q: err = %v, want not-exist", rel, err)
		}
		if len(got) != 0 {
			t.Errorf("%q: read %q from outside the repository", rel, got)
		}
	}
	if got, err := rd.ReadHead("docs/plans/ok.md"); err != nil || !strings.Contains(string(got), "Confirmed") {
		t.Errorf("a link inside the repository must read: %q %v", got, err)
	}
}

func TestLaneFillProposalReaderWithoutBaseRepositoryIsNotAFileNotFound(t *testing.T) {
	rd := repoProposalReader{root: func() string { return "" }}
	_, err := rd.ReadHead("docs/plans/a.md")
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want a failure that is not file-not-found (the rule then fails open)", err)
	}
}

func TestLaneFillProposalReaderUnreadableForOtherReasonsIsNotNotFound(t *testing.T) {
	repo, _, rd := newProposalReaderRepo(t)
	// A directory named like a proposal file is not a regular file; it is
	// reported as not readable, never as content.
	if err := os.Mkdir(filepath.Join(repo, "docs", "plans", "d.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := rd.ReadHead("docs/plans/d.md"); err == nil || len(got) != 0 {
		t.Fatalf("got %q err %v", got, err)
	}
}

// laneFillGit runs git in dir with a fixed identity and the multiplexer
// variables removed, and fails the test on error.
func laneFillGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Lane Fill", "-c", "user.email=lane-fill@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_TMPDIR=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func laneFillCommitFile(t *testing.T, dir, name, subject string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	laneFillGit(t, dir, "add", name)
	laneFillGit(t, dir, "commit", "-q", "-m", subject)
}

// newLaneFillProbeRepo builds a throwaway repository on branch main with one
// commit and returns it with the real probe over it.
func newLaneFillProbeRepo(t *testing.T) (string, *repoMainProbe) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	laneFillGit(t, repo, "init", "-q", "-b", "main")
	laneFillCommitFile(t, repo, "base.txt", "base commit")
	return repo, newLaneFillMainProbe(nil).withRoot(func() string { return repo })
}

func (p *repoMainProbe) withRoot(root func() string) *repoMainProbe {
	p.root = root
	return p
}

func TestLaneFillRealProbeMergeCommitForTheIDOnMainCarries(t *testing.T) {
	repo, p := newLaneFillProbeRepo(t)
	laneFillGit(t, repo, "commit", "-q", "--allow-empty", "-m", "Merge branch 'automaton/aaaa0001-thing'")

	carries, evidence, err := p.Carries(context.Background(), "aaaa0001", "")
	if err != nil || !carries {
		t.Fatalf("carries = %v err = %v, want true", carries, err)
	}
	if !strings.Contains(evidence, "automaton/aaaa0001") || !strings.Contains(evidence, "main") {
		t.Errorf("evidence %q does not name the commit and the branch", evidence)
	}
	// Another id is not carried.
	if carries, _, err := p.Carries(context.Background(), "aaaa0002", ""); err != nil || carries {
		t.Errorf("another id: carries = %v err = %v, want false", carries, err)
	}
}

func TestLaneFillRealProbeTaskCommitForTheIDOnMainCarries(t *testing.T) {
	repo, p := newLaneFillProbeRepo(t)
	laneFillCommitFile(t, repo, "t.txt", "task(bbbb0001): Add the thing")

	carries, evidence, err := p.Carries(context.Background(), "bbbb0001", "")
	if err != nil || !carries || !strings.Contains(evidence, "task(bbbb0001)") {
		t.Fatalf("carries = %v evidence %q err = %v", carries, evidence, err)
	}
}

func TestLaneFillRealProbeIDOnlyInACommitBodyDoesNotCarry(t *testing.T) {
	repo, p := newLaneFillProbeRepo(t)
	laneFillGit(t, repo, "commit", "-q", "--allow-empty", "-m", "Unrelated change", "-m", "mentions automaton/cccc0001 in the body only")

	if carries, _, err := p.Carries(context.Background(), "cccc0001", ""); err != nil || carries {
		t.Fatalf("carries = %v err = %v, want false: the rule reads subjects", carries, err)
	}
}

// laneFillBranchWithCommits creates branch from main with one commit per
// subject and returns to main.
func laneFillBranchWithCommits(t *testing.T, repo, branch string, subjects ...string) {
	t.Helper()
	laneFillGit(t, repo, "checkout", "-q", "-b", branch, "main")
	for i, s := range subjects {
		laneFillCommitFile(t, repo, fmt.Sprintf("%s-%d.txt", strings.ReplaceAll(branch, "/", "_"), i), s)
	}
	laneFillGit(t, repo, "checkout", "-q", "main")
}

func TestLaneFillRealProbeBranchFullyOnMainByPatchIDCarries(t *testing.T) {
	repo, p := newLaneFillProbeRepo(t)
	laneFillBranchWithCommits(t, repo, "automaton/dddd0001-work", "add the first piece", "add the second piece")
	laneFillCommitFile(t, repo, "main-only.txt", "something else on main")
	// Land both commits on main by cherry-pick: new hashes, same patches.
	laneFillGit(t, repo, "cherry-pick", "automaton/dddd0001-work~1", "automaton/dddd0001-work")

	// Named branch.
	carries, evidence, err := p.Carries(context.Background(), "dddd0001", "automaton/dddd0001-work")
	if err != nil || !carries {
		t.Fatalf("carries = %v err = %v, want true", carries, err)
	}
	if !strings.Contains(evidence, "automaton/dddd0001-work") || !strings.Contains(evidence, "2 commit") {
		t.Errorf("evidence %q does not name the branch and the comparison", evidence)
	}
	// No recorded branch: found by its automaton/<id> prefix.
	if carries, _, err := p.Carries(context.Background(), "dddd0001", ""); err != nil || !carries {
		t.Errorf("branch found by prefix: carries = %v err = %v", carries, err)
	}
}

func TestLaneFillRealProbeBranchPartlyOnMainDoesNotCarry(t *testing.T) {
	repo, p := newLaneFillProbeRepo(t)
	laneFillBranchWithCommits(t, repo, "automaton/eeee0001-work", "add the first piece", "add the second piece")
	laneFillCommitFile(t, repo, "main-only.txt", "something else on main")
	laneFillGit(t, repo, "cherry-pick", "automaton/eeee0001-work~1") // only the first

	carries, evidence, err := p.Carries(context.Background(), "eeee0001", "automaton/eeee0001-work")
	if err != nil || carries {
		t.Fatalf("carries = %v evidence %q err = %v, want false", carries, evidence, err)
	}
}

func TestLaneFillRealProbeBranchAheadOfMainDoesNotCarry(t *testing.T) {
	repo, p := newLaneFillProbeRepo(t)
	laneFillBranchWithCommits(t, repo, "automaton/ffff0001-work", "brand new work")

	if carries, _, err := p.Carries(context.Background(), "ffff0001", ""); err != nil || carries {
		t.Fatalf("carries = %v err = %v, want false", carries, err)
	}
}

func TestLaneFillRealProbeNoBranchAndNoCommitsDoesNotCarry(t *testing.T) {
	_, p := newLaneFillProbeRepo(t)
	if carries, evidence, err := p.Carries(context.Background(), "abcd0001", ""); err != nil || carries || evidence != "" {
		t.Fatalf("carries = %v evidence %q err = %v, want a clean no", carries, evidence, err)
	}
	// A recorded branch that is not in the repository is not an error.
	if carries, _, err := p.Carries(context.Background(), "abcd0001", "automaton/abcd0001-gone"); err != nil || carries {
		t.Fatalf("missing branch: carries = %v err = %v", carries, err)
	}
}

func TestLaneFillRealProbeErrorsAreUnknownNotAnAnswer(t *testing.T) {
	// No base repository.
	p := newLaneFillMainProbe(nil)
	if _, _, err := p.Carries(context.Background(), "abcd0001", ""); err == nil {
		t.Error("no base repository: want an error")
	}
	// A directory that is not a repository.
	plain := t.TempDir()
	p = p.withRoot(func() string { return plain })
	if carries, _, err := p.Carries(context.Background(), "abcd0001", ""); err == nil || carries {
		t.Errorf("not a repository: carries = %v err = %v, want an error", carries, err)
	}
	// An id that is not an Automaton id never reaches git.
	repo, p2 := newLaneFillProbeRepo(t)
	_ = repo
	if _, _, err := p2.Carries(context.Background(), "--all", ""); err == nil {
		t.Error("a malformed id: want an error")
	}
	// A cancelled context ends the call with an error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if carries, _, err := p2.Carries(ctx, "abcd0001", ""); err == nil || carries {
		t.Errorf("cancelled context: carries = %v err = %v, want an error", carries, err)
	}
}

func TestLaneFillRealProbeMainHeadMovesWithTheBaseBranch(t *testing.T) {
	repo, p := newLaneFillProbeRepo(t)
	h1, err := p.MainHead(context.Background())
	if err != nil || len(h1) < 7 {
		t.Fatalf("head = %q err = %v", h1, err)
	}
	if again, _ := p.MainHead(context.Background()); again != h1 {
		t.Errorf("head changed with no commit: %q then %q", h1, again)
	}
	laneFillCommitFile(t, repo, "next.txt", "next")
	h2, err := p.MainHead(context.Background())
	if err != nil || h2 == h1 {
		t.Errorf("head = %q err = %v, want a new head after a commit (was %q)", h2, err, h1)
	}
}

func TestLaneFillRealProbeReadsOnlyAndLeavesTheRepositoryAlone(t *testing.T) {
	repo, p := newLaneFillProbeRepo(t)
	laneFillBranchWithCommits(t, repo, "automaton/9999aaaa-work", "some work")
	before := laneFillGit(t, repo, "for-each-ref", "--format=%(refname) %(objectname)") + "\n" + laneFillGit(t, repo, "status", "--porcelain")
	for i := 0; i < 2; i++ {
		if _, _, err := p.Carries(context.Background(), "9999aaaa", ""); err != nil {
			t.Fatal(err)
		}
	}
	after := laneFillGit(t, repo, "for-each-ref", "--format=%(refname) %(objectname)") + "\n" + laneFillGit(t, repo, "status", "--porcelain")
	if before != after {
		t.Errorf("the probe changed the repository:\nbefore %s\nafter  %s", before, after)
	}
}

// The real probe and the real worktree view, over one repository, through the
// pure checks lane fill applies.
func TestLaneFillRealWorktreeViewRegisteredAndUnregisteredDirectories(t *testing.T) {
	repo, _ := newLaneFillProbeRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	laneFillGit(t, repo, "worktree", "add", "-q", "-b", "automaton/1111aaaa-work", wt, "main")
	fsys := autonomous.NewShellSupervisorWatchWorktreeFS()
	list := func() ([]string, error) { return fsys.RegisteredWorktrees(repo) }

	if state, problem := autonomous.CheckLaneFillWorktree(wt, list); state != autonomous.LaneFillWorktreeValid {
		t.Errorf("a registered worktree: state %q problem %q", state, problem)
	}
	// A standalone repository has a .git entry but is not in the list.
	other := filepath.Join(t.TempDir(), "standalone")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	laneFillGit(t, other, "init", "-q", "-b", "main")
	if state, _ := autonomous.CheckLaneFillWorktree(other, list); state != autonomous.LaneFillWorktreeInvalid {
		t.Errorf("an unregistered repository: state %q, want invalid", state)
	}
	// A pruned (removed) worktree directory.
	laneFillGit(t, repo, "worktree", "remove", "--force", wt)
	if state, _ := autonomous.CheckLaneFillWorktree(wt, list); state != autonomous.LaneFillWorktreeInvalid {
		t.Errorf("a removed worktree: state %q, want invalid", state)
	}
	// A list that cannot be read is unknown.
	bad := func() ([]string, error) { return fsys.RegisteredWorktrees(t.TempDir()) }
	if state, _ := autonomous.CheckLaneFillWorktree(other, bad); state != autonomous.LaneFillWorktreeUnknown {
		t.Errorf("an unreadable list: state %q, want unknown", state)
	}
}

func TestLaneFillCapSourceFollowsRegistryCaps(t *testing.T) {
	reg := inference.NewRegistry()
	for _, l := range []inference.LLM{
		{Name: "claude-code", Kind: inference.KindClaudeCode},                // subscription kind: 1
		{Name: "claude-pro", Kind: inference.KindClaudeCode, MaxInflight: 4}, // explicit cap
		{Name: "pi-prompt", Kind: inference.KindPiPrompt, Disabled: true},
		{Name: "ollama-local", Kind: inference.KindOllama}, // local kind: unlimited
	} {
		l := l
		if err := reg.Add(&l); err != nil {
			t.Fatalf("add %s: %v", l.Name, err)
		}
	}
	table := map[string]int{}
	src := newLaneFillCapSource(func() *inference.Registry { return reg }, func() map[string]int { return table })

	byBackend := map[string]autonomous.LaneCapEntry{}
	for _, e := range src.LaneCapEntries() {
		byBackend[e.Backend] = e
	}
	// The source reports exactly the number the capacity ledger limit is set from.
	for _, l := range reg.List() {
		e, ok := byBackend[l.Name]
		if !ok {
			t.Fatalf("no entry for %s", l.Name)
		}
		if want := inference.EffectiveMaxInflight(*l, table); e.Cap != want {
			t.Errorf("%s cap = %d, want %d", l.Name, e.Cap, want)
		}
		if e.Enabled != !l.Disabled {
			t.Errorf("%s enabled = %v", l.Name, e.Enabled)
		}
	}
	if byBackend["claude-code"].Cap != 1 || byBackend["claude-pro"].Cap != 4 || byBackend["ollama-local"].Cap != 0 {
		t.Errorf("caps = %+v", byBackend)
	}

	// A lowered cap shows on the next read of the registry.
	cc, _ := reg.Get("claude-code")
	cc.MaxInflight = 3
	if err := reg.Update(cc); err != nil {
		t.Fatal(err)
	}
	for _, e := range src.LaneCapEntries() {
		if e.Backend == "claude-code" && e.Cap != 3 {
			t.Errorf("claude-code cap after update = %d, want 3", e.Cap)
		}
	}
}

func TestLaneFillCapSourceNilRegistryIsEmpty(t *testing.T) {
	if got := newLaneFillCapSource(nil, nil).LaneCapEntries(); len(got) != 0 {
		t.Errorf("entries = %v", got)
	}
	src := newLaneFillCapSource(func() *inference.Registry { return nil }, nil)
	if got := src.LaneCapEntries(); len(got) != 0 {
		t.Errorf("entries = %v", got)
	}
}

// TestLaneFillPoolEvidenceTLSRegistryNodeSettingsApply checks the real wiring:
// the pool hosts come from the compute registry, the model fetcher reads each
// node with that node's own TLS settings, and an edit to the node record
// applies to the next read.
func TestLaneFillPoolEvidenceTLSRegistryNodeSettingsApply(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3.8:27b"}]}`))
	}))
	defer srv.Close()
	sum := sha256.Sum256(srv.Certificate().Raw)
	reg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	node := &compute.Node{Name: "gpu-box", Kind: compute.KindOllama, Address: srv.URL}
	if err := reg.Add(node); err != nil {
		t.Fatal(err)
	}
	fetch := autonomous.NewHTTPModelFetcherForNodes(nil, registryNodeLookup(reg))
	hosts := computePoolHosts{reg: reg}.PoolHosts(context.Background())
	if len(hosts) != 1 {
		t.Fatalf("hosts = %+v", hosts)
	}

	_, err = fetch.LoadedModels(context.Background(), hosts[0])
	if err == nil || !strings.Contains(err.Error(), "gpu-box") || !strings.Contains(err.Error(), "tls_cert_fingerprint") ||
		strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("err = %v, want the node and the fix, without the endpoint", err)
	}

	node.TLSCertFingerprint = hex.EncodeToString(sum[:])
	if err := reg.Update(node); err != nil {
		t.Fatal(err)
	}
	got, err := fetch.LoadedModels(context.Background(), hosts[0])
	if err != nil || len(got) != 1 {
		t.Fatalf("after pinning the certificate: models = %v, err = %v", got, err)
	}

	if registryNodeLookup(nil) != nil {
		t.Error("a nil registry resolved nodes")
	}
}
