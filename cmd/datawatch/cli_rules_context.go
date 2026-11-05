// datawatch autonomous rules-context — read and change the rules and
// context settings for Automata over the CLI: the same GET and PUT as
// /api/autonomous/rules-context.

package main

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

const rulesContextCLIPath = "/api/autonomous/rules-context"

func newAutonomousRulesContextCmd() *cobra.Command {
	var prd, dir string
	var asJSON bool
	c := &cobra.Command{
		Use:   "rules-context",
		Short: "Show the rules and context settings for Automata",
		Long:  "Show the rules file, context files, digest mode and digest size limit, resolved for a project directory, and (with --prd) what an Automaton's plan recorded. A read: nothing is written. Use `rules-context set` to change the values.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if prd != "" {
				q.Set("prd", prd)
			}
			if dir != "" {
				q.Set("project_dir", dir)
			}
			body, err := progressGet(rulesContextCLIPath, q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			text, err := autonomous.RenderRulesContextStatusText(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), text)
			return err
		},
	}
	c.Flags().StringVar(&prd, "prd", "", "also show what this Automaton's plan recorded")
	c.Flags().StringVar(&dir, "project-dir", "", "resolve the values for this project directory")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	c.AddCommand(newAutonomousRulesContextSetCmd())
	return c
}

func newAutonomousRulesContextSetCmd() *cobra.Command {
	var rulesFile, mode, dir string
	var ctxFiles []string
	var maxBytes int
	c := &cobra.Command{
		Use:   "set",
		Short: "Change the rules and context settings for Automata",
		Long:  "Change the rules file, context files, digest mode or digest size limit. Only the flags you give are changed. With --project-dir the override for that project is changed instead of the global values. Values are validated before anything is written.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			u := config.RulesContextUpdate{ProjectDir: dir}
			f := cmd.Flags()
			if f.Changed("rules-file") {
				u.RulesFile = &rulesFile
			}
			if f.Changed("context-file") {
				u.ContextFiles = &ctxFiles
			}
			if f.Changed("digest-mode") {
				m := config.NormalizeRulesContextMode(mode)
				u.DigestMode = &m
			}
			if f.Changed("digest-max-bytes") {
				u.DigestMaxBytes = &maxBytes
			}
			cur, err := progressGet(rulesContextCLIPath, nil)
			if err != nil {
				return err
			}
			curCfg, err := autonomous.ParseRulesContextConfig(cur)
			if err != nil {
				return err
			}
			next, err := config.ApplyRulesContextUpdate(curCfg, u)
			if err != nil {
				return err
			}
			path := rulesContextCLIPath
			if dir != "" {
				path += "?" + url.Values{"project_dir": {dir}}.Encode()
			}
			out, err := daemonJSONBody(http.MethodPut, path, next)
			if err != nil {
				return err
			}
			text, err := autonomous.RenderRulesContextStatusText(out)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), text)
			return err
		},
	}
	c.Flags().StringVar(&rulesFile, "rules-file", "", "rules file name, relative to the project")
	c.Flags().StringArrayVar(&ctxFiles, "context-file", nil, "context file name, relative to the project (repeatable)")
	c.Flags().StringVar(&mode, "digest-mode", "", "off, no_tools or all")
	c.Flags().IntVar(&maxBytes, "digest-max-bytes", 0, "digest size limit in bytes (0 = default)")
	c.Flags().StringVar(&dir, "project-dir", "", "change this project directory's override instead of the global values")
	return c
}
