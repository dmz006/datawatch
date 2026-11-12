// Comm verbs for the defect queue: `autonomous defect-intake <description>`,
// `defect-batches` and `defect-split <batch> <story>`. Each calls the REST
// endpoint of the same name over the loopback; the daemon decides the route.

package router

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	defectIntakeUsage  = "usage: autonomous defect-intake <description> project=<project> [kind=<kind>] [area=<area>] [priority=<n>] [files=<a,b>] [id=<id>]"
	defectBatchesUsage = "usage: autonomous defect-batches [project=<project>]"
	defectSplitUsage   = "usage: autonomous defect-split <batch-id> <story-id>"
)

// splitDefectKV separates trailing key=value options from free text. Only
// the given keys count as options; anything else stays part of the text.
func splitDefectKV(tokens []string, keys map[string]bool) (text []string, kv map[string]string) {
	kv = map[string]string{}
	for _, t := range tokens {
		if i := strings.IndexByte(t, '='); i > 0 && keys[strings.ToLower(t[:i])] {
			kv[strings.ToLower(t[:i])] = t[i+1:]
			continue
		}
		text = append(text, t)
	}
	return text, kv
}

func (r *Router) handleDefectQueueVerb(verb string, args []string) {
	switch strings.ReplaceAll(verb, "_", "-") {
	case "defect-intake":
		r.replyDefectIntake(args)
	case "defect-batches":
		r.replyDefectBatches(args)
	case "defect-split":
		r.replyDefectSplit(args)
	}
}

func (r *Router) replyDefectIntake(args []string) {
	text, kv := splitDefectKV(args[1:], map[string]bool{"project": true, "kind": true, "area": true, "priority": true, "files": true, "id": true})
	desc := strings.TrimSpace(strings.Join(text, " "))
	project := strings.TrimSpace(kv["project"])
	if desc == "" || project == "" {
		r.reply("autonomous defect-intake failed", defectIntakeUsage)
		return
	}
	title := desc
	if i := strings.IndexAny(title, ".\n"); i > 0 {
		title = title[:i]
	}
	if len(title) > 80 {
		title = title[:80]
	}
	id := kv["id"]
	if id == "" {
		var b [6]byte
		_, _ = rand.Read(b[:])
		id = "defect-" + hex.EncodeToString(b[:])
	}
	body := map[string]any{"id": id, "title": strings.TrimSpace(title), "description": desc, "project": project}
	if v := kv["kind"]; v != "" {
		body["kind"] = v
	}
	if v := kv["area"]; v != "" {
		body["area"] = v
	}
	if v := kv["priority"]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			r.reply("autonomous defect-intake failed", defectIntakeUsage)
			return
		}
		body["priority"] = n
	}
	if v := kv["files"]; v != "" {
		body["files"] = strings.Split(v, ",")
	}
	raw, _ := json.Marshal(body)
	out, err := r.commJSON(http.MethodPost, "/api/autonomous/defects/intake", string(raw))
	if err != nil {
		r.reply("autonomous defect-intake failed", err.Error())
		return
	}
	var dec struct {
		Route          string `json:"route"`
		BatchID        string `json:"batch_id"`
		Reason         string `json:"reason"`
		TinyFixRefusal string `json:"tiny_fix_refusal"`
	}
	if json.Unmarshal([]byte(out), &dec) != nil || dec.Route == "" {
		r.reply("autonomous defect-intake", prettyJSON(out))
		return
	}
	msg := fmt.Sprintf("defect %s\nroute: %s\nreason: %s", id, dec.Route, dec.Reason)
	if dec.BatchID != "" {
		msg += "\nbatch: " + dec.BatchID
	}
	if dec.TinyFixRefusal != "" {
		msg += "\ntiny-fix refused: " + dec.TinyFixRefusal
	}
	r.reply("autonomous defect-intake", msg)
}

func (r *Router) replyDefectBatches(args []string) {
	_, kv := splitDefectKV(args[1:], map[string]bool{"project": true})
	if len(args) > 2 || (len(args) == 2 && len(kv) == 0) {
		r.reply("autonomous defect-batches failed", defectBatchesUsage)
		return
	}
	var q url.Values
	if p := strings.TrimSpace(kv["project"]); p != "" {
		q = url.Values{"project": {p}}
	}
	out, err := r.commGet("/api/autonomous/defects/batches", q)
	if err != nil {
		r.reply("autonomous defect-batches failed", err.Error())
		return
	}
	r.reply("autonomous defect-batches", prettyJSON(out))
}

func (r *Router) replyDefectSplit(args []string) {
	if len(args) != 3 {
		r.reply("autonomous defect-split failed", defectSplitUsage)
		return
	}
	raw, _ := json.Marshal(map[string]string{"story_id": args[2]})
	out, err := r.commJSON(http.MethodPost, "/api/autonomous/defects/batches/"+url.PathEscape(args[1])+"/split", string(raw))
	if err != nil {
		r.reply("autonomous defect-split failed", err.Error())
		return
	}
	r.reply("autonomous defect-split", prettyJSON(out))
}
