// TS-147 — PWA: voice button hidden when whisper not configured
import { runStory, connectToPWA, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);
  await screenshot(page, '01-after-connect');

  // state._whisperEnabled is populated on boot from /api/config
  const whisperEnabled = await page.evaluate(() => {
    return typeof state !== 'undefined' && state._whisperEnabled === true;
  });

  if (whisperEnabled) {
    throw new Error('state._whisperEnabled is true — whisper is configured; voice button would be shown');
  }

  // No voice button should be present anywhere in the DOM
  const voiceBtnCount = await page.evaluate(() =>
    document.querySelectorAll('.voice-input-btn, #voiceInputBtn').length
  );

  if (voiceBtnCount > 0) {
    await screenshot(page, 'voice-btn-found');
    throw new Error(`Voice input button present (${voiceBtnCount}) despite whisper not configured`);
  }

  await saveLog('result', `whisperEnabled=${whisperEnabled} voiceBtnCount=${voiceBtnCount} — PASS`);
  await screenshot(page, '02-no-voice-btn');
});
