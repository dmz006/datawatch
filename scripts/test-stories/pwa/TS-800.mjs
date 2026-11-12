#!/usr/bin/env node
/**
 * TS-800 — PWA survives rapid session switching and a viewport resize storm
 * tags: surface:pwa feature:pwa-resize-switching-stress
 *
 * Flow:
 *   1. Track UNCAUGHT page errors (page.on('pageerror')) and console errors
 *      mentioning `dimensions` from the very start, before any navigation.
 *   2. Connect to the PWA and seed three shell sessions (ts800-rapid-a/b/c);
 *      wait ~1.5 s for them to appear.
 *   3. Rapid session switching: 36 iterations of navigate('session-detail', id)
 *      rotating through the three ids with only 0..40 ms jitter between
 *      iterations; every 5th iteration goes through the sessions list
 *      (navigate('sessions')) to force destroyXterm, then back into a session.
 *   4. Interleave a resize storm: 16 page.setViewportSize calls with widths
 *      360..1400 and heights 400..900, including an extreme 200x120 shrink and
 *      a restore; once toggle the header fullscreen control via toggleFullscreen
 *      if the function exists on the page.
 *
 * Assertions:
 *   1. zero uncaught page errors (pageerror events);
 *   2. no recorded console error mentions `dimensions`.
 * The three seeded sessions are always deleted afterwards (best-effort).
 */
import { runStory, connectToPWA, screenshot, saveLog } from './lib.mjs';

const SWITCHES = 36;
const LABELS = ['ts800-rapid-a', 'ts800-rapid-b', 'ts800-rapid-c'];

// Pre-sized resize sequence: 16 steps, widths 360..1400, heights 400..900,
// including a very small 200x120 and a restore to a normal size.
const RESIZES = [
  { width: 360,  height: 400  },
  { width: 480,  height: 480  },
  { width: 640,  height: 520  },
  { width: 800,  height: 600  },
  { width: 1024, height: 700  },
  { width: 1200, height: 800  },
  { width: 1400, height: 900  },
  { width: 200,  height: 120  },  // extreme shrink
  { width: 360,  height: 400  },
  { width: 560,  height: 760  },
  { width: 720,  height: 560  },
  { width: 960,  height: 640  },
  { width: 1280, height: 800  },
  { width: 360,  height: 900  },
  { width: 1400, height: 400  },
  { width: 1024, height: 768  },  // restore
];

async function api(page, path, method, body) {
  return page.evaluate(async ({ path, method, body }) => {
    const r = await fetch(path, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: body ? JSON.stringify(body) : undefined,
    });
    return r.json().catch(() => ({}));
  }, { path, method, body });
}

await runStory(async (page) => {
  // (1) Collect UNCAUGHT page errors and dimension-related console errors,
  // attached before any navigation.
  const errors = [];
  const dimErrors = [];
  page.on('pageerror', (e) => {
    const m = (e && e.message) ? String(e.message) : String(e);
    errors.push(m);
    if (/dimensions/i.test(m)) dimErrors.push(m);
  });
  page.on('console', (msg) => {
    if (msg.type() === 'error' && /dimensions/i.test(msg.text())) dimErrors.push(msg.text());
  });

  // (2) Connect and seed three shell sessions.
  await connectToPWA(page);

  const ids = [];
  for (const label of LABELS) {
    const d = await api(page, '/api/sessions/start', 'POST', { backend: 'shell', label });
    const id = d.id || d.full_id;
    if (!id) throw new Error(`session start failed for ${label}: ${JSON.stringify(d)}`);
    ids.push(id);
  }
  await page.waitForTimeout(1500);   // let them appear in the session list
  await saveLog('seeded-sessions', ids.join('\n'));

  const jitter = () => page.waitForTimeout(Math.floor(Math.random() * 41));

  try {
    // (3) Rapid session switching, interleaved with (4) a resize storm.
    let resizeIdx = 0;
    const nextResize = async () => {
      const size = RESIZES[resizeIdx % RESIZES.length];
      resizeIdx += 1;
      await page.setViewportSize(size);
    };

    for (let i = 0; i < SWITCHES; i++) {
      if (i % 5 === 4) {
        // Force destroyXterm via the sessions list view, then back in.
        await page.evaluate((id) => {
          if (typeof navigate === 'function') navigate('sessions');
        });
        await jitter();
        const back = ids[(i + 1) % ids.length];
        await page.evaluate((id) => {
          if (typeof navigate === 'function') navigate('session-detail', id);
        }, back);
      } else {
        const id = ids[i % ids.length];
        await page.evaluate((sessionId) => {
          if (typeof navigate === 'function') navigate('session-detail', sessionId);
        }, id);
      }
      await jitter();   // 0..40 ms only — no real settling wait

      // (4) Interleave the resize storm: every other switch, up to the 16
      // pre-sized steps (>= 15 resizes guaranteed across 36 switches).
      if (i % 2 === 0 && resizeIdx < RESIZES.length) {
        await nextResize();
      }
      if (i === Math.floor(SWITCHES / 2)) {
        // Once, toggle the header fullscreen control if it exists.
        await page.evaluate(() => {
          try { if (typeof toggleFullscreen === 'function') toggleFullscreen(); } catch (_) {}
        });
      }
    }

    // Safety net: guarantee at least 15 resizes even if the loop above
    // exhausted the sequence early.
    const minResizes = 15;
    while (resizeIdx < minResizes) {
      await page.setViewportSize({ ...RESIZES[resizeIdx % RESIZES.length] });
      resizeIdx += 1;
    }

    // (5) Let debounced 200 ms resize timers and 350 ms viewport timers fire.
    await page.waitForTimeout(1000);
    await page.setViewportSize({ width: 1024, height: 768 });   // restore

    if (errors.length > 0 || dimErrors.length > 0) {
      const parts = [];
      if (errors.length > 0) parts.push(`uncaught page errors: ${JSON.stringify(errors)}`);
      if (dimErrors.length > 0) parts.push(`dimensions console errors: ${JSON.stringify(dimErrors)}`);
      await screenshot(page, '01-after-storm').catch(() => {});
      throw new Error(parts.join('\n'));
    }

    await screenshot(page, '01-after-storm');
    const summary = `switches=${SWITCHES} resizes=${resizeIdx} errors=${errors.length} dimErrors=${dimErrors.length}`;
    console.log('TS-800', summary);
    await saveLog('result', `${summary} — no uncaught page errors, no dimensions console errors`);
  } finally {
    // (6) Best-effort cleanup of only the sessions this story created.
    for (const id of ids) {
      await api(page, '/api/sessions/delete', 'POST', { id, delete_data: false }).catch(() => {});
    }
  }
});
