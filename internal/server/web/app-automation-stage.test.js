// Automation member stage and auto-tags override in the web app.
//
// Covers the per-member Stage input on the form (draw, edit, payload), the
// stage chip and Rename stage action on the detail page (dialog, request,
// validation, stale prompt, hidden for a finished Automation), and the
// auto-tags override controls (draw for each state, read, payload, untouched
// form sends nothing). fetch is stubbed; rendered text is the English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const plain = value => JSON.parse(JSON.stringify(value));

function loadApp(routes, byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  const calls = [];
  const modals = [];
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
  sandbox.document.createElement = () => {
    const el = makeStubElement();
    el.remove = () => { el.removed = true; };
    modals.push(el);
    return el;
  };
  sandbox.__calls = calls;
  sandbox.__modals = modals;
  sandbox.__toasts = [];
  sandbox.showToast = (message, type) => { sandbox.__toasts.push({ message, type }); };
  return sandbox;
}

const formState = sb => vm.runInContext('_automationForm', sb);
const lastModal = sb => sb.__modals.filter(m => m.innerHTML).pop();

function member(id, status, extra) {
  return Object.assign({ id, prd_id: id + 'aaaaaaaaaaaa', status }, extra || {});
}

function automation(extra) {
  return Object.assign({
    id: 'a1b2c3d4', name: 'Release train', description: '', status: 'draft', version: 7,
    entry_member_id: 'm1',
    members: [
      member('m1', 'pending', { stage: 'build', on_complete: 'm2' }),
      member('m2', 'pending', { stage: 'review' }),
    ],
  }, extra || {});
}

// ── Stage input on the form ──────────────────────────────────────────────────

function formEnv(src) {
  const ids = {
    view: makeStubElement(),
    genericModalConfirmBtn: makeStubElement(),
    automationFormName: { value: 'Release train' },
    automationFormDescription: { value: '' },
    automationFormReason: { value: 'why' },
    automationFormError: makeStubElement(),
    automationFormSaveBtn: { disabled: false },
    automationAutoTags_enabled: { value: 'inherit' },
    automationAutoTags_automation_template: { value: '' },
    automationAutoTags_stage_template: { value: '' },
    automationAutoTags_kind_template: { value: '' },
  };
  const routes = {
    'GET /api/autonomous/prds': { body: { prds: [] } },
    'GET /api/automations': { body: [] },
  };
  const sb = loadApp(routes, ids);
  return { sb, ids, routes };
}

test('the form draws a Stage input per prd member holding the stage', async () => {
  const { sb, ids } = formEnv();
  sb.openAutomationForm(automation());
  await flushAsync();
  const html = ids.view.innerHTML;
  assert.match(html, /data-stage-for="m1" value="build"/);
  assert.match(html, /data-stage-for="m2" value="review"/);
  assert.match(html, /maxlength="64"/);
  assert.match(html, />Stage<\/span>/);
});

test('a member that cannot be edited shows its stage as text, and none when empty', () => {
  const { sb } = formEnv();
  const shown = sb._automationFormStageHTML({ id: 'm1', kind: 'prd', stage: 'build' }, false);
  assert.match(shown, /automation-member-stage">build</);
  assert.doesNotMatch(shown, /<input/);
  assert.equal(sb._automationFormStageHTML({ id: 'm1', kind: 'prd' }, false), '');
});

test('a fork and a join get no Stage field', () => {
  const { sb } = formEnv();
  assert.equal(sb._automationFormStageHTML({ id: 'f', kind: 'fork', stage: 'x' }, true), '');
  assert.equal(sb._automationFormStageHTML({ id: 'j', kind: 'join', stage: 'x' }, true), '');
});

test('typing a stage records it on the member and the payload carries it trimmed', async () => {
  const { sb } = formEnv();
  sb.openAutomationForm(automation());
  await flushAsync();
  sb.automationFormSetStage('m1', '  deploy  ');
  sb.automationFormSetStage('nope', 'ignored');
  sb.automationFormSetStage('m2', '');
  const body = plain(sb.buildAutomationFormPayload({ name: 'n', members: formState(sb).members }));
  assert.equal(body.members[0].stage, 'deploy');
  assert.equal('stage' in body.members[1], false, 'an empty stage is left out');
});

test('the payload never sends a stage for a fork or a join', () => {
  const { sb } = formEnv();
  const body = plain(sb.buildAutomationFormPayload({ name: 'n', members: [
    { id: 'f1', kind: 'fork', fork_members: ['b1'], stage: 'x' },
    { id: 'b1', prd_id: 'p', stage: 'x' },
    { id: 'j1', kind: 'join', join_members: ['b1'], stage: 'x' },
  ] }));
  assert.equal('stage' in body.members[0], false);
  assert.equal(body.members[1].stage, 'x');
  assert.equal('stage' in body.members[2], false);
});

test('saving an edit sends the stage in the PUT body', async () => {
  const { sb, routes } = formEnv();
  routes['PUT /api/automations/a1b2c3d4'] = { body: automation({ version: 8 }) };
  routes['GET /api/automations/a1b2c3d4'] = { body: automation({ version: 8 }) };
  sb.openAutomationForm(automation());
  await flushAsync();
  sb.automationFormSetStage('m1', 'ship');
  sb.saveAutomationForm();
  await flushAsync();
  const put = sb.__calls.find(c => c.method === 'PUT');
  assert.ok(put, 'a PUT was sent');
  assert.equal(put.body.members.find(m => m.id === 'm1').stage, 'ship');
});

// ── Rename stage on the detail page ──────────────────────────────────────────

async function detailEnv(a, extra) {
  const ids = {
    view: makeStubElement(),
    automationControls: makeStubElement(),
    automationStrip: makeStubElement(),
    genericModalConfirmBtn: makeStubElement(),
    automationRenameStageTo: { value: '' },
  };
  const routes = Object.assign({
    ['GET /api/automations/' + a.id]: { body: a },
    'GET /api/autonomous/prds': { body: { prds: [] } },
    'GET /api/automations': { body: [a] },
  }, extra || {});
  const sb = loadApp(routes, ids);
  sb.openAutomationDetail(a.id);
  await flushAsync();
  return { sb, ids };
}

test('a member card shows its stage chip and a Rename stage button', async () => {
  const { sb } = await detailEnv(automation());
  const html = sb._automationMemberStageHTML({ id: 'm1', stage: 'build' });
  assert.match(html, /automation-member-stage" title="Stage">build</);
  assert.match(html, /data-op="rename_stage" data-stage="build"/);
  assert.match(html, />Rename stage<\/button>/);
  assert.equal(sb._automationMemberStageHTML({ id: 'm2' }), '');
});

test('the Rename stage button is gone once the Automation is finished', async () => {
  const { sb } = await detailEnv(automation({ status: 'completed' }));
  const html = sb._automationMemberStageHTML({ id: 'm1', stage: 'build' });
  assert.match(html, /automation-member-stage/);
  assert.doesNotMatch(html, /rename_stage/);
});

test('renaming a stage posts from and to to stages/rename with the loaded version', async () => {
  const a = automation();
  const { sb, ids } = await detailEnv(a, {
    'POST /api/automations/a1b2c3d4/stages/rename': { body: automation({ version: 8 }) },
  });
  sb.openAutomationRenameStageDialog('build');
  assert.match(lastModal(sb).innerHTML, /Rename stage — build/);
  assert.match(lastModal(sb).innerHTML, /New stage name/);
  ids.automationRenameStageTo.value = '  compile ';
  ids.genericModalConfirmBtn.onclick();
  await flushAsync();
  const post = sb.__calls.find(c => c.method === 'POST');
  assert.ok(post, 'a POST was sent');
  assert.equal(post.url, '/api/automations/a1b2c3d4/stages/rename');
  assert.deepEqual(plain(post.body), { from: 'build', to: 'compile', expected_version: 7, actor: 'operator' });
});

test('an empty or unchanged new stage sends nothing and keeps the dialog open', async () => {
  const { sb, ids } = await detailEnv(automation());
  sb.openAutomationRenameStageDialog('build');
  ids.automationRenameStageTo.value = '';
  ids.genericModalConfirmBtn.onclick();
  ids.automationRenameStageTo.value = 'build';
  ids.genericModalConfirmBtn.onclick();
  await flushAsync();
  assert.equal(sb.__calls.filter(c => c.method === 'POST').length, 0);
});

test('a stage name over 64 characters is refused with a toast and sends nothing', async () => {
  const { sb, ids } = await detailEnv(automation());
  sb.openAutomationRenameStageDialog('build');
  ids.automationRenameStageTo.value = 'x'.repeat(65);
  ids.genericModalConfirmBtn.onclick();
  await flushAsync();
  assert.equal(sb.__calls.filter(c => c.method === 'POST').length, 0);
  assert.equal(sb.__toasts.length, 1);
  assert.match(sb.__toasts[0].message, /at most 64 characters/);
});

test('a stale version on rename opens the stale prompt', async () => {
  const { sb, ids } = await detailEnv(automation(), {
    'POST /api/automations/a1b2c3d4/stages/rename': { status: 409, body: 'version conflict' },
  });
  sb.openAutomationRenameStageDialog('build');
  ids.automationRenameStageTo.value = 'compile';
  ids.genericModalConfirmBtn.onclick();
  await flushAsync();
  assert.match(lastModal(sb).innerHTML, /version conflict/);
});

test('a refused rename (404 no such stage) shows the server text as a toast', async () => {
  const { sb, ids } = await detailEnv(automation(), {
    'POST /api/automations/a1b2c3d4/stages/rename': { status: 404, body: 'no member carries stage' },
  });
  sb.openAutomationRenameStageDialog('build');
  ids.automationRenameStageTo.value = 'compile';
  ids.genericModalConfirmBtn.onclick();
  await flushAsync();
  assert.match(sb.__toasts[0].message, /no member carries stage/);
  assert.equal(sb.__toasts[0].type, 'error');
});

// ── Auto-tags override on the form ───────────────────────────────────────────

test('the form section starts closed with the inherit choice and empty templates', () => {
  const { sb } = formEnv();
  const html = sb._automationAutoTagsSectionHTML(null);
  assert.match(html, /<details id="automationFormAutoTags"[^>]*>/);
  assert.doesNotMatch(html, /<details[^>]* open/);
  assert.match(html, /<option value="inherit" selected>Use the daemon setting/);
  assert.match(html, /placeholder="automation:\{automation\}"/);
  assert.match(html, /placeholder="stage:\{stage\}"/);
  assert.match(html, /placeholder="kind:\{kind\}"/);
});

test('the form section opens and shows a stored override', () => {
  const { sb } = formEnv();
  const off = sb._automationAutoTagsSectionHTML({ enabled: false, stage_template: 'phase:{stage}' });
  assert.match(off, /<details[^>]* open/);
  assert.match(off, /<option value="off" selected>Off/);
  assert.match(off, /value="phase:\{stage\}"/);
  const on = sb._automationAutoTagsSectionHTML({ enabled: true });
  assert.match(on, /<option value="on" selected>On/);
  const tpl = sb._automationAutoTagsSectionHTML({ kind_template: 'k:{kind}' });
  assert.match(tpl, /<details[^>]* open/);
});

test('hostile template text is escaped in the section', () => {
  const { sb } = formEnv();
  const html = sb._automationAutoTagsSectionHTML({ automation_template: '"><img src=x>' });
  assert.doesNotMatch(html, /<img src=x>/);
});

test('reading the override gives enabled only for on or off and templates only when filled', () => {
  const { sb, ids } = formEnv();
  assert.deepEqual(plain(sb.automationReadAutoTags()), {});
  ids.automationAutoTags_enabled.value = 'on';
  ids.automationAutoTags_stage_template.value = ' phase:{stage} ';
  assert.deepEqual(plain(sb.automationReadAutoTags()), { enabled: true, stage_template: 'phase:{stage}' });
  ids.automationAutoTags_enabled.value = 'off';
  ids.automationAutoTags_automation_template.value = 'a:{automation}';
  ids.automationAutoTags_kind_template.value = 'k:{kind}';
  assert.deepEqual(plain(sb.automationReadAutoTags()), {
    enabled: false, automation_template: 'a:{automation}', stage_template: 'phase:{stage}', kind_template: 'k:{kind}',
  });
});

test('an untouched form sends no auto_tags, a touched one sends the override', async () => {
  const { sb, ids, routes } = formEnv();
  routes['PUT /api/automations/a1b2c3d4'] = { body: automation({ version: 8 }) };
  routes['GET /api/automations/a1b2c3d4'] = { body: automation({ version: 8 }) };
  sb.openAutomationForm(automation({ auto_tags: { enabled: false } }));
  await flushAsync();
  sb.saveAutomationForm();
  await flushAsync();
  const first = sb.__calls.find(c => c.method === 'PUT');
  assert.equal('auto_tags' in first.body, false);

  sb.openAutomationForm(automation());
  await flushAsync();
  ids.automationAutoTags_enabled.value = 'off';
  sb.automationFormAutoTagsChanged();
  sb.saveAutomationForm();
  await flushAsync();
  const puts = sb.__calls.filter(c => c.method === 'PUT');
  assert.deepEqual(plain(puts[puts.length - 1].body.auto_tags), { enabled: false });
});

test('the payload helper copies a given override and omits it otherwise', () => {
  const { sb } = formEnv();
  assert.deepEqual(plain(sb.buildAutomationFormPayload({ name: 'n', autoTags: { enabled: true } }).auto_tags), { enabled: true });
  assert.equal('auto_tags' in sb.buildAutomationFormPayload({ name: 'n' }), false);
});

// ── Locales ──────────────────────────────────────────────────────────────────

test('every new stage and auto-tags string is in all five bundles', () => {
  const keys = ['automation_stage_label', 'automation_stage_placeholder', 'automation_rename_stage', 'automation_rename_stage_to',
    'automation_stage_too_long', 'automation_auto_tags_section', 'automation_auto_tags_hint', 'automation_auto_tags_mode',
    'automation_auto_tags_mode_inherit', 'automation_auto_tags_mode_on', 'automation_auto_tags_mode_off',
    'automation_auto_tags_automation_template', 'automation_auto_tags_stage_template', 'automation_auto_tags_kind_template'];
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of keys) assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' missing ' + k);
  }
});
