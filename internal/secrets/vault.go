// BL267 (v6.15.0) — HashiCorp Vault / OpenBao backend for the centralized
// secrets manager.
//
// Design captured in the BL267 interview (CHANGELOG v6.15.0):
//
//	auth         static token only in v1; AppRole + Kubernetes deferred (BL281)
//	kv version   v2 only (no v1 fallback)
//	path layout  flat default ("<mount>/data/<prefix>/<name>");
//	             tag_aware opt-in inserts the secret's first tag as a sub-folder.
//	             BL283 deferred: per-actor tokens + Vault-side scope policies.
//	write gate   actor-aware via the existing CallerCtx; agents read-only.
//	caching      none — every read hits Vault. BL282 deferred: cache + invalidate.
//	failure      fail-closed; no fallback to other backends. BL285 deferred: alerts.
//	scopes       defense-in-depth — daemon CheckScope fires first, then Vault read.
//	tls          system CA + optional CA file + tls_skip_verify escape hatch.
//	audit        every Vault op records X-Vault-Request-ID + status into the
//	             audit log Details map so investigators can pivot.
//
// The Store interface contract is unchanged; VaultStore is a drop-in for
// BuiltinStore / KeePassStore / OnePasswordStore. The actor-aware write
// gate sits OUTSIDE this file (in the secrets-manager wrapper that owns
// CallerCtx); this layer just talks Vault HTTP.

package secrets

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// VaultStore implements Store against a HashiCorp Vault / OpenBao server
// using the KV v2 secret engine.
type VaultStore struct {
	address     string
	namespace   string
	token       string
	kvMount     string
	pathPrefix  string
	pathLayout  string // "flat" | "tag_aware"
	httpClient  *http.Client
	timeout     time.Duration

	// LastError + LastSuccess support the Status surface (PWA card +
	// nav health badge). Updated on every operation; race-safe enough
	// for status-display purposes (single-writer in practice).
	lastError    string
	lastSuccess  time.Time
	lastReqID    string
}

// NewVaultStore builds a VaultStore from a VaultConfig-equivalent set of
// fields. Returns an error when the address is empty or the auth method
// is unsupported in v1.
//
// Caller is responsible for passing a non-empty token (the secrets-
// manager wrapper resolves ${secret:name} references and DATAWATCH_VAULT_TOKEN
// env-var fallback before calling here).
func NewVaultStore(address, namespace, authMethod, token, kvMount, pathPrefix, pathLayout, tlsCAFile string, tlsSkipVerify bool, requestTimeout time.Duration) (*VaultStore, error) {
	if address == "" {
		return nil, fmt.Errorf("vault: address is required")
	}
	if _, err := url.Parse(address); err != nil {
		return nil, fmt.Errorf("vault: address %q invalid: %w", address, err)
	}
	switch authMethod {
	case "", "token":
		// ok
	case "approle", "kubernetes":
		return nil, fmt.Errorf("vault: auth_method=%q not implemented in v6.15.0 (BL281 frozen for AppRole + Kubernetes)", authMethod)
	default:
		return nil, fmt.Errorf("vault: auth_method=%q unknown (supported: token)", authMethod)
	}
	if token == "" {
		return nil, fmt.Errorf("vault: token is required for auth_method=token (set vault.token in config or DATAWATCH_VAULT_TOKEN env)")
	}
	if kvMount == "" {
		kvMount = "secret"
	}
	if pathLayout == "" {
		pathLayout = "flat"
	}
	if pathLayout != "flat" && pathLayout != "tag_aware" {
		return nil, fmt.Errorf("vault: path_layout=%q invalid (supported: flat, tag_aware)", pathLayout)
	}
	if requestTimeout <= 0 {
		requestTimeout = 10 * time.Second
	}

	tlsCfg := &tls.Config{InsecureSkipVerify: tlsSkipVerify} // #nosec G402 -- operator opt-in
	if tlsCAFile != "" {
		caData, err := os.ReadFile(tlsCAFile)
		if err != nil {
			return nil, fmt.Errorf("vault: read tls_ca_file %q: %w", tlsCAFile, err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("vault: tls_ca_file %q contains no valid PEM certs", tlsCAFile)
		}
		tlsCfg.RootCAs = pool
	}

	return &VaultStore{
		address:    strings.TrimRight(address, "/"),
		namespace:  namespace,
		token:      token,
		kvMount:    kvMount,
		pathPrefix: pathPrefix,
		pathLayout: pathLayout,
		timeout:    requestTimeout,
		httpClient: &http.Client{
			Timeout:   requestTimeout,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
	}, nil
}

// pathFor returns the Vault KV-v2 data path for a given secret name.
// Layout-aware: flat → <mount>/data/<prefix>/<name>; tag_aware →
// <mount>/data/<prefix>/<tag>/<name> when a tag is supplied.
func (s *VaultStore) pathFor(name string, tags []string) string {
	parts := []string{s.kvMount, "data"}
	if s.pathPrefix != "" {
		parts = append(parts, s.pathPrefix)
	}
	if s.pathLayout == "tag_aware" && len(tags) > 0 && tags[0] != "" {
		parts = append(parts, tags[0])
	}
	parts = append(parts, name)
	return "/v1/" + strings.Join(parts, "/")
}

// metaPathFor returns the Vault KV-v2 metadata path for a given secret.
// Used by Delete (KV-v2 hard-delete is via DELETE on /metadata).
func (s *VaultStore) metaPathFor(name string, tags []string) string {
	dataPath := s.pathFor(name, tags)
	return strings.Replace(dataPath, "/data/", "/metadata/", 1)
}

// listPathFor returns the parent directory's metadata-list path. KV v2
// LIST is on /metadata/<dir>?list=true.
func (s *VaultStore) listPathFor() string {
	parts := []string{s.kvMount, "metadata"}
	if s.pathPrefix != "" {
		parts = append(parts, s.pathPrefix)
	}
	return "/v1/" + strings.Join(parts, "/")
}

// vaultResponse is the envelope every Vault KV-v2 call returns.
type vaultResponse struct {
	RequestID string                 `json:"request_id"`
	Data      map[string]interface{} `json:"data"`
	Errors    []string               `json:"errors,omitempty"`
}

// kvData is the inner shape of a KV-v2 read: Data.data is the user value
// map, Data.metadata holds versioning info.
type kvData struct {
	Data     map[string]string      `json:"data"`
	Metadata map[string]interface{} `json:"metadata"`
}

// do executes a Vault HTTP call and returns the parsed envelope. Records
// LastError / LastSuccess / LastReqID for the status surface.
func (s *VaultStore) do(method, path string, body interface{}) (*vaultResponse, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("vault: marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.address+path, bodyReader)
	if err != nil {
		s.recordErr("build request: " + err.Error())
		return nil, 0, err
	}
	req.Header.Set("X-Vault-Token", s.token)
	if s.namespace != "" {
		req.Header.Set("X-Vault-Namespace", s.namespace)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.recordErr("transport: " + err.Error())
		return nil, 0, fmt.Errorf("vault %s %s: %w", method, path, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		s.recordErr("read body: " + err.Error())
		return nil, resp.StatusCode, err
	}

	reqID := resp.Header.Get("X-Vault-Request-ID")

	// 204 No Content is success for DELETE.
	if resp.StatusCode == http.StatusNoContent {
		s.recordOK(reqID)
		return &vaultResponse{RequestID: reqID}, resp.StatusCode, nil
	}

	var env vaultResponse
	if len(respBody) > 0 {
		if uerr := json.Unmarshal(respBody, &env); uerr != nil {
			// Vault sometimes returns plain text on permission denied
			// — fall through with a synthetic error envelope.
			env = vaultResponse{Errors: []string{strings.TrimSpace(string(respBody))}}
		}
	}
	if env.RequestID == "" {
		env.RequestID = reqID
	}

	if resp.StatusCode >= 400 {
		errMsg := strings.Join(env.Errors, "; ")
		if errMsg == "" {
			errMsg = http.StatusText(resp.StatusCode)
		}
		// 404 means Vault answered cleanly with "no such secret" — this
		// is a normal case for Get/Exists, not a Vault health problem.
		// Record success (Vault is reachable) but still return the error
		// so the caller can distinguish ErrSecretNotFound.
		if resp.StatusCode == http.StatusNotFound {
			s.recordOK(env.RequestID)
		} else {
			s.recordErr(fmt.Sprintf("HTTP %d: %s (req_id=%s)", resp.StatusCode, errMsg, env.RequestID))
		}
		return &env, resp.StatusCode, fmt.Errorf("vault %s %s: HTTP %d: %s", method, path, resp.StatusCode, errMsg)
	}

	s.recordOK(env.RequestID)
	return &env, resp.StatusCode, nil
}

func (s *VaultStore) recordOK(reqID string) {
	s.lastSuccess = time.Now()
	s.lastReqID = reqID
	s.lastError = ""
}

func (s *VaultStore) recordErr(msg string) {
	s.lastError = msg
}

// VaultStatus is the operator-facing health summary for the Settings →
// Secrets Manager card and the nav health badge.
type VaultStatus struct {
	Reachable    bool      `json:"reachable"`
	Address      string    `json:"address"`
	Namespace    string    `json:"namespace,omitempty"`
	KVMount      string    `json:"kv_mount"`
	PathPrefix   string    `json:"path_prefix,omitempty"`
	PathLayout   string    `json:"path_layout"`
	LastSuccess  time.Time `json:"last_success,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	LastRequestID string   `json:"last_request_id,omitempty"`
}

// Status returns a snapshot of Vault connectivity. Used by the PWA card
// + the nav badge to render Vault's health without making a fresh round-
// trip on every paint. Operators that want a real probe call
// CheckHealth().
func (s *VaultStore) Status() VaultStatus {
	return VaultStatus{
		Reachable:     s.lastError == "" && !s.lastSuccess.IsZero(),
		Address:       s.address,
		Namespace:     s.namespace,
		KVMount:       s.kvMount,
		PathPrefix:    s.pathPrefix,
		PathLayout:    s.pathLayout,
		LastSuccess:   s.lastSuccess,
		LastError:     s.lastError,
		LastRequestID: s.lastReqID,
	}
}

// CheckHealth pings /v1/sys/health to test connectivity + token. Used by
// the PWA's "Test connection" affordance and by the daemon-startup
// validation pass. Returns nil on success, error otherwise.
func (s *VaultStore) CheckHealth() error {
	_, code, err := s.do(http.MethodGet, "/v1/sys/health", nil)
	if err != nil {
		return err
	}
	// /v1/sys/health returns 200 (initialized + unsealed + active),
	// 429 (standby), 472 (DR replication), 473 (perf standby), 501
	// (uninitialized), 503 (sealed). For datawatch's purposes,
	// 200 + 429 + 472 + 473 are all "Vault answered".
	switch code {
	case 200, 429, 472, 473:
		return nil
	case 501:
		return fmt.Errorf("vault: not initialized")
	case 503:
		return fmt.Errorf("vault: sealed")
	default:
		return fmt.Errorf("vault: unexpected health status %d", code)
	}
}

// LastRequestID returns the Vault X-Vault-Request-ID from the most
// recent operation. Plumbed into the audit log Details map so an
// investigator can pivot from a datawatch audit row to the matching
// Vault audit row.
func (s *VaultStore) LastRequestID() string { return s.lastReqID }

// ── Store interface ──────────────────────────────────────────────────────

// List returns every secret under the configured KV mount + prefix.
// Names only — values are NOT fetched (operator UI lists names, fetches
// value on click).
//
// In tag_aware path layout, List walks one level deep so tagged sub-
// folders are flattened back into the operator-facing names list.
func (s *VaultStore) List() ([]Secret, error) {
	out, err := s.listAt(s.listPathFor(), "")
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *VaultStore) listAt(path, tagPrefix string) ([]Secret, error) {
	env, code, err := s.do("LIST", path, nil)
	if err != nil {
		// 404 from a fresh prefix means "no secrets yet" — not an error.
		if code == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	if env == nil || env.Data == nil {
		return nil, nil
	}
	keysRaw, ok := env.Data["keys"]
	if !ok {
		return nil, nil
	}
	keysSlice, ok := keysRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("vault list: unexpected keys shape")
	}
	var out []Secret
	for _, k := range keysSlice {
		ks, _ := k.(string)
		if ks == "" {
			continue
		}
		if strings.HasSuffix(ks, "/") {
			// Sub-folder (tag-aware layout). Recurse one level deep.
			if s.pathLayout == "tag_aware" {
				subTag := strings.TrimSuffix(ks, "/")
				subPath := path + "/" + subTag
				subItems, _ := s.listAt(subPath, subTag)
				out = append(out, subItems...)
			}
			continue
		}
		sec := Secret{
			Name:    ks,
			Backend: "vault",
		}
		if tagPrefix != "" {
			sec.Tags = []string{tagPrefix}
		}
		out = append(out, sec)
	}
	return out, nil
}

// Get fetches one secret by name. KV-v2 read returns the value + metadata
// in env.Data.{data,metadata}. We pull operator-facing fields (value,
// description, tags, scopes) out of Data.data; tags + scopes are stored
// as comma-joined strings under the well-known keys "datawatch-tags" and
// "datawatch-scopes" so the round-trip is lossless.
func (s *VaultStore) Get(name string) (Secret, error) {
	// In tag_aware layout, we don't know the tag at lookup time, so try
	// the flat path first; on 404, walk sub-folders.
	sec, err := s.getAt(s.pathFor(name, nil), name)
	if err == nil {
		return sec, nil
	}
	// Only walk sub-folders when (a) we're in tag_aware layout AND
	// (b) the flat lookup specifically returned ErrSecretNotFound.
	// Other errors (transport, perm-denied, sealed) propagate verbatim.
	if err != ErrSecretNotFound || s.pathLayout != "tag_aware" {
		return Secret{}, err
	}
	// Walk top-level tag folders.
	listEnv, code, listErr := s.do("LIST", s.listPathFor(), nil)
	if listErr != nil || code == http.StatusNotFound || listEnv == nil || listEnv.Data == nil {
		return Secret{}, ErrSecretNotFound
	}
	keysRaw, _ := listEnv.Data["keys"].([]interface{})
	for _, k := range keysRaw {
		ks, _ := k.(string)
		if !strings.HasSuffix(ks, "/") {
			continue
		}
		tag := strings.TrimSuffix(ks, "/")
		sec, err = s.getAt(s.pathFor(name, []string{tag}), name)
		if err == nil {
			return sec, nil
		}
	}
	return Secret{}, ErrSecretNotFound
}

func (s *VaultStore) getAt(path, name string) (Secret, error) {
	env, code, err := s.do(http.MethodGet, path, nil)
	if err != nil {
		if code == http.StatusNotFound {
			return Secret{}, ErrSecretNotFound
		}
		return Secret{}, err
	}
	if env == nil || env.Data == nil {
		return Secret{}, ErrSecretNotFound
	}
	rawData, ok := env.Data["data"]
	if !ok {
		return Secret{}, ErrSecretNotFound
	}
	dataMap, ok := rawData.(map[string]interface{})
	if !ok {
		return Secret{}, fmt.Errorf("vault get: unexpected data shape")
	}
	sec := Secret{Name: name, Backend: "vault"}
	if v, ok := dataMap["value"].(string); ok {
		sec.Value = v
	}
	if v, ok := dataMap["description"].(string); ok {
		sec.Description = v
	}
	if v, ok := dataMap["datawatch-tags"].(string); ok && v != "" {
		sec.Tags = strings.Split(v, ",")
	}
	if v, ok := dataMap["datawatch-scopes"].(string); ok && v != "" {
		sec.Scopes = strings.Split(v, ",")
	}
	if metaRaw, ok := env.Data["metadata"].(map[string]interface{}); ok {
		if cs, ok := metaRaw["created_time"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, cs); err == nil {
				sec.CreatedAt = t
				sec.UpdatedAt = t // KV-v2 doesn't separate; updated == latest version time
			}
		}
	}
	return sec, nil
}

// Set creates or updates a secret. The KV-v2 write payload is
// {data: {value, description, datawatch-tags, datawatch-scopes}}.
// Operator-supplied tags + scopes are joined with commas for round-trip
// fidelity since Vault KV-v2 only stores string values in the data map.
func (s *VaultStore) Set(name, value string, tags []string, description string, scopes []string) error {
	body := map[string]interface{}{
		"data": map[string]string{
			"value":            value,
			"description":      description,
			"datawatch-tags":   strings.Join(tags, ","),
			"datawatch-scopes": strings.Join(scopes, ","),
		},
	}
	_, _, err := s.do(http.MethodPost, s.pathFor(name, tags), body)
	return err
}

// Delete removes a secret. KV-v2 has soft-delete (DELETE on /data) and
// hard-delete (DELETE on /metadata, removes all versions). Datawatch
// chooses hard-delete to match the other backends' semantics.
func (s *VaultStore) Delete(name string) error {
	// First check existence (and resolve tag-aware path).
	sec, err := s.Get(name)
	if err != nil {
		return err // ErrSecretNotFound or transport
	}
	_, _, err = s.do(http.MethodDelete, s.metaPathFor(name, sec.Tags), nil)
	return err
}

// Exists is List+iterate by name. KV-v2 has no cheap HEAD — but a GET
// with a 404 distinguishes cleanly.
func (s *VaultStore) Exists(name string) (bool, error) {
	_, err := s.Get(name)
	if err == nil {
		return true, nil
	}
	if err == ErrSecretNotFound {
		return false, nil
	}
	return false, err
}

// isVaultNotFound returns true for 404-shaped errors from do().
func isVaultNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "HTTP 404")
}
