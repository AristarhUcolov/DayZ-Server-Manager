// Copyright (c) 2026 Aristarh Ucolov.
//
// Contaminated zones on the map: the static gas zones and the safe positions
// of cfgeffectarea.json (click to add, drag to move, drag the edge handle to
// resize, right-click to delete), the dynamic gas zones — an events.xml event
// and its cfgeventspawns.xml spots — and the zones' loot, the types with the
// ContaminatedArea usage. Backend: internal/web/zones.go. The file is edited
// as the JSON it is, so fields this page does not know are kept.

// Friendly names of the zone fields DayZ knows; any other field is shown
// under its own name.
const ZONE_FIELDS = ['PosHeight', 'NegHeight', 'InnerRingCount', 'InnerPartDist', 'OuterRingToggle', 'OuterPartDist',
  'OuterOffset', 'VerticalLayers', 'VerticalOffset', 'ParticleName', 'EffectInterval', 'EffectDuration', 'EffectModifier'];
const ZONE_PLAYER_FIELDS = ['AroundPartName', 'TinyPartName', 'PPERequesterType'];

// A zone as vanilla Chernarus writes one, for a mission that has none yet.
const ZONE_TEMPLATE = {
  AreaName: '', Type: 'ContaminatedArea_Static', TriggerType: 'ContaminatedTrigger',
  Data: { Pos: [0, 0, 0], Radius: 100, PosHeight: 25, NegHeight: 10, InnerPartDist: 80, OuterOffset: 30,
    ParticleName: 'graphics/particles/contaminated_area_gas_bigass' },
  PlayerData: { AroundPartName: 'graphics/particles/contaminated_area_gas_around',
    TinyPartName: 'graphics/particles/contaminated_area_gas_around_tiny', PPERequesterType: 'PPERequester_ContaminatedAreaTint' },
};

const zoneIsGas = (a) => /contaminat/i.test(a.Type || '') || !a.Type;

Views.zones = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.zones', 'zones.subtitle', [revealBtn({ kind: 'missionfile', path: 'cfgeffectarea.json' })]));
  let d;
  try { d = await api.get('/api/zones'); }
  catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;
  const running = !!d.running;
  if (running) root.append(runningBanner());
  if (d.invalid) {
    root.append(h('div', { class: 'warning-bar err-bar' }, [
      h('span', { class: 'grow', i18n: 'zones.invalid' }),
      h('button', { type: 'button', class: 'secondary small', i18n: 'nav.missionDb', onclick: () => navigate('missiondb') }),
    ]));
  }

  let doc = d.doc ? JSON.parse(JSON.stringify(d.doc)) : { Areas: [], SafePositions: [] };
  if (!Array.isArray(doc.Areas)) doc.Areas = [];
  let saved = JSON.stringify(doc);
  const isDirty = () => JSON.stringify(doc) !== saved;
  root._isDirty = isDirty;
  const areas = () => doc.Areas;
  const safe = () => doc.SafePositions || (doc.SafePositions = []);
  let sel = -1;           // selected zone
  let mode = 'select';    // select | zone | safe
  let showDyn = true;

  const gm = buildGameMap(d.map, { onImageChange: () => navigate('zones') });
  gm.svg.classList.add('gm-edit', 'zones-map');
  const size = gm.size;
  const toS = (m) => m / size * gm.S; // metres → map units
  const readout = h('div', { class: 'map-readout' });

  // ---- the save bar
  const barText = h('span', { i18n: 'zones.unsaved' });
  const saveBtn = h('button', { type: 'button', class: 'primary small', i18n: 'action.save', disabled: running, onclick: (e) => withBusy(e.currentTarget, save) });
  const bar = h('div', { class: 'sticky-save', hidden: true }, [
    barText, h('span', { class: 'grow' }),
    h('button', { type: 'button', class: 'secondary small', i18n: 'action.discard', onclick: async () => {
      if (await confirmModal(t('zones.discard'), { danger: true, okText: t('action.discard') })) {
        doc = JSON.parse(saved); sel = -1; changed();
      }
    } }),
    saveBtn,
  ]);
  async function save() {
    try {
      await api.post('/api/zones/save', { doc });
      saved = JSON.stringify(doc);
      changed();
      toast(t('msg.saved'), 'ok');
    } catch (err) { handleErr(err); }
  }
  root._save = () => (isDirty() ? save() : undefined);

  // ---- toolbar: what a click on the map does
  const modeBtn = (id, icon) => h('button', { type: 'button', class: 'seg-btn' + (mode === id ? ' active' : ''), 'data-mode': id,
    onclick: () => { mode = id; for (const b of modes.children) b.classList.toggle('active', b.dataset.mode === id); gm.svg.dataset.mode = id; } },
  [h('span', { html: svgIcon(icon, 14) }), h('span', { i18n: 'zones.mode.' + id })]);
  const modes = h('div', { class: 'seg zones-modes', role: 'group' }, [modeBtn('select', 'i-edit'), modeBtn('zone', 'i-plus'), modeBtn('safe', 'i-shield')]);
  gm.svg.dataset.mode = mode;
  const dynSw = h('input', { type: 'checkbox', class: 'switch' });
  dynSw.checked = showDyn;
  dynSw.onchange = () => { showDyn = dynSw.checked; render(); };
  const counts = h('span', { class: 'hint' });

  const side = h('aside', { class: 'card zones-side' });
  const mapCard = h('div', { class: 'card zones-mapcard' }, [
    h('div', { class: 'map-toolbar' }, [
      modes,
      (d.dynamic || []).length ? h('label', { class: 'inline-switch' }, [dynSw, h('span', { i18n: 'zones.showDynamic' })]) : null,
      h('div', { class: 'grow' }), counts, readout,
    ].filter(Boolean)),
    h('p', { class: 'hint', i18n: 'zones.help' }),
    h('div', { class: 'map-wrap' }, [gm.svg, gm.controls]),
  ]);
  root.append(h('div', { class: 'zones-layout' }, [mapCard, side]), bar);
  mapCoordReadout(gm, readout);

  // ---- adding with a click on the map
  gm.svg.addEventListener('click', (e) => {
    if (running || mode === 'select') {
      if (mode === 'select' && sel >= 0 && !e.target.closest('.zone-g')) { sel = -1; render(); renderSide(); }
      return;
    }
    const [wx, wz] = clientToWorld(gm, e);
    if (wx < 0 || wz < 0 || wx > size || wz > size) return;
    if (mode === 'zone') {
      const base = areas().find(a => a.Type === 'ContaminatedArea_Static') || ZONE_TEMPLATE;
      const a = JSON.parse(JSON.stringify(base));
      a.AreaName = newZoneName();
      a.Data = a.Data || {};
      a.Data.Pos = [Math.round(wx), 0, Math.round(wz)];
      if (!(a.Data.Radius > 0)) a.Data.Radius = 100;
      areas().push(a);
      sel = areas().length - 1;
      mode = 'select';
      for (const b of modes.children) b.classList.toggle('active', b.dataset.mode === 'select');
      gm.svg.dataset.mode = mode;
    } else {
      safe().push([Math.round(wx), Math.round(wz)]);
    }
    changed();
  });
  function newZoneName() {
    const taken = new Set(areas().map(a => String(a.AreaName || '').toLowerCase()));
    for (let i = areas().length + 1; ; i++) {
      const n = t('zones.newName').replace('{n}', i);
      if (!taken.has(n.toLowerCase())) return n;
    }
  }

  // ---- drawing
  function render() {
    gm.layer.innerHTML = '';
    const dot = gm.ptR(4), hr = gm.ptR(6);
    // Dynamic spots first, under everything: where the economy may drop gas.
    if (showDyn) {
      for (const dz of d.dynamic || []) {
        const r = toS(dz.radius || 75);
        for (const p of dz.positions || []) {
          const [sx, sy] = gm.w2s(p.x, p.z);
          const g = svgEl('g', { class: 'zone-dyn' }, gm.layer);
          svgEl('circle', { cx: sx, cy: sy, r, class: 'zone-dyn-c' }, g);
          svgEl('circle', { cx: sx, cy: sy, r: dot * 0.7, class: 'zone-dyn-dot' }, g);
        }
      }
    }
    areas().forEach((a, i) => {
      const pos = (a.Data && a.Data.Pos) || [];
      if (pos.length < 3) return;
      const [sx, sy] = gm.w2s(pos[0], pos[2]);
      const r = toS(Number(a.Data.Radius) || 0);
      const g = svgEl('g', { class: 'zone-g' + (i === sel ? ' sel' : '') + (zoneIsGas(a) ? '' : ' other') }, gm.layer);
      const disc = svgEl('circle', { cx: sx, cy: sy, r: Math.max(r, dot), class: 'zone-c' }, g);
      const label = svgEl('text', { x: sx, y: sy - Math.max(r, dot) - gm.px(4), class: 'zone-label', 'text-anchor': 'middle', 'font-size': gm.px(11) }, g);
      label.textContent = a.AreaName || '';
      const centre = svgEl('circle', { cx: sx, cy: sy, r: i === sel ? hr : dot, class: 'zone-centre' }, g);
      mapPointDrag(gm, g, {
        locked: () => running,
        onMove: (wx, wz) => {
          pos[0] = Math.round(wx); pos[2] = Math.round(wz);
          const [nx, ny] = gm.w2s(pos[0], pos[2]);
          disc.setAttribute('cx', nx); disc.setAttribute('cy', ny);
          centre.setAttribute('cx', nx); centre.setAttribute('cy', ny);
          label.setAttribute('x', nx); label.setAttribute('y', ny - Math.max(r, dot) - gm.px(4));
          readout.textContent = 'X ' + pos[0] + ' · Z ' + pos[2];
        },
        onEnd: (moved) => { if (moved) changed(); setTimeout(() => { sel = i; render(); renderSide(); }, 0); },
        onDelete: () => removeZone(i),
      });
      // The edge handle of the selected zone resizes it.
      if (i === sel && !running) {
        const [ex, ey] = gm.w2s(pos[0] + (Number(a.Data.Radius) || 0), pos[2]);
        const handle = svgEl('circle', { cx: ex, cy: ey, r: hr, class: 'zone-handle' }, gm.layer);
        mapPointDrag(gm, handle, {
          onMove: (wx, wz) => {
            a.Data.Radius = Math.max(5, Math.round(Math.hypot(wx - pos[0], wz - pos[2])));
            const nr = toS(a.Data.Radius);
            disc.setAttribute('r', nr);
            const [hx, hy] = gm.w2s(pos[0] + a.Data.Radius, pos[2]);
            handle.setAttribute('cx', hx); handle.setAttribute('cy', hy);
            readout.textContent = t('zones.f.Radius') + ': ' + a.Data.Radius + ' ' + t('unit.m');
          },
          onEnd: (moved) => { if (moved) changed(); },
        });
      }
    });
    safe().forEach((p, i) => {
      if (!Array.isArray(p) || p.length < 2) return;
      const [sx, sy] = gm.w2s(p[0], p[1]);
      const g = svgEl('g', { class: 'zone-safe' }, gm.layer);
      const s = gm.ptR(4.5);
      const mark = svgEl('rect', { x: sx - s, y: sy - s, width: s * 2, height: s * 2, rx: s * 0.4, class: 'zone-safe-m' }, g);
      mapPointDrag(gm, g, {
        locked: () => running,
        onMove: (wx, wz) => {
          p[0] = Math.round(wx); p[1] = Math.round(wz);
          const [nx, ny] = gm.w2s(p[0], p[1]);
          mark.setAttribute('x', nx - s); mark.setAttribute('y', ny - s);
          readout.textContent = 'X ' + p[0] + ' · Z ' + p[1];
        },
        onEnd: (moved) => { if (moved) changed(); },
        onDelete: () => { safe().splice(i, 1); changed(); },
      });
    });
    const gas = areas().filter(zoneIsGas).length;
    counts.textContent = tn('zones.count', { n: gas }) + ' · ' + tn('zones.safeCount', { n: safe().length });
  }
  gm.onView = () => render();

  function removeZone(i) {
    areas().splice(i, 1);
    if (sel === i) sel = -1; else if (sel > i) sel--;
    changed();
  }

  // ---- the side panel: the selected zone, or the list of zones
  const listOf = (key) => [...new Set(areas().map(a => (a.Data && a.Data[key]) || (a.PlayerData && a.PlayerData[key])).filter(v => typeof v === 'string' && v))];
  function renderSide() {
    side.innerHTML = '';
    const a = areas()[sel];
    if (!a) {
      side.append(h('h3', { i18n: 'zones.list' }));
      if (!areas().length) side.append(h('p', { class: 'hint', i18n: 'zones.none' }));
      const list = h('div', { class: 'zones-list' });
      areas().forEach((z, i) => {
        const p = (z.Data && z.Data.Pos) || [];
        list.append(h('button', { type: 'button', class: 'zones-item' + (zoneIsGas(z) ? '' : ' other'), onclick: () => {
          sel = i; render(); renderSide();
          if (p.length >= 3) gm.focus(p[0], p[2], gm.S / 8);
        } }, [
          h('span', { class: 'zones-dot' }),
          h('span', { class: 'grow' }, [h('b', { text: z.AreaName || '—' }), h('small', { class: 'hint mono', text: (z.Type || '') })]),
          h('span', { class: 'hint mono', text: (z.Data && z.Data.Radius ? z.Data.Radius + ' ' + t('unit.m') : '') }),
        ]));
      });
      side.append(list);
      applyI18n();
      return;
    }
    a.Data = a.Data || {};
    const pos = a.Data.Pos || (a.Data.Pos = [0, 0, 0]);
    const num = (val, onInput, attrs = {}) => {
      const inp = h('input', { type: 'number', step: 'any', value: val ?? '', disabled: running, ...attrs });
      inp.addEventListener('input', () => { const v = Number(inp.value); if (inp.value.trim() !== '' && Number.isFinite(v)) { onInput(v); changedSoft(); } });
      return inp;
    };
    const field = (key, el, hintText) => h('div', { class: 'zones-field' }, [
      h('label', { for: assocId(el), text: t('zones.f.' + key) === 'zones.f.' + key ? key : t('zones.f.' + key) }), el,
      hintText ? h('small', { class: 'hint', text: hintText }) : null,
    ]);
    const name = h('input', { type: 'text', value: a.AreaName || '', disabled: running });
    name.addEventListener('input', () => { a.AreaName = name.value; changedSoft(); });
    const radius = num(a.Data.Radius, (v) => { a.Data.Radius = Math.max(1, v); slider.value = String(a.Data.Radius); });
    const slider = h('input', { type: 'range', min: '10', max: '600', step: '5', value: String(a.Data.Radius || 100), disabled: running, 'aria-label': t('zones.f.Radius') });
    slider.addEventListener('input', () => { a.Data.Radius = Number(slider.value); radius.value = slider.value; changedSoft(); });
    const fields = [
      field('AreaName', name),
      h('div', { class: 'zones-type' }, [h('span', { class: 'badge ' + (zoneIsGas(a) ? 'warn' : 'info') + ' mono', text: a.Type || '—' })]),
      h('div', { class: 'zones-field' }, [h('label', { for: assocId(radius), text: t('zones.f.Radius') }), h('div', { class: 'zones-radius' }, [slider, radius])]),
      h('div', { class: 'zones-xyz' }, [
        field('X', num(pos[0], (v) => { pos[0] = v; })),
        field('Y', num(pos[1], (v) => { pos[1] = v; }), t('zones.f.Y.hint')),
        field('Z', num(pos[2], (v) => { pos[2] = v; })),
      ]),
    ];
    // Every other field of Data, then of PlayerData: numbers as numbers, the
    // particle and tint names as text with the names the file already uses.
    const more = [];
    const keys = Object.keys(a.Data).filter(k => k !== 'Pos' && k !== 'Radius');
    keys.sort((x, y) => (ZONE_FIELDS.indexOf(x) + 1 || 99) - (ZONE_FIELDS.indexOf(y) + 1 || 99));
    const textField = (obj, k) => {
      const dl = 'zones-dl-' + k;
      const inp = h('input', { type: 'text', value: obj[k] ?? '', disabled: running, list: dl, class: 'mono' });
      inp.addEventListener('input', () => { obj[k] = inp.value; changedSoft(); });
      return h('div', { class: 'zones-field wide' }, [h('label', { for: assocId(inp), text: t('zones.f.' + k) === 'zones.f.' + k ? k : t('zones.f.' + k) }), inp,
        h('datalist', { id: dl }, listOf(k).map(v => h('option', { value: v })))]);
    };
    for (const k of keys) {
      if (typeof a.Data[k] === 'number') more.push(field(k, num(a.Data[k], (v) => { a.Data[k] = v; })));
      else if (typeof a.Data[k] === 'string') more.push(textField(a.Data, k));
    }
    if (a.PlayerData && typeof a.PlayerData === 'object') {
      for (const k of Object.keys(a.PlayerData)) {
        if (typeof a.PlayerData[k] === 'string') more.push(textField(a.PlayerData, k));
      }
    }
    side.append(...[
      h('div', { class: 'zones-side-head' }, [
        h('button', { type: 'button', class: 'linkish', text: '← ' + t('zones.list'), onclick: () => { sel = -1; render(); renderSide(); } }),
        h('span', { class: 'grow' }),
        h('button', { type: 'button', class: 'icon-btn', title: t('zones.focus'), 'aria-label': t('zones.focus'), html: svgIcon('i-eventmap', 16),
          onclick: () => gm.focus(pos[0], pos[2], gm.S / 8) }),
      ]),
      h('div', { class: 'zones-form' }, fields),
      more.length ? h('details', { class: 'zones-more' }, [h('summary', { i18n: 'zones.more' }), h('div', { class: 'zones-form' }, more)]) : null,
      h('div', { class: 'actions' }, [h('span', { class: 'grow' }),
        h('button', { type: 'button', class: 'danger', i18n: 'zones.delete', disabled: running, onclick: async () => {
          if (await confirmModal(t('zones.delete.confirm').replace('{name}', a.AreaName || ''), { danger: true, okText: t('zones.delete') })) removeZone(sel);
        } })]),
    ].filter(Boolean));
    applyI18n();
  }

  // changed: the map and the panel follow an edit; changedSoft keeps the
  // panel (a field being typed in) and redraws the map only.
  function changed() { render(); renderSide(); bar.hidden = !isDirty(); }
  function changedSoft() { render(); bar.hidden = !isDirty(); }

  // ---- dynamic zones
  for (const dz of d.dynamic || []) root.append(dynamicCard(dz));
  function dynamicCard(dz) {
    const F = {};
    const inp = (k, v) => (F[k] = h('input', { type: 'number', min: '0', value: v ?? '', disabled: running, class: 'zones-num' }));
    const active = h('input', { type: 'checkbox', class: 'switch', disabled: running });
    active.checked = (dz.active ?? 1) !== 0;
    const life = h('small', { class: 'hint', text: fmtLifetime(dz.lifetime) });
    const lifeIn = inp('Lifetime', dz.lifetime);
    lifeIn.addEventListener('input', () => { life.textContent = fmtLifetime(lifeIn.value); });
    const cell = (key, el, extra) => h('div', { class: 'zones-field' }, [h('label', { for: assocId(el), i18n: key }), el, extra || null]);
    return h('section', { class: 'card zones-dyn' }, [
      cardTitle('i-toxic', 'zones.dyn.title'),
      h('p', { class: 'hint', text: t('zones.dyn.hint').replace('{event}', dz.event) }),
      h('div', { class: 'zones-dyn-grid' }, [
        h('div', { class: 'zones-field' }, [h('label', { i18n: 'vehicles.f.active' }), h('label', { class: 'inline-switch' }, [active, h('span', { i18n: 'zones.dyn.on' })])]),
        cell('zones.dyn.nominal', inp('Nominal', dz.nominal)),
        cell('zones.dyn.min', inp('Min', dz.min)),
        cell('zones.dyn.max', inp('Max', dz.max)),
        cell('zones.dyn.lifetime', lifeIn, life),
      ]),
      h('div', { class: 'actions' }, [
        h('button', { type: 'button', class: 'secondary', onclick: () => { window._eventmapTarget = dz.event; navigate('eventmap'); } },
          [h('span', { html: svgIcon('i-eventmap', 14) }), h('span', { text: ' ' + tn('zones.dyn.spots', { n: (dz.positions || []).length }) })]),
        h('span', { class: 'grow' }),
        h('button', { type: 'button', class: 'primary', i18n: 'action.save', disabled: running, onclick: (e) => withBusy(e.currentTarget, async () => {
          const body = { Name: dz.event, Active: active.checked ? 1 : 0 };
          for (const [k, el] of Object.entries(F)) if (el.value.trim() !== '') body[k] = Number(el.value);
          try {
            await api.post(`/api/events/item?name=${encodeURIComponent(dz.event)}` +
              (dz.file && dz.file !== 'db/events.xml' ? `&file=${encodeURIComponent(dz.file)}` : ''), body);
            toast(t('msg.saved'), 'ok');
          } catch (err) { handleErr(err); }
        }) }),
      ]),
    ]);
  }

  // ---- loot
  root.append(lootCard());
  function lootCard() {
    const card = h('section', { class: 'card zones-loot' });
    const rows = (d.loot || []).map(l => ({ ...l }));
    const removed = new Set();
    const added = [];
    const tb = h('tbody');
    const draw = () => {
      tb.innerHTML = '';
      const shown = rows.filter(r => !removed.has(r.name));
      if (!shown.length && !added.length) tb.append(h('tr', {}, h('td', { colspan: '4', class: 'hint', i18n: 'zones.loot.none' })));
      for (const r of shown) {
        const nom = h('input', { type: 'number', min: '0', class: 'zones-num', value: r.nominal ?? '', disabled: running, 'aria-label': r.name + ' nominal' });
        const min = h('input', { type: 'number', min: '0', class: 'zones-num', value: r.min ?? '', disabled: running, 'aria-label': r.name + ' min' });
        nom.addEventListener('input', () => { r.nominal = nom.value === '' ? null : Number(nom.value); r.dirty = true; mark(); });
        min.addEventListener('input', () => { r.min = min.value === '' ? null : Number(min.value); r.dirty = true; mark(); });
        tb.append(h('tr', {}, [
          h('td', {}, [h('b', { text: r.name }), (r.usages || []).length > 1 ? h('small', { class: 'hint', text: ' · ' + r.usages.filter(u => u !== 'ContaminatedArea').join(', ') }) : null]),
          h('td', { class: 'num' }, nom), h('td', { class: 'num' }, min),
          h('td', {}, h('button', { type: 'button', class: 'icon-x', text: '×', disabled: running, title: t('zones.loot.remove'), 'aria-label': t('zones.loot.remove') + ': ' + r.name,
            onclick: () => { removed.add(r.name); mark(); draw(); } })),
        ]));
      }
      for (const name of added) {
        tb.append(h('tr', { class: 'zones-added' }, [h('td', {}, [h('b', { text: name }), h('small', { class: 'hint', text: ' · ' + t('zones.loot.new') })]),
          h('td'), h('td'), h('td', {}, h('button', { type: 'button', class: 'icon-x', text: '×', onclick: () => { added.splice(added.indexOf(name), 1); mark(); draw(); } }))]));
      }
      applyI18n();
    };
    const addIn = h('input', { type: 'text', class: 'mono', placeholder: t('zones.loot.add.ph'), list: 'zones-types-dl', disabled: running, 'aria-label': t('zones.loot.add.ph') });
    const dl = h('datalist', { id: 'zones-types-dl' });
    api.get('/api/types').then((r) => {
      for (const ty of (r.types || []).slice(0, 5000)) dl.append(h('option', { value: ty.name }));
    }).catch(() => {});
    const addOne = () => {
      const n = addIn.value.trim();
      if (!n || added.includes(n) || rows.some(r => r.name.toLowerCase() === n.toLowerCase() && !removed.has(r.name))) { addIn.value = ''; return; }
      added.push(n); addIn.value = ''; mark(); draw();
    };
    addIn.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); addOne(); } });
    const lootSave = h('button', { type: 'button', class: 'primary', i18n: 'action.save', disabled: true, onclick: (e) => withBusy(e.currentTarget, async () => {
      try {
        const r = await api.post('/api/zones/loot', {
          rows: rows.filter(x => x.dirty && !removed.has(x.name)).map(x => ({ name: x.name, nominal: x.nominal ?? undefined, min: x.min ?? undefined })),
          add: added, remove: [...removed],
        });
        if ((r.notFound || []).length) toast(t('zones.loot.notFound').replace('{names}', r.notFound.join(', ')), 'warn');
        else toast(t('msg.saved'), 'ok');
        await navigate('zones');
      } catch (err) { handleErr(err); }
    }) });
    const mark = () => { lootSave.disabled = running || !(added.length || removed.size || rows.some(r => r.dirty)); };
    card.append(
      cardTitle('i-randompresets', 'zones.loot.title'),
      h('p', { class: 'hint', i18n: d.usage === false ? 'zones.loot.noUsage' : 'zones.loot.hint' }),
      h('div', { class: 'table-scroll' }, h('table', { class: 'zones-loot-t' }, [
        h('thead', {}, h('tr', {}, [h('th', { i18n: 'zones.loot.item' }), h('th', { class: 'num', i18n: 'zones.loot.nominal' }), h('th', { class: 'num', i18n: 'zones.loot.min' }), h('th')])),
        tb,
      ])),
      h('div', { class: 'row zones-loot-add' }, [addIn, dl, h('button', { type: 'button', class: 'secondary', i18n: 'zones.loot.add', disabled: running, onclick: addOne }), h('span', { class: 'grow' }), lootSave]),
    );
    draw();
    return card;
  }

  render();
  renderSide();
  applyI18n();
};
