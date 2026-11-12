// PWA coverage for the compute node certificate verification settings: the
// three help link table entries and the manual heading they point at, the
// defsLink calls in the node editor, and the locale keys in all five bundles.
// app.js and the manual are read as text.

package server

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const nodeTLSDocsAnchor = "compute-node-tls-verification"

var nodeTLSHelpKeys = []string{
	"compute_tls_ca_file",
	"compute_tls_cert_fingerprint",
	"compute_tls_insecure_skip_verify",
}

var nodeTLSLocaleKeys = []string{
	"compute_tls_ca_file",
	"compute_tls_ca_file_ph",
	"compute_tls_cert_fingerprint",
	"compute_tls_cert_fingerprint_ph",
	"compute_tls_insecure_skip_verify",
	"compute_tls_insecure_skip_verify_warn",
	"compute_tls_group_title",
	"compute_tls_off_badge",
	"compute_tls_off_badge_tip",
	"compute_tls_pinned_badge",
	"compute_tls_ca_badge",
}

func TestNodeTLSSettings_LinkTableEntriesPointAtARealHeading(t *testing.T) {
	src := readQuestionQualityAppJS(t)
	start := strings.Index(src, "const DOCS_LINKS_BY_KEY = {")
	if start < 0 {
		t.Fatal("app.js has no DOCS_LINKS_BY_KEY table")
	}
	end := strings.Index(src[start:], "\n};")
	if end < 0 {
		t.Fatal("DOCS_LINKS_BY_KEY table does not close")
	}
	table := src[start : start+end]
	for _, key := range nodeTLSHelpKeys {
		want := key + ": DOCS_DEFINITIONS + '#" + nodeTLSDocsAnchor + "'"
		if !strings.Contains(table, want) {
			t.Errorf("DOCS_LINKS_BY_KEY has no entry %q", want)
		}
	}

	manual, err := os.ReadFile("../../docs/datawatch-definitions.md")
	if err != nil {
		t.Fatalf("read the definitions manual: %v", err)
	}
	nonSlug := regexp.MustCompile(`[^a-z0-9]+`)
	found := false
	for _, line := range strings.Split(string(manual), "\n") {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-") == nodeTLSDocsAnchor {
			found = true
		}
	}
	if !found {
		t.Errorf("datawatch-definitions.md has no heading for anchor #%s", nodeTLSDocsAnchor)
	}
}

func TestNodeTLSSettings_EditorCallsDefsLinkForEachField(t *testing.T) {
	src := readQuestionQualityAppJS(t)
	for _, key := range nodeTLSHelpKeys {
		call := "defsLink('" + key + "')"
		if !strings.Contains(src, call) {
			t.Errorf("app.js never calls %s", call)
		}
	}
	for _, id := range []string{"computeNewTLSCAFile", "computeNewTLSFingerprint", "computeNewTLSSkip"} {
		if !strings.Contains(src, `id="`+id+`"`) {
			t.Errorf("the node editor has no input with id %q", id)
		}
	}
	for _, field := range []string{"tls_ca_file:", "tls_cert_fingerprint:", "tls_insecure_skip_verify:"} {
		if !strings.Contains(src, field) {
			t.Errorf("the save body never sets %s", field)
		}
	}
}

func TestNodeTLSSettings_LocaleKeysInAllBundles(t *testing.T) {
	src := readQuestionQualityAppJS(t)
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, key := range nodeTLSLocaleKeys {
			value, ok := bundle[key]
			if !ok {
				t.Errorf("locale %s: missing key %q", lang, key)
				continue
			}
			if strings.TrimSpace(value) == "" {
				t.Errorf("locale %s: key %q is empty", lang, key)
			}
		}
	}
	// A key the editor or badge uses must be one of the keys checked above.
	for _, key := range nodeTLSLocaleKeys {
		if !strings.Contains(src, "t('"+key+"')") {
			t.Errorf("app.js never uses t('%s')", key)
		}
	}
	// The translations are real: no bundle repeats the English warning.
	en := loadLocaleBundle(t, "en")["compute_tls_insecure_skip_verify_warn"]
	for _, lang := range requiredLocales {
		if lang == "en" {
			continue
		}
		if loadLocaleBundle(t, lang)["compute_tls_insecure_skip_verify_warn"] == en {
			t.Errorf("locale %s: the opt-out warning is still the English text", lang)
		}
	}
}
