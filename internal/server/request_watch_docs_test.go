// Glossary coverage for the autonomous.request_watch block: every key the
// config type carries has a definitions section, a docs link in the web UI
// that points at the anchor that section produces, and a settings field that
// uses the same docs key. The key list comes from the yaml tags of the config
// type by reflection, so a key added later without documentation fails here.

package server

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	requestWatchDefinitionsPath = "../../docs/datawatch-definitions.md"
	requestWatchAppJSPath       = "web/app.js"
	requestWatchConceptHeading  = "Model request progress"
	requestWatchGroupDocsKey    = "settings_request_watch"
)

// requestWatchDocsSlug applies the slug rule of the web UI's defsLink: lower
// case, every run of non-alphanumeric characters becomes one hyphen, and the
// hyphens at both ends are trimmed.
func requestWatchDocsSlug(heading string) string {
	nonAlnum := regexp.MustCompile(`[^a-z0-9]+`)
	slug := nonAlnum.ReplaceAllString(strings.ToLower(heading), "-")
	return strings.Trim(slug, "-")
}

// requestWatchDocsKeys lists the yaml keys of config.RequestWatchConfig.
func requestWatchDocsKeys(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeOf(config.RequestWatchConfig{})
	var keys []string
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("yaml")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		keys = append(keys, name)
	}
	if len(keys) == 0 {
		t.Fatal("no yaml keys found on config.RequestWatchConfig")
	}
	return keys
}

func requestWatchDocsRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// requestWatchDocsHeadings returns the text of every markdown heading in doc.
func requestWatchDocsHeadings(doc string) []string {
	re := regexp.MustCompile(`(?m)^#{1,6}[ \t]+(.+?)[ \t]*$`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(doc, -1) {
		out = append(out, m[1])
	}
	return out
}

func requestWatchDocsHasHeading(doc, heading string) bool {
	for _, h := range requestWatchDocsHeadings(doc) {
		if h == heading {
			return true
		}
	}
	return false
}

// requestWatchDocsLink returns the DOCS_LINKS_BY_KEY value written for key in
// app.js, and whether the entry exists.
func requestWatchDocsLink(js, key string) (string, bool) {
	re := regexp.MustCompile(`['"]` + regexp.QuoteMeta(key) + `['"]\s*:\s*DOCS_DEFINITIONS\s*\+\s*['"]([^'"]*)['"]`)
	m := re.FindStringSubmatch(js)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// requestWatchDocsBareLink returns the docs link of a bare identifier key such
// as settings_request_watch.
func requestWatchDocsBareLink(js, key string) (string, bool) {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*:\s*DOCS_DEFINITIONS\s*\+\s*['"]([^'"]*)['"]`)
	m := re.FindStringSubmatch(js)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func TestRequestWatchDocsKeyListIsComplete(t *testing.T) {
	keys := requestWatchDocsKeys(t)
	want := []string{
		"enabled", "poll_seconds", "no_progress_minutes", "observe_workers",
		"observe_verifiers", "use_gpu_signal", "gpu_busy_percent",
		"unreachable_counts", "raise_alert", "raise_question", "remediation_options",
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("yaml keys of config.RequestWatchConfig = %v, want %v", keys, want)
	}
}

func TestRequestWatchDocsEveryKeyHasDefinitionHeading(t *testing.T) {
	doc := requestWatchDocsRead(t, requestWatchDefinitionsPath)
	for _, k := range requestWatchDocsKeys(t) {
		heading := requestWatchKeyPrefix + k
		if !requestWatchDocsHasHeading(doc, heading) {
			t.Errorf("docs/datawatch-definitions.md has no heading %q", heading)
		}
	}
}

func TestRequestWatchDocsEveryKeyHasDocsLinkToItsAnchor(t *testing.T) {
	js := requestWatchDocsRead(t, requestWatchAppJSPath)
	for _, k := range requestWatchDocsKeys(t) {
		key := requestWatchKeyPrefix + k
		got, ok := requestWatchDocsLink(js, key)
		if !ok {
			t.Errorf("app.js has no DOCS_LINKS_BY_KEY entry for %q", key)
			continue
		}
		want := "#" + requestWatchDocsSlug(key)
		if got != want {
			t.Errorf("DOCS_LINKS_BY_KEY[%q] points at %q, want %q", key, got, want)
		}
	}
}

func TestRequestWatchDocsEverySettingsFieldCarriesTheDocsKey(t *testing.T) {
	js := requestWatchDocsRead(t, requestWatchAppJSPath)
	for _, k := range requestWatchDocsKeys(t) {
		key := requestWatchKeyPrefix + k
		re := regexp.MustCompile(`\{\s*key:\s*['"]` + regexp.QuoteMeta(key) + `['"][^\n]*?docsKey:\s*['"]([^'"]*)['"]`)
		m := re.FindStringSubmatch(js)
		if m == nil {
			t.Errorf("app.js has no settings field for %q that carries a docsKey", key)
			continue
		}
		if m[1] != key {
			t.Errorf("settings field %q has docsKey %q, want %q", key, m[1], key)
		}
	}
}

func TestRequestWatchDocsConceptHeadingExists(t *testing.T) {
	doc := requestWatchDocsRead(t, requestWatchDefinitionsPath)
	if !requestWatchDocsHasHeading(doc, requestWatchConceptHeading) {
		t.Fatalf("docs/datawatch-definitions.md has no heading %q", requestWatchConceptHeading)
	}
	if got := requestWatchDocsSlug(requestWatchConceptHeading); got != "model-request-progress" {
		t.Fatalf("slug of %q = %q, want model-request-progress", requestWatchConceptHeading, got)
	}
}

func TestRequestWatchDocsGroupLinkResolvesToConceptHeading(t *testing.T) {
	js := requestWatchDocsRead(t, requestWatchAppJSPath)
	doc := requestWatchDocsRead(t, requestWatchDefinitionsPath)

	got, ok := requestWatchDocsBareLink(js, requestWatchGroupDocsKey)
	if !ok {
		t.Fatalf("app.js has no DOCS_LINKS_BY_KEY entry for %q", requestWatchGroupDocsKey)
	}
	if want := "#" + requestWatchDocsSlug(requestWatchConceptHeading); got != want {
		t.Fatalf("DOCS_LINKS_BY_KEY[%q] points at %q, want %q", requestWatchGroupDocsKey, got, want)
	}

	// Some heading in the definitions file must produce that anchor.
	found := false
	for _, h := range requestWatchDocsHeadings(doc) {
		if "#"+requestWatchDocsSlug(h) == got {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no heading in docs/datawatch-definitions.md produces the anchor %q", got)
	}

	// The settings group row must carry the same docs key.
	groupRe := regexp.MustCompile(`\{[^\n]*docsKey:\s*['"]` + regexp.QuoteMeta(requestWatchGroupDocsKey) + `['"][^\n]*type:\s*['"]group['"]`)
	if !groupRe.MatchString(js) {
		t.Errorf("app.js has no settings group row with docsKey %q", requestWatchGroupDocsKey)
	}
}

func TestRequestWatchDocsSlugRule(t *testing.T) {
	cases := map[string]string{
		"Model request progress":                    "model-request-progress",
		"autonomous.request_watch.poll_seconds":     "autonomous-request-watch-poll-seconds",
		"  --Spaced__Out!!  ":                       "spaced-out",
		"autonomous.request_watch.use_gpu_signal":   "autonomous-request-watch-use-gpu-signal",
		"autonomous.request_watch.gpu_busy_percent": "autonomous-request-watch-gpu-busy-percent",
	}
	for in, want := range cases {
		if got := requestWatchDocsSlug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}
