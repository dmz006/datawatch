// BL114 — driver injection tests for cluster-shared volumes.

package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
)

// Docker driver injects -v src:dst[:ro] for HostPath + NFS.
func TestDockerDriver_Spawn_SharedVolumes(t *testing.T) {
	dir := newFakeDocker(t)
	withFakePath(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "output.run"), []byte("cid\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "output.inspect"), []byte(`{"bridge":{"IPAddress":"1.2.3.4"}}`), 0644)

	d := NewDockerDriver("", "p", "v1", "http://parent")
	a := testAgent(t, nil, func(c *profile.ClusterProfile) {
		c.SharedVolumes = []profile.SharedVolume{
			{Name: "cache", MountPath: "/cache", HostPath: "/var/dw/cache"},
			{Name: "ro-data", MountPath: "/data", ReadOnly: true,
				HostPath: "/srv/datawatch-ro"},
		}
	})
	if err := d.Spawn(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
	for _, want := range []string{
		"-v /var/dw/cache:/cache",
		"-v /srv/datawatch-ro:/data:ro",
	} {
		if !strings.Contains(string(log), want) {
			t.Errorf("missing %q in invocation:\n%s", want, log)
		}
	}
}

// NFS sources are skipped on Docker (require operator pre-mount via
// HostPath in a separate SharedVolume entry). No "/mnt/..." prefix
// is inferred — that's per AGENT.md "no hard-coded configurations".
func TestDockerDriver_Spawn_SkipsNFS_NoHardCodedPath(t *testing.T) {
	dir := newFakeDocker(t)
	withFakePath(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "output.run"), []byte("cid\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "output.inspect"), []byte(`{"bridge":{"IPAddress":"1.2.3.4"}}`), 0644)

	d := NewDockerDriver("", "p", "v1", "http://parent")
	a := testAgent(t, nil, func(c *profile.ClusterProfile) {
		c.SharedVolumes = []profile.SharedVolume{
			{Name: "shared", MountPath: "/shared",
				NFS: &profile.NFSVolumeSpec{Server: "203.0.113.10", Path: "/exports/shared"}},
		}
	})
	if err := d.Spawn(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
	if strings.Contains(string(log), "-v") {
		t.Errorf("NFS source must not produce a -v entry on Docker:\n%s", log)
	}
	if strings.Contains(string(log), "/mnt/") {
		t.Errorf("must not infer a /mnt/ host path:\n%s", log)
	}
}

// PVC sources are skipped on Docker (k8s only).
func TestDockerDriver_Spawn_SkipsPVC(t *testing.T) {
	dir := newFakeDocker(t)
	withFakePath(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "output.run"), []byte("cid\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "output.inspect"), []byte(`{"bridge":{"IPAddress":"1.2.3.4"}}`), 0644)

	d := NewDockerDriver("", "p", "v1", "http://parent")
	a := testAgent(t, nil, func(c *profile.ClusterProfile) {
		c.SharedVolumes = []profile.SharedVolume{
			{Name: "data", MountPath: "/data", PVC: "datawatch-shared"},
		}
	})
	if err := d.Spawn(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
	if strings.Contains(string(log), "-v") {
		t.Errorf("PVC should not produce -v on Docker:\n%s", log)
	}
}
