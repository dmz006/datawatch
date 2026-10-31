// PWA coverage for the Identity card's backups and restore: the backups to
// keep field and its help link, the DOCS_LINKS_BY_KEY entry, the manual's
// heading and definition for it, and the locale keys in all five bundles.
// app.js and the manual are read as text.

package server

import (
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

const identityBackupsDocsAnchor = "identity-backups-and-restore"

// identityPWAKeys are the locale keys the Identity card's backups and restore
// controls use, all of which must exist in every bundle.
var identityPWAKeys = []string{
	"identity_btn_revert",
	"identity_btn_reset",
	"identity_btn_restore",
	"identity_reset_confirm",
	"identity_reset_done_toast",
	"identity_restore_title",
	"identity_restore_none",
	"identity_restore_confirm",
	"identity_restore_done_toast",
	"identity_restore_row_fields",
	"identity_backups_keep_saved_toast",
	"identity_backups_keep_invalid",
	"settings_identity_backups_to_keep",
}

func readIdentityAppJS(t *testing.T) string {
	t.Helper()
	raw, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatalf("app.js missing from embed: %v", err)
	}
	return string(raw)
}

func TestIdentityPWA_BackupsToKeepField(t *testing.T) {
	src := readIdentityAppJS(t)
	for _, want := range []string{
		`id="idBackupsKeep" type="number" min="1" step="1" placeholder="10"`,
		"${defsLink('settings_identity_backups_to_keep')}",
		"t('settings_identity_backups_to_keep')",
		"'identity.backups_to_keep': n",
		"cfg.identity.backups_to_keep",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("app.js is missing %q", want)
		}
	}
}

func TestIdentityPWA_Buttons(t *testing.T) {
	src := readIdentityAppJS(t)
	for _, want := range []string{
		`onclick="loadIdentityPanel()">${escHtml(t('identity_btn_revert')`,
		`onclick="identityReset()">${escHtml(t('identity_btn_reset')`,
		`onclick="identityRestoreOpen()">${escHtml(t('identity_btn_restore')`,
		"'/api/identity/reset'",
		"'/api/identity/restore'",
		"'/api/identity/backups'",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("app.js is missing %q", want)
		}
	}
	if strings.Count(src, "identityErrorText(e)") < 4 {
		t.Error("the identity save, wizard, reset and restore failures must show identityErrorText(e)")
	}
}

func TestIdentityPWA_LinkTableAndManual(t *testing.T) {
	src := readIdentityAppJS(t)
	start := strings.Index(src, "const DOCS_LINKS_BY_KEY = {")
	if start < 0 {
		t.Fatal("app.js has no DOCS_LINKS_BY_KEY table")
	}
	end := strings.Index(src[start:], "\n};")
	if end < 0 {
		t.Fatal("DOCS_LINKS_BY_KEY table does not close")
	}
	want := "settings_identity_backups_to_keep: DOCS_DEFINITIONS + '#" + identityBackupsDocsAnchor + "'"
	if !strings.Contains(src[start:start+end], want) {
		t.Errorf("DOCS_LINKS_BY_KEY has no entry %q", want)
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
		if strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-") == identityBackupsDocsAnchor {
			found = true
		}
	}
	if !found {
		t.Errorf("datawatch-definitions.md has no heading for anchor #%s", identityBackupsDocsAnchor)
	}
	if !strings.Contains(string(manual), "`identity.backups_to_keep`") {
		t.Error("datawatch-definitions.md does not define identity.backups_to_keep")
	}
	if prdWord.MatchString(string(manual[strings.Index(string(manual), "#### Identity backups and restore"):][:3000])) {
		t.Error("the Identity backups and restore section must say Automata, not PRD")
	}
}

func TestIdentityPWA_LocaleKeys(t *testing.T) {
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, key := range identityPWAKeys {
			if strings.TrimSpace(bundle[key]) == "" {
				t.Errorf("locale %s: missing or empty key %q", lang, key)
			}
		}
	}
	// A translation is not the English text.
	en := loadLocaleBundle(t, "en")
	for _, lang := range []string{"de", "es", "fr", "ja"} {
		bundle := loadLocaleBundle(t, lang)
		for _, key := range []string{"identity_btn_restore", "identity_restore_none", "identity_reset_confirm", "settings_identity_backups_to_keep"} {
			if bundle[key] == en[key] {
				t.Errorf("locale %s: key %q is still the English text", lang, key)
			}
		}
	}
}
