package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"sort"
	"strings"
)

// InfraKind classifies why a classify call produced no usable verdict.
type InfraKind string

// Infra error kinds. Every kind is turned into a block with the "infra"
// category by the caller; none is ever a pass.
const (
	KindMissingScores InfraKind = "missing_scores"
	KindTimeout       InfraKind = "timeout"
	KindTransport     InfraKind = "transport"
	KindAbstain       InfraKind = "abstain"
	KindBadRequest    InfraKind = "bad_request"
)

// InfraError is the typed failure of a classify call. The caller blocks.
type InfraError struct {
	Kind        InfraKind
	Node, Model string
	Cause       error
}

func (e *InfraError) Error() string {
	s := fmt.Sprintf("classifier: %s on node %q model %q", e.Kind, e.Node, e.Model)
	if e.Cause != nil {
		s += ": " + e.Cause.Error()
	}
	return s
}

// Unwrap returns the underlying cause.
func (e *InfraError) Unwrap() error { return e.Cause }

// IsInfraError reports whether err is (or wraps) an InfraError, and of which kind.
func IsInfraError(err error) (InfraKind, bool) {
	var ie *InfraError
	if errors.As(err, &ie) {
		return ie.Kind, true
	}
	return "", false
}

// Route names where a call ran.
type Route struct {
	Node  string `json:"node"`
	Model string `json:"model"`
}

// Request is one closed-option classification.
type Request struct {
	Node, Model string
	// Mode selects the read path: PathNative or PathLogprobs.
	Mode Path
	// Stem is the question text placed before the options.
	Stem string
	// Options are the option texts. They are lettered A, B, C... in order and
	// the set is fixed for the call.
	Options []string
}

// Result is a usable verdict.
type Result struct {
	// Chosen is the winning option letter.
	Chosen string `json:"chosen"`
	// Probabilities maps each option letter (in the caller's order) to its probability.
	Probabilities map[string]float64 `json:"probabilities"`
	Route         Route              `json:"route"`
	Mode          Path               `json:"mode"`
}

// OptionLetters returns the letters assigned to n options.
func OptionLetters(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = string(rune('A' + i))
	}
	return out
}

// BuildPrompt builds the closed-option prompt: the stem, one lettered line per
// option, and an "Answer:" cue. The option set is fixed.
func BuildPrompt(stem string, options []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(stem, "\n"))
	b.WriteString("\n")
	for i, l := range OptionLetters(len(options)) {
		fmt.Fprintf(&b, "%s) %s\n", l, options[i])
	}
	b.WriteString("Answer:")
	return b.String()
}

// Client is the classifier client.
type Client struct {
	http    HTTPDoer
	baseURL BaseURLFunc
	gate    Gate
	prober  *Prober
	cfg     ClientConfig
}

// NewClient builds a Client. gate is required (a nil gate fails every call as a
// GateError); prober may be nil to skip the capability check; a nil client uses
// http.DefaultClient.
func NewClient(client HTTPDoer, baseURL BaseURLFunc, gate Gate, prober *Prober, cfg ClientConfig) *Client {
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{http: client, baseURL: baseURL, gate: gate, prober: prober, cfg: cfg.withDefaults()}
}

// Classify runs one classification. Any failure is returned as an *InfraError,
// *CapabilityError or *GateError; there is no fallback to generate-and-parse.
func (c *Client) Classify(ctx context.Context, req Request) (*Result, error) {
	if len(req.Options) < 2 || len(req.Options) > 26 || strings.TrimSpace(req.Stem) == "" {
		return nil, c.infra(req, KindBadRequest, fmt.Errorf("need a stem and 2-26 options"))
	}
	if req.Mode != PathNative && req.Mode != PathLogprobs {
		return nil, c.infra(req, KindBadRequest, fmt.Errorf("unknown mode %q", req.Mode))
	}
	if c.prober != nil {
		if err := c.prober.Require(ctx, req.Node, req.Model, req.Mode); err != nil {
			return nil, err
		}
	}
	var res *Result
	err := WithGate(ctx, c.gate, req.Node, req.Model, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
		defer cancel()
		var err error
		if req.Mode == PathNative {
			res, err = c.native(ctx, req)
		} else {
			res, err = c.generic(ctx, req)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Client) infra(req Request, k InfraKind, cause error) *InfraError {
	return &InfraError{Kind: k, Node: req.Node, Model: req.Model, Cause: cause}
}

func (c *Client) post(ctx context.Context, req Request, path string, body any) ([]byte, error) {
	base, err := c.baseURL(req.Node)
	if err != nil {
		return nil, c.infra(req, KindTransport, fmt.Errorf("resolve node: %w", err))
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, c.infra(req, KindBadRequest, err)
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(raw))
	if err != nil {
		return nil, c.infra(req, KindTransport, err)
	}
	hr.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(hr)
	if err != nil {
		return nil, c.infra(req, transportKind(ctx, err), err)
	}
	defer resp.Body.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, c.infra(req, transportKind(ctx, err), err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, c.infra(req, KindTransport, fmt.Errorf("status %d", resp.StatusCode))
	}
	return data, nil
}

func transportKind(ctx context.Context, err error) InfraKind {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) ||
		(errors.As(err, &ne) && ne.Timeout()) {
		return KindTimeout
	}
	return KindTransport
}

const nativeQuestion = "verdict"

func (c *Client) native(ctx context.Context, req Request) (*Result, error) {
	letters := OptionLetters(len(req.Options))
	data, err := c.post(ctx, req, "/v1/systemone", SystemOneRequest{
		Model: req.Model, Prompt: BuildPrompt(req.Stem, req.Options),
		Questions: []SystemOneQuestion{{Name: nativeQuestion, Options: letters}},
	})
	if err != nil {
		return nil, err
	}
	var out SystemOneResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, c.infra(req, KindMissingScores, fmt.Errorf("decode: %w", err))
	}
	ans, ok := out.Questions[nativeQuestion]
	if !ok || len(ans.Probabilities) == 0 {
		return nil, c.infra(req, KindMissingScores, fmt.Errorf("no scores for question"))
	}
	probs := map[string]float64{}
	for _, l := range letters {
		p, ok := ans.Probabilities[l]
		if !ok || math.IsNaN(p) || math.IsInf(p, 0) || p < 0 {
			return nil, c.infra(req, KindMissingScores, fmt.Errorf("option %s has no valid score", l))
		}
		probs[l] = p
	}
	return c.decide(req, probs, req.Mode)
}

// decide applies the abstain rules to a probability vector over letters.
func (c *Client) decide(req Request, probs map[string]float64, mode Path) (*Result, error) {
	best, top, second := rank(probs)
	if top < c.cfg.MinProbability || top-second < c.cfg.AbstainMargin {
		return nil, c.infra(req, KindAbstain, fmt.Errorf("top %.4f runner-up %.4f", top, second))
	}
	return &Result{Chosen: best, Probabilities: probs, Route: Route{req.Node, req.Model}, Mode: mode}, nil
}

// rank returns the best letter and the top and runner-up probabilities; ties
// go to the earlier letter so the result is deterministic.
func rank(probs map[string]float64) (best string, top, second float64) {
	keys := make([]string, 0, len(probs))
	for k := range probs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	top, second = -1, 0
	for _, k := range keys {
		p := probs[k]
		if p > top {
			second = math.Max(top, 0)
			best, top = k, p
		} else if p > second {
			second = p
		}
	}
	return best, top, second
}

func (c *Client) thinkingOff(model string) bool {
	m := strings.ToLower(model)
	for _, s := range c.cfg.ThinkingOffModels {
		if s != "" && strings.Contains(m, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

func (c *Client) generic(ctx context.Context, req Request) (*Result, error) {
	n := len(req.Options)
	rotations := 1
	if c.cfg.RotateOptions {
		rotations = n
	}
	letters := OptionLetters(n)
	sum := make([]float64, n) // per original option index
	for k := 0; k < rotations; k++ {
		// Rotation k places original option i at letter position (i+k)%n.
		rot := make([]string, n)
		for i, t := range req.Options {
			rot[(i+k)%n] = t
		}
		byLetter, err := c.genericOnce(ctx, req, rot, letters)
		if err != nil {
			return nil, err
		}
		for i := range req.Options {
			sum[i] += byLetter[letters[(i+k)%n]]
		}
	}
	probs := map[string]float64{}
	for i, l := range letters {
		probs[l] = sum[i] / float64(rotations)
	}
	return c.decide(req, probs, PathLogprobs)
}

func (c *Client) genericOnce(ctx context.Context, req Request, options, letters []string) (map[string]float64, error) {
	body := map[string]any{
		"model": req.Model, "max_tokens": 1, "logprobs": true, "top_logprobs": c.cfg.TopLogprobs,
		"messages": []map[string]string{{"role": "user", "content": BuildPrompt(req.Stem, options)}},
	}
	if c.thinkingOff(req.Model) {
		body["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	}
	data, err := c.post(ctx, req, "/v1/chat/completions", body)
	if err != nil {
		return nil, err
	}
	var out LogprobsResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, c.infra(req, KindMissingScores, fmt.Errorf("decode: %w", err))
	}
	if !out.HasLogprobs() {
		return nil, c.infra(req, KindMissingScores, fmt.Errorf("response carries no logprobs"))
	}
	probs, mass := ScoreFirstToken(out.Choices[0].Logprobs.Content[0].TopLogprobs, letters)
	if mass < c.cfg.MinOptionMass {
		return nil, c.infra(req, KindMissingScores, fmt.Errorf("option tokens hold %.4f of the first-token mass", mass))
	}
	// Renormalise over the option set.
	for l := range probs {
		probs[l] /= mass
	}
	return probs, nil
}

// NormalizeToken maps a first-token variant to an option letter: surrounding
// whitespace, SentencePiece/BPE space markers, brackets and trailing
// punctuation are stripped and case is folded. It returns "" when the token is
// not a single letter.
func NormalizeToken(tok string) string {
	t := strings.Trim(tok, " \t\n▁Ġ([{\"'")
	t = strings.TrimRight(t, " .:)]}\"'")
	if len(t) != 1 {
		return ""
	}
	return strings.ToUpper(t)
}

// ScoreFirstToken sums the probability of every token variant of each option
// letter in the candidate list. It returns the unnormalised per-letter
// probabilities and their total mass.
func ScoreFirstToken(top []TokenLogprob, letters []string) (map[string]float64, float64) {
	want := map[string]bool{}
	probs := map[string]float64{}
	for _, l := range letters {
		want[l] = true
		probs[l] = 0
	}
	mass := 0.0
	for _, t := range top {
		l := NormalizeToken(t.Token)
		if !want[l] || math.IsNaN(t.Logprob) {
			continue
		}
		p := math.Exp(t.Logprob)
		probs[l] += p
		mass += p
	}
	return probs, mass
}
