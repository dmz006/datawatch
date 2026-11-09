// In-process smoke tests for the model-placement logic.
//
// Everything runs inside the test process:
//
//   - a fake Ollama server (httptest) per node that reports the model it has
//     loaded (GET /api/ps), the models it has (GET /api/tags), its version
//     and free memory (GET /api/version) and serves a fake model pull
//     (POST /api/pull, streaming progress ending in success);
//   - fake nodes with different free memory and (optionally) a pinned model;
//   - the pure compute.PlaceMissingModel planner driven by those nodes;
//   - the admission gate (compute_placement.missing_model policy: ask /
//     auto_load / refuse) with a fake audit recorder as the audit sink;
//   - the real inference.ModelLoader against the fake Ollama endpoint to
//     prove a missing model is pulled and loaded on the placed node.
//
// No daemons, no real model pulls, no real compute nodes.
package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

const (
	// smokeModel is the model the smoke checks place.
	smokeModel = "qwen3.8:27b"
	// smokeSizeGB is the model size the planner checks against free memory.
	smokeSizeGB = 20
)

var smokeGB = uint64(1024) * 1024 * 1024 // one GiB

// smokeOllamaNode is one fake compute node: an httptest Ollama endpoint
// that reports the model it has loaded, its version and its free memory,
// and answers a fake /api/pull (the fake model puller) with a streaming
// progress feed ending in a success line.
type smokeOllamaNode struct {
	name string
	// FreeMemoryGB / TotalMemoryGB are the live numbers the planner sees.
	FreeMemoryGB  int
	TotalMemoryGB int
	// Pin, when set, keeps the node on that model (no swap unless AllowSwap).
	Pin       string
	AllowSwap bool

	srv *httptest.Server

	mu               sync.Mutex
	resident         bool
	pulls            int
	pullSuccessLines int
}

func newSmokeOllamaNode(t *testing.T, name string, freeGB, totalGB int, pin string) *smokeOllamaNode {
	t.Helper()
	n := &smokeOllamaNode{name: name, FreeMemoryGB: freeGB, TotalMemoryGB: totalGB, Pin: pin}
	n.srv = httptest.NewServer(http.HandlerFunc(n.serve))
	t.Cleanup(n.srv.Close)
	return n
}

func (n *smokeOllamaNode) serve(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	defer n.mu.Unlock()
	switch r.URL.Path {
	case "/api/ps":
		if n.resident {
			_, _ = fmt.Fprintf(w, `{"models":[{"name":%q,"model":%q}]}`, smokeModel, smokeModel)
			return
		}
		_, _ = fmt.Fprint(w, `{"models":[]}`)
	case "/api/tags":
		if n.resident {
			_, _ = fmt.Fprintf(w, `{"models":[{"name":%q}]}`, smokeModel)
			return
		}
		_, _ = fmt.Fprint(w, `{"models":[]}`)
	case "/api/version":
		_, _ = fmt.Fprintf(w, `{"version":"0.9.9-smoke","free_memory_gb":%d}`, n.FreeMemoryGB)
	case "/api/pull":
		n.pulls++
		w.Header().Set("Content-Type", "application/x-ndjson")
		lines := []string{
			`{"status":"pulling manifest"}`,
			`{"status":"pulling weights","total":268435456,"completed":134217728}`,
			`{"status":"success"}`,
		}
		for _, l := range lines {
			_, _ = fmt.Fprintln(w, l)
		}
	case "/api/generate":
		n.resident = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"done":true,"done_reason":"load"}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// counts reports the pull requests and success lines the fake puller served.
func (n *smokeOllamaNode) counts() (pulls, success int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.pulls, n.pullSuccessLines
}

// candidate is the planner's view of this node: its registry entry plus the
// live memory and disk numbers the fake node reports (disk is ample in all
// smoke checks, so the memory and pin rules are the ones under test).
func (n *smokeOllamaNode) candidate() compute.PlacementCandidate {
	return compute.PlacementCandidate{
		Node: &compute.Node{
			Name:           n.name,
			Address:        n.srv.URL,
			NodePinnedModel: n.Pin,
			AllowModelSwap: n.AllowSwap,
		},
		MemTotalBytes: uint64(n.TotalMemoryGB) * smokeGB,
		MemFreeBytes:  uint64(n.FreeMemoryGB) * smokeGB,
		DiskFreeBytes: 500 * smokeGB,
		ServesModel:   false, // the model is missing on every smoke node
	}
}

// smokePlan runs the pure placement planner over the given fake nodes with
// the effective headroom of the given compute_placement block (unset fields
// read their defaults, as the daemon hands it to the planner).
func smokePlan(cfg config.ComputePlacementConfig, nodes ...*smokeOllamaNode) compute.Placement {
	memPct := cfg.MemHeadroomPct
	if memPct <= 0 {
		memPct = config.ComputePlacementDefaultMemHeadroomPct
	}
	diskGB := cfg.DiskHeadroomGB
	if diskGB <= 0 {
		diskGB = config.ComputePlacementDefaultDiskHeadroomGB
	}
	cands := make([]compute.PlacementCandidate, 0, len(nodes))
	for _, n := range nodes {
		cands = append(cands, n.candidate())
	}
	return compute.PlaceMissingModel(compute.PlacementRequest{
		Model:          smokeModel,
		ModelSizeGB:    smokeSizeGB,
		MemHeadroomPct: memPct,
		DiskHeadroomGB: diskGB,
		Now:            time.Now(),
		Candidates:     cands,
	})
}

// TestSmokeModelPlacement is the model-placement smoke: one subtest per
// check, each self-contained and in-process.
func TestSmokeModelPlacement(t *testing.T) {
	t.Run("missing model placed on the node with the most free memory", func(t *testing.T) {
		alpha := newSmokeOllamaNode(t, "alpha", 64, 66, "")
		beta := newSmokeOllamaNode(t, "beta", 20, 32, "")
		got := smokePlan(config.ComputePlacementConfig{}, alpha, beta)
		if !got.Chosen() {
			t.Fatalf("a fitting node must be chosen, got refusal %v", got.Refusal)
		}
		if got.Node != "alpha" {
			t.Fatalf("chosen = %q, want alpha (most free memory)", got.Node)
		}
		if len(got.Refusal) != 0 {
			t.Errorf("no refusal lines when a node is chosen, got %v", got.Refusal)
		}
	})

	t.Run("node below the free-memory headroom is refused", func(t *testing.T) {
		small := newSmokeOllamaNode(t, "small", 16, 16, "") // 16 GB free vs 20 GB model + 10% headroom
		got := smokePlan(config.ComputePlacementConfig{}, small)
		if got.Chosen() {
			t.Fatalf("a node below the headroom must not be chosen, got %q", got.Node)
		}
		if len(got.Refusal) != 1 {
			t.Fatalf("want one refusal line, got %v", got.Refusal)
		}
		line := got.Refusal[0]
		for _, want := range []string{"small", "free memory", "16.0 GB", "20.0 GB", "10% headroom"} {
			if !strings.Contains(line, want) {
				t.Errorf("refusal line %q does not name %q", line, want)
			}
		}
	})

	t.Run("node whose pinned model must not be displaced is refused", func(t *testing.T) {
		// beta alone: pinned to another model, no swap allowed -> refused,
		// with the pin named in the refusal line.
		beta := newSmokeOllamaNode(t, "beta", 96, 99, "llama3.1:70b")
		got := smokePlan(config.ComputePlacementConfig{}, beta)
		if got.Chosen() {
			t.Fatalf("the pinned node must not be chosen, got %q", got.Node)
		}
		if len(got.Refusal) != 1 || !strings.Contains(got.Refusal[0], "beta") || !strings.Contains(got.Refusal[0], "pinned") || !strings.Contains(got.Refusal[0], "llama3.1:70b") {
			t.Fatalf("refusal must name beta and its pinned model, got %v", got.Refusal)
		}
		// With alpha alongside, placement picks alpha instead of displacing beta.
		alpha := newSmokeOllamaNode(t, "alpha", 64, 66, "")
		if got := smokePlan(config.ComputePlacementConfig{}, beta, alpha); got.Node != "alpha" {
			t.Fatalf("chosen = %q (refusal %v), want alpha: the pinned beta must not be displaced", got.Node, got.Refusal)
		}
		// Control: with allow_model_swap set the same node is eligible and,
		// having the most free memory, wins — the pin was the refusal.
		beta.AllowSwap = true
		if got := smokePlan(config.ComputePlacementConfig{}, beta); got.Node != "beta" {
			t.Fatalf("with allow_model_swap the pinned node must be choosable again, got %q (%v)", got.Node, got.Refusal)
		}
	})

	t.Run("model not allowed to pull is refused with a reason", func(t *testing.T) {
		// missing_model policy "refuse": the load (and any pull) is not
		// allowed, and the refusal carries the planner's per-node reasons.
		alpha := newSmokeOllamaNode(t, "alpha", 16, 16, "")
		beta := newSmokeOllamaNode(t, "beta", 8, 8, "")
		plan := smokePlan(config.ComputePlacementConfig{}, alpha, beta)
		if plan.Chosen() {
			t.Fatal("smoke fixture: both nodes must be below the headroom")
		}
		fa := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
		fb := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
		p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": fa, "beta": fb},
			PlacementPlan{Relevant: true, Chosen: "", Policy: config.ComputePlacementRefuse, Refused: plan.Refusal}, 0)
		err := p.rig.admitErr(t)
		if !IsPlacementRefused(err) {
			t.Fatalf("the refuse policy must fail the start with a placement refusal, got %v", err)
		}
		reasons := strings.Join(plan.Refusal, " | ")
		if !strings.Contains(err.Error(), reasons) {
			t.Errorf("the refusal %q must carry the planner's reasons %q", err.Error(), reasons)
		}
		if _, loads := fa.counts(); loads != 0 {
			t.Errorf("a not-allowed model must never be loaded, got %d loads on alpha", loads)
		}
		if _, loads := fb.counts(); loads != 0 {
			t.Errorf("a not-allowed model must never be loaded, got %d loads on beta", loads)
		}
		if p.rig.led.Held(p.rig.task.ID) {
			t.Error("the lease must be released on a refusal")
		}
		raised := p.rig.sink.snapshot()
		if len(raised) != 1 || raised[0].Level != alerts.LevelWarn {
			t.Fatalf("want one warn deferred alert, got %+v", raised)
		}
		if !strings.Contains(raised[0].Title+" "+raised[0].Body, "beta") {
			t.Errorf("the alert must name the refused reasons, got %q", raised[0].Title+" "+raised[0].Body)
		}
	})

	t.Run("audit record written for each decision", func(t *testing.T) {
		t.Run("refuse decision", func(t *testing.T) {
			refused := []string{"alpha: free memory 16.0 GB, needs 20.0 GB plus 10% headroom"}
			fa := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
			fb := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
			p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": fa, "beta": fb},
				PlacementPlan{Relevant: true, Chosen: "", Policy: config.ComputePlacementRefuse, Refused: refused}, 0)
			cap := &placementAuditCapture{}
			p.rig.m.SetAuditFn(cap.fn)
			if err := p.rig.admitErr(t); !IsPlacementRefused(err) {
				t.Fatalf("want a placement refusal, got %v", err)
			}
			recs := cap.snapshot()
			if len(recs) != 1 || recs[0].action != audit.ActionPlacementRefused {
				t.Fatalf("want one %s event, got %v", audit.ActionPlacementRefused, cap.actions())
			}
			d := recs[0].details
			if d["model"] != nodeLoadModel || d["policy"] != config.ComputePlacementRefuse {
				t.Errorf("audit details %v miss model or policy", d)
			}
			if r, _ := d["reason"].(string); !strings.Contains(r, "16.0 GB") {
				t.Errorf("audit reason %q must carry the planner's line", r)
			}
		})

		t.Run("auto_load decision", func(t *testing.T) {
			fa := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
			fb := newFakeOllamaNode(t, fakeNodeOpts{}) // missing: loads on request
			p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": fa, "beta": fb},
				PlacementPlan{Relevant: true, Chosen: "beta", Policy: config.ComputePlacementAutoLoad, Refused: nil}, 0)
			cap := &placementAuditCapture{}
			p.rig.m.SetAuditFn(cap.fn)
			if err := p.rig.admitErr(t); err != nil {
				t.Fatalf("an auto_load decision must admit, got %v", err)
			}
			recs := cap.snapshot()
			if len(recs) != 1 || recs[0].action != audit.ActionPlacementLoaded {
				t.Fatalf("want one %s event, got %v", audit.ActionPlacementLoaded, cap.actions())
			}
			d := recs[0].details
			if d["node"] != "beta" || d["model"] != nodeLoadModel || d["policy"] != config.ComputePlacementAutoLoad {
				t.Errorf("audit details %v miss node, model or policy", d)
			}
		})

		t.Run("ask decision", func(t *testing.T) {
			fa := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
			fb := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
			p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": fa, "beta": fb},
				PlacementPlan{Relevant: true, Chosen: "beta", Policy: config.ComputePlacementAsk, Refused: nil}, 0)
			cap := &placementAuditCapture{}
			p.rig.m.SetAuditFn(cap.fn)
			p.withQuestionStore(t)

			done := make(chan error, 1)
			go func() { done <- p.rig.admitErr(t) }()
			p.answerFirstPlacementQuestion(t, placementOptionLoad)
			if err := <-done; err != nil {
				t.Fatalf("an approved ask must admit, got %v", err)
			}
			want := []string{audit.ActionPlacementAsked, audit.ActionPlacementApproved}
			gotActions := cap.actions()
			for i, a := range want {
				if i >= len(gotActions) || gotActions[i] != a {
					t.Fatalf("audit trail = %v, want %v then more", gotActions, want)
				}
			}
			for _, r := range cap.snapshot() {
				if r.details["node"] != "beta" || r.details["model"] != nodeLoadModel || r.details["policy"] != config.ComputePlacementAsk {
					t.Errorf("%s details %v miss node, model or policy", r.action, r.details)
				}
			}
		})
	})

	t.Run("policy defaults leave behaviour unchanged", func(t *testing.T) {
		// Unset reads the documented defaults.
		var def config.ComputePlacementConfig
		if got := def.EffectiveMissingModel(); got != config.ComputePlacementAsk {
			t.Errorf("EffectiveMissingModel = %q, want the ask default", got)
		}
		if config.ComputePlacementDefaultMemHeadroomPct != 10 || config.ComputePlacementDefaultDiskHeadroomGB != 5 ||
			config.ComputePlacementDefaultAskTimeoutMinutes != 30 {
			t.Errorf("placement defaults moved: mem %d, disk %d, ask %d",
				config.ComputePlacementDefaultMemHeadroomPct, config.ComputePlacementDefaultDiskHeadroomGB,
				config.ComputePlacementDefaultAskTimeoutMinutes)
		}
		if err := def.Validate(); err != nil {
			t.Errorf("an unset placement block must validate, got %v", err)
		}

		// With no advisor wired at all, admission is exactly what it was:
		// lease the higher-ranked node, fail over on the failed load, and no
		// placement refusal or placement audit event appears anywhere.
		fa := newFakeOllamaNode(t, fakeNodeOpts{FailLoad: true})
		fb := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
		rig := newNodeLoadRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": fa, "beta": fb})
		cap := &placementAuditCapture{}
		rig.m.SetAuditFn(cap.fn)
		rig.admit(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := rig.load(ctx); err != nil {
			t.Fatalf("with the defaults in force the failover load must succeed, got %v", err)
		}
		if got := rig.led.LeaseOf(rig.task.ID); got == nil || got.Node != "beta" {
			t.Fatalf("the lease should land on beta, got %+v", got)
		}
		for _, a := range cap.actions() {
			if strings.HasPrefix(a, "placement_") {
				t.Errorf("a defaults run records no placement event, got %q", a)
			}
		}
		raised := rig.sink.snapshot()
		for _, a := range raised {
			if strings.Contains(a.Title+" "+a.Body, "placement") {
				t.Errorf("a defaults run raises no placement alert, got %q", a.Title)
			}
		}
	})

	t.Run("the placed model is pulled and loaded on the chosen node", func(t *testing.T) {
		// The smoke node does not have the model downloaded: the real
		// ModelLoader must ask the fake puller for it, then make it resident.
		n := newSmokeOllamaNode(t, "placed", 64, 66, "")
		if got := smokePlan(config.ComputePlacementConfig{}, n); got.Node != "placed" {
			t.Fatalf("smoke fixture: the node must be choosable, got %q (%v)", got.Node, got.Refusal)
		}
		// The fake reports its version and free memory, as the operator's
		// tooling reads them.
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(n.srv.URL + "/api/version")
		if err != nil {
			t.Fatalf("GET /api/version: %v", err)
		}
		defer resp.Body.Close()
		var ver struct{ Version string }
		if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
			t.Fatalf("decode version: %v", err)
		}
		if ver.Version != "0.9.9-smoke" {
			t.Errorf("fake version = %q", ver.Version)
		}

		var pullReports []string
		loader := &inference.ModelLoader{Client: client, Wait: 10 * time.Second, Poll: 5 * time.Millisecond}
		err = loader.Ensure(context.Background(), &compute.Node{Name: "placed", Address: n.srv.URL}, smokeModel,
			func(reason string) { pullReports = append(pullReports, reason) })
		if err != nil {
			t.Fatalf("Ensure must pull and load the missing model, got %v", err)
		}
		pulls, success := n.counts()
		if pulls != 1 {
			t.Errorf("the model must be pulled exactly once, got %d pulls", pulls)
		}
		found := false
		for _, r := range pullReports {
			if strings.Contains(r, "pulling") {
				found = true
			}
		}
		if !found {
			t.Errorf("the pull progress must be reported to the wait callback, got %v", pullReports)
		}
		ps, err := client.Get(n.srv.URL + "/api/ps")
		if err != nil {
			t.Fatalf("GET /api/ps: %v", err)
		}
		defer ps.Body.Close()
		body, err := io.ReadAll(io.LimitReader(ps.Body, 4096))
		if err != nil {
			t.Fatalf("read /api/ps: %v", err)
		}
		if !strings.Contains(string(body), smokeModel) {
			t.Errorf("after Ensure the model must be resident, /api/ps reported %q", body)
		}
		_ = success
	})
}
