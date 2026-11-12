package autonomous

import "testing"

func TestLocalRelPath(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{"empty", "", "", false},
		{"dot", ".", "", false},
		{"parent gitignore", "../.gitignore", "", false},
		{"parent x", "../x", "", false},
		{"nested escape", "a/../../x", "", false},
		{"absolute", "/" + "outside" + "/x", "", false},
		{"nul byte", "a\x00b", "", false},
		{"dotfile", ".gitignore", ".gitignore", true},
		{"docs file", "docs/a.md", "docs/a.md", true},
		{"dot segment", "a/./b", "a/b", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := localRelPath(tc.in)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("localRelPath(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
