// TS-757 — PWA task row: Retry button absent for completed task (gets Re-run button instead)
// Structural: verify canRetry excludes 'completed' and canRequeue covers it
import { runStory, connectToPWA, saveLog } from './lib.mjs';
import { readFile } from 'node:fs/promises';
import path from 'node:path';

const APP_JS = new URL('../../../internal/server/web/app.js', import.meta.url).pathname;

await runStory(async (page) => {
  let appSrc = await readFile(APP_JS, 'utf8');

  // canRetry should NOT include 'completed'
  // Extract the canRetry assignment line
  const canRetryMatch = appSrc.match(/const canRetry\s*=\s*[^;]+;/);
  const canRetryLine = canRetryMatch ? canRetryMatch[0] : '';

  // canRequeue should cover 'completed' tasks
  const canRequeueMatch = appSrc.match(/const canRequeue\s*=\s*[^;]+;/);
  const canRequeueLine = canRequeueMatch ? canRequeueMatch[0] : '';

  const retryExcludesCompleted = canRetryLine && !canRetryLine.includes("'completed'");
  const requeueIncludesCompleted = canRequeueLine && canRequeueLine.includes("'completed'");

  await saveLog('structural-check', JSON.stringify({
    canRetryLine: canRetryLine.trim().slice(0, 200),
    canRequeueLine: canRequeueLine.trim().slice(0, 200),
    retryExcludesCompleted,
    requeueIncludesCompleted,
  }));

  if (!canRetryLine) throw new Error('canRetry assignment not found in app.js');
  if (!retryExcludesCompleted) throw new Error("canRetry should NOT include 'completed' status — retry vs requeue confusion");
  if (!canRequeueLine) throw new Error('canRequeue assignment not found in app.js');
  if (!requeueIncludesCompleted) throw new Error("canRequeue should cover 'completed' tasks for re-run button");

  await connectToPWA(page);
  await saveLog('result', 'canRetry/canRequeue separation verified: completed → requeue, failed → retry');
});
