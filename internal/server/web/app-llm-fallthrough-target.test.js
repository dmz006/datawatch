// LLM entry "Allowed fall-through target" switch — PWA side.
//
// Both LLM edit forms (the inline form and the add/edit modal) carry the
// fallthrough_target switch and send it as a boolean, so unchecking it clears
// the stored mark. Locales: the label and hint exist in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

const appSrc = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

test('inline LLM form: switch renders from the record and collectLLMForm sends a boolean', () => {
  const sandbox = loadApp();
  sandbox.__elems = { fe_llm_fallthrough_target: { checked: true } };
  vm.runInContext(
    "document.getElementById = function(id){ return __elems[id] || null; };", sandbox);
  const on = JSON.parse(vm.runInContext(
    "JSON.stringify(collectLLMForm({name:'a', fallthrough_target:false}))", sandbox));
  assert.strictEqual(on.fallthrough_target, true);
  sandbox.__elems.fe_llm_fallthrough_target.checked = false;
  const off = JSON.parse(vm.runInContext(
    "JSON.stringify(collectLLMForm({name:'a', fallthrough_target:true}))", sandbox));
  assert.strictEqual(off.fallthrough_target, false, 'unchecking must send false, not omit the key');
});

test('both forms render the switch from the stored value and the modal sends it', () => {
  assert.ok(/id="fe_llm_fallthrough_target" \$\{l\.fallthrough_target \? 'checked' : ''\}/.test(appSrc),
    'inline form switch missing or not bound to the record');
  assert.ok(/id="llmEditFallthroughTarget" \$\{existing\.fallthrough_target \? 'checked' : ''\}/.test(appSrc),
    'modal switch missing or not bound to the record');
  assert.ok(/fallthrough_target: fallthroughTarget/.test(appSrc),
    'modal payload does not send fallthrough_target');
});

test('fall-through target keys exist in all five locale bundles', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const key of ['llm_field_fallthrough_target', 'llm_fallthrough_target_hint']) {
      assert.ok(bundle[key], lang + ' missing ' + key);
    }
  }
});
