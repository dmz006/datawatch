// BL404 — imap_mcp config parity: applyConfigPatch had no imap_mcp.*
// cases (PUT silently no-op'd) and handleGetConfig emitted no
// "imap_mcp" block.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestApplyConfigPatch_ImapMcp_AllKeys(t *testing.T) {
	cfg := &config.Config{}
	skipped := applyConfigPatch(cfg, map[string]interface{}{
		"imap_mcp.enabled":        true,
		"imap_mcp.url":            "http://localhost:8765",
		"imap_mcp.account":        "work",
		"imap_mcp.subject_prefix": "dw",
		"imap_mcp.token":          "${secret:imap_token}",
		"imap_mcp.to":             "ops@example.com",
	})
	for _, k := range skipped {
		if strings.HasPrefix(k, "imap_mcp.") {
			t.Errorf("imap_mcp key skipped: %s", k)
		}
	}
	got := cfg.ImapMcp
	if !got.Enabled {
		t.Errorf("enabled not applied")
	}
	if got.URL != "http://localhost:8765" {
		t.Errorf("url=%q", got.URL)
	}
	if got.Account != "work" {
		t.Errorf("account=%q", got.Account)
	}
	if got.SubjectPrefix != "dw" {
		t.Errorf("subject_prefix=%q", got.SubjectPrefix)
	}
	if got.Token != "${secret:imap_token}" {
		t.Errorf("token=%q", got.Token)
	}
	if got.To != "ops@example.com" {
		t.Errorf("to=%q", got.To)
	}
}

func TestApplyConfigPatch_ImapMcp_TokenPreserved(t *testing.T) {
	for _, v := range []string{"", "***"} {
		cfg := &config.Config{}
		cfg.ImapMcp.Token = "${secret:imap_token}"
		applyConfigPatch(cfg, map[string]interface{}{"imap_mcp.token": v})
		if cfg.ImapMcp.Token != "${secret:imap_token}" {
			t.Errorf("token overwritten by %q: got %q", v, cfg.ImapMcp.Token)
		}
	}
}

func TestApplyConfigPatch_ImapMcp_ExplicitClear(t *testing.T) {
	cfg := &config.Config{}
	cfg.ImapMcp.URL = "http://localhost:8765"
	cfg.ImapMcp.To = "ops@example.com"
	skipped := applyConfigPatch(cfg, map[string]interface{}{
		"imap_mcp.url": "",
		"imap_mcp.to":  "",
	})
	if len(skipped) != 0 {
		t.Errorf("unexpected skipped: %v", skipped)
	}
	if cfg.ImapMcp.URL != "" {
		t.Errorf("url not cleared: %q", cfg.ImapMcp.URL)
	}
	if cfg.ImapMcp.To != "" {
		t.Errorf("to not cleared: %q", cfg.ImapMcp.To)
	}
}

func TestGetConfig_ImapMcpTokenMasked(t *testing.T) {
	const plaintext = "imap-plaintext-token-xyz"
	s := newTestServer(t, nil, nil)
	s.cfg = &config.Config{}
	s.cfg.ImapMcp = config.ImapMcpConfig{
		Enabled:       true,
		URL:           "http://localhost:8765",
		Account:       "work",
		SubjectPrefix: "datawatch",
		Token:         plaintext,
		To:            "ops@example.com",
	}

	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw := rr.Body.String()
	if strings.Contains(raw, plaintext) {
		t.Fatalf("plaintext token leaked in /api/config body")
	}
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	im, ok := body["imap_mcp"].(map[string]interface{})
	if !ok {
		t.Fatalf("imap_mcp block missing")
	}
	if im["token"] != "***" {
		t.Errorf("token=%v want ***", im["token"])
	}
	if im["enabled"] != true || im["url"] != "http://localhost:8765" || im["account"] != "work" ||
		im["subject_prefix"] != "datawatch" || im["to"] != "ops@example.com" {
		t.Errorf("unexpected imap_mcp block: %v", im)
	}
}
