package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fake credentials are assembled at runtime from string pieces so no
// real-looking secret is committed anywhere in this repository.

func fakeAWSKey() string { return "AK" + "IA" + strings.Repeat("B", 16) }

func fakeGitHubToken() string { return "gh" + "p_" + strings.Repeat("aB3x", 9) }

func fakeSlackToken() string { return "xox" + "b-" + strings.Repeat("k7", 10) }

const fakePrivateKeyLine = "-----BEGIN OPENSSH " + "PRIVATE KEY-----"

// fakeGenericValue is a 16-char high-entropy assignment value.
func fakeGenericValue() string { return "Xk9" + "mQ2" + "vL7n" + "R4tW8y" }

// fakeGenericPlaceholder is a 16-char low-entropy placeholder that must
// NOT be flagged by the generic detector.
func fakeGenericPlaceholder() string { return strings.Repeat("a", 16) }

// newTempGitRepo creates a temporary git repository with a working
// identity and no remote.
func newTempGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init")
	run("config", "user.name", "test")
	run("config", "user.email", "test@example.invalid")
	run("config", "commit.gpgsign", "false")
	t.Cleanup(func() { run("config", "user.name", "--unset") })
	return dir
}

// commitFile writes content to rel (repository-relative) and commits it.
func commitFile(t *testing.T, dir, rel, content, msg string) {
	t.Helper()
	abs := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("add", "-A")
	run("commit", "-m", msg)
}

// newBareOrigin creates a bare repository and registers it as origin of
// the work repo in dir.
func newBareOrigin(t *testing.T, dir string) string {
	t.Helper()
	bare := t.TempDir() + "/origin.git"
	cmd := exec.Command("git", "init", "--bare", bare)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, out)
	}
	remote := bare
	add := exec.Command("git", "remote", "add", "origin", remote)
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v: %s", err, out)
	}
	return remote
}

func pushAll(t *testing.T, dir, remote string) {
	t.Helper()
	cmd := exec.Command("git", "push", "origin", "HEAD")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git push: %v: %s", err, out)
	}
}

func detectorsOf(findings []SecretFinding) map[string][]SecretFinding {
	out := make(map[string][]SecretFinding)
	for _, f := range findings {
		out[f.Detector] = append(out[f.Detector], f)
	}
	return out
}

func TestScanUnpushedDetectsAllBuiltInDetectors(t *testing.T) {
	dir := newTempGitRepo(t)
	ghp := fakeGitHubToken()
	content := strings.Join([]string{
		"line one",
		"aws_access_key_id = \"" + fakeAWSKey() + "\"",
		fakePrivateKeyLine,
		"github_token: " + ghp,
		"api_key = \"" + fakeGenericValue() + "\"",
		"slack = \"" + fakeSlackToken() + "\"",
		"",
	}, "\n")
	commitFile(t, dir, "config/app.yaml", content, "add config")
	sha := headSHA(context.Background(), dir)

	findings, err := ScanUnpushed(context.Background(), dir, "origin")
	if err != nil {
		t.Fatalf("ScanUnpushed: %v", err)
	}
	byName := detectorsOf(findings)
	for _, want := range AllDetectors {
		if len(byName[want]) == 0 {
			t.Errorf("expected at least one %s finding, got none (findings: %v)", want, findings)
		}
	}
	// Every finding carries the full, secret-free field set.
	for _, f := range findings {
		if f.Commit != sha {
			t.Errorf("finding commit %q != HEAD %q", f.Commit, sha)
		}
		if f.Path != "config/app.yaml" {
			t.Errorf("finding path %q", f.Path)
		}
		if f.Line <= 0 {
			t.Errorf("finding line %d", f.Line)
		}
		if len(f.Fingerprint) != secretFingerprintLen {
			t.Errorf("fingerprint %q has wrong length", f.Fingerprint)
		}
	}
	// Line numbers must be exact.
	if got := byName[DetectorAWSAccessKey][0].Line; got != 2 {
		t.Errorf("aws finding line = %d, want 2", got)
	}
	if got := byName[DetectorSlackToken][0].Line; got != 6 {
		t.Errorf("slack finding line = %d, want 6", got)
	}
}

func TestScanUnpushedFindingNeverCarriesTheSecret(t *testing.T) {
	dir := newTempGitRepo(t)
	secret := fakeGitHubToken()
	commitFile(t, dir, "creds.txt", "token="+secret+"\n", "add creds")

	findings, err := ScanUnpushed(context.Background(), dir, "origin")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) == 0 {
		t.Fatal("expected findings, got none")
	}
	for _, f := range findings {
		rendered := f.String() + f.Detector + f.Commit + f.Path + f.Fingerprint
		if strings.Contains(rendered, secret) {
			t.Fatalf("finding rendered the secret: %q", rendered)
		}
		if f.Fingerprint == secret {
			t.Fatal("fingerprint equals the secret")
		}
	}
}

func TestScanUnpushedDetectorsSelectableByName(t *testing.T) {
	dir := newTempGitRepo(t)
	commitFile(t, dir, "mixed.txt", strings.Join([]string{
		"gh=" + fakeGitHubToken(),
		"aws=" + fakeAWSKey(),
	}, "\n")+"\n", "add creds")

	findings, err := ScanUnpushed(context.Background(), dir, "origin", ScanOptions{
		Detectors: []string{DetectorGitHubToken},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("want exactly 1 github_token finding, got %d: %v", len(findings), findings)
	}
	if findings[0].Detector != DetectorGitHubToken {
		t.Errorf("detector = %q", findings[0].Detector)
	}
}

func TestScanUnpushedUnknownDetectorIsAnError(t *testing.T) {
	dir := newTempGitRepo(t)
	commitFile(t, dir, "a.txt", "hello\n", "init")

	if _, err := ScanUnpushed(context.Background(), dir, "origin", ScanOptions{
		Detectors: []string{"no_such_detector"},
	}); err == nil {
		t.Fatal("expected an error for an unknown detector name")
	}
}

func TestScanUnpushedScansOnlyUnpushedCommits(t *testing.T) {
	dir := newTempGitRepo(t)
	commitFile(t, dir, "clean.txt", "nothing sensitive\n", "clean commit")
	newBareOrigin(t, dir)
	pushAll(t, dir, "origin")

	before, err := ScanUnpushed(context.Background(), dir, "origin")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 0 {
		t.Fatalf("want no findings once pushed, got %v", before)
	}

	commitFile(t, dir, "creds.txt", "ghp="+fakeGitHubToken()+"\n", "leak")
	sha2 := headSHA(context.Background(), dir)
	findings, err := ScanUnpushed(context.Background(), dir, "origin")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding on the new commit, got %d: %v", len(findings), findings)
	}
	if findings[0].Commit != sha2 {
		t.Errorf("finding commit %q != new HEAD %q", findings[0].Commit, sha2)
	}
	if findings[0].Path != "creds.txt" {
		t.Errorf("finding path %q", findings[0].Path)
	}
}

func TestScanUnpushedPathAllowList(t *testing.T) {
	dir := newTempGitRepo(t)
	ghp := fakeGitHubToken()
	commitFile(t, dir, "vendor/lib.js", "export const token = '"+ghp+"';\n", "vendor")
	commitFile(t, dir, "app.js", "const token = '"+ghp+"';\n", "app")

	findings, err := ScanUnpushed(context.Background(), dir, "origin", ScanOptions{
		PathPatterns: []string{"vendor/*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) == 0 {
		t.Fatalf("want findings for app.js, got none")
	}
	for _, f := range findings {
		if f.Path == "vendor/lib.js" {
			t.Fatalf("allow-listed vendor path still reported: %v", f)
		}
		if f.Path != "app.js" {
			t.Errorf("path = %q", f.Path)
		}
	}
}

func TestScanUnpushedFingerprintAllowList(t *testing.T) {
	dir := newTempGitRepo(t)
	commitFile(t, dir, "creds.txt", "ghp="+fakeGitHubToken()+"\n", "leak")
	commitFile(t, dir, "other.txt", "xox="+fakeSlackToken()+"\n", "slack")

	all, err := ScanUnpushed(context.Background(), dir, "origin")
	if err != nil {
		t.Fatal(err)
	}
	var ghFinding SecretFinding
	for _, f := range all {
		if f.Detector == DetectorGitHubToken {
			ghFinding = f
		}
	}
	if ghFinding.Fingerprint == "" {
		t.Fatal("no github_token finding to allow-list")
	}

	remaining, err := ScanUnpushed(context.Background(), dir, "origin", ScanOptions{
		Fingerprints: []string{ghFinding.Fingerprint},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range remaining {
		if f.Detector == DetectorGitHubToken {
			t.Fatalf("allow-listed fingerprint still reported: %v", f)
		}
	}
	if len(remaining) == 0 {
		t.Fatalf("expected the slack finding to remain, got none")
	}
}

func TestScanUnpushedGenericDetectorRequiresEntropy(t *testing.T) {
	dir := newTempGitRepo(t)
	commitFile(t, dir, "placeholder.txt",
		"token = \""+fakeGenericPlaceholder()+"\"\n", "placeholder")

	findings, err := ScanUnpushed(context.Background(), dir, "origin", ScanOptions{
		Detectors: []string{DetectorGenericToken},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("low-entropy placeholder must not be flagged, got %v", findings)
	}

	commitFile(t, dir, "real.txt",
		"token = \""+fakeGenericValue()+"\"\n", "real")
	findings, err = ScanUnpushed(context.Background(), dir, "origin", ScanOptions{
		Detectors: []string{DetectorGenericToken},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("high-entropy assignment must be flagged, got %v", findings)
	}
}

func TestScanUnpushedFingerprintIsDeterministic(t *testing.T) {
	a := fakeGitHubToken()
	b := "gh" + "p_" + strings.Repeat("aB3x", 9)
	fa, _ := secretFingerprint(a)
	fb, _ := secretFingerprint(b)
	if fa != fb {
		t.Errorf("same secret -> different fingerprints: %q vs %q", fa, fb)
	}
	if fa == a {
		t.Error("fingerprint equals the secret")
	}
	if len(fa) != 16 {
		t.Errorf("fingerprint length = %d, want 16", len(fa))
	}
}
