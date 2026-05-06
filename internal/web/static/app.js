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

// Top-of-page indeterminate progress bar tied to api call lifecycle.
// Counts in-flight requests; bar fades in after a short delay so quick
// (~<300ms) calls don't flash. Streaming endpoints (/api/logs/stream) bypass
// this — they're long-lived and would pin the bar forever.
const Progress = {
  pending: 0, showTimer: 0,
  bar() {
    let el = document.getElementById('top-progress');
    if (!el) {
      el = document.createElement('div');
      el.id = 'top-progress';
      el.className = 'top-progress';
      document.body.append(el);
    }
    return el;
  },
  start() {
    this.pending++;
    if (this.pending === 1) {
      clearTimeout(this.showTimer);
      this.showTimer = setTimeout(() => this.bar().classList.add('busy'), 250);
    }
  },
  end() {
    if (this.pending > 0) this.pending--;
    if (this.pending === 0) {
      clearTimeout(this.showTimer);
      this.bar().classList.remove('busy');
    }
  },
};

const api = {
  async get(path) {
    Progress.start();
    try {
      const r = await fetch(path, { credentials: 'same-origin' });
      if (r.status === 401) { showLogin(); throw new Error('unauthorized'); }
      if (!r.ok) throw new Error(await r.text());
      return r.json();
    } finally { Progress.end(); }
  },
  async send(path, method, body) {
    Progress.start();
    try {
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
    } finally { Progress.end(); }
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
  // Auth errors are handled by showLogin() inside api.get/send. The throw
  // we receive here is just a flow-control signal — never surface it as a
  // toast, which used to confuse users into thinking the panel was broken.
  if (String(err && err.message) === 'unauthorized') return;
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

// Global keyboard shortcuts.
//   Ctrl/Cmd+S — call root._save() if the active view registered one.
//   Ctrl/Cmd+K — open the command palette.
// Always swallows the browser default (Save Page As / search bar focus).
document.addEventListener('keydown', e => {
  const meta = e.ctrlKey || e.metaKey;
  if (!meta) return;
  if (e.key === 's' || e.key === 'S') {
    const root = document.getElementById('view');
    if (root && typeof root._save === 'function') {
      e.preventDefault();
      Promise.resolve(root._save()).catch(handleErr);
    }
  } else if (e.key === 'k' || e.key === 'K') {
    e.preventDefault();
    openCommandPalette();
  }
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

// --------------------------------------------------------------------- CodeMirror lazy loader
//
// CM is vendored under /vendor/codemirror so the panel stays single-exe and
// works offline. We load it on demand — only pages that actually render an
// editor pay the cost.

const CM = {
  loaded: false,
  loading: null,
  async load() {
    if (this.loaded) return window.CodeMirror;
    if (this.loading) return this.loading;
    const base = '/vendor/codemirror';
    const add = (tag, attrs) => new Promise((res, rej) => {
      const el = document.createElement(tag);
      Object.assign(el, attrs);
      el.onload = () => res(el);
      el.onerror = () => rej(new Error('load failed: ' + (attrs.src || attrs.href)));
      document.head.appendChild(el);
    });
    this.loading = (async () => {
      await Promise.all([
        add('link',   { rel: 'stylesheet', href: `${base}/codemirror.min.css` }),
        add('link',   { rel: 'stylesheet', href: `${base}/theme/material-darker.min.css` }),
        add('link',   { rel: 'stylesheet', href: `${base}/addon/fold/foldgutter.min.css` }),
        add('link',   { rel: 'stylesheet', href: `${base}/addon/dialog/dialog.min.css` }),
      ]);
      await add('script', { src: `${base}/codemirror.min.js` });
      await Promise.all([
        add('script', { src: `${base}/mode/xml/xml.min.js` }),
        add('script', { src: `${base}/mode/javascript/javascript.min.js` }),
        add('script', { src: `${base}/mode/clike/clike.min.js` }),
        add('script', { src: `${base}/mode/shell/shell.min.js` }),
        add('script', { src: `${base}/addon/edit/matchbrackets.min.js` }),
        add('script', { src: `${base}/addon/edit/closebrackets.min.js` }),
        add('script', { src: `${base}/addon/fold/foldcode.min.js` }),
        add('script', { src: `${base}/addon/fold/foldgutter.min.js` }),
        add('script', { src: `${base}/addon/fold/xml-fold.min.js` }),
        add('script', { src: `${base}/addon/fold/brace-fold.min.js` }),
        add('script', { src: `${base}/addon/search/searchcursor.min.js` }),
        add('script', { src: `${base}/addon/search/search.min.js` }),
        add('script', { src: `${base}/addon/dialog/dialog.min.js` }),
      ]);
      this.loaded = true;
      return window.CodeMirror;
    })();
    return this.loading;
  },

  modeFor(name) {
    const n = String(name || '').toLowerCase();
    if (n.endsWith('.xml'))  return { name: 'xml', htmlMode: false };
    if (n.endsWith('.json')) return { name: 'javascript', json: true };
    if (n.endsWith('.c') || n.endsWith('.h') || n.endsWith('.cpp') || n.endsWith('.hpp'))
      return 'text/x-csrc';
    if (n.endsWith('.js'))   return 'javascript';
    if (n.endsWith('.cfg') || n.endsWith('.bat') || n.endsWith('.sh')) return 'shell';
    return null; // plain text
  },

  // Mount a CodeMirror instance on a textarea-like host. Returns the CM
  // handle. Call cm.toTextArea() to clean up before replacing.
  async mount(hostTextarea, opts) {
    const CodeMirror = await this.load();
    const cm = CodeMirror.fromTextArea(hostTextarea, Object.assign({
      lineNumbers: true,
      theme: 'material-darker',
      matchBrackets: true,
      autoCloseBrackets: true,
      foldGutter: true,
      gutters: ['CodeMirror-linenumbers', 'CodeMirror-foldgutter'],
      lineWrapping: false,
      indentUnit: 2,
      tabSize: 2,
      viewportMargin: 80,
    }, opts || {}));
    return cm;
  },
};

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

  // Steam auto-detect button. Populates the vanilla path field in one click;
  // if multiple installs are found, renders a small chooser under it.
  const steamBtn = document.getElementById('fr-steam');
  const steamList = document.getElementById('fr-steam-list');
  if (steamBtn) {
    steamBtn.onclick = async () => {
      steamList.innerHTML = '';
      steamList.classList.add('hidden');
      try {
        const r = await api.get('/api/steam/detect');
        const installs = (r && r.installs) || [];
        if (installs.length === 0) {
          toast(t('firstRun.detect.none') || 'No DayZ install found', 'error');
          return;
        }
        if (installs.length === 1) {
          document.getElementById('fr-vanilla').value = installs[0].path || installs[0];
          toast(t('firstRun.detect.ok') || 'DayZ install detected', 'ok');
          refreshMissionsPreview();
          return;
        }
        steamList.classList.remove('hidden');
        for (const inst of installs) {
          const p = inst.path || inst;
          const row = h('div', { class: 'steam-row' }, [
            h('span', { text: p, class: 'mono' }),
            h('button', {
              class: 'secondary', text: t('action.select') || 'Select',
              onclick: () => {
                document.getElementById('fr-vanilla').value = p;
                steamList.classList.add('hidden');
                refreshMissionsPreview();
              },
            }),
          ]);
          steamList.append(row);
        }
      } catch (e) { handleErr(e); }
    };
  }

  // Missions preview — lists mpmissions/* in the current server dir so the
  // user sees what's already there before committing. Skipped if the folder
  // is empty or missing.
  const missionsHost = document.getElementById('fr-missions');
  async function refreshMissionsPreview() {
    if (!missionsHost) return;
    missionsHost.innerHTML = '';
    missionsHost.classList.add('hidden');
    try {
      const r = await api.get('/api/missions');
      const list = (r && r.missions) || [];
      if (list.length === 0) return;
      missionsHost.classList.remove('hidden');
      missionsHost.append(h('div', { class: 'missions-title',
        text: t('firstRun.missions.title') || 'Missions detected in server dir:' }));
      const ul = h('ul', { class: 'missions-list' });
      for (const m of list) {
        ul.append(h('li', { class: m.active ? 'active' : '' }, [
          h('span', { text: m.name }),
          m.active ? h('span', { class: 'badge ok', text: t('missions.active') || 'active' }) : null,
        ]));
      }
      missionsHost.append(ul);
    } catch {}
  }
  refreshMissionsPreview();

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
  // Re-apply i18n in case loadI18n hasn't run yet on stale-cookie reload.
  applyI18n();
  // Focus the username field so the user can start typing immediately.
  setTimeout(() => document.getElementById('login-user')?.focus(), 50);
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
  root.append(pageHeader('nav.dashboard', 'dashboard.subtitle', [
    h('button', { class: 'primary', i18n: 'action.start',
      onclick: async () => { try { await api.post('/api/server/start'); refreshStatus(); } catch (e) { handleErr(e); } } }),
    h('button', { i18n: 'action.restart',
      onclick: async () => { try { await api.post('/api/server/restart'); refreshStatus(); } catch (e) { handleErr(e); } } }),
  ]));

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

  const metricsHost = h('div');
  root.append(metricsHost);

  // Ring buffers for sparklines. 60 samples @ 5s = 5 minutes of history.
  const cpuHist = [];
  const memHist = [];
  const pushHist = (buf, v) => { buf.push(v); if (buf.length > 60) buf.shift(); };

  const render = (m) => {
    metricsHost.innerHTML = '';
    const s = {
      running: m.running, pid: m.pid, uptime: m.uptime, port: m.port,
    };
    const mods = m.mods || { total: 0, installed: 0, active: 0 };
    const disk = m.diskFreeBytes != null ? bytes(m.diskFreeBytes) : '—';
    const players = m.playerCount != null ? m.playerCount : '—';
    const recent = Array.isArray(m.recentAdm) ? m.recentAdm : [];
    const proc = m.proc || null;
    if (proc) {
      pushHist(cpuHist, proc.cpuPercent || 0);
      pushHist(memHist, proc.memBytes || 0);
    }
    const nextRestart = nextRestartLabel(s.running, s.uptime);

    metricsHost.append(
      h('div', { class: 'grid-4' }, [
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
            h('div', { class: 'k', i18n: 'status.players' }),
            h('div', { text: String(players) }),
            ...(nextRestart ? [
              h('div', { class: 'k', i18n: 'status.nextRestart' }),
              h('div', { text: nextRestart }),
            ] : []),
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
          h('h3', { i18n: 'dashboard.mods' }),
          h('div', { class: 'kv' }, [
            h('div', { class: 'k', i18n: 'dashboard.mods.active' }),
            h('div', { text: String(mods.active) }),
            h('div', { class: 'k', i18n: 'dashboard.mods.installed' }),
            h('div', { text: String(mods.installed) }),
            h('div', { class: 'k', text: 'total' }),
            h('div', { text: String(mods.total) }),
          ]),
          h('div', { class: 'actions' }, [
            h('button', { i18n: 'nav.mods', onclick: () => navigate('mods') }),
          ]),
        ]),
        h('div', { class: 'card' }, [
          h('h3', { i18n: 'dashboard.disk' }),
          h('p', { class: 'stat', text: disk }),
          h('div', { class: 'actions' }, [
            h('button', { i18n: 'nav.validator', onclick: () => navigate('validator') }),
          ]),
        ]),
        h('div', { class: 'card' }, [
          h('h3', { i18n: 'dashboard.process' }),
          h('div', { class: 'metric-num',
            text: proc ? (Math.round(proc.cpuPercent * 10) / 10).toFixed(1) + ' %' : '—' }),
          h('div', { class: 'metric-sub', i18n: 'dashboard.process.cpu' }),
          sparkline(cpuHist, 100),
          h('div', { class: 'metric-num',
            text: proc ? bytes(proc.memBytes) : '—', style: 'margin-top:8px' }),
          h('div', { class: 'metric-sub', i18n: 'dashboard.process.mem' }),
          sparkline(memHist, 0),
        ]),
      ])
    );

    // Recent ADM events strip.
    const events = recent.slice().reverse();
    const list = h('div', { class: 'adm-strip' });
    if (events.length === 0) {
      list.append(h('p', { class: 'hint', i18n: 'admlog.noEvents' }));
    } else {
      for (const e of events) {
        list.append(admEventRow(e));
      }
    }
    metricsHost.append(
      h('div', { class: 'card' }, [
        h('div', { class: 'toolbar' }, [
          h('h3', { i18n: 'dashboard.recentAdm' }),
          h('button', { i18n: 'nav.admlog', onclick: () => navigate('admlog') }),
        ]),
        list,
      ])
    );
  };

  let stopped = false;
  const tick = async () => {
    if (stopped) return;
    try {
      const m = await api.get('/api/dashboard/metrics');
      if (!stopped) render(m);
    } catch {}
  };
  await tick();
  const iv = setInterval(tick, 5000);
  root._teardown = () => { stopped = true; clearInterval(iv); };
};

// parseUptimeSeconds turns the backend's Duration string ("3h17m4s",
// "47m12s", "5s") into seconds. Returns 0 for unparsable input.
function parseUptimeSeconds(s) {
  if (!s || typeof s !== 'string') return 0;
  let total = 0;
  const re = /(\d+)([hms])/g;
  let m;
  while ((m = re.exec(s)) !== null) {
    const n = parseInt(m[1], 10);
    if (m[2] === 'h') total += n * 3600;
    else if (m[2] === 'm') total += n * 60;
    else total += n;
  }
  return total;
}

// nextRestartLabel returns "in HH:MM:SS" / "—" / null based on the active
// auto-restart configuration. Returns null if no schedule is configured at
// all (so the dashboard hides the row entirely).
function nextRestartLabel(running, uptimeStr) {
  const c = State.config || {};
  const times = Array.isArray(c.scheduledRestarts) ? c.scheduledRestarts : [];
  const intervalOn = !!c.autoRestartEnabled && (c.autoRestartSeconds || 0) > 0;
  const hasSchedule = times.length > 0 || intervalOn;
  if (!hasSchedule) return null;
  if (!running) return t('status.notRunning') || '—';

  const candidates = [];
  // Interval-based: AutoRestartSeconds - uptime.
  if (intervalOn) {
    const remaining = c.autoRestartSeconds - parseUptimeSeconds(uptimeStr);
    if (remaining > 0) candidates.push(remaining);
  }
  // HH:MM schedule: pick nearest in the future (today or tomorrow).
  const now = new Date();
  for (const tm of times) {
    const m = /^\s*(\d{1,2}):(\d{2})\s*$/.exec(tm || '');
    if (!m) continue;
    const hh = parseInt(m[1], 10), mm = parseInt(m[2], 10);
    if (hh > 23 || mm > 59) continue;
    const target = new Date(now);
    target.setHours(hh, mm, 0, 0);
    if (target <= now) target.setDate(target.getDate() + 1);
    candidates.push(Math.round((target - now) / 1000));
  }
  if (candidates.length === 0) return '—';
  let s = Math.min(...candidates);
  if (s < 0) s = 0;
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const pad = (n) => String(n).padStart(2, '0');
  return `${pad(h)}:${pad(m)}:${pad(sec)}`;
}

// sparkline renders an SVG polyline from a numeric series.
// maxFixed: if > 0, y-axis is clamped to [0, maxFixed] (use for CPU %);
// if 0, scales to max(data).
function sparkline(series, maxFixed) {
  const W = 180, H = 40, pad = 2;
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', `0 0 ${W} ${H}`);
  svg.setAttribute('preserveAspectRatio', 'none');
  svg.setAttribute('class', 'spark');
  if (!series || series.length === 0) return svg;
  const n = series.length;
  const maxV = maxFixed > 0 ? maxFixed : Math.max(1, ...series);
  const x = (i) => (n === 1 ? W / 2 : pad + (i * (W - 2 * pad)) / (n - 1));
  const y = (v) => {
    const ratio = Math.max(0, Math.min(1, v / maxV));
    return H - pad - ratio * (H - 2 * pad);
  };
  let d = '';
  for (let i = 0; i < n; i++) {
    d += (i === 0 ? 'M' : 'L') + x(i).toFixed(1) + ' ' + y(series[i]).toFixed(1) + ' ';
  }
  const area = `M${x(0).toFixed(1)} ${H - pad} ` +
    series.map((v, i) => 'L' + x(i).toFixed(1) + ' ' + y(v).toFixed(1)).join(' ') +
    ` L${x(n - 1).toFixed(1)} ${H - pad} Z`;
  const areaPath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  areaPath.setAttribute('class', 'spark-area');
  areaPath.setAttribute('d', area);
  const line = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  line.setAttribute('d', d);
  svg.appendChild(areaPath);
  svg.appendChild(line);
  return svg;
}

// openCommandPalette — Ctrl+K quick navigator. Lists all routes plus a few
// always-available actions (start/stop/restart/sync). Filterable by keyword;
// arrow keys move selection, Enter triggers, Esc closes. Reuses .modal-card
// styling but with its own search-result body.
let _commandPaletteOpen = false;
function openCommandPalette() {
  if (_commandPaletteOpen) return;
  _commandPaletteOpen = true;

  const items = [
    { kind: 'route', label: t('nav.dashboard') || 'Dashboard',  route: 'dashboard' },
    { kind: 'route', label: t('nav.server')    || 'Server',     route: 'server' },
    { kind: 'route', label: t('nav.mods')      || 'Mods',       route: 'mods' },
    { kind: 'route', label: t('nav.events')    || 'Events',     route: 'events' },
    { kind: 'route', label: t('nav.types')     || 'Types',      route: 'types' },
    { kind: 'route', label: t('nav.modedTypes')|| 'Custom types', route: 'moded' },
    { kind: 'route', label: t('nav.missionDb') || 'Mission DB', route: 'missiondb' },
    { kind: 'route', label: t('nav.files')     || 'Files',      route: 'files' },
    { kind: 'route', label: t('nav.profiles')  || 'Profiles',   route: 'profiles' },
    { kind: 'route', label: t('nav.battleye')  || 'BattlEye',   route: 'battleye' },
    { kind: 'route', label: t('nav.logs')      || 'Logs',       route: 'logs' },
    { kind: 'route', label: t('nav.admlog')    || 'ADM log',    route: 'admlog' },
    { kind: 'route', label: t('nav.rcon')      || 'RCon',       route: 'rcon' },
    { kind: 'route', label: t('nav.validator') || 'Validator',  route: 'validator' },
    { kind: 'route', label: t('nav.sync')      || 'Sync',       route: 'sync' },
    { kind: 'route', label: t('nav.settings')  || 'Settings',   route: 'settings' },
    { kind: 'action', label: (t('action.start') || 'Start server'),
      run: async () => { try { await api.post('/api/server/start'); toast('starting', 'ok'); refreshStatus(); } catch (e) { handleErr(e); } } },
    { kind: 'action', label: (t('action.stop') || 'Stop server'),
      run: async () => { try { await api.post('/api/server/stop'); toast('stopping', 'ok'); refreshStatus(); } catch (e) { handleErr(e); } } },
    { kind: 'action', label: (t('action.restart') || 'Restart server'),
      run: async () => { try { await api.post('/api/server/restart'); toast('restarting', 'ok'); refreshStatus(); } catch (e) { handleErr(e); } } },
    { kind: 'action', label: (t('action.logout') || 'Logout'),
      run: () => logout() },
    { kind: 'action', label: (t('action.theme.toggle') || 'Toggle theme'),
      run: () => { const cur = (localStorage.getItem('theme') || 'dark'); setTheme(cur === 'dark' ? 'light' : 'dark'); } },
  ];

  const overlay = h('div', { class: 'modal modal-overlay cmdp-overlay' });
  const card = h('div', { class: 'modal-card cmdp' });
  const input = h('input', { type: 'text',
    placeholder: (t('cmdp.placeholder') || 'Type a route or action — ↑↓ Enter Esc'),
    autocomplete: 'off', spellcheck: 'false' });
  const list = h('div', { class: 'cmdp-list' });
  card.append(
    h('div', { class: 'cmdp-input-wrap' }, [input]),
    list,
    h('div', { class: 'cmdp-foot hint' }, [
      h('span', { text: '↑↓ ' }), h('span', { text: t('cmdp.move') || 'navigate' }),
      h('span', { text: '   Enter ' }), h('span', { text: t('cmdp.go') || 'go' }),
      h('span', { text: '   Esc ' }), h('span', { text: t('cmdp.close') || 'close' }),
    ]),
  );
  overlay.append(card);

  let selected = 0;
  let filtered = items.slice();

  function score(label, q) {
    label = label.toLowerCase(); q = q.toLowerCase();
    if (!q) return 1;
    if (label === q) return 1000;
    if (label.startsWith(q)) return 500;
    if (label.includes(q)) return 100;
    // Subsequence fallback so "msd" matches "Mission DB".
    let li = 0; for (const ch of q) {
      const at = label.indexOf(ch, li);
      if (at < 0) return 0;
      li = at + 1;
    }
    return 10;
  }

  function render() {
    list.innerHTML = '';
    if (filtered.length === 0) {
      list.append(h('div', { class: 'cmdp-empty hint', text: t('cmdp.empty') || 'No matches' }));
      return;
    }
    filtered.forEach((it, i) => {
      const row = h('div', { class: 'cmdp-row' + (i === selected ? ' active' : '') }, [
        h('span', { class: 'cmdp-kind ' + it.kind, text: it.kind === 'route' ? '→' : '⚡' }),
        h('span', { class: 'cmdp-label', text: it.label }),
      ]);
      row.onclick = () => { selected = i; trigger(); };
      list.append(row);
    });
  }

  function refilter() {
    const q = input.value.trim();
    filtered = items
      .map(it => ({ it, s: score(it.label, q) }))
      .filter(x => x.s > 0)
      .sort((a, b) => b.s - a.s)
      .map(x => x.it);
    selected = 0;
    render();
  }
  refilter();

  function close() {
    _commandPaletteOpen = false;
    document.removeEventListener('keydown', onKey, true);
    overlay.remove();
  }
  function trigger() {
    const it = filtered[selected];
    if (!it) return;
    close();
    if (it.kind === 'route') navigate(it.route);
    else if (typeof it.run === 'function') Promise.resolve(it.run()).catch(handleErr);
  }
  function onKey(e) {
    if (e.key === 'Escape') { e.preventDefault(); close(); return; }
    if (e.key === 'ArrowDown') { e.preventDefault(); if (selected < filtered.length - 1) { selected++; render(); } return; }
    if (e.key === 'ArrowUp')   { e.preventDefault(); if (selected > 0) { selected--; render(); } return; }
    if (e.key === 'Enter')     { e.preventDefault(); trigger(); return; }
  }
  document.addEventListener('keydown', onKey, true);
  overlay.addEventListener('click', e => { if (e.target === overlay) close(); });
  input.addEventListener('input', refilter);

  document.body.append(overlay);
  setTimeout(() => input.focus(), 0);
}

// pageHeader renders the standard route header — large title + optional
// subtitle + right-aligned action buttons. Use as the first element appended
// to a view's `root` so every page reads consistently.
function pageHeader(titleKey, subKey, actions) {
  const left = h('div', {}, [h('h1', { i18n: titleKey })]);
  if (subKey) left.append(h('div', { class: 'page-sub', i18n: subKey }));
  const right = h('div', { class: 'page-actions' }, actions || []);
  return h('div', { class: 'page-header' }, [left, right]);
}

// openModal renders a content-agnostic modal overlay with a close button.
// Returns { close, body } so callers can build their UI inside `body` and
// dismiss programmatically. Click on backdrop or Escape both close. The
// caller's onClose callback (if provided) runs after the DOM is removed.
function openModal({ title, onClose, wide } = {}) {
  const body = h('div', { class: 'modal-body' });
  const closeBtn = h('button', { class: 'modal-close', text: '×',
    'aria-label': 'Close', onclick: () => close() });
  const card = h('div', { class: 'modal-card' + (wide ? ' wide' : '') }, [
    h('div', { class: 'modal-header' }, [
      h('h3', { text: title || '' }),
      closeBtn,
    ]),
    body,
  ]);
  const overlay = h('div', { class: 'modal modal-overlay' }, card);
  overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });
  const onKey = (e) => { if (e.key === 'Escape') close(); };
  document.addEventListener('keydown', onKey);
  function close() {
    document.removeEventListener('keydown', onKey);
    overlay.remove();
    if (onClose) try { onClose(); } catch {}
  }
  document.body.append(overlay);
  return { body, close };
}

function admEventRow(e) {
  const typeClass = 'adm-type adm-' + (e.type || 'other');
  const pieces = [
    h('span', { class: 'adm-time', text: e.time || '' }),
    h('span', { class: typeClass, text: e.type || '' }),
  ];
  if (e.player) pieces.push(h('span', { class: 'adm-player', text: e.player }));
  if (e.type === 'kill' || e.type === 'hit') {
    if (e.target) pieces.push(h('span', { class: 'adm-arrow', text: '→' }));
    if (e.target) pieces.push(h('span', { class: 'adm-player', text: e.target }));
    const meta = [];
    if (e.weapon) meta.push(e.weapon);
    if (e.distance) meta.push(e.distance + 'm');
    if (meta.length) pieces.push(h('span', { class: 'adm-meta', text: '(' + meta.join(', ') + ')' }));
  } else if (e.type === 'chat' && e.message) {
    pieces.push(h('span', { class: 'adm-msg', text: e.message }));
  } else if (e.message) {
    pieces.push(h('span', { class: 'adm-meta', text: e.message }));
  }
  return h('div', { class: 'adm-row' }, pieces);
}

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

  root.append(pageHeader('nav.server', 'server.subtitle'));
  if (State.serverStatus.running) root.append(runningBanner());
  root.append(
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
  root.append(pageHeader('nav.mods', 'mods.subtitle'));
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
      actions.append(h('button', { i18n: 'modtypes.scan',
        onclick: async () => {
          try {
            const s = await api.get('/api/mods/scan-types?mod=' + encodeURIComponent(m.name));
            const files = s.files || [];
            if (!files.length) { toast(t('modtypes.none')); return; }
            await openModTypesDialog(m.name, files);
          } catch (e) { handleErr(e); }
        }}));
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
    const tr = h('tr', {}, [
      nameCell,
      h('td', {}, statusBadge),
      h('td', {}, m.availableInWorkshop ? h('span', { class: 'badge ok', text: '✓' }) : h('span', { class: 'badge mute', text: '—' })),
      h('td', { text: m.keyCount }),
      h('td', { text: bytes(m.sizeBytes) }),
      h('td', {}, activeCb),
      h('td', {}, actions),
    ]);
    if (m.publishedId) tr.dataset.published = m.publishedId;
    tbody.append(tr);
  }
  tbl.append(tbody);

  // Apply Steam-side staleness to rows by publishedId. Adds a second status
  // line ("Steam: up-to-date" / "Steam: outdated by 3d") under each name cell.
  const applySteamCheck = (results) => {
    const byId = {};
    for (const r of results) byId[r.publishedId] = r;
    const trs = tbody.querySelectorAll('tr[data-published]');
    let outdated = 0;
    trs.forEach(tr => {
      const id = tr.dataset.published;
      const r = byId[id];
      const name = tr.firstChild;
      // Strip any prior Steam row.
      name.querySelectorAll('.steam-row').forEach(n => n.remove());
      if (!r) return;
      let cls = 'mute', label = t('mods.steam.unknown') || 'Steam: unknown';
      if (r.status === 'outdated') {
        cls = 'warn';
        outdated++;
        const ms = new Date(r.remoteUpdated).getTime() - new Date(r.localUpdated || 0).getTime();
        const days = Math.max(1, Math.round(ms / (24 * 3600 * 1000)));
        label = (t('mods.steam.outdated') || 'Steam: outdated') + ` (~${days}d)`;
      } else if (r.status === 'ok') {
        cls = 'ok'; label = t('mods.steam.ok') || 'Steam: up-to-date';
      } else if (r.status === 'missing') {
        cls = 'err'; label = t('mods.steam.missing') || 'Steam: removed/private';
      }
      name.appendChild(h('div', { class: 'hint steam-row' },
        h('span', { class: 'badge ' + cls, text: label })));
    });
    return outdated;
  };

  const toolbar = h('div', { class: 'toolbar' }, [
    h('button', { i18n: 'action.syncKeys',
      onclick: async () => { try { await api.post('/api/mods/sync-keys'); toast('keys synced','ok'); } catch (e) { handleErr(e); } }
    }),
    h('button', { i18n: 'mods.checkSteam',
      onclick: async (e) => {
        const btn = e.currentTarget;
        const original = btn.textContent;
        btn.disabled = true; btn.textContent = (t('mods.checkingSteam') || 'Checking…');
        try {
          const r = await api.post('/api/mods/check-updates', {});
          const out = applySteamCheck(r.results || []);
          if (r.error) toast(r.error, 'error');
          else toast((t('mods.checkSteam.done') || 'Checked: ') + (out > 0 ? `${out} outdated` : 'all up-to-date'), out > 0 ? 'warn' : 'ok');
        } catch (err) { handleErr(err); }
        finally { btn.disabled = false; btn.textContent = original; }
      }
    }),
    h('button', { class: 'primary', i18n: 'mods.syncAll',
      onclick: () => openSyncAllPicker(d),
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

  // Workshop Collection URL importer. Users paste a
  // `steamcommunity.com/.../?id=...` link, we fetch the public collection
  // page and match child IDs against the !Workshop folder by meta.cpp.
  const collectionInput = h('input', { type: 'text', placeholder: 'https://steamcommunity.com/sharedfiles/filedetails/?id=...' });
  const collectionResults = h('div', { class: 'hint', style: { marginTop: '8px' } });
  const collectionCard = h('div', { class: 'card' }, [
    h('h3', { i18n: 'mods.collection.title' }),
    h('p', { class: 'hint', i18n: 'mods.collection.hint' }),
    h('div', { class: 'toolbar' }, [
      collectionInput,
      h('button', { class: 'primary', i18n: 'mods.collection.resolve',
        onclick: async () => {
          const url = collectionInput.value.trim();
          if (!url) return;
          try {
            const r = await api.post('/api/mods/collection/resolve', { url, save: true });
            collectionResults.innerHTML = '';
            const ok = r.resolved || [];
            const missing = r.missing || [];
            collectionResults.append(
              h('div', { text: `Collection #${r.collectionId}: ${ok.length} resolved, ${missing.length} missing` }),
            );
            if (ok.length) {
              const list = h('ul');
              for (const m of ok) list.append(h('li', { text: `${m.modName}${m.displayName ? ' — ' + m.displayName : ''} (id ${m.publishedId})` }));
              collectionResults.append(list);
              const activate = h('button', { class: 'primary', style: { marginTop: '8px' }, text: t('mods.collection.activate'),
                onclick: async () => {
                  for (const m of ok) {
                    try { await api.post('/api/mods/enable', { mod: m.modName, enabled: true }); } catch {}
                  }
                  toast('activated', 'ok');
                  await navigate('mods');
                }
              });
              collectionResults.append(activate);
            }
            if (missing.length) {
              const dl = h('div', { style: { marginTop: '8px' } });
              dl.append(h('div', { text: t('mods.collection.missing') }));
              for (const id of missing) {
                dl.append(h('div', {}, h('a', {
                  href: `https://steamcommunity.com/sharedfiles/filedetails/?id=${id}`,
                  target: '_blank', rel: 'noopener', text: `subscribe id ${id}`,
                })));
              }
              collectionResults.append(dl);
            }
          } catch (e) { handleErr(e); }
        }
      }),
    ]),
    collectionResults,
  ]);

  wrap.append(h('div', { class: 'card' }, [
    h('h2', { i18n: 'mods.title' }),
    toolbar,
    tbl,
  ]));
  wrap.append(collectionCard);

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
  const pagerHost = h('div');

  // Pagination state. Page size kept conservative so large types.xml files
  // (5k+ entries) render snappily. Search resets to page 1.
  const PAGE_SIZE = 200;
  let currentPage = 1;
  let lastFilteredCount = 0;
  let lastTotalCount = 0;

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

  // Bulk-edit modal — opened from the toolbar when at least one row is
  // selected. Scalar fields only; per-type editor handles usages/values/tags.
  function openBulkEdit() {
    const selected = [...tableWrap.querySelectorAll('input[type=checkbox]:checked')]
      .map(cb => cb.dataset.name);
    if (!selected.length) { toast('select types first'); return; }
    const fields = {
      nominal: h('input', { type: 'number', placeholder: 'nominal' }),
      min: h('input', { type: 'number', placeholder: 'min' }),
      lifetime: h('input', { type: 'number', placeholder: 'lifetime' }),
      restock: h('input', { type: 'number', placeholder: 'restock' }),
      quantmin: h('input', { type: 'number', placeholder: 'quantmin' }),
      quantmax: h('input', { type: 'number', placeholder: 'quantmax' }),
      cost: h('input', { type: 'number', placeholder: 'cost' }),
      category: h('input', { type: 'text', placeholder: 'category' }),
    };
    const grid = h('div', { class: 'grid-3' });
    for (const [k, el] of Object.entries(fields)) {
      grid.append(h('div', {}, [h('label', { text: k }), el]));
    }
    const m = openModal({ title: t('types.bulk') + ` — ${selected.length} selected`, wide: true });
    m.body.append(
      h('p', { class: 'hint', i18n: 'types.bulk.hint' }),
      grid,
      h('div', { class: 'actions' }, [
        h('button', { class: 'primary', i18n: 'types.bulk.apply',
          onclick: async () => {
            const patch = {};
            for (const [k, el] of Object.entries(fields)) {
              const v = el.value.trim();
              if (v === '') continue;
              if (k === 'category') patch.category = v;
              else patch[k] = Number(v);
            }
            if (Object.keys(patch).length === 0) { toast('set at least one field'); return; }
            try {
              const r = await api.post('/api/types/bulk-patch', {
                file: fileSelect.value, names: selected, patch,
              });
              toast(`patched ${r.touched} type(s)`, 'ok');
              m.close();
              await refreshTable();
            } catch (e) { handleErr(e); }
          }
        }),
        h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
      ]),
    );
  }

  async function refreshTable() {
    tableWrap.innerHTML = '';
    pagerHost.innerHTML = '';
    let d;
    try { d = await api.get(`/api/types?file=${encodeURIComponent(fileSelect.value)}`); }
    catch (e) { tableWrap.append(h('p', { text: e.message })); return; }

    const q = search.value.toLowerCase();
    const filtered = q ? d.types.filter(r => r.name.toLowerCase().includes(q)) : d.types;
    lastFilteredCount = filtered.length;
    lastTotalCount = d.count;

    const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
    if (currentPage > totalPages) currentPage = totalPages;
    if (currentPage < 1) currentPage = 1;
    const start = (currentPage - 1) * PAGE_SIZE;
    const slice = filtered.slice(start, start + PAGE_SIZE);

    const tbl = h('table');
    const headCb = h('input', { type: 'checkbox', title: 'Select page' });
    headCb.onchange = () => {
      tableWrap.querySelectorAll('input[type=checkbox][data-name]').forEach(cb => cb.checked = headCb.checked);
    };
    tbl.append(h('thead', {}, h('tr', {}, [
      h('th', {}, headCb),
      h('th', { text: 'Name' }),
      h('th', { class: 'num', i18n: 'types.field.nominal' }),
      h('th', { class: 'num', i18n: 'types.field.min' }),
      h('th', { class: 'num', i18n: 'types.field.lifetime' }),
      h('th', { i18n: 'types.field.category' }),
      h('th', { text: '' }),
    ])));
    const tbody = h('tbody');
    for (const row of slice) {
      const cb = h('input', { type: 'checkbox' });
      cb.dataset.name = row.name;
      tbody.append(h('tr', {}, [
        h('td', {}, cb),
        h('td', { text: row.name }),
        h('td', { class: 'num', text: row.nominal ?? '' }),
        h('td', { class: 'num', text: row.min ?? '' }),
        h('td', { class: 'num', text: row.lifetime ?? '' }),
        h('td', { text: row.category || '' }),
        h('td', {}, h('button', { i18n: 'action.edit',
          onclick: () => openEditor(row.name)
        })),
      ]));
    }
    tbl.append(tbody);
    tableWrap.append(tbl);

    // Pager: prev / page X of Y / next + jump-to.
    const prev = h('button', { text: '←', onclick: () => { currentPage--; refreshTable(); } });
    const next = h('button', { text: '→', onclick: () => { currentPage++; refreshTable(); } });
    if (currentPage <= 1) prev.disabled = true;
    if (currentPage >= totalPages) next.disabled = true;
    const jump = h('input', { type: 'number', value: currentPage, min: 1, max: totalPages });
    jump.onchange = () => {
      const n = parseInt(jump.value, 10);
      if (Number.isFinite(n) && n >= 1 && n <= totalPages) { currentPage = n; refreshTable(); }
    };
    pagerHost.append(h('div', { class: 'pager' }, [
      prev, next,
      h('span', { class: 'pager-num',
        text: `${start + 1}–${Math.min(start + PAGE_SIZE, filtered.length)} / ${filtered.length}${q ? ` (of ${d.count})` : ''}` }),
      h('span', { class: 'spacer' }),
      h('span', { class: 'hint', text: t('pager.jump') || 'Page' }),
      jump,
      h('span', { class: 'hint', text: `/ ${totalPages}` }),
    ]));
  }

  async function openEditor(name) {
    let t;
    try {
      t = await api.get(`/api/types/item?file=${encodeURIComponent(fileSelect.value)}&name=${encodeURIComponent(name)}`);
    } catch (e) { handleErr(e); return; }
    const m = openModal({ title: name, wide: true });
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

    m.body.append(
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
              m.close();
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
              m.close();
              await refreshTable();
            } catch (e) { handleErr(e); }
          }
        }),
        h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
      ]),
    );
  }

  fileSelect.onchange = () => { currentPage = 1; refreshTable(); };
  search.oninput = () => { currentPage = 1; refreshTable(); };

  if (State.serverStatus.running) root.append(runningBanner());
  root.append(
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'types.title' }),
      h('div', { class: 'toolbar' }, [
        fileSelect, search,
        h('button', { i18n: 'types.bulk', onclick: openBulkEdit }),
      ]),
      tableWrap,
      pagerHost,
    ]),
    presetsWrap,
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

  root.append(pageHeader('nav.modedTypes', 'moded.subtitle'));
  if (State.serverStatus.running) root.append(runningBanner());
  root.append(
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'moded.title' }),
      h('p', { class: 'hint', text: d.folder }),
      table,
    ]),
    h('div', { class: 'card' }, [
      h('h3', { i18n: 'moded.import' }),
      h('p', { class: 'hint', i18n: 'moded.import.hint' }),
      h('div', { class: 'actions' }, [
        h('button', { class: 'primary', i18n: 'moded.import.pick',
          onclick: () => openImportFromMod() }),
      ]),
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
    ]),
  );

  // openImportFromMod: pick an installed @Mod, scan it for *_types.xml files,
  // pick one, install. Reuses /api/mods/scan-types and /api/mods/install-types
  // — same flow as the per-mod button on Mods page, just discoverable from
  // the ModedTypes view.
  async function openImportFromMod() {
    let mods;
    try { mods = await api.get('/api/mods'); }
    catch (e) { handleErr(e); return; }
    const installed = (mods.mods || []).filter(m => m.installedInServer);
    if (!installed.length) {
      toast(t('moded.import.noMods') || 'No mods installed', 'error');
      return;
    }
    const m = openModal({ title: t('moded.import') || 'Import types from mod', wide: true });
    const list = h('div', { class: 'mod-pick-list' });
    for (const mod of installed) {
      list.append(h('button', { class: 'mod-pick',
        onclick: async () => {
          try {
            const r = await api.get('/api/mods/scan-types?mod=' + encodeURIComponent(mod.name));
            const files = r.files || [];
            if (!files.length) { toast(t('modtypes.none')); return; }
            m.close();
            await openModTypesDialog(mod.name, files);
            await navigate('moded');
          } catch (e) { handleErr(e); }
        },
      }, [
        h('div', { text: mod.name, style: { fontWeight: '600' } }),
        mod.displayName ? h('div', { class: 'hint', text: mod.displayName }) : null,
      ]));
    }
    m.body.append(list);
    m.body.append(h('div', { class: 'actions' }, [
      h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
    ]));
  }
};

// --------------------------------------------------------------------- files

Views.files = async (root) => {
  const tree = h('div', { class: 'tree' });
  const editorHost = h('textarea', { placeholder: 'Select a file' });
  const pathLabel = h('div', { class: 'hint' });
  const backupList = h('div', { class: 'card', style: { marginTop: '12px' } });
  let currentPath = '';
  let cm = null;

  const getValue = () => cm ? cm.getValue() : editorHost.value;
  const setValue = (v) => { if (cm) cm.setValue(v || ''); else editorHost.value = v || ''; };
  const setMode = (path) => { if (cm) cm.setOption('mode', CM.modeFor(path)); };

  async function refreshBackups() {
    backupList.innerHTML = '';
    if (!currentPath) return;
    backupList.append(h('h3', { i18n: 'backup.title' }));
    try {
      const d = await api.get('/api/backups/list?path=' + encodeURIComponent(currentPath));
      const baks = d.backups || [];
      if (!baks.length) {
        backupList.append(h('p', { class: 'hint', i18n: 'backup.none' }));
        return;
      }
      for (const b of baks) {
        const when = new Date(b.time * 1000).toLocaleString();
        backupList.append(h('div', { class: 'row', style: { padding: '4px 0' } }, [
          h('span', { style: { flex: '1' }, text: b.name }),
          h('small', { class: 'hint', text: `${when} · ${bytes(b.size)}` }),
          h('button', { i18n: 'backup.restore', onclick: async () => {
            try {
              await api.post('/api/backups/restore', { path: currentPath, backup: b.name });
              const r = await api.get('/api/files/read?path=' + encodeURIComponent(currentPath));
              setValue(r.content);
              toast(t('backup.restored'), 'ok');
              refreshBackups();
            } catch (e) { handleErr(e); }
          }}),
        ]));
      }
    } catch (e) { handleErr(e); }
  }

  async function loadDir(path) {
    const d = await api.get(`/api/files/tree?path=${encodeURIComponent(path || '')}`);
    tree.innerHTML = '';
    if (path) {
      tree.append(h('div', { class: 'node dir', text: '⬆ ..',
        onclick: () => loadDir(path.split('/').slice(0, -1).join('/')) }));
    }
    for (const e of d.entries) {
      if (/\.bak\.\d/.test(e.name)) continue; // hide .bak.* clutter from the tree
      const node = h('div', { class: `node ${e.isDir ? 'dir' : ''}`,
        text: `${e.isDir ? '📁' : '📄'} ${e.name}`,
      });
      node.onclick = async () => {
        if (e.isDir) return loadDir(e.path);
        const r = await api.get(`/api/files/read?path=${encodeURIComponent(e.path)}`);
        currentPath = e.path;
        setMode(e.name);
        setValue(r.content);
        pathLabel.textContent = e.path;
        await refreshBackups();
      };
      tree.append(node);
    }
  }
  try { await loadDir(''); } catch (e) { handleErr(e); }

  if (State.serverStatus.running) root.append(runningBanner());
  root.append(
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'files.title' }),
      h('div', { class: 'grid-2' }, [
        h('div', {}, [h('h3', { i18n: 'files.tree' }), tree]),
        h('div', {}, [
          h('h3', { i18n: 'files.editor' }),
          pathLabel,
          editorHost,
          h('div', { class: 'actions' }, [
            h('button', { class: 'primary', i18n: 'files.save', title: 'Ctrl+S',
              onclick: () => doSave(),
            }),
          ]),
          backupList,
        ]),
      ]),
    ])
  );

  async function doSave() {
    if (!currentPath) return;
    try {
      await api.post('/api/files/write', { path: currentPath, content: getValue() });
      toast(t('files.save'), 'ok');
      await refreshBackups();
    } catch (e) { handleErr(e); }
  }
  root._save = doSave;

  try {
    cm = await CM.mount(editorHost, { mode: null });
    root._teardown = () => { try { cm && cm.toTextArea(); } catch {} };
  } catch (e) {
    // CM failed to load — textarea stays functional as a fallback.
    console.warn('CodeMirror load failed', e);
  }
};

// openSyncAllPicker — interactive modal for the "Sync all" flow. Shows
// every Workshop @Mod with its current state ("Not installed" / "Update
// available" / "Up to date"), pre-selects the actionable ones, and lets
// the user trim the list before applying. Surfaces a clear message when
// the !Workshop folder is empty so the user can spot a misconfigured
// vanilla path immediately.
function openSyncAllPicker(d) {
  const workshopMods = (d.mods || []).filter(m => m.availableInWorkshop);
  const m = openModal({ title: t('mods.syncAll') || 'Sync all mods', wide: true });

  if (workshopMods.length === 0) {
    m.body.append(
      h('p', { class: 'hint',
        text: (t('mods.syncAll.empty') || 'No mods found in !Workshop. Vanilla path:') + ' ' + (d.vanillaPath || '—') }),
      h('p', { class: 'hint', i18n: 'mods.syncAll.empty.hint' }),
      h('div', { class: 'actions' }, [
        h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
      ]),
    );
    return;
  }

  // Counts before sync, then individual rows with checkboxes.
  let cNot = 0, cOut = 0, cOk = 0;
  const checkboxes = [];
  const list = h('div', { class: 'sync-pick-list' });
  for (const mod of workshopMods) {
    let label, cls, preSelect = false;
    if (!mod.installedInServer)      { label = t('mods.notInstalled')   || 'Not installed';   cls = 'warn'; preSelect = true; cNot++; }
    else if (mod.updateAvailable)    { label = t('mods.updateAvailable')|| 'Update available'; cls = 'warn'; preSelect = true; cOut++; }
    else                             { label = t('mods.upToDate')       || 'Up to date';      cls = 'ok'; cOk++; }
    const cb = h('input', { type: 'checkbox' });
    cb.checked = preSelect;
    cb.dataset.name = mod.name;
    checkboxes.push(cb);
    list.append(h('label', { class: 'sync-pick-row' }, [
      cb,
      h('div', { class: 'sync-pick-name' }, [
        h('div', { text: mod.name, style: { fontWeight: '600' } }),
        mod.displayName ? h('div', { class: 'hint', text: mod.displayName }) : null,
      ]),
      h('span', { class: 'badge ' + cls, text: label }),
    ]));
  }

  const summary = h('p', { class: 'hint',
    text: `${workshopMods.length} ${(t('mods.foundInWorkshop') || 'in !Workshop')} · ` +
          `${cNot} ${t('mods.notInstalled') || 'not installed'} · ` +
          `${cOut} ${t('mods.updateAvailable') || 'updates'} · ` +
          `${cOk} ${t('mods.upToDate') || 'up-to-date'}` });

  const setAll = (val) => checkboxes.forEach(cb => cb.checked = val);
  m.body.append(
    summary,
    h('div', { class: 'toolbar' }, [
      h('button', { i18n: 'mods.pick.actionable', onclick: () => {
        // Re-select only what's actionable (not installed OR update available).
        for (const cb of checkboxes) {
          const row = (d.mods || []).find(x => x.name === cb.dataset.name);
          cb.checked = row && (!row.installedInServer || row.updateAvailable);
        }
      }}),
      h('button', { i18n: 'mods.pick.all',  onclick: () => setAll(true) }),
      h('button', { i18n: 'mods.pick.none', onclick: () => setAll(false) }),
    ]),
    list,
    h('div', { class: 'actions' }, [
      h('button', { class: 'primary', i18n: 'mods.pick.apply', onclick: async () => {
        const only = checkboxes.filter(cb => cb.checked).map(cb => cb.dataset.name);
        if (only.length === 0) { toast(t('mods.pick.empty') || 'Nothing selected', 'warn'); return; }
        try {
          const r = await api.post('/api/mods/sync-all', { only });
          const parts = [];
          if (r.installed?.length) parts.push(`+${r.installed.length} ${t('mods.installedShort') || 'installed'}`);
          if (r.updated?.length)   parts.push(`~${r.updated.length} ${t('mods.updatedShort') || 'updated'}`);
          if (r.skipped?.length)   parts.push(`${r.skipped.length} ${t('mods.upToDate') || 'up-to-date'}`);
          toast(parts.join(', ') || (t('mods.syncAll.done') || 'Done'), 'ok');
          m.close();
          await navigate('mods');
        } catch (e) { handleErr(e); }
      }}),
      h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
    ]),
  );
}

// Inline modal-ish dialog that lists candidate types.xml inside a mod and
// lets the user install each one into the active mission with a chosen name.
async function openModTypesDialog(mod, files) {
  const m = openModal({ title: `${mod} — XML candidates`, wide: true });
  if (!files.length) {
    m.body.append(h('p', { class: 'hint', i18n: 'modtypes.none' }));
    m.body.append(h('div', { class: 'actions' }, [
      h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
    ]));
    return;
  }
  m.body.append(h('p', { class: 'hint', i18n: 'modtypes.dialog.hint' }));
  for (const f of files) {
    const nameInput = h('input', { type: 'text',
      value: mod.replace(/^@/, '').replace(/\s+/g, '_') + '_' + f.rel.split('/').pop() });
    const btn = h('button', { class: 'primary', i18n: 'modtypes.install', onclick: async () => {
      try {
        await api.post('/api/mods/install-types', { mod, rel: f.rel, fileName: nameInput.value });
        toast(t('modtypes.installed'), 'ok');
        btn.disabled = true;
      } catch (e) { handleErr(e); }
    }});
    const tag = f.kind === 'types'
      ? h('span', { class: 'badge ok',  text: `<types> · ${f.types}` })
      : (f.kind === 'json'
          ? h('span', { class: 'badge mute', text: 'json' })
          : h('span', { class: 'badge mute', text: 'xml' }));
    m.body.append(h('div', { class: 'modxml-row' }, [
      h('div', { class: 'modxml-meta' }, [
        h('code', { text: f.rel }),
        tag,
        h('span', { class: 'hint', text: bytes(f.size || 0) }),
      ]),
      h('div', { class: 'row', style: { gap: '8px', marginTop: '6px' } }, [nameInput, btn]),
    ]));
  }
  m.body.append(h('div', { class: 'actions' }, [
    h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
  ]));
}

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
  const paused = h('input', { type: 'checkbox' });

  let source;
  const MAX_CHARS = 200_000;

  // Buffer incoming chunks and flush at most every 200 ms so a chatty RPT
  // does not produce hundreds of DOM mutations per second (which is what
  // froze the panel before).
  let pending = '';
  let flushScheduled = false;
  function scheduleFlush() {
    if (flushScheduled || paused.checked) return;
    flushScheduled = true;
    setTimeout(() => {
      flushScheduled = false;
      if (!pending) return;
      let next = pane.textContent + pending;
      pending = '';
      if (next.length > MAX_CHARS) next = next.slice(-MAX_CHARS);
      pane.textContent = next;
      if (autoScroll.checked) pane.scrollTop = pane.scrollHeight;
    }, 200);
  }
  function append(text) {
    pending += text;
    if (pending.length > MAX_CHARS) pending = pending.slice(-MAX_CHARS);
    scheduleFlush();
  }

  async function attach(id) {
    if (source) { source.close(); source = null; }
    pending = '';
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
        h('label', {}, [autoScroll, h('span', { text: ' autoscroll' })]),
        h('label', {}, [paused, h('span', { text: ' pause' })]),
        h('button', { text: 'Clear', onclick: () => { pane.textContent = ''; pending = ''; } }),
        h('span', { class: 'spacer' }),
        h('small', { class: 'hint', text: 'Tail capped at 200 KB on screen' }),
      ]),
      pane,
    ]),
  );

  if (sources.length) await attach(sources[0].id);

  // Tear down EventSource on navigation away.
  root._teardown = () => { if (source) source.close(); };
};

// --------------------------------------------------------------------- admlog

Views.admlog = async (root) => {
  const typeSel = h('select', {});
  const types = ['all','connect','disconnect','kill','hit','chat','death','other'];
  for (const tp of types) {
    typeSel.append(h('option', { value: tp === 'all' ? '' : tp, i18n: `admlog.type.${tp}` }));
  }
  const playerInp = h('input', { type: 'text', placeholder: t('admlog.player') });
  const reloadBtn = h('button', { i18n: 'action.reload' });
  const pathLbl = h('small', { class: 'hint' });
  const list = h('div', { class: 'adm-list' });

  async function load() {
    list.innerHTML = '';
    const q = new URLSearchParams();
    if (typeSel.value) q.set('type', typeSel.value);
    if (playerInp.value.trim()) q.set('player', playerInp.value.trim());
    q.set('limit', '500');
    try {
      const r = await api.get('/api/admlog/recent?' + q.toString());
      pathLbl.textContent = r.path || '';
      if (!r.path) { list.append(h('p', { class: 'hint', i18n: 'admlog.noFile' })); return; }
      if (!r.events || r.events.length === 0) {
        list.append(h('p', { class: 'hint', i18n: 'admlog.noEvents' }));
        return;
      }
      for (const e of r.events.slice().reverse()) list.append(admEventRow(e));
    } catch (err) { handleErr(err); }
  }

  reloadBtn.onclick = load;
  typeSel.onchange = load;
  playerInp.onkeydown = (e) => { if (e.key === 'Enter') load(); };

  root.append(
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'admlog.title' }),
      h('p', { class: 'hint', i18n: 'admlog.hint' }),
      h('div', { class: 'toolbar' }, [typeSel, playerInp, reloadBtn, pathLbl]),
      list,
    ])
  );
  await load();
};

// --------------------------------------------------------------------- rcon

Views.rcon = async (root) => {
  const card = h('div', { class: 'card' }, [h('h2', { i18n: 'nav.rcon' })]);
  const status = h('div', { class: 'hint' });
  const setupHost = h('div');
  const players = h('div', { class: 'players-grid' });
  const sayInp = h('input', { type: 'text', placeholder: 'Broadcast message' });
  const cmdInp = h('input', { type: 'text', placeholder: 'Raw RCon command, e.g. #shutdown' });
  const cmdOut = h('pre', { class: 'log-pane', style: { height: '180px' } });

  function renderSetup(message) {
    setupHost.innerHTML = '';
    const passInp = h('input', { type: 'password', placeholder: 'RCon password', style: { flex: '1' } });
    const portInp = h('input', { type: 'number', placeholder: 'Port (auto)', style: { width: '120px' } });
    setupHost.append(
      h('div', { class: 'card', style: { borderColor: 'var(--warn)' } }, [
        h('h3', { i18n: 'rcon.notConfigured' }),
        h('p', { class: 'hint', text: message || t('rcon.notConfigured.hint') }),
        h('div', { class: 'row', style: { gap: '8px' } }, [
          passInp, portInp,
          h('button', { class: 'primary', i18n: 'action.save', onclick: async () => {
            try {
              const cur = await api.get('/api/config');
              const next = { ...cur };
              next.rconPassword = passInp.value.trim();
              const p = parseInt(portInp.value, 10);
              if (Number.isFinite(p) && p > 0) next.rconPort = p;
              await api.post('/api/config', next);
              State.config = next;
              setupHost.innerHTML = '';
              await refresh();
            } catch (e) { handleErr(e); }
          }}),
        ]),
      ]),
    );
  }

  async function refresh() {
    status.textContent = '';
    setupHost.innerHTML = '';
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
      const msg = e.message || '';
      if (/not configured|not running|connect: connection refused|password/i.test(msg)) {
        renderSetup(msg);
      } else {
        status.textContent = `RCon: ${msg}`;
      }
    }
  }

  card.append(
    h('div', { class: 'actions' }, [
      h('button', { text: 'Refresh', onclick: refresh }),
    ]),
    status,
    setupHost,
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
  const pagerHost = h('div');
  const PAGE_SIZE = 200;
  let currentPage = 1;

  const NUM_FIELDS = ['nominal','min','max','lifetime','restock','saveable','active'];

  async function refreshTable() {
    tableWrap.innerHTML = '';
    pagerHost.innerHTML = '';
    let d;
    try { d = await api.get('/api/events'); }
    catch (e) { tableWrap.append(h('p', { text: e.message })); return; }
    const q = search.value.toLowerCase();
    const filtered = q ? d.events.filter(r => r.name.toLowerCase().includes(q)) : d.events;
    const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
    if (currentPage > totalPages) currentPage = totalPages;
    if (currentPage < 1) currentPage = 1;
    const start = (currentPage - 1) * PAGE_SIZE;
    const slice = filtered.slice(start, start + PAGE_SIZE);

    const tbl = h('table');
    tbl.append(h('thead', {}, h('tr', {}, [
      h('th', { text: 'Name' }),
      h('th', { class: 'num', i18n: 'events.field.nominal' }),
      h('th', { class: 'num', i18n: 'events.field.min' }),
      h('th', { class: 'num', i18n: 'events.field.max' }),
      h('th', { class: 'num', i18n: 'events.field.lifetime' }),
      h('th', { i18n: 'events.field.active' }),
      h('th', { class: 'num', i18n: 'events.field.children' }),
      h('th', { text: '' }),
    ])));
    const tbody = h('tbody');
    for (const row of slice) {
      tbody.append(h('tr', {}, [
        h('td', { text: row.name }),
        h('td', { class: 'num', text: row.nominal ?? '' }),
        h('td', { class: 'num', text: row.min ?? '' }),
        h('td', { class: 'num', text: row.max ?? '' }),
        h('td', { class: 'num', text: row.lifetime ?? '' }),
        h('td', {}, row.active ? h('span', { class: 'badge ok', text: '✓' }) : h('span', { class: 'badge mute', text: '—' })),
        h('td', { class: 'num', text: row.children || 0 }),
        h('td', {}, h('button', { i18n: 'action.edit', onclick: () => openEditor(row.name) })),
      ]));
    }
    tbl.append(tbody);
    tableWrap.append(tbl);

    const prev = h('button', { text: '←', onclick: () => { currentPage--; refreshTable(); } });
    const next = h('button', { text: '→', onclick: () => { currentPage++; refreshTable(); } });
    if (currentPage <= 1) prev.disabled = true;
    if (currentPage >= totalPages) next.disabled = true;
    const jump = h('input', { type: 'number', value: currentPage, min: 1, max: totalPages });
    jump.onchange = () => {
      const n = parseInt(jump.value, 10);
      if (Number.isFinite(n) && n >= 1 && n <= totalPages) { currentPage = n; refreshTable(); }
    };
    pagerHost.append(h('div', { class: 'pager' }, [
      prev, next,
      h('span', { class: 'pager-num',
        text: `${start + 1}–${Math.min(start + PAGE_SIZE, filtered.length)} / ${filtered.length}${q ? ` (of ${d.count})` : ''}` }),
      h('span', { class: 'spacer' }),
      h('span', { class: 'hint', text: t('pager.jump') || 'Page' }),
      jump,
      h('span', { class: 'hint', text: `/ ${totalPages}` }),
    ]));
  }

  async function openEditor(name) {
    let ev;
    try { ev = await api.get(`/api/events/item?name=${encodeURIComponent(name)}`); }
    catch (e) { handleErr(e); return; }
    const m = openModal({ title: name, wide: true });

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

    m.body.append(
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
              const field = k.charAt(0).toUpperCase() + k.slice(1);
              body[field] = Number(v);
            }
            if (children.length) body.Children = { Child: children };
            try {
              await api.post(`/api/events/item?name=${encodeURIComponent(name)}`, body);
              toast('saved', 'ok');
              m.close();
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
              m.close();
              await refreshTable();
            } catch (e) { handleErr(e); }
          }
        }),
        h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
      ]),
    );
    applyI18n();
  }

  search.oninput = () => { currentPage = 1; refreshTable(); };

  if (State.serverStatus.running) root.append(runningBanner());
  root.append(
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'events.title' }),
      h('p', { class: 'hint', i18n: 'events.hint' }),
      h('div', { class: 'toolbar' }, [search]),
      tableWrap,
      pagerHost,
    ]),
  );
  await refreshTable();
};

// --------------------------------------------------------------------- settings

// Scheduled RCon announcements UI. Renders a mini-editor inside Settings so
// admins can add/remove lines without hand-editing manager.json.
// scheduledRestartsCard — UI for cfg.ScheduledRestarts (HH:MM list) and
// cfg.RestartWarnMinutes. Mutates the parent F object so the main Settings
// save path picks up the values without a separate endpoint.
function scheduledRestartsCard(c, F) {
  const times = Array.isArray(c.scheduledRestarts) ? c.scheduledRestarts.slice() : [];
  const warnsCsv = (Array.isArray(c.restartWarnMinutes) ? c.restartWarnMinutes : [5,3,1]).join(',');

  const list = h('div');
  const warnInput = h('input', { type: 'text', value: warnsCsv, placeholder: '5,3,1', style: { width: '160px' } });

  function render() {
    list.innerHTML = '';
    times.forEach((tm, i) => {
      const inp = h('input', { type: 'text', value: tm, placeholder: 'HH:MM', style: { width: '90px' } });
      inp.onchange = () => { times[i] = inp.value.trim(); };
      const rm = h('button', { class: 'danger', text: '×',
        onclick: () => { times.splice(i, 1); render(); } });
      list.append(h('div', { class: 'row', style: { gap: '8px', marginBottom: '6px' } }, [inp, rm]));
    });
    if (times.length === 0) {
      list.append(h('p', { class: 'hint', i18n: 'settings.restarts.empty' }));
    }
  }
  render();

  // Expose getters via fake form fields so the parent save path picks them up.
  F.scheduledRestarts = {
    type: 'list-times',
    get value() {
      return times.map(s => (s || '').trim()).filter(Boolean);
    },
  };
  F.restartWarnMinutes = {
    type: 'list-numbers',
    get value() {
      return warnInput.value
        .split(',').map(s => parseInt(s.trim(), 10))
        .filter(n => Number.isFinite(n) && n > 0);
    },
  };

  return h('div', { class: 'card' }, [
    h('h3', { i18n: 'settings.restarts.title' }),
    h('p', { class: 'hint', i18n: 'settings.restarts.hint' }),
    list,
    h('div', { class: 'actions' }, [
      h('button', { i18n: 'settings.restarts.add',
        onclick: () => { times.push('04:00'); render(); } }),
    ]),
    h('div', { style: { marginTop: '12px' } }, [
      h('label', { i18n: 'settings.restarts.warn' }),
      warnInput,
      h('small', { class: 'hint', i18n: 'settings.restarts.warn.hint' }),
    ]),
  ]);
}

// securityCard — set / change / remove the panel password.
// Stays inert if Exposure=internet and the user clicks "remove" — the
// backend rejects that combination with 403.
function securityCard(c) {
  const hasPassword = !!c.requireAuth;
  const userInp = h('input', { type: 'text', value: c.adminUsername || 'admin', placeholder: 'admin' });
  const curInp  = h('input', { type: 'password', placeholder: t('security.current') || 'Current password', autocomplete: 'current-password' });
  const newInp  = h('input', { type: 'password', placeholder: t('security.new')     || 'New password',     autocomplete: 'new-password' });
  const new2Inp = h('input', { type: 'password', placeholder: t('security.confirm') || 'Confirm new password', autocomplete: 'new-password' });
  const status  = h('div', { class: 'hint' });

  const apply = async (clearing) => {
    status.textContent = '';
    if (!clearing) {
      if (!newInp.value) { status.textContent = t('security.required') || 'New password required'; return; }
      if (newInp.value !== new2Inp.value) { status.textContent = t('security.mismatch') || 'Passwords do not match'; return; }
    }
    try {
      const r = await api.post('/api/auth/password', {
        currentPassword: curInp.value,
        newPassword: clearing ? '' : newInp.value,
        username: userInp.value.trim(),
      });
      // Cookie was just cleared by the server. Force a re-login.
      State.config.requireAuth = !!r.requireAuth;
      State.config.adminUsername = r.username;
      toast(t(clearing ? 'security.removed' : 'security.changed') ||
            (clearing ? 'Password removed' : 'Password changed'), 'ok');
      if (r.requireAuth) {
        showLogin();
      } else {
        // Re-render Settings so the card reflects the new state.
        await navigate('settings');
      }
    } catch (e) { status.textContent = String(e.message || e); }
  };

  return h('div', { class: 'card' }, [
    h('h3', { i18n: 'settings.security' }),
    h('p', { class: 'hint',
      i18n: hasPassword ? 'settings.security.hint.has' : 'settings.security.hint.none' }),
    h('div', { class: 'grid-2' }, [
      h('div', {}, [h('label', { i18n: 'security.username' }), userInp]),
      h('div', {}, hasPassword ? [h('label', { i18n: 'security.current' }), curInp] : []),
    ]),
    h('div', { class: 'grid-2' }, [
      h('div', {}, [h('label', { i18n: 'security.new' }),     newInp]),
      h('div', {}, [h('label', { i18n: 'security.confirm' }), new2Inp]),
    ]),
    status,
    h('div', { class: 'actions' }, [
      h('button', { class: 'primary', i18n: hasPassword ? 'security.change' : 'security.set',
        onclick: () => apply(false) }),
      hasPassword
        ? h('button', { class: 'danger', i18n: 'security.remove',
            onclick: () => {
              if (!confirm(t('security.removeConfirm') || 'Remove password and disable login?')) return;
              apply(true);
            } })
        : null,
    ]),
  ]);
}

function announcementsCard() {
  const card = h('div', { class: 'card' }, [
    h('h3', { i18n: 'settings.announcements' }),
    h('p', { class: 'hint', i18n: 'settings.announcements.hint' }),
  ]);
  const list = h('div');
  card.append(list);

  const state = { items: [] };

  async function reload() {
    try {
      const r = await api.get('/api/announcements');
      state.items = r.announcements || [];
    } catch { state.items = []; }
    render();
  }

  function render() {
    list.innerHTML = '';
    state.items.forEach((a, i) => {
      const time = h('input', { type: 'text', value: a.time, placeholder: 'HH:MM', style: { width: '80px' } });
      const msg  = h('input', { type: 'text', value: a.message || '', style: { flex: '1' } });
      const en   = h('input', { type: 'checkbox' });
      en.checked = !!a.enabled;
      const row = h('div', { class: 'row', style: { gap: '8px', marginBottom: '6px' } }, [
        time, msg,
        h('label', {}, [en, h('span', { text: ' on' })]),
        h('button', { class: 'danger', text: '×',
          onclick: () => { state.items.splice(i, 1); render(); } }),
      ]);
      time.onchange = () => { state.items[i].time = time.value.trim(); };
      msg.oninput   = () => { state.items[i].message = msg.value; };
      en.onchange   = () => { state.items[i].enabled = en.checked; };
      list.append(row);
    });
  }

  card.append(
    h('div', { class: 'actions' }, [
      h('button', { i18n: 'settings.announcements.add',
        onclick: () => { state.items.push({ time: '12:00', message: '', enabled: true }); render(); }
      }),
      h('button', { class: 'primary', i18n: 'action.save',
        onclick: async () => {
          try {
            await api.post('/api/announcements', { announcements: state.items });
            toast('saved', 'ok');
          } catch (e) { handleErr(e); }
        }
      }),
    ]),
  );
  reload();
  return card;
}

Views.settings = async (root) => {
  const c = State.config;
  const F = {
    language:        h('select', {}, [h('option', { value: 'en', text: 'English' }), h('option', { value: 'ru', text: 'Русский' })]),
    vanillaDayZPath: h('input',  { type: 'text', value: c.vanillaDayZPath || '' }),
    exposure:        h('select', {}, [
                       h('option', { value: 'local',    i18n: 'settings.exposure.local' }),
                       h('option', { value: 'internet', i18n: 'settings.exposure.internet' }),
                     ]),
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
  F.exposure.value = c.exposure || 'local';
  for (const k of ['autoRestartEnabled','doLogs','adminLog','netLog','freezeCheck','filePatching']) {
    F[k].checked = !!c[k];
  }

  const detectBtn = h('button', { i18n: 'settings.detectDayZ', onclick: async () => {
    try {
      const d = await api.get('/api/steam/detect');
      const installs = d.installs || [];
      if (!installs.length) { toast(t('settings.detectDayZ.none')); return; }
      // Prefer an install that has !Workshop; fall back to the first hit.
      const hit = installs.find(i => i.hasWorkshop) || installs[0];
      F.vanillaDayZPath.value = hit.path;
      toast(hit.path, 'ok');
    } catch (e) { handleErr(e); }
  }});

  const section = (title, rows) => h('div', { class: 'card' }, [h('h3', { i18n: title }), ...rows]);
  const row = (labelKey, el) => h('div', {}, [h('label', { i18n: labelKey }), el]);

  const themeSel = h('select', {}, [
    h('option', { value: 'dark',  i18n: 'settings.theme.dark' }),
    h('option', { value: 'light', i18n: 'settings.theme.light' }),
    h('option', { value: 'auto',  i18n: 'settings.theme.auto' }),
  ]);
  themeSel.value = localStorage.getItem('theme') || 'dark';
  themeSel.onchange = () => setTheme(themeSel.value);

  root.append(
    pageHeader('nav.settings', 'settings.subtitle'),

    section('settings.title', [
      row('settings.language', F.language),
      row('settings.theme', themeSel),
      h('div', {}, [
        h('label', { i18n: 'settings.vanillaPath' }),
        h('div', { class: 'row' }, [F.vanillaDayZPath, detectBtn]),
      ]),
      h('div', {}, [
        h('label', { i18n: 'settings.exposure' }),
        F.exposure,
        h('small', { class: 'hint', i18n: 'settings.exposure.hint' }),
      ]),
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
      h('p', { class: 'hint', i18n: 'settings.autorestart.hint' }),
    ]),

    scheduledRestartsCard(c, F),

    announcementsCard(),

    securityCard(c),

    backupCard(),

    h('div', { class: 'actions' }, [
      h('button', { class: 'primary', i18n: 'action.save',
        onclick: async () => {
          const next = { ...c };
          for (const [k, el] of Object.entries(F)) {
            if (el.type === 'checkbox') next[k] = el.checked;
            else if (el.type === 'number') next[k] = Number(el.value);
            else if (el.type === 'list-times' || el.type === 'list-numbers') next[k] = el.value;
            else next[k] = el.value;
          }
          try {
            State.config = await api.post('/api/config', next);
            if (next.language !== c.language) {
              await loadI18n(next.language);
              document.getElementById('lang-switch').value = next.language;
            }
            toast(t('action.save'), 'ok');
          } catch (e) { handleErr(e); }
        }
      }),
    ]),

    donationCard(),
  );
};

function backupCard() {
  const fileInp = h('input', { type: 'file', accept: '.zip' });
  fileInp.style.display = 'none';
  const resultBox = h('div', { class: 'hint' });

  const exportBtn = h('a', { class: 'btn-link', href: '/api/backup/export', i18n: 'settings.backup.export' });
  const importBtn = h('button', { i18n: 'settings.backup.import',
    onclick: () => fileInp.click() });

  fileInp.onchange = async () => {
    const f = fileInp.files && fileInp.files[0];
    if (!f) return;
    const fd = new FormData();
    fd.append('zip', f);
    try {
      const r = await fetch('/api/backup/import', {
        method: 'POST', credentials: 'same-origin', body: fd,
      });
      if (!r.ok) throw new Error(await r.text());
      const data = await r.json();
      resultBox.textContent = `restored: ${(data.restored || []).length}, skipped: ${(data.skipped || []).length}`;
      toast(t('action.save'), 'ok');
      // Refresh config view so restored values surface.
      State.config = await api.get('/api/config');
    } catch (e) { handleErr(e); }
    fileInp.value = '';
  };

  return h('div', { class: 'card' }, [
    h('h3', { i18n: 'settings.backup' }),
    h('p', { class: 'hint', i18n: 'settings.backup.hint' }),
    h('div', { class: 'actions' }, [exportBtn, importBtn, fileInp]),
    resultBox,
  ]);
}

function donationCard() {
  return h('div', { class: 'donate-card' }, [
    h('h3', { i18n: 'support.title' }),
    h('p',  { i18n: 'support.text' }),
    h('div', { class: 'donate-buttons' }, [
      h('a', { href: 'https://buymeacoffee.com/aristarh.ucolov', target: '_blank', rel: 'noopener', class: 'coffee' }, [
        '☕ ', h('span', { i18n: 'support.coffee' }),
      ]),
      h('a', { href: 'https://www.donationalerts.com/r/aristarh_ucolov', target: '_blank', rel: 'noopener', class: 'da' }, [
        '♥ ', h('span', { i18n: 'support.donationalerts' }),
      ]),
    ]),
  ]);
}

// --------------------------------------------------------------------- theme

function setTheme(mode) {
  localStorage.setItem('theme', mode);
  let applied = mode;
  if (mode === 'auto') {
    applied = window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  }
  document.documentElement.setAttribute('data-theme', applied);
  const btn = document.querySelector('#theme-toggle .theme-icon');
  if (btn) btn.textContent = applied === 'light' ? '☀' : '☾';
}

// --------------------------------------------------------------------- sync / import

Views.sync = async (root) => {
  const src = h('input', { type: 'text', placeholder: 'F:\\SteamLibrary\\steamapps\\common\\DayZServer' });
  const out = h('div');
  const status = h('div', { class: 'hint' });

  const runPreview = async () => {
    out.innerHTML = '';
    status.textContent = '';
    if (!src.value.trim()) return;
    try {
      const p = await api.post('/api/import/preview', { sourceDir: src.value.trim() });
      const copyMods = h('input', { type: 'checkbox', checked: true });
      const copyCfg  = h('input', { type: 'checkbox' });
      const missionSel = h('select', {}, [h('option', { value: '', text: '—' }),
        ...(p.missions || []).map(m => h('option', { value: m, text: m }))]);
      if (p.mission) missionSel.value = p.mission;
      const modsList = (p.mods || []).length
        ? h('ul', {}, (p.mods || []).map(m => h('li', { text: m })))
        : h('p', { class: 'hint', text: '—' });
      out.append(
        h('div', { class: 'card' }, [
          h('h3', { i18n: 'sync.preview' }),
          h('div', {}, [h('label', { i18n: 'sync.mods' }), modsList]),
          h('div', {}, [h('label', { i18n: 'sync.cfgMission' }), h('code', { text: p.mission || '—' })]),
          h('div', {}, [h('label', {}, [copyMods, h('span', { i18n: 'sync.copyMods' })])]),
          h('div', {}, [h('label', {}, [copyCfg,  h('span', { i18n: 'sync.copyCfg'  })])]),
          h('div', {}, [h('label', { i18n: 'sync.mission' }), missionSel]),
          h('div', { class: 'actions' }, [
            h('button', { class: 'primary', i18n: 'sync.apply', onclick: async () => {
              try {
                const r = await api.post('/api/import/apply', {
                  sourceDir: src.value.trim(),
                  copyMods: copyMods.checked,
                  copyCfg: copyCfg.checked,
                  mission: missionSel.value,
                });
                status.textContent = t('sync.done') + ' ' + (r.copiedMods || []).join(', ');
                State.config = await api.get('/api/config');
              } catch (e) { handleErr(e); }
            }}),
          ]),
        ]),
      );
    } catch (e) { handleErr(e); }
  };

  root.append(
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'sync.title' }),
      h('p', { class: 'hint', i18n: 'sync.hint' }),
      h('label', { i18n: 'sync.source' }),
      h('div', { class: 'row' }, [src, h('button', { i18n: 'sync.preview', onclick: runPreview })]),
      status,
    ]),
    out,
  );
};

// --------------------------------------------------------------------- mod profile configs

Views.profiles = async (root) => {
  const tree = h('div', { class: 'left' });
  const editor = h('textarea', { style: { height: '60vh', fontFamily: 'monospace' }, placeholder: t('profiles.empty') });
  const savedPath = { path: '' };
  let cm = null;
  const getValue = () => cm ? cm.getValue() : editor.value;
  const setValue = (v) => { if (cm) cm.setValue(v || ''); else editor.value = v || ''; };
  const setMode  = (p) => { if (cm) cm.setOption('mode', CM.modeFor(p)); };

  async function doSave() {
    if (!savedPath.path) return;
    try {
      await api.post('/api/profiles/write', { path: savedPath.path, content: getValue() });
      toast(t('files.save'), 'ok');
    } catch (e) { handleErr(e); }
  }
  const savebtn = h('button', { class: 'primary', i18n: 'files.save', disabled: true, title: 'Ctrl+S',
    onclick: doSave });
  root._save = doSave;

  async function openFile(path) {
    try {
      const r = await api.get('/api/profiles/read?path=' + encodeURIComponent(path));
      setMode(path);
      setValue(r.content);
      savedPath.path = path;
      savebtn.disabled = false;
    } catch (e) { handleErr(e); }
  }

  async function renderDir(rel) {
    tree.innerHTML = '';
    if (rel) {
      const up = rel.split('/').slice(0, -1).join('/');
      tree.appendChild(h('div', { class: 'tree-node dir', text: '../', onclick: () => renderDir(up) }));
    }
    try {
      const r = await api.get('/api/profiles/tree?path=' + encodeURIComponent(rel));
      if (!(r.entries || []).length) {
        tree.appendChild(h('p', { class: 'hint', i18n: 'profiles.empty' }));
      }
      for (const e of (r.entries || [])) {
        if (e.isDir) {
          tree.appendChild(h('div', { class: 'tree-node dir', text: e.name + '/', onclick: () => renderDir(e.path) }));
        } else {
          tree.appendChild(h('div', { class: 'tree-node', text: e.name, onclick: () => openFile(e.path) }));
        }
      }
    } catch (e) { handleErr(e); }
  }

  root.append(
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'profiles.title' }),
      h('p', { class: 'hint', i18n: 'profiles.hint' }),
      h('div', { class: 'two-col' }, [
        tree,
        h('div', {}, [editor, h('div', { class: 'actions' }, [savebtn])]),
      ]),
    ]),
  );
  await renderDir('');
  try {
    cm = await CM.mount(editor, { mode: null });
    root._teardown = () => { try { cm && cm.toTextArea(); } catch {} };
  } catch (e) { console.warn('CodeMirror load failed', e); }
};

// --------------------------------------------------------------------- bootstrap

async function main() {
  try {
    // Apply theme as early as possible so the login/first-run modals don't
    // flash dark-on-light. Reads the same key the Settings page writes to.
    setTheme(localStorage.getItem('theme') || 'dark');
    const themeBtn = document.getElementById('theme-toggle');
    if (themeBtn) {
      themeBtn.onclick = () => {
        const cur = document.documentElement.getAttribute('data-theme');
        setTheme(cur === 'light' ? 'dark' : 'light');
      };
    }
    const cmdpBtn = document.getElementById('cmdp-btn');
    if (cmdpBtn) cmdpBtn.onclick = () => openCommandPalette();
    // Sidebar drawer behavior on narrow screens. Topbar ≡ button toggles it,
    // backdrop click and route navigation close it. No-op when sidebar is
    // already in always-visible mode (CSS handles the transform).
    const sidebar = document.getElementById('sidebar');
    const backdrop = document.getElementById('sidebar-backdrop');
    const closeSidebar = () => {
      sidebar?.classList.remove('open');
      backdrop?.classList.remove('visible');
    };
    const openSidebar = () => {
      sidebar?.classList.add('open');
      backdrop?.classList.add('visible');
    };
    const navToggle = document.getElementById('nav-toggle');
    if (navToggle) {
      navToggle.onclick = () => {
        if (sidebar?.classList.contains('open')) closeSidebar(); else openSidebar();
      };
    }
    if (backdrop) backdrop.onclick = closeSidebar;
    document.addEventListener('click', e => {
      const link = e.target.closest('.nav a');
      if (link && window.matchMedia('(max-width: 1000px)').matches) closeSidebar();
    });

    State.info = await api.get('/api/info');
    // Load i18n early — using a remembered locale or 'en' — so the login
    // modal has populated labels even before we know the configured
    // language. Without this the modal would render blank labels on a
    // stale-cookie reload, which looks like a broken state to the user.
    const earlyLang = localStorage.getItem('lang') || 'en';
    try { await loadI18n(earlyLang); } catch {}

    // Check auth before fetching the rest. If the panel requires auth and
    // we have no valid session, surface the login modal and bail out —
    // the user will re-enter main() from the login submit handler.
    const s = await api.get('/api/auth/status');
    if (s.requireAuth && !s.authenticated) { showLogin(); return; }

    State.config = await api.get('/api/config');
    if (State.config.language && State.config.language !== State.lang) {
      await loadI18n(State.config.language);
    }
    document.getElementById('lang-switch').value = State.lang;
    document.getElementById('lang-switch').onchange = async e => {
      const v = e.target.value;
      State.config.language = v;
      localStorage.setItem('lang', v);
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
    // A second main() entry (after re-login) creates a fresh interval
    // without clearing the previous one. Track and cancel.
    if (window._statusInterval) clearInterval(window._statusInterval);
    window._statusInterval = setInterval(refreshStatus, 5000);
    await navigate('dashboard');
  } catch (err) {
    if (String(err.message) !== 'unauthorized') handleErr(err);
  }
}

function currentRoute() {
  const active = document.querySelector('.nav a.active');
  return active ? active.dataset.route : 'dashboard';
}

// --------------------------------------------------------------------- battleye

Views.battleye = async (root) => {
  const d = await api.get('/api/battleye/list');
  const wrap = h('div');
  if (State.serverStatus.running) wrap.append(runningBanner());

  const fileSelect = h('select', {});
  for (const f of d.files) {
    const label = f.exists ? `${f.name} (${bytes(f.size)}${f.lineHint ? ', ' + f.lineHint : ''})` : `${f.name} (empty)`;
    fileSelect.append(h('option', { value: f.name, text: label }));
  }
  const editorHost = h('textarea', { rows: 22, style: { width: '100%', fontFamily: 'monospace' } });
  let cm = null;
  const getValue = () => cm ? cm.getValue() : editorHost.value;
  const setValue = (v) => { if (cm) cm.setValue(v || ''); else editorHost.value = v || ''; };

  const load = async () => {
    try {
      const r = await api.get('/api/battleye/read?name=' + encodeURIComponent(fileSelect.value));
      setValue(r.content || '');
    } catch (e) { handleErr(e); }
  };
  fileSelect.onchange = load;

  async function doSave() {
    try {
      await api.post('/api/battleye/write', { name: fileSelect.value, content: getValue() });
      toast('saved', 'ok');
    } catch (e) { handleErr(e); }
  }
  root._save = doSave;

  wrap.append(h('div', { class: 'card' }, [
    h('h2', { i18n: 'nav.battleye' }),
    h('p', { class: 'hint', text: d.dir }),
    h('div', { class: 'toolbar' }, [
      fileSelect,
      h('button', { i18n: 'action.reload', onclick: load }),
      h('button', { class: 'primary', i18n: 'action.save', title: 'Ctrl+S', onclick: doSave }),
    ]),
    editorHost,
    h('p', { class: 'hint', i18n: 'battleye.hint' }),
  ]));
  root.append(wrap);
  await load();

  try {
    cm = await CM.mount(editorHost, { mode: null, lineWrapping: true });
    // Re-apply initial value that was set before mount.
    if (editorHost.value && !cm.getValue()) cm.setValue(editorHost.value);
    root._teardown = () => { try { cm && cm.toTextArea(); } catch {} };
  } catch (e) { console.warn('CodeMirror load failed', e); }
};

// --------------------------------------------------------------------- mission DB

Views.missiondb = async (root) => {
  const d = await api.get('/api/mission/db/list');
  const wrap = h('div');
  if (State.serverStatus.running) wrap.append(runningBanner());

  const fileSelect = h('select', {});
  for (const f of d.files) {
    const label = f.exists ? `${f.path} (${bytes(f.size)})` : `${f.path} (missing)`;
    const opt = h('option', { value: f.path, text: label });
    if (!f.exists) opt.disabled = true;
    fileSelect.append(opt);
  }
  const editorHost = h('textarea', { rows: 28, style: { width: '100%', fontFamily: 'monospace' } });
  let cm = null;
  const getValue = () => cm ? cm.getValue() : editorHost.value;
  const setValue = (v) => { if (cm) cm.setValue(v || ''); else editorHost.value = v || ''; };
  const setMode = (path) => { if (cm) cm.setOption('mode', CM.modeFor(path)); };

  const load = async () => {
    try {
      const r = await api.get('/api/mission/db/read?path=' + encodeURIComponent(fileSelect.value));
      setMode(fileSelect.value);
      setValue(r.content || '');
    } catch (e) { handleErr(e); }
  };
  fileSelect.onchange = load;

  async function doSave() {
    try {
      await api.post('/api/mission/db/write', { path: fileSelect.value, content: getValue() });
      toast('saved', 'ok');
    } catch (e) { handleErr(e); }
  }
  root._save = doSave;

  wrap.append(h('div', { class: 'card' }, [
    h('h2', { i18n: 'nav.missionDb' }),
    h('p', { class: 'hint', text: d.dir }),
    h('div', { class: 'toolbar' }, [
      fileSelect,
      h('button', { i18n: 'action.reload', onclick: load }),
      h('button', { class: 'primary', i18n: 'action.save', title: 'Ctrl+S', onclick: doSave }),
    ]),
    editorHost,
    h('p', { class: 'hint', i18n: 'missionDb.hint' }),
  ]));
  root.append(wrap);

  // Pick the first existing file automatically so the textarea is not blank
  // on first open.
  const firstExisting = d.files.find(f => f.exists);
  if (firstExisting) fileSelect.value = firstExisting.path;

  try {
    cm = await CM.mount(editorHost, { mode: CM.modeFor(fileSelect.value) });
    root._teardown = () => { try { cm && cm.toTextArea(); } catch {} };
  } catch (e) { console.warn('CodeMirror load failed', e); }

  if (firstExisting) await load();
};

main();
