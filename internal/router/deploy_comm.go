// autonomous deploy over the chat channel:
//
//	autonomous deploy [show]   the effective settings and the last record
//	autonomous deploy run      start the deploy sequence for the current build
//
// The replies render the same text the CLI does (internal/deploy view.go) and
// never include the snapshot contents or any secret. The settings are changed
// with configure, for example configure autonomous.deploy.health_timeout=2m.

package router

import (
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/deploy"
)

const (
	deployCommPath    = "/api/autonomous/deploy"
	deployCommRunPath = deployCommPath + "/run"
	deployCommMaxChar = 3000
	deployUsage       = "usage: autonomous deploy [show] | run"
)

// DeployCommVerbs lists the deploy comm verbs, for the parity test.
func DeployCommVerbs() []string { return []string{"deploy"} }

// DeployConfigureKeys lists the configure keys of the block.
func DeployConfigureKeys() []string {
	return []string{
		"autonomous.deploy.build_command",
		"autonomous.deploy.install_command",
		"autonomous.deploy.binary_path",
		"autonomous.deploy.restart_command",
		"autonomous.deploy.health_check",
		"autonomous.deploy.drain_timeout",
		"autonomous.deploy.health_timeout",
	}
}

func (r *Router) replyDeploy(args []string) {
	const label = "autonomous deploy"
	fail := func(msg string) { r.reply(label+" failed", msg) }
	words := args[1:]
	sub := "show"
	if len(words) > 0 {
		sub, words = strings.ToLower(words[0]), words[1:]
	}
	if len(words) > 0 || (sub != "show" && sub != "run") {
		fail(deployUsage)
		return
	}
	if sub == "show" {
		out, err := r.commGet(deployCommPath, nil)
		if err != nil {
			fail(err.Error())
			return
		}
		v, err := deploy.ParseStatusView([]byte(out))
		if err != nil {
			fail(err.Error())
			return
		}
		r.reply(label, cutDeployChat(deploy.RenderStatusText(v)))
		return
	}
	out, err := r.commJSON(http.MethodPost, deployCommRunPath, "")
	if err != nil {
		fail(err.Error())
		return
	}
	text, err := deploy.RenderRunText([]byte(out))
	if err != nil {
		fail(err.Error())
		return
	}
	r.reply(label, cutDeployChat(strings.TrimRight(text, "\n")))
}

func cutDeployChat(s string) string {
	s = strings.TrimRight(s, "\n")
	if len(s) > deployCommMaxChar {
		return s[:deployCommMaxChar] + "\n…"
	}
	return s
}
