package requestwatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// FingerprintNotLoaded is the fingerprint reported when the node does not
	// list the model among its running models.
	FingerprintNotLoaded = "not-loaded"

	// ollamaPSPath is the running-models listing, the only route this source
	// ever requests.
	ollamaPSPath = "/api/ps"
	// defaultOllamaTimeout bounds the one request.
	defaultOllamaTimeout = 5 * time.Second
	// maxOllamaBody caps how much of the response is read.
	maxOllamaBody = 1 << 20
)

// OllamaSource is a read-only Source for an Ollama node. Its only request is
// one GET of the running-models listing: it never calls a generate, chat or
// completion route, so observing a request never adds to it. The zero value
// is ready to use.
type OllamaSource struct {
	// Client sends the request. Nil uses a client with a bounded timeout.
	Client *http.Client
	// Timeout bounds the request. Zero uses a default of five seconds.
	Timeout time.Duration
	// GPU supplies the node's GPU utilisation when the node has an attached
	// observer. Nil, or ok false, leaves the sample's GPU reading unknown.
	GPU func(ctx context.Context, node NodeRef) (percent float64, ok bool)
}

// psModel is one entry of the running-models listing.
type psModel struct {
	Name          string `json:"name"`
	Model         string `json:"model"`
	ExpiresAt     string `json:"expires_at"`
	SizeVRAM      int64  `json:"size_vram"`
	ContextLength int64  `json:"context_length"`
}

// Sample reads the node's running-models listing and returns the fingerprint
// of the entry for model. An error means the node could not be read.
func (o *OllamaSource) Sample(ctx context.Context, node NodeRef, model string) (Sample, error) {
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultOllamaTimeout
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	base := ollamaBase(node.Address)
	if base == "" {
		return Sample{}, errors.New("node has no address")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+ollamaPSPath, nil)
	if err != nil {
		return Sample{}, fmt.Errorf("running models: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Sample{}, fmt.Errorf("running models: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return Sample{}, fmt.Errorf("running models: HTTP %d", resp.StatusCode)
	}
	var ps struct {
		Models []psModel `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxOllamaBody)).Decode(&ps); err != nil {
		return Sample{}, fmt.Errorf("running models: decode: %w", err)
	}

	s := Sample{Fingerprint: FingerprintNotLoaded}
	for _, m := range ps.Models {
		if matchesModel(m, model) {
			s.ModelLoaded = true
			s.Fingerprint = fmt.Sprintf("%s|%s|%d|%d", m.Name, m.ExpiresAt, m.SizeVRAM, m.ContextLength)
			break
		}
	}
	if o.GPU != nil {
		if pct, ok := o.GPU(ctx, node); ok {
			s.GPUPercent = pct
			s.GPUKnown = true
		}
	}
	return s, nil
}

// ollamaBase returns the node's base URL without a trailing slash, adding the
// http scheme when the address has none.
func ollamaBase(address string) string {
	a := strings.TrimSpace(address)
	if a == "" {
		return ""
	}
	if !strings.Contains(a, "://") {
		a = "http://" + a
	}
	return strings.TrimRight(a, "/")
}

// matchesModel reports whether a listing entry is the session's model. A
// leading provider segment on the session's model is ignored, the comparison
// ignores case, and a name without a tag means the latest tag. An empty model
// matches any entry.
func matchesModel(m psModel, model string) bool {
	if strings.TrimSpace(model) == "" {
		return true
	}
	want := modelForms(model)
	for _, have := range append(modelForms(m.Name), modelForms(m.Model)...) {
		for _, w := range want {
			if have == w {
				return true
			}
		}
	}
	return false
}

// modelForms returns the normalised spellings of a model name: as written,
// and without a leading provider segment.
func modelForms(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	forms := []string{normaliseModel(name)}
	if i := strings.Index(name, "/"); i > 0 && i < len(name)-1 {
		forms = append(forms, normaliseModel(name[i+1:]))
	}
	return forms
}

// normaliseModel lower-cases a model name and adds the latest tag when the
// last path segment has no tag.
func normaliseModel(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if !strings.Contains(name[strings.LastIndex(name, "/")+1:], ":") {
		name += ":latest"
	}
	return name
}
