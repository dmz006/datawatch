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

package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	// planRecordsUsage is the usage line for the plan record comm verbs; a bad
	// verb or a missing argument replies with it.
	planRecordsUsage = "usage: autonomous plan-records [registry-row|finding] [project=P] [state=S] | plan-record get <ref> | plan-record create <json> | plan-record update <ref> <json> | plan-record search <text> | plan-record import [dry-run] [project=P] [source=registry|findings|all] [policy=suffix|rename|skip]"

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
	return []string{"plan-records", "plan-record"}
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
