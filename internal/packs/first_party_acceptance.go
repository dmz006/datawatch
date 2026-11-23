package packs

import (
	"fmt"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/skills"
)

// FirstPartySource tells AcceptFirstParty where a first-party source's packs
// are and where its trust records go. The daemon registers one with
// RegisterFirstPartySource before the acceptance is requested.
type FirstPartySource struct {
	// InstallRoot is the install root that holds trust.json.
	InstallRoot string
	// CurrentPacks returns the packs currently published by the named source.
	// Each pack must carry Source equal to that name and a Digest.
	CurrentPacks func(sourceName string) ([]*Pack, error)
}

var (
	firstPartyMu     sync.RWMutex
	firstPartySource FirstPartySource
)

// RegisterFirstPartySource sets the source that AcceptFirstParty reads. A later
// call replaces an earlier one. The zero value unregisters the source.
func RegisterFirstPartySource(src FirstPartySource) {
	firstPartyMu.Lock()
	defer firstPartyMu.Unlock()
	firstPartySource = src
}

// AcceptFirstParty records acceptedBy's acceptance of every current pack of
// the first-party source sourceName, at time now, and returns the records it
// wrote. The records bind to each pack's exact name, version and digest.
//
// It refuses a source that is not first party. It also refuses a source with no
// current packs, and a pack whose Source is not sourceName. Nothing is written
// when any of these checks fails.
//
// Acceptance covers only the versions present now. A later version has no
// record, so it reaches the operator through the version diff rules
// (DiffVersions and AutoAcceptUpdate), not through this call.
func AcceptFirstParty(sourceName string, acceptedBy string, now time.Time) ([]TrustRecord, error) {
	if !skills.IsFirstPartyPackSource(sourceName) {
		return nil, fmt.Errorf("pack trust: %q is not a first-party pack source", sourceName)
	}
	firstPartyMu.RLock()
	src := firstPartySource
	firstPartyMu.RUnlock()
	if src.CurrentPacks == nil || src.InstallRoot == "" {
		return nil, fmt.Errorf("pack trust: no first-party pack source is registered")
	}
	packs, err := src.CurrentPacks(sourceName)
	if err != nil {
		return nil, fmt.Errorf("pack trust: list packs of %q: %w", sourceName, err)
	}
	if len(packs) == 0 {
		return nil, fmt.Errorf("pack trust: first-party source %q has no current packs to accept", sourceName)
	}
	for _, p := range packs {
		if p == nil {
			return nil, fmt.Errorf("pack trust: first-party source %q listed a nil pack", sourceName)
		}
		if p.Source != sourceName {
			return nil, fmt.Errorf("pack trust: pack %s %s comes from %q, not %q", p.Name, p.Version, p.Source, sourceName)
		}
	}
	store, err := LoadTrustStore(src.InstallRoot)
	if err != nil {
		return nil, err
	}
	return store.acceptAll(packs, acceptedBy, now)
}
