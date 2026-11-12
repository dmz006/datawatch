// packs_commands.go — channel commands for installable task and Automaton
// template packs. Each verb proxies the matching /api/packs route over the
// loopback, so the daemon's handlers stay the one place the work happens.
//
//	packs                                      list installed packs
//	packs setting                              read the packs setting
//	packs setting [enabled=true|false] [active=a,b|none]
//	packs install <source_dir>
//	packs accept <name> <version>
//	packs remove <name> <version>
//	packs inputs get <global|project|automaton> <pack> [key]
//	packs inputs set <global|project|automaton> <pack> [key] name=value ...

package router

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

const packsUsage = `usage:
packs                                  list installed packs
packs setting [enabled=true|false] [active=a,b|none]
packs install <source_dir>
packs accept <name> <version>
packs remove <name> <version>
packs inputs get <global|project|automaton> <pack> [key]
packs inputs set <global|project|automaton> <pack> [key] name=value ...`

// PacksCommVerbs lists the packs comm verbs for the parity test.
func PacksCommVerbs() []string {
	return []string{"list", "setting", "install", "accept", "remove", "inputs"}
}

func (r *Router) handlePacks(cmd Command) {
	args := strings.Fields(cmd.Text)
	verb := "list"
	if len(args) > 0 {
		verb, args = strings.ToLower(args[0]), args[1:]
	}
	label := "packs " + verb + " failed"
	fail := func(msg string) { r.reply(label, msg) }

	switch verb {
	case "list":
		if len(args) != 0 {
			fail(packsUsage)
			return
		}
		r.packsReply(label, "packs", func() (string, error) { return r.commGet("/api/packs", nil) })
	case "setting":
		r.packsSetting(label, args)
	case "install":
		if len(args) != 1 {
			fail("usage: packs install <source_dir>")
			return
		}
		r.packsSend(label, "POST", "/api/packs/install", map[string]any{"source_dir": args[0]})
	case "accept":
		if len(args) != 2 {
			fail("usage: packs accept <name> <version>")
			return
		}
		r.packsSend(label, "POST", "/api/packs/accept", map[string]any{"name": args[0], "version": args[1]})
	case "remove":
		if len(args) != 2 {
			fail("usage: packs remove <name> <version>")
			return
		}
		if strings.ContainsAny(args[0]+args[1], `/\`) || args[0] == ".." || args[1] == ".." {
			fail("unsafe name or version")
			return
		}
		r.packsSend(label, "DELETE", "/api/packs/"+url.PathEscape(args[0])+"/"+url.PathEscape(args[1]), nil)
	case "inputs":
		r.packsInputs(args)
	default:
		r.reply("packs failed", packsUsage)
	}
}

func (r *Router) packsReply(label, ok string, call func() (string, error)) {
	out, err := call()
	if err != nil {
		r.reply(label, err.Error())
		return
	}
	r.reply(ok, strings.TrimSpace(out))
}

func (r *Router) packsSend(label, method, path string, body map[string]any) {
	js := ""
	if body != nil {
		b, _ := json.Marshal(body)
		js = string(b)
	}
	r.packsReply(label, strings.TrimSuffix(label, " failed"), func() (string, error) { return r.commJSON(method, path, js) })
}

func (r *Router) packsSetting(label string, args []string) {
	if len(args) == 0 {
		r.packsReply(label, "packs setting", func() (string, error) { return r.commGet("/api/packs/config", nil) })
		return
	}
	body := map[string]any{}
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			r.reply(label, packsUsage)
			return
		}
		switch strings.ToLower(k) {
		case "enabled":
			b, err := strconv.ParseBool(v)
			if err != nil {
				r.reply(label, "enabled must be true or false")
				return
			}
			body["enabled"] = b
		case "active":
			names := []string{}
			if v != "" && !strings.EqualFold(v, "none") {
				for _, n := range strings.Split(v, ",") {
					if n = strings.TrimSpace(n); n != "" {
						names = append(names, n)
					}
				}
			}
			body["active"] = names
		default:
			r.reply(label, "unknown field "+k+"; use enabled= or active=")
			return
		}
	}
	b, _ := json.Marshal(body)
	r.packsReply(label, "packs setting", func() (string, error) { return r.commJSON("PUT", "/api/packs/config", string(b)) })
}

func (r *Router) packsInputs(args []string) {
	const label = "packs inputs failed"
	if len(args) < 3 {
		r.reply(label, packsUsage)
		return
	}
	op, level, pack := strings.ToLower(args[0]), strings.ToLower(args[1]), args[2]
	rest := args[3:]
	switch level {
	case "global", "project", "automaton":
	default:
		r.reply(label, "level must be global, project or automaton")
		return
	}
	key := ""
	var pairs []string
	for _, a := range rest {
		if strings.Contains(a, "=") && op == "set" {
			pairs = append(pairs, a)
		} else if key == "" {
			key = a
		} else {
			r.reply(label, packsUsage)
			return
		}
	}
	if level != "global" && key == "" {
		r.reply(label, "key is required for the "+level+" level")
		return
	}
	q := url.Values{}
	q.Set("level", level)
	q.Set("pack", pack)
	if key != "" {
		q.Set("key", key)
	}
	switch op {
	case "get":
		r.packsReply(label, "packs inputs", func() (string, error) { return r.commGet("/api/packs/inputs", q) })
	case "set":
		inputs := map[string]string{}
		for _, p := range pairs {
			k, v, _ := strings.Cut(p, "=")
			if k == "" {
				r.reply(label, "empty input name")
				return
			}
			inputs[k] = v
		}
		b, _ := json.Marshal(map[string]any{"inputs": inputs})
		r.packsReply(label, "packs inputs", func() (string, error) {
			return r.commJSON("PUT", "/api/packs/inputs?"+q.Encode(), string(b))
		})
	default:
		r.reply(label, packsUsage)
	}
}
