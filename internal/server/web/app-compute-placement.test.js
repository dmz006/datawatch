'use strict'
// Compute placement settings card: the missing_model policy control, the
// pending-ask queue with per-ask Approval/Rejection, per-node refusal
// reasons, the help link, and the i18n bundles behind all of it.
//
// The backend contract under test (internal/compute/placement_ask.go +
// internal/server/compute_placement_asks.go):
//   - GET  /api/compute/placements          → { asks: [PlacementAsk…], pending }
//   - POST /api/compute/placements/<id>/approve (200 | 409 already-decided/expired)
//   - POST /api/compute/placements/<id>/deny   (200 | 409 already-decided/expired)
//   - the policy values live under compute_placement.* and save through
//     the same PUT /api/config path as every other Settings > General field
//   - PlacementAsk.reason is the operator-facing multi-line string: one
//     line per checked node, node name first — the UI must render it line
//     by line, not invent its own wording
//
// Run: node --test app-compute-placement.test.js

const test = require('node:test')
const assert = require('node:assert/strict')
const path = require('path')
const fs = require('fs')
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub')

const here = __dirname

function newSandbox() {
  const sandbox = buildSandbox()
  vm.createContext(sandbox)
  loadScript(path.join(here, 'app.js'), sandbox)
  return sandbox
}

// okResp is the full response object the app inspects: ok/status/
// statusText/json/text (apiFetch's failure path calls r.text()).
function okResp(payload) {
  return {
    ok: true, status: 200, statusText: 'OK',
    json: () => Promise.resolve(payload),
    text: () => Promise.resolve(JSON.stringify(payload)),
  }
}

// routeFetch installs a fetch stub keyed by URL substring and records every
// call (URL, parsed body, Authorization) for the assertions.
function routeFetch(sandbox, routes) {
  const calls = []
  sandbox.fetch = function (url, opts) {
    const u = String(url)
    const body = opts && opts.body ? JSON.parse(String(opts.body)) : null
    calls.push({ url: u, body, auth: opts && opts.headers ? String(opts.headers['Authorization'] || '') : '' })
    for (const [needle, resp] of routes) {
      if (u.includes(needle)) {
        if (resp instanceof Function) return Promise.resolve(resp(u, opts)).then(payload => okResp(payload))
        if (resp === null) return Promise.resolve({ ok: true, status: 204, statusText: '', text: () => Promise.resolve('') })
        if (resp && resp.errStatus) return Promise.resolve({ ok: false, status: resp.errStatus, statusText: 'stub', json: () => Promise.resolve({}), text: () => Promise.resolve('stub error') })
        return Promise.resolve(okResp(resp))
      }
    }
    return Promise.resolve(okResp(null))
  }
  return calls
}

// ── The static wiring: policy fields, help link ────────────────────

test('GENERAL_CONFIG_FIELDS lists the compute_placement policy section', () => {
  const sandbox = newSandbox()
  // JSON round-trip: values come out of another vm realm, where Array
  // prototypes differ and deepStrictEqual refuses to match them.
  const sec = JSON.parse(vm.runInContext(
    `JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === 'compute_placement') || null)`,
    sandbox,
  ))
  assert_sec(sec)
})

function assert_sec(sec) {
  assert.ok(sec, 'the Compute placements section must exist in GENERAL_CONFIG_FIELDS')
  assert.strictEqual(sec.docs, 'howto/compute-placement-ask.md')

  const by = {}
  sec.fields.forEach(f => { by[f.key] = f })

  const missing = by['compute_placement.missing_model']
  assert.ok(missing, 'missing_model policy field is missing')
  assert.strictEqual(missing.type, 'select')
  assert.deepStrictEqual(missing.options, ['ask', 'auto_load', 'refuse'])
  assert.deepStrictEqual(Object.keys(missing.optionLabelKeys), ['ask', 'auto_load', 'refuse'])
  Object.keys(missing.optionLabelKeys).forEach(o => {
    assert.match(missing.optionLabelKeys[o], /^options_compute_placement_/, `option '${o}' must map to a human label key`)
  })

  const mem = by['compute_placement.mem_headroom_pct']
  assert.ok(mem, 'mem_headroom_pct is missing')
  assert.strictEqual(mem.type, 'number')
  assert.strictEqual(mem.labelKey, 'settings_compute_placement_mem_headroom_pct')

  const disk = by['compute_placement.disk_headroom_gb']
  assert.ok(disk, 'disk_headroom_gb is missing')
  assert.strictEqual(disk.type, 'number')

  const timeout = by['compute_placement.ask_timeout_minutes']
  assert.ok(timeout, 'ask_timeout_minutes is missing')
  assert.strictEqual(timeout.type, 'number')
  assert.ok(timeout.min >= 1, 'an ask that expires before it asked is a defect, not a policy — the UI floor must be >= 1')
}

test('DOCS_LINKS_BY_KEY points the placement help links at the howto', () => {
  const sandbox = newSandbox()
  const entries = vm.runInContext(
    `JSON.stringify(Object.entries(DOCS_LINKS_BY_KEY).filter(([, v]) => String(v).includes('compute-placement-ask.md')))`,
    sandbox,
  )
  const map = JSON.parse(entries)
  assert.ok(map.length >= 1, 'at least one help-link entry must target the howto')
  map.forEach(([k]) => assert.match(k, /placement/i, 'the link key should name the feature'))
})

// ── Saving the policy goes through the same PUT as every settings field ─

test('saveGeneralField sends the placement policy through authenticated PUT /api/config', async () => {
  const sandbox = newSandbox()
  vm.runInContext(`localStorage.setItem('cs_token','tok')`, sandbox)

  const calls = routeFetch(sandbox, [])
  vm.runInContext(`saveGeneralField('compute_placement.missing_model','refuse')`, sandbox)
  await flushAsync(20)

  assert.ok(calls.length >= 1, 'PUT /api/config must fire')
  const call = calls.find(c => c.url.endsWith('/api/config'))
  assert.ok(call, 'the write must hit /api/config')
  assert.deepStrictEqual(call.body, { 'compute_placement.missing_model': 'refuse' },
    'the daemon reads compute_placement.* dot-path keys; the PUT body must carry the full dot-path, nothing else')
  assert.match(call.auth, /^Bearer tok$/, 'authenticated like every other settings write')
})

// ── The card: controls pre-filled from config, the ask queue ────────

test('loadGeneralConfig renders the placements card and the ask queue skeleton', async () => {
  const sandbox = newSandbox()
  const card = makeStubElement()
  const body = makeStubElement()
  sandbox.document.getElementById = (id) => {
    if (id === 'gcfg_compute_placement') return card
    if (id === 'place_asks_body') return body
    const empty = makeStubElement()
    return empty
  }

  routeFetch(sandbox, [
    ['/api/config', {
      compute_placement: {
        missing_model: 'ask',
        mem_headroom_pct: 10,
        disk_headroom_gb: 5,
        ask_timeout_minutes: 30,
      },
    }],
    ['/api/compute/placements', { asks: [], pending: 0 }],
  ])

  vm.runInContext(`loadGeneralConfig()`, sandbox)
  await flushAsync(20)

  const html = String(card.innerHTML)
  assert.ok(html.length > 0, 'the card must render')
  assert.ok(html.includes('compute-placement-ask.md'), 'the help link must target the howto')

  // the policy control: all three modes offered, the stored one pre-selected
  assert.ok(html.includes('value="ask"'), 'select must offer "ask"')
  assert.ok(html.includes('value="auto_load"'), 'select must offer "auto_load"')
  assert.ok(html.includes('value="refuse"'), 'select must offer "refuse"')
  assert.ok(html.includes('selected'), 'the stored policy value carries pre-selection')

  // the numeric controls carry the daemon's floors
  assert.ok(html.includes('min="0"'), 'headroom floors at 0')
  assert.ok(html.includes('min="1"'), 'an ask timeout of 1 min is the UI floor')

  // the ask queue is inside the card (re-loading it refreshes the queue)
  assert.ok(html.includes('id="place_asks"'), 'the pending-asks block must live in the settings card')
  assert.ok(body.innerHTML.length > 0, 'the queue skeleton renders')
})

test('loadComputePlacementAsks renders an ask with its per-node refusal reasons', async () => {
  const sandbox = newSandbox()
  const body = makeStubElement()
  sandbox.document.getElementById = (id) => (id === 'place_asks_body' ? body : makeStubElement())

  const ask = {
    id: 'ask-7',
    model: 'qwen3-coder-30b-a3b',
    node: 'node-a',
    reason: 'node-b: not enough disk for the headroom\nnode-c: not enough memory\nnode-a: chosen, has room',
    model_size_gb: 130,
    requested_at: '2026-11-01T21:00:00Z',
    expires_at: '2026-11-01T21:30:00Z',
    decision: 'pending',
  }
  routeFetch(sandbox, [
    ['/api/compute/placements', { asks: [ask], pending: 1 }],
    ['/api/compute/placements/ask-7/approve', null],
    ['/api/compute/placements/ask-7/deny', null],
    ['/api/config', {}], ['/api/llms', []], ['/api/interfaces', []],
  ])

  vm.runInContext(`loadComputePlacementAsks()`, sandbox)
  await flushAsync(20)

  const html = String(body.innerHTML)
  assert.ok(html.includes('ask-7'), 'the ask id is the key — decisions route through it')
  assert.ok(html.includes('qwen3-coder-30b-a3b'), 'the model is shown')
  assert.ok(html.includes('node-a'), 'the chosen node is shown')
  assert.ok(html.includes('2026-11-01T21:30:00Z'), 'the expiry is shown')
  // per-node refusal reasons render line by line, verbatim, node name first
  assert.ok(html.includes('node-b: not enough disk for the headroom'), 'refusal line for node-b')
  assert.ok(html.includes('node-c: not enough memory'), 'refusal line for node-c')
  assert.ok(html.includes('white-space:pre-line'), 'line breaks are preserved, not collapsed')
  // the two decisions
  assert.ok(html.includes("decidePlacement('ask-7','approve')"), 'Approve button present')
  assert.ok(html.includes("decidePlacement('ask-7','deny')"), 'Deny button present')
})

test('placementAskRow shows a decided ask as text, with no buttons', async () => {
  const sandbox = newSandbox()
  const askJson = JSON.stringify({ id: 'ask-8', model: 'm', node: 'n', reason: '', model_size_gb: 0,
    requested_at: 't', expires_at: '', decision: 'approved', decided_by: 'operator' })
  const out = JSON.parse(vm.runInContext(
    'JSON.stringify(placementAskRow(JSON.parse(' + JSON.stringify(askJson) + ')))',
    sandbox,
  ))
  assert.ok(out.includes('approved'), 'its decision is shown')
  assert.ok(out.includes('n ← m') || out.includes('n &larr; m') || out.includes('←'), 'the node ← model line is shown')
  assert.ok(!out.includes('decidePlacement'), 'a decided ask offers no buttons')
})

// ── Deciding, and what a conflict does ──────────────────────────────

test('decidePlacement approves then re-renders the queue from the server', async () => {
  const sandbox = newSandbox()
  const body = makeStubElement()
  sandbox.document.getElementById = (id) => (id === 'place_asks_body' ? body : makeStubElement())

  let placementsCalls = 0
  const ask = { id: 'ask-7', model: 'm', node: 'n', reason: 'n: fits', model_size_gb: 1, requested_at: 't', expires_at: '', decision: 'pending' }
  const calls = routeFetch(sandbox, [
    ['/api/compute/placements/ask-7/approve', null],
    ['/api/compute/placements', () => {
      placementsCalls++
      // the server lists the ask while pending and no longer lists it after the decision
      return placementsCalls <= 1 ? { asks: [ask], pending: 1 } : { asks: [], pending: 0 }
    }],
    ['/api/config', {}], ['/api/llms', []], ['/api/interfaces', []],
  ])

  vm.runInContext(`loadComputePlacementAsks()`, sandbox)
  await flushAsync(20)
  assert.ok(String(body.innerHTML).includes('ask-7'), 'the queue shows the pending ask before the decision')

  vm.runInContext(`decidePlacement('ask-7','approve')`, sandbox)
  await flushAsync(20)

  const put = calls.find(c => c.url.endsWith('/ask-7/approve'))
  assert.ok(put, 'the decision must POST to /api/compute/placements/ask-7/approve')
  assert.ok(String(body.innerHTML).includes('No placement asks pending.'),
    'after a successful approve the queue re-renders empty from the server, not from stale DOM')
})

test('decidePlacement still re-renders when the server says the ask is already decided', async () => {
  const sandbox = newSandbox()
  const body = makeStubElement()
  sandbox.document.getElementById = (id) => (id === 'place_asks_body' ? body : makeStubElement())

  const calls = routeFetch(sandbox, [
    ['/api/compute/placements/ask-7/deny', { errStatus: 409, message: 'unknown ask id (already decided or expired)' }],
    ['/api/compute/placements', { asks: [], pending: 0 }],
    ['/api/config', {}], ['/api/llms', []], ['/api/interfaces', []],
  ])

  vm.runInContext(`decidePlacement('ask-7','deny')`, sandbox)
  await flushAsync(20)

  const call = calls.find(c => c.url.endsWith('/ask-7/deny'))
  assert.ok(call, 'the deny must POST to /api/compute/placements/ask-7/deny')
  assert.ok(String(body.innerHTML).includes('No placement asks pending.'),
    'even on a 409 the queue refreshes from the server — no stale row left behind')
})

// ── i18n: every new t() key exists in every bundle ──────────────────

test('every new t() key exists in all five locale bundles', () => {
  const locales = ['en', 'de', 'es', 'fr', 'ja']
  const required = [
    'settings_compute_placement_section',
    'settings_compute_placement_missing_model',
    'settings_compute_placement_missing_model_hint',
    'options_compute_placement_ask',
    'options_compute_placement_auto_load',
    'options_compute_placement_refuse',
    'settings_compute_placement_mem_headroom_pct',
    'settings_compute_placement_disk_headroom_gb',
    'settings_compute_placement_ask_timeout_minutes',
    'settings_compute_placement_asks_title',
    'settings_compute_placement_no_pending',
    'settings_compute_placement_load_failed',
    'settings_compute_placement_decided',
    'settings_compute_placement_decision_failed',
    'placement_btn_approve',
    'placement_btn_deny',
  ]
  for (const loc of locales) {
    const bundle = JSON.parse(fs.readFileSync(path.join(here, 'locales', loc + '.json'), 'utf8'))
    for (const key of required) {
      assert.ok(bundle[key], `locales/${loc}.json is missing ${key}`)
      assert.match(String(bundle[key]), /\S/, `locales/${loc}.json ${key} must not be empty`)
    }
  }
})
