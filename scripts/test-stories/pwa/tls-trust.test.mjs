// scripts/test-stories/pwa/tls-trust.test.mjs — node:test coverage for the
// sandboxTlsAgent() helper in tls-trust.mjs (re-exported by lib.mjs).
//
// It generates a throwaway self-signed certificate with
// SAN IP:127.0.0.1 / DNS:localhost using the system openssl binary, serves a
// trivial https endpoint with it on 127.0.0.1:0, and asserts:
//   (a) a request through sandboxTlsAgent() succeeds (the pinned CA works),
//   (b) a request through a default https.Agent() fails with a certificate
//       error (so the check in (a) proves real trust, not blanket bypass),
//   (c) with TEST_TLS_CA unset and no TEST_DATA, sandboxTlsAgent() throws an
//       error that names TEST_TLS_CA.
//
// Run:  node --test scripts/test-stories/pwa/tls-trust.test.mjs

import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import https from 'node:https';
import { tmpdir } from 'node:os';
import path from 'node:path';

import { sandboxTlsAgent, resetSandboxTlsAgent } from './tls-trust.mjs';

// --- openssl detection (skip with a clear message when absent) ---------------

function resolveOpenssl() {
  const out = execFileSync('sh', ['-c', 'command -v openssl'], { encoding: 'utf8' });
  return out.trim();
}

const opensslBin = (() => { try { return resolveOpenssl() || null; } catch { return null; } })();
const SKIP_REASON = 'openssl not found on PATH (command -v openssl failed) — cannot generate a throwaway self-signed cert';

// --- one-off TLS fixture ------------------------------------------------------

const ctx = { dir: null, cert: null, server: null, port: null };
const savedEnv = {
  TEST_TLS_CA: process.env.TEST_TLS_CA,
  TEST_DATA: process.env.TEST_DATA,
  DATAWATCH_TEST_DATA: process.env.DATAWATCH_TEST_DATA,
};

function buildFixture() {
  ctx.dir = mkdtempSync(path.join(tmpdir(), 'dw-tls-trust-'));
  const cert = path.join(ctx.dir, 'cert.pem');
  const key = path.join(ctx.dir, 'key.pem');
  execFileSync(opensslBin, [
    'req', '-x509', '-newkey', 'rsa:2048',
    '-keyout', key,
    '-out', cert,
    '-days', '1', '-nodes',
    '-subj', '/CN=localhost',
    '-addext', 'subjectAltName=IP:127.0.0.1,DNS:localhost',
  ], { stdio: 'pipe' });
  ctx.cert = cert;
  ctx.key = key;
  ctx.server = https.createServer(
    { key: readFileSync(key), cert: readFileSync(cert) },
    (req, res) => {
      res.writeHead(200, { 'content-type': 'text/plain' });
      res.end('tls-trust-ok');
    },
  );
}

before(async () => {
  if (!opensslBin) {
    console.log(`SKIP: ${SKIP_REASON}`);
    return;
  }
  buildFixture();
  await new Promise((resolve, reject) =>
    ctx.server.once('error', reject).listen(0, '127.0.0.1', resolve));
  ctx.port = ctx.server.address().port;
});

after(() => new Promise((resolve) => {
  const done = () => {
    if (ctx.dir) rmSync(ctx.dir, { recursive: true, force: true });
    resolve();
  };
  if (!ctx.server) return done();
  ctx.server.closeAllConnections?.();
  const timer = setTimeout(done, 2000); // never hang teardown on a stuck client
  ctx.server.close(() => { clearTimeout(timer); done(); });
}));

function restoreEnv() {
  for (const [name, value] of Object.entries(savedEnv)) {
    if (value === undefined) delete process.env[name];
    else process.env[name] = value;
  }
}

// --- tests ---------------------------------------------------------------------

function getVia(agent) {
  return new Promise((resolve, reject) => {
    const req = https.get(
      { host: '127.0.0.1', port: ctx.port, path: '/', method: 'GET', agent },
      (res) => {
        let body = '';
        res.on('data', (d) => { body += d; });
        res.on('end', () => resolve({ status: res.statusCode, body }));
      },
    );
    req.on('error', reject);
  });
}

test('sandboxTlsAgent() trusts the certificate named by TEST_TLS_CA', async (t) => {
  if (!opensslBin || !ctx.server) return t.skip(SKIP_REASON);
  process.env.TEST_TLS_CA = ctx.cert;
  resetSandboxTlsAgent();
  const res = await getVia(sandboxTlsAgent());
  assert.equal(res.status, 200);
  assert.equal(res.body, 'tls-trust-ok');
});

test('a default https.Agent() rejects the same self-signed certificate', async (t) => {
  if (!opensslBin || !ctx.server) return t.skip(SKIP_REASON);
  const err = await getVia(new https.Agent()).then(() => null, (e) => e);
  assert.ok(err, 'expected the default agent to fail certificate verification');
  assert.match(`${err.code || ''} ${err.message || ''}`, /self[-_ ]?signed|certificat|tls/i,
    `error should be a TLS certificate error, got: ${err.code} ${err.message}`);
});

test('sandboxTlsAgent() without TEST_TLS_CA or TEST_DATA throws naming TEST_TLS_CA', (t) => {
  if (!opensslBin || !ctx.server) return t.skip(SKIP_REASON);
  resetSandboxTlsAgent();
  delete process.env.TEST_TLS_CA;
  delete process.env.TEST_DATA;
  delete process.env.DATAWATCH_TEST_DATA;
  t.after(() => { restoreEnv(); resetSandboxTlsAgent(); });
  assert.throws(() => sandboxTlsAgent(), /TEST_TLS_CA/);
});
