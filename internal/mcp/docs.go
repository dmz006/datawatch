// BL274 Sprint 1, v6.16.0 — MCP tools for Docs-as-MCP-Interface.
//
//   docs_search        — query the docs corpus (vector + BM25 fallback)
//   docs_read          — fetch one section by path + anchor
//   docs_list_howtos   — list runnable how-tos (with exec_provenance flag)
//   docs_apply         — plan-mode in Sprint 1; execute mode lands Sprint 3
//
// All four proxy to the REST endpoints in internal/server/docs.go so
// auth + audit + the actor-aware trust gate stay in one place.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolDocsSearch() mcpsdk.Tool {
	return mcpsdk.NewTool("docs_search",
		mcpsdk.WithDescription("BL274 — search the datawatch docs corpus (datawatch-definitions.md, howtos, architecture, API, agents, etc.). Vector primary + BM25 fallback. Returns ranked excerpts with source/path/anchor for follow-up docs_read calls."),
		mcpsdk.WithString("q", mcpsdk.Required(), mcpsdk.Description("Query string. Natural-language or keyword.")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("Max hits (default 10, max 100)")),
		mcpsdk.WithString("sources", mcpsdk.Description("Comma-separated source filter. Values: 'core', 'skill:<name>', 'plugin:<name>'. Empty = all trusted sources.")),
	)
}
func (s *Server) handleDocsSearch(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	q.Set("q", req.GetString("q", ""))
	if l := req.GetFloat("limit", 0); l > 0 {
		q.Set("limit", fmt.Sprintf("%d", int(l)))
	}
	if src := req.GetString("sources", ""); src != "" {
		q.Set("sources", src)
	}
	out, err := s.proxyGet("/api/docs/search", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolDocsRead() mcpsdk.Tool {
	return mcpsdk.NewTool("docs_read",
		mcpsdk.WithDescription("BL274 — fetch the full markdown body of one section identified by path + anchor (anchor is the slugified heading). The result also carries the doc's See-also footer for navigation."),
		mcpsdk.WithString("path", mcpsdk.Required(), mcpsdk.Description("Doc path relative to the corpus root, e.g. 'howto/secrets-manager.md'.")),
		mcpsdk.WithString("anchor", mcpsdk.Description("Section anchor (slug of the heading), e.g. 'rotating-a-secret'. Empty = preamble.")),
	)
}
func (s *Server) handleDocsRead(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	q.Set("path", req.GetString("path", ""))
	if a := req.GetString("anchor", ""); a != "" {
		q.Set("anchor", a)
	}
	out, err := s.proxyGet("/api/docs/read", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolDocsListHowtos() mcpsdk.Tool {
	return mcpsdk.NewTool("docs_list_howtos",
		mcpsdk.WithDescription("BL274 — list runnable how-tos. Each entry reports has_exec_steps + exec_provenance ('authored' for hand-curated, 'llm_translatable' for the long-tail). Filter by topic via the optional argument."),
		mcpsdk.WithString("topic", mcpsdk.Description("Optional: filter to howtos tagged with this topic.")),
	)
}
func (s *Server) handleDocsListHowtos(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/docs/list-howtos", nil)
	if err != nil {
		return nil, err
	}
	// Topic filter applied client-side here; server returns full list.
	if topic := req.GetString("topic", ""); topic != "" {
		var resp struct {
			Howtos []map[string]interface{} `json:"howtos"`
		}
		if err := json.Unmarshal(out, &resp); err == nil {
			filtered := resp.Howtos[:0]
			for _, h := range resp.Howtos {
				topics, _ := h["topics"].([]interface{})
				for _, t := range topics {
					if s, _ := t.(string); s == topic {
						filtered = append(filtered, h)
						break
					}
				}
			}
			body, _ := json.Marshal(map[string]interface{}{"howtos": filtered})
			return textOK(string(body)), nil
		}
	}
	return textOK(string(out)), nil
}

func (s *Server) toolDocsApply() mcpsdk.Tool {
	return mcpsdk.NewTool("docs_apply",
		mcpsdk.WithDescription("BL274 — produce a concrete MCP-call plan for a how-to with operator-supplied params. v6.16.0 ships plan-only (mode='plan'); execute mode arrives in v6.18.0 (BL274 sprint 3) gated behind the plan-then-execute approval token. Curated howtos return provenance:'authored' steps; the long tail returns provenance:'llm_translated' (also Sprint 3)."),
		mcpsdk.WithString("howto_id", mcpsdk.Required(), mcpsdk.Description("Path to the howto, e.g. 'howto/secrets-manager.md'. Optional '#anchor' suffix for a specific section.")),
		mcpsdk.WithObject("params", mcpsdk.Description("Operator-supplied param map (keys per the howto's exec_params declaration).")),
		mcpsdk.WithString("mode", mcpsdk.Description("'plan' (default, returns the step list) or 'execute' (Sprint 3, requires approval_token).")),
		mcpsdk.WithString("approval_token", mcpsdk.Description("Required for mode='execute'; obtained from a prior plan call.")),
		mcpsdk.WithString("risk_gate", mcpsdk.Description("'default' (one approval per howto) or 'per_step' (operator confirms each step) — Sprint 3.")),
	)
}
func (s *Server) handleDocsApply(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]interface{}{
		"howto_id": req.GetString("howto_id", ""),
		"mode":     req.GetString("mode", "plan"),
	}
	if t := req.GetString("approval_token", ""); t != "" {
		body["approval_token"] = t
	}
	if g := req.GetString("risk_gate", ""); g != "" {
		body["risk_gate"] = g == "true" || g == "per_step"
	}
	// Params come through as an object; pass through verbatim.
	if raw := req.GetArguments(); raw != nil {
		if p, ok := raw["params"]; ok {
			body["params"] = p
		}
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/docs/apply", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ── BL274 v6.22.0 — docs_trust_* MCP tools (audit-honesty backfill) ────────
//
// S1 claimed "Trust commands across all 7 surfaces" but the MCP layer
// shipped with zero trust tools. This module backfills the 6 operator-set
// surfaces (list / add / remove / pending / accept / dismiss / export) so
// MCP achieves real parity with REST + CLI + comm.

func (s *Server) toolDocsTrustList() mcpsdk.Tool {
	return mcpsdk.NewTool("docs_trust_list",
		mcpsdk.WithDescription("BL274 — list trusted docs sources (core + accepted skill: + accepted plugin: tiers). Untrusted sources do not surface in docs_search results."),
	)
}
func (s *Server) handleDocsTrustList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/docs/trust", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolDocsTrustAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("docs_trust_add",
		mcpsdk.WithDescription("BL274 — add a docs source to the trust list. Once trusted, the source's docs land in the index and surface through docs_search/list_howtos."),
		mcpsdk.WithString("source", mcpsdk.Required(), mcpsdk.Description("Source identifier, e.g. 'skill:test-first' or 'plugin:gh-hooks'.")),
		mcpsdk.WithString("granted_by", mcpsdk.Description("Who granted trust (default 'operator').")),
		mcpsdk.WithString("note", mcpsdk.Description("Optional free-text note kept with the trust entry.")),
	)
}
func (s *Server) handleDocsTrustAdd(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]interface{}{"source": req.GetString("source", "")}
	if g := req.GetString("granted_by", ""); g != "" {
		body["granted_by"] = g
	}
	if n := req.GetString("note", ""); n != "" {
		body["note"] = n
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/docs/trust", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolDocsTrustRemove() mcpsdk.Tool {
	return mcpsdk.NewTool("docs_trust_remove",
		mcpsdk.WithDescription("BL274 — remove a source from the trust list. Its docs immediately stop appearing in docs_search results."),
		mcpsdk.WithString("source", mcpsdk.Required(), mcpsdk.Description("Source identifier to untrust.")),
	)
}
func (s *Server) handleDocsTrustRemove(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	src := req.GetString("source", "")
	if src == "" {
		return nil, fmt.Errorf("source required")
	}
	out, err := s.proxyJSON(http.MethodDelete, "/api/docs/trust/"+url.PathEscape(src), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolDocsTrustPending() mcpsdk.Tool {
	return mcpsdk.NewTool("docs_trust_pending",
		mcpsdk.WithDescription("BL274 — list pending-trust queue. Sources auto-discovered (skill:* / plugin:* under ~/.datawatch/) land here on first sight per Q6 all-opt-in trust model. Operator accepts via docs_trust_accept."),
	)
}
func (s *Server) handleDocsTrustPending(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/docs/trust/pending", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolDocsTrustAccept() mcpsdk.Tool {
	return mcpsdk.NewTool("docs_trust_accept",
		mcpsdk.WithDescription("BL274 — accept one or more pending-trust sources. Bulk operation — accepts an array."),
		mcpsdk.WithString("sources", mcpsdk.Required(), mcpsdk.Description("Comma-separated list of sources to trust, e.g. 'skill:test-first,plugin:gh-hooks'.")),
	)
}
func (s *Server) handleDocsTrustAccept(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	srcs := splitCSV(req.GetString("sources", ""))
	if len(srcs) == 0 {
		return nil, fmt.Errorf("sources required")
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/docs/trust/accept", map[string]interface{}{"sources": srcs})
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolDocsTrustDismiss() mcpsdk.Tool {
	return mcpsdk.NewTool("docs_trust_dismiss",
		mcpsdk.WithDescription("BL274 — dismiss one or more pending-trust sources without trusting them. Bulk operation."),
		mcpsdk.WithString("sources", mcpsdk.Required(), mcpsdk.Description("Comma-separated list of sources to dismiss.")),
	)
}
func (s *Server) handleDocsTrustDismiss(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	srcs := splitCSV(req.GetString("sources", ""))
	if len(srcs) == 0 {
		return nil, fmt.Errorf("sources required")
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/docs/trust/dismiss", map[string]interface{}{"sources": srcs})
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

