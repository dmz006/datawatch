package autonomous

import (
	"context"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// peerSpendTTL is how long a SpendForPeers result, success or failure, is
// reused, so evaluating a budget on every advance does not call the peers
// each time and an unreachable peer costs its timeout once per interval.
const peerSpendTTL = 30 * time.Second

// PeerOptInNote is the note on every opted-in peer line of an Automation
// budget status.
const PeerOptInNote = "counted because the operator opted this peer in; there is no automatic confirmation that the peer shares the account, because the resource fingerprint exchange does not exist yet"

type peerAdhocEntry struct {
	peerSpendEntry
	at time.Time
}

// SpendForPeers sums llm's spend on the named peers over [from, to]. Only the
// named peers are queried: the caller's naming them is the operator's opt-in.
// A Confirm set on the PeerBudget may still veto a peer. A peer that cannot be
// reached counts as zero, is logged once per failure episode, and never blocks
// local work. Results are reused for peerSpendTTL.
//
// lines has one entry per named peer, each carrying PeerOptInNote, followed by
// every other registered peer as not counted; those are never queried.
func (p *PeerBudget) SpendForPeers(ctx context.Context, llm string, peers []string, from, to time.Time) (usd float64, lines []PeerBudgetStatus) {
	named := map[string]bool{}
	var order []string
	for _, peer := range peers {
		if peer = strings.TrimSpace(peer); peer != "" && !named[peer] {
			named[peer] = true
			order = append(order, peer)
		}
	}
	now := p.now()

	results := make([]peerSpendEntry, len(order))
	var misses []int
	p.mu.Lock()
	for i, peer := range order {
		if e, ok := p.adhoc[peerWindowKey(llm, peer, from)]; ok && now.Sub(e.at) < peerSpendTTL && !e.at.After(now) {
			results[i] = e.peerSpendEntry
		} else {
			misses = append(misses, i)
		}
	}
	p.mu.Unlock()

	var wg sync.WaitGroup
	for _, i := range misses {
		if p.Confirm != nil && !p.Confirm(order[i], llm) {
			results[i] = peerSpendEntry{reason: peerReasonNotConfirm}
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = p.fetch(ctx, llm, order[i], from, to)
		}(i)
	}
	wg.Wait()

	p.mu.Lock()
	if p.adhoc == nil {
		p.adhoc = map[string]peerAdhocEntry{}
	}
	if p.adhocFail == nil {
		p.adhocFail = map[string]bool{}
	}
	for _, i := range misses {
		key := peerKey(llm, order[i])
		p.adhoc[peerWindowKey(llm, order[i], from)] = peerAdhocEntry{peerSpendEntry: results[i], at: now}
		switch {
		case results[i].reason != "" && !p.adhocFail[key]:
			p.adhocFail[key] = true
			log.Printf("[budget] peer %s spend for llm %s not counted: %s", order[i], llm, results[i].reason)
		case results[i].reason == "":
			delete(p.adhocFail, key)
		}
	}
	p.mu.Unlock()

	for i, peer := range order {
		st := PeerBudgetStatus{Peer: peer, Note: PeerOptInNote}
		if results[i].reason != "" {
			st.Reason = results[i].reason
		} else {
			st.SpendUSD, st.Counted = results[i].spend, true
			usd += results[i].spend
		}
		lines = append(lines, st)
	}
	if p.KnownPeers != nil {
		for _, peer := range p.KnownPeers() {
			if peer != "" && !named[peer] {
				named[peer] = true
				lines = append(lines, PeerBudgetStatus{Peer: peer, Reason: peerReasonNotOptedIn})
			}
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Peer < lines[j].Peer })
	return usd, lines
}

func peerWindowKey(llm, peer string, from time.Time) string {
	return peerKey(llm, peer) + "\x00" + from.UTC().Format(time.RFC3339)
}
