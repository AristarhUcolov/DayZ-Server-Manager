/*
  DayZ Server Manager — multi-server: the server switcher in the top bar, the
  Servers page, and the "All servers" card of the dashboard.
  Copyright (c) 2026 Aristarh Ucolov.
*/

// apiOn calls the API of a server other than the one this tab manages (Start
// on the Servers page, the dashboard's all-servers card). Errors keep the
// server's message without apiError's side effects.
async function apiOn(id, method, path, body) {
  const r = await fetch(path, {
    method, credentials: 'same-origin',
    headers: Object.assign({ 'X-DZM-Server': id }, body ? { 'Content-Type': 'application/json' } : {}),
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await r.text();
  if (!r.ok) {
    let msg = text;
    try { const d = JSON.parse(text); if (d && d.error) msg = State.i18n[d.error] ? t(d.error) : (d.message || d.error); } catch {}
    const err = new Error(msg || (r.status + ' ' + r.statusText));
    err.status = r.status;
    throw err;
  }
  try { return text ? JSON.parse(text) : {}; } catch { return {}; }
}

// loadServers refreshes Srv.list (and the switcher label) from the backend.
async function loadServers() {
  const d = await api.get('/api/servers');
  Srv.list = d.servers || [];
  updateServerSwitcher();
  return d;
}

// srvState is a server row's status: an i18n key and a colour class.
function srvState(s) {
  if (s.error) return { key: 'servers.state.error', cls: 'err' };
  if (s.stopping) return { key: 'status.stopping', cls: 'warn' };
  if (s.running && s.loading) return { key: 'status.loading', cls: 'warn' };
  if (s.running) return { key: 'status.running', cls: 'ok' };
  return { key: 'status.stopped', cls: 'mute' };
}

function srvPlayersText(s) {
  if (!s.running) return '';
  if (s.players == null) return t('servers.players.unknown');
  return t('servers.players.n').replace('{n}', s.players).replace('{max}', s.maxPlayers || '—');
}

// ------------------------------------------------------------------ switcher

function updateServerSwitcher() {
  const btn = document.getElementById('srv-switch');
  if (!btn) return;
  const cur = Srv.current();
  const name = cur ? cur.name : (State.info && State.info.serverName) || '';
  document.getElementById('srv-switch-name').textContent = name;
  btn.hidden = !name;
  btn.classList.toggle('multi', Srv.list.length > 1);
  const st = cur ? srvState(cur) : null;
  const dot = btn.querySelector('.srv-dot');
  if (dot) dot.className = 'srv-dot' + (st ? ' ' + st.cls : '');
  // The window title says which server this tab manages.
  document.title = (Srv.list.length > 1 && name ? name + ' — ' : '') + 'DayZ Server Manager';
}

async function setupServerSwitcher() {
  const btn = document.getElementById('srv-switch');
  if (!btn) return;
  try { await loadServers(); } catch { return; }
  // The tab asked for a server that is not (or no longer) in the panel.
  if (!Srv.current() && Srv.id !== 'main') { Srv.remember('main'); Srv.switchTo('main'); return; }
  Srv.remember(Srv.id);
  btn.onclick = (e) => { e.stopPropagation(); toggleServerMenu(btn); };
}

let _srvMenu = null;
function closeServerMenu() {
  if (!_srvMenu) return;
  _srvMenu.remove();
  _srvMenu = null;
  const btn = document.getElementById('srv-switch');
  if (btn) btn.setAttribute('aria-expanded', 'false');
}

async function toggleServerMenu(btn) {
  if (_srvMenu) { closeServerMenu(); return; }
  const menu = h('div', { class: 'srv-menu', role: 'menu' });
  _srvMenu = menu;
  btn.setAttribute('aria-expanded', 'true');
  const place = () => {
    const r = btn.getBoundingClientRect();
    menu.style.top = (r.bottom + 6) + 'px';
    const left = Math.min(r.left, window.innerWidth - menu.offsetWidth - 10);
    menu.style.left = Math.max(10, left) + 'px';
  };
  const render = () => {
    menu.innerHTML = '';
    menu.append(h('div', { class: 'srv-menu-head', i18n: 'servers.switch' }));
    for (const s of Srv.list) {
      const st = srvState(s);
      const sub = [t(st.key), srvPlayersText(s)].filter(Boolean).join(' · ');
      menu.append(h('button', {
        class: 'srv-menu-item' + (s.id === Srv.id ? ' current' : ''), role: 'menuitemradio',
        'aria-checked': String(s.id === Srv.id), disabled: !!s.error && s.id !== Srv.id,
        onclick: () => { closeServerMenu(); if (s.id !== Srv.id) Srv.switchTo(s.id); },
      }, [
        h('span', { class: 'srv-dot ' + st.cls }),
        h('span', { class: 'srv-menu-text' }, [
          h('span', { class: 'srv-menu-name', text: s.name }),
          h('span', { class: 'srv-menu-sub', text: sub }),
        ]),
        s.id === Srv.id ? h('span', { class: 'srv-menu-check', text: '✓' }) : null,
      ]));
    }
    menu.append(h('div', { class: 'srv-menu-sep' }),
      h('button', { class: 'srv-menu-item srv-menu-link', role: 'menuitem',
        onclick: () => { closeServerMenu(); navigate('servers'); } }, [
        h('span', { class: 'srv-menu-icon', html: svgIcon('i-servers', 15) }),
        h('span', { i18n: 'servers.manage' }),
      ]));
    applyI18n();
  };
  render();
  document.body.append(menu);
  place();
  // Fresh states (another server may have started meanwhile).
  try { await loadServers(); if (_srvMenu === menu) { render(); place(); } } catch {}
}

document.addEventListener('click', (e) => {
  if (_srvMenu && !_srvMenu.contains(e.target)) closeServerMenu();
});
document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeServerMenu(); });
window.addEventListener('resize', closeServerMenu);

// ------------------------------------------------------------- server actions

async function srvAction(s, action, btn) {
  const labels = { start: 'servers.started', stop: 'servers.stopped', restart: 'servers.restarted' };
  if (action === 'stop' && !(await confirmModal(t('servers.confirmStop').replace('{name}', s.name), { danger: true, okText: t('action.stop') }))) return false;
  if (action === 'restart' && !(await confirmModal(t('servers.confirmRestart').replace('{name}', s.name), { okText: t('action.restart') }))) return false;
  let ok = false;
  await withBusy(btn, async () => {
    try {
      await apiOn(s.id, 'POST', '/api/server/' + action, {});
      toast(t(labels[action]).replace('{name}', s.name), 'ok');
      ok = true;
    } catch (e) { handleErr(e); }
  });
  return ok;
}

// ------------------------------------------------------------ dashboard card

// allServersCard is the dashboard's overview of every server when the panel
// runs more than one: state, players and Start/Stop at a glance.
function allServersCard() {
  if (Srv.list.length < 2) return null;
  const card = h('div', { class: 'card all-servers' });
  const render = () => {
    card.innerHTML = '';
    card.append(h('div', { class: 'card-head' }, [
      cardTitle('i-servers', 'servers.all'),
      h('a', { class: 'btn-link small', href: '#servers', i18n: 'servers.manage' }),
    ]));
    const list = h('div', { class: 'all-servers-list' });
    for (const s of Srv.list) {
      const st = srvState(s);
      // The server this tab manages has its controls in the card above.
      const cur = s.id === Srv.id;
      const row = h('div', { class: 'as-row' + (cur ? ' current' : '') }, [
        h('span', { class: 'srv-dot ' + st.cls }),
        h('div', { class: 'as-main' }, [
          cur
            ? h('span', { class: 'as-name', text: s.name })
            : h('a', { class: 'as-name', href: '#', text: s.name, title: t('servers.open.title'), onclick: (e) => { e.preventDefault(); Srv.switchTo(s.id); } }),
          h('span', { class: 'as-sub', text: [t(st.key), srvPlayersText(s), s.running && s.uptimeSec ? fmtAge(s.uptimeSec) : ''].filter(Boolean).join(' · ') }),
        ]),
        cur ? h('span', { class: 'badge info', i18n: 'servers.here' }) : (s.error ? null : (s.running
          ? h('button', { class: 'small secondary', i18n: 'action.stop', onclick: async (e) => { if (await srvAction(s, 'stop', e.currentTarget)) refresh(); } })
          : h('button', { class: 'small secondary', i18n: 'action.start', onclick: async (e) => { if (await srvAction(s, 'start', e.currentTarget)) refresh(); } }))),
      ]);
      list.append(row);
    }
    card.append(list);
    applyI18n();
  };
  const refresh = async () => { try { await loadServers(); } catch {} if (card.isConnected) render(); };
  render();
  refresh(); // the list was loaded at start-up — show the states of now
  return { card, refresh };
}

// ------------------------------------------------------------------ the page

Views.servers = async (root) => {
  const myNav = _navSeq;
  let d;
  try { d = await loadServers(); } catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;

  root.append(pageHeader('nav.servers', 'servers.subtitle', [
    h('button', { class: 'primary', onclick: () => addCard.scrollIntoView({ behavior: 'smooth', block: 'start' }) }, [
      h('span', { text: '+ ' }), h('span', { i18n: 'servers.add' }),
    ]),
  ]));
  root.append(h('div', { class: 'info-bar servers-intro', i18n: 'servers.intro' }));

  const grid = h('div', { class: 'servers-grid' });
  root.append(grid);

  function portsLine(s) {
    const p = s.ports || {};
    return t('servers.ports.line').replace('{game}', p.game || '—').replace('{query}', p.query || '—').replace('{rcon}', p.rcon || '—');
  }

  function serverCard(s, i) {
    const st = srvState(s);
    const isCur = s.id === Srv.id;
    const head = h('div', { class: 'srv-card-head' }, [
      h('span', { class: 'srv-dot ' + st.cls }),
      h('h3', { class: 'srv-card-name', text: s.name }),
      s.primary ? h('span', { class: 'badge info', i18n: 'servers.primary' }) : null,
      isCur ? h('span', { class: 'badge ok', i18n: 'servers.current' }) : null,
      h('span', { class: 'grow' }),
      h('span', { class: 'badge ' + st.cls, i18n: st.key }),
    ]);
    const kv = h('div', { class: 'kv srv-kv' });
    const row = (k, v, cls) => { if (v) kv.append(h('span', { class: 'k', i18n: k }), h('span', { class: cls || '', text: v })); };
    row('servers.hostname', s.hostname);
    row('servers.mission', s.mission, 'mono');
    if (s.dir) {
      // The last two folders are what tells servers apart; the full path is
      // on hover and one click away.
      const parts = s.dir.split(/[\\/]+/).filter(Boolean);
      const short = parts.length > 2 ? '…' + (s.dir.includes('\\') ? '\\' : '/') + parts.slice(-2).join(s.dir.includes('\\') ? '\\' : '/') : s.dir;
      kv.append(h('span', { class: 'k', i18n: 'servers.folder' }), h('span', { class: 'srv-dir-line' }, [
        h('span', { class: 'mono srv-dir', text: short, title: s.dir }),
        revealServerDir(s),
      ]));
    }
    if (!s.error) row('servers.ports', portsLine(s));
    if (s.running) {
      row('servers.playersLabel', srvPlayersText(s));
      if (s.uptimeSec) row('servers.uptime', fmtAge(s.uptimeSec));
    }
    const notes = [];
    if (s.error) notes.push(h('div', { class: 'warning-bar err-bar', text: s.error }));
    if (s.clashes && s.clashes.length) {
      notes.push(h('div', { class: 'warning-bar srv-clash' }, [
        h('div', {}, [h('b', { i18n: 'servers.clash' }), ' ', h('span', { text: s.clashes.join('; ') })]),
        !s.running && !s.primary ? h('button', { class: 'small', i18n: 'servers.freePorts', onclick: (e) => withBusy(e.currentTarget, async () => {
          try {
            const r = await api.post('/api/servers/free-ports', { id: s.id });
            const p = r.ports || {};
            toast(t('servers.freePorts.done').replace('{game}', p.game).replace('{query}', p.query).replace('{rcon}', p.rcon), 'ok');
            await redraw();
          } catch (err) { handleErr(err); }
        }) }) : null,
      ]));
    }
    if (s.loopPaused) notes.push(h('div', { class: 'warning-bar', i18n: 'servers.loopPaused' }));

    const actions = h('div', { class: 'actions srv-actions' });
    if (s.error) {
      actions.append(h('button', { class: 'primary', i18n: 'servers.retry', onclick: (e) => withBusy(e.currentTarget, async () => {
        try { await api.post('/api/servers/retry', { id: s.id }); toast(t('servers.retried'), 'ok'); await redraw(); }
        catch (err) { handleErr(err); }
      }) }));
    } else {
      if (!isCur) actions.append(h('button', { class: 'primary', i18n: 'servers.open', onclick: () => Srv.switchTo(s.id) }));
      if (s.running) {
        actions.append(
          h('button', { class: 'secondary', i18n: 'action.restart', onclick: async (e) => { if (await srvAction(s, 'restart', e.currentTarget)) redraw(); } }),
          h('button', { class: 'secondary', i18n: 'action.stop', onclick: async (e) => { if (await srvAction(s, 'stop', e.currentTarget)) redraw(); } }),
        );
      } else {
        actions.append(h('button', { class: isCur ? 'primary' : 'secondary', i18n: 'action.start',
          onclick: async (e) => { if (await srvAction(s, 'start', e.currentTarget)) redraw(); } }));
      }
    }
    actions.append(h('span', { class: 'grow' }));
    actions.append(h('button', { class: 'icon-btn', title: t('servers.rename'), 'aria-label': t('servers.rename'), text: '✎',
      onclick: async () => {
        const v = await promptModal(t('servers.renamePrompt'), { value: s.name, okText: t('action.save') });
        if (!v) return;
        try { await api.post('/api/servers/rename', { id: s.id, name: v }); await redraw(); } catch (e) { handleErr(e); }
      } }));
    if (!s.primary) {
      if (i > 1) actions.append(h('button', { class: 'icon-btn', title: t('servers.moveUp'), 'aria-label': t('servers.moveUp'), text: '↑',
        onclick: async () => { try { await api.post('/api/servers/move', { id: s.id, delta: -1 }); await redraw(); } catch (e) { handleErr(e); } } }));
      if (i < Srv.list.length - 1) actions.append(h('button', { class: 'icon-btn', title: t('servers.moveDown'), 'aria-label': t('servers.moveDown'), text: '↓',
        onclick: async () => { try { await api.post('/api/servers/move', { id: s.id, delta: 1 }); await redraw(); } catch (e) { handleErr(e); } } }));
      actions.append(h('button', { class: 'icon-btn danger-text', title: t('servers.remove'), 'aria-label': t('servers.remove'), text: '×',
        disabled: !!s.running,
        onclick: async () => {
          if (!(await confirmModal(t('servers.removeConfirm').replace('{name}', s.name), { danger: true, okText: t('servers.remove') }))) return;
          try {
            await api.post('/api/servers/remove', { id: s.id });
            toast(t('servers.removed').replace('{name}', s.name), 'ok');
            if (s.id === Srv.id) { Srv.switchTo('main'); return; }
            await redraw();
          } catch (e) { handleErr(e); }
        } }));
    }
    return h('div', { class: 'card srv-card' + (isCur ? ' current' : '') + (s.error ? ' broken' : '') }, [head, kv, ...notes, actions]);
  }

  function renderGrid() {
    grid.innerHTML = '';
    Srv.list.forEach((s, i) => grid.append(serverCard(s, i)));
    applyI18n();
  }

  // ---- add a server
  const addCard = h('div', { class: 'card srv-add', id: 'servers-add' });
  root.append(addCard);
  let mode = 'existing';
  const modeBar = h('div', { class: 'section-tabs srv-add-tabs', role: 'tablist' });
  const body = h('div');

  function renderModeBar() {
    modeBar.innerHTML = '';
    for (const [id, key] of [['existing', 'servers.add.existing'], ['copy', 'servers.add.copy']]) {
      modeBar.append(h('button', { type: 'button', role: 'tab', class: 'section-tab' + (mode === id ? ' active' : ''),
        'aria-selected': String(mode === id), i18n: key, onclick: () => { mode = id; renderModeBar(); renderBody(); } }));
    }
  }

  function inspectLine(host, dirInp, opts) {
    let timer = 0;
    const check = async () => {
      const v = dirInp.value.trim();
      host.innerHTML = '';
      host.className = 'srv-inspect';
      if (!v) return;
      let r;
      try { r = await api.post('/api/servers/inspect', { dir: v }); } catch (e) { host.textContent = e.message; return; }
      if (dirInp.value.trim() !== v) return;
      // Element.append(null) would print "null" (the ".null" after the hint).
      const say = (cls, key, extra) => { host.classList.add(cls); host.append(h('span', { i18n: key }), ...(extra ? [h('span', { text: ' ' + extra })] : [])); applyI18n(); };
      if (r.error) return say('bad', 'servers.inspect.problem', r.error);
      if (opts.forCopy) {
        if (!r.exists) return say('ok', 'servers.inspect.newFolder');
        if (r.empty) return say('ok', 'servers.inspect.emptyFolder');
        return say('bad', 'servers.inspect.notEmpty');
      }
      if (!r.exists) { opts.valid && opts.valid(false); return say('bad', 'servers.inspect.missing'); }
      if (!r.dayz) { opts.valid && opts.valid(false); return say('bad', 'servers.inspect.noServer'); }
      opts.valid && opts.valid(true);
      if (r.managedBefore) return say('ok', 'servers.inspect.managedBefore');
      return say('ok', 'servers.inspect.ok');
    };
    dirInp.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(check, 450); });
    return check;
  }

  function renderBody() {
    body.innerHTML = '';
    if (mode === 'existing') {
      const dirInp = h('input', { type: 'text', placeholder: t('servers.folder.ph'), spellcheck: 'false', 'aria-label': t('servers.folder') });
      const nameInp = h('input', { type: 'text', maxlength: '40', placeholder: t('servers.name.ph'), 'aria-label': t('servers.name') });
      const status = h('div', { class: 'srv-inspect' });
      let addBtn = null;
      const check = inspectLine(status, dirInp, { valid: (ok) => { if (addBtn) addBtn.disabled = !ok; } });
      dirInp.addEventListener('input', () => { if (addBtn) addBtn.disabled = true; });
      body.append(
        h('p', { class: 'hint', i18n: 'servers.add.existing.hint' }),
        h('div', { class: 'grid-2' }, [field('servers.folder', dirInp), field('servers.name', nameInp)]),
        status,
        h('div', { class: 'actions' }, [
          addBtn = h('button', { class: 'primary', i18n: 'servers.add.button', disabled: true, onclick: (e) => withBusy(e.currentTarget, async () => {
            try {
              const r = await api.post('/api/servers/add', { dir: dirInp.value.trim(), name: nameInp.value.trim() });
              toast(t('servers.added'), 'ok');
              dirInp.value = ''; nameInp.value = ''; status.innerHTML = '';
              await redraw();
              if (r.id && await confirmModal(t('servers.openNew'), { okText: t('servers.open') })) Srv.switchTo(r.id);
            } catch (err) { handleErr(err); check(); }
          }) }),
        ]),
      );
    } else {
      const srcSel = h('select', { 'aria-label': t('servers.copy.from') }, Srv.list.filter(s => !s.error).map(s => h('option', { value: s.id, text: s.name })));
      srcSel.value = Srv.current() && !Srv.current().error ? Srv.id : 'main';
      const dirInp = h('input', { type: 'text', value: d.suggestDir || '', spellcheck: 'false', 'aria-label': t('servers.copy.to') });
      const nameInp = h('input', { type: 'text', maxlength: '40', placeholder: t('servers.name.ph'), 'aria-label': t('servers.name') });
      const modsCb = h('input', { type: 'checkbox', class: 'switch' });
      modsCb.checked = true;
      const status = h('div', { class: 'srv-inspect' });
      const check = inspectLine(status, dirInp, { forCopy: true });
      if (dirInp.value) check();
      const fp = d.freePorts || {};
      body.append(
        h('p', { class: 'hint', i18n: 'servers.add.copy.hint' }),
        h('div', { class: 'grid-3' }, [field('servers.copy.from', srcSel), field('servers.copy.to', dirInp), field('servers.name', nameInp)]),
        status,
        switchRow(modsCb, 'servers.copy.mods'),
        h('p', { class: 'hint', text: t('servers.copy.ports').replace('{game}', fp.game || '—').replace('{query}', fp.query || '—').replace('{rcon}', fp.rcon || '—') }),
        h('div', { class: 'actions' }, [
          h('button', { class: 'primary', i18n: 'servers.copy.button', onclick: (e) => withBusy(e.currentTarget, async () => {
            try {
              await api.post('/api/servers/create', { source: srcSel.value, dir: dirInp.value.trim(), name: nameInp.value.trim(), mods: modsCb.checked });
              watchJob();
            } catch (err) { handleErr(err); check(); }
          }) }),
        ]),
        jobHost,
      );
    }
    applyI18n();
  }

  // ---- copy progress
  const jobHost = h('div', { class: 'srv-job' });
  let jobTimer = 0;
  function renderJob(j) {
    jobHost.innerHTML = '';
    if (!j || !j.state) return;
    const pct = j.total > 0 ? Math.min(100, Math.round(j.copied * 100 / j.total)) : 0;
    if (j.state === 'copying') {
      jobHost.append(
        h('div', { class: 'srv-job-line', text: t('servers.copy.progress').replace('{name}', j.name).replace('{done}', bytes(j.copied)).replace('{total}', bytes(j.total)).replace('{pct}', pct) }),
        h('div', { class: 'progress' }, h('div', { class: 'progress-bar', style: { width: pct + '%' } })),
        h('div', { class: 'hint mono srv-job-file', text: j.current || '' }),
      );
    } else if (j.state === 'done') {
      jobHost.append(h('div', { class: 'info-bar' }, [
        h('span', { text: t('servers.copy.done').replace('{name}', j.name) + ' ' }),
        j.id ? h('button', { class: 'small primary', i18n: 'servers.open', onclick: () => Srv.switchTo(j.id) }) : null,
      ]));
    } else if (j.state === 'error') {
      jobHost.append(h('div', { class: 'warning-bar err-bar', text: t('servers.copy.failed') + ' ' + (j.error || '') }));
    }
    applyI18n();
  }
  function watchJob() {
    clearInterval(jobTimer);
    const poll = async () => {
      if (!jobHost.isConnected) { clearInterval(jobTimer); return; }
      let r;
      try { r = await api.get('/api/servers/create/status'); } catch { return; }
      renderJob(r.job);
      if (!r.job || r.job.state !== 'copying') {
        clearInterval(jobTimer);
        if (r.job && r.job.state === 'done') await redraw();
      }
    };
    poll();
    jobTimer = setInterval(poll, 1000);
  }

  addCard.append(cardTitle('i-servers', 'servers.add'), modeBar, body);
  renderModeBar();

  async function redraw() {
    try { d = await loadServers(); } catch (e) { handleErr(e); return; }
    if (!grid.isConnected) return;
    renderGrid();
  }

  renderGrid();
  renderBody();
  if (d.job && d.job.state) {
    if (d.job.state === 'copying') { mode = 'copy'; renderModeBar(); renderBody(); watchJob(); }
    else renderJob(d.job);
  }
  if (location.hash === '#servers/add') addCard.scrollIntoView({ block: 'start' });

  // Live states while the page is open.
  const poll = setInterval(async () => {
    if (!grid.isConnected) return;
    // Never redraw under an open form field being typed in.
    if (grid.contains(document.activeElement)) return;
    try { await loadServers(); renderGrid(); } catch {}
  }, 5000);
  root._teardown = () => { clearInterval(poll); clearInterval(jobTimer); };
};

// #servers/add opens the page at the add form.
Views['servers/add'] = (root) => Views.servers(root);

// revealServerDir opens a server's folder in the file manager of the computer
// the manager runs on — asked of that server's own API (the card may belong to
// another server than the one this tab manages); a remote viewer gets the
// path to copy instead.
function revealServerDir(s) {
  const btn = h('button', { class: 'secondary btn-reveal icon-only', type: 'button',
    title: t('reveal.title'), 'aria-label': t('reveal.title'), html: svgIcon('i-folderopen', 15),
    onclick: async (e) => {
      e.stopPropagation();
      try {
        const resp = await fetch('/api/fs/reveal', { method: 'POST', credentials: 'same-origin',
          headers: { 'Content-Type': 'application/json', 'X-DZM-Server': s.id }, body: JSON.stringify({ kind: 'server' }) });
        if (!resp.ok) throw await apiError(resp);
        const r = await resp.json();
        if (!r.opened) showPathModal(r.path || s.dir, r.reason);
      } catch (err) { handleErr(err); }
    } });
  return btn;
}
