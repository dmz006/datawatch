package memory

import "github.com/dmz006/datawatch/internal/router"

// PGKGAdapter adapts PGStore KG methods for the router interface.
type PGKGAdapter struct {
	store *PGStore
}

// NewPGKGAdapter creates an adapter.
func NewPGKGAdapter(store *PGStore) *PGKGAdapter {
	return &PGKGAdapter{store: store}
}

func (a *PGKGAdapter) AddTriple(s, p, o, validFrom, source string) (int64, error) {
	return a.store.KGAddTriple(s, p, o, validFrom, source)
}

func (a *PGKGAdapter) Invalidate(s, p, o, ended string) error {
	return a.store.KGInvalidate(s, p, o, ended)
}

func (a *PGKGAdapter) QueryEntity(name, asOf string) ([]router.KGTriple, error) {
	triples, err := a.store.KGQueryEntity(name, asOf)
	if err != nil {
		return nil, err
	}
	return convertTriples(triples), nil
}

func (a *PGKGAdapter) Timeline(name string) ([]router.KGTriple, error) {
	triples, err := a.store.KGTimeline(name)
	if err != nil {
		return nil, err
	}
	return convertTriples(triples), nil
}

func (a *PGKGAdapter) Stats() router.KGStats {
	s := a.store.KGStats()
	return router.KGStats{
		EntityCount:  s.EntityCount,
		TripleCount:  s.TripleCount,
		ActiveCount:  s.ActiveCount,
		ExpiredCount: s.ExpiredCount,
	}
}
