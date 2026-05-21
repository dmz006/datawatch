// internal/server/api_dashboard_cards.go — BL303 dashboard card CRUD (#57/#58).
//
// Routes (all under /api/dashboard/cards):
//
//	GET    /api/dashboard/cards        — list cards array
//	POST   /api/dashboard/cards        — append a card
//	GET    /api/dashboard/cards/{id}   — get one card by type-id
//	PUT    /api/dashboard/cards/{id}   — update cs/rs for one card
//	DELETE /api/dashboard/cards/{id}   — remove a card

package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
)

// dashCard is a single card in the layout.
type dashCard struct {
	ID     string `json:"id"`
	CS     int    `json:"cs"`
	RS     int    `json:"rs,omitempty"`
	System bool   `json:"system,omitempty"`
}

// dashLayout is the full layout file.
type dashLayout struct {
	Cards []dashCard `json:"cards"`
}

// defaultDashCards returns the factory-default layout shipped with every
// instance. These cards carry System=true and cannot be deleted.
// Mirrors DASH_DEFAULT_LAYOUT in app.js.
func defaultDashCards() []dashCard {
	return []dashCard{
		{ID: "tree", CS: 2, RS: 2, System: true},
		{ID: "orbital", CS: 6, RS: 2, System: true},
		{ID: "events", CS: 2, RS: 2, System: true},
		{ID: "sparklines", CS: 2, RS: 1, System: true},
		{ID: "gantt", CS: 12, RS: 1, System: true},
		{ID: "heatmap", CS: 3, RS: 1, System: true},
		{ID: "guardrails", CS: 3, RS: 1, System: true},
		{ID: "ekg", CS: 6, RS: 2, System: true},
		{ID: "smoke", CS: 6, RS: 2, System: true},
	}
}

// mergeSystemCards ensures all default system cards are present, re-adding
// any that were removed (inserted at the front in their original order).
// Also stamps System=true on any existing card whose ID is a system card ID.
func mergeSystemCards(cards []dashCard) []dashCard {
	defaults := defaultDashCards()
	systemIDs := make(map[string]bool, len(defaults))
	for _, d := range defaults {
		systemIDs[d.ID] = true
	}
	present := make(map[string]bool, len(cards))
	for i := range cards {
		present[cards[i].ID] = true
		if systemIDs[cards[i].ID] {
			cards[i].System = true
		}
	}
	var missing []dashCard
	for _, d := range defaults {
		if !present[d.ID] {
			missing = append(missing, d)
		}
	}
	if len(missing) > 0 {
		cards = append(missing, cards...)
	}
	return cards
}

func (s *Server) readDashLayout() (dashLayout, error) {
	var layout dashLayout
	data, err := os.ReadFile(s.dashLayoutPath())
	if err != nil {
		if os.IsNotExist(err) {
			layout.Cards = defaultDashCards()
			_ = s.writeDashLayout(layout) // seed on first run
			return layout, nil
		}
		return layout, err
	}
	_ = json.Unmarshal(data, &layout)
	// Re-merge system cards so they survive a PUT wipe or manual edit.
	merged := mergeSystemCards(layout.Cards)
	if len(merged) != len(layout.Cards) {
		layout.Cards = merged
		_ = s.writeDashLayout(layout) // persist restored cards
	} else {
		layout.Cards = merged
	}
	return layout, nil
}

func (s *Server) writeDashLayout(layout dashLayout) error {
	path := s.dashLayoutPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(layout, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

// handleDashboardCards routes /api/dashboard/cards and /api/dashboard/cards/{id}.
func (s *Server) handleDashboardCards(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/dashboard/cards")
	rest = strings.TrimPrefix(rest, "/")

	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			s.dashCardsListHandler(w, r)
		case http.MethodPost:
			s.dashCardsAddHandler(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	cardID := rest
	switch r.Method {
	case http.MethodGet:
		s.dashCardGetHandler(w, r, cardID)
	case http.MethodPut:
		s.dashCardUpdateHandler(w, r, cardID)
	case http.MethodDelete:
		s.dashCardDeleteHandler(w, r, cardID)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) dashCardsListHandler(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapDashboardRead) {
		return
	}
	layout, err := s.readDashLayout()
	if err != nil {
		http.Error(w, "read layout: "+err.Error(), http.StatusInternalServerError)
		return
	}
	cards := layout.Cards
	if cards == nil {
		cards = []dashCard{}
	}
	writeJSONOK(w, cards)
}

func (s *Server) dashCardsAddHandler(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapDashboardWrite) {
		return
	}
	var card dashCard
	if err := json.NewDecoder(r.Body).Decode(&card); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if card.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if card.CS == 0 {
		card.CS = 6 // default half-width
	}
	layout, err := s.readDashLayout()
	if err != nil {
		http.Error(w, "read layout: "+err.Error(), http.StatusInternalServerError)
		return
	}
	layout.Cards = append(layout.Cards, card)
	if err := s.writeDashLayout(layout); err != nil {
		http.Error(w, "write layout: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, card)
}

func (s *Server) dashCardGetHandler(w http.ResponseWriter, r *http.Request, id string) {
	if !s.fedCap(w, r, federation.CapDashboardRead) {
		return
	}
	layout, err := s.readDashLayout()
	if err != nil {
		http.Error(w, "read layout: "+err.Error(), http.StatusInternalServerError)
		return
	}
	for _, c := range layout.Cards {
		if c.ID == id {
			writeJSONOK(w, c)
			return
		}
	}
	http.Error(w, "card not found", http.StatusNotFound)
}

func (s *Server) dashCardUpdateHandler(w http.ResponseWriter, r *http.Request, id string) {
	if !s.fedCap(w, r, federation.CapDashboardWrite) {
		return
	}
	var req dashCard
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	layout, err := s.readDashLayout()
	if err != nil {
		http.Error(w, "read layout: "+err.Error(), http.StatusInternalServerError)
		return
	}
	found := false
	for i, c := range layout.Cards {
		if c.ID == id {
			if req.CS != 0 {
				layout.Cards[i].CS = req.CS
			}
			if req.RS != 0 {
				layout.Cards[i].RS = req.RS
			}
			found = true
			break
		}
	}
	if !found {
		// Upsert: append new card so `set` works for add-or-update.
		card := dashCard{ID: id, CS: req.CS, RS: req.RS}
		if card.CS == 0 {
			card.CS = 6
		}
		layout.Cards = append(layout.Cards, card)
	}
	if err := s.writeDashLayout(layout); err != nil {
		http.Error(w, "write layout: "+err.Error(), http.StatusInternalServerError)
		return
	}
	for _, c := range layout.Cards {
		if c.ID == id {
			writeJSONOK(w, c)
			return
		}
	}
}

func (s *Server) dashCardDeleteHandler(w http.ResponseWriter, r *http.Request, id string) {
	if !s.fedCap(w, r, federation.CapDashboardWrite) {
		return
	}
	layout, err := s.readDashLayout()
	if err != nil {
		http.Error(w, "read layout: "+err.Error(), http.StatusInternalServerError)
		return
	}
	newCards := layout.Cards[:0]
	found := false
	for _, c := range layout.Cards {
		if c.ID == id && !found {
			if c.System {
				http.Error(w, "cannot delete system card", http.StatusConflict)
				return
			}
			found = true
			continue
		}
		newCards = append(newCards, c)
	}
	if !found {
		http.Error(w, "card not found", http.StatusNotFound)
		return
	}
	layout.Cards = newCards
	if err := s.writeDashLayout(layout); err != nil {
		http.Error(w, "write layout: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{"status": "deleted", "id": id})
}
