# Go upstream breaking changes (dependabot branches)

## Modules and target versions

Target versions were read from `go.mod` on each fetched branch (`git show FETCH_HEAD:go.mod`). No fallback to `go list -m -versions` was needed.

Note: the mcp-go branch is named `mcp-go-1.4.0` but its `go.mod` carries **v1.5.0**. The branch contents were used.

**Branch `dependabot/go_modules/github.com/mark3labs/mcp-go-1.4.0`**
- github.com/mark3labs/mcp-go: v0.46.0 -> v1.5.0 (adds indirect santhosh-tekuri/jsonschema/v6 v6.0.2)

**Branch `dependabot/go_modules/github.com/klauspost/compress-1.18.7`**
- github.com/klauspost/compress (indirect): v1.18.6 -> v1.18.7
- The branch is based on an older main. It also carries golang.org/x/{crypto v0.57.0, net v0.60.0, sys v0.48.0, term v0.46.0, text v0.42.0, sync v0.23.0, tools v0.49.0}, which are lower than this branch. Rebase before merging so these are not downgraded.

**Branch `dependabot/go_modules/version-updates-4d782c56a3` (grouped)**
- bwmarrin/discordgo v0.28.1 -> v0.29.0
- slack-go/slack v0.23.1 -> v0.34.0
- maunium.net/go/mautrix v0.22.0 -> v0.32.0
- jackc/pgx/v5 v5.9.2 -> v5.12.0
- miekg/dns v1.1.72 -> v1.1.73
- modernc.org/sqlite v1.48.1 -> v1.61.1
- prometheus/client_golang v1.23.2 -> v1.25.0 (client_model v0.6.2 -> v0.6.3)
- cloudflare/circl v1.6.3 -> v1.6.5
- ebitengine/purego v0.11.0 -> v0.11.3
- fsnotify/fsnotify v1.10.0 -> v1.10.1
- mdp/qrterminal/v3 v3.2.0 -> v3.2.1
- spf13/pflag v1.0.10 -> v1.1.0
- Indirect-only bumps: modernc.org/libc v1.70.0 -> v1.79.0, modernc.org/memory v1.11.0 -> v1.12.1, go.mau.fi/util v0.8.2 -> v0.11.0, protobuf v1.36.11 -> v1.36.12, prometheus/common v0.66.1 -> v0.72.0, procfs v0.16.1 -> v0.22.0, golang.org/x/exp bumped. Several others also change.
- The `x/mod` and `x/tools` lines and `go.yaml.in/yaml/v2` are dropped from `go.mod`.

## Breaking changes that apply

Repo usage: mcp-go is imported by ~190 files, almost all `mcp-go/mcp` and 14 uses of `mcp-go/server` (`internal/mcp/server.go`, `internal/mcp/prompts.go`, `internal/mcp/resources.go`, `cmd/datawatch-channel/main.go`). Notes were partly gathered from release-note titles, so verify against the diffs.

mcp-go v0.46.0 -> v1.5.0:
- v1.2.0: empty arrays are sent instead of `null` in results. Applies to tool and list results from `internal/mcp` and could change tests that compare JSON.
- v1.2.0: requests missing client capabilities are rejected with invalid params, and unsupported `MCP-Protocol-Version` headers are rejected. Applies to `server.NewSSEServer` (`internal/mcp/server.go:1628`) and to older clients.
- v1.3.0: tool results are size-limited. Large tool outputs, such as session output, may now be rejected.
- v1.3.0: output schemas are replaced rather than merged, and `WithRawInputSchema` now replaces the default input schema. The repo does not use `WithRawInputSchema`, so no change is needed there.
- v1.4.0: resource and prompt registries are replaced atomically. Applies to `AddResource`, `AddResourceTemplate` and `AddPrompt` in `cmd/datawatch-channel/main.go:183-206` and in `internal/mcp/{prompts,resources}.go`.
- v1.4.0: integer request IDs are preserved across decoding. Applies to `NewStdioServer` and the SSE server, and could change ID types in tests.
- v1.0.0: httptest helpers moved to `server/servertest`. Not applicable, because the repo does not import it.
- v1.0.0: adds the 2026-07-28 spec. Protocol behavior may change.
- v1.5.0: `subscriptions/listen` is refused when the response cannot stream. This matters only if clients use subscriptions.

slack-go/slack v0.23.1 -> v0.34.0 (used only in `internal/messaging/backends/slack/backend.go`):
- v0.30.0: the minimum Go version is 1.26. The repo is on `go 1.26.10`, so no impact.
- v0.34.0: decoding is stricter, so malformed block elements now return errors. Applies to `NewSectionBlock`, `NewActionBlock` and `NewButtonBlockElement`.
- v0.34.0: `GetUsersOptionPresence` removed. Not used by the repo.
- v0.34.0: `EventMapping` precedence change. Not used by the repo.
- A later release makes socket-mode `Ack()` and `Send()` return an error. The repo does not use `socketmode`.

maunium.net/go/mautrix v0.22.0 -> v0.32.0 (used in `internal/messaging/backends/matrix/`):
- v0.23.0: `JoinRoom` signature changed. The repo calls `JoinRoomByID` (`backend.go:188`), not `JoinRoom`.
- v0.23.1: `IsDeviceTrusted` takes a context. Not used by the repo.
- v0.23.2: legacy bridge module removed, and `m.federate` became a pointer. Not used by the repo.
- v0.25.0: gorilla replaced by stdlib routers and `coder/websocket`. This affects only the bridge packages, which the repo does not import.
- v0.28.0: federation `NewClient` takes an extra parameter. The repo does not import the federation package.
- v0.31.0: the `event` package no longer registers content structs with gob. The repo does not use gob for events.
- No change needed for `mautrix.NewClient` or `mautrix.UserIdentifier`. No breaking change to them was found, but the changelog was not read in full.

Other modules: no breaking change found that touches this repo's usage.
- discordgo v0.29.0: not reviewed beyond the version bump. The repo uses `discordgo.New`, `Session`, `MessageSend` and `MessageComponent`.
- pgx v5.12.0 (`pgxpool.New` and `pgxpool.Pool` only) and modernc.org/sqlite v1.61.1 were not individually reviewed. Run the sqlite and pgx tests after upgrading.

## Sources

- https://github.com/mark3labs/mcp-go/releases
- https://github.com/slack-go/slack/releases/tag/v0.34.0
- https://github.com/slack-go/slack/releases
- https://github.com/mautrix/go/releases/tag/v0.32.0
- https://mau.fi/blog/2025-07-mautrix-release/
- https://mau.fi/blog/2025-08-mautrix-release/

## mcp-go v1.5.0 migration notes (call-site review)

- Server-initiated requests: v1.x refuses `RequestSampling`/`RequestElicitation` for 2026-07-28+ clients by default. Both `server.NewMCPServer` calls (`internal/mcp/server.go`, `cmd/datawatch-channel/main.go`) now pass `server.WithLegacyServerInitiatedRequests()`, so both features keep working. `ErrServerInitiatedRequestUnsupported` is also mapped to the existing fallback as a safety net. No Blocked bumps.
- Tool result size limit (v1.3.0): opt-in through `WithToolResultSizeLimit`, default 0 (off). The repo does not set it, so session output is not rejected.
- Empty arrays instead of null, integer request IDs (v1.2.0/v1.4.0): the full `internal/mcp` and `cmd/datawatch-channel` suites pass, so no test compares against the old shape. The golden tool listing is unchanged.
- `NewSSEServer` protocol-version/capability checks (v1.2.0) and atomic registry replacement (v1.4.0): `AddResource`, `AddResourceTemplate` and `AddPrompt` are called at startup only, so atomic replacement has no effect. The SSE tests pass.
