package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── Name validation ─────────────────────────────────────────────────────

func TestValidateName(t *testing.T) {
	cases := []struct {
		name    string
		want_ok bool
	}{
		{"", false},
		{"a", true},
		{"my-profile", true},
		{"datawatch-app", true},
		{"MyProfile", false},          // uppercase
		{"-leading", false},           // leading hyphen
		{"trailing-", false},          // trailing hyphen
		{"has.dots", false},
		{"has_underscore", false},
		{"has space", false},
		{strings.Repeat("a", 63), true},
		{strings.Repeat("a", 64), false},
	}
	for _, c := range cases {
		got := ValidateName(c.name)
		if c.want_ok && got != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", c.name, got)
		}
		if !c.want_ok && got == nil {
			t.Errorf("ValidateName(%q) = nil, want error", c.name)
		}
	}
}

// ── ProjectProfile validation ───────────────────────────────────────────

func validProject() *ProjectProfile {
	return &ProjectProfile{
		Name: "test-proj",
		Git: GitSpec{
			Provider: "github",
			URL:      "https://github.com/example/repo",
			Branch:   "main",
		},
		ImagePair: ImagePair{
			Agent:   "agent-claude",
			Sidecar: "lang-go",
		},
		Memory: MemorySpec{Mode: MemorySyncBack},
	}
}

func TestProjectProfile_Validate_Ok(t *testing.T) {
	if err := validProject().Validate(); err != nil {
		t.Fatalf("valid profile rejected: %v", err)
	}
}

func TestProjectProfile_Validate_Errors(t *testing.T) {
	cases := map[string]func(*ProjectProfile){
		"missing git url":   func(p *ProjectProfile) { p.Git.URL = "" },
		"bad git provider":  func(p *ProjectProfile) { p.Git.Provider = "bitbucket" },
		"no agent":          func(p *ProjectProfile) { p.ImagePair.Agent = "" },
		"unknown agent":     func(p *ProjectProfile) { p.ImagePair.Agent = "agent-imaginary" },
		"unknown sidecar":   func(p *ProjectProfile) { p.ImagePair.Sidecar = "lang-cobol" },
		"bad memory mode":   func(p *ProjectProfile) { p.Memory.Mode = "invalid" },
		"negative idle":    func(p *ProjectProfile) { p.IdleTimeout = -time.Second },
		"negative budget":  func(p *ProjectProfile) { p.SpawnBudgetTotal = -1 },
		"budget but no spawn allow": func(p *ProjectProfile) {
			p.AllowSpawnChildren = false
			p.SpawnBudgetTotal = 5
		},
		"bad name":          func(p *ProjectProfile) { p.Name = "BadName" },
	}
	for label, mutate := range cases {
		t.Run(label, func(t *testing.T) {
			p := validProject()
			mutate(p)
			if err := p.Validate(); err == nil {
				t.Errorf("%s: validator should have rejected", label)
			}
		})
	}
}

func TestProjectProfile_EffectiveNamespace(t *testing.T) {
	p := &ProjectProfile{Name: "foo"}
	if p.EffectiveNamespace() != "project-foo" {
		t.Errorf("got %q, want project-foo", p.EffectiveNamespace())
	}
	p.Memory.Namespace = "override"
	if p.EffectiveNamespace() != "override" {
		t.Errorf("got %q, want override", p.EffectiveNamespace())
	}
}

// ── ClusterProfile validation ───────────────────────────────────────────

func validCluster() *ClusterProfile {
	return &ClusterProfile{
		Name:    "test-k8s",
		Kind:    ClusterK8s,
		Context: "testing",
	}
}

func TestClusterProfile_Validate_Ok(t *testing.T) {
	if err := validCluster().Validate(); err != nil {
		t.Fatalf("valid cluster profile rejected: %v", err)
	}
}

func TestClusterProfile_Validate_K8sRequiresContext(t *testing.T) {
	c := validCluster()
	c.Context = ""
	c.Endpoint = ""
	if err := c.Validate(); err == nil {
		t.Errorf("k8s cluster with no context/endpoint should fail")
	}
	// endpoint-only also ok
	c.Endpoint = "https://k8s.example:6443"
	if err := c.Validate(); err != nil {
		t.Errorf("endpoint-only k8s rejected: %v", err)
	}
}

func TestClusterProfile_Validate_BadKind(t *testing.T) {
	c := validCluster()
	c.Kind = "nomad"
	if err := c.Validate(); err == nil {
		t.Errorf("kind=nomad should be rejected")
	}
}

func TestClusterProfile_Validate_TrustedCAs(t *testing.T) {
	c := validCluster()
	c.TrustedCAs = []string{"not a pem"}
	if err := c.Validate(); err == nil {
		t.Errorf("non-PEM trusted CA should fail")
	}
	c.TrustedCAs = []string{"-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----"}
	if err := c.Validate(); err != nil {
		t.Errorf("PEM-encoded trusted CA rejected: %v", err)
	}
}

func TestClusterProfile_Validate_CredsRef(t *testing.T) {
	c := validCluster()
	c.CredsRef = CredsRef{Provider: CredsFile, Key: ""}
	if err := c.Validate(); err == nil {
		t.Errorf("provider set but empty key should fail")
	}
	c.CredsRef.Key = "/etc/secrets/git-pat"
	if err := c.Validate(); err != nil {
		t.Errorf("file creds ref with key rejected: %v", err)
	}
	c.CredsRef.Provider = "weirdvault"
	if err := c.Validate(); err == nil {
		t.Errorf("unknown provider should fail")
	}
}

// ── Project store CRUD ──────────────────────────────────────────────────

func TestProjectStore_CreateListGetUpdateDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	store, err := NewProjectStore(path)
	if err != nil {
		t.Fatalf("NewProjectStore: %v", err)
	}

	if got := store.List(); len(got) != 0 {
		t.Errorf("empty store List len=%d, want 0", len(got))
	}

	p := validProject()
	if err := store.Create(p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := store.List(); len(got) != 1 {
		t.Errorf("after Create len=%d, want 1", len(got))
	}

	got, err := store.Get("test-proj")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CreatedAt.IsZero() {
		t.Errorf("Create should have stamped CreatedAt")
	}

	// Duplicate create rejected
	if err := store.Create(p); err == nil {
		t.Errorf("duplicate Create should have failed")
	}

	// Update preserves CreatedAt and bumps UpdatedAt
	orig := got.CreatedAt
	p.Description = "after update"
	time.Sleep(2 * time.Millisecond)
	if err := store.Update(p); err != nil {
		t.Fatalf("Update: %v", err)
	}
	updated, _ := store.Get("test-proj")
	if !updated.CreatedAt.Equal(orig) {
		t.Errorf("Update clobbered CreatedAt: got %v want %v", updated.CreatedAt, orig)
	}
	if !updated.UpdatedAt.After(orig) {
		t.Errorf("Update didn't bump UpdatedAt: %v vs %v", updated.UpdatedAt, orig)
	}
	if updated.Description != "after update" {
		t.Errorf("Description not updated")
	}

	// Persistence: reopening the store sees the same data
	store2, err := NewProjectStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(store2.List()) != 1 {
		t.Errorf("reopen lost data")
	}

	// Delete
	if err := store.Delete("test-proj"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(store.List()) != 0 {
		t.Errorf("Delete didn't remove the profile")
	}
	if err := store.Delete("test-proj"); err == nil {
		t.Errorf("second Delete should fail")
	}
}

func TestProjectStore_Smoke(t *testing.T) {
	store, _ := NewProjectStore(filepath.Join(t.TempDir(), "p.json"))
	p := validProject()
	if err := store.Create(p); err != nil {
		t.Fatal(err)
	}
	r, err := store.Smoke("test-proj")
	if err != nil {
		t.Fatalf("Smoke: %v", err)
	}
	if !r.Passed() {
		t.Errorf("smoke should pass for valid profile, errors=%v", r.Errors)
	}
	if len(r.Checks) == 0 {
		t.Errorf("smoke should record at least one check")
	}

	// Smoke surfaces warnings for no sidecar
	p2 := validProject()
	p2.Name = "solo"
	p2.ImagePair.Sidecar = ""
	if err := store.Create(p2); err != nil {
		t.Fatal(err)
	}
	r2, _ := store.Smoke("solo")
	if !r2.Passed() {
		t.Errorf("solo-agent profile should still pass smoke")
	}
	if len(r2.Warnings) == 0 {
		t.Errorf("solo-agent should emit a warning about no sidecar")
	}
}

// ── Cluster store CRUD ──────────────────────────────────────────────────

func TestClusterStore_CreateListGetUpdateDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clusters.json")
	store, err := NewClusterStore(path)
	if err != nil {
		t.Fatal(err)
	}
	c := validCluster()
	if err := store.Create(c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := store.List(); len(got) != 1 {
		t.Errorf("len=%d want 1", len(got))
	}
	if _, err := store.Get("test-k8s"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	c2 := validCluster()
	c2.Name = "other"
	c2.Kind = ClusterDocker
	c2.Context = ""
	if err := store.Create(c2); err != nil {
		t.Fatalf("docker cluster create: %v", err)
	}
	if err := store.Delete("other"); err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 1 {
		t.Errorf("after Delete len mismatch")
	}
}

func TestClusterStore_Smoke_CFWarns(t *testing.T) {
	store, _ := NewClusterStore(filepath.Join(t.TempDir(), "c.json"))
	c := validCluster()
	c.Name = "cf-future"
	c.Kind = ClusterCF
	c.Context = "x"
	if err := store.Create(c); err != nil {
		t.Fatal(err)
	}
	r, err := store.Smoke("cf-future")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Passed() {
		t.Errorf("cf smoke should pass at schema level, errors=%v", r.Errors)
	}
	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "Cloud Foundry") {
			found = true
		}
	}
	if !found {
		t.Errorf("cf smoke should warn about not-implemented driver, got warnings=%v", r.Warnings)
	}
}

// F10 S4.5 — smoke flags missing driver CLI so operators know to
// install kubectl/docker before trying to spawn.
func TestClusterStore_Smoke_MissingDockerCLI(t *testing.T) {
	// Isolate PATH so whatever docker binary the host has is hidden.
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	store, _ := NewClusterStore(filepath.Join(t.TempDir(), "c.json"))
	c := validCluster()
	c.Name = "no-docker"
	c.Kind = ClusterDocker
	c.Context = "x"
	if err := store.Create(c); err != nil {
		t.Fatal(err)
	}
	r, err := store.Smoke("no-docker")
	if err != nil {
		t.Fatal(err)
	}
	if r.Passed() {
		t.Error("smoke should fail when docker CLI is missing")
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e, "docker CLI on PATH") {
			found = true
		}
	}
	if !found {
		t.Errorf("errors should mention docker CLI, got %v", r.Errors)
	}
}

// Fake kubectl that exits 0 on cluster-info but records the call —
// proves the smoke surfaces apiserver reachability as ok.
func TestClusterStore_Smoke_KubectlReachable(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
echo "$@" >> "` + dir + `/calls.log"
# cluster-info success path
exit 0
`
	kubectlPath := filepath.Join(dir, "kubectl")
	if err := os.WriteFile(kubectlPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	prev := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+prev)

	store, _ := NewClusterStore(filepath.Join(t.TempDir(), "c.json"))
	c := validCluster()
	c.Name = "reachable"
	c.Kind = ClusterK8s
	c.Context = "testing"
	if err := store.Create(c); err != nil {
		t.Fatal(err)
	}
	r, err := store.Smoke("reachable")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Passed() {
		t.Errorf("smoke should pass, errors=%v", r.Errors)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls.log"))
	if !strings.Contains(string(calls), "cluster-info") ||
		!strings.Contains(string(calls), "--context testing") {
		t.Errorf("kubectl not invoked correctly:\n%s", calls)
	}
}

// Fake kubectl that exits non-zero — smoke must surface the
// reachability error without crashing.
func TestClusterStore_Smoke_KubectlUnreachable(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
echo "connection refused" >&2
exit 1
`
	kubectlPath := filepath.Join(dir, "kubectl")
	if err := os.WriteFile(kubectlPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	prev := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+prev)

	store, _ := NewClusterStore(filepath.Join(t.TempDir(), "c.json"))
	c := validCluster()
	c.Name = "unreachable"
	c.Kind = ClusterK8s
	c.Context = "testing"
	if err := store.Create(c); err != nil {
		t.Fatal(err)
	}
	r, err := store.Smoke("unreachable")
	if err != nil {
		t.Fatal(err)
	}
	if r.Passed() {
		t.Error("smoke should fail when apiserver is unreachable")
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e, "apiserver reachability") {
			found = true
		}
	}
	if !found {
		t.Errorf("errors should mention apiserver reachability, got %v", r.Errors)
	}
}

// ── Encrypted store round-trip ──────────────────────────────────────────

func TestProjectStore_Encrypted_RoundTrip(t *testing.T) {
	key := make([]byte, 32) // AES-256
	for i := range key {
		key[i] = byte(i)
	}
	path := filepath.Join(t.TempDir(), "projects.enc.json")

	store, err := NewProjectStoreEncrypted(path, key)
	if err != nil {
		t.Fatalf("NewProjectStoreEncrypted: %v", err)
	}
	if err := store.Create(validProject()); err != nil {
		t.Fatal(err)
	}

	// Re-open with the SAME key reads successfully
	store2, err := NewProjectStoreEncrypted(path, key)
	if err != nil {
		t.Fatalf("reopen enc: %v", err)
	}
	if len(store2.List()) != 1 {
		t.Errorf("encrypted reopen lost data")
	}

	// Opening plaintext against the encrypted file should error on load
	if _, err := NewProjectStore(path); err == nil {
		t.Errorf("plaintext open of encrypted file should have failed")
	}
}

// ── Known-lists export ──────────────────────────────────────────────────

func TestKnownAgents_SidecarsCopies(t *testing.T) {
	a := KnownAgents()
	a[0] = "tampered"
	if KnownAgents()[0] == "tampered" {
		t.Errorf("KnownAgents returned a shared slice — caller could mutate state")
	}
	s := KnownSidecars()
	s[0] = "tampered"
	if KnownSidecars()[0] == "tampered" {
		t.Errorf("KnownSidecars returned a shared slice")
	}
}

// ── F10 S6.5 — cross-profile sharing (mutual opt-in) ─────────────────

// EffectiveNamespacesFor returns just the own namespace when no
// SharedWith is declared.
func TestProjectStore_EffectiveNamespacesFor_NoSharing(t *testing.T) {
	store, _ := NewProjectStore(filepath.Join(t.TempDir(), "p.json"))
	_ = store.Create(&ProjectProfile{
		Name:      "alone",
		Git:       GitSpec{URL: "https://github.com/x/y"},
		ImagePair: ImagePair{Agent: "agent-claude"},
		Memory:    MemorySpec{Mode: MemoryShared},
	})
	got := store.EffectiveNamespacesFor("alone")
	if len(got) != 1 || got[0] != "project-alone" {
		t.Errorf("want [project-alone], got %v", got)
	}
}

// Mutual SharedWith → both peers see each other.
func TestProjectStore_EffectiveNamespacesFor_MutualSharing(t *testing.T) {
	store, _ := NewProjectStore(filepath.Join(t.TempDir(), "p.json"))
	_ = store.Create(&ProjectProfile{
		Name: "a",
		Git:  GitSpec{URL: "https://github.com/x/a"},
		ImagePair: ImagePair{Agent: "agent-claude"},
		Memory: MemorySpec{Mode: MemoryShared, SharedWith: []string{"b"}},
	})
	_ = store.Create(&ProjectProfile{
		Name: "b",
		Git:  GitSpec{URL: "https://github.com/x/b"},
		ImagePair: ImagePair{Agent: "agent-claude"},
		Memory: MemorySpec{Mode: MemoryShared, SharedWith: []string{"a"}},
	})
	gotA := store.EffectiveNamespacesFor("a")
	if len(gotA) != 2 || gotA[0] != "project-a" || gotA[1] != "project-b" {
		t.Errorf("a's view: want [project-a project-b], got %v", gotA)
	}
	gotB := store.EffectiveNamespacesFor("b")
	if len(gotB) != 2 || gotB[0] != "project-b" || gotB[1] != "project-a" {
		t.Errorf("b's view: want [project-b project-a], got %v", gotB)
	}
}

// Single-sided SharedWith does NOT grant access — defence against
// operator misconfiguration leaking memory the peer never opted in to.
func TestProjectStore_EffectiveNamespacesFor_SingleSidedRejected(t *testing.T) {
	store, _ := NewProjectStore(filepath.Join(t.TempDir(), "p.json"))
	_ = store.Create(&ProjectProfile{
		Name: "greedy",
		Git:  GitSpec{URL: "https://github.com/x/g"},
		ImagePair: ImagePair{Agent: "agent-claude"},
		Memory: MemorySpec{Mode: MemoryShared, SharedWith: []string{"private"}},
	})
	_ = store.Create(&ProjectProfile{
		Name: "private",
		Git:  GitSpec{URL: "https://github.com/x/p"},
		ImagePair: ImagePair{Agent: "agent-claude"},
		Memory: MemorySpec{Mode: MemoryShared}, // no SharedWith
	})
	got := store.EffectiveNamespacesFor("greedy")
	if len(got) != 1 || got[0] != "project-greedy" {
		t.Errorf("greedy should NOT see private without mutual opt-in: %v", got)
	}
}

// Missing peer is silently skipped (no panic, no leak of other peers).
func TestProjectStore_EffectiveNamespacesFor_MissingPeerSkipped(t *testing.T) {
	store, _ := NewProjectStore(filepath.Join(t.TempDir(), "p.json"))
	_ = store.Create(&ProjectProfile{
		Name: "a",
		Git:  GitSpec{URL: "https://github.com/x/a"},
		ImagePair: ImagePair{Agent: "agent-claude"},
		Memory: MemorySpec{Mode: MemoryShared, SharedWith: []string{"ghost", "b"}},
	})
	_ = store.Create(&ProjectProfile{
		Name: "b",
		Git:  GitSpec{URL: "https://github.com/x/b"},
		ImagePair: ImagePair{Agent: "agent-claude"},
		Memory: MemorySpec{Mode: MemoryShared, SharedWith: []string{"a"}},
	})
	got := store.EffectiveNamespacesFor("a")
	if len(got) != 2 || got[1] != "project-b" {
		t.Errorf("missing-peer skip: want [project-a project-b], got %v", got)
	}
}

// Unknown profile → nil (caller falls back to DefaultNamespace).
func TestProjectStore_EffectiveNamespacesFor_UnknownProfile(t *testing.T) {
	store, _ := NewProjectStore(filepath.Join(t.TempDir(), "p.json"))
	if got := store.EffectiveNamespacesFor("nope"); got != nil {
		t.Errorf("want nil for unknown profile, got %v", got)
	}
}

// ── F10 S8.7 — on_crash policy validation ────────────────────────────

func TestProjectProfile_Validate_OnCrash(t *testing.T) {
	cases := map[string]bool{
		"":                       true,  // empty defaults to fail_parent
		"fail_parent":            true,
		"respawn_once":           true,
		"respawn_with_backoff":   true,
		"unknown_policy":         false,
		"FAIL_PARENT":            false, // case-sensitive on purpose
	}
	for v, ok := range cases {
		t.Run(v, func(t *testing.T) {
			p := validProject()
			p.OnCrash = v
			err := p.Validate()
			if ok && err != nil {
				t.Errorf("on_crash=%q rejected: %v", v, err)
			}
			if !ok && err == nil {
				t.Errorf("on_crash=%q should have been rejected", v)
			}
		})
	}
}

// ── F10 S8.2 — worker mode validation ────────────────────────────────

func TestProjectProfile_Validate_Mode(t *testing.T) {
	cases := map[string]bool{
		"":          true,  // empty = ephemeral default
		"ephemeral": true,
		"service":   true,
		"daemon":    false,
		"EPHEMERAL": false, // case-sensitive
	}
	for v, ok := range cases {
		t.Run(v, func(t *testing.T) {
			p := validProject()
			p.Mode = v
			err := p.Validate()
			if ok && err != nil {
				t.Errorf("mode=%q rejected: %v", v, err)
			}
			if !ok && err == nil {
				t.Errorf("mode=%q should have been rejected", v)
			}
		})
	}
}
