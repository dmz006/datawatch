// TS-845.mjs
// PWA story: the About tab on a federated peer running a DIFFERENT release
// than the daemon serving this page shows the version-skew note, and nothing
// in the switch reads as an "Update available" — no banner, no reload — and
// switching back to the primary must not reload the page (no
// location.reload(), no controllerchange-driven reload) either.
//
// Setup: the local /api/health is fulfilled with one build ('9.4.2') and the
// same endpoint behind the peer proxy (/api/proxy/edge-box-1/api/health) with
// a different build ('9.4.1'), so the note's two version slots are
// deterministic no matter what the sandbox daemon itself reports. The server
// picker list (/api/servers) is fulfilled with the primary plus the peer.
//
// Product contract mirrored by app-multi-server-version-skew.test.js: a
// peer's version is recorded under the peer's own key, never replaces the
// page's own daemon version, and never takes the update path (banner or
// reload). The skew note appears only while the two builds differ.

import {
  runPerfStory,
  coldPage,
  openPWA,
  fulfillEndpoint,
  check,
  saveJson,
} from './perf-lib.mjs';

const PEER = 'edge-box-1';
const LOCAL_VERSION = '9.4.2'; // the daemon serving this page (primary)
const PEER_VERSION = '9.4.1';  // the federated peer's own daemon — a different build

runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());

  // Primary's /api/health: fixed build, boot fallback for state._daemonVersion.
  const hLocal = await fulfillEndpoint(p, '/api/health', {
    version: LOCAL_VERSION, status: 'ok', hostname: 'primary-host',
  });
  // Same endpoint behind the peer proxy: a DIFFERENT build. pathname exact,
  // so this never shadows the local /api/health route above.
  const hPeer = await fulfillEndpoint(
    p, `/api/proxy/${encodeURIComponent(PEER)}/api/health`,
    { version: PEER_VERSION, status: 'ok', hostname: 'peer-host' },
  );
  // Picker list: primary + one federated peer.
  const serversCtl = await fulfillEndpoint(p, '/api/servers', [
    { name: 'local',  hostname: 'primary-host', url: 'http://127.0.0.1:18080', has_auth: true,  enabled: true },
    { name: PEER,     hostname: 'peer-host',    url: 'https://edge-box-1:18443', has_auth: false, enabled: true },
  ]);
  // selectServer()'s independent federated probe fetches this; an empty
  // peer session list keeps the switch clean (no error phase).
  const peerSessions = await fulfillEndpoint(
    p, `/api/proxy/${encodeURIComponent(PEER)}/api/sessions`, { sessions: [], total: 0 },
  );

  await openPWA(p);
  check(typeof p.evaluate('window.navigate') === 'function', 'PWA app not loaded (window.navigate missing)');

  // Boot must have recorded the PRIMARY's build as the page's own daemon
  // version. This is the value the note's second slot is filled from.
  const bootVersion = await p.evaluate('state._daemonVersion');
  check(bootVersion === LOCAL_VERSION, `boot should record the local daemon version ${LOCAL_VERSION}, got ${JSON.stringify(bootVersion)}`);

  // A full reload discards this marker — the reload sentinel for the whole
  // story (the peer switch AND the switch back must both leave it behind).
  await p.evaluate(() => { window.__skewStoryMarker = 'vskew-about-story'; return true; });
  const marker = () => p.evaluate('window.__skewStoryMarker');

  const bannerPresent = () =>
    p.evaluate(() => {
      const el = document.getElementById('updateAvailableBanner');
      return !!el && el.style.display !== 'none' && !el.classList.contains('hidden');
    });

  // ── Open Settings → About on the primary: no skew note yet (local vs local).
  await p.evaluate(() => { window.navigate('settings'); });
  await p.evaluate(() => {
    const btn = document.querySelector('.settings-tab-btn[data-tab="about"]');
    if (btn) { if (!btn.classList.contains('active')) btn.click(); }
    else window.switchSettingsTab('about');
  });
  await p.waitForFunction(() => {
    const v = document.getElementById('aboutVersion');
    return v && (v.textContent || '').indexOf(LOCAL_VERSION) !== -1;
  }, null, { timeout: 15000 });
  const noteOnPrimary = await p.evaluate(() => {
    const el = document.getElementById('aboutVersionSkewNote');
    return !!el && getComputedStyle(el).display !== 'none';
  });
  check(!noteOnPrimary, 'no version-skew note on the primary (local build vs local build)');

  // ── Select the peer (operator path: the picker, i.e. selectServer()). ────
  await p.evaluate((name) => window.selectServer(name), PEER);
  await p.waitForFunction((name) => state.activeServer === name, PEER, { timeout: 15000 });

  // The switch must not have taken the update path: peer's build recorded
  // under the PEER key, page's own version intact, no banner, no reload.
  const afterPeer = await p.evaluate(() => ({
    byServer: state._daemonVersionByServer || {},
    own: state._daemonVersion,
    updateAvailable: !!state._updateAvailable,
    updateBannerShown: !!state._updateBannerShown,
  }));
  check(afterPeer.byServer[PEER] === PEER_VERSION,
    `peer build should be recorded under the peer key, got ${JSON.stringify(afterPeer.byServer || {})}`);
  check(afterPeer.own === LOCAL_VERSION,
    `the page's own daemon version must not be replaced by the peer's (own=${JSON.stringify(afterPeer.own)})`);
  check(!afterPeer.updateAvailable && !afterPeer.updateBannerShown,
    'peer version switch must not arm the update path (updateAvailable/banner shown)');
  check(!(await bannerPresent()), "'Update available' banner must not appear while viewing the peer");
  check((await marker()) === 'vskew-about-story', 'page reloaded during the peer switch (marker lost)');

  // ── About tab while the peer is selected: the version-skew note. ────────
  // Re-open Settings → About the operator way: the switch resets the view,
  // and renderSettingsView() refetches About through
  // /api/proxy/<peer>/api/health — the proxied endpoint we fulfilled above.
  await p.evaluate(() => { window.navigate('settings'); });
  await p.evaluate(() => {
    const btn = document.querySelector('.settings-tab-btn[data-tab="about"]');
    if (btn) { if (!btn.classList.contains('active')) btn.click(); }
    else window.switchSettingsTab('about');
  });

  // Wait for the note to be visible with the "different release" wording.
  await p.waitForFunction(() => {
    const el = document.getElementById('aboutVersionSkewNote');
    if (!el) return false;
    if (getComputedStyle(el).display === 'none') return false;
    const txt = el.textContent || '';
    return txt.indexOf('a different release than') !== -1
        && txt.indexOf(PEER_VERSION) !== -1
        && txt.indexOf(LOCAL_VERSION) !== -1;
  }, null, { timeout: 20000 });

  const noteText = await p.evaluate(() => document.getElementById('aboutVersionSkewNote').textContent.trim());
  check(noteText.indexOf('a different release than') !== -1, `skew note missing the "different release" explanation: ${noteText}`);
  check(noteText.indexOf(PEER_VERSION) !== -1 && noteText.indexOf(LOCAL_VERSION) !== -1,
    `skew note must name both builds (${PEER_VERSION} and ${LOCAL_VERSION}): ${noteText}`);
  // The About version row itself reads the PEER daemon, through the proxy.
  const aboutVersion = await p.evaluate(() => document.getElementById('aboutVersion').textContent.trim());
  check(aboutVersion.indexOf(PEER_VERSION) !== -1, `About version should show the peer build ${PEER_VERSION}: ${aboutVersion}`);

  // Still no update banner, still no reload — the note IS the only signal.
  check(!(await bannerPresent()), "'Update available' banner must not appear on the About tab of a version-skewed peer");
  check((await marker()) === 'vskew-about-story', 'page reloaded while viewing the version-skewed peer (marker lost)');
  const hPeerHits = hPeer.hits;
  check(hPeerHits >= 1, `About version fetch should have gone through the peer proxy (hPeer.hits=${hPeerHits})`);

  // ── Switch back to the primary: must NOT reload the page. ──────────────
  await p.evaluate(() => window.selectServer(null));
  await p.waitForFunction(() => state.activeServer === null, null, { timeout: 15000 });
  await new Promise((r) => setTimeout(r, 500)); // let any (buggy) reload path fire

  // The page is the SAME document: our marker survived the switch back.
  check((await marker()) === 'vskew-about-story', 'switching back to the primary reloaded the page (marker lost)');
  check(!(await bannerPresent()), "'Update available' banner must not appear after switching back to the primary");
  const afterBack = await p.evaluate(() => ({
    own: state._daemonVersion,
    updateAvailable: !!state._updateAvailable,
    updateBannerShown: !!state._updateBannerShown,
  }));
  check(afterBack.own === LOCAL_VERSION, `primary's own version must be intact after the switch back (own=${JSON.stringify(afterBack.own)})`);
  check(!afterBack.updateAvailable && !afterBack.updateBannerShown, 'switch back to the primary must not read as an update');

  // Operator re-checks the About tab on the primary: builds match again, so
  // the note is gone. (Re-open the tab: the operator sees the switch-reset
  // view, and renderSettingsView() re-runs loadVersionInfo() against the
  // local /api/health we still fulfill.)
  await p.evaluate(() => { window.navigate('settings'); });
  await p.evaluate(() => {
    const btn = document.querySelector('.settings-tab-btn[data-tab="about"]');
    if (btn) { if (!btn.classList.contains('active')) btn.click(); }
    else window.switchSettingsTab('about');
  });
  await p.waitForFunction((local) => {
    const v = document.getElementById('aboutVersion');
    return v && (v.textContent || '').indexOf(local) !== -1;
  }, LOCAL_VERSION, { timeout: 15000 });
  const noteAfterBack = await p.evaluate(() => {
    const el = document.getElementById('aboutVersionSkewNote');
    return !!el && getComputedStyle(el).display !== 'none';
  });
  check(!noteAfterBack, 'version-skew note must be hidden back on the primary (builds match)');
  check((await marker()) === 'vskew-about-story', 'page reloaded on the final About re-check (marker lost)');

  await saveJson('version-skew-evidence', {
    peer: PEER,
    localVersion: LOCAL_VERSION,
    peerVersion: PEER_VERSION,
    bootVersion,
    afterPeer,
    noteText,
    aboutVersionOnPeer: aboutVersion,
    peerHealthProxyHits: hPeerHits,
    localHealthHits: hLocal.hits,
    afterBack,
    bannerPresent: await bannerPresent(),
    markerSurvived: await marker() === 'vskew-about-story',
  });

  console.log(`${PEER}@${PEER_VERSION} vs primary@${LOCAL_VERSION}: note="${noteText}" — no banner, no reload, switch back clean`);
  console.log('RESULT: pass');

  peerSessions.stop(); serversCtl.stop(); hPeer.stop(); hLocal.stop();
  await context.close();
});
