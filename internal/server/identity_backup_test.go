// Tests for the identity wipe guard and the backup routes: the 409 for an
// empty write over a saved identity, GET /api/identity/backups,
// POST /api/identity/restore and POST /api/identity/reset, with the 503,
// 405 and federation capability guards on each.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/identity"
)

// identityHandlerFor returns the handler the route table maps path to.
func identityHandlerFor(s *Server, path string) http.HandlerFunc {
	switch path {
	case "/api/identity/backups":
		return s.handleIdentityBackups
	case "/api/identity/restore":
		return s.handleIdentityRestore
	case "/api/identity/reset":
		return s.handleIdentityReset
	}
	return s.handleIdentity
}

// idCall sends one request straight to the handler for path.
func idCall(s *Server, method, path, body string) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body != "" {
		rd = bytes.NewReader([]byte(body))
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	w := httptest.NewRecorder()
	identityHandlerFor(s, path)(w, req)
	return w
}

func idDecode(t *testing.T, w *httptest.ResponseRecorder) identity.Identity {
	t.Helper()
	var got identity.Identity
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode identity: %v body=%s", err, w.Body.String())
	}
	return got
}

// idSeed saves a first identity with a known set of fields.
func idSeed(t *testing.T, s *Server) {
	t.Helper()
	w := idCall(s, http.MethodPut, "/api/identity", `{"role":"platform engineer","values":["clarity"],"current_focus":"reliability"}`)
	if w.Code != 200 {
		t.Fatalf("seed PUT: %d %s", w.Code, w.Body.String())
	}
}

func TestIdentityEmptyPutOverSavedIs409(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	idSeed(t, s)

	w := idCall(s, http.MethodPut, "/api/identity", `{}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("empty PUT: %d (want 409) body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type %q", ct)
	}
	var body struct {
		Error      string   `json:"error"`
		Code       string   `json:"code"`
		FieldsKept []string `json:"fields_kept"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 409: %v", err)
	}
	if body.Code != "identity_wipe_refused" {
		t.Errorf("code %q", body.Code)
	}
	if !strings.Contains(body.Error, "/api/identity/reset") {
		t.Errorf("error does not point at the reset route: %q", body.Error)
	}
	want := []string{"role", "values", "current_focus"}
	if !reflect.DeepEqual(body.FieldsKept, want) {
		t.Errorf("fields_kept %v, want %v", body.FieldsKept, want)
	}

	got := idDecode(t, idCall(s, http.MethodGet, "/api/identity", ""))
	if got.Role != "platform engineer" || len(got.Values) != 1 || got.CurrentFocus != "reliability" {
		t.Errorf("identity changed by a refused write: %+v", got)
	}
}

func TestIdentityEmptyPutOverEmptyIs200(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	w := idCall(s, http.MethodPut, "/api/identity", `{}`)
	if w.Code != 200 {
		t.Fatalf("empty PUT over empty: %d body=%s", w.Code, w.Body.String())
	}
	if !idDecode(t, w).IsEmpty() {
		t.Errorf("expected empty identity: %s", w.Body.String())
	}
}

func TestIdentityEmptyPatchAndPostAreHarmless(t *testing.T) {
	for _, method := range []string{http.MethodPatch, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			s, _ := newIdentityTestServer(t)
			idSeed(t, s)
			w := idCall(s, method, "/api/identity", `{}`)
			if w.Code != 200 {
				t.Fatalf("%s: %d body=%s", method, w.Code, w.Body.String())
			}
			got := idDecode(t, w)
			if got.Role != "platform engineer" || got.CurrentFocus != "reliability" || len(got.Values) != 1 {
				t.Errorf("%s changed the identity: %+v", method, got)
			}
		})
	}
}

func TestIdentityBackupsListAfterTwoPuts(t *testing.T) {
	s, mgr := newIdentityTestServer(t)

	w := idCall(s, http.MethodGet, "/api/identity/backups", "")
	if w.Code != 200 {
		t.Fatalf("GET backups: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"backups":[]`) {
		t.Errorf("no backups should encode as an empty array: %s", w.Body.String())
	}

	idCall(s, http.MethodPut, "/api/identity", `{"role":"first"}`)
	idCall(s, http.MethodPut, "/api/identity", `{"role":"second","values":["x"]}`)

	w = idCall(s, http.MethodGet, "/api/identity/backups", "")
	var out struct {
		Backups []identity.Backup `json:"backups"`
		Keep    int               `json:"keep"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Backups) != 1 {
		t.Fatalf("want one backup, got %d: %s", len(out.Backups), w.Body.String())
	}
	b := out.Backups[0]
	if b.Role != "first" || b.FieldCount != 1 || b.SizeBytes <= 0 || b.CreatedAt.IsZero() || b.Name == "" {
		t.Errorf("backup fields: %+v", b)
	}
	if out.Keep != mgr.BackupsToKeep() || out.Keep < 1 {
		t.Errorf("keep %d, manager says %d", out.Keep, mgr.BackupsToKeep())
	}
	for _, f := range []string{`"name"`, `"created_at"`, `"size_bytes"`, `"role"`, `"field_count"`} {
		if !strings.Contains(w.Body.String(), f) {
			t.Errorf("response lacks %s: %s", f, w.Body.String())
		}
	}
}

func TestIdentityBackupsNewestFirst(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	for _, r := range []string{"a", "b", "c", "d"} {
		idCall(s, http.MethodPut, "/api/identity", `{"role":"`+r+`"}`)
	}
	var out struct {
		Backups []identity.Backup `json:"backups"`
	}
	_ = json.Unmarshal(idCall(s, http.MethodGet, "/api/identity/backups", "").Body.Bytes(), &out)
	var roles []string
	for _, b := range out.Backups {
		roles = append(roles, b.Role)
	}
	if !reflect.DeepEqual(roles, []string{"c", "b", "a"}) {
		t.Errorf("order %v, want newest first [c b a]", roles)
	}
}

func TestIdentityRestoreByNameAndLatest(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	idCall(s, http.MethodPut, "/api/identity", `{"role":"older"}`)
	idCall(s, http.MethodPut, "/api/identity", `{"role":"newer"}`)

	var out struct {
		Backups []identity.Backup `json:"backups"`
	}
	_ = json.Unmarshal(idCall(s, http.MethodGet, "/api/identity/backups", "").Body.Bytes(), &out)
	if len(out.Backups) != 1 {
		t.Fatalf("setup: %d backups", len(out.Backups))
	}
	name := out.Backups[0].Name

	w := idCall(s, http.MethodPost, "/api/identity/restore", `{"backup":"`+name+`"}`)
	if w.Code != 200 {
		t.Fatalf("restore by name: %d %s", w.Code, w.Body.String())
	}
	if got := idDecode(t, w); got.Role != "older" {
		t.Errorf("restored role %q, want older", got.Role)
	}
	if got := idDecode(t, idCall(s, http.MethodGet, "/api/identity", "")); got.Role != "older" {
		t.Errorf("GET after restore: %q", got.Role)
	}

	// The restore backed up "newer", so latest brings it back.
	w = idCall(s, http.MethodPost, "/api/identity/restore", `{"backup":"latest"}`)
	if w.Code != 200 {
		t.Fatalf("restore latest: %d %s", w.Code, w.Body.String())
	}
	if got := idDecode(t, w); got.Role != "newer" {
		t.Errorf("latest restored %q, want newer", got.Role)
	}
}

func TestIdentityRestoreErrors(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	idSeed(t, s)
	idCall(s, http.MethodPut, "/api/identity", `{"role":"changed"}`)

	cases := []struct {
		name, body string
		want       int
	}{
		{"bad json", `{not json`, http.StatusBadRequest},
		{"empty body", ``, http.StatusBadRequest},
		{"missing backup", `{}`, http.StatusBadRequest},
		{"blank backup", `{"backup":"  "}`, http.StatusBadRequest},
		{"unknown name", `{"backup":"identity-20200101T000000.000Z.yaml"}`, http.StatusNotFound},
		{"path-like name", `{"backup":"../identity.yaml"}`, http.StatusNotFound},
		{"absolute path", `{"backup":"/etc/passwd"}`, http.StatusNotFound},
		{"backslash path", `{"backup":"..\\identity.yaml"}`, http.StatusNotFound},
		{"not a backup name", `{"backup":"whatever"}`, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := idCall(s, http.MethodPost, "/api/identity/restore", c.body)
			if w.Code != c.want {
				t.Errorf("status %d, want %d body=%s", w.Code, c.want, w.Body.String())
			}
		})
	}
	if got := s.identityMgr.Get(); got.Role != "changed" {
		t.Errorf("a failed restore changed the identity: %+v", got)
	}
}

func TestIdentityRestoreLatestWithNoBackupsIs404(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	w := idCall(s, http.MethodPost, "/api/identity/restore", `{"backup":"latest"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404 body=%s", w.Code, w.Body.String())
	}
}

func TestIdentityRestoreEmptyBackupIs422(t *testing.T) {
	s, mgr := newIdentityTestServer(t)
	idSeed(t, s)
	// The manager never writes an empty backup itself, so plant one.
	dir := filepath.Join(filepath.Dir(mgr.Path()), "identity-backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "identity-20990101T000000.000Z.yaml")
	if err := os.WriteFile(empty, []byte("updated_at: 2099-01-01T00:00:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := idCall(s, http.MethodPost, "/api/identity/restore", `{"backup":"identity-20990101T000000.000Z.yaml"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422 body=%s", w.Code, w.Body.String())
	}
}

func TestIdentityResetClearsAndRestoreBringsBack(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	idSeed(t, s)

	w := idCall(s, http.MethodPost, "/api/identity/reset", "")
	if w.Code != 200 {
		t.Fatalf("reset: %d %s", w.Code, w.Body.String())
	}
	if !idDecode(t, w).IsEmpty() {
		t.Errorf("reset returned a non-empty identity: %s", w.Body.String())
	}
	if !idDecode(t, idCall(s, http.MethodGet, "/api/identity", "")).IsEmpty() {
		t.Error("identity not empty after reset")
	}

	w = idCall(s, http.MethodGet, "/api/identity/backups", "")
	if !strings.Contains(w.Body.String(), `"role":"platform engineer"`) {
		t.Fatalf("reset kept no backup of the identity: %s", w.Body.String())
	}

	w = idCall(s, http.MethodPost, "/api/identity/restore", `{"backup":"latest"}`)
	if w.Code != 200 {
		t.Fatalf("restore after reset: %d %s", w.Code, w.Body.String())
	}
	if got := idDecode(t, w); got.Role != "platform engineer" || got.CurrentFocus != "reliability" {
		t.Errorf("restore after reset: %+v", got)
	}
}

func TestIdentityResetOfEmptyIdentityIs200(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	w := idCall(s, http.MethodPost, "/api/identity/reset", "")
	if w.Code != 200 {
		t.Fatalf("reset of empty: %d %s", w.Code, w.Body.String())
	}
}

func TestIdentityNewRoutesDisabledWithoutManager(t *testing.T) {
	s := &Server{}
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/identity/backups"},
		{http.MethodPost, "/api/identity/restore"},
		{http.MethodPost, "/api/identity/reset"},
	} {
		w := idCall(s, c.method, c.path, `{"backup":"latest"}`)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: %d, want 503", c.method, c.path, w.Code)
		}
		if !strings.Contains(w.Body.String(), "identity disabled") {
			t.Errorf("%s %s body %q", c.method, c.path, w.Body.String())
		}
	}
}

func TestIdentityNewRoutesWrongMethodIs405(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	cases := map[string][]string{
		"/api/identity/backups": {http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
		"/api/identity/restore": {http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete},
		"/api/identity/reset":   {http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete},
	}
	for path, methods := range cases {
		for _, m := range methods {
			if w := idCall(s, m, path, ""); w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: %d, want 405", m, path, w.Code)
			}
		}
	}
	// Wrong method is 405 even with no manager wired.
	bare := &Server{}
	if w := idCall(bare, http.MethodGet, "/api/identity/reset", ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("no manager, GET reset: %d, want 405", w.Code)
	}
}

// TestIdentityNewRoutesFederationCaps: a read-only peer may list backups
// but is refused restore and reset (and a wipe-refused PUT); a full-control
// peer and the admin token pass.
func TestIdentityNewRoutesFederationCaps(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	mgr, err := identity.NewManager(filepath.Join(t.TempDir(), "identity.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s.identityMgr = IdentityManagerAdapter{M: mgr}
	if _, err := mgr.Set(identity.Identity{Role: "seed"}); err != nil {
		t.Fatal(err)
	}

	capTest(t, s, http.MethodGet, "/api/identity/backups", nil, s.handleIdentityBackups,
		http.StatusOK, http.StatusOK, http.StatusOK)
	capTest(t, s, http.MethodPost, "/api/identity/restore", []byte(`{"backup":"latest"}`), s.handleIdentityRestore,
		http.StatusNotFound, http.StatusNotFound, http.StatusForbidden)
	capTest(t, s, http.MethodPost, "/api/identity/reset", nil, s.handleIdentityReset,
		http.StatusOK, http.StatusOK, http.StatusForbidden)
}

// TestIdentityWriteRoutesKeepErrorsAs500 pins that an error other than the
// wipe guard still answers 500 with its message.
func TestIdentityWriteRoutesKeepErrorsAs500(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	s.identityMgr = failingIdentityMgr{identityManager: s.identityMgr}
	for _, m := range []string{http.MethodPut, http.MethodPatch, http.MethodPost} {
		w := idCall(s, m, "/api/identity", `{"role":"x"}`)
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "disk on fire") {
			t.Errorf("%s: %d %q", m, w.Code, w.Body.String())
		}
	}
	if w := idCall(s, http.MethodPost, "/api/identity/reset", ""); w.Code != 500 {
		t.Errorf("reset: %d", w.Code)
	}
	if w := idCall(s, http.MethodGet, "/api/identity/backups", ""); w.Code != 500 {
		t.Errorf("backups: %d", w.Code)
	}
	if w := idCall(s, http.MethodPost, "/api/identity/restore", `{"backup":"latest"}`); w.Code != 500 {
		t.Errorf("restore: %d", w.Code)
	}
}

// failingIdentityMgr fails every operation with a plain error.
type failingIdentityMgr struct{ identityManager }

var errDisk = &diskErr{}

type diskErr struct{}

func (*diskErr) Error() string { return "disk on fire" }

func (failingIdentityMgr) Set(identity.Identity) (identity.Identity, error) {
	return identity.Identity{}, errDisk
}
func (failingIdentityMgr) Update(identity.Identity) (identity.Identity, error) {
	return identity.Identity{}, errDisk
}
func (failingIdentityMgr) Reset() (identity.Identity, error)       { return identity.Identity{}, errDisk }
func (failingIdentityMgr) ListBackups() ([]identity.Backup, error) { return nil, errDisk }
func (failingIdentityMgr) Restore(string) (identity.Identity, error) {
	return identity.Identity{}, errDisk
}
