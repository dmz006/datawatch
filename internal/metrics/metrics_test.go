package metrics

import (
	"net/http"
	"testing"
)

func TestHandler(t *testing.T) {
	h := Handler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	var _ http.Handler = h
}
