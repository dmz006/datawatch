package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/inference"
)

// peerFetchTimeout bounds one peer usage request, so a slow peer is counted
// as unreachable instead of holding up the refresh.
const peerFetchTimeout = 3 * time.Second

// Reasons a peer's spend is not counted, reported in the per-peer breakdown.
const (
	peerReasonNotOptedIn = "not opted in"
	peerReasonNotConfirm = "not confirmed"
	peerReasonNotFound   = "not registered"
	peerReasonUnreach    = "unreachable"
	peerReasonBadReply   = "bad response"
	peerReasonPending    = "not fetched yet"
)

// PeerShareConfirmer reports whether peer's spend on llm may count toward
// llm's budget ceiling. It is the single gate in front of every peer fetch:
// a peer it rejects gets no HTTP call and contributes nothing.
//
// The default, PeerWindowConfirmer, confirms exactly the peers the operator
// listed in the LLM's peer_window. A future resource fingerprint exchange
// plugs in here: it must be able to veto a peer (for example one whose
// resources do not match the budgeted LLM's) even when the operator listed
// it, so a confirmer wrapping the default must return false whenever either
// the operator declaration or the fingerprint check says no.
type PeerShareConfirmer func(peer, llm string) bool

// PeerWindowConfirmer returns the default PeerShareConfirmer: it confirms a
// peer only when peerWindow(llm) lists it. The operator's explicit
// declaration is the only thing it trusts.
func PeerWindowConfirmer(peerWindow func(llm string) []string) PeerShareConfirmer {
	return func(peer, llm string) bool {
		if peerWindow == nil {
			return false
		}
		for _, p := range peerWindow(llm) {
			if p == peer {
				return true
			}
		}
		return false
	}
}

// PeerBudgetStatus is one peer's line in a budget status: what the peer
// spent, whether that spend is counted toward the ceiling, and why not when
// it is not.
type PeerBudgetStatus struct {
	Peer     string  `json:"peer"`
	SpendUSD float64 `json:"spend_usd"`
	Counted  bool    `json:"counted"`
	Reason   string  `json:"reason,omitempty"`
}

// peerSpendEntry is one cached fetch result.
type peerSpendEntry struct {
	spend  float64
	reason string // empty on success
}

// PeerBudget caches the spend of opted-in federation peers per budgeted LLM.
// Refresh does all the network work, from a periodic tick; Spend and
// Breakdown only read the cache, so budget admission never waits on a peer.
// A peer that cannot be reached fails open: it counts as zero and never
// blocks local work. Configure the fields before first use.
type PeerBudget struct {
	// Clock supplies the current time; defaults to time.Now.
	Clock func() time.Time
	// Budgets returns the LLM's current budget, or nil when it has none.
	Budgets func(llm string) *inference.BudgetWindow
	// LLMs lists the LLM names to refresh.
	LLMs func() []string
	// Lookup resolves a peer's URL and bearer token from the multiserver
	// store (GetByName).
	Lookup func(name string) (url, token string, ok bool)
	// KnownPeers optionally lists every registered peer so the breakdown can
	// show the ones that are not opted in. Nil lists only opted-in peers.
	KnownPeers func() []string
	// Confirm gates every peer; nil means PeerWindowConfirmer over each
	// LLM's budget peer_window.
	Confirm PeerShareConfirmer
	// Client performs the fetches; nil means a client with a short timeout.
	Client *http.Client

	mu         sync.Mutex
	cache      map[string]peerSpendEntry // key: llm + "\x00" + peer
	failing    map[string]bool           // keys in a failure episode, so each is logged once
	refreshing bool
}

func peerKey(llm, peer string) string { return llm + "\x00" + peer }

func (p *PeerBudget) now() time.Time {
	if p.Clock == nil {
		return time.Now()
	}
	return p.Clock()
}

func (p *PeerBudget) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: peerFetchTimeout}
}

func (p *PeerBudget) peerWindow(llm string) []string {
	if p.Budgets == nil {
		return nil
	}
	if bw := p.Budgets(llm); bw != nil {
		return bw.PeerWindow
	}
	return nil
}

func (p *PeerBudget) confirm(peer, llm string) bool {
	if p.Confirm != nil {
		return p.Confirm(peer, llm)
	}
	return PeerWindowConfirmer(p.peerWindow)(peer, llm)
}

// Refresh fetches, for every budgeted LLM, the spend of each peer in its
// peer_window that the confirmer accepts, and replaces the cache. Peers are
// fetched concurrently. A concurrent Refresh call returns at once.
func (p *PeerBudget) Refresh(ctx context.Context) {
	p.mu.Lock()
	if p.refreshing {
		p.mu.Unlock()
		return
	}
	p.refreshing = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.refreshing = false
		p.mu.Unlock()
	}()

	type job struct {
		llm, peer  string
		start, end time.Time
	}
	var jobs []job
	if p.LLMs != nil && p.Budgets != nil {
		now := p.now()
		for _, llm := range p.LLMs() {
			bw := p.Budgets(llm)
			if bw == nil {
				continue
			}
			start, end := bw.WindowAt(now)
			for _, peer := range bw.PeerWindow {
				if peer != "" && p.confirm(peer, llm) {
					jobs = append(jobs, job{llm, peer, start, end})
				}
			}
		}
	}

	results := make([]peerSpendEntry, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			results[i] = p.fetch(ctx, j.llm, j.peer, j.start, j.end)
		}(i, j)
	}
	wg.Wait()

	cache := make(map[string]peerSpendEntry, len(jobs))
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failing == nil {
		p.failing = map[string]bool{}
	}
	live := map[string]bool{}
	for i, j := range jobs {
		key := peerKey(j.llm, j.peer)
		live[key] = true
		cache[key] = results[i]
		switch {
		case results[i].reason != "" && !p.failing[key]:
			p.failing[key] = true
			log.Printf("[budget] peer %s spend for llm %s not counted: %s", j.peer, j.llm, results[i].reason)
		case results[i].reason == "":
			delete(p.failing, key)
		}
	}
	for key := range p.failing {
		if !live[key] {
			delete(p.failing, key)
		}
	}
	p.cache = cache
}

// fetch asks one peer for its own local spend in [start, end]. Every failure
// returns a zero-spend entry carrying the reason.
func (p *PeerBudget) fetch(ctx context.Context, llm, peer string, start, end time.Time) peerSpendEntry {
	if p.Lookup == nil {
		return peerSpendEntry{reason: peerReasonNotFound}
	}
	base, token, ok := p.Lookup(peer)
	if !ok || base == "" {
		return peerSpendEntry{reason: peerReasonNotFound}
	}
	q := url.Values{}
	q.Set("llm", llm)
	q.Set("from", start.UTC().Format(time.RFC3339))
	q.Set("to", end.UTC().Format(time.RFC3339))
	endpoint := strings.TrimRight(base, "/") + "/api/budget/usage?" + q.Encode()

	ctx, cancel := context.WithTimeout(ctx, peerFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return peerSpendEntry{reason: peerReasonUnreach}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return peerSpendEntry{reason: peerReasonUnreach}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return peerSpendEntry{reason: fmt.Sprintf("%s: HTTP %d", peerReasonUnreach, resp.StatusCode)}
	}
	var body struct {
		SpendUSD float64 `json:"spend_usd"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&body); err != nil ||
		math.IsNaN(body.SpendUSD) || math.IsInf(body.SpendUSD, 0) || body.SpendUSD < 0 {
		return peerSpendEntry{reason: peerReasonBadReply}
	}
	return peerSpendEntry{spend: body.SpendUSD}
}

// Breakdown lists, for llm, each opted-in peer with its cached spend and
// whether it counts, then (when KnownPeers is set) every other registered
// peer as not opted in. It reads only the cache and the confirmer.
func (p *PeerBudget) Breakdown(llm string) []PeerBudgetStatus {
	window := p.peerWindow(llm)
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []PeerBudgetStatus
	listed := map[string]bool{}
	for _, peer := range window {
		if peer == "" || listed[peer] {
			continue
		}
		listed[peer] = true
		st := PeerBudgetStatus{Peer: peer}
		switch e, ok := p.cache[peerKey(llm, peer)]; {
		case !p.confirm(peer, llm):
			st.Reason = peerReasonNotConfirm
		case !ok:
			st.Reason = peerReasonPending
		case e.reason != "":
			st.Reason = e.reason
		default:
			st.SpendUSD, st.Counted = e.spend, true
		}
		out = append(out, st)
	}
	if p.KnownPeers != nil {
		for _, peer := range p.KnownPeers() {
			if peer != "" && !listed[peer] {
				listed[peer] = true
				out = append(out, PeerBudgetStatus{Peer: peer, Reason: peerReasonNotOptedIn})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Peer < out[j].Peer })
	return out
}

// Spend sums the cached spend of llm's counted peers. It is the ledger's
// PeerSpend source.
func (p *PeerBudget) Spend(llm string) float64 {
	var total float64
	for _, st := range p.Breakdown(llm) {
		if st.Counted {
			total += st.SpendUSD
		}
	}
	return total
}
