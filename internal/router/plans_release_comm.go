// plans_release_comm.go — the release plan verbs over the chat channel. They
// are subcommands of the plan verb (arcs works in place of plan):
//
//	autonomous plan themes <plan>
//		a release plan's themes in order, with the items in each. A read.
//	autonomous plan theme add <plan> <name…>
//	autonomous plan theme rename <plan> <theme> <new name…>
//	autonomous plan theme remove <plan> <theme>
//	autonomous plan theme place <plan> <theme> automaton|automation <id>
//	autonomous plan theme unplace <plan> automaton|automation <id>
//	autonomous plan theme reorder <plan> <theme> [<theme>…]
//	autonomous plan theme reorder-items <plan> <theme> automaton|automation <id> [<id>…]
//	autonomous plan archive <plan> [protect=<plan>] [reason…]
//	autonomous plan restore <plan>
//	autonomous plan unattributed [project=P] [current=R] [next=R]
//		the Automata no release plan holds, with the release and theme the rules
//		suggest. A read.
//	autonomous plan migrate release project=P [current=R] [next=R]
//		preview the release migration; writes nothing and replies with the
//		preview id.
//	autonomous plan migrate release approve <preview-id> project=P [current=R] [next=R]
//		write exactly that preview. Without the preview id it is refused before
//		anything is sent, and the daemon refuses a stale id.
//	autonomous plan attach <plan> automaton|automation <id> [<id>…] [theme=T]
//		attach one or several; each id gets its own line, and an Automaton
//		attached to another plan is reported as refused with the holder named.
//
// A theme is named by its id, or by its name when it is one word. Every verb
// calls the loopback REST route the other surfaces call, with the daemon's
// bearer token, so the chat channel runs the same code and the same
// authorization as REST: the channel's sender checks come first, then the
// route's own capability check.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// planReleaseVerbUsage is the usage line of the release plan verbs.
const planReleaseVerbUsage = "usage: autonomous plan themes <plan> | plan theme add <plan> <name...> | plan theme rename <plan> <theme> <new name...> | plan theme remove <plan> <theme> | plan theme place <plan> <theme> automaton|automation <id> | plan theme unplace <plan> automaton|automation <id> | plan theme reorder <plan> <theme>... | plan theme reorder-items <plan> <theme> automaton|automation <id>... | plan archive <plan> [protect=<plan>] [reason...] | plan restore <plan> | plan unattributed [project=P] [current=R] [next=R] | plan migrate release project=P [current=R] [next=R] | plan migrate release approve <preview-id> project=P [current=R] [next=R] | plan attach <plan> automaton|automation <id>... [theme=T]"

// PlanReleaseCommVerbs lists the release plan verbs (the words after plan),
// for the parity test. Keep it in step with handlePlanVerb.
func PlanReleaseCommVerbs() []string {
	return []string{"themes", "theme", "archive", "restore", "unattributed", "migrate"}
}

// planReleaseLines caps the lines of one release plan reply.
const planReleaseLines = 20

// planKV splits arguments into the plain words and the key=value words whose
// key is one of keys (case-insensitive); a key=value word with another key
// stays a plain word, so a title with an equals sign is not lost.
func planKV(args []string, keys ...string) (words []string, kv map[string]string) {
	kv = map[string]string{}
	known := map[string]bool{}
	for _, k := range keys {
		known[k] = true
	}
	for _, a := range args {
		if i := strings.Index(a, "="); i > 0 {
			if k := strings.ToLower(a[:i]); known[k] {
				kv[k] = strings.TrimSpace(a[i+1:])
				continue
			}
		}
		words = append(words, a)
	}
	return words, kv
}

func planItemKindWord(s string) (string, bool) {
	switch k := strings.ToLower(strings.TrimSpace(s)); k {
	case "automaton", "automation":
		return k, true
	}
	return "", false
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// ----- themes ---------------------------------------------------------------

type planThemesWire struct {
	Plan struct {
		Title    string `json:"title"`
		ID       string `json:"id"`
		Release  string `json:"release"`
		Archived bool   `json:"archived"`
	} `json:"plan"`
	Themes []struct {
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Items []planThemeItem `json:"items"`
	} `json:"themes"`
	Unplaced []planThemeItem `json:"unplaced"`
}

type planThemeItem struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (it planThemeItem) label() string {
	if t := oneLine(it.Title); t != "" {
		return t + " [" + it.ID + "]"
	}
	return "[" + it.ID + "]"
}

func planThemesText(body string) string {
	var res planThemesWire
	if err := json.Unmarshal([]byte(body), &res); err != nil || res.Plan.ID == "" {
		return "the daemon sent a theme list I cannot read"
	}
	head := fmt.Sprintf("Plan %s [%s]", oneLine(res.Plan.Title), res.Plan.ID)
	if res.Plan.Release != "" {
		head += " release " + res.Plan.Release
	}
	if res.Plan.Archived {
		head += " archived"
	}
	lines := []string{head}
	var rows []string
	for i, t := range res.Themes {
		var names []string
		for _, it := range t.Items {
			names = append(names, it.label())
		}
		row := fmt.Sprintf("%d. %s [%s]", i+1, oneLine(t.Name), t.ID)
		if len(names) == 0 {
			row += ": empty"
		} else {
			row += ": " + strings.Join(names, "; ")
		}
		rows = append(rows, row)
	}
	if len(res.Unplaced) > 0 {
		var names []string
		for _, it := range res.Unplaced {
			names = append(names, it.label())
		}
		rows = append(rows, "unplaced: "+strings.Join(names, "; "))
	}
	if len(rows) == 0 {
		rows = append(rows, "No themes yet.")
	}
	for i, row := range rows {
		if i >= planReleaseLines {
			lines = append(lines, fmt.Sprintf("… and %d more (use plan theme list in the CLI for the rest)", len(rows)-i))
			break
		}
		lines = append(lines, row)
	}
	return strings.Join(lines, "\n")
}

func planPath(plan, suffix string) string { return "/api/plans/" + url.PathEscape(plan) + suffix }

// planThemes answers "autonomous plan themes <plan>".
func (r *Router) planThemes(args []string) {
	if len(args) != 1 {
		r.reply("autonomous plan themes failed", planReleaseVerbUsage)
		return
	}
	out, err := r.commGet(planPath(args[0], "/themes"), nil)
	if err != nil {
		r.reply("autonomous plan themes failed", err.Error())
		return
	}
	r.reply("autonomous plan themes", planThemesText(out))
}

// planTheme answers "autonomous plan theme <action> ...".
func (r *Router) planTheme(args []string) {
	fail := func(msg string) { r.reply("autonomous plan theme failed", msg) }
	if len(args) < 2 {
		fail(planReleaseVerbUsage)
		return
	}
	action, plan, rest := strings.ToLower(args[0]), args[1], args[2:]
	body := map[string]any{}
	var done string
	switch action {
	case "add", "create":
		if len(rest) < 1 {
			fail(planReleaseVerbUsage)
			return
		}
		name := strings.Join(rest, " ")
		body["action"], body["name"] = "create", name
		done = fmt.Sprintf("Added theme %s to plan %s", name, plan)
	case "rename":
		if len(rest) < 2 {
			fail(planReleaseVerbUsage)
			return
		}
		name := strings.Join(rest[1:], " ")
		body["action"], body["theme"], body["name"] = "rename", rest[0], name
		done = fmt.Sprintf("Renamed theme %s to %s in plan %s", rest[0], name, plan)
	case "remove", "delete":
		if len(rest) != 1 {
			fail(planReleaseVerbUsage)
			return
		}
		body["action"], body["theme"] = "delete", rest[0]
		done = fmt.Sprintf("Removed theme %s from plan %s; its items stay in the plan, unplaced", rest[0], plan)
	case "place":
		kind, ok := "", false
		if len(rest) == 3 {
			kind, ok = planItemKindWord(rest[1])
		}
		if !ok {
			fail(planReleaseVerbUsage)
			return
		}
		body["action"], body["theme"], body["kind"], body["id"] = "place", rest[0], kind, rest[2]
		done = fmt.Sprintf("Placed %s %s in theme %s of plan %s", kind, rest[2], rest[0], plan)
	case "unplace":
		kind, ok := "", false
		if len(rest) == 2 {
			kind, ok = planItemKindWord(rest[0])
		}
		if !ok {
			fail(planReleaseVerbUsage)
			return
		}
		body["action"], body["kind"], body["id"] = "unplace", kind, rest[1]
		done = fmt.Sprintf("Unplaced %s %s in plan %s", kind, rest[1], plan)
	case "reorder":
		if len(rest) < 1 {
			fail(planReleaseVerbUsage)
			return
		}
		body["action"], body["themes"] = "reorder", rest
		done = fmt.Sprintf("Reordered the themes of plan %s", plan)
	case "reorder-items", "reorder_items":
		kind, ok := "", false
		if len(rest) >= 3 {
			kind, ok = planItemKindWord(rest[1])
		}
		if !ok {
			fail(planReleaseVerbUsage)
			return
		}
		body["action"], body["theme"], body["kind"], body["items"] = "reorder_items", rest[0], kind, rest[2:]
		done = fmt.Sprintf("Reordered the %s items of theme %s in plan %s", kind, rest[0], plan)
	default:
		fail(planReleaseVerbUsage)
		return
	}
	raw, _ := json.Marshal(body)
	out, err := r.commJSON(http.MethodPost, planPath(plan, "/themes"), string(raw))
	if err != nil {
		r.reply("autonomous plan theme "+action+" failed", err.Error())
		return
	}
	r.reply("autonomous plan theme "+action, done+"\n"+planThemesText(out))
}

// ----- archive and restore -------------------------------------------------

type planArchiveWire struct {
	Plan struct {
		Title string `json:"title"`
		ID    string `json:"id"`
	} `json:"plan"`
	AutomataHeld int  `json:"automata_held"`
	Changed      bool `json:"changed"`
}

func planArchiveText(verb, body string) string {
	var res planArchiveWire
	if err := json.Unmarshal([]byte(body), &res); err != nil || res.Plan.ID == "" {
		return verb + " done"
	}
	name := fmt.Sprintf("%s [%s]", oneLine(res.Plan.Title), res.Plan.ID)
	switch {
	case !res.Changed && verb == "archive":
		return fmt.Sprintf("Plan %s was already archived; nothing changed", name)
	case !res.Changed:
		return fmt.Sprintf("Plan %s was not archived; nothing changed", name)
	case verb == "archive":
		return fmt.Sprintf("Archived plan %s (it still holds %d Automata; nothing was deleted)", name, res.AutomataHeld)
	}
	return fmt.Sprintf("Restored plan %s", name)
}

// planArchive answers "autonomous plan archive <plan> [protect=<plan>] [reason…]".
func (r *Router) planArchive(args []string) {
	words, kv := planKV(args, "protect", "reason")
	if len(words) < 1 {
		r.reply("autonomous plan archive failed", planReleaseVerbUsage)
		return
	}
	body := map[string]any{}
	reason := strings.TrimSpace(strings.Join(words[1:], " "))
	if kv["reason"] != "" {
		reason = strings.TrimSpace(kv["reason"] + " " + reason)
	}
	if reason != "" {
		body["reason"] = reason
	}
	if kv["protect"] != "" {
		body["protected_plan"] = kv["protect"]
	}
	raw, _ := json.Marshal(body)
	out, err := r.commJSON(http.MethodPost, planPath(words[0], "/archive"), string(raw))
	if err != nil {
		r.reply("autonomous plan archive failed", err.Error())
		return
	}
	r.reply("autonomous plan archive", planArchiveText("archive", out))
}

// planRestore answers "autonomous plan restore <plan>".
func (r *Router) planRestore(args []string) {
	if len(args) != 1 {
		r.reply("autonomous plan restore failed", planReleaseVerbUsage)
		return
	}
	out, err := r.commJSON(http.MethodPost, planPath(args[0], "/restore"), "{}")
	if err != nil {
		r.reply("autonomous plan restore failed", err.Error())
		return
	}
	r.reply("autonomous plan restore", planArchiveText("restore", out))
}

// ----- unattributed ---------------------------------------------------------

type planUnattributedWire struct {
	Automata []struct {
		Title            string `json:"title"`
		ID               string `json:"id"`
		State            string `json:"state"`
		Project          string `json:"project"`
		SuggestedRelease string `json:"suggested_release"`
		SuggestedTheme   string `json:"suggested_theme"`
		HoldingPlan      string `json:"holding_plan"`
		HoldingPlanID    string `json:"holding_plan_id"`
	} `json:"automata"`
	Page struct {
		Total int `json:"total"`
	} `json:"page"`
}

func planUnattributedText(body string) string {
	var res planUnattributedWire
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return "the daemon sent a list I cannot read"
	}
	if len(res.Automata) == 0 {
		return "Every Automaton is held by a release plan."
	}
	var lines []string
	for i, a := range res.Automata {
		if i >= planRecordsMaxLines {
			break
		}
		l := fmt.Sprintf("%s [%s] %s", oneLine(a.Title), a.ID, a.State)
		if a.Project != "" {
			l += " " + a.Project
		}
		if a.SuggestedRelease != "" {
			l += " → release " + a.SuggestedRelease
			if a.SuggestedTheme != "" {
				l += ", theme " + a.SuggestedTheme
			}
		}
		if a.HoldingPlan != "" {
			l += fmt.Sprintf(" — held by plan %s [%s]; detach it first", oneLine(a.HoldingPlan), a.HoldingPlanID)
		}
		lines = append(lines, l)
	}
	total := res.Page.Total
	if total < len(res.Automata) {
		total = len(res.Automata)
	}
	if total > len(lines) {
		lines = append(lines, fmt.Sprintf("… and %d more (use plan unattributed in the CLI for the rest)", total-len(lines)))
	}
	return strings.Join(lines, "\n")
}

// planUnattributed answers "autonomous plan unattributed [project=P] [current=R] [next=R]".
func (r *Router) planUnattributed(args []string) {
	words, kv := planKV(args, "project", "current", "current_release", "next", "next_release")
	if len(words) != 0 {
		r.reply("autonomous plan unattributed failed", planReleaseVerbUsage)
		return
	}
	q := url.Values{"limit": {fmt.Sprint(planRecordsMaxLines)}}
	if kv["project"] != "" {
		q.Set("project", kv["project"])
	}
	for _, k := range []string{"current", "next"} {
		v := kv[k]
		if v == "" {
			v = kv[k+"_release"]
		}
		if v != "" {
			q.Set(k+"_release", v)
		}
	}
	out, err := r.commGet("/api/plans/unattributed", q)
	if err != nil {
		r.reply("autonomous plan unattributed failed", err.Error())
		return
	}
	r.reply("autonomous plan unattributed", planUnattributedText(out))
}

// ----- migrate release ------------------------------------------------------

type planReleaseMigrateWire struct {
	Applied        bool                     `json:"applied"`
	PreviewID      string                   `json:"preview_id"`
	CurrentRelease string                   `json:"current_release"`
	NextRelease    string                   `json:"next_release"`
	Created        []planReleaseMigrateItem `json:"created"`
	Attached       []planReleaseMigrateItem `json:"attached"`
	Unplaced       []planReleaseMigrateItem `json:"unplaced"`
	Conflicts      []planReleaseMigrateItem `json:"conflicts"`
	Unchanged      []planReleaseMigrateItem `json:"unchanged"`
	Skipped        []planReleaseMigrateItem `json:"skipped"`
}

type planReleaseMigrateItem struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	Title         string `json:"title"`
	HoldingPlan   string `json:"holding_plan"`
	HoldingPlanID string `json:"holding_plan_id"`
	Reason        string `json:"reason"`
}

// planReleaseMigrateText summarises a release migration answer: the counts,
// then each conflict (the rest is in the REST answer). The hint for writing
// exactly this preview is again, then the preview id, then suffix (the project
// and release words the approve needs).
func planReleaseMigrateText(body, again, suffix string) string {
	var res planReleaseMigrateWire
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return "the daemon sent a migration result I cannot read"
	}
	head, mk, at := "Preview", "would create", "would attach"
	if res.Applied {
		head, mk, at = "Release migration", "created", "attached"
	}
	lines := []string{fmt.Sprintf("%s (release %s, next %s): %s %d, %s %d, no theme %d, refused %d, already right %d, skipped %d",
		head, res.CurrentRelease, res.NextRelease, mk, len(res.Created), at, len(res.Attached),
		len(res.Unplaced), len(res.Conflicts), len(res.Unchanged), len(res.Skipped))}
	for i, c := range res.Conflicts {
		if i >= planRecordsMaxLines {
			lines = append(lines, fmt.Sprintf("… and %d more refused", len(res.Conflicts)-i))
			break
		}
		name := oneLine(c.Title)
		if name == "" {
			name = c.ID
		}
		l := "refused: " + name
		if c.HoldingPlan != "" {
			l += fmt.Sprintf(" — held by plan %s [%s]", oneLine(c.HoldingPlan), c.HoldingPlanID)
		}
		if c.Reason != "" {
			l += " — " + c.Reason
		}
		lines = append(lines, l)
	}
	if !res.Applied && res.PreviewID != "" {
		lines = append(lines, "Preview id: "+res.PreviewID, "To write exactly this send: "+again+" "+res.PreviewID+suffix)
	}
	return strings.Join(lines, "\n")
}

// planMigrate answers "autonomous plan migrate ...". "plan migrate [apply]" is
// the older arcs migration (the same as plan-migrate); "plan migrate release"
// is the release migration.
func (r *Router) planMigrate(args []string) {
	if len(args) == 0 || !strings.EqualFold(args[0], "release") {
		r.handlePlanMigrateVerb(args)
		return
	}
	words, kv := planKV(args[1:], "project", "current", "current_release", "next", "next_release")
	approve := len(words) > 0 && strings.EqualFold(words[0], "approve")
	previewID := ""
	if approve {
		words = words[1:]
		if len(words) > 0 {
			previewID, words = words[0], words[1:]
		}
	}
	if len(words) != 0 || kv["project"] == "" {
		r.reply("autonomous plan migrate failed", "release needs project=P: "+planReleaseVerbUsage)
		return
	}
	if approve && previewID == "" {
		r.reply("autonomous plan migrate failed",
			"approve needs the preview id: send autonomous plan migrate release project="+kv["project"]+" first, read the preview, then approve with its id. Nothing was written.")
		return
	}
	body := map[string]any{"mode": "release", "project": kv["project"], "apply": approve}
	again := "autonomous plan migrate release approve"
	suffix := " project=" + kv["project"]
	for _, k := range []string{"current", "next"} {
		v := kv[k]
		if v == "" {
			v = kv[k+"_release"]
		}
		if v != "" {
			body[k+"_release"] = v
			suffix += " " + k + "=" + v
		}
	}
	if approve {
		body["preview_id"] = previewID
	}
	raw, _ := json.Marshal(body)
	out, err := r.commJSON(http.MethodPost, "/api/plans/migrate", string(raw))
	if err != nil {
		r.reply("autonomous plan migrate failed", err.Error())
		return
	}
	r.reply("autonomous plan migrate release", planReleaseMigrateText(out, again, suffix))
}

// ----- attach several -------------------------------------------------------

type planBulkAttachWire struct {
	Plan struct {
		Title string `json:"title"`
		ID    string `json:"id"`
	} `json:"plan"`
	Results []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
		Theme  string `json:"theme"`
		Holder string `json:"holder"`
		Error  string `json:"error"`
	} `json:"results"`
}

func planBulkAttachText(body string) string {
	var res planBulkAttachWire
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return "the daemon sent an attach result I cannot read"
	}
	ok := 0
	var lines []string
	for _, x := range res.Results {
		name := x.ID
		if t := oneLine(x.Title); t != "" {
			name = t + " [" + x.ID + "]"
		}
		switch x.Status {
		case "attached", "already_attached":
			ok++
		case "refused":
			l := "refused: " + name
			if x.Holder != "" {
				l += " — attached to plan " + x.Holder + "; detach it first"
			} else if x.Error != "" {
				l += " — " + x.Error
			}
			lines = append(lines, l)
		default:
			lines = append(lines, fmt.Sprintf("%s: %s — %s", strings.ReplaceAll(x.Status, "_", " "), name, x.Error))
		}
	}
	head := fmt.Sprintf("Attached %d of %d to plan %s [%s]", ok, len(res.Results), oneLine(res.Plan.Title), res.Plan.ID)
	if len(lines) > planRecordsMaxLines {
		more := len(lines) - planRecordsMaxLines
		lines = append(lines[:planRecordsMaxLines], fmt.Sprintf("… and %d more", more))
	}
	return strings.Join(append([]string{head}, lines...), "\n")
}

// planAttachMany answers "autonomous plan attach <plan> automaton|automation
// <id>… [theme=T]" when more than one id, or a theme, is given.
func (r *Router) planAttachMany(args []string) {
	words, kv := planKV(args, "theme")
	kind, okKind := "", false
	if len(words) >= 3 {
		kind, okKind = planItemKindWord(words[1])
	}
	if !okKind {
		r.reply("autonomous plan attach failed", planReleaseVerbUsage)
		return
	}
	body := map[string]any{"kind": kind, "ids": words[2:]}
	if kv["theme"] != "" {
		body["theme"] = kv["theme"]
	}
	raw, _ := json.Marshal(body)
	out, err := r.commJSON(http.MethodPost, planPath(words[0], "/attach"), string(raw))
	if err != nil {
		r.reply("autonomous plan attach failed", err.Error())
		return
	}
	r.reply("autonomous plan attach", planBulkAttachText(out))
}
