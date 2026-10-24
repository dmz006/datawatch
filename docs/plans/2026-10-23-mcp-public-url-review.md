# Review: external SSE public URL change (PR 208, issue 207)

PR head: ddb94e16e2978b9dca7374f6b9987d1bb0dbe1a0

Date: 2026-10-23. First draft, no merge decision. PR 208 adds `mcp.public_url` so the MCP SSE
`endpoint` event can advertise a reverse-proxy address instead of the bind address. It branched
from main at c468569, which is 1802 commits behind the current main.

## Behaviour check

- (a) `public_url` set: `sseBaseURL` trims whitespace, then trailing slashes, and returns the
  result, which `ServeSSE` passes to `server.WithBaseURL`. Verified from the diff and its table tests.
- (b) `public_url` empty or whitespace only: the result is `<scheme>://<first sse_host entry,
  trimmed>:<sse_port>`, https when `tls_enabled`. The current code computes the same string
  (`"%s:%d"` then `"%s://%s"`), so the output is byte-for-byte equal.
- Empty `sse_host`: `strings.Split("", ",")` returns `[""]`, so both versions give `http://:8081`.
  The existing `len(hosts) == 0` fallback in `ServeSSE` is dead code, and the PR leaves it as is.
- Differences between the PR's `ServeSSE` and the current one:
  - The PR builds `sseSrv` with `server.NewSSEServer(s.srv, server.WithBaseURL(baseURL))` only.
    Current main also passes `server.WithKeepAliveInterval(10*time.Second)` with its multi-line
    CRASH-SAFETY / RELIABILITY comment. Both must survive the merge.
  - The PR's hunk sits at line 1306; the same code is now near line 1446, and the surrounding
    code changed (`mcpFedAuthMiddleware`, `ObserveSSEHandler`, `LogSSEServerTimeouts`,
    `listenWithRetry` with `SSEBindTimeout`, TLS via `tlsutil.Build`). The PR does not touch those.
  - The PR adds a `fmt.Printf` of the advertised URL when `public_url` is set. Keep it.
- Likely conflicts beyond `server.go`: `CHANGELOG.md` (the PR's entry sits under `[Unreleased]`;
  main now has many versioned headings below it and uses short entries), `docs/config-reference.yaml`,
  `internal/config/config.go`, `internal/config/template.go`, `internal/router/router.go` (the
  configure key list), `internal/server/web/app.js` (the `COMMS_CONFIG_FIELDS` and `RESTART_FIELDS`
  lines moved by about 1700 lines), and all five locale files.
- `internal/server/api.go` and `internal/server/reload.go` apply cleanly, but the context has
  moved: the PUT case sits near `mcp.sse_port` at about line 6834 and the GET map near line 5683.
- Not covered by the PR: `internal/wizard/defs.go` (MCP setup wizard) and `internal/mcp/README.md`
  list `sse_port` but get no `public_url`. `internal/server/web/assets/docs-bm25-index.json` is
  generated and needs regenerating after the docs change.
- Rules still open on the PR: no version bump (correct for a branch, the merge script handles it),
  no Android or iPhone parity issue, no datawatch-app locale issue for `settings_mcp_public_url`,
  and the PR's CHANGELOG text uses a tracker reference. All five locales carry the English value
  as a placeholder, as the Localization Rule allows.

## Safety review

- Every non-test use of `MCPConfig.PublicURL`: the struct field, the config template line, the
  `GET /api/config` map, the `applyConfigPatch` case, and `sseBaseURL`, which is called only from
  `ServeSSE`. The value is read only to compute the base URL.
- It does not feed the listen address or the `hosts`/`addrs` binding loop (those use `SSEHost`
  and `SSEPort`), any `http.Redirect`, any outbound HTTP client call, TLS certificate generation
  (`tlsutil.Build` gets cert, key, auto-generate and data dir only), or any CORS or origin check.
- `go doc github.com/mark3labs/mcp-go/server WithBaseURL`: "sets the base URL for the SSE
  server". The library only prefixes the message endpoint it sends in the `endpoint` event with it;
  it does not bind, redirect or fetch. The value reaches the client as text, so a bad value
  misdirects clients rather than the daemon. Anyone who can write config can already change `sse_host`.
- Validation today: none. `applyConfigPatch` only trims whitespace and trailing slashes. Values
  such as `ftp://x`, `host:8081`, a relative path like `/mcp`, or text with a query, fragment or
  user-info (`https://u:p@host/?a=b#c`) are accepted and advertised to clients.
- Required validation, applied after trimming surrounding whitespace and trailing slashes: the
  value is empty, or an absolute URL with scheme http or https (case-insensitive), a non-empty
  host, no user-info, no query and no fragment. Reject anything else with a clear error from the
  PUT path, and add table tests for each rejected shape.

## Extension check

- Skills inject markdown at session spawn. Plugins are executables hooking `pre_session_start`,
  `post_session_output`, `post_session_complete` and `on_alert` (see `docs/api/plugins.md`). None
  of these hooks sees or can change the daemon's MCP SSE transport: the `endpoint` event is
  written by the mcp-go SSE handler inside the daemon from the `WithBaseURL` value fixed at
  startup, and no hook runs on the transport or receives that event. Session output hooks act on
  session text, not on the daemon's own protocol responses.
- The one non-core workaround is a reverse proxy that rewrites the `endpoint` event body in the
  stream. It is worse than a setting because it must buffer and edit a long-lived event stream
  (breaking streaming and the keepalive pings if done badly), needs per-proxy configuration
  (Caddy, nginx and `tailscale serve` differ, and `tailscale serve` is not known to offer body rewriting), and
  moves a daemon fact into infrastructure the operator then has to keep in step. So the request
  needs core code, and a config key is the smaller change.

## Mergeability

- The PR head commit is not in the local object store. Fetching it would write outside the
  project directory, so no `git merge-tree` run was done. Equivalent dry run:
  `gh pr diff 208 | git apply --check` against the current tree, which touches nothing.
- Result: does not apply as a whole. Conflicting files (13): `CHANGELOG.md`,
  `docs/config-reference.yaml`, `internal/config/config.go`, `internal/config/template.go`,
  `internal/mcp/server.go`, `internal/router/router.go`, `internal/server/web/app.js`, and
  `internal/server/web/locales/{de,en,es,fr,ja}.json`.
- Applies cleanly (each checked on its own): `docs/implementation.md`, `docs/mcp.md`,
  `docs/operations.md`, `internal/config/config_test.go`, `internal/mcp/sse_base_url_test.go`,
  `internal/server/api.go`, `internal/server/mcp_public_url_patch_test.go`,
  `internal/server/reload.go`.
- The `internal/mcp/server.go` conflict is the one that matters: resolving it by taking the PR's
  side would drop `server.WithKeepAliveInterval(10*time.Second)` and its comment.

## Config surface parity

Candidate branch `review/mcp-public-url-candidate` (nested worktree `.datawatch/candidate`): merge
commit of the PR head (sha above, merge kept with the contributor's authorship and a co-author
trailer), then one follow-up commit. The `ServeSSE` merge keeps `sseBaseURL(s.cfg)` and
`server.WithKeepAliveInterval(10*time.Second)` with its comment. The CHANGELOG entry is plain
bullets under `## [Unreleased]` with no version heading; no version variable was touched.

| Surface | Status |
|---|---|
| YAML (struct tag, annotated template) | Present in the PR; conflicts with the new bind-timeout key resolved by keeping both |
| REST read (`GET /api/config`) | Present in the PR |
| REST write (`PUT /api/config`) | Present in the PR; validation and 400 added by the follow-up |
| Restart-required list (`reload.go`) | Present in the PR; not applied live (confirmed by the parity test) |
| MCP `config_set` | Already worked: no key allow-list, forwards to the same PUT (still gated by the self-config setting) |
| CLI `datawatch config set` | Already worked: same PUT, no key allow-list or key-naming help text |
| Comm channel `configure` | Help text in the PR; the follow-up proves the key reaches the same PUT through `configureFn` and adds router tests |
| PWA field and restart flag | Present in the PR (label key, restart list); tracker reference not found in the PWA label |
| Locales (en, de, es, fr, ja) | Present in the PR (English placeholder); follow-up adds a parity test |
| Config reference | Present in the PR; tracker reference removed from the comment |
| Operator docs | `mcp.md`, `implementation.md` and `operations.md` in the PR; follow-up adds the five-row access-method table and validation text; `internal/mcp/README.md` line added |

Also removed the tracker reference from the config struct comment. It remains in the CHANGELOG
bullet and in test-file comments the contributor wrote.

Not changed: `internal/wizard/defs.go` (the setup wizard asks only for the port; the setting is
optional and set after setup), `docs/cursor-mcp.md`, and the generated docs search index
(regenerated at build time, git-ignored here).

### Validation rules implemented

`config.NormalizePublicURL` trims surrounding whitespace and trailing slashes. Empty or
whitespace-only input is empty with no error. Otherwise the value must parse with `net/url`, have
scheme `http` or `https` (case-insensitive), a non-empty host, no user-info, no query (including a
bare `?`) and no fragment (including a bare `#`). Rejected shapes covered by table tests:
`ftp://x`, `host:8081`, `//host`, `/relative`, `javascript:alert(1)`, user-info, query, fragment,
`https://`, a port-only host, and a space in the host.

- `PUT /api/config` validates before anything is applied: a bad value returns 400 naming
  `mcp.public_url` and leaves the config and file unchanged.
- `sseBaseURL` treats an invalid configured value as empty and falls back to the bind-address
  URL; `ServeSSE` prints one warning line. The bound addresses are unchanged.

Checks run in the candidate: `gofmt -l` on added Go files, `go vet` on the four packages, the
targeted tests, `node --check app.js` and `scripts/check-docs-sync.sh` all pass. The full test
suite was not run.

### Follow-ups for the operator

Nothing was filed. For the operator to decide:

- File the Android and iPhone settings parity issues for the MCP SSE public URL field (AGENT.md
  Mobile-Parity Rule).
- File the datawatch-app issue for the new locale key `settings_mcp_public_url` (Section B, B4).
- Decide whether the setup wizard should ask for the public URL.

## Test and lint results

Candidate commit: c3f1150a7e1f10c55bac9241ab758494b4bf450c

- `make build`: passed; the docs sync left the tree clean, so no regenerated files were committed.
- `go test -timeout 30m -count=1 ./...`: passed, every package ok (run on the original candidate head; after the one-line test fix below, `internal/mcp` was rerun and passed).
- `make lint`: first run failed with one finding, `errcheck` on the unchecked `resp.Body.Close` in `internal/mcp/sse_base_url_test.go`. That file does not exist on the merge base, so the finding was introduced by the candidate. Fixed with a follow-up commit that discards the close error as the neighbouring tests do; rerun reports 0 issues.
- `node --check internal/server/web/app.js`: passed.
- `bash scripts/check-no-internal-refs.sh`: reports 1 leak (README release line); the merge base reports the same 1 leak, so it is pre-existing and not fixed here.
- `bash scripts/check-docs-sync.sh`: passed, docs and embedded docs in sync.
- Locale files (de, en, es, fr, ja): all five parse as JSON.

Pre-existing failures found on the merge base: the internal-refs leak in README.md (script check, no Go package or test). No failing Go package or test.

New failures: the lint finding in `internal/mcp/sse_base_url_test.go`, fixed in the candidate with a follow-up commit; no unresolved new failures.
