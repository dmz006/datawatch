// BL117 — JSON-backed Graph persistence.
//
// One JSON-lines file per kind under <data_dir>/orchestrator/. Same
// pattern as internal/autonomous/store.go; small dataset assumption.

package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu     sync.Mutex
	dir    string
	graphs map[string]*Graph
	// issued is every id this store has handed out, so a node id is not drawn
	// twice before its graph is saved.
	issued map[string]bool
}

func NewStore(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, "orchestrator")
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", root, err)
	}
	s := &Store{dir: root, graphs: map[string]*Graph{}, issued: map[string]bool{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// CreateGraph persists a draft graph with the supplied PRD IDs. The
// caller is responsible for appending Node entries (via SaveGraph).
func (s *Store) CreateGraph(title, projectDir string, prdIDs []string) (*Graph, error) {
	if len(prdIDs) == 0 {
		return nil, fmt.Errorf("prd_ids required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g := &Graph{
		ID:         s.unusedIDLocked(),
		Title:      title,
		ProjectDir: projectDir,
		PRDIDs:     append([]string{}, prdIDs...),
		Status:     GraphDraft,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	s.graphs[g.ID] = g
	if err := s.persist(); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Store) SaveGraph(g *Graph) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g.UpdatedAt = time.Now()
	s.graphs[g.ID] = g
	return s.persist()
}

func (s *Store) GetGraph(id string) (*Graph, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.graphs[id]
	return g, ok
}

// ListGraphs returns all graphs newest-first.
func (s *Store) ListGraphs() []*Graph {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Graph, 0, len(s.graphs))
	for _, g := range s.graphs {
		out = append(out, g)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// ListVerdicts flattens every guardrail Verdict across all graphs.
func (s *Store) ListVerdicts() []Verdict {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Verdict
	for _, g := range s.graphs {
		for _, n := range g.Nodes {
			if n.Verdict != nil {
				out = append(out, *n.Verdict)
			}
		}
	}
	return out
}

func (s *Store) load() error {
	data, err := os.ReadFile(filepath.Join(s.dir, "graphs.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var g Graph
		if err := json.Unmarshal([]byte(line), &g); err != nil {
			return err
		}
		s.graphs[g.ID] = &g
	}
	return nil
}

func (s *Store) persist() error {
	path := filepath.Join(s.dir, "graphs.jsonl")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for _, g := range s.graphs {
		if err := enc.Encode(g); err != nil {
			return err
		}
	}
	return nil
}

// idClock is the clock newID reads; tests replace it to force a repeat.
var idClock = time.Now

// newID is the low 32 bits of the nanosecond clock, so it repeats every
// 2^32 ns (about 4.29 s): draw ids through unusedIDLocked.
func newID() string {
	t := idClock().UnixNano()
	return fmt.Sprintf("%08x", uint32(t))
}

// unusedIDLocked returns an id that no graph or node holds and that this store
// has not issued, counting up from the clock draw. Callers hold s.mu.
func (s *Store) unusedIDLocked() string {
	taken := func(id string) bool {
		if s.issued[id] || s.graphs[id] != nil {
			return true
		}
		for _, g := range s.graphs {
			for _, n := range g.Nodes {
				if n.ID == id {
					return true
				}
			}
		}
		return false
	}
	id := newID()
	for taken(id) {
		v, _ := strconv.ParseUint(id, 16, 32)
		id = fmt.Sprintf("%08x", uint32(v)+1)
	}
	s.issued[id] = true
	return id
}

func (s *Store) unusedID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unusedIDLocked()
}
