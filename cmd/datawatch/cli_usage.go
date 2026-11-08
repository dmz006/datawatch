// cli_usage.go — the usage calculator's CLI (docs/plans/2026-10-23-usage-calculator-design.md,
// "Shipping variants side by side behind a setting"). Beside cli_budget.go, which formats
// GET /api/budget the same way: the daemon's GET /api/usage/advice body is the reference
// model's own AdviceResult, and internal/usagecalc renders it as the CLI text form —
// the CLI never recomputes or rewords anything.
//
//	 usage advice [--variant V] [--policy P] [--lanes N] [--json]
//	 usage reading add --percent N | --spend-usd N [--at RFC3339] [--session-percent N] [--lanes N (deprecated, ignored)]
//	 usage reading list
//	 usage reading delete <id>
//	 usage extra-credit <points>
//	 usage remaining-work <points|clear>
//
// The variant and policy of one call are validated locally against the package's own
// lists before a request goes out (the daemon enforces the same two lists) and never
// stored: `usage advice --variant lane-planner` does not save anything.

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

func newUsageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Weekly usage allowance: advice, readings, extra credit, remaining work",
		Long: `The weekly usage allowance: 100 points per week plus any bought extra credit,
read against the operator's weekly readings.

"usage advice" shows the reference model's verdict for the running lanes;
"usage reading" manages the readings the verdict is computed from;
"usage extra-credit" sets the bought extra credit the ceiling is raised by;
"usage remaining-work" sets the typed remaining-work figure of the
run-flat-out policy.`,
	}
	cmd.AddCommand(newUsageAdviceCmd())
	cmd.AddCommand(newUsageReadingCmd())
	cmd.AddCommand(newUsageExtraCreditCmd())
	cmd.AddCommand(newUsageRemainingWorkCmd())
	cmd.AddCommand(newUsageBurnGuardCmd())
	cmd.AddCommand(newUsageAutoLanesCmd())
	return cmd
}

// ----- advice --------------------------------------------------------------

func newUsageAdviceCmd() *cobra.Command {
	var variant, policy string
	var lanes int
	var asJSON bool
	c := &cobra.Command{
		Use:   "advice",
		Short: "Show this week's usage advice (verdict, pace, burn, run-out)",
		Long: "GETs the reference model's AdviceResult from the daemon and prints it as the\n" +
			"CLI text form: one sentence per field, in the design doc's field order.\n" +
			"--variant and --policy pick one call each and are validated against the\n" +
			"calculator's own variant and policy lists; they are not stored.\n" +
			"--lanes is the what-if lane count (0 included); it is not saved.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			q := url.Values{}
			if v := strings.TrimSpace(variant); v != "" {
				if !slices.Contains(usagecalc.Variants(), v) {
					return fmt.Errorf("unknown variant %q; one of: %s", v, strings.Join(usagecalc.Variants(), ", "))
				}
				q.Set("variant", v)
			}
			if p := strings.TrimSpace(policy); p != "" {
				if !slices.Contains(usagecalc.Policies(), p) {
					return fmt.Errorf("unknown policy %q; one of: %s", p, strings.Join(usagecalc.Policies(), ", "))
				}
				q.Set("policy", p)
			}
			if c.Flags().Changed("lanes") {
				if lanes < 0 {
					return fmt.Errorf("lanes must be 0 or more")
				}
				q.Set("lanes", strconv.Itoa(lanes))
			}
			path := "/api/usage/advice"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			body, err := daemonJSONBody(http.MethodGet, path, nil)
			if err != nil {
				return err
			}
			if asJSON {
				// The daemon's body byte for byte — no re-encoding, so the compact
				// shape and order it sent are exactly what a pipe sees, with a
				// terminating newline if the body did not carry one.
				w := c.OutOrStdout()
				if _, err := w.Write(body); err != nil {
					return err
				}
				if len(body) == 0 || body[len(body)-1] != '\n' {
					if _, err := w.Write([]byte("\n")); err != nil {
						return err
					}
				}
				return nil
			}
			var resp usagecalc.Response
			if err := json.Unmarshal(body, &resp); err != nil {
				return fmt.Errorf("the daemon returned a body that is not an AdviceResult: %w", err)
			}
			_, err = fmt.Fprint(c.OutOrStdout(), usagecalc.FormatCLI(resp))
			return err
		},
	}
	f := c.Flags()
	f.StringVar(&variant, "variant", "",
		"display variant for this one call: pace-gauge, lane-planner, burn-ledger or all (validated; not stored)")
	f.StringVar(&policy, "policy", "",
		"policy for this one call: use-all, pace or finish-early (validated; not stored)")
	f.IntVar(&lanes, "lanes", 0, "what-if lane count: advise as if this many lanes were running (0 or more; not saved; default: the lanes the ledger counts)")
	f.BoolVar(&asJSON, "json", false, "print the raw AdviceResult body as JSON instead of the text form")
	return c
}

// ----- readings -------------------------------------------------------------

func newUsageReadingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reading",
		Short: "Manage the account's weekly readings (percent or spend figure)",
	}
	cmd.AddCommand(newUsageReadingAddCmd())
	cmd.AddCommand(newUsageReadingListCmd())
	cmd.AddCommand(newUsageReadingDeleteCmd())
	return cmd
}

func newUsageReadingAddCmd() *cobra.Command {
	var percent, spendUSD, sessionPercent float64
	var at string
	var lanes int // deprecated and ignored: the daemon stamps the lanes
	c := &cobra.Command{
		Use:   "add --percent N | --spend-usd N",
		Short: "Append a weekly reading: a percentage, or a spend figure with the session percent",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			pSet := c.Flags().Changed("percent")
			sSet := c.Flags().Changed("spend-usd")
			switch {
			case pSet && sSet:
				return fmt.Errorf("carry either --percent or --spend-usd, not both")
			case !pSet && !sSet:
				return fmt.Errorf("carry --percent N or --spend-usd N (a reading needs a figure)")
			}
			r := usagecalc.Reading{Source: usagecalc.SourceTyped}
			if pSet {
				if err := percentFigure(percent, "percent"); err != nil {
					return err
				}
				p := percent
				r.Percent = &p
			} else {
				if err := readingFigure(spendUSD, "spend"); err != nil {
					return err
				}
				s := spendUSD
				r.SpendUSD = &s
			}
			if c.Flags().Changed("session-percent") {
				if err := percentFigure(sessionPercent, "session percent"); err != nil {
					return err
				}
				sp := sessionPercent
				r.SessionPercent = &sp
			}
			t := time.Now().UTC()
			if s := strings.TrimSpace(at); s != "" {
				v, err := time.Parse(time.RFC3339, s)
				if err != nil {
					return fmt.Errorf("--at must be RFC3339 (for example 2026-10-24T09:30:00Z): %v", err)
				}
				t = v.UTC()
			}
			r.At = t
			body, err := daemonJSONBody(http.MethodPost, "/api/usage/readings", r)
			if err != nil {
				return err
			}
			prettyPrint(body)
			return nil
		},
	}
	f := c.Flags()
	f.Float64Var(&percent, "percent", 0, "the weekly percentage used, 0 or more (up to "+strconv.FormatFloat(usagecalc.MaxPercentReading, 'f', -1, 64)+")")
	f.Float64Var(&spendUSD, "spend-usd", 0, "the spend figure in dollars, 0 or more (exactly one of --percent / --spend-usd)")
	f.StringVar(&at, "at", "", "when the reading was taken, RFC3339 (default: now)")
	f.Float64Var(&sessionPercent, "session-percent", 0, "the in-session percentage, secondary (0 or more)")
	f.IntVar(&lanes, "lanes", 0, "ignored: the daemon stamps each reading with the Claude lanes running in the ledger")
	_ = f.MarkDeprecated("lanes", "the daemon stamps each reading with the Claude lanes running; the value is ignored and not sent")
	return c
}

// readingFigure checks a figure the operator typed for a reading: a finite
// number of 0 or more (the daemon enforces the same bounds).
func readingFigure(v float64, what string) error {
	switch {
	case math.IsNaN(v) || math.IsInf(v, 0):
		return fmt.Errorf("%s must be a number", what)
	case v < 0:
		return fmt.Errorf("%s must be 0 or more", what)
	}
	return nil
}

// percentFigure is readingFigure plus the store's percent ceiling.
func percentFigure(v float64, what string) error {
	if err := readingFigure(v, what); err != nil {
		return err
	}
	if v > usagecalc.MaxPercentReading {
		return fmt.Errorf("%s must be from 0 to %g", what, usagecalc.MaxPercentReading)
	}
	return nil
}

func newUsageReadingListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show the account's readings record, with the keeper and the stale-copy markers",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return daemonJSON(http.MethodGet, "/api/usage/readings", nil)
		},
	}
}

func newUsageReadingDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete one reading by its id (from `usage reading list`)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("a reading id is needed")
			}
			return daemonJSON(http.MethodDelete, "/api/usage/readings?id="+url.QueryEscape(id), nil)
		},
	}
}

// ----- extra credit and remaining work --------------------------------------

func newUsageExtraCreditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "extra-credit <points>",
		Short: "Set the bought extra credit added on top of the 100-point weekly allowance (0 clears it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			points, err := parseUsagePoints(args[0], "extra credit")
			if err != nil {
				return err
			}
			return daemonJSON(http.MethodPut, "/api/usage/extra-credit", map[string]float64{"points": points})
		},
	}
}

func newUsageRemainingWorkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remaining-work <points|clear>",
		Short: "Set the operator's typed remaining-work figure of the run-flat-out policy, or clear it",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if strings.EqualFold(strings.TrimSpace(args[0]), "clear") {
				return daemonJSON(http.MethodDelete, "/api/usage/remaining-work", nil)
			}
			points, err := parseUsagePoints(args[0], "remaining work")
			if err != nil {
				return err
			}
			return daemonJSON(http.MethodPut, "/api/usage/remaining-work", map[string]float64{"points": points})
		},
	}
}

// ----- auto lanes ------------------------------------------------------------

// newUsageAutoLanesCmd is the auto-lanes surface under the usage command:
// status reads the daemon's GET /api/usage/autolanes, and set goes through
// the same config set path as `datawatch config set` (PUT /api/config), so
// the daemon validates both settings exactly the way it would for any key:
//
//	 usage auto-lanes status
//	 usage auto-lanes set --on | --off [--max N]
func newUsageAutoLanesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auto-lanes",
		Short: "Auto-lanes: status, or set the two settings (auto_lanes, auto_lanes_max)",
		Long: "Auto-lanes makes the daemon itself apply the usage calculator's\n" +
			"lane advice to the Claude lanes it runs (the first enabled claude-code\n" +
			"entry's max_inflight), capped at the operator maximum. It leaves the\n" +
			"lanes to the operator while off (the default).\n\n" +
			"Its two settings, settable through the config set path:\n\n" +
			"  usage.calculator.auto_lanes        on/off (default: off)\n" +
			"  usage.calculator.auto_lanes_max    the operator maximum, 1 to 8 (default: 2)\n\n" +
			"\"auto-lanes status\" shows the daemon's view: those settings, the cap the\n" +
			"daemon applies now, what the fresh advice's decision would do to it, and\n" +
			"the loop's recent changes.\n" +
			"\"auto-lanes set\" writes through PUT /api/config, so the daemon validates\n" +
			"and stores each value exactly like `datawatch config set` would.",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show the auto-lanes settings, current cap, decision target and recent changes",
		Long:  "GETs /api/usage/autolanes and prints the body: whether auto-lanes is on (usage.calculator.auto_lanes), the policy and the operator maximum (usage.calculator.auto_lanes_max), the cap the daemon applies now, the advice decision's target and direction when the advice is servable, the last change that moved the cap, and the loop's recent decisions, newest first.",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return daemonJSON(http.MethodGet, "/api/usage/autolanes", nil)
		},
	})

	var on, off bool
	var max int
	set := &cobra.Command{
		Use:   "set",
		Short: "Set auto-lanes on or off (--on / --off) and its maximum (--max N)",
		Long: "Sets the auto-lanes settings through the config set path — the same\n" +
			"PUT /api/config that `datawatch config set <key> <value>` uses — so the\n" +
			"daemon validates and stores each value the way it would for any key:\n\n" +
			"  --on            sets usage.calculator.auto_lanes to true\n" +
			"  --off           sets usage.calculator.auto_lanes to false\n" +
			"  --max N         sets usage.calculator.auto_lanes_max to N (1 to 8)\n\n" +
			"Carry at least one flag; --on and --off are exclusive. A refused value\n" +
			"stores nothing.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if on && off {
				return fmt.Errorf("carry either --on or --off, not both")
			}
			var pairs [][2]string
			if on {
				pairs = append(pairs, [2]string{"usage.calculator.auto_lanes", "true"})
			}
			if off {
				pairs = append(pairs, [2]string{"usage.calculator.auto_lanes", "false"})
			}
			if c.Flags().Changed("max") {
				if max < 1 || max > config.UsageMaxLanesMax {
					return fmt.Errorf("--max must be a whole number from 1 to %d: %d", config.UsageMaxLanesMax, max)
				}
				pairs = append(pairs, [2]string{"usage.calculator.auto_lanes_max", strconv.Itoa(max)})
			}
			if len(pairs) == 0 {
				return fmt.Errorf("carry --on or --off (and --max N)")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			for _, p := range pairs {
				if err := runConfigSet(cfg, p[0], p[1]); err != nil {
					return err
				}
			}
			return nil
		},
	}
	f := set.Flags()
	f.BoolVar(&on, "on", false, "turn auto-lanes on: set usage.calculator.auto_lanes to true")
	f.BoolVar(&off, "off", false, "turn auto-lanes off: set usage.calculator.auto_lanes to false")
	f.IntVar(&max, "max", 0, "the operator maximum the daemon never exceeds, 1 to 8: sets usage.calculator.auto_lanes_max")
	cmd.AddCommand(set)
	return cmd
}

// parseUsagePoints parses a points figure a CLI operator would type: a whole
// or decimal number of 0 or more points, no units.
func parseUsagePoints(s, what string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("%s must be a number of 0 or more points: %q", what, s)
	}
	return v, nil
}
