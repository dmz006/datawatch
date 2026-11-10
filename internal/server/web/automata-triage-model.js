// Automata triage view model.
//
// Pure helpers that turn one triage record (the wire shape of
// GET /api/autonomous/triage, see internal/autonomous/triage_record.go)
// into what the Automata inbox shows: a headline, a reason, the owning
// Automation's label, the waiting time, and the inbox sort order.
//
// No DOM access and no fetch: the module only computes. Operator-facing
// text is never built here; every headline and reason that needs words
// returns a locale key plus its parameters, so the UI translates it.
// Loads in the browser as window.AutomataTriageModel and under node
// through module.exports.

(function () {
  'use strict';

  // Longest reason text a failed task's error contributes to a headline
  // or reason card; the rest is cut so one huge worker error stays short.
  const REASON_MAX_CHARS = 240;
  const REASON_MAX_LINES = 2;

  // The Go zero time (time.Time{}) as JSON; a record that never set
  // first_seen_at carries this value rather than an absent field.
  const GO_ZERO_TIME_MS = Date.parse('0001-01-01T00:00:00Z');

  // Inbox kinds in display order. Unknown kinds sort with "other".
  const KIND_ORDER = ['question', 'held_question', 'triage', 'security', 'other'];

  function str(v) {
    return typeof v === 'string' ? v : '';
  }

  // firstSentence returns the text up to and including the first sentence
  // end (. ! ?) or the first line break, trimmed. Text with no sentence
  // end comes back whole, trimmed.
  function firstSentence(text) {
    const line = str(text).trim().split(/\r?\n/)[0].trim();
    if (!line) return '';
    const m = line.match(/^.*?[.!?](?=\s|$)/);
    return (m ? m[0] : line).trim();
  }

  // triageHeadline returns the locale key and parameters for the record's
  // one-line headline, chosen by status.
  function triageHeadline(record) {
    const rec = record || {};
    switch (rec.status) {
      case 'failed':
        return { key: 'triage_headline_failed', params: { task: str(rec.task_title) } };
      case 'blocked':
        return { key: 'triage_headline_blocked', params: { reason: firstSentence(rec.blocked_reason) } };
      case 'merge_pending':
        return { key: 'triage_headline_merge_pending', params: { branch: str(rec.branch) } };
      default:
        return { key: 'triage_headline_none', params: {} };
    }
  }

  // triageReason returns the reason text for the record. A failed record
  // reads its error (first two lines, capped at 240 characters), a blocked
  // record its blocked_reason, and anything else its session_end_reason.
  // When the status's own text is empty, session_end_reason is the
  // fallback. When every candidate is empty it returns the locale key
  // triage_reason_none instead of text.
  function triageReason(record) {
    const rec = record || {};
    let text = '';
    if (rec.status === 'failed') {
      const lines = str(rec.error).split(/\r?\n/).slice(0, REASON_MAX_LINES).join('\n').trim();
      text = lines.length > REASON_MAX_CHARS ? lines.slice(0, REASON_MAX_CHARS) : lines;
    } else if (rec.status === 'blocked') {
      text = str(rec.blocked_reason).trim();
    }
    if (!text) {
      text = str(rec.session_end_reason).trim();
    }
    if (!text) {
      return { key: 'triage_reason_none' };
    }
    return { text };
  }

  // triageOwnerLabel is the owning Automation's name, shown only when the
  // record belongs to an Automation; a standalone Automaton gets ''.
  function triageOwnerLabel(record) {
    const rec = record || {};
    return rec.automation_id ? str(rec.automation_name) : '';
  }

  // triageWaitingSince is first_seen_at as given, or '' when the field is
  // missing, unparseable or the Go zero time.
  function triageWaitingSince(record) {
    const v = (record || {}).first_seen_at;
    if (!v) return '';
    const ms = Date.parse(v);
    if (Number.isNaN(ms) || ms === GO_ZERO_TIME_MS) return '';
    return v;
  }

  // timeKey is the item's creation time in ms: created_at, else
  // first_seen_at. Items without a usable time sort after dated ones.
  function timeKey(item) {
    const ms = Date.parse(item.created_at || item.first_seen_at || '');
    return Number.isNaN(ms) ? Infinity : ms;
  }

  // sortInboxItems returns a new array ordered by kind (question, held
  // question, triage, security, other), then oldest first inside each
  // kind. Equal items keep their input order. The input is not mutated.
  function sortInboxItems(items) {
    const rank = (kind) => {
      const i = KIND_ORDER.indexOf(kind);
      return i < 0 ? KIND_ORDER.length - 1 : i;
    };
    return (Array.isArray(items) ? items : [])
      .map((item, index) => ({ item, index, kindRank: rank((item || {}).kind), time: timeKey(item || {}) }))
      .sort((a, b) => {
        if (a.kindRank !== b.kindRank) return a.kindRank - b.kindRank;
        if (a.time !== b.time) return a.time < b.time ? -1 : 1;
        return a.index - b.index;
      })
      .map((entry) => entry.item);
  }

  const AutomataTriageModel = {
    triageHeadline,
    triageReason,
    triageOwnerLabel,
    triageWaitingSince,
    sortInboxItems,
  };

  if (typeof window !== 'undefined') {
    window.AutomataTriageModel = AutomataTriageModel;
  }
  if (typeof module !== 'undefined' && module.exports) {
    module.exports = AutomataTriageModel;
  }
})();
