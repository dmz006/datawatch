// Pins the PRD Settings modal's per-PRD git push-on-complete override:
//   - a select #prdSettingsGitPush after the base-branch input, with
//     exactly four options ("", never, branch, default_branch) where
//     "" means "inherit the daemon setting", pre-selected from
//     prd.git.push_on_complete
//   - the localised keys prd_settings_git_push_label / _inherit / _hint
//     resolved through t() (not raw strings), with the keys present in
//     all five locale bundles
//   - on save, push_on_complete is included in the POST
//     /api/autonomous/prds/<id>/set_git body only when it changed;
//     auto_pr and base_branch keep riding along as before, and the
//     request is skipped entirely when nothing git-related changed
//     (the backend treats an omitted push_on_complete as unchanged).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const PRD_ID = 'p1';

// setup returns the sandbox plus capture handles. `selectValue` is the
// value the #prdSettingsGitPush select reports on submit; `prd` is the
// GET /api/autonomous/prds/<id> payload.
function setup({ selectValue = 'never', prd = null, byId = {} } = {}) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  // Record every fetch; the GET returns the PRD, POSTs resolve empty.
  const requests = [];
  sandbox.fetch = (url, opts = {}) => {
    requests.push({ url: String(url), method: (opts && opts.method) || 'GET', body: (opts && opts.body) || null });
    if (String(url).includes('/api/autonomous/prds/')) {
      const body = JSON.stringify(prd || { id: PRD_ID });
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(JSON.parse(body)), text: () => Promise.resolve(body) });
    }
    return Promise.resolve({ ok: false, status: 404, text: () => Promise.resolve('stub: not found'), json: () => Promise.reject(new Error('no json')) });
  };
  loadScript(APP_JS, sandbox);
  // Deterministic locale: a distinctive per-key marker bundle proves the
  // labels come from t(keys) on submit/render time. _i18n is a top-level
  // lexical const in app.js, so set it via the shared context.
  vm.runInContext(`window._i18n.bundle = {
    prd_settings_git_push_label: 'TESTPUSHLABEL',
    prd_settings_git_push_inherit: 'TESTPUSHINHERIT',
    prd_settings_git_push_hint: 'TESTPUSHHINT',
  };`, sandbox);
  // Elements read by the submit handler. Defaults mirror a PRD with all
  // unset fields so only the fields under test cause diffs.
  sandbox.document.getElementById = (id) => {
    if (byId[id]) return byId[id];
    if (id === 'prdSettingsGitPush') return { value: selectValue };
    if (id === 'prdSettingsGitAutoPR') return { checked: false };
    const el = makeStubElement(); // value === '', checked === undefined
    return el;
  };
  // Capture the modal instead of mounting it; stub the post-save
  // follow-ups so their DOM work can't interfere. The modal arrives in
  // `held` (mutable) because _prdMountModal fires after the async apiFetch
  // + ensureLLMModelLists chain settles.
  const held = { modal: null };
  sandbox._prdMountModal = (html, onSubmit) => { held.modal = { html, onSubmit }; };
  sandbox.showToast = () => {};
  sandbox._refreshAutomataOrPRD = () => {};
  sandbox._prdCloseModal = () => {};

  vm.runInContext('openPRDSettingsModal(' + JSON.stringify(PRD_ID) + ')', sandbox);
  return { sandbox, requests, held };
}

// mounted flushes the apiFetch->ensureLLMModelLists->mount chain and
// resolves with { captured, requests, sandbox }.
async function mounted({ selectValue = 'never', prd = null, byId = {} } = {}) {
  const { sandbox, requests, held } = setup({ selectValue, prd, byId });
  for (let i = 0; i < 100 && !held.modal; i++) await flushAsync(1);
  if (!held.modal) await flushAsync(20);
  return { captured: held.modal, requests, sandbox };
}

test('PRD Settings modal renders the git push-on-complete select with its four options', async () => {
  const { captured, requests } = await mounted({ prd: {} });
  assert.ok(captured, 'PRD Settings modal was not mounted (check sandbox stubs)');
  const html = captured.html;
  assert.match(html, /<select[^>]*id="prdSettingsGitPush"/, 'select #prdSettingsGitPush missing');
  const opts = [...html.matchAll(/<select[^>]*id="prdSettingsGitPush"[\s\S]*?<\/select>/g)][0][0];
  const values = [...opts.matchAll(/<option value="([^"]*)"/g)].map(m => m[1]);
  assert.deepEqual(values, ['', 'never', 'branch', 'default_branch'],
    'options must be exactly "", never, branch, default_branch — got: ' + JSON.stringify(values));
  // The select sits after the base-branch input, not before it.
  assert.ok(html.indexOf('id="prdSettingsGitBaseBranch"') < html.indexOf('id="prdSettingsGitPush"'),
    'push select must come after the base-branch input');
  // t() was actually used for label/inherit/hint (marker bundle visible).
  assert.ok(html.includes('TESTPUSHLABEL'), 'label must resolve through t(\'prd_settings_git_push_label\')');
  assert.ok(html.includes('TESTPUSHINHERIT'), 'inherit option must resolve through t(\'prd_settings_git_push_inherit\')');
  assert.ok(html.includes('TESTPUSHHINT'), 'hint must resolve through t(\'prd_settings_git_push_hint\')');
  // The GET that fed the modal hit the per-PRD endpoint.
  assert.ok(requests.some(r => r.url.endsWith('/api/autonomous/prds/' + PRD_ID) && r.method === 'GET'));
});

test('pre-selected from prd.git.push_on_complete when set', async () => {
  const { captured } = await mounted({ prd: { id: PRD_ID, git: { push_on_complete: 'default_branch' } } });
  const sel = [...captured.html.matchAll(/<select[^>]*id="prdSettingsGitPush"[\s\S]*?<\/select>/g)][0][0];
  assert.match(sel, /<option value="default_branch" selected>/,
    'the current push_on_complete value must be pre-selected');
  assert.doesNotMatch(sel, /value="" selected/, 'inherit must not be selected when an override is set');
});

test('pre-selected to inherit when prd.git.push_on_complete is empty', async () => {
  const { captured } = await mounted({ prd: { id: PRD_ID, git: {} } });
  const sel = [...captured.html.matchAll(/<select[^>]*id="prdSettingsGitPush"[\s\S]*?<\/select>/g)][0][0];
  assert.match(sel, /<option value="" selected>/, 'empty push_on_complete must pre-select the inherit option');
});

test('changed push policy sends push_on_complete alongside auto_pr/base_branch', async () => {
  const prd = { id: PRD_ID, git: { auto_pr: true, base_branch: 'main', push_on_complete: 'branch' } };
  const { captured, requests } = await mounted({
    selectValue: 'never', prd,
    // The UI mirrors the PRD's current state; only push differs.
    byId: { prdSettingsGitAutoPR: { checked: true }, prdSettingsGitBaseBranch: { value: 'main' } },
  });
  captured.onSubmit();
  await flushAsync(20);
  const setGit = requests.filter(r => r.url.endsWith('/api/autonomous/prds/' + PRD_ID + '/set_git') && r.method === 'POST');
  assert.equal(setGit.length, 1, 'exactly one set_git call expected');
  const body = JSON.parse(setGit[0].body);
  assert.deepEqual(body, { auto_pr: true, base_branch: 'main', push_on_complete: 'never' },
    'changed push policy must send push_on_complete with auto_pr/base_branch');
});

test('unchanged push policy is omitted from the set_git body when only push differs', async () => {
  const prd = { id: PRD_ID, git: { auto_pr: false, base_branch: '', push_on_complete: 'branch' } };
  const { captured, requests } = await mounted({
    selectValue: 'never', prd,
    byId: { prdSettingsGitAutoPR: { checked: false }, prdSettingsGitBaseBranch: { value: '' } },
  });
  captured.onSubmit();
  await flushAsync(20);
  const setGit = requests.filter(r => r.url.endsWith('/api/autonomous/prds/' + PRD_ID + '/set_git') && r.method === 'POST');
  assert.equal(setGit.length, 1, 'exactly one set_git call expected');
  const body = JSON.parse(setGit[0].body);
  assert.deepEqual(body, { auto_pr: false, base_branch: '', push_on_complete: 'never' },
    'push-only change must still carry auto_pr/base_branch (set_git takes both unconditionally)');
});

test('unchanged push policy is omitted from the set_git body', async () => {
  const prd = { id: PRD_ID, git: { auto_pr: false, base_branch: 'main', push_on_complete: 'branch' } };
  // push unchanged at 'branch'; base_branch changes 'main' -> 'release'
  // so a set_git call fires, but push_on_complete must NOT ride along.
  const { captured, requests } = await mounted({
    selectValue: 'branch', prd,
    byId: { prdSettingsGitBaseBranch: { value: 'release' } },
  });
  captured.onSubmit();
  await flushAsync(20);
  const setGit = requests.filter(r => r.url.endsWith('/api/autonomous/prds/' + PRD_ID + '/set_git') && r.method === 'POST');
  assert.equal(setGit.length, 1, 'exactly one set_git call expected');
  const body = JSON.parse(setGit[0].body);
  assert.deepEqual(body, { auto_pr: false, base_branch: 'release' },
    'unchanged push_on_complete must be omitted (backend treats omission as no change)');
});

test('no git field changed — no set_git call at all', async () => {
  const prd = { id: PRD_ID, git: { auto_pr: false, base_branch: '', push_on_complete: 'branch' } };
  const { captured, requests } = await mounted({ selectValue: 'branch', prd });
  captured.onSubmit();
  await flushAsync(20);
  const setGit = requests.filter(r => r.url.endsWith('/set_git'));
  assert.equal(setGit.length, 0, 'set_git must not be called when nothing git-related changed');
});

test('prd_settings_git_push_* keys exist non-empty in all five locale bundles', () => {
  for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const key of ['prd_settings_git_push_label', 'prd_settings_git_push_inherit', 'prd_settings_git_push_hint']) {
      assert.ok(typeof bundle[key] === 'string' && bundle[key].length > 0,
        key + ' missing or empty in ' + locale + ' locale');
    }
  }
});
