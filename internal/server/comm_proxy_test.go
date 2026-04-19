// BL102 — REST handler tests for /api/proxy/comm/{channel}/send.

package server

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/messaging"
)

// fakeBackend records what Send was called with.
type fakeBackend struct {
	name      string
	gotRecip  string
	gotMsg    string
	sendErr   error
}

func (f *fakeBackend) Name() string                                { return f.name }
func (f *fakeBackend) Send(recip, msg string) error {
	f.gotRecip = recip
	f.gotMsg = msg
	return f.sendErr
}
func (f *fakeBackend) Subscribe(ctx context.Context, h func(messaging.Message)) error {
	return nil
}
func (f *fakeBackend) Link(deviceName string, onQR func(string)) error { return nil }
func (f *fakeBackend) SelfID() string                                  { return "" }
func (f *fakeBackend) Close() error                                    { return nil }

func TestHandleCommProxy_HappyPath(t *testing.T) {
	fb := &fakeBackend{name: "signal"}
	s := &Server{
		commBackends: map[string]messaging.Backend{"signal": fb},
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/proxy/comm/signal/send",
		bytes.NewBufferString(`{"recipient":"+15555550100","message":"build failed"}`))
	rr := httptest.NewRecorder()
	s.handleCommProxySend(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if fb.gotRecip != "+15555550100" || fb.gotMsg != "build failed" {
		t.Errorf("backend called with recip=%q msg=%q", fb.gotRecip, fb.gotMsg)
	}
}

func TestHandleCommProxy_DefaultRecipient(t *testing.T) {
	fb := &fakeBackend{name: "telegram"}
	s := &Server{
		commBackends: map[string]messaging.Backend{"telegram": fb},
		commDefaults: map[string]string{"telegram": "default-chat-id"},
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/proxy/comm/telegram/send",
		bytes.NewBufferString(`{"message":"hi"}`))
	rr := httptest.NewRecorder()
	s.handleCommProxySend(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if fb.gotRecip != "default-chat-id" {
		t.Errorf("default recipient not used: gotRecip=%q", fb.gotRecip)
	}
}

func TestHandleCommProxy_UnknownChannel_404(t *testing.T) {
	s := &Server{
		commBackends: map[string]messaging.Backend{"signal": &fakeBackend{name: "signal"}},
	}
	req := httptest.NewRequest(http.MethodPost,
		"/api/proxy/comm/nope/send",
		bytes.NewBufferString(`{"recipient":"x","message":"y"}`))
	rr := httptest.NewRecorder()
	s.handleCommProxySend(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rr.Code)
	}
}

func TestHandleCommProxy_NoRegistry_503(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost,
		"/api/proxy/comm/signal/send",
		bytes.NewBufferString(`{"recipient":"x","message":"y"}`))
	rr := httptest.NewRecorder()
	s.handleCommProxySend(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want 503", rr.Code)
	}
}

func TestHandleCommProxy_MissingMessage_400(t *testing.T) {
	s := &Server{
		commBackends: map[string]messaging.Backend{"signal": &fakeBackend{name: "signal"}},
	}
	req := httptest.NewRequest(http.MethodPost,
		"/api/proxy/comm/signal/send",
		bytes.NewBufferString(`{"recipient":"x"}`))
	rr := httptest.NewRecorder()
	s.handleCommProxySend(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}

func TestHandleCommProxy_MissingRecipient_400(t *testing.T) {
	// Backend registered but no default + no recipient in body.
	s := &Server{
		commBackends: map[string]messaging.Backend{"signal": &fakeBackend{name: "signal"}},
	}
	req := httptest.NewRequest(http.MethodPost,
		"/api/proxy/comm/signal/send",
		bytes.NewBufferString(`{"message":"x"}`))
	rr := httptest.NewRecorder()
	s.handleCommProxySend(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}

func TestHandleCommProxy_BackendError_502(t *testing.T) {
	fb := &fakeBackend{name: "signal", sendErr: errors.New("rate limit")}
	s := &Server{
		commBackends: map[string]messaging.Backend{"signal": fb},
	}
	req := httptest.NewRequest(http.MethodPost,
		"/api/proxy/comm/signal/send",
		bytes.NewBufferString(`{"recipient":"x","message":"y"}`))
	rr := httptest.NewRecorder()
	s.handleCommProxySend(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Errorf("status=%d want 502", rr.Code)
	}
}

func TestHandleCommProxy_RejectsGet(t *testing.T) {
	s := &Server{commBackends: map[string]messaging.Backend{"signal": &fakeBackend{name: "signal"}}}
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/comm/signal/send", nil)
	rr := httptest.NewRecorder()
	s.handleCommProxySend(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want 405", rr.Code)
	}
}
