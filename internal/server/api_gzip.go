// gzip compression for authenticated /api/* responses (BL432 / GH#242).
//
// gzipFileServer only ever covered the PWA's static files, so large JSON
// bodies such as GET /api/sessions went over the wire uncompressed (the
// mobile audit measured 3.0 MB). apiGzipMiddleware wraps apiMux and
// compresses a response only once it is known to be both large enough
// and JSON-ish:
//
//   - the first server.api_gzip_min_bytes bytes are buffered; small
//     bodies are written out unchanged with their headers untouched;
//   - only application/json, application/*+json, text/plain and YAML
//     are compressed — anything else is flushed through as-is;
//   - streams and connection takeovers are never compressed: WebSocket
//     upgrades and Hijack callers, text/event-stream, a Flush before the
//     threshold (commits uncompressed), HEAD, 1xx/204/206/304, responses
//     that already carry Content-Encoding or Content-Disposition, file
//     downloads, and the /api/proxy/ forwarding paths (their bodies are
//     a peer's, already decoded by Go's transport; re-encoding them here
//     would only fight streaming proxies such as the LLM proxy).
//
// Both knobs are read from the live config on every request so
// PUT /api/config takes effect without a restart. The gzip writers come
// from the same gzipPool gzipFileServer uses.

package server

import (
	"bufio"
	"compress/gzip"
	"mime"
	"net"
	"net/http"
	"strings"
)

const defaultAPIGzipMinBytes = 1024

// apiGzipSettings reads server.api_gzip / server.api_gzip_min_bytes from
// the live config.
func (s *Server) apiGzipSettings() (enabled bool, minBytes int) {
	if s.cfg == nil {
		return true, defaultAPIGzipMinBytes
	}
	minBytes = s.cfg.Server.APIGzipMinBytes
	if minBytes < 0 {
		minBytes = 0
	}
	return s.cfg.Server.APIGzip, minBytes
}

// apiGzipMiddleware wraps next (apiMux) with threshold-gated gzip.
func (s *Server) apiGzipMiddleware(next http.Handler) http.Handler {
	return newAPIGzipHandler(s.apiGzipSettings, next)
}

// newAPIGzipHandler is apiGzipMiddleware with the settings source
// injectable, so tests can flip the knobs between requests.
func newAPIGzipHandler(settings func() (bool, int), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enabled, minBytes := settings()
		if apiGzipSkipRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		// Handler output can echo request values; never let a browser
		// sniff it into something renderable, whether or not it ends up
		// compressed.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		compress := enabled
		if compress {
			addVaryAcceptEncoding(w.Header())
			compress = r.Method != http.MethodHead && acceptsGzip(r)
		}
		cw := &apiGzipWriter{rw: w, minBytes: minBytes, noGzip: !compress}
		defer cw.release()
		next.ServeHTTP(cw, r)
		cw.finish()
	})
}

// apiGzipSkipRequest reports requests that must bypass the middleware
// entirely: WebSocket (or any) protocol upgrades, the proxy forwarding
// paths and binary file downloads.
func apiGzipSkipRequest(r *http.Request) bool {
	if r.Header.Get("Upgrade") != "" {
		return true
	}
	p := r.URL.Path
	return strings.HasPrefix(p, "/api/proxy/") || p == "/api/files/download"
}

// addVaryAcceptEncoding adds Accept-Encoding to Vary unless present.
func addVaryAcceptEncoding(h http.Header) {
	for _, v := range h.Values("Vary") {
		for _, part := range strings.Split(v, ",") {
			p := strings.TrimSpace(part)
			if p == "*" || strings.EqualFold(p, "Accept-Encoding") {
				return
			}
		}
	}
	h.Add("Vary", "Accept-Encoding")
}

// apiGzipCompressibleType reports whether a Content-Type is JSON-ish.
func apiGzipCompressibleType(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		mt = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	}
	switch mt {
	case "application/json", "text/plain", "application/yaml", "application/x-yaml", "text/yaml":
		return true
	}
	return strings.HasPrefix(mt, "application/") && strings.HasSuffix(mt, "+json")
}

// safeContentType returns the Content-Type to set for a response that has
// none yet, taken from the body's first bytes. Types a browser would render
// as markup (HTML, XML) become text/plain, so a body that echoes request
// input is never served as a document. It returns "" for a response that
// already carries a Content-Type or Content-Encoding, or has no body yet.
func safeContentType(h http.Header, body []byte) string {
	if len(body) == 0 || h.Get("Content-Type") != "" || h.Get("Content-Encoding") != "" {
		return ""
	}
	ct := http.DetectContentType(body)
	if mt, _, err := mime.ParseMediaType(ct); err == nil {
		switch mt {
		case "text/html", "text/xml", "image/svg+xml":
			ct = "text/plain; charset=utf-8"
		}
	}
	return ct
}

// setSafeContentType sets the type safeContentType picks, if any.
func setSafeContentType(h http.Header, body []byte) {
	if ct := safeContentType(h, body); ct != "" {
		h.Set("Content-Type", ct)
	}
}

// setNoSniff adds X-Content-Type-Options: nosniff unless the handler set it.
// It has to run before the underlying WriteHeader: net/http snapshots the
// header map there, so a later Set never reaches the client.
func setNoSniff(h http.Header) {
	if h.Get("X-Content-Type-Options") == "" {
		h.Set("X-Content-Type-Options", "nosniff")
	}
}

// apiGzipWriter buffers the start of a response until it can decide
// between gzip and an untouched passthrough.
type apiGzipWriter struct {
	rw       http.ResponseWriter
	minBytes int
	noGzip   bool // only guarantee an explicit Content-Type; never compress

	status      int  // status the handler asked for (0 = implicit 200)
	wroteHeader bool // handler called WriteHeader or Write
	committed   bool // headers sent downstream; decision made
	buf         []byte
	gz          *gzip.Writer // non-nil once compressing
	hijacked    bool
}

func (w *apiGzipWriter) Header() http.Header { return w.rw.Header() }

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *apiGzipWriter) Unwrap() http.ResponseWriter { return w.rw }

func (w *apiGzipWriter) WriteHeader(code int) {
	if w.committed || w.wroteHeader {
		return
	}
	// Informational responses (100 Continue, 103 Early Hints) go
	// straight through; the final status still follows.
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		w.rw.WriteHeader(code)
		return
	}
	w.status = code
	w.wroteHeader = true
	if !w.mayCompress() {
		// Nothing is buffered before the first WriteHeader, so this only
		// sends the status line and cannot fail on a write.
		_ = w.commitPassthrough()
	}
}

func (w *apiGzipWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.gz != nil {
		return w.gz.Write(b)
	}
	if w.committed {
		return w.forward(b)
	}
	w.buf = append(w.buf, b...)
	if w.noGzip {
		if err := w.commitPassthrough(); err != nil {
			return 0, err
		}
		return len(b), nil
	}
	if len(w.buf) > w.minBytes {
		if err := w.decide(); err != nil {
			return 0, err
		}
	}
	return len(b), nil
}

// mayCompress reports whether, given the status and headers known so
// far, this response could still be compressed. A missing Content-Type
// stays undecided until the body is sniffed in decide.
func (w *apiGzipWriter) mayCompress() bool {
	switch w.status {
	case http.StatusSwitchingProtocols, http.StatusNoContent,
		http.StatusPartialContent, http.StatusNotModified:
		return false
	}
	h := w.rw.Header()
	if h.Get("Content-Encoding") != "" || h.Get("Content-Disposition") != "" || h.Get("Content-Range") != "" {
		return false
	}
	if ct := h.Get("Content-Type"); ct != "" && !apiGzipCompressibleType(ct) {
		return false
	}
	return true
}

// decide runs once the buffer passes the threshold.
func (w *apiGzipWriter) decide() error {
	h := w.rw.Header()
	if w.mayCompress() {
		// Same sniff net/http would apply on the first Write, done here
		// so the gzip bytes are never what gets sniffed.
		setSafeContentType(h, w.buf)
	}
	setNoSniff(h)
	if !w.mayCompress() {
		return w.commitPassthrough()
	}
	w.committed = true
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	w.rw.WriteHeader(w.statusOrOK())
	w.gz = acquireGzipWriter(w.rw)
	buf := w.buf
	w.buf = nil
	_, err := w.gz.Write(buf)
	return err
}

func (w *apiGzipWriter) statusOrOK() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// commitPassthrough sends the headers unchanged and any buffered bytes
// uncompressed; later writes go straight through.
func (w *apiGzipWriter) commitPassthrough() error {
	if w.committed {
		return nil
	}
	w.committed = true
	if w.mayCompress() {
		// Without this net/http sniffs the first Write itself and can
		// pick text/html for a body that echoes request input.
		setSafeContentType(w.rw.Header(), w.buf)
	}
	setNoSniff(w.rw.Header())
	w.rw.WriteHeader(w.statusOrOK())
	if len(w.buf) == 0 {
		return nil
	}
	buf := w.buf
	w.buf = nil
	_, err := w.forward(buf)
	return err
}

// forward writes b to the underlying writer uncompressed, pinning nosniff
// and a non-HTML Content-Type on the header map right before the write.
// After the underlying WriteHeader net/http has already snapshotted the
// headers, so on the committed path these Sets change nothing on the wire;
// the Sets that reach the client are the ones in decide and commitPassthrough.
// They are kept here so every uncompressed write site sets the headers
// itself and a Content-Type the handler set is never overridden.
func (w *apiGzipWriter) forward(b []byte) (int, error) {
	h := w.rw.Header()
	setNoSniff(h)
	if ct := safeContentType(h, b); ct != "" {
		h.Set("Content-Type", ct)
	}
	return w.rw.Write(b)
}

// Flush implements http.Flusher. Flushing before the threshold commits
// the response uncompressed, so SSE-style streams never stall in the
// buffer.
func (w *apiGzipWriter) Flush() {
	if w.hijacked {
		return
	}
	if w.gz != nil {
		_ = w.gz.Flush()
	} else if !w.committed {
		if !w.wroteHeader {
			w.status = http.StatusOK
			w.wroteHeader = true
		}
		_ = w.commitPassthrough()
	}
	_ = http.NewResponseController(w.rw).Flush()
}

// Hijack implements http.Hijacker by handing the connection straight
// to the caller; nothing buffered here is written.
func (w *apiGzipWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.rw).Hijack()
	if err == nil {
		w.hijacked = true
		w.committed = true
		w.buf = nil
	}
	return conn, rw, err
}

// finish completes the response after the handler returns: an
// under-threshold body is written as-is, a gzip stream gets its trailer.
func (w *apiGzipWriter) finish() {
	if w.hijacked {
		return
	}
	if w.gz != nil {
		_ = w.gz.Close()
		return
	}
	if !w.committed && (w.wroteHeader || len(w.buf) > 0) {
		_ = w.commitPassthrough()
	}
}

// release returns the gzip writer to the pool; deferred so it also runs
// when the handler panics.
func (w *apiGzipWriter) release() {
	if w.gz != nil {
		releaseGzipWriter(w.gz)
		w.gz = nil
	}
}
