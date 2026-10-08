// splash-art.js — Canvas port of datawatch-app's MatrixSplashScreen.kt
// (Compose/Kotlin) so the PWA's load splash, Settings -> About, and the
// session-connect overlay visually match the Android app (operator:
// "each should look like in android"). Source of truth for every color,
// radius fraction, and animation timing is
// composeApp/.../ui/splash/MatrixSplashScreen.kt +
// composeApp/.../ui/sessions/SessionLoadingOverlay.kt in dmz006/datawatch-app.
//
// Three entry points, each a self-contained requestAnimationFrame loop:
//   DWSplashArt.startScene(canvas, {compact})   — full Earthrise scene
//     compact=false: main app splash (tablet centered)
//     compact=true:  Settings -> About (tablet in moon area)
//   DWSplashArt.startEyeOnly(canvas)             — bare eye + dot rain + glow
//   DWSplashArt.startSessionLoading(canvas)      — eye-only + lightning bolt
// Each returns a stop() function; callers MUST call it when the canvas
// leaves the DOM/view so the rAF loop doesn't run forever in the background.
(function () {
  'use strict';

  var PAL = {
    bg: '#0F1117',
    bezelDark: '#0D0720',
    screenDark: '#08031A',
    border: '#8B5CF6',
    irisOuter: '#3B0764',
    irisMid: '#8B5CF6',
    irisInner: '#A855F7',
    pupil: '#04020E',
    crosshair: '#E879F9',
    highlight: '#F0ABFC',
    matrix: '#A855F7',
    matrixBright: '#C084FC',
    matrixLead: '#F0ABFC',
    speaker: '#2D1B4E',
    screenBorder: 'rgba(76,29,149,0.8)',
  };
  var MATRIX_CHARS = 'ABCDEF0123456789xWTCHR'.split('');

  function rgba(hex, a) {
    var n = parseInt(hex.slice(1), 16);
    var r = (n >> 16) & 255, g = (n >> 8) & 255, b = n & 255;
    return 'rgba(' + r + ',' + g + ',' + b + ',' + a + ')';
  }

  // Deterministic PRNG (mulberry32) — reproducible column layout, no Math.random().
  function mulberry32(seed) {
    return function () {
      seed |= 0; seed = (seed + 0x6D2B79F5) | 0;
      var t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
      t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }

  function triangleWave(t, durationMs, delayMs) {
    var local = ((t - (delayMs || 0)) % durationMs + durationMs) % durationMs;
    var phase = local / durationMs;
    return phase < 0.5 ? phase * 2 : 2 - phase * 2;
  }
  function smooth(x) { return x * x * (3 - 2 * x); }
  function lerp(a, b, f) { return a + (b - a) * f; }
  function sawtooth(t, durationMs) { return ((t % durationMs) + durationMs) % durationMs / durationMs; }

  function makeColumns(n, seed, xStart, xSpan, charRange) {
    var rng = mulberry32(seed);
    var cols = [];
    for (var i = 0; i < n; i++) {
      cols.push({
        xFrac: xStart + i * (xSpan / Math.max(1, n - 1)),
        delayFrac: rng(),
        charCount: charRange[0] + Math.floor(rng() * (charRange[1] - charRange[0] + 1)),
      });
    }
    return cols;
  }
  function assignChars(cols, seed) {
    var rng = mulberry32(seed);
    return cols.map(function (c) {
      var chars = [];
      for (var i = 0; i < c.charCount; i++) chars.push(MATRIX_CHARS[Math.floor(rng() * MATRIX_CHARS.length)]);
      return chars;
    });
  }
  function makeFlickers(n, seed) {
    // Stable per-index (duration, delay) pairs matching the Kotlin formula.
    var out = [];
    for (var i = 0; i < n; i++) out.push({ dur: 900 + ((i * 73) % 900), delay: (i * 41) % 500 });
    return out;
  }

  // -- eye -----------------------------------------------------------------
  function drawEye(ctx, cx, cy, radius, pupilScale, glowAlpha) {
    if (glowAlpha > 0) {
      ctx.beginPath(); ctx.fillStyle = rgba(PAL.irisMid, glowAlpha * 0.55);
      ctx.arc(cx, cy, radius * 2.3, 0, Math.PI * 2); ctx.fill();
      ctx.beginPath(); ctx.fillStyle = rgba(PAL.irisInner, glowAlpha * 0.28);
      ctx.arc(cx, cy, radius * 1.75, 0, Math.PI * 2); ctx.fill();
    }
    var erx = radius * 1.92, ery = radius * 1.20;
    ctx.beginPath(); ctx.ellipse(cx, cy, erx, ery, 0, 0, Math.PI * 2);
    ctx.fillStyle = '#080518'; ctx.fill();
    ctx.lineWidth = 3.5; ctx.strokeStyle = PAL.border; ctx.stroke();

    ctx.beginPath(); ctx.ellipse(cx, cy, erx * 0.91, ery * 0.91, 0, 0, Math.PI * 2);
    ctx.lineWidth = 1.5; ctx.strokeStyle = rgba(PAL.irisMid, 0.40); ctx.stroke();

    ctx.beginPath(); ctx.fillStyle = PAL.irisOuter; ctx.arc(cx, cy, radius, 0, Math.PI * 2); ctx.fill();
    ctx.beginPath(); ctx.fillStyle = PAL.irisMid; ctx.arc(cx, cy, radius * 0.82, 0, Math.PI * 2); ctx.fill();
    ctx.beginPath(); ctx.fillStyle = rgba(PAL.irisInner, 0.75); ctx.arc(cx, cy, radius * 0.52, 0, Math.PI * 2); ctx.fill();
    ctx.beginPath(); ctx.arc(cx, cy, radius * 0.80, 0, Math.PI * 2);
    ctx.lineWidth = 1.8; ctx.strokeStyle = rgba(PAL.irisInner, 0.55); ctx.stroke();

    var pr = radius * 0.38 * pupilScale;
    ctx.beginPath(); ctx.fillStyle = PAL.pupil; ctx.arc(cx, cy, pr, 0, Math.PI * 2); ctx.fill();
    ctx.beginPath(); ctx.arc(cx, cy, radius * 0.42 * pupilScale, 0, Math.PI * 2);
    ctx.lineWidth = 1.2; ctx.strokeStyle = rgba(PAL.irisOuter, 0.65); ctx.stroke();

    var cr = radius * 0.40, gap = radius * 0.13;
    ctx.strokeStyle = PAL.crosshair; ctx.lineWidth = 4.5; ctx.lineCap = 'round';
    ctx.beginPath(); ctx.moveTo(cx, cy - cr); ctx.lineTo(cx, cy - gap); ctx.stroke();
    ctx.beginPath(); ctx.moveTo(cx, cy + gap); ctx.lineTo(cx, cy + cr); ctx.stroke();
    ctx.beginPath(); ctx.moveTo(cx - cr, cy); ctx.lineTo(cx - gap, cy); ctx.stroke();
    ctx.beginPath(); ctx.moveTo(cx + gap, cy); ctx.lineTo(cx + cr, cy); ctx.stroke();
    ctx.lineCap = 'butt';

    ctx.beginPath(); ctx.fillStyle = PAL.highlight;
    ctx.arc(cx, cy, radius * 0.09 * pupilScale, 0, Math.PI * 2); ctx.fill();

    ctx.save();
    ctx.translate(cx - radius * 0.42, cy - radius * 0.28);
    ctx.rotate(-30 * Math.PI / 180);
    ctx.beginPath();
    ctx.ellipse(-radius * 0.13, -radius * 0.04, radius * 0.14, radius * 0.065, 0, 0, Math.PI * 2);
    ctx.fillStyle = 'rgba(255,255,255,0.13)'; ctx.fill();
    ctx.restore();
  }

  // -- full scene (splash + about) -----------------------------------------
  var STARS = [
    [0.05, 0.06], [0.12, 0.10], [0.20, 0.04], [0.32, 0.08], [0.42, 0.05],
    [0.62, 0.07], [0.72, 0.04], [0.84, 0.10], [0.92, 0.06], [0.08, 0.18],
    [0.94, 0.20], [0.34, 0.16], [0.66, 0.18],
  ];
  var CRATERS = [
    [0.12, 0.78, 0.08], [0.88, 0.80, 0.09], [0.32, 0.55, 0.06], [0.68, 0.55, 0.06],
    [0.06, 0.58, 0.05], [0.94, 0.58, 0.04], [0.22, 0.94, 0.04], [0.78, 0.94, 0.05],
  ];

  function drawScene(ctx, w, h, compact, t) {
    ctx.clearRect(0, 0, w, h);
    ctx.fillStyle = PAL.bg; ctx.fillRect(0, 0, w, h);

    var cx = w / 2, cy = h / 2;
    var discRadius = Math.min(w, h) / 2.2;
    var pupilScale = lerp(0.90, 1.10, smooth(triangleWave(t, 2000, 0)));
    var rainTime = sawtooth(t, 5000);
    var scanY = sawtooth(t, 9000);

    // stars
    ctx.fillStyle = 'rgba(255,255,255,0.55)';
    STARS.forEach(function (s) { ctx.beginPath(); ctx.arc(w * s[0], h * s[1], 1.2, 0, Math.PI * 2); ctx.fill(); });

    // earth
    var earthR = w * 0.065, ex = cx, ey = h * 0.16;
    ctx.beginPath(); ctx.fillStyle = rgba('#7AB8E8', 0.30); ctx.arc(ex, ey, earthR * 1.28, 0, Math.PI * 2); ctx.fill();
    ctx.beginPath(); ctx.fillStyle = '#0B2A55'; ctx.arc(ex, ey, earthR, 0, Math.PI * 2); ctx.fill();
    ctx.beginPath(); ctx.fillStyle = '#4988C8'; ctx.arc(ex - earthR * 0.10, ey - earthR * 0.10, earthR * 0.78, 0, Math.PI * 2); ctx.fill();
    ctx.beginPath(); ctx.fillStyle = '#A8D4F2'; ctx.arc(ex - earthR * 0.20, ey - earthR * 0.20, earthR * 0.40, 0, Math.PI * 2); ctx.fill();
    ctx.beginPath(); ctx.fillStyle = rgba('#2F5E36', 0.7); ctx.arc(ex + earthR * 0.20, ey + earthR * 0.10, earthR * 0.30, 0, Math.PI * 2); ctx.fill();
    ctx.beginPath(); ctx.fillStyle = 'rgba(255,255,255,0.35)'; ctx.arc(ex - earthR * 0.30, ey - earthR * 0.30, earthR * 0.18, 0, Math.PI * 2); ctx.fill();

    // moon
    var horizonY = h * 0.28;
    var grad = ctx.createLinearGradient(0, horizonY, 0, h);
    grad.addColorStop(0, '#7A6B5F'); grad.addColorStop(0.5, '#332B25'); grad.addColorStop(1, '#0F0A08');
    ctx.fillStyle = '#332B25'; ctx.fillRect(0, horizonY, w, h - horizonY);
    ctx.fillStyle = grad; ctx.fillRect(0, horizonY, w, h - horizonY);
    ctx.strokeStyle = rgba('#7AB8E8', 0.4); ctx.lineWidth = 2;
    ctx.beginPath(); ctx.moveTo(0, horizonY); ctx.lineTo(w, horizonY); ctx.stroke();

    CRATERS.forEach(function (c) {
      var ccx = w * c[0], ccy = h * c[1], rx = w * c[2], ry = rx * 0.28;
      ctx.beginPath(); ctx.ellipse(ccx, ccy, rx, ry, 0, 0, Math.PI * 2);
      ctx.fillStyle = '#1A1310'; ctx.fill();
      ctx.beginPath(); ctx.ellipse(ccx, ccy, rx, ry, 0, 0, Math.PI * 2);
      ctx.lineWidth = 1.2; ctx.strokeStyle = rgba('#8B7B6E', 0.6); ctx.stroke();
    });

    // tablet
    var tw = discRadius * 1.8, th = discRadius * 1.4;
    var tl = cx - tw / 2;
    var moonAreaCenter = (horizonY + h) / 2;
    var tt = compact ? moonAreaCenter - th / 2 : cy - th / 2;

    ctx.beginPath(); ctx.ellipse(tl + tw / 2, tt + th - 6, tw / 2 + 6, 9, 0, 0, Math.PI * 2);
    ctx.fillStyle = 'rgba(0,0,0,0.5)'; ctx.fill();

    roundRect(ctx, tl, tt, tw, th, 28); ctx.fillStyle = PAL.bezelDark; ctx.fill();
    ctx.lineWidth = 3; ctx.strokeStyle = PAL.border; ctx.stroke();
    roundRect(ctx, cx - 20, tt + 8, 40, 4, 2); ctx.fillStyle = PAL.speaker; ctx.fill();

    // screen recess (clipped)
    var pad = 14;
    var sl = tl + pad, sTop = tt + pad + 14, sw = tw - 2 * pad, sh = th - 2 * pad - 14;
    roundRect(ctx, sl, sTop, sw, sh, 18); ctx.fillStyle = PAL.screenDark; ctx.fill();
    ctx.lineWidth = 1; ctx.strokeStyle = PAL.screenBorder; ctx.stroke();

    ctx.save();
    roundRect(ctx, sl, sTop, sw, sh, 18); ctx.clip();

    var cols = drawScene._cols || (drawScene._cols = makeColumns(9, 4242, 0.12, 0.76, [7, 10]));
    var chars = drawScene._chars || (drawScene._chars = assignChars(cols, 13));
    var flick = drawScene._flick || (drawScene._flick = makeFlickers(32, 0));
    ctx.font = '13px "SFMono-Regular",Consolas,monospace';
    ctx.textAlign = 'center'; ctx.textBaseline = 'top';
    cols.forEach(function (col, ci) {
      var phase = (rainTime + col.delayFrac) % 1;
      var colY = sTop - 20 + phase * (sh + 40);
      var colX = sl + col.xFrac * sw;
      chars[ci].forEach(function (ch, ri) {
        var y = colY + ri * 16;
        if (y < sTop - 16 || y > sTop + sh) return;
        var fi = (ci * 7 + ri) % flick.length;
        var base = lerp(0.25, 0.85, smooth(triangleWave(t, flick[fi].dur, flick[fi].delay)));
        var posW = 1 - ri / col.charCount;
        var alpha = Math.max(0, Math.min(1, base * (0.45 + 0.55 * posW)));
        var tint = ri === 0 ? PAL.matrixLead : (ri % 3 === 0 ? PAL.matrixBright : PAL.matrix);
        ctx.fillStyle = rgba(tint, alpha);
        ctx.fillText(ch, colX, y);
      });
    });

    ctx.fillStyle = rgba(PAL.border, 0.30);
    ctx.fillRect(sl, sTop + scanY * sh, sw, 2);
    ctx.restore();

    drawEye(ctx, cx, sTop + sh / 2, discRadius * 0.44, pupilScale, 0);
  }

  function roundRect(ctx, x, y, w, h, r) {
    ctx.beginPath();
    ctx.moveTo(x + r, y);
    ctx.arcTo(x + w, y, x + w, y + h, r);
    ctx.arcTo(x + w, y + h, x, y + h, r);
    ctx.arcTo(x, y + h, x, y, r);
    ctx.arcTo(x, y, x + w, y, r);
    ctx.closePath();
  }

  // -- eye-only (session-connect overlay + reusable standalone) -----------
  function drawEyeOnly(ctx, w, h, t) {
    ctx.clearRect(0, 0, w, h);
    ctx.fillStyle = PAL.bg; ctx.fillRect(0, 0, w, h);
    var cx = w / 2, cy = h / 2;
    var pupilScale = lerp(0.90, 1.10, smooth(triangleWave(t, 2000, 0)));
    var glowAlpha = lerp(0.10, 0.28, smooth(triangleWave(t, 3200, 0)));
    var rainTime = sawtooth(t, 5000);

    var cols = drawEyeOnly._cols || (drawEyeOnly._cols = makeColumns(7, 7979, 0.08, 0.84, [6, 9]));
    var flick = drawEyeOnly._flick || (drawEyeOnly._flick = makeFlickers(16, 0));
    cols.forEach(function (col, ci) {
      var phase = (rainTime + col.delayFrac) % 1;
      var colY = -20 + phase * (h + 40);
      var colX = col.xFrac * w;
      for (var ri = 0; ri < col.charCount; ri++) {
        var y = colY + ri * 18;
        if (y < -16 || y > h) continue;
        var fi = (ci * 5 + ri) % flick.length;
        var base = lerp(0.15, 0.60, smooth(triangleWave(t, flick[fi].dur, flick[fi].delay)));
        var alpha = Math.max(0, Math.min(1, base * (0.4 + 0.6 * (1 - ri / col.charCount))));
        ctx.beginPath(); ctx.fillStyle = rgba(PAL.matrix, alpha * 0.6);
        ctx.arc(colX, y, 3, 0, Math.PI * 2); ctx.fill();
      }
    });

    // drawEye's outer ellipse (the eye's actual outline shape) extends to
    // radius*1.92 horizontally and radius*1.20 vertically -- see erx/ery
    // in drawEye. The old flat `Math.min(w,h) * 0.37` assumed a landscape
    // canvas with horizontal headroom beyond "radius" that doesn't exist
    // on a SQUARE canvas -- every loadingEyeBlock() call site (~40 of
    // them) uses a square canvas, so the eye's outer ellipse overflowed
    // the canvas on both sides and got clipped by it: "cut in 1/3 and
    // looks terrible everywhere" (operator-reported 2026-10-08). Size
    // radius so the actual drawn ellipse, including its stroke, fits
    // inside the canvas on both axes regardless of aspect ratio.
    var radius = Math.min(w / (2 * 1.92), h / (2 * 1.20)) * 0.92;
    drawEye(ctx, cx, cy, radius, pupilScale, glowAlpha);
  }

  function drawLightningBolt(ctx, w, h, t) {
    var cx = w / 2, cy = h / 2;
    var boltAlpha = lerp(0.55, 1.0, triangleWave(t, 220, 0));
    var jitter = lerp(-1.5, 1.5, triangleWave(t, 80, 0));
    var stroke = lerp(2.0, 3.5, smooth(triangleWave(t, 600, 0)));
    var bh = Math.min(w, h) * 0.085, bw = bh * 0.55;
    var x = cx + jitter;

    ctx.save();
    ctx.beginPath();
    ctx.moveTo(x + bw * 0.35, cy - bh);
    ctx.lineTo(x - bw * 0.25, cy - bh * 0.08);
    ctx.lineTo(x + bw * 0.55, cy - bh * 0.08);
    ctx.lineTo(x - bw * 0.35, cy + bh);
    ctx.lineJoin = 'round'; ctx.lineCap = 'round';

    ctx.strokeStyle = rgba('#00E5A0', boltAlpha * 0.30); ctx.lineWidth = stroke * 3.5; ctx.stroke();
    ctx.strokeStyle = rgba('#E879F9', boltAlpha); ctx.lineWidth = stroke; ctx.stroke();
    ctx.strokeStyle = 'rgba(255,255,255,' + (boltAlpha * 0.55) + ')'; ctx.lineWidth = stroke * 0.4; ctx.stroke();
    ctx.restore();
  }

  // -- animation driver ------------------------------------------------------
  function fitCanvas(canvas) {
    var dpr = window.devicePixelRatio || 1;
    var rect = canvas.getBoundingClientRect();
    var w = Math.max(1, Math.round(rect.width)), h = Math.max(1, Math.round(rect.height));
    if (canvas.width !== w * dpr || canvas.height !== h * dpr) {
      canvas.width = w * dpr; canvas.height = h * dpr;
    }
    var ctx = canvas.getContext('2d');
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    return { ctx: ctx, w: w, h: h };
  }

  function loop(canvas, draw) {
    var reduced = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    var start = performance.now();
    var stopped = false;
    function frame(now) {
      // Self-stop once the canvas leaves the DOM (view navigated away, or a
      // re-render replaced it with a fresh node) — callers should still call
      // stop() explicitly where they know the exact moment, but this is the
      // backstop so a forgotten teardown path can't leak a rAF loop forever.
      if (stopped || !canvas.isConnected) return;
      var dims = fitCanvas(canvas);
      draw(dims.ctx, dims.w, dims.h, reduced ? 0 : now - start);
      if (!reduced) requestAnimationFrame(frame);
    }
    requestAnimationFrame(frame);
    return function stop() { stopped = true; };
  }

  window.DWSplashArt = {
    startScene: function (canvas, opts) {
      var compact = !!(opts && opts.compact);
      return loop(canvas, function (ctx, w, h, t) { drawScene(ctx, w, h, compact, t); });
    },
    startEyeOnly: function (canvas) {
      return loop(canvas, function (ctx, w, h, t) { drawEyeOnly(ctx, w, h, t); });
    },
    startSessionLoading: function (canvas) {
      return loop(canvas, function (ctx, w, h, t) {
        drawEyeOnly(ctx, w, h, t);
        drawLightningBolt(ctx, w, h, t);
      });
    },
  };
})();
