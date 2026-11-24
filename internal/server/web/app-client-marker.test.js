'use strict'
// The web app tells the daemon it is the web app. Every apiFetch call carries
// X-Datawatch-Client: pwa, so a pause resume or an attempt-count force reset
// made from the app raises no operator alert (the daemon still audits it with
// the marker), while a script or CLI call without it does. The marker is not a
// secret; it can only suppress the alert.
//
// Run: node --test app-client-marker.test.js

const test = require('node:test')
const assert = require('node:assert/strict')
const path = require('path')
const fs = require('fs')
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub')

function newSandbox() {
  const sandbox = buildSandbox()
  vm.createContext(sandbox)
  loadScript(path.join(__dirname, 'app.js'), sandbox)
  return sandbox
}

function recordFetch(sandbox) {
  const calls = []
  sandbox.fetch = function (url, opts) {
    calls.push({ url: String(url), method: opts && opts.method, headers: Object.assign({}, opts && opts.headers), body: opts && opts.body ? JSON.parse(String(opts.body)) : null })
    return Promise.resolve({ ok: true, status: 200, statusText: 'OK', json: () => Promise.resolve({}), text: () => Promise.resolve('{}') })
  }
  return calls
}

test('apiFetch sends the web app client marker on every call', async () => {
  const sandbox = newSandbox()
  vm.runInContext(`localStorage.setItem('cs_token','tok')`, sandbox)
  const calls = recordFetch(sandbox)
  await vm.runInContext(`apiFetch('/api/pause')`, sandbox)
  await vm.runInContext(`apiFetch('/api/pause/resume', { method: 'POST', body: JSON.stringify({ scope: 'global' }) })`, sandbox)
  assert.equal(calls.length, 2)
  for (const c of calls) {
    assert.equal(c.headers['X-Datawatch-Client'], 'pwa', c.url + ' must carry the marker')
    assert.equal(c.headers['Authorization'], 'Bearer tok', 'the credential is unchanged')
  }
})

test('the marker is sent with the force reset buttons too', async () => {
  const sandbox = newSandbox()
  const calls = recordFetch(sandbox)
  vm.runInContext(`prdRequeueTask('p1', 't1')`, sandbox)
  await flushAsync(20)
  const call = calls.find(c => c.url.endsWith('/api/autonomous/prds/p1/reset_task'))
  assert.ok(call, 'the requeue must hit reset_task')
  assert.equal(call.headers['X-Datawatch-Client'], 'pwa')
  assert.equal(call.body.force, true)
})

test('the marker is a plain, non-secret constant, not derived from the token', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8')
  assert.match(src, /headers\['X-Datawatch-Client'\] = 'pwa';/)
})

test('the audit setting has a label in every locale', () => {
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    const j = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'))
    assert.ok(j.settings_audit_safeguard_alert && j.settings_audit_safeguard_alert.length > 5, l + ' lacks settings_audit_safeguard_alert')
  }
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'))
  const de = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'de.json'), 'utf8'))
  assert.notEqual(de.settings_audit_safeguard_alert, en.settings_audit_safeguard_alert, 'de must be translated')
})
