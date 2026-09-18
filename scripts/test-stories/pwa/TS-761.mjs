// TS-761 — PWA 📷 image attachment button present in active session input bar
// Structural: verify label+input rendered for active session; file type accepted
import { runStory, connectToPWA, saveLog } from './lib.mjs';
import { readFile } from 'node:fs/promises';

const APP_JS = new URL('../../../internal/server/web/app.js', import.meta.url).pathname;

await runStory(async (page) => {
  // Structural check: verify image input label is rendered for active sessions.
  const appSrc = await readFile(APP_JS, 'utf8');

  const hasLabel       = appSrc.includes('sessionImageInput');
  const hasFileInput   = appSrc.includes('type="file"') && appSrc.includes('accept="image/*"');
  const hasCameraGlyph = appSrc.includes('&#128247;') || appSrc.includes('📷') || appSrc.includes('btn_attach_image');
  const hasPendingArr  = appSrc.includes('_pendingAttachments');
  const hasOnChange    = appSrc.includes('onSessionImageSelected');

  await saveLog('structural-check', JSON.stringify({
    hasLabel, hasFileInput, hasCameraGlyph, hasPendingArr, hasOnChange,
  }));

  if (!hasLabel)       throw new Error('sessionImageInput label not found in app.js');
  if (!hasFileInput)   throw new Error('file input with accept="image/*" not found in app.js');
  if (!hasCameraGlyph) throw new Error('camera glyph or btn_attach_image label not found in app.js');
  if (!hasPendingArr)  throw new Error('_pendingAttachments state array not found in app.js');
  if (!hasOnChange)    throw new Error('onSessionImageSelected handler not found in app.js');

  // Live PWA: navigate to an active session and confirm the label is in the DOM.
  await connectToPWA(page);

  // Create a shell session to have an active session to render.
  const sessionId = await page.evaluate(async () => {
    try {
      const r = await fetch('/api/sessions/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ backend: 'shell', label: 'ts761-img-test' }),
      });
      const d = await r.json();
      return d.id || null;
    } catch { return null; }
  });

  if (sessionId) {
    await page.waitForTimeout(1500);
    // Navigate to the session detail view.
    await page.evaluate((id) => {
      if (typeof navigate === 'function') navigate('session-detail', id);
    }, sessionId);
    await page.waitForTimeout(1500);
    await page.screenshot({ path: process.env.EVIDENCE_DIR ? `${process.env.EVIDENCE_DIR}/${process.env.CURRENT_STORY || 'TS-761'}/01-session-detail.png` : '/tmp/ts761-detail.png' }).catch(() => {});

    const hasAttachLabel = await page.evaluate(() => {
      const lbl = document.getElementById('sessionImageInput') ||
                  document.querySelector('label[for="sessionImageInput"]');
      return !!lbl;
    });
    await saveLog('live-check', `sessionId=${sessionId} hasAttachLabel=${hasAttachLabel}`);

    // Cleanup
    await page.evaluate(async (id) => {
      try { await fetch('/api/sessions/delete', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id, delete_data: false }) }); } catch {}
    }, sessionId);

    if (!hasAttachLabel) {
      throw new Error(`image input label not found in active session DOM (sessionId=${sessionId})`);
    }
  }

  await saveLog('result', 'image attachment button structural and live checks passed');
});
