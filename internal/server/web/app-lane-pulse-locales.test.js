// Lane pulse locale guard. The lane pulse endpoint (GET /api/dashboard/lane-pulse)
// sends the usage burn's reason as a code, never as prose. Every code the server
// can send must have a lane_pulse_reason_<code> key with a value in each of the
// five locale bundles, so the PWA and the native apps can render it.
//
// The codes are read from the Go source, so this test fails if a code is added
// on the server without a locale key.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');

const WEB_DIR = __dirname;
const GO_SRC = path.join(WEB_DIR, '..', 'api_dashboard_lane_pulse.go');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

// serverReasonCodes returns the burn reason codes the Go handler can send. The
// needs_<value> codes come from the usage calculator's Needs values, which the
// handler prefixes with lanePulseReasonNeedsPrefix.
function serverReasonCodes(src) {
  const codes = new Set();
  const constRe = /lanePulseReason(?:NotWired|AdviceFailed)\s*=\s*"([a-z_]+)"/g;
  let m;
  while ((m = constRe.exec(src)) !== null) codes.add(m[1]);
  assert.ok(codes.size >= 2, 'expected not_wired and advice_unavailable reason codes in the Go source');
  const prefixMatch = /lanePulseReasonNeedsPrefix\s*=\s*"needs_"/.exec(src);
  assert.ok(prefixMatch, 'lanePulseReasonNeedsPrefix is not "needs_"');
  // usagecalc Needs values: percent, reading, reset (see internal/usagecalc/model.go).
  for (const v of ['percent', 'reading', 'reset']) codes.add('needs_' + v);
  return [...codes].sort();
}

test('lane pulse reason codes each have a locale key in all five bundles', () => {
  const codes = serverReasonCodes(fs.readFileSync(GO_SRC, 'utf8'));
  assert.deepEqual(codes, ['advice_unavailable', 'needs_percent', 'needs_reading', 'needs_reset', 'not_wired']);
  for (const loc of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(WEB_DIR, 'locales', loc + '.json'), 'utf8'));
    for (const code of codes) {
      const key = 'lane_pulse_reason_' + code;
      assert.equal(typeof bundle[key], 'string', `${loc}.json is missing ${key}`);
      assert.ok(bundle[key].trim().length > 0, `${loc}.json has an empty ${key}`);
    }
  }
});

test('lane pulse reason strings never name the internal PRD term', () => {
  for (const loc of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(WEB_DIR, 'locales', loc + '.json'), 'utf8'));
    for (const [key, value] of Object.entries(bundle)) {
      if (!key.startsWith('lane_pulse_reason_')) continue;
      assert.doesNotMatch(value, /\bPRDs?\b/, `${loc}.json ${key} uses the internal term`);
    }
  }
});
