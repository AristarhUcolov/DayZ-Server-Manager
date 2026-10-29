// Copyright (c) 2026 Aristarh Ucolov.
//
// Player loadout: what goes on and into one item. The game's own config says
// which attachment slots an item has (a rifle's optics, muzzle, buttstock…,
// a plate carrier's holster and pouches) and which parts fit each slot; a
// weapon lists its magazines, or the rounds an internal magazine takes. The
// editor draws the item with what is attached, a tile per slot around it,
// and lists what else goes inside. Backend: GET /api/items/detail
// (internal/items/detail.go).
//
// DayZ spawns a kit's children with CreateInInventory (an attachment slot
// first, then cargo); a magazine or rounds given to a weapon are loaded into
// it (Weapon_Base.SpawnAmmo).

const _loDetail = new Map(); // lang|class → Promise<detail>

function loDetail(cls) {
  const lang = State.lang || 'en';
  const key = lang + '|' + String(cls || '').toLowerCase();
  if (!_loDetail.has(key)) {
    _loDetail.set(key, api.get('/api/items/detail?class=' + encodeURIComponent(cls) + '&lang=' + encodeURIComponent(lang))
      .catch(() => ({ c: cls, attach: [], unknown: true })));
  }
  return _loDetail.get(key);
}

// What a slot is, in words, which side of the drawing its tile goes, and
// which part of the drawing it lights up.
const LO_ATT = [
  [/optic/i, 'att.optics', 'top', 'optic'],
  [/flashlight|light/i, 'att.flashlight', 'top', 'light'],
  [/nvg|nightvision/i, 'att.nvg', 'top', 'nvg'],
  [/visor|^glass$/i, 'att.visor', 'top', 'visor'],
  [/mouth/i, 'att.mouth', 'bottom', 'mask'],
  [/mask|glasses|eyewear/i, 'att.mask', 'top', 'mask'],
  [/suppressor/i, 'att.suppressor', 'right', 'muzzle'],
  [/muzzle|barrel/i, 'att.muzzle', 'right', 'muzzle'],
  [/bayonet/i, 'att.bayonet', 'right', 'bayonet'],
  [/buttstock|stock/i, 'att.buttstock', 'left', 'stock'],
  [/wrap/i, 'att.wrap', 'left', 'wrap'],
  [/handguard/i, 'att.handguard', 'bottom', 'hand'],
  [/holster/i, 'att.holster', 'left', 'holster'],
  [/grenade/i, 'att.grenade', 'right', 'grenade'],
  [/vestbackpack|backpack/i, 'att.backpouch', 'top', 'backpouch'],
  [/pouch/i, 'att.pouch', 'right', 'pouch'],
  [/chemlight/i, 'att.chemlight', 'right', 'chem'],
  [/walkie|radio/i, 'att.radio', 'left', 'radio'],
  [/battery/i, 'att.battery', 'bottom', 'battery'],
  [/knife|sheath/i, 'att.knife', 'left', 'knife'],
  [/belt/i, 'att.belt', 'bottom', 'belt'],
];
function loAttInfo(slot) {
  for (const [re, key, pos, part] of LO_ATT) if (re.test(slot)) return { key, pos, part };
  return { key: '', pos: 'bottom', part: '' };
}
function loAttLabel(slot) {
  const a = loAttInfo(slot);
  if (!a.key) return loHumanize(slot);
  // A vest's grenade loops are A, B, C, D.
  const m = /grenade([a-d])$/i.exec(slot);
  return t(a.key) + (m ? ' ' + m[1].toUpperCase() : '');
}

// The drawing an item gets.
function loItemKind(info) {
  const s = info.s || [];
  if (info.k === 'attachments' && /optic|scope|sight/i.test(info.c)) return 'scope';
  if (/flashlight|universallight|headtorch|torch/i.test(info.c)) return 'torch';
  if (info.k === 'weapons' && s.includes('Shoulder')) return 'rifle';
  if (info.k === 'weapons' && !s.includes('Melee')) return 'pistol';
  if (s.includes('Vest')) return 'vest';
  if (s.includes('Back')) return 'pack';
  if (s.includes('Headgear')) return 'helmet';
  return 'box';
}

// loItemArt draws the item; the parts named in on (a Set) are shown
// attached, the rest as faint outlines. Original line art.
function loItemArt(kind, on) {
  const p = (id, svg) => `<g class="li-part li-${id}${on.has(id) ? ' on' : ''}">${svg}</g>`;
  if (kind === 'rifle') {
    return `<svg viewBox="0 0 320 124" class="li-svg" aria-hidden="true">
  ${p('stock', '<path d="M8 50 L72 46 L76 70 L60 88 L14 88 Q4 70 8 50 Z"/>')}
  <path class="li-body" d="M72 44 L188 40 L192 66 L152 70 L128 70 L120 66 L76 70 Z"/>
  <path class="li-body" d="M118 66 L128 70 L120 96 L106 93 Z"/>
  ${p('hand', '<path d="M188 42 L250 42 L250 62 L192 64 Z"/>')}
  <rect class="li-body" x="250" y="48" width="44" height="7" rx="2"/>
  ${p('muzzle', '<rect x="292" y="45" width="24" height="13" rx="3"/>')}
  ${p('bayonet', '<path d="M244 64 L304 72 L244 72 Z"/>')}
  ${p('optic', '<rect x="128" y="18" width="58" height="15" rx="6"/><path d="M146 33 v8 M168 33 v8" class="li-stroke"/>')}
  ${p('light', '<rect x="214" y="28" width="24" height="11" rx="3"/>')}
  ${p('mag', '<path d="M152 70 L172 70 L182 112 L160 115 Z"/>')}
  ${p('wrap', '<path d="M198 44 l8 20 M212 43 l8 20 M226 43 l8 20" class="li-stroke"/>')}
</svg>`;
  }
  if (kind === 'pistol') {
    return `<svg viewBox="0 0 240 150" class="li-svg" aria-hidden="true">
  <path class="li-body" d="M40 42 L178 42 L178 66 L40 66 Z"/>
  <path class="li-body" d="M48 66 L94 66 L86 128 L56 128 Z"/>
  <path class="li-stroke" d="M94 66 Q106 88 122 68"/>
  ${p('muzzle', '<rect x="178" y="46" width="48" height="16" rx="4"/>')}
  ${p('optic', '<rect x="100" y="24" width="42" height="13" rx="5"/>')}
  ${p('light', '<rect x="128" y="70" width="38" height="13" rx="3"/>')}
  ${p('mag', '<path d="M58 124 L84 124 L82 142 L60 142 Z"/>')}
</svg>`;
  }
  if (kind === 'vest') {
    return `<svg viewBox="0 0 220 200" class="li-svg" aria-hidden="true">
  <path class="li-body" d="M50 22 L80 14 L110 32 L140 14 L170 22 L178 182 L42 182 Z"/>
  <path class="li-stroke" d="M58 72 H162 M58 112 H162"/>
  ${p('holster', '<path d="M20 120 L46 116 L50 170 L28 172 Z"/>')}
  ${p('pouch', '<rect x="72" y="126" width="76" height="40" rx="6"/>')}
  ${p('grenade', '<circle cx="190" cy="98" r="12"/><circle cx="190" cy="130" r="12"/>')}
  ${p('backpouch', '<rect x="80" y="2" width="60" height="16" rx="5"/>')}
  ${p('radio', '<rect x="56" y="40" width="16" height="28" rx="3"/>')}
  ${p('knife', '<path d="M154 40 L164 40 L160 92 Z"/>')}
</svg>`;
  }
  if (kind === 'pack') {
    return `<svg viewBox="0 0 200 200" class="li-svg" aria-hidden="true">
  <path class="li-body" d="M50 42 Q50 18 100 18 Q150 18 150 42 L156 182 L44 182 Z"/>
  <rect class="li-stroke" x="66" y="112" width="68" height="50" rx="8"/>
  ${p('chem', '<rect x="158" y="70" width="10" height="34" rx="4"/>')}
  ${p('radio', '<rect x="22" y="70" width="18" height="40" rx="3"/>')}
  ${p('knife', '<path d="M28 124 L38 124 L34 180 Z"/>')}
</svg>`;
  }
  if (kind === 'helmet') {
    return `<svg viewBox="0 0 200 160" class="li-svg" aria-hidden="true">
  <path class="li-body" d="M30 112 Q30 30 100 30 Q170 30 170 112 Z"/>
  <path class="li-stroke" d="M24 112 H176"/>
  ${p('nvg', '<rect x="80" y="10" width="40" height="22" rx="5"/>')}
  ${p('visor', '<path d="M42 78 Q100 58 158 78 L154 104 Q100 88 46 104 Z"/>')}
  ${p('light', '<rect x="162" y="64" width="26" height="12" rx="3"/>')}
  ${p('mask', '<rect x="62" y="116" width="76" height="22" rx="8"/>')}
</svg>`;
  }
  if (kind === 'scope') {
    return `<svg viewBox="0 0 240 110" class="li-svg" aria-hidden="true">
  <path class="li-body" d="M16 34 H58 L70 42 H170 L182 34 H224 V70 H182 L170 62 H70 L58 70 H16 Z"/>
  <rect class="li-body" x="96" y="62" width="48" height="16" rx="3"/>
  <path class="li-stroke" d="M40 40 V64 M200 40 V64"/>
  ${p('battery', '<rect x="150" y="16" width="24" height="18" rx="3"/>')}
</svg>`;
  }
  if (kind === 'torch') {
    return `<svg viewBox="0 0 240 110" class="li-svg" aria-hidden="true">
  <path class="li-body" d="M24 40 H150 L186 26 V84 L150 70 H24 Z"/>
  <path class="li-stroke" d="M186 34 V76"/>
  ${p('battery', '<rect x="44" y="46" width="70" height="18" rx="4"/>')}
</svg>`;
  }
  return `<svg viewBox="0 0 200 140" class="li-svg" aria-hidden="true">
  <rect class="li-body" x="40" y="24" width="120" height="92" rx="12"/>
  <path class="li-stroke" d="M40 52 H160"/>
</svg>`;
}

// loPips: a dot under the drawing for every slot it has no part for (a
// mod's, a holster's pistol, a hat's hook), lit when filled — so a filled
// slot always shows on the item.
function loPips(slots, filled) {
  const extra = slots.filter(s => !s.part);
  if (!extra.length) return null;
  return h('div', { class: 'li-pips', 'aria-hidden': 'true' },
    extra.map(s => h('span', { class: 'li-pip' + (filled.has(s) ? ' on' : ''), title: s.label })));
}

// loPlace sorts slots onto the four sides of the drawing and keeps the left
// and right columns even (a vest's four grenade loops are not all on one side).
function loPlace(slots) {
  const pos = { top: [], left: [], right: [], bottom: [] };
  for (const s of slots) pos[s.pos].push(s);
  const move = (from, to) => {
    const k = Math.floor((pos[from].length - pos[to].length) / 2);
    if (k > 0) pos[to].push(...pos[from].splice(pos[from].length - k, k));
  };
  move('right', 'left');
  move('left', 'right');
  return pos;
}

// loLoaded: whether a kit's weapon spawns loaded — 'mag' (a magazine among
// its children), 'ammo' (rounds for an internal magazine) or false. null: it
// takes none.
async function loLoaded(c) {
  const det = await loDetail(c.itemType);
  const set = (list) => new Set((list || []).map(x => String(x.c).toLowerCase()));
  const mags = set(det.mags), ammo = set(det.ammo);
  if (!mags.size && !ammo.size) return null;
  const kids = (c.complexChildrenTypes || []).map(k => String(k.itemType || '').toLowerCase());
  return kids.some(k => mags.has(k)) ? 'mag' : kids.some(k => ammo.has(k)) ? 'ammo' : false;
}

// loAmmoBadge marks a weapon loaded or empty once its details are known.
function loAmmoBadge(c) {
  const el = h('span', { class: 'lo-ammo', hidden: true });
  loLoaded(c).then((v) => {
    if (v === null) return;
    el.hidden = false;
    el.className = 'lo-ammo ' + (v ? 'on' : 'off');
    el.textContent = t(v ? 'li.loaded.short' : 'li.empty.short');
    el.title = t(v === 'ammo' ? 'li.feed.ammo' : v ? 'li.feed.mag' : 'li.empty');
  });
  return el;
}

// The weapon classes that spawn without a round in the chamber
// (/api/spawngear/chamber), as a Set of lower-case classes; fetched once.
let _loChamber = null;
function loChamberList() {
  if (!_loChamber) {
    _loChamber = api.get('/api/spawngear/chamber')
      .then(d => new Set((d.unchambered || []).map(c => String(c).toLowerCase())))
      .catch(() => { _loChamber = null; return null; });
  }
  return _loChamber;
}

// loChamberRow is a loaded weapon's "round in the chamber" switch. DayZ
// chambers a round in every spawn-gear weapon (Weapon_Base.SpawnAmmo); off,
// the panel's server mod leaves the chamber empty for this weapon class in
// every loadout (app.ChamberScript). Saved at once, like the fresh-character
// stats. ammo: an internal magazine — a single-shot weapon then spawns empty.
function loChamberRow(item, info, ammo) {
  const cls = String(item.itemType || '');
  const sw = h('input', { type: 'checkbox', class: 'switch', disabled: true, 'aria-label': t('li.chamber') });
  sw.checked = true;
  const note = h('span', { class: 'hint li-chamber-note' });
  const say = () => {
    note.textContent = (sw.checked ? t('li.chamber.on') : t('li.chamber.off') + (ammo ? ' ' + t('li.chamber.single') : ''))
      + ' ' + t('li.chamber.all').replace('{item}', info.n || cls);
  };
  loChamberList().then((set) => {
    if (!set) return;
    sw.checked = !set.has(cls.toLowerCase());
    sw.disabled = false;
    say();
  });
  sw.onchange = async () => {
    sw.disabled = true;
    try {
      const r = await api.post('/api/spawngear/chamber', { class: cls, chambered: sw.checked });
      _loChamber = Promise.resolve(new Set((r.unchambered || []).map(c => String(c).toLowerCase())));
      toast(r.applied ? t('msg.saved') : t('lo.stats.nextStart'), 'ok');
    } catch (e) {
      sw.checked = !sw.checked;
      handleErr(e);
    }
    sw.disabled = false;
    say();
  };
  say();
  return h('label', { class: 'li-chamber' }, [
    sw,
    h('span', { class: 'li-chamber-main' }, [h('b', { text: t('li.chamber') }), note]),
  ]);
}

// DayZ's attach rules the game files do not show: a part's script decides
// (CanPutAsAttachment, scripts 1.29). A bayonet and a suppressor or muzzle
// brake exclude each other — the slots listed block the part — and a weapon
// light (UniversalLight) on a gun with a handguard needs a handguard with a
// rail attached first, the Atlas bipod a RIS or rail one. DayZ attaches a
// kit's parts in order and silently skips one that breaks a rule.
const LO_BLOCKED_BY = {
  ak_bayonet: ['suppressorimpro', 'weaponmuzzleak'],
  mosin_bayonet: ['suppressorimpro', 'weaponmuzzlemosin'],
  sks_bayonet: ['suppressorimpro', 'weaponmuzzle'],
  p1907_bayonet: ['suppressorimpro', 'weaponmuzzle'],
  m9a1_bayonet: ['suppressorimpro', 'weaponmuzzlem4'],
  ak_suppressor: ['suppressorimpro', 'weaponbayonetak'],
  m4_suppressor: ['suppressorimpro', 'weaponbayonet'],
  pistolsuppressor: ['suppressorimpro', 'weaponmuzzlemp5'],
  makarovpbsuppressor: ['suppressorimpro', 'weaponmuzzlemp5'],
  improvisedsuppressor: ['weaponbayonetak', 'weaponbayonet', 'weaponbayonetmosin', 'weaponbayonetsks', 'weaponmuzzlemosin', 'weaponmuzzlemp5'],
  mosin_compensator: ['suppressorimpro', 'pistolmuzzle'],
  mp5_compensator: ['pistolmuzzle'],
};
const loRuleOf = (cls) => {
  const c = String(cls || '').toLowerCase();
  for (const k of Object.keys(LO_BLOCKED_BY)) if (c === k || c.startsWith(k + '_')) return LO_BLOCKED_BY[k];
  return null;
};
const loIsHandguard = (cls) => /hndgrd|handguard/i.test(cls || '');
const loIsRail = (cls) => /railhndgrd|rishndgrd/i.test(cls || '');
const loNeedsRail = (cls) => /^(universallight|atlasbipod)/i.test(cls || '');

// loSpawnOrder sorts a kit item's children the way they should be created:
// handguards first (a light or a bipod goes on their rail), lights and
// bipods last, the rest as listed.
function loSpawnOrder(kids) {
  const rank = (c) => (loIsHandguard(c.itemType) ? 0 : loNeedsRail(c.itemType) ? 2 : 1);
  return kids.map((c, i) => [c, i]).sort((a, b) => rank(a[0]) - rank(b[0]) || a[1] - b[1]).map(x => x[0]);
}

// loSimulate attaches kids to slots the way DayZ will (in spawn order, each
// into the first free slot it fits) and says which part does not go on:
// Map child → { slot, by (the part blocking it) | rail (no rail handguard) }.
function loSimulate(kids, slots) {
  const taken = new Map(); // slot (lower case) → child
  const out = new Map();
  const hasGuardSlot = slots.some(s => /handguard/i.test(s.slot));
  for (const c of loSpawnOrder(kids)) {
    const cls = String(c.itemType || '').toLowerCase();
    const s = slots.find(x => !x.mag && !x.ammo && x.set.has(cls) && !taken.has(x.slot.toLowerCase()));
    if (!s) continue;
    const rule = loRuleOf(cls);
    const by = rule ? rule.map(sl => taken.get(sl)).find(Boolean) : null;
    if (by) { out.set(c, { slot: s, by }); continue; }
    if (loNeedsRail(cls) && hasGuardSlot) {
      const guard = [...taken.entries()].find(([sl]) => /handguard/i.test(sl));
      if (!guard || !loIsRail(guard[1].itemType)) { out.set(c, { slot: s, rail: true }); continue; }
    }
    taken.set(s.slot.toLowerCase(), c);
    out.set(c, { slot: s });
  }
  return out;
}

// loItemEditor edits what goes on and into item (a kit entry with
// complexChildrenTypes): a tile per attachment slot around the drawing, the
// magazine (or rounds) of a weapon, and what else goes inside. ctx is the
// page: info(class) → catalog entry, row(child, onRemove, opts) → a content
// row, open(child, opts) → that child's own dialog, changed() after an edit.
function loItemEditor(item, info, ctx) {
  const host = h('div', { class: 'li-editor' }, h('p', { class: 'hint li-loading', text: t('li.loading') }));
  const kids = () => item.complexChildrenTypes || (item.complexChildrenTypes = []);
  const drop = (c) => { item.complexChildrenTypes = kids().filter(x => x !== c); };
  loDetail(item.itemType).then((det) => {
    const slots = (det.attach || []).map(s => ({ slot: s.slot, items: s.items || [] }));
    if (det.mags && det.mags.length) slots.push({ slot: 'magazine', items: det.mags, mag: true });
    else if (det.ammo && det.ammo.length) slots.push({ slot: 'ammo', items: det.ammo, ammo: true });
    for (const s of slots) {
      s.set = new Set(s.items.map(x => x.c.toLowerCase()));
      const a = s.mag || s.ammo ? { pos: 'bottom', part: 'mag' } : loAttInfo(s.slot);
      s.pos = a.pos;
      s.part = a.part;
      s.label = s.mag ? t('att.magazine') : s.ammo ? t('att.ammo') : loAttLabel(s.slot);
    }
    const kind = loItemKind(info);

    // Which child sits in which slot: the first one not yet placed that fits.
    function assign() {
      const used = new Set(), bySlot = new Map();
      for (const s of slots) {
        const c = kids().find(k => !used.has(k) && s.set.has(String(k.itemType || '').toLowerCase()));
        if (c) { used.add(c); bySlot.set(s, c); }
      }
      return { bySlot, rest: kids().filter(k => !used.has(k)) };
    }
    const edited = () => { render(); ctx.changed(); };

    // put sets what is in slot s (replacing c), pristine and full — a part
    // written without a condition spawned ruined. A part DayZ will not
    // attach next to one already on (a bayonet and a suppressor) asks which
    // to keep; a weapon light without a rail handguard offers the handguard.
    const put = async (s, c, it) => {
      const cls = String(it.c).toLowerCase();
      const placed = loSimulate(kids().filter(k => k !== c), slots);
      const mine = loRuleOf(cls) || [];
      const clash = kids().filter(k => k !== c && placed.has(k) && !placed.get(k).by && !placed.get(k).rail && (
        mine.includes(placed.get(k).slot.slot.toLowerCase()) ||
        (loRuleOf(k.itemType) || []).includes((slots.find(x => x.set.has(cls) && !x.mag && !x.ammo) || { slot: '' }).slot.toLowerCase())));
      if (clash.length) {
        const names = clash.map(k => ctx.info(k.itemType).n).join(', ');
        const ask = t('li.clash.ask').replace(/\{new\}/g, () => it.n || it.c).replace(/\{old\}/g, () => names);
        if (!(await confirmModal(ask, { okText: t('li.clash.replace') }))) return;
        for (const k of clash) drop(k);
      }
      if (c) drop(c);
      kids().push({ itemType: it.c, quickBarSlot: -1, attributes: loFullAttrs(), complexChildrenTypes: [] });
      if (loNeedsRail(cls)) {
        const guardSlot = slots.find(x => /handguard/i.test(x.slot));
        const rail = guardSlot && guardSlot.items.find(r => loIsRail(r.c));
        const guard = guardSlot && kids().find(k => guardSlot.set.has(String(k.itemType || '').toLowerCase()));
        if (rail && !(guard && loIsRail(guard.itemType)) &&
            await confirmModal(t('li.rail.ask').replace('{item}', it.n || it.c).replace('{rail}', rail.n || rail.c), { okText: t('li.rail.add') })) {
          if (guard) drop(guard);
          kids().unshift({ itemType: rail.c, quickBarSlot: -1, attributes: loFullAttrs(), complexChildrenTypes: [] });
        }
      }
      edited();
    };
    const choose = (s, c) => loPick({ title: s.label + ' — ' + info.n, only: s.items, single: true, onAdd: (it) => put(s, c, it) });

    function tile(s, c, sim) {
      const ci = c ? ctx.info(c.itemType) : null;
      const fits = s.items.length;
      const key = c && c.quickBarSlot != null && c.quickBarSlot >= 0 && c.quickBarSlot <= 9;
      const st = c && sim ? sim.get(c) : null;
      const off = st && (st.by || st.rail) ? (st.by ? t('li.off.by').replace('{item}', ctx.info(st.by.itemType).n) : t('li.off.rail')) : '';
      const parts = [h('button', { type: 'button', class: 'li-tile-main', disabled: !fits,
        title: c ? ci.n : fits ? tn('li.fits', { n: fits }) : t('li.nothingFits'), onclick: () => choose(s, c) }, [
        h('span', { class: 'li-tile-label', text: s.label }),
        h('span', { class: 'li-tile-item' + (c ? '' : ' empty'), text: c ? ci.n : (fits ? '+ ' + t('li.choose') : t('li.nothingFits')) }),
        c && (c.complexChildrenTypes || []).length ? h('span', { class: 'li-tile-sub', text: tn('li.inner', { n: c.complexChildrenTypes.length }) }) : null,
        key ? h('span', { class: 'li-tile-key', text: t('lo.hotbar.key').replace('{n}', String((c.quickBarSlot + 1) % 10)) }) : null,
        off ? h('span', { class: 'li-tile-off', text: '⚠ ' + off }) : null,
      ])];
      if (c) {
        // Rounds loaded into an internal magazine take no condition or amount.
        if (!s.ammo) {
          parts.push(h('button', { type: 'button', class: 'icon-btn li-tile-set', title: t('li.settings'), 'aria-label': t('li.settings') + ': ' + ci.n,
            html: svgIcon('i-settings', 14), onclick: () => ctx.open(c, { row: true, parent: info, after: render, onRemove: () => { drop(c); edited(); } }) }));
        }
        parts.push(h('button', { type: 'button', class: 'icon-x li-tile-x', text: '×', title: t('action.delete'), 'aria-label': t('action.delete') + ': ' + ci.n,
          onclick: () => { drop(c); edited(); } }));
      }
      return h('div', { class: 'li-tile' + (c ? ' on' : '') + (off ? ' off' : '') + (fits ? '' : ' none'), 'data-slot': s.slot,
        title: off ? off + ' — ' + t(st.by ? 'li.off.byHint' : 'li.off.railHint') : null }, parts);
    }

    function render() {
      host.innerHTML = '';
      const { bySlot, rest } = assign();
      const sim = loSimulate(kids(), slots);
      if (slots.length) {
        const on = new Set();
        for (const [s, c] of bySlot) if (s.part && !(sim.get(c) && (sim.get(c).by || sim.get(c).rail))) on.add(s.part);
        const pos = loPlace(slots);
        const col = (side) => h('div', { class: 'li-side li-' + side }, pos[side].map(s => tile(s, bySlot.get(s), sim)));
        const filled = new Set([...bySlot.keys()].filter(s => !(sim.get(bySlot.get(s)) && (sim.get(bySlot.get(s)).by || sim.get(bySlot.get(s)).rail))));
        host.append(h('div', { class: 'li-doll li-k-' + kind }, [
          col('top'), col('left'),
          h('div', { class: 'li-art', html: loItemArt(kind, on) }, loPips(slots, filled)),
          col('right'), col('bottom'),
        ]));
        host.append(h('p', { class: 'hint li-count', text: tn('li.filled', { n: bySlot.size, total: slots.length }) }));
      }
      // A weapon given a magazine (or rounds) spawns loaded: DayZ attaches the
      // magazine and chambers a round (Weapon_Base.SpawnAmmo) unless the
      // switch below says otherwise. Load fills the feed at once when one
      // thing fits.
      const feed = slots.find(s => s.mag || s.ammo);
      if (feed) {
        const loaded = bySlot.has(feed);
        host.append(h('div', { class: 'li-loaded' + (loaded ? ' on' : '') }, [
          loIcon(loaded ? 'i-c-ammo' : 'i-c-mag', 16),
          h('span', { class: 'grow', text: t(loaded ? (feed.mag ? 'li.feed.mag' : 'li.feed.ammo') : 'li.empty') }),
          loaded ? null : h('button', { type: 'button', class: 'primary small', text: t('li.load'), disabled: !feed.items.length,
            onclick: () => (feed.items.length === 1 ? put(feed, null, feed.items[0]) : choose(feed, null)) }),
        ]));
        if (loaded) host.append(loChamberRow(item, info, !!feed.ammo));
      }
      // What else goes inside: its cargo, or parts the game files do not
      // name (a mod's) — DayZ still attaches them where they fit.
      const box = h('div', { class: 'li-inside' });
      if (det.cargo || rest.length || !slots.length) {
        box.append(h('span', { class: 'lo-kids-label', i18n: det.cargo ? 'li.cargo' : slots.length ? 'li.other' : 'lo.inside' }));
        for (const c of rest) box.append(ctx.row(c, () => { drop(c); edited(); }));
      }
      box.append(h('button', { type: 'button', class: 'lo-add-inside',
        text: '+ ' + t(det.cargo || !slots.length ? 'lo.inside.add' : 'li.addOther'), onclick: () =>
          loPick({ title: t('lo.inside') + ' — ' + info.n, onAdd: (it) => {
            kids().push({ itemType: it.c, quickBarSlot: -1, attributes: loFullAttrs(), complexChildrenTypes: [] });
            edited();
          } }) }));
      host.append(box);
      applyI18n();
    }
    render();
  });
  return host;
}
