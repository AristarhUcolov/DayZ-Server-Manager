/*
  DayZ Server Manager — players: the Players page (all players, online now,
  bans, kill feed), the player profile, and the ban / kick / message dialogs
  shared with the RCon page.
  Copyright (c) 2026 Aristarh Ucolov.
*/

// ------------------------------------------------------------- durations

// banDurText is a ban length in words: "1 hour", "6 h", "1 day", "7 days".
function banDurText(min) {
  min = Math.round(min || 0);
  if (min <= 0) return t('profile.banDur.perm');
  if (min % 1440 === 0) {
    const d = min / 1440;
    return d === 1 ? t('ban.dur.day1') : t('ban.dur.days').replace('{n}', d);
  }
  if (min % 60 === 0) {
    const hrs = min / 60;
    return hrs === 1 ? t('ban.dur.hour1') : t('ban.dur.hours').replace('{n}', hrs);
  }
  return min + ' ' + t('unit.minutes');
}

// fmtDateTime renders a unix time (seconds) as a local date and time.
function fmtDateTime(sec) {
  if (!sec) return '—';
  const d = new Date(sec * 1000);
  return d.toLocaleDateString() + ' ' + String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
}

// leftText is the time until a unix moment: "3 h 10 min", "2 d 4 h".
function leftText(until) {
  const sec = Math.max(0, until - Math.floor(Date.now() / 1000));
  const min = Math.ceil(sec / 60);
  if (min >= 2880) return Math.floor(min / 1440) + ' ' + t('unit.days') + ' ' + Math.floor((min % 1440) / 60) + ' ' + t('unit.hours');
  return fmtMinutes(min);
}

// banBadgeLabel: "Banned" or "Banned · 3 d 4 h left".
function banBadgeLabel(ban) {
  if (!ban || !ban.until) return t('profile.banned');
  return t('profile.banned') + ' · ' + t('profile.banLeft').replace('{left}', leftText(ban.until));
}

// ------------------------------------------------------------- dialogs

// openBanDialog bans a player. target is one of {key} (a player of the
// database), {playerNo} (an online player) or {target} (a typed ID). Resolves
// to true when the ban was made.
function openBanDialog(target, name) {
  return new Promise((resolve) => {
    let done = false;
    const m = openModal({ title: t('ban.title').replace('{name}', name || '—'), onClose: () => resolve(done) });
    const presets = [60, 360, 1440, 4320, 10080, 43200, 0];
    let minutes = 1440;
    let custom = false;
    const chips = h('div', { class: 'ban-chips', role: 'radiogroup', 'aria-label': t('ban.duration') });
    const customNum = h('input', { type: 'number', min: '1', value: '2', 'aria-label': t('ban.custom') });
    const customUnit = h('select', { 'aria-label': t('ban.custom') }, [
      h('option', { value: '60', i18n: 'ban.custom.hours' }),
      h('option', { value: '1440', i18n: 'ban.custom.days' }),
    ]);
    customUnit.value = '1440';
    const customRow = h('div', { class: 'ban-custom', hidden: true }, [customNum, customUnit]);
    const summary = h('div', { class: 'ban-summary' });
    const reason = h('input', { type: 'text', maxlength: '200', list: 'ban-reasons', placeholder: t('ban.reason.ph'), 'aria-label': t('rcon.reason') });
    const reasons = h('datalist', { id: 'ban-reasons' },
      ['cheat', 'toxic', 'grief', 'exploit', 'combatlog', 'rules'].map(k => h('option', { value: t('ban.r.' + k) })));
    const okBtn = h('button', { class: 'danger' });

    const current = () => custom ? Math.max(1, parseInt(customNum.value, 10) || 1) * Number(customUnit.value) : minutes;
    const update = () => {
      const min = current();
      chips.querySelectorAll('.ban-chip').forEach(c => {
        const on = c.dataset.custom ? custom : (!custom && Number(c.dataset.min) === minutes);
        c.classList.toggle('active', on);
        c.setAttribute('aria-checked', String(on));
      });
      customRow.hidden = !custom;
      summary.textContent = min > 0
        ? t('ban.until').replace('{date}', fmtDateTime(Math.floor(Date.now() / 1000) + min * 60))
        : t('ban.perm.note');
      summary.classList.toggle('perm', min <= 0);
      okBtn.textContent = min > 0 ? t('ban.button.for').replace('{dur}', banDurText(min)) : t('ban.button.perm');
    };
    for (const p of presets) {
      chips.append(h('button', { type: 'button', class: 'ban-chip', role: 'radio', 'data-min': String(p), text: banDurText(p),
        onclick: () => { custom = false; minutes = p; update(); } }));
    }
    chips.append(h('button', { type: 'button', class: 'ban-chip', role: 'radio', 'data-custom': '1', i18n: 'ban.custom',
      onclick: () => { custom = true; update(); setTimeout(() => customNum.focus(), 0); } }));
    customNum.addEventListener('input', update);
    customUnit.addEventListener('change', update);

    okBtn.onclick = () => withBusy(okBtn, async () => {
      try {
        const r = await api.post('/api/bans/add', Object.assign({}, target, { minutes: current(), reason: reason.value.trim() }));
        done = true;
        toast(t('ban.done').replace('{name}', (r.ban && r.ban.name) || name || '—'), 'ok');
        m.close();
      } catch (e) { handleErr(e); }
    });
    m.body.append(
      h('label', { i18n: 'ban.duration' }), chips, customRow, summary,
      h('label', { i18n: 'rcon.reason' }), reason, reasons,
      h('p', { class: 'hint ban-info', i18n: 'ban.info' }),
      h('div', { class: 'actions' }, [okBtn, h('button', { i18n: 'action.cancel', onclick: () => m.close() })]),
    );
    update();
    applyI18n();
    update(); // applyI18n rewrote the chip labels; the button text is ours
    setTimeout(() => reason.focus(), 0);
  });
}

// openKickDialog kicks an online player (by RCon number) with a reason.
function openKickDialog(playerNo, name, key) {
  return new Promise((resolve) => {
    let done = false;
    const m = openModal({ title: t('kick.title').replace('{name}', name || '—'), onClose: () => resolve(done) });
    const reason = h('input', { type: 'text', maxlength: '120', placeholder: t('kick.reason.ph'), 'aria-label': t('rcon.reason') });
    const okBtn = h('button', { class: 'danger', i18n: 'action.kick' });
    const go = () => withBusy(okBtn, async () => {
      try {
        await api.post('/api/players/kick', { playerNo, reason: reason.value.trim(), key: key || '' });
        done = true;
        toast(t('kick.done').replace('{name}', name || '—'), 'ok');
        m.close();
      } catch (e) { handleErr(e); }
    });
    okBtn.onclick = go;
    reason.addEventListener('keydown', (e) => { if (e.key === 'Enter') go(); });
    m.body.append(
      h('label', { i18n: 'rcon.reason' }), reason,
      h('div', { class: 'actions' }, [okBtn, h('button', { i18n: 'action.cancel', onclick: () => m.close() })]),
    );
    applyI18n();
    setTimeout(() => reason.focus(), 0);
  });
}

// openMessageDialog sends a private message to an online player.
function openMessageDialog(playerNo, name) {
  const m = openModal({ title: t('action.message') + ' — ' + (name || '—') });
  const msg = h('input', { type: 'text', maxlength: '200', placeholder: t('rcon.message.ph'), style: { width: '100%' } });
  const okBtn = h('button', { class: 'primary', i18n: 'action.send' });
  const send = () => withBusy(okBtn, async () => {
    const text = msg.value.trim();
    if (!text) return;
    try { await api.post('/api/rcon/say', { playerId: playerNo, message: text }); m.close(); toast(t('rcon.message.sent'), 'ok'); }
    catch (e) { handleErr(e); }
  });
  okBtn.onclick = send;
  msg.addEventListener('keydown', (e) => { if (e.key === 'Enter') send(); });
  m.body.append(h('label', { i18n: 'rcon.message.label' }), msg,
    h('div', { class: 'actions' }, [okBtn, h('button', { i18n: 'action.cancel', onclick: () => m.close() })]));
  applyI18n();
  setTimeout(() => msg.focus(), 0);
}

// serverActions is the bar over the online list: a message to everyone,
// kicking everyone, and locking joining (BattlEye #lock) — say, before
// maintenance. reload re-reads the list.
function serverActions(online, locked, reload) {
  const bar = h('div', { class: 'online-actions' }, [
    h('button', { type: 'button', class: 'small secondary', i18n: 'players.msgAll', disabled: !online,
      onclick: () => openMessageDialog(undefined, t('players.everyone')) }),
    h('button', { type: 'button', class: 'small secondary', i18n: 'players.kickAll', disabled: !online,
      onclick: () => openKickAllDialog(online).then(ok => { if (ok) reload(); }) }),
    h('button', { type: 'button', class: 'small ' + (locked ? 'primary' : 'secondary'), i18n: locked ? 'players.unlock' : 'players.lock',
      title: t(locked ? 'players.unlock.hint' : 'players.lock.hint'),
      onclick: (e) => withBusy(e.currentTarget, async () => {
        if (!locked && !await confirmModal(t('players.lock.confirm'), { okText: t('players.lock') })) return;
        try {
          await api.post('/api/players/lock', { lock: !locked });
          toast(t(locked ? 'players.unlocked' : 'players.locked'), 'ok');
          reload();
        } catch (err) { handleErr(err); }
      }) }),
  ]);
  if (locked) bar.append(h('span', { class: 'badge warn online-lock', text: '🔒 ' + t('players.lockedBadge') }));
  return bar;
}

// openKickAllDialog kicks every online player with one reason.
function openKickAllDialog(online) {
  return new Promise((resolve) => {
    let done = false;
    const m = openModal({ title: tn('players.kickAll.title', { n: online }), onClose: () => resolve(done) });
    const reason = h('input', { type: 'text', maxlength: '120', placeholder: t('players.kickAll.ph'), 'aria-label': t('rcon.reason') });
    const okBtn = h('button', { class: 'danger', i18n: 'players.kickAll' });
    const go = () => withBusy(okBtn, async () => {
      try {
        const r = await api.post('/api/players/kickall', { reason: reason.value.trim() });
        done = true;
        toast(tn('players.kickAll.done', { n: r.kicked || 0 }), 'ok');
        m.close();
      } catch (e) { handleErr(e); }
    });
    okBtn.onclick = go;
    reason.addEventListener('keydown', (e) => { if (e.key === 'Enter') go(); });
    m.body.append(
      h('p', { class: 'hint', i18n: 'players.kickAll.hint' }),
      h('label', { i18n: 'rcon.reason' }), reason,
      h('div', { class: 'actions' }, [okBtn, h('button', { i18n: 'action.cancel', onclick: () => m.close() })]),
    );
    applyI18n();
    setTimeout(() => reason.focus(), 0);
  });
}

// playersCSV is the players list as a spreadsheet file (UTF-8 with a BOM, so
// Excel reads names in any alphabet).
function playersCSV(items, bannedSet) {
  const cols = ['name', 'id', 'beGuid', 'ip', 'firstSeen', 'lastSeen', 'sessions', 'playtimeMinutes', 'kills', 'deaths', 'online', 'watch', 'banned', 'aliases', 'note'];
  const cell = (v) => {
    const s = v == null ? '' : String(v);
    return /[",;\r\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
  };
  const rows = [cols.join(',')];
  for (const p of items) {
    const banned = bannedSet.has(p.key) || (p.id && bannedSet.has(p.id));
    rows.push(cols.map(c => cell(c === 'aliases' ? (p.aliases || []).join(' | ')
      : c === 'banned' ? (banned ? 'yes' : '')
      : (c === 'online' || c === 'watch') ? (p[c] ? 'yes' : '')
      : p[c])).join(','));
  }
  const blob = new Blob(['\ufeff' + rows.join('\r\n') + '\r\n'], { type: 'text/csv;charset=utf-8' });
  const a = h('a', { href: URL.createObjectURL(blob), download: 'players-' + new Date().toISOString().slice(0, 10) + '.csv' });
  document.body.append(a);
  a.click();
  setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
}

// showOnLiveMap opens the live map centred on a player.
function showOnLiveMap(key) {
  try { sessionStorage.setItem('liveFocus', key); } catch {}
  document.querySelectorAll('.modal-overlay').forEach(o => o.remove());
  navigate('livemap');
}

// emptyBox is an .empty-state: icon, title, hint and the next step's buttons.
function emptyBox(icon, titleKey, hintKey, buttons) {
  return h('div', { class: 'empty-state' }, [
    h('div', { class: 'es-icon', html: svgIcon(icon, 34) }),
    h('div', { class: 'es-title', i18n: titleKey }),
    hintKey ? h('div', { class: 'es-hint', i18n: hintKey }) : null,
    buttons && buttons.length ? h('div', { class: 'actions' }, buttons) : null,
  ].filter(Boolean));
}

// startServerBtn starts the DayZ server from a page that needs it running.
function startServerBtn(onStarted) {
  const b = h('button', { type: 'button', class: 'primary', i18n: 'action.start' });
  b.onclick = () => withBusy(b, async () => {
    try {
      await api.post('/api/server/start');
      toast(t('msg.starting'), 'ok');
      await refreshStatus();
      if (onStarted) onStarted();
    } catch (e) { handleErr(e); }
  });
  return b;
}

// unbanFlow asks and lifts a ban (a panel record id, or "dayz:…"/"be:…").
async function unbanFlow(id, name) {
  if (!(await confirmModal(t('profile.unbanConfirm').replace('{name}', name || '—')))) return false;
  try { await api.post('/api/bans/remove', { id }); toast(t('profile.unbanned.msg'), 'ok'); return true; }
  catch (e) { handleErr(e); return false; }
}

// ------------------------------------------------------------- profile

function idRow(labelKey, value) {
  if (!value) return null;
  return h('div', { class: 'profile-idrow' }, [
    h('span', { class: 'hint', i18n: labelKey }),
    h('span', { class: 'mono profile-idval', text: value }),
    h('button', { class: 'icon-btn small-icon', title: t('action.copy'), 'aria-label': t('action.copy'), text: '⧉',
      onclick: () => copyText(value) }),
  ]);
}

// openPlayerProfile shows a per-player admin panel: identity, stats,
// moderation (ban / unban / kick / message, note & watch, history) and
// recent combat.
async function openPlayerProfile(key, onChange) {
  let d;
  try { d = await api.get('/api/players/profile?key=' + encodeURIComponent(key)); }
  catch (e) { handleErr(e); return; }
  const p = d.player || {};
  const m = openModal({ title: p.name || '—', wide: true });
  const changed = () => { if (onChange) onChange(); };
  const reopen = () => { m.close(); changed(); openPlayerProfile(key, onChange); };

  const badges = h('div', { class: 'profile-badges' }, [
    p.online ? h('span', { class: 'badge ok', text: t('players.onlineBadge') + (d.ping != null ? ' · ' + t('profile.ping').replace('{ms}', d.ping) : '') }) : null,
    p.watch ? h('span', { class: 'badge warn', text: '★ ' + t('players.watched') }) : null,
    d.banned ? h('span', { class: 'badge err', text: banBadgeLabel(d.ban) }) : null,
  ]);
  const identity = h('div', { class: 'profile-id' }, [
    idRow('profile.dayzId', p.id),
    idRow('profile.beGuid', p.beGuid),
    idRow('profile.ip', d.onlineIp || p.ip),
    (p.aliases && p.aliases.length) ? h('div', { class: 'hint', text: t('players.aliases') + ': ' + p.aliases.join(', ') }) : null,
    h('div', { class: 'hint', text: fmtWhen(p.firstSeen) + ' → ' + fmtWhen(p.lastSeen) }),
    (d.sameIp && d.sameIp.length) ? h('div', { class: 'profile-sameip', title: t('profile.sameIp.hint') }, [
      h('span', { class: 'badge warn', i18n: 'profile.sameIp' }),
      ...d.sameIp.map(o => h('button', { type: 'button', class: 'link-btn', text: o.name || o.key,
        onclick: () => { m.close(); openPlayerProfile(o.key, onChange); } })),
    ]) : null,
  ]);

  const kd = p.deaths ? ((p.kills || 0) / p.deaths) : (p.kills || 0);
  const stat = (label, val) => h('div', { class: 'profile-stat' }, [
    h('div', { class: 'ps-num', text: val }), h('div', { class: 'hint', i18n: label })]);
  const stats = h('div', { class: 'profile-stats' }, [
    stat('col.sessions', String(p.sessions || 0)),
    stat('col.playtime', fmtMinutes(p.playtimeMinutes || 0)),
    stat('col.kills', String(p.kills || 0)),
    stat('col.deaths', String(p.deaths || 0)),
    stat('col.kd', kd.toFixed(2)),
  ]);

  // Ban details.
  let banBox = null;
  if (d.banned && d.ban) {
    const b = d.ban;
    banBox = h('div', { class: 'warning-bar err-bar profile-ban' }, [
      h('b', { text: b.until ? t('ban.until').replace('{date}', fmtDateTime(b.until)) : t('ban.perm.note') }),
      b.reason ? h('div', { text: t('profile.ban.reason').replace('{reason}', b.reason) }) : null,
      b.by ? h('div', { class: 'hint', text: t('profile.ban.by').replace('{by}', b.by) }) : null,
      b.source !== 'panel' ? h('div', { class: 'hint', i18n: 'bans.external' }) : null,
    ]);
  }

  // Note + watch.
  const ta = h('textarea', { rows: '2', class: 'note-input', style: { width: '100%', height: '58px', minHeight: '58px' }, placeholder: t('players.note.placeholder') });
  ta.value = p.note || '';
  const watchCb = h('input', { type: 'checkbox' });
  watchCb.checked = !!p.watch;
  const noteRow = h('div', {}, [
    ta,
    h('div', { class: 'actions' }, [
      h('label', { class: 'inline-check' }, [watchCb, h('span', { i18n: 'players.watch.flag' })]),
      h('button', { class: 'primary', i18n: 'action.save', onclick: (e) => withBusy(e.currentTarget, async () => {
        try { await api.post('/api/players/note', { key: p.key, note: ta.value, watch: watchCb.checked }); toast(t('msg.saved'), 'ok'); reopen(); }
        catch (err) { handleErr(err); }
      }) }),
    ]),
  ]);

  // Moderation actions.
  const actions = h('div', { class: 'actions profile-mod' });
  if (d.banned && d.ban) {
    actions.append(h('button', { class: 'secondary', i18n: 'profile.unban', onclick: async () => {
      if (await unbanFlow(d.ban.id, p.name)) reopen();
    } }));
  } else {
    const b = h('button', { class: 'danger', i18n: 'profile.ban', onclick: async () => {
      if (await openBanDialog({ key: p.key }, p.name)) reopen();
    } });
    if (!d.canBan) { b.disabled = true; b.title = t('profile.noIds'); }
    actions.append(b);
  }
  if (d.onlineId >= 0) {
    actions.append(
      h('button', { class: 'secondary', i18n: 'profile.kick', onclick: async () => { if (await openKickDialog(d.onlineId, p.name, p.key)) reopen(); } }),
      h('button', { class: 'secondary', i18n: 'profile.message', onclick: () => openMessageDialog(d.onlineId, p.name) }),
      h('button', { class: 'secondary', i18n: 'profile.showOnMap', onclick: () => showOnLiveMap(p.key) }),
    );
  }
  if (p.id && /^[A-Za-z0-9+/_-]{43}=$/.test(p.id)) {
    actions.append(h('button', { class: 'secondary', i18n: d.whitelisted ? 'wl.removeFrom' : 'wl.add', onclick: (e) => withBusy(e.currentTarget, async () => {
      try {
        if (d.whitelisted) await api.post('/api/whitelist/remove', { id: p.id });
        else await api.post('/api/whitelist/add', { key: p.key });
        toast(t(d.whitelisted ? 'wl.removed' : 'wl.added'), 'ok');
        reopen();
      } catch (err) { handleErr(err); }
    }) }));
  }
  const noIds = !d.canBan && !d.banned ? h('p', { class: 'hint', i18n: 'profile.noIds' }) : null;

  // Moderation history.
  const hist = h('div', { class: 'profile-activity' });
  if (!(d.history && d.history.length)) {
    hist.append(h('p', { class: 'hint', i18n: 'profile.history.none' }));
  } else {
    for (const e of d.history) {
      const meta = [];
      if (e.action === 'ban') meta.push(e.until ? t('ban.until').replace('{date}', fmtDateTime(e.until)) : t('profile.banDur.perm'));
      if (e.reason) meta.push(e.reason);
      if (e.by) meta.push(t('profile.ban.by').replace('{by}', e.by));
      hist.append(h('div', { class: 'adm-row' }, [
        h('span', { class: 'adm-time', text: fmtDateTime(e.at) }),
        h('span', { class: 'adm-type mod-' + e.action, i18n: 'mod.action.' + e.action }),
        meta.length ? h('span', { class: 'adm-meta', text: meta.join(' · ') }) : null,
      ]));
    }
  }

  // Recent combat.
  const activity = h('div', { class: 'profile-activity' });
  if (!(d.events && d.events.length)) {
    activity.append(h('p', { class: 'hint', i18n: 'profile.noActivity' }));
  } else {
    for (const k of d.events) activity.append(killRow(k));
  }

  // Base building: what they placed, built, destroyed or buried.
  let basesBox = null;
  if (d.bases && d.bases.length) {
    basesBox = h('div', { class: 'profile-activity' });
    for (const b of d.bases) {
      basesBox.append(h('div', { class: 'adm-row' }, [
        h('span', { class: 'adm-time', text: fmtWhen(b.at) }),
        h('span', { class: 'adm-type adm-base-' + b.kind, i18n: 'profile.base.' + b.kind }),
        h('span', { class: 'adm-meta', text: heatBaseText(Object.assign({}, b, { at: '' })) }),
        b.pos ? h('span', { class: 'adm-meta mono', text: 'X ' + Math.round(b.pos[0]) + ' · Z ' + Math.round(b.pos[1]) }) : null,
      ]));
    }
  }

  m.body.append(...[
    badges, identity, stats, banBox,
    h('h4', { i18n: 'profile.mod' }), noteRow, actions, noIds,
    h('h4', { i18n: 'profile.history' }), hist,
    h('h4', { i18n: 'profile.activity' }), activity,
    basesBox ? h('h4', { i18n: 'profile.bases' }) : null, basesBox,
  ].filter(Boolean));
  applyI18n();
}

// killTime is when a kill happened: the log's HH:MM:SS today, the day and
// minute for anything older (the feed spans days).
function killTime(k) {
  const d = k.at ? new Date(k.at) : null;
  if (!d || isNaN(d) || d.toDateString() === new Date().toDateString()) return k.time || '';
  let day;
  try { day = d.toLocaleDateString(State.lang || undefined, { day: '2-digit', month: '2-digit' }); }
  catch { day = d.toLocaleDateString(); }
  return day + ' ' + String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
}

// playerKeyLookup maps a name (or an older alias) to the one player who has
// it. Shared names — "Survivor" is every fresh character's — stay unlinked
// rather than open the wrong profile.
function playerKeyLookup(players) {
  const m = new Map();
  for (const p of players || []) {
    if (!p.key) continue;
    for (const n of [p.name, ...(p.aliases || [])]) {
      if (n) m.set(n, m.has(n) && m.get(n) !== p.key ? '' : p.key);
    }
  }
  return (name) => m.get(name) || '';
}

// killRow is one kill-feed line. With opts.keyOf(name) → player key, the names
// open that player's profile (opts.onChange runs after changes made there).
function killRow(k, opts = {}) {
  // kind is "pvp" | "env" | "suicide". Older data has no kind.
  const kind = k.kind || (k.suicide ? 'suicide' : (k.killer ? 'pvp' : 'env'));
  const who = (name) => admName(name, opts);
  const pieces = [
    h('span', { class: 'adm-time', text: killTime(k), title: k.at ? fmtWhen(k.at) : null }),
    h('span', { class: 'adm-type adm-kill', text: t('kill.kind.' + kind) }),
  ];
  // Whoever did it: a player, or the zombie/animal/fall that the log named.
  if (kind === 'pvp' && k.killer) pieces.push(who(k.killer), h('span', { class: 'adm-arrow', text: '→' }));
  else if (kind === 'env' && k.source) pieces.push(killSourceEl(k.source), h('span', { class: 'adm-arrow', text: '→' }));
  pieces.push(who(k.victim));
  const meta = [];
  if (k.weapon) meta.push(k.weapon);
  if (k.distance) meta.push(fmtMeters(k.distance));
  if (meta.length) pieces.push(h('span', { class: 'adm-meta', text: '(' + meta.join(', ') + ')' }));
  return h('div', { class: 'adm-row' }, pieces);
}

// ------------------------------------------------------------- the page

const PLAYER_TABS = ['all', 'online', 'bans', 'whitelist', 'killfeed'];

Views.players = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.players', 'players.subtitle'));
  { const w = adminLogWarning(); if (w) root.append(w); }
  let d, bans = { active: [], history: [] };
  try {
    [d, bans] = await Promise.all([api.get('/api/players'), api.get('/api/bans').catch(() => bans)]);
  } catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;

  let list = d.players || [];
  let tab = PLAYER_TABS.includes(localStorage.getItem('playersTab')) ? localStorage.getItem('playersTab') : 'all';

  // Summary tiles.
  const tileNum = (v) => h('div', { class: 'metric-num', text: String(v) });
  const tiles = {
    total: tileNum(d.total || 0), online: tileNum(d.online || 0),
    watch: tileNum(list.filter(p => p.watch).length), bans: tileNum((bans.active || []).length),
  };
  // A tile opens its tab; target may also be a function (the watchlist tile
  // switches the "on watchlist" filter on, too).
  const tile = (key, el, target) => h('button', { type: 'button', class: 'card tile-btn',
    onclick: () => { if (typeof target === 'function') target(); else if (target) setTab(target); } }, [h('h3', { i18n: key }), el]);
  root.append(h('div', { class: 'grid-4 players-tiles' }, [
    tile('players.total', tiles.total, () => { onlyWatch = false; shown = 100; setTab('all'); }),
    tile('players.online', tiles.online, 'online'),
    tile('players.watching', tiles.watch, () => { onlyWatch = true; shown = 100; setTab('all'); }),
    tile('players.bannedCount', tiles.bans, 'bans'),
  ]));

  const tabBar = h('div', { class: 'section-tabs players-tabs', role: 'tablist' });
  const host = h('div');
  root.append(tabBar, host);

  function setTab(id) {
    tab = id;
    try { localStorage.setItem('playersTab', id); } catch {}
    renderTabs();
    renderTab();
  }
  function renderTabs() {
    tabBar.innerHTML = '';
    const labels = { all: 'players.tab.all', online: 'players.online', bans: 'players.tab.bans', whitelist: 'wl.tab', killfeed: 'players.killfeed' };
    for (const id of PLAYER_TABS) {
      tabBar.append(h('button', { type: 'button', role: 'tab', class: 'section-tab' + (tab === id ? ' active' : ''),
        'aria-selected': String(tab === id), i18n: labels[id], onclick: () => setTab(id) }));
    }
    // A phone shows only part of the strip: bring the active tab into view
    // (sideways only — the page itself must not jump).
    const act = tabBar.querySelector('.section-tab.active');
    if (act && tabBar.scrollWidth > tabBar.clientWidth) {
      const a = act.getBoundingClientRect(), b = tabBar.getBoundingClientRect();
      tabBar.scrollLeft += (a.left + a.width / 2) - (b.left + b.width / 2);
    }
    stripFades();
  }
  // stripFades fades the edge of the strip that hides more tabs.
  function stripFades() {
    const max = tabBar.scrollWidth - tabBar.clientWidth;
    const x = getComputedStyle(tabBar).direction === 'rtl' ? -tabBar.scrollLeft : tabBar.scrollLeft;
    tabBar.classList.toggle('tabs-fade-start', max > 1 && x > 1);
    tabBar.classList.toggle('tabs-fade-end', max > 1 && x < max - 1);
  }
  tabBar.addEventListener('scroll', stripFades, { passive: true });
  window.addEventListener('resize', stripFades);

  async function reloadAll() {
    try {
      [d, bans] = await Promise.all([api.get('/api/players'), api.get('/api/bans').catch(() => bans)]);
    } catch (e) { handleErr(e); return; }
    list = d.players || [];
    tiles.total.textContent = String(d.total || 0);
    tiles.online.textContent = String(d.online || 0);
    tiles.watch.textContent = String(list.filter(p => p.watch).length);
    tiles.bans.textContent = String((bans.active || []).length);
    if (host.isConnected) renderTab();
  }

  // ---- all players
  let query = '', sortBy = localStorage.getItem('playersSort') || 'lastSeen', shown = 100, onlyWatch = false;
  const bannedIDs = () => {
    const s = new Set();
    for (const b of bans.active || []) { if (b.key) s.add(b.key); if (b.dayzId) s.add(b.dayzId); }
    return s;
  };
  function sorted(items) {
    const by = {
      lastSeen: (a, b) => String(b.lastSeen || '').localeCompare(String(a.lastSeen || '')),
      firstSeen: (a, b) => String(a.firstSeen || '').localeCompare(String(b.firstSeen || '')),
      name: (a, b) => String(a.name || '').localeCompare(String(b.name || '')),
      playtime: (a, b) => (b.playtimeMinutes || 0) - (a.playtimeMinutes || 0),
      kills: (a, b) => (b.kills || 0) - (a.kills || 0),
      sessions: (a, b) => (b.sessions || 0) - (a.sessions || 0),
    }[sortBy] || (() => 0);
    return items.slice().sort((a, b) => (b.online ? 1 : 0) - (a.online ? 1 : 0) || by(a, b));
  }
  function matches(p) {
    if (onlyWatch && !p.watch) return false;
    if (!query) return true;
    const q = query.toLowerCase();
    return [p.name, p.id, p.beGuid, p.ip, p.note, ...(p.aliases || [])].some(v => v && String(v).toLowerCase().includes(q));
  }
  function renderAll() {
    host.innerHTML = '';
    const search = h('input', { type: 'search', value: query, placeholder: t('players.search.ph'), 'aria-label': t('players.search.ph'), class: 'players-search' });
    const sortSel = h('select', { 'aria-label': t('players.sort') }, ['lastSeen', 'name', 'playtime', 'kills', 'sessions', 'firstSeen'].map(k =>
      h('option', { value: k, i18n: 'col.' + k })));
    sortSel.value = sortBy;
    const watchCb = h('input', { type: 'checkbox' });
    watchCb.checked = onlyWatch;
    const tableHost = h('div');
    const draw = () => {
      tableHost.innerHTML = '';
      const banned = bannedIDs();
      const items = sorted(list.filter(matches));
      if (!list.length) {
        tableHost.append(h('div', { class: 'empty-state' }, [h('div', { class: 'es-hint', i18n: 'players.noData' })]));
        applyI18n();
        return;
      }
      if (!items.length) {
        tableHost.append(h('p', { class: 'hint', i18n: 'players.none.match' }));
        applyI18n();
        return;
      }
      // Phone: the pl-hide-sm columns go; their values move to a line under
      // the name (pl-sub), so the table fits without a sideways scroll.
      const tbl = h('table', { class: 'players-table' });
      tbl.append(h('thead', {}, h('tr', {}, [
        h('th', { text: '' }), h('th', { i18n: 'col.name' }), h('th', { class: 'pl-hide-sm', i18n: 'col.playerId' }),
        h('th', { class: 'pl-hide-sm', i18n: 'col.lastSeen' }), h('th', { class: 'num pl-hide-sm', i18n: 'col.sessions' }), h('th', { class: 'num', i18n: 'col.playtime' }),
        h('th', { class: 'num pl-hide-sm', i18n: 'col.kills' }), h('th', { class: 'num pl-hide-sm', i18n: 'col.deaths' }),
      ])));
      const tb = h('tbody');
      for (const p of items.slice(0, shown)) {
        const isBanned = banned.has(p.key) || (p.id && banned.has(p.id));
        tb.append(h('tr', { class: p.watch ? 'row-watch' : '' }, [
          h('td', { class: 'star-cell' }, h('button', {
            class: 'star-btn' + (p.watch ? ' on' : ''), title: t(p.watch ? 'players.watch.off' : 'players.watch.on'),
            'aria-label': t(p.watch ? 'players.watch.off' : 'players.watch.on'), text: p.watch ? '★' : '☆',
            onclick: async () => {
              try { await api.post('/api/players/note', { key: p.key, note: p.note || '', watch: !p.watch }); p.watch = !p.watch; tiles.watch.textContent = String(list.filter(x => x.watch).length); draw(); }
              catch (e) { handleErr(e); }
            } })),
          h('td', { class: 'pl-name' }, [
            h('div', { class: 'player-cell' }, [
              h('span', { class: 'player-link', text: p.name || '—', onclick: () => openPlayerProfile(p.key, reloadAll) }),
              p.online ? h('span', { class: 'badge ok', i18n: 'players.onlineBadge' }) : null,
              isBanned ? h('span', { class: 'badge err', i18n: 'profile.banned' }) : null,
            ]),
            h('div', { class: 'pl-sub hint', text: [fmtWhen(p.lastSeen), tn('players.sub.sessions', { n: p.sessions || 0 }),
              t('col.kd') + ' ' + (p.kills || 0) + '/' + (p.deaths || 0)].join(' · ') }),
            p.aliases && p.aliases.length ? h('div', { class: 'hint', text: t('players.aliases') + ': ' + p.aliases.join(', ') }) : null,
            p.note ? h('div', { class: 'player-note', text: p.note }) : null,
          ]),
          h('td', { class: 'mono id-cell pl-hide-sm', title: p.id || '' }, h('span', { text: p.id ? p.id.slice(0, 12) + (p.id.length > 12 ? '…' : '') : '—' })),
          h('td', { class: 'pl-hide-sm', text: fmtWhen(p.lastSeen) }),
          h('td', { class: 'num pl-hide-sm', text: String(p.sessions || 0) }),
          h('td', { class: 'num', text: fmtMinutes(p.playtimeMinutes || 0) }),
          h('td', { class: 'num pl-hide-sm', text: String(p.kills || 0) }),
          h('td', { class: 'num pl-hide-sm', text: String(p.deaths || 0) }),
        ]));
      }
      tbl.append(tb);
      tableHost.append(h('div', { class: 'table-scroll' }, tbl));
      const foot = h('div', { class: 'players-foot' }, [
        h('span', { class: 'hint', text: t('players.shown').replace('{n}', Math.min(shown, items.length)).replace('{total}', items.length) }),
      ]);
      if (items.length > shown) {
        foot.append(h('button', { class: 'small', text: t('players.showMore').replace('{n}', Math.min(100, items.length - shown)),
          onclick: () => { shown += 100; draw(); } }));
      }
      tableHost.append(foot);
      applyI18n();
    };
    let timer = 0;
    search.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(() => { query = search.value.trim(); shown = 100; draw(); }, 200); });
    sortSel.addEventListener('change', () => { sortBy = sortSel.value; try { localStorage.setItem('playersSort', sortBy); } catch {} draw(); });
    watchCb.addEventListener('change', () => { onlyWatch = watchCb.checked; shown = 100; draw(); });
    host.append(h('div', { class: 'card' }, [
      h('div', { class: 'players-toolbar' }, [
        search,
        h('label', { class: 'inline-check' }, [watchCb, h('span', { i18n: 'players.watching' })]),
        h('span', { class: 'grow' }),
        // Label and select wrap as one piece on a narrow screen.
        h('div', { class: 'players-sort' }, [h('label', { class: 'hint', for: assocId(sortSel), i18n: 'players.sort' }), sortSel]),
        h('button', { type: 'button', class: 'small secondary', i18n: 'players.exportCsv', title: t('players.exportCsv.hint'),
          onclick: () => playersCSV(sorted(list.filter(matches)), bannedIDs()) }),
      ]),
      tableHost,
    ]));
    draw();
  }

  // ---- online now
  let onlineTimer = 0;
  async function renderOnline() {
    host.innerHTML = '';
    const card = h('div', { class: 'card' });
    host.append(card);
    const body = h('div');
    const refreshBtn = h('button', { class: 'small secondary', i18n: 'rcon.refresh', onclick: () => load() });
    card.append(h('div', { class: 'card-head' }, [
      cardTitle('i-players', 'players.online'),
      h('div', { class: 'row' }, [refreshBtn]),
    ]), body);
    const load = async () => {
      let r;
      try { r = await api.get('/api/players/online'); } catch (e) { body.textContent = e.message; return; }
      if (!body.isConnected) return;
      body.innerHTML = '';
      refreshBtn.hidden = !r.running;
      if (!r.running) {
        // The next step, in the right order: BattlEye reads the RCon password
        // when the server starts, so a missing one comes first.
        body.append(r.configured === false
          ? emptyBox('i-rcon', 'online.needRcon.title', 'online.needRcon.hint',
            [h('button', { type: 'button', class: 'primary', i18n: 'online.setRcon', onclick: () => navigate('rcon') })])
          : emptyBox('i-server', 'online.stopped.title', 'online.stopped.hint', [startServerBtn(() => load())]));
        return;
      }
      if (r.rcon === 'notConfigured') {
        body.append(h('p', { class: 'hint' }, [h('span', { i18n: 'online.noRcon' }), ' ', h('a', { href: '#rcon', i18n: 'nav.rcon' })]));
        applyI18n(); return;
      }
      if (r.rcon === 'error') {
        body.append(r.errorKey ? h('div', { class: 'warning-bar' + (r.errorKey === 'rcon.err.auth' ? ' err-bar' : '') }, [
          h('span', { i18n: r.errorKey }), ' ', h('a', { href: '#rcon', i18n: 'nav.rcon' })])
          : h('p', { class: 'hint', text: 'RCon: ' + (r.error || '') }));
        return;
      }
      const ps = r.players || [];
      tiles.online.textContent = String(ps.length);
      body.append(serverActions(ps.length, r.locked, load));
      if (!ps.length) { body.append(h('p', { class: 'hint', i18n: 'online.empty' })); applyI18n(); return; }
      // "In game": this session so far (a fresh join next to a fight reads
      // differently from a regular); the total playtime is in its tooltip.
      // Phone: #, in-game, IP and GUID go under the name.
      const tbl = h('table', { class: 'online-table' });
      tbl.append(h('thead', {}, h('tr', {}, [
        h('th', { class: 'pl-hide-sm', text: '#' }), h('th', { i18n: 'col.name' }), h('th', { class: 'num', i18n: 'col.ping' }),
        h('th', { class: 'num pl-hide-sm', i18n: 'online.col.inGame' }),
        h('th', { class: 'pl-hide-sm', i18n: 'col.ip' }), h('th', { class: 'pl-hide-sm', i18n: 'col.guid' }), h('th', { text: '' }),
      ])));
      const tb = h('tbody');
      const now = Date.now() + (State.clockSkew || 0);
      for (const p of ps) {
        const nameEl = p.key
          ? h('span', { class: 'player-link', text: p.name, onclick: () => openPlayerProfile(p.key, reloadAll) })
          : h('span', { text: p.name });
        const inGame = p.since ? fmtMinutes((now - Date.parse(p.since)) / 60000) : '';
        tb.append(h('tr', {}, [
          h('td', { class: 'num pl-hide-sm', text: String(p.no) }),
          h('td', { class: 'pl-name' }, [nameEl, p.lobby ? h('span', { class: 'badge mute', i18n: 'rcon.lobby' }) : null,
            h('div', { class: 'pl-sub hint', text: [inGame ? t('online.col.inGame') + ': ' + inGame : '', p.ip].filter(Boolean).join(' · ') })]),
          h('td', { class: 'num' }, h('span', { class: 'ping ' + (p.ping < 0 ? '' : p.ping < 100 ? 'ok' : p.ping < 200 ? 'warn' : 'bad'), text: p.ping < 0 ? '—' : String(p.ping) })),
          h('td', { class: 'num pl-hide-sm', text: inGame || '—',
            title: p.playtimeMinutes != null ? tn('online.playtimeTotal', { t: fmtMinutes(p.playtimeMinutes) }) : null }),
          h('td', { class: 'mono pl-hide-sm', text: p.ip || '—' }),
          h('td', { class: 'mono id-cell pl-hide-sm', title: p.guid || '' }, h('span', { text: p.guid ? p.guid.slice(0, 10) + '…' : '—' })),
          h('td', { class: 'row-actions' }, [
            h('button', { class: 'small', i18n: 'action.message', onclick: () => openMessageDialog(p.no, p.name) }),
            h('button', { class: 'small secondary', i18n: 'action.kick', onclick: async () => { if (await openKickDialog(p.no, p.name, p.key)) load(); } }),
            h('button', { class: 'small danger', i18n: 'action.ban', onclick: async () => { if (await openBanDialog({ playerNo: p.no }, p.name)) { load(); reloadAll(); } } }),
          ]),
        ]));
      }
      tbl.append(tb);
      body.append(h('div', { class: 'table-scroll' }, tbl));
      applyI18n();
    };
    await load();
    clearInterval(onlineTimer);
    onlineTimer = setInterval(() => { if (!body.isConnected) { clearInterval(onlineTimer); return; } load(); }, 10000);
  }

  // ---- bans
  let showHistory = false;
  function renderBans() {
    host.innerHTML = '';
    const card = h('div', { class: 'card' });
    host.append(card);
    card.append(h('div', { class: 'card-head' }, [
      cardTitle('i-shield', 'players.tab.bans'),
      h('button', { class: 'small danger', i18n: 'bans.add', onclick: () => addByID() }),
    ]));
    const active = bans.active || [];
    if (!active.length) {
      card.append(h('p', { class: 'hint', i18n: 'bans.empty' }));
    } else {
      // Phone: only the player and Unban stay; the rest goes under the name.
      const tbl = h('table', { class: 'bans-table' });
      tbl.append(h('thead', {}, h('tr', {}, [
        h('th', { i18n: 'bans.col.player' }), h('th', { class: 'pl-hide-sm', i18n: 'bans.col.reason' }), h('th', { class: 'pl-hide-sm', i18n: 'bans.col.since' }),
        h('th', { class: 'pl-hide-sm', i18n: 'bans.col.until' }), h('th', { class: 'pl-hide-sm', i18n: 'bans.col.by' }), h('th', { text: '' }),
      ])));
      const tb = h('tbody');
      for (const b of active) {
        const ident = b.name || b.steam64 || b.dayzId || b.guid || b.ip || '—';
        const who = b.key
          ? h('span', { class: 'player-link', text: ident, onclick: () => openPlayerProfile(b.key, reloadAll) })
          : h('span', { class: b.name ? '' : 'mono', text: ident });
        const untilText = !b.until ? t('profile.banDur.perm')
          : b.expired ? t('bans.expired') : tn('profile.banLeft', { left: leftText(b.until) });
        const byText = b.by || (b.source === 'panel' ? t('bans.byPanel') : '');
        tb.append(h('tr', {}, [
          h('td', { class: 'pl-name' }, [who,
            b.source !== 'panel' ? h('div', { class: 'hint', text: t('bans.external') + ' · ' + (b.source === 'dayz' ? 'ban.txt' : 'BattlEye') }) : null,
            h('div', { class: 'pl-sub hint', text: [b.reason, untilText, byText].filter(Boolean).join(' · ') }),
          ]),
          h('td', { class: 'pl-hide-sm', text: b.reason || '—' }),
          h('td', { class: 'pl-hide-sm', text: b.at ? fmtDateTime(b.at) : '—' }),
          h('td', { class: 'pl-hide-sm', title: b.until ? fmtDateTime(b.until) : null, text: untilText }),
          h('td', { class: 'pl-hide-sm', text: byText || '—' }),
          h('td', { class: 'row-actions' }, h('button', { class: 'small secondary', i18n: 'profile.unban', onclick: async () => {
            if (await unbanFlow(b.id, b.name || ident)) reloadAll();
          } })),
        ]));
      }
      tbl.append(tb);
      card.append(h('div', { class: 'table-scroll' }, tbl));
    }
    // History.
    const hist = bans.history || [];
    if (hist.length) {
      const histBody = h('div', { hidden: !showHistory });
      const toggle = h('button', { class: 'small btn-link', text: (showHistory ? '▾ ' : '▸ ') + t('bans.history') + ' (' + hist.length + ')',
        onclick: () => { showHistory = !showHistory; renderBans(); } });
      for (const b of hist) {
        const how = b.liftedBy === 'expired' ? t('bans.lifted.expired') : (b.liftedBy === 'replaced' ? t('bans.lifted.replaced') : t('bans.lifted.by').replace('{by}', b.liftedBy || '—'));
        histBody.append(h('div', { class: 'adm-row' }, [
          h('span', { class: 'adm-time', text: fmtDateTime(b.lifted) }),
          h('span', { class: 'adm-player', text: b.name || b.steam64 || b.dayzId || b.guid || '—' }),
          h('span', { class: 'adm-meta', text: [b.reason, how].filter(Boolean).join(' · ') }),
        ]));
      }
      card.append(h('div', { class: 'bans-history' }, [toggle, histBody]));
    }
    applyI18n();
  }

  async function addByID() {
    const m = openModal({ title: t('bans.add') });
    const inp = h('input', { type: 'text', spellcheck: 'false', placeholder: '76561198…', 'aria-label': t('bans.add.target') });
    const go = () => {
      const v = inp.value.trim();
      if (!v) return;
      m.close();
      openBanDialog({ target: v }, v).then(ok => { if (ok) reloadAll(); });
    };
    inp.addEventListener('keydown', (e) => { if (e.key === 'Enter') go(); });
    m.body.append(
      h('label', { i18n: 'bans.add.target' }), inp,
      h('p', { class: 'hint', i18n: 'bans.add.hint' }),
      h('div', { class: 'actions' }, [h('button', { class: 'danger', i18n: 'action.continue', onclick: go }), h('button', { i18n: 'action.cancel', onclick: () => m.close() })]),
    );
    applyI18n();
    setTimeout(() => inp.focus(), 0);
  }

  // ---- whitelist
  async function renderWhitelist() {
    host.innerHTML = '';
    const card = h('div', { class: 'card' });
    host.append(card);
    let r;
    try { r = await api.get('/api/whitelist'); } catch (e) { card.textContent = e.message; return; }
    if (!card.isConnected) return;
    const entries = (r.entries || []).filter(e => !e.example);
    // On but empty: from the next start nobody gets in — admins included.
    const emptyWarn = h('div', { class: 'warning-bar', i18n: 'wl.enable.emptyOn', hidden: !(r.enabled && !entries.length) });
    const sw = h('input', { type: 'checkbox', class: 'switch' });
    sw.checked = !!r.enabled;
    sw.onchange = async () => {
      // An empty list switched on would lock everyone out at the next start,
      // a scheduled night restart included: ask first.
      if (sw.checked && !entries.length &&
          !(await confirmModal(t('wl.enable.empty'), { danger: true, okText: t('wl.enable.anyway') }))) {
        sw.checked = false;
        return;
      }
      try {
        const res = await api.post('/api/whitelist/enable', { on: sw.checked });
        toast(t(res.restartNeeded ? 'wl.enable.restart' : 'msg.saved'), 'ok');
        r.enabled = sw.checked;
        emptyWarn.hidden = !(r.enabled && !entries.length);
      } catch (e) { sw.checked = !sw.checked; handleErr(e); }
    };
    card.append(
      h('div', { class: 'card-head' }, [
        cardTitle('i-shield', 'wl.tab'),
        h('button', { class: 'small primary', i18n: 'wl.add', onclick: () => addToWhitelist() }),
      ]),
      h('p', { class: 'hint', i18n: 'wl.hint' }),
      switchRow(sw, 'wl.enable'),
      emptyWarn,
    );
    // Removing works at once; while the list is enforced it also kicks the
    // player, so that asks first — otherwise an undo is enough.
    const removeEntry = async (e, btn) => {
      const enforced = r.enabled && r.running;
      if (enforced && !(await confirmModal(tn('wl.remove.confirm', { name: e.name || e.comment || e.id }), { danger: true, okText: t('wl.remove') }))) return;
      await withBusy(btn, async () => {
        try { await api.post('/api/whitelist/remove', { id: e.id }); }
        catch (err) { handleErr(err); return; }
        if (tab === 'whitelist') renderWhitelist();
        if (enforced) { toast(t('wl.removed'), 'ok'); return; }
        toastAction(t('wl.removed'), 'ok', t('wl.undo'), async () => {
          try { await api.post('/api/whitelist/add', { target: e.id }); toast(t('wl.added'), 'ok'); if (tab === 'whitelist') renderWhitelist(); }
          catch (err) { handleErr(err); }
        });
      });
    };
    if (!entries.length) {
      card.append(h('p', { class: 'hint', i18n: 'wl.empty' }));
    } else {
      // The 44-character ID is cut short (full in the tooltip, ⧉ copies it);
      // on a phone it moves under the name so Remove stays on screen.
      const shortId = (id) => id.length > 14 ? id.slice(0, 12) + '…' : id;
      const tbl = h('table');
      tbl.append(h('thead', {}, h('tr', {}, [h('th', { i18n: 'bans.col.player' }), h('th', { class: 'pl-hide-sm', i18n: 'col.playerId' }), h('th', { text: '' })])));
      const tb = h('tbody');
      for (const e of entries) {
        const who = e.key
          ? h('span', { class: 'player-link', text: e.name, onclick: () => openPlayerProfile(e.key, reloadAll) })
          : h('span', { class: 'hint', text: e.comment || '—' });
        tb.append(h('tr', {}, [
          h('td', { class: 'pl-name' }, [
            h('div', { class: 'player-cell' }, [who,
              e.banned ? h('span', { class: 'badge err', i18n: 'profile.banned', title: t('wl.banned.hint') }) : null]),
            h('div', { class: 'pl-sub hint mono', title: e.id, text: shortId(e.id) }),
          ]),
          h('td', { class: 'mono id-cell pl-hide-sm', title: e.id }, [
            h('span', { text: shortId(e.id) }),
            h('button', { type: 'button', class: 'icon-btn small-icon', title: t('action.copy'), 'aria-label': t('action.copy'), text: '⧉',
              onclick: () => copyText(e.id) }),
          ]),
          h('td', { class: 'row-actions' }, h('button', { class: 'small secondary', i18n: 'wl.remove',
            onclick: (ev) => removeEntry(e, ev.currentTarget) })),
        ]));
      }
      tbl.append(tb);
      card.append(h('div', { class: 'table-scroll' }, tbl));
    }
    applyI18n();
  }

  function addToWhitelist() {
    const m = openModal({ title: t('wl.add') });
    const inp = h('input', { type: 'text', spellcheck: 'false', list: 'wl-players', placeholder: t('wl.add.ph'), 'aria-label': t('wl.add.ph') });
    const dl = h('datalist', { id: 'wl-players' }, list.filter(p => p.id).slice(0, 400).map(p => h('option', { value: p.name })));
    const go = (btn) => withBusy(btn, async () => {
      const v = inp.value.trim();
      if (!v) return;
      const p = list.find(x => x.name === v && x.id);
      try {
        await api.post('/api/whitelist/add', p ? { key: p.key } : { target: v });
        toast(t('wl.added'), 'ok');
        m.close();
        if (tab === 'whitelist') renderWhitelist();
      } catch (e) { handleErr(e); }
    });
    const ok = h('button', { class: 'primary', i18n: 'wl.add', onclick: (e) => go(e.currentTarget) });
    inp.addEventListener('keydown', (e) => { if (e.key === 'Enter') go(ok); });
    m.body.append(h('label', { i18n: 'wl.add.ph' }), inp, dl, h('p', { class: 'hint', i18n: 'wl.add.hint' }),
      h('div', { class: 'actions' }, [ok, h('button', { i18n: 'action.cancel', onclick: () => m.close() })]));
    applyI18n();
    setTimeout(() => inp.focus(), 0);
  }

  // ---- kill feed
  let kfQuery = '', kfKind = 'all';
  function renderKillfeed() {
    host.innerHTML = '';
    const card = h('div', { class: 'card' }, [cardTitle('i-heatmap', 'players.killfeed')]);
    host.append(card);
    // Newest kill first by when it happened — the store keeps the order logs
    // were read in, which can put an older log's kills above newer ones.
    const all = (d.killfeed || []).slice().sort((a, b) => (Date.parse(b.at) || 0) - (Date.parse(a.at) || 0));
    if (!all.length) { card.append(h('p', { class: 'hint', i18n: 'admlog.noEvents' })); applyI18n(); return; }
    const keyOf = playerKeyLookup(list);
    const isPvp = (k) => (k.kind || (k.suicide ? 'suicide' : (k.killer ? 'pvp' : 'env'))) === 'pvp';
    const KINDS = [['all', 'kf.f.all', () => true], ['pvp', 'kf.f.pvp', isPvp], ['env', 'kf.f.env', k => !isPvp(k)]];
    const search = h('input', { type: 'search', value: kfQuery, class: 'players-search', placeholder: t('kf.search.ph'), 'aria-label': t('kf.search.ph') });
    const chips = h('div', { class: 'pills kf-kinds', role: 'radiogroup', 'aria-label': t('kf.f.label') });
    const rows = h('div', { class: 'adm-list' });
    const draw = () => {
      const q = kfQuery.toLowerCase();
      const byKind = (KINDS.find(x => x[0] === kfKind) || KINDS[0])[2];
      const items = all.filter(k => byKind(k) && (!q || [k.killer, k.victim, k.source, k.weapon]
        .some(v => v && String(v).toLowerCase().includes(q))));
      chips.querySelectorAll('.pill').forEach(c => {
        const on = c.dataset.kind === kfKind;
        c.classList.toggle('active', on);
        c.setAttribute('aria-checked', String(on));
      });
      rows.innerHTML = '';
      if (!items.length) rows.append(h('p', { class: 'hint', i18n: 'kf.none' }));
      else rows.append(...items.map(k => killRow(k, { keyOf, onChange: reloadAll })));
    };
    for (const [id, key, fn] of KINDS) {
      chips.append(h('button', { type: 'button', class: 'pill', role: 'radio', 'data-kind': id,
        onclick: () => { kfKind = id; draw(); } }, [h('span', { i18n: key }), h('span', { class: 'pill-count', text: String(all.filter(fn).length) })]));
    }
    let timer = 0;
    search.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(() => { kfQuery = search.value.trim(); draw(); }, 200); });
    card.append(h('div', { class: 'players-toolbar' }, [search, chips]), rows);
    draw();
    applyI18n();
  }

  function renderTab() {
    clearInterval(onlineTimer);
    ({ all: renderAll, online: renderOnline, bans: renderBans, whitelist: renderWhitelist, killfeed: renderKillfeed })[tab]();
  }

  renderTabs();
  renderTab();
  root._teardown = () => { clearInterval(onlineTimer); window.removeEventListener('resize', stripFades); };
};
