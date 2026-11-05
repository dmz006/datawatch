# Expose verifier slot settings and status on every operator surface

## REST (internal/server)
- `compute.go`:
  - New `VerifierNodeStatus` type (daemon's per-node verifier admission
    state: ExecutorCap, VerifierCap, FreeMB, HasData, HeadroomOK,
    HeadroomReason, SuggestedCap *int) + `VerifierNodeStatusReader` func
    type + `Server.verifierNodeStatus` field + `SetVerifierNodeStatus` on
    `Server` and `HTTPServer` (nil = not wired → status fields absent).
    Handlers read ONLY this injected reader — no observer I/O.
  - `withNodeSlots` (node detail) adds:
    - `verifier_slots` — operator value or JSON null (from the node record)
    - `verifier_slots_effective` — the node's verifier pool limit in effect
      (reader's VerifierCap)
    - `verifier_min_free_mem_mb_effective` — record value, 1024 when unset
      (compute.EffectiveVerifierMinFreeMB)
    - `verifier_context_mb` — record value (0 = no sizing figure)
    - `suggested_verifier_slots` — reader's SuggestedCap, null when none
    - `executor_slots` — same figure as `derived_slots` (node pool limit;
      absent until one exists), so the two slots are shown together
    - `free_gpu_mb`, `headroom_ok`, `headroom_reason` — headroom state
    (last three + effective/suggested/verifier_slots_effective present only
    when the reader is wired and has a record for the node)
- `compute.go` PUT handler: run `n.Validate()` before `Update` (mirror the
  POST path) so negative verifier settings are refused with the same
  "compute node: verifier_slots %d must be >= 0" message style.
- `capacity.go`: `capacityPoolJSON` gains (node:<name> pools only):
  - `verifier_limit` / `verifier_held` — the node-verify:<name> pool's
    limit and held from the ledger snapshot (absent when the pool has none)
  - `free_gpu_mb`, `headroom_ok`, `headroom_reason` — reader's state

## MCP (internal/mcp/compute.go)
- `compute_node_add` / `compute_node_update` schemas: `verifier_slots`,
  `verifier_min_free_mem_mb`, `verifier_context_mb` string params; update
  wording says update replaces the whole node, omitting clears/resets.
- `computeBodyFromReq` parses the three (sent only when given).
- node detail tool description names the new figures.

## Comm (internal/router/compute.go)
- `parseKVPairs` accepts `verifier_slots=N`, `verifier_min_free_mem_mb=N`,
  `verifier_context_mb=N`.
- `computeUsage` documents the three keys.
- `node detail` proxies the REST detail — new figures appear automatically.

## CLI (cmd/datawatch/cli_compute.go)
- `compute node add|update` gains `--verifier-slots`,
  `--verifier-min-free-mem-mb` (default **-1 = unset** so `0` can mean
  "explicit zero"; `-1` is omitted from the body so update clears it; below
  -1 is refused) and `--verifier-context-mb` (default 0 = no sizing
  figure; negative refused). Values sent only when set.

## Wiring (cmd/datawatch/capacity_wiring.go)
- `wireVerifierNodeStatus` adapts `VerifierNodeStatus` (previous story) to
  the server's reader type; wired once in wireCapacity next to
  `httpServer.SetCapacity(led)`.

## OpenAPI (internal/server/web/openapi.yaml)
- node GET/PUT schemas + detail response + capacity node pool fields.

## Tests (new _test.go files only)
- internal/server/compute_verifier_slots_test.go — detail figures (fake
  reader incl. null suggested + null verifier_slots), headroom absence
  without wiring, PUT/POST negative refusal, capacity verifier fields.
- internal/mcp/compute_verifier_slots_test.go — schemas + forwarding
  (values in, absent keys out).
- internal/router/compute_verifier_slots_test.go — KV parse to int, absent
  keys stay absent.
- cmd/datawatch/cli_compute_verifier_test.go — flags exist with usage
  wording, body carries set values, -1 omits, below -1 refused.

## Validation
`rtk go build ./...` + `rtk go test ./internal/compute/... ./internal/server/... ./internal/mcp/... ./internal/router/... ./cmd/datawatch/...`
