package tagregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// The keeper's REST routes, used by HTTPKeeperClient on a peer and served by
// the daemon that keeps the registry:
//
//	GET  /api/autonomous/tags/registry        the keeper's Snapshot
//	POST /api/autonomous/tags/registry/write  one forwarded Write, answered with a WriteResult
//
// A refusal comes back as a JSON body {"error": text, "kind": kind}. The kind
// names the sentinel error, so a peer maps a refusal back to the same errors.Is
// check a local refusal matches.
const (
	SnapshotPath = "/api/autonomous/tags/registry"
	WritePath    = "/api/autonomous/tags/registry/write"
)

// Refusal kinds on the wire.
const (
	KindNotFound     = "not_found"
	KindInvalid      = "invalid"
	KindHasChildren  = "has_children"
	KindInUse        = "in_use"
	KindCycle        = "cycle"
	KindNotKeeper    = "not_keeper"
	KindNoteNotFound = "note_not_found"
	KindUnreachable  = "unreachable"
)

// keeperRequestTimeout bounds one call to the keeper.
const keeperRequestTimeout = 10 * time.Second

// ErrorKind returns the wire kind of a registry error, or "" when the error is
// not one the wire names.
func ErrorKind(err error) string {
	switch {
	case errors.Is(err, ErrTagNotFound):
		return KindNotFound
	case errors.Is(err, ErrTagInvalid):
		return KindInvalid
	case errors.Is(err, ErrTagHasChildren):
		return KindHasChildren
	case errors.Is(err, ErrTagInUse):
		return KindInUse
	case errors.Is(err, ErrTagCycle):
		return KindCycle
	case errors.Is(err, ErrNotKeeper):
		return KindNotKeeper
	case errors.Is(err, ErrNoteNotFound):
		return KindNoteNotFound
	case errors.Is(err, ErrKeeperUnreachable):
		return KindUnreachable
	}
	return ""
}

// wireError is a refusal relayed from the keeper. Its text is the keeper's
// text, and it matches the sentinel for its kind with errors.Is.
type wireError struct {
	msg  string
	kind error
}

func (e *wireError) Error() string        { return e.msg }
func (e *wireError) Is(target error) bool { return e.kind != nil && target == e.kind }

// refusalFromWire rebuilds a keeper refusal from its kind and text.
func refusalFromWire(kind, msg string) error {
	sentinels := map[string]error{
		KindNotFound:     ErrTagNotFound,
		KindInvalid:      ErrTagInvalid,
		KindHasChildren:  ErrTagHasChildren,
		KindInUse:        ErrTagInUse,
		KindCycle:        ErrTagCycle,
		KindNotKeeper:    ErrNotKeeper,
		KindNoteNotFound: ErrNoteNotFound,
	}
	if s, ok := sentinels[kind]; ok {
		return &wireError{msg: msg, kind: s}
	}
	if msg == "" {
		msg = "keeper refused the request"
	}
	return errors.New(msg)
}

// PeerEndpoint resolves a federation peer by name to its base URL and bearer
// token. ok is false when no such peer is registered.
type PeerEndpoint func(name string) (baseURL, token string, ok bool)

// HTTPKeeperClient is the KeeperClient a peer uses. It calls the keeper over
// the federation REST API with the keeper's peer token. Keeper returns the
// current autonomous.tags.registry_keeper value, so a change at runtime is
// picked up on the next call.
type HTTPKeeperClient struct {
	Peers  PeerEndpoint
	Keeper func() string
	// HTTP is the client for the calls. Nil uses a client with a 10 second timeout.
	HTTP *http.Client
}

// FetchSnapshot returns the keeper's registry.
func (c *HTTPKeeperClient) FetchSnapshot(ctx context.Context) (Snapshot, error) {
	var snap Snapshot
	err := c.do(ctx, http.MethodGet, SnapshotPath, nil, &snap)
	return snap, err
}

// Write forwards one edit to the keeper. A conflict the keeper decided is part
// of the WriteResult, not an error.
func (c *HTTPKeeperClient) Write(ctx context.Context, w Write) (WriteResult, error) {
	var res WriteResult
	err := c.do(ctx, http.MethodPost, WritePath, w, &res)
	return res, err
}

// do sends one request to the keeper and decodes a 200 reply into out. A
// transport failure, or a 502, 503 or 504 reply, is ErrKeeperUnreachable.
func (c *HTTPKeeperClient) do(ctx context.Context, method, path string, in, out any) error {
	name := c.Keeper()
	if c.Peers == nil {
		return fmt.Errorf("%w: no federation peers are configured", ErrKeeperUnreachable)
	}
	base, token, ok := c.Peers(name)
	if !ok || base == "" {
		return fmt.Errorf("%w: keeper %q is not a federation peer", ErrKeeperUnreachable, name)
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("%w: keeper %q: %v", ErrKeeperUnreachable, name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("keeper %q sent an unreadable reply: %w", name, err)
		}
		return nil
	}
	switch resp.StatusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Errorf("%w: keeper %q answered HTTP %d", ErrKeeperUnreachable, name, resp.StatusCode)
	}
	var env struct {
		Error string `json:"error"`
		Kind  string `json:"kind"`
	}
	_ = json.Unmarshal(raw, &env)
	msg := env.Error
	if msg == "" {
		msg = strings.TrimSpace(string(raw))
	}
	return refusalFromWire(env.Kind, msg)
}

func (c *HTTPKeeperClient) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: keeperRequestTimeout}
}
