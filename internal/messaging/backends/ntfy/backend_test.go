package ntfy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A send counts as accepted only when the server itself answers with success.
// A redirect to a sign-in page that answers 200 publishes nothing.
func TestSendAcceptsOnlyASuccessFromTheServerItself(t *testing.T) {
	var elsewhere int
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		elsewhere++
		_, _ = w.Write([]byte("sign in"))
	}))
	defer login.Close()
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status >= 300 && status < 400 {
			w.Header().Set("Location", login.URL+"/login")
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	b := New(srv.URL, "secret-topic", "tok-123")
	for _, code := range []int{200, 201, 204} {
		status = code
		if err := b.Send("", "hello"); err != nil {
			t.Errorf("%d: %v", code, err)
		}
	}
	for _, code := range []int{301, 302, 303, 307, 308, 304, 401, 500} {
		status = code
		err := b.Send("", "hello")
		if err == nil {
			t.Errorf("%d read as sent", code)
			continue
		}
		if strings.Contains(err.Error(), "secret-topic") || strings.Contains(err.Error(), "tok-123") {
			t.Errorf("%d: the error names the topic or token: %v", code, err)
		}
	}
	if elsewhere != 0 {
		t.Errorf("a redirect was followed %d times", elsewhere)
	}
}
