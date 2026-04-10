package memory

import "github.com/dmz006/datawatch/internal/router"

// KGRouterAdapter adapts KnowledgeGraph for the router interface.
type KGRouterAdapter struct {
	kg *KnowledgeGraph
}

// NewKGRouterAdapter creates an adapter.
func NewKGRouterAdapter(kg *KnowledgeGraph) *KGRouterAdapter {
	return &KGRouterAdapter{kg: kg}
}

func (a *KGRouterAdapter) AddTriple(s, p, o, validFrom, source string) (int64, error) {
	return a.kg.AddTriple(s, p, o, validFrom, source)
}

func (a *KGRouterAdapter) Invalidate(s, p, o, ended string) error {
	return a.kg.Invalidate(s, p, o, ended)
}

func (a *KGRouterAdapter) QueryEntity(name, asOf string) ([]router.KGTriple, error) {
	triples, err := a.kg.QueryEntity(name, asOf)
	if err != nil {
		return nil, err
	}
	return convertTriples(triples), nil
}

func (a *KGRouterAdapter) Timeline(name string) ([]router.KGTriple, error) {
	triples, err := a.kg.Timeline(name)
	if err != nil {
		return nil, err
	}
	return convertTriples(triples), nil
}

func (a *KGRouterAdapter) Stats() router.KGStats {
	s := a.kg.Stats()
	return router.KGStats{
		EntityCount:  s.EntityCount,
		TripleCount:  s.TripleCount,
		ActiveCount:  s.ActiveCount,
		ExpiredCount: s.ExpiredCount,
	}
}

func convertTriples(triples []KGTriple) []router.KGTriple {
	result := make([]router.KGTriple, len(triples))
	for i, t := range triples {
		result[i] = router.KGTriple{
			ID: t.ID, Subject: t.Subject, Predicate: t.Predicate,
			Object: t.Object, ValidFrom: t.ValidFrom, ValidTo: t.ValidTo,
		}
	}
	return result
}
