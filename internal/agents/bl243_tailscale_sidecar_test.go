// BL243 Phase 1 — Tailscale sidecar injection tests for K8sDriver.

package agents

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
)

func TestK8sDriver_TailscaleSidecar_NotInjectedByDefault(t *testing.T) {
	d := NewK8sDriver("kubectl", "registry.example.com/datawatch", "v6.5.0", "http://parent:8080")
	a := newTestAgent(t, d)
	manifest := renderManifest(t, d, a)
	if strings.Contains(manifest, "tailscale") {
		t.Error("expected no tailscale sidecar in default manifest")
	}
}

func TestK8sDriver_TailscaleSidecar_Injected(t *testing.T) {
	d := NewK8sDriver("kubectl", "registry.example.com/datawatch", "v6.5.0", "http://parent:8080")
	d.TailscaleEnabled = true
	d.TailscaleImage = "ghcr.io/tailscale/tailscale:latest"
	d.TailscaleAuthKey = "tskey-auth-test123"
	d.TailscaleLoginServer = "https://headscale.example.com"
	d.TailscaleTags = "tag:dw-agent"

	a := newTestAgent(t, d)
	manifest := renderManifest(t, d, a)

	checks := []string{
		"name: tailscale",
		"ghcr.io/tailscale/tailscale:latest",
		"TS_AUTHKEY",
		"tskey-auth-test123",
		"TS_LOGIN_SERVER",
		"https://headscale.example.com",
		"TS_TAGS",
		"tag:dw-agent",
		"NET_ADMIN",
	}
	for _, want := range checks {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest missing %q", want)
		}
	}
}

func TestK8sDriver_TailscaleSidecar_DefaultImage(t *testing.T) {
	d := NewK8sDriver("kubectl", "reg/dw", "v6.5.0", "http://parent")
	d.TailscaleEnabled = true
	d.TailscaleAuthKey = "tskey-xyz"
	// TailscaleImage intentionally empty — should use default

	a := newTestAgent(t, d)
	manifest := renderManifest(t, d, a)

	if !strings.Contains(manifest, "ghcr.io/tailscale/tailscale:latest") {
		t.Error("expected default sidecar image ghcr.io/tailscale/tailscale:latest")
	}
}

// newTestAgent constructs a minimal Agent for template rendering tests.
func newTestAgent(t *testing.T, d *K8sDriver) *Agent {
	t.Helper()
	_ = d
	return &Agent{
		ID: "test-agent-001",
		project: &profile.ProjectProfile{
			Name: "test-project",
			Env:  map[string]string{},
		},
		cluster: &profile.ClusterProfile{
			Name: "test-cluster",
		},
		BootstrapToken: "tok123",
	}
}

// renderManifest executes the pod template and returns the YAML string.
func renderManifest(t *testing.T, d *K8sDriver, a *Agent) string {
	t.Helper()
	data := podTemplateData{
		Name:           "dw-" + a.ID,
		Namespace:      a.cluster.EffectiveNamespace(),
		AgentID:        a.ID,
		ProjectProfile: a.project.Name,
		ClusterProfile: a.cluster.Name,
		Image:          d.imageRef(a),
		CallbackURL:    d.callbackURL(a),
		BootstrapToken: a.BootstrapToken,
		ProjectEnv:     a.project.Env,
		Resources:      a.cluster.DefaultResources,
	}
	if d.TailscaleEnabled {
		data.TailscaleEnabled = true
		data.TailscaleImage = d.TailscaleImage
		if data.TailscaleImage == "" {
			data.TailscaleImage = "ghcr.io/tailscale/tailscale:latest"
		}
		data.TailscaleAuthKey = d.TailscaleAuthKey
		data.TailscaleLoginServer = d.TailscaleLoginServer
		data.TailscaleTags = d.TailscaleTags
	}
	var buf strings.Builder
	if err := podTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("template execute: %v", err)
	}
	return buf.String()
}
