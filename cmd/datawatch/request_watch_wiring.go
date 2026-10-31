package main

import (
	"context"
	"strings"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	observerpkg "github.com/dmz006/datawatch/internal/observer"
	"github.com/dmz006/datawatch/internal/requestwatch"
	"github.com/dmz006/datawatch/internal/session"
)

// requestWatcher is the running request watch, nil until the daemon starts it.
// It is the requestwatch.Reader the REST server, the MCP tool and the other
// surfaces read.
var requestWatcher *requestwatch.Watcher

// requestWatchLocalStats returns the local daemon's own observer snapshot, or
// nil when the local observer is not running. main wires it once the local
// observer exists.
var requestWatchLocalStats func() *observerpkg.StatsResponse

// requestWatchLister builds the watcher's target list from the session
// manager, the Automata manager and the compute registry. The three reads are
// functions so tests can supply them without a running daemon.
type requestWatchLister struct {
	// sessions lists the daemon's sessions.
	sessions func() []*session.Session
	// prds lists the Automata with their stories and tasks.
	prds func() []*autonomouspkg.PRD
	// node resolves a compute node by name; ok is false for a name that is
	// not registered.
	node func(name string) (*compute.Node, bool)
}

// newRequestWatchLister wires the lister to the live managers. Any of them may
// be nil, which leaves the lister with nothing to observe.
func newRequestWatchLister(mgr *session.Manager, amgr *autonomouspkg.Manager, reg *compute.Registry) *requestWatchLister {
	l := &requestWatchLister{}
	if mgr != nil {
		l.sessions = mgr.ListSessions
	}
	if amgr != nil && amgr.Store() != nil {
		l.prds = amgr.Store().ListPRDs
	}
	if reg != nil {
		l.node = func(name string) (*compute.Node, bool) {
			n, err := reg.Get(name)
			if err != nil || n == nil {
				return nil, false
			}
			return n, true
		}
	}
	return l
}

// requestWatchTaskRef is the Automaton task a session works for.
type requestWatchTaskRef struct {
	automatonID string
	taskID      string
}

// Targets lists the sessions to observe. A session is a target only when it is
// running or waiting for input, carries a compute node reference that resolves
// in the compute registry and belongs to an Automaton task: it is the session
// of a task that is in progress (a worker), or a verifier session named for
// its task. Every other session is left alone.
func (l *requestWatchLister) Targets(ctx context.Context) []requestwatch.Target {
	if l == nil || l.sessions == nil || l.prds == nil || l.node == nil {
		return nil
	}

	workers := map[string]requestWatchTaskRef{}
	verifiable := map[string]requestWatchTaskRef{}
	for _, prd := range l.prds() {
		if prd == nil {
			continue
		}
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				ref := requestWatchTaskRef{automatonID: prd.ID, taskID: t.ID}
				verifiable[t.ID] = ref
				if t.Status == autonomouspkg.TaskInProgress && t.SessionID != "" {
					workers[t.SessionID] = ref
				}
			}
		}
	}

	var out []requestwatch.Target
	for _, s := range l.sessions() {
		if ctx.Err() != nil {
			return out
		}
		if s == nil || (s.State != session.StateRunning && s.State != session.StateWaitingInput) {
			continue
		}
		if s.ComputeNodeRef == "" {
			continue
		}
		node, ok := l.node(s.ComputeNodeRef)
		if !ok {
			continue
		}

		role, ref, found := "", requestWatchTaskRef{}, false
		if taskID, isVerifier := verifierTaskFromSessionName(s.Name); isVerifier {
			if ref, found = verifiable[taskID]; found {
				role = requestwatch.RoleVerifier
			}
		} else {
			for _, id := range []string{s.FullID, s.ID} {
				if id == "" {
					continue
				}
				if ref, found = workers[id]; found {
					role = requestwatch.RoleWorker
					break
				}
			}
		}
		if !found {
			continue
		}

		out = append(out, requestwatch.Target{
			SessionID:    s.FullID,
			AutomatonID:  ref.automatonID,
			TaskID:       ref.taskID,
			Role:         role,
			NodeName:     node.Name,
			NodeAddress:  node.Address,
			NodeKind:     string(node.Kind),
			ObserverPeer: node.ObserverPeer,
			Model:        s.Model,
		})
	}
	return out
}

// requestWatchGPU reads a node's GPU utilisation from its observer data. It
// follows the path the compute node detail route uses: the node's attached
// observer peer (or, with none attached, the peer named like the node), whose
// last pushed snapshot carries the GPU list; the local daemon's own observer
// answers for the peer named like the daemon.
type requestWatchGPU struct {
	// enabled reports whether the GPU signal is on right now.
	enabled func() bool
	// peerPayload returns the last snapshot a peer pushed, or nil.
	peerPayload func(peer string) *observerpkg.StatsResponse
	// localName is the daemon's own peer name.
	localName string
	// local returns the daemon's own snapshot, or nil.
	local func() *observerpkg.StatsResponse
}

// Read returns the busiest GPU's utilisation percent. ok is false when the
// signal is off, the node has no observer data or the data has no GPU figure.
func (g requestWatchGPU) Read(_ context.Context, node requestwatch.NodeRef) (float64, bool) {
	if g.enabled != nil && !g.enabled() {
		return 0, false
	}
	peer := node.ObserverPeer
	if peer == "" {
		peer = node.Name
	}
	var snap *observerpkg.StatsResponse
	if g.peerPayload != nil {
		snap = g.peerPayload(peer)
	}
	if snap == nil && g.local != nil && g.localName != "" && peer == g.localName {
		snap = g.local()
	}
	if snap == nil || len(snap.GPU) == 0 {
		return 0, false
	}
	best := snap.GPU[0].UtilPct
	for _, gpu := range snap.GPU[1:] {
		if gpu.UtilPct > best {
			best = gpu.UtilPct
		}
	}
	return best, true
}

// requestWatchSources returns the selector the watcher uses to pick a Source
// by node kind. Today only the Ollama kind has one; a target of any other kind
// is skipped. A kind is added here and nowhere else.
func requestWatchSources(gpu func(ctx context.Context, node requestwatch.NodeRef) (float64, bool)) requestwatch.SourceFor {
	ollama := &requestwatch.OllamaSource{GPU: gpu}
	return func(kind string) requestwatch.Source {
		if strings.EqualFold(kind, string(compute.KindOllama)) {
			return ollama
		}
		return nil
	}
}

// startRequestWatch builds the watcher and starts its poll loop. cfg is read on
// every tick, so a change to autonomous.request_watch applies with no restart.
// peers returns the observer peer registry at call time (it is wired after the
// watcher starts); it may be nil.
func startRequestWatch(
	ctx context.Context,
	cfg func() config.RequestWatchConfig,
	lister requestwatch.TargetLister,
	hostname string,
	peers func(peer string) *observerpkg.StatsResponse,
) *requestwatch.Watcher {
	gpu := requestWatchGPU{
		enabled:     func() bool { return cfg().EffectiveUseGPUSignal() },
		peerPayload: peers,
		localName:   hostname,
		local: func() *observerpkg.StatsResponse {
			if requestWatchLocalStats == nil {
				return nil
			}
			return requestWatchLocalStats()
		},
	}
	w := requestwatch.NewWatcher(lister, requestWatchSources(gpu.Read), cfg, nil, requestwatch.Callbacks{})
	w.Start(ctx)
	return w
}
