/*
  DayZ Server Manager — web UI.
  Copyright (c) 2026 Aristarh Ucolov.
*/

// --------------------------------------------------------------------- state

const State = {
  info: null,
  config: null,
  lang: 'en',
  i18n: {},
  serverStatus: { running: false },
};

// --------------------------------------------------------------------- api

const api = {
  async get(path) {
    const r = await fetch(path, { credentials: 'same-origin' });
    if (r.status === 401) { showLogin(); throw new Error('unauthorized'); }
    if (!r.ok) throw new Error(await r.text());
    return r.json();
  },
  async send(path, method, body) {
    const r = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers: body ? { 'Content-Type': 'application/json' } : {},
      body: body ? JSON.stringify(body) : undefined,
    });
    if (r.status === 401) { showLogin(); throw new Error('unauthorized'); }
    if (!r.ok) throw new Error(await r.text());
    const text = await r.text();
    try { return text ? JSON.parse(text) : {}; } catch { return {}; }
  },
  post(p, b)   { return this.send(p, 'POST',   b); },
  put(p, b)    { return this.send(p, 'PUT',    b); },
  del(p, b)    { return this.send(p, 'DELETE', b); },
};

// --------------------------------------------------------------------- i18n

async function loadI18n(lang) {
  const data = await api.get(`/api/i18n?lang=${encodeURIComponent(lang)}`);
  State.lang = data.locale;
  State.i18n = data.messages || {};
  document.documentElement.lang = State.lang;
  applyI18n();
}

function t(key) { return State.i18n[key] || key; }

function applyI18n() {
  document.querySelectorAll('[data-i18n]').forEach(el => {
    el.textContent = t(el.dataset.i18n);
  });
}

// --------------------------------------------------------------------- toast

function toast(msg, kind = '') {
  const el = document.getElementById('toast');
  el.textContent = msg;
  el.className = `toast visible ${kind}`;
  clearTimeout(toast._t);
  toast._t = setTimeout(() => { el.className = 'toast'; }, 3500);
}

function handleErr(err) {
  console.error(err);
  toast(String(err.message || err), 'error');
}

// --------------------------------------------------------------------- router

const Views = {};

function setActiveRoute(name) {
  document.querySelectorAll('.nav a').forEach(a => {
    a.classList.toggle('active', a.dataset.route === name);
  });
}

async function navigate(name) {
  setActiveRoute(name);
  const view = Views[name] || Views.dashboard;
  const root = document.getElementById('view');
  if (root._teardown) { try { root._teardown(); } catch {} root._teardown = null; }
  root.innerHTML = '';
  try {
    await view(root);
    applyI18n();
  } catch (err) {
    if (String(err.message) === 'unauthorized') return;
    handleErr(err);
    root.innerHTML = `<div class="card"><p>${err.message || err}</p></div>`;
  }
}

document.addEventListener('click', e => {
  const a = e.target.closest('.nav a');
  if (a && a.dataset.route) { e.preventDefault(); navigate(a.dataset.route); }
});

// --------------------------------------------------------------------- helpers

const h = (tag, attrs = {}, children = []) => {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'class') el.className = v;
    else if (k === 'text') el.textContent = v;
    else if (k === 'html') el.innerHTML = v;
    else if (k === 'i18n') { el.dataset.i18n = v; el.textContent = t(v); }
    else if (k.startsWith('on')) el.addEventListener(k.slice(2).toLowerCase(), v);
    else if (k === 'style' && typeof v === 'object') Object.assign(el.style, v);
    else if (v === true) el.setAttribute(k, '');
    else if (v === false || v == null) {} // skip
    else el.setAttribute(k, v);
  }
  for (const c of [].concat(children)) {
    if (c == null || c === false) continue;
    el.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
  }
  return el;
};

function bytes(n) {
  if (n == null) return '';
  const u = ['B','KB','MB','GB','TB'];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return `${n.toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
}

function runningBanner() {
  return h('div', { class: 'warning-bar', i18n: 'guard.serverRunning' });
}

// --------------------------------------------------------------------- status

async function refreshStatus() {
  try {
    const s = await api.get('/api/server/status');
    State.serverStatus = s;
    const chip = document.getElementById('server-indicator');
    const txt = document.getElementById('server-indicator-text');
    chip.classList.toggle('running', s.running);
    chip.classList.toggle('stopped', !s.running);
    txt.dataset.i18n = s.running ? 'status.running' : 'status.stopped';
    txt.textContent = t(txt.dataset.i18n);
  } catch (err) { /* ignore */ }
}

// --------------------------------------------------------------------- first-run

async function ensureFirstRunDone() {
  if (State.config.firstRunDone) return;
  const modal = document.getElementById('first-run');
  modal.classList.remove('hidden');
  document.getElementById('fr-lang').value = State.config.language || 'en';
  document.getElementById('fr-vanilla').value = State.config.vanillaDayZPath || '';
  document.getElementById('fr-finish').onclick = async () => {
    const pass = document.getElementById('fr-admin-pass').value;
    const exposure = document.querySelector('input[name=fr-exposure]:checked').value;
    if (exposure === 'internet' && !pass) {
      toast(t('firstRun.adminPassword.hint'), 'error');
      return;
    }
    const body = {
      language: document.getElementById('fr-lang').value,
      vanillaDayZPath: document.getElementById('fr-vanilla').value.trim(),
      exposure,
      adminUsername: document.getElementById('fr-admin-user').value.trim() || 'admin',
      adminPassword: pass,
    };
    try {
      State.config = await api.post('/api/config/finish-first-run', body);
      await loadI18n(State.config.language);
      document.getElementById('lang-switch').value = State.config.language;
      modal.classList.add('hidden');
      // If a password was set, an explicit login is now required (no
      // auto-session is granted from the wizard).
      if (State.config.requireAuth) { showLogin(); return; }
      await navigate('dashboard');
    } catch (err) { handleErr(err); }
  };
  document.getElementById('fr-lang').addEventListener('change', async e => {
    await loadI18n(e.target.value);
  });
}

// --------------------------------------------------------------------- login

function showLogin() {
  const modal = document.getElementById('login');
  modal.classList.remove('hidden');
  const err = document.getElementById('login-error');
  err.textContent = '';
  document.getElementById('login-submit').onclick = async () => {
    err.textContent = '';
    try {
      await api.post('/api/auth/login', {
        username: document.getElementById('login-user').value.trim(),
        password: document.getElementById('login-pass').value,
      });
      modal.classList.add('hidden');
      document.getElementById('login-pass').value = '';
      await main();
    } catch (e) {
      err.textContent = t('login.invalid');
    }
  };
  const onKey = (e) => { if (e.key === 'Enter') document.getElementById('login-submit').click(); };
  document.getElementById('login-user').onkeydown = onKey;
  document.getElementById('login-pass').onkeydown = onKey;
}

async function logout() {
  try { await api.post('/api/auth/logout'); } catch (e) {}
  showLogin();
}

// --------------------------------------------------------------------- dashboard

Views.dashboard = async (root) => {
  await refreshStatus();
  const s = State.serverStatus;

  // Silent probe — if a newer manager version is out, show a non-blocking
  // banner. Failures are swallowed; this should never block the dashboard.
  (async () => {
    try {
      const u = await api.get('/api/update/check');
      if (u.updateAvailable && u.latest) {
        const banner = h('div', { class: 'warning-bar' }, [
          h('span', { text: `Update available: ${u.current} → ${u.latest}. ` }),
          u.releaseUrl ? h('a', { href: u.releaseUrl, target: '_blank', rel: 'noopener', text: 'Open release' }) : null,
        ]);
        root.prepend(banner);
      }
    } catch {}
  })();

  root.append(
    h('div', { class: 'grid-3' }, [
      h('div', { class: 'card' }, [
        h('h3', { i18n: 'nav.server' }),
        h('div', { class: 'kv' }, [
          h('div', { class: 'k', i18n: 'status.running' }),
          h('div', { text: s.running ? 'YES' : 'NO' }),
          h('div', { class: 'k', i18n: 'status.pid' }),
          h('div', { text: s.pid || '—' }),
          h('div', { class: 'k', i18n: 'status.uptime' }),
          h('div', { text: s.uptime || '—' }),
          h('div', { class: 'k', i18n: 'status.port' }),
          h('div', { text: s.port }),
        ]),
        h('div', { class: 'actions' }, [
          s.running
            ? h('button', { class: 'danger', i18n: 'action.stop',
                onclick: async () => { try { await api.post('/api/server/stop'); await navigate('dashboard'); } catch(e){handleErr(e);} }})
            : h('button', { class: 'primary', i18n: 'action.start',
                onclick: async () => { try { await api.post('/api/server/start'); await navigate('dashboard'); } catch(e){handleErr(e);} }}),
          h('button', { i18n: 'action.restart',
            onclick: async () => { try { await api.post('/api/server/restart'); await navigate('dashboard'); } catch(e){handleErr(e);} }}),
        ]),
      ]),
      h('div', { class: 'card' }, [
        h('h3', { i18n: 'nav.mods' }),
        h('p', { text: `${(State.config.mods || []).length} active` }),
        h('button', { i18n: 'nav.mods', onclick: () => navigate('mods') }),
      ]),
      h('div', { class: 'card' }, [
        h('h3', { i18n: 'nav.validator' }),
        h('button', { i18n: 'action.validate', onclick: () => navigate('validator') }),
      ]),
    ])
  );
};

// --------------------------------------------------------------------- server.cfg

Views.server = async (root) => {
  const data = await api.get('/api/servercfg');
  const form = h('div', { class: 'grid-2' });
  const bag = {};
  const KEYS = [
    'hostname','password','passwordAdmin','description','maxPlayers','serverTime',
    'serverTimeAcceleration','serverNightTimeAcceleration','serverTimePersistent',
    'enableWhitelist','disableVoN','vonCodecQuality','disable3rdPerson','disableCrosshair',
    'disablePersonalLight','lightingConfig','verifySignatures','forceSameBuild',
    'shardId','instanceId','storageAutoFix','loginQueueConcurrentPlayers','loginQueueMaxPlayers',
  ];
  for (const k of KEYS) {
    const val = data.values[k] ?? '';
    const input = h('input', { type: 'text', value: val });
    bag[k] = input;
    form.append(h('div', {}, [h('label', { text: k }), input]));
  }

  const missionSelect = h('select', { id: 'mission-input' });
  let missions = { missions: [], active: data.mission || '' };
  try { missions = await api.get('/api/missions'); } catch {}
  for (const m of missions.missions) {
    missionSelect.append(h('option', { value: m.name, text: m.name }));
  }
  missionSelect.value = data.mission || missions.active || '';

  root.append(
    State.serverStatus.running ? runningBanner() : null,
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'settings.title' }),
      h('div', { class: 'kv' }, [
        h('div', { class: 'k', text: 'mission' }),
        h('div', {}, [
          missionSelect,
          h('button', { style: { marginLeft: '8px' }, text: 'Change',
            onclick: async () => {
              try {
                const v = missionSelect.value.trim();
                await api.post('/api/servercfg/mission', { template: v });
                toast('mission changed', 'ok');
              } catch (e) { handleErr(e); }
            }
          }),
          h('button', { style: { marginLeft: '8px' }, i18n: 'mission.duplicate',
            onclick: async () => {
              const src = missionSelect.value.trim();
              if (!src) return;
              const tgt = prompt(t('mission.duplicate.prompt'), `${src}.copy`);
              if (!tgt) return;
              try {
                await api.post('/api/missions/duplicate', { source: src, target: tgt.trim() });
                toast('duplicated', 'ok');
                await navigate('server');
              } catch (e) { handleErr(e); }
            }
          }),
        ]),
      ]),
      form,
      h('div', { class: 'actions' }, [
        h('button', { class: 'primary', i18n: 'action.save',
          onclick: async () => {
            const patch = {};
            for (const k of KEYS) {
              const v = bag[k].value.trim();
              patch[k] = isNaN(Number(v)) || v === '' ? v : Number(v);
            }
            try {
              await api.post('/api/servercfg', patch);
              toast('saved', 'ok');
            } catch (e) { handleErr(e); }
          }
        }),
      ]),
    ])
  );
};

// --------------------------------------------------------------------- mods

Views.mods = async (root) => {
  const d = await api.get('/api/mods');
  const wrap = h('div');

  if (State.serverStatus.running) wrap.append(runningBanner());
  if (!d.vanillaPath) wrap.append(h('div', { class: 'warning-bar', text: 'Vanilla DayZ path is not set — Settings.' }));

  const outdatedCount = d.mods.filter(m => m.updateAvailable).length;

  const tbl = h('table');
  tbl.append(
    h('thead', {}, h('tr', {}, [
      h('th', { text: 'Mod' }),
      h('th', { text: 'Status' }),
      h('th', { text: 'Workshop' }),
      h('th', { text: 'Keys' }),
      h('th', { text: 'Size' }),
      h('th', { text: 'Active' }),
      h('th', { text: '' }),
    ]))
  );
  const tbody = h('tbody');
  for (const m of d.mods) {
    const activeCb = h('input', { type: 'checkbox' });
    activeCb.checked = d.activeMods.includes(m.name);
    activeCb.onchange = async () => {
      try { await api.post('/api/mods/enable', { mod: m.name, enabled: activeCb.checked }); }
      catch (e) { handleErr(e); activeCb.checked = !activeCb.checked; }
    };

    const actions = h('div', { class: 'actions', style: { margin: 0 } });
    if (!m.installedInServer && m.availableInWorkshop) {
      actions.append(h('button', { class: 'primary', i18n: 'action.install',
        onclick: async () => { try { await api.post('/api/mods/install', { mod: m.name }); toast('installed','ok'); await navigate('mods'); } catch (e) { handleErr(e); } }}));
    }
    if (m.installedInServer && m.availableInWorkshop) {
      actions.append(h('button', {
        class: m.updateAvailable ? 'primary' : '',
        i18n: 'action.update',
        onclick: async () => {
          try { await api.post('/api/mods/update', { mod: m.name }); toast('updated','ok'); await navigate('mods'); }
          catch (e) { handleErr(e); }
        }
      }));
    }
    if (m.installedInServer) {
      actions.append(h('button', { class: 'danger', i18n: 'action.uninstall',
        onclick: async () => {
          if (!confirm(`Uninstall ${m.name}?`)) return;
          try { await api.post('/api/mods/uninstall', { mod: m.name }); toast('removed','ok'); await navigate('mods'); } catch (e) { handleErr(e); }
        }}));
    }

    let statusBadge;
    if (!m.installedInServer) {
      statusBadge = h('span', { class: 'badge mute', text: '—' });
    } else if (m.updateAvailable) {
      statusBadge = h('span', { class: 'badge warn', i18n: 'mods.updateAvailable' });
    } else {
      statusBadge = h('span', { class: 'badge ok', i18n: 'mods.upToDate' });
    }

    const nameCell = h('td', {}, [
      h('div', { text: m.name, style: { fontWeight: '600' } }),
      m.displayName ? h('div', { class: 'hint', text: m.displayName }) : null,
      m.publishedId ? h('div', { class: 'hint' }, h('a', {
        href: `https://steamcommunity.com/sharedfiles/filedetails/?id=${m.publishedId}`,
        target: '_blank', rel: 'noopener', text: `id: ${m.publishedId}`,
      })) : null,
    ]);
    tbody.append(h('tr', {}, [
      nameCell,
      h('td', {}, statusBadge),
      h('td', {}, m.availableInWorkshop ? h('span', { class: 'badge ok', text: '✓' }) : h('span', { class: 'badge mute', text: '—' })),
      h('td', { text: m.keyCount }),
      h('td', { text: bytes(m.sizeBytes) }),
      h('td', {}, activeCb),
      h('td', {}, actions),
    ]));
  }
  tbl.append(tbody);

  const toolbar = h('div', { class: 'toolbar' }, [
    h('button', { i18n: 'action.syncKeys',
      onclick: async () => { try { await api.post('/api/mods/sync-keys'); toast('keys synced','ok'); } catch (e) { handleErr(e); } }
    }),
  ]);
  if (outdatedCount > 0) {
    toolbar.append(h('button', {
      class: 'primary',
      onclick: async () => {
        try {
          const r = await api.post('/api/mods/update-all');
          toast(`updated ${r.count || 0} mod(s)`, 'ok');
          await navigate('mods');
        } catch (e) { handleErr(e); }
      },
    }, [h('span', { i18n: 'action.updateAll' }), h('span', { text: ` (${outdatedCount})` })]));
  }

  wrap.append(h('div', { class: 'card' }, [
    h('h2', { i18n: 'mods.title' }),
    toolbar,
    tbl,
  ]));

  // Load-order (drag-to-reorder) panel. Reflects the current config.mods list.
  const orderWrap = h('div', { class: 'card' }, [
    h('h3', { i18n: 'mods.loadOrder' }),
    h('p', { class: 'hint', i18n: 'mods.loadOrder.hint' }),
  ]);
  const orderList = h('div', { class: 'order-list' });
  const order = [...(d.activeMods || [])];

  function renderOrder() {
    orderList.innerHTML = '';
    for (let i = 0; i < order.length; i++) {
      const name = order[i];
      const row = h('div', { class: 'order-row', draggable: 'true' }, [
        h('span', { class: 'drag-handle', text: '⋮⋮' }),
        h('span', { text: `${i + 1}. ${name}` }),
      ]);
      row.dataset.index = String(i);
      row.addEventListener('dragstart', e => {
        row.classList.add('dragging');
        e.dataTransfer.setData('text/plain', String(i));
        e.dataTransfer.effectAllowed = 'move';
      });
      row.addEventListener('dragend', () => row.classList.remove('dragging'));
      row.addEventListener('dragover', e => { e.preventDefault(); e.dataTransfer.dropEffect = 'move'; });
      row.addEventListener('drop', async e => {
        e.preventDefault();
        const from = Number(e.dataTransfer.getData('text/plain'));
        const to = Number(row.dataset.index);
        if (isNaN(from) || isNaN(to) || from === to) return;
        const [moved] = order.splice(from, 1);
        order.splice(to, 0, moved);
        renderOrder();
        try {
          await api.post('/api/mods/order', { mods: order, serverSide: false });
          toast('order saved', 'ok');
        } catch (err) { handleErr(err); }
      });
      orderList.append(row);
    }
    if (!order.length) orderList.append(h('p', { class: 'hint', text: '—' }));
  }
  renderOrder();
  orderWrap.append(orderList);
  wrap.append(orderWrap);

  root.append(wrap);
};

// --------------------------------------------------------------------- types

Views.types = async (root) => {
  const fileSelect = h('select', {});
  fileSelect.append(h('option', { value: '', text: 'types.xml (base)' }));
  try {
    const moded = await api.get('/api/moded');
    for (const f of moded.files || []) {
      fileSelect.append(h('option', { value: f.name, text: `moded_types/${f.name}${f.registered ? '' : ' (unregistered)'}` }));
    }
  } catch (e) { /* no mission yet */ }

  const search = h('input', { type: 'text', placeholder: t('types.search') });
  const tableWrap = h('div');
  const editorWrap = h('div');

  const presetsWrap = h('div', { class: 'card' }, [h('h3', { i18n: 'types.presets' })]);
  try {
    const presets = await api.get('/api/types/presets');
    const pills = h('div', { class: 'pills' });
    for (const p of presets) {
      pills.append(h('button', {
        class: 'pill', text: State.lang === 'ru' ? p.labelRu : p.label,
        onclick: async () => {
          const selected = [...tableWrap.querySelectorAll('input[type=checkbox]:checked')]
            .map(cb => cb.dataset.name);
          if (!selected.length) { toast('select types first'); return; }
          try {
            await api.post('/api/types/apply-preset', {
              file: fileSelect.value, names: selected, presetId: p.id,
            });
            toast('applied', 'ok');
            await refreshTable();
          } catch (e) { handleErr(e); }
        }
      }));
    }
    presetsWrap.append(pills);
    presetsWrap.append(h('p', { class: 'hint', i18n: 'types.presets.hint' }));
  } catch (e) { /* empty */ }

  async function refreshTable() {
    tableWrap.innerHTML = '';
    let d;
    try { d = await api.get(`/api/types?file=${encodeURIComponent(fileSelect.value)}`); }
    catch (e) { tableWrap.append(h('p', { text: e.message })); return; }
    const q = search.value.toLowerCase();

    const tbl = h('table');
    tbl.append(h('thead', {}, h('tr', {}, [
      h('th', {}),
      h('th', { text: 'Name' }),
      h('th', { i18n: 'types.field.nominal' }),
      h('th', { i18n: 'types.field.min' }),
      h('th', { i18n: 'types.field.lifetime' }),
      h('th', { i18n: 'types.field.category' }),
      h('th', { text: '' }),
    ])));
    const tbody = h('tbody');
    let shown = 0;
    for (const row of d.types) {
      if (q && !row.name.toLowerCase().includes(q)) continue;
      if (++shown > 500) break;
      const cb = h('input', { type: 'checkbox' });
      cb.dataset.name = row.name;
      tbody.append(h('tr', {}, [
        h('td', {}, cb),
        h('td', { text: row.name }),
        h('td', { text: row.nominal ?? '' }),
        h('td', { text: row.min ?? '' }),
        h('td', { text: row.lifetime ?? '' }),
        h('td', { text: row.category || '' }),
        h('td', {}, h('button', { text: 'Edit',
          onclick: () => openEditor(row.name)
        })),
      ]));
    }
    tbl.append(tbody);
    tableWrap.append(
      h('p', { class: 'hint', text: `${shown}/${d.count} shown${d.count>500?' (first 500)':''}` }),
      tbl,
    );
  }

  async function openEditor(name) {
    editorWrap.innerHTML = '';
    const t = await api.get(`/api/types/item?file=${encodeURIComponent(fileSelect.value)}&name=${encodeURIComponent(name)}`);
    const fields = {
      nominal: h('input', { type: 'number', value: t.nominal ?? '' }),
      min: h('input', { type: 'number', value: t.min ?? '' }),
      lifetime: h('input', { type: 'number', value: t.lifetime ?? '' }),
      restock: h('input', { type: 'number', value: t.restock ?? '' }),
      quantmin: h('input', { type: 'number', value: t.quantmin ?? '' }),
      quantmax: h('input', { type: 'number', value: t.quantmax ?? '' }),
      cost: h('input', { type: 'number', value: t.cost ?? '' }),
      category: h('input', { type: 'text',   value: t.category?.name || '' }),
    };
    const grid = h('div', { class: 'grid-3' });
    grid.append(
      ...Object.entries(fields).map(([k, el]) => h('div', {}, [h('label', { text: k }), el]))
    );

    // Usages / Values / Tags as editable pill lists.
    function editableList(label, items, setItems) {
      const wrap = h('div', { class: 'pills' });
      const render = () => {
        wrap.innerHTML = '';
        for (let i = 0; i < items.length; i++) {
          const pill = h('span', { class: 'pill', text: items[i].name });
          pill.append(h('button', { text: '×', onclick: () => { items.splice(i, 1); render(); } }));
          wrap.append(pill);
        }
        const inp = h('input', { type: 'text', style: { width: '140px' }, placeholder: '+ add' });
        inp.onkeydown = (e) => {
          if (e.key === 'Enter' && inp.value.trim()) {
            items.push({ name: inp.value.trim() });
            render();
          }
        };
        wrap.append(inp);
      };
      render();
      return h('div', {}, [h('label', { text: label }), wrap]);
    }

    const usages = t.usages || [];
    const values = t.values || [];
    const tags = t.tags || [];
    const lists = h('div', { class: 'grid-3' }, [
      editableList('usages', usages, v => {}),
      editableList('values', values, v => {}),
      editableList('tags', tags, v => {}),
    ]);

    // Flags.
    const flags = t.flags || {};
    const flagInputs = {};
    const flagGrid = h('div', { class: 'grid-3' });
    for (const key of ['countInCargo','countInHoarder','countInMap','countInPlayer','crafted','deloot']) {
      const inp = h('input', { type: 'number', value: flags[key] ?? 0 });
      flagInputs[key] = inp;
      flagGrid.append(h('div', {}, [h('label', { text: key }), inp]));
    }

    editorWrap.append(
      h('div', { class: 'card' }, [
        h('h3', { text: `${name}` }),
        grid,
        lists,
        h('label', { text: 'flags' }),
        flagGrid,
        h('div', { class: 'actions' }, [
          h('button', { class: 'primary', i18n: 'action.save',
            onclick: async () => {
              const body = { name };
              for (const [k, el] of Object.entries(fields)) {
                const v = el.value.trim();
                if (v === '') continue;
                if (k === 'category') body.category = { name: v };
                else body[k] = Number(v);
              }
              body.usages = usages;
              body.values = values;
              body.tags = tags;
              body.flags = Object.fromEntries(Object.entries(flagInputs).map(([k, el]) => [k, Number(el.value) || 0]));
              try {
                await api.post(`/api/types/item?file=${encodeURIComponent(fileSelect.value)}&name=${encodeURIComponent(name)}`, body);
                toast('saved', 'ok');
                await refreshTable();
              } catch (e) { handleErr(e); }
            }
          }),
          h('button', { class: 'danger', i18n: 'action.delete',
            onclick: async () => {
              if (!confirm(`Delete ${name}?`)) return;
              try {
                await api.del(`/api/types/item?file=${encodeURIComponent(fileSelect.value)}&name=${encodeURIComponent(name)}`);
                toast('deleted', 'ok');
                editorWrap.innerHTML = '';
                await refreshTable();
              } catch (e) { handleErr(e); }
            }
          }),
        ]),
      ])
    );
  }

  fileSelect.onchange = refreshTable;
  search.oninput = refreshTable;

  if (State.serverStatus.running) root.append(runningBanner());
  root.append(
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'types.title' }),
      h('div', { class: 'toolbar' }, [fileSelect, search]),
      tableWrap,
    ]),
    presetsWrap,
    editorWrap,
  );
  await refreshTable();
};

// --------------------------------------------------------------------- moded types

Views.moded = async (root) => {
  let d;
  try { d = await api.get('/api/moded'); }
  catch (e) { root.append(h('div', { class: 'card' }, h('p', { text: e.message }))); return; }

  const table = h('table');
  table.append(h('thead', {}, h('tr', {}, [
    h('th', { text: 'File' }),
    h('th', { text: 'Size' }),
    h('th', { text: 'Registered' }),
    h('th', { text: '' }),
  ])));
  const tbody = h('tbody');
  for (const f of d.files) {
    tbody.append(h('tr', {}, [
      h('td', { text: f.name }),
      h('td', { text: bytes(f.size) }),
      h('td', {}, f.registered ? h('span', { class: 'badge ok', text: '✓' }) : h('span', { class: 'badge warn', text: '!' })),
      h('td', {}, h('button', { class: 'danger', i18n: 'action.delete',
        onclick: async () => {
          if (!confirm(`Delete ${f.name}?`)) return;
          try { await api.post('/api/moded/delete', { fileName: f.name }); toast('deleted','ok'); await navigate('moded'); }
          catch (e) { handleErr(e); }
        }
      })),
    ]));
  }
  table.append(tbody);

  const nameInput = h('input', { type: 'text', placeholder: 'mymod_types.xml' });
  const regCb    = h('input', { type: 'checkbox' }); regCb.checked = true;

  root.append(
    State.serverStatus.running ? runningBanner() : null,
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'moded.title' }),
      h('p', { class: 'hint', text: d.folder }),
      table,
    ]),
    h('div', { class: 'card' }, [
      h('h3', { i18n: 'moded.create' }),
      h('label', { i18n: 'moded.fileName' }), nameInput,
      h('label', { style: { display: 'flex', gap: '8px', alignItems: 'center' } }, [
        regCb, h('span', { i18n: 'moded.autoRegister' })
      ]),
      h('div', { class: 'actions' }, [
        h('button', { class: 'primary', i18n: 'action.create',
          onclick: async () => {
            let name = nameInput.value.trim();
            if (!name) return;
            if (!name.endsWith('.xml')) name += '.xml';
            try {
              await api.post('/api/moded/create', { fileName: name, autoRegister: regCb.checked });
              toast('created', 'ok');
              await navigate('moded');
            } catch (e) { handleErr(e); }
          }
        }),
      ]),
    ])
  );
};

// --------------------------------------------------------------------- files

Views.files = async (root) => {
  const tree = h('div', { class: 'tree' });
  const editor = h('textarea', { placeholder: 'Select a file' });
  const pathLabel = h('div', { class: 'hint' });
  let currentPath = '';

  async function loadDir(path) {
    const d = await api.get(`/api/files/tree?path=${encodeURIComponent(path || '')}`);
    tree.innerHTML = '';
    if (path) {
      tree.append(h('div', { class: 'node dir', text: '⬆ ..',
        onclick: () => loadDir(path.split('/').slice(0, -1).join('/')) }));
    }
    for (const e of d.entries) {
      const node = h('div', { class: `node ${e.isDir ? 'dir' : ''}`,
        text: `${e.isDir ? '📁' : '📄'} ${e.name}`,
      });
      node.onclick = async () => {
        if (e.isDir) return loadDir(e.path);
        const r = await api.get(`/api/files/read?path=${encodeURIComponent(e.path)}`);
        currentPath = e.path;
        editor.value = r.content;
        pathLabel.textContent = e.path;
      };
      tree.append(node);
    }
  }
  try { await loadDir(''); } catch (e) { handleErr(e); }

  root.append(
    State.serverStatus.running ? runningBanner() : null,
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'files.title' }),
      h('div', { class: 'grid-2' }, [
        h('div', {}, [h('h3', { i18n: 'files.tree' }), tree]),
        h('div', {}, [
          h('h3', { i18n: 'files.editor' }),
          pathLabel,
          editor,
          h('div', { class: 'actions' }, [
            h('button', { class: 'primary', i18n: 'files.save',
              onclick: async () => {
                if (!currentPath) return;
                try {
                  await api.post('/api/files/write', { path: currentPath, content: editor.value });
                  toast('saved', 'ok');
                } catch (e) { handleErr(e); }
              }
            }),
          ]),
        ]),
      ]),
    ])
  );
};

// --------------------------------------------------------------------- validator

Views.validator = async (root) => {
  const listEl = h('div');
  const run = async () => {
    listEl.innerHTML = '';
    try {
      const d = await api.get('/api/validate');
      if (!d.count) { listEl.append(h('p', { i18n: 'validator.none' })); return; }
      const tbl = h('table');
      tbl.append(h('thead', {}, h('tr', {}, [
        h('th', { text: 'Severity' }),
        h('th', { text: 'File' }),
        h('th', { text: 'Line' }),
        h('th', { text: 'Message' }),
      ])));
      const tb = h('tbody');
      for (const i of d.issues) {
        const cls = i.severity === 'error' ? 'err' : (i.severity === 'warning' ? 'warn' : 'mute');
        tb.append(h('tr', {}, [
          h('td', {}, h('span', { class: `badge ${cls}`, text: i.severity })),
          h('td', { text: i.file }),
          h('td', { text: i.line || '' }),
          h('td', { text: i.message }),
        ]));
      }
      tbl.append(tb);
      listEl.append(tbl);
    } catch (e) { handleErr(e); }
  };
  root.append(
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'validator.title' }),
      h('div', { class: 'actions' }, [
        h('button', { class: 'primary', i18n: 'action.validate', onclick: run }),
      ]),
      listEl,
    ])
  );
  run();
};

// --------------------------------------------------------------------- logs

Views.logs = async (root) => {
  const sources = await api.get('/api/logs/list');
  const picker = h('select', {});
  for (const s of sources) {
    picker.append(h('option', {
      value: s.id,
      text: `${s.label}${s.exists ? '' : ' (no file yet)'}`,
    }));
  }

  const pane = h('pre', { class: 'log-pane' });
  const autoScroll = h('input', { type: 'checkbox' });
  autoScroll.checked = true;

  let source;
  const MAX_CHARS = 400_000;

  function append(text) {
    pane.textContent += text;
    if (pane.textContent.length > MAX_CHARS) {
      pane.textContent = pane.textContent.slice(-MAX_CHARS);
    }
    if (autoScroll.checked) pane.scrollTop = pane.scrollHeight;
  }

  async function attach(id) {
    if (source) { source.close(); source = null; }
    pane.textContent = '';
    try {
      const r = await api.get(`/api/logs/read?id=${encodeURIComponent(id)}`);
      append(r.content || '');
    } catch (e) { append(`[snapshot failed: ${e.message}]\n`); }

    source = new EventSource(`/api/logs/stream?id=${encodeURIComponent(id)}`, { withCredentials: true });
    source.onmessage = ev => { append(ev.data + '\n'); };
    source.onerror = () => { append('\n[stream disconnected — reconnecting...]\n'); };
  }

  picker.onchange = () => attach(picker.value);

  root.append(
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'nav.logs' }),
      h('div', { class: 'toolbar' }, [
        picker,
        h('label', { style: { display: 'flex', gap: '6px', alignItems: 'center' } }, [
          autoScroll, h('span', { text: 'autoscroll' }),
        ]),
        h('button', { text: 'Clear', onclick: () => { pane.textContent = ''; } }),
      ]),
      pane,
    ]),
  );

  if (sources.length) await attach(sources[0].id);

  // Tear down EventSource on navigation away.
  root._teardown = () => { if (source) source.close(); };
};

// --------------------------------------------------------------------- rcon

Views.rcon = async (root) => {
  const card = h('div', { class: 'card' }, [h('h2', { i18n: 'nav.rcon' })]);
  const status = h('div', { class: 'hint' });
  const players = h('div', { class: 'players-grid' });
  const sayInp = h('input', { type: 'text', placeholder: 'Broadcast message' });
  const cmdInp = h('input', { type: 'text', placeholder: 'Raw RCon command, e.g. #shutdown' });
  const cmdOut = h('pre', { class: 'log-pane', style: { height: '180px' } });

  async function refresh() {
    status.textContent = '';
    players.innerHTML = '';
    try {
      const d = await api.get('/api/rcon/players');
      players.append(
        h('div', { class: 'head', text: 'ID' }),
        h('div', { class: 'head', text: 'Name' }),
        h('div', { class: 'head', text: 'GUID' }),
        h('div', { class: 'head', text: 'Ping' }),
        h('div', { class: 'head', text: '' }),
      );
      for (const p of d.players || []) {
        players.append(
          h('div', { text: p.id }),
          h('div', { text: p.name + (p.lobby ? ' (lobby)' : '') }),
          h('div', { text: p.guid }),
          h('div', { text: p.ping }),
          h('div', {}, [
            h('button', { text: 'Kick', onclick: async () => {
              const r = prompt(`Kick ${p.name}? Reason:`, 'kicked');
              if (r === null) return;
              try { await api.post('/api/rcon/kick', { playerId: p.id, reason: r }); await refresh(); }
              catch (e) { handleErr(e); }
            }}),
            ' ',
            h('button', { class: 'danger', text: 'Ban', onclick: async () => {
              const mins = prompt(`Ban ${p.name}. Minutes (0 = permanent):`, '60');
              if (mins === null) return;
              const reason = prompt('Reason:', 'banned') || '';
              try { await api.post('/api/rcon/ban', { playerId: p.id, minutes: Number(mins) || 0, reason }); await refresh(); }
              catch (e) { handleErr(e); }
            }}),
          ]),
        );
      }
      status.textContent = `${d.count || 0} player(s)`;
    } catch (e) {
      status.textContent = `RCon error: ${e.message}. Configure RCon in settings.`;
    }
  }

  card.append(
    h('div', { class: 'actions' }, [
      h('button', { text: 'Refresh', onclick: refresh }),
    ]),
    status,
    players,
    h('h3', { text: 'Broadcast' }),
    h('div', { class: 'row' }, [
      sayInp,
      h('button', { class: 'primary', text: 'Say', onclick: async () => {
        if (!sayInp.value.trim()) return;
        try { await api.post('/api/rcon/say', { message: sayInp.value }); sayInp.value = ''; toast('sent','ok'); }
        catch (e) { handleErr(e); }
      }}),
    ]),
    h('h3', { text: 'Raw command' }),
    h('div', { class: 'row' }, [
      cmdInp,
      h('button', { text: 'Send', onclick: async () => {
        if (!cmdInp.value.trim()) return;
        try { const r = await api.post('/api/rcon/command', { command: cmdInp.value }); cmdOut.textContent = r.output || '(empty)'; }
        catch (e) { cmdOut.textContent = `ERR: ${e.message}`; }
      }}),
    ]),
    cmdOut,
  );

  root.append(card);
  await refresh();

  const timer = setInterval(refresh, 5000);
  root._teardown = () => clearInterval(timer);
};

// --------------------------------------------------------------------- events

Views.events = async (root) => {
  const search = h('input', { type: 'text', placeholder: t('events.search') });
  const tableWrap = h('div');
  const editorWrap = h('div');

  const NUM_FIELDS = ['nominal','min','max','lifetime','restock','saveable','active'];

  async function refreshTable() {
    tableWrap.innerHTML = '';
    let d;
    try { d = await api.get('/api/events'); }
    catch (e) { tableWrap.append(h('p', { text: e.message })); return; }
    const q = search.value.toLowerCase();

    const tbl = h('table');
    tbl.append(h('thead', {}, h('tr', {}, [
      h('th', { text: 'Name' }),
      h('th', { i18n: 'events.field.nominal' }),
      h('th', { i18n: 'events.field.min' }),
      h('th', { i18n: 'events.field.max' }),
      h('th', { i18n: 'events.field.lifetime' }),
      h('th', { i18n: 'events.field.active' }),
      h('th', { i18n: 'events.field.children' }),
      h('th', { text: '' }),
    ])));
    const tbody = h('tbody');
    let shown = 0;
    for (const row of d.events) {
      if (q && !row.name.toLowerCase().includes(q)) continue;
      if (++shown > 500) break;
      tbody.append(h('tr', {}, [
        h('td', { text: row.name }),
        h('td', { text: row.nominal ?? '' }),
        h('td', { text: row.min ?? '' }),
        h('td', { text: row.max ?? '' }),
        h('td', { text: row.lifetime ?? '' }),
        h('td', {}, row.active ? h('span', { class: 'badge ok', text: '✓' }) : h('span', { class: 'badge mute', text: '—' })),
        h('td', { text: row.children || 0 }),
        h('td', {}, h('button', { text: 'Edit', onclick: () => openEditor(row.name) })),
      ]));
    }
    tbl.append(tbody);
    tableWrap.append(
      h('p', { class: 'hint', text: `${shown}/${d.count} shown${d.count>500?' (first 500)':''}` }),
      tbl,
    );
  }

  async function openEditor(name) {
    editorWrap.innerHTML = '';
    const ev = await api.get(`/api/events/item?name=${encodeURIComponent(name)}`);

    const inputs = {};
    const grid = h('div', { class: 'grid-3' });
    for (const k of NUM_FIELDS) {
      const el = h('input', { type: 'number', value: ev[k] ?? '' });
      inputs[k] = el;
      grid.append(h('div', {}, [h('label', { i18n: `events.field.${k}` }), el]));
    }

    // Children editor.
    const children = (ev.Children && ev.Children.Child) ? ev.Children.Child.slice() : [];
    const childrenWrap = h('div');
    function renderChildren() {
      childrenWrap.innerHTML = '';
      const tbl = h('table');
      tbl.append(h('thead', {}, h('tr', {}, [
        h('th', { i18n: 'events.child.type' }),
        h('th', { i18n: 'events.child.min' }),
        h('th', { i18n: 'events.child.max' }),
        h('th', { i18n: 'events.child.lootmin' }),
        h('th', { i18n: 'events.child.lootmax' }),
        h('th', { text: '' }),
      ])));
      const tb = h('tbody');
      for (let i = 0; i < children.length; i++) {
        const c = children[i];
        const typeIn = h('input', { type: 'text', value: c.Type || '' });
        const minIn  = h('input', { type: 'number', value: c.Min ?? 0 });
        const maxIn  = h('input', { type: 'number', value: c.Max ?? -1 });
        const lmiIn  = h('input', { type: 'number', value: c.LootMin ?? 0 });
        const lmaIn  = h('input', { type: 'number', value: c.LootMax ?? 0 });
        typeIn.oninput = () => { c.Type = typeIn.value; };
        minIn.oninput  = () => { c.Min  = Number(minIn.value)  || 0; };
        maxIn.oninput  = () => { c.Max  = Number(maxIn.value)  || 0; };
        lmiIn.oninput  = () => { c.LootMin = Number(lmiIn.value) || 0; };
        lmaIn.oninput  = () => { c.LootMax = Number(lmaIn.value) || 0; };
        tb.append(h('tr', {}, [
          h('td', {}, typeIn),
          h('td', {}, minIn),
          h('td', {}, maxIn),
          h('td', {}, lmiIn),
          h('td', {}, lmaIn),
          h('td', {}, h('button', { class: 'danger', text: '×', onclick: () => { children.splice(i, 1); renderChildren(); applyI18n(); } })),
        ]));
      }
      tbl.append(tb);
      childrenWrap.append(tbl);
      childrenWrap.append(h('button', {
        i18n: 'events.addChild',
        onclick: () => { children.push({ Type: '', Min: 1, Max: -1, LootMin: 0, LootMax: 0 }); renderChildren(); applyI18n(); },
      }));
    }
    renderChildren();

    editorWrap.append(
      h('div', { class: 'card' }, [
        h('h3', { text: name }),
        grid,
        h('h4', { i18n: 'events.field.children' }),
        childrenWrap,
        h('div', { class: 'actions' }, [
          h('button', { class: 'primary', i18n: 'action.save',
            onclick: async () => {
              const body = { Name: name };
              for (const k of NUM_FIELDS) {
                const v = inputs[k].value.trim();
                if (v === '') continue;
                // Go json decodes `*int` from a JSON number; capitalize to match struct tags
                // won't work because xml struct tags drive json too via field name. Use struct field name.
                const field = k.charAt(0).toUpperCase() + k.slice(1);
                body[field] = Number(v);
              }
              if (children.length) body.Children = { Child: children };
              try {
                await api.post(`/api/events/item?name=${encodeURIComponent(name)}`, body);
                toast('saved', 'ok');
                await refreshTable();
              } catch (e) { handleErr(e); }
            }
          }),
          h('button', { class: 'danger', i18n: 'action.delete',
            onclick: async () => {
              if (!confirm(`Delete event ${name}?`)) return;
              try {
                await api.del(`/api/events/item?name=${encodeURIComponent(name)}`);
                toast('deleted', 'ok');
                editorWrap.innerHTML = '';
                await refreshTable();
              } catch (e) { handleErr(e); }
            }
          }),
        ]),
      ])
    );
    applyI18n();
  }

  search.oninput = refreshTable;

  if (State.serverStatus.running) root.append(runningBanner());
  root.append(
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'events.title' }),
      h('p', { class: 'hint', i18n: 'events.hint' }),
      h('div', { class: 'toolbar' }, [search]),
      tableWrap,
    ]),
    editorWrap,
  );
  await refreshTable();
};

// --------------------------------------------------------------------- settings

Views.settings = async (root) => {
  const c = State.config;
  const F = {
    language:        h('select', {}, [h('option', { value: 'en', text: 'English' }), h('option', { value: 'ru', text: 'Русский' })]),
    vanillaDayZPath: h('input',  { type: 'text', value: c.vanillaDayZPath || '' }),
    serverName:      h('input',  { type: 'text', value: c.serverName || '' }),
    serverPort:      h('input',  { type: 'number', value: c.serverPort }),
    serverCfg:       h('input',  { type: 'text', value: c.serverCfg }),
    cpuCount:        h('input',  { type: 'number', value: c.cpuCount }),
    bePath:          h('input',  { type: 'text', value: c.bePath }),
    profilesDir:     h('input',  { type: 'text', value: c.profilesDir }),
    autoRestartSeconds: h('input', { type: 'number', value: c.autoRestartSeconds }),
    autoRestartEnabled: h('input', { type: 'checkbox' }),
    doLogs:       h('input', { type: 'checkbox' }),
    adminLog:     h('input', { type: 'checkbox' }),
    netLog:       h('input', { type: 'checkbox' }),
    freezeCheck:  h('input', { type: 'checkbox' }),
    filePatching: h('input', { type: 'checkbox' }),
  };
  F.language.value = c.language;
  for (const k of ['autoRestartEnabled','doLogs','adminLog','netLog','freezeCheck','filePatching']) {
    F[k].checked = !!c[k];
  }

  const section = (title, rows) => h('div', { class: 'card' }, [h('h3', { i18n: title }), ...rows]);
  const row = (labelKey, el) => h('div', {}, [h('label', { i18n: labelKey }), el]);

  root.append(
    h('div', { class: 'card' }, [h('h2', { i18n: 'settings.title' })]),

    section('settings.title', [
      row('settings.language', F.language),
      row('settings.vanillaPath', F.vanillaDayZPath),
    ]),

    section('nav.server', [
      h('div', { class: 'grid-2' }, [
        row('settings.serverName', F.serverName),
        row('settings.serverPort', F.serverPort),
        row('settings.serverCfg', F.serverCfg),
        row('settings.cpuCount', F.cpuCount),
        row('settings.bepath', F.bePath),
        row('settings.profilesDir', F.profilesDir),
      ]),
    ]),

    section('settings.flags', [
      h('div', { class: 'grid-3' }, [
        h('label', {}, [F.doLogs,       h('span', { i18n: 'settings.flag.dologs' })]),
        h('label', {}, [F.adminLog,     h('span', { i18n: 'settings.flag.adminlog' })]),
        h('label', {}, [F.netLog,       h('span', { i18n: 'settings.flag.netlog' })]),
        h('label', {}, [F.freezeCheck,  h('span', { i18n: 'settings.flag.freezecheck' })]),
        h('label', {}, [F.filePatching, h('span', { i18n: 'settings.flag.filePatching' })]),
      ]),
    ]),

    section('settings.autorestart', [
      h('div', { class: 'row' }, [
        h('label', {}, [F.autoRestartEnabled, h('span', { i18n: 'settings.autorestart.enable' })]),
        row('settings.autorestart', F.autoRestartSeconds),
      ]),
    ]),

    h('div', { class: 'actions' }, [
      h('button', { class: 'primary', i18n: 'action.save',
        onclick: async () => {
          const next = { ...c };
          for (const [k, el] of Object.entries(F)) {
            if (el.type === 'checkbox') next[k] = el.checked;
            else if (el.type === 'number') next[k] = Number(el.value);
            else next[k] = el.value;
          }
          try {
            State.config = await api.post('/api/config', next);
            if (next.language !== c.language) {
              await loadI18n(next.language);
              document.getElementById('lang-switch').value = next.language;
            }
            toast('saved', 'ok');
          } catch (e) { handleErr(e); }
        }
      }),
    ]),
  );
};

// --------------------------------------------------------------------- bootstrap

async function main() {
  try {
    State.info = await api.get('/api/info');
    // Check auth before anything else. If the panel requires auth and we
    // have no valid session, surface the login modal and bail out — the
    // user will re-enter main() from the login submit handler.
    const s = await api.get('/api/auth/status');
    if (s.requireAuth && !s.authenticated) { showLogin(); return; }

    State.config = await api.get('/api/config');
    await loadI18n(State.config.language || 'en');
    document.getElementById('lang-switch').value = State.lang;
    document.getElementById('lang-switch').onchange = async e => {
      const v = e.target.value;
      State.config.language = v;
      try { await api.post('/api/config', State.config); } catch (err) {}
      await loadI18n(v);
      await navigate(currentRoute());
    };
    // Wire the topbar logout button (hidden when auth is disabled).
    const lo = document.getElementById('logout-btn');
    if (lo) {
      lo.classList.toggle('hidden', !State.config.requireAuth);
      lo.onclick = () => logout();
    }
    await ensureFirstRunDone();
    await refreshStatus();
    setInterval(refreshStatus, 5000);
    await navigate('dashboard');
  } catch (err) {
    if (String(err.message) !== 'unauthorized') handleErr(err);
  }
}

function currentRoute() {
  const active = document.querySelector('.nav a.active');
  return active ? active.dataset.route : 'dashboard';
}

main();
