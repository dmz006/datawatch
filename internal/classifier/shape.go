package classifier

// Wire shapes of the two classification read paths.
//
// STATUS: the shapes below are reconstructed from the design document's
// Backend note (which records one live call returning, per named question, the
// chosen option and a probability for each option) and from the OpenAI chat
// completions logprobs format. The official Ollama documentation was NOT
// re-read for this change; the operator must confirm every field name against
// it before any non-fixture use. The minimum Ollama release is named only in
// the Backend note of the design document.
//
// Native decision endpoint, POST {base}/v1/systemone:
//
//	request:  {"model": "<model>", "prompt": "<text ending in the option set>",
//	           "questions": [{"name": "<id>", "options": ["A", "B"]}]}
//	response: {"model": "<model>",
//	           "questions": {"<id>": {"choice": "A",
//	                                  "probabilities": {"A": 0.93, "B": 0.07},
//	                                  "score": 0.93}},
//	           "usage": {"input_tokens": N, "output_tokens": 0}}
//
// "score" is optional. output_tokens is 0: nothing is generated.
//
// Generic logprobs path, POST {base}/v1/chat/completions with
// {"logprobs": true, "top_logprobs": N, "max_tokens": 1}; the verdict is read
// from choices[0].logprobs.content[0].top_logprobs.

// SystemOneQuestion is one named closed question in a SystemOneRequest.
type SystemOneQuestion struct {
	Name    string   `json:"name"`
	Options []string `json:"options"`
}

// SystemOneRequest is the native decision endpoint request body.
type SystemOneRequest struct {
	Model     string              `json:"model"`
	Prompt    string              `json:"prompt"`
	Questions []SystemOneQuestion `json:"questions"`
}

// SystemOneAnswer is the answer to one question.
type SystemOneAnswer struct {
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Score         *float64           `json:"score,omitempty"`
}

// SystemOneUsage reports token counts; OutputTokens is 0 for this endpoint.
type SystemOneUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// SystemOneResponse is the native decision endpoint response body.
type SystemOneResponse struct {
	Model     string                     `json:"model"`
	Questions map[string]SystemOneAnswer `json:"questions"`
	Usage     SystemOneUsage             `json:"usage"`
}

// TokenLogprob is one candidate token and its log probability.
type TokenLogprob struct {
	Token       string         `json:"token"`
	Logprob     float64        `json:"logprob"`
	TopLogprobs []TokenLogprob `json:"top_logprobs,omitempty"`
}

// LogprobsChoice is one choice of a chat completion that carries logprobs.
type LogprobsChoice struct {
	Index   int `json:"index"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
	Logprobs     *struct {
		Content []TokenLogprob `json:"content"`
	} `json:"logprobs"`
}

// LogprobsResponse is the generic OpenAI-compatible chat completion response.
type LogprobsResponse struct {
	Model   string           `json:"model"`
	Choices []LogprobsChoice `json:"choices"`
}

// HasLogprobs reports whether the response carries first-token candidates.
func (r LogprobsResponse) HasLogprobs() bool {
	if len(r.Choices) == 0 || r.Choices[0].Logprobs == nil {
		return false
	}
	c := r.Choices[0].Logprobs.Content
	return len(c) > 0 && len(c[0].TopLogprobs) > 0
}
