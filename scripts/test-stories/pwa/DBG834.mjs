import { runStory, connectToPWA, navigateTo, env } from './lib.mjs';
await runStory(async (page) => {
  const reqs = [];
  page.on('response', r => { const u = r.url(); if (u.includes('/api/') || u.endsWith('/')) reqs.push(`${r.status()} ${u}`); });
  await connectToPWA(page);
  const info = {
    env_http: env.http, env_tls: env.tls,
    href: page.url(),
    reqs: reqs.slice(0, 25),
    splash: await page.evaluate(() => { const e = document.getElementById('splash'); return e ? { display: getComputedStyle(e).display, text: (e.textContent||'').slice(0,60) } : null; }),
    conn: await page.evaluate(() => (window.state ? { ws: state.ws ? state.ws.readyState : 'no-ws', activeView: state.activeView, connText: (document.querySelector('.connection-banner, .conn-banner')||{}).textContent } : 'no-state')),
    lsToken: await page.evaluate(() => (localStorage.getItem('cs_token')||'').slice(0,6)+'...'),
  };
  console.log('DBGRESULTS ' + JSON.stringify(info, null, 1));
});
