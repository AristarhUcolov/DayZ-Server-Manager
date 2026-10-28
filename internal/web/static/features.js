/*
  DayZ Server Manager — v1.0.0 "October Update" features.
  Copyright (c) 2026 Aristarh Ucolov. All rights reserved.

  Loaded after app.js and sharing its globals (h, t, api, Views, State, …).
  app.js starts main() on DOMContentLoaded, i.e. after this file has run, so
  every view registered here exists before the first navigation.
*/
'use strict';

// ------------------------------------------------------------ settings: mods

// modSourceCard — where mods come from: the DayZ client's !Workshop, any folder
// (incl. SteamCMD's workshop/content/221100 for hosts without the game), or
// nothing (server-only: mods uploaded by hand, no auto-update). Registers
// F.modSource so the Settings auto-save picks it up.
function modSourceCard(c, F, detectBtn) {
  const cur = c.modSource || 'client';
  const modes = ['client', 'folder', 'server', 'steamcmd'];
  const radios = modes.map(v => {
    const r = h('input', { type: 'radio', name: 'mod-source', value: v });
    r.checked = v === cur;
    return r;
  });
  F.modSource = { type: 'radio', get value() { return (radios.find(r => r.checked) || radios[0]).value; } };

  const status = h('div', { class: 'source-status' });
  const clientRow = h('div', {}, [
    h('label', { i18n: 'settings.vanillaPath', for: assocId(F.vanillaDayZPath) }),
    h('div', { class: 'row path-row' }, [F.vanillaDayZPath, detectBtn]),
    h('small', { class: 'hint', i18n: 'settings.modSource.client.hint' }),
  ]);
  // Suggest SteamCMD / Steam workshop download folders found on this machine.
  const findFolder = h('button', { type: 'button', i18n: 'settings.detectDayZ', onclick: async () => {
    try {
      const d = await api.get('/api/steam/detect');
      const list = d.workshop || [];
      if (!list.length) { toast(t('settings.modSource.noWorkshopDir')); return; }
      F.modsFolderPath.value = list[0];
      F.modsFolderPath.dispatchEvent(new Event('change', { bubbles: true }));
      toast(list[0], 'ok');
    } catch (e) { handleErr(e); }
  } });
  const folderRow = h('div', {}, [
    h('label', { i18n: 'settings.modsFolder', for: assocId(F.modsFolderPath) }),
    h('div', { class: 'row path-row' }, [F.modsFolderPath, findFolder]),
    h('small', { class: 'hint', i18n: 'settings.modSource.folder.hint' }),
  ]);
  const serverRow = h('div', { class: 'warning-bar', i18n: 'settings.modSource.server.hint' });
  const steamcmdRow = h('div', { class: 'info-bar source-ws' }, [
    h('span', { class: 'grow', i18n: 'settings.modSource.steamcmd.hint' }),
    h('button', { type: 'button', class: 'secondary small', i18n: 'nav.mods', onclick: () => navigate('mods') }),
  ]);

  let seq = 0;
  async function inspect() {
    const mode = F.modSource.value;
    const my = ++seq;
    status.innerHTML = '';
    if (mode === 'server' || mode === 'steamcmd') return;
    const path = (mode === 'client' ? F.vanillaDayZPath.value : F.modsFolderPath.value).trim();
    if (!path) { status.append(h('span', { class: 'badge warn', i18n: 'settings.modSource.empty' })); applyI18n(); return; }
    try {
      const r = await api.get(`/api/mods/source/inspect?mode=${mode}&path=${encodeURIComponent(path)}`);
      if (my !== seq) return;
      if (!r.exists) {
        status.append(h('span', { class: 'badge err', text: t('settings.modSource.notFound').replace('{path}', r.path) }));
      } else {
        const txt = t('settings.modSource.found').replace('{n}', r.mods).replace('{s}', r.steamcmd);
        status.append(h('span', { class: 'badge ' + (r.mods ? 'ok' : 'warn'), text: txt }));
      }
    } catch (e) { /* inspection is advisory */ }
  }
  const sync = () => {
    const v = F.modSource.value;
    clientRow.hidden = v !== 'client';
    folderRow.hidden = v !== 'folder';
    serverRow.hidden = v !== 'server';
    steamcmdRow.hidden = v !== 'steamcmd';
    inspect();
  };
  radios.forEach(r => r.addEventListener('change', sync));
  F.vanillaDayZPath.addEventListener('change', inspect);
  F.modsFolderPath.addEventListener('change', inspect);
  sync();

  const opt = (i, key) => h('label', { class: 'source-opt' }, [
    radios[i],
    h('span', {}, [h('b', { i18n: key }), h('small', { class: 'hint', i18n: key + '.desc' })]),
  ]);
  return h('div', { class: 'card' }, [
    cardTitle('i-mods', 'settings.modSource', 'settings.modSource'),
    h('div', { class: 'source-opts' }, [
      opt(0, 'settings.modSource.client'),
      opt(1, 'settings.modSource.folder'),
      opt(2, 'settings.modSource.server'),
      opt(3, 'settings.modSource.steamcmd'),
    ]),
    clientRow, folderRow, serverRow, steamcmdRow, status,
  ]);
}

// ------------------------------------------------------- settings: restarts

// restartBehaviourCard — what players see and what happens around a restart:
// the countdown text (in the players' language), the kick-and-lock window just
// before it, and the graceful #shutdown that saves the world. A live timeline
// shows the exact sequence for the next restart.
function restartBehaviourCard(c, F, onChange) {
  const msg = h('input', { type: 'text', value: c.restartWarnMessage || '', placeholder: 'Server restart in {m} min' });
  const kickMsg = h('input', { type: 'text', value: c.restartKickMessage || '', placeholder: 'Server restart - please reconnect in a few minutes' });
  const kickSel = h('select', {}, [
    [0, 'settings.kick.off'], [30, 'settings.kick.30s'], [60, 'settings.kick.1m'],
    [120, 'settings.kick.2m'], [300, 'settings.kick.5m'],
  ].map(([v, k]) => h('option', { value: String(v), i18n: k })));
  const kickVal = String(c.restartKickSeconds || 0);
  if (![...kickSel.options].some(o => o.value === kickVal)) kickSel.append(h('option', { value: kickVal, text: kickVal + ' s' }));
  kickSel.value = kickVal;
  const lock = h('input', { type: 'checkbox', class: 'switch' });
  lock.checked = c.restartLock !== false;
  const graceful = h('input', { type: 'number', min: '0', max: '600', value: c.gracefulStopSeconds ?? 30, style: { width: '110px' } });

  F.restartWarnMessage = msg;
  F.restartKickMessage = kickMsg;
  F.restartKickSeconds = { type: 'number', get value() { return kickSel.value; } };
  F.restartLock = lock;
  F.gracefulStopSeconds = graceful;

  // One click fills the messages in the current interface language.
  const fill = h('button', { class: 'pill', type: 'button', i18n: 'settings.restarts.message.fill',
    onclick: () => {
      msg.value = t('restart.msg.template');
      kickMsg.value = t('restart.kick.template');
      msg.dispatchEvent(new Event('change', { bubbles: true }));
      renderTimeline();
    } });

  const kickRows = h('div', {}, [
    h('label', { class: 'row', style: { gap: '8px', alignItems: 'center' } }, [lock, h('span', { i18n: 'settings.kick.lock' })]),
    h('div', { style: { marginTop: '8px' } }, [h('label', { i18n: 'settings.kick.message', for: assocId(kickMsg) }), kickMsg]),
  ]);
  const timeline = h('div', { class: 'restart-timeline' });

  function renderTimeline() {
    kickRows.hidden = kickSel.value === '0';
    timeline.innerHTML = '';
    const warns = (F.restartWarnMinutes ? F.restartWarnMinutes.value : (c.restartWarnMinutes || []))
      .filter(n => n > 0).sort((a, b) => b - a);
    const kick = Number(kickSel.value) || 0;
    // Anchor on the next scheduled HH:MM when there is one, else a relative T.
    const times = F.scheduledRestarts ? F.scheduledRestarts.value : (c.scheduledRestarts || []);
    let base = null;
    const now = new Date();
    for (const tm of times) {
      const m = /^\s*(\d{1,2}):(\d{2})\s*$/.exec(tm || '');
      if (!m) continue;
      const d = new Date(now); d.setHours(+m[1], +m[2], 0, 0);
      if (d <= now) d.setDate(d.getDate() + 1);
      if (!base || d < base) base = d;
    }
    const at = (offSec) => {
      if (!base) {
        if (!offSec) return 'T';
        const mm = Math.floor(offSec / 60), ss = offSec % 60;
        return 'T−' + mm + ':' + String(ss).padStart(2, '0');
      }
      return new Date(base.getTime() - offSec * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: kick % 60 ? '2-digit' : undefined });
    };
    // Empty = the server's built-in English default, so preview exactly that.
    const text = msg.value.trim() || 'Server restart in {m} min';
    const steps = warns.map(m => ({ off: m * 60, kind: 'warn', label: '“' + text.replace('{m}', m) + '”' }));
    if (kick > 0) steps.push({ off: kick, kind: 'kick', label: t(lock.checked ? 'timeline.kickLock' : 'timeline.kick') });
    steps.sort((a, b) => b.off - a.off);
    steps.push({ off: 0, kind: 'restart', label: t(Number(graceful.value) > 0 ? 'timeline.restartGraceful' : 'timeline.restart') });
    timeline.append(h('div', { class: 'hint', i18n: 'settings.restarts.preview' }));
    const ol = h('ol', { class: 'tl' });
    for (const st of steps) {
      ol.append(h('li', { class: 'tl-' + st.kind }, [h('span', { class: 'tl-at mono', text: at(st.off) }), h('span', { text: st.label })]));
    }
    timeline.append(ol);
  }
  [msg, kickMsg, kickSel, lock, graceful].forEach(el => el.addEventListener('input', renderTimeline));
  kickSel.addEventListener('change', renderTimeline);
  lock.addEventListener('change', renderTimeline);
  // The warn-minutes and times inputs live in the neighbouring card.
  setTimeout(renderTimeline, 0);
  // Re-render when the neighbouring schedule card changes; the listener removes
  // itself once this Settings page is gone, so revisits don't stack handlers.
  const onDocChange = () => {
    if (!document.body.contains(timeline)) { document.removeEventListener('change', onDocChange); return; }
    renderTimeline();
  };
  document.addEventListener('change', onDocChange);

  return h('div', { class: 'card' }, [
    cardTitle('i-players', 'settings.restartBehaviour', 'settings.restartBehaviour'),
    h('div', {}, [
      h('label', { i18n: 'settings.restarts.message', for: assocId(msg) }),
      h('div', { class: 'row path-row' }, [msg, fill]),
      h('small', { class: 'hint', i18n: 'settings.restarts.message.hint' }),
    ]),
    h('h4', { i18n: 'settings.kick.title', style: { marginTop: '16px' } }),
    h('p', { class: 'hint', i18n: 'settings.kick.hint' }),
    h('div', {}, [h('label', { i18n: 'settings.kick.seconds', for: assocId(kickSel) }), kickSel]),
    kickRows,
    h('h4', { i18n: 'settings.graceful', style: { marginTop: '16px' } }),
    h('div', { class: 'row', style: { gap: '8px', alignItems: 'center' } }, [
      graceful, h('span', { class: 'hint', i18n: 'settings.graceful.unit' }),
    ]),
    h('small', { class: 'hint', i18n: 'settings.graceful.hint' }),
    timeline,
  ]);
}

// ---------------------------------------------------- settings: performance

// performanceCard — CPU affinity (which cores DayZServer may use) and process
// priority, applied right after every start and immediately on save.
function performanceCard(c, F) {
  const card = h('div', { class: 'card' }, [
    cardTitle('i-dashboard', 'settings.perf.title', 'settings.perf'),
    h('p', { class: 'hint', i18n: 'settings.perf.hint' }),
  ]);
  const prio = h('select', {}, [
    h('option', { value: '', i18n: 'prio.default' }),
    ...['idle', 'belownormal', 'normal', 'abovenormal', 'high'].map(v => h('option', { value: v, i18n: 'prio.' + v })),
  ]);
  prio.value = c.processPriority || '';
  F.processPriority = prio;

  let boxes = [];
  F.cpuAffinity = {
    type: 'list',
    get value() {
      if (!boxes.length) return (c.cpuAffinity || []).slice();
      const on = boxes.map((b, i) => (b.checked ? i : -1)).filter(i => i >= 0);
      return (on.length === boxes.length || on.length === 0) ? [] : on;
    },
  };
  const grid = h('div', { class: 'core-grid' });
  const now = h('div', { class: 'hint' });
  const quick = h('div', { class: 'pills' });
  // -cpuCount belongs with the cores it describes; kept equal to the cores
  // picked (the old hint asked the admin to sync the two cards by hand).
  F.cpuCount = h('input', { type: 'number', min: '1', max: '256', value: c.cpuCount ?? '' });
  card.append(
    h('label', { i18n: 'settings.perf.cores' }), quick, grid,
    h('div', { class: 'grid-2', style: { marginTop: '10px' } }, [
      field('settings.cpuCount', F.cpuCount, 'settings.perf.cpuCountAuto'),
      h('div', { class: 'field' }, [h('label', { i18n: 'settings.perf.priority', for: assocId(prio) }), prio]),
    ]),
    now,
  );

  (async () => {
    let info = {};
    try { info = await api.get('/api/server/tuning'); } catch { return; }
    const n = Math.max(1, info.numCPU || 1);
    const sel = new Set(c.cpuAffinity || []);
    boxes = [];
    for (let i = 0; i < n; i++) {
      const cb = h('input', { type: 'checkbox', 'aria-label': 'CPU ' + i });
      cb.checked = sel.size === 0 || sel.has(i);
      boxes.push(cb);
      grid.append(h('label', { class: 'core-cb' }, [cb, h('span', { text: String(i) })]));
    }
    // Picking cores sets -cpuCount to how many (all cores: back to the
    // machine's count).
    const syncCount = () => {
      const on = boxes.filter(b => b.checked).length;
      F.cpuCount.value = String(on || n);
    };
    grid.addEventListener('change', syncCount);
    const setAll = (fn) => {
      boxes.forEach((b, i) => { b.checked = fn(i); });
      syncCount();
      // Fire from a checkbox: the form's autosave only reacts to input targets.
      if (boxes[0]) boxes[0].dispatchEvent(new Event('change', { bubbles: true }));
    };
    quick.append(
      h('button', { class: 'pill', type: 'button', i18n: 'settings.perf.all', onclick: () => setAll(() => true) }),
      h('button', { class: 'pill', type: 'button', i18n: 'settings.perf.skip0', onclick: () => setAll(i => i !== 0 || n === 1) }),
      h('button', { class: 'pill', type: 'button', i18n: 'settings.perf.firstHalf', onclick: () => setAll(i => i < Math.ceil(n / 2)) }),
      h('button', { class: 'pill', type: 'button', i18n: 'settings.perf.secondHalf', onclick: () => setAll(i => i >= Math.floor(n / 2)) }),
    );
    if (info.running) {
      const cores = (info.cores || []).length === n ? t('settings.perf.allCores') : (info.cores || []).join(', ');
      now.textContent = t('settings.perf.current').replace('{c}', cores || '—').replace('{p}', info.priority ? t('prio.' + info.priority) : '—');
    }
    if (info.error) now.append(h('div', { class: 'badge err', text: t('settings.perf.error').replace('{e}', info.error) }));
    applyI18n();
  })();
  return card;
}

// --------------------------------------------------------- open folder / refresh

// svgIcon returns an inline sprite icon (the h() helper can't build SVG).
function svgIcon(id, size = 16) {
  return `<svg viewBox="0 0 24 24" width="${size}" height="${size}" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><use href="#${id}"/></svg>`;
}

// revealBtn — "Open folder": opens Explorer / the file manager at a file or
// folder on the server machine. target is {kind}, {path}, {mod}, or a function
// returning one (for "the file currently open in this editor"). When the panel
// is used from another device the path is shown to copy instead.
function revealBtn(target, opts = {}) {
  const btn = h('button', {
    class: 'secondary btn-reveal' + (opts.iconOnly ? ' icon-only' : ''), type: 'button',
    'data-i18n-title': 'reveal.title', title: t('reveal.title'), 'aria-label': t('reveal.title'),
    onclick: async (e) => {
      e.stopPropagation();
      const tg = typeof target === 'function' ? target() : target;
      if (!tg) { toast(t('files.selectFile')); return; }
      await revealPath(tg);
    },
  });
  btn.innerHTML = svgIcon('i-folderopen', 15);
  if (!opts.iconOnly) btn.append(h('span', { i18n: opts.label || 'reveal.open' }));
  return btn;
}

async function revealPath(tg) {
  try {
    const r = await api.post('/api/fs/reveal', tg);
    if (r.opened) return;
    showPathModal(r.path, r.reason);
  } catch (e) { handleErr(e); }
}

// showPathModal — the path to open by hand (remote viewer / headless host).
function showPathModal(path, reason) {
  const m = openModal({ title: t('reveal.title') });
  const inp = h('input', { type: 'text', value: path, readonly: true, 'aria-label': t('reveal.title') });
  m.body.append(
    h('p', { class: 'hint', i18n: reason === 'remote' ? 'reveal.remote' : (reason === 'headless' ? 'reveal.headless' : 'reveal.failed') }),
    h('div', { class: 'row path-row' }, [inp, h('button', { i18n: 'action.copy', onclick: () => copyText(path, inp) })]),
  );
  applyI18n();
  inp.select();
}

async function copyText(text, inp) {
  try {
    await navigator.clipboard.writeText(text);
    toast(t('msg.copied'), 'ok');
  } catch {
    // Clipboard API is unavailable on plain-http LAN origins: copy from a
    // selected field instead (a hidden one when the caller has none).
    const tmp = inp ? null : h('textarea', { 'aria-hidden': 'true', style: { position: 'fixed', top: '-1000px', opacity: '0' } });
    if (tmp) { tmp.value = text; document.body.appendChild(tmp); }
    const el = inp || tmp;
    el.focus(); el.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch { /* not supported */ }
    if (tmp) tmp.remove();
    if (ok) toast(t('msg.copied'), 'ok');
  }
}

// refreshCurrentView re-renders the current page, re-reading everything from
// disk (files edited outside the panel show up). Views with unsaved edits set
// root._isDirty; views that should reopen the same file set root._beforeRefresh.
async function refreshCurrentView() {
  const root = document.getElementById('view');
  if (root && typeof root._isDirty === 'function' && root._isDirty() &&
      !(await confirmModal(t('refresh.discard'), { danger: true, okText: t('action.refreshPage') }))) return;
  if (root && typeof root._beforeRefresh === 'function') { try { root._beforeRefresh(); } catch {} }
  await refreshStatus();
  await navigate(currentRoute());
  toast(t('refresh.done'), 'ok');
}
(() => {
  const btn = document.getElementById('refresh-btn');
  if (btn) btn.addEventListener('click', refreshCurrentView);
})();

// quickFoldersCard — one-click jumps to the folders admins hop between all day.
function quickFoldersCard() {
  const items = [
    ['server', 'folders.server'], ['mission', 'folders.mission'], ['db', 'folders.db'],
    ['profiles', 'folders.profiles'], ['keys', 'folders.keys'], ['battleye', 'folders.battleye'],
    ['mpmissions', 'folders.mpmissions'], ['modsource', 'folders.modsource'], ['backups', 'folders.backups'],
  ];
  return h('div', { class: 'card' }, [
    h('h3', { i18n: 'folders.title' }),
    h('div', { class: 'quick-folders' }, items.map(([kind, key]) => {
      const b = revealBtn({ kind }, { label: key });
      return b;
    })),
  ]);
}

// ------------------------------------------------------------ event spawn map

// parseCoordList turns pasted text into spawn positions. Accepts, one or more
// per line: `<pos x=".." z=".." a=".." y=".."/>` (cfgeventspawns format), and
// plain numbers in any separator style — "x y z", "x, y, z", "<x, y, z>",
// "[x, y, z]", iZurvive's "x / z", with or without an angle as a 4th number.
// Decimal commas ("7500,12 245,3 3200,4") are understood when numbers are
// space-separated. `order` says what three numbers mean: 'xyz' = the DayZ
// vector order X, height, Z (admin tools, DayZ Editor); 'xzy' = X, Z, height.
function parseCoordList(text, order) {
  const out = [];
  let bad = 0;
  for (let line of String(text || '').split(/\r?\n/)) {
    line = line.trim();
    if (!line || line.startsWith('//') || line.startsWith('#')) continue;
    if (/<pos\b/i.test(line)) {
      for (const m of line.matchAll(/<pos\b([^>]*)>/gi)) {
        const at = {};
        for (const a of m[1].matchAll(/([A-Za-z_]+)\s*=\s*"([^"]*)"/g)) at[a[1].toLowerCase()] = a[2];
        const x = parseFloat(at.x), z = parseFloat(at.z);
        if (!Number.isFinite(x) || !Number.isFinite(z)) { bad++; continue; }
        const p = { x, z, a: Number.isFinite(parseFloat(at.a)) ? parseFloat(at.a) : 0 };
        if (at.y !== undefined && Number.isFinite(parseFloat(at.y))) p.y = parseFloat(at.y);
        out.push(p);
      }
      continue;
    }
    let tokens = line.split(/[\s;|/<>\[\]()"']+/).filter(Boolean);
    if (tokens.length === 1) tokens = tokens[0].split(',');
    const nums = [];
    for (let tok of tokens) {
      tok = tok.replace(/^,+|,+$/g, '');
      if (/^-?\d+,\d+$/.test(tok)) tok = tok.replace(',', '.');
      for (const part of tok.split(',')) {
        const n = parseFloat(part);
        if (Number.isFinite(n) && /^-?\d/.test(part)) nums.push(n);
      }
    }
    if (nums.length < 2 || nums.length > 4) { bad++; continue; }
    let p;
    if (nums.length === 2) p = { x: nums[0], z: nums[1], a: 0 };
    else if (order === 'xzy') p = { x: nums[0], z: nums[1], y: nums[2], a: nums[3] || 0 };
    else p = { x: nums[0], y: nums[1], z: nums[2], a: nums[3] || 0 };
    out.push(p);
  }
  return { points: out, bad };
}

const round2 = (v) => Math.round(v * 100) / 100;

Views.eventmap = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.eventmap', 'eventmap.subtitle', [revealBtn({ kind: 'missionfile', path: 'cfgeventspawns.xml' })]));
  const running = !!State.serverStatus.running;
  if (running) root.append(runningBanner());

  let d;
  try { d = await api.get('/api/eventspawns'); }
  catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;

  // Name -> positions, seeded from cfgeventspawns and extended with any events
  // defined in events.xml that have no points yet.
  const byName = new Map();
  for (const e of (d.events || [])) byName.set(e.name, (e.pos || []).map(p => ({ ...p })));
  for (const name of (d.defined || [])) if (!byName.has(name)) byName.set(name, []);
  const names = [...byName.keys()].sort();
  const size = (d.map && d.map.size) || 15360;
  // What events.xml says about each event: { mode, active, nominal }.
  const info = d.info || {};
  const anyDefined = (d.defined || []).length > 0;

  // Open on the first event that actually has points (vanilla lists several
  // empty animal/infected entries first).
  const firstWithPoints = (d.events || []).find(e => (e.pos || []).length);
  let cur = firstWithPoints ? firstWithPoints.name : (names[0] || '');
  // Deep link from the Vehicles and Events pages ("Edit spawn points").
  if (window._eventmapTarget) {
    if (byName.has(window._eventmapTarget)) cur = window._eventmapTarget;
    else { cur = window._eventmapTarget; byName.set(cur, []); names.push(cur); names.sort(); }
    window._eventmapTarget = null;
  }
  let pos = [];      // working copy for the selected event
  let saved = '[]';  // pos as loaded or last saved (JSON): unsaved = pos differs
  let last = '[]';   // pos after the latest recorded change (JSON)
  let undo = [];     // earlier states of pos (JSON), newest last
  let sel = -1;      // selected point index
  const isDirty = () => JSON.stringify(pos) !== saved;
  root._isDirty = isDirty;

  const gm = buildGameMap(d.map, {
    onImageChange: () => { if (isDirty()) toast(t('map.image.savedReload'), 'ok'); else navigate('eventmap'); },
  });
  gm.svg.classList.add('gm-edit');
  const coarse = coarsePointer();
  const circles = [], hits = [];

  const sel$ = h('select', { 'aria-label': t('eventmap.event') });
  function rebuildSelect() {
    sel$.innerHTML = '';
    for (const name of names) {
      const n = (name === cur ? pos : (byName.get(name) || [])).length;
      const o = h('option', { value: name, text: name + '  (' + n + ')' });
      if (name === cur) o.selected = true;
      sel$.append(o);
    }
  }
  const readout = h('div', { class: 'map-readout' });
  const countLbl = h('span', { class: 'hint' });
  const infoLine = h('div', { class: 'ev-info' });
  const addMode = h('input', { type: 'checkbox', class: 'switch' });
  addMode.checked = true;

  // One unsaved-changes bar for the page: Ctrl+S saves, Ctrl+Z undoes.
  const barText = h('span');
  const undoBtn = h('button', { type: 'button', class: 'secondary small', i18n: 'map.undo', title: t('map.undo.hint'), onclick: () => undoStep() });
  const saveBtn = h('button', { type: 'button', class: 'primary small', i18n: 'eventmap.save', disabled: running, onclick: (e) => save(e.currentTarget) });
  const bar = h('div', { class: 'sticky-save', hidden: true }, [
    barText, h('span', { class: 'grow' }), undoBtn,
    h('button', { type: 'button', class: 'secondary small', i18n: 'action.discard', onclick: async () => {
      if (await confirmModal(t('eventmap.discard').replace('{name}', cur), { danger: true, okText: t('action.discard') })) loadCur();
    } }),
    saveBtn,
  ]);
  function updateBar() {
    bar.hidden = !isDirty();
    barText.textContent = t('eventmap.unsaved').replace('{name}', cur);
    undoBtn.disabled = !undo.length;
  }
  // markDirty records a change: the state before it becomes one undo step.
  function markDirty() {
    const now = JSON.stringify(pos);
    if (now !== last) {
      undo.push(last);
      if (undo.length > 100) undo.shift();
      last = now;
    }
    updateBar();
  }
  function undoStep() {
    if (!undo.length) return;
    last = undo.pop();
    pos = JSON.parse(last);
    if (sel >= pos.length) sel = -1;
    render(); renderTable(); updateBar();
  }
  root._save = () => (isDirty() ? save(saveBtn) : undefined);
  // Ctrl+Z undoes, Delete removes the selected point — except while typing in
  // a field, where both keep their usual meaning.
  const onKey = (e) => {
    if (myNav !== _navSeq || document.querySelector('.modal-overlay')) return;
    const el = e.target;
    if (el && (/^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName) || el.isContentEditable)) return;
    if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey && (e.code === 'KeyZ' || e.key === 'z' || e.key === 'Z')) {
      e.preventDefault();
      undoStep();
    } else if (e.key === 'Delete' && sel >= 0 && sel < pos.length) {
      e.preventDefault();
      removePoint(sel);
    }
  };
  document.addEventListener('keydown', onKey);
  root._teardown = () => document.removeEventListener('keydown', onKey);

  sel$.addEventListener('change', () => { switchTo(sel$.value); });
  // switchTo opens another event, asking first when this one has unsaved points.
  async function switchTo(name) {
    if (!name || name === cur) return true;
    if (isDirty() && !(await confirmModal(t('eventmap.discard').replace('{name}', cur), { danger: true, okText: t('action.discard') }))) {
      rebuildSelect();
      return false;
    }
    if (!byName.has(name)) { byName.set(name, []); names.push(name); names.sort(); }
    cur = name;
    loadCur();
    rebuildSelect();
    return true;
  }

  const mapCard = h('div', { class: 'card' }, [
    h('div', { class: 'map-toolbar' }, [
      h('label', { i18n: 'eventmap.event', for: assocId(sel$) }), sel$,
      h('label', { class: 'inline-switch' }, [addMode, h('span', { i18n: 'eventmap.addMode' })]),
      h('div', { class: 'grow' }), countLbl, readout,
    ]),
    infoLine,
    h('p', { class: 'hint', i18n: 'eventmap.help2' }),
    h('div', { class: 'map-wrap' }, [gm.svg, gm.controls]),
  ]);
  root.append(mapCard);
  mapCoordReadout(gm, readout);
  // Zoom and pan only resize the dots; the points themselves stay put.
  gm.onView = () => {
    const r = gm.ptR(5), hr = gm.px(14);
    circles.forEach((c, i) => c.setAttribute('r', i === sel ? r * 1.5 : r));
    hits.forEach(c => c.setAttribute('r', hr));
  };

  // ---- point table (manual X / Y / Z / angle editing)
  const tableHost = h('div', { class: 'ev-table-wrap' });
  const pointsCard = h('div', { class: 'card' }, [
    h('h3', { i18n: 'eventmap.points.title' }),
    h('p', { class: 'hint', i18n: 'eventmap.points.hint' }),
    tableHost,
  ]);

  // ---- bulk tools: paste a coordinate list, set height for all
  const pasteArea = h('textarea', { class: 'coord-paste', placeholder: '7500.12 245.3 3200.40\n<7512.5, 247.1, 3190.2>\n<pos x="7486.2" z="3211.9" a="90" y="244.8"/>\n7466.40 / 3222.10', 'aria-label': t('eventmap.paste.title') });
  const orderSel = h('select', { 'aria-label': t('eventmap.paste.order') }, [
    h('option', { value: 'xyz', i18n: 'eventmap.paste.xyz' }),
    h('option', { value: 'xzy', i18n: 'eventmap.paste.xzy' }),
  ]);
  const keepY = h('input', { type: 'checkbox', class: 'switch' });
  keepY.checked = true;
  const pasteInfo = h('div', { class: 'hint' });
  const updatePasteInfo = () => {
    const r = parseCoordList(pasteArea.value, orderSel.value);
    const off = r.points.filter(p => p.x < 0 || p.z < 0 || p.x > size || p.z > size).length;
    pasteInfo.textContent = pasteArea.value.trim()
      ? tn('eventmap.paste.parsed', { n: r.points.length, bad: r.bad }) + (off ? ' · ' + t('eventmap.paste.off').replace('{n}', off) : '')
      : '';
  };
  pasteArea.addEventListener('input', updatePasteInfo);
  orderSel.addEventListener('change', updatePasteInfo);

  const oneX = h('input', { type: 'number', step: 'any', placeholder: 'X', 'aria-label': 'X' });
  const oneY = h('input', { type: 'number', step: 'any', placeholder: t('eventmap.col.y'), 'aria-label': t('eventmap.col.y') });
  const oneZ = h('input', { type: 'number', step: 'any', placeholder: 'Z', 'aria-label': 'Z' });
  const oneA = h('input', { type: 'number', step: 'any', placeholder: t('eventmap.col.a'), 'aria-label': t('eventmap.col.a') });
  const addOne = () => {
    const x = parseFloat(oneX.value), z = parseFloat(oneZ.value);
    if (!Number.isFinite(x) || !Number.isFinite(z)) { toast(t('eventmap.add.need'), 'warn'); return; }
    const p = { x: round2(x), z: round2(z), a: parseFloat(oneA.value) || 0 };
    if (oneY.value.trim() !== '' && Number.isFinite(parseFloat(oneY.value))) p.y = round2(parseFloat(oneY.value));
    pos.push(p);
    sel = pos.length - 1;
    markDirty(); render(); renderTable();
    [oneX, oneY, oneZ, oneA].forEach(i => { i.value = ''; });
    oneX.focus();
  };
  [oneX, oneY, oneZ, oneA].forEach(i => i.addEventListener('keydown', (e) => { if (e.key === 'Enter') addOne(); }));

  // Visible labels: the two inputs used to have only aria-labels.
  const heightInp = h('input', { type: 'number', step: 'any', placeholder: t('unit.m'), style: { width: '120px' } });
  const heightDelta = h('input', { type: 'number', step: 'any', placeholder: '± ' + t('unit.m'), style: { width: '120px' } });

  root.append(pointsCard, h('div', { class: 'grid-2 ev-tools' }, [
    h('div', { class: 'card' }, [
      h('h3', { i18n: 'eventmap.add.title' }),
      h('div', { class: 'ev-one' }, [oneX, oneY, oneZ, oneA, h('button', { class: 'primary', i18n: 'eventmap.add.btn', onclick: addOne })]),
      h('small', { class: 'hint', i18n: 'eventmap.add.hint' }),
      h('h3', { i18n: 'eventmap.paste.title', style: { marginTop: '18px' } }),
      pasteArea,
      h('div', { class: 'row', style: { gap: '10px', marginTop: '8px' } }, [
        h('label', { i18n: 'eventmap.paste.order', for: assocId(orderSel), style: { margin: 0 } }), orderSel,
        h('label', { class: 'inline-switch' }, [keepY, h('span', { i18n: 'eventmap.paste.keepY' })]),
      ]),
      pasteInfo,
      h('div', { class: 'actions' }, [
        h('button', { class: 'primary', i18n: 'eventmap.paste.add', onclick: () => {
          const r = parseCoordList(pasteArea.value, orderSel.value);
          if (!r.points.length) { toast(t('eventmap.paste.none'), 'warn'); return; }
          for (const p of r.points) {
            const q = { x: round2(p.x), z: round2(p.z), a: p.a || 0 };
            if (keepY.checked && Number.isFinite(p.y)) q.y = round2(p.y);
            pos.push(q);
          }
          markDirty(); render(); renderTable();
          toast(tn('eventmap.paste.added', { n: r.points.length }), 'ok');
          pasteArea.value = ''; updatePasteInfo();
        } }),
        h('button', { i18n: 'eventmap.paste.replace', onclick: async () => {
          const r = parseCoordList(pasteArea.value, orderSel.value);
          if (!r.points.length) { toast(t('eventmap.paste.none'), 'warn'); return; }
          if (!(await confirmModal(tn('eventmap.paste.replaceConfirm', { name: cur, n: r.points.length }), { danger: true }))) return;
          pos = r.points.map(p => {
            const q = { x: round2(p.x), z: round2(p.z), a: p.a || 0 };
            if (keepY.checked && Number.isFinite(p.y)) q.y = round2(p.y);
            return q;
          });
          sel = -1; markDirty(); render(); renderTable();
        } }),
        h('button', { class: 'secondary', i18n: 'eventmap.export', onclick: () => {
          const text = pos.map(p => `<pos x="${p.x}" z="${p.z}" a="${p.a || 0}"${p.y != null ? ` y="${p.y}"` : ''}/>`).join('\n');
          copyText(text);
        } }),
      ]),
    ]),
    h('div', { class: 'card' }, [
      h('h3', { i18n: 'eventmap.height.title' }),
      h('p', { class: 'hint', i18n: 'eventmap.height.hint' }),
      h('label', { class: 'ev-hlabel', i18n: 'eventmap.height.value', for: assocId(heightInp) }),
      h('div', { class: 'row', style: { gap: '8px' } }, [
        heightInp,
        h('button', { i18n: 'eventmap.height.setAll', onclick: () => {
          const v = parseFloat(heightInp.value);
          if (!Number.isFinite(v) || !pos.length) return;
          pos.forEach(p => { p.y = round2(v); });
          markDirty(); render(); renderTable();
        } }),
      ]),
      h('label', { class: 'ev-hlabel', i18n: 'eventmap.height.delta', for: assocId(heightDelta) }),
      h('div', { class: 'row', style: { gap: '8px' } }, [
        heightDelta,
        h('button', { i18n: 'eventmap.height.shift', onclick: () => {
          const v = parseFloat(heightDelta.value);
          if (!Number.isFinite(v) || !pos.length) return;
          let n = 0;
          pos.forEach(p => { if (p.y != null) { p.y = round2(p.y + v); n++; } });
          if (!n) { toast(t('eventmap.height.noneSet'), 'warn'); return; }
          markDirty(); renderTable();
        } }),
      ]),
      h('div', { class: 'actions' }, [
        h('button', { class: 'secondary', i18n: 'eventmap.height.clear', onclick: () => {
          if (!pos.some(p => p.y != null)) return;
          pos.forEach(p => { delete p.y; });
          markDirty(); render(); renderTable();
        } }),
      ]),
    ]),
  ]));

  // Validation panel: a chip opens that event on the map.
  const noPos = d.noPositions || [];
  const orphans = d.orphans || [];
  const chip = (cls, name) => h('button', { type: 'button', class: 'badge ' + cls + ' ev-chip', text: name, title: t('events.points.open'),
    onclick: async () => { if (await switchTo(name)) scrollToCard(mapCard); } });
  if (noPos.length || orphans.length) {
    const vc = h('div', { class: 'card' }, [h('h3', { i18n: 'eventmap.checks' })]);
    if (noPos.length) {
      vc.append(h('p', { class: 'hint', i18n: 'eventmap.noPositions' }),
        h('div', { class: 'chip-list' }, noPos.map(n => chip('warn', n))));
    }
    if (orphans.length) {
      vc.append(h('p', { class: 'hint', i18n: 'eventmap.orphans' }),
        h('div', { class: 'chip-list' }, orphans.map(n => chip('mute', n))));
    }
    root.append(vc);
  }
  root.append(bar);

  function loadCur() {
    pos = (byName.get(cur) || []).map(p => ({ ...p }));
    saved = last = JSON.stringify(pos);
    undo = [];
    sel = -1;
    render();
    renderTable();
    updateBar();
  }

  function select(i, scroll) {
    sel = i;
    render();
    tableHost.querySelectorAll('tr.sel').forEach(tr => tr.classList.remove('sel'));
    const tr = i >= 0 ? tableHost.querySelector(`tr[data-i="${i}"]`) : null;
    if (!tr) return;
    tr.classList.add('sel');
    if (scroll) scrollBoxTo(tableHost, tr);
  }

  function render() {
    gm.layer.innerHTML = '';
    circles.length = 0;
    hits.length = 0;
    countLbl.textContent = tn('eventmap.points', { n: pos.length });
    const r = gm.ptR(5);
    pos.forEach((p, i) => {
      const [sx, sy] = gm.w2s(p.x, p.z);
      const g = svgEl('g', { class: 'ev-pt-g' }, gm.layer);
      // Touch: a finger-sized invisible target around the small dot.
      if (coarse) hits.push(svgEl('circle', { cx: sx, cy: sy, r: gm.px(14), class: 'map-hit' }, g));
      circles[i] = svgEl('circle', { cx: sx, cy: sy, r: i === sel ? r * 1.5 : r, class: 'ev-pt' + (i === sel ? ' sel' : '') + (p.y != null ? ' has-y' : '') }, g);
      mapPointDrag(gm, g, {
        onMove: (wx, wz) => {
          p.x = round2(wx);
          p.z = round2(wz);
          const [nx, ny] = gm.w2s(p.x, p.z);
          for (const c of g.children) { c.setAttribute('cx', nx); c.setAttribute('cy', ny); }
          readout.textContent = 'X ' + Math.round(wx) + ' · Z ' + Math.round(wz);
        },
        onEnd: (moved) => {
          if (moved) { markDirty(); renderTable(); }
          // After the click this press makes, so that click still lands on
          // the point (and adds nothing to the map).
          setTimeout(() => select(i, true), 0);
        },
        onDelete: () => removePoint(i),
      });
    });
    renderInfo();
  }

  // renderInfo: what events.xml says about the event, and why its points may
  // not be used — not a "fixed" event, switched off, or fewer than its nominal.
  function renderInfo() {
    infoLine.innerHTML = '';
    if (!cur) return;
    const m = info[cur];
    const items = [];
    if (!m) {
      if (anyDefined) items.push(h('span', { class: 'badge warn', i18n: 'eventmap.warn.undefined' }));
    } else {
      if (m.mode && m.mode !== 'points') items.push(h('span', { class: 'badge warn', i18n: 'events.pos.' + m.mode }));
      if (!m.active) items.push(h('span', { class: 'badge warn', i18n: 'eventmap.warn.inactive' }));
      if (m.nominal != null) items.push(h('span', { class: 'hint', text: t('events.col.nominal') + ': ' + m.nominal }));
      if (m.mode === 'points' && m.active && (m.nominal == null || m.nominal > 0)) {
        if (!pos.length) items.push(h('span', { class: 'badge warn', i18n: 'eventmap.warn.noPoints' }));
        else if (m.nominal > pos.length) items.push(h('span', { class: 'badge warn', text: tn('eventmap.warn.fewPoints', { p: pos.length, n: m.nominal }) }));
      }
      items.push(h('button', { type: 'button', class: 'link-btn', i18n: 'eventmap.openEvents',
        onclick: () => { window._eventsTarget = cur; navigate('events'); } }));
    }
    infoLine.append(...items);
  }

  function removePoint(i) {
    pos.splice(i, 1);
    if (sel === i) sel = -1; else if (sel > i) sel--;
    markDirty(); render(); renderTable();
  }

  function renderTable() {
    tableHost.innerHTML = '';
    if (!pos.length) {
      tableHost.append(h('p', { class: 'hint', i18n: 'eventmap.points.empty' }));
      applyI18n();
      return;
    }
    const tbl = h('table', { class: 'ev-table' });
    tbl.append(h('thead', {}, h('tr', {}, [
      h('th', { text: '#' }), h('th', { class: 'num', text: 'X' }), h('th', { class: 'num', i18n: 'eventmap.col.y' }),
      h('th', { class: 'num', text: 'Z' }), h('th', { class: 'num', i18n: 'eventmap.col.a' }), h('th', { text: '' }),
    ])));
    const tb = h('tbody');
    const shown = (v) => (v == null ? '' : String(round2(v)));
    pos.forEach((p, i) => {
      const cell = (field, label, allowEmpty) => {
        // Two decimals on screen; the file's full value stays until edited.
        const inp = h('input', { type: 'number', step: 'any', class: 'inline-cell', value: shown(p[field]), 'aria-label': `#${i + 1} ${label}` });
        if (field === 'y') inp.placeholder = t('eventmap.ground');
        inp.addEventListener('focus', () => select(i, false));
        inp.addEventListener('change', () => {
          const v = inp.value.trim();
          if (v === '' && allowEmpty) delete p[field];
          else {
            const n = parseFloat(v);
            if (!Number.isFinite(n)) { inp.value = shown(p[field]); return; }
            p[field] = round2(n);
          }
          markDirty(); render();
          select(i, false);
        });
        return h('td', { class: 'num ev-c-' + field, 'data-label': label }, inp);
      };
      const off = p.x < 0 || p.z < 0 || p.x > size || p.z > size;
      const tr = h('tr', { 'data-i': String(i), class: (i === sel ? 'sel' : '') + (off ? ' off-map' : '') }, [
        h('td', { class: 'hint ev-c-n', text: String(i + 1), title: off ? t('eventmap.offMap') : '' }),
        cell('x', 'X'), cell('y', t('eventmap.col.y'), true), cell('z', 'Z'), cell('a', t('eventmap.col.a')),
        h('td', { class: 'ev-c-act' }, h('div', { class: 'row', style: { gap: '4px', flexWrap: 'nowrap', justifyContent: 'flex-end' } }, [
          h('button', { type: 'button', class: 'secondary icon-only', title: t('eventmap.zoomTo'), 'aria-label': t('eventmap.zoomTo'), html: svgIcon('i-search', 14),
            onclick: () => {
              select(i, false);
              gm.focus(p.x, p.z);
              const r = gm.svg.getBoundingClientRect();
              if (r.bottom < 80 || r.top > window.innerHeight - 80) scrollToCard(mapCard);
            } }),
          h('button', { type: 'button', class: 'danger icon-only', title: t('action.delete'), 'aria-label': t('action.delete'), text: '×', onclick: () => removePoint(i) }),
        ])),
      ]);
      tb.append(tr);
    });
    tbl.append(tb);
    tableHost.append(tbl);
  }

  // Click on empty map: add a point (add mode) or clear the selection. A drag
  // that panned the map, or a point's own press, never gets here (see
  // buildGameMap's swallowClick and mapPointDrag).
  gm.svg.addEventListener('click', (e) => {
    if (e.target && e.target.closest && e.target.closest('.ev-pt-g')) return;
    if (!cur) return;
    if (!addMode.checked) { select(-1); return; }
    const [wx, wz] = clientToWorld(gm, e);
    pos.push({ x: round2(wx), z: round2(wz), a: 0 });
    markDirty();
    renderTable();
    select(pos.length - 1, true);
  });

  async function save(btn) {
    if (!cur) return;
    await withBusy(btn, async () => {
      try {
        const r = await api.post('/api/eventspawns/save', { name: cur, pos });
        byName.set(cur, pos.map(p => ({ ...p })));
        saved = JSON.stringify(pos);
        rebuildSelect();
        updateBar();
        toast(tn('eventmap.saved', { n: r.count || 0 }), 'ok');
      } catch (e) { handleErr(e); }
    });
  }

  // Load the working copy first: the select labels the current event with
  // pos.length, which read "(0)" when built before it.
  loadCur();
  rebuildSelect();
  applyI18n();
};

// ------------------------------------------------------------------ vehicles

// fmtLifetime turns a CE lifetime (seconds) into "4 h" / "3 d".
function fmtLifetime(sec) {
  sec = Number(sec) || 0;
  if (sec <= 0) return '—';
  if (sec >= 86400) return (Math.round(sec / 8640) / 10) + ' ' + t('unit.days');
  if (sec >= 3600) return (Math.round(sec / 360) / 10) + ' ' + t('unit.hours');
  if (sec < 120) return sec + ' ' + t('unit.seconds');
  return Math.round(sec / 60) + ' ' + t('unit.minutes');
}

// vehicleDisplayName is the name players know a vehicle by: the vanilla
// models by their in-game names, a mod's car by its event name made readable
// (VehicleMyTruck_01 → My Truck 01).
const VANILLA_VEHICLE_NAMES = {
  VehicleBoat: '', // translated: vehicles.dn.boat
  VehicleCivilianSedan: 'Olga 24',
  VehicleHatchback02: 'Gunter 2',
  VehicleOffroad02: 'M1025',
  VehicleOffroadHatchback: 'Ada 4x4',
  VehicleSedan02: 'Sarka 120',
  VehicleTruck01: 'M3S',
};
function vehicleDisplayName(event) {
  const k = String(event || '').toLowerCase();
  for (const [name, dn] of Object.entries(VANILLA_VEHICLE_NAMES)) {
    if (name.toLowerCase() === k) return dn || t('vehicles.dn.boat');
  }
  const base = String(event || '').replace(/^Vehicle/i, '') || String(event || '');
  return base.replace(/_/g, ' ').replace(/([a-z])([A-Z0-9])/g, '$1 $2').replace(/\s+/g, ' ').trim();
}

// Vehicles — laid out like the player loadout: a tab per vehicle event and
// the chosen vehicle under them. Its parts are drawn on the car — each colour
// variant's cfgspawnabletypes entry, edited in place (spawnedit.js) — beside
// how many exist, how long they live, how full the tank is and where they
// spawn. DayZ has no vanilla "spawn a car here" command; the Central Economy
// spawns up to `nominal` vehicles at the event's spawn points.
Views.vehicles = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.vehicles', 'vehicles.subtitle', [
    h('button', { type: 'button', class: 'secondary', onclick: () => navigate('eventmap') }, [h('span', { html: svgIcon('i-eventmap', 15) }), h('span', { text: ' ' + t('nav.eventmap') })]),
    revealBtn({ kind: 'missionfile', path: 'db/events.xml' }),
  ]));
  const running = !!State.serverStatus.running;
  if (running) root.append(runningBanner());
  let d;
  try { d = await api.get('/api/vehicles'); }
  catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;

  // Vehicles that installed mods ship server files for but the mission does
  // not define yet — offered for a one-click import (loaded in the background).
  const modsHost = h('div');
  const list = d.vehicles || [];

  if (!list.length) {
    root.append(modsHost, h('div', { class: 'card' }, h('div', { class: 'empty-state' }, [
      h('div', { class: 'es-icon', html: svgIcon('i-vehicles', 34) }),
      h('div', { class: 'es-hint', i18n: 'vehicles.none' }),
    ])));
    vehiclesFromMods(modsHost, myNav, running, list);
    return;
  }

  // Problems a vehicle can show — counted for the summary.
  const problemsOf = (v) => {
    const out = [];
    const nominal = Number(v.nominal) || 0;
    if (v.modActive === false) out.push('modOff');
    else if ((v.active ?? 1) === 0) out.push('inactive');
    if (v.positions === 0) out.push('noPositions');
    else if (v.positions < nominal) out.push('fewPositions');
    if ((v.children || []).some(c => !c.known)) out.push('unknownParts');
    if ((v.children || []).length && (v.children || []).every(c => (Number(c.max) || 0) === 0 && (Number(c.min) || 0) === 0)) out.push('noVariants');
    return out;
  };
  const isMod = (v) => !!v.mod || (v.file && v.file !== 'db/events.xml');
  const total = list.length;
  const onMap = list.filter(v => (v.active ?? 1) !== 0 && v.modActive !== false).reduce((s, v) => s + (Number(v.nominal) || 0), 0);
  const spots = list.reduce((s, v) => s + (Number(v.positions) || 0), 0);
  const withProblems = list.filter(v => problemsOf(v).length).length;
  const modCount = list.filter(isMod).length;

  let filter = 'all';
  try { filter = localStorage.getItem('vehiclesFilter') || 'all'; } catch {}
  if ((filter === 'mods' && !modCount) || (filter === 'problems' && !withProblems)) filter = 'all';
  const tile = (key, value, kind, onClick) => h(onClick ? 'button' : 'div', {
    type: onClick ? 'button' : null, class: 'veh-tile' + (kind ? ' ' + kind : '') + (onClick ? ' clickable' : ''), onclick: onClick || null }, [
    h('div', { class: 'veh-tile-v', text: String(value) }), h('div', { class: 'veh-tile-k', i18n: key }),
  ]);
  const tabs = h('div', { class: 'section-tabs veh-tabs', role: 'tablist' });
  // A tab per vehicle; the chosen one opens below. Its editor is kept while
  // another is open, so switching keeps unsaved edits.
  const strip = h('div', { class: 'veh-strip', role: 'tablist', 'aria-label': t('nav.vehicles') });
  const host = h('div', { class: 'veh-host' });
  const editors = new Map();
  let current = null;
  try { current = localStorage.getItem('vehiclesPick'); } catch {}
  const shows = (v, f) => f === 'all' || (f === 'vanilla' && !isMod(v)) || (f === 'mods' && isMod(v)) || (f === 'problems' && problemsOf(v).length > 0);
  const setFilter = (f) => {
    filter = f;
    try { localStorage.setItem('vehiclesFilter', f); } catch {}
    tabs.querySelectorAll('button').forEach(b => { const on = b.dataset.f === f; b.classList.toggle('active', on); b.setAttribute('aria-selected', String(on)); });
    let first = null, keep = false;
    for (const b of strip.querySelectorAll('.veh-pick')) {
      const on = shows(b._vehicle, f);
      b.hidden = !on;
      if (on && !first) first = b._vehicle.name;
      if (on && b._vehicle.name === current) keep = true;
    }
    select(keep ? current : first);
  };
  const tab = (f, label, n) => h('button', { type: 'button', role: 'tab', class: 'section-tab', 'data-f': f, onclick: () => setFilter(f) },
    [h('span', { text: label }), n != null ? h('span', { class: 'pill-count', text: String(n) }) : null].filter(Boolean));
  tabs.append(...[
    tab('all', t('events.cat.all'), total),
    tab('vanilla', t('vehicles.tab.vanilla'), total - modCount),
    modCount ? tab('mods', t('events.cat.mods'), modCount) : null,
    withProblems ? tab('problems', t('vehicles.tab.problems'), withProblems) : null,
  ].filter(Boolean));

  root.append(h('div', { class: 'veh-summary' }, [
    tile('vehicles.sum.kinds', total),
    tile('vehicles.sum.onMap', onMap, 'accent'),
    tile('vehicles.sum.spots', spots),
    tile('vehicles.sum.problems', withProblems, withProblems ? 'warn' : 'ok', withProblems ? () => setFilter('problems') : null),
  ]), h('p', { class: 'hint veh-intro', i18n: 'vehicles.intro' }),
  // Once for the page, not in every card: fuel and radiator need nothing installed.
  h('p', { class: 'hint veh-intro veh-tweaks-note' }, [h('span', { html: svgIcon('i-fuel', 13) }), h('span', { i18n: 'veh.cond.hint' })]), modsHost, tabs);
  vehiclesFromMods(modsHost, myNav, running, list);

  // Unsaved vehicles: counted for the sticky bar, which saves them all.
  const dirtyCards = new Set();
  root._isDirty = () => dirtyCards.size > 0;
  const barText = h('span');
  const bar = h('div', { class: 'sticky-save', hidden: true }, [
    barText, h('span', { class: 'grow' }),
    h('button', { type: 'button', class: 'secondary small', i18n: 'action.discard', onclick: () => { dirtyCards.clear(); navigate('vehicles'); } }),
    h('button', { type: 'button', class: 'primary small', i18n: 'vehicles.saveAll', disabled: running, onclick: (e) => withBusy(e.currentTarget, async () => {
      for (const card of [...dirtyCards]) { if (!(await card._save())) break; }
    }) }),
  ]);
  const updateBar = () => {
    bar.hidden = dirtyCards.size === 0;
    barText.textContent = t('vehicles.unsaved').replace('{n}', dirtyCards.size);
    for (const b of strip.querySelectorAll('.veh-pick')) b.classList.toggle('dirty', dirtyCards.has(editors.get(b._vehicle.name)));
  };

  for (const v of list) strip.append(pickTab(v));
  root.append(strip, host, bar);
  setFilter(filter);
  applyI18n();

  function pickTab(v) {
    const kind = vehicleKind(v);
    const kids = v.children || [];
    const shown = kids.find(c => (Number(c.max) || 0) > 0) || kids[0];
    const off = v.modActive === false || (v.active ?? 1) === 0;
    const b = h('button', { type: 'button', role: 'tab', class: 'veh-pick' + (off ? ' off' : ''), title: v.name, onclick: () => select(v.name) }, [
      h('span', { class: 'veh-pick-art', html: vehicleArtSVG(kind, variantColor(shown ? shown.type : '', kind)) }),
      h('span', { class: 'veh-pick-txt' }, [
        h('b', { text: vehicleDisplayName(v.name) }),
        h('small', { text: off ? t('veh.pick.off') : t('veh.pick.count').replace('{n}', Number(v.nominal) || 0) }),
      ]),
      problemsOf(v).length ? h('span', { class: 'veh-pick-warn', title: t('vehicles.tab.problems') }) : null,
    ]);
    b._vehicle = v;
    return b;
  }

  function select(name) {
    const v = list.find(x => x.name === name);
    for (const b of strip.querySelectorAll('.veh-pick')) {
      const on = !!v && b._vehicle === v;
      b.classList.toggle('on', on);
      b.setAttribute('aria-selected', String(on));
    }
    if (!v) {
      host.replaceChildren(h('div', { class: 'card' }, h('p', { class: 'hint', i18n: 'veh.pick.none' })));
      applyI18n();
      return;
    }
    current = v.name;
    try { localStorage.setItem('vehiclesPick', v.name); } catch {}
    let ed = editors.get(v.name);
    if (!ed) { ed = vehicleEditor(v); editors.set(v.name, ed); }
    host.replaceChildren(ed);
    applyI18n();
  }

  function vehicleEditor(v) {
    let card;
    const setDirty = () => { dirtyCards.add(card); saveBtn.classList.add('pulse'); updateBar(); };
    const clearDirty = () => { dirtyCards.delete(card); saveBtn.classList.remove('pulse'); updateBar(); };
    const modOff = v.modActive === false;
    const num = (key, val, attrs = {}) => {
      const inp = h('input', { type: 'number', value: val ?? '', ...attrs, disabled: running, 'aria-label': t('vehicles.f.' + key) });
      inp.addEventListener('input', setDirty);
      return inp;
    };
    const F = {
      nominal: num('nominal', v.nominal, { min: '0' }),
      min: num('min', v.min, { min: '0' }),
      max: num('max', v.max, { min: '0' }),
      lifetime: num('lifetime', v.lifetime, { min: '0' }),
      restock: num('restock', v.restock, { min: '0' }),
    };
    // A switched-off mod's vehicles stay off: CE would try to spawn classes
    // that are not loaded.
    const active = h('input', { type: 'checkbox', class: 'switch', disabled: running || modOff });
    active.checked = !modOff && (v.active ?? 1) !== 0;
    active.addEventListener('change', setDirty);
    const lifeHint = h('small', { class: 'hint', text: fmtLifetime(v.lifetime) });
    F.lifetime.addEventListener('input', () => { lifeHint.textContent = fmtLifetime(F.lifetime.value); });

    const kids = (v.children || []).map(c => ({ ...c }));
    const kind = vehicleKind(v);
    const shown = kids.find(c => (Number(c.max) || 0) > 0) || kids[0];
    const art = h('div', { class: 'veh-panel-art', html: vehicleArtSVG(kind, variantColor(shown ? shown.type : '', kind)) });

    // The colours: how many of each, and a click opens its parts above.
    const kidRows = h('tbody');
    const partBadges = [];
    kids.forEach(c => {
      const cell = (field, cls) => {
        const inp = h('input', { type: 'number', class: 'inline-cell', value: c[field] ?? 0, disabled: running, 'aria-label': c.type + ' ' + field });
        inp.addEventListener('input', () => { c[field] = Number(inp.value) || 0; setDirty(); tr.classList.toggle('never', !c.max && !c.min); });
        return h('td', { class: 'num' + (cls ? ' ' + cls : '') }, inp);
      };
      const badge = h('span', { class: 'badge' });
      partBadges.push({ el: badge, c });
      const tr = h('tr', { class: (!c.max && !c.min) ? 'never' : '' }, [
        h('td', { class: 'mono veh-variant' }, h('button', { type: 'button', class: 'veh-variant-btn', title: t('veh.shop.show'),
          onclick: () => { shop.select(c.type); shop.el.scrollIntoView({ behavior: 'smooth', block: 'start' }); } }, [
          h('span', { class: 'veh-dot', style: { background: variantColor(c.type, kind) } }),
          ...breakAtUnderscores(c.type), h('span', { class: 'badge mute veh-never', i18n: 'vehicles.never' })])),
        cell('min'), cell('max'), cell('lootmin', 'veh-loot'), cell('lootmax', 'veh-loot'),
        h('td', { class: 'num veh-parts-cell' }, badge),
      ]);
      tr._type = c.type;
      kidRows.append(tr);
    });

    // The parts: a tab per colour, the chosen one drawn on the car.
    const shop = vehicleWorkshop(v, kids, kind, {
      root, setDirty,
      onVariant: (type) => {
        art.innerHTML = vehicleArtSVG(kind, variantColor(type, kind));
        for (const r of kidRows.children) r.classList.toggle('on', r._type === type);
      },
      onParts: () => updateBadges(),
    });
    // The parts column counts what the editor holds once it has loaded,
    // what the server said until then.
    function updateBadges() {
      for (const { el, c } of partBadges) {
        const m = shop.model(c.type);
        const known = m ? m.known || m.groups.length > 0 : c.known;
        const full = m ? seComplete(m.groups) : c.complete;
        el.className = 'badge ' + (!known ? 'warn' : full ? 'ok' : 'mute');
        el.textContent = !known ? t('vehicles.parts.unknown') : full ? t('vehicles.parts.complete') : String(m ? m.groups.length : c.parts);
      }
    }
    updateBadges();

    // Spawn condition: how full the tank and the radiator are. Vanilla fills
    // a random 0–35 % of the tank in script; the panel's server-side mod
    // applies these (app.SyncServerTweaks).
    const isCar = !['boat', 'moto'].includes(kind);
    const fluidSel = (cur, opts, labelKey) => {
      const sel = h('select', { class: 'veh-fluid-sel', disabled: running, 'aria-label': t(labelKey) },
        opts.map(([id]) => h('option', { value: id, text: t('veh.fluid.' + (id === 'default' && labelKey === 'veh.fuel' ? 'defaultFuel' : id)) })));
      const minIn = h('input', { type: 'number', min: '0', max: '100', step: '5', class: 'veh-fluid-num', disabled: running, 'aria-label': 'min %' });
      const maxIn = h('input', { type: 'number', min: '0', max: '100', step: '5', class: 'veh-fluid-num', disabled: running, 'aria-label': 'max %' });
      const custom = h('span', { class: 'veh-fluid-custom' }, [minIn, h('span', { text: '–' }), maxIn, h('span', { text: '%' })]);
      let id = 'default';
      if (cur) {
        id = 'custom';
        for (const [oid, r] of opts) if (Array.isArray(r) && Math.abs(r[0] - cur.min) < 0.001 && Math.abs(r[1] - cur.max) < 0.001) id = oid;
        minIn.value = String(Math.round(cur.min * 100)); maxIn.value = String(Math.round(cur.max * 100));
      } else { minIn.value = '0'; maxIn.value = '100'; }
      sel.value = id;
      custom.hidden = id !== 'custom';
      const onAny = () => { custom.hidden = sel.value !== 'custom'; setDirty(); };
      sel.addEventListener('change', onAny);
      minIn.addEventListener('input', setDirty); maxIn.addEventListener('input', setDirty);
      return {
        el: h('span', { class: 'veh-fluid' }, [sel, custom]),
        get() {
          if (sel.value === 'default') return null;
          if (sel.value === 'custom') {
            const lo = Math.max(0, Math.min(100, Number(minIn.value) || 0)) / 100, hi = Math.max(0, Math.min(100, Number(maxIn.value) || 0)) / 100;
            return { min: Math.min(lo, hi), max: Math.max(lo, hi) };
          }
          const r = opts.find(o => o[0] === sel.value)[1];
          return { min: r[0], max: r[1] };
        },
      };
    };
    const FUEL = [['default'], ['empty', [0, 0]], ['quarter', [0.25, 0.25]], ['half', [0.5, 0.5]], ['full', [1, 1]], ['random', [0, 1]], ['custom']];
    const COOL = [['default'], ['empty', [0, 0]], ['half', [0.5, 0.5]], ['full', [1, 1]], ['custom']];
    const fuelCtl = fluidSel(v.fuel, FUEL, 'veh.fuel');
    const coolCtl = isCar ? fluidSel(v.coolant, COOL, 'veh.coolant') : null;
    const tweakKey = () => JSON.stringify([fuelCtl.get(), coolCtl ? coolCtl.get() : null]);
    let tweakSaved = tweakKey();

    const warns = [];
    const nominal = Number(v.nominal) || 0;
    if (modOff) warns.push(h('div', { class: 'badge warn', text: t('vehicles.warn.modOff').replace('{mod}', v.mod) }));
    else if (!active.checked) warns.push(h('div', { class: 'badge warn', i18n: 'vehicles.warn.inactive' }));
    if (v.positions === 0) warns.push(h('div', { class: 'badge err', i18n: 'vehicles.warn.noPositions' }));
    else if (v.positions < nominal) warns.push(h('div', { class: 'badge warn', text: tn('vehicles.warn.fewPositions', { p: v.positions, n: nominal }) }));

    async function save() {
      const body = { Name: v.name, Active: active.checked ? 1 : 0 };
      for (const [k, el] of Object.entries(F)) {
        const val = el.value.trim();
        if (val !== '') body[k.charAt(0).toUpperCase() + k.slice(1)] = Number(val);
      }
      if (kids.length) body.Children = { Child: kids.map(c => ({ Type: c.type, Min: c.min, Max: c.max, LootMin: c.lootmin, LootMax: c.lootmax })) };
      try {
        // Modded vehicles live in their own registered events file.
        await api.post(`/api/events/item?name=${encodeURIComponent(v.name)}` +
          (v.file && v.file !== 'db/events.xml' ? `&file=${encodeURIComponent(v.file)}` : ''), body);
        if (tweakKey() !== tweakSaved) {
          const r = await api.post('/api/vehicles/tweak', { event: v.name, classes: kids.map(c => c.type),
            fuel: fuelCtl.get(), coolant: coolCtl ? coolCtl.get() : null });
          tweakSaved = tweakKey();
          if (!r.applied) toast(t('veh.cond.nextStart'), 'ok');
        }
        await shop.save();
        clearDirty();
        updateBadges();
        toast(t('msg.saved') + ' · ' + vehicleDisplayName(v.name), 'ok');
        return true;
      } catch (err) { handleErr(err); return false; }
    }
    const saveBtn = h('button', { class: 'primary', i18n: 'action.save', disabled: running, onclick: (e) => withBusy(e.currentTarget, save) });
    // A modded vehicle can be deleted outright (switching it off keeps its
    // settings); the game's own ones only switched off.
    const delBtn = isMod(v) ? h('button', { class: 'danger', i18n: 'vehicles.delete', disabled: running,
      onclick: async (e) => {
        const btn = e.currentTarget;
        if (!(await confirmModal(t('vehicles.delete.confirm').replace('{name}', vehicleDisplayName(v.name)), { danger: true, okText: t('vehicles.delete') }))) return;
        await withBusy(btn, async () => {
          try {
            await api.post('/api/vehicles/delete', { event: v.name });
            dirtyCards.delete(card);
            toast(t('vehicles.delete.done').replace('{name}', vehicleDisplayName(v.name)), 'ok');
            await navigate('vehicles');
          } catch (err) { handleErr(err); }
        });
      } }) : null;

    const field = (key, el, extra) => h('div', { class: 'veh-field' }, [withHelp(h('label', { i18n: 'vehicles.f.' + key, for: assocId(el) }), 'vehicles.' + key), el, extra || null]);
    const panel = h('aside', { class: 'card veh-panel' + (modOff ? ' mod-off' : '') }, [
      art,
      h('div', { class: 'veh-title' }, [h('h3', { text: vehicleDisplayName(v.name) }), h('span', { class: 'hint mono', text: v.name })]),
      h('div', { class: 'veh-badges' }, [
        v.mod ? h('span', { class: 'badge ' + (modOff ? 'warn' : 'info') + ' mono', text: v.mod, title: t('vehicles.fromMod').replace('{mod}', v.mod) }) : null,
        v.file && v.file !== 'db/events.xml'
          ? h('span', { class: 'badge mute mono veh-file', text: v.file, title: t('vehicles.definedIn').replace('{file}', v.file) }) : null,
      ].filter(Boolean)),
      h('button', { type: 'button', class: 'secondary small veh-pos-btn', onclick: () => { window._eventmapTarget = v.name; navigate('eventmap'); } }, [
        h('span', { html: svgIcon('i-eventmap', 14) }),
        h('span', { text: ' ' + tn('vehicles.positions', { n: v.positions }) }),
      ]),
      warns.length ? h('div', { class: 'veh-warns' }, warns) : null,
      h('div', { class: 'veh-fields' }, [
        field('nominal', F.nominal), field('min', F.min), field('max', F.max),
        field('lifetime', F.lifetime, lifeHint), field('restock', F.restock),
        h('div', { class: 'veh-field' }, [h('label', { i18n: 'vehicles.f.active' }), h('label', { class: 'veh-active' }, [active, h('span', { i18n: 'vehicles.activeOn' })])]),
      ]),
      h('div', { class: 'veh-cond' }, [
        h('div', { class: 'veh-cond-row' }, [
          h('span', { class: 'veh-cond-k' }, [h('span', { html: svgIcon('i-fuel', 14) }), h('span', { i18n: 'veh.fuel' }), help('veh.tweaks')]), fuelCtl.el,
        ]),
        coolCtl ? h('div', { class: 'veh-cond-row' }, [
          h('span', { class: 'veh-cond-k' }, [h('span', { html: svgIcon('i-drop', 14) }), h('span', { i18n: 'veh.coolant' })]), coolCtl.el,
        ]) : null,
      ]),
      h('div', { class: 'veh-foot' }, [h('span', { class: 'grow' }), delBtn, saveBtn].filter(Boolean)),
    ].filter(Boolean));
    const colours = kids.length ? h('section', { class: 'card veh-variants' }, [
      h('div', { class: 'veh-sec-head' }, [h('h3', { i18n: 'veh.colours' })]),
      h('p', { class: 'hint', i18n: 'veh.colours.hint' }),
      h('div', { class: 'table-scroll veh-kids-wrap' }, h('table', { class: 'veh-kids' }, [
        h('thead', {}, h('tr', {}, [
          h('th', { i18n: 'vehicles.col.variant' }), h('th', { class: 'num', i18n: 'vehicles.col.min' }), h('th', { class: 'num', i18n: 'vehicles.col.max' }),
          hlp(h('th', { class: 'num veh-loot', i18n: 'vehicles.col.lootmin' }), 'vehicles.loot'), h('th', { class: 'num veh-loot', i18n: 'vehicles.col.lootmax' }),
          h('th', { class: 'num', i18n: 'vehicles.col.parts' }),
        ])),
        kidRows,
      ])),
    ]) : null;
    card = h('div', { class: 'veh-editor' }, [h('div', { class: 'veh-main' }, [shop.el, colours].filter(Boolean)), panel]);
    card._vehicle = v;
    card._save = save;
    if (shown) shop.select(shown.type);
    else shop.empty();
    return card;
  }
};

// vehicleWorkshop is a vehicle's parts and trunk: a tab per colour variant
// and that variant's cfgspawnabletypes entry — the parts drawn on the car
// (seEditor), what is in the trunk listed like the loadout's carried things
// (seGroupList). Edits stay in memory per variant until save() writes the
// changed ones, each into the file that defines it (POST
// /api/vehicles/variants). The variants load on the first select(). ctx:
// root (the page), setDirty(), onVariant(type) when another colour is shown,
// onParts() after any change to the parts.
function vehicleWorkshop(v, kids, kind, ctx) {
  const models = new Map(); // lower-cased type → { type, file, known, groups, cargo, saved }
  const tabs = h('div', { class: 'veh-vars', role: 'tablist', 'aria-label': t('veh.colours') });
  const body = h('div', { class: 'veh-shop-body' });
  const el = h('section', { class: 'card veh-shop' }, [
    h('div', { class: 'veh-sec-head' }, [h('span', { class: 'veh-shop-ico', html: svgIcon('i-attach', 15) }), h('h3', { i18n: 'veh.shop' })]),
    h('p', { class: 'hint', i18n: 'veh.shop.hint' }),
    tabs, body,
  ]);
  let cur = null, cat = null, classLower = null, loading = null, failed = '';
  const model = (type) => models.get(String(type || '').toLowerCase()) || null;
  const snap = (m) => JSON.stringify([m.groups, m.cargo]);
  const changed = (m) => snap(m) !== m.saved;

  function load() {
    if (!loading) loading = (async () => {
      try {
        const [r, c, cl] = await Promise.all([
          api.get('/api/vehicles/variants?event=' + encodeURIComponent(v.name)), loCatalog(), seLists(ctx.root)]);
        cat = c;
        classLower = cl;
        for (const x of r.variants || []) {
          const groups = JSON.parse(JSON.stringify((x.entry && x.entry.attachments) || []));
          const cargo = JSON.parse(JSON.stringify((x.entry && x.entry.cargo) || []));
          // Edited in place by the group cards, which give an item-less
          // group its array: given here, so an untouched entry stays unchanged.
          for (const g of [...groups, ...cargo]) if (typeof g.preset !== 'string') g.items = g.items || [];
          const m = { type: x.type, file: x.file, known: !!x.entry, groups, cargo };
          m.saved = snap(m);
          models.set(x.type.toLowerCase(), m);
        }
      } catch (e) { failed = e.message || String(e); }
      ctx.onParts();
    })();
    return loading;
  }

  // The slot chance every colour's parts share, or null.
  function shared() {
    let c = null;
    for (const m of models.values()) {
      if (!m.groups.length) continue;
      const x = seChanceOf(m.groups);
      if (x === null || (c !== null && Math.abs(c - x) > 0.001)) return null;
      c = x;
    }
    return c;
  }

  function drawTabs() {
    tabs.replaceChildren(...kids.map(c => {
      const m = model(c.type);
      const on = c.type === cur;
      const never = !(Number(c.max) || 0) && !(Number(c.min) || 0);
      return h('button', { type: 'button', role: 'tab', class: 'veh-var' + (on ? ' on' : '') + (never ? ' never' : ''), 'aria-selected': String(on),
        title: never ? t('vehicles.never') : c.type, onclick: () => select(c.type) }, [
        h('span', { class: 'veh-dot', style: { background: variantColor(c.type, kind) } }),
        h('span', { class: 'veh-var-name', text: c.type }),
        m ? h('span', { class: 'veh-var-n' + (seComplete(m.groups) ? ' full' : ''), text: String(m.groups.length), title: t('vehicles.col.parts') }) : null,
        m && changed(m) ? h('span', { class: 'veh-var-dirty', text: '•' }) : null,
      ]);
    }));
  }

  function draw(m, det) {
    const se = seEditor(m.groups, {
      cls: m.type, det, cat, info: cat ? loItemInfo(cat, m.type) : null, classLower,
      collapse: m.groups.length > 4,
      edited: () => { ctx.setDirty(); drawTabs(); ctx.onParts(); },
      // The chance buttons set every colour of the vehicle at once.
      bulkLabel: 'veh.shop.bulk',
      bulkButtons: [[1, t('vehicles.parts.full')], [0.75, t('vehicles.parts.most')], [0.5, t('vehicles.parts.half')], [0.25, t('vehicles.parts.wreck')]],
      bulkOn: (x) => { const c = shared(); return c !== null && Math.abs(c - x) < 0.01; },
      bulk: (x) => {
        for (const o of models.values()) seSetChance(o.groups, x);
        ctx.setDirty();
        ctx.onParts();
        drawTabs();
        draw(m, det);
      },
    });
    // What is in the trunk: groups that each roll on their own, one thing
    // out of each by weight — like the loadout's carried things.
    const onTrunk = () => { ctx.setDirty(); drawTabs(); };
    const trunk = seGroupList(m.cargo, { kind: 'cargo', cat, classLower, edited: onTrunk, changed: onTrunk,
      addKey: 'veh.trunk.add', titleKey: 'se.cargoGroup', emptyKey: 'veh.trunk.empty' });
    body.replaceChildren(...[
      m.known ? null : h('p', { class: 'hint veh-shop-new', text: t('veh.shop.newEntry').replace('{file}', m.file) }),
      se.el,
      h('section', { class: 'veh-trunk' }, [
        h('div', { class: 'veh-sec-head' }, [h('span', { class: 'veh-shop-ico', html: svgIcon('i-c-box', 15) }), h('h3', { i18n: 'veh.trunk' })]),
        h('p', { class: 'hint', i18n: 'veh.trunk.hint' }),
        trunk.el,
      ]),
      h('p', { class: 'hint mono veh-shop-file', text: m.file }),
    ].filter(Boolean));
    applyI18n();
  }

  async function select(type) {
    cur = type;
    drawTabs();
    ctx.onVariant(type);
    if (!loading) body.replaceChildren(h('p', { class: 'hint li-loading', text: t('li.loading') }));
    await load();
    if (cur !== type) return;
    drawTabs();
    if (failed) { body.replaceChildren(h('p', { class: 'hint', text: failed })); return; }
    const m = model(type);
    if (!m) { body.replaceChildren(h('p', { class: 'hint', i18n: 'veh.shop.none' })); applyI18n(); return; }
    const det = await loDetail(type);
    if (cur === type) draw(m, det);
  }

  // save writes the colours whose parts changed. What the server drops — a
  // part without a name, a group left without parts — goes here too.
  async function save() {
    const list = [...models.values()].filter(changed);
    if (!list.length) return 0;
    await api.post('/api/vehicles/variants', { event: v.name, variants: list.map(m => ({ type: m.type, attachments: m.groups, cargo: m.cargo })) });
    for (const m of list) {
      for (const arr of [m.groups, m.cargo]) {
        for (let i = arr.length - 1; i >= 0; i--) {
          const g = arr[i];
          if (typeof g.preset === 'string') { if (!g.preset.trim()) arr.splice(i, 1); continue; }
          g.items = g.items.filter(it => String(it.name || '').trim());
          if (!g.items.length) arr.splice(i, 1);
        }
      }
      m.saved = snap(m);
      m.known = true;
    }
    if (cur && list.includes(model(cur))) select(cur);
    else drawTabs();
    return list.length;
  }

  return {
    el, select, save, model,
    empty: () => body.replaceChildren(h('p', { class: 'hint', i18n: 'veh.shop.none' })),
  };
}

// ------------------------------------------------------------ vehicle art
//
// Side-view drawings of the vehicle kinds (our own simple shapes, no game
// art), painted in the colour a variant's class name ends with.

const VEH_COLOR = {
  black: '#2a2e35', blue: '#2f5ea8', wine: '#6e1d2c', red: '#b23a32', white: '#dfe2e7', grey: '#8a9099', gray: '#8a9099',
  green: '#3e6b42', yellow: '#d8b23a', orange: '#e07a2e', beige: '#c7ae7f', sand: '#c7ae7f', tan: '#b89a66', camo: '#5f6b3c',
  rust: '#8a4b2a', brown: '#6b4a2f', silver: '#b8bec7', olive: '#5a6236', banana: '#e2c84a', police: '#2b3f6e',
};
const VEH_DEFAULT = { sedan: '#8a9099', hatch: '#2f5ea8', sarka: '#c7ae7f', suv: '#3e6b42', humvee: '#5f6b3c', truck: '#4c5a3a', boat: '#e07a2e', moto: '#b23a32', car: '#8a9099' };

// vehicleKind guesses the drawing from the event and its classes.
function vehicleKind(v) {
  const s = [v.name, ...(v.children || []).map(c => c.type)].join(' ').toLowerCase();
  if (/boat/.test(s)) return 'boat';
  if (/motorbike|motorcycle|bike|moto/.test(s)) return 'moto';
  if (/truck|v3s|m3s|ural|kamaz/.test(s)) return 'truck';
  if (/offroad_02|humvee|hmmwv|m1025/.test(s)) return 'humvee';
  if (/offroadhatchback|niva/.test(s)) return 'suv';
  if (/hatchback_02|gunter/.test(s)) return 'hatch';
  if (/sedan_02|sarka/.test(s)) return 'sarka';
  if (/sedan|olga/.test(s)) return 'sedan';
  return 'car';
}

// variantColor reads the colour a class name ends with ("_Black", "_Wine").
function variantColor(cls, kind) {
  const m = /_([a-z]+)$/i.exec(cls || '');
  return (m && VEH_COLOR[m[1].toLowerCase()]) || VEH_DEFAULT[kind] || '#8a9099';
}

// vehicleArtSVG draws one kind as an inline SVG (viewBox 240 × 110).
function vehicleArtSVG(kind, color) {
  const wheel = (x, y, r) => `<circle cx="${x}" cy="${y}" r="${r}" class="va-tyre"/><circle cx="${x}" cy="${y}" r="${(r * 0.45).toFixed(1)}" class="va-rim"/>`;
  const glass = 'class="va-glass"';
  const body = (d) => `<path d="${d}" fill="${color}" class="va-body"/>`;
  let s;
  switch (kind) {
    case 'boat':
      s = `<path d="M4 90 Q30 84 60 90 T120 90 T180 90 T236 90" class="va-water"/>
        <rect x="8" y="46" width="12" height="30" rx="3" class="va-dark"/>
        ${body('M16 56 L220 56 Q212 78 186 84 L44 84 Q24 80 16 56 Z')}
        <path d="M74 56 L88 38 L124 38 L130 56 Z" ${glass}/>
        <path d="M16 62 L214 62" class="va-line"/>`;
      break;
    case 'moto':
      s = `${wheel(58, 78, 22)}${wheel(178, 78, 22)}
        <path d="M58 78 L98 52 L150 52 L178 78" class="va-frame"/>
        ${body('M92 40 Q100 30 124 32 L150 40 L146 52 L98 52 Z')}
        <path d="M150 40 L166 26 L176 28" class="va-frame"/>
        <rect x="76" y="40" width="30" height="8" rx="4" class="va-dark"/>`;
      break;
    case 'truck':
      s = `${body('M14 78 L14 52 Q16 42 28 40 L44 40 L52 22 L84 22 Q90 22 90 30 L90 78 Z')}
        <path d="M56 28 L82 28 L82 44 L52 44 Z" ${glass}/>
        <path d="M94 30 Q160 18 230 30 L230 72 L94 72 Z" class="va-tarp"/>
        <path d="M118 26 V72 M146 22 V72 M174 22 V72 M202 24 V72" class="va-line"/>
        <rect x="14" y="76" width="218" height="6" class="va-dark"/>
        ${wheel(48, 86, 15)}${wheel(174, 86, 15)}${wheel(206, 86, 15)}`;
      break;
    case 'humvee':
      s = `<path d="M100 28 L94 6" class="va-line"/>
        ${body('M10 76 L10 54 Q10 48 18 48 L66 46 L74 28 L168 28 L174 46 L222 48 Q230 49 230 56 L230 76 Q230 80 224 80 L210 80 A22 22 0 0 0 166 80 L86 80 A22 22 0 0 0 42 80 L16 80 Q10 80 10 76 Z')}
        <path d="M80 33 L118 33 L118 46 L76 46 Z M124 33 L162 33 L168 46 L124 46 Z" ${glass}/>
        <path d="M121 48 V78 M176 50 V78" class="va-line"/>
        <rect x="224" y="56" width="10" height="16" rx="2" class="va-dark"/>
        ${wheel(64, 82, 20)}${wheel(188, 82, 20)}`;
      break;
    case 'suv':
      s = `${body('M18 74 L18 52 Q20 46 30 46 L60 44 L74 24 L160 24 Q168 24 170 32 L176 44 Q184 46 184 54 L184 74 Q184 80 178 80 L168 80 A19 19 0 0 0 130 80 L78 80 A19 19 0 0 0 40 80 L24 80 Q18 80 18 74 Z')}
        <path d="M80 30 L118 30 L118 44 L70 44 Z M124 30 L162 30 L168 44 L124 44 Z" ${glass}/>
        <path d="M121 46 V78" class="va-line"/>
        ${wheel(59, 82, 17)}${wheel(149, 82, 17)}`;
      break;
    case 'hatch':
      s = `${body('M16 72 L20 56 Q26 50 46 48 L76 46 L98 28 L158 28 Q168 30 174 40 L186 56 Q190 60 190 72 Q190 78 184 78 L174 78 A18 18 0 0 0 138 78 L86 78 A18 18 0 0 0 50 78 L22 78 Q16 78 16 72 Z')}
        <path d="M102 33 L130 33 L130 46 L84 46 Z M136 33 L160 33 Q166 36 170 46 L136 46 Z" ${glass}/>
        <path d="M133 48 V76" class="va-line"/>
        ${wheel(68, 80, 15)}${wheel(156, 80, 15)}`;
      break;
    case 'sarka':
      s = `${body('M16 72 L18 60 Q22 54 40 52 L70 50 L90 34 L150 34 L170 50 L204 52 Q216 54 220 62 L222 72 Q222 78 216 78 L196 78 A16 16 0 0 0 164 78 L80 78 A16 16 0 0 0 48 78 L22 78 Q16 78 16 72 Z')}
        <path d="M94 38 L118 38 L118 49 L80 49 Z M124 38 L148 38 L162 49 L124 49 Z" ${glass}/>
        <path d="M121 51 V76" class="va-line"/>
        ${wheel(64, 80, 14)}${wheel(180, 80, 14)}`;
      break;
    default: // sedan / generic car
      s = `${body('M10 72 L14 58 Q20 52 40 50 L70 48 L92 30 Q96 28 104 28 L150 28 Q158 28 162 32 L180 48 L210 50 Q226 52 230 60 L232 72 Q232 78 226 78 L200 78 A20 20 0 0 0 160 78 L84 78 A20 20 0 0 0 44 78 L16 78 Q10 78 10 72 Z')}
        <path d="M98 33 L126 33 L126 47 L82 47 Z M132 33 L154 33 L172 47 L132 47 Z" ${glass}/>
        <path d="M129 49 V76" class="va-line"/>
        ${wheel(64, 80, 16)}${wheel(180, 80, 16)}`;
  }
  return `<svg viewBox="0 0 240 110" class="va-svg" role="img" aria-hidden="true">${s}</svg>`;
}

// breakAtUnderscores lets a long class name wrap after an underscore
// (Hatchback_02_Blue) instead of in the middle of a word.
function breakAtUnderscores(name) {
  const parts = String(name).split('_');
  const out = [];
  parts.forEach((part, i) => {
    const last = i === parts.length - 1;
    out.push(document.createTextNode(last ? part : part + '_'));
    if (!last) out.push(document.createElement('wbr'));
  });
  return out;
}

// vehiclesFromMods fills host with the vehicles installed mods ship server
// files for (events, spawn points, parts, types) that the mission does not
// define yet, each mod with an "Add to the server" button.
async function vehiclesFromMods(host, myNav, running, missionVehicles) {
  let r;
  try { r = await api.get('/api/vehicles/mods'); } catch { return; }
  if (myNav !== _navSeq) return;
  const mods = (r.mods || []).map(m => ({
    mod: m.mod, active: m.active,
    files: (m.vehicles || []).filter(v => !v.inMission && !v.removed),
    // Deleted by the admin: not added automatically, but can come back.
    removed: (m.vehicles || []).filter(v => !v.inMission && v.removed),
    detected: (m.detected || []),
  })).filter(g => g.files.length || g.removed.length || g.detected.length);
  if (!mods.length) return;

  const card = h('div', { class: 'card veh-mods' }, [
    h('div', { class: 'card-head' }, [
      cardTitle('i-mods', 'vehicles.mods.title'),
      h('span', { class: 'badge ' + (r.auto ? 'ok' : 'mute'), i18n: r.auto ? 'vehicles.auto.on' : 'vehicles.auto.offBadge' }),
    ]),
    h('p', { class: 'hint', i18n: r.auto ? 'vehicles.mods.hint.auto' : 'vehicles.mods.hint' }),
  ]);
  if (running) card.append(h('p', { class: 'hint warn-text', i18n: 'vehicles.mods.stopFirst' }));

  for (const g of mods) {
    const group = h('div', { class: 'veh-mod-group' }, [
      h('div', { class: 'veh-mod-head' }, [
        h('h4', { class: 'sub-title mono', text: g.mod }),
        h('span', { class: 'badge ' + (g.active ? 'ok' : 'mute'), i18n: g.active ? 'vehicles.mod.on' : 'vehicles.mod.off' }),
      ]),
    ]);
    // Cars the mod ships server files for: one-click import (or automatic).
    if (g.files.length) {
      group.append(h('ul', { class: 'veh-mod-list' }, g.files.map(v => h('li', {}, [
        h('span', { class: 'mono', text: v.event }),
        (v.variants || []).length ? h('span', { class: 'hint', text: ' — ' + v.variants.join(', ') }) : null,
        v.positions ? null : h('span', { class: 'badge warn', i18n: 'vehicles.mods.noPositions' }),
      ].filter(Boolean)))));
      group.append(h('div', { class: 'actions' }, [h('button', { class: 'primary', i18n: 'vehicles.mods.add', disabled: running,
        onclick: (e) => withBusy(e.currentTarget, async () => {
          try {
            const res = await api.post('/api/vehicles/import', { mod: g.mod, events: g.files.map(v => v.event) });
            toast(t('vehicles.mods.added').replace('{events}', res.events).replace('{positions}', res.positions)
              .replace('{parts}', res.parts).replace('{types}', res.types), 'ok');
            await navigate('vehicles');
          } catch (err) { handleErr(err); }
        }) })]));
    }
    for (const v of g.removed) {
      group.append(h('div', { class: 'veh-removed' }, [
        h('span', { class: 'mono', text: v.event }),
        h('span', { class: 'badge mute', i18n: 'vehicles.mods.removed' }),
        h('span', { class: 'grow' }),
        h('button', { class: 'secondary small', i18n: 'vehicles.mods.addAgain', disabled: running,
          onclick: (e) => withBusy(e.currentTarget, async () => {
            try {
              await api.post('/api/vehicles/import', { mod: g.mod, events: [v.event] });
              toast(t('msg.saved'), 'ok');
              await navigate('vehicles');
            } catch (err) { handleErr(err); }
          }) }),
      ]));
    }
    // Cars only the mod's configs know about: set up spawning.
    for (const f of g.detected) {
      const row = h('div', { class: 'veh-detected' + (f.spawning ? ' done' : '') }, [
        h('div', { class: 'veh-detected-main' }, [
          h('div', {}, [
            h('b', { text: f.display || f.family }),
            f.display ? h('span', { class: 'hint mono', text: ' ' + f.family }) : null,
          ]),
          h('div', { class: 'hint', text: t('vehicles.detected.variants').replace('{list}', (f.variants || []).join(', ')) }),
        ]),
        f.spawning
          ? h('span', { class: 'badge ok', i18n: 'vehicles.detected.spawning' })
          : h('button', { class: 'primary small', i18n: 'vehicles.setup', disabled: running || !g.active,
            title: !g.active ? t('vehicles.setup.modOff') : '',
            onclick: () => vehicleSetupDialog(g.mod, f, missionVehicles) }),
      ]);
      group.append(row);
    }
    card.append(group);
  }
  host.append(card);
  applyI18n();
}

// vehicleSetupDialog sets up spawning for a car a mod defines without server
// files: how many on the map, which vanilla car to model it on (its settings
// and spawn spots), and whether to add parts matched to the car's slots.
function vehicleSetupDialog(mod, fam, missionVehicles) {
  const m = openModal({ title: t('vehicles.setup.title').replace('{name}', fam.display || fam.family) });
  const nominal = h('input', { type: 'number', min: '1', max: '50', value: '3', 'aria-label': t('vehicles.setup.count') });
  const models = (missionVehicles || []).filter(v => /^Vehicle/i.test(v.name) || (v.children || []).length);
  const model = h('select', { 'aria-label': t('vehicles.setup.model') }, models.map(v => h('option', { value: v.name, text: v.name + (v.positions != null ? ` (${v.positions})` : '') })));
  const prefer = models.find(v => /CivilianSedan/i.test(v.name)) || models[0];
  if (prefer) model.value = prefer.name;
  const parts = h('input', { type: 'checkbox', class: 'switch' });
  parts.checked = true;
  const ok = h('button', { class: 'primary', i18n: 'vehicles.setup.go', disabled: !models.length, onclick: () => withBusy(ok, async () => {
    try {
      const res = await api.post('/api/vehicles/setup', { mod, family: fam.family, nominal: Number(nominal.value) || 3, copyFrom: model.value, parts: parts.checked });
      toast(t('vehicles.setup.done').replace('{event}', res.event).replace('{spots}', res.spots).replace('{parts}', res.parts), 'ok');
      m.close();
      await navigate('vehicles');
    } catch (err) { handleErr(err); }
  }) });
  m.body.append(
    h('p', { class: 'hint', text: t('vehicles.detected.variants').replace('{list}', (fam.variants || []).join(', ')) }),
    h('div', { class: 'grid-2' }, [field('vehicles.setup.count', nominal), field('vehicles.setup.model', model, 'vehicles.setup.model.hint')]),
    switchRow(parts, 'vehicles.setup.parts'),
    h('small', { class: 'hint', i18n: 'vehicles.setup.parts.hint' }),
    ...(models.length ? [] : [h('p', { class: 'warning-bar', i18n: 'vehicles.setup.noModel' })]),
    h('p', { class: 'hint', i18n: 'vehicles.setup.after' }),
    h('div', { class: 'actions' }, [ok, h('button', { i18n: 'action.cancel', onclick: () => m.close() })]),
  );
  applyI18n();
}

// withBusyAll disables every button on the page while fn runs (bulk writes).
async function withBusyAll(fn) {
  const btns = [...document.querySelectorAll('#view button:not(:disabled)')];
  btns.forEach(b => { b.disabled = true; });
  try { await fn(); } finally { btns.forEach(b => { if (document.body.contains(b)) b.disabled = false; }); }
}

// ------------------------------------------------------------------ missions

// Missions (maps) — install a map's mission into mpmissions from its mod or a
// .zip, switch the active one, and remove old ones to a recoverable trash.
Views.missions = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.missions', 'missions.subtitle', [revealBtn({ kind: 'mpmissions' })]));
  const running = !!State.serverStatus.running;
  if (running) root.append(runningBanner());
  let full, avail;
  try {
    [full, avail] = await Promise.all([api.get('/api/missions/full'), api.get('/api/missions/available')]);
  } catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;

  // ---- installed missions: the same tiles as the Server page's map card,
  // plus open-folder and delete (a table hid its buttons off-screen on phones).
  const list = (full.missions || []).slice().sort((a, b) => (b.active - a.active) || a.name.localeCompare(b.name));
  const installed = h('div', { class: 'card' }, [
    cardTitle('i-missions', 'missions.installed'),
    h('p', { class: 'hint', i18n: 'missions.installed.hint' }),
  ]);
  if (!list.length) {
    installed.append(h('p', { class: 'warning-bar', i18n: 'missions.none' }));
  } else {
    installed.append(h('div', { class: 'map-grid' }, list.map(m => missionTile(m, { route: 'missions', extra: [
      revealBtn({ path: 'mpmissions/' + m.name }, { iconOnly: true }),
      m.active ? null : h('button', { type: 'button', class: 'danger icon-only', text: '×', title: t('action.delete'), 'aria-label': t('action.delete') + ' ' + m.name,
        disabled: running, onclick: () => deleteMission(m) }),
    ] }))));
  }
  root.append(installed);

  async function deleteMission(m) {
    if (!(await confirmModal(t('missions.delete.confirm').replace('{name}', m.name), { danger: true, okText: t('action.delete') }))) return;
    try {
      const r = await api.post('/api/missions/delete', { name: m.name });
      await navigate('missions');
      if (r && r.entry) toastAction(t('missions.deleted'), 'ok', t('missions.trash.undo'), () => restoreEntry(r.entry));
      else toast(t('missions.deleted'), 'ok');
    } catch (err) { handleErr(err); }
  }
  async function restoreEntry(entry) {
    try {
      const r = await api.post('/api/missions/restore', { entry });
      toast(t('missions.restored').replace('{name}', (r && r.name) || entry), 'ok');
      if (currentRoute() === 'missions') await navigate('missions');
    } catch (err) { handleErr(err); }
  }

  // ---- missions shipped inside mods
  const av = avail.missions || [];
  const availCard = h('div', { class: 'card' }, [cardTitle('i-mods', 'missions.fromMods')]);
  const importCard = h('div', { class: 'card' });
  if (!av.length) {
    availCard.append(h('div', { class: 'empty-state msn-empty' }, [
      h('div', { class: 'es-hint', i18n: 'missions.fromMods.none' }),
      h('div', { class: 'actions' }, [
        h('button', { type: 'button', class: 'secondary small', i18n: 'missions.import.jump', onclick: () => scrollToCard(importCard) }),
      ]),
    ]));
  } else {
    availCard.append(h('p', { class: 'hint', i18n: 'missions.fromMods.hint' }));
    const at = h('table');
    at.append(h('thead', {}, h('tr', {}, [
      h('th', { i18n: 'col.mod' }), h('th', { i18n: 'missions.col.name' }), h('th', { i18n: 'missions.col.world' }), h('th', { text: '' }),
    ])));
    const ab = h('tbody');
    for (const a of av) {
      const activate = h('input', { type: 'checkbox', class: 'switch' });
      activate.checked = !a.installed;
      ab.append(h('tr', {}, [
        h('td', { class: 'mono', text: a.mod }),
        h('td', {}, [h('div', { class: 'mono', text: a.name }), h('small', { class: 'hint', text: a.zip ? a.path + (a.inner ? ' → ' + a.inner : '') : a.path })]),
        h('td', { text: a.world }),
        h('td', {}, h('div', { class: 'row', style: { gap: '8px', justifyContent: 'flex-end' } }, [
          h('button', { class: a.installed ? 'secondary' : 'primary', i18n: a.installed ? 'missions.reinstall' : 'missions.install', disabled: running,
            onclick: (e) => withBusy(e.currentTarget, () => installFromMod(a, activate.checked, a.installed)) }),
          h('label', { class: 'inline-switch' }, [activate, h('span', { i18n: 'missions.andActivate' })]),
        ])),
      ]));
    }
    at.append(ab);
    availCard.append(h('div', { class: 'table-scroll' }, at));
  }
  root.append(availCard);

  async function installFromMod(a, activate, overwrite) {
    if (overwrite && !(await confirmModal(t('missions.overwrite').replace('{name}', a.name), { danger: true }))) return;
    try {
      await api.post('/api/missions/install', { mod: a.mod, path: a.path, zip: !!a.zip, inner: a.inner || '', activate, overwrite });
      toast(t('missions.installed.ok').replace('{name}', a.name), 'ok');
      await navigate('missions');
    } catch (err) {
      if (/already exists/.test(err.message || '') && (await confirmModal(t('missions.overwrite').replace('{name}', a.name), { danger: true }))) {
        return installFromMod(a, activate, true);
      }
      handleErr(err);
    }
  }

  // ---- import a .zip or a folder
  const zipInput = h('input', { type: 'file', accept: '.zip,application/zip', 'aria-label': t('missions.zip'), style: { display: 'none' } });
  const zipBtn = h('button', { type: 'button', class: 'secondary', i18n: 'missions.zip.pickBtn', onclick: () => zipInput.click() });
  const zipPicked = h('span', { class: 'hint' });
  const dirInput = h('input', { type: 'file', webkitdirectory: true, multiple: true, 'aria-label': t('missions.folder'), style: { display: 'none' } });
  // The native control would say "Choose files"; a folder is what is wanted.
  const dirBtn = h('button', { type: 'button', class: 'secondary', i18n: 'missions.folder.pickBtn', onclick: () => dirInput.click() });
  const zipName = h('input', { type: 'text', placeholder: t('missions.zip.namePh'), 'aria-label': t('missions.zip.name') });
  const zipActivate = h('input', { type: 'checkbox', class: 'switch' });
  zipActivate.checked = true;
  const picked = h('span', { class: 'hint' });
  let folder = null; // { name, files: [[File, relPath]] } — the mission inside the picked folder
  zipInput.onchange = () => {
    if (!zipInput.files.length) return;
    dirInput.value = ''; folder = null; picked.textContent = '';
    zipPicked.textContent = zipInput.files[0].name;
  };
  dirInput.onchange = async () => {
    zipInput.value = '';
    zipPicked.textContent = '';
    folder = null;
    picked.textContent = '';
    const roots = missionRootsIn([...dirInput.files]);
    if (!roots.length) { toast(t('missions.folder.none'), 'warn'); dirInput.value = ''; return; }
    const root = roots.length === 1 ? roots[0] : await chooseMissionRoot(roots);
    if (!root) { dirInput.value = ''; return; }
    const base = root.split('/').pop();
    const files = [];
    for (const f of dirInput.files) {
      const rel = f.webkitRelativePath || f.name;
      if (rel.startsWith(root + '/')) files.push([f, base + rel.slice(root.length)]);
    }
    folder = { name: base, files };
    picked.textContent = t('missions.folder.selected').replace('{name}', base).replace('{n}', files.length);
  };
  const doImport = async (btn, overwrite) => {
    const f = zipInput.files && zipInput.files[0];
    if (!f && !folder) { toast(t('missions.zip.pick'), 'warn'); return; }
    const fd = new FormData();
    if (folder) for (const [file, rel] of folder.files) fd.append('files', file, rel);
    else fd.append('zip', f);
    fd.append('name', zipName.value.trim());
    fd.append('activate', zipActivate.checked ? '1' : '0');
    fd.append('overwrite', overwrite ? '1' : '0');
    await withBusy(btn, async () => {
      try {
        const r = await fetch(folder ? '/api/missions/import-folder' : '/api/missions/import', { method: 'POST', body: fd, headers: srvHeaders() });
        const txt = await r.text();
        if (r.status === 409 && !overwrite) {
          const shown = zipName.value.trim() || (folder ? folder.name : f.name);
          if (await confirmModal(t('missions.overwrite').replace('{name}', shown), { danger: true })) return doImport(btn, true);
          return;
        }
        if (!r.ok) throw new Error(txt);
        const res = JSON.parse(txt);
        toast(t('missions.installed.ok').replace('{name}', res.name), 'ok');
        await navigate('missions');
      } catch (err) { handleErr(err); }
    });
  };
  importCard.append(
    cardTitle('i-sync', 'missions.import.title'),
    h('p', { class: 'hint', i18n: 'missions.import.hint' }),
    h('div', { class: 'grid-2' }, [
      h('div', {}, [h('label', { i18n: 'missions.zip.file', for: assocId(zipBtn) }),
        h('div', { class: 'row', style: { gap: '10px', alignItems: 'center', flexWrap: 'wrap' } }, [zipBtn, zipInput, zipPicked])]),
      h('div', {}, [h('label', { i18n: 'missions.folder', for: assocId(dirBtn) }),
        h('div', { class: 'row', style: { gap: '10px', alignItems: 'center', flexWrap: 'wrap' } }, [dirBtn, dirInput, picked])]),
    ]),
    h('div', { style: { marginTop: '10px', maxWidth: '520px' } }, [h('label', { i18n: 'missions.zip.name', for: assocId(zipName) }), zipName]),
    h('label', { class: 'inline-switch', style: { marginTop: '10px' } }, [zipActivate, h('span', { i18n: 'missions.andActivate' })]),
    h('div', { class: 'actions' }, [h('button', { class: 'primary', i18n: 'missions.zip.import', disabled: running, onclick: (e) => doImport(e.currentTarget, false) })]),
  );
  root.append(importCard);

  // ---- trash: a deleted mission can be put back, or removed for good
  const trashCount = h('span', { class: 'pill-count' });
  const trashBody = h('div', { class: 'msn-trash-body' });
  root.append(h('details', { class: 'card msn-trash' }, [
    h('summary', {}, [cardTitle('i-archive', 'missions.trash.title'), trashCount]),
    trashBody,
  ]));
  async function loadTrash() {
    let tr;
    try { tr = await api.get('/api/missions/trash'); }
    catch (e) { trashBody.textContent = e.message; return; }
    if (myNav !== _navSeq) return;
    const items = tr.missions || [];
    trashCount.textContent = String(items.length);
    trashBody.innerHTML = '';
    trashBody.append(h('p', { class: 'hint', i18n: items.length ? 'missions.trash.hint' : 'missions.trash.empty' }));
    for (const it of items) {
      const meta = [it.at ? fmtWhen(it.at) : '', bytes(it.size)].filter(Boolean).join(' · ');
      trashBody.append(h('div', { class: 'msn-trash-row' }, [
        h('div', { class: 'msn-trash-info' }, [
          h('div', { class: 'mono', text: it.name }),
          h('small', { class: 'hint', text: meta }),
          it.taken ? h('small', { class: 'hint warn-text', text: t('missions.trash.taken') }) : null,
        ].filter(Boolean)),
        h('div', { class: 'msn-trash-act' }, [
          h('button', { type: 'button', class: 'secondary small', i18n: 'missions.trash.restore', disabled: running || it.taken,
            onclick: (e) => withBusy(e.currentTarget, () => restoreEntry(it.entry)) }),
          h('button', { type: 'button', class: 'danger small', i18n: 'missions.trash.purge', onclick: async (e) => {
            const btn = e.currentTarget;
            if (!(await confirmModal(t('missions.trash.purgeConfirm').replace('{name}', it.name), { danger: true, okText: t('missions.trash.purge') }))) return;
            await withBusy(btn, async () => {
              try { await api.post('/api/missions/trash/delete', { entry: it.entry }); toast(t('msg.deleted'), 'ok'); await loadTrash(); }
              catch (err) { handleErr(err); }
            });
          } }),
        ]),
      ]));
    }
    applyI18n();
  }
  loadTrash();
  applyI18n();
};

// uninstallModFlow — uninstall a mod; when it is a map whose mission sits in
// mpmissions, offer to remove that mission too (moved to the trash).
async function uninstallModFlow(modName, btn) {
  let linked = [];
  try { linked = (await api.get('/api/missions/linked?mod=' + encodeURIComponent(modName))).missions || []; } catch {}
  const m = openModal({ title: t('action.uninstall') + ' ' + modName });
  const boxes = linked.map(l => {
    const cb = h('input', { type: 'checkbox', class: 'switch' });
    cb.checked = !l.active;
    cb.disabled = l.active;
    return { l, cb };
  });
  m.body.append(h('p', { text: t('confirm.uninstall').replace('{mod}', modName) }));
  if (boxes.length) {
    m.body.append(h('p', { class: 'hint', i18n: 'missions.uninstall.linked' }));
    for (const { l, cb } of boxes) {
      m.body.append(h('label', { class: 'inline-switch', style: { display: 'flex' } }, [cb,
        h('span', { class: 'mono', text: l.name + (l.active ? ' — ' + t('missions.uninstall.activeKeep') : '') })]));
    }
  }
  m.body.append(h('div', { class: 'actions' }, [
    h('button', { class: 'danger', i18n: 'action.delete', onclick: (e) => withBusy(e.currentTarget, async () => {
      try {
        await api.post('/api/mods/uninstall', { mod: modName });
        for (const { l, cb } of boxes) {
          if (cb.checked && !l.active) await api.post('/api/missions/delete', { name: l.name });
        }
        m.close();
        toast(t('msg.removed'), 'ok');
        await navigate('mods');
      } catch (err) { handleErr(err); }
    }) }),
    h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
  ]));
  applyI18n();
}

// ------------------------------------------------------------- day / night

// Approximate latitude of each world (° north) — decides how long in-game days
// are at a given date. Unknown worlds use Chernarus' 45°.
const WORLD_LAT = { chernarus: 45, livonia: 52, sakhal: 50, nasdara: 35, namalsk: 60, deerisle: 44, banov: 49, esseker: 44, takistan: 34, pripyat: 51, bitterroot: 46 };

// daylightHours: sunrise→sunset length for a latitude and date (standard solar
// declination formula; good to a few minutes, which is all the planner needs).
function daylightHours(lat, date) {
  const start = new Date(date.getFullYear(), 0, 0);
  const n = Math.floor((date - start) / 86400000);
  const decl = 23.44 * Math.sin((2 * Math.PI / 365) * (284 + n)) * Math.PI / 180;
  const x = Math.max(-1, Math.min(1, -Math.tan(lat * Math.PI / 180) * Math.tan(decl)));
  return 2 * Math.acos(x) * 180 / Math.PI / 15;
}

// parseServerTime: "SystemTime" or "YYYY/MM/DD/HH/MM" → { system, date, minutes }.
function parseServerTime(v) {
  const m = /^\s*(\d{4})\/(\d{1,2})\/(\d{1,2})\/(\d{1,2})\/(\d{1,2})\s*$/.exec(v || '');
  if (!m) return { system: true, date: new Date(), minutes: null };
  return { system: false, date: new Date(+m[1], +m[2] - 1, +m[3]), minutes: (+m[4]) * 60 + (+m[5]) };
}

const fmtHM = (mins) => {
  mins = Math.round(mins);
  const hh = Math.floor(mins / 60), mm = mins % 60;
  return hh ? `${hh} ${t('unit.hours')} ${String(mm).padStart(2, '0')} ${t('unit.minutes')}` : `${mm} ${t('unit.minutes')}`;
};
const clock = (gameMin) => {
  const m = ((Math.round(gameMin) % 1440) + 1440) % 1440;
  return String(Math.floor(m / 60)).padStart(2, '0') + ':' + String(m % 60).padStart(2, '0');
};

// restartOffsets: real minutes from now at which the server restarts over the
// next 24 h, from the Settings schedule (interval measured from a start now).
function restartOffsets(cfg) {
  const out = [];
  const now = new Date();
  for (const tm of (cfg.scheduledRestarts || [])) {
    const m = /^\s*(\d{1,2}):(\d{2})\s*$/.exec(tm || '');
    if (!m) continue;
    const d = new Date(now); d.setHours(+m[1], +m[2], 0, 0);
    if (d <= now) d.setDate(d.getDate() + 1);
    out.push(Math.round((d - now) / 60000));
  }
  if (cfg.autoRestartEnabled && cfg.autoRestartSeconds > 0) {
    const step = cfg.autoRestartSeconds / 60;
    for (let x = step; x < 1440; x += step) out.push(Math.round(x));
  }
  return [...new Set(out)].filter(x => x > 0 && x < 1440).sort((a, b) => a - b);
}

// simulateDay walks 24 real hours minute by minute from a server start now and
// returns in-game day/night segments plus the restart marks. The in-game clock
// runs at accel by day and accel × nightAccel by night; a restart resets it to
// the start time unless time is persistent (SystemTime restarts at wall time).
function simulateDay(p) {
  const segs = [];
  const now = new Date();
  const realNow = now.getHours() * 60 + now.getMinutes();
  let g = p.start.system ? realNow : p.start.minutes;
  const isDay = (gm) => { const m = ((gm % 1440) + 1440) % 1440; return m >= p.sunrise && m < p.sunset; };
  const restarts = new Set(p.restarts);
  let cur = null;
  for (let r = 0; r < 1440; r++) {
    if (restarts.has(r) && !p.persistent) g = p.start.system ? (realNow + r) : p.start.minutes;
    const day = isDay(g);
    if (!cur || cur.day !== day) { cur = { from: r, to: r + 1, day }; segs.push(cur); } else cur.to = r + 1;
    g += day ? p.accel : p.accel * p.nightAccel;
  }
  return segs;
}

// gameMinuteAfter walks the in-game clock from startMin for realMin real
// minutes: day runs at accel, night at accel × nightAccel. Returns 0..1439.
function gameMinuteAfter(startMin, realMin, p) {
  let g = startMin;
  let r = Math.min(realMin, 43200); // a month of uptime is plenty
  while (r > 0) {
    const step = Math.min(1, r);
    const m = ((g % 1440) + 1440) % 1440;
    g += step * (m >= p.sunrise && m < p.sunset ? p.accel : p.accel * p.nightAccel);
    r -= step;
  }
  return ((g % 1440) + 1440) % 1440;
}

// restartEvery is the time between restarts in real minutes (0 = none).
function restartEvery(cfg) {
  if (cfg.autoRestartEnabled && cfg.autoRestartSeconds > 0) return cfg.autoRestartSeconds / 60;
  const n = (cfg.scheduledRestarts || []).filter(x => /^\s*\d{1,2}:\d{2}\s*$/.test(x || '')).length;
  return n ? 1440 / n : 0;
}

// skySVG draws the sky at in-game minute m: the sun on its arc by day, the
// moon by night, over a line of hills.
function skySVG(m, sunrise, sunset) {
  const day = m >= sunrise && m < sunset;
  const frac = day ? (m - sunrise) / Math.max(1, sunset - sunrise)
    : (((m - sunset) % 1440) + 1440) % 1440 / Math.max(1, 1440 - (sunset - sunrise));
  const x = 70 + 860 * frac, y = 172 - 128 * Math.sin(Math.PI * frac);
  // Low sun = dawn or dusk colours.
  const alt = Math.sin(Math.PI * frac);
  const phase = !day ? 'night' : alt < 0.28 ? 'dusk' : 'day';
  const stars = [[90, 40], [180, 75], [260, 30], [340, 62], [420, 24], [520, 50], [600, 28], [690, 70], [760, 36], [850, 58], [930, 30], [140, 110], [880, 100]];
  return `<svg class="dn-sky-svg ${phase}" viewBox="0 0 1000 200" preserveAspectRatio="xMidYMid slice" aria-hidden="true">
    <defs>
      <linearGradient id="dnSkyDay" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#3a78d4"/><stop offset="1" stop-color="#b9dcf7"/></linearGradient>
      <linearGradient id="dnSkyDusk" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#3a3f78"/><stop offset=".6" stop-color="#d9774a"/><stop offset="1" stop-color="#f6b56b"/></linearGradient>
      <linearGradient id="dnSkyNight" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#070b1d"/><stop offset="1" stop-color="#1c2550"/></linearGradient>
      <radialGradient id="dnGlow"><stop offset="0" stop-color="#ffe7a3" stop-opacity=".85"/><stop offset="1" stop-color="#ffe7a3" stop-opacity="0"/></radialGradient>
    </defs>
    <rect width="1000" height="200" fill="url(#${phase === 'day' ? 'dnSkyDay' : phase === 'dusk' ? 'dnSkyDusk' : 'dnSkyNight'})"/>
    ${!day ? stars.map(([sx, sy]) => `<circle cx="${sx}" cy="${sy}" r="1.6" fill="#fff" opacity=".75"/>`).join('') : ''}
    <path d="M70 172 Q500 -84 930 172" fill="none" stroke="#fff" stroke-opacity=".22" stroke-dasharray="4 7"/>
    ${day ? `<circle cx="${x}" cy="${y}" r="46" fill="url(#dnGlow)"/><circle cx="${x}" cy="${y}" r="16" fill="#ffd96b"/>`
    : `<circle cx="${x}" cy="${y}" r="12" fill="#e7edf7"/><circle cx="${x + 6}" cy="${y - 4}" r="11" fill="#1a2350"/>`}
    <path d="M0 168 Q110 138 230 158 T470 150 T720 164 T1000 146 V200 H0 Z" fill="#1b2a2a" opacity="${day ? '.55' : '.9'}"/>
    <path d="M0 184 Q160 160 330 178 T660 176 T1000 170 V200 H0 Z" fill="#0f1a1a" opacity="${day ? '.75' : '1'}"/>
  </svg>`;
}

// timeSummary is the Server page's view of serverDZ.cfg's time settings:
// the sky at the start time, how long a day and a night last in real time
// (and their share of a whole in-game day), where the clock starts — the
// Weather & Time page, which changes them, one click away.
function timeSummary(values, link) {
  const v = (k, d) => (values[k] ?? '') !== '' ? String(values[k]).trim() : d;
  const a = Math.min(24, Math.max(0.1, Number(v('serverTimeAcceleration', '1')) || 1));
  const na = Math.min(64, Math.max(0.1, Number(v('serverNightTimeAcceleration', '1')) || 1));
  const start = parseServerTime(v('serverTime', 'SystemTime'));
  const dayH = daylightHours(45, start.date);
  const sunrise = (12 - dayH / 2) * 60, sunset = (12 + dayH / 2) * 60;
  const dayReal = dayH * 60 / a, nightReal = (24 - dayH) * 60 / (a * na);
  const now = new Date();
  const startMin = start.system ? now.getHours() * 60 + now.getMinutes() : start.minutes;
  let when = t('dn.start.system');
  if (!start.system) {
    let day = '';
    try { day = start.date.toLocaleDateString(State.lang || undefined, { day: 'numeric', month: 'long' }); } catch {}
    when = clock(start.minutes) + (day ? ' · ' + day : '');
  }
  const x = (n) => '×' + (Math.round(n * 10) / 10);
  const stat = (cls, icon, k, mult, real) => h('div', { class: 'tsum-stat ' + cls }, [
    h('span', { class: 'tsum-ico', html: icon }),
    h('div', {}, [h('div', { class: 'tsum-k' }, [h('span', { i18n: k }), mult ? h('b', { text: ' ' + mult }) : null]), h('div', { class: 'tsum-v', text: real })]),
  ]);
  const SUN = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>';
  const MOON = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg>';
  const CLOCK = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>';
  const total = dayReal + nightReal;
  return h('div', { class: 'tsum' }, [
    h('div', { class: 'tsum-sky', html: skySVG(startMin, sunrise, sunset) }, [
      h('span', { class: 'tsum-clock', text: start.system ? clock(startMin) : clock(start.minutes) }),
    ]),
    h('div', { class: 'tsum-main' }, [
      h('div', { class: 'tsum-stats' }, [
        stat('day', SUN, 'dn.dayReal', x(a), fmtHM(dayReal)),
        stat('night', MOON, 'dn.nightReal', x(a * na), fmtHM(nightReal)),
        stat('start', CLOCK, 'weather.time.serverTime', '', when),
      ]),
      h('div', { class: 'tsum-bar', title: t('dn.cycle') + ': ' + fmtHM(total) }, [
        h('span', { class: 'tsum-seg day', style: { flex: String(dayReal) } }, h('span', { text: fmtHM(dayReal) })),
        h('span', { class: 'tsum-seg night', style: { flex: String(nightReal) } }, h('span', { text: fmtHM(nightReal) })),
      ]),
      h('div', { class: 'tsum-foot' }, [
        h('span', { class: 'hint', text: t('dn.cycle') + ': ' + fmtHM(total) }),
        v('serverTimePersistent', '0') === '1' ? h('span', { class: 'badge mute', text: t('server.time.persistent') }) : null,
        h('span', { class: 'grow' }),
        h('button', { class: 'small secondary', type: 'button', i18n: 'server.time.edit', onclick: () => navigate(link) }),
      ].filter(Boolean)),
    ]),
  ]);
}

// dayNightCard — the Weather page's time section: serverTimeAcceleration,
// serverNightTimeAcceleration, start time and persistence, with the real-time
// length of day and night, a planner that works backwards from "I want X
// minutes of day and Y of night", and a 24-hour timeline across restarts.
function dayNightCard(tm, map, running) {
  const lat = WORLD_LAT[(map && map.key) || ''] ?? 45;
  const accel = h('input', { type: 'number', min: '0.1', max: '24', step: '0.1', value: tm.serverTimeAcceleration ?? '1', disabled: running });
  const nAccel = h('input', { type: 'number', min: '0.1', max: '64', step: '0.1', value: tm.serverNightTimeAcceleration ?? '1', disabled: running });
  const sTime = h('input', { type: 'text', value: tm.serverTime ?? 'SystemTime', style: { maxWidth: '220px' }, disabled: running });
  const sTimeHint = h('small', { class: 'hint' });
  const persist = h('input', { type: 'checkbox', class: 'switch', disabled: running });
  persist.checked = String(tm.serverTimePersistent ?? '0') === '1';
  const snapshot = () => [accel.value, nAccel.value, sTime.value.trim(), persist.checked].join('|');
  let baseline = snapshot();

  const stats = h('div', { class: 'dn-stats' });
  // The sky at the in-game time: live for a running server, the start time
  // otherwise; ▶ plays one in-game day (■ stops it sooner), and the white
  // marker on the bar under it can be dragged to any moment. Built once and updated
  // in place: a button rebuilt every frame never gets its click.
  const sky = h('div', { class: 'dn-sky' });
  const realBar = h('div', { class: 'dn-realbar' });
  const presetHost = h('div', { class: 'dn-feel' });
  let anim = null;  // { from: performance.now(), m0 } while playing
  let scrub = null; // an in-game minute picked on the bar
  const DAY_MS = 16000; // one in-game day while playing
  // Sliders next to the number fields (the fields stay for exact values).
  const accelR = h('input', { type: 'range', min: '0.1', max: '24', step: '0.1', value: accel.value, disabled: running, 'aria-label': t('weather.time.accel') });
  const nAccelR = h('input', { type: 'range', min: '0.1', max: '64', step: '0.1', value: nAccel.value, disabled: running, 'aria-label': t('weather.time.nightAccel') });
  accelR.addEventListener('input', () => { accel.value = accelR.value; planTouched = false; render(); });
  nAccelR.addEventListener('input', () => { nAccel.value = nAccelR.value; planTouched = false; render(); });
  const bar = h('div', { class: 'dn-bar' });
  const axis = h('div', { class: 'dn-axis-row', 'aria-hidden': 'true' });
  const legend = h('div', { class: 'dn-legend' });

  const num = (el, lo, hi, dflt) => { const v = Number(el.value); return Number.isFinite(v) ? Math.min(hi, Math.max(lo, v)) : dflt; };
  const fmtNum = (n) => { try { return n.toLocaleString(State.lang || undefined, { maximumFractionDigits: 1 }); } catch { return n.toFixed(1); } };

  // "SystemTime" or "YYYY/MM/DD/HH/MM" in words.
  function describeStart() {
    const v = sTime.value.trim();
    if (!v || /^systemtime$/i.test(v)) { sTimeHint.textContent = t('dn.start.system'); return; }
    const m = /^(\d{4})\/(\d{1,2})\/(\d{1,2})\/(\d{1,2})\/(\d{1,2})$/.exec(v);
    if (!m) { sTimeHint.textContent = t('dn.start.format'); return; }
    const dt = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]), Number(m[4]), Number(m[5]));
    let day = '';
    try { day = dt.toLocaleDateString(State.lang || undefined, { day: 'numeric', month: 'long' }); } catch { day = m[3] + '.' + m[2]; }
    sTimeHint.textContent = String(m[4]).padStart(2, '0') + ':' + String(m[5]).padStart(2, '0') + ' · ' + day;
  }

  function render() {
    const a = num(accel, 0.1, 24, 1), na = num(nAccel, 0.1, 64, 1);
    const start = parseServerTime(sTime.value);
    const dayH = daylightHours(lat, start.date);
    const sunrise = (12 - dayH / 2) * 60, sunset = (12 + dayH / 2) * 60;
    const dayReal = dayH * 60 / a, nightReal = (24 - dayH) * 60 / (a * na);
    const cycle = dayReal + nightReal;
    stats.innerHTML = '';
    const stat = (k, v, cls, sub) => h('div', { class: 'dn-stat ' + (cls || '') }, [
      h('div', { class: 'k', i18n: k }), h('div', { class: 'v', text: v }), sub ? h('div', { class: 'sub', text: sub }) : null].filter(Boolean));
    // How much of an in-game day one restart period covers.
    const every = restartEvery(State.config || {});
    const share = every ? every / cycle : 0;
    stats.append(
      stat('dn.sun', `${clock(sunrise)} → ${clock(sunset)}`, 'sun'),
      stat('dn.dayReal', fmtHM(dayReal), 'day'),
      stat('dn.nightReal', fmtHM(nightReal), 'night'),
      stat('dn.cycle', fmtHM(cycle), '', tn('dn.perDay', { n: Math.round(1440 / cycle * 10) / 10 })),
      every
        ? h('div', { class: 'dn-stat restart' }, [
          h('div', { class: 'k', text: t('dn.share').replace('{every}', fmtHM(every)) }),
          h('div', { class: 'v', text: share <= 1 ? t('dn.share.pct').replace('{pct}', Math.round(share * 100)) : t('dn.share.days').replace('{n}', Math.round(share * 10) / 10) }),
        ])
        : h('div', { class: 'dn-stat restart' }, [h('div', { class: 'k', i18n: 'dn.share.title' }), h('div', { class: 'v small', i18n: 'dn.share.none' })]),
    );
    accelR.value = String(a); nAccelR.value = String(na);
    renderSky({ a, na, sunrise, sunset, start, dayReal, nightReal, cycle });
    renderFeel(dayH);
    // 24-hour real-time timeline. The SVG only draws the bars (it stretches);
    // the hours are HTML so they keep their shape at any width.
    const cfg = State.config || {};
    const restarts = restartOffsets(cfg);
    const segs = simulateDay({ accel: a, nightAccel: na, start, persistent: persist.checked, restarts, sunrise, sunset });
    const W = 1000, H = 34;
    let svg = `<svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none" class="dn-svg" role="img" aria-label="${t('dn.timeline')}">`;
    for (const s of segs) {
      svg += `<rect x="${s.from / 1440 * W}" y="0" width="${(s.to - s.from) / 1440 * W}" height="${H}" class="${s.day ? 'dn-day' : 'dn-night'}"/>`;
    }
    for (const r of restarts) svg += `<line x1="${r / 1440 * W}" x2="${r / 1440 * W}" y1="0" y2="${H}" class="dn-restart"/>`;
    svg += '</svg>';
    bar.innerHTML = svg;
    axis.innerHTML = '';
    const now = new Date();
    for (let hh = 0; hh <= 24; hh += 3) {
      const lbl = hh === 0 ? t('dn.now') : new Date(now.getTime() + hh * 3600000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
      axis.append(h('span', { class: 'dn-tick' + (hh % 6 ? ' odd' : ''), text: lbl }));
    }
    legend.innerHTML = '';
    // (append() would print a literal "null" for an absent item.)
    legend.append(...[
      h('span', { class: 'dn-key day', i18n: 'dn.legendDay' }),
      h('span', { class: 'dn-key night', i18n: 'dn.legendNight' }),
      restarts.length ? h('span', { class: 'dn-key restart', i18n: 'dn.legendRestart' }) : null,
      h('span', { class: 'hint', text: (map && map.name ? map.name + ' · ' : '') + t('dn.approx').replace('{lat}', lat) }),
    ].filter(Boolean));
    if (!persist.checked && !start.system && restarts.length) {
      legend.append(h('div', { class: 'hint', text: t('dn.resetNote').replace('{t}', clock(start.minutes)) }));
    }
    describeStart();
    // The planner shows what the current speeds give, until it is edited.
    if (!planTouched) {
      wantDay.value = Math.round(dayReal);
      wantNight.value = Math.round(nightReal);
    }
    saveBar.hidden = !card.isDirty();
  }

  // The sky scene and the real-time bar under it.
  let lastSky = null;
  const skyArt = h('div', { class: 'dn-sky-art' });
  const phaseEl = h('div', { class: 'dn-sky-phase' });
  const clockEl = h('div', { class: 'dn-sky-clock' });
  const subEl = h('div', { class: 'dn-sky-sub' });
  const leftEl = h('div', { class: 'dn-sky-sub' });
  const ICON_PLAY = '<svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true"><path d="M7 4v16l13-8z" fill="currentColor"/></svg>';
  const ICON_STOP = '<svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true"><rect x="6" y="5" width="4" height="14" fill="currentColor"/><rect x="14" y="5" width="4" height="14" fill="currentColor"/></svg>';
  const playBtn = h('button', { type: 'button', class: 'dn-play', onclick: () => {
    if (anim) { scrub = curMinute(lastSky).m; anim = null; renderSky(lastSky); return; }
    // keep: the moment picked before playing, shown again once the day is over.
    anim = { from: performance.now(), m0: curMinute(lastSky).m, keep: scrub };
    scrub = null;
    renderSky(lastSky);
    requestAnimationFrame(tick);
  } });
  const backBtn = h('button', { type: 'button', class: 'dn-play dn-back', hidden: true, title: t('dn.sky.back'), 'aria-label': t('dn.sky.back'),
    html: '<svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true"><path d="M4 12a8 8 0 1 0 2.4-5.7M4 4v5h5" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    onclick: () => { anim = null; scrub = null; renderSky(lastSky); } });
  sky.append(skyArt, h('div', { class: 'dn-sky-info' }, [phaseEl, clockEl, subEl, leftEl]), h('div', { class: 'dn-sky-btns' }, [backBtn, playBtn]));

  const rbDayTxt = h('span'), rbNightTxt = h('span');
  const rbDay = h('span', { class: 'dn-rb-day' }, rbDayTxt);
  const rbNight = h('span', { class: 'dn-rb-night' }, rbNightTxt);
  const rbNow = h('span', { class: 'dn-rb-now', role: 'slider', tabindex: '0', 'aria-label': t('dn.sky.scrub'),
    'aria-valuemin': '0', 'aria-valuemax': '1439' }, h('span', { class: 'dn-rb-knob' }));
  const rb = h('div', { class: 'dn-rb', title: t('dn.rb.hint') }, [rbDay, rbNight, rbNow]);
  const rbSunrise = h('span'), rbSunset = h('span'), rbCycle = h('span');
  realBar.append(rb, h('div', { class: 'dn-rb-legend' }, [rbSunrise, rbSunset, h('span', { class: 'grow' }), rbCycle]));

  // The in-game minute shown: playing, picked on the bar, live for a running
  // server, or the start time.
  function curMinute(p) {
    if (anim) return { m: (anim.m0 + (performance.now() - anim.from) / DAY_MS * 1440) % 1440, mode: 'play' };
    if (scrub != null) return { m: scrub, mode: 'scrub' };
    const st = State.serverStatus || {};
    const now = new Date();
    const wall = now.getHours() * 60 + now.getMinutes();
    if (st.running && st.uptimeSec >= 0) {
      const up = st.uptimeSec / 60;
      const startMin = p.start.system ? wall - up : p.start.minutes;
      return { m: gameMinuteAfter(startMin, up, { accel: p.a, nightAccel: p.na, sunrise: p.sunrise, sunset: p.sunset }), mode: 'live' };
    }
    return { m: p.start.system ? wall : p.start.minutes, mode: 'start' };
  }
  // A point on the bar → the in-game minute: the bar is one in-game day in
  // real time, day first, then night.
  function barMinute(frac, p) {
    const r = frac * p.cycle;
    const g = r <= p.dayReal ? p.sunrise + r * p.a : p.sunset + (r - p.dayReal) * p.a * p.na;
    return ((g % 1440) + 1440) % 1440;
  }
  function moveTo(m) {
    // Dragged while playing: the day goes on from here, and ends here.
    if (anim) anim = { from: performance.now(), m0: m, keep: m }; else scrub = m;
    renderSky(lastSky);
  }
  let dragging = false;
  const fromPointer = (e) => {
    const box = rb.getBoundingClientRect();
    let frac = (e.clientX - box.left) / Math.max(1, box.width);
    if (getComputedStyle(rb).direction === 'rtl') frac = 1 - frac;
    moveTo(barMinute(Math.min(1, Math.max(0, frac)), lastSky));
  };
  rb.addEventListener('pointerdown', (e) => {
    if (e.button !== 0) return;
    dragging = true;
    rb.classList.add('drag');
    try { rb.setPointerCapture(e.pointerId); } catch { /* a synthetic event */ }
    fromPointer(e);
    rbNow.focus({ preventScroll: true });
    e.preventDefault();
  });
  rb.addEventListener('pointermove', (e) => { if (dragging) fromPointer(e); });
  const endDrag = () => { dragging = false; rb.classList.remove('drag'); };
  rb.addEventListener('pointerup', endDrag);
  rb.addEventListener('pointercancel', endDrag);
  rbNow.addEventListener('keydown', (e) => {
    const p = lastSky;
    if (!p) return;
    const m = curMinute(p).m;
    const step = e.shiftKey ? 60 : 15;
    const keys = { ArrowRight: step, ArrowUp: step, ArrowLeft: -step, ArrowDown: -step };
    if (keys[e.key] != null) moveTo((((m + keys[e.key]) % 1440) + 1440) % 1440);
    else if (e.key === 'Home') moveTo(p.sunrise);
    else if (e.key === 'End') moveTo(p.sunset);
    else return;
    e.preventDefault();
  });

  function renderSky(p) {
    lastSky = p;
    const { m, mode } = curMinute(p);
    const day = m >= p.sunrise && m < p.sunset;
    // Real time until the phase changes.
    const left = day ? (p.sunset - m) / p.a : ((((p.sunrise - m) % 1440) + 1440) % 1440) / (p.a * p.na);
    skyArt.innerHTML = skySVG(m, p.sunrise, p.sunset);
    phaseEl.textContent = t(day ? 'dn.sky.day' : 'dn.sky.night');
    clockEl.textContent = clock(m);
    subEl.textContent = mode === 'start' ? t('dn.sky.atStart') : mode === 'play' ? t('dn.sky.preview')
      : mode === 'scrub' ? t('dn.sky.scrubbed') : t('dn.sky.live') + (persist.checked ? ' ' + t('dn.sky.approx') : '');
    leftEl.textContent = t(day ? 'dn.sky.nightIn' : 'dn.sky.dayIn').replace('{t}', fmtHM(left));
    const playing = !!anim;
    if (playBtn.dataset.state !== String(playing)) {
      playBtn.dataset.state = String(playing);
      playBtn.innerHTML = playing ? ICON_STOP : ICON_PLAY;
      playBtn.title = t(playing ? 'dn.sky.pause' : 'dn.sky.play');
      playBtn.setAttribute('aria-label', playBtn.title);
      playBtn.classList.toggle('on', playing);
    }
    backBtn.hidden = mode !== 'play' && mode !== 'scrub';
    // Real-time bar: one in-game day, day and night to scale, a marker now.
    const dayPct = p.dayReal / p.cycle * 100;
    const pos = day ? (m - p.sunrise) / p.a : p.dayReal + ((((m - p.sunset) % 1440) + 1440) % 1440) / (p.a * p.na);
    rbDay.style.width = dayPct + '%';
    rbNight.style.width = (100 - dayPct) + '%';
    rbDayTxt.textContent = t('dn.rb.day') + ' ' + fmtHM(p.dayReal);
    rbNightTxt.textContent = fmtHM(p.nightReal);
    rbNow.style.insetInlineStart = Math.min(100, Math.max(0, pos / p.cycle * 100)) + '%';
    rbNow.setAttribute('aria-valuenow', String(Math.round(m)));
    rbNow.setAttribute('aria-valuetext', clock(m));
    rbSunrise.textContent = '☀ ' + t('dn.rb.sunrise').replace('{time}', clock(p.sunrise));
    rbSunset.textContent = '☾ ' + t('dn.rb.sunset').replace('{time}', clock(p.sunset));
    rbCycle.textContent = t('dn.rb.cycle').replace('{len}', fmtHM(p.cycle));
  }
  function tick() {
    if (!anim) return;
    if (!sky.isConnected) { anim = null; return; }
    // One full day played: back to where it started.
    if (performance.now() - anim.from >= DAY_MS) { scrub = anim.keep; anim = null; renderSky(lastSky); return; }
    renderSky(lastSky);
    requestAnimationFrame(tick);
  }
  // A running server's clock moves: refresh the scene every 20 s.
  const live = setInterval(() => { if (!sky.isConnected) { clearInterval(live); return; } if (!anim && lastSky) renderSky(lastSky); }, 20000);

  // "Aim for a feel": the presets as cards, each with the day/night split it
  // gives at this date.
  const FEEL = [
    ['dn.preset.real', 1, 1], ['dn.preset.relaxed', 4, 4], ['dn.preset.fast', 12, 4],
    ['dn.preset.shortNights', 6, 16], ['dn.preset.mostlyDay', 8, 64],
  ];
  function renderFeel(dayH) {
    presetHost.innerHTML = '';
    const ca = Number(accel.value), cn = Number(nAccel.value);
    for (const [k, a, n] of FEEL) {
      const d = dayH * 60 / a, nn = (24 - dayH) * 60 / (a * n);
      const on = Math.abs(ca - a) < 0.05 && Math.abs(cn - n) < 0.05;
      presetHost.append(h('button', { type: 'button', class: 'dn-feel-card' + (on ? ' on' : ''), disabled: running,
        onclick: () => { planTouched = false; accel.value = a; nAccel.value = n; render(); } }, [
        h('b', { i18n: k }),
        h('span', { class: 'dn-feel-bar' }, [h('span', { class: 'd', style: { width: (d / (d + nn) * 100) + '%' } }), h('span', { class: 'n' })]),
        h('small', { text: t('dn.feel.split').replace('{day}', fmtHM(d)).replace('{night}', fmtHM(nn)) }),
      ]));
    }
    applyI18n();
  }

  // Planner: work backwards from the real-time day / night length wanted —
  // recalculated as you type.
  let planTouched = false;
  const wantDay = h('input', { type: 'number', min: '1', step: '1', style: { width: '96px' }, disabled: running, 'aria-label': t('dn.plan.day') });
  const wantNight = h('input', { type: 'number', min: '1', step: '1', style: { width: '96px' }, disabled: running, 'aria-label': t('dn.plan.night') });
  const planOut = h('div', { class: 'hint' });
  const plan = () => {
    planTouched = true;
    const start = parseServerTime(sTime.value);
    const dayH = daylightHours(lat, start.date);
    const d = Number(wantDay.value), n = Number(wantNight.value);
    if (!(d > 0) || !(n > 0)) { planOut.textContent = t('dn.plan.need'); return; }
    let a = dayH * 60 / d;
    let na = (24 - dayH) * 60 / (n * a);
    const clamped = a < 0.1 || a > 24 || na < 0.1 || na > 64;
    a = Math.min(24, Math.max(0.1, a));
    na = Math.min(64, Math.max(0.1, (24 - dayH) * 60 / (n * a)));
    accel.value = Math.round(a * 10) / 10;
    nAccel.value = Math.round(na * 10) / 10;
    planOut.textContent = t(clamped ? 'dn.plan.clamped' : 'dn.plan.ok');
    render();
  };
  wantDay.addEventListener('input', plan);
  wantNight.addEventListener('input', plan);

  const startQuick = [
    ['weather.time.start.system', 'SystemTime'], ['weather.time.start.morning', '2024/7/1/8/0'],
    ['weather.time.start.noon', '2024/7/1/12/0'], ['weather.time.start.evening', '2024/7/1/19/0'],
    ['weather.time.start.night', '2024/7/1/0/0'],
  ].map(([k, v]) => h('button', { class: 'pill', type: 'button', i18n: k, disabled: running, onclick: () => { sTime.value = v; render(); } }));

  [accel, nAccel].forEach(el => el.addEventListener('input', () => { planTouched = false; render(); }));
  sTime.addEventListener('input', render);
  persist.addEventListener('change', render);

  async function saveTime(btn) {
    const check = (inp, lo, hi) => {
      const v = Number(inp.value);
      const bad = !Number.isFinite(v) || v < lo || v > hi;
      inp.style.borderColor = bad ? 'var(--error)' : '';
      return bad ? null : v;
    };
    const day = check(accel, 0.1, 24), night = check(nAccel, 0.1, 64);
    if (day === null || night === null) { toast(t('weather.time.range'), 'error'); return; }
    await withBusy(btn, async () => {
      try {
        await api.post('/api/weather/time', {
          serverTimeAcceleration: day, serverNightTimeAcceleration: night,
          serverTime: sTime.value.trim() || 'SystemTime', serverTimePersistent: persist.checked ? 1 : 0,
        });
        baseline = snapshot();
        saveBar.hidden = true;
        toast(t('msg.saved'), 'ok');
      } catch (err) { handleErr(err); }
    });
  }
  const save = h('button', { class: 'primary', i18n: 'weather.time.save', disabled: running, onclick: (e) => saveTime(e.currentTarget) });
  const saveBar = h('div', { class: 'sticky-save', hidden: true }, [
    h('span', { i18n: 'gameplay.unsaved' }), h('span', { class: 'grow' }),
    h('button', { type: 'button', class: 'primary small', i18n: 'weather.time.save', disabled: running, onclick: (e) => saveTime(e.currentTarget) }),
  ]);

  const fieldRow = (k, el, extra, target) => h('div', { class: 'dn-field' }, [h('label', { i18n: k, for: assocId(target || el) }), el, extra || null]);
  const card = h('div', { class: 'card', id: 'daynight' }, [
    h('div', { class: 'card-head' }, [cardTitle('i-clock', 'dn.title', 'weather.accel')]),
    h('p', { class: 'hint', i18n: 'dn.intro' }),
    sky,
    realBar,
    stats,
    bar,
    axis,
    legend,
    h('h4', { class: 'dn-feel-title', i18n: 'dn.feel.title' }),
    presetHost,
    h('div', { class: 'dn-grid' }, [
      fieldRow('weather.time.accel', h('div', { class: 'dn-slide' }, [accelR, accel]), h('small', { class: 'hint', i18n: 'dn.accel.hint' }), accel),
      fieldRow('weather.time.nightAccel', h('div', { class: 'dn-slide' }, [nAccelR, nAccel]), h('small', { class: 'hint', i18n: 'dn.night.hint' }), nAccel),
      fieldRow('weather.time.serverTime', sTime, h('div', {}, [sTimeHint, h('div', { class: 'pills' }, startQuick)])),
      h('div', { class: 'dn-field' }, [h('label', { class: 'inline-switch dn-persist' }, [persist, h('span', { i18n: 'dn.persist.on' })]), h('small', { class: 'hint', i18n: 'dn.persist.hint' })]),
    ]),
    h('div', { class: 'dn-plan' }, [
      h('h4', { i18n: 'dn.plan.title' }),
      h('div', { class: 'row dn-plan-row' }, [
        h('label', { class: 'dn-plan-field' }, [h('span', { i18n: 'dn.plan.day' }), wantDay]),
        h('label', { class: 'dn-plan-field' }, [h('span', { i18n: 'dn.plan.night' }), wantNight]),
      ]),
      planOut,
    ]),
    h('div', { class: 'actions' }, [save]),
    saveBar,
  ]);
  card.isDirty = () => snapshot() !== baseline;
  render();
  return card;
}

// weatherScene draws the manual weather: clouds that thicken and darken
// with overcast, the sun behind them, fog, rain or snow, lightning, and a
// tree, bushes and grass that lean and sway with the wind (leaves fly off
// in a gale). Built once; update() moves it with the sliders.
function weatherScene() {
  // A fixed pseudo-random spread, so drops and flakes keep their places.
  let seed = 7;
  const rnd = () => { seed = (seed * 16807) % 2147483647; return (seed - 1) / 2147483646; };
  const drops = [], flakes = [], blades = [];
  for (let i = 0; i < 70; i++) {
    const x = Math.round(rnd() * 680) - 40, y = Math.round(rnd() * 260) - 260, d = (0.45 + rnd() * 0.35).toFixed(2), dl = (-rnd() * 2).toFixed(2);
    drops.push(`<line x1="${x}" y1="${y}" x2="${x}" y2="${y + 16}" style="animation-duration:${d}s;animation-delay:${dl}s" class="wx-drop${i % 3 ? '' : ' far'}"/>`);
  }
  for (let i = 0; i < 60; i++) {
    const x = Math.round(rnd() * 680) - 40, y = Math.round(rnd() * 260) - 260, r = (1.2 + rnd() * 1.8).toFixed(1), d = (3 + rnd() * 3).toFixed(2), dl = (-rnd() * 6).toFixed(2);
    flakes.push(`<circle cx="${x}" cy="${y}" r="${r}" style="animation-duration:${d}s;animation-delay:${dl}s" class="wx-flake"/>`);
  }
  for (let x = 4; x < 600; x += 9) {
    const hgt = 8 + Math.round(rnd() * 10), dl = (-rnd() * 2).toFixed(2);
    blades.push(`<path d="M${x} 232 q${2 + Math.round(rnd() * 3)} ${-hgt / 2} ${1 + Math.round(rnd() * 3)} ${-hgt}" style="animation-delay:${dl}s; transform-origin:${x}px 232px" class="wx-blade"/>`);
  }
  const cloud = (i, x, y, k) => `<g class="wx-cloud" data-i="${i}" style="--cx:${x}px"><g transform="translate(${x} ${y}) scale(${k})">
      <ellipse cx="0" cy="18" rx="62" ry="20"/><ellipse cx="-30" cy="8" rx="30" ry="22"/><ellipse cx="14" cy="-2" rx="38" ry="30"/><ellipse cx="46" cy="12" rx="26" ry="18"/></g></g>`;
  const leaves = [0, 1, 2, 3, 4, 5, 6, 7].map(i => `<path class="wx-leaf" style="animation-delay:${(-i * 0.47).toFixed(2)}s; --ly:${40 + (i * 23) % 120}px" d="M0 0 q5 -5 10 0 q-5 5 -10 0z"/>`).join('');
  const el = h('div', { class: 'wx-scene', role: 'img', 'aria-label': t('wx.preview') });
  el.innerHTML = `<svg viewBox="0 0 600 260" preserveAspectRatio="xMidYMid slice" aria-hidden="true">
    <defs>
      <linearGradient id="wxClear" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#2f7fd8"/><stop offset="1" stop-color="#a9d6ff"/></linearGradient>
      <linearGradient id="wxGrey" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#5d6672"/><stop offset="1" stop-color="#a7aeb7"/></linearGradient>
      <linearGradient id="wxDark" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#1f252e"/><stop offset="1" stop-color="#4b525c"/></linearGradient>
      <radialGradient id="wxSunGlow"><stop offset="0" stop-color="#fff3c4" stop-opacity=".9"/><stop offset="1" stop-color="#fff3c4" stop-opacity="0"/></radialGradient>
      <linearGradient id="wxFogBand" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#e3e7ec" stop-opacity="0"/><stop offset="1" stop-color="#e3e7ec" stop-opacity="1"/></linearGradient>
    </defs>
    <rect width="600" height="260" fill="url(#wxClear)"/>
    <rect class="wx-grey" width="600" height="260" fill="url(#wxGrey)"/>
    <rect class="wx-dark" width="600" height="260" fill="url(#wxDark)"/>
    <g class="wx-sun"><circle cx="478" cy="62" r="64" fill="url(#wxSunGlow)"/><circle cx="478" cy="62" r="24" fill="#ffd866"/></g>
    <g class="wx-clouds">${cloud(0, 110, 50, 0.9)}${cloud(1, 330, 34, 1.1)}${cloud(2, 520, 70, 0.8)}${cloud(3, 220, 86, 1.25)}${cloud(4, 430, 98, 1.15)}${cloud(5, 40, 104, 1.3)}</g>
    <path class="wx-bolt" d="M300 70 L282 128 L300 128 L284 184 L330 112 L308 112 L324 70 Z"/>
    <path class="wx-hill far" d="M0 196 Q90 168 190 186 T400 178 T600 170 V260 H0 Z"/>
    <path class="wx-hill" d="M0 222 Q140 200 300 216 T600 206 V260 H0 Z"/>
    <path class="wx-snowcap" d="M0 222 Q140 200 300 216 T600 206 V214 Q450 216 300 224 T0 230 Z"/>
    <g class="wx-bush" style="transform-origin:84px 226px"><ellipse cx="70" cy="218" rx="26" ry="16"/><ellipse cx="98" cy="214" rx="22" ry="18"/></g>
    <g class="wx-bush b2" style="transform-origin:520px 222px"><ellipse cx="506" cy="214" rx="24" ry="15"/><ellipse cx="532" cy="211" rx="20" ry="17"/></g>
    <g class="wx-tree" style="transform-origin:220px 228px">
      <path class="wx-trunk" d="M213 228 L216 150 Q218 140 222 150 L226 228 Z"/>
      <g class="wx-crown" style="transform-origin:219px 150px">
        <circle cx="219" cy="112" r="40"/><circle cx="190" cy="132" r="28"/><circle cx="250" cy="130" r="30"/><circle cx="222" cy="84" r="26"/>
      </g>
    </g>
    <g class="wx-tree small" style="transform-origin:396px 224px">
      <path class="wx-trunk" d="M392 224 L394 176 Q396 170 398 176 L400 224 Z"/>
      <g class="wx-crown" style="transform-origin:396px 176px"><circle cx="396" cy="152" r="26"/><circle cx="378" cy="166" r="17"/><circle cx="414" cy="166" r="18"/></g>
    </g>
    <g class="wx-grass">${blades.join('')}</g>
    <g class="wx-leaves">${leaves}</g>
    <g class="wx-rain">${drops.join('')}</g>
    <g class="wx-snow">${flakes.join('')}</g>
    <rect class="wx-fog" width="600" height="260"/>
    <rect class="wx-fogband" y="140" width="600" height="120" fill="url(#wxFogBand)"/>
    <rect class="wx-flash" width="600" height="260"/>
  </svg>
  <div class="wx-scene-tags"><span class="wx-tag wx-tag-oc"></span><span class="wx-tag wx-tag-wind"></span></div>`;
  const tagOc = el.querySelector('.wx-tag-oc'), tagWind = el.querySelector('.wx-tag-wind');
  const clouds = [...el.querySelectorAll('.wx-cloud')];
  function update(v) {
    const oc = v.overcast, fog = v.fog, wind = Math.min(20, Math.max(0, v.wind));
    const raining = v.rain > 0.05 && oc >= 0.6, storming = v.storm > 0.05 && oc >= 0.9, snowing = v.snow > 0.05;
    const st = el.style;
    st.setProperty('--oc', oc.toFixed(3));
    st.setProperty('--fog', fog.toFixed(3));
    st.setProperty('--rain', (raining ? 0.35 + v.rain * 0.65 : 0).toFixed(3));
    st.setProperty('--snow', (snowing ? 0.35 + v.snow * 0.65 : 0).toFixed(3));
    // Wind: a lean the harder it blows, a sway that quickens with it.
    st.setProperty('--lean', (wind * 1.1).toFixed(2) + 'deg');
    st.setProperty('--amp', (0.8 + wind * 0.32).toFixed(2) + 'deg');
    st.setProperty('--glean', (wind * 2.2).toFixed(2) + 'deg');
    st.setProperty('--gamp', (2 + wind * 0.45).toFixed(2) + 'deg');
    st.setProperty('--sway', Math.max(0.55, 3.2 - wind * 0.13).toFixed(2) + 's');
    st.setProperty('--drift', Math.max(14, 120 - wind * 5.5).toFixed(1) + 's');
    st.setProperty('--slant', (-wind * 1.6).toFixed(2) + 'deg');
    st.setProperty('--snowdx', (wind * 9).toFixed(1) + 'px');
    // A cloud more for every step of overcast; they darken as it thickens.
    clouds.forEach((c, i) => { c.style.opacity = String(Math.max(0, Math.min(1, (oc - i * 0.13) * 3.2)).toFixed(2)); });
    el.classList.toggle('rain', raining);
    el.classList.toggle('snow', snowing);
    el.classList.toggle('storm', storming);
    el.classList.toggle('windy', wind >= 9);
    el.classList.toggle('calm', wind < 0.5);
    tagOc.textContent = '☁ ' + Math.round(oc * 100) + '%';
    tagWind.innerHTML = '<svg viewBox="0 0 24 24" width="13" height="13" aria-hidden="true"><path d="M3 8h11a3 3 0 1 0-3-3M3 12h16a3 3 0 1 1-3 3M3 16h8" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"/></svg>';
    tagWind.append(' ' + (Math.round(wind * 10) / 10) + ' ' + t('unit.mps'));
  }
  return { el, update };
}

// weatherPreview — the scene plus a plain-language summary of what players
// will experience with the current slider values.
function weatherPreview(get) {
  const scene = weatherScene();
  const text = h('ul', { class: 'wx-notes' });
  function update() {
    const oc = get.overcast(), fog = get.fog(), rain = get.rain(), snow = get.snow(), storm = get.storm(), wind = get.wind();
    // Thresholds match the cfgweather.xml the manager writes: rain from 60 %
    // overcast, lightning from 90 %.
    scene.update({ overcast: oc, fog, rain, snow, storm, wind });
    text.innerHTML = '';
    const note = (k) => text.append(h('li', { i18n: k }));
    note(oc < 0.3 ? 'wx.sky.clear' : oc < 0.6 ? 'wx.sky.partly' : oc < 0.85 ? 'wx.sky.cloudy' : 'wx.sky.heavy');
    if (rain > 0.05) note(oc >= 0.6 ? 'wx.rain.yes' : 'wx.rain.needsClouds');
    if (storm > 0.05) note(oc >= 0.9 ? 'wx.storm.yes' : 'wx.storm.needsClouds');
    if (fog >= 0.6) note('wx.fog.heavy'); else if (fog >= 0.25) note('wx.fog.some');
    if (wind >= 12) note('wx.wind.strong'); else if (wind >= 6) note('wx.wind.breeze'); else if (wind < 0.5) note('wx.wind.calm');
    if (snow > 0.05) note('wx.snow');
  }
  update();
  return {
    el: h('div', { class: 'wx-preview' }, [scene.el, h('div', { class: 'wx-side' }, [h('h4', { i18n: 'wx.preview' }), text])]),
    update,
  };
}

// ------------------------------------------------------------------ validator

// vtext is a validator message or repair in the panel's language: i18n key
// prefix + key with its args in the {placeholders} ("n" counts, for the plural
// forms); the English text when the language has no such key.
function vtext(prefix, key, args, english) {
  const k = prefix + (key || '');
  if (!key || !State.i18n[k]) return english || '';
  const vars = {};
  for (const [a, v] of Object.entries(args || {})) vars[a] = a === 'n' && /^-?\d+$/.test(v) ? Number(v) : String(v);
  return tn(k, vars);
}
const vIssueText = (is) => vtext('vmsg.', is.key || is.code, is.args, is.message);
const vFixText = (x) => typeof x === 'string' ? x : vtext('vfix.', x.key, x.args, x.text);

// Validator — problems grouped by kind (stable issue codes), each with a
// translated title, the affected files (open in the editor at the line, or the
// folder), and a Fix button for kinds with a safe automatic repair.
Views.validator = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.validator', 'validator.subtitle'));
  const running = !!State.serverStatus.running;
  if (running) root.append(runningBanner());
  const host = h('div');
  const report = h('div');
  root.append(report, host);
  const sevRank = { error: 0, warning: 1, info: 2 };
  const cls = (sev) => sev === 'error' ? 'err' : (sev === 'warning' ? 'warn' : 'mute');

  async function fix(codes, btn) {
    await withBusy(btn, async () => {
      try {
        const r = await api.post('/api/validate/fix', codes ? { codes } : {});
        report.innerHTML = '';
        if (!r.count) toast(t('validator.fix.none'), 'ok');
        else {
          toast(tn('validator.fix.done', { n: r.count }), 'ok');
          report.append(h('div', { class: 'card validator-report' }, [
            h('h3', { i18n: 'validator.report' }),
            h('ul', {}, (r.fixed || []).map(x => h('li', { text: vFixText(x) }))),
          ]));
        }
        await run();
      } catch (e) { handleErr(e); }
    });
  }

  async function run() {
    host.innerHTML = '';
    host.append(h('div', { class: 'card' }, h('p', { class: 'hint', i18n: 'validator.running' })));
    applyI18n();
    let d;
    try { d = await api.get('/api/validate'); }
    catch (e) { host.innerHTML = ''; handleErr(e); return; }
    if (myNav !== _navSeq) return;
    host.innerHTML = '';
    const issues = d.issues || [];
    // Group by code (issues without one fall into "other").
    const groups = new Map();
    for (const is of issues) {
      const code = is.code || 'other';
      if (!groups.has(code)) groups.set(code, { code, sev: is.severity, fix: is.fix, items: [] });
      const g = groups.get(code);
      g.items.push(is);
      if ((sevRank[is.severity] ?? 3) < (sevRank[g.sev] ?? 3)) g.sev = is.severity;
      if (is.fix) g.fix = is.fix;
    }
    const list = [...groups.values()].sort((a, b) => (sevRank[a.sev] - sevRank[b.sev]) || (b.items.length - a.items.length));
    const nErr = issues.filter(i => i.severity === 'error').length;
    const nWarn = issues.filter(i => i.severity === 'warning').length;
    const nInfo = issues.filter(i => i.severity === 'info').length;
    const fixable = list.filter(g => g.fix);
    // The whole list as text, to paste into a support chat or a ticket.
    const copyAll = () => copyText(list.map(g => {
      const title = State.i18n['vcode.' + g.code] ? t('vcode.' + g.code) : g.code;
      return '## ' + title + '\n' + g.items.map(is => '- ' + (is.rel || (is.file || '').split(/[\\/]/).pop()) + (is.line ? ':' + is.line : '') + ' — ' + vIssueText(is)).join('\n');
    }).join('\n\n'));

    const summary = h('div', { class: 'card validator-top' }, [
      h('div', { class: 'validator-summary' }, [
        issues.length
          ? [nErr ? h('span', { class: 'badge err', text: tn('validator.count.error', { n: nErr }) }) : null,
             nWarn ? h('span', { class: 'badge warn', text: tn('validator.count.warning', { n: nWarn }) }) : null,
             nInfo ? h('span', { class: 'badge mute', text: tn('validator.count.info', { n: nInfo }) }) : null,
             h('span', { class: 'hint', text: tn('validator.kinds2', { n: list.length }) })].filter(Boolean)
          : [h('span', { class: 'validator-ok', i18n: 'validator.none' })],
      ].flat()),
      h('div', { class: 'actions' }, [
        h('button', { class: 'secondary', i18n: 'validator.recheck', onclick: (e) => withBusy(e.currentTarget, run) }),
        issues.length ? h('button', { class: 'secondary', i18n: 'validator.copy', onclick: copyAll }) : null,
        fixable.length ? h('button', { class: 'primary', disabled: running, text: t('validator.fixAll').replace('{n}', fixable.reduce((s, g) => s + g.items.length, 0)),
          'data-help': 'validator.autofix', onclick: (e) => fix(null, e.currentTarget) }) : null,
      ].filter(Boolean)),
      h('p', { class: 'hint', i18n: 'validator.fix.hint2' }),
    ]);
    host.append(summary);

    for (const g of list) {
      const title = t('vcode.' + g.code);
      const shown = 5;
      const body = h('div', { class: 'vgroup-items' });
      const renderItems = (all) => {
        body.innerHTML = '';
        const items = all ? g.items : g.items.slice(0, shown);
        for (const is of items) {
          const where = (is.file || '').split(/[\\/]/).pop() + (is.line ? ':' + is.line : '');
          const acts = h('div', { class: 'vitem-acts' });
          if (is.rel && /\.(xml|json|cfg|c|txt)$/i.test(is.rel)) {
            acts.append(h('button', { class: 'secondary icon-only', title: t('validator.openEditor'), 'aria-label': t('validator.openEditor'), text: '✎',
              onclick: () => { _filesTarget = { path: is.rel, line: is.line || 0 }; navigate('files'); } }));
          }
          if (is.file && is.file !== 'manager.json') acts.append(revealBtn({ path: is.file }, { iconOnly: true }));
          if (is.file === 'manager.json') acts.append(h('button', { class: 'secondary', i18n: 'nav.mods', onclick: () => navigate('mods') }));
          body.append(h('div', { class: 'vitem' }, [
            h('span', { class: 'mono vitem-where', text: where, title: is.file || '' }),
            h('span', { class: 'vitem-msg', text: vIssueText(is), title: is.message }),
            acts,
          ]));
        }
        if (!all && g.items.length > shown) {
          body.append(h('button', { class: 'link-btn', text: t('validator.more').replace('{n}', g.items.length - shown), onclick: () => renderItems(true) }));
        }
      };
      renderItems(false);
      host.append(h('div', { class: 'card vgroup vgroup-' + cls(g.sev) }, [
        h('div', { class: 'vgroup-head' }, [
          h('span', { class: 'badge ' + cls(g.sev), text: t('validator.severity.' + g.sev) || g.sev }),
          h('h3', { text: title === 'vcode.' + g.code ? g.code : title }),
          h('span', { class: 'badge mute', text: '×' + g.items.length }),
          h('span', { class: 'grow' }),
          g.fix ? h('button', { class: 'primary small', disabled: running, i18n: 'validator.fixThis', onclick: (e) => fix([g.fix], e.currentTarget) }) : null,
        ].filter(Boolean)),
        body,
      ]));
    }
    applyI18n();
  }
  await run();
};

// ---------------------------------------------------------------- map layers

// Real map data drawn from the server's own mission files (see maplayers.go).
// Fetched once per page load and shared by every map on the page.
let _mapDataCache = null; // { key, info, buildings, zones }

async function mapLayerData(kind, key) {
  // Keyed by world: activating another mission must not show the old data.
  if (!_mapDataCache || _mapDataCache.key !== key) _mapDataCache = { key, info: null, buildings: null, zones: null };
  const c = _mapDataCache;
  if (kind === 'info') {
    if (!c.info) c.info = api.get('/api/map/layers').catch(() => ({}));
    return c.info;
  }
  if (kind === 'buildings') {
    if (!c.buildings) c.buildings = api.get('/api/map/buildings').catch(() => null);
    return c.buildings;
  }
  if (!c.zones) c.zones = api.get('/api/map/zones').catch(() => null);
  return c.zones;
}

const LAYER_KEYS = ['land', 'tiers', 'buildings', 'infected', 'wildlife', 'toxic'];

function layerPrefs() {
  try { return JSON.parse(localStorage.getItem('mapLayers') || '{}') || {}; } catch { return {}; }
}
function saveLayerPref(k, on) {
  try { const p = layerPrefs(); p[k] = on; localStorage.setItem('mapLayers', JSON.stringify(p)); } catch {}
}

// attachMapLayers adds the layers button + panel to a map built by
// buildGameMap and draws the enabled layers into gm.overlay. The panel also
// holds the map background (picture upload / URL / remove, modded map size):
// a one-time setting per world, not a toolbar button on every map page.
function attachMapLayers(gm, map, opts = {}) {
  const mapKey = (map && map.key) || '';
  const btn = h('button', { class: 'gm-zbtn gm-layers-btn', type: 'button', title: t('layers.title'), 'aria-label': t('layers.title') });
  btn.innerHTML = svgIcon('i-layers', 16);
  const layersHost = h('div');
  const panel = h('div', { class: 'gm-layers-panel', hidden: true }, [
    layersHost,
    h('div', { class: 'gm-bg-sec' }, [
      h('div', { class: 'gm-layers-title', i18n: 'map.bg.title' }),
      mapImageControls(map, opts.onImageChange),
    ]),
  ]);
  gm.controls.append(btn, panel);
  btn.addEventListener('click', (e) => { e.preventDefault(); panel.hidden = !panel.hidden; });
  gm.openLayers = () => { panel.hidden = false; };
  // The "upload a map picture" prompt on a blank world opens the panel.
  const cta = gm.svg.querySelector('.gm-cta');
  if (cta) cta.addEventListener('click', (e) => { e.stopPropagation(); gm.openLayers(); });

  const groups = {};
  const S = gm.S, k = S / gm.size;
  const draw = {
    land(info) {
      return svgEl('image', { href: srvUrl('/api/map/layer.png?layer=land&v=' + ((info.ver || {}).areaflags || 0)), x: 0, y: 0, width: S, height: S, preserveAspectRatio: 'none', class: 'ov-land' });
    },
    tiers(info) {
      return svgEl('image', { href: srvUrl('/api/map/layer.png?layer=tiers&v=' + ((info.ver || {}).areaflags || 0)), x: 0, y: 0, width: S, height: S, preserveAspectRatio: 'none', class: 'ov-tiers' });
    },
    async buildings() {
      const d = await mapLayerData('buildings', mapKey);
      const g = svgEl('g', { class: 'ov-buildings' });
      if (!d) return g;
      const w = Math.max(0.6, 14 * k);
      for (const kind of ['house', 'ind', 'misc', 'mil']) {
        const pts = d[kind] || [];
        let path = '';
        for (let i = 0; i + 1 < pts.length; i += 2) {
          const [sx, sy] = gm.w2s(pts[i], pts[i + 1]);
          path += `M${(sx - w / 2).toFixed(1)} ${(sy - w / 2).toFixed(1)}h${w.toFixed(2)}v${w.toFixed(2)}h-${w.toFixed(2)}z`;
        }
        if (path) svgEl('path', { d: path, class: 'ov-b ov-b-' + kind }, g);
      }
      return g;
    },
    async infected() { return zones('infected', 'ov-zone ov-infected'); },
    async wildlife() { return zones('wildlife', 'ov-zone ov-wildlife'); },
    async toxic() { return zones('toxic', 'ov-zone ov-toxic'); },
  };
  async function zones(key, cls) {
    const d = await mapLayerData('zones', mapKey);
    const g = svgEl('g', { class: cls });
    for (const z of ((d && d[key]) || [])) {
      const [sx, sy] = gm.w2s(z.x, z.z);
      const c = svgEl('circle', { cx: sx, cy: sy, r: Math.max(1, z.r * k) }, g);
      const tt = svgEl('title', {}, c);
      tt.textContent = z.k;
    }
    return g;
  }

  async function setLayer(key, on, info) {
    gm.svg.classList.toggle('gm-' + key + '-on', on);
    if (groups[key]) { groups[key].remove(); groups[key] = null; }
    if (!on) return;
    const el = await draw[key](info);
    // Keep a stable stacking order whatever the toggle order.
    groups[key] = el;
    for (const kk of LAYER_KEYS) if (groups[kk]) gm.overlay.append(groups[kk]);
  }

  (async () => {
    const info = await mapLayerData('info', mapKey);
    const avail = info.layers || {};
    const hasImage = !!(map && map.hasImage);
    // Defaults: without an uploaded image, the real land and buildings from
    // the mission replace the approximate schematic on every world — except
    // the land mask on a landlocked world (Nasdara, Takistan): it is one flat
    // square there and would only hide the schematic's relief.
    const shape = (typeof MAP_SHAPES !== 'undefined' && mapKey) ? MAP_SHAPES[mapKey] : null;
    const landlocked = !!(shape && shape.coast === false);
    const defaults = {
      land: !hasImage && !landlocked, tiers: false,
      buildings: !hasImage, infected: false, wildlife: false, toxic: false,
    };
    const prefs = layerPrefs();
    const any = LAYER_KEYS.some(k2 => avail[k2]);
    layersHost.append(h('div', { class: 'gm-layers-title', i18n: 'layers.title' }));
    if (!any) {
      layersHost.append(h('p', { class: 'hint', i18n: 'layers.none' }));
      applyI18n();
      return;
    }
    for (const key of LAYER_KEYS) {
      if (!avail[key]) continue;
      const cb = h('input', { type: 'checkbox' });
      cb.checked = key in prefs ? !!prefs[key] : defaults[key];
      cb.addEventListener('change', () => { saveLayerPref(key, cb.checked); setLayer(key, cb.checked, info); });
      layersHost.append(h('label', { class: 'gm-layer-row' }, [cb, h('span', { class: 'gm-layer-swatch sw-' + key }), h('span', { i18n: 'layers.' + key })]));
      if (key === 'tiers' && (info.tierNames || []).length) {
        layersHost.append(h('div', { class: 'gm-tier-legend' }, info.tierNames.map(n => h('span', { class: 'tier-' + n.toLowerCase(), text: n }))));
      }
      if (cb.checked) setLayer(key, true, info);
    }
    layersHost.append(h('p', { class: 'hint gm-layers-hint', i18n: 'layers.hint' }));
    applyI18n();
  })();
}

// ------------------------------------------------------------ manager card

// managerCard — which build is running (browser / desktop app), and an Exit
// button: the desktop app has no console to Ctrl+C, and either build can be
// left with the DayZ server still running (it is adopted on the next start).
function managerCard() {
  const card = h('div', { class: 'card' }, [
    cardTitle('i-server', 'settings.manager.title'),
    h('p', { class: 'hint', i18n: 'settings.manager.hint' }),
  ]);
  const line = h('div', { class: 'row', style: { gap: '10px', alignItems: 'center' } });
  card.append(line);
  (async () => {
    let info = {};
    try { info = await api.get('/api/info'); } catch {}
    line.append(
      h('span', { class: 'badge info', i18n: info.mode === 'panel' ? 'settings.manager.mode.panel' : 'settings.manager.mode.web' }),
      h('span', { class: 'hint', text: 'v' + (info.version || '') + ' · ' + (info.os || '') }),
      h('span', { class: 'grow' }),
      h('button', { class: 'danger', i18n: 'settings.manager.exit', onclick: exitFlow }),
    );
    applyI18n();
  })();
  return card;
}

// exitFlow asks how to leave the manager: stop the DayZ server too, keep it
// running (it is adopted on the next start), or cancel. The Linux app window
// calls it (window.dzmAskExit) when it is closed while a server runs.
async function exitFlow() {
  if (document.querySelector('.modal-overlay[data-exit]')) return;
  try { await refreshStatus(); } catch {}
  const running = !!(State.serverStatus && State.serverStatus.running);
  const m = openModal({ title: t('settings.manager.exit') });
  const overlay = document.querySelector('.modal-overlay:last-of-type');
  if (overlay) overlay.dataset.exit = '1';
  const go = (stop) => async (e) => {
    await withBusy(e.currentTarget, async () => {
      try {
        await api.post('/api/app/exit', { stopServer: stop });
        m.close();
        document.getElementById('view').innerHTML = '';
        document.getElementById('view').append(h('div', { class: 'card' }, h('p', { i18n: 'settings.manager.bye' })));
        applyI18n();
      } catch (err) { handleErr(err); }
    });
  };
  m.body.append(h('div', { class: 'actions' }, [
    h('button', { class: 'danger', i18n: running ? 'settings.manager.exitStop' : 'settings.manager.exit', onclick: go(true) }),
    running ? h('button', { i18n: 'settings.manager.exitKeep', onclick: go(false) }) : null,
    h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
  ].filter(Boolean)));
  applyI18n();
}
window.dzmAskExit = exitFlow;

// ------------------------------------------------------------ getting started

// Where each checklist item is set: route + the element to bring into view.
const SETUP_TARGETS = {
  hostname:      ['server',   '[data-key="hostname"]'],
  adminPassword: ['server',   '[data-key="passwordAdmin"]'],
  modSource:     ['settings', 'input[name="mod-source"]'],
  rcon:          ['rcon',     '[data-help="rcon.password"]'],
  restarts:      ['settings/restarts', '[data-i18n="settings.autorestart.enable"]'],
  watchdog:      ['settings/restarts', '[data-i18n="settings.watchdog"]'],
  backups:       ['settings/backups', '[data-i18n="settings.autobackup.every"]'],
  started:       ['dashboard', null],
  login:         ['settings/access', '#auth-card'],
};

// focusTarget opens a page and scrolls a specific field into view with a short
// highlight, so "Set this up" lands on the field instead of the page top.
async function focusTarget(route, selector) {
  await navigate(route);
  if (!selector) { window.scrollTo({ top: 0, behavior: 'smooth' }); return; }
  for (let i = 0; i < 20; i++) { // views render asynchronously
    const el = document.querySelector(selector);
    if (el) {
      const box = el.closest('label, .field, .kv > div, div') || el;
      box.scrollIntoView({ behavior: 'smooth', block: 'center' });
      box.classList.remove('flash-target'); void box.offsetWidth; box.classList.add('flash-target');
      if (el.matches('input, select, textarea') && el.type !== 'radio' && el.type !== 'checkbox') el.focus({ preventScroll: true });
      return;
    }
    await new Promise(r => setTimeout(r, 100));
  }
}

// setupChecklistCard: the dashboard's "Getting started" list. Computed on the
// server from the real configuration; hides itself once everything is done,
// and can be dismissed (per browser).
function setupChecklistCard() {
  const host = h('div');
  (async () => {
    let r;
    try { r = await api.get('/api/setup/checklist'); } catch { return; }
    if (!r || !Array.isArray(r.items) || r.done >= r.total) return;
    if (State.config && State.config.setupDismissed) return; // removed for good
    // A compact strip by default (progress + the next step); the full list on
    // demand. "Hide" collapses it; "Don't show again" removes it (Settings →
    // General brings it back).
    let open = false;
    try { open = localStorage.getItem('setupOpen') === '1'; } catch {}
    const pct = Math.round(100 * r.done / r.total);
    const next = r.items.find(it => !it.done);
    const goBtn = (it) => {
      if (it.id === 'started') {
        return h('button', { class: 'link-btn', type: 'button', i18n: 'action.start', onclick: (e) => withBusy(e.currentTarget, async () => {
          try { await api.post('/api/server/start'); toast(t('msg.starting'), 'ok'); await navigate('dashboard'); } catch (err) { handleErr(err); }
        }) });
      }
      const [route, sel] = SETUP_TARGETS[it.id] || [it.route, null];
      return h('button', { class: 'link-btn', type: 'button', i18n: 'setup.go', onclick: () => focusTarget(route, sel) });
    };
    const render = () => {
      host.innerHTML = '';
      const head = h('div', { class: 'setup-head' }, [
        h('h3', { i18n: 'setup.title' }),
        h('span', { class: 'setup-count', text: t('setup.progress').replace('{done}', r.done).replace('{total}', r.total) }),
        h('button', { class: 'link-btn', type: 'button', i18n: open ? 'setup.hide' : 'setup.showAll', onclick: () => {
          open = !open;
          try { localStorage.setItem('setupOpen', open ? '1' : '0'); } catch {}
          render();
        } }),
        h('button', { class: 'icon-x setup-dismiss', type: 'button', text: '×', title: t('setup.dismiss'), 'aria-label': t('setup.dismiss'),
          onclick: async () => {
            try {
              State.config = await api.post('/api/config', { setupDismissed: true });
              host.innerHTML = '';
              toast(t('setup.dismissed'), 'ok');
            } catch (err) { handleErr(err); }
          } }),
      ]);
      const card = h('div', { class: 'card setup-card' + (open ? '' : ' compact') }, [head, h('div', { class: 'setup-bar' }, [h('span', { style: { width: pct + '%' } })])]);
      if (!open && next) {
        card.append(h('div', { class: 'setup-next' }, [
          h('span', { class: 'hint', i18n: 'setup.next' }),
          h('b', { i18n: 'setup.item.' + next.id }),
          goBtn(next),
        ]));
      }
      if (open) {
        card.append(h('ul', { class: 'setup-list' }, r.items.map(it => h('li', { class: it.done ? 'done' : '' }, [
          h('span', { class: 'setup-check', 'aria-hidden': 'true', text: it.done ? '✓' : '' }),
          h('div', { class: 'setup-text' }, [
            h('div', { class: 'setup-title', i18n: 'setup.item.' + it.id }),
            it.done ? null : h('div', { class: 'setup-why', i18n: 'setup.why.' + it.id }),
          ].filter(Boolean)),
          it.done ? null : goBtn(it),
        ].filter(Boolean)))));
      }
      host.append(card);
      applyI18n();
    };
    render();
  })();
  return host;
}

// ------------------------------------------------------- missions inside mods

// missionRootsIn finds mission folders among files picked with a folder
// dialog (webkitRelativePath): folders holding cfgeconomycore.xml, or init.c
// next to db/ (or under a "template.world" name). Nested ones are dropped.
function missionRootsIn(files) {
  const set = new Set(files.map(f => (f.webkitRelativePath || f.name).toLowerCase()));
  const found = new Map(); // lower → original
  for (const f of files) {
    const rel = f.webkitRelativePath || f.name;
    const i = rel.lastIndexOf('/');
    if (i < 0) continue;
    const dir = rel.slice(0, i), base = rel.slice(i + 1).toLowerCase(), low = dir.toLowerCase();
    const named = /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(dir.split('/').pop());
    if (base === 'cfgeconomycore.xml' ||
        (base === 'init.c' && (set.has(low + '/db/types.xml') || set.has(low + '/db/events.xml') || named))) {
      found.set(low, dir);
    }
  }
  const roots = [...found.values()].sort();
  return roots.filter(r => !roots.some(o => o !== r && r.startsWith(o + '/')));
}

// chooseMissionRoot asks which mission to import when a folder holds several.
function chooseMissionRoot(roots) {
  return new Promise(resolve => {
    let done = false;
    const m = openModal({ title: t('missions.import.title'), onClose: () => { if (!done) resolve(null); } });
    m.body.append(h('p', { i18n: 'missions.folder.choose' }));
    for (const r of roots) {
      m.body.append(h('button', { class: 'secondary', style: { display: 'block', width: '100%', textAlign: 'left', marginTop: '6px' },
        onclick: () => { done = true; m.close(); resolve(r); } }, [h('span', { class: 'mono', text: r })]));
    }
    applyI18n();
  });
}

// reportModMissions tells the admin what happened to map missions found inside
// a mod that was just installed / updated / synced — and offers to switch to
// the new map right away.
function reportModMissions(r) {
  reportModVehicles(r);
  const list = (r && Array.isArray(r.missions)) ? r.missions : [];
  for (const x of list.filter(x => x.error)) {
    toast(t('missions.auto.failed').replace('{name}', x.name).replace('{err}', x.error), 'error');
  }
  const added = list.filter(x => x.installed);
  if (!added.length) return;
  const running = !!(State.serverStatus && State.serverStatus.running);
  const m = openModal({ title: t('missions.auto.title') });
  m.body.append(h('p', { i18n: 'missions.auto.text' }));
  for (const a of added) {
    m.body.append(h('div', { class: 'row', style: { gap: '10px', alignItems: 'center', margin: '8px 0' } }, [
      h('span', { class: 'mono', style: { flex: '1' }, text: a.name }),
      h('span', { class: 'hint', text: a.mod }),
      h('button', { class: 'primary', i18n: 'missions.activate', disabled: running, title: running ? t('mods.toggleRunning') : '',
        onclick: (e) => withBusy(e.currentTarget, async () => {
          try {
            await api.post('/api/missions/activate', { name: a.name });
            toast(t('missions.activated').replace('{name}', a.name), 'ok');
            m.close();
          } catch (err) { handleErr(err); }
        }) }),
    ]));
  }
  m.body.append(h('div', { class: 'actions' }, [
    h('button', { i18n: 'missions.auto.open', onclick: () => { m.close(); navigate('missions'); } }),
    h('button', { i18n: 'action.close', onclick: () => m.close() }),
  ]));
  applyI18n();
}

// reportModVehicles tells the admin that the vehicles of a mod just switched on
// (or installed/updated while on) were added to the mission — and that those
// of a mod switched off stopped spawning.
function reportModVehicles(r) {
  const list = (r && Array.isArray(r.vehicles)) ? r.vehicles : [];
  for (const x of list.filter(x => x.error)) toast(x.mod + ': ' + x.error, 'error');
  const added = list.flatMap(x => (x.added || []).concat(x.reactivated || []));
  if (added.length) {
    toastAction(t('vehicles.auto.added').replace('{n}', added.length).replace('{mods}', list.filter(x => (x.added || []).length || (x.reactivated || []).length).map(x => x.mod).join(', ')),
      'ok', t('nav.vehicles'), () => navigate('vehicles'));
  }
  const off = list.flatMap(x => x.deactivated || []);
  if (off.length) {
    toast(t('vehicles.auto.off').replace('{n}', off.length).replace('{mods}', list.filter(x => (x.deactivated || []).length).map(x => x.mod).join(', ')), 'ok');
  }
}

// pendingModMissionsBanner lists missions shipped inside installed mods that
// are not in mpmissions yet — e.g. a map mod uploaded by hand — with a
// one-click copy. Renders nothing when there are none.
function pendingModMissionsBanner() {
  const host = h('div');
  (async () => {
    let r;
    try { r = await api.get('/api/missions/available'); } catch { return; }
    const pending = (r.missions || []).filter(a => !a.installed && a.where === 'server');
    if (!pending.length) return;
    const bar = h('div', { class: 'info-bar pending-missions' });
    for (const a of pending) {
      const line = h('div', { class: 'row', style: { gap: '10px', alignItems: 'center', flexWrap: 'wrap' } }, [
        h('span', { style: { flex: '1 1 260px' }, text: t('missions.pending.text').replace('{mod}', a.mod).replace('{name}', a.name) }),
        h('button', { class: 'primary', i18n: 'missions.pending.copy', disabled: !!State.serverStatus.running,
          onclick: (e) => withBusy(e.currentTarget, async () => {
            try {
              await api.post('/api/missions/install', { mod: a.mod, path: a.path, zip: !!a.zip, inner: a.inner || '', activate: false, overwrite: false });
              toast(t('missions.pending.done').replace('{name}', a.name), 'ok');
              line.remove();
              if (!bar.querySelector('.row')) host.remove();
              reportModMissions({ missions: [{ mod: a.mod, name: a.name, installed: true }] });
            } catch (err) { handleErr(err); }
          }) }),
      ]);
      bar.append(line);
    }
    host.append(bar);
    applyI18n();
  })();
  return host;
}

