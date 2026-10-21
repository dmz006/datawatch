// scripts/test-stories/pwa/run-all.mjs — run every TS-*.mjs PWA story in this directory.
//
// Each story runs as a child node process with CURRENT_STORY set to its id (e.g. TS-149).
// Exit 0 when every story passes; exit 1 on any failure or when no stories are found.
//
// Usage:
//   node run-all.mjs             — run all stories
//   node run-all.mjs --dry-run   — list the stories that would run, then exit

import { spawnSync } from 'node:child_process';
import { readdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = path.dirname(fileURLToPath(import.meta.url));
const dryRun = process.argv.includes('--dry-run');

// --- discovery --------------------------------------------------------------

const stories = readdirSync(dir)
  .filter((f) => /^TS-.*\.mjs$/.test(f))
  .sort();

if (stories.length === 0) {
  console.error(`no TS-*.mjs stories found in ${dir}`);
  process.exit(1);
}

if (dryRun) {
  for (const file of stories) console.log(path.basename(file, '.mjs'));
  process.exit(0);
}

// --- run --------------------------------------------------------------------

let passed = 0;
let failed = 0;

for (const file of stories) {
  const story = path.basename(file, '.mjs');
  const res = spawnSync(process.execPath, [path.join(dir, file)], {
    env: { ...process.env, CURRENT_STORY: story },
    stdio: 'inherit',
  });
  const code = res.status ?? 1;
  if (code === 0) {
    passed++;
    console.log(`${story} PASS (exit ${code})`);
  } else {
    failed++;
    console.log(`${story} FAIL (exit ${code})`);
  }
}

console.log(`summary: ${passed} passed, ${failed} failed, ${stories.length} total`);
process.exit(failed > 0 ? 1 : 0);
