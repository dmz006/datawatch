// BL255 v6.7.0 — CLI subcommands for the skills subsystem.
//
//   datawatch skills registry list
//   datawatch skills registry get <name>
//   datawatch skills registry add <name> <git-url> [--branch main] [--auth-secret-ref ${secret:...}] [--description "..."]
//   datawatch skills registry update <name> [--branch ...] [--auth-secret-ref ...] [--description ...]
//   datawatch skills registry delete <name>
//   datawatch skills registry add-default
//   datawatch skills registry connect <name>
//   datawatch skills registry browse <name>          (alias for available)
//   datawatch skills registry available <name>
//   datawatch skills registry sync <name> [skill...] [--all]
//   datawatch skills registry unsync <name> [skill...] [--all]
//   datawatch skills list
//   datawatch skills get <name>
//   datawatch skills load <name>                     (option D — print markdown)

package main

import (
	"net/http"

	"github.com/spf13/cobra"
)

func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage skill registries and synced skills (BL255)",
		Long: `Skills are markdown-and-script packages (PAI format + datawatch extensions)
that influence how an AI session does its work. Registries hold catalogs of
skills; sync downloads selected skills locally for use by sessions.

Built-in default: PAI (https://github.com/danielmiessler/Personal_AI_Infrastructure).
Add it any time with:  datawatch skills registry add-default

Per the Secrets-Store Rule, --auth-secret-ref must be a ${secret:name} reference
for any private repo. Plaintext tokens are rejected at config load.`,
	}
	cmd.AddCommand(newSkillsRegistryCmd())
	cmd.AddCommand(newSkillsListCmd())
	cmd.AddCommand(newSkillsGetCmd())
	cmd.AddCommand(newSkillsLoadCmd())
	return cmd
}

func newSkillsRegistryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage skill registries",
	}
	cmd.AddCommand(newSkillsRegistryListCmd())
	cmd.AddCommand(newSkillsRegistryGetCmd())
	cmd.AddCommand(newSkillsRegistryAddCmd())
	cmd.AddCommand(newSkillsRegistryUpdateCmd())
	cmd.AddCommand(newSkillsRegistryDeleteCmd())
	cmd.AddCommand(newSkillsRegistryAddDefaultCmd())
	cmd.AddCommand(newSkillsRegistryConnectCmd())
	cmd.AddCommand(newSkillsRegistryBrowseCmd())
	cmd.AddCommand(newSkillsRegistrySyncCmd())
	cmd.AddCommand(newSkillsRegistryUnsyncCmd())
	return cmd
}

func newSkillsRegistryListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured skill registries",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/skills/registries") },
	}
}

func newSkillsRegistryGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get one skill registry by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/skills/registries/" + args[0])
		},
	}
}

func newSkillsRegistryAddCmd() *cobra.Command {
	var branch, authRef, description string
	c := &cobra.Command{
		Use:   "add <name> <git-url>",
		Short: "Add a new git-backed skill registry",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{
				"name":            args[0],
				"url":             args[1],
				"branch":          branch,
				"auth_secret_ref": authRef,
				"description":     description,
				"enabled":         true,
				"kind":            "git",
			}
			return daemonJSON(http.MethodPost, "/api/skills/registries", body)
		},
	}
	c.Flags().StringVar(&branch, "branch", "main", "git branch")
	c.Flags().StringVar(&authRef, "auth-secret-ref", "", "${secret:name} for private repos")
	c.Flags().StringVar(&description, "description", "", "free-form description")
	return c
}

func newSkillsRegistryUpdateCmd() *cobra.Command {
	var branch, authRef, description, url string
	c := &cobra.Command{
		Use:   "update <name>",
		Short: "Update fields on an existing skill registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{
				"name":            args[0],
				"url":             url,
				"branch":          branch,
				"auth_secret_ref": authRef,
				"description":     description,
				"enabled":         true,
				"kind":            "git",
			}
			return daemonJSON(http.MethodPut, "/api/skills/registries/"+args[0], body)
		},
	}
	c.Flags().StringVar(&url, "url", "", "git URL")
	c.Flags().StringVar(&branch, "branch", "", "git branch")
	c.Flags().StringVar(&authRef, "auth-secret-ref", "", "${secret:name}")
	c.Flags().StringVar(&description, "description", "", "description")
	return c
}

func newSkillsRegistryDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a skill registry (cascades synced skills)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/skills/registries/"+args[0], nil)
		},
	}
}

func newSkillsRegistryAddDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-default",
		Short: "Idempotently add the built-in PAI default registry",
		Long: `Adds the canonical PAI registry (danielmiessler/Personal_AI_Infrastructure) to the
local registries list. Idempotent — safe to run any time. Operator can delete the
registry and recreate it via this command.`,
		RunE: func(*cobra.Command, []string) error {
			return daemonJSON(http.MethodPost, "/api/skills/registries/add-default", nil)
		},
	}
}

func newSkillsRegistryConnectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "connect <name>",
		Short: "Shallow-clone the registry repo and refresh available-skills cache",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/skills/registries/"+args[0]+"/connect", nil)
		},
	}
}

func newSkillsRegistryBrowseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "browse <name>",
		Short: "List available skills in a registry (auto-connects if cache empty)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/skills/registries/" + args[0] + "/available")
		},
	}
}

func newSkillsRegistrySyncCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "sync <name> [skill ...]",
		Short: "Sync selected (or --all) skills from a registry",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{"all": all}
			if len(args) > 1 {
				body["skills"] = args[1:]
			}
			return daemonJSON(http.MethodPost, "/api/skills/registries/"+args[0]+"/sync", body)
		},
	}
	c.Flags().BoolVar(&all, "all", false, "sync every available skill in the registry")
	return c
}

func newSkillsRegistryUnsyncCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "unsync <name> [skill ...]",
		Short: "Unsync selected (or --all) skills from a registry",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{"all": all}
			if len(args) > 1 {
				body["skills"] = args[1:]
			}
			return daemonJSON(http.MethodPost, "/api/skills/registries/"+args[0]+"/unsync", body)
		},
	}
	c.Flags().BoolVar(&all, "all", false, "unsync everything from this registry")
	return c
}

func newSkillsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every synced skill across all registries",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/skills") },
	}
}

func newSkillsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a synced skill's manifest + on-disk path",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/skills/" + args[0])
		},
	}
}

func newSkillsLoadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "load <name>",
		Short: "Print a synced skill's markdown content (option D)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/skills/" + args[0] + "/content")
		},
	}
}
