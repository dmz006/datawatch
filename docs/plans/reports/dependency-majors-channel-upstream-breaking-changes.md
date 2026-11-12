# Channel upstream breaking changes

Open dependabot branches under `dependabot/npm_and_yarn/channel/*`. Current versions come from `channel/package-lock.json` on this branch; targets come from each branch's lockfile.

## Modules and target versions

| Module | Current | Target | Branch | Declared in |
|---|---|---|---|---|
| @modelcontextprotocol/sdk | 1.31.0 | 1.32.0 | modelcontextprotocol/sdk-1.32.0 | direct dependency, range `^1.32.0` |
| @hono/node-server | 2.1.0 | 2.1.5 | hono/node-server-2.1.5 | transitive, override `>=2.0.10` |
| qs | 6.16.0 | 6.17.0 | qs-6.17.0 | transitive, override raised to `>=6.17.0` |
| ip-address | 10.7.3 | 10.7.4 | ip-address-10.7.4 | transitive, override `>=10.7.1` |
| zod | 4.3.6 | 4.6.7 | zod-4.6.7 | transitive via the SDK, no override |
| fast-uri | 4.2.1 | 4.2.3 | fast-uri-4.2.3 | transitive, override `>=4.1.5` |
| body-parser | 2.3.0 | 2.4.0 | body-parser-2.4.0 | transitive, override raised to `>=2.4.0` |

`channel/index.ts` imports only `Server`, `StdioServerTransport`, `CallToolRequestSchema`, `ListToolsRequestSchema` (SDK), and `node:http`. It registers tools through `mcp.setRequestHandler(...)`, not `registerTool`/`McpServer`, and it makes no zod calls. It does not import hono, qs, body-parser, ip-address or fast-uri.

## Breaking changes that apply

- SDK 1.32.0: `tools/call` requests without an `arguments` field are now accepted, so `req.params.arguments` can be undefined. `channel/index.ts` casts it and destructures it in the `reply`, `memory_remember`, `memory_recall`, `memory_list` and `memory_forget` handlers (lines 135-175). A call with no arguments would throw a TypeError. This is the one change that touches our code, and a `?? {}` guard would cover it.
- SDK 1.32.0: HTTP client transports follow only same-origin redirects. This does not apply, because the channel uses `StdioServerTransport` and plain `fetch`/`http`.
- SDK 1.32.0: `maxToolInputElements` and `expectedResource` are opt-in and off by default. No effect.
- zod 4.4.0 to 4.6.x: tuple output, `z.undefined()` keys, `.merge()`, `z.emoji()`, record-key and `fromJSONSchema()` changes. They apply only to code that calls zod. We do not call it, and the SDK's use is internal, so the risk is low.
- zod 4.6.0: the CommonJS exports are frozen. This is irrelevant because the channel is ESM (`"type": "module"`).
- @hono/node-server 2.1.3 and 2.1.5: `serveStatic` rejects `%` in decoded paths and serves only GET and HEAD. The channel does not use `serveStatic`, and hono is only pulled in by the SDK's HTTP transport, so this does not apply.
- qs 6.17.0: own `__proto__` keys are kept as data, lone surrogates encode as U+FFFD, and `allowEmptyArrays` encodes keys. The channel does not call qs directly. Only body-parser and the SDK's HTTP transport use it.
- fast-uri 4.2.2 and 4.2.3: stricter validation. Percent-encoded schemes, malformed hosts and invalid reference edges are now rejected (security fixes). The channel passes no untrusted URIs to it, so the risk is low.
- body-parser 2.4.0: security fix GHSA-27r3-jxmh-p828, added zstd support, and `req.body` may be undefined in v2. The channel does not use body-parser. It uses `node:http` directly.
- ip-address 10.7.4: the release has no published notes. Nothing is known to apply, and the diff v10.7.3...v10.7.4 should be skimmed.

No release is labelled breaking by its maintainers. Only the SDK `arguments` change touches `channel/index.ts`.

## Sources

- https://github.com/modelcontextprotocol/typescript-sdk/releases/tag/1.32.0
- https://github.com/modelcontextprotocol/typescript-sdk/releases
- https://github.com/honojs/node-server/releases
- https://github.com/ljharb/qs/blob/main/CHANGELOG.md
- https://github.com/beaugunderson/ip-address/releases
- https://github.com/colinhacks/zod/releases
- https://github.com/fastify/fast-uri/releases
- https://github.com/expressjs/body-parser/releases
- `channel/index.ts`, `channel/package.json`, and `channel/package-lock.json` on each dependabot branch (FETCH_HEAD)
