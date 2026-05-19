// v7.0.0 S5 — CLI for the scope-hierarchy memory model.
//
//	datawatch memory scope recall --persona alice --project /home/me/proj --session sess1
//	datawatch memory scope borrow --scope project-shared --project /home/me/proj
//	datawatch memory scope seed --from-scope project-shared --from-project /a --to-scope session-local --to-project /b --to-session sess2
//	datawatch memory scope promote --memory-id 42 --from-scope session-local --to-scope project-shared --persona alice

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func newMemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Memory operations (v7.0.0 S5: scope-hierarchy)",
	}
	cmd.AddCommand(newMemoryScopeCmd())
	cmd.AddCommand(newDiscussionCmd()) // BL332 T42c — discussion scopes
	return cmd
}

func newMemoryScopeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scope",
		Short: "Scope-hierarchy ops: recall / borrow / seed / promote (v7.0.0 S5)",
		Long: `4 ownership/visibility scopes per BL295 Q17:
  persona-global        per persona, ALL projects
  persona-in-project    per persona, current project
  project-shared        current episodic memory (cross-persona)
  session-local         per council run / session

Recall walks top-down + returns merged results with layer attribution.
Borrow reads another scope as read-only context (no copy).
Seed copies entries with optional filter into a target scope.
Promote moves an entry up the hierarchy preserving breadcrumb provenance.`,
	}
	cmd.AddCommand(newMemoryScopeRecallCmd())
	cmd.AddCommand(newMemoryScopeBorrowCmd())
	cmd.AddCommand(newMemoryScopeSeedCmd())
	cmd.AddCommand(newMemoryScopePromoteCmd())
	return cmd
}

func newMemoryScopeRecallCmd() *cobra.Command {
	var persona, project, session string
	var topK int
	cmd := &cobra.Command{
		Use:   "recall",
		Short: "Walk every scope top-down + return merged results with layer attribution",
		RunE: func(*cobra.Command, []string) error {
			q := url.Values{}
			if persona != "" {
				q.Set("persona", persona)
			}
			if project != "" {
				q.Set("project", project)
			}
			if session != "" {
				q.Set("session", session)
			}
			if topK > 0 {
				q.Set("top_k", fmt.Sprintf("%d", topK))
			}
			return daemonGet("/api/memory/scopes/recall?" + q.Encode())
		},
	}
	cmd.Flags().StringVar(&persona, "persona", "", "persona name (omit to skip persona-* layers)")
	cmd.Flags().StringVar(&project, "project", "", "project dir")
	cmd.Flags().StringVar(&session, "session", "", "session id (omit to skip session-local layer)")
	cmd.Flags().IntVar(&topK, "top-k", 10, "max results per layer")
	return cmd
}

func newMemoryScopeBorrowCmd() *cobra.Command {
	var scope, persona, project, session string
	var topK int
	cmd := &cobra.Command{
		Use:   "borrow",
		Short: "Read-only query against another scope (no copy)",
		RunE: func(*cobra.Command, []string) error {
			q := url.Values{}
			q.Set("scope", scope)
			if persona != "" {
				q.Set("persona", persona)
			}
			if project != "" {
				q.Set("project", project)
			}
			if session != "" {
				q.Set("session", session)
			}
			if topK > 0 {
				q.Set("top_k", fmt.Sprintf("%d", topK))
			}
			return daemonGet("/api/memory/scopes/borrow?" + q.Encode())
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "persona-global|persona-in-project|project-shared|session-local")
	cmd.Flags().StringVar(&persona, "persona", "", "persona name (for persona-* scopes)")
	cmd.Flags().StringVar(&project, "project", "", "project dir")
	cmd.Flags().StringVar(&session, "session", "", "session id (for session-local)")
	cmd.Flags().IntVar(&topK, "top-k", 10, "max results")
	_ = cmd.MarkFlagRequired("scope")
	return cmd
}

func newMemoryScopeSeedCmd() *cobra.Command {
	var fromScope, fromPersona, fromProject, fromSession string
	var toScope, toPersona, toProject, toSession string
	var rolePrefix, contentSubstring string
	var limit int
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Copy entries from one scope to another (operator-curated, with filter)",
		RunE: func(*cobra.Command, []string) error {
			body := map[string]any{
				"from": map[string]any{
					"scope": fromScope, "persona": fromPersona, "project": fromProject, "session_id": fromSession,
				},
				"to": map[string]any{
					"scope": toScope, "persona": toPersona, "project": toProject, "session_id": toSession,
				},
				"filter": map[string]any{
					"role_prefix":       rolePrefix,
					"content_substring": contentSubstring,
				},
				"limit": limit,
			}
			return daemonJSON(http.MethodPost, "/api/memory/scopes/seed", body)
		},
	}
	cmd.Flags().StringVar(&fromScope, "from-scope", "", "source scope")
	cmd.Flags().StringVar(&fromPersona, "from-persona", "", "source persona (for persona-* scopes)")
	cmd.Flags().StringVar(&fromProject, "from-project", "", "source project")
	cmd.Flags().StringVar(&fromSession, "from-session", "", "source session id")
	cmd.Flags().StringVar(&toScope, "to-scope", "", "target scope")
	cmd.Flags().StringVar(&toPersona, "to-persona", "", "target persona")
	cmd.Flags().StringVar(&toProject, "to-project", "", "target project")
	cmd.Flags().StringVar(&toSession, "to-session", "", "target session id")
	cmd.Flags().StringVar(&rolePrefix, "role-prefix", "", "filter: role prefix (e.g. persona/security-skeptic)")
	cmd.Flags().StringVar(&contentSubstring, "content-substring", "", "filter: case-insensitive substring on content")
	cmd.Flags().IntVar(&limit, "limit", 100, "max entries to copy")
	_ = cmd.MarkFlagRequired("from-scope")
	_ = cmd.MarkFlagRequired("to-scope")
	return cmd
}

func newMemoryScopePromoteCmd() *cobra.Command {
	var memoryID int64
	var fromScope, fromPersona, fromProject, fromSession string
	var toScope, toPersona, toProject, toSession string
	var promotedBy, persona, run string
	cmd := &cobra.Command{
		Use:   "promote",
		Short: "Move a memory up the hierarchy preserving breadcrumb provenance",
		RunE: func(*cobra.Command, []string) error {
			body := map[string]any{
				"memory_id": memoryID,
				"from": map[string]any{
					"scope": fromScope, "persona": fromPersona, "project": fromProject, "session_id": fromSession,
				},
				"to": map[string]any{
					"scope": toScope, "persona": toPersona, "project": toProject, "session_id": toSession,
				},
				"breadcrumb": map[string]any{
					"persona":     persona,
					"run":         run,
					"promoted_by": promotedBy,
				},
			}
			return daemonJSON(http.MethodPost, "/api/memory/scopes/promote", body)
		},
	}
	cmd.Flags().Int64Var(&memoryID, "memory-id", 0, "id of the memory to promote (required)")
	cmd.Flags().StringVar(&fromScope, "from-scope", "", "source scope (required)")
	cmd.Flags().StringVar(&fromPersona, "from-persona", "", "source persona")
	cmd.Flags().StringVar(&fromProject, "from-project", "", "source project")
	cmd.Flags().StringVar(&fromSession, "from-session", "", "source session id")
	cmd.Flags().StringVar(&toScope, "to-scope", "", "target scope (required)")
	cmd.Flags().StringVar(&toPersona, "to-persona", "", "target persona")
	cmd.Flags().StringVar(&toProject, "to-project", "", "target project")
	cmd.Flags().StringVar(&toSession, "to-session", "", "target session id")
	cmd.Flags().StringVar(&promotedBy, "promoted-by", "operator", "who/what initiated the promotion")
	cmd.Flags().StringVar(&persona, "persona", "", "persona attribution for breadcrumb")
	cmd.Flags().StringVar(&run, "run", "", "run id attribution for breadcrumb")
	_ = cmd.MarkFlagRequired("memory-id")
	_ = cmd.MarkFlagRequired("from-scope")
	_ = cmd.MarkFlagRequired("to-scope")
	return cmd
}

var _ = json.Marshal
