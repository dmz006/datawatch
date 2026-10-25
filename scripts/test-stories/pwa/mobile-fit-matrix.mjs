// scripts/test-stories/pwa/mobile-fit-matrix.mjs — pure (no browser, no
// Playwright) matrix builder for the mobile-fit scenario harness.
//
// It turns a profile (smoke | quick | full, plus an exhaustive flag) and a
// view's state lists into an ordered, deduplicated array of cells:
//
//   { scenario, viewport, theme, zoom, state }
//
// where scenario is the stable, human-readable name
// `${state}@${width}x${height}/${theme}/z${zoom}`, viewport is the ACTUAL
// browser viewport the cell should be rendered at (width by height in device
// CSS pixels before zoom), and zoom is the page zoom percentage the cell
// should be zoomed to (see cssViewportForZoom).
//
// The cell's viewport is the device viewport, not the effective CSS
// viewport: the harness derives the CSS viewport with cssViewportForZoom so
// findings can report both.

// Physical device height (CSS px) per logical width, per device class.
export const PHONE_HEIGHTS = { 320: 568, 360: 740, 375: 667, 390: 844, 412: 915 };
export const TABLET_HEIGHTS = { 600: 960, 768: 1024 };
export const DESKTOP_HEIGHTS = { 1280: 800, 1920: 1080 };

export const THEMES = ['light', 'dark'];
export const ZOOMS = [100, 130, 200];

// Logical widths that belong to each device class in the matrix sweeps.
const SMOKE_WIDTH = 390;
const QUICK_PORTRAIT = [320, 360, 390, 768];
const FULL_PORTRAIT = [320, 360, 375, 390, 412, 600, 768];
const FULL_LANDSCAPE = [320, 360, 375, 390, 412];

export const PROFILES = ['smoke', 'quick', 'full'];

// Look up the device for a logical width and build its viewport. Landscape
// swaps width and height, so a 360 phone in landscape is 740 by 360.
export function viewport(width, orientation = 'portrait') {
  const w = Number(width);
  const height = PHONE_HEIGHTS[w] ?? TABLET_HEIGHTS[w] ?? DESKTOP_HEIGHTS[w];
  if (!Number.isInteger(w) || height == null) {
    throw new Error(
      `viewport: no known height for width ${width} ` +
      `(known: ${Object.keys(PHONE_HEIGHTS).concat(Object.keys(TABLET_HEIGHTS), Object.keys(DESKTOP_HEIGHTS)).join(', ')})`,
    );
  }
  if (orientation === 'landscape') {
    return { width: height, height: w, orientation: 'landscape' };
  }
  if (orientation !== 'portrait') {
    throw new Error(`viewport: unknown orientation "${orientation}" (want 'portrait' or 'landscape')`);
  }
  return { width: w, height, orientation: 'portrait' };
}

// cssViewportForZoom derives the effective CSS viewport after applying page
// zoom. This reproduces browser page zoom (Ctrl/= or pinch): the CSS
// viewport shrinks to round(device * 100 / zoom) in each axis, and media
// queries see the narrower width. That makes zoom levels the STRICT case for
// fixed-pixel layouts, which is exactly what mobile-fit is hunting for.
// Findings should therefore report BOTH the effective CSS viewport and the
// zoom the cell was drawn at.
export function cssViewportForZoom(vp, zoom) {
  const z = Number(zoom);
  if (!Number.isFinite(z) || z <= 0) {
    throw new Error(`cssViewportForZoom: invalid zoom ${zoom}`);
  }
  if (!vp || typeof vp !== 'object' || !Number.isFinite(vp.width) || !Number.isFinite(vp.height)) {
    throw new Error('cssViewportForZoom: invalid viewport');
  }
  return {
    width: Math.round((vp.width * 100) / z),
    height: Math.round((vp.height * 100) / z),
    deviceScaleFactor: z / 100,
  };
}

function assertStateList(list, name) {
  if (!Array.isArray(list) || list.length === 0
      || list.some((s) => typeof s !== 'string' || s.trim() === '')) {
    throw new Error(`buildMatrix: invalid ${name} (want a non-empty array of non-empty state name strings)`);
  }
  return list;
}

// buildMatrix turns profile + state lists into the ordered, deduplicated
// cell array:
//
//   smoke     one cell: 390 x 844 portrait, dark, zoom 100, first state.
//   quick     portrait 320/360/390/768 plus 360 landscape, dark, zoom 100,
//             over quickStates; plus light at 360 portrait over quickStates;
//             plus zoom 200 at 360 portrait over the first quick state.
//   full      sweep A: all 12 viewports (portrait 320, 360, 375, 390, 412,
//             600, 768 and landscape 320, 360, 375, 390, 412) x all states,
//             dark, zoom 100.
//             sweep B: light theme x {360 portrait, 390 portrait,
//             768 portrait, 360 landscape} x all states, zoom 100.
//             sweep C: zoom 130 and zoom 200 x {320, 360, 390, 412}
//             portrait x zoomStates, dark.
//             (16 states + 4 zoom states => 288 cells.)
//   exhaustive  profile must be 'full'. The complete cross product of the
//             12 viewports, both themes, all three zooms and all states.
//
// Duplicate cells (same scenario name) are removed, first one wins.
export function buildMatrix({
  profile,
  states = ['default'],
  quickStates = states,
  zoomStates = states,
  exhaustive = false,
} = {}) {
  if (!PROFILES.includes(profile)) {
    throw new Error(`buildMatrix: unknown profile "${profile}" (want one of ${PROFILES.join(', ')})`);
  }
  assertStateList(states, 'states');
  assertStateList(quickStates, 'quickStates');
  assertStateList(zoomStates, 'zoomStates');
  if (typeof exhaustive !== 'boolean') throw new Error('buildMatrix: exhaustive must be a boolean');
  if (exhaustive && profile !== 'full') {
    throw new Error('buildMatrix: exhaustive requires profile "full"');
  }

  const cells = [];
  const seen = new Set();
  const add = (state, vp, theme, zoom) => {
    const scenario = `${state}@${vp.width}x${vp.height}/${theme}/z${zoom}`;
    if (seen.has(scenario)) return; // duplicate scenario name: drop
    seen.add(scenario);
    cells.push({ scenario, viewport: vp, theme, zoom, state });
  };

  const firstOf = (list) => list[0];
  const vps = (widths, orientation) => widths.map((w) => viewport(w, orientation));

  if (profile === 'smoke') {
    add(firstOf(states), viewport(SMOKE_WIDTH), 'dark', 100);
  } else if (profile === 'quick') {
    for (const s of quickStates) for (const w of QUICK_PORTRAIT) add(s, viewport(w), 'dark', 100);
    for (const s of quickStates) add(s, viewport(360, 'landscape'), 'dark', 100);
    for (const s of quickStates) add(s, viewport(360), 'light', 100);
    add(firstOf(quickStates), viewport(360), 'dark', 200);
  } else { // full
    const allViewports = vps(FULL_PORTRAIT, 'portrait').concat(vps(FULL_LANDSCAPE, 'landscape'));
    if (exhaustive) {
      for (const vp of allViewports)
        for (const theme of THEMES)
          for (const z of ZOOMS)
            for (const s of states) add(s, vp, theme, z);
    } else {
      // sweep A: all 12 viewports, dark, zoom 100, all states.
      for (const s of states) for (const vp of allViewports) add(s, vp, 'dark', 100);
      // sweep B: light theme at the reference set, zoom 100, all states.
      for (const s of states) {
        for (const w of [360, 390, 768]) add(s, viewport(w), 'light', 100);
        add(s, viewport(360, 'landscape'), 'light', 100);
      }
      // sweep C: zoom 130 and 200 across the phone widths, dark, zoomStates.
      for (const z of [130, 200])
        for (const w of [320, 360, 390, 412])
          for (const s of zoomStates) add(s, viewport(w), 'dark', z);
    }
  }

  return cells;
}
