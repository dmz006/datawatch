// datawatch autonomous merge-pipeline show|set-record|clear-record|validate
//
// The merge pipeline that would run for a project, the pipeline record the
// daemon keeps for it, and a check of a pipeline before it is saved. The text
// is rendered by the same functions the MCP tools and the chat verbs use
// (internal/mergepipeline view.go); --json prints the REST body untouched.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/mergepipeline"
)

const (
	mergePipelineAPI = "/api/autonomous/merge-pipeline"
	// mergePipelineFileMax is the most pipeline text the daemon accepts.
	mergePipelineFileMax = 1 << 20
)

// mergePipelineSend sends one request to a merge pipeline route and returns
// the body of a 2xx reply.
func mergePipelineSend(method, path string, q url.Values, body []byte) ([]byte, error) {
	u := daemonURL() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon not reachable (%s): %w", daemonURL(), err)
	}
	defer resp.Body.Close() //nolint:errcheck
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// readPipelineText reads pipeline text from a file, or from standard input
// when path is "-". It refuses empty text and text larger than the daemon
// accepts.
func readPipelineText(cmd *cobra.Command, path string) (string, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(io.LimitReader(cmd.InOrStdin(), mergePipelineFileMax+1))
	} else {
		var f *os.File
		if f, err = os.Open(path); err == nil { // #nosec G304 -- the operator names the file to read
			defer f.Close() //nolint:errcheck
			data, err = io.ReadAll(io.LimitReader(f, mergePipelineFileMax+1))
		}
	}
	if err != nil {
		return "", fmt.Errorf("could not read the pipeline from %s: %w", path, err)
	}
	if len(data) > mergePipelineFileMax {
		return "", fmt.Errorf("the pipeline in %s is larger than %d bytes", path, mergePipelineFileMax)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", fmt.Errorf("the pipeline in %s is empty", path)
	}
	return string(data), nil
}

// mergePipelineProjectRequiredLine is the plain line the CLI and the MCP tool
// both add when the daemon refuses a merge pipeline read because no project
// is known (no project argument and no merge base repository set).
const mergePipelineProjectRequiredLine = "a project is needed, pass the project argument or set the merge base repository"

const mergePipelineHTTP400Prefix = "HTTP 400: "

// mergePipelineProjectRequired reports whether a non-2xx error from a merge
// pipeline read is the daemon's structured project_required refusal: an HTTP
// 400 whose body parses as JSON with code project_required. The second result
// is the body the error text carries. A body that is not JSON, every other
// status and every other code never match and keep today's error text.
func mergePipelineProjectRequired(err error) (body string, ok bool) {
	if err == nil {
		return "", false
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, mergePipelineHTTP400Prefix) {
		return "", false
	}
	body = strings.TrimSpace(msg[len(mergePipelineHTTP400Prefix):])
	var code struct {
		Code string `json:"code"`
	}
	if json.Unmarshal([]byte(body), &code) != nil || code.Code != "project_required" {
		return "", false
	}
	return body, true
}

// mergePipelineProjectRequiredError builds the error returned for the
// project_required refusal: the daemon's JSON body followed by one plain line.
func mergePipelineProjectRequiredError(body string) error {
	return fmt.Errorf("%s\n%s", body, mergePipelineProjectRequiredLine)
}

// mergePipelineConflictConfirmLine is the confirmation line of the show text:
// the mode, how many of the project's first model-resolved conflicts need the
// operator's confirmation, and how many are counted and remaining. It is empty
// unless confirmation is enabled, so a confirmation that is off prints nothing.
func mergePipelineConflictConfirmLine(c mergepipeline.ConflictConfirmationView) string {
	if !c.Enabled {
		return ""
	}
	counted, remaining := "unknown", "unknown"
	if c.Counted != nil {
		counted = strconv.Itoa(*c.Counted)
	}
	if c.Remaining != nil {
		remaining = strconv.Itoa(*c.Remaining)
	}
	return fmt.Sprintf("Conflict confirmation: mode %s, the first %d model-resolved conflicts need confirmation, counted %s, remaining %s\n",
		c.Mode, c.ConfirmFirstN, counted, remaining)
}

func newAutonomousMergePipelineCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "merge-pipeline",
		Short: "Show, record or check the merge pipeline of a project",
		Long: `The merge pipeline is the list of steps a merge run goes through. It comes from the
project's repository file, else from a record the daemon keeps, else from the built-in
pipeline; the order is a setting.

  show [--project P]                      The pipeline in effect and why
  set-record --from-file F [--project P]  Keep a pipeline record for the project (F is - for stdin)
  clear-record [--project P]              Remove the project's record
  validate --from-file F | --repo-file    Check a pipeline without saving it
  post-ship show                          The post-ship hook block: switch, hooks, targets
  post-ship set KEY VALUE                 Set one key of it (the daemon validates the value)

  POST-SHIP KEY: enabled, registry_file, registry_row_pattern, companion_repository,
  issue_title_template, notify_template (text; empty clears), hooks (a JSON array of
  {name, builtin or command, enabled} objects; empty removes all).
`,
	}
	c.AddCommand(
		newMergePipelineShowCmd(),
		newMergePipelineSetRecordCmd(),
		newMergePipelineClearRecordCmd(),
		newMergePipelineValidateCmd(),
		newMergePipelinePostShipCmd(),
	)
	return c
}

func newMergePipelineShowCmd() *cobra.Command {
	var project string
	var asJSON bool
	c := &cobra.Command{
		Use:   "show",
		Short: "Show the merge pipeline in effect for a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if p := strings.TrimSpace(project); p != "" {
				q.Set("project", p)
			}
			body, err := mergePipelineSend(http.MethodGet, mergePipelineAPI, q, nil)
			if err != nil {
				if b, ok := mergePipelineProjectRequired(err); ok {
					cmd.SilenceUsage = true // the body already says what is needed; no usage dump
					if asJSON {
						// The raw daemon body belongs on stdout; the error names what is needed.
						if _, werr := cmd.OutOrStdout().Write([]byte(b)); werr != nil {
							return werr
						}
					}
					return mergePipelineProjectRequiredError(b)
				}
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			v, err := mergepipeline.ParseView(body)
			if err != nil {
				return err
			}
			text := mergepipeline.RenderViewText(v) + mergePipelineConflictConfirmLine(v.ConflictConfirmation)
			_, err = fmt.Fprint(cmd.OutOrStdout(), text)
			return err
		},
	}
	c.Flags().StringVar(&project, "project", "", "the project (default: the merge base repository)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newMergePipelineSetRecordCmd() *cobra.Command {
	var project, from string
	var asJSON bool
	c := &cobra.Command{
		Use:   "set-record",
		Short: "Keep a merge pipeline record for a project, used when its repository has no pipeline file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(from) == "" {
				return fmt.Errorf("--from-file is required: give the YAML file that holds the pipeline, or - for standard input")
			}
			text, err := readPipelineText(cmd, from)
			if err != nil {
				return err
			}
			req := map[string]any{"yaml": text}
			if p := strings.TrimSpace(project); p != "" {
				req["project"] = p
			}
			payload, _ := json.Marshal(req)
			body, err := mergePipelineSend(http.MethodPut, mergePipelineAPI+"/record", nil, payload)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			rec, err := mergepipeline.ParseRecordReport(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), mergepipeline.RenderRecordText(rec))
			return err
		},
	}
	c.Flags().StringVar(&project, "project", "", "the project (default: the merge base repository)")
	c.Flags().StringVar(&from, "from-file", "", "YAML file that holds the pipeline; - reads standard input")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newMergePipelineClearRecordCmd() *cobra.Command {
	var project string
	var asJSON bool
	c := &cobra.Command{
		Use:   "clear-record",
		Short: "Remove the merge pipeline record the daemon keeps for a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if p := strings.TrimSpace(project); p != "" {
				q.Set("project", p)
			}
			body, err := mergePipelineSend(http.MethodDelete, mergePipelineAPI+"/record", q, nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var done struct {
				Project string `json:"project"`
			}
			if err := json.Unmarshal(body, &done); err != nil {
				return fmt.Errorf("the daemon's reply could not be read: %w", err)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), mergepipeline.RenderRecordClearedText(done.Project))
			return err
		},
	}
	c.Flags().StringVar(&project, "project", "", "the project (default: the merge base repository)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newMergePipelineValidateCmd() *cobra.Command {
	var project, from, repoFile string
	var asJSON bool
	c := &cobra.Command{
		Use:   "validate",
		Short: "Check a merge pipeline, or a project's repository pipeline file, without saving it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fromSet := strings.TrimSpace(from) != ""
			repoSet := cmd.Flags().Changed("repo-file")
			switch {
			case fromSet && repoSet:
				return fmt.Errorf("check a pipeline file you give (--from-file) or the repository's (--repo-file), not both")
			case !fromSet && !repoSet:
				return fmt.Errorf("say what to check: --from-file <file> (- for standard input) or --repo-file")
			}
			req := map[string]any{}
			if p := strings.TrimSpace(project); p != "" {
				req["project"] = p
			}
			if fromSet {
				text, err := readPipelineText(cmd, from)
				if err != nil {
					return err
				}
				req["yaml"] = text
			} else {
				name := strings.TrimSpace(repoFile)
				switch name {
				case "":
					return fmt.Errorf("--repo-file is empty; leave it bare for the configured file or name a file inside the project")
				case "true":
					req["file"] = true
				default:
					req["file"] = name
				}
			}
			payload, _ := json.Marshal(req)
			body, err := mergePipelineSend(http.MethodPost, mergePipelineAPI+"/validate", nil, payload)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			rep, err := mergepipeline.ParseValidation(body)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), mergepipeline.RenderValidationText(rep)); err != nil {
				return err
			}
			if !rep.Valid {
				// The text above names every problem; a failing exit status lets a script act on it.
				cmd.SilenceErrors, cmd.SilenceUsage = true, true
				return fmt.Errorf("the pipeline is not valid")
			}
			return nil
		},
	}
	c.Flags().StringVar(&project, "project", "", "the project (default: the merge base repository)")
	c.Flags().StringVar(&from, "from-file", "", "YAML file that holds the pipeline to check; - reads standard input")
	c.Flags().StringVar(&repoFile, "repo-file", "", "check the project's repository pipeline file; bare for the configured name, or --repo-file=NAME for a file inside the project")
	c.Flags().Lookup("repo-file").NoOptDefVal = "true"
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// mergePipelinePostShipKeys are the keys of the autonomous.merge_pipeline
// post_ship block. The command lines and the daemon must agree on them; the
// daemon is the last word on a value.
var mergePipelinePostShipKeys = []string{
	"enabled",
	"registry_file",
	"registry_row_pattern",
	"companion_repository",
	"issue_title_template",
	"notify_template",
	"hooks",
}

// newMergePipelinePostShipCmd is the `merge-pipeline post-ship` group: the
// post-ship hooks of an Automaton pipeline, read with show and written one
// key at a time with set.
func newMergePipelinePostShipCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "post-ship",
		Short: "Post-ship hooks: show the block, or set one key of it",
	}
	c.AddCommand(newMergePipelinePostShipShowCmd(), newMergePipelinePostShipSetCmd())
	return c
}

// newMergePipelinePostShipShowCmd is `merge-pipeline post-ship show`: the
// post-ship hook block as the daemon keeps it -- the switch, each hook with
// its built-in or its command and whether it is on, and the targets the
// built-ins use.
func newMergePipelinePostShipShowCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show",
		Short: "Show the post-ship hook block (switch, hooks, targets)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := mergePipelineSend(http.MethodGet, "/api/config", nil, nil)
			if err != nil {
				return err
			}
			detail, err := mergepipeline.PostShipDetailFromBody(body)
			if err != nil {
				return err
			}
			if asJSON {
				if detail == nil {
					detail = []byte("null")
				}
				_, err = cmd.OutOrStdout().Write(detail)
				return err
			}
			if detail == nil {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "Post-ship hooks: the daemon does not report a post-ship block")
				return err
			}
			d, perr := mergepipeline.ParsePostShipDetail(detail)
			if perr != nil {
				return perr
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), mergepipeline.RenderPostShipDetailText(d))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON block instead of the text")
	return c
}

// newMergePipelinePostShipSetCmd is `merge-pipeline post-ship set KEY VALUE`:
// one key of the post-ship block, sent as a PUT /api/config write; the daemon
// validates the value.
func newMergePipelinePostShipSetCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Set one key of the post-ship hook block",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := args[0], args[1]
			payload, err := postShipSetPayload(key, value)
			if err != nil {
				return err
			}
			body, err := mergePipelineSend(http.MethodPut, "/api/config", nil, payload)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			show := value
			if key == "enabled" {
				if b, berr := parsePostShipBool(value); berr == nil {
					if b {
						show = "true"
					} else {
						show = "false"
					}
				}
			}
			if len(show) > 60 {
				show = show[:57] + "..."
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Sent autonomous.merge_pipeline.post_ship.%s = %s to the daemon\n", key, show)
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the daemon reply (status ok) as raw JSON")
	return c
}

// postShipSetPayload builds the one-key PUT /api/config body for a post-ship
// set. It checks the key locally so a typo is caught without a round trip;
// the daemon still validates the value and is the last word on it.
func postShipSetPayload(key, value string) ([]byte, error) {
	known := false
	for _, k := range mergePipelinePostShipKeys {
		if k == key {
			known = true
			break
		}
	}
	if !known {
		return nil, fmt.Errorf("unknown post-ship key %q; use one of: %s", key, strings.Join(mergePipelinePostShipKeys, ", "))
	}
	full := "autonomous.merge_pipeline.post_ship." + key
	payload := map[string]any{}
	switch key {
	case "enabled":
		b, err := parsePostShipBool(value)
		if err != nil {
			return nil, err
		}
		payload[full] = b
	case "hooks":
		if text := strings.TrimSpace(value); text != "" {
			var arr []map[string]any
			if err := json.Unmarshal([]byte(text), &arr); err != nil {
				return nil, fmt.Errorf("hooks must be a JSON array of {name, builtin or command, enabled} objects (or empty to remove all): %v", err)
			}
		}
		payload[full] = value
	default:
		payload[full] = value
	}
	return json.Marshal(payload)
}

// parsePostShipBool reads the enabled value as a boolean; the daemon expects
// a JSON boolean for it, so anything but true or false is refused here.
func parsePostShipBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("enabled takes true or false, not %q", value)
}
