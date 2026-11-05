// Settings > Automata: the autonomous.auto_tags block (enabled, the three tag
// templates and the kind labels). Covers the field list, the group heading, how
// the card draws a reported config (toggle state, template text, the kind
// labels mapping as "kind:label" text), the defaults when nothing is reported,
// settingsValueText, and the five locale bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const bundle = lang => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));

function loadApp(byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function autonomousFields(sb) {
  return JSON.parse(vm.runInContext(
    "JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous').fields)", sb));
}

const KEYS = [
  'autonomous.auto_tags.enabled',
  'autonomous.auto_tags.automation_template',
  'autonomous.auto_tags.stage_template',
  'autonomous.auto_tags.kind_template',
  'autonomous.auto_tags.kind_labels',
];

test('the autonomous card lists the auto_tags fields in order under their own heading', () => {
  const fields = autonomousFields(loadApp());
  const at = fields.findIndex(f => f.labelKey === 'settings_auto_tags_group');
  assert.ok(at >= 0, 'group heading present');
  assert.equal(fields[at].type, 'group');
  assert.equal(fields[at].hintKey, 'settings_auto_tags_group_help');
  assert.deepEqual(fields.slice(at + 1, at + 6).map(f => f.key), KEYS);
});

test('enabled is a toggle that defaults on; the rest are text with the daemon defaults', () => {
  const byKey = {};
  autonomousFields(loadApp()).forEach(f => { byKey[f.key] = f; });
  assert.equal(byKey[KEYS[0]].type, 'toggle');
  assert.equal(byKey[KEYS[0]].default, true);
  assert.equal(byKey[KEYS[1]].default, 'automation:{automation}');
  assert.equal(byKey[KEYS[2]].default, 'stage:{stage}');
  assert.equal(byKey[KEYS[3]].default, 'kind:{kind}');
  assert.equal(byKey[KEYS[4]].default, 'feature:feature, bug:bug, other:maintenance');
  for (const k of KEYS.slice(1)) assert.equal(byKey[k].type, 'text', k);
});

test('every auto_tags field has a label key present in all five bundles', () => {
  const labelKeys = autonomousFields(loadApp()).filter(f => KEYS.includes(f.key)).map(f => f.labelKey);
  assert.equal(labelKeys.length, 5);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of labelKeys.concat(['settings_auto_tags_group', 'settings_auto_tags_group_help'])) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' missing ' + k);
    }
  }
});

test('settingsValueText joins a list, flattens a mapping to kind:label pairs and stringifies the rest', () => {
  const sb = loadApp();
  assert.equal(sb.settingsValueText(['a', 'b']), 'a, b');
  assert.equal(sb.settingsValueText({ feature: 'feature', other: 'maintenance' }), 'feature:feature, other:maintenance');
  assert.equal(sb.settingsValueText('x'), 'x');
  assert.equal(sb.settingsValueText(7), '7');
  assert.equal(sb.settingsValueText(null), 'null');
});

async function render(cfg) {
  const el = makeStubElement();
  const sb = loadApp({ gcfg_autonomous: el });
  sb.apiFetch = p => Promise.resolve(p === '/api/config' ? cfg : (p === '/api/interfaces' ? [] : { llms: [] }));
  sb.loadGeneralConfig();
  await flushAsync();
  return el.innerHTML;
}

test('the card draws a reported block: toggle off, template text and the labels mapping', async () => {
  const html = await render({ autonomous: { auto_tags: {
    enabled: false, automation_template: 'a:{automation}', stage_template: 'st:{stage}', kind_template: 'k:{kind}',
    kind_labels: { feature: 'feat', bug: 'bug', other: 'maintenance' },
  } } });
  assert.match(html, />Automatic tags</);
  const toggle = html.match(/<input type="checkbox"([^>]*?)onchange="saveGeneralField\('autonomous\.auto_tags\.enabled'/);
  assert.ok(toggle, 'toggle drawn');
  assert.doesNotMatch(toggle[1], /checked/);
  assert.match(html, /value="a:\{automation\}"/);
  assert.match(html, /value="st:\{stage\}"/);
  assert.match(html, /value="k:\{kind\}"/);
  assert.match(html, /value="feature:feat, bug:bug, other:maintenance"/);
  assert.doesNotMatch(html, /\[object Object\]/);
});

test('with nothing reported the card shows the toggle on and the default templates', async () => {
  const html = await render({ autonomous: {} });
  const toggle = html.match(/<input type="checkbox"([^>]*?)onchange="saveGeneralField\('autonomous\.auto_tags\.enabled'/);
  assert.ok(toggle, 'toggle drawn');
  assert.match(toggle[1], /checked/);
  assert.match(html, /value="automation:\{automation\}"/);
  assert.match(html, /value="stage:\{stage\}"/);
  assert.match(html, /value="kind:\{kind\}"/);
  assert.match(html, /value="feature:feature, bug:bug, other:maintenance"/);
});

test('editing a field saves it through the general config path', async () => {
  const html = await render({ autonomous: {} });
  assert.match(html, /saveGeneralField\('autonomous\.auto_tags\.stage_template', this\.value\)/);
  assert.match(html, /saveGeneralField\('autonomous\.auto_tags\.kind_labels', this\.value\)/);
  assert.match(html, /saveGeneralField\('autonomous\.auto_tags\.enabled', this\.checked\)/);
});
