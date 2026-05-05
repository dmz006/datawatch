// BL255 v6.7.0 — comm-channel verbs for the skills subsystem.
//
//   skills                                  → list synced
//   skills registry [list]                  → list registries
//   skills registry get <name>
//   skills registry add <name> <url> [branch=main] [auth=${secret:...}] [desc="..."]
//   skills registry update <name> [...]
//   skills registry delete <name>
//   skills registry add-default
//   skills registry connect <name>
//   skills registry browse <name>           → available list
//   skills registry sync <name> <skill1,skill2|all>
//   skills registry unsync <name> <skill1|all>
//   skills get <name>
//   skills load <name>                      → markdown content

package router

import (
	"encoding/json"
	"net/http"
	"strings"
)

const skillsUsage = `Usage:
  skills                                          list synced skills
  skills registry [list]                          list registries
  skills registry get <name>
  skills registry add <name> <git-url> [branch=...] [auth=${secret:...}] [desc="..."]
  skills registry update <name> [branch=...] [auth=...] [desc=...]
  skills registry delete <name>
  skills registry add-default                     idempotent PAI default
  skills registry connect <name>                  shallow-clone repo
  skills registry browse <name>                   list available
  skills registry sync <name> <skill[,skill]|all>
  skills registry unsync <name> <skill[,skill]|all>
  skills get <name>
  skills load <name>                              markdown content`

func (r *Router) handleSkillsCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	lower := strings.ToLower(text)

	// "skills" with no arg → list synced
	if text == "" {
		r.skillsListSynced()
		return
	}

	// "skills get <name>"
	if strings.HasPrefix(lower, "get ") {
		name := strings.TrimSpace(text[4:])
		if name == "" {
			r.reply("skills get", "Usage: skills get <name>")
			return
		}
		out, err := r.commGet("/api/skills/"+name, nil)
		if err != nil {
			r.reply("skills get failed", err.Error())
			return
		}
		r.reply("skill: "+name, prettyJSON(out))
		return
	}

	// "skills load <name>"
	if strings.HasPrefix(lower, "load ") {
		name := strings.TrimSpace(text[5:])
		if name == "" {
			r.reply("skills load", "Usage: skills load <name>")
			return
		}
		out, err := r.commGet("/api/skills/"+name+"/content", nil)
		if err != nil {
			r.reply("skills load failed", err.Error())
			return
		}
		r.reply("skill: "+name, string(out))
		return
	}

	// "skills list" → list synced (alias for "skills" with no arg)
	if lower == "list" {
		r.skillsListSynced()
		return
	}

	// "skills registry ..."
	if !strings.HasPrefix(lower, "registry") {
		r.reply("skills", skillsUsage)
		return
	}
	rest := strings.TrimSpace(text[len("registry"):])
	r.handleSkillsRegistryCmd(rest)
}

func (r *Router) skillsListSynced() {
	out, err := r.commGet("/api/skills", nil)
	if err != nil {
		r.reply("skills failed", err.Error())
		return
	}
	r.reply("synced skills", prettyJSON(out))
}

func (r *Router) handleSkillsRegistryCmd(rest string) {
	lower := strings.ToLower(rest)
	if rest == "" || lower == "list" {
		out, err := r.commGet("/api/skills/registries", nil)
		if err != nil {
			r.reply("skills registry list failed", err.Error())
			return
		}
		r.reply("skill registries", prettyJSON(out))
		return
	}

	if strings.HasPrefix(lower, "get ") {
		name := strings.TrimSpace(rest[4:])
		out, err := r.commGet("/api/skills/registries/"+name, nil)
		if err != nil {
			r.reply("skills registry get failed", err.Error())
			return
		}
		r.reply("registry: "+name, prettyJSON(out))
		return
	}

	if lower == "add-default" {
		out, err := r.commJSON(http.MethodPost, "/api/skills/registries/add-default", "")
		if err != nil {
			r.reply("skills registry add-default failed", err.Error())
			return
		}
		r.reply("default registry", prettyJSON(out))
		return
	}

	if strings.HasPrefix(lower, "add ") {
		parts := strings.Fields(rest[4:])
		if len(parts) < 2 {
			r.reply("skills registry add", "Usage: skills registry add <name> <git-url> [branch=main] [auth=${secret:...}] [desc=\"...\"]")
			return
		}
		body := map[string]any{
			"name":    parts[0],
			"url":     parts[1],
			"kind":    "git",
			"enabled": true,
			"branch":  "main",
		}
		for _, p := range parts[2:] {
			if eq := strings.Index(p, "="); eq > 0 {
				k := strings.ToLower(p[:eq])
				v := p[eq+1:]
				switch k {
				case "branch":
					body["branch"] = v
				case "auth":
					body["auth_secret_ref"] = v
				case "desc", "description":
					body["description"] = v
				}
			}
		}
		j, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, "/api/skills/registries", string(j))
		if err != nil {
			r.reply("skills registry add failed", err.Error())
			return
		}
		r.reply("registry added", prettyJSON(out))
		return
	}

	if strings.HasPrefix(lower, "update ") {
		parts := strings.Fields(rest[7:])
		if len(parts) < 1 {
			r.reply("skills registry update", "Usage: skills registry update <name> [branch=...] [auth=...] [desc=...]")
			return
		}
		name := parts[0]
		body := map[string]any{
			"name":    name,
			"kind":    "git",
			"enabled": true,
		}
		for _, p := range parts[1:] {
			if eq := strings.Index(p, "="); eq > 0 {
				k := strings.ToLower(p[:eq])
				v := p[eq+1:]
				switch k {
				case "branch":
					body["branch"] = v
				case "auth":
					body["auth_secret_ref"] = v
				case "desc", "description":
					body["description"] = v
				case "url":
					body["url"] = v
				}
			}
		}
		j, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPut, "/api/skills/registries/"+name, string(j))
		if err != nil {
			r.reply("skills registry update failed", err.Error())
			return
		}
		r.reply("registry updated", prettyJSON(out))
		return
	}

	if strings.HasPrefix(lower, "delete ") {
		name := strings.TrimSpace(rest[7:])
		out, err := r.commJSON(http.MethodDelete, "/api/skills/registries/"+name, "")
		if err != nil {
			r.reply("skills registry delete failed", err.Error())
			return
		}
		r.reply("registry deleted", prettyJSON(out))
		return
	}

	if strings.HasPrefix(lower, "connect ") {
		name := strings.TrimSpace(rest[8:])
		out, err := r.commJSON(http.MethodPost, "/api/skills/registries/"+name+"/connect", "")
		if err != nil {
			r.reply("skills registry connect failed", err.Error())
			return
		}
		r.reply("registry connected: "+name, prettyJSON(out))
		return
	}

	if strings.HasPrefix(lower, "browse ") || strings.HasPrefix(lower, "available ") {
		name := strings.TrimSpace(rest[strings.Index(lower, " ")+1:])
		out, err := r.commGet("/api/skills/registries/"+name+"/available", nil)
		if err != nil {
			r.reply("skills registry browse failed", err.Error())
			return
		}
		r.reply("available skills: "+name, prettyJSON(out))
		return
	}

	if strings.HasPrefix(lower, "sync ") || strings.HasPrefix(lower, "unsync ") {
		op := "sync"
		body := map[string]any{}
		idx := 5
		if strings.HasPrefix(lower, "unsync ") {
			op = "unsync"
			idx = 7
		}
		parts := strings.Fields(rest[idx:])
		if len(parts) < 2 {
			r.reply("skills registry "+op, "Usage: skills registry "+op+" <name> <skill[,skill...]|all>")
			return
		}
		name := parts[0]
		spec := strings.Join(parts[1:], " ")
		if strings.EqualFold(spec, "all") {
			body["all"] = true
		} else {
			var skills []string
			for _, s := range strings.Split(spec, ",") {
				if s = strings.TrimSpace(s); s != "" {
					skills = append(skills, s)
				}
			}
			body["skills"] = skills
		}
		j, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, "/api/skills/registries/"+name+"/"+op, string(j))
		if err != nil {
			r.reply("skills registry "+op+" failed", err.Error())
			return
		}
		r.reply("registry "+op+": "+name, prettyJSON(out))
		return
	}

	r.reply("skills registry", skillsUsage)
}
