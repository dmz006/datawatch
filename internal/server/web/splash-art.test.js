// Operator-reported (2026-10-08): the datawatch eye icon (loadingEyeBlock,
// drawn by DWSplashArt.startEyeOnly) was "cut in 1/3 and looks terrible"
// on every card and loading page -- all ~40 call sites use a SQUARE
// canvas. Root cause: drawEye's outer ellipse (the eye's actual outline)
// extends to radius*1.92 horizontally and radius*1.20 vertically, but
// drawEyeOnly's old `radius = Math.min(w,h) * 0.37` assumed a landscape
// canvas with horizontal headroom a square canvas doesn't have -- the
// ellipse overflowed the canvas and got clipped on both sides.
//
// splash-art.js has no prior test coverage at all. This pins the actual
// geometric property that must hold regardless of future tuning: the
// eye's drawn outer ellipse (plus its stroke) must fit inside the canvas
// on both axes, for both a square canvas (every loadingEyeBlock() site)
// and a wide one (the full splash screen).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const SPLASH_ART_JS = path.join(__dirname, 'splash-art.js');

// Builds a fake <canvas> + 2D context that records every ellipse()/arc()
// call's center + radii instead of actually drawing, and a
// requestAnimationFrame that invokes its callback exactly once,
// synchronously, instead of looping forever (real rAF is stubbed to a
// no-op by buildSandbox(), which would mean drawEyeOnly() never runs at
// all in a test).
function loadSplashArtAndCaptureOneFrame(canvasW, canvasH) {
  const sandbox = buildSandbox();
  const calls = { ellipse: [], arc: [] };
  const ctx = {
    clearRect() {}, fillRect() {}, save() {}, restore() {}, translate() {}, rotate() {},
    beginPath() {}, fill() {}, stroke() {}, setTransform() {}, moveTo() {}, lineTo() {},
    set fillStyle(_v) {}, get fillStyle() { return ''; },
    set strokeStyle(_v) {}, get strokeStyle() { return ''; },
    set lineWidth(_v) {}, get lineWidth() { return 0; },
    set lineCap(_v) {}, get lineCap() { return ''; },
    set lineJoin(_v) {}, get lineJoin() { return ''; },
    ellipse(cx, cy, rx, ry) { calls.ellipse.push({ cx, cy, rx, ry }); },
    arc(cx, cy, r) { calls.arc.push({ cx, cy, r }); },
  };
  const canvas = {
    width: 0, height: 0, isConnected: true,
    getContext() { return ctx; },
    getBoundingClientRect() { return { width: canvasW, height: canvasH }; },
  };
  sandbox.window.devicePixelRatio = 1;
  sandbox.performance = { now: () => 0 };
  // Fire exactly once, synchronously -- loop()'s own frame() requests the
  // next frame again at the end, so a naive "always invoke immediately"
  // stub recurses forever. One real frame is enough to capture what
  // drawEyeOnly actually drew.
  let _rafFired = false;
  sandbox.requestAnimationFrame = (cb) => {
    if (_rafFired) return 0;
    _rafFired = true;
    cb(0);
    return 0;
  };
  vm.createContext(sandbox);
  loadScript(SPLASH_ART_JS, sandbox);
  assert.equal(typeof sandbox.DWSplashArt, 'object', 'DWSplashArt is not defined -- did it get renamed?');
  sandbox.DWSplashArt.startEyeOnly(canvas);
  return { calls, canvasW, canvasH };
}

function assertEllipseFitsCanvas(calls, canvasW, canvasH, label) {
  assert.ok(calls.ellipse.length >= 2, `${label}: expected at least 2 ellipse() calls (the eye's outer outline + inner ring), got ${calls.ellipse.length}`);
  // Only the first two ellipse() calls in drawEye are in absolute canvas
  // coordinates -- the outer eye-shape outline (erx/ery, the one that
  // was overflowing) and the inner ring at 0.91x. A third ellipse (the
  // small eyelash/highlight flourish) is drawn after ctx.translate()+
  // ctx.rotate(), so its own cx/cy arguments are LOCAL to that
  // transform, not absolute -- this stub ctx doesn't track a transform
  // matrix, so checking that one here would be comparing the wrong
  // coordinate space, not a real overflow.
  const absoluteCoordEllipses = calls.ellipse.slice(0, 2);
  // Stroke width (~3.5px) adds a little beyond the path itself; allow a
  // small margin for that.
  const strokeMargin = 4;
  for (const { cx, cy, rx, ry } of absoluteCoordEllipses) {
    assert.ok(cx - rx >= -strokeMargin, `${label}: ellipse left edge (cx=${cx}, rx=${rx}) overflows canvas left bound`);
    assert.ok(cx + rx <= canvasW + strokeMargin, `${label}: ellipse right edge (cx=${cx}, rx=${rx}) overflows canvas width ${canvasW}`);
    assert.ok(cy - ry >= -strokeMargin, `${label}: ellipse top edge (cy=${cy}, ry=${ry}) overflows canvas top bound`);
    assert.ok(cy + ry <= canvasH + strokeMargin, `${label}: ellipse bottom edge (cy=${cy}, ry=${ry}) overflows canvas height ${canvasH}`);
  }
}

test('drawEyeOnly: the eye outline fits inside a square canvas (every loadingEyeBlock() call site)', () => {
  const { calls, canvasW, canvasH } = loadSplashArtAndCaptureOneFrame(40, 40);
  assertEllipseFitsCanvas(calls, canvasW, canvasH, '40x40 square');
});

test('drawEyeOnly: the eye outline fits inside a smaller square canvas too', () => {
  const { calls, canvasW, canvasH } = loadSplashArtAndCaptureOneFrame(32, 32);
  assertEllipseFitsCanvas(calls, canvasW, canvasH, '32x32 square');
});

test('drawEyeOnly: the eye outline still fits inside a wide (landscape) canvas', () => {
  const { calls, canvasW, canvasH } = loadSplashArtAndCaptureOneFrame(300, 120);
  assertEllipseFitsCanvas(calls, canvasW, canvasH, '300x120 landscape');
});

test('drawEyeOnly: the eye outline still fits inside a tall (portrait) canvas', () => {
  const { calls, canvasW, canvasH } = loadSplashArtAndCaptureOneFrame(80, 200);
  assertEllipseFitsCanvas(calls, canvasW, canvasH, '80x200 portrait');
});
