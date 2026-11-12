package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestInstallStatsArchiveNamesMatchRelease cross-checks the archive names
// computed by install/install-stats.sh (primary stats archive and legacy
// main-archive fallback) against the asset names goreleaser publishes, taken
// from the name_template of the corresponding archives entries in
// .goreleaser.yaml. The two must agree, or the installer silently downloads
// the wrong (or missing) release asset.
func TestInstallStatsArchiveNamesMatchRelease(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	goreleaserSrc, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("read .goreleaser.yaml: %v", err)
	}
	installerSrc, err := os.ReadFile(filepath.Join(root, "install", "install-stats.sh"))
	if err != nil {
		t.Fatalf("read install/install-stats.sh: %v", err)
	}

	version := "9.9.9"
	osName := "linux"

	// name_templates for the archives entries we compare against.
	archiveNames, err := goreleaserArchiveNameTemplates(goreleaserSrc, []string{"datawatch", "datawatch-stats"})
	if err != nil {
		t.Fatalf("parse .goreleaser.yaml archives: %v", err)
	}

	// Archive assignments from the installer (primary stats archive and
	// legacy fallback, both built from ${VERSION} and ${GOARCH}).
	archRe := regexp.MustCompile(` ARCHIVE="([^"]+)"`)
	fallArchRe := regexp.MustCompile(`FALLBACK_ARCHIVE="([^"]+)"`)
	installerStats, err := bashArchiveName(t, installerSrc, archRe, "ARCHIVE")
	if err != nil {
		t.Fatalf("parse installer: %v", err)
	}
	installerFallback, err := bashArchiveName(t, installerSrc, fallArchRe, "FALLBACK_ARCHIVE")
	if err != nil {
		t.Fatalf("parse installer: %v", err)
	}

	for _, arch := range []string{"amd64", "arm64"} {
		expectedStats := renderGoReleaserTemplate(archiveNames["datawatch-stats"], version, osName, arch)
		gotStats := renderInstallerName(installerStats, version, arch)
		assertEqualName(t, "stats archive", arch, gotStats, expectedStats)

		expectedLegacy := renderGoReleaserTemplate(archiveNames["datawatch"], version, osName, arch)
		gotLegacy := renderInstallerName(installerFallback, version, arch)
		assertEqualName(t, "legacy fallback archive", arch, gotLegacy, expectedLegacy)

		// Negative case: the main datawatch archive name used where the
		// stats archive name is expected must NOT pass — proves the
		// comparison can detect a divergence.
		if renderer := renderGoReleaserTemplate(archiveNames["datawatch"], version, osName, arch); renderer == expectedStats {
			t.Errorf("negative case broken for %s: main archive name %q is identical to the stats archive name %q", arch, renderer, expectedStats)
		}
		if wrong := renderInstallerName(expectedLegacy, version, arch); wrong == expectedStats {
			t.Errorf("negative case broken for %s: main-archive name %q compared against stats name %q did not report a mismatch", arch, wrong, expectedStats)
		}
	}
}

// goreleaserArchiveNameTemplates extracts the name_template associated with
// each of the given "- id: X" lists under the archives section of
// .goreleaser.yaml.
func goreleaserArchiveNameTemplates(src []byte, ids []string) (map[string]string, error) {
	idRe := regexp.MustCompile(`(?m)^\s*-\s*id:\s*(\S+)\s*$`)
	nameRe := regexp.MustCompile(`(?m)^\s*name_template:\s*["']([^"']*)["']`)
	names := map[string]string{}
	lines := strings.Split(string(src), "\n")
	for i, line := range lines {
		if m := idRe.FindStringSubmatch(line); m != nil {
			// The id opens a list entry; its name_template is the first one
			// that follows it before another id opens a new entry.
			for _, next := range lines[i+1:] {
				if idRe.MatchString(next) {
					break
				}
				if m2 := nameRe.FindStringSubmatch(next); m2 != nil {
					names[m[1]] = m2[1]
					break
				}
			}
		}
	}
	for _, id := range ids {
		if _, ok := names[id]; !ok {
			return nil, &archiveTemplateError{id: id, missing: true}
		}
	}
	return names, nil
}

type archiveTemplateError struct {
	id      string
	missing bool
}

func (e *archiveTemplateError) Error() string {
	return "archives entry or name_template not found for id: " + e.id
}

// bashArchiveName extracts the quoted value of a shell assignment matching
// re from the installer source.
func bashArchiveName(t *testing.T, src []byte, re *regexp.Regexp, label string) (string, error) {
	t.Helper()
	m := re.FindSubmatch(src)
	if m == nil {
		return "", &archiveTemplateError{id: label, missing: true}
	}
	return string(m[1]), nil
}

// renderGoReleaserTemplate renders a goreleaser name_template by substituting
// {{ .Version }}, {{ .Os }} and {{ .Arch }}, then appends the archive format.
func renderGoReleaserTemplate(template, version, osName, arch string) string {
	name := strings.ReplaceAll(template, "{{ .Version }}", version)
	name = strings.ReplaceAll(name, "{{ .Os }}", osName)
	name = strings.ReplaceAll(name, "{{ .Arch }}", arch)
	return name + ".tar.gz"
}

// renderInstallerName renders an installer archive name by substituting
// ${VERSION} and ${GOARCH}.
func renderInstallerName(name, version, goarch string) string {
	name = strings.ReplaceAll(name, "${VERSION}", version)
	name = strings.ReplaceAll(name, "${GOARCH}", goarch)
	return name
}

func assertEqualName(t *testing.T, label, arch, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s name mismatch for %s: installer uses %q but goreleaser publishes %q", label, arch, got, want)
	}
}
