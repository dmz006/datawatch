package autonomous

import "testing"

func TestProjectKey(t *testing.T) {
	cases := []struct {
		name string
		p    *PRD
		want string
	}{
		{"explicit profile", &PRD{ProjectProfile: "alpha", ProjectDir: "/w/beta"}, "alpha"},
		{"explicit directory", &PRD{ProjectDir: "/w/beta"}, "/w/beta"},
		{"missing project", &PRD{}, ProjectKeyNone},
		{"empty string", &PRD{ProjectProfile: "", ProjectDir: ""}, "(none)"},
		{"whitespace only", &PRD{ProjectProfile: "  ", ProjectDir: " "}, "(none)"},
		{"nil record", nil, "(none)"},
	}
	for _, c := range cases {
		if got := ProjectKey(c.p); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
