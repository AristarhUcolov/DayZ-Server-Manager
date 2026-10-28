// Copyright (c) 2026 Aristarh Ucolov.
//
// Player loadout — what fresh spawns wear, hold and carry, edited on a figure.
// It edits DayZ's spawn-gear preset files ("kits") and registers them in
// cfggameplay.json; until that system is switched on the page says so and
// offers one button, not an editor pretending to work (vanilla spawns from
// init.c, which the panel does not touch).
//
// How DayZ reads a kit (scripts/4_World/Classes/PlayerGearSpawn, 1.29):
//  - attachmentSlotItemSets: per survivor slot, one item is picked by weight;
//    slot "Hands" puts it in the hands (CreateInHands), shoulderL/shoulderR
//    are old names of Shoulder/Melee. A worn item WITHOUT "attributes" is
//    skipped, so the editor always writes them.
//  - discreteUnsortedItemSets: one bundle is picked by weight; its items go
//    wherever they fit.
//  - quickBarSlot: 0..9 = hotbar keys 1..0, -1 = none (a missing value reads
//    as 0, key 1 — the backend writes -1).
//  - an item's complexChildrenTypes go inside/onto it (a magazine in a gun,
//    food in a backpack); a child without attributes spawns as the game makes
//    it. The editor keeps every child as a complex child: simple children are
//    converted on load, with the same effect.
// The item list comes from the server's own game files and mods
// (/api/items/catalog): names in the panel's language, the slots each item
// fits, categories.

const LO_SLOTS = [
  { id: 'Headgear', side: 'l', key: 'lo.slot.head', icon: 'i-s-head' },
  { id: 'Mask', side: 'l', key: 'lo.slot.face', icon: 'i-s-face' },
  { id: 'Eyewear', side: 'l', key: 'lo.slot.eyes', icon: 'i-s-eyes' },
  { id: 'Body', side: 'l', key: 'lo.slot.top', icon: 'i-s-top' },
  { id: 'Vest', side: 'l', key: 'lo.slot.vest', icon: 'i-s-vest' },
  { id: 'Gloves', side: 'l', key: 'lo.slot.gloves', icon: 'i-s-gloves' },
  { id: 'Armband', side: 'l', key: 'lo.slot.armband', icon: 'i-s-armband' },
  { id: 'Back', side: 'r', key: 'lo.slot.back', icon: 'i-s-back' },
  { id: 'Shoulder', side: 'r', key: 'lo.slot.shoulder', icon: 'i-s-shoulder' },
  { id: 'Melee', side: 'r', key: 'lo.slot.melee', icon: 'i-s-melee' },
  { id: 'Hips', side: 'r', key: 'lo.slot.belt', icon: 'i-s-belt' },
  { id: 'Legs', side: 'r', key: 'lo.slot.legs', icon: 'i-s-legs' },
  { id: 'Feet', side: 'r', key: 'lo.slot.feet', icon: 'i-s-feet' },
  { id: 'Hands', side: 'r', key: 'lo.slot.hands', icon: 'i-s-hands' },
];
const LO_SLOT_ALIAS = { shoulderl: 'Shoulder', shoulderr: 'Melee', headgear: 'Headgear', mask: 'Mask', eyewear: 'Eyewear',
  body: 'Body', vest: 'Vest', gloves: 'Gloves', armband: 'Armband', back: 'Back', shoulder: 'Shoulder', melee: 'Melee',
  hips: 'Hips', legs: 'Legs', feet: 'Feet', hands: 'Hands' };
const LO_CATS = ['clothing', 'weapons', 'medical', 'food', 'tools', 'containers', 'ammo', 'magazines', 'attachments', 'explosives', 'other'];
const LO_CAT_ICON = { clothing: 'i-s-top', weapons: 'i-s-shoulder', magazines: 'i-c-mag', ammo: 'i-c-ammo', attachments: 'i-c-scope',
  medical: 'i-c-med', food: 'i-c-food', tools: 'i-c-tool', containers: 'i-c-box', explosives: 'i-c-boom', other: 'i-c-other' };
// Named condition ranges (healthMin/healthMax are health fractions, 1 =
// pristine). "default" (children only) writes no attributes: as the game
// makes the item.
const LO_COND = [
  { id: 'pristine', min: 1.0, max: 1.0 },
  { id: 'worn', min: 0.7, max: 0.9 },
  { id: 'damaged', min: 0.45, max: 0.65 },
  { id: 'badly', min: 0.1, max: 0.25 },
];
const LO_QTY = [
  { id: 'full', min: 1, max: 1 },
  { id: 'half', min: 0.5, max: 0.5 },
  { id: 'random', min: 0, max: 1 },
  { id: 'empty', min: 0, max: 0 },
];
const loNear = (a, b) => Math.abs((a ?? -1) - b) < 0.001;

// The item catalog, fetched once per language.
let _loCatalog = null;
// The kit to reopen after the page reloads itself (a save, an import).
let _loPrefer = '';
async function loCatalog() {
  const lang = State.lang || 'en';
  if (_loCatalog && _loCatalog.lang === lang) return _loCatalog;
  let d;
  try { d = await api.get('/api/items/catalog?lang=' + encodeURIComponent(lang)); }
  catch (e) { d = { items: [], survivors: [], error: e.message }; }
  const byClass = new Map();
  for (const it of d.items || []) byClass.set(it.c.toLowerCase(), it);
  // Vehicles are named too (the Vehicles and Attachments pages), but never
  // offered by a picker: they are not in items.
  for (const v of d.vehicles || []) if (!byClass.has(v.c.toLowerCase())) byClass.set(v.c.toLowerCase(), v);
  _loCatalog = { lang, items: d.items || [], survivors: d.survivors || [], vehicles: d.vehicles || [], byClass, error: d.error || '', mods: d.mods || [] };
  return _loCatalog;
}

function loItemInfo(cat, cls) {
  const it = cat.byClass.get(String(cls || '').toLowerCase());
  if (it) return it;
  // Without a catalog (game files unreadable) nothing can be called unknown.
  return { c: cls, n: loHumanize(cls), k: 'other', unknown: cat.items.length > 0 };
}
function loHumanize(cls) {
  return String(cls || '').replace(/_/g, ' ').replace(/([a-z])([A-Z])/g, '$1 $2').replace(/([A-Z])([A-Z][a-z])/g, '$1 $2').trim();
}
const loIcon = (id, size = 18) => h('span', { class: 'lo-ico', 'aria-hidden': 'true', html: svgIcon(id, size) });
// loIconFor picks an item's icon: clothing by the slot it is worn in, the
// rest by category.
function loIconFor(it) {
  if (it.k === 'clothing' && it.s && it.s.length) {
    const def = LO_SLOTS.find(d => d.id === it.s[0]);
    if (def) return def.icon;
  }
  return LO_CAT_ICON[it.k] || 'i-c-other';
}

// --------------------------------------------------------------- the model
//
// The editor works on a normalised copy: slots keyed by survivor slot, every
// child complex. toPreset() turns it back into DayZ's file.

function loConvertChildren(item) {
  const kids = (item.complexChildrenTypes || []).map(c => { loConvertChildren(c); return c; });
  const simple = (item.simpleChildrenTypes || []).filter(Boolean);
  if (simple.length) {
    // Same effect as a simple child: the parent's attributes when the parent
    // hands them down, otherwise none (as the game makes it).
    const useParent = item.simpleChildrenUseDefaultAttributes === false && item.attributes;
    for (const cls of simple) {
      const c = { itemType: cls, quickBarSlot: -1 };
      if (useParent) c.attributes = Object.assign({}, item.attributes);
      kids.push(c);
    }
  }
  item.complexChildrenTypes = kids;
  delete item.simpleChildrenTypes;
  delete item.simpleChildrenUseDefaultAttributes;
}

function loFromPreset(p) {
  const kit = {
    name: p.name || '',
    weight: p.spawnWeight || 1,
    chars: Array.isArray(p.characterTypes) ? p.characterTypes.slice() : [],
    slots: {},       // slot id → [variant]
    otherSlots: [],  // slot entries the editor does not know (kept as-is)
    bundles: [],     // [{name, weight, items: [child]}]
    extra: {},       // unknown top-level keys
  };
  for (const [k, v] of Object.entries(p)) {
    if (!['name', 'spawnWeight', 'characterTypes', 'attachmentSlotItemSets', 'discreteUnsortedItemSets'].includes(k)) kit.extra[k] = v;
  }
  for (const s of p.attachmentSlotItemSets || []) {
    const id = LO_SLOT_ALIAS[String(s.slotName || '').toLowerCase()];
    if (!id) { kit.otherSlots.push(s); continue; }
    const list = kit.slots[id] || (kit.slots[id] = []);
    for (const v of s.discreteItemSets || []) { loConvertChildren(v); list.push(v); }
  }
  for (const b of p.discreteUnsortedItemSets || []) {
    loConvertChildren(b);
    kit.bundles.push({ name: b.name || '', weight: b.spawnWeight || 1, attributes: b.attributes, items: b.complexChildrenTypes || [] });
  }
  return kit;
}

// loOrderKids puts every item's children in spawn order, all the way down.
function loOrderKids(item) {
  if (!Array.isArray(item.complexChildrenTypes)) return;
  item.complexChildrenTypes = loSpawnOrder(item.complexChildrenTypes);
  for (const c of item.complexChildrenTypes) loOrderKids(c);
}

function loToPreset(kit) {
  for (const id of Object.keys(kit.slots)) for (const v of kit.slots[id] || []) loOrderKids(v);
  for (const b of kit.bundles) for (const c of b.items) loOrderKids(c);
  const p = Object.assign({}, kit.extra, {
    spawnWeight: Math.max(1, parseInt(kit.weight, 10) || 1),
    name: kit.name.trim(),
    characterTypes: kit.chars.slice(),
    attachmentSlotItemSets: [],
    discreteUnsortedItemSets: [],
  });
  for (const def of LO_SLOTS) {
    const list = (kit.slots[def.id] || []).filter(v => (v.itemType || '').trim());
    if (!list.length) continue;
    for (const v of list) {
      // A worn item without attributes is skipped by DayZ.
      v.attributes = Object.assign({ healthMin: 1, healthMax: 1, quantityMin: 1, quantityMax: 1 }, v.attributes || {});
      if (v.spawnWeight == null || v.spawnWeight < 1) v.spawnWeight = 1;
      if (v.quickBarSlot == null) v.quickBarSlot = -1;
    }
    p.attachmentSlotItemSets.push({ slotName: def.id, discreteItemSets: list });
  }
  p.attachmentSlotItemSets.push(...kit.otherSlots);
  for (const b of kit.bundles) {
    const items = b.items.filter(c => (c.itemType || '').trim());
    if (!items.length) continue;
    p.discreteUnsortedItemSets.push({
      name: b.name || 'Cargo', spawnWeight: Math.max(1, parseInt(b.weight, 10) || 1),
      attributes: b.attributes || { healthMin: 1, healthMax: 1, quantityMin: 1, quantityMax: 1 },
      complexChildrenTypes: items, simpleChildrenUseDefaultAttributes: true,
    });
  }
  return p;
}

const loNewItem = (cls, withAttrs) => {
  const it = { itemType: cls, spawnWeight: 1, quickBarSlot: -1, complexChildrenTypes: [] };
  if (withAttrs) it.attributes = { healthMin: 1, healthMax: 1, quantityMin: 1, quantityMax: 1 };
  return it;
};

// ------------------------------------------------------------- the figure

// loDollSVG draws the survivor. Every slot has its own layer; a filled slot
// lights its part of the figure up (the CSS turns on .has-<slot> layers).
function loDollSVG() {
  return `<svg class="lo-doll-svg" viewBox="0 0 200 380" role="img" aria-hidden="true">
  <defs>
    <linearGradient id="loBody" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="var(--lo-on-1)"/><stop offset="1" stop-color="var(--lo-on-2)"/></linearGradient>
  </defs>
  <ellipse class="lo-shadow" cx="100" cy="364" rx="58" ry="8"/>
  <g class="lay lay-shoulder"><path d="M48 52 L126 214" class="gun"/><path d="M44 44 L56 66" class="gun-tip"/></g>
  <g class="lay lay-melee"><path d="M154 54 L80 214" class="gun"/><path d="M146 40 Q164 40 166 58 L154 60 Z" class="axe"/></g>
  <g class="lay lay-back"><rect x="58" y="86" width="84" height="104" rx="14" class="pack"/></g>
  <g class="part part-body">
    <path d="M64 80 Q100 70 136 80 L141 98 L133 192 L67 192 L59 98 Z"/>
    <path d="M62 84 L44 102 L37 192 L49 196 L58 124 L66 104 Z"/>
    <path d="M138 84 L156 102 L163 192 L151 196 L142 124 L134 104 Z"/>
  </g>
  <rect class="part part-neck" x="92" y="62" width="16" height="14" rx="4"/>
  <circle class="part part-head" cx="100" cy="44" r="20"/>
  <g class="lay lay-body-detail"><path d="M90 80 L100 96 L110 80" class="line"/><path d="M100 96 V186" class="line"/><rect x="84" y="140" width="32" height="20" rx="3" class="line"/></g>
  <g class="lay lay-vest"><path d="M70 86 L130 86 L127 172 L73 172 Z" class="vest"/><path d="M80 86 L84 172 M120 86 L116 172 M74 128 H126" class="line"/></g>
  <g class="lay lay-back-straps"><path d="M78 80 L82 150 M122 80 L118 150" class="strap"/></g>
  <g class="part part-legs"><path d="M70 190 L99 190 L97 332 L76 332 Z"/><path d="M101 190 L130 190 L124 332 L103 332 Z"/></g>
  <g class="lay lay-legs-detail"><path d="M84 250 h10 M110 250 h10" class="line"/></g>
  <g class="lay lay-hips"><rect x="66" y="182" width="68" height="11" rx="3" class="belt"/><rect x="93" y="181" width="14" height="13" rx="2" class="buckle"/></g>
  <g class="part part-feet"><path d="M74 328 L98 328 L100 348 L64 348 Q64 336 74 328 Z"/><path d="M102 328 L126 328 Q136 336 136 348 L100 348 Z"/></g>
  <g class="part part-hands"><circle cx="43" cy="203" r="8"/><circle cx="157" cy="203" r="8"/></g>
  <g class="lay lay-armband"><path d="M47 118 L61 122 L59 134 L45 130 Z" class="band"/></g>
  <g class="lay lay-headgear"><path d="M78 44 Q78 18 100 18 Q122 18 122 44 Z" class="hat"/><path d="M70 45 H130" class="brim"/></g>
  <g class="lay lay-mask"><rect x="86" y="49" width="28" height="13" rx="6" class="mask"/></g>
  <g class="lay lay-eyewear"><rect x="83" y="37" width="34" height="9" rx="4.5" class="glasses"/></g>
  <g class="lay lay-inhands"><rect x="158" y="190" width="22" height="9" rx="2" class="held"/><path d="M162 199 l-2 9 h5 l2 -9" class="held"/></g>
</svg>`;
}

// ------------------------------------------------------------ item picker

// loPick opens the catalog: search, category chips ("Fits Head" first when a
// slot is given), a list with "Add". onAdd(item) runs per added item; the
// dialog stays open for more until "Done". only (a list of {c, n, k, m})
// limits it to what fits an item's slot; single closes it on the first pick.
async function loPick({ title, slot, only, single, onAdd }) {
  const cat = await loCatalog();
  const pool = only ? only.map(r => cat.byClass.get(String(r.c).toLowerCase()) || { c: r.c, n: r.n, k: r.k || 'other', m: r.m }) : cat.items;
  const m = openModal({ title, wide: true });
  m.body.classList.add('lo-pick');
  const search = h('input', { type: 'search', class: 'lo-pick-search', placeholder: t('pick.search'), 'aria-label': t('pick.search') });
  let filter = slot ? 'fits' : 'all';
  const chips = h('div', { class: 'lo-pick-chips', role: 'tablist' });
  const info = h('div', { class: 'lo-pick-info hint' });
  const list = h('div', { class: 'lo-pick-list' });
  const slotDef = LO_SLOTS.find(s => s.id === slot);
  const chipDefs = [];
  if (slotDef && slot !== 'Hands') chipDefs.push({ id: 'fits', label: t('pick.fits').replace('{slot}', t(slotDef.key)) });
  if (!only) {
    chipDefs.push({ id: 'all', label: t('pick.all') });
    for (const c of LO_CATS) chipDefs.push({ id: c, label: t('pick.cat.' + c) });
  }
  if (slot === 'Hands') filter = 'all';
  function renderChips() {
    chips.innerHTML = '';
    for (const c of chipDefs) {
      chips.append(h('button', { type: 'button', class: 'lo-chip' + (filter === c.id ? ' on' : ''), role: 'tab',
        'aria-selected': String(filter === c.id), text: c.label, onclick: () => { filter = c.id; renderChips(); renderList(); } }));
    }
  }
  const added = new Set();
  function renderList() {
    list.innerHTML = '';
    const q = search.value.trim().toLowerCase();
    let rows = pool;
    if (filter === 'fits') rows = rows.filter(it => (it.s || []).includes(slot));
    else if (filter !== 'all') rows = rows.filter(it => it.k === filter);
    if (q) rows = rows.filter(it => it.n.toLowerCase().includes(q) || it.c.toLowerCase().includes(q));
    rows = rows.slice().sort((a, b) => a.n.localeCompare(b.n));
    const shown = rows.slice(0, 250);
    info.textContent = cat.error && !only
      ? t('pick.noCatalog').replace('{error}', cat.error)
      : only ? tn('li.fits', { n: pool.length })
      : (rows.length > shown.length ? t('pick.more').replace('{shown}', shown.length).replace('{n}', rows.length)
        : t('pick.source').replace('{n}', cat.items.length));
    if (!shown.length) list.append(h('p', { class: 'hint lo-pick-none', text: t('pick.none') }));
    for (const it of shown) {
      const btn = h('button', { type: 'button', class: 'lo-pick-add' + (added.has(it.c) ? ' done' : ''),
        text: added.has(it.c) ? '✓ ' + t('pick.added') : '+ ' + t('pick.add'),
        onclick: () => {
          onAdd(it);
          if (single) { m.close(); return; }
          added.add(it.c); btn.className = 'lo-pick-add done'; btn.textContent = '✓ ' + t('pick.added');
        } });
      list.append(h('div', { class: 'lo-pick-row' }, [
        h('span', { class: 'lo-pick-ico' }, loIcon(loIconFor(it), 18)),
        h('span', { class: 'lo-pick-name' }, [
          h('b', { text: it.n }),
          h('code', { text: it.c }),
          it.m && it.m !== '?' ? h('span', { class: 'lo-mod', text: it.m }) : null,
        ]),
        btn,
      ]));
    }
  }
  // A class name typed by hand: modded items the catalog could not read.
  const clsIn = h('input', { type: 'text', class: 'mono', placeholder: t('pick.classPh'), spellcheck: 'false', 'aria-label': t('pick.classPh') });
  const addCls = () => {
    const v = clsIn.value.trim();
    if (!v) return;
    onAdd(loItemInfo(cat, v));
    clsIn.value = '';
    toast(t('pick.added') + ': ' + v, 'ok');
    if (single) m.close();
  };
  clsIn.addEventListener('keydown', (e) => { if (e.key === 'Enter') addCls(); });
  search.addEventListener('input', () => { clearTimeout(search._t); search._t = setTimeout(renderList, 120); });
  m.body.append(
    search, chips, info, list,
    h('details', { class: 'lo-pick-manual' }, [
      h('summary', { text: t('pick.addClass') }),
      h('div', { class: 'row', style: { gap: '8px', marginTop: '8px' } }, [clsIn, h('button', { type: 'button', text: t('pick.add'), onclick: addCls })]),
    ]),
    h('div', { class: 'actions' }, [h('span', { class: 'grow' }), h('button', { class: 'primary', text: t('pick.done'), onclick: () => m.close() })]),
  );
  if (chipDefs.length < 2) chips.style.display = 'none';
  renderChips();
  renderList();
  setTimeout(() => search.focus(), 30);
}

// ---------------------------------------------------- attribute controls

// loCondSel binds a condition select to obj.attributes. allowDefault (for
// contents) adds "as the game makes it" = no attributes at all.
function loCondSel(obj, allowDefault, onChange) {
  const opts = [...(allowDefault ? ['default'] : []), ...LO_COND.map(c => c.id), 'custom'];
  const sel = h('select', { class: 'lo-sel', 'aria-label': t('loadout.condition') },
    opts.map(id => h('option', { value: id, text: id === 'default' ? t('lo.cond.default') : t('loadout.cond.' + id) })));
  const a = obj.attributes;
  let cur = 'custom';
  if (!a || a.healthMin == null) cur = allowDefault && !a ? 'default' : 'pristine';
  else for (const c of LO_COND) if (loNear(a.healthMin, c.min) && loNear(a.healthMax, c.max)) cur = c.id;
  sel.value = cur;
  const minIn = h('input', { type: 'number', min: '0', max: '1', step: '0.05', class: 'lo-num', 'aria-label': 'min' });
  const maxIn = h('input', { type: 'number', min: '0', max: '1', step: '0.05', class: 'lo-num', 'aria-label': 'max' });
  minIn.value = String(a && a.healthMin != null ? a.healthMin : 1);
  maxIn.value = String(a && a.healthMax != null ? a.healthMax : 1);
  const custom = h('span', { class: 'lo-custom', style: { display: cur === 'custom' ? '' : 'none' } }, [minIn, h('span', { text: '–' }), maxIn]);
  const ensure = () => (obj.attributes || (obj.attributes = { quantityMin: 1, quantityMax: 1 }));
  sel.onchange = () => {
    custom.style.display = sel.value === 'custom' ? '' : 'none';
    if (sel.value === 'default') { delete obj.attributes; onChange(); return; }
    if (sel.value === 'custom') { ensure().healthMin = parseFloat(minIn.value) || 0; ensure().healthMax = parseFloat(maxIn.value) || 0; onChange(); return; }
    const c = LO_COND.find(x => x.id === sel.value);
    ensure().healthMin = c.min; ensure().healthMax = c.max;
    minIn.value = String(c.min); maxIn.value = String(c.max);
    onChange();
  };
  const onNum = () => { ensure().healthMin = parseFloat(minIn.value) || 0; ensure().healthMax = parseFloat(maxIn.value) || 0; onChange(); };
  minIn.oninput = onNum; maxIn.oninput = onNum;
  return h('span', { class: 'lo-attr' }, [sel, custom]);
}

// loQtySel binds an amount select (items with a quantity: stacks, liquids,
// magazines' rounds) to obj.attributes.quantity{Min,Max}.
function loQtySel(obj, onChange, allowDefault) {
  const ids = [...(allowDefault && !obj.attributes ? ['default'] : []), ...LO_QTY.map(q => q.id), 'custom'];
  const sel = h('select', { class: 'lo-sel', 'aria-label': t('lo.qty') },
    ids.map(id => h('option', { value: id, text: id === 'default' ? t('lo.cond.default') : t('lo.qty.' + id) })));
  const a = obj.attributes || {};
  let cur = 'custom';
  if (!obj.attributes && allowDefault) cur = 'default';
  else if (a.quantityMin == null && a.quantityMax == null) cur = 'full';
  else for (const q of LO_QTY) if (loNear(a.quantityMin, q.min) && loNear(a.quantityMax, q.max)) cur = q.id;
  sel.value = cur;
  const minIn = h('input', { type: 'number', min: '0', max: '1', step: '0.05', class: 'lo-num', 'aria-label': 'min' });
  const maxIn = h('input', { type: 'number', min: '0', max: '1', step: '0.05', class: 'lo-num', 'aria-label': 'max' });
  minIn.value = String(a.quantityMin != null ? a.quantityMin : 1);
  maxIn.value = String(a.quantityMax != null ? a.quantityMax : 1);
  const custom = h('span', { class: 'lo-custom', style: { display: cur === 'custom' ? '' : 'none' } }, [minIn, h('span', { text: '–' }), maxIn]);
  const ensure = () => (obj.attributes || (obj.attributes = { healthMin: 1, healthMax: 1 }));
  sel.onchange = () => {
    custom.style.display = sel.value === 'custom' ? '' : 'none';
    if (sel.value === 'default') { delete obj.attributes; onChange(); return; }
    if (sel.value === 'custom') { ensure().quantityMin = parseFloat(minIn.value) || 0; ensure().quantityMax = parseFloat(maxIn.value) || 0; onChange(); return; }
    const q = LO_QTY.find(x => x.id === sel.value);
    ensure().quantityMin = q.min; ensure().quantityMax = q.max;
    minIn.value = String(q.min); maxIn.value = String(q.max);
    onChange();
  };
  const onNum = () => { ensure().quantityMin = parseFloat(minIn.value) || 0; ensure().quantityMax = parseFloat(maxIn.value) || 0; onChange(); };
  minIn.oninput = onNum; maxIn.oninput = onNum;
  return h('span', { class: 'lo-attr' }, [sel, custom]);
}

// loHotbarSel binds quickBarSlot (-1 none, 0..9 = keys 1..0).
function loHotbarSel(obj, onChange) {
  const sel = h('select', { class: 'lo-sel lo-hotbar-sel', 'aria-label': t('lo.summary.hotbar') }, [
    h('option', { value: '-1', text: t('lo.hotbar.none') }),
    ...[0, 1, 2, 3, 4, 5, 6, 7, 8, 9].map(i => h('option', { value: String(i), text: t('lo.hotbar.key').replace('{n}', String((i + 1) % 10)) })),
  ]);
  sel.value = String(obj.quickBarSlot != null && obj.quickBarSlot >= 0 && obj.quickBarSlot <= 9 ? obj.quickBarSlot : -1);
  sel.onchange = () => { obj.quickBarSlot = parseInt(sel.value, 10); onChange(); };
  return sel;
}

// ------------------------------------------------------------------- page

// loStatsCard: how fed, watered and warm every fresh character starts,
// whatever kit it gets. DayZ sets these in script (energy and water 600 of
// 5000, a neutral heat buffer), so the panel's server-side mod applies them
// (/api/spawngear/stats, app.SyncServerTweaks). Each level is a share of
// the stat's maximum; game is what DayZ starts with, for the bar.
const LO_STATS = [
  { id: 'food', icon: 'i-c-food', game: 0.12, levels: [[0.4, 'lo.stat.food.fed'], [0.7, 'lo.stat.food.well'], [1, 'lo.stat.full']] },
  { id: 'water', icon: 'i-drop', game: 0.12, levels: [[0.4, 'lo.stat.water.ok'], [0.7, 'lo.stat.water.well'], [1, 'lo.stat.full']] },
  { id: 'warmth', icon: 'i-flame', game: 0, levels: [[0.5, 'lo.stat.warm'], [1, 'lo.stat.warm.full']] },
];

function loStatsCard() {
  const card = h('section', { class: 'card lo-stats' }, h('p', { class: 'hint', text: t('li.loading') }));
  api.get('/api/spawngear/stats').then((d) => {
    const cur = d.stats || {};
    const get = {};
    const rows = LO_STATS.map((s) => {
      const sel = h('select', { class: 'lo-stat-sel', 'aria-label': t('lo.stat.' + s.id) }, [
        h('option', { value: 'default', text: t('lo.stat.game.' + s.id) }),
        ...s.levels.map(([v, k]) => h('option', { value: String(v), text: t(k) + ' · ' + Math.round(v * 100) + '%' })),
        h('option', { value: 'custom', text: t('lo.stat.custom') }),
      ]);
      const num = h('input', { type: 'number', min: '0', max: '100', step: '5', class: 'lo-stat-num', 'aria-label': t('lo.stat.' + s.id) + ' %' });
      const custom = h('span', { class: 'lo-stat-custom' }, [num, h('span', { text: '%' })]);
      const fill = h('span', { class: 'lo-stat-fill' });
      const v = cur[s.id];
      const lv = v == null ? null : s.levels.find(([x]) => Math.abs(x - v) < 0.001);
      sel.value = v == null ? 'default' : lv ? String(lv[0]) : 'custom';
      num.value = String(Math.round((v ?? s.game) * 100));
      get[s.id] = () => (sel.value === 'default' ? null
        : sel.value === 'custom' ? Math.max(0, Math.min(100, Number(num.value) || 0)) / 100 : Number(sel.value));
      const sync = () => {
        custom.hidden = sel.value !== 'custom';
        const x = get[s.id]();
        fill.style.width = Math.round((x ?? s.game) * 100) + '%';
        fill.classList.toggle('game', x == null);
        update();
      };
      sel.onchange = sync;
      num.oninput = sync;
      const row = h('div', { class: 'lo-stat lo-stat-' + s.id }, [
        h('span', { class: 'lo-stat-ico' }, loIcon(s.icon, 18)),
        h('div', { class: 'lo-stat-main' }, [
          h('span', { class: 'lo-stat-label', text: t('lo.stat.' + s.id) }),
          h('span', { class: 'lo-stat-bar' }, fill),
          h('div', { class: 'lo-stat-ctl' }, [sel, custom]),
        ]),
      ]);
      row._sync = sync;
      return row;
    });
    const body = () => ({ food: get.food(), water: get.water(), warmth: get.warmth() });
    let saved = JSON.stringify(body());
    const state = h('span', { class: 'lo-stat-state' });
    const saveBtn = h('button', { type: 'button', class: 'primary small', i18n: 'action.save', onclick: (e) => withBusy(e.currentTarget, async () => {
      try {
        const r = await api.post('/api/spawngear/stats', body());
        saved = JSON.stringify(body());
        update();
        toast(r.applied ? t('msg.saved') : t('lo.stats.nextStart'), 'ok');
      } catch (err) { handleErr(err); }
    }) });
    function update() {
      const dirty = JSON.stringify(body()) !== saved;
      saveBtn.disabled = !dirty;
      card.classList.toggle('dirty', dirty);
      state.textContent = dirty ? t('lo.stats.unsaved') : '';
    }
    card.replaceChildren(
      withHelp(h('h3', { i18n: 'lo.stats.title' }), 'loadout.stats'),
      h('p', { class: 'hint', i18n: 'lo.stats.hint' }),
      h('div', { class: 'lo-stats-grid' }, rows),
      h('div', { class: 'lo-stats-foot' }, [state, h('span', { class: 'grow' }), saveBtn]),
    );
    rows.forEach(r => r._sync());
    applyI18n();
  }).catch((e) => { card.replaceChildren(h('p', { class: 'hint', text: e.message || String(e) })); });
  return card;
}

Views.loadout = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.loadout', 'loadout.subtitle', [revealBtn({ kind: 'mission' }, { label: 'folders.mission' })]));
  if (State.serverStatus.running) root.append(runningBanner());

  let d;
  try { d = await api.get('/api/spawngear'); }
  catch (e) { handleErr(e); return; }
  const cat = await loCatalog();
  if (myNav !== _navSeq) return;
  const locked = !!State.serverStatus.running;

  // ---- status ----------------------------------------------------------
  const statusCard = h('div', { class: 'card lo-status' });
  root.append(statusCard, loStatsCard());
  const enableBtn = (key) => h('button', { class: 'primary', i18n: key, disabled: locked,
    onclick: (e) => withBusy(e.currentTarget, async () => {
      try {
        const r = await api.post('/api/spawngear/enable', {});
        const reused = (r.linked || []).filter(f => f !== r.created);
        toast(reused.length ? t('loadout.linked').replace('{files}', reused.join(', ')) : t('msg.saved'), 'ok');
        await navigate('loadout');
      } catch (err) { handleErr(err); }
    }) });
  const linkBtn = (file) => h('button', { i18n: 'loadout.connect', disabled: locked,
    onclick: (e) => withBusy(e.currentTarget, async () => {
      try { await api.post('/api/spawngear/register', { file }); toast(t('msg.saved'), 'ok'); await navigate('loadout'); }
      catch (err) { handleErr(err); }
    }) });
  const presets = (d.presets || []);
  const good = presets.filter(p => !p.missing && !p.invalid);
  const broken = presets.filter(p => p.missing || p.invalid);
  const unlinked = (d.unlinked || []).filter(p => !p.invalid);

  statusCard.append(withHelp(h('h3', { i18n: 'loadout.title' }), 'loadout'));
  if (d.gameplayError) {
    statusCard.append(
      h('p', { class: 'loadout-state err', text: t('loadout.state.gameplayError').replace('{error}', d.gameplayError) }),
      h('div', { class: 'actions' }, h('button', { class: 'primary', i18n: 'loadout.openGameplay', onclick: () => navigate('gameplay') })));
    applyI18n();
    return;
  }
  if (!presets.length && unlinked.length) {
    statusCard.append(
      h('p', { class: 'loadout-state warn', i18n: 'loadout.state.unlinked' }),
      h('ul', { class: 'loadout-unlinked' }, unlinked.map(p => h('li', {}, [h('b', { text: p.name || p.file }), ' ', h('span', { class: 'attach-cat', text: p.file })]))),
      h('div', { class: 'actions' }, enableBtn('loadout.connectAll')));
    applyI18n();
    return;
  }
  if (!presets.length) {
    statusCard.append(
      h('p', { class: 'loadout-state', i18n: 'loadout.state.initc' }),
      h('div', { class: 'actions' }, enableBtn('loadout.enable')));
    applyI18n();
    return;
  }
  if (d.active) statusCard.append(h('p', { class: 'loadout-state ok', i18n: 'loadout.state.active' }));
  else if (d.blocked) statusCard.append(h('p', { class: 'loadout-state err', i18n: 'loadout.state.blocked' }));
  else if (!d.flagEnabled) statusCard.append(h('p', { class: 'loadout-state warn', i18n: 'loadout.state.flagOff' }), h('div', { class: 'actions' }, enableBtn('loadout.turnOn')));
  // Broken entries: DayZ stops at the first one, so they come first.
  for (const p of broken) {
    statusCard.append(h('div', { class: 'row lo-broken' }, [
      h('b', { text: p.file }),
      p.missing ? h('span', { class: 'kind-badge err', i18n: 'loadout.missing' }) : null,
      p.invalid ? h('span', { class: 'kind-badge err', i18n: 'loadout.invalid', title: p.invalid }) : null,
      h('span', { class: 'grow' }),
      h('button', { class: 'danger', i18n: 'loadout.removeEntry', disabled: locked, onclick: (e) => withBusy(e.currentTarget, async () => {
        try { await api.del('/api/spawngear/delete?file=' + encodeURIComponent(p.file)); toast(t('msg.deleted'), 'ok'); await navigate('loadout'); }
        catch (err) { handleErr(err); }
      }) }),
    ]));
  }
  if (unlinked.length) {
    statusCard.append(h('div', { class: 'loadout-unlinked-box' }, [
      h('h4', { i18n: 'loadout.unlinked.title' }),
      h('p', { class: 'hint', i18n: 'loadout.unlinked.hint' }),
      ...unlinked.map(p => h('div', { class: 'row loadout-unlinked-row' }, [
        h('b', { text: p.name || p.file }), h('span', { class: 'attach-cat', text: p.file }), h('span', { class: 'grow' }), linkBtn(p.file),
      ])),
    ]));
  }

  // ---- kits --------------------------------------------------------------
  const tabs = h('div', { class: 'lo-tabs', role: 'tablist' });
  const editor = h('div', { class: 'lo-editor' });
  root.append(tabs, editor);

  let file = good.length ? good[0].file : '';
  let kit = null;
  let saved = '';
  const snap = () => JSON.stringify(loToPreset(JSON.parse(JSON.stringify(kit))));
  const dirty = () => !!kit && snap() !== saved;
  root._isDirty = dirty;
  const totalWeight = () => good.reduce((s, p) => s + (p.file === file && kit ? Math.max(1, parseInt(kit.weight, 10) || 1) : (p.weight || 1)), 0)
    + (file === '' && kit ? Math.max(1, parseInt(kit.weight, 10) || 1) : 0);

  function renderTabs() {
    tabs.innerHTML = '';
    const total = totalWeight();
    const tab = (p, isNew) => {
      const on = isNew ? file === '' : p.file === file;
      const w = on && kit ? Math.max(1, parseInt(kit.weight, 10) || 1) : (p ? p.weight || 1 : 1);
      const sub = isNew ? t('lo.kit.unsaved')
        : (good.length + (file === '' && kit ? 1 : 0) > 1 ? t('lo.kit.chance').replace('{pct}', Math.round(w * 100 / total)) : t('lo.kit.every'));
      const nm = on && kit ? (kit.name || t('lo.kit.new')) : (isNew ? t('lo.kit.new') : p.name || p.file);
      return h('button', { type: 'button', class: 'lo-tab' + (on ? ' on' : ''), role: 'tab', 'aria-selected': String(on),
        onclick: async () => { if (!on) await openKit(isNew ? '' : p.file); } }, [
        h('span', { class: 'lo-tab-ico' }, loIcon('i-loadout', 16)),
        h('span', { class: 'lo-tab-txt' }, [h('b', { text: nm + (on && dirty() ? ' •' : '') }), h('small', { text: sub })]),
      ]);
    };
    for (const p of good) tabs.append(tab(p, false));
    if (file === '' && kit) tabs.append(tab(null, true));
    tabs.append(h('button', { type: 'button', class: 'lo-tab lo-tab-new', disabled: locked,
      onclick: () => openKit('', true) }, [h('span', { class: 'lo-tab-ico' }, loIcon('i-plus', 16)), h('span', { i18n: 'lo.kit.new' })]));
    tabs.append(h('span', { class: 'grow' }));
    tabs.append(h('button', { type: 'button', class: 'secondary small', i18n: 'loadout.import', disabled: locked, onclick: importPreset }));
    if (file) tabs.append(h('button', { type: 'button', class: 'secondary small', i18n: 'loadout.export', onclick: () => exportPreset(file) }));
    applyI18n();
  }

  async function openKit(f, fresh) {
    if (dirty() && !(await confirmModal(t('lo.leave'), { danger: true, okText: t('lo.discard') }))) return;
    let p;
    if (f) {
      try { p = await api.get('/api/spawngear/preset?file=' + encodeURIComponent(f)); }
      catch (e) { handleErr(e); return; }
    } else {
      p = { spawnWeight: 1, name: fresh ? t('lo.kit.new') : '', characterTypes: [], attachmentSlotItemSets: [], discreteUnsortedItemSets: [] };
    }
    file = f;
    kit = loFromPreset(p);
    if (!kit.bundles.length) kit.bundles.push({ name: 'Cargo', weight: 1, items: [] });
    saved = f ? snap() : '';
    renderTabs();
    renderEditor();
  }

  // ---- editor ------------------------------------------------------------
  const changed = () => { renderSummary(); renderSaveBar(); renderTabsLight(); };
  let tabsTimer = 0;
  function renderTabsLight() { clearTimeout(tabsTimer); tabsTimer = setTimeout(renderTabs, 150); }

  let dollHost, summaryHost, carryHost, saveBar, jsonArea;
  function renderEditor() {
    editor.innerHTML = '';
    if (!kit) return;
    const main = h('div', { class: 'lo-main' });
    summaryHost = h('aside', { class: 'card lo-summary' });
    editor.append(main, summaryHost);

    // 1 — name and who gets it
    const nameIn = h('input', { type: 'text', value: kit.name, maxlength: '64', placeholder: t('loadout.name.ph'), 'aria-label': t('lo.kitName') });
    nameIn.oninput = () => { kit.name = nameIn.value; changed(); };
    const weightIn = h('input', { type: 'number', min: '1', step: '1', value: String(kit.weight), class: 'lo-weight-in', 'aria-label': t('lo.chance') });
    weightIn.oninput = () => { kit.weight = Math.max(1, parseInt(weightIn.value, 10) || 1); changed(); };
    main.append(h('section', { class: 'card lo-step' }, [
      loStepHead(1, 'lo.step.who', 'lo.step.who.hint'),
      h('div', { class: 'lo-who' }, [
        h('label', { class: 'lo-field' }, [h('span', { class: 'lo-flabel', i18n: 'lo.kitName' }), nameIn]),
        h('div', { class: 'lo-field' }, [withHelp(h('span', { class: 'lo-flabel', i18n: 'lo.chars' }), 'loadout.characters'), charsControl()]),
        (good.length + (file === '' ? 1 : 0) > 1 || kit.weight > 1)
          ? h('label', { class: 'lo-field lo-field-narrow' }, [withHelp(h('span', { class: 'lo-flabel', i18n: 'lo.chance' }), 'loadout.weight'), weightIn]) : null,
      ]),
    ]));

    // 2 — what they wear
    dollHost = h('div', { class: 'lo-doll' });
    main.append(h('section', { class: 'card lo-step' }, [loStepHead(2, 'lo.step.wear', 'lo.step.wear.hint'), dollHost]));
    renderDoll();

    // 3 — what they carry
    carryHost = h('div', { class: 'lo-carry' });
    main.append(h('section', { class: 'card lo-step' }, [loStepHead(3, 'lo.step.carry', 'lo.step.carry.hint'), carryHost]));
    renderCarry();

    // Advanced: the JSON
    jsonArea = h('textarea', { class: 'mono lo-json', rows: '16', spellcheck: 'false', 'aria-label': t('lo.json.title') });
    const det = h('details', { class: 'card lo-json-card' }, [
      h('summary', {}, [h('b', { i18n: 'lo.json.title' }), h('span', { class: 'hint', i18n: 'lo.json.hint' })]),
      jsonArea,
      h('div', { class: 'actions' }, [h('button', { type: 'button', i18n: 'lo.json.apply', onclick: applyJSON })]),
    ]);
    det.addEventListener('toggle', () => { if (det.open) jsonArea.value = JSON.stringify(loToPreset(JSON.parse(JSON.stringify(kit))), null, 4); });
    main.append(det);

    saveBar = h('div', { class: 'lo-savebar' });
    main.append(saveBar);
    renderSummary();
    renderSaveBar();
    applyI18n();
  }

  function loStepHead(n, key, hintKey) {
    return h('div', { class: 'lo-step-head' }, [
      h('span', { class: 'lo-step-n', text: String(n) }),
      h('div', {}, [h('h3', { i18n: key }), h('p', { class: 'hint', i18n: hintKey })]),
    ]);
  }

  // Characters: any / men / women / a chosen set.
  function charsControl() {
    const surv = cat.survivors || [];
    const males = surv.filter(s => !s.f).map(s => s.c), females = surv.filter(s => s.f).map(s => s.c);
    const same = (a, b) => a.length === b.length && a.every(x => b.includes(x));
    const mode = () => !kit.chars.length ? 'any' : (males.length && same(kit.chars, males) ? 'male' : (females.length && same(kit.chars, females) ? 'female' : 'pick'));
    const sel = h('select', { class: 'lo-chars-sel', 'aria-label': t('lo.chars') },
      ['any', 'male', 'female', 'pick'].map(id => h('option', { value: id, text: t('lo.chars.' + id) })));
    sel.value = mode();
    const pickBox = h('div', { class: 'lo-chars-pick' });
    function renderPick() {
      pickBox.innerHTML = '';
      pickBox.style.display = sel.value === 'pick' ? '' : 'none';
      if (sel.value !== 'pick') return;
      const all = surv.map(s => s.c);
      // Classes the catalog does not list (a mod's survivors) stay selectable.
      for (const c of kit.chars) if (!all.includes(c)) all.push(c);
      for (const c of all) {
        const s = surv.find(x => x.c === c);
        const cb = h('input', { type: 'checkbox' });
        cb.checked = kit.chars.includes(c);
        cb.onchange = () => {
          if (cb.checked && !kit.chars.includes(c)) kit.chars.push(c);
          if (!cb.checked) kit.chars = kit.chars.filter(x => x !== c);
          changed();
        };
        pickBox.append(h('label', { class: 'lo-char' + (s && s.f ? ' f' : ''), title: c }, [cb, h('span', { text: s ? s.n : c }),
          s ? h('span', { class: 'lo-char-sex', 'aria-label': t(s.f ? 'lo.char.f' : 'lo.char.m'), text: s.f ? '♀' : '♂' }) : null]));
      }
    }
    sel.onchange = () => {
      if (sel.value === 'any') kit.chars = [];
      else if (sel.value === 'male') kit.chars = males.slice();
      else if (sel.value === 'female') kit.chars = females.slice();
      else if (!kit.chars.length) kit.chars = males.concat(females);
      renderPick();
      changed();
    };
    renderPick();
    return h('div', { class: 'lo-chars' }, [sel, pickBox]);
  }

  // ---- the figure and its slots
  function renderDoll() {
    dollHost.innerHTML = '';
    const filled = LO_SLOTS.filter(s => (kit.slots[s.id] || []).some(v => (v.itemType || '').trim()));
    const fig = h('div', { class: 'lo-figure' + filled.map(s => ' has-' + s.id.toLowerCase()).join(''), html: loDollSVG() });
    const col = (side) => h('div', { class: 'lo-col lo-col-' + side }, LO_SLOTS.filter(s => s.side === side).map(slotCard));
    dollHost.append(
      col('l'),
      h('div', { class: 'lo-center' }, [fig, h('div', { class: 'lo-filled', text: t('lo.filled').replace('{n}', filled.length).replace('{total}', LO_SLOTS.length) })]),
      col('r'),
    );
    if (kit.otherSlots.length) {
      dollHost.append(h('p', { class: 'loadout-state warn lo-other', text: t('lo.otherSlots.hint').replace('{slots}', kit.otherSlots.map(s => s.slotName || '?').join(', ')) }));
    }
  }

  function slotCard(def) {
    const list = (kit.slots[def.id] || []).filter(v => (v.itemType || '').trim());
    const first = list[0] ? loItemInfo(cat, list[0].itemType) : null;
    const inside = list.reduce((n, v) => n + (v.complexChildrenTypes || []).length, 0);
    return h('button', { type: 'button', class: 'lo-slot' + (first ? ' on' : ''), disabled: false,
      'aria-label': t(def.key) + (first ? ': ' + first.n : ''), onclick: () => openSlot(def) }, [
      h('span', { class: 'lo-slot-ico' }, loIcon(def.icon, 20)),
      h('span', { class: 'lo-slot-txt' }, [
        h('span', { class: 'lo-slot-label', text: t(def.key) }),
        first ? h('span', { class: 'lo-slot-item' + (first.unknown ? ' unknown' : ''), text: first.n })
          : h('span', { class: 'lo-slot-hint', text: t(def.key + '.hint') }),
        first && first.k === 'weapons' ? loAmmoBadge(list[0]) : null,
        list.length > 1 ? h('span', { class: 'lo-slot-rand' }, [loIcon('i-shuffle', 13), h('span', { text: t('lo.random').replace('{n}', list.length) })]) : null,
        inside ? h('span', { class: 'lo-slot-inside', text: t('lo.inside.count').replace('{n}', inside) }) : null,
      ]),
      first ? null : h('span', { class: 'lo-slot-plus', text: '+' }),
    ]);
  }

  function openSlot(def) {
    const list = kit.slots[def.id] || (kit.slots[def.id] = []);
    const m = openModal({ title: t(def.key) + ' — ' + t(def.key + '.hint'), wide: true, onClose: () => { renderDoll(); changed(); } });
    const host = h('div', { class: 'lo-variants' });
    const holdable = ['Shoulder', 'Melee', 'Hands'].includes(def.id);
    function render() {
      host.innerHTML = '';
      const items = list.filter(v => (v.itemType || '').trim());
      const total = items.reduce((s, v) => s + Math.max(1, v.spawnWeight || 1), 0);
      if (!items.length) host.append(h('p', { class: 'hint', text: t('lo.slot.empty') }));
      for (const v of items) {
        const info = loItemInfo(cat, v.itemType);
        const wIn = h('input', { type: 'number', min: '1', step: '1', value: String(v.spawnWeight || 1), class: 'lo-num', 'aria-label': t('lo.weight') });
        const pct = h('span', { class: 'lo-pct', text: Math.round(Math.max(1, v.spawnWeight || 1) * 100 / total) + '%' });
        wIn.oninput = () => { v.spawnWeight = Math.max(1, parseInt(wIn.value, 10) || 1); render(); changed(); };
        // Attachments and contents: drawn, with the parts that fit.
        const inside = info.i || info.unknown || (v.complexChildrenTypes || []).length
          ? h('div', { class: 'lo-variant-kids' }, loItemEditor(v, info, liCtx)) : null;
        host.append(h('div', { class: 'lo-variant' }, [
          h('div', { class: 'lo-variant-head' }, [
            h('span', { class: 'lo-pick-ico' }, loIcon(info.unknown ? def.icon : loIconFor(info), 18)),
            h('span', { class: 'lo-pick-name' }, [h('b', { text: info.n }), h('code', { text: v.itemType }),
              info.unknown ? h('span', { class: 'lo-unknown', text: t('lo.unknownItem') }) : null]),
            items.length > 1 ? h('label', { class: 'lo-mini' }, [h('span', { i18n: 'lo.weight' }), wIn, pct]) : null,
            loCondSel(v, false, changed),
            info.q ? loQtySel(v, changed) : null,
            holdable ? loHotbarSel(v, changed) : null,
            h('button', { type: 'button', class: 'icon-x', text: '×', title: t('action.delete'), 'aria-label': t('action.delete'),
              onclick: () => { list.splice(list.indexOf(v), 1); render(); changed(); } }),
          ]),
          inside,
        ]));
      }
      applyI18n();
    }
    render();
    m.body.append(
      h('p', { class: 'hint', i18n: 'lo.slot.dialogHint' }),
      host,
      h('div', { class: 'actions' }, [
        h('button', { type: 'button', class: 'primary', text: '+ ' + t('lo.carry.addItem'), onclick: () =>
          loPick({ title: t('pick.title').replace('{slot}', t(def.key)), slot: def.id, onAdd: (it) => {
            list.push(loNewItem(it.c, true));
            render(); changed();
          } }) }),
        list.length ? h('button', { type: 'button', class: 'danger', i18n: 'lo.slot.clear', onclick: () => { list.length = 0; render(); changed(); } }) : null,
        h('span', { class: 'grow' }),
        h('button', { type: 'button', text: t('pick.done'), onclick: () => m.close() }),
      ]),
    );
    applyI18n();
  }

  // A content item (inside a worn item, or carried): no attributes means as
  // the game makes it; items with an amount get a full one.
  function loNewChild(it) {
    const c = { itemType: it.c, quickBarSlot: -1, complexChildrenTypes: [] };
    if (it.q) c.attributes = { healthMin: 1, healthMax: 1, quantityMin: 1, quantityMax: 1 };
    return c;
  }

  // itemRow is one carried / contained item: name, condition, amount, hotbar
  // and — when things go on or into it — a button to its attachments and
  // contents. opts.noHotbar: an attached part (it cannot sit on the hotbar);
  // opts.noInside: the row heads that item's own dialog.
  function itemRow(c, allowDefault, onRemove, compact, opts = {}) {
    const info = loItemInfo(cat, c.itemType);
    const row = h('div', { class: 'lo-row' + (compact ? ' compact' : '') });
    const render = () => {
      const had = !!c.attributes;
      const onAttr = () => { changed(); if (!!c.attributes !== had) render(); };
      const n = (c.complexChildrenTypes || []).length;
      const weapon = info.k === 'weapons';
      row.replaceChildren(
        h('span', { class: 'lo-pick-ico' }, loIcon(loIconFor(info), 16)),
        h('span', { class: 'lo-pick-name' }, [h('b', { text: info.n }), h('code', { text: c.itemType }),
          info.unknown ? h('span', { class: 'lo-unknown', text: t('lo.unknownItem') }) : null, weapon ? loAmmoBadge(c) : null]),
        loCondSel(c, allowDefault, onAttr),
        info.q ? loQtySel(c, onAttr, allowDefault) : h('span'),
        opts.noHotbar ? h('span') : loHotbarSel(c, changed),
        (info.i || n) && !opts.noInside
          ? h('button', { type: 'button', class: 'lo-inside-btn' + (n ? ' on' : ''), title: t(weapon ? 'li.open.weapon' : 'li.open'),
            onclick: () => openInside(c, { after: render }) }, [loIcon(weapon ? 'i-c-scope' : 'i-c-box', 14),
            h('span', { text: t(weapon ? 'li.btn.weapon' : 'li.btn') + (n ? ' · ' + n : '') })])
          : h('span'),
        h('button', { type: 'button', class: 'icon-x', text: '×', title: t('action.delete'), 'aria-label': t('action.delete'), onclick: onRemove }),
      );
    };
    render();
    return row;
  }

  // openInside: what goes on and into one item, in its own dialog (a carried
  // rifle, the scope on a rifle). opts.row puts the item's own condition and
  // amount on top; opts.after runs when it closes.
  function openInside(c, opts = {}) {
    const info = loItemInfo(cat, c.itemType);
    const m = openModal({ title: info.n, wide: true, onClose: () => { if (opts.after) opts.after(); changed(); } });
    const parts = [];
    // Anything a player carries can sit on the hotbar, a pistol in a vest's
    // holster too (DayZ sets the key at any depth), but not a weapon's parts.
    if (opts.row) parts.push(itemRow(c, true, () => { if (opts.onRemove) opts.onRemove(); m.close(); }, true,
      { noHotbar: !!(opts.parent && opts.parent.k === 'weapons'), noInside: true }));
    parts.push(info.i || info.unknown || (c.complexChildrenTypes || []).length
      ? loItemEditor(c, info, liCtx) : h('p', { class: 'hint', i18n: 'li.nothing' }));
    parts.push(h('div', { class: 'actions' }, [h('span', { class: 'grow' }), h('button', { type: 'button', class: 'primary', text: t('pick.done'), onclick: () => m.close() })]));
    m.body.classList.add('li-dialog');
    m.body.append(...parts);
    applyI18n();
  }

  // What the item editor needs from the page.
  const liCtx = {
    info: (cls) => loItemInfo(cat, cls),
    row: (c, onRemove, opts) => itemRow(c, true, onRemove, true, opts),
    open: openInside,
    changed: () => changed(),
  };

  // ---- what they carry
  function renderCarry() {
    carryHost.innerHTML = '';
    const total = kit.bundles.reduce((s, b) => s + Math.max(1, parseInt(b.weight, 10) || 1), 0);
    kit.bundles.forEach((b, bi) => {
      const box = h('div', { class: 'lo-bundle' });
      if (kit.bundles.length > 1) {
        const wIn = h('input', { type: 'number', min: '1', step: '1', value: String(b.weight || 1), class: 'lo-num', 'aria-label': t('lo.weight') });
        wIn.oninput = () => { b.weight = Math.max(1, parseInt(wIn.value, 10) || 1); renderCarry(); changed(); };
        box.append(h('div', { class: 'lo-bundle-head' }, [
          h('b', { text: t('lo.carry.bundle').replace('{n}', bi + 1) }),
          h('span', { class: 'lo-pct', text: Math.round(Math.max(1, b.weight || 1) * 100 / total) + '%' }),
          h('label', { class: 'lo-mini' }, [h('span', { i18n: 'lo.weight' }), wIn]),
          h('span', { class: 'grow' }),
          h('button', { type: 'button', class: 'linkish', i18n: 'lo.carry.removeBundle', onclick: () => { kit.bundles.splice(bi, 1); renderCarry(); changed(); } }),
        ]));
      }
      if (!b.items.length) box.append(h('p', { class: 'hint', i18n: 'lo.carry.empty' }));
      for (const c of b.items) {
        box.append(itemRow(c, true, () => { b.items = b.items.filter(x => x !== c); renderCarry(); changed(); }));
      }
      box.append(h('button', { type: 'button', class: 'lo-add', text: '+ ' + t('lo.carry.addItem'), onclick: () =>
        loPick({ title: t('lo.carry.addItem'), onAdd: (it) => { b.items.push(loNewChild(it)); renderCarry(); changed(); } }) }));
      carryHost.append(box);
    });
    carryHost.append(h('div', { class: 'lo-bundle-add' }, [
      h('button', { type: 'button', class: 'lo-add', text: '+ ' + t('lo.carry.addBundle'),
        onclick: () => { kit.bundles.push({ name: 'Cargo', weight: 1, items: [] }); renderCarry(); changed(); } }),
      h('span', { class: 'hint', i18n: 'lo.carry.addBundle.hint' }),
    ]));
    applyI18n();
  }

  // ---- summary
  function hotbarOf() {
    const keys = Array(10).fill(null).map(() => []);
    const visit = (it) => {
      if (it.quickBarSlot != null && it.quickBarSlot >= 0 && it.quickBarSlot <= 9) keys[it.quickBarSlot].push(it);
      for (const c of it.complexChildrenTypes || []) visit(c);
    };
    for (const id of Object.keys(kit.slots)) for (const v of kit.slots[id]) visit(v);
    for (const b of kit.bundles) for (const c of b.items) visit(c);
    return keys;
  }
  function hotbarStrip(keys) {
    return h('div', { class: 'lo-hotbar', role: 'list' }, keys.map((list, i) => {
      const it = list[0] ? loItemInfo(cat, list[0].itemType) : null;
      return h('div', { class: 'lo-hk' + (it ? ' on' : '') + (list.length > 1 ? ' clash' : ''), role: 'listitem',
        title: it ? list.map(x => loItemInfo(cat, x.itemType).n).join(' / ') : '' }, [
        h('span', { class: 'lo-hk-n', text: String((i + 1) % 10) }),
        it ? loIcon(loIconFor(it), 16) : null,
      ]);
    }));
  }
  function renderSummary() {
    if (!summaryHost) return;
    summaryHost.innerHTML = '';
    const surv = cat.survivors || [];
    const m = surv.filter(s => !s.f).length, f = surv.filter(s => s.f).length;
    const who = !kit.chars.length
      ? t('lo.chars.summaryAny').replace('{m}', m || 20).replace('{f}', f || 11)
      : t('lo.chars.summaryN').replace('{n}', kit.chars.length);
    summaryHost.append(
      h('div', { class: 'lo-sum-head' }, [loIcon('i-loadout', 18), h('h3', { i18n: 'lo.summary.title' })]),
      h('p', { class: 'lo-sum-who' }, [loIcon('i-players', 15), h('span', { text: who })]),
    );
    const worn = [];
    for (const def of LO_SLOTS) {
      const list = (kit.slots[def.id] || []).filter(v => (v.itemType || '').trim());
      if (!list.length) continue;
      worn.push(h('div', { class: 'lo-sum-slot' }, [
        h('span', { class: 'lo-sum-k' }, [loIcon(def.icon, 14), h('span', { text: t(def.key) })]),
        h('span', { class: 'lo-sum-v', text: list.map(v => loItemInfo(cat, v.itemType).n).join(t('lo.summary.or')) }),
      ]));
    }
    summaryHost.append(h('div', { class: 'lo-sum-sec' }, [h('h4', { i18n: 'lo.summary.wears' }),
      worn.length ? h('div', {}, worn) : h('p', { class: 'hint', i18n: 'lo.summary.nothing' })]));
    const bundles = kit.bundles.filter(b => b.items.length);
    const carry = bundles.map(b => b.items.map(c => loItemInfo(cat, c.itemType).n).join(', '));
    summaryHost.append(h('div', { class: 'lo-sum-sec' }, [
      h('h4', { i18n: 'lo.summary.carries' }),
      carry.length ? h('p', { class: 'lo-sum-v', text: carry.join(' | ') + (bundles.length > 1 ? ' — ' + t('lo.summary.oneOf').replace('{n}', bundles.length) : '') })
        : h('p', { class: 'hint', i18n: 'lo.summary.nothing' }),
    ]));
    const keys = hotbarOf();
    const clash = keys.findIndex(l => l.length > 1);
    summaryHost.append(h('div', { class: 'lo-sum-sec' }, [
      h('h4', { i18n: 'lo.summary.hotbar' }),
      hotbarStrip(keys),
      clash >= 0 ? h('p', { class: 'hint warn-text', text: t('lo.hotbar.clash').replace('{n}', String((clash + 1) % 10)) }) : null,
    ]));
    const rollHost = h('div', { class: 'lo-roll' });
    summaryHost.append(h('div', { class: 'lo-sum-sec' }, [
      h('button', { type: 'button', class: 'secondary', i18n: 'loadout.testRoll', onclick: () => roll(rollHost) }), rollHost,
    ]));
    summaryHost.append(h('p', { class: 'hint lo-sum-file', text: t('lo.summary.file').replace('{file}', file || t('lo.kit.unsaved')) }));
    applyI18n();
  }

  // One simulated fresh spawn: DayZ's weighted picks and a health draw.
  function roll(host) {
    host.innerHTML = '';
    const pick = (arr, w) => {
      const items = arr.filter(x => w(x) > 0);
      if (!items.length) return null;
      let r = Math.random() * items.reduce((s, x) => s + w(x), 0);
      for (const x of items) { r -= w(x); if (r <= 0) return x; }
      return items[items.length - 1];
    };
    const cond = (a) => {
      const min = a && a.healthMin != null ? a.healthMin : 1, max = a && a.healthMax != null ? a.healthMax : 1;
      const v = min + Math.random() * (Math.max(min, max) - min);
      const id = v > 0.9 ? 'pristine' : v > 0.7 ? 'worn' : v > 0.5 ? 'damaged' : v > 0.3 ? 'badly' : 'ruined';
      return t('loadout.cond.' + id);
    };
    const surv = cat.survivors || [];
    const pool = kit.chars.length ? kit.chars : surv.map(s => s.c);
    const who = pool.length ? pool[Math.floor(Math.random() * pool.length)] : '';
    const sv = surv.find(s => s.c === who);
    host.append(h('div', { class: 'lo-roll-line' }, [h('b', { i18n: 'loadout.roll.character' }), h('span', { text: sv ? sv.n : (who || t('loadout.roll.anyChar')) })]));
    for (const def of LO_SLOTS) {
      const v = pick((kit.slots[def.id] || []).filter(x => (x.itemType || '').trim()), x => Math.max(1, x.spawnWeight || 1));
      if (!v) continue;
      const parts = (v.complexChildrenTypes || []).map(c => loItemInfo(cat, c.itemType).n);
      host.append(h('div', { class: 'lo-roll-line' }, [h('span', { class: 'lo-sum-k', text: t(def.key) }),
        h('span', { text: loItemInfo(cat, v.itemType).n + ' · ' + cond(v.attributes) + (parts.length ? ' — ' + parts.join(', ') : '') })]));
    }
    const b = pick(kit.bundles.filter(x => x.items.length), x => Math.max(1, parseInt(x.weight, 10) || 1));
    if (b) host.append(h('div', { class: 'lo-roll-line' }, [h('b', { i18n: 'loadout.roll.carrying' }),
      h('span', { text: b.items.map(c => loItemInfo(cat, c.itemType).n).join(', ') })]));
    applyI18n();
  }

  // ---- save / discard / delete
  function renderSaveBar() {
    if (!saveBar) return;
    saveBar.innerHTML = '';
    const isDirty = dirty() || !file;
    saveBar.classList.toggle('dirty', isDirty);
    // Element.append(null) would print "null": optional buttons go via a list.
    const parts = [
      h('span', { class: 'lo-save-state', text: isDirty ? t('lo.unsaved') : t('lo.allSaved') }),
      h('span', { class: 'grow' }),
    ];
    if (file) parts.push(h('button', { type: 'button', class: 'danger secondary', i18n: 'lo.kit.delete', disabled: locked, onclick: (e) => deleteKit(e.currentTarget) }));
    if (isDirty && file) parts.push(h('button', { type: 'button', i18n: 'lo.discard', onclick: async () => { kit = null; await openKit(file); } }));
    parts.push(h('button', { type: 'button', class: 'primary', i18n: 'lo.save', disabled: locked || !isDirty, onclick: (e) => withBusy(e.currentTarget, save) }));
    saveBar.append(...parts);
    applyI18n();
  }
  async function save() {
    if (!kit.name.trim()) { toast(t('loadout.needName'), 'error'); return; }
    const preset = loToPreset(JSON.parse(JSON.stringify(kit)));
    try {
      const r = await api.post('/api/spawngear/save', { file, preset });
      toast(t('lo.saved'), 'ok');
      if (!r.flagSet && !d.flagEnabled) toast(t('loadout.enabledNote'), 'ok');
      // Reload the page (status, tabs), back on this kit.
      _loPrefer = r.file || file;
      kit = null;
      await navigate('loadout');
    } catch (e) { handleErr(e); }
  }

  async function deleteKit(btn) {
    if (!(await confirmModal(t('confirm.delete').replace('{name}', kit.name || file), { danger: true, okText: t('action.delete') }))) return;
    await withBusy(btn, async () => {
      try { await api.del('/api/spawngear/delete?file=' + encodeURIComponent(file)); toast(t('msg.deleted'), 'ok'); kit = null; await navigate('loadout'); }
      catch (err) { handleErr(err); }
    });
  }

  function applyJSON() {
    let p;
    try { p = JSON.parse(jsonArea.value); }
    catch (e) { toast(t('lo.json.bad').replace('{error}', e.message), 'error'); return; }
    if (!p || typeof p !== 'object' || Array.isArray(p)) { toast(t('lo.json.bad').replace('{error}', '{ }'), 'error'); return; }
    kit = loFromPreset(p);
    if (!kit.bundles.length) kit.bundles.push({ name: 'Cargo', weight: 1, items: [] });
    renderEditor();
    renderTabs();
  }

  // ---- import / export (plain JSON files, shared between servers)
  async function exportPreset(f) {
    try {
      const preset = await api.get('/api/spawngear/preset?file=' + encodeURIComponent(f));
      const blob = new Blob([JSON.stringify(preset, null, 4)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url; a.download = f.split('/').pop() || 'loadout.json';
      document.body.appendChild(a); a.click(); a.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (e) { handleErr(e); }
  }
  function importPreset() {
    const inp = h('input', { type: 'file', accept: '.json,application/json', style: { display: 'none' } });
    inp.onchange = async () => {
      const fl = inp.files && inp.files[0];
      if (!fl) { inp.remove(); return; }
      try {
        let preset;
        try { preset = JSON.parse(await fl.text()); }
        catch { toast(t('loadout.import.badJson'), 'error'); return; }
        if (!preset || typeof preset !== 'object' || !Array.isArray(preset.attachmentSlotItemSets)) {
          if (!(await confirmModal(t('loadout.import.notPreset'), { danger: true, okText: t('action.confirm') }))) return;
        }
        let name = (preset.name || '').trim();
        if (!name) {
          name = await promptModal(t('loadout.import.name'), { value: fl.name.replace(/\.json$/i, '') });
          if (!name) return;
        }
        preset.name = name;
        const r = await api.post('/api/spawngear/save', { file: '', preset });
        toast(t('loadout.import.done').replace('{name}', name), 'ok');
        _loPrefer = r.file || '';
        kit = null;
        await navigate('loadout');
      } catch (e) { handleErr(e); }
      finally { inp.remove(); }
    };
    document.body.appendChild(inp); inp.click();
  }

  const prefer = good.find(p => p.file === _loPrefer);
  _loPrefer = '';
  if (good.length) await openKit((prefer || good[0]).file);
  else renderTabs();
  applyI18n();
};
