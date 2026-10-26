package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

func newSelfSignedPSServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"m1","size":10,"size_vram":5}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeNodeModels_SelfSignedFailsByDefault(t *testing.T) {
	srv := newSelfSignedPSServer(t)
	n := compute.Node{Name: "n", Kind: compute.KindOllama, Address: srv.URL}
	_, probed, err := probeNodeModels(context.Background(), nil, n)
	if !probed || err == nil {
		t.Fatalf("want probe failure on self-signed cert, got probed=%v err=%v", probed, err)
	}
}

func TestProbeNodeModels_SelfSignedSucceedsWithOptOut(t *testing.T) {
	srv := newSelfSignedPSServer(t)
	n := compute.Node{Name: "n", Kind: compute.KindOllama, Address: srv.URL, TLSInsecureSkipVerify: true}
	models, probed, err := probeNodeModels(context.Background(), nil, n)
	if err != nil || !probed || len(models) != 1 || models[0].Name != "m1" {
		t.Fatalf("opt-out probe: models=%v probed=%v err=%v", models, probed, err)
	}
}
