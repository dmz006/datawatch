package mergepipeline

import "testing"

func TestNextVersion(t *testing.T) {
	cases := []struct {
		name    string
		current string
		size    ReleaseSize
		want    string
		wantErr bool
	}{
		{name: "patch raises patch", current: "1.2.3", size: ReleaseSizePatch, want: "1.2.4"},
		{name: "minor resets patch", current: "1.2.3", size: ReleaseSizeMinor, want: "1.3.0"},
		{name: "major resets minor and patch", current: "1.2.3", size: ReleaseSizeMajor, want: "2.0.0"},
		{name: "empty size means patch", current: "1.2.3", size: "", want: "1.2.4"},
		{name: "patch carries past 9", current: "1.2.9", size: ReleaseSizePatch, want: "1.2.10"},
		{name: "minor carries past 9", current: "1.9.4", size: ReleaseSizeMinor, want: "1.10.0"},
		{name: "major carries past 9", current: "9.4.4", size: ReleaseSizeMajor, want: "10.0.0"},
		{name: "zero version patch", current: "0.0.0", size: ReleaseSizePatch, want: "0.0.1"},
		{name: "invalid size", current: "1.2.3", size: ReleaseSize("huge"), wantErr: true},
		{name: "invalid size is case sensitive", current: "1.2.3", size: ReleaseSize("Patch"), wantErr: true},
		{name: "current with two parts", current: "1.2", size: ReleaseSizePatch, wantErr: true},
		{name: "current with four parts", current: "1.2.3.4", size: ReleaseSizePatch, wantErr: true},
		{name: "current with letters", current: "1.x.3", size: ReleaseSizeMinor, wantErr: true},
		{name: "current with v prefix", current: "v1.2.3", size: ReleaseSizePatch, wantErr: true},
		{name: "current with empty part", current: "1..3", size: ReleaseSizePatch, wantErr: true},
		{name: "current with negative part", current: "1.-2.3", size: ReleaseSizePatch, wantErr: true},
		{name: "current with plus sign", current: "+1.2.3", size: ReleaseSizePatch, wantErr: true},
		{name: "empty current", current: "", size: ReleaseSizePatch, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NextVersion(tc.current, tc.size)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NextVersion(%q, %q) = %q, want error", tc.current, tc.size, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NextVersion(%q, %q) error: %v", tc.current, tc.size, err)
			}
			if got != tc.want {
				t.Fatalf("NextVersion(%q, %q) = %q, want %q", tc.current, tc.size, got, tc.want)
			}
		})
	}
}
