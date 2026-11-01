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
`,
	}
	c.AddCommand(
		newMergePipelineShowCmd(),
		newMergePipelineSetRecordCmd(),
		newMergePipelineClearRecordCmd(),
		newMergePipelineValidateCmd(),
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
			_, err = fmt.Fprint(cmd.OutOrStdout(), mergepipeline.RenderViewText(v))
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
				switch {
				case name == "":
					return fmt.Errorf("--repo-file is empty; leave it bare for the configured file or name a file inside the project")
				case name == "true":
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
