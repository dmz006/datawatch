// Voice transcription failure handling: classification of server failures,
// the store of failed recordings, and retry with the very same Blob.
// Tests _voiceClassifyFailure, _voiceKeepFailed/_voiceGetFailed/
// _voiceDiscardFailed/_voiceFailedCount, _voiceTranscribeBlob and _voiceRetry.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

// FormData stand-in that records entries untouched, so a test can assert the
// 'audio' entry is the exact Blob that was passed in.
class StubFormData {
  constructor() { this.entries = []; }
  append(name, value, filename) { this.entries.push({ name, value, filename }); }
  get(name) { const e = this.entries.find((x) => x.name === name); return e ? e.value : null; }
}

function jsonResponse(status, obj) {
  const text = JSON.stringify(obj);
  return { ok: status >= 200 && status < 300, status, text: async () => text, json: async () => JSON.parse(text) };
}
function textResponse(status, text) {
  return { ok: status >= 200 && status < 300, status, text: async () => text, json: async () => JSON.parse(text) };
}

// loadAppJS returns a sandbox whose fetch replays `responses` in order (an
// Error instance is thrown instead of returned) and records each call.
function loadAppJS(responses = []) {
  const sandbox = buildSandbox();
  sandbox.WebSocket.CONNECTING = 0;
  sandbox.WebSocket.OPEN = 1;
  sandbox.WebSocket.CLOSING = 2;
  sandbox.WebSocket.CLOSED = 3;
  sandbox.FormData = StubFormData;
  sandbox.calls = [];
  const queue = responses.slice();
  sandbox.fetch = async (url, init) => {
    sandbox.calls.push({ url, init });
    const next = queue.shift();
    if (next instanceof Error) throw next;
    return next;
  };
  sandbox.enqueue = (r) => queue.push(r);
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  for (const fn of ['_voiceClassifyFailure', '_voiceKeepFailed', '_voiceGetFailed', '_voiceDiscardFailed',
    '_voiceFailedCount', '_voiceTranscribeBlob', '_voiceRetry']) {
    assert.equal(typeof sandbox[fn], 'function', fn + ' is not defined');
  }
  return sandbox;
}

const newBlob = (label = 'audio') => ({ size: 5, label });
const opts = (extra = {}) => ({ filename: 'voice.webm', sessionId: 'abcd', targetId: 'sessionInput', ...extra });

test('a 502 JSON timeout keeps the blob and returns voice_error_timeout', async () => {
  const sb = loadAppJS([jsonResponse(502, { error: 'transcription failed', cause: 'timeout', hint: 'h', detail: 'deadline exceeded', latency_ms: 1 })]);
  const blob = newBlob();
  const r = await sb._voiceTranscribeBlob(blob, opts());
  assert.equal(r.ok, false);
  assert.equal(r.cause, 'timeout');
  assert.equal(r.messageKey, 'voice_error_timeout');
  assert.equal(r.detail, 'deadline exceeded');
  assert.equal(sb._voiceFailedCount(), 1);
  const kept = sb._voiceGetFailed(r.keepId);
  assert.equal(kept.blob, blob);
  assert.equal(kept.filename, 'voice.webm');
  assert.equal(kept.sessionId, 'abcd');
  assert.equal(kept.targetId, 'sessionInput');
  assert.equal(kept.cause, 'timeout');
});

test('killed and out_of_memory both map to voice_error_killed', async () => {
  const sb = loadAppJS([
    jsonResponse(502, { cause: 'killed' }),
    jsonResponse(502, { cause: 'out_of_memory' }),
  ]);
  const a = await sb._voiceTranscribeBlob(newBlob(), opts());
  const b = await sb._voiceTranscribeBlob(newBlob(), opts());
  assert.equal(a.messageKey, 'voice_error_killed');
  assert.equal(b.messageKey, 'voice_error_killed');
  assert.equal(b.cause, 'out_of_memory');
});

test('ffmpeg_missing, backend_error and unknown causes map to their keys', () => {
  const sb = loadAppJS();
  const c = (cause) => sb._voiceClassifyFailure(502, JSON.stringify({ cause }));
  assert.equal(c('ffmpeg_missing').messageKey, 'voice_error_ffmpeg_missing');
  assert.equal(c('backend_error').messageKey, 'voice_error_backend');
  assert.equal(c('unknown').messageKey, 'voice_error_backend');
  assert.equal(c('something_new').messageKey, 'voice_error_backend');
});

test('HTTP 413 maps to voice_error_too_large', async () => {
  const sb = loadAppJS([textResponse(413, 'request entity too large')]);
  const r = await sb._voiceTranscribeBlob(newBlob(), opts());
  assert.equal(r.ok, false);
  assert.equal(r.messageKey, 'voice_error_too_large');
  assert.equal(sb._voiceFailedCount(), 1);
});

test('a text/plain 502 maps to backend with the text as detail', async () => {
  const sb = loadAppJS([textResponse(502, 'whisper exploded\n')]);
  const r = await sb._voiceTranscribeBlob(newBlob(), opts());
  assert.equal(r.ok, false);
  assert.equal(r.messageKey, 'voice_error_backend');
  assert.equal(r.detail, 'whisper exploded\n');
  assert.equal(sb._voiceFailedCount(), 1);
});

test('a rejected fetch (TypeError) maps to network and keeps the blob', async () => {
  const sb = loadAppJS([new TypeError('Failed to fetch')]);
  const blob = newBlob();
  const r = await sb._voiceTranscribeBlob(blob, opts());
  assert.equal(r.ok, false);
  assert.equal(r.cause, 'network');
  assert.equal(r.messageKey, 'voice_error_network');
  assert.equal(sb._voiceGetFailed(r.keepId).blob, blob);
  assert.equal(sb._voiceClassifyFailure(0, '').messageKey, 'voice_error_network');
});

test('retry resends the very same Blob, returns the transcript and empties the store', async () => {
  const sb = loadAppJS([jsonResponse(502, { cause: 'timeout' }), jsonResponse(200, { transcript: 'hello world' })]);
  const blob = newBlob();
  const first = await sb._voiceTranscribeBlob(blob, opts());
  assert.equal(sb._voiceFailedCount(), 1);
  const second = await sb._voiceRetry(first.keepId);
  assert.deepEqual({ ok: second.ok, transcript: second.transcript }, { ok: true, transcript: 'hello world' });
  assert.equal(sb.calls.length, 2);
  assert.equal(sb.calls[1].init.body.get('audio'), blob);
  assert.equal(sb.calls[1].init.body.get('session_id'), 'abcd');
  assert.equal(sb._voiceFailedCount(), 0);
  assert.equal(sb._voiceGetFailed(first.keepId), undefined);
});

test('a retry that fails again keeps exactly one entry', async () => {
  const sb = loadAppJS([jsonResponse(502, { cause: 'timeout' }), jsonResponse(502, { cause: 'killed' })]);
  const blob = newBlob();
  const first = await sb._voiceTranscribeBlob(blob, opts());
  const second = await sb._voiceRetry(first.keepId);
  assert.equal(second.ok, false);
  assert.equal(second.keepId, first.keepId);
  assert.equal(second.messageKey, 'voice_error_killed');
  assert.equal(sb._voiceFailedCount(), 1);
  assert.equal(sb._voiceGetFailed(first.keepId).blob, blob);
  assert.equal(sb._voiceGetFailed(first.keepId).cause, 'killed');
});

test('discard removes the entry', async () => {
  const sb = loadAppJS([jsonResponse(502, { cause: 'timeout' })]);
  const r = await sb._voiceTranscribeBlob(newBlob(), opts());
  sb._voiceDiscardFailed(r.keepId);
  assert.equal(sb._voiceFailedCount(), 0);
  assert.equal(sb._voiceGetFailed(r.keepId), undefined);
});

test('a success on the first try never stores anything', async () => {
  const sb = loadAppJS([jsonResponse(200, { transcript: 'first try' })]);
  const r = await sb._voiceTranscribeBlob(newBlob(), opts());
  assert.deepEqual({ ok: r.ok, transcript: r.transcript }, { ok: true, transcript: 'first try' });
  assert.equal(sb._voiceFailedCount(), 0);
});

test('two failures keep two distinct entries', async () => {
  const sb = loadAppJS([jsonResponse(502, { cause: 'timeout' }), jsonResponse(502, { cause: 'timeout' })]);
  const a = newBlob('a');
  const b = newBlob('b');
  const ra = await sb._voiceTranscribeBlob(a, opts());
  const rb = await sb._voiceTranscribeBlob(b, opts());
  assert.notEqual(ra.keepId, rb.keepId);
  assert.equal(sb._voiceFailedCount(), 2);
  assert.equal(sb._voiceGetFailed(ra.keepId).blob, a);
  assert.equal(sb._voiceGetFailed(rb.keepId).blob, b);
});

test('the request carries the bearer token, session id and no abort signal', async () => {
  const sb = loadAppJS([jsonResponse(200, { transcript: 'x' })]);
  sb.localStorage.setItem('cs_token', 'tok123');
  await sb._voiceTranscribeBlob(newBlob(), opts());
  const { url, init } = sb.calls[0];
  assert.equal(url, '/api/voice/transcribe');
  assert.equal(init.method, 'POST');
  assert.equal(init.headers.Authorization, 'Bearer tok123');
  assert.equal(init.signal, undefined);
  assert.ok(init.body.get('ts_client'));
});

test('retry of an unknown id fails without calling fetch', async () => {
  const sb = loadAppJS();
  const r = await sb._voiceRetry(999);
  assert.equal(r.ok, false);
  assert.equal(sb.calls.length, 0);
});
