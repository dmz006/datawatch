package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectAliasResolve(t *testing.T) {
	root := t.TempDir()
	web := filepath.Join(root, "web")
	webApp := filepath.Join(web, "app")
	other := filepath.Join(root, "other")
	for _, d := range []string{webApp, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	aliases := map[string]string{"web": web, "webapp": webApp, "other": other}

	cases := []struct {
		name, profile, dir, want string
	}{
		{"profile wins when registered", "other", web, "other"},
		{"profile wins over dir match", "web", other, "web"},
		{"unregistered profile falls back to dir", "ghost", web, "web"},
		{"exact dir", "", web, "web"},
		{"nested dir", "", filepath.Join(web, "docs", "deep"), "web"},
		{"longest prefix wins", "", filepath.Join(webApp, "src"), "webapp"},
		{"longest prefix exact", "", webApp, "webapp"},
		{"sibling with shared name prefix does not match", "", web + "-extra", ""},
		{"no match", "", filepath.Join(root, "elsewhere"), ""},
		{"empty dir and profile", "", "", ""},
		{"unclean dir", "", web + "/./docs/../app", "webapp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveProjectAlias(c.profile, c.dir, aliases); got != c.want {
				t.Fatalf("ResolveProjectAlias(%q,%q) = %q, want %q", c.profile, c.dir, got, c.want)
			}
		})
	}
}

func TestProjectAliasEmptyAliases(t *testing.T) {
	for _, aliases := range []map[string]string{nil, {}} {
		if got := ResolveProjectAlias("web", t.TempDir(), aliases); got != "" {
			t.Fatalf("empty aliases = %q, want empty", got)
		}
	}
	// A blank-directory alias is registered by name but never matches a dir.
	if got := ResolveProjectAlias("", t.TempDir(), map[string]string{"blank": ""}); got != "" {
		t.Fatalf("blank-dir alias matched a dir: %q", got)
	}
	if got := ResolveProjectAlias("blank", "", map[string]string{"blank": ""}); got != "blank" {
		t.Fatalf("registered blank-dir alias by profile = %q", got)
	}
	var nilRes *ProjectAliasResolver
	if nilRes.Resolve("a", "/x") != "" || nilRes.Registered("a") {
		t.Fatal("nil resolver must resolve nothing")
	}
}

func TestProjectAliasSymlinkedDir(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(filepath.Join(real, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	// Alias registered by real path; record reached through the link, and the reverse.
	if got := ResolveProjectAlias("", filepath.Join(link, "sub"), map[string]string{"p": real}); got != "p" {
		t.Fatalf("record via symlink = %q, want p", got)
	}
	if got := ResolveProjectAlias("", filepath.Join(real, "sub"), map[string]string{"p": link}); got != "p" {
		t.Fatalf("alias via symlink = %q, want p", got)
	}
}

func TestProjectAliasTieIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	aliases := map[string]string{"b": dir, "a": dir}
	for i := 0; i < 20; i++ {
		if got := ResolveProjectAlias("", dir, aliases); got != "a" {
			t.Fatalf("tie = %q, want a", got)
		}
	}
}

func TestProjectAliasHelpers(t *testing.T) {
	dir := t.TempDir()
	aliases := map[string]string{"p": dir}
	if got := ProjectAliasOfPRD(&PRD{ProjectDir: dir}, aliases); got != "p" {
		t.Fatalf("PRD by dir = %q", got)
	}
	if got := ProjectAliasOfPRD(&PRD{ProjectProfile: "p"}, aliases); got != "p" {
		t.Fatalf("PRD by profile = %q", got)
	}
	if ProjectAliasOfPRD(nil, aliases) != "" || ProjectAliasOfAutomation(nil, aliases) != "" {
		t.Fatal("nil record must have no project")
	}
	if got := ProjectAliasOfAutomation(&Automation{ProjectDir: filepath.Join(dir, "x")}, aliases); got != "p" {
		t.Fatalf("Automation by dir = %q", got)
	}
	// ProjectKey is unchanged: an unmatched record still counts as (none).
	if got := ProjectKey(&PRD{}); got != ProjectKeyNone {
		t.Fatalf("ProjectKey = %q", got)
	}
}

func TestProjectAliasValidate(t *testing.T) {
	aliases := map[string]string{"p": "/x"}
	if err := ValidateProjectAlias("p", aliases); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProjectAlias("  ", aliases); err != nil {
		t.Fatalf("empty project must be valid: %v", err)
	}
	if err := ValidateProjectAlias("ghost", aliases); err == nil {
		t.Fatal("unregistered alias must be rejected")
	}
	if err := ValidateProjectAlias("p", nil); err == nil {
		t.Fatal("no aliases registered must reject a named project")
	}
}
