// F10 sprint 2 S2.6 — `datawatch profile …` CLI parity.
//
// All subcommands talk to the running daemon via REST (same pattern as
// `datawatch sessions …`). Requires the daemon to be up; a clear error
// surfaces when it isn't so the user knows to `datawatch start` first.
//
// Subcommands:
//   datawatch profile project {list, show, create, update, delete, smoke}
//   datawatch profile cluster {list, show, create, update, delete, smoke}
//
// Input shape for create/update: either --file <path> (reads JSON or
// YAML) or stdin. Output shape: pretty-printed JSON by default;
// --format yaml when the user prefers. `list` defaults to a compact
// summary table.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// newProfileCmd is the top-level `datawatch profile` command; real work
// lives in the project/cluster subtrees.
func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage Project and Cluster profiles",
		Long: `Project profiles describe what work a session does (git repo,
image pair, memory policy). Cluster profiles describe where it runs
(docker / k8s / cf). A session references one of each at spawn time.

Requires the daemon to be running on the current host.`,
	}
	cmd.AddCommand(newProfileProjectCmd(), newProfileClusterCmd())
	return cmd
}

func newProfileProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage Project Profiles",
	}
	cmd.AddCommand(
		newProfileListCmd("project"),
		newProfileShowCmd("project"),
		newProfileCreateCmd("project"),
		newProfileUpdateCmd("project"),
		newProfileDeleteCmd("project"),
		newProfileSmokeCmd("project"),
		newProfileAgentSettingsCmd(),
	)
	return cmd
}

func newProfileClusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage Cluster Profiles",
	}
	cmd.AddCommand(
		newProfileListCmd("cluster"),
		newProfileShowCmd("cluster"),
		newProfileCreateCmd("cluster"),
		newProfileUpdateCmd("cluster"),
		newProfileDeleteCmd("cluster"),
		newProfileSmokeCmd("cluster"),
	)
	return cmd
}

// ── subcommand builders ────────────────────────────────────────────────

func newProfileListCmd(kind string) *cobra.Command {
	var formatFlag string
	cmd := &cobra.Command{
		Use:   "list",
		Short: fmt.Sprintf("List %s profiles", kind),
		RunE: func(_ *cobra.Command, _ []string) error {
			body, err := profileCLIGet("/api/profiles/" + pluralize(kind))
			if err != nil {
				return err
			}
			return renderProfileOutput(body, formatFlag, "profiles")
		},
	}
	cmd.Flags().StringVarP(&formatFlag, "format", "f", "table", "Output format: table|json|yaml")
	return cmd
}

func newProfileShowCmd(kind string) *cobra.Command {
	var formatFlag string
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: fmt.Sprintf("Show one %s profile", kind),
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, err := profileCLIGet("/api/profiles/" + pluralize(kind) + "/" + args[0])
			if err != nil {
				return err
			}
			return renderProfileOutput(body, formatFlag, "")
		},
	}
	cmd.Flags().StringVarP(&formatFlag, "format", "f", "json", "Output format: json|yaml")
	return cmd
}

func newProfileCreateCmd(kind string) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "create",
		Short: fmt.Sprintf("Create a %s profile from JSON/YAML (stdin or --file)", kind),
		Long: `Reads the profile body as JSON or YAML from --file (or stdin when --file
is absent) and POSTs to /api/profiles. Example:

  cat <<EOF | datawatch profile project create
  name: my-proj
  git:
    url: https://github.com/example/repo
    branch: main
  image_pair:
    agent: agent-claude
    sidecar: lang-go
  memory:
    mode: sync-back
  EOF`,
		RunE: func(_ *cobra.Command, _ []string) error {
			data, err := readProfileInput(file)
			if err != nil {
				return err
			}
			body, err := profileCLIPost("/api/profiles/"+pluralize(kind), data)
			if err != nil {
				return err
			}
			return renderProfileOutput(body, "json", "")
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Read profile body from file (default: stdin)")
	return cmd
}

func newProfileUpdateCmd(kind string) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: fmt.Sprintf("Update a %s profile from JSON/YAML", kind),
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			data, err := readProfileInput(file)
			if err != nil {
				return err
			}
			body, err := profileCLIPut("/api/profiles/"+pluralize(kind)+"/"+args[0], data)
			if err != nil {
				return err
			}
			return renderProfileOutput(body, "json", "")
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Read profile body from file (default: stdin)")
	return cmd
}

func newProfileDeleteCmd(kind string) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: fmt.Sprintf("Delete a %s profile", kind),
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if _, err := profileCLIDelete("/api/profiles/" + pluralize(kind) + "/" + args[0]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Deleted %s profile %q\n", kind, args[0])
			return nil
		},
	}
}

// newProfileAgentSettingsCmd — BL251: datawatch profile project agent-settings <name>
// PATCH /api/profiles/projects/{name}/agent-settings
func newProfileAgentSettingsCmd() *cobra.Command {
	var claudeKeySecret, ollamaURL, ollamaModel string
	cmd := &cobra.Command{
		Use:   "agent-settings <name>",
		Short: "Set AgentSettings on a Project Profile (BL251)",
		Long: `Update the AgentSettings block on a Project Profile without replacing the entire profile.

  --claude-key-secret   Name of the secret containing ANTHROPIC_API_KEY (injected into claude-code agents)
  --ollama-url          Ollama base URL for opencode agents (injected as OPENCODE_PROVIDER_URL)
  --model               Model name for opencode agents (injected as OPENCODE_MODEL)

Omitted flags are cleared. To leave a field unchanged, pass its current value.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, err := json.Marshal(map[string]string{
				"claude_auth_key_secret": claudeKeySecret,
				"opencode_ollama_url":    ollamaURL,
				"opencode_model":         ollamaModel,
			})
			if err != nil {
				return err
			}
			respBody, err := profileCLIPatch("/api/profiles/projects/"+args[0]+"/agent-settings", body)
			if err != nil {
				return err
			}
			return renderProfileOutput(respBody, "json", "")
		},
	}
	cmd.Flags().StringVar(&claudeKeySecret, "claude-key-secret", "", "Secret name for ANTHROPIC_API_KEY")
	cmd.Flags().StringVar(&ollamaURL, "ollama-url", "", "Ollama base URL (OPENCODE_PROVIDER_URL)")
	cmd.Flags().StringVar(&ollamaModel, "model", "", "Model name (OPENCODE_MODEL)")
	return cmd
}

func newProfileSmokeCmd(kind string) *cobra.Command {
	return &cobra.Command{
		Use:   "smoke <name>",
		Short: fmt.Sprintf("Run validation smoke test on a %s profile", kind),
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, err := profileCLIPost("/api/profiles/"+pluralize(kind)+"/"+args[0]+"/smoke", nil)
			if err != nil {
				return err
			}
			// Decode enough to set exit code based on Passed-ness
			var r struct {
				Errors []string `json:"errors"`
			}
			_ = json.Unmarshal(body, &r)
			if err := renderProfileOutput(body, "json", ""); err != nil {
				return err
			}
			if len(r.Errors) > 0 {
				os.Exit(2) // distinguish validation failure from CLI failure
			}
			return nil
		},
	}
}

// ── plumbing ───────────────────────────────────────────────────────────

func pluralize(kind string) string {
	switch kind {
	case "project":
		return "projects"
	case "cluster":
		return "clusters"
	}
	return kind
}

// profileCLIGet + Post + Put + Delete talk to the daemon at the address
// from config.Server.Host:Port (falling back to 127.0.0.1:8080). Body
// may be nil for methods that don't send one.
func profileCLIGet(path string) ([]byte, error)    { return profileCLIDo("GET", path, nil) }
func profileCLIPost(path string, body []byte) ([]byte, error) {
	return profileCLIDo("POST", path, body)
}
func profileCLIPut(path string, body []byte) ([]byte, error)   { return profileCLIDo("PUT", path, body) }
func profileCLIPatch(path string, body []byte) ([]byte, error) { return profileCLIDo("PATCH", path, body) }
func profileCLIDelete(path string) ([]byte, error)             { return profileCLIDo("DELETE", path, nil) }

func profileCLIDo(method, path string, body []byte) ([]byte, error) {
	addr, token := daemonAddressForCLI()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, addr+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contact daemon at %s: %w (is it running?)", addr, err)
	}
	defer resp.Body.Close()
	buf := &bytes.Buffer{}
	_, _ = io.Copy(buf, resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("daemon returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(buf.String()))
	}
	return buf.Bytes(), nil
}

// daemonAddressForCLI resolves the local daemon URL + optional auth
// token from the loaded config, falling back to http://127.0.0.1:8080
// when config can't be read.
func daemonAddressForCLI() (addr, token string) {
	cfg, _ := loadConfig()
	if cfg == nil {
		return "http://127.0.0.1:8080", ""
	}
	host := cfg.Server.Host
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	scheme := "http"
	port := cfg.Server.Port
	if port == 0 {
		port = 8080
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, port), cfg.Server.Token
}

// readProfileInput reads a JSON or YAML profile body from the given
// path or, when path is empty, stdin. YAML is normalized to JSON
// before the REST request so the server gets canonical input.
func readProfileInput(path string) ([]byte, error) {
	var raw []byte
	var err error
	if path == "" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read profile body: %w", err)
	}
	// If it parses as JSON, ship it as-is
	if json.Valid(raw) {
		return raw, nil
	}
	// Otherwise treat as YAML
	var anyVal interface{}
	if err := yaml.Unmarshal(raw, &anyVal); err != nil {
		return nil, fmt.Errorf("profile body is neither JSON nor YAML: %w", err)
	}
	// A profile body must deserialize to a map — scalar/list inputs
	// like "random text" would parse as a yaml string and slip
	// through json.Marshal. Reject them explicitly.
	converted := yamlToJSONCompat(anyVal)
	if _, ok := converted.(map[string]interface{}); !ok {
		return nil, fmt.Errorf("profile body must be a JSON object or YAML mapping, got %T", converted)
	}
	j, err := json.Marshal(converted)
	if err != nil {
		return nil, fmt.Errorf("convert YAML to JSON: %w", err)
	}
	return j, nil
}

// yamlToJSONCompat walks the YAML decode output and converts
// map[interface{}]interface{} (YAML v2 default) to
// map[string]interface{} (required by json.Marshal). yaml/v3 returns
// map[string]interface{} directly so this is mostly a no-op, kept in
// case the project ever pins an older yaml lib.
func yamlToJSONCompat(v interface{}) interface{} {
	switch t := v.(type) {
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			out[fmt.Sprintf("%v", k)] = yamlToJSONCompat(val)
		}
		return out
	case map[string]interface{}:
		for k, val := range t {
			t[k] = yamlToJSONCompat(val)
		}
		return t
	case []interface{}:
		for i, val := range t {
			t[i] = yamlToJSONCompat(val)
		}
		return t
	default:
		return v
	}
}

// renderProfileOutput writes body to stdout in the requested format.
// listKey, when non-empty, is the JSON key of a profile list to render
// as a compact table when format=="table".
func renderProfileOutput(body []byte, format, listKey string) error {
	switch format {
	case "json", "":
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, body, "", "  "); err != nil {
			// not parseable JSON — dump raw
			fmt.Println(string(body))
			return nil
		}
		fmt.Println(pretty.String())
		return nil
	case "yaml":
		var v interface{}
		if err := json.Unmarshal(body, &v); err != nil {
			return err
		}
		out, err := yaml.Marshal(v)
		if err != nil {
			return err
		}
		fmt.Print(string(out))
		return nil
	case "table":
		if listKey != "" {
			return renderProfileTable(body, listKey)
		}
		// fall through to JSON for single-profile requests
		return renderProfileOutput(body, "json", "")
	default:
		return fmt.Errorf("unknown format %q (want table|json|yaml)", format)
	}
}

// renderProfileTable pretty-prints a profile list as two columns:
// name and a short summary. Intentionally compact; full shape via
// `show <name>` or `--format json`.
func renderProfileTable(body []byte, listKey string) error {
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return err
	}
	items := []map[string]interface{}{}
	if err := json.Unmarshal(wrapper[listKey], &items); err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("(no profiles)")
		return nil
	}
	fmt.Printf("%-30s  %s\n", "NAME", "SUMMARY")
	fmt.Printf("%-30s  %s\n", strings.Repeat("-", 30), strings.Repeat("-", 40))
	for _, item := range items {
		name, _ := item["name"].(string)
		summary := profileSummary(item)
		fmt.Printf("%-30s  %s\n", name, summary)
	}
	return nil
}

// profileSummary picks one-line highlights from a profile so the
// table is scannable: for projects → agent + sidecar; for clusters →
// kind + context/namespace.
func profileSummary(item map[string]interface{}) string {
	// Project?
	if pair, ok := item["image_pair"].(map[string]interface{}); ok {
		agent, _ := pair["agent"].(string)
		sidecar, _ := pair["sidecar"].(string)
		if sidecar == "" {
			sidecar = "(solo)"
		}
		return fmt.Sprintf("%s + %s", agent, sidecar)
	}
	// Cluster?
	if kind, ok := item["kind"].(string); ok {
		ctx, _ := item["context"].(string)
		ns, _ := item["namespace"].(string)
		if ns == "" {
			ns = "default"
		}
		return fmt.Sprintf("kind=%s context=%s ns=%s", kind, ctx, ns)
	}
	return ""
}
