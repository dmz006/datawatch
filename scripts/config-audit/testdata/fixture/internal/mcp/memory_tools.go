package mcp

func (s *Server) handleConfigSet(_ context.Context, req Request) (*Result, error) {
	// /api/config accepts PUT (not POST).
	r, err := http.NewRequest(http.MethodPut, s.base+"/api/config", body)
	_ = r
	return nil, err
}
