(function () {
  'use strict';

  /* ───────── scroll-in animations ───────── */
  var io = new IntersectionObserver(function (entries) {
    entries.forEach(function (e) {
      if (e.isIntersecting) { e.target.classList.add('in'); io.unobserve(e.target); }
    });
  }, { threshold: 0.15 });

  var animating = document.documentElement.classList.contains('hero-anim');
  var hero = document.querySelector('.hero');
  var mark = hero.querySelector('.mark');
  var h1 = hero.querySelector('h1');
  var sub = hero.querySelector('.sub');
  var actions = hero.querySelector('.actions');
  var canvas = document.getElementById('heroCanvas');
  var apparition = document.getElementById('apparition');

  if (animating) {
    /* the boot animation drives the hero elements itself */
    [mark, h1, sub, actions].forEach(function (el) { el.classList.remove('fx'); });
  } else {
    if (canvas) canvas.remove();
    if (apparition) apparition.remove();
  }
  document.querySelectorAll('.fx').forEach(function (el) { io.observe(el); });

  if (!animating) return;

  /* ───────── hero boot animation ───────── */

  /* 30×30 squid sprite extracted from the original 8-bit artwork */
  var SPRITE = [
    '000000000000000000000000000000','000000000000000000000000000000','000000000000000000000000000000',
    '000000000000011110000000000000','000000000001100001100000000000','000000000010000000010000000000',
    '000000000100000011001000000000','000000000100000011001000000000','000000001000000000100100000000',
    '000000001000000000000100000000','000000001000010010000100000000','000000001000010010000100000000',
    '000000000110000000011000000000','000000000002211112200000000000','000000000000000000000000000000',
    '000000000000011110000000000000','000000000001111111100000000000','000000000011111111110000000000',
    '000000000010011001011000000000','000000000011001001001000000000','000000000001001101001100000000',
    '000000000001001101001100000000','000000001000100100100100000000','000000000100100100000100000000',
    '000000000011001000001000000000','000000000000000000010000000000','000000000000000000000000000000',
    '000000000000000000000000000000','000000000000000000000000000000','000000000000000000000000000000'
  ];
  var SBOX = { x: 8, y: 3, w: 16, h: 23 };

  var ctx = canvas.getContext('2d');
  var dpr = Math.min(window.devicePixelRatio || 1, 2);
  var W = hero.clientWidth, H = hero.clientHeight;
  canvas.width = W * dpr;
  canvas.height = H * dpr;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

  function drawPixels(px, py, cell, c1, c2, glitch) {
    for (var y = SBOX.y; y < SBOX.y + SBOX.h; y++) {
      /* glitch: random rows tear sideways */
      var dx = (glitch > 0 && Math.random() < glitch * 0.4) ? (Math.random() * 2 - 1) * cell * 4 : 0;
      var row = SPRITE[y];
      for (var x = SBOX.x; x < SBOX.x + SBOX.w; x++) {
        var v = row[x];
        if (v === '0') continue;
        ctx.fillStyle = v === '1' ? c1 : c2;
        ctx.fillRect(px + (x - SBOX.x) * cell + dx, py + (y - SBOX.y) * cell, cell + 0.5, cell + 0.5);
      }
    }
  }

  function drawSprite(x, y, cell, alpha, glitch) {
    if (glitch > 0.05 && Math.random() < 0.75) {
      /* RGB split ghosts */
      ctx.globalAlpha = alpha * 0.5;
      drawPixels(x - 3 - glitch * 5, y, cell, 'rgba(224,64,80,1)', 'rgba(224,64,80,0.6)', 0);
      drawPixels(x + 3 + glitch * 5, y, cell, 'rgba(64,160,216,1)', 'rgba(64,160,216,0.6)', 0);
    }
    ctx.globalAlpha = alpha;
    drawPixels(x, y, cell, '#f5891a', '#754627', glitch);
    ctx.globalAlpha = 1;
  }

  /* accelerating blitz of pops building tension into the flash:
     pop … pop … pop-pop-popopop, then a breath of silence before the strike */
  var POP_T = [150, 620, 1020, 1300, 1470, 1580, 1660, 1720];
  var minis = POP_T.map(function (t0, i) {
    return {
      t0: t0,
      /* later pops live shorter and never touch the strike at 1800 */
      life: Math.min(420 - i * 35, 1760 - t0),
      x: 0.06 + Math.random() * 0.88,
      y: 0.08 + Math.random() * 0.72,
      cell: 2 + Math.random() * 1.6
    };
  });

  var start = performance.now();

  function frame(now) {
    var t = now - start;
    ctx.clearRect(0, 0, W, H);

    minis.forEach(function (m) {
      var lt = t - m.t0;
      if (lt < 0 || lt > m.life) return;
      var flicker = Math.random() > 0.18 ? 1 : 0.25;
      /* glitch harder while popping in/out */
      var g = (lt < 90 || m.life - lt < 110) ? 0.8 : 0.15;
      drawSprite(m.x * W - SBOX.w * m.cell / 2, m.y * H - SBOX.h * m.cell / 2, m.cell, flicker, g);
    });

    /* the 8-bit squid is already flashing behind the apparition while it
       fades (canvas sits under the apparition layer); once the apparition
       is gone, the morph into the real logo begins */
    if (t >= 2100 && t < 4350) {
      var r = mark.getBoundingClientRect();
      var hr = hero.getBoundingClientRect();
      var cell = r.height / SBOX.h;
      var bx = r.left - hr.left + (r.width - SBOX.w * cell) / 2;
      var by = r.top - hr.top;
      var g, a;
      if (t < 3050) {
        /* already drawn, flashing behind the fading apparition */
        g = 0.45;
        a = Math.random() > 0.2 ? 1 : 0.4;
      } else {
        /* apparition completely gone — zen dissolve into the logo */
        var p = (t - 3050) / 1300;
        g = 0.12 * (1 - p);
        a = 1 - p;
      }
      drawSprite(bx, by, cell, Math.max(0, a), g);
    }

    if (t < 4350) {
      requestAnimationFrame(frame);
    } else {
      ctx.clearRect(0, 0, W, H);
    }
  }
  requestAnimationFrame(frame);

  /* thunder apparition: fast flickers, then long fade */
  setTimeout(function () { apparition.classList.add('strike'); }, 1800);

  /* the title starts its long fade the moment the 8-bit squid appears,
     so it too emerges from behind the fading apparition */
  setTimeout(function () { h1.classList.add('show'); }, 2100);

  /* the real logo follows a little later, under the flashing pixel squid */
  setTimeout(function () { mark.classList.add('show'); }, 2700);

  /* apparition fully faded — morph starts: pixel squid dissolves over the
     logo while the title sputters as it fades in */
  setTimeout(function () {
    var seq = [
      ['white', 'g1'], ['g2'], [], ['white', 'g2'],
      ['g1'], ['g2'], [], ['white', 'g1'], []
    ];
    var i = 0;
    var iv = setInterval(function () {
      h1.classList.remove('white', 'g1', 'g2');
      if (i >= seq.length) { clearInterval(iv); return; }
      seq[i].forEach(function (c) { h1.classList.add(c); });
      i++;
    }, 65);
  }, 3050);

  /* subtitle and buttons fade in while the title is still flickering */
  setTimeout(function () {
    sub.classList.add('show');
    actions.classList.add('show');
  }, 3200);

  /* cleanup */
  setTimeout(function () {
    canvas.remove();
    apparition.remove();
  }, 5200);
})();
