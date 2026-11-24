package server

func (s *Server) serverMap() map[string]interface{} {
	return map[string]interface{}{
		"port": s.cfg.Server.Port,
		"tls":  s.cfg.Server.TLS,
		"limits": map[string]interface{}{
			"max": s.cfg.Server.Limits.Max,
		},
	}
}
