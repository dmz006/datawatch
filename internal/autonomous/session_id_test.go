package autonomous

import "testing"

func TestSessionShortID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{name: "short id", id: "8b57", want: "8b57"},
		{name: "full id", id: "local-node-8b57", want: "8b57"},
		{name: "empty id", id: "", want: ""},
		{name: "host name with hyphens", id: "my-host-name-8b57", want: "8b57"},
		{name: "no hyphen", id: "8b57", want: "8b57"},
		{name: "surrounding spaces", id: "  local-node-8b57 ", want: "8b57"},
		{name: "trailing hyphen", id: "local-node-", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionShortID(tc.id); got != tc.want {
				t.Errorf("sessionShortID(%q) = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}

func TestSameSessionID(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "short against full, same worker", a: "8b57", b: "local-node-8b57", want: true},
		{name: "full against full, same worker", a: "local-node-8b57", b: "local-node-8b57", want: true},
		{name: "mixed hosts sharing a short id", a: "alpha-8b57", b: "beta-8b57", want: true},
		{name: "host name with hyphens", a: "my-host-name-8b57", b: "8b57", want: true},
		{name: "different short ids", a: "local-node-8b57", b: "local-node-c3d4", want: false},
		{name: "both empty", a: "", b: "", want: false},
		{name: "one empty", a: "", b: "8b57", want: false},
		{name: "trailing hyphen is empty short form", a: "local-node-", b: "local-node-", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameSessionID(tc.a, tc.b); got != tc.want {
				t.Errorf("sameSessionID(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
