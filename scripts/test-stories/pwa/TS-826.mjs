// TS-826 — PWA task card: wait-cause label and escaped task/board text
//
// Renders the waiting-task line (window.taskWaitHtml), the Automata progress
// bar and the chat bubble header with hostile values and checks that:
//   - known wait causes render the same chip label as before,
//   - unknown causes (including prototype names) render no chip, and
//   - markup in task, board and role text is shown as text, never parsed.
import { runStory, connectToPWA, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  const result = await page.evaluate(() => {
    const out = {};
    const payload = '<img src=x onerror="window.__ts826=1">';
    window.__ts826 = 0;

    const host = document.createElement('div');
    host.id = 'ts826-host';
    document.body.appendChild(host);

    // Wait-cause chip: every known cause keeps its label.
    const causes = ['pool_full', 'node_busy', 'backoff', 'budget', 'global_cooldown', 'await_external', 'tree_lock'];
    out.chips = {};
    for (const c of causes) {
      host.innerHTML = window.taskWaitHtml({ wait_cause: c, wait_reason: 'held by x' });
      const chip = host.querySelector('.task-wait-chip');
      out.chips[c] = chip ? chip.textContent : null;
    }

    // Unknown causes (including prototype names) render no chip, only the reason.
    out.unknown = {};
    for (const c of ['nope', '__proto__', 'constructor', 'toString', 'hasOwnProperty']) {
      host.innerHTML = window.taskWaitHtml({ wait_cause: c, wait_reason: 'plain reason' });
      out.unknown[c] = {
        chip: !!host.querySelector('.task-wait-chip'),
        text: host.textContent.trim(),
      };
    }

    // Task and cause text with markup renders as text.
    host.innerHTML = window.taskWaitHtml({
      wait_cause: 'pool_full',
      wait_reason: payload,
      prd_title: payload,
      task_title: 'T',
    });
    out.taskImg = host.querySelectorAll('img').length;
    out.taskText = host.textContent;
    host.innerHTML = window.taskWaitHtml({ wait_cause: payload, wait_reason: 'r', wait_resets_at: '' });
    out.causeImg = host.querySelectorAll('img').length;

    // Automata progress bar: a hostile task count renders as text.
    host.innerHTML = renderProgressBar({ stories: [], task_count: payload, task_status_counts: { completed: payload } });
    out.progressImg = host.querySelectorAll('img').length;
    out.progressText = host.textContent;
    host.innerHTML = renderProgressBar({ stories: [{ tasks: [{ status: 'completed' }, { status: 'pending' }] }] });
    out.progressNormal = host.textContent.replace(/\s+/g, ' ').trim();

    // Chat bubble header: an unknown role shows as text.
    const chat = document.createElement('div');
    chat.id = 'chatArea';
    host.appendChild(chat);
    appendChatBubble('ts826', payload, 'hello');
    out.chatImg = chat.querySelectorAll('img').length;
    out.chatRole = chat.querySelector('.chat-role') ? chat.querySelector('.chat-role').textContent : null;

    out.xssFlag = window.__ts826;
    return out;
  });

  await screenshot(page, '01-rendered');
  await saveLog('result', JSON.stringify(result, null, 2));

  const expect = {
    pool_full: 'Pool full', node_busy: 'Node busy', backoff: 'Back-off', budget: 'Budget',
    global_cooldown: 'Cool-down', await_external: 'Awaiting external', tree_lock: 'Tree lock',
  };
  for (const [cause, label] of Object.entries(expect)) {
    if (result.chips[cause] !== label) {
      throw new Error(`wait cause ${cause}: expected chip "${label}", got ${JSON.stringify(result.chips[cause])}`);
    }
  }
  for (const [cause, r] of Object.entries(result.unknown)) {
    if (r.chip) throw new Error(`unknown wait cause ${cause} rendered a chip`);
    if (r.text !== 'plain reason') throw new Error(`unknown wait cause ${cause}: expected "plain reason", got ${JSON.stringify(r.text)}`);
  }
  if (result.taskImg !== 0 || result.causeImg !== 0) throw new Error('task or cause markup was parsed into an element');
  if (!result.taskText.includes('<img src=x')) throw new Error(`task markup not shown as text: ${result.taskText}`);
  if (result.progressImg !== 0) throw new Error('progress bar markup was parsed into an element');
  if (!result.progressText.includes('<img src=x')) throw new Error(`progress bar markup not shown as text: ${result.progressText}`);
  if (!/1\/2 tasks · 50%/.test(result.progressNormal)) throw new Error(`progress bar normal text changed: ${result.progressNormal}`);
  if (result.chatImg !== 0) throw new Error('chat role markup was parsed into an element');
  if (!result.chatRole || !result.chatRole.includes('<img src=x')) throw new Error(`chat role not shown as text: ${result.chatRole}`);
  if (result.xssFlag !== 0) throw new Error('injected handler ran');
});
