// Per-service model-use reporting — GET /api/plugins/{name}/models.
//
// For a registered service-kind plugin the endpoint reports, per model:
// the Ollama node it is resident on, the RAM/VRAM that node says the
// model holds, the context length the node's Ollama reader reports for
// it, and the two findings raised with parameterised thresholds
// (plugins.ModelUseThresholds, zero values normalise to the named
// defaults). small_model_large_context fires when a model under the
// small-model memory threshold is pinned above the context threshold;
// node_overloaded fires when the model's node holds more than the
// node_load_ratio times another node that could serve the same model.
// When the service's contract does not document per-model call counts
// the report carries only what the nodes report and says so (a note in
// the report). The endpoint reports node reachability in the report
// only:
// model_node_unreachable is raised (and cleared) by the background
// model-node events task (StartModelNodeEvents, plugins.ModelNodeEvents)
// through the plugins ServiceEventRecorder — once per transition — not
// by this request.
//
// The endpoint reuses the existing readers, it does not call Ollama on
// its own terms: served models come from compute.HTTPModelLister (or the
// pre-seeded list) and resident models + memory come from
// observer.OllamaTap.Read on /api/ps per node.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/observer"
	"github.com/dmz006/datawatch/internal/plugins"
	"github.com/dmz006/datawatch/internal/secrets"
)

// defaultModelNodeRec is the process-wide fallback recorder (see
// defaultModelNodeRecorder).
var (
	defaultModelNodeRec     *plugins.ServiceEventRecorder
	defaultModelNodeRecOnce sync.Once
	// modelNodeEventsOnce guards the process-wide model-node events
	// task so StartModelNodeEvents is idempotent even if wired twice.
	modelNodeEventsOnce sync.Once
)

// PluginModelsSource produces one service's model-use report. The
// default (built from the already-wired plugin registry, compute node
// registry, secrets store) needs no extra wiring; tests inject fakes.
type PluginModelsSource interface {
	ModelUse(ctx context.Context, name string) (*plugins.ModelUseReport, error)
}

// SetPluginModels overrides the model-use source for the
// /api/plugins/{name}/models endpoint. A nil value falls back to the
// default source.
func (s *Server) SetPluginModels(src PluginModelsSource) { s.pluginModelsSrc = src }

// SetModelNodeRecorder wires the recorder that raises
// model_node_unreachable when a node does not answer. A nil recorder
// (the usual wiring) uses a process-wide default that alerts through
// alerts.EmitSystem and audits through the server audit log.
func (s *Server) SetModelNodeRecorder(rec *plugins.ServiceEventRecorder) {
	if rec != nil {
		s.pluginModelNodeRec = rec
	}
}

// modelUse source sentinels.
var (
	// ErrModelUseUnknownPlugin — no registered plugin with that name.
	ErrModelUseUnknownPlugin = errors.New("model use: plugin not found")
	// ErrModelUseNotService — the plugin exists but is not a
	// service-kind plugin with a connection base_url.
	ErrModelUseNotService = errors.New("model use: plugin is not a service")
)

// modelUseReadTimeout bounds each individual node/service read inside one
// report (the report fans out sequentially over a small node set).
const modelUseReadTimeout = 5 * time.Second

// defaultModelUseSource resolves one service's usage inputs with the
// existing readers:
//
//   - per node — served models via compute.HTTPModelLister (or the
//     pre-seeded list) and resident models + memory via
//     observer.OllamaTap.Read of /api/ps; a node that does not answer
//     lands in plugins.ModelNode.Error;
//   - per service — the models it calls from <base_url>/api/models (a
//     404 there means the service does not document them) and the
//     per-model call counts it documents from <base_url>/api/stats.
//     The bearer token comes from the manifest's
//     connection.auth.token ${secret:name} reference.
type defaultModelUseSource struct {
	srv      *Server
	manifest func(name string) (plugins.Manifest, bool)
}

func (s *Server) defaultModelUseSource() *defaultModelUseSource {
	if s.pluginTokensSrc == nil {
		return nil
	}
	return &defaultModelUseSource{srv: s, manifest: s.pluginTokensSrc.Manifest}
}

func (d *defaultModelUseSource) ModelUse(ctx context.Context, name string) (*plugins.ModelUseReport, error) {
	m, ok := d.manifest(name)
	if !ok {
		return nil, ErrModelUseUnknownPlugin
	}
	if m.Kind != plugins.KindService || m.Connection == nil || strings.TrimSpace(m.Connection.BaseURL) == "" {
		return nil, ErrModelUseNotService
	}
	base := strings.TrimRight(strings.TrimSpace(m.Connection.BaseURL), "/")

	in := plugins.ModelUseInputs{}
	if reg := d.srv.computeReg; reg != nil {
		for _, n := range reg.List() {
			if n == nil || n.Disabled || n.Kind != compute.KindOllama {
				continue
			}
			in.Nodes = append(in.Nodes, d.nodeModels(ctx, n))
		}
	}
	tok, tokErr := d.tokenFor(m)
	in.Service = d.serviceModels(ctx, base, tok, tokErr)
	return plugins.CollectModelUse(name, in, d.srv.pluginModelUseThresholds()), nil
}

// pluginModelUseThresholds supplies the daemon's plugins.services settings
// (node_load_ratio, small_model_context_tokens) to the model-use findings. A
// server built without a config (library embed), or a field never PUT, reads
// the documented config defaults — the Effective* accessors never return an
// ineffective zero. SmallModelMaxBytes stays the package's byte boundary:
// Ollama reports memory, not parameter counts, so small_model_max_params
// _billions is the documented criterion while the byte boundary is the
// operative one.
func (s *Server) pluginModelUseThresholds() plugins.ModelUseThresholds {
	svc := s.pluginsServicesConfig()
	return plugins.ModelUseThresholds{
		NodeLoadRatio:    svc.EffectiveNodeLoadRatio(),
		MinContextTokens: int64(svc.EffectiveSmallModelContextTokens()),
	}
}

// pluginsServicesConfig returns the live plugins.services block, or a zero
// value when the server has no config at all; the zero value reads each
// documented default through its Effective* accessor.
func (s *Server) pluginsServicesConfig() config.PluginServicesConfig {
	if s.cfg == nil {
		return config.PluginServicesConfig{}
	}
	return s.cfg.Plugins.Services
}

// nodeModels reads one node's served and resident models.
func (d *defaultModelUseSource) nodeModels(ctx context.Context, n *compute.Node) plugins.ModelNode {
	out := plugins.ModelNode{Name: n.Name}
	if served, err := compute.HTTPModelLister()(ctx, n, modelUseReadTimeout); err == nil {
		out.Served = served
	} else {
		out.Error = err.Error()
	}
	if tap := observer.NewOllamaTap(n.Address); tap != nil {
		loaded, err := tap.Read(ctx)
		if err == nil {
			for _, lm := range loaded {
				out.Loaded = append(out.Loaded, plugins.LoadedModel{
					Name:          lm.LoadedName(),
					RAMBytes:      lm.Size,
					VRAMBytes:     lm.SizeVRAM,
					ContextTokens: lm.ContextTokens(),
				})
			}
		} else if out.Error == "" {
			out.Error = err.Error()
		}
	}
	return out
}

// tokenFor resolves the manifest's bearer ${secret:name} reference. An
// empty auth token (or a literal one, which the Secrets-Store Rule
// forbids) yields no token — the service call then runs unauthenticated.
func (d *defaultModelUseSource) tokenFor(m plugins.Manifest) (string, error) {
	if m.Connection == nil || m.Connection.Auth == nil || strings.TrimSpace(m.Connection.Auth.Token) == "" {
		return "", nil
	}
	refs := secrets.RefNames(m.Connection.Auth.Token)
	if len(refs) == 0 {
		return "", nil
	}
	st := d.srv.secretsStore
	if st == nil {
		return "", nil
	}
	sec, err := st.Get(refs[0])
	if err != nil {
		return "", err
	}
	return sec.Value, nil
}

// serviceModels reads the service's side: the models it calls (documented
// when the endpoint answers) and the per-model call counts it documents.
func (d *defaultModelUseSource) serviceModels(ctx context.Context, base, token string, tokenErr error) plugins.ServiceUsage {
	out := plugins.ServiceUsage{}
	if tokenErr != nil {
		out.Error = tokenErr.Error()
		return out
	}
	body, status, err := d.httpGet(ctx, base+"/api/models", token)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	if status == http.StatusNotFound {
		// The service does not document the models it calls: the report
		// carries only what the nodes report and says so.
		return out
	}
	if status != http.StatusOK {
		out.Error = fmt.Sprintf("models endpoint returned HTTP %d", status)
		return out
	}
	var payload struct {
		Models []string `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		out.Error = "could not parse the models endpoint response"
		return out
	}
	out.Documented = true
	for _, name := range payload.Models {
		out.Models = append(out.Models, plugins.ServiceModel{Name: name})
	}
	// Call counts are optional per the service's contract: a 404 (or an
	// unparseable body) leaves them at 0 and the report says so.
	if stats, sErr := d.serviceStats(ctx, base, token); sErr == nil {
		for i := range out.Models {
			if c, ok := stats[out.Models[i].Name]; ok {
				out.Models[i].Calls = c
			}
		}
	}
	return out
}

// serviceStats reads <base>/api/stats in either of the two documented
// shapes: an object {"<model>": <calls>} or an array
// [{"name": "<model>", "calls": <n>}].
func (d *defaultModelUseSource) serviceStats(ctx context.Context, base, token string) (map[string]int64, error) {
	out := map[string]int64{}
	body, status, err := d.httpGet(ctx, base+"/api/stats", token)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound || status != http.StatusOK {
		return out, nil
	}
	var generic struct {
		Models json.RawMessage `json:"models"`
		Calls  json.RawMessage `json:"calls"`
		Stats  json.RawMessage `json:"stats"`
	}
	if err := json.Unmarshal(body, &generic); err != nil {
		return out, nil
	}
	for _, raw := range []json.RawMessage{generic.Models, generic.Calls, generic.Stats} {
		if len(raw) == 0 {
			continue
		}
		for k, v := range parseStatsBlock(raw) {
			if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
	}
	return out, nil
}

// parseStatsBlock decodes one stats block, either a flat
// {"<model>": <n>} object or a [{"name": "<model>", "calls": <n>}] array.
func parseStatsBlock(raw json.RawMessage) map[string]int64 {
	out := map[string]int64{}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		for k, v := range obj {
			if f, ok := v.(float64); ok {
				out[k] = int64(f)
			}
		}
		return out
	}
	var arr []struct {
		Name  string  `json:"name"`
		Calls float64 `json:"calls"`
		Count float64 `json:"count"`
	}
	if json.Unmarshal(raw, &arr) == nil {
		for _, e := range arr {
			if e.Calls != 0 || e.Count != 0 {
				out[e.Name] = int64(e.Calls)
			} else {
				out[e.Name] = 0
			}
		}
	}
	return out
}

func (d *defaultModelUseSource) httpGet(ctx context.Context, url, token string) (body []byte, status int, err error) {
	client := &http.Client{Timeout: modelUseReadTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close() //nolint:errcheck
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return b, resp.StatusCode, nil
}

// handlePluginModels serves GET /api/plugins/{name}/models — one
// service's model-use report: per model, the Ollama node it runs on,
// the memory it holds, the context the node reports for it, and the
// parameterised findings (small_model_large_context, node_overloaded).
// Node reachability is reported in the body only; the
// model_node_unreachable raise is owned by the background model-node
// events task (StartModelNodeEvents), not by this request.
func (s *Server) handlePluginModels(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if peerFromContext(r.Context()) != nil || sessionCapsFromContext(r.Context()) != nil {
		http.Error(w, "operator token required", http.StatusForbidden)
		return
	}
	src := s.pluginModelsSrc
	if src == nil {
		src = s.defaultModelUseSource()
	}
	if src == nil {
		http.Error(w, "plugins not available", http.StatusServiceUnavailable)
		return
	}
	rep, err := src.ModelUse(r.Context(), name)
	if err != nil {
		switch {
		case errors.Is(err, ErrModelUseUnknownPlugin):
			http.Error(w, fmt.Sprintf("plugin %q not found", name), http.StatusNotFound)
		case errors.Is(err, ErrModelUseNotService):
			http.Error(w, fmt.Sprintf("plugin %q is not a service plugin", name), http.StatusUnprocessableEntity)
		default:
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		}
		return
	}
	// Node reachability is reported in the body (ModelNode.Error for
	// nodes that did not answer). Raising model_node_unreachable is
	// owned by the background model-node events task (see
	// StartModelNodeEvents), not by this request.
	writeJSONOK(w, rep)
}

// StartModelNodeEvents starts the process-wide model-node events task:
// on each interval pass it resolves every service plugin's model-use
// report from the source and feeds node reachability to the model-node
// recorder — raising model_node_unreachable for every node that did not
// answer and clearing it for every node that did, once per transition.
// This is where model_node_unreachable is raised; a request to the
// model-use endpoint no longer raises it. servicePlugins supplies the
// current service-kind plugin roster and is re-invoked on every pass, so
// it tracks what is registered then; an empty list is a no-op pass. A
// nil recorder (the usual wiring) uses the process-wide default.
// Idempotent: a second call starts nothing.
func (s *Server) StartModelNodeEvents(ctx context.Context, servicePlugins func() []string) {
	modelNodeEventsOnce.Do(func() {
		rec := s.pluginModelNodeRec
		if rec == nil {
			rec = s.defaultModelNodeRecorder()
		}
		events := &plugins.ModelNodeEvents{Recorder: rec, Source: s.pluginModelsSrcIfConfigured(s.defaultModelUseSource())}
		events.Plugins = func(context.Context) []string { return servicePlugins() }
		if s.modelNodeEventInterval > 0 {
			events.Interval = s.modelNodeEventInterval
		}
		s.modelNodeEvents = events
		go events.Run(ctx)
	})
}

// SetModelNodeEventInterval sets the model-node events task pass
// interval (zero keeps DefaultModelNodeEventInterval, 15 s). It is read
// when the task starts, not after.
func (s *Server) SetModelNodeEventInterval(d time.Duration) {
	s.modelNodeEventInterval = d
}

// pluginModelsSrcIfConfigured returns the explicitly wired source, or
// def to when none (an explicit source may be a test fake).
func (s *Server) pluginModelsSrcIfConfigured(def PluginModelsSource) PluginModelsSource {
	if s.pluginModelsSrc != nil {
		return s.pluginModelsSrc
	}
	return def
}

// defaultModelNodeRecorder is the recorder used when none is wired:
// alerts through alerts.EmitSystem (the recorder's own default) and
// audits through the server audit log. One per process.
func (s *Server) defaultModelNodeRecorder() *plugins.ServiceEventRecorder {
	defaultModelNodeRecOnce.Do(func() {
		defaultModelNodeRec = plugins.NewServiceEventRecorder(plugins.ServiceEventOptions{
			Audit: func(action, subject string, details map[string]any) {
				ctx := context.Background()
				s.audit(ctx, action, "plugin", subject, details)
			},
			// The backlog-growth streak follows plugins.services.backlog
			// _growth_pulls (default 3).
			BacklogRisePulls: s.pluginsServicesConfig().EffectiveBacklogGrowthPulls(),
		})
	})
	return defaultModelNodeRec
}
