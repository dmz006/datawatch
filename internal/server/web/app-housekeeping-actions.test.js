const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const src = fs.readFileSync(__dirname + '/app.js', 'utf8');
test('unlanded card, commit dialog and permission fix are wired', () => {
  for (const s of ['window.unlandedWorkCardHtml', 'window.unlandedWorkAct', 'window.uncommittedWorkCommitOpen',
    'window.cleanupPermissionHtml', "'/api/autonomous/unlanded-work'", '/api/autonomous/cleanup/fix-permissions',
    'showConfirmModal(t(\'unlanded_work_delete_confirm\'']) assert.ok(src.includes(s), s);
});
test('locale keys exist in every locale', () => {
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    const j = JSON.parse(fs.readFileSync(`${__dirname}/locales/${l}.json`, 'utf8'));
    for (const k of ['unlanded_work_card_title', 'unlanded_work_keep', 'uncommitted_work_commit', 'cleanup_permission_fix']) assert.ok(j[k], l + k);
  }
});
