package memory

// KGUnified wraps a Backend's KG operations for router, server, and MCP interfaces.
// Works with both SQLite (KnowledgeGraph) and PostgreSQL (PGStore KG methods).
type KGUnified struct {
	addTriple   func(s, p, o, validFrom, source string) (int64, error)
	invalidate  func(s, p, o, ended string) error
	queryEntity func(name, asOf string) ([]KGTriple, error)
	timeline    func(name string) ([]KGTriple, error)
	stats       func() KGStats
}

// NewKGUnifiedFromSQLite creates a unified KG adapter from a SQLite KnowledgeGraph.
func NewKGUnifiedFromSQLite(kg *KnowledgeGraph) *KGUnified {
	return &KGUnified{
		addTriple:   kg.AddTriple,
		invalidate:  kg.Invalidate,
		queryEntity: kg.QueryEntity,
		timeline:    kg.Timeline,
		stats:       kg.Stats,
	}
}

// NewKGUnifiedFromPG creates a unified KG adapter from a PGStore.
func NewKGUnifiedFromPG(pg *PGStore) *KGUnified {
	return &KGUnified{
		addTriple:   pg.KGAddTriple,
		invalidate:  pg.KGInvalidate,
		queryEntity: pg.KGQueryEntity,
		timeline:    pg.KGTimeline,
		stats:       pg.KGStats,
	}
}

func (u *KGUnified) AddTriple(s, p, o, validFrom, source string) (int64, error) {
	return u.addTriple(s, p, o, validFrom, source)
}
func (u *KGUnified) Invalidate(s, p, o, ended string) error {
	return u.invalidate(s, p, o, ended)
}
func (u *KGUnified) QueryEntity(name, asOf string) ([]KGTriple, error) {
	return u.queryEntity(name, asOf)
}
func (u *KGUnified) Timeline(name string) ([]KGTriple, error) {
	return u.timeline(name)
}
func (u *KGUnified) Stats() KGStats {
	return u.stats()
}

// ── server.KGAPI + mcp.KGMCP interface implementation ────────────────────────
// These methods use map returns for the HTTP API and MCP tool interfaces.
// Method names match the server.KGAPI and mcp.KGMCP interfaces exactly.

// ServerKGAdapter wraps KGUnified to implement server.KGAPI and mcp.KGMCP.
type ServerKGAdapter struct{ u *KGUnified }

// NewServerKGAdapter creates an adapter for HTTP/MCP KG interfaces.
func NewServerKGAdapter(u *KGUnified) *ServerKGAdapter { return &ServerKGAdapter{u: u} }

func (a *ServerKGAdapter) AddTriple(s, p, o, vf, src string) (int64, error) { return a.u.addTriple(s, p, o, vf, src) }
func (a *ServerKGAdapter) Invalidate(s, p, o, ended string) error { return a.u.invalidate(s, p, o, ended) }
func (a *ServerKGAdapter) QueryEntity(name, asOf string) ([]map[string]interface{}, error) { return a.u.QueryEntityMaps(name, asOf) }
func (a *ServerKGAdapter) Timeline(name string) ([]map[string]interface{}, error) { return a.u.TimelineMaps(name) }
func (a *ServerKGAdapter) Stats() map[string]interface{} { return a.u.StatsMaps() }

// QueryEntityMaps returns triples as maps for MCP/API.
func (u *KGUnified) QueryEntityMaps(name, asOf string) ([]map[string]interface{}, error) {
	triples, err := u.queryEntity(name, asOf)
	if err != nil {
		return nil, err
	}
	return convertTriplesToMaps(triples), nil
}

// TimelineMaps returns triples as maps for MCP/API.
func (u *KGUnified) TimelineMaps(name string) ([]map[string]interface{}, error) {
	triples, err := u.timeline(name)
	if err != nil {
		return nil, err
	}
	return convertTriplesToMaps(triples), nil
}

// StatsMaps returns stats as a map for MCP/API.
func (u *KGUnified) StatsMaps() map[string]interface{} {
	s := u.stats()
	return map[string]interface{}{
		"entity_count":  s.EntityCount,
		"triple_count":  s.TripleCount,
		"active_count":  s.ActiveCount,
		"expired_count": s.ExpiredCount,
	}
}
