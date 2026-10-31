// Settings → Automata → Identity card: the Save / Revert / Reset / Restore
// buttons, the explicit Reset behind a confirmation, the Restore list and its
// POST, the empty list message, the daemon's 409 identity_wipe_refused sentence
// shown as the error toast with the typed text left alone, and the
// identity.backups_to_keep field. fetch is stubbed and records every request;
// rendered text is the real English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const REFUSAL = 'refusing to replace the saved identity with an empty one; to clear it on purpose use POST /api/identity/reset';

// loadApp returns a sandbox with the app loaded and a recording fetch. routes
// maps "METHOD /path" to {status, body}; an unknown request answers 404. Every
// request is recorded in __calls with its parsed JSON body; every toast in
// __toasts; the text of every confirmation dialog in __confirms.
function loadApp(routes, byId) {
  const ids = byId || {};
  const sandbox = buildSandbox({ byId: ids });
  const calls = [];
  const confirms = [];
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method) || 'GET';
    calls.push({ url, method, body: opts && opts.body ? JSON.parse(opts.body) : undefined });
    const hit = routes[method + ' ' + url];
    if (!hit) return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    const status = hit.status || 200;
    return Promise.resolve({
      ok: status < 400,
      status,
      statusText: String(status),
      json: () => Promise.resolve(hit.body),
      text: () => Promise.resolve(typeof hit.body === 'string' ? hit.body : JSON.stringify(hit.body)),
    });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  // showConfirmModal builds its dialog with createElement and wires the Yes
  // button by id: record the dialog text and expose the button as byId.
  sandbox.document.createElement = () => {
    const el = makeStubElement();
    el.removed = false;
    el.remove = () => { el.removed = true; };
    confirms.push(el);
    return el;
  };
  ids.confirmYesBtn = makeStubElement();
  sandbox.__calls = calls;
  sandbox.__confirms = confirms;
  sandbox.__toasts = [];
  sandbox.showToast = (message, type) => { sandbox.__toasts.push({ message, type }); };
  return sandbox;
}

// field is a form control whose value the test sets.
const field = value => ({ value, removed: false, remove() { this.removed = true; } });

const IDENTITY = { role: 'platform engineer', north_star_goals: ['ship'], current_projects: [], values: [], current_focus: '', context_notes: '' };

const BACKUPS = {
  keep: 10,
  backups: [
    { name: 'identity-20261029T101500.000Z.yaml', created_at: '2026-10-29T10:15:00Z', size_bytes: 120, role: 'platform engineer', field_count: 3 },
    { name: 'identity-20261028T080000.000Z.yaml', created_at: '2026-10-28T08:00:00Z', size_bytes: 90, role: '<b>x</b>', field_count: 1 },
  ],
};

const posts = sb => sb.__calls.filter(c => c.method === 'POST');
const lastConfirmText = sb => sb.__confirms.filter(m => m.innerHTML).pop().innerHTML;

// renderCard renders the form into a panel and resolves the env.
async function renderCard(extraRoutes, extraIds) {
  const panel = makeStubElement();
  const byId = Object.assign({ identityPanel: panel }, extraIds || {});
  const routes = Object.assign({
    'GET /api/identity': { body: IDENTITY },
    'GET /api/config': { body: { identity: { backups_to_keep: 7 } } },
  }, extraRoutes || {});
  const sb = loadApp(routes, byId);
  sb._renderIdentityForm(panel, IDENTITY);
  await flushAsync();
  return { sb, panel, byId };
}

test('Identity card shows Save, Revert, Reset and Restore', async () => {
  const { panel } = await renderCard();
  const html = panel.innerHTML;
  for (const [fn, label] of [['saveIdentity()', 'Save'], ['loadIdentityPanel()', 'Revert'], ['identityReset()', 'Reset'], ['identityRestoreOpen()', 'Restore']]) {
    assert.match(html, new RegExp('onclick="' + fn.replace(/[()]/g, '\\$&') + '"[^>]*>' + label + '<'), label + ' button');
  }
});

test('Identity card shows the backups to keep field with its help link and the loaded value', async () => {
  const keep = field('');
  const { panel } = await renderCard({}, { idBackupsKeep: keep });
  const html = panel.innerHTML;
  assert.match(html, /id="idBackupsKeep" type="number" min="1" step="1" placeholder="10"/);
  assert.match(html, /Identity backups to keep/);
  assert.match(html, /href="\/diagrams\.html#docs\/datawatch-definitions\.md#identity-backups-and-restore"/);
  assert.equal(keep.value, '7');
});

test('Identity backups to keep saves through PUT /api/config', async () => {
  const keep = field('12');
  const { sb } = await renderCard({ 'PUT /api/config': { body: {} } }, { idBackupsKeep: keep });
  keep.value = '12';
  sb.saveIdentityBackupsKeep();
  await flushAsync();
  const put = sb.__calls.filter(c => c.method === 'PUT');
  assert.equal(put.length, 1);
  assert.equal(put[0].url, '/api/config');
  assert.deepEqual(put[0].body, { 'identity.backups_to_keep': 12 });
  assert.deepEqual(sb.__toasts.pop(), { message: 'Backup limit saved', type: 'success' });
});

test('Identity backups to keep refuses a value below 1 without sending', async () => {
  const keep = field('0');
  const { sb } = await renderCard({}, { idBackupsKeep: keep });
  keep.value = '0';
  sb.saveIdentityBackupsKeep();
  await flushAsync();
  assert.equal(sb.__calls.filter(c => c.method === 'PUT').length, 0);
  assert.equal(sb.__toasts.pop().type, 'error');
});

test('Identity Reset declined sends nothing', async () => {
  const { sb, byId } = await renderCard();
  sb.identityReset();
  assert.match(lastConfirmText(sb), /A backup is kept, and Restore can bring it back/);
  // The operator clicks No: the Yes handler never runs.
  assert.equal(typeof byId.confirmYesBtn.onclick, 'function');
  await flushAsync();
  assert.equal(posts(sb).length, 0);
});

test('Identity Reset confirmed posts /api/identity/reset and reloads the card', async () => {
  const { sb, byId } = await renderCard({ 'POST /api/identity/reset': { body: {} } });
  const before = sb.__calls.length;
  sb.identityReset();
  byId.confirmYesBtn.onclick();
  await flushAsync();
  const after = sb.__calls.slice(before);
  assert.deepEqual(posts(sb).map(c => c.url), ['/api/identity/reset']);
  assert.ok(after.some(c => c.method === 'GET' && c.url === '/api/identity'), 'the panel reloads');
  assert.deepEqual(sb.__toasts.pop(), { message: 'Identity cleared; a backup was kept', type: 'success' });
});

test('Identity Restore lists the backups and posts the chosen name', async () => {
  const modal = makeStubElement();
  modal.removed = false;
  modal.remove = () => { modal.removed = true; };
  const { sb, byId } = await renderCard({
    'GET /api/identity/backups': { body: BACKUPS },
    'POST /api/identity/restore': { body: IDENTITY },
  }, { identityRestoreModal: modal });
  sb.identityRestoreOpen();
  await flushAsync();
  const html = modal.innerHTML;
  assert.match(html, /Restore identity from a backup/);
  assert.match(html, /platform engineer/);
  assert.match(html, /3 fields filled in/);
  assert.match(html, /1 fields filled in/);
  assert.match(html, /data-backup="identity-20261029T101500\.000Z\.yaml"/);
  assert.match(html, /data-backup="identity-20261028T080000\.000Z\.yaml"/);
  // A server string is escaped, never injected.
  assert.ok(!html.includes('<b>x</b>'), 'role is escaped');
  assert.match(html, /&lt;b&gt;x&lt;\/b&gt;/);

  sb.identityRestoreChoose('identity-20261029T101500.000Z.yaml', 'Oct 29');
  assert.match(lastConfirmText(sb), /Restore the identity saved on Oct 29/);
  assert.equal(posts(sb).length, 0, 'nothing is sent before the confirmation');
  byId.confirmYesBtn.onclick();
  await flushAsync();
  assert.deepEqual(posts(sb).map(c => [c.url, c.body]), [['/api/identity/restore', { backup: 'identity-20261029T101500.000Z.yaml' }]]);
  assert.equal(modal.removed, true, 'the list closes');
  assert.deepEqual(sb.__toasts.pop(), { message: 'Identity restored', type: 'success' });
});

test('Identity Restore with no backups shows the none message', async () => {
  const modal = makeStubElement();
  const { sb } = await renderCard({ 'GET /api/identity/backups': { body: { backups: [], keep: 10 } } }, { identityRestoreModal: modal });
  sb.identityRestoreOpen();
  await flushAsync();
  assert.match(modal.innerHTML, /No identity backups yet\./);
  assert.ok(!modal.innerHTML.includes('data-backup='));
});

test('Identity save refused with 409 shows the daemon sentence and keeps the typed text', async () => {
  const typed = { idRole: field(''), idGoals: field(''), idProjects: field(''), idValues: field(''), idFocus: field(''), idNotes: field('half-typed note') };
  const { sb } = await renderCard({
    'PUT /api/identity': { status: 409, body: { error: REFUSAL, code: 'identity_wipe_refused', fields_kept: ['role'] } },
  }, typed);
  const before = sb.__calls.length;
  sb.saveIdentity();
  await flushAsync();
  assert.deepEqual(sb.__toasts.pop(), { message: REFUSAL, type: 'error' });
  assert.equal(typed.idNotes.value, 'half-typed note');
  const after = sb.__calls.slice(before);
  assert.deepEqual(after.map(c => c.method + ' ' + c.url), ['PUT /api/identity'], 'the panel is not reloaded');
});

test('Identity wizard refused with 409 shows the sentence and stays open', async () => {
  const wiz = field('');
  const input = field('some notes');
  const { sb } = await renderCard({
    'PUT /api/identity': { status: 409, body: { error: REFUSAL, code: 'identity_wipe_refused', fields_kept: [] } },
  }, { identityWizardModal: wiz, wizInput: input });
  const w = vm.runInContext('identityWizard', sb);
  w.step = w.steps.length - 1;
  sb.identityWizardNext();
  await flushAsync();
  assert.deepEqual(sb.__toasts.pop(), { message: REFUSAL, type: 'error' });
  assert.equal(wiz.removed, false, 'the wizard stays open');
  assert.equal(w.values.context_notes, 'some notes');
});

test('Identity errors that are plain text still show the text', async () => {
  const typed = { idRole: field('r') };
  const { sb } = await renderCard({ 'PUT /api/identity': { status: 500, body: 'disk full' } }, typed);
  sb.saveIdentity();
  await flushAsync();
  assert.deepEqual(sb.__toasts.pop(), { message: 'disk full', type: 'error' });
});
