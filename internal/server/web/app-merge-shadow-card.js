// Merge shadow card — PWA side.
//
// window.mergeShadowCardHtml(view) turns the view from mergeShadowFetch into
// the Merge shadow card: the matched and different comparison counts for each
// release size (patch, minor, major, none), summed over every project. The
// card is not rendered at all (an empty string) when the shadow is disabled
// or when there are zero comparisons, so the Setup sub-tab shows nothing for
// it. Every value is escaped.
//
// window.mergeShadowFetch reads GET /api/autonomous/merge/shadow for the
// comparisons and GET /api/config for autonomous.merge_shadow.enabled.
// window.loadMergeShadowCard paints the card into #automationMergeShadowCard.
// The card has no poll of its own; the Setup sub-tab mount reads it.

const MERGE_SHADOW_SIZES = [
  ['patch', 'merge_shadow_size_patch'],
  ['minor', 'merge_shadow_size_minor'],
  ['major', 'merge_shadow_size_major'],
  ['none', 'merge_shadow_size_none'],
];

window.mergeShadowCardHtml = function(view) {
  if (!view || typeof view !== 'object' || view.enabled !== true) return '';
  const projects = Array.isArray(view.projects) ? view.projects : [];
  const sizes = {};
  MERGE_SHADOW_SIZES.forEach(([s]) => { sizes[s] = { matched: 0, different: 0 }; });
  let total = 0;
  projects.forEach(p => {
    const counts = (p && typeof p.counts === 'object' && p.counts) ? p.counts : {};
    MERGE_SHADOW_SIZES.forEach(([s]) => {
      const c = (counts[s] && typeof counts[s] === 'object') ? counts[s] : {};
      const m = Math.max(0, Number(c.matched) || 0);
      const d = Math.max(0, Number(c.different) || 0);
      sizes[s].matched += m;
      sizes[s].different += d;
      total += m + d;
    });
  });
  if (total === 0) return '';
  const rows = MERGE_SHADOW_SIZES.map(([s, key]) =>
    `<tr class="merge-shadow-row merge-shadow-row-${s}">` +
    `<th scope="row" style="text-align:left;font-weight:600;padding:2px 4px 2px 0;">${escHtml(t(key))}</th>` +
    `<td class="merge-shadow-matched" style="text-align:right;padding:2px 4px;">${escHtml(String(sizes[s].matched))}</td>` +
    `<td class="merge-shadow-different" style="text-align:right;padding:2px 0 2px 4px;">${escHtml(String(sizes[s].different))}</td>` +
    `</tr>`).join('');
  return `<div class="prd-capacity-card merge-shadow-card" style="min-width:0;">` +
    `<div class="cap-title">${escHtml(t('merge_shadow_card_title'))}</div>` +
    `<table class="merge-shadow-table" style="width:100%;font-size:12px;border-collapse:collapse;margin-top:4px;">` +
    `<thead><tr><th></th>` +
    `<th style="text-align:right;color:var(--text2);padding:2px 4px;">${escHtml(t('merge_shadow_matched'))}</th>` +
    `<th style="text-align:right;color:var(--text2);padding:2px 0 2px 4px;">${escHtml(t('merge_shadow_different'))}</th>` +
    `</tr></thead><tbody>${rows}</tbody></table></div>`;
};

window.mergeShadowFetch = function() {
  return Promise.all([apiFetch('/api/autonomous/merge/shadow'), apiFetch('/api/config')]).then(([shadow, cfg]) => {
    const ms = cfg && cfg.autonomous && cfg.autonomous.merge_shadow;
    return {
      enabled: !!(ms && ms.enabled === true),
      projects: (shadow && Array.isArray(shadow.projects)) ? shadow.projects : [],
    };
  });
};

window.loadMergeShadowCard = function() {
  const el = document.getElementById('automationMergeShadowCard');
  if (!el) return Promise.resolve();
  return window.mergeShadowFetch()
    .then(view => { el.innerHTML = window.mergeShadowCardHtml(view); })
    .catch(err => {
      el.innerHTML = `<div class="merge-shadow-failed" role="alert" style="color:var(--error,#c00);font-size:12px;overflow-wrap:anywhere;">${escHtml(t('merge_shadow_failed', [String((err && err.message) || err)]))}</div>`;
    });
};
