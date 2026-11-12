// Gathers the configured model names that could run on a local ollama node,
// for inference.DetectModelNodeConflicts. Lives here rather than in the
// inference package because this package already imports inference.

package config

import "github.com/dmz006/datawatch/internal/inference"

// ModelNodeInputs collects every model reference in the config, with the
// backend each one runs on and the ollama host it would be sent to. Empty
// names are kept; the detector skips them.
//
// Backend fallbacks follow the daemon defaults: a planning, verification or
// guardrail model with no backend of its own inherits session.llm_backend,
// and an assistant with no backend is ollama. A per-PRD backend override is
// not visible here, so such a PRD is not counted.
func (c *Config) ModelNodeInputs() inference.ModelNodeInputs {
	host := c.Ollama.Host
	refs := []inference.ModelReference{
		{Model: c.Ollama.Model, Backend: "ollama", Host: host},
		{Model: c.Autonomous.PlanningModel, Backend: firstNonEmpty(c.Autonomous.PlanningBackend, c.Session.LLMBackend), Host: host},
		{Model: c.Autonomous.VerificationModel, Backend: firstNonEmpty(c.Autonomous.VerificationBackend, c.Session.LLMBackend), Host: host},
		{Model: c.Autonomous.GuardrailModel, Backend: firstNonEmpty(c.Orchestrator.GuardrailBackend, c.Session.LLMBackend), Host: host},
		{Model: c.Session.Summarizer.Model, Host: host},
		{Model: c.Pi.Model, Backend: "pi", Host: host},
		{Model: c.PiPrompt.Model, Backend: "pi-prompt", Host: host},
		{Model: c.Session.AssistantModel, Backend: firstNonEmpty(c.Session.AssistantBackend, "ollama"), Host: host},
	}
	if c.Memory.EffectiveEmbedder() == "ollama" {
		refs = append(refs, inference.ModelReference{
			Model:   c.Memory.EmbedderModel,
			Backend: "ollama",
			Host:    firstNonEmpty(c.Memory.EmbedderHost, host),
		})
	}
	nodes := make([]inference.NodeAddress, 0, len(c.ComputeNodes))
	for _, n := range c.ComputeNodes {
		nodes = append(nodes, inference.NodeAddress{Name: n.Name, Address: n.Address})
	}
	return inference.ModelNodeInputs{References: refs, Nodes: nodes}
}

// firstNonEmpty returns the first value that is not empty, or "" if all are.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
