// scripts/test-stories/pwa/tls-trust.mjs — sandbox TLS trust helper.
//
// Depends only on node: builtins (no playwright), so it can be imported by
// standalone node:test files even where the pwa/ node_modules has not been
// installed. lib.mjs re-exports it for the browser stories.
//
// env.tlsCa / TEST_TLS_CA — path to the daemon's own PEM certificate
//   (self-signed, generated at <data_dir>/tls/server/cert.pem), used as the
//   trust anchor for sandboxTlsAgent(). Unset, sandboxTlsAgent() falls back
//   to $TEST_DATA/tls/server/cert.pem (or $DATAWATCH_TEST_DATA/...).

import https from 'node:https';
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';

export const tlsCa = process.env.TEST_TLS_CA || '';

// https.Agent that pins the test daemon's self-signed certificate as its CA.
// Node's normal certificate verification stays fully in force (SNI, chain and
// validity checks all run); only the trust anchor is the daemon's own cert.
// The agent is cached per process; resetSandboxTlsAgent() drops the cache
// (used by tests that change TEST_TLS_CA between calls).

let sandboxAgent = null;

export function resetSandboxTlsAgent() {
  sandboxAgent = null;
}

function resolveTlsCaPath() {
  const explicit = process.env.TEST_TLS_CA || tlsCa;
  if (explicit) return explicit;
  for (const root of [process.env.TEST_DATA, process.env.DATAWATCH_TEST_DATA]) {
    if (!root) continue;
    const candidate = path.join(root, 'tls/server/cert.pem');
    if (existsSync(candidate)) return candidate;
  }
  throw new Error(
    'TEST_TLS_CA is not set and no fallback certificate was found — '
    + 'point TEST_TLS_CA at the daemon\'s tls/server/cert.pem '
    + '(self-signed cert in <data_dir>/tls/server/cert.pem)',
  );
}

export function sandboxTlsAgent() {
  if (!sandboxAgent) {
    const caPath = resolveTlsCaPath();
    let ca;
    try {
      ca = readFileSync(caPath);
    } catch (err) {
      throw new Error(`TEST_TLS_CA: cannot read CA file ${caPath}: ${err.message}`);
    }
    sandboxAgent = new https.Agent({ ca });
  }
  return sandboxAgent;
}
