// Table-driven tests for the backend version probe (version_probe.go).
// All I/O goes through fakes — no daemon, no real Ollama, no docker
// CLI, no model pulls, no network.

package compute

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// fakeVersionClient is the injectable HTTP stand-in.
type fakeVersionClient struct {
	status int
	body   []byte
	err    error
	gotURL string
}

func (f *fakeVersionClient) Get(_ context.Context, url string) (int, []byte, error) {
	f.gotURL = url
	return f.status, f.body, f.err
}

// fakeVersionRunner is the injectable command stand-in.
type fakeVersionRunner struct {
	out string
	err error
	got []string // name + args for last call
}

func (f *fakeVersionRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	f.got = append([]string{name}, args...)
	return f.out, f.err
}

func TestParseBackendVersion(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string // normalized core; "" = malformed / no version
		wantOK  bool   // ParseBackendVersionParts().OK
		wantMaj int
	}{
		{"plain", "0.5.14", "0.5.14", true, 0},
		{"v-prefix", "v0.5.14", "0.5.14", true, 0},
		{"banner", `Docker version 27.1.1, build 537b5d6`, "27.1.1", true, 27},
		{"suffix-prerelease", "27.1.1-canary", "27.1.1", true, 27},
		{"suffix-build", "26.1.2+bullseye", "26.1.2", true, 26},
		{"two-part", "1.2", "1.2", true, 1},
		{"whitespace", "  0.5.14  ", "0.5.14", true, 0},
		{"embedded-noise", `ollama version 0.5.14 (build abc)`, "0.5.14", true, 0},
		{"four-part", "1.2.3.4", "1.2.3", true, 1},
		{"empty", "", "", false, 0},
		{"na", "N/A", "", false, 0},
		{"garbage", "unknown (no version reported)", "", false, 0},
		{"only-prefix", "v.", "", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseBackendVersion(tc.in); got != tc.want {
				t.Fatalf("ParseBackendVersion(%q) = %q, want %q", tc.in, got, tc.want)
			}
			parts := ParseBackendVersionParts(tc.in)
			if parts.OK != tc.wantOK {
				t.Fatalf("ParseBackendVersionParts(%q).OK = %v, want %v", tc.in, parts.OK, tc.wantOK)
			}
			if tc.wantOK && parts.Major != tc.wantMaj {
				t.Fatalf("ParseBackendVersionParts(%q).Major = %d, want %d", tc.in, parts.Major, tc.wantMaj)
			}
		})
	}
}

func TestProbeVersionOllama(t *testing.T) {
	cases := []struct {
		name    string
		client  *fakeVersionClient
		node    *Node
		wantErr string // substring of error; empty = success
		wantRaw string
		wantVer string
	}{
		{
			name:    "success",
			client:  &fakeVersionClient{status: 200, body: []byte(`{"version":"0.5.14","packages":{"ollama":"0.5.14"}}`)},
			node:    &Node{Name: "n", Kind: KindOllama, Address: "http://ollama.local:11434"},
			wantRaw: "0.5.14",
			wantVer: "0.5.14",
		},
		{
			name:    "unreachable",
			client:  &fakeVersionClient{err: &net.DNSError{Err: "no such host"}},
			node:    &Node{Name: "n", Kind: KindOllama, Address: "http://ollama.local:11434"},
			wantErr: "unreachable",
		},
		{
			name:    "malformed-json",
			client:  &fakeVersionClient{status: 200, body: []byte(`<html>not json</html>`)},
			node:    &Node{Name: "n", Kind: KindOllama, Address: "http://ollama.local:11434"},
			wantErr: "malformed",
		},
		{
			name:    "missing-version-field",
			client:  &fakeVersionClient{status: 200, body: []byte(`{"packages":{}}`)},
			node:    &Node{Name: "n", Kind: KindOllama, Address: "http://ollama.local:11434"},
			wantErr: `no "version" field`,
		},
		{
			name:    "non-200",
			client:  &fakeVersionClient{status: 401, body: []byte(`unauthorized`)},
			node:    &Node{Name: "n", Kind: KindOllama, Address: "http://ollama.local:11434"},
			wantErr: "HTTP 401",
		},
		{
			name:    "no-address",
			client:  &fakeVersionClient{},
			node:    &Node{Name: "n", Kind: KindOllama},
			wantErr: "no address",
		},
		{
			name:    "bare-host-normalized",
			client:  &fakeVersionClient{status: 200, body: []byte(`{"version":"0.5.13"}`)},
			node:    &Node{Name: "n", Kind: KindOllama, Address: "ollama.local:11434"},
			wantRaw: "0.5.13",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vp := &VersionProbe{Client: tc.client, Now: func() time.Time { return testNow }}
			res, err := vp.ProbeVersion(context.Background(), tc.node)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil (res=%+v)", tc.wantErr, res)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Backend != "ollama" {
				t.Errorf("Backend = %q, want ollama", res.Backend)
			}
			if res.Raw != tc.wantRaw {
				t.Errorf("Raw = %q, want %q", res.Raw, tc.wantRaw)
			}
			if tc.wantVer != "" && res.Version != tc.wantVer {
				t.Errorf("Version = %q, want %q", res.Version, tc.wantVer)
			}
			if !res.ObservedAt.Equal(testNow) {
				t.Errorf("ObservedAt = %v, want %v", res.ObservedAt, testNow)
			}
		})
	}
}

func TestOllamaProbeURL(t *testing.T) {
	client := &fakeVersionClient{status: 200, body: []byte(`{"version":"0.5.14"}`)}
	vp := &VersionProbe{Client: client, Now: func() time.Time { return testNow }}
	node := &Node{Name: "n", Kind: KindOllama, Address: "http://ollama.local:11434"}
	if _, err := vp.ProbeVersion(context.Background(), node); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if client.gotURL != "http://ollama.local:11434/api/version" {
		t.Fatalf("GET %q, want http://ollama.local:11434/api/version", client.gotURL)
	}
}

func TestProbeVersionDocker(t *testing.T) {
	cases := []struct {
		name    string
		runner  *fakeVersionRunner
		node    *Node
		wantErr string
		wantRaw string
	}{
		{
			name:    "success",
			runner:  &fakeVersionRunner{out: "27.1.1\n"},
			node:    &Node{Name: "n", Kind: KindDocker},
			wantRaw: "27.1.1",
		},
		{
			name:    "unreachable-daemon",
			runner:  &fakeVersionRunner{out: "Cannot connect to the Docker daemon", err: errors.New("exit status 1")},
			node:    &Node{Name: "n", Kind: KindDocker},
			wantErr: "Cannot connect",
		},
		{
			name:    "empty-response",
			runner:  &fakeVersionRunner{out: "   "},
			node:    &Node{Name: "n", Kind: KindDocker},
			wantErr: "empty server version",
		},
		{
			name:   "docker-network-routing",
			runner: &fakeVersionRunner{out: "27.1.1"},
			node: &Node{
				Name: "n", Kind: KindOllama,
				Routing:              RoutingDockerNetwork,
				RoutingDockerNetwork: &RoutingDockerNetworkConfig{Image: "ollama/ollama:latest", DockerEndpoint: "tcp://172.17.0.1:2375"},
			},
			wantRaw: "27.1.1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vp := &VersionProbe{Runner: tc.runner, Now: func() time.Time { return testNow }}
			res, err := vp.ProbeVersion(context.Background(), tc.node)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil (res=%+v)", tc.wantErr, res)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Backend != "docker" {
				t.Errorf("Backend = %q, want docker", res.Backend)
			}
			if res.Raw != tc.wantRaw {
				t.Errorf("Raw = %q, want %q", res.Raw, tc.wantRaw)
			}
			if res.Version == "" {
				t.Errorf("Version empty for raw %q", res.Raw)
			}
		})
	}
}

func TestProbeVersionDockerNetworkFlags(t *testing.T) {
	runner := &fakeVersionRunner{out: "27.1.1"}
	vp := &VersionProbe{Runner: runner, Now: func() time.Time { return testNow }}
	node := &Node{
		Name: "n", Routing: RoutingDockerNetwork,
		RoutingDockerNetwork: &RoutingDockerNetworkConfig{Image: "ollama/ollama:latest", DockerEndpoint: "tcp://10.0.0.9:2375"},
	}
	if _, err := vp.ProbeVersion(context.Background(), node); err != nil {
		t.Fatalf("probe: %v", err)
	}
	joined := strings.Join(runner.got, " ")
	if !strings.Contains(joined, "--host tcp://10.0.0.9:2375") {
		t.Errorf("docker args %q missing --host", joined)
	}
}

func TestProbeVersionNoSource(t *testing.T) {
	cases := []*Node{
		{Name: "n", Kind: KindOpenAICompat, Address: "http://gw.local:8080"},
		{Name: "n", Kind: KindSSH},
	}
	for _, n := range cases {
		_, err := NewVersionProbe().ProbeVersion(context.Background(), n)
		if !errors.Is(err, ErrNoVersionProbe) {
			t.Errorf("kind %q: want ErrNoVersionProbe, got %v", n.Kind, err)
		}
	}
}

func TestProbeVersionNilNode(t *testing.T) {
	if _, err := NewVersionProbe().ProbeVersion(context.Background(), nil); err == nil {
		t.Fatal("want error for nil node")
	}
}

func TestRecordBackendVersion(t *testing.T) {
	n := &Node{Name: "n", Kind: KindOllama}
	n.RecordBackendVersion(VersionResult{Backend: "ollama", Raw: "0.5.14", Version: "0.5.14", ObservedAt: testNow})
	if n.VersionBackend != "ollama" {
		t.Errorf("VersionBackend = %q", n.VersionBackend)
	}
	if n.BackendVersion != "0.5.14" {
		t.Errorf("BackendVersion = %q", n.BackendVersion)
	}
	if !n.VersionProbedAt.Equal(testNow) {
		t.Errorf("VersionProbedAt = %v", n.VersionProbedAt)
	}

	// Unparseable raw: verbatim kept.
	n2 := &Node{}
	n2.RecordBackendVersion(VersionResult{Backend: "docker", Raw: "git-abc123", ObservedAt: testNow})
	if n2.BackendVersion != "git-abc123" {
		t.Errorf("BackendVersion = %q, want git-abc123", n2.BackendVersion)
	}

	// Empty result: no-op.
	n3 := &Node{Name: "n"}
	n3.RecordBackendVersion(VersionResult{})
	if n3.BackendVersion != "" || !n3.VersionProbedAt.IsZero() {
		t.Errorf("empty result should be a no-op: %+v", n3)
	}
}

func TestRegistryRecordBackendVersion(t *testing.T) {
	path := t.TempDir() + "/nodes.json"
	r, err := NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Add(&Node{Name: "n", Kind: KindOllama, Address: "http://ollama.local:11434"}); err != nil {
		t.Fatal(err)
	}
	vr := VersionResult{Backend: "ollama", Raw: "0.5.14", Version: "0.5.14", ObservedAt: testNow}
	if err := r.RecordBackendVersion("n", vr); err != nil {
		t.Fatal(err)
	}
	got, err := r.Get("n")
	if err != nil {
		t.Fatal(err)
	}
	if got.BackendVersion != "0.5.14" || got.VersionBackend != "ollama" || !got.VersionProbedAt.Equal(testNow) {
		t.Errorf("recorded: %+v", got)
	}
	if err := r.RecordBackendVersion("missing", vr); err != ErrNotFound {
		t.Errorf("missing node: want ErrNotFound, got %v", err)
	}

	// Reload from disk — the fields must survive a registry round trip.
	r2, err := NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err = r2.Get("n")
	if err != nil {
		t.Fatal(err)
	}
	if got.BackendVersion != "0.5.14" || !got.VersionProbedAt.Equal(testNow) {
		t.Errorf("after reload: %+v", got)
	}
}

func TestNodeVersionFieldsMarshal(t *testing.T) {
	n := &Node{Name: "n", Kind: KindOllama, VersionBackend: "ollama", BackendVersion: "0.5.14", VersionProbedAt: testNow}
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	b := string(raw)
	if !strings.Contains(b, `"backend_version":"0.5.14"`) ||
		!strings.Contains(b, `"version_backend":"ollama"`) ||
		!strings.Contains(b, `"version_probed_at":"2026-11-01T12:00:00Z"`) {
		t.Errorf("node JSON missing version fields: %s", b)
	}
}
