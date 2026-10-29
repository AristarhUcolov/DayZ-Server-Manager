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
