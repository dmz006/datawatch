// scripts/test-stories/pwa/run-all.mjs — run every TS-*.mjs PWA story in this directory,
// plus the shared-nav-mobile-fit.mjs navigation scenario (or a named group of them).
//
// Each story runs as a child node process with CURRENT_STORY set to its id (e.g. TS-149).
// Exit 0 when no story fails; exit 1 on any failure, when no stories are found,
// or when an unknown group is requested. Stories that exit 2 are skipped
// (precondition not met, e.g. no seeded fixture or no Chrome): they are
// counted as skipped in the summary and do not fail the run.
//
// Usage:
//   node run-all.mjs                          — run all stories
//   node run-all.mjs --dry-run                — list the stories that would run, then exit
//   node run-all.mjs --group load             — run a named group (load = TS-802..TS-812)
//   node run-all.mjs --dry-run --group load   — list the load group's stories, then exit
//   node run-all.mjs --group wire             — run the websocket byte-budget group (TS-816)
//   node run-all.mjs --group shared-nav       — run the shared navigation phone-width scenario
//   node run-all.mjs --group usage            — run the usage calculator group (TS-819, TS-832, TS-840)
//   node run-all.mjs --group automation       — run the automation sub-tabs group (TS-833)

import { spawnSync } from 'node:child_process';
import { readdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = path.dirname(fileURLToPath(import.meta.url));
const argv = process.argv.slice(2);
const dryRun = argv.includes('--dry-run');

// Named groups: fixed lists of scenario names (a TS id, or a file basename for
// scenarios that have no TS id), run in this order.
const GROUPS = {
  load: ['TS-802', 'TS-803', 'TS-804', 'TS-805', 'TS-806', 'TS-807', 'TS-808', 'TS-809', 'TS-810', 'TS-811', 'TS-812'],
  wire: ['TS-816'],
  'shared-nav': ['shared-nav-mobile-fit'],
  // usage: TS-819.mjs, TS-832.mjs, TS-840.mjs (the Claude lane stamp story)
  usage: ['TS-819', 'TS-832', 'TS-840'],
  automation: ['TS-833'],
};

// Scenarios outside the TS-*.mjs naming that the default run also discovers.
const EXTRA_SCENARIOS = ['shared-nav-mobile-fit.mjs'];

let groupName = null;
const groupIndex = argv.indexOf('--group');
if (groupIndex !== -1) {
  groupName = argv[groupIndex + 1];
  if (!groupName || groupName.startsWith('--')) {
    console.error('usage: node run-all.mjs [-h] [--dry-run] [--group <name>]');
    process.exit(1);
  }
  if (!(groupName in GROUPS)) {
    console.error(`unknown group: ${groupName} (known groups: ${Object.keys(GROUPS).join(', ')})`);
    process.exit(1);
  }
}

// --- discovery --------------------------------------------------------------

const stories = readdirSync(dir)
  .filter((f) => /^TS-.*\.mjs$/.test(f) || EXTRA_SCENARIOS.includes(f))
  .sort();

if (stories.length === 0) {
  console.error(`no TS-*.mjs stories or named scenarios found in ${dir}`);
  process.exit(1);
}

let selected = stories;
if (groupName) {
  const byId = new Map(stories.map((f) => [path.basename(f, '.mjs'), f]));
  const files = [];
  for (const id of GROUPS[groupName]) {
    const f = byId.get(id);
    if (!f) {
      console.error(`group ${groupName}: story not found in ${dir}: ${id}.mjs`);
      process.exit(1);
    }
    files.push(f);
  }
  selected = files;
}

if (dryRun) {
  for (const file of selected) console.log(path.basename(file, '.mjs'));
  process.exit(0);
}

// --- run --------------------------------------------------------------------

let passed = 0;
let failed = 0;
let skipped = 0;

if (groupName) console.log(`run-all: group ${groupName} (${selected.length} stories)`);

for (const file of selected) {
  const story = path.basename(file, '.mjs');
  const res = spawnSync(process.execPath, [path.join(dir, file)], {
    env: { ...process.env, CURRENT_STORY: story },
    stdio: 'inherit',
  });
  const code = res.status ?? 1;
  if (code === 0) {
    passed++;
    console.log(`${story} PASS (exit ${code})`);
  } else if (code === 2) {
    skipped++;
    console.log(`${story} SKIP (exit ${code})`);
  } else {
    failed++;
    console.log(`${story} FAIL (exit ${code})`);
  }
}

console.log(`summary: ${passed} passed, ${failed} failed, ${skipped} skipped, ${selected.length} total`);
process.exit(failed > 0 ? 1 : 0);
