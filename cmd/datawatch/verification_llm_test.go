package main

import (
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// selectInputFor builds selectVerifierLLM's input the same way the
// autonomousVerify closure does.
func selectInputFor(prd *autonomouspkg.PRD, task *autonomouspkg.Task, cfg autonomouspkg.Config) verifierSelectInput {
	noEffort := cfg
	noEffort.VerificationEffort = ""
	return verifierSelectInput{
		Resolved:       autonomouspkg.ResolveVerificationLLM(prd, task, cfg),
		ExplicitEffort: autonomouspkg.ResolveVerificationLLM(prd, task, noEffort).Effort,
		Pool:           cfg.VerificationBackends,
		GlobalBackend:  cfg.VerificationBackend,
		GlobalModel:    cfg.VerificationModel,
		PRDBackend:     prd.Backend,
		PRDModel:       prd.Model,
		ProjectDir:     prd.ProjectDir,
	}
}

// poolAdmitter records tryAdmit calls and admits every candidate.
type poolAdmitter struct{ calls []string }

func (p *poolAdmitter) tryAdmit(backend, _ string) (func(), bool) {
	p.calls = append(p.calls, backend)
	return func() {}, true
}

func overrideFixture() (*autonomouspkg.PRD, *autonomouspkg.Task, autonomouspkg.Config) {
	task := autonomouspkg.Task{ID: "t1"}
	prd := &autonomouspkg.PRD{
		ID: "p1", Backend: "claude-code", Model: "claude-sonnet-5", ProjectDir: "/work/p1",
		Story: []autonomouspkg.Story{{ID: "s1", Tasks: []autonomouspkg.Task{task}}},
	}
	cfg := autonomouspkg.Config{
		VerificationBackend:  "ollama",
		VerificationModel:    "qwen3.8:27b",
		VerificationEffort:   "high",
		VerificationBackends: []string{"pool-a", "pool-b"},
	}
	return prd, &prd.Story[0].Tasks[0], cfg
}

func TestSelectVerifierLLM_TaskOverrideBypassesPool(t *testing.T) {
	prd, task, cfg := overrideFixture()
	task.VerificationBackend = "openwebui"
	task.VerificationModel = "llama-task"
	task.VerificationEffort = "low"
	prd.VerificationBackend = "ollama-prd" // less specific — must lose
	resolve, compatible := fakeAskResolver(nil, nil)
	pool := &poolAdmitter{}

	sel := selectVerifierLLM(selectInputFor(prd, task, cfg), pool.tryAdmit, resolve, compatible, fakeIsSessionKind)

	if len(pool.calls) != 0 {
		t.Errorf("pool consulted for an explicit override: %v", pool.calls)
	}
	if sel.Release != nil {
		t.Error("Release set without a pool admission")
	}
	if sel.Backend != "openwebui" || sel.Kind != "openwebui" || sel.Model != "llama-task" || sel.UseSession {
		t.Errorf("got %+v, want openwebui/llama-task ask call", sel)
	}
	if sel.Source != autonomouspkg.VerifierSourceTask || sel.Effort != "low" {
		t.Errorf("source/effort = %q/%q, want task/low", sel.Source, sel.Effort)
	}
}

func TestSelectVerifierLLM_StoryAndPRDOverridesBypassPool(t *testing.T) {
	cases := []struct {
		name       string
		set        func(*autonomouspkg.PRD)
		wantSource string
	}{
		{"story", func(p *autonomouspkg.PRD) {
			p.Story[0].VerificationBackend = "openwebui"
			p.Story[0].VerificationModel = "m-story"
		}, autonomouspkg.VerifierSourceStory},
		{"prd", func(p *autonomouspkg.PRD) {
			p.VerificationBackend = "openwebui"
			p.VerificationModel = "m-prd"
		}, autonomouspkg.VerifierSourcePRD},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prd, task, cfg := overrideFixture()
			c.set(prd)
			resolve, compatible := fakeAskResolver(nil, nil)
			pool := &poolAdmitter{}
			sel := selectVerifierLLM(selectInputFor(prd, task, cfg), pool.tryAdmit, resolve, compatible, fakeIsSessionKind)
			if len(pool.calls) != 0 {
				t.Errorf("pool consulted: %v", pool.calls)
			}
			if sel.Backend != "openwebui" || sel.Model != "m-"+c.name || sel.Source != c.wantSource {
				t.Errorf("got %+v", sel)
			}
			// No explicit effort anywhere: the global "high" must not leak in.
			if sel.Effort != "" {
				t.Errorf("Effort = %q, want empty (global verification_effort not applied)", sel.Effort)
			}
		})
	}
}

func TestSelectVerifierLLM_GlobalUsesPoolFirst(t *testing.T) {
	prd, task, cfg := overrideFixture()
	task.VerificationEffort = "high" // effort alone is not a backend override
	resolve, compatible := fakeAskResolver(
		map[string]string{"pool-a": "ollama", "pool-b": "ollama"},
		map[string]string{"pool-a": "m-a", "pool-b": "m-b"})
	pool := &poolAdmitter{}

	sel := selectVerifierLLM(selectInputFor(prd, task, cfg), pool.tryAdmit, resolve, compatible, fakeIsSessionKind)

	if len(pool.calls) != 1 || pool.calls[0] != "pool-a" {
		t.Errorf("pool calls = %v, want [pool-a]", pool.calls)
	}
	if sel.Backend != "pool-a" || sel.Kind != "ollama" || sel.Model != "m-a" || sel.Release == nil {
		t.Errorf("got %+v, want admitted pool-a", sel)
	}
	if sel.Source != autonomouspkg.VerifierSourcePool || sel.Effort != "" {
		t.Errorf("source/effort = %q/%q, want pool/empty", sel.Source, sel.Effort)
	}
}

func TestSelectVerifierLLM_GlobalFallsBackLikeToday(t *testing.T) {
	prd, task, cfg := overrideFixture()
	resolve, compatible := fakeAskResolver(
		map[string]string{"pool-a": "ollama", "pool-b": "ollama"}, nil)
	full := func(string, string) (func(), bool) { return nil, false }

	in := selectInputFor(prd, task, cfg)
	sel := selectVerifierLLM(in, full, resolve, compatible, fakeIsSessionKind)

	wb, wk, wm, ws := resolveVerifierBackendModel(cfg.VerificationBackend, cfg.VerificationModel,
		prd.Backend, prd.Model, prd.ProjectDir, resolve, compatible, fakeIsSessionKind)
	if sel.Backend != wb || sel.Kind != wk || sel.Model != wm || sel.UseSession != ws {
		t.Errorf("got %+v, want resolveVerifierBackendModel's %s/%s/%s/%v", sel, wb, wk, wm, ws)
	}
	if sel.Source != autonomouspkg.VerifierSourceGlobal || sel.Release != nil || sel.Effort != "" {
		t.Errorf("got %+v, want global source, no release, no effort", sel)
	}

	// No pool configured / no admitter: same default.
	cfg.VerificationBackends = nil
	sel2 := selectVerifierLLM(selectInputFor(prd, task, cfg), nil, resolve, compatible, fakeIsSessionKind)
	if sel2.Backend != wb || sel2.Model != wm || sel2.Source != autonomouspkg.VerifierSourceGlobal {
		t.Errorf("no-pool got %+v", sel2)
	}
}

func TestSelectVerifierLLM_SessionOverrideUsesOverrideModel(t *testing.T) {
	prd, task, cfg := overrideFixture()
	task.VerificationBackend = "claude-code"
	task.VerificationModel = "opus"
	task.VerificationEffort = "high"
	resolve, compatible := fakeAskResolver(
		map[string]string{"claude-code": "claude-code"},
		map[string]string{"claude-code": "registry-default"})

	sel := selectVerifierLLM(selectInputFor(prd, task, cfg), (&poolAdmitter{}).tryAdmit, resolve, compatible, fakeIsSessionKind)

	if !sel.UseSession || sel.Backend != "claude-code" || sel.Kind != "claude-code" {
		t.Fatalf("got %+v, want session-routed claude-code", sel)
	}
	if sel.Model != "opus" {
		t.Errorf("Model = %q, want the override's own model %q (not the global ask model or registry default)", sel.Model, "opus")
	}
	if sel.Effort != "high" || sel.Source != autonomouspkg.VerifierSourceTask {
		t.Errorf("effort/source = %q/%q", sel.Effort, sel.Source)
	}

	// Override without a model: the backend's registry default, as today.
	task.VerificationModel = ""
	sel = selectVerifierLLM(selectInputFor(prd, task, cfg), nil, resolve, compatible, fakeIsSessionKind)
	if !sel.UseSession || sel.Model != "registry-default" {
		t.Errorf("model-less override got %+v, want registry-default session", sel)
	}
}

func TestSelectVerifierLLM_SessionOverrideNoProjectDirDowngrades(t *testing.T) {
	prd, task, cfg := overrideFixture()
	prd.ProjectDir = ""
	task.VerificationBackend = "claude-code"
	task.VerificationModel = "opus"
	resolve, compatible := fakeAskResolver(
		map[string]string{"claude-code": "claude-code"}, nil)

	sel := selectVerifierLLM(selectInputFor(prd, task, cfg), (&poolAdmitter{}).tryAdmit, resolve, compatible, fakeIsSessionKind)

	wb, wk, wm, ws := resolveVerifierBackendModel("claude-code", "opus", prd.Backend, prd.Model, "",
		resolve, compatible, fakeIsSessionKind)
	if sel.UseSession || sel.Backend != "ollama" || sel.Kind != "ollama" {
		t.Errorf("got %+v, want ollama downgrade", sel)
	}
	if sel.Backend != wb || sel.Kind != wk || sel.Model != wm || sel.UseSession != ws {
		t.Errorf("got %+v, want today's downgrade %s/%s/%s/%v", sel, wb, wk, wm, ws)
	}
	if sel.Source != autonomouspkg.VerifierSourceTask {
		t.Errorf("Source = %q, want task", sel.Source)
	}
}

func TestVerifierSelection_Stamp(t *testing.T) {
	sel := verifierSelection{Backend: "b", Model: "m", Effort: "high", Source: autonomouspkg.VerifierSourceStory}
	vr := autonomouspkg.VerificationResult{VerifierBackend: "spoofed-by-llm-json"}
	sel.stamp(&vr)
	if vr.VerifierBackend != "b" || vr.VerifierModel != "m" || vr.VerifierEffort != "high" || vr.VerifierSource != "story" {
		t.Errorf("stamp = %+v", vr)
	}
}

func TestResolveVerifierBackendModelWithSessionModel_EmptyMatchesBase(t *testing.T) {
	resolve, compatible := fakeAskResolver(
		map[string]string{"claude-code": "claude-code"},
		map[string]string{"claude-code": "registry-default"})
	for _, pd := range []string{"", "/work/x"} {
		b1, k1, m1, s1 := resolveVerifierBackendModel("claude-code", "qwen3.8:27b", "", "", pd, resolve, compatible, fakeIsSessionKind)
		b2, k2, m2, s2 := resolveVerifierBackendModelWithSessionModel("claude-code", "qwen3.8:27b", "", "", pd, "", resolve, compatible, fakeIsSessionKind)
		if b1 != b2 || k1 != k2 || m1 != m2 || s1 != s2 {
			t.Errorf("projectDir=%q: base %s/%s/%s/%v != extended %s/%s/%s/%v", pd, b1, k1, m1, s1, b2, k2, m2, s2)
		}
	}
}
