// plan_records_comm.go — the plan record verbs over the chat channel, so the
// operator can read, create and change registry rows and findings from
// Signal / Telegram without curl. The verbs proxy the REST surface
// (internal/server/plan_records.go) through the loopback helpers:
//
//	autonomous plan-records [registry-row|finding] [project=P] [state=S]
//	autonomous plan-record list [registry-row|finding] [project=P] [state=S]
//		ten newest records, one per line: the title first, then the id,
//		the project, the state and the old row number. A read.
//	autonomous plan-record get <ref>
//		the full record. <ref> is an id, "row <n>", a title, an older title
//		or a title prefix; an ambiguous one replies with the candidates.
//	autonomous plan-record create <json>
//		create a record; the JSON carries kind (registry-row or finding),
//		project, title and the other content fields of the REST create.
//	autonomous plan-record update <ref> <json>
//		change a record; the JSON carries only the fields to change.
//	autonomous plan-record search <text>
//		search both kinds; ten best hits. A read.
//	autonomous plan-record import [dry-run] [project=P] [source=S] [policy=P]
//		import the registry file and findings files as records. With
//		dry-run it only previews every record and conflict; without, it
//		writes (records already imported are left alone).
//	autonomous plan list <project>
//		the project's plans in order, with progress and what runs next. A read.
//	autonomous plan new <project> <title…>
//		create a plan (it goes last in the plan order).
//	autonomous plan move <project> <plan> <position>
//		move a plan to a position in the plan order.
//	autonomous plan move <plan> automaton <id> <position>
//		move an Automaton to a position in the plan's order; the reply says what
//		runs next (blocked and held Automata are skipped).
//	autonomous plan attach <plan> automaton|automation|finding <id>
//	autonomous plan detach <plan> automaton|automation|finding <id>
//		link or unlink one; a link held elsewhere is refused and named.
//	"arcs" is accepted in place of "plan" (autonomous arcs list <project> …).
//	Each verb calls the loopback REST route, so chat runs the same code as REST.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const (
	// planRecordsUsage is the usage line for the plan record comm verbs; a bad
	// verb or a missing argument replies with it.
	planRecordsUsage = "usage: autonomous plan-records [registry-row|finding] [project=P] [state=S] | plan-record get <ref> | plan-record create <json> | plan-record update <ref> <json> | plan-record search <text> | plan-record import [dry-run] [project=P] [source=registry|findings|all] [policy=suffix|rename|skip]"

	// planVerbUsage is the usage line for the plan verbs.
	planVerbUsage = "usage: autonomous plan list <project> | plan new <project> <title...> | plan move <project> <plan> <position> | plan move <plan> automaton <id> <position> | plan attach <plan> automaton|automation|finding <id> | plan detach <plan> automaton|automation|finding <id>  (arcs works in place of plan)"

	// planRecordsMaxLines is the most lines one list or search reply carries.
	planRecordsMaxLines = 10

	// planRecordsTitleCut is the rune limit for a title on a list line.
	planRecordsTitleCut = 80
)

// planRecordLineWire is one record of a list or search reply.
type planRecordLineWire struct {
	Title        string `json:"title"`
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Project      string `json:"project"`
	State        string `json:"state"`
	LegacyNumber int    `json:"legacy_number"`
	NeedsReview  bool   `json:"needs_review"`
}

// PlanRecordsCommVerbs lists the plan record comm verbs for the parity test.
// Keep it in step with the verbs handleAutonomous routes to this handler.
func PlanRecordsCommVerbs() []string {
	return []string{"plan-records", "plan-record", "plan", "arcs"}
}

// planRecordCommSegment maps a kind word to its REST collection segment.
func planRecordCommSegment(word string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "registry-row", "registry_row", "registry-rows", "row", "rows":
		return "registry-rows", true
	case "finding", "findings":
		return "findings", true
	}
	return "", false
}

// handlePlanRecordsVerb answers the autonomous command's plan record verbs.
func (r *Router) handlePlanRecordsVerb(verb string, args []string) {
	rest := []string{}
	if len(args) > 1 {
		rest = args[1:]
	}
	sub := ""
	if len(rest) > 0 {
		sub = strings.ToLower(rest[0])
	}
	switch sub {
	case "list", "ls":
		r.planRecordsList(rest[1:])
	case "get", "show":
		r.planRecordGet(rest[1:])
	case "create":
		r.planRecordCreate(rest[1:])
	case "update":
		r.planRecordUpdate(rest[1:])
	case "search":
		r.planRecordSearch(rest[1:])
	case "import":
		r.planRecordImport(rest[1:])
	default:
		// "plan-records" alone, or with only filters, is the list.
		if strings.EqualFold(strings.ReplaceAll(verb, "_", "-"), "plan-records") {
			r.planRecordsList(rest)
			return
		}
		r.reply("autonomous plan-record failed", planRecordsUsage)
	}
}

// planRecordsFilterArgs reads the optional kind word and the project= and
// state= filters of a list.
func planRecordsFilterArgs(args []string) (segs []string, q url.Values, ok bool) {
	q = url.Values{"limit": {strconv.Itoa(planRecordsMaxLines)}}
	segs = []string{"registry-rows", "findings"}
	for _, a := range args {
		if k, v, isKV := strings.Cut(a, "="); isKV {
			switch strings.ToLower(k) {
			case "project", "state":
				if strings.TrimSpace(v) == "" {
					return nil, nil, false
				}
				q.Set(strings.ToLower(k), strings.TrimSpace(v))
				continue
			}
			return nil, nil, false
		}
		seg, isKind := planRecordCommSegment(a)
		if !isKind {
			return nil, nil, false
		}
		segs = []string{seg}
	}
	return segs, q, true
}

// planRecordLines renders at most ten records, one per line, title first.
func planRecordLines(rows []planRecordLineWire, suffix func(i int) string) string {
	var b strings.Builder
	for i, rec := range rows {
		if i >= planRecordsMaxLines {
			break
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(cutToRunes(strings.Join(strings.Fields(rec.Title), " "), planRecordsTitleCut))
		b.WriteString(" [" + rec.ID + "]")
		if rec.Project != "" {
			b.WriteString(" " + rec.Project)
		}
		if rec.State != "" {
			b.WriteString(" " + rec.State)
		}
		if rec.LegacyNumber > 0 {
			b.WriteString(" row " + strconv.Itoa(rec.LegacyNumber))
		}
		if rec.NeedsReview {
			b.WriteString(" needs-review")
		}
		if suffix != nil {
			b.WriteString(suffix(i))
		}
	}
	return b.String()
}

// planRecordsList lists the newest records of one kind, or of both.
func (r *Router) planRecordsList(args []string) {
	segs, q, ok := planRecordsFilterArgs(args)
	if !ok {
		r.reply("autonomous plan-records failed", planRecordsUsage)
		return
	}
	var parts []string
	for _, seg := range segs {
		out, err := r.commGet("/api/plan-records/"+seg, q)
		if err != nil {
			// A state that fits only one kind is refused by the other one;
			// when both were asked that is not a failure.
			if len(segs) > 1 && q.Get("state") != "" && strings.HasPrefix(err.Error(), "HTTP 400") {
				continue
			}
			r.reply("autonomous plan-records failed", err.Error())
			return
		}
		var list struct {
			Records []planRecordLineWire `json:"records"`
		}
		if err := json.Unmarshal([]byte(out), &list); err != nil {
			r.reply("autonomous plan-records failed", err.Error())
			return
		}
		text := "No plan records to show."
		if len(list.Records) > 0 {
			text = planRecordLines(list.Records, nil)
		}
		if len(segs) > 1 {
			text = "== " + seg + " ==\n" + text
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		parts = append(parts, "No plan records to show.")
	}
	r.reply("autonomous plan-records", strings.Join(parts, "\n"))
}

// planRecordCommCandidates renders the candidates of a 409 reply, or reports
// false for any other failure.
func planRecordCommCandidates(err error, ref string) (string, bool) {
	if err == nil || !strings.HasPrefix(err.Error(), "HTTP 409: ") {
		return "", false
	}
	var wire struct {
		Error      string               `json:"error"`
		Candidates []planRecordLineWire `json:"candidates"`
	}
	if json.Unmarshal([]byte(strings.TrimPrefix(err.Error(), "HTTP 409: ")), &wire) != nil || len(wire.Candidates) == 0 {
		return "", false
	}
	msg := wire.Error
	if msg == "" {
		msg = strconv.Quote(ref) + " matches more than one record"
	}
	return msg + "\n" + planRecordLines(wire.Candidates, func(i int) string {
		if wire.Candidates[i].Kind != "" {
			return " (" + wire.Candidates[i].Kind + ")"
		}
		return ""
	}) + "\nSay which one: send its id instead of the title.", true
}

// planRecordFind resolves a reference. With a kind it asks that collection;
// without, it asks both and refuses a reference that resolves in both. It
// returns the REST segment and the record body, or the text of the failure.
func (r *Router) planRecordFind(ref, project, kind string) (seg, body, failure string) {
	q := url.Values{}
	if project != "" {
		q.Set("project", project)
	}
	segs := []string{"registry-rows", "findings"}
	if kind != "" {
		s, ok := planRecordCommSegment(kind)
		if !ok {
			return "", "", "kind must be registry-row or finding"
		}
		segs = []string{s}
	}
	type hit struct{ seg, body string }
	var hits []hit
	for _, s := range segs {
		out, err := r.commGet("/api/plan-records/"+s+"/"+url.PathEscape(ref), q)
		if err != nil {
			if text, ok := planRecordCommCandidates(err, ref); ok {
				return "", "", text
			}
			if strings.HasPrefix(err.Error(), "HTTP 404") {
				continue
			}
			return "", "", err.Error()
		}
		hits = append(hits, hit{s, out})
	}
	switch len(hits) {
	case 0:
		return "", "", "no plan record matches " + strconv.Quote(ref)
	case 1:
		return hits[0].seg, hits[0].body, ""
	}
	return "", "", strconv.Quote(ref) + " matches a registry row and a finding; say which with the id"
}

// planRecordGet replies with the full record, pretty-printed.
func (r *Router) planRecordGet(args []string) {
	ref := strings.TrimSpace(strings.Join(args, " "))
	if ref == "" {
		r.reply("autonomous plan-record failed", planRecordsUsage)
		return
	}
	_, body, failure := r.planRecordFind(ref, "", "")
	if failure != "" {
		r.reply("autonomous plan-record failed", failure)
		return
	}
	r.reply("autonomous plan-record", planRecordShowText(body))
}

// planRecordShowText puts the title on the first line and the id on the
// second, then the rest of the record as indented JSON.
func planRecordShowText(body string) string {
	var env struct {
		Record struct {
			Title string `json:"title"`
			ID    string `json:"id"`
		} `json:"record"`
	}
	if json.Unmarshal([]byte(body), &env) != nil || env.Record.ID == "" {
		return prettyJSON(body)
	}
	return env.Record.Title + "\nid " + env.Record.ID + "\n" + prettyJSON(body)
}

// planRecordJSONArg splits "<words…> {json}" into the words and the parsed
// object. ok is false when there is no valid JSON object.
func planRecordJSONArg(args []string) (words string, obj map[string]any, ok bool) {
	for i, a := range args {
		if strings.HasPrefix(a, "{") {
			if json.Unmarshal([]byte(strings.Join(args[i:], " ")), &obj) != nil || obj == nil {
				return "", nil, false
			}
			return strings.TrimSpace(strings.Join(args[:i], " ")), obj, true
		}
	}
	return strings.TrimSpace(strings.Join(args, " ")), nil, false
}

// planRecordTakeString removes a string field from obj and returns it.
func planRecordTakeString(obj map[string]any, key string) string {
	v, _ := obj[key].(string)
	delete(obj, key)
	return strings.TrimSpace(v)
}

// planRecordCreate sends the JSON body to the create route of its kind.
func (r *Router) planRecordCreate(args []string) {
	_, obj, ok := planRecordJSONArg(args)
	if !ok {
		r.reply("autonomous plan-record create failed", "create takes one JSON object with kind, project and title: "+planRecordsUsage)
		return
	}
	kind := planRecordTakeString(obj, "kind")
	seg, isKind := planRecordCommSegment(kind)
	if !isKind {
		r.reply("autonomous plan-record create failed", "the JSON needs kind: registry-row or finding")
		return
	}
	raw, _ := json.Marshal(obj)
	out, err := r.commJSON(http.MethodPost, "/api/plan-records/"+seg, string(raw))
	if err != nil {
		r.reply("autonomous plan-record create failed", err.Error())
		return
	}
	r.reply("autonomous plan-record create", planRecordSavedText(out))
}

// planRecordSavedText is the one-line confirmation of a saved record.
func planRecordSavedText(body string) string {
	var env struct {
		Record planRecordLineWire `json:"record"`
	}
	if json.Unmarshal([]byte(body), &env) != nil || env.Record.ID == "" {
		return prettyJSON(body)
	}
	return planRecordLines([]planRecordLineWire{env.Record}, nil)
}

// planRecordUpdate resolves the reference, then sends the JSON as the change.
func (r *Router) planRecordUpdate(args []string) {
	ref, obj, ok := planRecordJSONArg(args)
	if !ok || ref == "" {
		r.reply("autonomous plan-record update failed", "update takes a reference and one JSON object: "+planRecordsUsage)
		return
	}
	kind := planRecordTakeString(obj, "kind")
	project := planRecordTakeString(obj, "project")
	if len(obj) == 0 {
		r.reply("autonomous plan-record update failed", "nothing to change: the JSON has no fields")
		return
	}
	seg, _, failure := r.planRecordFind(ref, project, kind)
	if failure != "" {
		r.reply("autonomous plan-record update failed", failure)
		return
	}
	path := "/api/plan-records/" + seg + "/" + url.PathEscape(ref)
	if project != "" {
		path += "?" + url.Values{"project": {project}}.Encode()
	}
	raw, _ := json.Marshal(obj)
	out, err := r.commJSON(http.MethodPut, path, string(raw))
	if err != nil {
		r.reply("autonomous plan-record update failed", err.Error())
		return
	}
	r.reply("autonomous plan-record update", planRecordSavedText(out))
}

// planRecordSearch replies with the ten best hits.
func (r *Router) planRecordSearch(args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		r.reply("autonomous plan-record search failed", planRecordsUsage)
		return
	}
	q := url.Values{"q": {text}, "limit": {strconv.Itoa(planRecordsMaxLines)}}
	out, err := r.commGet("/api/plan-records/search", q)
	if err != nil {
		r.reply("autonomous plan-record search failed", err.Error())
		return
	}
	var res struct {
		Hits []struct {
			Record  planRecordLineWire `json:"record"`
			Matched string             `json:"matched"`
		} `json:"hits"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		r.reply("autonomous plan-record search failed", err.Error())
		return
	}
	if len(res.Hits) == 0 {
		r.reply("autonomous plan-record search", "No plan records match.")
		return
	}
	rows := make([]planRecordLineWire, 0, len(res.Hits))
	for _, h := range res.Hits {
		rows = append(rows, h.Record)
	}
	r.reply("autonomous plan-record search", planRecordLines(rows, func(i int) string {
		if res.Hits[i].Matched != "" {
			return " (matched " + res.Hits[i].Matched + ")"
		}
		return ""
	}))
}

// planRecordImport previews (dry-run) or runs the import.
func (r *Router) planRecordImport(args []string) {
	dry := false
	body := map[string]any{}
	for _, a := range args {
		if strings.EqualFold(a, "dry-run") || strings.EqualFold(a, "dry_run") {
			dry = true
			continue
		}
		k, v, isKV := strings.Cut(a, "=")
		v = strings.ToLower(strings.TrimSpace(v))
		if !isKV || v == "" {
			r.reply("autonomous plan-record import failed", planRecordsUsage)
			return
		}
		switch strings.ToLower(k) {
		case "project":
			body["project"] = strings.TrimSpace(strings.SplitN(a, "=", 2)[1])
		case "source":
			if v != "registry" && v != "findings" && v != "all" {
				r.reply("autonomous plan-record import failed", "source must be registry, findings or all")
				return
			}
			body["source"] = v
		case "policy", "conflict_policy":
			if v != "suffix" && v != "rename" && v != "skip" {
				r.reply("autonomous plan-record import failed", "policy must be suffix, rename or skip")
				return
			}
			body["conflict_policy"] = v
		default:
			r.reply("autonomous plan-record import failed", planRecordsUsage)
			return
		}
	}
	raw, _ := json.Marshal(body)
	path := "/api/plan-records/import?" + url.Values{"dry_run": {strconv.FormatBool(dry)}}.Encode()
	out, err := r.commJSON(http.MethodPost, path, string(raw))
	if err != nil {
		r.reply("autonomous plan-record import failed", err.Error())
		return
	}
	r.reply("autonomous plan-record import", planRecordImportText(out))
}

// planRecordImportText summarises an import result: the counts, then each
// conflict and what was (or would be) done about it.
func planRecordImportText(body string) string {
	var res struct {
		Note   string `json:"note"`
		Result struct {
			Project string `json:"project"`
			DryRun  bool   `json:"dry_run"`
			Policy  string `json:"policy"`
			Counts  struct {
				Create, Unchanged, Changed, Conflicts, Skipped int
			} `json:"counts"`
			Conflicts []struct {
				Title    string `json:"title"`
				Reason   string `json:"reason"`
				Action   string `json:"action"`
				NewTitle string `json:"new_title"`
			} `json:"conflicts"`
			Unparseable []struct {
				Source string `json:"source"`
				Reason string `json:"reason"`
			} `json:"unparseable"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(body), &res) != nil {
		return prettyJSON(body)
	}
	c := res.Result.Counts
	mode := "Import"
	if res.Result.DryRun {
		mode = "Preview"
	}
	var b strings.Builder
	b.WriteString(mode + " for " + res.Result.Project + " (policy " + res.Result.Policy + "): ")
	b.WriteString("create " + strconv.Itoa(c.Create) + ", unchanged " + strconv.Itoa(c.Unchanged) +
		", changed " + strconv.Itoa(c.Changed) + ", conflicts " + strconv.Itoa(c.Conflicts) + ", skipped " + strconv.Itoa(c.Skipped))
	for i, cf := range res.Result.Conflicts {
		if i >= planRecordsMaxLines {
			b.WriteString("\n… and " + strconv.Itoa(len(res.Result.Conflicts)-i) + " more conflicts")
			break
		}
		b.WriteString("\nconflict (" + cf.Reason + "): " + cutToRunes(cf.Title, planRecordsTitleCut) + " — " + cf.Action)
		if cf.NewTitle != "" {
			b.WriteString(" → " + cutToRunes(cf.NewTitle, planRecordsTitleCut))
		}
	}
	for _, u := range res.Result.Unparseable {
		b.WriteString("\nnot imported: " + u.Source + " — " + u.Reason)
	}
	if res.Result.DryRun {
		b.WriteString("\nNothing was written. Send the same command without dry-run to write.")
	}
	return b.String()
}

// ----- plan verbs: list, new, move, attach, detach --------------------------

// planWireRow is one plan of the plans list answer.
type planWireRow struct {
	Title    string `json:"title"`
	ID       string `json:"id"`
	State    string `json:"state"`
	Order    int    `json:"order"`
	Progress struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	} `json:"progress"`
	Next *struct {
		AutomatonID string `json:"automaton_id"`
		Title       string `json:"title"`
	} `json:"next"`
}

type planWireList struct {
	Plans   []planWireRow `json:"plans"`
	Backlog struct {
		Plans []planWireRow `json:"plans"`
	} `json:"backlog"`
}

type planWireRun struct {
	Plan struct {
		Title string `json:"title"`
		ID    string `json:"id"`
	} `json:"plan"`
	Next *struct {
		AutomatonID string `json:"automaton_id"`
		Title       string `json:"title"`
	} `json:"next"`
	Automata []struct {
		Title    string `json:"title"`
		ID       string `json:"id"`
		Run      string `json:"run"`
		Position int    `json:"position"`
	} `json:"automata"`
}

// handlePlanVerb answers "autonomous plan …" and its alias "autonomous arcs …".
func (r *Router) handlePlanVerb(args []string) {
	rest := []string{}
	if len(args) > 1 {
		rest = args[1:]
	}
	if len(rest) == 0 {
		r.reply("autonomous plan failed", planVerbUsage)
		return
	}
	sub, rest := strings.ToLower(rest[0]), rest[1:]
	switch sub {
	case "list", "ls":
		r.planList(rest)
	case "new", "create":
		r.planNew(rest)
	case "move":
		r.planMove(rest)
	case "attach", "detach":
		r.planLink(sub, rest)
	default:
		r.reply("autonomous plan failed", planVerbUsage)
	}
}

func (r *Router) planFetchList(project string) (planWireList, error) {
	var res planWireList
	out, err := r.commGet("/api/projects/"+url.PathEscape(project)+"/plans",
		url.Values{"limit": {"500"}, "backlog_limit": {"500"}})
	if err != nil {
		return res, err
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return res, fmt.Errorf("the daemon sent a plans list I cannot read")
	}
	return res, nil
}

func (r *Router) planList(args []string) {
	if len(args) != 1 {
		r.reply("autonomous plan failed", planVerbUsage)
		return
	}
	res, err := r.planFetchList(args[0])
	if err != nil {
		r.reply("autonomous plan list failed", err.Error())
		return
	}
	var lines []string
	add := func(prefix string, rows []planWireRow) {
		for i, p := range rows {
			if len(lines) >= planRecordsMaxLines {
				return
			}
			l := fmt.Sprintf("%s%d. %s [%s] %s %d/%d done", prefix, i+1, strings.Join(strings.Fields(p.Title), " "), p.ID, p.State, p.Progress.Done, p.Progress.Total)
			if p.Next != nil {
				l += " next: " + strings.Join(strings.Fields(p.Next.Title), " ")
			}
			lines = append(lines, l)
		}
	}
	add("", res.Plans)
	add("backlog ", res.Backlog.Plans)
	if len(lines) == 0 {
		r.reply("autonomous plan list", "No plans yet.")
		return
	}
	if total := len(res.Plans) + len(res.Backlog.Plans); total > len(lines) {
		lines = append(lines, fmt.Sprintf("… and %d more (use plan list on REST or the CLI for the rest)", total-len(lines)))
	}
	r.reply("autonomous plan list", strings.Join(lines, "\n"))
}

func (r *Router) planNew(args []string) {
	if len(args) < 2 {
		r.reply("autonomous plan failed", planVerbUsage)
		return
	}
	raw, _ := json.Marshal(map[string]any{"project": args[0], "title": strings.Join(args[1:], " ")})
	out, err := r.commJSON(http.MethodPost, "/api/plans", string(raw))
	if err != nil {
		r.reply("autonomous plan new failed", err.Error())
		return
	}
	var res struct {
		Plan struct {
			Title string `json:"title"`
			ID    string `json:"id"`
		} `json:"plan"`
	}
	if json.Unmarshal([]byte(out), &res) != nil || res.Plan.ID == "" {
		r.reply("autonomous plan new", "created")
		return
	}
	r.reply("autonomous plan new", fmt.Sprintf("Created plan %s [%s]", res.Plan.Title, res.Plan.ID))
}

// planMovePosition parses a 1-based position.
func planMovePosition(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil && n >= 1
}

// moveIndex returns ids with the entry at from moved to position pos (1-based,
// clamped to the end).
func moveIndex(ids []string, from, pos int) []string {
	out := make([]string, 0, len(ids))
	id := ids[from]
	for i, v := range ids {
		if i != from {
			out = append(out, v)
		}
	}
	at := pos - 1
	if at > len(out) {
		at = len(out)
	}
	out = append(out, "")
	copy(out[at+1:], out[at:])
	out[at] = id
	return out
}

func (r *Router) planMove(args []string) {
	switch {
	case len(args) == 3:
		pos, ok := planMovePosition(args[2])
		if !ok {
			r.reply("autonomous plan failed", planVerbUsage)
			return
		}
		r.planMovePlan(args[0], args[1], pos)
	case len(args) == 4 && strings.EqualFold(args[1], "automaton"):
		pos, ok := planMovePosition(args[3])
		if !ok {
			r.reply("autonomous plan failed", planVerbUsage)
			return
		}
		r.planMoveAutomaton(args[0], args[2], pos)
	default:
		r.reply("autonomous plan failed", planVerbUsage)
	}
}

func (r *Router) planMovePlan(project, plan string, pos int) {
	res, err := r.planFetchList(project)
	if err != nil {
		r.reply("autonomous plan move failed", err.Error())
		return
	}
	all := append(append([]planWireRow{}, res.Plans...), res.Backlog.Plans...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].Order < all[j].Order })
	ids := make([]string, 0, len(all))
	from := -1
	for i, p := range all {
		ids = append(ids, p.ID)
		if strings.EqualFold(p.ID, plan) || strings.EqualFold(strings.Join(strings.Fields(p.Title), " "), strings.Join(strings.Fields(plan), " ")) {
			from = i
		}
	}
	if from < 0 {
		r.reply("autonomous plan move failed", fmt.Sprintf("no plan %q in project %s", plan, project))
		return
	}
	raw, _ := json.Marshal(map[string]any{"plans": moveIndex(ids, from, pos)})
	if _, err := r.commJSON(http.MethodPut, "/api/plans/order", string(raw)); err != nil {
		r.reply("autonomous plan move failed", err.Error())
		return
	}
	r.reply("autonomous plan move", fmt.Sprintf("Moved plan %s to position %d", plan, pos))
}

func (r *Router) planMoveAutomaton(plan, automaton string, pos int) {
	out, err := r.commGet("/api/plans/"+url.PathEscape(plan)+"/automata", url.Values{"limit": {"500"}})
	if err != nil {
		r.reply("autonomous plan move failed", err.Error())
		return
	}
	var cur planWireRun
	if json.Unmarshal([]byte(out), &cur) != nil {
		r.reply("autonomous plan move failed", "the daemon sent a plan I cannot read")
		return
	}
	// The stored plan order is the position field, not the run order.
	sort.SliceStable(cur.Automata, func(i, j int) bool { return cur.Automata[i].Position < cur.Automata[j].Position })
	ids := make([]string, 0, len(cur.Automata))
	from := -1
	for i, a := range cur.Automata {
		ids = append(ids, a.ID)
		if a.ID == automaton {
			from = i
		}
	}
	if from < 0 {
		r.reply("autonomous plan move failed", fmt.Sprintf("Automaton %q is not linked to plan %s", automaton, plan))
		return
	}
	raw, _ := json.Marshal(map[string]any{"automata": moveIndex(ids, from, pos)})
	out, err = r.commJSON(http.MethodPut, "/api/plans/"+url.PathEscape(plan)+"/automata/order", string(raw))
	if err != nil {
		r.reply("autonomous plan move failed", err.Error())
		return
	}
	msg := fmt.Sprintf("Moved Automaton %s to position %d.", automaton, pos)
	var res planWireRun
	if json.Unmarshal([]byte(out), &res) == nil {
		if res.Next != nil {
			next := strings.Join(strings.Fields(res.Next.Title), " ")
			if next == "" {
				next = res.Next.AutomatonID
			}
			msg += " Runs next: " + next + " (blocked and held are skipped)."
		} else {
			msg += " Nothing is waiting to run next."
		}
	}
	r.reply("autonomous plan move", msg)
}

func (r *Router) planLink(verb string, args []string) {
	if len(args) != 3 {
		r.reply("autonomous plan failed", planVerbUsage)
		return
	}
	kind := strings.ToLower(args[1])
	switch kind {
	case "automaton", "automation", "finding":
	default:
		r.reply("autonomous plan "+verb+" failed", "the kind must be automaton, automation or finding")
		return
	}
	raw, _ := json.Marshal(map[string]any{"kind": kind, "id": args[2]})
	if _, err := r.commJSON(http.MethodPost, "/api/plans/"+url.PathEscape(args[0])+"/"+verb, string(raw)); err != nil {
		r.reply("autonomous plan "+verb+" failed", err.Error())
		return
	}
	past := "Attached"
	prep := "to"
	if verb == "detach" {
		past, prep = "Detached", "from"
	}
	r.reply("autonomous plan "+verb, fmt.Sprintf("%s %s %s %s plan %s", past, kind, args[2], prep, args[0]))
}
