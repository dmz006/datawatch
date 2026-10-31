package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
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
		{Name: "gpu-box", Kind: compute.KindOllama, Address: "http://10.0.0.5:11434/"},
		{Name: "off", Kind: compute.KindOllama, Address: "http://10.0.0.6:11434", Disabled: true},
		{Name: "api", Kind: compute.KindOpenAICompat, Address: "http://10.0.0.7:8000"},
	} {
		if err := reg.Add(n); err != nil {
			t.Fatal(err)
		}
	}
	h := computePoolHosts{reg: reg, localHost: func() string { return "http://10.0.0.5:11434" }}
	got := h.PoolHosts(context.Background())
	if len(got) != 1 || got[0].Name != "gpu-box" || got[0].Endpoint != "http://10.0.0.5:11434" {
		t.Fatalf("hosts = %+v, want gpu-box once", got)
	}

	h.localHost = func() string { return "http://localhost:11434" }
	if got := h.PoolHosts(context.Background()); len(got) != 2 || got[1].Name != "localhost" {
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
