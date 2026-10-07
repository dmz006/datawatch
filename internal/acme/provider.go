package acme

import (
	"net/http"
	"strings"
	"sync"

	"github.com/go-acme/lego/v4/challenge/http01"
)

// httpProvider implements challenge.Provider (Present/CleanUp) for
// HTTP-01, backed by an in-memory token->keyAuth map and an http.Handler
// the daemon registers on its EXISTING mux — per the BL397 plan's
// decision not to open a second listener on port 80. lego's own
// http01.NewProviderServer starts a standalone listener; this is a
// from-scratch alternative serving the same /.well-known/acme-challenge/
// path shape through the daemon's normal HTTP server.
type httpProvider struct {
	mu     sync.Mutex
	tokens map[string]string // token -> keyAuth
}

func newHTTPProvider() *httpProvider {
	return &httpProvider{tokens: make(map[string]string)}
}

// Present implements challenge.Provider.
func (p *httpProvider) Present(_, token, keyAuth string) error {
	p.mu.Lock()
	p.tokens[token] = keyAuth
	p.mu.Unlock()
	return nil
}

// CleanUp implements challenge.Provider.
func (p *httpProvider) CleanUp(_, token, _ string) error {
	p.mu.Lock()
	delete(p.tokens, token)
	p.mu.Unlock()
	return nil
}

// Handler returns the http.Handler to register on the daemon's existing
// mux at http01.PathPrefix ("/.well-known/acme-challenge/"). Returns 404
// for any token it doesn't currently hold — including after CleanUp, so a
// stale/replayed request doesn't leak a solved challenge's keyAuth.
func (p *httpProvider) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.URL.Path, http01.PathPrefix)
		p.mu.Lock()
		keyAuth, ok := p.tokens[token]
		p.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(keyAuth))
	})
}
