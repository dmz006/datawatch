// BL171 (S9) — envelope classifier. Takes the flat process list
// from the /proc walker and groups it into per-session and
// per-backend envelopes with rolled-up CPU / RSS / FD counters.
//
// Classification priority (first-match, process left-to-right):
//
//   1. session:<full_id>    — PID is in the closure of a known
//                              tmux-pane PID registered via
//                              RegisterSessionRoot
//   2. backend:<name>       — process comm / cmdline[0] basename
//                              matches one of the configured
//                              backend signatures AND has no
//                              datawatch/tmux ancestor (so we don't
//                              double-count Claude inside a session)
//   3. container:<short_id> — process has a non-empty cgroup
//                              container ID
//   4. system               — the rest
//
// The collector consults a live sessionRootMap to attribute sub-
// process trees under each tmux pane PID. Without that map
// session attribution silently falls through to backend/system.

package observer

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SessionRoot is one entry in the session-attribution map: the
// tmux-pane PID that roots a session's process subtree.
type SessionRoot struct {
	FullID string
	Label  string
	RootPID int
}

type rootMap struct {
	mu   sync.Mutex
	data map[string]SessionRoot // full_id -> root
}

var gRoots = &rootMap{data: map[string]SessionRoot{}}

// RegisterSessionRoot is called by session.Manager when a session
// starts so the observer knows which PID to root its envelope at.
// Safe to call multiple times with the same full_id — last write
// wins (re-attach after restart).
func RegisterSessionRoot(fullID, label string, rootPID int) {
	gRoots.mu.Lock()
	defer gRoots.mu.Unlock()
	gRoots.data[fullID] = SessionRoot{FullID: fullID, Label: label, RootPID: rootPID}
}

// UnregisterSessionRoot is called on session end.
func UnregisterSessionRoot(fullID string) {
	gRoots.mu.Lock()
	defer gRoots.mu.Unlock()
	delete(gRoots.data, fullID)
}

// sessionRootsSnapshot returns a defensive copy.
func sessionRootsSnapshot() []SessionRoot {
	gRoots.mu.Lock()
	defer gRoots.mu.Unlock()
	out := make([]SessionRoot, 0, len(gRoots.data))
	for _, v := range gRoots.data {
		out = append(out, v)
	}
	return out
}

// classify walks `recs` once and returns the envelope list. It also
// returns a map pid→envelopeID so the collector can annotate the
// process-tree view.
func classify(recs []ProcRecord, cfg EnvelopesCfg) ([]Envelope, map[int]string) {
	if len(recs) == 0 {
		return nil, nil
	}
	// Index by PID and build parent→children map.
	byPID := make(map[int]*ProcRecord, len(recs))
	children := make(map[int][]int, len(recs))
	for i := range recs {
		r := &recs[i]
		byPID[r.PID] = r
		children[r.PPID] = append(children[r.PPID], r.PID)
	}

	assignment := make(map[int]string, len(recs))

	// Pass 1 — session attribution via the live roots map.
	if cfg.SessionAttribution {
		for _, sr := range sessionRootsSnapshot() {
			if _, ok := byPID[sr.RootPID]; !ok {
				continue
			}
			label := sr.Label
			if label == "" {
				label = sr.FullID
			}
			envID := "session:" + sr.FullID
			walkSubtree(sr.RootPID, children, func(pid int) {
				if _, claimed := assignment[pid]; !claimed {
					assignment[pid] = envID
				}
			})
		}
	}

	// Pass 2 — backend attribution.
	if cfg.BackendAttribution {
		for name, sig := range cfg.BackendSignatures {
			if !sig.Track {
				continue
			}
			// Find the shallowest matching process; everything under it
			// joins that backend's envelope.
			var roots []int
			for pid, r := range byPID {
				if _, claimed := assignment[pid]; claimed {
					continue
				}
				if matchesBackend(r, sig) {
					// Skip if any ancestor already matched the same backend
					// (we only want the shallowest root per instance).
					anc := r.PPID
					shallowest := true
					for anc > 1 {
						parent, ok := byPID[anc]
						if !ok {
							break
						}
						if matchesBackend(parent, sig) {
							shallowest = false
							break
						}
						anc = parent.PPID
					}
					if shallowest {
						roots = append(roots, pid)
					}
				}
			}
			for _, root := range roots {
				suffix := ""
				if byPID[root].ContainerID != "" {
					suffix = "-docker"
				}
				envID := "backend:" + name + suffix
				walkSubtree(root, children, func(pid int) {
					if _, claimed := assignment[pid]; !claimed {
						assignment[pid] = envID
					}
				})
			}
		}
	}

	// Pass 3 — container attribution for anything unclaimed.
	if cfg.DockerDiscovery {
		for pid, r := range byPID {
			if _, claimed := assignment[pid]; claimed {
				continue
			}
			if r.ContainerID != "" {
				short := r.ContainerID
				if len(short) > 12 {
					short = short[:12]
				}
				assignment[pid] = "container:" + short
			}
		}
	}

	// Pass 4 — system for the rest.
	for pid := range byPID {
		if _, claimed := assignment[pid]; !claimed {
			assignment[pid] = "system"
		}
	}

	// Roll up per envelope.
	type rollup struct {
		env     Envelope
		firstPID int
	}
	grouped := map[string]*rollup{}
	for _, r := range recs {
		id := assignment[r.PID]
		g, ok := grouped[id]
		if !ok {
			g = &rollup{env: Envelope{ID: id}, firstPID: r.PID}
			// Parse kind + label from id.
			switch {
			case strings.HasPrefix(id, "session:"):
				g.env.Kind = EnvelopeSession
				fullID := strings.TrimPrefix(id, "session:")
				g.env.Label = lookupSessionLabel(fullID)
				g.env.RootPID = lookupSessionRootPID(fullID)
			case strings.HasPrefix(id, "backend:"):
				g.env.Kind = EnvelopeBackend
				name := strings.TrimPrefix(id, "backend:")
				g.env.Label = name
			case strings.HasPrefix(id, "container:"):
				g.env.Kind = EnvelopeContainer
				g.env.Label = "container " + strings.TrimPrefix(id, "container:")
				g.env.ContainerID = strings.TrimPrefix(id, "container:")
			default:
				g.env.Kind = EnvelopeSystem
				g.env.Label = "system"
			}
			grouped[id] = g
		}
		g.env.PIDs = append(g.env.PIDs, r.PID)
		g.env.CPUPct += r.CPUPct
		g.env.RSSBytes += r.RSSBytes
		g.env.Threads += r.Threads
		g.env.FDs += r.FDs
		if r.ContainerID != "" && g.env.ContainerID == "" {
			g.env.ContainerID = r.ContainerID
		}
	}

	out := make([]Envelope, 0, len(grouped))
	for _, g := range grouped {
		g.env.CPUPct = round2(g.env.CPUPct)
		g.env.LastActivityUnixMs = time.Now().UnixMilli()
		out = append(out, g.env)
	}
	// Sort by CPU descending, with system last as a tie-breaker.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind == EnvelopeSystem && out[j].Kind != EnvelopeSystem {
			return false
		}
		if out[j].Kind == EnvelopeSystem && out[i].Kind != EnvelopeSystem {
			return true
		}
		return out[i].CPUPct > out[j].CPUPct
	})
	return out, assignment
}

func matchesBackend(r *ProcRecord, sig BackendSig) bool {
	comm := strings.ToLower(r.Comm)
	for _, e := range sig.Exec {
		if strings.EqualFold(comm, e) {
			return true
		}
	}
	// Fallback: basename of cmdline[0].
	if r.Cmdline != "" {
		parts := strings.Fields(r.Cmdline)
		if len(parts) > 0 {
			base := strings.ToLower(filepath.Base(parts[0]))
			for _, e := range sig.Exec {
				if base == strings.ToLower(e) {
					return true
				}
			}
		}
	}
	return false
}

func walkSubtree(root int, children map[int][]int, visit func(int)) {
	stack := []int{root}
	for len(stack) > 0 {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		visit(pid)
		stack = append(stack, children[pid]...)
	}
}

func lookupSessionLabel(fullID string) string {
	gRoots.mu.Lock()
	defer gRoots.mu.Unlock()
	if sr, ok := gRoots.data[fullID]; ok {
		if sr.Label != "" {
			return sr.Label
		}
	}
	return fullID
}

func lookupSessionRootPID(fullID string) int {
	gRoots.mu.Lock()
	defer gRoots.mu.Unlock()
	if sr, ok := gRoots.data[fullID]; ok {
		return sr.RootPID
	}
	return 0
}
