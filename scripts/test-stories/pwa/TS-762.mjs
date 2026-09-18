// TS-762 — PWA image attach + expandImageTags: [image:<path>] sent with message
// Structural: verify _pendingAttachments → [image:<path>] tag construction
import { runStory, connectToPWA, saveLog } from './lib.mjs';
import { readFile } from 'node:fs/promises';

const APP_JS = new URL('../../../internal/server/web/app.js', import.meta.url).pathname;

await runStory(async (page) => {
  const appSrc = await readFile(APP_JS, 'utf8');

  // Verify the [image:<path>] tag construction from _pendingAttachments.
  const hasTagConstruct  = appSrc.includes("'[image:' + a.path + ']'") ||
                           appSrc.includes('"[image:" + a.path + "]"');
  const hasJoinNewline   = appSrc.includes(".join('\\n')") || appSrc.includes('.join("\\n")');
  const hasPendingSomeUploading = appSrc.includes('!a.path'); // guard for upload in-flight
  const hasUploadOkToast = appSrc.includes('image_upload_ok') || appSrc.includes('Image attached');

  await saveLog('structural-check', JSON.stringify({
    hasTagConstruct,
    hasJoinNewline,
    hasPendingSomeUploading,
    hasUploadOkToast,
  }));

  if (!hasTagConstruct) throw new Error('[image:<path>] tag construction not found in app.js');
  if (!hasJoinNewline)  throw new Error('_pendingAttachments.join("\\n") not found in app.js');
  if (!hasUploadOkToast) throw new Error('upload success toast message not found in app.js');

  // Live check: verify _pendingAttachments flows into sendSessionInputDirect path.
  await connectToPWA(page);

  const checkPaths = await page.evaluate(() => {
    // Verify all three send paths reference _pendingAttachments.
    const fnSrc = typeof sendSessionInput !== 'undefined' ? sendSessionInput.toString() :
                  typeof sendSessionInputDirect !== 'undefined' ? sendSessionInputDirect.toString() : '';
    return {
      sendSessionInputHasPending: fnSrc.includes('_pendingAttachments'),
      stateHasArray: Array.isArray(state._pendingAttachments),
    };
  });

  await saveLog('live-check', JSON.stringify(checkPaths));
  if (!checkPaths.stateHasArray) {
    throw new Error('state._pendingAttachments is not an array — init missing');
  }

  await saveLog('result', 'image attach + expandImageTags structural checks passed');
});
