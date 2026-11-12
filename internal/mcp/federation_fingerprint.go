// MCP tools for the fingerprint match records surfaced for operator
// decision (docs/plans/2026-10-14-concurrency-and-budget-design.md §3c).
//
// Each tool wraps the corresponding /api/federation/fingerprint-matches
// endpoint via the in-process HTTP loopback, so REST and MCP share one
// implementation:
//
//	GET  /api/federation/fingerprint-matches           → federation_fingerprint_matches
//	POST /api/federation/fingerprint-matches/{id}/confirm → federation_fingerprint_confirm
//	POST /api/federation/fingerprint-matches/{id}/reject  → federation_fingerprint_reject
//
// A match is a *suggestion* that two daemons hold the same resource;
// these tools let an operator confirm or reject it. Nothing here acts
// on a match automatically.

package mcp

import (
	"context"
	"fmt"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
)

func (s *Server) RegisterFederationFingerprintTools() {
	if s.srv == nil {
		return
	}
	tracked := func(fn func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			result, err := fn(ctx, req)
			if s.chanStats != nil {
				reqSize := len(fmt.Sprintf("%v", req.Params.Arguments))
				respSize := 0
				if result != nil {
					for _, c := range result.Content {
						if tc, ok := c.(mcpsdk.TextContent); ok {
							respSize += len(tc.Text)
						}
					}
				}
				s.chanStats.RecordRecv(reqSize)
				s.chanStats.RecordSent(respSize)
				if err != nil {
					s.chanStats.RecordError()
				}
			}
			return result, err
		}
	}

	s.srv.AddTool(s.toolFingerprintMatches(), tracked(s.handleFingerprintMatchesMCP))
	s.srv.AddTool(s.toolFingerprintConfirm(), tracked(s.handleFingerprintConfirmMCP))
	s.srv.AddTool(s.toolFingerprintReject(), tracked(s.handleFingerprintRejectMCP))
}

func (s *Server) toolFingerprintMatches() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_fingerprint_matches",
		mcpsdk.WithDescription("List fingerprint match records between this daemon and its peers — "+
			"each proposes that two daemons hold the same resource (node/disk/billing kind only; "+
			"identities are shown as 8-char hash prefixes, never full values). "+
			"state is optional: pending | confirmed | rejected; omit for all."),
		mcpsdk.WithString("state", mcpsdk.Description("Optional filter: pending, confirmed or rejected")),
	)
}

func (s *Server) handleFingerprintMatchesMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationList); deny != nil {
		return deny, nil
	}
	q := url.Values{}
	if state := req.GetString("state", ""); state != "" {
		q.Set("state", state)
	}
	out, err := s.proxyGet("/api/federation/fingerprint-matches", q)
	if err != nil {
		return toolError("federation_fingerprint_matches", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolFingerprintConfirm() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_fingerprint_confirm",
		mcpsdk.WithDescription("Confirm one fingerprint match record (operator decision). "+
			"Once confirmed, the matching budget pools may be treated by the ledger "+
			"as the same resource; a confirmation may later be reverted by reject."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Match record id")),
	)
}

func (s *Server) handleFingerprintConfirmMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationWrite); deny != nil {
		return deny, nil
	}
	id := req.GetString("id", "")
	out, err := s.proxyJSON("POST", "/api/federation/fingerprint-matches/"+id+"/confirm", nil)
	if err != nil {
		return toolError("federation_fingerprint_confirm", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolFingerprintReject() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_fingerprint_reject",
		mcpsdk.WithDescription("Reject one fingerprint match record (operator decision). "+
			"Rejecting a previously confirmed match takes its resource pools back apart."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Match record id")),
	)
}

func (s *Server) handleFingerprintRejectMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationWrite); deny != nil {
		return deny, nil
	}
	id := req.GetString("id", "")
	out, err := s.proxyJSON("POST", "/api/federation/fingerprint-matches/"+id+"/reject", nil)
	if err != nil {
		return toolError("federation_fingerprint_reject", err), nil
	}
	return textOK(string(out)), nil
}
