// Identity resolution for node fingerprints: the value a node's
// fingerprint is derived from is the node's own stable identity —
// its GPU UUIDs when it has any, else its disk-persisted
// node-instance id. Never the name or address a daemon has
// configured for the node: one physical node reached through a LAN
// address and a tailnet address must resolve to the identical value,
// otherwise a peer-pair fingerprint match is a false negative.
package fingerprint

import (
	"errors"
	"log"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/compute"
)

// ErrNoIdentity is returned by Resolve (and skipped by ResolveAll)
// when a node exposes no node-side identity: no observer peer bound,
// not a local node, or the host reported neither GPU UUIDs nor a
// node-instance id. Such a node is simply not fingerprinted.
var ErrNoIdentity = errors.New("fingerprint: node has no identity source")

// NodeValue is one resolved node identity — the raw value that goes
// into Hash; it stays local, only the hash ever crosses the wire.
type NodeValue struct {
	Name  string
	Value string
}

// IdentityResolver resolves the identity value for each compute
// node. All three hooks are injectable so tests drive it without a
// live observer fleet:
//   - Nodes lists the registry entries to resolve.
//   - HostInfo fetches the reported identity of a remote node by
//     observer peer name.
//   - Local returns the local host's identity for kind=local nodes.
type IdentityResolver struct {
	Nodes    func() []compute.Node
	HostInfo func(observerPeer string) map[string]any
	Local    func() map[string]any
}

// Resolve returns the identity value for one node: "gpu:" + the
// sorted GPU UUIDs comma-joined when the host reports any, else
// "inst:" + the node-instance id, else (ErrNoIdentity). The value is
// never derived from the node's name or address.
func (r *IdentityResolver) Resolve(n compute.Node) (string, error) {
	var info map[string]any
	switch {
	case n.ObserverPeer != "":
		if r.HostInfo == nil {
			return "", ErrNoIdentity
		}
		info = r.HostInfo(n.ObserverPeer)
	case n.Kind == compute.KindLocal:
		if r.Local == nil {
			return "", ErrNoIdentity
		}
		info = r.Local()
	default:
		return "", ErrNoIdentity
	}
	if info == nil {
		return "", ErrNoIdentity
	}
	if uuids := hostGPUUUIDs(info); len(uuids) > 0 {
		return "gpu:" + strings.Join(uuids, ","), nil
	}
	if id := hostInstanceID(info); id != "" {
		return "inst:" + id, nil
	}
	return "", ErrNoIdentity
}

// ResolveAll resolves every node from Nodes, skipping (and logging
// once each) the ones with no identity source. A skipped node is
// simply not fingerprinted — that is not an operator-facing failure.
func (r *IdentityResolver) ResolveAll() []NodeValue {
	if r.Nodes == nil {
		return nil
	}
	var values []NodeValue
	logged := make(map[string]bool)
	for _, n := range r.Nodes() {
		value, err := r.Resolve(n)
		if err != nil {
			if !logged[n.Name] {
				log.Printf("fingerprint: skipping node %q — no node-side identity (%v)", n.Name, err)
				logged[n.Name] = true
			}
			continue
		}
		values = append(values, NodeValue{Name: n.Name, Value: value})
	}
	return values
}

// hostGPUUUIDs pulls the sorted, non-empty GPU UUID list out of a
// HostInfo-style map. Accepts []string (same process) and []any
// (JSON round-trip); returns nil when absent or empty.
func hostGPUUUIDs(info map[string]any) []string {
	var out []string
	switch v := info["gpu_uuids"].(type) {
	case []string:
		for _, u := range v {
			if u = strings.TrimSpace(u); u != "" {
				out = append(out, u)
			}
		}
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// hostInstanceID pulls the node-instance id string out of a
// HostInfo-style map. Empty when absent.
func hostInstanceID(info map[string]any) string {
	if s, ok := info["node_instance_id"].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
