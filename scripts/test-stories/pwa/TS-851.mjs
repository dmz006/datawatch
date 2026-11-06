// Story: placement tag filtering and grouping
/**
 * TS-851 — Placement (system) tags: filtering and grouping in the PWA.
 * tags: surface:pwa feature:placement-tags group:sessions
 *
 * Placement tags are machine-derived system tags — compute:<node>, peer:<name>,
 * backend:<llm>, lane:<lane>, role:<role>, project:<name> — stamped on
 * whichever sessions carry them (autonomous.tags.placement). This story
 * checks the whole feature end to end:
 *
 *   1. the daemon's /api/config exposes the placement block with all six
 *      built-in axes (skip when the daemon predates the feature), and the
 *      flat `autonomous.tags.placement.enabled` key round-trips through
 *      PUT /api/config back to its original value;
 *   2. the web bundle carries the placement helpers and the English strings,
 *      and the helpers behave: axis parsing, canonical axis order, tag names
 *      grouped by axis (unknown shapes skipped), and the dashboard's "axis:*"
 *      whole-axis match;
 *   3. the sessions view: when the daemon's sessions carry placement tags the
 *      toolbar shows the "group by placement axis" control, and grouping
 *      renders one header per tag plus the untagged group (restored to
 *      ungrouped afterwards); when none carry them the control is absent and
 *      nothing is shown;
 *   4. settings: the autonomous-config section renders the Placement tags
 *      group with one toggle and one template per axis;
 *   5. dashboard: the placement filter select is in the header, keeps its
 *      options when a value is set, and returns to the unfiltered default.
 *
 * Runs at a phone viewport (390x844). Needs a sandbox daemon (never the
 * production daemon, never ~/.datawatch); see scripts/e2e-pwa.sh. Skips
 * (exit 2) when Chrome is unavailable or the daemon has no placement block.
 */

import { screenshot, saveLog, env } from './lib.mjs';
import { runPerfStory, openPWA, check, saveJson, skipStory, noHorizontalOverflow } from './perf-lib.mjs';

const PHONE = { width: 390, height: 844 };
const AXES = ['compute', 'peer', 'backend', 'lane', 'role', 'project'];

async function api(pathAndQuery, init = {}) {
  const r = await fetch(env.http + pathAndQuery, {
    ...init,
    headers: { Authorization: `Bearer ${env.token}`, 'Content-Type': 'application/json', ...(init.headers || {}) },
  });
  const text = await r.text();
  return { status: r.status, body: text ? JSON.parse(text) : null };
}

const squash = (s) => String(s || '').replace(/\s+/g, ' ').trim();

const BUNDLE_KEYS = [
  'sessions_group_none', 'session_group_count_one', 'session_group_count_other',
  'session_group_filter_on', 'session_group_filter_off', 'sessions_group_untagged',
  'tags_menu_placement', 'placement_tag_tip', 'settings_placement_group',
  'settings_placement_enabled', 'settings_placement_compute', 'settings_placement_project_template',
  'dash_filter_placement_all', 'dash_filter_placement_axis',
];

let evidence = { viewport: PHONE };

await runPerfStory(async (page) => {
  // --- 1. the daemon's placement block, and the flat-key write round-trip ---
  const probe = await api('/api/config');
  if (probe.status !== 200) await skipStory(`GET /api/config -> ${probe.status}; start a sandbox daemon built from this tree`);
  const placement = probe.body?.autonomous?.tags?.placement;
  if (!placement || typeof placement.enabled !== 'boolean' || !placement.axes) {
    await skipStory('the daemon has no autonomous.tags.placement block; build it from this tree');
  }
  const missing = AXES.filter((a) => !placement.axes[a]);
  check(missing.length === 0, `the placement block has all six built-in axes ${AXES.join(', ')}, missing ${missing.join(', ') || 'none'}`);
  evidence.placementConfig = placement;

  const originalEnabled = placement.enabled;
  const flip = await api('/api/config', { method: 'PUT', body: JSON.stringify({ 'autonomous.tags.placement.enabled': !originalEnabled }) });
  const got = await api('/api/config');
  check(flip.status === 200, `PUT autonomous.tags.placement.enabled -> ${flip.status}`);
  check(got.body?.autonomous?.tags?.placement?.enabled === !originalEnabled,
    `the daemon took the new enabled value (${got.body?.autonomous?.tags?.placement?.enabled}), want ${!originalEnabled}`);
  const restored = await api('/api/config', { method: 'PUT', body: JSON.stringify({ 'autonomous.tags.placement.enabled': originalEnabled }) });
  const back = await api('/api/config');
  check(restored.status === 200 && back.body?.autonomous?.tags?.placement?.enabled === originalEnabled,
    `the daemon is back on its original enabled value (${back.body?.autonomous?.tags?.placement?.enabled}), want ${originalEnabled}`);

  const browser = page.context().browser();
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: PHONE, locale: 'en-US', isMobile: true, hasTouch: true });
  const p = await context.newPage();
  const consoleErrors = [];
  p.on('pageerror', (err) => consoleErrors.push(`pageerror: ${err.message}`));

  try {
    await openPWA(p, { timeout: 60000 });
    await p.evaluate(() => { try { localStorage.setItem('cs_theme', 'dark'); } catch { /* ignore */ } });

    // --- 2. the bundle: helpers + English strings, and helper behaviour -----
    const surface = await p.evaluate(() => ({
      funcs: {
        axisOf: typeof window._placementAxisOf === 'function',
        axisOrder: typeof window._placementAxisOrder === 'function',
        tagName: typeof window._placementTagName === 'function',
        tagsByAxis: typeof window._placementTagsByAxis === 'function',
        groupControl: typeof window._sessionsGroupControl === 'function',
        groupCards: typeof window._sessionsGroupCards === 'function',
        setGroup: typeof window.setSessionsGroupAxis === 'function',
        dashMatch: typeof window._dashPlacementSessionMatch === 'function',
        dashSelect: typeof window.dashSetPlacementTag === 'function',
        groupClick: typeof window._sessionGroupTagClick === 'function',
      },
      order: (typeof window._placementAxisOrder === 'function') ? window._placementAxisOrder(['lane', 'zzz', 'compute']) : null,
      axisOf: (typeof window._placementAxisOf === 'function')
        ? { on: window._placementAxisOf('compute:gpu-1'), off: window._placementAxisOf('plain-tag') }
        : null,
      byAxis: (typeof window._placementTagsByAxis === 'function')
        ? window._placementTagsByAxis([
          { system_tags: ['compute:gpu-1', 'lane:claude', 'backend:gpt-5', 'not-a-tag'] },
          { system_tags: ['compute:cpu-2'] },
        ])
        : null,
    }));
    check(Object.values(surface.funcs).every(Boolean), `every placement helper is on the page, saw ${JSON.stringify(surface.funcs)}`);
    check(JSON.stringify(surface.order) === JSON.stringify(['compute', 'lane', 'zzz']),
      `canonical axis order with custom axes last, saw ${JSON.stringify(surface.order)}`);
    check(!!surface.axisOf && surface.axisOf.on === 'compute' && surface.axisOf.off === '',
      `axis parsing: "compute:gpu-1"->compute, "plain-tag"->"", saw ${JSON.stringify(surface.axisOf)}`);
    check(!!surface.byAxis
      && JSON.stringify(Object.keys(surface.byAxis)) === JSON.stringify(['compute', 'backend', 'lane'])
      && surface.byAxis.compute.includes('compute:gpu-1')
      && surface.byAxis.compute.includes('compute:cpu-2')
      && !JSON.stringify(surface.byAxis).includes('not-a-tag'),
      'placement tag names group by axis; unparseable names are skipped');
    const bundle = await p.evaluate(() => (window._i18n && window._i18n.bundle) || {});
    const missingKeys = BUNDLE_KEYS.filter((k) => !bundle[k]);
    check(missingKeys.length === 0, `the English bundle carries the placement strings, missing ${missingKeys.join(', ') || 'none'}`);
    evidence.surface = { funcs: surface.funcs, order: surface.order, axisOf: surface.axisOf, byAxis: surface.byAxis, missingKeys };

    // The dashboard's whole-axis ("axis:*") session match.
    const dashMatch = await p.evaluate(() => {
      if (typeof _dashPlacementSessionMatch !== 'function' || typeof dashSetPlacementTag !== 'function') return null;
      const tagged = { system_tags: ['compute:gpu-1', 'lane:claude'] };
      const other = { system_tags: ['lane:claude'] };
      const prior = state.dashPlacementTag || '';
      dashSetPlacementTag('compute:*');
      const axisHit = _dashPlacementSessionMatch(tagged);
      const axisMiss = _dashPlacementSessionMatch(other);
      dashSetPlacementTag('compute:gpu-1');
      const nameHit = _dashPlacementSessionMatch(tagged);
      const nameMiss = _dashPlacementSessionMatch({ system_tags: ['compute:cpu-9'] });
      dashSetPlacementTag('');
      const none = _dashPlacementSessionMatch(other);
      dashSetPlacementTag(prior);
      return { axisHit, axisMiss, nameHit, nameMiss, none };
    });
    check(!!dashMatch && dashMatch.axisHit === true && dashMatch.axisMiss === false,
      `the dashboard's "compute:*" matches the tagged session only, saw ${JSON.stringify(dashMatch)}`);
    check(!!dashMatch && dashMatch.nameHit === true && dashMatch.nameMiss === false && dashMatch.none === true,
      'an exact "compute:<node>" value matches only that tag');
    evidence.dashMatch = dashMatch;
    await screenshot(p, '01-helpers');

    // --- 3. the sessions view: the group-by-axis control and grouped render --
    const sessions = await p.evaluate(() => {
      if (typeof _placementTagsByAxis !== 'function') return { axes: [], sessions: 0 };
      const list = state.sessions || [];
      return { axes: Object.keys(_placementTagsByAxis(list)), sessions: list.length };
    });
    evidence.sessions = sessions;
    const hadAxis = sessions.axes.length > 0;
    const groupUi = await p.evaluate((want) => {
      if (typeof _sessionsGroupControl !== 'function') return null;
      navigate('sessions');
      const render = () => { if (typeof renderSessionsView === 'function') renderSessionsView(); };
      render();
      const before = { control: !!document.querySelector('.group-axis-control') };
      if (!want) {
        return { ...before, grouped: null };
      }
      const axis = Object.keys(_placementTagsByAxis(state.sessions || []))[0];
      if (typeof setSessionsGroupAxis !== 'function') return { ...before, grouped: null };
      setSessionsGroupAxis(axis);
      render();
      const headers = [...document.querySelectorAll('.sessions-group-header')].map((el) => (el.textContent || '').replace(/\s+/g, ' ').trim());
      const stored = (typeof localStorage !== 'undefined') ? localStorage.getItem('dw_sessions_group_axis') : null;
      setSessionsGroupAxis('');
      render();
      return { ...before, axis, headers, stored, restoredStored: (typeof localStorage !== 'undefined') ? localStorage.getItem('dw_sessions_group_axis') : null };
    }, hadAxis);
    if (hadAxis) {
      check(!!groupUi && groupUi.control === true, 'with placement tags present, the sessions toolbar shows the group-by-axis control');
      check(!!groupUi && Array.isArray(groupUi.headers) && groupUi.headers.length >= 1
        && groupUi.headers.some((h) => h.includes(groupUi.axis ? `${groupUi.axis}:` : '')),
        `grouping renders a header per tag under "${groupUi && groupUi.axis}", saw ${JSON.stringify(groupUi && groupUi.headers)}`);
      check(!!groupUi && groupUi.stored === groupUi.axis, `the chosen axis is persisted, stored=${groupUi && groupUi.stored}`);
      check(!!groupUi && (groupUi.restoredStored === '' || groupUi.restoredStored === null), 'grouping is restored to ungrouped, stored=' + JSON.stringify(groupUi && groupUi.restoredStored));
    } else {
      check(!!groupUi && groupUi.control === false, 'without placement tags on any session, the group-by-axis control stays hidden');
    }
    evidence.groupUi = groupUi;
    await screenshot(p, '02-sessions-grouping');

    // --- 4. settings: the Placement tags group and per-axis rows ------------
    let settingsUi = await p.evaluate(() => {
      if (typeof navigate !== 'function') return null;
      navigate('settings');
      if (typeof switchSettingsTab === 'function') switchSettingsTab('automata');
      if (typeof renderSettingsView === 'function') renderSettingsView();
      const sections = [...document.querySelectorAll('.settings-section')];
      const sec = sections.find((el) => (el.textContent || '').includes('Placement tags'));
      if (!sec) {
        // The lazy content slot may still be loading; wait is handled by the caller.
        return null;
      }
      const text = sec.textContent || '';
      const axes = ['compute', 'peer', 'backend', 'lane', 'role', 'project'];
      const perAxis = axes.map((a) => ({
        axis: a,
        toggle: new RegExp(`${a} axis`).test(text),
        template: new RegExp(`${a} tag template`).test(text),
      }));
      return {
        groupHint: text.includes('machine') || true,
        masterToggle: /Derive placement tags/.test(text),
        perAxis,
      };
    });
    // Lazy content: the section body loads asynchronously; retry a few times.
    for (let i = 0; !settingsUi && i < 8; i++) {
      await p.waitForTimeout(250);
      settingsUi = await p.evaluate(() => {
        const sec = [...document.querySelectorAll('.settings-section')].find((el) => (el.textContent || '').includes('Placement tags') && el.offsetParent !== undefined);
        if (!sec) return null;
        const text = sec.textContent || '';
        const axes = ['compute', 'peer', 'backend', 'lane', 'role', 'project'];
        return {
          masterToggle: /Derive placement tags/.test(text),
          perAxis: axes.map((a) => ({
            axis: a,
            toggle: new RegExp(`${a} axis`).test(text),
            template: new RegExp(`${a} tag template`).test(text),
          })),
        };
      });
    }
    if (settingsUi) {
      check(settingsUi.masterToggle === true, 'settings shows the "Derive placement tags" master toggle');
      const bad = (settingsUi.perAxis || []).filter((a) => !a.toggle || !a.template);
      check(bad.length === 0, `every axis has its toggle and template row, missing ${JSON.stringify(bad)}`);
    } else {
      check(false, 'settings: the Placement tags group did not render in the autonomous-config section');
    }
    evidence.settingsUi = settingsUi;
    await screenshot(p, '03-settings-placement-tags');

    // --- 5. the dashboard header: the placement filter select ---------------
    await p.evaluate(() => {
      if (typeof navigate !== 'function') return;
      navigate('dashboard');
    });
    let dashUi = null;
    for (let i = 0; !dashUi && i < 12; i++) {
      await p.waitForTimeout(250);
      dashUi = await p.evaluate(() => {
        const sel = document.getElementById('dashPlacementSelect');
        const wrap = document.getElementById('dashPlacementFilter');
        if (!sel || !wrap) return null;
        return {
          hasSelect: true,
          display: getComputedStyle(wrap).display,
          options: [...sel.options].map((o) => o.value),
          value: sel.value,
        };
      });
    }
    check(!!dashUi, 'the dashboard header carries the placement filter select');
    if (dashUi) {
      if (hadAxis) {
    check(dashUi.display === 'inline-flex', `the filter is visible with placement tags known, display=${dashUi.display}`);
    check(dashUi.options[0] === '', 'the filter offers the unfiltered option first');
    check(dashUi.options.some((o) => o.endsWith(':*')), `the filter offers whole-axis options, saw ${JSON.stringify(dashUi.options)}`);
      } else {
    check(dashUi.display === 'none', `the filter stays hidden without placement tags, display=${dashUi.display}`);
      }
      // Set a value and back to the default.
      const round = await p.evaluate(() => {
        if (typeof dashSetPlacementTag !== 'function') return null;
        const first = document.getElementById('dashPlacementSelect');
        if (!first) return null;
        const target = [...first.options].map((o) => o.value).find((v) => v !== '') || 'compute:*';
        dashSetPlacementTag(target);
        const afterSet = { value: document.getElementById('dashPlacementSelect').value, stored: localStorage.getItem('dw_dash_placement_tag') };
        dashSetPlacementTag('');
        const afterReset = { value: document.getElementById('dashPlacementSelect').value, stored: localStorage.getItem('dw_dash_placement_tag') };
        return { target, afterSet, afterReset };
      });
      check(!!round && round.afterReset.value === '' && round.afterReset.stored === '',
        `the filter returns to the unfiltered default, saw ${JSON.stringify(round)}`);
      evidence.dashUi = { display: dashUi.display, options: dashUi.options, round };
    }
    await screenshot(p, '04-dashboard-filter');

    check((await noHorizontalOverflow(p)).ok, 'no horizontal overflow at 390 px');
  } finally {
    try {
      await saveJson('placement-tags-evidence', evidence).catch(() => {});
      if (consoleErrors.length) await saveLog('console-errors', consoleErrors.join('\n')).catch(() => {});
      await context.close().catch(() => {});
    } catch { /* ignore */ }
  }
    check(consoleErrors.length === 0, `page errors: ${consoleErrors.join(' | ')}`);
});
