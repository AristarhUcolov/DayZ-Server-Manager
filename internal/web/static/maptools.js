// Copyright (c) 2026 Aristarh Ucolov.
//
// Tools on every map (buildGameMap calls attachMapTools): a ruler that
// measures distances, and admin markers — notes pinned on the map (a base, a
// trader, a reported spot) that every admin of this server sees on every map.

const MARKER_COLORS = ['red', 'orange', 'yellow', 'green', 'blue', 'purple'];
// The swatch buttons carry their colour inline: the global button hover and
// focus backgrounds would beat a class.
const MARKER_HEX = { red: '#e5484d', orange: '#f76b15', yellow: '#e8b500', green: '#30a46c', blue: '#0b84f3', purple: '#8e4ec6' };

// fmtDist formats a world distance in metres: "850 m", "1.24 km".
function fmtDist(m) {
  const num = (v, d) => {
    try { return v.toLocaleString(State.lang || undefined, { minimumFractionDigits: d, maximumFractionDigits: d }); }
    catch { return v.toFixed(d); }
  };
  if (m < 1000) return num(Math.round(m), 0) + ' ' + t('unit.m');
  return num(m / 1000, m < 10000 ? 2 : 1) + ' ' + t('unit.km');
}

// fmtXZ is a world position as the admin reads and pastes it.
const fmtXZ = (x, z) => 'X ' + Math.round(x) + ' · Z ' + Math.round(z);

function markersShown() {
  try { return localStorage.getItem('mapMarkers') !== '0'; } catch { return true; }
}

function attachMapTools(gm, map) {
  const svg = gm.svg;
  let mode = ''; // '' | 'measure' | 'marker'

  // Floating boxes go into the map's frame once the page has put the map in
  // one (.map-wrap); the controls column is too narrow for them.
  const frame = () => svg.parentElement;
  const mount = (el) => { const f = frame(); if (f && el.parentElement !== f) f.append(el); };

  const ctlBtn = (icon, key) => {
    const b = h('button', { type: 'button', class: 'gm-zbtn gm-tool-btn', title: t(key), 'aria-label': t(key), 'aria-pressed': 'false' });
    b.innerHTML = svgIcon(icon, 16);
    return b;
  };
  const rulerBtn = ctlBtn('i-ruler', 'map.ruler');
  const pinBtn = ctlBtn('i-pin', 'map.markers');
  gm.controls.append(rulerBtn, pinBtn);

  // ---- modes -----------------------------------------------------------
  const onKey = (e) => {
    if (!svg.isConnected) { document.removeEventListener('keydown', onKey); return; }
    if (e.key === 'Escape' && mode && !document.querySelector('.modal-overlay')) setMode('');
  };
  function setMode(m) {
    if (mode === 'measure' && m !== 'measure') clearRuler();
    mode = m;
    svg.classList.toggle('gm-measuring', m === 'measure');
    svg.classList.toggle('gm-placing', m === 'marker');
    rulerBtn.classList.toggle('on', m === 'measure');
    rulerBtn.setAttribute('aria-pressed', String(m === 'measure'));
    pinBtn.classList.toggle('on', m === 'marker');
    rulerBox.hidden = m !== 'measure';
    placeHint.hidden = m !== 'marker';
    if (m) { mount(rulerBox); mount(placeHint); document.addEventListener('keydown', onKey); }
    else document.removeEventListener('keydown', onKey);
    if (m === 'measure') renderRuler();
  }

  // A click on the map in a tool mode belongs to the tool: it runs before the
  // page's own click (adding an editor point, opening a player) and stops it.
  // A click that ended a pan never gets here (buildGameMap swallows it first).
  svg.addEventListener('click', (e) => {
    if (!mode) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    const [wx, wz] = clientToWorld(gm, e);
    if (mode === 'measure') {
      ruler.push([wx, wz]);
      renderRuler();
    } else if (mode === 'marker') {
      setMode('');
      editMarker({ x: wx, z: wz, label: '', note: '', color: 'red' });
    }
  }, true);
  svg.addEventListener('contextmenu', (e) => {
    if (mode !== 'measure') return;
    e.preventDefault();
    e.stopImmediatePropagation();
    ruler.pop();
    renderRuler();
  }, true);

  // ---- ruler -----------------------------------------------------------
  const ruler = []; // [[x, z], …] world metres
  const rulerG = svgEl('g', { class: 'gm-ruler' }, svg);
  const totalEl = h('b', { class: 'gm-ruler-total' });
  const countEl = h('span', { class: 'hint' });
  const rulerBox = h('div', { class: 'gm-toolbox', hidden: true, role: 'status' }, [
    h('div', { class: 'gm-toolbox-head' }, [
      h('span', { class: 'gm-toolbox-icon', html: svgIcon('i-ruler', 15) }),
      h('span', { i18n: 'map.ruler.total' }), totalEl, countEl,
    ]),
    h('p', { class: 'hint', i18n: 'map.ruler.hint' }),
    h('div', { class: 'gm-toolbox-actions' }, [
      h('button', { type: 'button', class: 'secondary small', i18n: 'map.ruler.copy', onclick: () => {
        if (!ruler.length) return;
        copyText(ruler.map(([x, z]) => x.toFixed(2) + ' ' + z.toFixed(2)).join('\n'));
      } }),
      h('button', { type: 'button', class: 'secondary small', i18n: 'map.ruler.clear', onclick: () => { clearRuler(); renderRuler(); } }),
      h('button', { type: 'button', class: 'primary small', i18n: 'map.ruler.done', onclick: () => setMode('') }),
    ]),
  ]);
  function clearRuler() { ruler.length = 0; rulerG.innerHTML = ''; }
  function renderRuler() {
    rulerG.innerHTML = '';
    let total = 0;
    const s = gm.px(1); // one screen pixel in map units
    const pts = ruler.map(([x, z]) => gm.w2s(x, z));
    if (pts.length > 1) {
      const d = pts.map((p, i) => (i ? 'L' : 'M') + p[0].toFixed(2) + ' ' + p[1].toFixed(2)).join(' ');
      svgEl('path', { d, class: 'gm-ruler-casing' }, rulerG);
      svgEl('path', { d, class: 'gm-ruler-line' }, rulerG);
    }
    ruler.forEach(([x, z], i) => {
      if (i) total += Math.hypot(x - ruler[i - 1][0], z - ruler[i - 1][1]);
      const [sx, sy] = pts[i];
      const g = svgEl('g', { transform: `translate(${sx} ${sy}) scale(${s})` }, rulerG);
      svgEl('circle', { cx: 0, cy: 0, r: i ? 4 : 5, class: 'gm-ruler-pt' + (i ? '' : ' first') }, g);
      if (i) svgEl('text', { x: 8, y: -7, class: 'gm-ruler-label' }, g).textContent = fmtDist(total);
    });
    totalEl.textContent = fmtDist(total);
    countEl.textContent = ruler.length ? tn('map.ruler.points', { n: ruler.length }) : '';
  }
  rulerBtn.addEventListener('click', (e) => { e.preventDefault(); setMode(mode === 'measure' ? '' : 'measure'); });

  // ---- admin markers ---------------------------------------------------
  const placeHint = h('div', { class: 'gm-toolbox gm-place-hint', hidden: true }, [
    h('span', { class: 'gm-toolbox-icon', html: svgIcon('i-pin', 15) }),
    h('span', { i18n: 'map.markers.addHint' }),
    h('button', { type: 'button', class: 'secondary small', i18n: 'action.cancel', onclick: () => setMode('') }),
  ]);
  // Markers sit under the page's own points, so editor points stay on top.
  const markG = svgEl('g', { class: 'gm-markers' });
  svg.insertBefore(markG, gm.layer);
  let markers = [];
  let shown = markersShown();

  const listHost = h('div', { class: 'gm-mk-list' });
  const showCb = h('input', { type: 'checkbox' });
  showCb.checked = shown;
  showCb.addEventListener('change', () => {
    shown = showCb.checked;
    try { localStorage.setItem('mapMarkers', shown ? '1' : '0'); } catch {}
    drawMarkers();
  });
  const panel = h('div', { class: 'gm-layers-panel gm-mk-panel', hidden: true }, [
    h('div', { class: 'gm-layers-title', i18n: 'map.markers' }),
    h('button', { type: 'button', class: 'primary small gm-mk-add', onclick: () => { panel.hidden = true; setMode('marker'); } }, [
      h('span', { html: svgIcon('i-plus', 14) }), h('span', { i18n: 'map.markers.add' }),
    ]),
    h('label', { class: 'gm-layer-row' }, [showCb, h('span', { i18n: 'map.markers.show' })]),
    listHost,
  ]);
  gm.controls.append(panel);
  pinBtn.addEventListener('click', (e) => {
    e.preventDefault();
    if (mode === 'marker') { setMode(''); return; }
    panel.hidden = !panel.hidden;
    // One panel at a time next to the controls.
    if (!panel.hidden) for (const p of gm.controls.querySelectorAll('.gm-layers-panel')) if (p !== panel) p.hidden = true;
  });
  gm.controls.addEventListener('click', (e) => {
    if (e.target.closest && e.target.closest('.gm-layers-btn')) panel.hidden = true;
  });

  function drawMarkers() {
    markG.innerHTML = '';
    if (!shown) return;
    const s = gm.px(1);
    for (const m of markers) {
      const [sx, sy] = gm.w2s(m.x, m.z);
      const g = svgEl('g', { class: 'gm-mk gm-mk-' + m.color, transform: `translate(${sx} ${sy}) scale(${s})` }, markG);
      // A pin whose tip is the point.
      svgEl('path', { d: 'M0 0C-2-5-8-9-8-15a8 8 0 0 1 16 0c0 6-6 10-8 15Z', class: 'gm-mk-pin' }, g);
      svgEl('circle', { cx: 0, cy: -15, r: 3, class: 'gm-mk-dot' }, g);
      svgEl('text', { x: 11, y: -11, class: 'gm-mk-label' }, g).textContent = m.label;
      svgEl('title', {}, g).textContent = m.label + (m.note ? '\n' + m.note : '') + '\n' + fmtXZ(m.x, m.z);
      g.addEventListener('click', (e) => {
        if (mode) return; // a tool owns the click (handled on the map)
        e.stopPropagation(); // not an editor point, not a player
        editMarker(m);
      });
    }
  }
  function renderList() {
    listHost.innerHTML = '';
    if (!markers.length) {
      listHost.append(h('p', { class: 'hint', i18n: 'map.markers.empty' }));
      applyI18n();
      return;
    }
    for (const m of markers.slice().sort((a, b) => a.label.localeCompare(b.label))) {
      listHost.append(h('div', { class: 'gm-mk-row' }, [
        h('button', { type: 'button', class: 'gm-mk-go', title: t('map.markers.focus'), onclick: () => {
          if (!shown) { shown = true; showCb.checked = true; drawMarkers(); }
          gm.focus(m.x, m.z);
        } }, [
          h('span', { class: 'gm-mk-swatch mk-' + m.color }),
          h('span', { class: 'gm-mk-name', text: m.label }),
        ]),
        h('button', { type: 'button', class: 'icon-btn', title: t('action.edit'), 'aria-label': t('action.edit') + ' ' + m.label,
          html: svgIcon('i-edit', 14), onclick: () => editMarker(m) }),
      ]));
    }
  }
  function setMarkers(list) {
    markers = (list || []).filter(m => m && Number.isFinite(m.x) && Number.isFinite(m.z));
    drawMarkers();
    renderList();
  }
  async function send(op, marker) {
    const r = await api.post('/api/map/markers/edit', { op, marker });
    setMarkers(r.markers);
    return r;
  }

  // editMarker opens the marker dialog: a new one (no id) or an existing one.
  function editMarker(m) {
    const isNew = !m.id;
    const md = openModal({ title: t(isNew ? 'map.markers.new' : 'map.markers.edit'),
      isDirty: () => label.value.trim() !== (m.label || '') || note.value.trim() !== (m.note || '') || color !== (m.color || 'red') });
    const label = h('input', { type: 'text', maxlength: 60, placeholder: t('map.markers.labelPh') });
    label.value = m.label || '';
    const note = h('textarea', { rows: 3, maxlength: 500 });
    note.value = m.note || '';
    let color = MARKER_COLORS.includes(m.color) ? m.color : 'red';
    const swatches = h('div', { class: 'gm-mk-colors', role: 'radiogroup', 'aria-label': t('map.markers.color') });
    const paintSwatches = () => {
      for (const b of swatches.children) {
        const on = b.dataset.c === color;
        b.classList.toggle('on', on);
        b.setAttribute('aria-checked', String(on));
      }
    };
    for (const c of MARKER_COLORS) {
      swatches.append(h('button', { type: 'button', class: 'gm-mk-color', role: 'radio', 'data-c': c, style: { background: MARKER_HEX[c] },
        title: t('map.color.' + c), 'aria-label': t('map.color.' + c),
        onclick: () => { color = c; paintSwatches(); } }));
    }
    paintSwatches();
    const where = fmtXZ(m.x, m.z);
    const save = (btn) => withBusy(btn, async () => {
      const v = label.value.trim();
      if (!v) { toast(t('map.markers.needLabel'), 'error'); label.focus(); return; }
      try {
        await send(isNew ? 'add' : 'update', { id: m.id, x: m.x, z: m.z, label: v, note: note.value.trim(), color });
        md.close();
        toast(t('map.markers.saved'), 'ok');
      } catch (err) { handleErr(err); }
    });
    label.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); save(saveBtn); } });
    const saveBtn = h('button', { type: 'button', class: 'primary', i18n: 'action.save', onclick: (e) => save(e.currentTarget) });
    md.body.append(
      h('div', { class: 'gm-mk-form' }, [
        h('label', { i18n: 'map.markers.label', for: assocId(label) }), label,
        h('label', { i18n: 'map.markers.color' }), swatches,
        h('label', { i18n: 'map.markers.note', for: assocId(note) }), note,
        h('label', { i18n: 'map.markers.position' }),
        h('div', { class: 'gm-mk-where' }, [
          h('span', { class: 'mono', text: where }),
          h('button', { type: 'button', class: 'secondary small', i18n: 'action.copy',
            onclick: () => copyText(m.x.toFixed(2) + ' ' + m.z.toFixed(2)) }),
        ]),
      ]),
      ...(isNew || !m.at ? [] : [h('p', { class: 'hint gm-mk-changed', text: (m.by ? t('map.markers.changedBy') : t('map.markers.changed'))
        .replace('{when}', fmtWhen(m.at)).replace('{user}', m.by || '') })]),
      h('div', { class: 'actions' }, [
        saveBtn,
        h('button', { type: 'button', i18n: 'action.cancel', onclick: () => md.close() }),
        isNew ? null : h('div', { class: 'grow' }),
        isNew ? null : h('button', { type: 'button', class: 'danger', i18n: 'action.delete', onclick: async (e) => {
          const btn = e.currentTarget;
          if (!await confirmModal(t('map.markers.deleteConfirm').replace('{name}', m.label), { danger: true, okText: t('action.delete') })) return;
          await withBusy(btn, async () => {
            try { await send('delete', { id: m.id }); md.close(); toast(t('map.markers.deleted'), 'ok'); }
            catch (err) { handleErr(err); }
          });
        } }),
      ]),
    );
    applyI18n();
    setTimeout(() => label.focus(), 0);
  }

  gm.viewHooks.push(() => {
    drawMarkers();
    if (ruler.length) renderRuler();
  });
  api.get('/api/map/markers').then(d => setMarkers(d.markers)).catch(() => renderList());
}
