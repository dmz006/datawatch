package mcp

import (
	"context"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// The rules and context tools proxy GET and PUT /api/autonomous/rules-context.

const rulesContextPath = "/api/autonomous/rules-context"

func (s *Server) toolRulesContextStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("rules_context_status",
		mcpsdk.WithDescription("Read the rules and context settings that shape how Automata are planned: the rules file name, the context file names, the digest mode (off, no_tools or all) and the digest size limit, with the values resolved for a project directory. Name an Automaton to also see what its plan recorded (file counts, missing files, digest size — never file content). Read-only; the same object as GET /api/autonomous/rules-context."),
		mcpsdk.WithString("project_dir",
			mcpsdk.Description("Optional — resolve the values for this project directory (per-project overrides apply). Empty uses the global values.")),
		mcpsdk.WithString("prd",
			mcpsdk.Description("Optional — an Automaton id; adds what its plan recorded.")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolRulesContextSet() mcpsdk.Tool {
	return mcpsdk.NewTool("rules_context_set",
		mcpsdk.WithDescription("Change the rules and context settings for Automata. Only the fields you give are changed; the rest keep their values. Without project_dir the global values change; with project_dir that project's override is changed (created when missing). Values are validated: file names must be relative to the project with no \"..\", at most 8 context files, digest_mode off, no_tools or all, digest_max_bytes 0 or 512 to 65536. Writes the config file; the same change as PUT /api/autonomous/rules-context."),
		mcpsdk.WithString("rules_file", mcpsdk.Description("Rules file name relative to the project directory.")),
		mcpsdk.WithArray("context_files", mcpsdk.Description("Context file names relative to the project directory; an empty list clears them."), mcpsdk.WithStringItems()),
		mcpsdk.WithString("digest_mode", mcpsdk.Description("off, no_tools or all.")),
		mcpsdk.WithNumber("digest_max_bytes", mcpsdk.Description("Digest size limit in bytes; 0 uses the default.")),
		mcpsdk.WithString("project_dir", mcpsdk.Description("Optional — change this project directory's override instead of the global values.")),
	)
}

// RulesContextToolNames lists the rules and context tools, for the parity test.
func RulesContextToolNames() []string { return []string{"rules_context_status", "rules_context_set"} }

func (s *Server) handleRulesContextStatus(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := strings.TrimSpace(req.GetString("project_dir", "")); v != "" {
		q.Set("project_dir", v)
	}
	if v := strings.TrimSpace(req.GetString("prd", "")); v != "" {
		q.Set("prd", v)
	}
	out, err := s.proxyGet(rulesContextPath, q)
	if err != nil {
		return toolError("rules_context_status", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleRulesContextSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	args := req.GetArguments()
	u := config.RulesContextUpdate{ProjectDir: strings.TrimSpace(req.GetString("project_dir", ""))}
	if _, ok := args["rules_file"]; ok {
		v := req.GetString("rules_file", "")
		u.RulesFile = &v
	}
	if raw, ok := args["context_files"]; ok {
		files := []string{}
		switch t := raw.(type) {
		case []any:
			for _, e := range t {
				str, isStr := e.(string)
				if !isStr {
					return toolErrorf("rules_context_set", "context_files must be a list of file names"), nil
				}
				files = append(files, str)
			}
		case string:
			if err := u.SetRulesContextField("context_files", t); err != nil {
				return toolError("rules_context_set", err), nil
			}
			files = *u.ContextFiles
		default:
			return toolErrorf("rules_context_set", "context_files must be a list of file names"), nil
		}
		u.ContextFiles = &files
	}
	if _, ok := args["digest_mode"]; ok {
		v := config.NormalizeRulesContextMode(req.GetString("digest_mode", ""))
		u.DigestMode = &v
	}
	if raw, ok := args["digest_max_bytes"]; ok {
		f, isNum := raw.(float64)
		if !isNum || f != float64(int(f)) {
			return toolErrorf("rules_context_set", "digest_max_bytes must be a whole number"), nil
		}
		n := int(f)
		u.DigestMaxBytes = &n
	}
	cur, err := s.proxyGet(rulesContextPath, nil)
	if err != nil {
		return toolError("rules_context_set", err), nil
	}
	curCfg, err := autonomous.ParseRulesContextConfig(cur)
	if err != nil {
		return toolError("rules_context_set", err), nil
	}
	next, err := config.ApplyRulesContextUpdate(curCfg, u)
	if err != nil {
		return toolError("rules_context_set", err), nil
	}
	path := rulesContextPath
	if u.ProjectDir != "" {
		path += "?" + url.Values{"project_dir": {u.ProjectDir}}.Encode()
	}
	out, err := s.proxyJSON("PUT", path, next)
	if err != nil {
		return toolError("rules_context_set", err), nil
	}
	return textOK(string(out)), nil
}
