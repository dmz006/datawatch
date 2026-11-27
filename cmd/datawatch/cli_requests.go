// `datawatch request`: requests, the saved things that must happen. Same names
// and meaning as the REST, assistant tool and chat surfaces:
//
//	datawatch request list [--state --kind --role --scope-kind --scope --holder]
//	datawatch request <id>                     the same as "show <id>"
//	datawatch request mine | roles | settings
//	datawatch request role give --session <id> --scope <project> [--force --reason <code>]
//	datawatch request create --scope <project> --ask <words>
//	datawatch request take|accept|report|decline <id>      the holder's calls
//	datawatch request reissue|drop|force|accept-unproven <id> --reason <code>
//	datawatch request from-backlog <entry key> --scope <project>
//
// The command line sends DATAWATCH_TOKEN when it is set, else the daemon's own
// token. Only a session's own credential may take, accept, report or decline.
// There is no "seen": only a worker's own program reports that. A command aimed
// at another datawatch computer (--server) is refused before anything is sent.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/federation"
)

// requestCLIDo calls the daemon on this computer.
func requestCLIDo(method, path string, body any) ([]byte, error) {
	if serverName != "" && serverName != "local" {
		return nil, delivery.ErrCrossHost
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, daemonURL()+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(federation.HeaderRequestSurface, string(delivery.SurfaceCLI))
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon not reachable: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// requestCLIText turns an answer into the text shown without --json.
func requestCLIText[T any](render func(T) string) func([]byte) (string, error) {
	return func(b []byte) (string, error) {
		var v T
		if err := json.Unmarshal(b, &v); err != nil {
			return "", err
		}
		return render(v), nil
	}
}

func requestCLIChanged(v delivery.RequestView) string {
	return v.ID + ": " + delivery.RenderLabel(v)
}

type requestCLICreated struct {
	Request delivery.RequestView `json:"request"`
	Created bool                 `json:"created"`
}

func requestCLIMade(a requestCLICreated) string {
	if a.Created {
		return "created " + requestCLIChanged(a.Request)
	}
	return "already there " + requestCLIChanged(a.Request)
}

// requestCLIRun sends one call and prints its answer.
func requestCLIRun(cmd *cobra.Command, method, path string, body any, text func([]byte) (string, error)) error {
	answer, err := requestCLIDo(method, path, body)
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	return deliveryPrint(cmd.OutOrStdout(), asJSON, answer, text)
}

// requestCLIShow reads one request.
func requestCLIShow(cmd *cobra.Command, id string) error {
	return requestCLIRun(cmd, http.MethodGet, delivery.RouteRequests+"?id="+url.QueryEscape(id), nil, requestCLIText(delivery.RenderRequest))
}

// requestCLIFlags are string flags kept under the shared field names.
type requestCLIFlags map[string]*string

func (f requestCLIFlags) add(cmd *cobra.Command, flag, field, help string) {
	f[field] = cmd.Flags().String(flag, "", help)
}

func (f requestCLIFlags) values() map[string]string {
	out := map[string]string{}
	for name, v := range f {
		if s := strings.TrimSpace(*v); s != "" {
			out[name] = s
		}
	}
	return out
}

func (f requestCLIFlags) addScope(cmd *cobra.Command) {
	f.add(cmd, "scope", "scope_id", "Project, task, Automaton, Automation, computer or session the role is for (required)")
	f.add(cmd, "scope-kind", "scope_kind", "project (default), task, automaton, automation, host or session")
	f.add(cmd, "role", "role", "Role (default supervisor)")
}

// addContent adds what a creator may write; the returned func adds --param.
func (f requestCLIFlags) addContent(cmd *cobra.Command) func(map[string]string) error {
	f.add(cmd, "ask", "ask", "The ask, in words")
	f.add(cmd, "detail", "detail", "More of the ask")
	f.add(cmd, "items", "items", "JSON array of done-when items: {id, statement, from, method (receipt, state:file, confirm, review), path, contains, effect, change, before, target}. No commands are run")
	f.add(cmd, "project-dir", "project_dir", "Project folder the work is in")
	params := cmd.Flags().StringArray("param", nil, "name=value (repeatable)")
	return func(fields map[string]string) error {
		if len(*params) == 0 {
			return nil
		}
		m := map[string]string{}
		for _, p := range *params {
			name, value, ok := strings.Cut(p, "=")
			if !ok || name == "" {
				return errors.New("--param is name=value")
			}
			m[name] = value
		}
		b, err := json.Marshal(m)
		fields["params"] = string(b)
		return err
	}
}

func requestCLICreateCmd() *cobra.Command {
	f := requestCLIFlags{}
	var content func(map[string]string) error
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Save a request addressed to a role on this computer",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fields := f.values()
			if err := content(fields); err != nil {
				return err
			}
			in, err := delivery.SurfaceCreate(fields)
			if err != nil {
				return err
			}
			return requestCLIRun(cmd, http.MethodPost, delivery.RouteRequests, in, requestCLIText(requestCLIMade))
		},
	}
	f.addScope(cmd)
	content = f.addContent(cmd)
	f.add(cmd, "kind", "kind", "Request kind (default ask)")
	f.add(cmd, "key", "key", "Your own id for this occurrence, never the content; the same key returns the request already made")
	f.add(cmd, "after", "after", "Request that must be done first")
	f.add(cmd, "take-by", "take_by", "Time to be taken, such as 30m (default: the setting)")
	f.add(cmd, "due", "due", "Time to be finished, such as 24h (default: the setting)")
	f.add(cmd, "source-type", "source_type", "Kind of record it came from")
	f.add(cmd, "source-id", "source_id", "Id of that record")
	f.add(cmd, "host", "host", "Must be this computer or empty")
	return cmd
}

func requestCLIBacklogCmd() *cobra.Command {
	f := requestCLIFlags{}
	var content func(map[string]string) error
	cmd := &cobra.Command{
		Use:   "from-backlog <entry key>",
		Short: "Turn one backlog entry of the delivery list into a request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fields := f.values()
			if err := content(fields); err != nil {
				return err
			}
			fields["entry_key"] = args[0]
			in, err := delivery.SurfaceBacklog(fields)
			if err != nil {
				return err
			}
			return requestCLIRun(cmd, http.MethodPost, delivery.RouteRequestFromBacklog, in, requestCLIText(requestCLIMade))
		},
	}
	f.addScope(cmd)
	content = f.addContent(cmd)
	return cmd
}

func requestCLIRoleCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "role",
		Short: "Give a role to a session or release it",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	build := func(use, short, path string, give bool) *cobra.Command {
		f := requestCLIFlags{}
		var force bool
		cmd := &cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				fields := f.values()
				role, scope, err := delivery.SurfaceScope(fields)
				if err != nil {
					return errors.New("--scope is required")
				}
				if give && fields["session_id"] == "" {
					return errors.New("--session is required")
				}
				in := delivery.RoleInput{Role: role, Scope: scope, SessionID: fields["session_id"], Force: force, ReasonCode: fields["reason_code"]}
				text := func([]byte) (string, error) { return "released", nil }
				if give {
					text = requestCLIText(func(b delivery.RoleBinding) string { return delivery.RenderRoles([]delivery.RoleBinding{b}) })
				}
				return requestCLIRun(cmd, http.MethodPost, path, in, text)
			},
		}
		f.addScope(cmd)
		f.add(cmd, "session", "session_id", "Full session id")
		if give {
			cmd.Flags().BoolVar(&force, "force", false, "Take the role from a live holder; needs --reason")
			f.add(cmd, "reason", "reason_code", "Reason code, needed with --force")
		}
		return cmd
	}
	root.AddCommand(
		build("give", "Give a role to a session", delivery.RouteRequestRoleGive, true),
		build("release", "Release a role", delivery.RouteRequestRoleRelease, false))
	return root
}

// requestCLIIDCmd is a command on one request: POST /api/requests/{id}/{verb}.
// extra declares its flags and returns what fills the body.
func requestCLIIDCmd(verb, short string, text func([]byte) (string, error), extra func(*cobra.Command) func(map[string]any) error) *cobra.Command {
	var fill func(map[string]any) error
	cmd := &cobra.Command{
		Use:   verb + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			if fill != nil {
				if err := fill(body); err != nil {
					return err
				}
			}
			return requestCLIRun(cmd, http.MethodPost, delivery.RouteRequests+"/"+url.PathEscape(args[0])+"/"+verb, body, text)
		},
	}
	if extra != nil {
		fill = extra(cmd)
	}
	return cmd
}

// requestCLIBody declares optional and required text flags of a request call.
// Each is {flag, field, help}.
func requestCLIBody(required, optional [][3]string) func(*cobra.Command) func(map[string]any) error {
	return func(cmd *cobra.Command) func(map[string]any) error {
		f := requestCLIFlags{}
		for _, o := range append(append([][3]string{}, required...), optional...) {
			f.add(cmd, o[0], o[1], o[2])
		}
		return func(body map[string]any) error {
			values := f.values()
			for _, r := range required {
				if values[r[1]] == "" {
					return fmt.Errorf("--%s is required", r[0])
				}
			}
			for name, v := range values {
				body[name] = v
			}
			return nil
		}
	}
}

// requestSettingKeys are the request settings, in the order they are shown.
var requestSettingKeys = []string{"request_take_by_seconds", "request_due_seconds", "request_due_seconds_by_kind",
	"reminder_interval_seconds", "reminders_per_session_hour", "reminders_per_host_hour"}

func requestCLISettingsText(b []byte) (string, error) {
	var view struct {
		Requests map[string]any `json:"delivery_requests"`
	}
	if err := json.Unmarshal(b, &view); err != nil {
		return "", err
	}
	if view.Requests == nil {
		return "", errors.New("the daemon reported no request settings")
	}
	lines := make([]string, 0, len(requestSettingKeys))
	for _, key := range requestSettingKeys {
		value := view.Requests[key]
		if byKind, ok := value.(map[string]any); ok {
			pairs := make([]string, 0, len(byKind))
			for kind, v := range byKind {
				pairs = append(pairs, fmt.Sprintf("%s=%v", kind, v))
			}
			sort.Strings(pairs)
			value = strings.Join(pairs, ",")
		}
		lines = append(lines, fmt.Sprintf("delivery.%s = %v", key, value))
	}
	return strings.Join(lines, "\n"), nil
}

// newRequestCmd is "datawatch request".
func newRequestCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "request [id]",
		Short: "Requests: saved work addressed to a role, that is not lost between sessions",
		Long: `A request is one saved thing that must happen, addressed to a role and not to a
session. Listings hold identifiers, states, reason codes and a plain reading,
never a request's words; only a request datawatch's own check proved reads
"done". Requests are for this computer only. Who may create or manage requests
is recorded and not enforced in this version.`,
		Example: `  datawatch request role give --scope web --session host-1a2b
  datawatch request create --scope web --ask "Fix the login page"
  datawatch request list --state waiting
  datawatch request r-1a2b3c4d`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if !delivery.IsRequestID(args[0]) {
				return fmt.Errorf("unknown request command %q", args[0])
			}
			return requestCLIShow(cmd, args[0])
		},
	}
	root.PersistentFlags().Bool("json", false, "Print the daemon's answer as JSON")

	filter := requestCLIFlags{}
	list := &cobra.Command{
		Use:   "list",
		Short: "List requests",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			for name, v := range filter.values() {
				q.Set(name, v)
			}
			path := delivery.RouteRequests
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			return requestCLIRun(cmd, http.MethodGet, path, nil, requestCLIText(func(a struct {
				Requests []delivery.RequestView `json:"requests"`
			}) string {
				return delivery.RenderRequests(a.Requests, 0)
			}))
		},
	}
	for _, o := range [][3]string{{"state", "state", "Only this state"}, {"kind", "kind", "Only this kind"}, {"role", "role", "Only this role"},
		{"scope-kind", "scope_kind", "Only this kind of scope"}, {"scope", "scope_id", "Only this scope"}, {"holder", "holder", "Only those this session holds"}} {
		filter.add(list, o[0], o[1], o[2])
	}

	get := func(use, short, path string, text func([]byte) (string, error)) *cobra.Command {
		return &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error { return requestCLIRun(cmd, http.MethodGet, path, nil, text) }}
	}

	changed := requestCLIText(requestCLIChanged)
	reason := [3]string{"reason", "reason_code", "Reason code (required): a-z, 0-9, dot, dash and underscore"}
	words := [3]string{"words", "words", "The words that were said"}

	reissue := requestCLIIDCmd(delivery.VerbReissue, "Make a new request from a failed, expired or cancelled one", changed,
		func(cmd *cobra.Command) func(map[string]any) error {
			fill := requestCLIBody([][3]string{reason}, [][3]string{words})(cmd)
			repeat := cmd.Flags().Bool("repeat-ok", false, "Reissue although the work may already have been done")
			return func(body map[string]any) error {
				if *repeat {
					body["repeat_ok"] = true
				}
				return fill(body)
			}
		})

	take := requestCLIIDCmd(delivery.VerbTake, "Fetch a request's content (the holder's own session credential only)",
		requestCLIText(delivery.RenderTake), func(*cobra.Command) func(map[string]any) error {
			return func(body map[string]any) error { body["via"] = delivery.ViaTool; return nil }
		})

	root.AddCommand(
		list,
		&cobra.Command{Use: "show <id>", Short: "Show one request, without its content", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error { return requestCLIShow(cmd, args[0]) }},
		get("mine", "List what this session holds (a session credential only)", delivery.RouteRequestsMine,
			requestCLIText(func(m delivery.MineReply) string { return delivery.RenderMine(m, 0) })),
		get("roles", "List who holds which role", delivery.RouteRequestRoles, requestCLIText(func(a struct {
			Roles []delivery.RoleBinding `json:"roles"`
		}) string {
			return delivery.RenderRoles(a.Roles)
		})),
		requestCLIRoleCmd(),
		requestCLICreateCmd(),
		requestCLIBacklogCmd(),
		take,
		requestCLIIDCmd(delivery.VerbAccept, "Say that this session will do a request it holds", changed,
			requestCLIBody(nil, [][3]string{{"code", "code", "Receipt code written inside the request's content"}})),
		requestCLIIDCmd(delivery.VerbReport, "Say that this session believes a request it holds is finished; it does not make it done", changed,
			requestCLIBody(nil, [][3]string{{"text", "text", "What was done"}, {"result-id", "result_id", "Result-store id"}, {"rules-version", "rules_version", "Rules version followed"}})),
		requestCLIIDCmd(delivery.VerbDecline, "Say that this session will not do a request it holds", changed,
			requestCLIBody([][3]string{reason}, [][3]string{{"text", "text", "Why"}})),
		reissue,
		requestCLIIDCmd(delivery.VerbDrop, "Cancel or close a request", changed, requestCLIBody([][3]string{reason}, [][3]string{words})),
		requestCLIIDCmd(delivery.VerbForce, "Move a request off its holder, even with accepted work; it may run twice",
			requestCLIText(func(v delivery.RequestView) string { return requestCLIChanged(v) + " (moved; it may run twice)" }),
			requestCLIBody([][3]string{reason}, [][3]string{words, {"to-session", "to_session", "Move to this session; empty returns it to the role"}})),
		requestCLIIDCmd(delivery.VerbAcceptUnproven, "Record a request as accepted without proof; a doer's is only a claim", changed,
			requestCLIBody([][3]string{reason, {"words", "words", "What was accepted without proof and why (required)"}}, nil)),
		get("settings", "Show the request settings in force (change one with: datawatch config set delivery.<key> <value>)", "/api/config", requestCLISettingsText),
	)
	return root
}
