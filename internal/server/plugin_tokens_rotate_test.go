// Per-secret plugin token rotate + delete:
//   POST   /api/plugins/{name}/tokens/{secret}/rotate
//   DELETE /api/plugins/{name}/tokens/{secret}
//
// Operator-authenticated only. The secret must be one the plugin's
// manifest actually references — anything else is refused. The rotate
// response never carries the secret value. Both actions land in the
// audit log.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/secrets"
)

// recordingSecretStore implements the local secretsStore interface and
// records Set/Delete calls so tests can assert on them.
type recordingSecretStore struct {
	values  map[string]string
	deleted []string
}

func newRecordingSecretStore(initial map[string]string) *recordingSecretStore {
	values := map[string]string{}
	for k, v := range initial {
		values[k] = v
	}
	return &recordingSecretStore{values: values}
}

func (f *recordingSecretStore) List() ([]secrets.Secret, error) {
	out := make([]secrets.Secret, 0, len(f.values))
	for n, v := range f.values {
		out = append(out, secrets.Secret{Name: n, Value: v})
	}
	return out, nil
}

func (f *recordingSecretStore) Get(name string) (secrets.Secret, error) {
	v, ok := f.values[name]
	if !ok {
		return secrets.Secret{}, secrets.ErrSecretNotFound
	}
	return secrets.Secret{Name: name, Value: v}, nil
}

func (f *recordingSecretStore) Set(name, value string, _ []string, _ string, _ []string) error {
	f.values[name] = value
	return nil
}

func (f *recordingSecretStore) Delete(name string) error {
	if _, ok := f.values[name]; !ok {
		return secrets.ErrSecretNotFound
	}
	delete(f.values, name)
	f.deleted = append(f.deleted, name)
	return nil
}

func (f *recordingSecretStore) Exists(name string) (bool, error) {
	_, ok := f.values[name]
	return ok, nil
}

func rotateEnv(t *testing.T) (*pluginTokenEnv, *recordingSecretStore) {
	t.Helper()
	e := newPluginTokenEnv(t)
	rec := newRecordingSecretStore(map[string]string{"svc_token": "OLD-SECRET-VALUE"})
	e.s.secretsStore = rec
	return e, rec
}

func rotateSecretDo(t *testing.T, e *pluginTokenEnv, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+pluginTokenAdmin)
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, req)
	return rr
}

func TestPluginToken_RotateStoresNewValue(t *testing.T) {
	e, rec := rotateEnv(t)
	rr := rotateSecretDo(t, e, http.MethodPost, "/api/plugins/svc/tokens/svc_token/rotate", `{"value":"NEW-ROTATED-SECRET-XYZ"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
	if got := rec.values["svc_token"]; got != "NEW-ROTATED-SECRET-XYZ" {
		t.Errorf("stored value = %q, want NEW-ROTATED-SECRET-XYZ", got)
	}
}

func TestPluginToken_RotateResponseHasNoValue(t *testing.T) {
	e, _ := rotateEnv(t)
	newValue := "NEW-ROTATED-SECRET-XYZ"
	rr := rotateSecretDo(t, e, http.MethodPost, "/api/plugins/svc/tokens/svc_token/rotate", `{"value":"`+newValue+`"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
	body := rr.Body.String()
	if strings.Contains(body, newValue) {
		t.Errorf("response body echoes the new secret value: %q", body)
	}
	if strings.Contains(body, "\"value\"") {
		t.Errorf("response body carries a value field: %q", body)
	}
	if strings.Contains(body, "OLD-SECRET-VALUE") {
		t.Errorf("response body echoes the old secret value: %q", body)
	}
}

func TestPluginToken_RotateRotatesRegisteredServiceToken(t *testing.T) {
	e, _ := rotateEnv(t)
	// Register a service token under the same name as the secret being
	// rotated; the sibling tokens must come out untouched.
	oldTok, err := e.s.serviceTokenStore.Mint("svc_token", "test")
	if err != nil {
		t.Fatal(err)
	}
	svcTok := e.tokens["svc"]
	otherTok := e.tokens["other"]

	rr := rotateSecretDo(t, e, http.MethodPost, "/api/plugins/svc/tokens/svc_token/rotate", `{"value":"NEW-ROTATED-SECRET-XYZ"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}

	// Old token value must no longer resolve.
	if name, ok := e.s.serviceTokenStore.Lookup(oldTok); ok {
		t.Errorf("old service token for svc_token still resolves to %q", name)
	}
	// A token is still registered under the secret's name (the fresh
	// mint), and it is not the old value.
	found := false
	for _, tk := range e.s.serviceTokenStore.List() {
		if tk.Name == "svc_token" {
			found = true
		}
	}
	if !found {
		t.Errorf("no service token registered under svc_token after rotation")
	}
	// No other token was touched.
	if name, ok := e.s.serviceTokenStore.Lookup(svcTok); !ok || name != "svc" {
		t.Errorf("token for svc disturbed: ok=%v name=%q", ok, name)
	}
	if name, ok := e.s.serviceTokenStore.Lookup(otherTok); !ok || name != "other" {
		t.Errorf("token for other disturbed: ok=%v name=%q", ok, name)
	}
}

func TestPluginToken_RotateRequiresValue(t *testing.T) {
	e, rec := rotateEnv(t)
	rr := rotateSecretDo(t, e, http.MethodPost, "/api/plugins/svc/tokens/svc_token/rotate", `{}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body %q", rr.Code, rr.Body)
	}
	if got := rec.values["svc_token"]; got != "OLD-SECRET-VALUE" {
		t.Errorf("value changed to %q, want OLD-SECRET-VALUE", got)
	}
}

func TestPluginToken_RotateRefusesUnreferencedSecret(t *testing.T) {
	e, rec := rotateEnv(t)
	rr := rotateSecretDo(t, e, http.MethodPost, "/api/plugins/svc/tokens/no_such_secret/rotate", `{"value":"x"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body %q", rr.Code, rr.Body)
	}
	if len(rec.deleted) != 0 || rec.values["no_such_secret"] != "" {
		t.Errorf("unreferenced secret was touched: %+v", rec)
	}

	// A plugin with no secret refs at all must refuse as well.
	rr = rotateSecretDo(t, e, http.MethodPost, "/api/plugins/plain/tokens/svc_token/rotate", `{"value":"x"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body %q", rr.Code, rr.Body)
	}
}

func TestPluginToken_RotateUnknownPlugin(t *testing.T) {
	e, _ := rotateEnv(t)
	rr := rotateSecretDo(t, e, http.MethodPost, "/api/plugins/gone/tokens/svc_token/rotate", `{"value":"x"}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body %q", rr.Code, rr.Body)
	}
}

func TestPluginToken_RotateMethodNotAllowed(t *testing.T) {
	e, _ := rotateEnv(t)
	rr := rotateSecretDo(t, e, http.MethodGet, "/api/plugins/svc/tokens/svc_token/rotate", "")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405, body %q", rr.Code, rr.Body)
	}
}

func TestPluginToken_RemoveDeletesSecret(t *testing.T) {
	e, rec := rotateEnv(t)
	rr := rotateSecretDo(t, e, http.MethodDelete, "/api/plugins/svc/tokens/svc_token", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
	if len(rec.deleted) != 1 || rec.deleted[0] != "svc_token" {
		t.Errorf("deleted = %v, want [svc_token]", rec.deleted)
	}
	if _, ok := rec.values["svc_token"]; ok {
		t.Errorf("secret svc_token still present after delete")
	}
}

func TestPluginToken_RemoveRefusesUnreferencedSecret(t *testing.T) {
	e, rec := rotateEnv(t)
	rr := rotateSecretDo(t, e, http.MethodDelete, "/api/plugins/svc/tokens/no_such_secret", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body %q", rr.Code, rr.Body)
	}
	if len(rec.deleted) != 0 {
		t.Errorf("unreferenced secret was deleted: %v", rec.deleted)
	}

	rr = rotateSecretDo(t, e, http.MethodDelete, "/api/plugins/plain/tokens/svc_token", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body %q", rr.Code, rr.Body)
	}
}

func TestPluginToken_RemoveUnknownPlugin(t *testing.T) {
	e, rec := rotateEnv(t)
	rr := rotateSecretDo(t, e, http.MethodDelete, "/api/plugins/gone/tokens/svc_token", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body %q", rr.Code, rr.Body)
	}
	if len(rec.deleted) != 0 {
		t.Errorf("unknown plugin still deleted a secret: %v", rec.deleted)
	}
}

func TestPluginToken_RemoveMethodNotAllowed(t *testing.T) {
	e, rec := rotateEnv(t)
	rr := rotateSecretDo(t, e, http.MethodPost, "/api/plugins/svc/tokens/svc_token", `{"value":"x"}`)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405, body %q", rr.Code, rr.Body)
	}
	if len(rec.deleted) != 0 {
		t.Errorf("secret deleted via wrong method: %v", rec.deleted)
	}
}

func TestPluginToken_RotateAndRemoveAreAudited(t *testing.T) {
	e, _ := rotateEnv(t)
	l, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	e.s.SetAuditLog(l)

	rr := rotateSecretDo(t, e, http.MethodPost, "/api/plugins/svc/tokens/svc_token/rotate", `{"value":"NEW-ROTATED-SECRET-XYZ"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, body %q", rr.Code, rr.Body)
	}
	rr = rotateSecretDo(t, e, http.MethodDelete, "/api/plugins/svc/tokens/svc_token", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("remove status = %d, body %q", rr.Code, rr.Body)
	}

	entries, err := l.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	sawRotate, sawRemove := false, false
	for _, ent := range entries {
		switch ent.Action {
		case "plugin_token_rotate":
			sawRotate = true
			if ent.Details["plugin"] != "svc" {
				t.Errorf("rotate audit details = %v, want plugin=svc", ent.Details)
			}
		case "plugin_token_remove":
			sawRemove = true
		}
	}
	if !sawRotate {
		t.Errorf("no plugin_token_rotate audit entry")
	}
	if !sawRemove {
		t.Errorf("no plugin_token_remove audit entry")
	}

	// Audit entries must not carry the secret values either.
	for _, ent := range entries {
		data, _ := json.Marshal(ent)
		for _, leaky := range []string{"OLD-SECRET-VALUE", "NEW-ROTATED-SECRET-XYZ"} {
			if strings.Contains(string(data), leaky) {
				t.Errorf("audit entry for %q carries %q: %s", ent.Action, leaky, data)
			}
		}
	}
}
