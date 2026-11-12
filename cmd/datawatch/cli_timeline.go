// datawatch autonomous timeline [--json] [--project DIR] [--automaton ID] [--stage ID] [--kind KIND] [--tag ID]
//
// Draw the Automata timeline as text bars from the daemon: one line per
// member, every bar scaled to the same chart span, blocked time in 'X', the
// today marker column 'T' and the projected finish line 'P'. The text is the
// one renderer the comm channels use; --json prints the REST body untouched.

package main

import (
	"io"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func newAutonomousTimelineCmd() *cobra.Command {
	var asJSON bool
	var project, automaton, stage, kind, tag string
	c := &cobra.Command{
		Use:   "timeline",
		Short: "Draw the Automata timeline as text bars: work, blocked, today, projected finish",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			for k, v := range map[string]string{"project": project, "automaton": automaton, "stage": stage, "kind": kind, "tag": tag} {
				if v != "" {
					q.Set(k, v)
				}
			}
			body, err := progressGet("/api/autonomous/timeline", q)
			if err != nil {
				return err
			}
			return renderTimeline(cmd.OutOrStdout(), body, asJSON)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	c.Flags().StringVar(&project, "project", "", "draw only the Automata of this project")
	c.Flags().StringVar(&automaton, "automaton", "", "draw only this Automaton (id or title)")
	c.Flags().StringVar(&stage, "stage", "", "draw only this stage (id or title)")
	c.Flags().StringVar(&kind, "kind", "", "draw only the Automata of this kind")
	c.Flags().StringVar(&tag, "tag", "", "draw only the Automata carrying this tag")
	return c
}

func renderTimeline(w io.Writer, body []byte, asJSON bool) error {
	if asJSON {
		_, err := w.Write(body)
		return err
	}
	res, err := autonomous.ParseTimeline(body)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, autonomous.RenderTimelineBars(res))
	return err
}
