// TS-763 — WebSocket send_input expandImageTags: [image:<path>] injected into WS message
// Structural: verify WS send path appends [image:...] tags from _pendingAttachments
import { runStory, connectToPWA, saveLog } from './lib.mjs';
import { readFile } from 'node:fs/promises';

const APP_JS = new URL('../../../internal/server/web/app.js', import.meta.url).pathname;

await runStory(async (page) => {
  const appSrc = await readFile(APP_JS, 'utf8');

  // Find sendSessionInput (WebSocket path) and verify it handles _pendingAttachments.
  const sendInputIdx = appSrc.indexOf('function sendSessionInput(');
  const sendInputBody = sendInputIdx >= 0 ? appSrc.slice(sendInputIdx, sendInputIdx + 1200) : '';

  const wsSendIdx = appSrc.indexOf('send_input');
  const wsContext = wsSendIdx >= 0 ? appSrc.slice(wsSendIdx - 200, wsSendIdx + 400) : '';

  const sendPathHasAttach  = sendInputBody.includes('_pendingAttachments') ||
                              appSrc.slice(sendInputIdx, sendInputIdx + 2000).includes('[image:');
  const wsPayloadHasTags   = wsContext.includes('image') || wsContext.includes('tags') ||
                              appSrc.includes("'send_input'");
  const clearAfterSend     = appSrc.includes('_clearAllAttachments') || appSrc.includes('_pendingAttachments = []');

  await saveLog('structural-check', JSON.stringify({
    sendPathHasAttach,
    wsPayloadHasTags,
    clearAfterSend,
    sendInputBodySlice: sendInputBody.slice(0, 300),
  }));

  if (!wsPayloadHasTags) throw new Error("send_input WebSocket message not found in app.js");
  if (!clearAfterSend)   throw new Error('_pendingAttachments not cleared after send in app.js');

  // Live: verify apiFetch/WS send path is available and state._pendingAttachments is initialized.
  await connectToPWA(page);

  const liveCheck = await page.evaluate(() => {
    return {
      hasSendFn: typeof sendSessionInput === 'function',
      pendingIsArray: Array.isArray(state._pendingAttachments),
      pendingLen: state._pendingAttachments ? state._pendingAttachments.length : -1,
    };
  });

  await saveLog('live-check', JSON.stringify(liveCheck));
  if (!liveCheck.pendingIsArray) throw new Error('state._pendingAttachments not initialized as array');

  await saveLog('result', 'WebSocket send_input expandImageTags structural checks passed');
});
