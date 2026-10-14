/*
  DayZ Server Manager — page layout: the Server page (map choice, serverDZ.cfg,
  launch parameters, performance) and Settings split into tabs.
  Copyright (c) 2026 Aristarh Ucolov. All rights reserved.

  Loaded after app.js / features.js / access.js and sharing their globals.
  Everything that configures the DayZ server itself lives on the Server page;
  everything about the manager lives in Settings, one topic per tab.
*/
'use strict';

// ---------------------------------------------------------------- shared

// configForm wires an auto-saving form for manager.json fields. Controls
// registered on F save 600 ms after the last change by posting ONLY their own
// keys — the backend merges, so a page can never reset fields it does not own.
// onSaved (optional) runs after each successful save, once State.config is fresh.
function configForm(onSaved) {
  const F = {};
  const savedNote = h('span', { class: 'saved-note', i18n: 'settings.saved' });
  const gather = () => {
    const next = {};
    for (const [k, el] of Object.entries(F)) {
      if (el.type === 'checkbox') next[k] = el.checked;
      else if (el.type === 'number') {
        // A field cleared mid-edit must not auto-save as 0 (serverPort 0 600 ms
        // after select-all-delete); omitting it keeps the old value.
        const v = String(el.value).trim();
        if (v === '' || Number.isNaN(Number(v))) continue;
        next[k] = Number(v);
      } else next[k] = el.value; // text/select + the custom list/radio getters
    }
    return next;
  };
  const save = async () => {
    const next = gather();
    if (!Object.keys(next).length) return;
    const langChanged = 'language' in next && next.language !== State.config.language;
    try {
      State.config = await api.post('/api/config', next);
      savedNote.classList.add('show');
      clearTimeout(save._t); save._t = setTimeout(() => savedNote.classList.remove('show'), 1500);
      if (langChanged) {
        await loadI18n(next.language);
        const ls = document.getElementById('lang-switch'); if (ls) ls.value = next.language;
      }
      if (onSaved) onSaved();
    } catch (e) { handleErr(e); }
  };
  let timer = 0;
  const autoSave = () => { clearTimeout(timer); timer = setTimeout(save, 600); };
  const body = h('div', { class: 'section-body' });
  body.addEventListener('change', (e) => {
    if (e.target && e.target.matches('input, select, textarea')) autoSave();
  });
  return { F, body, save, autoSave, savedNote };
}

// cardTitle: a card heading with an icon from the sprite, in the sidebar's style.
function cardTitle(icon, key, helpKey) {
  const t3 = h('h3', { class: 'card-title' }, [
    h('span', { class: 'card-title-icon', html: svgIcon(icon, 16) }),
    h('span', { i18n: key }),
  ]);
  return helpKey ? withHelp(t3, helpKey) : t3;
}

function field(labelKey, el, hintKey) {
  const id = assocId(el);
  return h('div', { class: 'field' }, [
    h('label', id ? { i18n: labelKey, for: id } : { i18n: labelKey }),
    el,
    hintKey ? h('small', { class: 'hint', i18n: hintKey }) : null,
  ].filter(Boolean));
}

function switchRow(el, labelKey, helpKey) {
  return h('label', { class: 'switch-row' }, [el, h('span', { i18n: labelKey }), helpKey ? help(helpKey) : null].filter(Boolean));
}

// ---------------------------------------------------------------- map thumbnails

// mapThumb draws a small, label-free picture of a world from its built-in
// schematic (land, relief, lakes, rivers) — or a neutral tile with the
// world's initial for maps without one.
function mapThumb(key, size, name) {
  const shape = (typeof MAP_SHAPES !== 'undefined' && key) ? MAP_SHAPES[key] : null;
  const wrap = h('div', { class: 'map-thumb' });
  if (!shape) {
    wrap.classList.add('placeholder');
    wrap.append(h('span', { class: 'map-thumb-initial', text: String(name || '?').trim().charAt(0).toUpperCase() || '?' }));
    return wrap;
  }
  const S = 100, sz = size > 0 ? size : 15360;
  const pt = (p) => (p[0] / sz * S).toFixed(1) + ' ' + (S - p[1] / sz * S).toFixed(1);
  const poly = (pts) => pts.map((p, i) => (i ? 'L' : 'M') + pt(p)).join(' ') + ' Z';
  const line = (pts) => pts.map((p, i) => (i ? 'L' : 'M') + pt(p)).join(' ');
  const svg = svgEl('svg', { viewBox: '0 0 100 100', preserveAspectRatio: 'xMidYMid slice', class: 'map-thumb-svg', 'aria-hidden': 'true', focusable: 'false' });
  const coast = shape.coast !== false;
  svgEl('rect', { x: 0, y: 0, width: S, height: S, class: coast ? 'mt-water' : 'mt-land' }, svg);
  if (shape.land) svgEl('path', { d: poly(shape.land), class: 'mt-land' }, svg);
  for (const isl of (shape.islands || [])) svgEl('path', { d: poly(isl), class: 'mt-land' }, svg);
  const bands = (shape.terrain || []).slice().sort((a, b) => (a.level || 1) - (b.level || 1));
  for (const b of bands) svgEl('path', { d: poly(b.poly), class: 'mt-hi' + (b.level || 1) }, svg);
  for (const lake of (shape.lakes || [])) svgEl('path', { d: poly(lake), class: 'mt-water' }, svg);
  for (const r of (shape.rivers || [])) svgEl('path', { d: line(r), class: 'mt-river' }, svg);
  wrap.append(svg);
  return wrap;
}

// mapCard — which map the server runs: every mission in mpmissions as a tile,
// the active one first; one click makes another one active.
function mapCard(full) {
  const running = !!State.serverStatus.running;
  const list = (full && full.missions) || [];
  const card = h('div', { class: 'card map-card' }, [
    h('div', { class: 'card-head' }, [
      cardTitle('i-missions', 'server.map.title', 'server.mission'),
      h('a', { class: 'btn-link', href: '#missions', i18n: 'server.map.manage' }),
    ]),
    h('p', { class: 'hint', i18n: 'server.map.hint' }),
  ]);
  if (!list.length) {
    card.append(h('p', { class: 'warning-bar', i18n: 'server.map.empty' }));
    return card;
  }
  const sorted = list.slice().sort((a, b) => (b.active - a.active) || a.name.localeCompare(b.name));
  card.append(h('div', { class: 'map-grid' }, sorted.map(m => missionTile(m, { route: 'server' }))));
  if (running) card.append(h('small', { class: 'hint', i18n: 'mods.toggleRunning' }));
  return card;
}

// missionTile is one installed mission as a tile: thumbnail, world, folder
// name, size, badges, then Activate / Duplicate and any opts.extra buttons.
// Shared by the Server page's map card and the Missions page (opts.route is
// the page re-rendered after a change).
function missionTile(m, opts = {}) {
  const running = !!State.serverStatus.running;
  const route = opts.route || 'server';
  const badges = [];
  if (m.active) badges.push(h('span', { class: 'badge ok', i18n: 'missions.active' }));
  if (!m.valid) badges.push(h('span', { class: 'badge warn', i18n: 'missions.invalid' }));
  if (m.fromMod && !m.modActive) badges.push(h('span', { class: 'badge warn', text: t('missions.modOff').replace('{mod}', m.fromMod) }));
  const actions = h('div', { class: 'map-tile-actions' });
  if (!m.active) {
    actions.append(h('button', { type: 'button', class: 'primary', i18n: 'missions.activate', disabled: running,
      onclick: (e) => activateMission(m, e.currentTarget, route) }));
  }
  actions.append(h('button', { type: 'button', class: 'secondary', i18n: 'missions.duplicate', disabled: running, onclick: async () => {
    const tgt = await promptModal(t('missions.duplicate.prompt'), { title: t('missions.duplicate'), value: m.name + '_copy' });
    if (!tgt) return;
    try {
      await api.post('/api/missions/duplicate', { source: m.name, target: tgt.trim() });
      toast(t('msg.duplicated'), 'ok');
      await navigate(route);
    } catch (err) { handleErr(err); }
  } }));
  for (const b of (opts.extra || [])) if (b) actions.append(b);
  return h('div', { class: 'map-tile' + (m.active ? ' active' : '') + (m.valid ? '' : ' invalid') }, [
    mapThumb(m.worldKey, m.worldSize, m.world),
    h('div', { class: 'map-tile-body' }, [
      h('div', { class: 'map-tile-world', text: m.world || m.name }),
      h('div', { class: 'map-tile-name mono', text: m.name, title: m.name }),
      h('div', { class: 'map-tile-meta', text: bytes(m.size) + (m.fromMod ? ' · ' + m.fromMod : '') }),
      badges.length ? h('div', { class: 'map-tile-badges' }, badges) : null,
      actions,
    ].filter(Boolean)),
  ]);
}

// activateMission makes m the active mission. A community map whose mod is
// switched off would not start, so that case asks first and offers Mods.
async function activateMission(m, btn, route) {
  if (m.fromMod && !m.modActive) {
    const choice = await modOffChoice(m);
    if (choice === 'mods') { navigate('mods'); return; }
    if (choice !== 'go') return;
  }
  await withBusy(btn, async () => {
    try {
      await api.post('/api/missions/activate', { name: m.name });
      toast(t('missions.activated').replace('{name}', m.name), 'ok');
      await navigate(route);
    } catch (err) { handleErr(err); }
  });
}

// modOffChoice resolves to 'mods' (open the Mods page), 'go' (activate
// anyway) or null (cancelled).
function modOffChoice(m) {
  return new Promise((resolve) => {
    let result = null;
    const md = openModal({ title: t('missions.activate') + ' ' + m.name, onClose: () => resolve(result) });
    const done = (v) => { result = v; md.close(); };
    const cancel = h('button', { type: 'button', i18n: 'action.cancel', onclick: () => done(null) });
    md.body.append(
      h('p', { text: t('missions.modOff.confirm').replace('{mod}', m.fromMod) }),
      h('div', { class: 'actions' }, [
        h('button', { type: 'button', class: 'primary', i18n: 'missions.modOff.openMods', onclick: () => done('mods') }),
        h('button', { type: 'button', class: 'danger', i18n: 'missions.modOff.anyway', onclick: () => done('go') }),
        cancel,
      ]),
    );
    setTimeout(() => cancel.focus(), 0);
  });
}

// ---------------------------------------------------------------- serverDZ.cfg

// The serverDZ.cfg keys the form edits, grouped by topic. kind: text | secret |
// number | bool (0/1 switch) | select (fixed values). help → hover-help key.
const CFG_GROUPS = [
  { id: 'general', icon: 'i-server', fields: [
    { k: 'hostname', kind: 'text', help: 'server.hostname', wide: true },
    { k: 'description', kind: 'text', wide: true },
    { k: 'password', kind: 'secret', help: 'server.password' },
    { k: 'passwordAdmin', kind: 'secret', help: 'server.passwordAdmin' },
    { k: 'maxPlayers', kind: 'number', min: 1, max: 127, help: 'server.maxPlayers' },
  ] },
  // Day/night lives on the Weather & time page (planner, presets, checks):
  // here only a summary with a link, so the two pages can't fight.
  { id: 'time', icon: 'i-weather', link: 'weather', fields: [] },
  // Ports a second server must not share (the game port is a launch
  // parameter below, RCon has its own page).
  { id: 'network', icon: 'i-servers', fields: [
    { k: 'steamQueryPort', kind: 'number', min: 1, max: 65535, help: 'server.steamQueryPort' },
  ] },
  { id: 'gameplay', icon: 'i-gameplay', fields: [
    { k: 'disable3rdPerson', kind: 'bool', help: 'server.thirdPerson' },
    { k: 'disableCrosshair', kind: 'bool' },
    { k: 'disablePersonalLight', kind: 'bool', gameplay: true },
    { k: 'lightingConfig', kind: 'select', options: ['0', '1'], gameplay: true },
    { k: 'disableVoN', kind: 'bool', help: 'server.von' },
    { k: 'vonCodecQuality', kind: 'number', min: 0, max: 30 },
  ] },
  { id: 'security', icon: 'i-battleye', fields: [
    { k: 'verifySignatures', kind: 'select', options: ['2', '0'], help: 'server.verifySignatures' },
    { k: 'forceSameBuild', kind: 'bool', help: 'server.forceSameBuild' },
    { k: 'enableWhitelist', kind: 'bool', help: 'server.whitelist' },
  ] },
  { id: 'storage', icon: 'i-missiondb', fields: [
    { k: 'instanceId', kind: 'number', min: 0, help: 'server.instanceId' },
    { k: 'storageAutoFix', kind: 'bool', help: 'server.storageAutoFix' },
    { k: 'shardId', kind: 'text' },
    { k: 'loginQueueConcurrentPlayers', kind: 'number', min: 1, help: 'server.loginQueue' },
    { k: 'loginQueueMaxPlayers', kind: 'number', min: 1, help: 'server.loginQueue' },
    { k: 'steamProtocolMaxDataSize', kind: 'number', min: 0 },
  ] },
  // Written into the .RPT log every N seconds (needs -doLogs); the dashboard
  // reads the server FPS from it.
  { id: 'logs', icon: 'i-logs', hint: 'server.logs.hint', fields: [
    { k: 'logAverageFps', kind: 'number', min: 0, ph: 'cfg.logs.ph' },
    { k: 'logMemory', kind: 'number', min: 0, ph: 'cfg.logs.ph' },
    { k: 'logPlayers', kind: 'number', min: 0, ph: 'cfg.logs.ph' },
  ] },
];

function serverCfgCard(data) {
  const values = (data && data.values) || {};
  const bag = {};
  // The backend saves into whatever file is configured at request time, so the
  // card must never save once the config file it was loaded from has changed.
  const loadedCfg = State.config.serverCfg || 'serverDZ.cfg';
  const card = h('div', { class: 'card', id: 'srv-cfg' }, [
    h('div', { class: 'card-head' }, [
      cardTitle('i-files', 'server.cfg.title'),
      loadedCfg.toLowerCase() === 'serverdz.cfg' ? null : h('span', { class: 'mono hint', text: loadedCfg }),
    ].filter(Boolean)),
    h('p', { class: 'hint', i18n: 'server.cfg.hint' }),
  ]);
  // With cfggameplay.json on, its copy of a few settings is the one DayZ
  // uses: say so on those fields and link there.
  const gameplayOn = String(values.enableCfgGameplayFile ?? '').trim() === '1';
  const overrideTag = (f) => (gameplayOn && f.gameplay) ? h('button', { type: 'button', class: 'link-btn cfg-override',
    title: t('server.cfg.overridden.hint'), text: t('server.cfg.overridden'),
    onclick: (e) => { e.preventDefault(); e.stopPropagation(); navigate('gameplay'); } }) : null;
  for (const g of CFG_GROUPS) {
    if (g.link) {
      // Summary + link (see CFG_GROUPS).
      const v = (k, d) => (values[k] ?? '') !== '' ? String(values[k]).trim() : d;
      const persistent = v('serverTimePersistent', '0') === '1';
      card.append(h('section', { class: 'cfg-group' }, [
        h('h4', { class: 'cfg-group-title' }, [h('span', { class: 'card-title-icon', html: svgIcon(g.icon, 15) }), h('span', { i18n: 'server.group.' + g.id })]),
        h('div', { class: 'cfg-summary' }, [
          h('span', { text: t('server.time.summary').replace('{day}', v('serverTimeAcceleration', '1')).replace('{night}', v('serverNightTimeAcceleration', '1'))
            .replace('{start}', v('serverTime', 'SystemTime')) + (persistent ? ' · ' + t('server.time.persistent') : '') }),
          h('button', { class: 'small secondary', type: 'button', i18n: 'server.time.edit', onclick: () => navigate(g.link) }),
        ]),
      ]));
      continue;
    }
    // Inputs first, then the on/off switches as their own block — mixing the
    // two in one grid made rows of uneven height.
    const grid = h('div', { class: 'cfg-grid' });
    const bools = h('div', { class: 'cfg-bools' });
    for (const f of g.fields) {
      const raw = values[f.k] ?? '';
      let input, cell;
      const keyTag = h('code', { class: 'cfg-key', text: f.k });
      if (f.kind === 'bool') {
        input = h('input', { type: 'checkbox', class: 'switch', 'data-key': f.k });
        input.checked = String(raw).trim() === '1' || String(raw).trim().toLowerCase() === 'true';
        bag[f.k] = { read: () => (input.checked ? 1 : 0) };
        bools.append(h('label', { class: 'cfg-bool' }, [
          input,
          h('span', { class: 'cfg-bool-text' }, [
            h('span', { class: 'cfg-bool-label' }, [h('span', { i18n: 'cfg.' + f.k }), f.help ? help(f.help) : null].filter(Boolean)),
            keyTag,
            overrideTag(f),
          ].filter(Boolean)),
        ]));
        continue;
      } else {
        if (f.kind === 'select') {
          input = h('select', { 'data-key': f.k }, f.options.map(o => h('option', { value: o, text: o + ' — ' + t('cfg.' + f.k + '.' + o) })));
          const cur = String(raw).trim();
          if (cur && !f.options.includes(cur)) input.append(h('option', { value: cur, text: cur }));
          input.value = cur || f.options[0];
        } else {
          input = h('input', {
            type: f.kind === 'number' ? 'number' : (f.kind === 'secret' ? 'password' : 'text'),
            value: raw, 'data-key': f.k, autocomplete: 'off', spellcheck: 'false',
            placeholder: f.ph ? t(f.ph) : '',
          });
          if (f.min !== undefined) input.min = String(f.min);
          if (f.max !== undefined) input.max = String(f.max);
          if (f.step) input.step = f.step;
        }
        bag[f.k] = { read: () => input.value.trim(), kind: f.kind };
        const id = assocId(input);
        const label = h('label', { for: id, i18n: 'cfg.' + f.k });
        let control = input;
        if (f.kind === 'secret') {
          const eye = h('button', { type: 'button', class: 'icon-btn eye-btn', title: t('rcon.showPassword'), 'aria-label': t('rcon.showPassword'), text: '👁', onclick: () => {
            input.type = input.type === 'password' ? 'text' : 'password';
          } });
          control = h('div', { class: 'secret-wrap' }, [input, eye]);
        }
        cell = h('div', { class: 'cfg-field' + (f.wide ? ' wide' : '') }, [
          h('div', { class: 'cfg-label' }, [f.help ? withHelp(label, f.help) : label, keyTag]),
          control,
          overrideTag(f),
        ].filter(Boolean));
      }
      grid.append(cell);
    }
    card.append(h('section', { class: 'cfg-group' }, [
      h('h4', { class: 'cfg-group-title' }, [h('span', { class: 'card-title-icon', html: svgIcon(g.icon, 15) }), h('span', { i18n: 'server.group.' + g.id })]),
      g.hint ? h('p', { class: 'hint cfg-group-hint', i18n: g.hint }) : null,
      grid.childElementCount ? grid : null,
      bools.childElementCount ? bools : null,
    ].filter(Boolean)));
  }
  const NUMERIC = new Set(CFG_GROUPS.flatMap(g => g.fields.filter(f => f.kind !== 'text' && f.kind !== 'secret').map(f => f.k)));
  // What the file holds now — edits are what differs from it.
  let saved = {};
  const snapshot = () => { saved = {}; for (const [k, b] of Object.entries(bag)) saved[k] = String(b.read()); };
  snapshot();
  const changedKeys = () => Object.keys(bag).filter(k => String(bag[k].read()) !== saved[k]);
  const saveCfg = async () => {
    // Another file was selected since this card loaded: its values belong to
    // the old file, so rebuild the page from the new one instead of saving.
    if ((State.config.serverCfg || 'serverDZ.cfg') !== loadedCfg) { await navigate('server'); return; }
    const keys = changedKeys();
    if (!keys.length) { toast(t('msg.saved'), 'ok'); return; }
    // Only what was changed here: a page open in another tab (or the Weather
    // page) must not be reverted to this page's old values.
    const patch = {};
    for (const k of keys) {
      const v = bag[k].read();
      // Coerce ONLY numeric keys — a numeric hostname or password must stay a string.
      patch[k] = (NUMERIC.has(k) && v !== '' && !isNaN(Number(v))) ? Number(v) : v;
    }
    try { await api.post('/api/servercfg', patch); snapshot(); updateBar(); toast(t('msg.saved'), 'ok'); }
    catch (e) { handleErr(e); }
  };
  const discard = () => navigate('server');
  // Sticky bar while something is unsaved — the Save button is far below.
  const barText = h('span');
  const bar = h('div', { class: 'sticky-save', hidden: true }, [
    barText,
    h('span', { class: 'grow' }),
    h('button', { class: 'secondary small', i18n: 'action.discard', onclick: discard }),
    h('button', { class: 'primary small', i18n: 'action.save', onclick: (e) => withBusy(e.currentTarget, saveCfg) }),
  ]);
  const updateBar = () => {
    const n = changedKeys().length;
    bar.hidden = n === 0;
    barText.textContent = t('server.cfg.unsaved').replace('{n}', n);
  };
  card.addEventListener('input', updateBar);
  card.addEventListener('change', updateBar);
  card.append(h('div', { class: 'actions' }, [
    h('button', { class: 'primary', i18n: 'action.save', onclick: (e) => withBusy(e.currentTarget, saveCfg) }),
  ]), bar);
  card._save = saveCfg;
  card._isDirty = () => changedKeys().length > 0;
  return card;
}

// launchCard — how the manager starts DayZServer: port, config file, cores,
// folders, launch flags, the server FPS limit and extra parameters. Manager
// settings (manager.json), auto-saved.
function launchCard(c, F) {
  const num = (k, v, extra) => { F[k] = h('input', Object.assign({ type: 'number', value: v ?? '' }, extra || {})); return F[k]; };
  const txt = (k, v, extra) => { F[k] = h('input', Object.assign({ type: 'text', value: v ?? '', spellcheck: 'false' }, extra || {})); return F[k]; };
  const sw = (k) => { F[k] = h('input', { type: 'checkbox', class: 'switch' }); F[k].checked = !!c[k]; return F[k]; };
  return h('div', { class: 'card', id: 'srv-launch' }, [
    cardTitle('i-rcon', 'server.launch.title'),
    h('p', { class: 'hint', i18n: 'server.launch.hint' }),
    h('div', { class: 'grid-3' }, [
      field('settings.serverPort', num('serverPort', c.serverPort, { min: '1', max: '65535' })),
      field('settings.limitFps', num('limitFPS', c.limitFPS || 0, { min: '0', max: '1000', step: '1' }), 'settings.limitFps.hint'),
      field('settings.serverCfg', txt('serverCfg', c.serverCfg)),
      field('settings.bepath', txt('bePath', c.bePath)),
      field('settings.profilesDir', txt('profilesDir', c.profilesDir)),
    ]),
    h('h4', { class: 'sub-title', i18n: 'settings.flags' }),
    h('div', { class: 'grid-3 flags-grid' }, [
      switchRow(sw('doLogs'), 'settings.flag.dologs'),
      switchRow(sw('adminLog'), 'settings.flag.adminlog', 'settings.adminlog'),
      switchRow(sw('netLog'), 'settings.flag.netlog'),
      switchRow(sw('freezeCheck'), 'settings.flag.freezecheck'),
      switchRow(sw('filePatching'), 'settings.flag.filePatching'),
    ]),
    field('settings.extraParams', txt('extraLaunchParams', c.extraLaunchParams, { placeholder: '-scrAllowFileWrite' }), 'settings.extraParams.hint'),
  ]);
}

Views.server = async (root) => {
  const myNav = _navSeq;
  const shownCfg = State.config.serverCfg;
  // A new "Server config file" leaves the serverDZ.cfg card showing (and able
  // to save) the old file's values — redraw the page from the new file.
  const form = configForm(() => {
    if (myNav === _navSeq && State.config.serverCfg !== shownCfg) { navigate('server'); return; }
    // The header's "Saved" note is far above the launch and CPU cards.
    if (window.scrollY > 400) toast(t('msg.saved'), 'ok');
  });
  root.append(pageHeader('nav.server', 'server.subtitle', [form.savedNote, revealBtn(() => ({ path: State.config.serverCfg || 'serverDZ.cfg' }))]));
  if (State.serverStatus.running) root.append(runningBanner());
  // A missing or unreadable config file must not take the whole page down: the
  // field that selects it lives here too.
  const [data, full] = await Promise.all([
    api.get('/api/servercfg').catch(e => ({ error: String(e && e.message || e) })),
    api.get('/api/missions/full').catch(() => ({ missions: [] })),
  ]);
  if (myNav !== _navSeq) return;
  const c = State.config;
  const cfgCard = data.error
    ? h('div', { class: 'card' }, [
      cardTitle('i-files', 'server.cfg.title'),
      h('p', { class: 'hint', text: data.error }),
    ])
    : serverCfgCard(data);
  const mc = mapCard(full);
  mc.id = 'srv-map';
  const pc = performanceCard(c, form.F);
  pc.id = 'srv-perf';
  // A long page: a bar of shortcuts to its parts.
  root.append(pageAnchors([['srv-map', 'server.anchor.map'], ['srv-cfg', 'server.anchor.cfg'], ['srv-launch', 'server.anchor.launch'], ['srv-perf', 'server.anchor.perf']]));
  form.body.append(mc, cfgCard, launchCard(c, form.F), pc);
  // The serverDZ.cfg card saves on its own button; keep its edits out of the
  // manager.json auto-save.
  cfgCard.addEventListener('change', (e) => e.stopPropagation());
  root.append(form.body);
  // The card saves first, while serverCfg still names the file it came from.
  root._save = async () => { if (cfgCard._save) await cfgCard._save(); await form.save(); };
  root._isDirty = () => !!(cfgCard._isDirty && cfgCard._isDirty());
};

// pageAnchors is a row of shortcuts to the cards of a long page.
function pageAnchors(items) {
  return h('nav', { class: 'page-anchors', 'aria-label': t('a11y.sections') }, items.map(([id, key]) =>
    h('button', { type: 'button', class: 'anchor-chip', i18n: key, onclick: () => {
      const el = document.getElementById(id);
      if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
    } })));
}

// ---------------------------------------------------------------- settings tabs

const SETTINGS_TABS = [
  { id: 'general', icon: 'i-settings', key: 'settings.tab.general' },
  { id: 'access', icon: 'i-shield', key: 'settings.tab.access' },
  { id: 'restarts', icon: 'i-clock', key: 'settings.tab.restarts' },
  { id: 'backups', icon: 'i-archive', key: 'settings.tab.backups' },
  { id: 'discord', icon: 'i-chat', key: 'settings.tab.discord' },
];

function settingsTabBar(active) {
  const bar = h('nav', { class: 'section-tabs', role: 'tablist' }, SETTINGS_TABS.map(tb => h('a', {
    href: '#settings' + (tb.id === 'general' ? '' : '/' + tb.id),
    role: 'tab',
    class: 'section-tab' + (tb.id === active ? ' active' : ''),
    'aria-selected': String(tb.id === active),
    onclick: (e) => { e.preventDefault(); navigate(tb.id === 'general' ? 'settings' : 'settings/' + tb.id); },
  }, [h('span', { class: 'section-tab-icon', html: svgIcon(tb.icon, 15) }), h('span', { i18n: tb.key })])));
  // On a phone the bar scrolls sideways — keep the open tab in view. Only the
  // strip scrolls: scrollIntoView also shifted the whole window sideways.
  const reveal = (tries) => {
    if (!bar.isConnected) { if (tries > 0) setTimeout(() => reveal(tries - 1), 50); return; }
    const cur = bar.querySelector('.section-tab.active');
    if (cur && bar.scrollWidth > bar.clientWidth) {
      const br = bar.getBoundingClientRect(), cr = cur.getBoundingClientRect();
      bar.scrollLeft += (cr.left + cr.width / 2) - (br.left + br.width / 2);
    }
  };
  requestAnimationFrame(() => reveal(10));
  return bar;
}

function generalTab(c, form) {
  const { F } = form;
  F.language = h('select', { 'aria-label': t('settings.language') });
  fillLangSelect(F.language, c.language, false);
  const themeSel = h('select', { 'aria-label': t('settings.theme') }, [
    h('option', { value: 'dark', i18n: 'settings.theme.dark' }),
    h('option', { value: 'light', i18n: 'settings.theme.light' }),
    h('option', { value: 'auto', i18n: 'settings.theme.auto' }),
  ]);
  themeSel.value = localStorage.getItem('theme') || 'dark';
  // A per-browser preference, not manager config: keep it out of the auto-save.
  themeSel.addEventListener('change', (e) => { e.stopPropagation(); setTheme(themeSel.value); });

  F.vanillaDayZPath = h('input', { type: 'text', value: c.vanillaDayZPath || '', 'aria-label': t('settings.vanillaPath') });
  F.modsFolderPath = h('input', { type: 'text', value: c.modsFolderPath || '', placeholder: '…/steamapps/workshop/content/221100', 'aria-label': t('settings.modsFolder') });
  const detectBtn = h('button', { type: 'button', i18n: 'settings.detectDayZ', onclick: async () => {
    try {
      const d = await api.get('/api/steam/detect');
      const installs = d.installs || [];
      if (!installs.length) { toast(t('settings.detectDayZ.none')); return; }
      const hit = installs.find(i => i.hasWorkshop) || installs[0];
      F.vanillaDayZPath.value = hit.path;
      F.vanillaDayZPath.dispatchEvent(new Event('change', { bubbles: true }));
      toast(hit.path, 'ok');
    } catch (e) { handleErr(e); }
  } });

  const sw = (k) => { F[k] = h('input', { type: 'checkbox', class: 'switch' }); F[k].checked = !!c[k]; return F[k]; };
  F.autoUpdateCheckMinutes = h('input', { type: 'number', min: '0', value: c.autoUpdateCheckMinutes ?? 0 });

  return [
    h('div', { class: 'card' }, [
      cardTitle('i-settings', 'settings.title'),
      h('div', { class: 'grid-2' }, [field('settings.language', F.language), field('settings.theme', themeSel)]),
    ]),
    modSourceCard(c, F, detectBtn),
    h('div', { class: 'card' }, [
      cardTitle('i-mods', 'settings.autoupdate.title'),
      switchRow(sw('autoUpdateModsOnRestart'), 'settings.autoupdate.onRestart'),
      h('small', { class: 'hint', i18n: 'settings.autoupdate.onRestart.hint' }),
      switchRow(sw('autoInstallMissions'), 'settings.autoInstallMissions'),
      h('small', { class: 'hint', i18n: 'settings.autoInstallMissions.hint' }),
      switchRow(autoVehiclesSwitch(c, F), 'settings.autoModVehicles'),
      h('small', { class: 'hint', i18n: 'settings.autoModVehicles.hint' }),
      field('settings.autoupdate.check', F.autoUpdateCheckMinutes, 'settings.autoupdate.check.hint'),
    ]),
    managerCard(),
  ];
}

// autoVehiclesSwitch: "add vehicles from mods automatically" — stored inverted
// (autoModVehiclesOff), so the switch reads the way the admin thinks of it.
function autoVehiclesSwitch(c, F) {
  const cb = h('input', { type: 'checkbox', class: 'switch' });
  cb.checked = !c.autoModVehiclesOff;
  F.autoModVehiclesOff = { type: 'inverted', get value() { return !cb.checked; } };
  return cb;
}

function accessTab(c, form) {
  const { F } = form;
  F.exposure = h('select', {}, [
    h('option', { value: 'local', i18n: 'settings.exposure.local' }),
    h('option', { value: 'lan', i18n: 'settings.exposure.lan' }),
    h('option', { value: 'internet', i18n: 'settings.exposure.internet' }),
  ]);
  F.exposure.value = c.exposure || 'local';
  // Internet exposure needs a panel login first — say so instead of letting
  // the auto-save bounce off the server.
  F.exposure.addEventListener('change', (e) => {
    if (F.exposure.value === 'internet' && !(State.info && State.info.auth && State.info.auth.enabled)) {
      e.stopPropagation();
      F.exposure.value = State.config.exposure || 'local';
      toast(t('auth.err.internetNeedsLogin'), 'error');
      const card = document.getElementById('auth-card');
      if (card) { card.scrollIntoView({ behavior: 'smooth', block: 'center' }); card.classList.add('flash-target'); setTimeout(() => card.classList.remove('flash-target'), 1600); }
    }
  });
  const id = assocId(F.exposure);
  return [
    h('div', { class: 'card', id: 'settings-exposure' }, [
      cardTitle('i-livemap', 'settings.exposure', 'settings.exposure'),
      h('label', { for: id, class: 'sr-only', i18n: 'settings.exposure' }),
      F.exposure,
      h('small', { class: 'hint', i18n: 'settings.exposure.hint' }),
    ]),
    authCard(),
    remoteAccessCard(c, F),
  ];
}

function restartsTab(c, form) {
  const { F, autoSave } = form;
  const sw = (k) => { F[k] = h('input', { type: 'checkbox', class: 'switch' }); F[k].checked = !!c[k]; return F[k]; };
  F.autoRestartSeconds = h('input', { type: 'number', min: '0', value: c.autoRestartSeconds });
  F.restartEmptyMaxWaitMinutes = h('input', { type: 'number', min: '0', value: c.restartEmptyMaxWaitMinutes ?? 60 });
  F.restartMemThresholdMB = h('input', { type: 'number', min: '0', value: c.restartMemThresholdMB ?? 8000 });
  return [
    h('div', { class: 'card' }, [
      cardTitle('i-refresh', 'settings.autorestart.title', 'settings.restart'),
      h('div', { class: 'grid-2' }, [
        h('div', { class: 'field' }, [switchRow(sw('autoRestartEnabled'), 'settings.autorestart.enable')]),
        field('settings.autorestart', F.autoRestartSeconds),
      ]),
      h('p', { class: 'hint', i18n: 'settings.autorestart.hint' }),
      switchRow(sw('restartOnCrash'), 'settings.watchdog', 'settings.watchdog'),
      h('small', { class: 'hint', i18n: 'settings.watchdog.hint' }),
      h('h4', { class: 'sub-title', i18n: 'settings.smartrestart.title' }),
      switchRow(sw('restartOnlyWhenEmpty'), 'settings.smartrestart.empty'),
      h('small', { class: 'hint', i18n: 'settings.smartrestart.empty.hint' }),
      field('settings.smartrestart.maxwait', F.restartEmptyMaxWaitMinutes),
      switchRow(sw('restartOnHighMem'), 'settings.smartrestart.mem'),
      h('small', { class: 'hint', i18n: 'settings.smartrestart.mem.hint' }),
      field('settings.smartrestart.memthresh', F.restartMemThresholdMB),
    ]),
    scheduledRestartsCard(c, F, autoSave),
    restartBehaviourCard(c, F, autoSave),
    scheduledProfilesCard(c, F, autoSave),
    announcementsCard(c, F, autoSave),
    intervalAnnouncementsCard(c, F, autoSave),
  ];
}

function backupsTab(c, form) {
  const { F } = form;
  F.backupIntervalHours = h('input', { type: 'number', min: '0', value: c.backupIntervalHours ?? 0 });
  F.backupKeep = h('input', { type: 'number', min: '1', value: c.backupKeep || 10 });
  return [
    h('div', { class: 'card' }, [
      cardTitle('i-archive', 'settings.autobackup.title', 'settings.backup'),
      h('p', { class: 'hint', i18n: 'settings.autobackup.hint' }),
      h('div', { class: 'grid-2' }, [field('settings.autobackup.every', F.backupIntervalHours), field('settings.autobackup.keep', F.backupKeep)]),
      h('div', { class: 'actions' }, [
        h('button', { i18n: 'settings.autobackup.now', onclick: (e) => withBusy(e.currentTarget, async () => {
          try { const r = await api.post('/api/backup/run', {}); toast(`${t('msg.saved')} — ${r.path}`, 'ok'); }
          catch (err) { handleErr(err); }
        }) }),
      ]),
    ]),
    backupCard(),
  ];
}

// discordTab: notifications to a Discord channel — webhook, which events, who
// is pinged on alerts, the bot's name and language, the live status message.
function discordTab(c, form) {
  const { F } = form;
  F.discordEnabled = h('input', { type: 'checkbox', class: 'switch' });
  F.discordEnabled.checked = !!c.discordEnabled;
  F.discordWebhookURL = h('input', { type: 'text', value: c.discordWebhookURL || '', placeholder: 'https://discord.com/api/webhooks/…', spellcheck: 'false', 'aria-label': t('settings.discord.url') });
  F.discordPlayersWebhookURL = h('input', { type: 'text', value: c.discordPlayersWebhookURL || '', placeholder: 'https://discord.com/api/webhooks/…', spellcheck: 'false', 'aria-label': t('discord.webhook.players') });
  F.discordMention = h('input', { type: 'text', value: c.discordMention || '', placeholder: 'here / everyone / 123456789012345678', spellcheck: 'false', 'aria-label': t('discord.mention') });
  F.discordUsername = h('input', { type: 'text', maxlength: '80', value: c.discordUsername || '', placeholder: 'DayZ Server', 'aria-label': t('discord.username') });
  F.discordAvatarURL = h('input', { type: 'text', value: c.discordAvatarURL || '', placeholder: 'https://…/logo.png', spellcheck: 'false', 'aria-label': t('discord.avatar') });
  F.discordLanguage = h('select', { 'aria-label': t('discord.language') });
  F.discordLanguage.append(h('option', { value: '', i18n: 'discord.language.panel' }));
  for (const l of (State.languages || [])) F.discordLanguage.append(h('option', { value: l.code, text: l.name }));
  F.discordLanguage.value = c.discordLanguage || '';
  F.discordStatus = h('input', { type: 'checkbox', class: 'switch' });
  F.discordStatus.checked = !!c.discordStatus;

  // Events: a map {key: on/off}, gathered by the form like any field.
  const events = Object.assign({}, c.discordEvents || {});
  const boxes = {};
  F.discordEvents = { type: 'map', get value() {
    // Not loaded yet: send nothing, so a quick edit elsewhere keeps the
    // saved switches instead of resetting them to the defaults.
    if (!Object.keys(boxes).length) return undefined;
    const out = {};
    for (const [k, cb] of Object.entries(boxes)) out[k] = cb.checked;
    return out;
  } };
  const eventsHost = h('div', { class: 'dc-events' }, h('p', { class: 'hint', i18n: 'msg.loading' }));
  // What the pointed-at event's message looks like in the channel: its emoji,
  // colour, title, the ping on alerts and the footer.
  const preview = h('div', { class: 'dc-preview' });
  const previewWrap = h('div', { class: 'dc-preview-wrap' }, [
    h('div', { class: 'dc-preview-label' }, [h('span', { i18n: 'discord.preview' }), h('small', { class: 'hint', i18n: 'discord.preview.hint' })]),
    preview,
  ]);
  const sample = {
    join: () => t('discord.line.join').replace('{name}', 'Survivor'),
    leave: () => t('discord.line.leave').replace('{name}', 'Survivor'),
    watch: () => t('discord.line.watch').replace('{name}', 'Survivor'),
    kill: () => t('discord.line.kill').replace('{killer}', 'Survivor').replace('{victim}', 'Bandit'),
    restartplanned: () => t('discord.restartplanned.in').replace('{m}', '5'),
  };
  const showPreview = (ev) => {
    const name = F.discordUsername.value.trim() || 'DayZ Server';
    const avatar = F.discordAvatarURL.value.trim();
    const ping = ev.alert ? F.discordMention.value.trim() : '';
    const srv = (Srv.current && Srv.current()) ? Srv.current().name : '';
    const at = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    preview.style.setProperty('--dc', ev.color || 'var(--accent)');
    preview.replaceChildren(
      /^https:\/\//.test(avatar)
        ? h('img', { class: 'dc-pv-avatar', src: avatar, alt: '', referrerpolicy: 'no-referrer', onerror: (e) => { e.currentTarget.replaceWith(h('span', { class: 'dc-pv-avatar', text: name.charAt(0).toUpperCase() })); } })
        : h('span', { class: 'dc-pv-avatar', 'aria-hidden': 'true', text: name.charAt(0).toUpperCase() }),
      h('div', { class: 'dc-pv-body' }, [
        h('div', { class: 'dc-pv-head' }, [h('b', { text: name }), h('span', { class: 'dc-pv-time', text: at })]),
        ping ? h('div', { class: 'dc-pv-ping', text: /^\d+$/.test(ping) ? '@role' : '@' + ping.replace(/^@/, '') }) : null,
        h('div', { class: 'dc-pv-embed' }, [
          h('div', { class: 'dc-pv-title', text: t('discord.' + ev.key) }),
          sample[ev.key] ? h('div', { class: 'dc-pv-desc', text: sample[ev.key]() }) : null,
          h('div', { class: 'dc-pv-foot', text: (srv ? srv + ' · ' : '') + 'DayZ Server Manager' }),
        ].filter(Boolean)),
      ].filter(Boolean)),
    );
  };
  (async () => {
    let cat;
    try { cat = await api.get('/api/discord/catalog'); } catch (e) { eventsHost.textContent = e.message; return; }
    eventsHost.innerHTML = '';
    // Server's long list on the left; players, moderation and the preview
    // stacked on the right, so the two columns end about level.
    const cols = [h('div', { class: 'dc-col' }), h('div', { class: 'dc-col' })];
    eventsHost.append(...cols);
    const groupIcon = { server: 'i-server', players: 'i-players', admin: 'i-shield' };
    for (const group of ['server', 'players', 'admin']) {
      const items = (cat.events || []).filter(e => e.group === group);
      const count = h('span', { class: 'dc-count' });
      const recount = () => {
        const on = items.filter(ev => boxes[ev.key].checked).length;
        count.textContent = on + '/' + items.length;
        count.classList.toggle('none', on === 0);
      };
      const col = h('div', { class: 'dc-group' }, [h('div', { class: 'dc-group-head' }, [
        h('span', { class: 'card-title-icon', html: svgIcon(groupIcon[group], 15) }),
        h('h4', { class: 'sub-title', i18n: 'discord.group.' + group }), count,
      ])]);
      for (const ev of items) {
        const cb = h('input', { type: 'checkbox', class: 'switch', onchange: recount });
        cb.checked = (ev.key in events) ? !!events[ev.key] : !!ev.default;
        boxes[ev.key] = cb;
        const row = h('label', { class: 'dc-ev', style: { '--dc': ev.color || 'var(--accent)' },
          onmouseenter: () => showPreview(ev), onfocusin: () => showPreview(ev) }, [
          h('span', { class: 'dc-ico', 'aria-hidden': 'true', text: ev.icon || '•' }),
          h('span', { class: 'dc-ev-name', i18n: 'discord.ev.' + ev.key }),
          ev.alert ? h('span', { class: 'badge warn dc-tag', i18n: 'discord.alertTag', title: t('discord.mention.hint') }) : null,
          cb,
        ].filter(Boolean));
        col.append(row);
      }
      recount();
      cols[group === 'server' ? 0 : 1].append(col);
    }
    cols[1].append(previewWrap);
    const first = (cat.events || [])[0];
    if (first) showPreview(first);
    applyI18n();
  })();

  const testBtn = (key, input) => h('button', { class: 'secondary', i18n: key, onclick: (e) => withBusy(e.currentTarget, async () => {
    const url = input.value.trim();
    if (!url) { toast(t('discord.needUrl'), 'error'); return; }
    try {
      await api.post('/api/discord/test', { url, mention: F.discordMention.value.trim(), username: F.discordUsername.value.trim(),
        avatar: F.discordAvatarURL.value.trim(), language: F.discordLanguage.value });
      toast(t('msg.sent'), 'ok');
    } catch (err) { handleErr(err); }
  }) });

  return [
    h('div', { class: 'card' }, [
      cardTitle('i-chat', 'settings.discord.title'),
      h('p', { class: 'hint', i18n: 'settings.discord.hint' }),
      switchRow(F.discordEnabled, 'settings.discord.enable'),
      field('settings.discord.url', F.discordWebhookURL),
      h('div', { class: 'actions' }, [testBtn('settings.discord.test', F.discordWebhookURL)]),
      field('discord.webhook.players', F.discordPlayersWebhookURL, 'discord.webhook.players.hint'),
      h('div', { class: 'actions' }, [testBtn('discord.testPlayers', F.discordPlayersWebhookURL)]),
    ]),
    h('div', { class: 'card' }, [
      cardTitle('i-events', 'discord.events'),
      h('p', { class: 'hint', i18n: 'discord.events.hint' }),
      eventsHost,
    ]),
    h('div', { class: 'card' }, [
      cardTitle('i-settings', 'discord.look'),
      h('div', { class: 'grid-2' }, [
        field('discord.mention', F.discordMention, 'discord.mention.hint'),
        field('discord.language', F.discordLanguage),
        field('discord.username', F.discordUsername),
        field('discord.avatar', F.discordAvatarURL, 'discord.avatar.hint'),
      ]),
    ]),
    h('div', { class: 'card' }, [
      cardTitle('i-dashboard', 'discord.status'),
      switchRow(F.discordStatus, 'discord.status.enable'),
      h('small', { class: 'hint', i18n: 'discord.status.hint' }),
    ]),
  ];
}

// settingsScope says what a settings tab applies to when the panel runs more
// than one server: the server this tab manages, or the whole panel.
function settingsScope(tab) {
  if (!Srv.list || Srv.list.length < 2) return null;
  const cur = Srv.current();
  const name = cur ? cur.name : '';
  if (tab === 'access') return h('div', { class: 'scope-note panel', i18n: 'settings.scope.panel' });
  const key = tab === 'general' ? 'settings.scope.mixed' : 'settings.scope.server';
  return h('div', { class: 'scope-note' }, [h('span', { text: t(key).replace('{name}', name) })]);
}

async function renderSettings(root, tab) {
  const c = State.config;
  const form = configForm();
  root.append(pageHeader('nav.settings', 'settings.tab.' + tab + '.subtitle', [form.savedNote]));
  root.append(settingsTabBar(tab));
  const scope = settingsScope(tab);
  if (scope) root.append(scope);
  const build = { general: generalTab, access: accessTab, restarts: restartsTab, backups: backupsTab, discord: discordTab }[tab] || generalTab;
  form.body.append(...build(c, form).filter(Boolean));
  root.append(form.body);
  root._save = form.save; // Ctrl+S saves immediately
}

Views.settings = (root) => renderSettings(root, 'general');
for (const tb of SETTINGS_TABS) {
  if (tb.id !== 'general') Views['settings/' + tb.id] = (root) => renderSettings(root, tb.id);
}
