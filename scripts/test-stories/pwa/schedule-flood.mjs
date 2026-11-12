#!/usr/bin/env node
// schedule-flood.mjs — standalone entry point for the PWA schedules-flood story (TS-797).
//
// Run directly (see `make test-e2e-pwa` / scripts/e2e-pwa.sh):
//   TEST_HTTP=http://127.0.0.1:<port> TEST_TLS=https://127.0.0.1:<tlsport> \
//   TEST_TOKEN=<token> [TEST_TRUSTED_TLS=https://...] node schedule-flood.mjs
//
// The story (implemented in TS-797.mjs) verifies the /api/schedules
// request-flood fix from the PWA after a WS reconnect with 20+ live sessions:
//   - the GET /api/schedules request count stays bounded (≤ 5, independent of
//     the session count) instead of fanning out per-session fetches that hit
//     the browser's per-origin connection limit and surface as
//     net::ERR_INSUFFICIENT_RESOURCES,
//   - and it holds against BOTH origins:
//       (A) untrusted self-signed TLS origin ($TEST_TLS, ignoreHTTPSErrors —
//           the service worker cannot claim control there),
//       (B) trusted TLS origin ($TEST_TRUSTED_TLS if provided, else a
//           generated throwaway CA + Node TLS proxy + Chrome spki-list, where
//           navigator.serviceWorker.controller is asserted before counting).
//
// Exit codes: 0 pass · 1 fail · 2 skip (scenario B infeasible; see result.txt
// in the evidence dir). Seeded sessions are always cleaned up (best-effort).
//
// See TS-797.mjs header for the full spec and docs/testing/README.md for the
// `make test-e2e-pwa` target.
process.env.CURRENT_STORY ??= 'schedule-flood';
await import('./TS-797.mjs');
