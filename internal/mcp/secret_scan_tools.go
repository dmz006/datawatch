// Pre-push secret scan (git.secret_scan) MCP surface parity — v9.0.0.
//
// The config was only reachable via the generic config_set tool. These
// dedicated tools add typed parameters and discoverability, mirroring the
// BL220 pattern (detection / dns_channel / proxy config tools):
//
//   secret_scan_config_get  — read the git section of GET /api/config (mode,
//                             detectors, allow-list: paths + fingerprints).
//   secret_scan_config_set  — write via PUT /api/config (applyConfigPatch),
//                             gated behind mcp.allow_self_config like the
//                             generic config_set tool.
//
// Findings are redacted by design (detector, file:line, fingerprint — never
// the matched secret) and surface on the PRD itself: `git.push_warning`
// holds the redacted summary of the last attempt, and the PRD's decisions
// list carries the `git_secret_scan_shadow` / `secret_found` decision rows.

package mcp

import (
	"context"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
)

// ── secret_scan_config_get ──────────────────────────────────────────────────

func (s *Server) toolSecretScanConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("secret_scan_config_get",
		mcpsdk.WithDescription("Read the pre-push secret scan config (git.secret_scan): mode (shadow reports without blocking, enforce blocks the push, off disables), detectors (empty = all built-ins), and the allow-list (path globs + 16/64-hex fingerprints). Findings are redacted (detector, file:line, fingerprint — never the secret) and recorded on the PRD: git.push_warning holds the last summary and the decisions list carries the shadow/secret_found rows. Read-only — no permission gate required."),
	)
}

func (s *Server) handleSecretScanConfigGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.configSection("git")
	if err != nil {
		return toolError("secret_scan_config_get", err), nil
	}
	return textOK(out), nil
}

// ── secret_scan_config_set ──────────────────────────────────────────────────

func (s *Server) toolSecretScanConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("secret_scan_config_set",
		mcpsdk.WithDescription("Update the pre-push secret scan config (git.secret_scan). Changes take effect on the next push — no restart needed. Requires mcp.allow_self_config=true."),
		mcpsdk.WithString("mode", mcpsdk.Description("off, shadow (default: reports findings without blocking the push), or enforce (blocks the push when a secret is found)")),
		mcpsdk.WithString("detectors", mcpsdk.Description("Comma-separated detector names (private_key, aws_access_key, github_token, generic_token, slack_token). Leave unset to keep the current value; reset to all built-ins via the generic config_set with key=git.secret_scan.detectors and an empty value")),
		mcpsdk.WithString("allow_list_paths", mcpsdk.Description("Comma-separated path globs to skip (e.g. vendor, testdata). Leave unset to keep the current value; clear via the generic config_set with key=git.secret_scan.allow_list.paths")),
		mcpsdk.WithString("allow_list_fingerprints", mcpsdk.Description("Comma-separated 16- or 64-char hex fingerprints (sha256 of the secret) to allow. Leave unset to keep the current value; clear via the generic config_set with key=git.secret_scan.allow_list.fingerprints")),
	)
}

func (s *Server) handleSecretScanConfigSet(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapConfigWrite); deny != nil {
		return deny, nil
	}
	if denied := s.selfConfigGate(); denied != nil {
		return denied, nil
	}
	patch := map[string]any{}
	if v := req.GetString("mode", ""); v != "" {
		patch["git.secret_scan.mode"] = v
	}
	if v := req.GetString("detectors", ""); v != "" {
		patch["git.secret_scan.detectors"] = v
	}
	if v := req.GetString("allow_list_paths", ""); v != "" {
		patch["git.secret_scan.allow_list.paths"] = v
	}
	if v := req.GetString("allow_list_fingerprints", ""); v != "" {
		patch["git.secret_scan.allow_list.fingerprints"] = v
	}
	if len(patch) == 0 {
		return textOK("no fields provided — nothing updated"), nil
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/config", patch)
	if err != nil {
		return toolError("secret_scan_config_set", err), nil
	}
	return textOK(string(out)), nil
}
