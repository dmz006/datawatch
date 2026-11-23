package server

import (
	"strings"
	"testing"
)

var deployHelpKeys = []string{
	"build_command", "install_command", "binary_path", "restart_command",
	"health_check", "drain_timeout", "health_timeout",
}

func TestDeployHelpEveryKeyHasLinkAndAnchor(t *testing.T) {
	js := requestWatchDocsRead(t, requestWatchAppJSPath)
	doc := requestWatchDocsRead(t, requestWatchDefinitionsPath)
	for _, k := range deployHelpKeys {
		key := "autonomous.deploy." + k
		got, ok := requestWatchDocsLink(js, key)
		if !ok {
			t.Errorf("app.js has no DOCS_LINKS_BY_KEY entry for %q", key)
			continue
		}
		if want := "#" + requestWatchDocsSlug(key); got != want {
			t.Errorf("link for %q = %q, want %q", key, got, want)
		}
		if !requestWatchDocsHasHeading(doc, key) {
			t.Errorf("definitions page has no heading %q", key)
		}
	}
}

func TestDeployHelpCardLinkAndSection(t *testing.T) {
	js := requestWatchDocsRead(t, requestWatchAppJSPath)
	doc := requestWatchDocsRead(t, requestWatchDefinitionsPath)
	got, ok := requestWatchDocsBareLink(js, "deploy_card_title")
	if !ok || got != "#deploy-after-ship" {
		t.Fatalf("deploy_card_title link = %q, %v", got, ok)
	}
	if !requestWatchDocsHasHeading(doc, "Deploy after ship") {
		t.Error("definitions page has no Deploy after ship heading")
	}
	if !strings.Contains(doc, "(#deploy-after-ship)") {
		t.Error("table of contents has no Deploy after ship entry")
	}
	if !strings.Contains(js, "'settings_deploy_'") || !strings.Contains(js, "DOCS_DEPLOY_LINKS") {
		t.Error("app.js has no deploy prefix fallback")
	}
}
