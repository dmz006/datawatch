package memory

// KGMCPAdapter adapts KnowledgeGraph for the MCP server's KGMCP interface.
type KGMCPAdapter struct {
	kg *KnowledgeGraph
}

// NewKGMCPAdapter creates an adapter for MCP KG tools.
func NewKGMCPAdapter(kg *KnowledgeGraph) *KGMCPAdapter {
	return &KGMCPAdapter{kg: kg}
}

func (a *KGMCPAdapter) AddTriple(s, p, o, validFrom, source string) (int64, error) {
	return a.kg.AddTriple(s, p, o, validFrom, source)
}

func (a *KGMCPAdapter) Invalidate(s, p, o, ended string) error {
	return a.kg.Invalidate(s, p, o, ended)
}

func (a *KGMCPAdapter) QueryEntity(name, asOf string) ([]map[string]interface{}, error) {
	triples, err := a.kg.QueryEntity(name, asOf)
	if err != nil {
		return nil, err
	}
	return convertTriplesToMaps(triples), nil
}

func (a *KGMCPAdapter) Timeline(name string) ([]map[string]interface{}, error) {
	triples, err := a.kg.Timeline(name)
	if err != nil {
		return nil, err
	}
	return convertTriplesToMaps(triples), nil
}

func (a *KGMCPAdapter) Stats() map[string]interface{} {
	s := a.kg.Stats()
	return map[string]interface{}{
		"entity_count":  s.EntityCount,
		"triple_count":  s.TripleCount,
		"active_count":  s.ActiveCount,
		"expired_count": s.ExpiredCount,
	}
}

func convertTriplesToMaps(triples []KGTriple) []map[string]interface{} {
	result := make([]map[string]interface{}, len(triples))
	for i, t := range triples {
		result[i] = map[string]interface{}{
			"id":         t.ID,
			"subject":    t.Subject,
			"predicate":  t.Predicate,
			"object":     t.Object,
			"valid_from": t.ValidFrom,
			"valid_to":   t.ValidTo,
			"source":     t.Source,
			"created_at": t.CreatedAt,
		}
	}
	return result
}
