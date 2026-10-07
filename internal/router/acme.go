// BL397 — comm-channel verb for the native ACME/Let's Encrypt subsystem.
//
//	acme                → status (default)
//	acme status          → cert state for every configured domain
//	acme renew           → force a re-order now
//	acme verify           → DNS + directory reachability pre-flight check

package router

import "strings"

const acmeUsage = `Usage:
  acme                 show cert state (same as 'acme status')
  acme status           show cert state for every configured domain
  acme renew            force an ACME re-order for every domain now
  acme verify            DNS resolution + ACME directory reachability check`

func (r *Router) handleAcmeCmd(cmd Command) {
	text := strings.ToLower(strings.TrimSpace(cmd.Text))

	switch text {
	case "", "status":
		out, err := r.commGet("/api/acme/status", nil)
		if err != nil {
			r.reply("acme", err.Error())
			return
		}
		r.reply("acme", prettyJSON(out))
	case "renew":
		out, err := r.commJSON("POST", "/api/acme/renew", "")
		if err != nil {
			r.reply("acme", err.Error())
			return
		}
		r.reply("acme", prettyJSON(out))
	case "verify":
		out, err := r.commGet("/api/acme/verify", nil)
		if err != nil {
			r.reply("acme", err.Error())
			return
		}
		r.reply("acme", prettyJSON(out))
	default:
		r.reply("acme", acmeUsage)
	}
}
