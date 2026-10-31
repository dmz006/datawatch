package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/compute"
)

const cliTLSFingerprint = "ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89"

// computeTLSCLIDaemon records the last write and answers it the way the REST
// handler does: decode into a compute.Node, run Node.Validate, and reply 400
// with the error text if it fails.
type computeTLSCLIDaemon struct {
	mu     sync.Mutex
	method string
	path   string
	body   map[string]any
}

func withComputeTLSDaemon(t *testing.T) *computeTLSCLIDaemon {
	t.Helper()
	d := &computeTLSCLIDaemon{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		d.mu.Lock()
		d.method, d.path = r.Method, r.URL.Path
		d.body = map[string]any{}
		_ = json.Unmarshal(raw, &d.body)
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var n compute.Node
		err := json.Unmarshal(raw, &n)
		if err == nil {
			err = n.Validate()
		}
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"` + err.Error() + `"}`))
			return
		}
		_, _ = w.Write([]byte(`{"name":"` + n.Name + `"}`))
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	return d
}

// runComputeNodeWrite executes `compute-node add|update <args…>` on a root
// command and returns the combined output and the command's error.
func runComputeNodeWrite(t *testing.T, update bool, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "datawatch"}
	root.AddCommand(newComputeNodeAddCmd(update))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	verb := "add"
	if update {
		verb = "update"
	}
	root.SetArgs(append([]string{verb}, args...))
	err := root.Execute()
	return out.String(), err
}

// TestComputeNodeTLSFlagsExistOnAddAndUpdate checks both commands define the
// three flags with the right types and mention them in the help text.
func TestComputeNodeTLSFlagsExistOnAddAndUpdate(t *testing.T) {
	for _, update := range []bool{false, true} {
		cmd := newComputeNodeAddCmd(update)
		name := cmd.Name()
		for flag, typ := range map[string]string{
			"tls-ca-file":              "string",
			"tls-cert-fingerprint":     "string",
			"tls-insecure-skip-verify": "bool",
		} {
			f := cmd.Flags().Lookup(flag)
			if f == nil {
				t.Errorf("%s: missing --%s", name, flag)
				continue
			}
			if f.Value.Type() != typ {
				t.Errorf("%s --%s type = %s, want %s", name, flag, f.Value.Type(), typ)
			}
			if f.DefValue != "" && f.DefValue != "false" {
				t.Errorf("%s --%s default = %q, want empty/false", name, flag, f.DefValue)
			}
			if !strings.Contains(cmd.Long, "--"+flag) {
				t.Errorf("%s help text does not mention --%s", name, flag)
			}
		}
		skip := cmd.Flags().Lookup("tls-insecure-skip-verify").Usage
		for _, phrase := range []string{"turn certificate checking off", "audited", "PWA", "replaces the whole node"} {
			if !strings.Contains(skip, phrase) {
				t.Errorf("%s --tls-insecure-skip-verify usage lacks %q: %s", name, phrase, skip)
			}
		}
	}
}

// TestComputeNodeTLSFlagsLandInBody checks each flag arrives in the request
// body with the right JSON type, for add (POST) and update (PUT).
func TestComputeNodeTLSFlagsLandInBody(t *testing.T) {
	for _, update := range []bool{false, true} {
		verb, method, path := "add", http.MethodPost, "/api/compute/nodes"
		if update {
			verb, method, path = "update", http.MethodPut, "/api/compute/nodes/gpu-1"
		}
		t.Run(verb+"/ca and fingerprint", func(t *testing.T) {
			d := withComputeTLSDaemon(t)
			if _, err := runComputeNodeWrite(t, update, "gpu-1", "--kind", "ollama",
				"--tls-ca-file", "/etc/datawatch/ca.pem", "--tls-cert-fingerprint", cliTLSFingerprint); err != nil {
				t.Fatalf("command: %v", err)
			}
			if d.method != method || d.path != path {
				t.Errorf("request = %s %s, want %s %s", d.method, d.path, method, path)
			}
			if d.body["tls_ca_file"] != "/etc/datawatch/ca.pem" {
				t.Errorf("tls_ca_file = %v", d.body["tls_ca_file"])
			}
			if d.body["tls_cert_fingerprint"] != cliTLSFingerprint {
				t.Errorf("tls_cert_fingerprint = %v", d.body["tls_cert_fingerprint"])
			}
			if d.body["tls_insecure_skip_verify"] != false {
				t.Errorf("tls_insecure_skip_verify = %v, want bool false", d.body["tls_insecure_skip_verify"])
			}
		})
		t.Run(verb+"/skip verify", func(t *testing.T) {
			d := withComputeTLSDaemon(t)
			if _, err := runComputeNodeWrite(t, update, "gpu-1", "--kind", "ollama", "--tls-insecure-skip-verify"); err != nil {
				t.Fatalf("command: %v", err)
			}
			if d.body["tls_insecure_skip_verify"] != true {
				t.Errorf("tls_insecure_skip_verify = %v, want bool true", d.body["tls_insecure_skip_verify"])
			}
		})
		t.Run(verb+"/omitted settings are cleared", func(t *testing.T) {
			d := withComputeTLSDaemon(t)
			if _, err := runComputeNodeWrite(t, update, "gpu-1", "--kind", "ollama"); err != nil {
				t.Fatalf("command: %v", err)
			}
			if d.body["tls_ca_file"] != "" || d.body["tls_cert_fingerprint"] != "" || d.body["tls_insecure_skip_verify"] != false {
				t.Errorf("omitted TLS settings must be sent empty so update clears them: %v", d.body)
			}
		})
	}
}

// TestComputeNodeTLSValidationErrorsSurfaceUnchanged checks the server's
// Validate text is the command's error for a bad fingerprint and for skip
// combined with a CA file.
func TestComputeNodeTLSValidationErrorsSurfaceUnchanged(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"invalid fingerprint", []string{"--tls-cert-fingerprint", "abc123"}, "compute node: tls_cert_fingerprint must be 64 hex chars (SHA-256), got 6"},
		{"skip plus ca file", []string{"--tls-ca-file", "/ca.pem", "--tls-insecure-skip-verify"}, "compute node: tls_insecure_skip_verify cannot be combined with tls_ca_file or tls_cert_fingerprint (choose one: verify, pin or skip)"},
	}
	for _, tc := range cases {
		for _, update := range []bool{false, true} {
			name := tc.name + "/add"
			if update {
				name = tc.name + "/update"
			}
			t.Run(name, func(t *testing.T) {
				withComputeTLSDaemon(t)
				_, err := runComputeNodeWrite(t, update, append([]string{"gpu-1", "--kind", "ollama"}, tc.args...)...)
				if err == nil {
					t.Fatal("expected the server's validation error, got nil")
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tc.want)
				}
			})
		}
	}
}
