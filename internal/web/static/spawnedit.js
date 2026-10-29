// Copyright (c) 2026 Aristarh Ucolov.
//
// The drawing-based editor of one cfgspawnabletypes.xml entry, shared by the
// Attachments and Vehicles pages. The game files say which attachment slots
// an item or a vehicle has and which parts fit each (GET /api/items/detail).
// The entry's <attachments> groups are placed on those slots — a group goes
// to the first free slot one of its parts fits, the way DayZ attaches it
// (CreateInInventory, in the order of the parent's attachments[]) — and drawn
// as tiles around the item: the slot, its chance, its part. A click on a tile
// opens that group in the list under the drawing; an empty tile adds a group
// made of the parts that fit. Groups the drawing cannot place (a preset
// reference, parts the game files do not describe) stay in the list.

// ------------------------------------------------------------------ slots

// What a vehicle slot is (Sedan_02_Wheel_1_1, NivaDriverDoors, CarBattery…),
// the side of the drawing its tile goes to, the part it lights up and its
// place on that side. Wheel_<side>_<axle>: side 1 is left, 2 right; axle 1 the
// front, 2 and 3 the rear. Door_<side>_<row>: row 1 front, 2 rear.
function seVehSlot(slot) {
  const s = String(slot);
  const side = (n) => (n === '1' ? 'l' : 'r');
  let m;
  if ((m = /wheel_spare_?(\d*)/i.exec(s))) {
    const n = m[1] && m[1] !== '1' ? m[1] : '';
    return { label: t('veh.slot.spare') + (n ? ' ' + n : ''), pos: 'bottom', part: 'spare' + (n || '1'), order: 3 + (Number(n) || 1) };
  }
  if ((m = /wheel_(\d)_(\d)/i.exec(s))) {
    const sd = side(m[1]), axle = Number(m[2]);
    const id = (axle === 1 ? 'f' : 'r') + sd + (axle >= 3 ? '2' : '');
    return { label: t('veh.slot.wheel.' + id), pos: sd === 'l' ? 'left' : 'right', part: 'wheel_' + sd + Math.min(axle, 3), order: axle === 1 ? 0 : 10 + axle };
  }
  const door = (sd, row) => ({ label: t('veh.slot.door.' + (row === '1' ? 'f' : 'r') + side(sd)),
    pos: sd === '1' ? 'left' : 'right', part: 'door_' + side(sd) + row, order: row === '1' ? 1 : 2 });
  if ((m = /door_(\d)_(\d)/i.exec(s))) return door(m[1], m[2]);
  if (/codriverdoors?$/i.test(s)) return door('2', '1');
  if (/driverdoors?$/i.test(s)) return door('1', '1');
  if (/cargo1doors?$/i.test(s)) return door('1', '2');
  if (/cargo2doors?$/i.test(s)) return door('2', '2');
  if (/hood/i.test(s)) return { label: t('veh.slot.hood'), pos: 'top', part: 'hood', order: 2 };
  if (/trunk/i.test(s)) return { label: t('veh.slot.trunk'), pos: 'bottom', part: 'trunk', order: 1 };
  if ((m = /reflector_(\d)/i.exec(s))) return { label: t(m[1] === '1' ? 'veh.slot.lightL' : 'veh.slot.lightR'), pos: 'top', part: 'light' + m[1], order: m[1] === '1' ? 1 : 3 };
  if (/radiator/i.test(s)) return { label: t('veh.slot.radiator'), pos: 'top', part: 'radiator', order: 4 };
  if (/battery/i.test(s)) return { label: t('veh.slot.battery'), pos: 'top', part: 'battery', order: 5 };
  if (/glowplug/i.test(s)) return { label: t('veh.slot.glowplug'), pos: 'top', part: 'plug', order: 6 };
  if (/sparkplug/i.test(s)) return { label: t('veh.slot.plug'), pos: 'top', part: 'plug', order: 6 };
  if (/belt/i.test(s)) return { label: t('veh.slot.belt'), pos: 'top', part: 'belt', order: 7 };
  // A truck's load and whatever a mod adds: named after the slot.
  return { label: loHumanize(s.replace(/^[A-Za-z]+_\d+_/, '')), pos: 'bottom', part: 'cargo', order: 20 };
}

// seSlots describes the slots of an item or vehicle (its detail), in the
// order the game fills them; a weapon's magazine is one more.
function seSlots(det) {
  const veh = det.k === 'vehicles';
  const slots = (det.attach || []).map((s, i) => {
    const a = veh ? seVehSlot(s.slot) : Object.assign({ order: i, label: loAttLabel(s.slot) }, loAttInfo(s.slot));
    return Object.assign({ slot: s.slot, refs: s.items || [], set: new Set((s.items || []).map(x => String(x.c).toLowerCase())) }, a);
  });
  if (!veh && det.mags && det.mags.length) {
    slots.push({ slot: 'magazine', refs: det.mags, set: new Set(det.mags.map(x => String(x.c).toLowerCase())),
      label: t('att.magazine'), pos: 'bottom', part: 'mag', order: 99, mag: true });
  }
  return slots;
}

// seAssign places groups on slots: each slot takes the first group not yet
// placed that has a part fitting it.
function seAssign(groups, slots) {
  const bySlot = new Map(), slotOf = new Map();
  for (const s of slots) {
    const g = groups.find(x => !slotOf.has(x) && typeof x.preset !== 'string' &&
      (x.items || []).some(it => s.set.has(String(it.name || '').trim().toLowerCase())));
    if (g) { bySlot.set(s, g); slotOf.set(g, s); }
  }
  return { bySlot, slotOf, rest: groups.filter(g => !slotOf.has(g)) };
}

// seBestRef picks the part a new group starts with: a colour variant's own
// part when there is one (Sedan_02_Red → …_Red), otherwise the plain one —
// never a ruined or rusty spare.
function seBestRef(s, cls) {
  const good = s.refs.filter(r => !/ruined|rust|broken|destroyed|damaged/i.test(r.c));
  const pool = good.length ? good : s.refs;
  const m = /_([A-Za-z]+)$/.exec(cls || '');
  if (m) {
    const hit = pool.find(r => r.c.toLowerCase().endsWith('_' + m[1].toLowerCase()));
    if (hit) return hit;
  }
  return pool.slice().sort((a, b) => a.c.length - b.c.length || a.c.localeCompare(b.c))[0];
}

// ---------------------------------------------------------------- drawing

// seVehArt draws a vehicle from above, front up: a car (two or four doors),
// a truck (three axles, a load bed) or a boat. The parts named in on (a Set)
// are drawn fitted, the others as faint outlines. Original line art.
function seVehArt(det, on) {
  const slots = (det.attach || []).map(s => s.slot);
  const has = (re) => slots.some(s => re.test(s));
  const p = (id, svg) => `<g class="li-part li-${id}${on.has(id) ? ' on' : ''}">${svg}</g>`;
  // A part lit by any of several slots (a bike's wheel may be named left or right).
  const pa = (ids, svg) => `<g class="li-part li-${ids[0]}${ids.some(i => on.has(i)) ? ' on' : ''}">${svg}</g>`;
  if (/moto|bike/i.test(det.c)) {
    return `<svg viewBox="0 0 160 420" class="li-svg se-veh-svg" aria-hidden="true">
  <path class="li-body" d="M64 92 Q80 78 96 92 L102 250 Q80 266 58 250 Z"/>
  <path class="li-stroke" d="M38 96 Q80 84 122 96"/>
  <rect class="li-body" x="66" y="252" width="28" height="70" rx="12"/>
  ${pa(['wheel_l1', 'wheel_r1'], '<rect x="68" y="22" width="24" height="70" rx="11"/>')}
  ${pa(['wheel_l2', 'wheel_r2'], '<rect x="68" y="324" width="24" height="76" rx="11"/>')}
  ${pa(['light1', 'light2'], '<ellipse cx="80" cy="96" rx="12" ry="7"/>')}
  ${p('battery', '<rect x="100" y="170" width="16" height="22" rx="2"/>')}
  ${p('plug', '<rect x="46" y="176" width="10" height="16" rx="2"/>')}
  ${p('spare1', '<circle cx="80" cy="196" r="12"/>')}
</svg>`;
  }
  if (/boat/i.test(det.c) || (!has(/wheel/i) && has(/plug/i))) {
    return `<svg viewBox="0 0 220 420" class="li-svg se-veh-svg" aria-hidden="true">
  <path class="li-body" d="M110 16 Q172 80 178 200 L174 372 Q172 402 110 406 Q48 402 46 372 L42 200 Q48 80 110 16 Z"/>
  <path class="li-stroke" d="M110 40 L110 330 M70 150 H150 M64 250 H156"/>
  ${p('plug', '<rect x="88" y="376" width="44" height="30" rx="5"/>')}
</svg>`;
  }
  if (has(/wheel_\d_3/i) || /truck/i.test(det.c)) {
    return `<svg viewBox="0 0 260 470" class="li-svg se-veh-svg" aria-hidden="true">
  <path class="li-body" d="M84 24 Q130 10 176 24 L190 66 L190 150 L70 150 L70 66 Z"/>
  <rect class="li-body" x="62" y="158" width="136" height="296" rx="6"/>
  <path class="li-stroke" d="M78 74 Q130 62 182 74"/>
  ${p('hood', '<path d="M86 28 Q130 14 174 28 L186 62 L74 62 Z"/>')}
  ${p('cargo', '<rect x="74" y="170" width="112" height="272" rx="4"/>')}
  ${p('light1', '<ellipse cx="96" cy="26" rx="12" ry="6"/>')}
  ${p('light2', '<ellipse cx="164" cy="26" rx="12" ry="6"/>')}
  ${p('battery', '<rect x="112" y="96" width="36" height="18" rx="2"/>')}
  ${p('door_l1', '<rect x="58" y="76" width="12" height="66" rx="3"/>')}
  ${p('door_r1', '<rect x="190" y="76" width="12" height="66" rx="3"/>')}
  ${p('wheel_l1', '<rect x="38" y="84" width="22" height="54" rx="7"/>')}
  ${p('wheel_r1', '<rect x="200" y="84" width="22" height="54" rx="7"/>')}
  ${p('wheel_l2', '<rect x="38" y="300" width="22" height="54" rx="7"/>')}
  ${p('wheel_r2', '<rect x="200" y="300" width="22" height="54" rx="7"/>')}
  ${p('wheel_l3', '<rect x="38" y="366" width="22" height="54" rx="7"/>')}
  ${p('wheel_r3', '<rect x="200" y="366" width="22" height="54" rx="7"/>')}
  ${p('spare1', '<circle cx="104" cy="204" r="18"/>')}
  ${p('spare2', '<circle cx="156" cy="204" r="18"/>')}
</svg>`;
  }
  const four = has(/door_\d_2|cargo\ddoors?/i);
  const doors = four
    ? p('door_l1', '<rect x="60" y="146" width="12" height="66" rx="3"/>') + p('door_l2', '<rect x="60" y="218" width="12" height="66" rx="3"/>') +
      p('door_r1', '<rect x="188" y="146" width="12" height="66" rx="3"/>') + p('door_r2', '<rect x="188" y="218" width="12" height="66" rx="3"/>')
    : p('door_l1', '<rect x="60" y="146" width="12" height="112" rx="3"/>') + p('door_r1', '<rect x="188" y="146" width="12" height="112" rx="3"/>');
  return `<svg viewBox="0 0 260 420" class="li-svg se-veh-svg" aria-hidden="true">
  <path class="li-body" d="M86 38 Q130 16 174 38 L194 120 L194 350 Q194 398 130 404 Q66 398 66 350 L66 120 Z"/>
  <path class="li-stroke" d="M78 132 Q130 116 182 132 M80 296 Q130 310 180 296"/>
  <rect class="li-stroke" x="84" y="144" width="92" height="142" rx="12"/>
  ${p('hood', '<path d="M88 44 Q130 26 172 44 L186 118 L74 118 Z"/>')}
  ${p('trunk', '<path d="M76 316 L184 316 L186 350 Q184 388 130 394 Q76 388 74 350 Z"/>')}
  ${p('radiator', '<rect x="106" y="22" width="48" height="8" rx="3"/>')}
  ${p('light1', '<ellipse cx="98" cy="42" rx="13" ry="6"/>')}
  ${p('light2', '<ellipse cx="162" cy="42" rx="13" ry="6"/>')}
  ${p('battery', '<rect x="84" y="62" width="24" height="16" rx="2"/>')}
  ${p('plug', '<rect x="152" y="62" width="10" height="18" rx="2"/>')}
  ${p('belt', '<circle cx="130" cy="92" r="10"/>')}
  ${doors}
  ${p('wheel_l1', '<rect x="42" y="76" width="22" height="56" rx="7"/>')}
  ${p('wheel_r1', '<rect x="196" y="76" width="22" height="56" rx="7"/>')}
  ${p('wheel_l2', '<rect x="42" y="284" width="22" height="56" rx="7"/>')}
  ${p('wheel_r2', '<rect x="196" y="284" width="22" height="56" rx="7"/>')}
  ${p('spare1', '<circle cx="130" cy="354" r="18"/>')}
</svg>`;
}

// sePlace sorts tiles onto the sides of the drawing. A vehicle's tiles stay
// where the part is (left wheels on the left); an item's are balanced.
function sePlace(slots, veh) {
  if (!veh) return loPlace(slots);
  const pos = { top: [], left: [], right: [], bottom: [] };
  for (const s of slots) pos[s.pos].push(s);
  for (const k of Object.keys(pos)) pos[k].sort((a, b) => a.order - b.order);
  return pos;
}

// ------------------------------------------------------------------- doll

// seDoll draws det with a tile per slot for the groups in list.
// opts: info (catalog entry of the item), nameOf(class), open(group),
// add(slot), remove(group). refresh() redraws after an edit.
function seDoll(det, list, opts) {
  const veh = det.k === 'vehicles';
  const slots = seSlots(det);
  const kind = veh ? '' : loItemKind(opts.info || { c: det.c, k: det.k, s: [] });
  const el = h('div', { class: 'se-doll-wrap' });
  function tile(s, g) {
    const items = g ? (g.items || []).filter(it => String(it.name || '').trim()) : [];
    const pct = g ? chanceOf(g.chance) : 0;
    const fits = s.refs.length;
    const name = items.length ? opts.nameOf(items[0].name) : '';
    const main = h('button', { type: 'button', class: 'li-tile-main', disabled: !g && !fits,
      title: g ? items.map(it => opts.nameOf(it.name)).join(' / ') : fits ? tn('li.fits', { n: fits }) : t('li.nothingFits'),
      onclick: () => (g ? opts.open(g) : opts.add(s)) }, [
      h('span', { class: 'li-tile-label', text: s.label }),
      h('span', { class: 'li-tile-item' + (g ? '' : ' empty'),
        text: g ? name + (items.length > 1 ? ' +' + (items.length - 1) : '') : fits ? '+ ' + t('se.add') : t('li.nothingFits') }),
      !g && veh && fits ? h('span', { class: 'li-tile-sub', text: t('se.never') }) : null,
    ]);
    const parts = [main];
    if (g) {
      parts.push(h('span', { class: 'se-pct' + (pct >= 0.995 ? ' full' : pct < 0.5 ? ' low' : ''), text: fmtPct(pct), title: t('attach.slotChance') }));
      parts.push(h('button', { type: 'button', class: 'icon-x li-tile-x', text: '×', title: t('attach.removeSlot'),
        'aria-label': t('attach.removeSlot') + ': ' + s.label, onclick: () => opts.remove(g) }));
    }
    return h('div', { class: 'li-tile se-tile' + (g ? ' on' : '') + (fits || g ? '' : ' none'), 'data-slot': s.slot }, parts);
  }
  function refresh() {
    const { bySlot } = seAssign(list, slots);
    const on = new Set();
    for (const s of bySlot.keys()) if (s.part) on.add(s.part);
    // A truck's load (logs, planks, barrels…) and slots a mod adds rarely
    // spawn filled: empty ones are buttons under the drawing, not tiles.
    const spare = veh ? slots.filter(s => s.part === 'cargo' && !bySlot.has(s)) : [];
    const pos = sePlace(slots.filter(s => !spare.includes(s)), veh);
    const col = (side) => h('div', { class: 'li-side li-' + side }, pos[side].map(s => tile(s, bySlot.get(s))));
    const more = spare.filter(s => s.refs.length);
    el.replaceChildren(...[
      h('div', { class: 'li-doll se-doll' + (veh ? ' se-veh' : ' li-k-' + kind) }, [
        col('top'), col('left'),
        h('div', { class: 'li-art', html: veh ? seVehArt(det, on) : loItemArt(kind, on) }, veh ? null : loPips(slots, new Set(bySlot.keys()))),
        col('right'), col('bottom'),
      ]),
      more.length ? h('div', { class: 'se-more' }, [
        h('span', { class: 'hint', text: t('se.more') }),
        ...more.map(s => h('button', { type: 'button', class: 'pill', text: '+ ' + s.label, title: tn('li.fits', { n: s.refs.length }), onclick: () => opts.add(s) })),
      ]) : null,
      h('p', { class: 'hint li-count', text: tn('li.filled', { n: bySlot.size, total: slots.length }) }),
    ].filter(Boolean));
  }
  refresh();
  return { el, slots, refresh };
}

// ------------------------------------------------------------ group list

// seGroupCard edits one group — an <attachments> or <cargo> slot of
// cfgspawnabletypes.xml, or a preset of cfgrandompresets.xml: the chance the
// group spawns with and the parts DayZ picks ONE of, by weight. Laid out like
// the loadout's rows — each part's icon and name, its class, its weight and
// the real chance that gives — and parts come from the catalog (the slot's
// own list when the drawing knows the slot); a click on a part swaps it.
// ctx: kind ('attachments'|'cargo'), slot (the drawing's slot, or null),
// title (head text without a slot), removable (false: no × for the group),
// chanceKey (its chance label), cat (catalog), classLower (the server's
// types, to flag typos), fitsAll (what fits the item at all), disabled,
// edited() after a value edit, changed() after a part or the group is added
// or removed, remove() for the group's ×.
function seGroupCard(g, ctx) {
  const cat = ctx.cat || { byClass: new Map() };
  const infoOf = (cls) => cat.byClass.get(String(cls || '').trim().toLowerCase()) || null;
  const nameOf = (cls) => { const it = infoOf(cls); return it ? it.n : loHumanize(cls); };
  const off = !!ctx.disabled;
  const chanceKey = ctx.chanceKey || 'attach.slotChance';
  const el = h('div', { class: 'attach-group se-group' + (typeof g.preset === 'string' ? ' attach-preset' : '') });
  const chanceInp = h('input', { type: 'text', inputmode: 'decimal', value: g.chance ?? '1.00', class: 'attach-num', disabled: off,
    title: t(chanceKey), 'aria-label': t(chanceKey) });
  const slotBadge = h('span', { class: 'attach-pct slot' });
  const removeBtn = ctx.removable === false ? null : h('button', { type: 'button', class: 'icon-x', text: '×', disabled: off,
    title: t('attach.removeSlot'), 'aria-label': t('attach.removeSlot'), onclick: () => ctx.remove && ctx.remove() });
  const headText = ctx.slot ? ctx.slot.label : ctx.title || '';
  if (typeof g.preset === 'string') {
    // A reference to a preset of cfgrandompresets.xml.
    const nameInp = h('input', { type: 'text', value: g.preset || '', list: ctx.kind === 'cargo' ? 'dz-cargopresets' : 'dz-attachpresets',
      placeholder: t('attach.preset.ph'), 'aria-label': t('attach.preset'), disabled: off });
    nameInp.oninput = () => { g.preset = nameInp.value.trim(); ctx.edited(); };
    chanceInp.oninput = () => { g.chance = chanceInp.value.trim(); slotBadge.textContent = fmtPct(chanceOf(g.chance)); ctx.edited(); };
    slotBadge.textContent = fmtPct(chanceOf(g.chance));
    el.append(
      h('div', { class: 'attach-head' }, [
        h('span', { class: 'attach-gear', text: '⚙' }), h('span', { class: 'attach-slot-name', i18n: 'attach.preset' }),
        h('span', { class: 'grow' }), removeBtn,
      ].filter(Boolean)),
      h('div', { class: 'attach-body' }, [
        h('div', { class: 'attach-preset-row' }, [nameInp, h('span', { class: 'attach-head-label', i18n: chanceKey }), chanceInp,
          h('span', { class: 'attach-eq', text: '=' }), slotBadge]),
        h('p', { class: 'hint', i18n: 'attach.preset.hint' }),
      ]));
    return el;
  }
  const items = g.items || (g.items = []);
  const badges = [];
  const recalc = () => {
    const slotPct = chanceOf(g.chance);
    const total = items.reduce((a, it) => a + chanceOf(it.chance), 0);
    slotBadge.textContent = fmtPct(slotPct);
    for (const b of badges) {
      const real = total > 0 ? slotPct * (chanceOf(b.it.chance) / total) : 0;
      b.el.textContent = fmtPct(real);
      b.el.className = 'attach-pct' + (real > 0 ? '' : ' zero');
    }
  };
  chanceInp.oninput = () => { g.chance = chanceInp.value.trim(); recalc(); ctx.edited(); };
  const refs = ctx.slot && ctx.slot.refs && ctx.slot.refs.length ? ctx.slot.refs : null;
  const pick = (single, onAdd) => loPick({ title: headText || t(ctx.kind === 'cargo' ? 'attach.section.cargo' : 'attach.section.attachments'),
    only: refs || undefined, single, onAdd });
  const rows = h('div', { class: 'se-rows' });
  items.forEach((it, ii) => {
    const cls = String(it.name || '').trim();
    const info = infoOf(cls);
    const low = cls.toLowerCase();
    // A class missing from types.xml never spawns; a part that fits no slot
    // of the item cannot go on it.
    const unknown = !!cls && !!ctx.classLower && ctx.classLower.size > 0 && !ctx.classLower.has(low);
    const nofit = !!cls && !unknown && !!ctx.fitsAll && !!info && !ctx.fitsAll.has(low);
    const wIn = h('input', { type: 'text', inputmode: 'decimal', value: it.chance ?? '1.00', class: 'attach-num', disabled: off,
      title: t('attach.weight'), 'aria-label': t('attach.weight') + ': ' + (cls ? nameOf(cls) : '') });
    const pctBadge = h('span', { class: 'attach-pct', title: t('attach.realChance') });
    wIn.oninput = () => { it.chance = wIn.value.trim(); recalc(); ctx.edited(); };
    badges.push({ it, el: pctBadge });
    rows.append(h('div', { class: 'se-row' + (unknown || nofit ? ' warn' : '') }, [
      h('span', { class: 'lo-pick-ico' }, loIcon(info ? loIconFor(info) : 'i-c-other', 16)),
      h('button', { type: 'button', class: 'se-row-name', disabled: off, title: t('se.swap'),
        onclick: () => pick(true, (x) => { it.name = x.c; ctx.changed(); }) }, [
        h('b', { text: cls ? nameOf(cls) : t('se.noClass') }),
        h('code', { text: cls || '—' }),
        unknown ? h('span', { class: 'se-row-warn', text: '⚠ ' + t('attach.unknownClass') })
          : nofit ? h('span', { class: 'se-row-warn', text: '⚠ ' + t('se.noFit') }) : null,
      ]),
      // A weapon (or anything with slots or room inside) carries its own
      // attachments: DayZ puts them on it wherever it spawns — open them.
      info && info.i ? seOwnButton(cls, info, ctx) : h('span'),
      h('span', { class: 'se-row-w' }, [h('span', { class: 'se-row-k', i18n: 'attach.col.weight' }), wIn]),
      pctBadge,
      h('button', { type: 'button', class: 'icon-x', text: '×', disabled: off, title: t('attach.removeItem'),
        'aria-label': t('attach.removeItem') + (cls ? ': ' + nameOf(cls) : ''), onclick: () => { items.splice(ii, 1); ctx.changed(); } }),
    ]));
  });
  if (!items.length) rows.append(h('p', { class: 'hint se-empty', i18n: 'se.empty' }));
  recalc();
  el.append(
    h('div', { class: 'attach-head' }, [
      headText ? h('span', { class: 'attach-slot-name', text: headText }) : null,
      h('span', { class: 'attach-head-label', i18n: chanceKey }), help('attach.slotChance'),
      chanceInp, h('span', { class: 'attach-eq', text: '=' }), slotBadge,
      h('span', { class: 'grow' }), removeBtn,
    ].filter(Boolean)),
    h('div', { class: 'attach-body' }, [
      rows,
      items.length > 1 ? h('p', { class: 'hint se-one', i18n: 'se.one' }) : null,
      h('button', { type: 'button', class: 'lo-add-inside', disabled: off, text: '+ ' + t('lo.carry.addItem'),
        onclick: () => pick(false, (x) => { items.push({ name: x.c, chance: '1.00' }); ctx.changed(); }) }),
    ].filter(Boolean)),
  );
  return el;
}

// What each class's own entry holds, per page load: the count on its button.
const _seOwn = new Map(); // lower-cased class → Promise<{entry, file}>
function seOwnEntry(cls, fresh) {
  const k = String(cls).toLowerCase();
  if (fresh || !_seOwn.has(k)) _seOwn.set(k, api.get('/api/spawnable/entry?name=' + encodeURIComponent(cls)).catch(() => null));
  return _seOwn.get(k);
}

// seOwnButton is a row's way into its item's own entry: "Attachments · 3",
// like the loadout's button on a carried weapon.
function seOwnButton(cls, info, ctx) {
  const weapon = info.k === 'weapons';
  const label = h('span', { text: t(weapon ? 'li.btn.weapon' : 'li.btn') });
  const btn = h('button', { type: 'button', class: 'lo-inside-btn se-own', title: t('se.own.title'),
    onclick: () => seEntryDialog(cls, { classLower: ctx.classLower, onSaved: () => paint(true) }) },
  [loIcon(weapon ? 'i-c-scope' : 'i-c-box', 14), label]);
  const paint = (fresh) => seOwnEntry(cls, fresh).then((r) => {
    const e = r && r.entry;
    const n = e ? (e.attachments || []).length + (e.cargo || []).length : 0;
    label.textContent = t(weapon ? 'li.btn.weapon' : 'li.btn') + (n ? ' · ' + n : '');
    btn.classList.toggle('on', n > 0);
  });
  paint(false);
  return btn;
}

// seEntryDialog opens an item's own cfgspawnabletypes entry — what DayZ puts
// on and into this item wherever the economy spawns it (loot, a car's trunk,
// an infected) — drawn like the Attachments editor, and saves it into the
// file that defines it (/api/spawnable/entry). onSaved() after a save.
async function seEntryDialog(cls, opts = {}) {
  let r, cat, det;
  try {
    [r, cat, det] = await Promise.all([api.get('/api/spawnable/entry?name=' + encodeURIComponent(cls)), loCatalog(), loDetail(cls)]);
  } catch (e) { handleErr(e); return; }
  const norm = (list) => {
    const out = JSON.parse(JSON.stringify(list || []));
    for (const g of out) if (typeof g.preset !== 'string') g.items = g.items || [];
    return out;
  };
  const entry = r.entry || {};
  const model = { attachments: norm(entry.attachments), cargo: norm(entry.cargo) };
  const snap = () => JSON.stringify(model);
  const saved = snap();
  let done = false;
  const info = loItemInfo(cat, cls);
  const m = openModal({ title: info.n || cls, wide: true, isDirty: () => !done && snap() !== saved });
  const edited = () => {};
  const se = seEditor(model.attachments, { cls, det, cat, info, classLower: opts.classLower, edited });
  const roomy = (det && det.cargo) || model.cargo.length;
  const cargo = roomy ? seGroupList(model.cargo, { kind: 'cargo', cat, classLower: opts.classLower, edited, changed: edited,
    addKey: 'se.addCargo', titleKey: 'se.cargoGroup', emptyKey: 'attach.noCargo' }) : null;
  const save = (e) => withBusy(e.currentTarget, async () => {
    try {
      await api.post('/api/spawnable/entry', { name: cls, attachments: model.attachments, cargo: model.cargo });
      done = true;
      toast(t('msg.saved'), 'ok');
      if (opts.onSaved) opts.onSaved();
      m.close();
    } catch (err) { handleErr(err); }
  });
  m.body.append(...[
    h('p', { class: 'hint se-own-hint', text: t('se.own.hint') }),
    h('div', { class: 'attach-section' }, [h('h3', { i18n: 'attach.section.attachments' }), se.el]),
    cargo ? h('div', { class: 'attach-section' }, [h('h3', { i18n: 'attach.section.cargo' }), cargo.el]) : null,
    h('p', { class: 'hint mono', text: r.file }),
    h('div', { class: 'actions' }, [h('span', { class: 'grow' }),
      h('button', { type: 'button', class: 'secondary', i18n: 'action.cancel', onclick: () => m.close() }),
      h('button', { type: 'button', class: 'primary', i18n: 'action.save', onclick: save })]),
  ].filter(Boolean));
  applyI18n();
}

// seGroupList is a list of groups edited in place — an item's cargo, a
// vehicle's trunk — like the loadout's "what they carry": a card per group,
// "+ group" (picked from the catalog) and "+ preset" under them. opts:
// seGroupCard's ctx plus addKey (the add button), emptyKey, titleKey
// ("{n}" = the group's number) and changed().
function seGroupList(list, opts) {
  const el = h('div', { class: 'se-list' });
  function changed() { refresh(); if (opts.changed) opts.changed(); }
  // A new group starts with what is picked for it, the first part at once.
  function addGroup() {
    let g = null;
    loPick({ title: t(opts.addKey || 'se.addGroup'), onAdd: (x) => {
      if (!g) { g = { chance: opts.kind === 'cargo' ? '0.50' : '1.00', items: [] }; list.push(g); }
      g.items.push({ name: x.c, chance: '1.00' });
      changed();
    } });
  }
  function refresh() {
    el.replaceChildren(...list.map((g, gi) => seGroupCard(g, Object.assign({}, opts, {
      title: t(opts.titleKey || 'se.group').replace('{n}', gi + 1),
      remove: () => { list.splice(list.indexOf(g), 1); changed(); },
      changed,
    }))));
    if (!list.length && opts.emptyKey) el.append(h('p', { class: 'hint', i18n: opts.emptyKey }));
    el.append(h('div', { class: 'attach-add-row' }, [
      h('button', { type: 'button', class: 'lo-add se-add-group', disabled: !!opts.disabled, text: '+ ' + t(opts.addKey || 'se.addGroup'), onclick: addGroup }),
      h('button', { type: 'button', class: 'attach-add', disabled: !!opts.disabled, i18n: 'attach.addPreset',
        onclick: () => { list.push({ chance: '1.00', preset: '' }); changed(); } }),
    ]));
    applyI18n();
  }
  refresh();
  return { el, refresh };
}

// ------------------------------------------------------------- the editor

// seSetChance gives every part group of list the slot chance v. A lone part
// is certain once its slot rolls, so the slot chance is then the part's
// chance whichever way DayZ reads the weight; a group of several keeps its
// weights (which of them appears). Preset references carry their own.
function seSetChance(list, v) {
  for (const g of list) {
    if (typeof g.preset === 'string') continue;
    g.chance = v.toFixed(2);
    if ((g.items || []).length === 1) g.items[0].chance = '1.00';
  }
}

// seChanceOf is the slot chance all part groups of list share, or null.
function seChanceOf(list) {
  let c = null;
  for (const g of list) {
    if (typeof g.preset === 'string') continue;
    const v = chanceOf(g.chance);
    if (c === null) c = v;
    else if (Math.abs(c - v) > 0.001) return null;
  }
  return c;
}

// seComplete: every slot certain to spawn, with its part (the Vehicles
// page's "complete" mark; the server's partsComplete).
function seComplete(list) {
  return list.length > 0 && list.every(g => typeof g.preset !== 'string' && chanceOf(g.chance) >= 0.999 &&
    ((g.items || []).length !== 1 || chanceOf(g.items[0].chance) >= 0.999));
}

// seEditor is the whole attachments side of an entry, laid out like the
// loadout: the item or vehicle drawn with a tile per slot — a click opens
// that slot in a dialog, an empty one adds a part — the chance buttons, and
// the groups the drawing cannot place (a preset reference, parts the game
// files do not describe; every group when there is no drawing). opts: cls
// (the entry's class), det (its detail, or null), info (catalog entry), cat
// (catalog), classLower, edited(). bulk(v), bulkButtons ([v, label]…),
// bulkLabel (i18n key) and bulkOn(v) replace the chance buttons — the
// Vehicles page sets every colour at once.
function seEditor(list, opts) {
  const el = h('div', { class: 'se-editor' });
  const cat = opts.cat || { byClass: new Map(), items: [] };
  const nameOf = (cls) => {
    const it = cat.byClass.get(String(cls || '').trim().toLowerCase());
    return it ? it.n : loHumanize(cls);
  };
  const det = opts.det && !opts.det.unknown && ((opts.det.attach || []).length || (opts.det.mags || []).length) ? opts.det : null;
  let doll = null, slots = [], fitsAll = null, assigned = { slotOf: new Map(), rest: list };
  if (det) {
    slots = seSlots(det);
    fitsAll = new Set();
    for (const s of slots) for (const c of s.set) fitsAll.add(c);
  }
  const base = { kind: 'attachments', cat, classLower: opts.classLower, fitsAll };
  const restHost = h('div', { class: 'se-rest' });
  const edited = () => { if (doll) doll.refresh(); opts.edited(); };
  function redraw(edit) {
    if (det) assigned = seAssign(list, slots);
    if (doll) doll.refresh();
    drawRest();
    if (edit) opts.edited();
  }
  const removeGroup = (g) => { const i = list.indexOf(g); if (i >= 0) list.splice(i, 1); redraw(true); };
  function drawRest() {
    const rest = det ? assigned.rest : list;
    restHost.replaceChildren(...[
      det && rest.length ? h('div', { class: 'se-rest-head' }, [h('b', { i18n: 'se.rest' }), h('span', { class: 'hint', i18n: 'se.rest.hint' })]) : null,
      ...rest.map(g => seGroupCard(g, Object.assign({}, base, {
        title: t('se.group').replace('{n}', list.indexOf(g) + 1),
        remove: () => removeGroup(g), changed: () => redraw(true), edited,
      }))),
      !list.length ? h('p', { class: 'hint', i18n: 'attach.noSlots' }) : null,
    ].filter(Boolean));
    applyI18n();
  }
  // A tile opens its group in a dialog, like a slot of the loadout.
  function openGroup(g) {
    const s = assigned.slotOf.get(g) || null;
    const m = openModal({ title: (s ? s.label + ' — ' : '') + nameOf(opts.cls), wide: true, onClose: () => redraw(false) });
    const body = h('div', { class: 'se-dialog' });
    const draw = () => {
      if (!list.includes(g)) { m.close(); return; }
      body.replaceChildren(seGroupCard(g, Object.assign({}, base, {
        slot: assigned.slotOf.get(g) || s,
        remove: () => { removeGroup(g); m.close(); },
        changed: () => { redraw(true); draw(); }, edited,
      })));
      applyI18n();
    };
    draw();
    m.body.append(body, h('div', { class: 'actions' }, [h('span', { class: 'grow' }),
      h('button', { type: 'button', class: 'primary', text: t('pick.done'), onclick: () => m.close() })]));
  }
  // A new group from the picker: the parts picked go into it, the first one
  // at once — DayZ then picks one of them by weight.
  function addFrom(title, only) {
    let g = null;
    loPick({ title, only, onAdd: (r) => {
      if (!g) { g = { chance: '1.00', items: [] }; list.push(g); }
      g.items.push({ name: r.c, chance: '1.00' });
      redraw(true);
    } });
  }
  if (det) {
    doll = seDoll(det, list, {
      info: opts.info, nameOf,
      open: openGroup,
      remove: removeGroup,
      add: (s) => {
        if (s.refs.length === 1) { list.push({ chance: '1.00', items: [{ name: s.refs[0].c, chance: '1.00' }] }); redraw(true); return; }
        addFrom(s.label + ' — ' + nameOf(opts.cls), s.refs);
      },
    });
    assigned = seAssign(list, slots);
    // All parts at once: the chance every slot spawns with (the caller's own
    // bulk when it sets several entries), and the empty slots filled with
    // the part that suits this class.
    const bulk = opts.bulk || ((v) => { seSetChance(list, v); redraw(true); });
    const fill = () => {
      const { bySlot } = seAssign(list, slots);
      let n = 0;
      for (const s of slots) {
        if (bySlot.has(s) || !s.refs.length || s.mag) continue;
        const r = seBestRef(s, opts.cls);
        if (!r) continue;
        list.push({ chance: '1.00', items: [{ name: r.c, chance: '1.00' }] });
        n++;
      }
      redraw(n > 0);
      toast(n ? tn('se.filledN', { n }) : t('se.filledNone'), 'ok');
    };
    const buttons = opts.bulkButtons || [[1, '100%'], [0.75, '75%'], [0.5, '50%'], [0.25, '25%']];
    el.append(doll.el, h('div', { class: 'se-bulk' }, [
      h('span', { class: 'hint', i18n: opts.bulkLabel || 'se.bulk' }),
      ...buttons.map(([v, l]) => h('button', { type: 'button', class: 'pill' + (opts.bulkOn && opts.bulkOn(v) ? ' active' : ''), text: l, onclick: () => bulk(v) })),
      h('span', { class: 'grow' }),
      h('button', { type: 'button', class: 'secondary small', title: t('se.fill.hint'), onclick: fill }, [h('span', { html: svgIcon('i-plus', 13) }), h('span', { text: ' ' + t('se.fill') })]),
    ]));
    if (slots.length) el.append(h('p', { class: 'hint se-howto', i18n: det.k === 'vehicles' ? 'se.howto.veh' : 'se.howto' }));
  }
  drawRest();
  // Parts the drawing has no tile for (or an item it cannot draw): a group
  // from the catalog — what fits the item when that is known — or a preset.
  const allRefs = det ? [...new Map(slots.flatMap(s => s.refs).map(r => [String(r.c).toLowerCase(), r])).values()] : null;
  el.append(restHost, h('div', { class: 'attach-add-row' }, [
    h('button', { type: 'button', class: 'attach-add', i18n: 'attach.addSlot',
      onclick: () => addFrom(t('attach.addSlot') + (opts.cls ? ' — ' + nameOf(opts.cls) : ''), allRefs && allRefs.length ? allRefs : undefined) }),
    h('button', { type: 'button', class: 'attach-add', i18n: 'attach.addPreset', onclick: () => { list.push({ chance: '1.00', preset: '' }); redraw(true); } }),
  ]));
  applyI18n();
  return { el, refresh: () => redraw(false) };
}

// seLists loads what the class and preset fields autocomplete against —
// the server's types (vanilla and mods) and cfgrandompresets.xml names — into
// datalists on root, once per page. Resolves to the lower-cased class set the
// editor flags typos with (empty when the names could not be read).
async function seLists(root) {
  if (root._seLists) return root._seLists;
  root._seLists = (async () => {
    let names = [];
    const presets = { cargo: [], attachments: [] };
    try { names = (await api.get('/api/spawnable/classnames')).names || []; } catch {}
    try {
      for (const p of (await api.get('/api/randompresets')).presets || []) (p.kind === 'cargo' ? presets.cargo : presets.attachments).push(p.name);
    } catch {}
    const list = (id, values) => h('datalist', { id }, values.map(v => h('option', { value: v })));
    if (!document.getElementById('dz-classnames')) root.append(list('dz-classnames', names.slice(0, 5000)));
    if (!document.getElementById('dz-cargopresets')) root.append(list('dz-cargopresets', presets.cargo));
    if (!document.getElementById('dz-attachpresets')) root.append(list('dz-attachpresets', presets.attachments));
    return new Set(names.map(n => n.toLowerCase()));
  })();
  return root._seLists;
}
