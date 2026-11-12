// `datawatch packs` — installable standard-task and Automaton template packs.
// Thin REST proxies over /api/packs (internal/server/api_packs.go).

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

var packInputLevels = []string{"global", "project", "automaton"}

func validPackLevel(level string) error {
	for _, l := range packInputLevels {
		if l == level {
			return nil
		}
	}
	return fmt.Errorf("level must be one of %s, got %q", strings.Join(packInputLevels, ", "), level)
}

// packInputsPath builds /api/packs/inputs?level=&pack=[&key=] after checking
// the level and that project/automaton levels carry a key.
func packInputsPath(level, pack, key string) (string, error) {
	if err := validPackLevel(level); err != nil {
		return "", err
	}
	if pack == "" {
		return "", fmt.Errorf("pack is required")
	}
	if level != "global" && key == "" {
		return "", fmt.Errorf("--key is required for the %s level (the project directory or Automaton id)", level)
	}
	q := url.Values{}
	q.Set("level", level)
	q.Set("pack", pack)
	if key != "" {
		q.Set("key", key)
	}
	return "/api/packs/inputs?" + q.Encode(), nil
}

func newPacksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "packs",
		Short: "Manage installable standard-task and Automaton template packs",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "enable",
			Short: "Turn packs on (checks the connected registry sources)",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				return daemonJSON(http.MethodPut, "/api/packs/config", map[string]any{"enabled": true})
			},
		},
		&cobra.Command{
			Use:   "disable",
			Short: "Turn packs off",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				return daemonJSON(http.MethodPut, "/api/packs/config", map[string]any{"enabled": false})
			},
		},
		&cobra.Command{
			Use:   "list",
			Short: "List installed packs with their trust state",
			Args:  cobra.NoArgs,
			RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/packs") },
		},
		&cobra.Command{
			Use:   "install <source-dir>",
			Short: "Install a pack from a folder",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonJSON(http.MethodPost, "/api/packs/install", map[string]any{"source_dir": args[0]})
			},
		},
		&cobra.Command{
			Use:   "accept <name> <version>",
			Short: "Record operator acceptance of an installed pack",
			Args:  cobra.ExactArgs(2),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonJSON(http.MethodPost, "/api/packs/accept", map[string]any{"name": args[0], "version": args[1]})
			},
		},
		&cobra.Command{
			Use:   "remove <name> <version>",
			Short: "Remove an installed pack",
			Args:  cobra.ExactArgs(2),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonJSON(http.MethodDelete, "/api/packs/"+url.PathEscape(args[0])+"/"+url.PathEscape(args[1]), nil)
			},
		},
		newPacksInputsCmd(),
	)
	return cmd
}

func newPacksInputsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inputs",
		Short: "Get or set pack inputs at the global, project or automaton level",
		Long: `Pack inputs are stored per level: global, project (key = project directory)
or automaton (key = Automaton id). Secret inputs are shown as references.`,
	}

	var getKey string
	get := &cobra.Command{
		Use:   "get <level> <pack> [name]",
		Short: "Show the stored inputs of one pack at one level (optionally one input)",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(_ *cobra.Command, args []string) error {
			path, err := packInputsPath(args[0], args[1], getKey)
			if err != nil {
				return err
			}
			if len(args) == 3 {
				return daemonGetInput(path, args[2])
			}
			return daemonGet(path)
		},
	}
	get.Flags().StringVar(&getKey, "key", "", "project directory or Automaton id (required for project and automaton levels)")

	var setKey string
	var replace bool
	set := &cobra.Command{
		Use:   "set <level> <pack> <name>=<value>...",
		Short: "Set one or more inputs of a pack at one level",
		Long: `Sets the named inputs and keeps the other stored inputs at that level.
Use --replace to replace the whole scope with exactly the given inputs.`,
		Args: cobra.MinimumNArgs(3),
		RunE: func(_ *cobra.Command, args []string) error {
			path, err := packInputsPath(args[0], args[1], setKey)
			if err != nil {
				return err
			}
			vals := map[string]string{}
			if !replace {
				cur, err := daemonJSONBody(http.MethodGet, path, nil)
				if err != nil {
					return err
				}
				var env struct {
					Inputs map[string]string `json:"inputs"`
				}
				if err := json.Unmarshal(cur, &env); err != nil {
					return fmt.Errorf("unexpected response reading current inputs: %w", err)
				}
				for k, v := range env.Inputs {
					vals[k] = v
				}
			}
			for _, kv := range args[2:] {
				name, value, ok := strings.Cut(kv, "=")
				if !ok || name == "" {
					return fmt.Errorf("input %q must be name=value", kv)
				}
				vals[name] = value
			}
			return daemonJSON(http.MethodPut, path, map[string]any{"inputs": vals})
		},
	}
	set.Flags().StringVar(&setKey, "key", "", "project directory or Automaton id (required for project and automaton levels)")
	set.Flags().BoolVar(&replace, "replace", false, "replace the whole scope instead of merging")

	cmd.AddCommand(get, set)
	return cmd
}

// daemonGetInput fetches an inputs scope and prints the one named input.
func daemonGetInput(path, name string) error {
	body, err := daemonJSONBody(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	var env struct {
		Inputs map[string]string `json:"inputs"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	v, ok := env.Inputs[name]
	if !ok {
		return fmt.Errorf("input %q is not set at this level", name)
	}
	fmt.Println(v)
	return nil
}
