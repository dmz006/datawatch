package server

import "strings"

func (s *Server) handleGetConfig() {
	ptr := map[string]interface{}{"flag": s.cfg.Ptr.Flag}
	out := map[string]interface{}{
		"hostname": s.cfg.Hostname,
		"no_ref":   s.cfg.NoRef,
		"no_web":   s.cfg.NoWeb,
		"no_put":   s.cfg.NoPut,
		"untagged": s.cfg.Untagged,
		"server":   s.serverMap(),
		// a comment mentioning "not_a_key" must not count
		"labels":     s.cfg.Labels,
		"shared_one": s.cfg.SharedOne,
		"embedded":   map[string]interface{}{"embedded_one": s.cfg.EmbeddedOne},
		"wait":       s.cfg.Wait,
	}
	out["ptr"] = ptr
	out["list"] = s.cfg.List
	_ = out
}

func (s *Server) handlePutConfig() {
	switch k := ""; k {
	case "server.tls":
		// validated here, applied in applyConfigPatch
	}
}

func applyConfigPatch(patch map[string]interface{}) {
	for k := range patch {
		if strings.HasPrefix(k, "labels.") {
			continue
		}
		switch k {
		case "hostname", "no_ref":
		case "no_web", "no_get":
		case "untagged", "server.port", "server.tls", "server.limits.max":
		case "ptr.flag", "shared_one", "embedded.embedded_one", "list", "wait":
		}
	}
}
