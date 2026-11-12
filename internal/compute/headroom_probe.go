// Package compute — headroom measurement probe.
//
// headroom.go defines the gate (does a fresh measurement support a cap?).
// This file defines what feeds it: a measurement of one node's free
// memory with its resident model loaded, and the memory one extra
// parallel request needs.
//
// The measurement is idle-only: Measure never runs on a node that is
// currently running work (Held > 0 is refused with ErrNodeBusy), so the
// numbers it reports describe an idle node, not one under load.
//
// The probe is injected as an interface (HeadroomProbe). Measure itself
// has no transport details; OllamaHeadroomProbe (below) is the concrete
// probe for an Ollama node. Tests use a fake probe and an injected clock,
// so the code path is identical in tests and production and never depends
// on wall time.

package compute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNodeBusy is returned when a node has workers holding a lease, i.e.
// it is running work now. A node under load is never measured.
var ErrNodeBusy = errors.New("node busy")

// ErrModelNotAllowed is returned when the allow-list is empty, or when
// the model currently resident on the node is not in it. Measuring an
// unauthorised (or unknowable) model set would report numbers the gate
// must not be allowed to rely on.
var ErrModelNotAllowed = errors.New("model not allowed")

// HeadroomProbe reads a node's memory state. OllamaHeadroomProbe is the
// production implementation; tests may supply fakes.
type HeadroomProbe interface {
	// ReadFreeMB returns the node's free memory (MB, free video memory
	// when the node has GPU data, else free system memory) and the name
	// of the model currently resident on the node.
	ReadFreeMB(ctx context.Context, node Node) (freeMB int, residentModel string, err error)
	// RequestMemoryMB returns how many MB one extra parallel request
	// needs beyond the resident model, estimated by issuing a probe
	// request for model.
	RequestMemoryMB(ctx context.Context, node Node, model string) (perRequestMB int, err error)
}

// MeasureDeps carries everything Measure needs. Nothing is read from
// globals: the clock is injected (Now) and the probe is injected
// (Probe), so tests drive both.
type MeasureDeps struct {
	Probe HeadroomProbe // required; the measurement transport
	Now   func() time.Time

	// Held is the number of workers holding a lease on the node now.
	// > 0 means the node is running work and is never measured
	// (ErrNodeBusy).
	Held int

	// AllowedModels are the models this measurement is permitted to
	// involve. The caller is responsible for sorting them smallest
	// first; Measure always picks the first one (AllowedModels[0]) as
	// the probe request's model. Empty = refused (ErrModelNotAllowed).
	AllowedModels []string

	Node Node

	// PerProcessMB is optional, from observer data: a known memory
	// cost per parallel request in MB. When > 0 it is used as
	// PerRequestMB and the RequestMemoryMB probe request is skipped.
	PerProcessMB int
}

// Measure produces a HeadroomMeasurement for deps.Node.
//
// Rules, in order:
//
//  1. Held > 0 → ErrNodeBusy. Never measure a node that is running
//     work; a node under load reports a free memory that understates
//     its idle headroom.
//
//  2. Empty AllowedModels → ErrModelNotAllowed.
//
//  3. ReadFreeMB is always probed first; if the resident model is not
//     in AllowedModels → ErrModelNotAllowed.
//
//  4. PerRequestMB comes from deps.PerProcessMB when that is > 0 (the
//     RequestMemoryMB request is not sent at all); otherwise it comes
//     from Probe.RequestMemoryMB using the smallest allowed model
//     (AllowedModels[0]).
//
// The measurement's MeasuredAt is deps.Now().
func Measure(ctx context.Context, deps MeasureDeps) (HeadroomMeasurement, error) {
	if deps.Held > 0 {
		return HeadroomMeasurement{}, fmt.Errorf("measure node %s: %w", nodeID(deps.Node), ErrNodeBusy)
	}
	if len(deps.AllowedModels) == 0 {
		return HeadroomMeasurement{}, fmt.Errorf("measure node %s: %w", nodeID(deps.Node), ErrModelNotAllowed)
	}
	if deps.Probe == nil {
		return HeadroomMeasurement{}, fmt.Errorf("measure node %s: probe required", nodeID(deps.Node))
	}
	if deps.Now == nil {
		return HeadroomMeasurement{}, fmt.Errorf("measure node %s: clock required", nodeID(deps.Node))
	}

	freeMB, resident, err := deps.Probe.ReadFreeMB(ctx, deps.Node)
	if err != nil {
		return HeadroomMeasurement{}, fmt.Errorf("measure node %s: read free memory: %v", nodeID(deps.Node), err)
	}
	if !modelAllowed(resident, deps.AllowedModels) {
		return HeadroomMeasurement{}, fmt.Errorf("measure node %s: resident model %q: %w",
			nodeID(deps.Node), resident, ErrModelNotAllowed)
	}

	perRequestMB := deps.PerProcessMB
	if perRequestMB <= 0 {
		probeModel := deps.AllowedModels[0] // caller kept it sorted smallest first
		perRequestMB, err = deps.Probe.RequestMemoryMB(ctx, deps.Node, probeModel)
		if err != nil {
			return HeadroomMeasurement{}, fmt.Errorf("measure node %s: request memory: %v", nodeID(deps.Node), err)
		}
	}

	return HeadroomMeasurement{
		Node:          nodeID(deps.Node),
		FreeMB:        freeMB,
		PerRequestMB:  perRequestMB,
		ResidentModel: resident,
		MeasuredAt:    deps.Now(),
	}, nil
}

func modelAllowed(resident string, allowed []string) bool {
	for _, m := range allowed {
		if m == resident {
			return true
		}
	}
	return false
}

func nodeID(n Node) string {
	if n.Name != "" {
		return n.Name
	}
	return "unknown"
}

// ObserverReader returns the observer's reading for a node. A zero
// NodeObservation means the observer has no data for that node.
type ObserverReader func(node Node) NodeObservation

// probeHTTPTimeout bounds the one probe request and the /api/ps read.
const probeHTTPTimeout = 30 * time.Second

// OllamaHeadroomProbe is the concrete HeadroomProbe for an Ollama node.
// Observe and Client are injected; a nil Client uses a client with
// probeHTTPTimeout.
type OllamaHeadroomProbe struct {
	Observe ObserverReader
	Client  *http.Client
}

// Compile-time check that the concrete probe satisfies the interface.
var _ HeadroomProbe = (*OllamaHeadroomProbe)(nil)

// NewOllamaHeadroomProbe returns a probe that reads observations with
// observe and talks to nodes with client (nil means a default client).
func NewOllamaHeadroomProbe(observe ObserverReader, client *http.Client) *OllamaHeadroomProbe {
	return &OllamaHeadroomProbe{Observe: observe, Client: client}
}

// ReadFreeMB returns the node's free memory in MB and the resident model.
// Free memory is free GPU memory when the observer has GPU data, else free
// system memory. The resident model is the observer's ResidentModel when it
// is set, else the first model /api/ps lists (empty when none is loaded).
func (p *OllamaHeadroomProbe) ReadFreeMB(ctx context.Context, node Node) (int, string, error) {
	obs, err := p.observation(node)
	if err != nil {
		return 0, "", err
	}
	freeMB, ok := FreeGPUMB(obs)
	if !ok {
		if obs.SystemTotalBytes == 0 || obs.SystemUsedBytes >= obs.SystemTotalBytes {
			return 0, "", fmt.Errorf("read free memory of node %s: observer has no free GPU or system memory data", nodeID(node))
		}
		freeMB = bytesToMB(obs.SystemTotalBytes - obs.SystemUsedBytes)
	}
	resident := obs.ResidentModel
	if resident == "" {
		resident, err = p.residentFromPS(ctx, node)
		if err != nil {
			return 0, "", err
		}
	}
	return int(freeMB), resident, nil
}

// RequestMemoryMB returns the MB one extra parallel request takes. An
// observer per-process figure is used as is and no request is sent.
// Otherwise one probe request for model is sent and the growth in the
// observer's loaded-memory reading across it is returned. A request that
// shows no growth is an error: a zero figure would let the gate treat
// requests as free.
func (p *OllamaHeadroomProbe) RequestMemoryMB(ctx context.Context, node Node, model string) (int, error) {
	before, err := p.observation(node)
	if err != nil {
		return 0, err
	}
	if before.PerRequestBytes > 0 {
		return int(bytesToMB(before.PerRequestBytes)), nil
	}
	loadedBefore := loadedBytes(before)
	if err := p.generate(ctx, node, model); err != nil {
		return 0, err
	}
	after, err := p.observation(node)
	if err != nil {
		return 0, err
	}
	loadedAfter := loadedBytes(after)
	if loadedAfter <= loadedBefore {
		return 0, fmt.Errorf("measure request memory of node %s: no growth in loaded memory after probe request for %s", nodeID(node), model)
	}
	return int(bytesToMB(loadedAfter - loadedBefore)), nil
}

// observation reads the observer for node and refuses an empty reading.
func (p *OllamaHeadroomProbe) observation(node Node) (NodeObservation, error) {
	if p.Observe == nil {
		return NodeObservation{}, fmt.Errorf("observer reader not configured for node %s", nodeID(node))
	}
	obs := p.Observe(node)
	if len(obs.GPUs) == 0 && obs.SystemTotalBytes == 0 {
		return NodeObservation{}, fmt.Errorf("no observer data for node %s", nodeID(node))
	}
	return obs, nil
}

func (p *OllamaHeadroomProbe) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: probeHTTPTimeout}
}

// loadedBytes is the total memory the observer reports as loaded, on GPU
// and on CPU.
func loadedBytes(obs NodeObservation) uint64 {
	var total uint64
	for _, b := range obs.LoadedModelBytes {
		total += b
	}
	for _, b := range obs.CPUModelBytes {
		total += b
	}
	return total
}

// generate sends one small non-streaming /api/generate request for model.
func (p *OllamaHeadroomProbe) generate(ctx context.Context, node Node, model string) error {
	body, err := json.Marshal(map[string]any{
		"model":   model,
		"prompt":  "ping",
		"stream":  false,
		"options": map[string]any{"num_predict": 1},
	})
	if err != nil {
		return fmt.Errorf("probe request for %s: %w", model, err)
	}
	url, err := ollamaURL(node, "/api/generate")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("probe request for %s: %w", model, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client().Do(req)
	if err != nil {
		return fmt.Errorf("probe request for %s to node %s: %w", model, nodeID(node), err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("probe request for %s to node %s: HTTP %d", model, nodeID(node), resp.StatusCode)
	}
	return nil
}

// residentFromPS returns the first model /api/ps lists, or "" when none is
// loaded.
func (p *OllamaHeadroomProbe) residentFromPS(ctx context.Context, node Node) (string, error) {
	url, err := ollamaURL(node, "/api/ps")
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("read resident model of node %s: %w", nodeID(node), err)
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("read resident model of node %s: %w", nodeID(node), err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read resident model of node %s: HTTP %d", nodeID(node), resp.StatusCode)
	}
	var doc struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", fmt.Errorf("read resident model of node %s: malformed /api/ps: %w", nodeID(node), err)
	}
	if len(doc.Models) == 0 {
		return "", nil
	}
	return doc.Models[0].Name, nil
}

// ollamaURL joins the node's address and path. A node with no address
// cannot be reached.
func ollamaURL(node Node, path string) (string, error) {
	addr := strings.TrimRight(normalizeAddress(node.Address), "/")
	if addr == "" {
		return "", errors.New("node " + nodeID(node) + " has no address")
	}
	return addr + path, nil
}
