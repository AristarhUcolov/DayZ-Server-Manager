// Copyright (c) 2026 Aristarh Ucolov.
//
// Pictures for settings that are easier seen than read: the Gameplay page's
// interface options (the hit indicator, the 2D or 3D map, the glow around a
// player at night) picked from drawings, and the Globals page's main numbers
// as sliders over a picture of what they do (infected and animals on the map,
// the condition loot spawns in, how far away infected appear, the logout
// timer, how often a base flag needs raising). All drawings are our own.

// gpPicker shows a setting's values as pictures to pick from (the night
// lighting picker's look): options [{ v, art() → svg, title, sub }].
function gpPicker(label, options, cur, onPick, disabled) {
  const wrap = h('div', { class: 'gp-night', role: 'radiogroup', 'aria-label': label });
  const draw = () => wrap.replaceChildren(...options.map(o => h('button', {
    type: 'button', role: 'radio', 'aria-checked': String(o.v === cur), class: 'gp-night-opt' + (o.v === cur ? ' on' : ''), disabled,
    onclick: () => { cur = o.v; onPick(o.v); draw(); },
  }, [
    h('span', { class: 'gp-night-art', html: o.art() }),
    h('b', { text: o.title }),
    h('small', { text: o.sub }),
  ])));
  draw();
  wrap._redraw = draw;
  wrap._set = (v) => { cur = v; draw(); };
  return wrap;
}

// gpArgb reads cfggameplay's 0xAARRGGBB colour as a CSS colour.
function gpArgb(s) {
  const m = /^0x([0-9a-f]{2})([0-9a-f]{6})$/i.exec(String(s || '').trim());
  if (!m) return 'rgba(187, 10, 30, .9)';
  const a = Math.max(0.25, parseInt(m[1], 16) / 255);
  const n = parseInt(m[2], 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${a.toFixed(2)})`;
}

// gpScene is the game's view the interface pictures sit on: a field, trees
// and a crosshair in the middle.
function gpScene(inner, id) {
  return `<svg viewBox="0 0 160 90" preserveAspectRatio="xMidYMid slice" aria-hidden="true">
  <defs><linearGradient id="gps${id}" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#6d8fb0"/><stop offset="1" stop-color="#b9cfdf"/></linearGradient></defs>
  <rect width="160" height="90" fill="url(#gps${id})"/>
  <path d="M0 58 Q40 48 80 55 T160 52 V90 H0 Z" fill="#5d7a48"/>
  <path d="M0 70 Q50 62 100 68 T160 66 V90 H0 Z" fill="#4a6638"/>
  <path d="M20 56 l5-15 5 15 Z M118 53 l6-17 6 17 Z M132 55 l4-11 4 11 Z" fill="#2f4428"/>
  ${inner}
  <path d="M80 41 v-4 M80 49 v4 M76 45 h-4 M84 45 h4" stroke="#fff" stroke-width="1.2" opacity=".85"/>
</svg>`;
}

// The hit indicator styles, drawn on the left of the screen in colour c.
const GP_HIT = {
  splash: (c, id) => `<defs><radialGradient id="gph${id}"><stop offset="0" stop-color="${c}"/><stop offset="1" stop-color="${c}" stop-opacity="0"/></radialGradient></defs>
    <ellipse cx="14" cy="45" rx="26" ry="30" fill="url(#gph${id})"/>`,
  spike: (c) => `<path d="M6 32 L58 45 L6 58 Z" fill="${c}"/>`,
  arrow: (c) => `<path d="M52 33 L38 45 L52 57 M64 33 L50 45 L64 57" stroke="${c}" stroke-width="5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>`,
};

// gpHitStylePicker: hitDirectionStyle (0 splash, 1 spike, 2 arrow) in the
// colour hitDirectionIndicatorColorStr sets — colour() reads it at draw time.
function gpHitStylePicker(cur, onPick, disabled, colour) {
  const kinds = ['splash', 'spike', 'arrow'];
  return gpPicker(t('gp.hit.style'), kinds.map((k, i) => ({
    v: i, art: () => gpScene(GP_HIT[k](gpArgb(colour()), 's' + i), 'hs' + i),
    title: t('gp.hit.style.' + i), sub: t('gp.hit.style.' + i + '.sub'),
  })), cur, onPick, disabled);
}

// gpHitBehaviourPicker: hitDirectionBehaviour (0 none, 1 static, 2 follows
// the attacker) — the third picture shows the indicator sliding round.
function gpHitBehaviourPicker(cur, onPick, disabled, colour) {
  const arts = [
    () => gpScene('<path d="M20 30 l16 16 M36 30 l-16 16" stroke="#fff" stroke-width="2.5" opacity=".7" stroke-linecap="round"/>', 'hb0'),
    () => gpScene(GP_HIT.spike(gpArgb(colour())), 'hb1'),
    () => gpScene(`<path d="M6 40 L50 32 L10 54 Z" fill="${gpArgb(colour())}" opacity=".45"/>${GP_HIT.spike(gpArgb(colour()))}
      <path d="M40 22 Q60 14 74 26" stroke="#fff" stroke-width="2" fill="none" stroke-dasharray="3 3"/><path d="M70 21 l5 5 -7 1" fill="#fff"/>`, 'hb2'),
  ];
  return gpPicker(t('gp.hit.beh'), arts.map((art, i) => ({ v: i, art, title: t('gp.hit.beh.' + i), sub: t('gp.hit.beh.' + i + '.sub') })), cur, onPick, disabled);
}

// gpMapPicker: use3DMap — the map full screen, or a paper map in the hands.
function gpMapPicker(cur, onPick, disabled) {
  const grid = 'M30 14 V78 M55 14 V78 M80 14 V78 M105 14 V78 M130 14 V78 M18 30 H142 M18 46 H142 M18 62 H142';
  const flat = () => `<svg viewBox="0 0 160 90" aria-hidden="true">
  <rect width="160" height="90" fill="#cdbf98"/>
  <path d="${grid}" stroke="#a89a73" stroke-width=".8"/>
  <path d="M10 70 Q50 40 90 52 T150 24" stroke="#8d6a3c" stroke-width="2.5" fill="none"/>
  <path d="M60 10 Q70 40 64 86" stroke="#5a88b5" stroke-width="3" fill="none"/>
  <circle cx="98" cy="50" r="4" fill="#c0392b"/><circle cx="40" cy="66" r="3" fill="#7d5a3c"/><circle cx="120" cy="30" r="3" fill="#7d5a3c"/>
</svg>`;
  const held = () => `<svg viewBox="0 0 160 90" aria-hidden="true">
  <rect width="160" height="90" fill="#46593a"/>
  <path d="M0 46 Q60 36 160 44 V90 H0 Z" fill="#3a4d30"/>
  <path d="M30 22 L130 22 L146 78 L14 78 Z" fill="#d8caa2" stroke="#8d7d58"/>
  <path d="M55 22 L48 78 M80 22 V78 M105 22 L112 78 M25 40 H135 M20 59 H140" stroke="#ad9f78" stroke-width=".8"/>
  <path d="M28 70 Q60 46 92 56 T138 32" stroke="#8d6a3c" stroke-width="2" fill="none"/>
  <path d="M8 90 Q12 70 26 66 L36 76 Q30 84 30 90 Z M152 90 Q148 70 134 66 L124 76 Q130 84 130 90 Z" fill="#c99a76"/>
</svg>`;
  return gpPicker(t('gp.map'), [
    { v: false, art: flat, title: t('gp.map.2d'), sub: t('gp.map.2d.sub') },
    { v: true, art: held, title: t('gp.map.3d'), sub: t('gp.map.3d.sub') },
  ], cur, onPick, disabled);
}

// gpLightPicker: disablePersonalLight — the faint glow around a player at
// night, or none (darker nights).
function gpLightPicker(cur, onPick, disabled) {
  const night = (glow) => `<svg viewBox="0 0 160 90" aria-hidden="true">
  <defs><radialGradient id="gpl${glow ? 1 : 0}"><stop offset="0" stop-color="#c9d6ff" stop-opacity="${glow ? '.55' : '0'}"/><stop offset="1" stop-color="#c9d6ff" stop-opacity="0"/></radialGradient></defs>
  <rect width="160" height="90" fill="#05070c"/>
  <path d="M0 64 Q60 54 160 62 V90 H0 Z" fill="${glow ? '#141a26' : '#080a10'}"/>
  <circle cx="80" cy="56" r="${glow ? 34 : 1}" fill="url(#gpl${glow ? 1 : 0})"/>
  <g fill="${glow ? '#56607a' : '#161a24'}"><circle cx="80" cy="44" r="4"/><path d="M75 49 h10 l2 16 h-4 l-1 12 h-3 l-1 -9 -1 9 h-3 l-1 -12 h-4 Z"/></g>
</svg>`;
  return gpPicker(t('gp.plight'), [
    { v: false, art: () => night(true), title: t('gp.plight.on'), sub: t('gp.plight.on.sub') },
    { v: true, art: () => night(false), title: t('gp.plight.off'), sub: t('gp.plight.off.sub') },
  ], cur, onPick, disabled);
}

// ------------------------------------------------------------------ Globals

// Deterministic spots for drawn figures (x, ground y, scale), back to front.
const GL_SPOTS = [
  [22, 58, .55], [46, 57, .5], [70, 58, .55], [96, 56, .5], [122, 57, .55], [146, 58, .5],
  [12, 66, .7], [36, 67, .72], [60, 66, .7], [86, 68, .72], [110, 66, .7], [136, 67, .72],
  [26, 78, .9], [52, 80, .92], [78, 79, .9], [104, 80, .92], [128, 78, .9], [150, 80, .92],
  [8, 86, 1.05], [40, 88, 1.05], [92, 88, 1.08], [118, 87, 1.05], [64, 89, 1.08], [144, 88, 1.05],
];
const glZombie = (x, y, s) => `<g transform="translate(${x} ${y}) scale(${s})" fill="#5c7a4c" stroke="#2c3b24" stroke-width=".6"><circle cx="0" cy="-19" r="3.4"/><path d="M-3.5 -15 h7 l1.5 9 h-2.2 l-.6 9 h-2.4 l-.4 -7 -.4 7 h-2.4 l-.6 -9 h-2.2 Z"/><path d="M3.4 -14 l8 2.5 -.8 1.8 -7.4 -1.6 Z M-3.4 -14 l7 4.5 -.8 1.6 -6.6 -3.6 Z"/></g>`;
const glDeer = (x, y, s) => `<g transform="translate(${x} ${y}) scale(${s})" fill="#8a6644"><ellipse cx="0" cy="-10" rx="9" ry="4.4"/><path d="M6 -12 l4 -8 2.4 .4 -3 8.6 Z"/><ellipse cx="11.6" cy="-20" rx="3" ry="2"/><path d="M-7 -7 v8 h1.6 v-7 Z M-3 -7 v8 h1.6 v-7 Z M3 -7 v8 h1.6 v-7 Z M6.6 -7 v8 h1.6 v-7 Z"/><path d="M11 -22 l-2 -6 M12.5 -22 l2.6 -5.4" stroke="#8a6644" stroke-width="1.1" fill="none"/></g>`;

function glSky(top, bottom, id, inner) {
  return `<svg viewBox="0 0 160 90" preserveAspectRatio="xMidYMid slice" aria-hidden="true">
  <defs><linearGradient id="gls${id}" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${top}"/><stop offset="1" stop-color="${bottom}"/></linearGradient></defs>
  <rect width="160" height="90" fill="url(#gls${id})"/>
  <path d="M0 56 Q40 46 80 53 T160 50 V90 H0 Z" fill="#43583a"/>
  <path d="M0 68 Q50 60 100 66 T160 64 V90 H0 Z" fill="#37492f"/>
  ${inner}
</svg>`;
}

// The DayZ condition a damage value (0 pristine … 1 ruined) spawns in.
const GL_COND = [[0.3, 'loadout.cond.pristine', '#3ecf8e'], [0.5, 'loadout.cond.worn', '#a3d65c'], [0.7, 'loadout.cond.damaged', '#f5b740'], [0.9999, 'loadout.cond.badly', '#f08a3c'], [Infinity, 'gl.vis.ruined', '#e5484d']];
const glCond = (d) => GL_COND.find(c => d < c[0]) || GL_COND[GL_COND.length - 1];

// The tiles: which variables each one moves, the slider ranges, the picture
// and the sentence under it. A slider can show another unit than the file
// (the flag in days, the file in seconds) through in/out.
const GL_TILES = [
  { id: 'zombies', vars: [{ name: 'ZombieMaxCount', min: 0, max: 3000, step: 50 }],
    draw: ([v]) => glSky('#2b3350', '#7d6a6a', 'z', GL_SPOTS.slice(0, Math.max(0, Math.min(GL_SPOTS.length, Math.round(v / 1000 * 10)))).map(([x, y, s]) => glZombie(x, y, s)).join('')),
    say: ([v]) => tn('gl.vis.zombies', { n: Math.round(v) }) },
  { id: 'animals', vars: [{ name: 'AnimalMaxCount', min: 0, max: 800, step: 10 }],
    draw: ([v]) => glSky('#7fb0d8', '#d6e6c2', 'a', GL_SPOTS.slice(0, Math.max(0, Math.min(GL_SPOTS.length, Math.round(v / 200 * 5)))).map(([x, y, s]) => glDeer(x, y, s)).join('')),
    say: ([v]) => tn('gl.vis.animals', { n: Math.round(v) }) },
  { id: 'loot', vars: [{ name: 'LootDamageMin', min: 0, max: 1, step: 0.01 }, { name: 'LootDamageMax', min: 0, max: 1, step: 0.01 }],
    draw: ([lo, hi]) => {
      const a = Math.min(lo, hi), b = Math.max(lo, hi), X = (d) => 10 + 140 * Math.max(0, Math.min(1, d));
      let zones = '', prev = 0;
      for (const [to, , col] of GL_COND) { const end = Math.min(1, to); zones += `<rect x="${X(prev)}" y="46" width="${Math.max(0, X(end) - X(prev))}" height="14" fill="${col}" opacity=".85"/>`; prev = end; if (end >= 1) break; }
      return `<svg viewBox="0 0 160 90" aria-hidden="true"><rect width="160" height="90" fill="#1b2130"/>
        ${zones}<rect x="${X(1) - 2}" y="46" width="4" height="14" fill="#e5484d"/>
        <rect x="${X(a)}" y="40" width="${Math.max(2, X(b) - X(a))}" height="26" rx="3" fill="none" stroke="#fff" stroke-width="2"/>
        <path d="M${X(a)} 34 v-8 M${X(b)} 34 v-8" stroke="#fff" stroke-width="1.4"/>
        <g fill="#c7cfdd" font-size="8" font-family="sans-serif"><text x="10" y="78">0</text><text x="146" y="78">1</text></g>
        <path d="M58 14 h44 v14 h-44 Z" fill="#c9a96e" stroke="#7d6640"/><path d="M62 14 q18 -8 36 0" stroke="#7d6640" fill="none"/></svg>`;
    },
    say: ([lo, hi]) => t('gl.vis.loot').replace('{from}', t(glCond(Math.min(lo, hi))[1])).replace('{to}', t(glCond(Math.max(lo, hi))[1])) },
  { id: 'dist', vars: [{ name: 'ZoneSpawnDist', min: 50, max: 1000, step: 10 }],
    draw: ([v]) => {
      const r = 8 + Math.max(0, Math.min(1, v / 1000)) * 34;
      const dots = [0, 1, 2, 3, 4, 5, 6, 7].map(i => { const a = i / 8 * Math.PI * 2 + .3; return `<circle cx="${(80 + Math.cos(a) * r).toFixed(1)}" cy="${(45 + Math.sin(a) * r).toFixed(1)}" r="2.6" fill="#7da35f"/>`; }).join('');
      return `<svg viewBox="0 0 160 90" aria-hidden="true"><rect width="160" height="90" fill="#3f5634"/>
        <path d="M0 20 Q40 30 70 18 T160 26 M0 70 Q50 60 90 74 T160 64" stroke="#55703f" stroke-width="6" fill="none"/>
        <circle cx="80" cy="45" r="${r.toFixed(1)}" fill="rgba(255,255,255,.06)" stroke="#fff" stroke-width="1.2" stroke-dasharray="4 3"/>
        ${dots}<circle cx="80" cy="45" r="4" fill="#fff"/></svg>`;
    },
    say: ([v]) => t('gl.vis.dist').replace('{m}', Math.round(v)) },
  { id: 'logout', vars: [{ name: 'TimeLogout', min: 0, max: 120, step: 1 }],
    draw: ([v]) => {
      const f = Math.max(0, Math.min(1, v / 60)), a = f * Math.PI * 2 - Math.PI / 2;
      const big = f > 0.5 ? 1 : 0, x = 80 + Math.cos(a) * 26, y = 45 + Math.sin(a) * 26;
      const arc = f >= 0.999 ? '<circle cx="80" cy="45" r="26" fill="none" stroke="#ff9a52" stroke-width="6"/>'
        : f <= 0 ? '' : `<path d="M80 19 A26 26 0 ${big} 1 ${x.toFixed(1)} ${y.toFixed(1)}" stroke="#ff9a52" stroke-width="6" fill="none"/>`;
      return `<svg viewBox="0 0 160 90" aria-hidden="true"><rect width="160" height="90" fill="#1b2130"/>
        <circle cx="80" cy="45" r="26" fill="#252d3f" stroke="#3a445c" stroke-width="6"/>${arc}
        <path d="M80 45 V28 M80 45 L91 51" stroke="#e8ecf4" stroke-width="2.4" stroke-linecap="round"/>
        <g fill="#56607a"><circle cx="128" cy="40" r="5"/><path d="M122 46 h12 l2 18 h-5 l-1 14 h-4 l-1 -10 -1 10 h-4 l-1 -14 h-5 Z"/></g>
        <path d="M20 30 h14 v34 h-14 Z" fill="none" stroke="#56607a" stroke-width="2"/><path d="M34 47 h10 m-4 -4 l4 4 -4 4" stroke="#56607a" stroke-width="2" fill="none"/></svg>`;
    },
    say: ([v]) => t('gl.vis.logout').replace('{t}', v > 0 ? fmtLifetime(v) : '0') },
  { id: 'flag', vars: [{ name: 'FlagRefreshFrequency', min: 1, max: 30, step: 1, in: (s) => Math.round(s / 86400), out: (d) => String(d * 86400) }],
    draw: ([v]) => {
      const days = Math.max(1, Math.round(v / 86400)), h = Math.max(10, 50 - Math.min(30, days) * 1.2);
      return glSky('#7fb0d8', '#d6e6c2', 'f', `<path d="M112 82 V${h - 4}" stroke="#d8d8d8" stroke-width="3"/>
        <path d="M113 ${h} h30 l-6 8 6 8 h-30 Z" fill="#c0392b"/>
        <path d="M44 82 h46 v-18 l-23 -14 -23 14 Z" fill="#7d6640" stroke="#4f3f27"/>`);
    },
    say: ([v]) => t('gl.vis.flag').replace('{t}', fmtLifetime(v)) },
];

// glVisuals builds the Globals page's picture tiles over its rows ({name,
// read(), set(), paint()}); changed() after a slider moves. The returned
// paint() redraws everything (after typing in a row, a reset, a discard).
function glVisuals(rows, changed, disabled, label) {
  const byName = new Map(rows.map(r => [r.name, r]));
  const tiles = [];
  for (const def of GL_TILES) {
    const rs = def.vars.map(v => byName.get(v.name));
    if (rs.some(r => !r)) continue;
    const art = h('div', { class: 'gl-vis-art' });
    const say = h('p', { class: 'gl-vis-say' });
    const vals = () => rs.map(r => parseFloat(String(r.read()).replace(',', '.')) || 0);
    const sliders = def.vars.map((v, i) => {
      const r = rs[i];
      const s = h('input', { type: 'range', class: 'wslider', min: String(v.min), max: String(v.max), step: String(v.step), disabled, 'aria-label': label(v.name) });
      s.oninput = () => {
        const x = Number(s.value);
        r.set(v.out ? v.out(x) : (v.step < 1 ? x.toFixed(2) : String(x)));
        r.paint();
        changed();
      };
      return { s, v, r };
    });
    const paint = () => {
      const x = vals();
      for (const sl of sliders) {
        const raw = parseFloat(String(sl.r.read()).replace(',', '.')) || 0;
        const val = sl.v.in ? sl.v.in(raw) : raw;
        sl.s.value = String(val);
        sl.s.style.setProperty('--val', ((Math.max(sl.v.min, Math.min(sl.v.max, val)) - sl.v.min) / (sl.v.max - sl.v.min) * 100) + '%');
      }
      art.innerHTML = def.draw(x);
      say.textContent = def.say(x);
    };
    paint();
    tiles.push({ paint, el: h('div', { class: 'gl-vis-tile' }, [
      art, say,
      ...sliders.map(sl => h('label', { class: 'gl-vis-sl' }, [h('span', { text: label(sl.v.name) }), sl.s])),
    ]) });
  }
  return { el: h('div', { class: 'gl-vis' }, tiles.map(x => x.el)), paint: () => tiles.forEach(x => x.paint()), count: tiles.length };
}

// ------------------------------------------------------------- Server page

// The Server page's gameplay settings as pictures (the camera, the
// crosshair, the night, voice chat) and the login queue as a drawing. The
// fields stay the source of truth: a picture sets its field as if typed and
// follows it when it changes.

function srvField(card, k) { return card.querySelector('[data-key="' + k + '"]'); }

// srvSet changes a field as if the admin had: the unsaved bar follows.
function srvSet(input, v) {
  if (input.type === 'checkbox') input.checked = !!v; else input.value = String(v);
  input.dispatchEvent(new Event('change', { bubbles: true }));
}

function srvSky(id, inner) {
  return `<svg viewBox="0 0 160 90" preserveAspectRatio="xMidYMid slice" aria-hidden="true">
  <defs><linearGradient id="svs${id}" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#6d8fb0"/><stop offset="1" stop-color="#b9cfdf"/></linearGradient></defs>
  <rect width="160" height="90" fill="url(#svs${id})"/>
  <path d="M0 58 Q40 48 80 55 T160 52 V90 H0 Z" fill="#5d7a48"/>
  <path d="M0 70 Q50 62 100 68 T160 66 V90 H0 Z" fill="#4a6638"/>
  <path d="M20 56 l5-15 5 15 Z M118 53 l6-17 6 17 Z M132 55 l4-11 4 11 Z" fill="#2f4428"/>
  ${inner}
</svg>`;
}

const SRV_CROSS = '<path d="M80 41 v-4 M80 49 v4 M76 45 h-4 M84 45 h4" stroke="#fff" stroke-width="1.2" opacity=".9"/>';

// Over the shoulder (third person), the hands and the rifle (first), or a
// car seen from behind (third person in vehicles only).
function srvViewSVG(first) {
  if (first === 'car') {
    return srvSky('v2', `<path d="M60 54 L67 40 H93 L100 54 Z" fill="#5c2420"/>
       <path d="M65 52 L70 43 H90 L95 52 Z" fill="#9fb6c8"/>
       <rect x="50" y="52" width="60" height="26" rx="6" fill="#7a2f2a"/>
       <rect x="53" y="58" width="9" height="5" rx="1.2" fill="#ff6a4d"/>
       <rect x="98" y="58" width="9" height="5" rx="1.2" fill="#ff6a4d"/>
       <rect x="73" y="66" width="14" height="5" rx=".8" fill="#e8e8e8"/>
       <rect x="52" y="76" width="13" height="9" rx="2" fill="#1e1e22"/>
       <rect x="95" y="76" width="13" height="9" rx="2" fill="#1e1e22"/>`);
  }
  return srvSky(first ? 'v1' : 'v0', first
    ? `<path d="M160 70 L96 55 L95 59 L160 79 Z" fill="#2b2b2f"/>
       <path d="M128 64 L160 69 L160 90 L121 90 L117 75 Z" fill="#232326"/>
       <path d="M110 58 L136 63.5 L135 68 L109 62.5 Z" fill="#18181b"/>
       <circle cx="110" cy="60.3" r="3.2" fill="#18181b"/>
       <path d="M97 55 l-1.5 -4.5 h2.2 l1.6 4 Z" fill="#18181b"/>
       <path d="M104 61 q7 -5 13 -1 l-1.5 9 q-7 3.5 -12 -1.5 Z" fill="#6f6048"/>
       <path d="M135 79 q8 -6 15 -2 l-2 13 h-14 Z" fill="#6f6048"/>`
    : `<g transform="translate(56 90)">
         <path d="M-25 0 C-25 -18 -21 -30 -12 -34 L12 -34 C21 -30 25 -18 25 0 Z" fill="#3e4a35"/>
         <path d="M-13 -33 h26 v24 a4 4 0 0 1 -4 4 h-18 a4 4 0 0 1 -4 -4 Z" fill="#6b5536"/>
         <path d="M-13 -23 h26" stroke="#4d3d26" stroke-width="1.5"/>
         <circle cx="0" cy="-42" r="9" fill="#2a2622"/>
         <path d="M-9.5 -45 Q0 -56 9.5 -45 Z" fill="#5c6b44"/>
       </g>`);
}

// A zombie ahead, with or without the crosshair on it.
function srvCrossSVG(on) {
  return srvSky(on ? 'c1' : 'c0', glZombie(80, 60, 0.75) + (on ? SRV_CROSS : ''));
}

// srvVoiceSVG: two players and the voice between them, finer as the codec
// quality rises (0-30); a crossed speaker when voice chat is off.
function srvVoiceSVG(off, q) {
  const fig = (x) => `<g fill="#56607a" transform="translate(${x} 0)"><circle cx="0" cy="34" r="5"/><path d="M-6 40 h12 l2 18 h-5 l-1 14 h-4 l-1 -10 -1 10 h-4 l-1 -14 h-5 Z"/></g>`;
  let mid;
  if (off) {
    mid = `<path d="M66 40 h7 l9 -8 v28 l-9 -8 h-7 Z" fill="#7d879c"/>
      <path d="M88 38 l12 12 M100 38 l-12 12" stroke="#e5484d" stroke-width="3" stroke-linecap="round"/>`;
  } else {
    const levels = 2 + Math.round(Math.max(0, Math.min(30, q)) / 2.5);
    const pts = [];
    for (let i = 0; i <= 64; i++) {
      const u = i / 64, x = 44 + u * 72;
      const env = Math.pow(Math.sin(Math.PI * u), 0.8) * (0.55 + 0.45 * Math.sin(u * 23));
      let v = Math.sin(u * 52) * env;
      v = Math.round(v * levels / 2) / (levels / 2); // coarser at low quality
      pts.push(`${x.toFixed(1)} ${(46 - v * 15).toFixed(1)}`);
    }
    mid = `<path d="M${pts.join(' L')}" fill="none" stroke="#ff9a52" stroke-width="1.8" stroke-linejoin="round"/>`;
  }
  return `<svg viewBox="0 0 160 90" aria-hidden="true"><rect width="160" height="90" fill="#1b2130"/>
    ${fig(26)}${fig(134)}${mid}</svg>`;
}

// srvQueueSVG: the players waiting to log in, and how many go in at once.
function srvQueueSVG(conc, wait) {
  const n = Math.max(1, Math.min(11, Math.round(2 + Math.log10(Math.max(1, wait)) * 3)));
  let q = '';
  for (let i = 0; i < n; i++) {
    const x = 96 - i * 8.2;
    q += `<g fill="#56607a" opacity="${(1 - i * 0.06).toFixed(2)}"><circle cx="${x}" cy="50" r="3"/><path d="M${x - 3.6} 54 h7.2 l1.2 11 h-3 l-.6 8 h-2.4 l-.6 -6 -.6 6 h-2.4 l-.6 -8 h-3 Z"/></g>`;
  }
  const arrows = Math.max(1, Math.min(6, conc));
  let a = '';
  for (let i = 0; i < arrows; i++) {
    const y = 38 + (i - (arrows - 1) / 2) * 6;
    a += `<path d="M104 ${y.toFixed(1)} h10 m-3 -2.5 l3 2.5 -3 2.5" stroke="#3ecf8e" stroke-width="1.6" fill="none"/>`;
  }
  return `<svg viewBox="0 0 160 90" aria-hidden="true"><rect width="160" height="90" fill="#1b2130"/>
    <rect x="120" y="18" width="30" height="56" rx="3" fill="#252d3f" stroke="#3a445c" stroke-width="1.5"/>
    <rect x="128" y="44" width="14" height="30" rx="1.5" fill="#2f3a52"/>
    <g fill="#3ecf8e"><circle cx="127" cy="26" r="1.6"/><circle cx="133" cy="26" r="1.6"/><circle cx="139" cy="26" r="1.6"/><circle cx="145" cy="26" r="1.6" opacity=".35"/></g>
    ${a}${q}
    <path d="M8 76 H112" stroke="#3a445c" stroke-width="1.5" stroke-dasharray="3 3"/></svg>`;
}

// srvVisuals adds the pictures to a group of the Server page's serverDZ.cfg
// card (section: the group's element). The switches they replace are hidden.
function srvVisuals(card, groupId, section, gameplayOn) {
  const box = h('div', { class: 'srv-vis' });
  const hideCell = (input) => {
    const cell = input.closest('.cfg-bool') || input.closest('.cfg-field');
    if (cell) cell.classList.add('cfg-hidden');
  };
  const item = (cls, labelKey, key, control) => h('div', { class: 'srv-vis-item ' + cls }, [
    h('div', { class: 'srv-vis-label' }, [h('span', { text: t(labelKey) }), key ? h('code', { class: 'cfg-key', text: key }) : null].filter(Boolean)),
    control,
  ]);
  // A yes/no field as two pictures.
  const pick = (key, labelKey, opts) => {
    const input = srvField(card, key);
    if (!input) return;
    const cur = () => (input.checked ? 1 : 0);
    const p = gpPicker(t(labelKey), opts, cur(), (v) => srvSet(input, v));
    input.addEventListener('change', () => p._set(cur()));
    hideCell(input);
    box.append(item('', labelKey, key, p));
  };
  if (groupId === 'gameplay') {
    const tpv = srvField(card, 'thirdPersonView');
    if (tpv) {
      // DayZ 1.30+: on (1), off (0), or in vehicles only (2).
      const cur = () => { const n = parseInt(tpv.value, 10); return [0, 1, 2].includes(n) ? n : 1; };
      const p = gpPicker(t('srv.vis.view'), [
        { v: 1, art: () => srvViewSVG(false), title: t('srv.vis.view.3rd'), sub: t('srv.vis.view.3rd.sub') },
        { v: 0, art: () => srvViewSVG(true), title: t('srv.vis.view.1st'), sub: t('srv.vis.view.1st.sub') },
        { v: 2, art: () => srvViewSVG('car'), title: t('srv.vis.view.car'), sub: t('srv.vis.view.car.sub') },
      ], cur(), (v) => srvSet(tpv, v));
      tpv.addEventListener('change', () => p._set(cur()));
      hideCell(tpv);
      box.append(item('srv-vis-wide', 'srv.vis.view', 'thirdPersonView', p));
    } else {
      pick('disable3rdPerson', 'srv.vis.view', [
        { v: 0, art: () => srvViewSVG(false), title: t('srv.vis.view.3rd'), sub: t('srv.vis.view.3rd.sub') },
        { v: 1, art: () => srvViewSVG(true), title: t('srv.vis.view.1st'), sub: t('srv.vis.view.1st.sub') },
      ]);
    }
    pick('disableCrosshair', 'srv.vis.cross', [
      { v: 0, art: () => srvCrossSVG(true), title: t('srv.vis.cross.on'), sub: t('srv.vis.cross.on.sub') },
      { v: 1, art: () => srvCrossSVG(false), title: t('srv.vis.cross.off'), sub: t('srv.vis.cross.off.sub') },
    ]);
    // cfggameplay.json's own lighting wins when it is on: the Gameplay page
    // has the picture then, and this field keeps its "set there" note.
    const light = srvField(card, 'lightingConfig');
    if (light && !gameplayOn) {
      const cur = () => Math.max(0, Math.min(3, parseInt(light.value, 10) || 0));
      const wrap = h('div');
      const draw = () => wrap.replaceChildren(gpNightPicker(cur(), (n) => srvSet(light, n)));
      draw();
      light.addEventListener('change', draw);
      hideCell(light);
      box.append(item('srv-vis-night', 'gp.light.title', 'lightingConfig', wrap));
    }
    const von = srvField(card, 'disableVoN');
    const q = srvField(card, 'vonCodecQuality');
    if (von && q) {
      const art = h('div', { class: 'gl-vis-art' });
      const say = h('p', { class: 'gl-vis-say' });
      const on = h('input', { type: 'checkbox', class: 'switch', 'aria-label': t('srv.vis.von') });
      const sl = h('input', { type: 'range', class: 'wslider', min: '0', max: '30', step: '1', 'aria-label': t('cfg.vonCodecQuality') });
      // DayZ uses 20 when the file leaves it out.
      const qv = () => { const n = parseInt(q.value, 10); return Number.isFinite(n) ? Math.max(0, Math.min(30, n)) : 20; };
      const paint = () => {
        const off = von.checked;
        art.innerHTML = srvVoiceSVG(off, qv());
        say.textContent = off ? t('srv.vis.von.off') : t('srv.vis.von.on').replace('{q}', qv());
        on.checked = !off;
        sl.value = String(qv());
        sl.disabled = off;
        sl.style.setProperty('--val', (qv() / 30 * 100) + '%');
      };
      on.onchange = () => srvSet(von, !on.checked);
      sl.oninput = () => { q.value = sl.value; q.dispatchEvent(new Event('input', { bubbles: true })); paint(); };
      von.addEventListener('change', paint);
      q.addEventListener('input', paint);
      paint();
      hideCell(von);
      hideCell(q);
      box.append(item('srv-vis-tile', 'srv.vis.von', 'disableVoN · vonCodecQuality', h('div', { class: 'gl-vis-tile' }, [
        art, say,
        h('label', { class: 'inline-switch' }, [on, h('span', { text: t('srv.vis.von.switch') })]),
        h('label', { class: 'gl-vis-sl' }, [h('span', { text: t('cfg.vonCodecQuality') }), sl]),
      ])));
    }
  } else if (groupId === 'storage') {
    const conc = srvField(card, 'loginQueueConcurrentPlayers');
    const wait = srvField(card, 'loginQueueMaxPlayers');
    if (conc && wait) {
      const art = h('div', { class: 'gl-vis-art' });
      const say = h('p', { class: 'gl-vis-say' });
      // DayZ's own values when the file leaves them out: 5 at once, 500 waiting.
      const num = (el, d) => { const n = parseInt(el.value, 10); return Number.isFinite(n) && n > 0 ? n : d; };
      const paint = () => {
        art.innerHTML = srvQueueSVG(num(conc, 5), num(wait, 500));
        say.textContent = t('srv.vis.queue').replace('{at}', num(conc, 5)).replace('{wait}', num(wait, 500));
      };
      conc.addEventListener('input', paint);
      wait.addEventListener('input', paint);
      paint();
      box.append(item('srv-vis-tile', 'srv.vis.queue.title', '', h('div', { class: 'gl-vis-tile' }, [art, say])));
    }
  }
  if (!box.childElementCount) return;
  const title = section.querySelector('.cfg-group-title');
  if (title) title.after(box); else section.prepend(box);
}

// ----------------------------------------------------------- Gameplay page

// gpTempChart draws the year's lowest and highest temperatures (WorldsData)
// as a band over the months; read() returns [min[12], max[12]] at draw time.
function gpTempChart(read, months) {
  const el = h('div', { class: 'gp-temps', 'data-gpvis': '1' });
  el._redraw = () => {
    const [lo, hi] = read();
    if (!Array.isArray(lo) || !Array.isArray(hi) || lo.length < 12 || hi.length < 12) { el.replaceChildren(); return; }
    const all = [...lo, ...hi].map(Number).filter(Number.isFinite);
    if (!all.length) { el.replaceChildren(); return; }
    const top = Math.ceil((Math.max(...all) + 3) / 5) * 5, bot = Math.floor((Math.min(...all) - 3) / 5) * 5;
    const W = 900, H = 200, L = 38, R = 10, T = 12, B = 28;
    const X = (i) => L + (W - L - R) * (i + 0.5) / 12;
    const Y = (v) => T + (H - T - B) * (top - v) / Math.max(1, top - bot);
    let grid = '';
    for (let v = bot; v <= top; v += 5) {
      grid += `<line x1="${L}" x2="${W - R}" y1="${Y(v).toFixed(1)}" y2="${Y(v).toFixed(1)}" class="gpt-grid${v === 0 ? ' zero' : ''}"/>` +
        `<text x="${L - 6}" y="${(Y(v) + 3.5).toFixed(1)}" text-anchor="end" class="gpt-ax">${v}°</text>`;
    }
    const line = (arr) => arr.map((v, i) => `${i ? 'L' : 'M'}${X(i).toFixed(1)} ${Y(Number(v)).toFixed(1)}`).join(' ');
    const band = line(hi) + ' ' + lo.map((v, i) => `L${X(11 - i).toFixed(1)} ${Y(Number(lo[11 - i])).toFixed(1)}`).join(' ') + ' Z';
    const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
    const dots = (arr, cls) => arr.map((v, i) => `<circle cx="${X(i).toFixed(1)}" cy="${Y(Number(v)).toFixed(1)}" r="3" class="${cls}"><title>${esc(months[i])}: ${esc(v)}°C</title></circle>`).join('');
    const labels = months.map((m, i) => `<text x="${X(i).toFixed(1)}" y="${H - 8}" text-anchor="middle" class="gpt-ax">${esc(m)}</text>`).join('');
    el.innerHTML = `<div class="gp-temps-head"><b>${esc(t('gp.vis.temps'))}</b>
      <span class="gp-temps-leg"><i class="hi"></i>${esc(t('gp.environmentMaxTemps'))}</span>
      <span class="gp-temps-leg"><i class="lo"></i>${esc(t('gp.environmentMinTemps'))}</span></div>
      <svg viewBox="0 0 ${W} ${H}" class="gp-temps-svg" role="img" aria-label="${esc(t('gp.vis.temps'))}">
      ${grid}<path d="${band}" class="gpt-band"/><path d="${line(hi)}" class="gpt-hi"/><path d="${line(lo)}" class="gpt-lo"/>
      ${dots(hi, 'gpt-dot-hi')}${dots(lo, 'gpt-dot-lo')}${labels}</svg>`;
  };
  el._redraw();
  return el;
}

// gpStaminaTile: how long a player sprints with a given load, from the
// StaminaData numbers (read(key) gives the value in force) and the game's own
// constants (4 stamina a second sprinting upright, 1 crouched; 1.75 a kg
// over the limit off the bar; 4 a second back walking, 2 jogging).
function gpStaminaTile(read) {
  const el = h('div', { class: 'gp-stam', 'data-gpvis': '1' });
  el._redraw = () => {
    const num = (k, d) => { const v = Number(read(k)); return Number.isFinite(v) ? v : d; };
    const max = Math.max(1, num('staminaMax', 100));
    const limit = num('staminaWeightLimitThreshold', 6000);
    const pen = num('staminaKgToStaminaPercentPenalty', 1.75);
    const minCap = num('staminaMinCap', 5);
    const erc = num('sprintStaminaModifierErc', 1);
    const cro = num('sprintStaminaModifierCro', 1);
    const cap = (kg) => kg * 1000 >= limit ? Math.max(max - (kg * 1000 - limit) / 1000 * pen, minCap) : max;
    const secs = (c, rate) => rate > 0 ? String(Math.round(c / rate)) : '∞';
    const rows = [0, 10, 20, 30].map(kg => {
      const c = cap(kg);
      return h('div', { class: 'gp-stam-row' }, [
        h('span', { class: 'gp-stam-kg', text: kg ? t('gp.vis.stam.kg').replace('{kg}', kg) : t('gp.vis.stam.none') }),
        h('span', { class: 'gp-stam-bar' }, h('i', { style: { width: Math.max(2, Math.min(100, c / max * 100)) + '%' } })),
        h('b', { class: 'gp-stam-s', text: t('gp.vis.stam.secs').replace('{s}', secs(c, 4 * erc)) }),
      ]);
    });
    el.replaceChildren(
      h('div', { class: 'gp-stam-head' }, [h('b', { text: t('gp.vis.stam') })]),
      ...rows,
      h('p', { class: 'hint', text: t('gp.vis.stam.note').replace('{c}', secs(max, 1 * cro)).replace('{w}', secs(max, 4)).replace('{j}', secs(max, 2)) }),
    );
  };
  el._redraw();
  return el;
}
