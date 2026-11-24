package server

import (
	"os/exec"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// A merge Runner built with the configured git hosts carries them, plus the
// project's origin URL, so the post-ship steps that leave the machine go
// through the host facade for a remote that belongs to a configured host.
func TestMergeRunnerGitHostsOption_WiresHostsAndRemote(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", "git@git.example.test:acme/p.git"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	hosts := []config.GitHostConfig{{
		Name: "lab", Kind: "gitea", BaseURL: "https://git.example.test",
		CredentialSource: "vault", VaultSecret: "lab-token", SignTags: true, SigningKey: "ABCD1234",
	}}
	r := newMergeRunner(&mergeRunSpy{}, config.MergePipelineConfig{}, repo, "demo", t.TempDir(), nil, withGitHosts(hosts))
	if r.RemoteURL != "git@git.example.test:acme/p.git" {
		t.Errorf("RemoteURL = %q, want the repository's origin URL", r.RemoteURL)
	}
	if len(r.Hosts) != 1 {
		t.Fatalf("Hosts = %+v, want the one configured host", r.Hosts)
	}
	h := r.Hosts[0]
	if h.Name != "lab" || h.Kind != "gitea" || h.CredentialSource != "vault" || h.VaultSecret != "lab-token" || !h.SignTags || h.SigningKey != "ABCD1234" {
		t.Errorf("host not copied in full: %+v", h)
	}
}

// With no git.hosts configured the Runner is left exactly as built, so a
// daemon that never configures a host keeps the gh path.
func TestMergeRunnerGitHostsOption_NoHostsLeavesRunnerUntouched(t *testing.T) {
	r := newMergeRunner(&mergeRunSpy{}, config.MergePipelineConfig{}, t.TempDir(), "demo", t.TempDir(), nil, withGitHosts(nil))
	if len(r.Hosts) != 0 || r.RemoteURL != "" {
		t.Errorf("Hosts=%+v RemoteURL=%q, want both empty", r.Hosts, r.RemoteURL)
	}
}

// A repository with no origin keeps the existing path even when hosts are set.
func TestMergeRunnerGitHostsOption_NoOriginKeepsGhPath(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	hosts := []config.GitHostConfig{{Name: "lab", Kind: "gitea", BaseURL: "https://git.example.test"}}
	r := newMergeRunner(&mergeRunSpy{}, config.MergePipelineConfig{}, repo, "demo", t.TempDir(), nil, withGitHosts(hosts))
	if r.RemoteURL != "" {
		t.Errorf("RemoteURL = %q, want empty for a repository with no origin", r.RemoteURL)
	}
}
