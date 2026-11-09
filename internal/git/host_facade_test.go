package git

import (
	"context"
	"errors"
	"testing"
)

// fakeForge records the calls it receives and returns a fixed URL.
type fakeForge struct {
	kind  string
	calls []string
	err   error
}

func (f *fakeForge) OpenPR(_ context.Context, opts PROptions) (string, error) {
	f.calls = append(f.calls, "OpenPR:"+opts.HeadBranch)
	return "https://" + f.kind + ".example.test/pr/1", f.err
}

func (f *fakeForge) CreateIssue(_ context.Context, opts IssueOptions) (string, error) {
	f.calls = append(f.calls, "CreateIssue:"+opts.Title)
	return "https://" + f.kind + ".example.test/issue/1", f.err
}

func (f *fakeForge) CreateRelease(_ context.Context, opts ReleaseOptions) (string, error) {
	f.calls = append(f.calls, "CreateRelease:"+opts.Tag)
	return "https://" + f.kind + ".example.test/release/1", f.err
}

// withFakeForges swaps forgeFactory for one that hands out a fakeForge per
// kind, and returns the fakes keyed by kind. The real factory is restored
// when the test ends.
func withFakeForges(t *testing.T) map[string]*fakeForge {
	t.Helper()
	fakes := map[string]*fakeForge{
		hostKindGitHub: {kind: "github"},
		hostKindGitLab: {kind: "gitlab"},
	}
	prev := forgeFactory
	forgeFactory = func(host GitHostConfig) hostForge {
		if f, ok := fakes[host.Kind]; ok {
			return f
		}
		return prev(host)
	}
	t.Cleanup(func() { forgeFactory = prev })
	return fakes
}

func TestHostFacade(t *testing.T) {
	configured := []GitHostConfig{
		{Name: "work-gitea", Kind: hostKindGitea, BaseURL: "https://git.work.example.test"},
		{Name: "corp-gitlab", Kind: hostKindGitLab, BaseURL: "https://gitlab.corp.example.test:8443"},
		{Name: "github-empty", Kind: hostKindGitHub},
	}

	t.Run("resolves configured host by base URL", func(t *testing.T) {
		cases := map[string]string{
			"https://git.work.example.test/team/repo.git": "work-gitea",
			"git@git.work.example.test:team/repo.git":     "work-gitea",
			"ssh://git@GIT.WORK.EXAMPLE.TEST/team/repo":   "work-gitea",
			"https://gitlab.corp.example.test/g/p.git":    "corp-gitlab",
		}
		for remote, want := range cases {
			got, ok := ResolveProjectHost(configured, remote)
			if !ok {
				t.Fatalf("ResolveProjectHost(%q): no match, want %q", remote, want)
			}
			if got.Name != want {
				t.Errorf("ResolveProjectHost(%q) = %q, want %q", remote, got.Name, want)
			}
		}
	})

	t.Run("configured host with empty base URL is skipped", func(t *testing.T) {
		// github-empty has no base URL, so it never matches by base URL;
		// the built-in github.com default answers instead.
		got, ok := ResolveProjectHost(configured, "https://github.com/o/r.git")
		if !ok || got.Name != hostKindGitHub || got.Kind != hostKindGitHub {
			t.Fatalf("got %+v ok=%v, want built-in github default", got, ok)
		}
	})

	t.Run("falls back to built-in defaults", func(t *testing.T) {
		got, ok := ResolveProjectHost(nil, "git@github.com:o/r.git")
		if !ok || got.Kind != hostKindGitHub || got.Name != "github" {
			t.Errorf("github default: got %+v ok=%v", got, ok)
		}
		got, ok = ResolveProjectHost(nil, "https://gitlab.com/g/p.git")
		if !ok || got.Kind != hostKindGitLab || got.Name != "gitlab" {
			t.Errorf("gitlab default: got %+v ok=%v", got, ok)
		}
	})

	t.Run("no match", func(t *testing.T) {
		for _, remote := range []string{
			"https://unknown.example.test/o/r.git",
			"",
			"   ",
			"not-a-remote",
		} {
			if got, ok := ResolveProjectHost(configured, remote); ok {
				t.Errorf("ResolveProjectHost(%q) = %+v, want no match", remote, got)
			}
		}
	})

	t.Run("dispatches to github fake", func(t *testing.T) {
		fakes := withFakeForges(t)
		host := GitHostConfig{Name: "github", Kind: hostKindGitHub}
		ctx := context.Background()

		pr, err := OpenChangeRequest(ctx, host, PROptions{HeadBranch: "feat"})
		if err != nil || pr.URL != "https://github.example.test/pr/1" || pr.Kind != "github" || pr.Host != "github" {
			t.Errorf("OpenChangeRequest = %+v, %v", pr, err)
		}
		iss, err := FileIssue(ctx, host, IssueOptions{Title: "bug"})
		if err != nil || iss.URL != "https://github.example.test/issue/1" || iss.Kind != "github" {
			t.Errorf("FileIssue = %+v, %v", iss, err)
		}
		rel, err := PublishRelease(ctx, host, ReleaseOptions{Tag: "v1.0.0"})
		if err != nil || rel.URL != "https://github.example.test/release/1" || rel.Host != "github" {
			t.Errorf("PublishRelease = %+v, %v", rel, err)
		}
		want := []string{"OpenPR:feat", "CreateIssue:bug", "CreateRelease:v1.0.0"}
		if got := fakes[hostKindGitHub].calls; len(got) != len(want) {
			t.Fatalf("github calls = %v, want %v", got, want)
		} else {
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("github call %d = %q, want %q", i, got[i], want[i])
				}
			}
		}
		if n := len(fakes[hostKindGitLab].calls); n != 0 {
			t.Errorf("gitlab fake received %d calls, want 0", n)
		}
	})

	t.Run("dispatches to gitlab fake", func(t *testing.T) {
		fakes := withFakeForges(t)
		host := GitHostConfig{Name: "corp-gitlab", Kind: hostKindGitLab, BaseURL: "https://gitlab.corp.example.test"}
		ctx := context.Background()

		pr, err := OpenChangeRequest(ctx, host, PROptions{HeadBranch: "fix"})
		if err != nil || pr.Kind != "gitlab" || pr.Host != "corp-gitlab" || pr.URL == "" {
			t.Errorf("OpenChangeRequest = %+v, %v", pr, err)
		}
		if _, err := FileIssue(ctx, host, IssueOptions{Title: "t"}); err != nil {
			t.Errorf("FileIssue: %v", err)
		}
		if _, err := PublishRelease(ctx, host, ReleaseOptions{Tag: "v2"}); err != nil {
			t.Errorf("PublishRelease: %v", err)
		}
		if n := len(fakes[hostKindGitLab].calls); n != 3 {
			t.Errorf("gitlab fake calls = %d, want 3", n)
		}
		if n := len(fakes[hostKindGitHub].calls); n != 0 {
			t.Errorf("github fake received %d calls, want 0", n)
		}
	})

	t.Run("propagates fake errors", func(t *testing.T) {
		fakes := withFakeForges(t)
		boom := errors.New("boom")
		fakes[hostKindGitHub].err = boom
		host := GitHostConfig{Name: "github", Kind: hostKindGitHub}
		if _, err := FileIssue(context.Background(), host, IssueOptions{}); !errors.Is(err, boom) {
			t.Errorf("FileIssue err = %v, want boom", err)
		}
	})

	t.Run("plain git kinds return ErrNoForgeAPI", func(t *testing.T) {
		withFakeForges(t)
		ctx := context.Background()
		for _, kind := range []string{hostKindGit, hostKindGitea, hostKindBitbucket} {
			host := GitHostConfig{Name: "plain-" + kind, Kind: kind, BaseURL: "https://plain.example.test"}
			if _, err := OpenChangeRequest(ctx, host, PROptions{}); !errors.Is(err, ErrNoForgeAPI) {
				t.Errorf("%s OpenChangeRequest err = %v, want ErrNoForgeAPI", kind, err)
			}
			if _, err := FileIssue(ctx, host, IssueOptions{}); !errors.Is(err, ErrNoForgeAPI) {
				t.Errorf("%s FileIssue err = %v, want ErrNoForgeAPI", kind, err)
			}
			res, err := PublishRelease(ctx, host, ReleaseOptions{})
			if !errors.Is(err, ErrNoForgeAPI) {
				t.Errorf("%s PublishRelease err = %v, want ErrNoForgeAPI", kind, err)
			}
			if res.Kind != kind || res.Host != host.Name || res.URL != "" {
				t.Errorf("%s PublishRelease result = %+v", kind, res)
			}
		}
	})
}
