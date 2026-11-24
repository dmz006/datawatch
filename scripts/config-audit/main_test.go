package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// copyFixture copies testdata/fixture into a temp dir so a test can edit it.
func copyFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := "testdata/fixture"
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runAudit(t *testing.T, root, exemptions string, extra ...string) (int, string, string) {
	t.Helper()
	args := []string{"-root", root}
	if exemptions != "" {
		args = append(args, "-exemptions", exemptions)
	}
	args = append(args, extra...)
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestConfigKeysListsLeavesFromTheStruct(t *testing.T) {
	root := copyFixture(t)
	got, err := configKeys(filepath.Join(root, "internal/config"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"embedded.embedded_one", "hostname", "labels", "list", "no_get", "no_put", "no_ref", "no_web",
		"ptr.flag", "server.limits.max", "server.port", "server.tls", "shared_one", "untagged", "wait",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keys\n got %v\nwant %v", got, want)
	}
}

func TestRefKeysReadsCommentedKeysAndIgnoresProseAndListItems(t *testing.T) {
	root := copyFixture(t)
	ks, err := refKeys(filepath.Join(root, "docs/config-reference.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"hostname", "server.limits.max", "labels", "list", "wait", "shared_one"} {
		if !ks.covers(k) {
			t.Errorf("ref should cover %s", k)
		}
	}
	if ks.covers("no_ref") {
		t.Error("a prose comment that only mentions a key name is not a key line")
	}
}

func TestWebKeysCoversArraysAndOtherRowsButNotInternalRows(t *testing.T) {
	root := copyFixture(t)
	ks, err := webKeys(filepath.Join(root, "internal/server/web/app.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"hostname", "server.port", "server.tls", "labels", "server.limits.max"} {
		if !ks.covers(k) {
			t.Errorf("web should cover %s", k)
		}
	}
	if ks.covers("no_web") {
		t.Error("no_web has no Settings row")
	}
	if ks.covers("_cert_source") {
		t.Error("rows whose key starts with an underscore are not configuration keys")
	}
}

func TestGetKeysFlattensNestedHelperAndIndexAssignedMaps(t *testing.T) {
	root := copyFixture(t)
	ks, err := getKeys(filepath.Join(root, "internal/server"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"hostname", "server.port", "server.tls", "server.limits.max", "ptr.flag", "list", "labels", "wait"} {
		if !ks.covers(k) {
			t.Errorf("GET should cover %s", k)
		}
	}
	if ks.covers("no_get") {
		t.Error("no_get is not in the GET map")
	}
	if ks.covers("not_a_key") {
		t.Error("a word in a comment is not a key")
	}
}

func TestPutKeysReadsCaseLabelsAndPrefixHandlers(t *testing.T) {
	root := copyFixture(t)
	ks, err := putKeys(filepath.Join(root, "internal/server"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"hostname", "no_get", "server.limits.max", "list", "labels", "labels"} {
		if !ks.covers(k) {
			t.Errorf("PUT should cover %s", k)
		}
	}
	if ks.covers("no_put") {
		t.Error("no_put is not accepted by PUT")
	}
}

func TestAuditReportsOneGapPerSurface(t *testing.T) {
	root := copyFixture(t)
	code, out, errs := runAudit(t, root, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	for _, want := range []string{"no_ref", "no_web", "no_get", "no_put"} {
		if !strings.Contains(out, want) {
			t.Errorf("table should name %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hostname") {
		t.Errorf("a fully covered key must not be listed without --all:\n%s", out)
	}
	if !strings.Contains(out, "15 keys checked, 4 gaps, 0 exempt") {
		t.Errorf("summary line missing or wrong:\n%s", out)
	}
	// each missing key is MISSING on exactly its own surface
	for key, col := range map[string]int{"no_ref": 1, "no_web": 2, "no_get": 3, "no_put": 4} {
		var line string
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, key+" ") {
				line = l
			}
		}
		f := strings.Fields(line)
		if len(f) != 5 {
			t.Fatalf("row for %s: %q", key, line)
		}
		for i := 1; i <= 4; i++ {
			if (f[i] == "MISSING") != (i == col) {
				t.Errorf("%s column %d = %s", key, i, f[i])
			}
		}
	}
}

func TestAllFlagListsEveryKey(t *testing.T) {
	root := copyFixture(t)
	_, out, _ := runAudit(t, root, "", "-all")
	if !strings.Contains(out, "hostname") {
		t.Errorf("-all should list covered keys:\n%s", out)
	}
}

func TestExemptionsClearTheGapsAndExitZero(t *testing.T) {
	root := copyFixture(t)
	ex := filepath.Join(root, "ex.txt")
	writeFile(t, ex, "# header\n"+
		"no_ref | ref | the reference lists struct keys elsewhere\n"+
		"no_web | web | internal only\n"+
		"no_get | get | write only\n"+
		"no_put | put | read only\n")
	code, out, errs := runAudit(t, root, ex)
	if code != 0 {
		t.Fatalf("exit %d, want 0\n%s\n%s", code, out, errs)
	}
	if !strings.Contains(out, "15 keys checked, 0 gaps, 4 exempt") {
		t.Errorf("summary wrong:\n%s", out)
	}
}

func TestExemptionForTheWrongSurfaceDoesNotClearTheGap(t *testing.T) {
	root := copyFixture(t)
	ex := filepath.Join(root, "ex.txt")
	writeFile(t, ex, "no_ref | web | wrong surface\nno_web | web | ok\nno_get | get | ok\nno_put | put | ok\n")
	code, _, _ := runAudit(t, root, ex)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}

func TestPrefixAndStarExemptions(t *testing.T) {
	root := copyFixture(t)
	ex := filepath.Join(root, "ex.txt")
	// no_* is not a prefix form; only a trailing .* is. Use * for all surfaces.
	writeFile(t, ex, "no_ref | * | a\nno_web | * | a\nno_get | * | a\nno_put | * | a\n")
	if code, out, errs := runAudit(t, root, ex); code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errs)
	}
	root2 := copyFixture(t)
	writeFile(t, filepath.Join(root2, "internal/server/web/app.js"), "const GENERAL_CONFIG_FIELDS = [\n];\n")
	ex2 := filepath.Join(root2, "ex.txt")
	// every key is missing from web; one prefix covers server.*, the rest by name
	writeFile(t, ex2, "server.* | web | settings page has one server card\n")
	_, out, _ := runAudit(t, root2, ex2)
	if strings.Contains(out, "server.port") || strings.Contains(out, "server.limits.max") {
		t.Errorf("prefix exemption should clear server.*:\n%s", out)
	}
	if !strings.Contains(out, "hostname") {
		t.Errorf("keys outside the prefix still gap:\n%s", out)
	}
}

func TestExemptionWithoutReasonExitsTwo(t *testing.T) {
	root := copyFixture(t)
	ex := filepath.Join(root, "ex.txt")
	writeFile(t, ex, "no_ref | ref |   \n")
	code, _, errs := runAudit(t, root, ex)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errs, "reason") {
		t.Errorf("stderr should say a reason is required: %s", errs)
	}
}

func TestExemptionWithUnknownSurfaceExitsTwo(t *testing.T) {
	root := copyFixture(t)
	ex := filepath.Join(root, "ex.txt")
	writeFile(t, ex, "no_ref | nowhere | reason\n")
	if code, _, _ := runAudit(t, root, ex); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestStaleExemptionWarnsButDoesNotFail(t *testing.T) {
	root := copyFixture(t)
	ex := filepath.Join(root, "ex.txt")
	writeFile(t, ex, "no_ref | ref | a\nno_web | web | a\nno_get | get | a\nno_put | put | a\nhostname | web | no longer a gap\ngone.key | * | key was removed\n")
	code, _, errs := runAudit(t, root, ex)
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(errs, "stale") || !strings.Contains(errs, "hostname") || !strings.Contains(errs, "gone.key") {
		t.Errorf("stale exemptions should be warned about:\n%s", errs)
	}
}

func TestNoExemptionsFlagIgnoresTheFile(t *testing.T) {
	root := copyFixture(t)
	ex := filepath.Join(root, "ex.txt")
	writeFile(t, ex, "no_ref | ref | a\nno_web | web | a\nno_get | get | a\nno_put | put | a\n")
	code, out, _ := runAudit(t, root, ex, "-no-exemptions")
	if code != 1 || !strings.Contains(out, "4 gaps, 0 exempt") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestBrokenForwarderFailsTheChannelCheck(t *testing.T) {
	root := copyFixture(t)
	p := filepath.Join(root, "internal/mcp/memory_tools.go")
	b, _ := os.ReadFile(p)
	writeFile(t, p, strings.ReplaceAll(string(b), "MethodPut", "MethodPost"))
	ex := filepath.Join(root, "ex.txt")
	writeFile(t, ex, "no_ref | ref | a\nno_web | web | a\nno_get | get | a\nno_put | put | a\n")
	code, out, _ := runAudit(t, root, ex)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (a channel stopped forwarding to PUT)", code)
	}
	if !strings.Contains(out, "mcp") {
		t.Errorf("output should name the mcp channel:\n%s", out)
	}
}

func TestChannelsReportOkWhenForwardersAreGeneric(t *testing.T) {
	root := copyFixture(t)
	_, out, _ := runAudit(t, root, "")
	for _, ch := range []string{"configure", "cli", "mcp"} {
		if !strings.Contains(out, ch+": ok") {
			t.Errorf("channel %s should report ok:\n%s", ch, out)
		}
	}
}

func TestGapTableFileGroupsBySurfaceAndFeature(t *testing.T) {
	root := copyFixture(t)
	gt := filepath.Join(t.TempDir(), "gaps.md")
	code, _, _ := runAudit(t, root, "", "-no-exemptions", "-gap-table", gt)
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	b, err := os.ReadFile(gt)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"## Gaps by surface", "### ref", "### web", "### get", "### put", "## Gaps by feature", "| feature | keys with a gap |", "no_ref", "no_put"} {
		if !strings.Contains(s, want) {
			t.Errorf("gap table missing %q:\n%s", want, s)
		}
	}
}

func TestOnlyNamedFilesAreRead(t *testing.T) {
	root := copyFixture(t)
	// unrelated broken Go files and a deep tree beside the real ones must never be touched
	writeFile(t, filepath.Join(root, "internal/other/broken.go"), "package other\nfunc (((\n")
	writeFile(t, filepath.Join(root, "node_modules/x/y/z/deep.js"), "key: 'hostname'\n")
	writeFile(t, filepath.Join(root, "internal/config/zz_test.go"), "package config\nfunc (((\n")
	code, _, errs := runAudit(t, root, "")
	if code != 1 || errs != "" {
		t.Fatalf("exit %d stderr %q: an unrelated or test file was read", code, errs)
	}
}

func TestMissingInputFileExitsTwo(t *testing.T) {
	root := copyFixture(t)
	if err := os.Remove(filepath.Join(root, "docs/config-reference.yaml")); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runAudit(t, root, ""); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
