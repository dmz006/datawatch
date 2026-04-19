// BL114 — SharedVolume validation tests.

package profile

import "testing"

func TestSharedVolume_Validate(t *testing.T) {
	cases := []struct {
		name string
		v    SharedVolume
		ok   bool
	}{
		{"valid nfs", SharedVolume{Name: "share", MountPath: "/work",
			NFS: &NFSVolumeSpec{Server: "203.0.113.10", Path: "/exports/share"}}, true},
		{"valid hostpath", SharedVolume{Name: "h", MountPath: "/work", HostPath: "/var/data"}, true},
		{"valid pvc", SharedVolume{Name: "p", MountPath: "/work", PVC: "datawatch-shared"}, true},
		{"missing name", SharedVolume{MountPath: "/work", HostPath: "/x"}, false},
		{"missing mount", SharedVolume{Name: "x", HostPath: "/x"}, false},
		{"missing source", SharedVolume{Name: "x", MountPath: "/work"}, false},
		{"two sources", SharedVolume{Name: "x", MountPath: "/work",
			HostPath: "/x", PVC: "p"}, false},
		{"nfs missing server", SharedVolume{Name: "x", MountPath: "/work",
			NFS: &NFSVolumeSpec{Path: "/exports"}}, false},
		{"nfs missing path", SharedVolume{Name: "x", MountPath: "/work",
			NFS: &NFSVolumeSpec{Server: "203.0.113.10"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.v.Validate()
			if c.ok && err != nil {
				t.Errorf("expected ok, got %v", err)
			}
			if !c.ok && err == nil {
				t.Errorf("expected error, got nil")
			}
		})
	}
}
