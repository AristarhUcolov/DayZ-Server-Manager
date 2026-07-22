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
  languages: null,
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
  State.help = data.help || {}; // hover-help travels with the bundle
  if (data.languages) State.languages = data.languages;
  document.documentElement.lang = State.lang;
  applyI18n();
}

function t(key) { return State.i18n[key] || key; }

// fillLangSelect populates a <select> with the available UI languages. `short`
// shows the uppercase code (compact, for the topbar); otherwise the native name.
function fillLangSelect(sel, selected, short) {
  if (!sel) return;
  const langs = State.languages || [{ code: 'en', name: 'English' }, { code: 'ru', name: 'Русский' }];
  sel.innerHTML = '';
  for (const l of langs) sel.append(h('option', { value: l.code, text: short ? l.code.toUpperCase() : l.name }));
  if (selected != null) sel.value = selected;
}

function applyI18n() {
  document.querySelectorAll('[data-i18n]').forEach(el => {
    el.textContent = t(el.dataset.i18n);
  });
}


// --------------------------------------------------------------------- help
//
// Hover-help. `help('key')` returns a small ⓘ marker whose tooltip text comes
// from i18n key `help.<key>`; `withHelp(node, 'key')` attaches one to any
// element. One delegated listener drives them all, and focus works too so the
// explanations are reachable by keyboard, not just by mouse.

let _helpTip = null;
function helpTip() {
  if (!_helpTip) {
    _helpTip = h('div', { class: 'help-tip', role: 'tooltip' });
    document.body.append(_helpTip);
  }
  return _helpTip;
}

function showHelp(anchor) {
  const key = anchor.dataset.help;
  if (!key) return;
  const text = (State.help || {})[key];
  if (!text) return; // no explanation written for this key
  const tip = helpTip();
  tip.textContent = text;
  tip.classList.add('visible');
  // Place above the anchor when there is room, otherwise below; always keep
  // the bubble fully on screen.
  const r = anchor.getBoundingClientRect();
  const tr = tip.getBoundingClientRect();
  let top = r.top - tr.height - 10;
  if (top < 8) top = r.bottom + 10;
  let left = r.left + r.width / 2 - tr.width / 2;
  left = Math.max(8, Math.min(left, window.innerWidth - tr.width - 8));
  tip.style.top = Math.round(top + window.scrollY) + 'px';
  tip.style.left = Math.round(left) + 'px';
}
function hideHelp() { if (_helpTip) _helpTip.classList.remove('visible'); }

document.addEventListener('mouseover', e => {
  const a = e.target.closest && e.target.closest('[data-help]');
  if (a) showHelp(a);
});
document.addEventListener('mouseout', e => {
  if (e.target.closest && e.target.closest('[data-help]')) hideHelp();
});
document.addEventListener('focusin', e => {
  const a = e.target.closest && e.target.closest('[data-help]');
  if (a) showHelp(a);
});
document.addEventListener('focusout', hideHelp);
window.addEventListener('scroll', hideHelp, { passive: true });

// help returns the ⓘ marker element for an i18n help key.
function help(key) {
  return h('span', { class: 'help-mark', 'data-help': key, tabindex: '0',
    'aria-label': 'help', text: 'i' });
}
// withHelp wraps a label/heading so the marker sits right after it.
function withHelp(node, key) {
  return h('span', { class: 'with-help' }, [node, help(key)]);
}
// hlp tags an element in place, with no wrapper — use this wherever an extra
// <span> would be invalid markup (a <th> inside <tr>, an <option>, an input).
function hlp(node, key) { node.dataset.help = key; return node; }

// --------------------------------------------------------------------- toast

// Stacked toasts: each call appends its own element to #toast-region so a
// success can no longer overwrite an in-flight error. Errors stay longer and
// every toast is click-to-dismiss.
function toast(msg, kind = '') {
  const region = document.getElementById('toast-region');
  if (!region) return;
  while (region.children.length >= 3) region.firstChild.remove();
  const el = h('div', { class: `toast ${kind}`, role: kind === 'error' ? 'alert' : undefined, text: msg });
  const dismiss = () => {
    el.classList.remove('visible');
    setTimeout(() => el.remove(), 250);
  };
  el.onclick = dismiss;
  region.append(el);
  requestAnimationFrame(() => el.classList.add('visible'));
  setTimeout(dismiss, kind === 'error' ? 8000 : 3500);
}

function handleErr(err) {
  console.error(err);
  let msg = String(err && err.message || err);
  // Backend errors can be whole Go error chains or HTML pages — keep the toast
  // readable, full detail stays in the console.
  if (msg.length > 220) msg = msg.slice(0, 220) + '…';
  toast(msg, 'error');
}

// withBusy wraps an async button action: disables the button and shows the
// design-system spinner (.loading) until the action settles. Prevents
// double-submits on every long operation (server start, mod installs, wipe).
async function withBusy(btn, fn) {
  // A missing button (e.g. currentTarget read after an await) must NEVER
  // swallow the action — run it without the spinner instead. This exact
  // mistake once made "Uninstall mod" a silent no-op.
  if (!btn) return fn();
  if (btn.classList.contains('loading')) return;
  btn.classList.add('loading');
  btn.disabled = true;
  try { return await fn(); }
  finally { btn.classList.remove('loading'); btn.disabled = false; }
}

// --------------------------------------------------------------------- router

const Views = {};

function setActiveRoute(name) {
  document.querySelectorAll('.nav a').forEach(a => {
    const on = a.dataset.route === name;
    a.classList.toggle('active', on);
    if (on) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
  });
}

// routeFromHash reads the current section from location.hash, falling back to
// 'dashboard' for an empty or unknown route. This is what lets a page reload
// (F5) land back on the section the user was looking at instead of always
// bouncing to the dashboard.
function routeFromHash() {
  const raw = decodeURIComponent((location.hash || '').replace(/^#/, '')).trim();
  return Views[raw] ? raw : 'dashboard';
}

// _navSeq tags each navigation. If a newer navigate starts while an older one
// is still awaiting its (async) view, the older one bails out on resume instead
// of appending its content/footer into the new page — which is what produced
// duplicate "Support" cards when switching sections quickly.
let _navSeq = 0;

async function navigate(name) {
  const myNav = ++_navSeq;
  setActiveRoute(name);
  // Mirror the route into the URL hash so a reload restores this section.
  // Done before awaiting the view so the resulting hashchange sees the active
  // route already updated and becomes a no-op (avoids a double render).
  if (routeFromHash() !== name) location.hash = name;
  const view = Views[name] || Views.dashboard;
  const root = document.getElementById('view');
  if (root._teardown) { try { root._teardown(); } catch {} root._teardown = null; }
  root._save = null; // stale Ctrl+S handlers must not survive navigation
  root.innerHTML = '';
  window.scrollTo(0, 0); // a new section always starts at the top
  try {
    await view(root);
    if (myNav !== _navSeq) return; // superseded by a newer navigation
    applyI18n();
  } catch (err) {
    if (myNav !== _navSeq) return;
    handleErr(err);
    // Build the fallback with textContent — error bodies can contain markup.
    root.innerHTML = '';
    root.append(h('div', { class: 'card' }, [
      h('p', { text: String(err && err.message || err) }),
      h('div', { class: 'actions' }, [
        h('button', { i18n: 'action.reload', onclick: () => navigate(name) }),
      ]),
    ]));
  }
}

// renderSupportFooter populates the persistent #support-footer exactly once.
// Because it lives outside #view, navigation never touches or duplicates it.
function renderSupportFooter() {
  const host = document.getElementById('support-footer');
  if (!host || host.firstChild) return;
  host.append(donationCard());
}

document.addEventListener('click', e => {
  const a = e.target.closest('.nav a');
  if (a && a.dataset.route) { e.preventDefault(); navigate(a.dataset.route); }
});

// Browser back/forward and manual hash edits. navigate() also writes the hash,
// but only when it differs from the active route, so its own updates don't
// re-trigger this handler.
window.addEventListener('hashchange', () => {
  const r = routeFromHash();
  if (r !== currentRoute()) navigate(r);
});

// Global keyboard shortcuts.
//   Ctrl/Cmd+S — call root._save() if the active view registered one.
//   Ctrl/Cmd+K — open the command palette.
// Always swallows the browser default (Save Page As / search bar focus).
document.addEventListener('keydown', e => {
  const meta = e.ctrlKey || e.metaKey;
  if (!meta) return;
  if (e.key === 's' || e.key === 'S') {
    // Always swallow the browser's Save-Page dialog, even on views without a
    // registered save handler — it is never what the user wants here.
    e.preventDefault();
    const root = document.getElementById('view');
    if (root && typeof root._save === 'function') {
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

let _statusFails = 0;
async function refreshStatus() {
  const chip = document.getElementById('server-indicator');
  const txt = document.getElementById('server-indicator-text');
  try {
    const s = await api.get('/api/server/status');
    State.serverStatus = s;
    _statusFails = 0;
    chip.classList.remove('offline');
    chip.classList.toggle('running', s.running);
    chip.classList.toggle('stopped', !s.running);
    txt.dataset.i18n = s.running ? 'status.running' : 'status.stopped';
    txt.textContent = t(txt.dataset.i18n);
  } catch (err) {
    // After a few consecutive failures the manager itself is unreachable —
    // show that instead of freezing on the last known Running/Stopped forever.
    if (++_statusFails >= 3 && chip) {
      chip.classList.remove('running', 'stopped');
      chip.classList.add('offline');
      txt.dataset.i18n = 'status.offline';
      txt.textContent = t('status.offline');
    }
  }
}

// --------------------------------------------------------------------- first-run

async function ensureFirstRunDone() {
  if (State.config.firstRunDone) return;
  const modal = document.getElementById('first-run');
  modal.classList.remove('hidden');
  fillLangSelect(document.getElementById('fr-lang'), State.config.language || 'en', false);
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

  document.getElementById('fr-finish').onclick = (e) => withBusy(e.currentTarget, async () => {
    const exposure = document.querySelector('input[name=fr-exposure]:checked').value;
    const body = {
      language: document.getElementById('fr-lang').value,
      vanillaDayZPath: document.getElementById('fr-vanilla').value.trim(),
      exposure,
    };
    try {
      State.config = await api.post('/api/config/finish-first-run', body);
      await loadI18n(State.config.language);
      document.getElementById('lang-switch').value = State.config.language;
      modal.classList.add('hidden');
      await navigate('dashboard');
    } catch (err) { handleErr(err); }
  });
  document.getElementById('fr-lang').addEventListener('change', async e => {
    localStorage.setItem('lang', e.target.value); // reload mid-wizard keeps the picked language
    await loadI18n(e.target.value);
  });
}

// --------------------------------------------------------------------- dashboard

Views.dashboard = async (root) => {
  const myNav = _navSeq;
  // Timers are created much further down, after several awaits. Register the
  // teardown NOW against mutable refs: navigate() already ran the previous
  // view's teardown, so assigning later would land on the next view instead
  // and leak both intervals for the rest of the session.
  const timers = { tick: 0, perf: 0, stopped: false };
  root._teardown = () => {
    timers.stopped = true;
    if (timers.tick) clearInterval(timers.tick);
    if (timers.perf) clearInterval(timers.perf);
  };
  await refreshStatus();
  // Start/Stop/Restart live in the Server card below — no duplicate set in the
  // header (was confusing to have two).
  root.append(pageHeader('nav.dashboard', 'dashboard.subtitle'));

  // Silent probe — if a newer manager version is out, show a non-blocking
  // banner. Failures are swallowed; this should never block the dashboard.
  (async () => {
    try {
      const u = await api.get('/api/update/check');
      // A dismissed version stays dismissed — the banner is a nudge, not a nag.
      if (u.updateAvailable && u.latest && localStorage.getItem('skipVersion') !== u.latest) {
        const banner = h('div', { class: 'warning-bar', style: { display: 'flex', gap: '10px', alignItems: 'center' } }, [
          h('span', { style: { flex: '1' }, text: `${t('update.available')}: ${u.current} → ${u.latest}` }),
          u.releaseUrl ? h('a', { href: u.releaseUrl, target: '_blank', rel: 'noopener', i18n: 'update.open' }) : null,
          h('button', { text: '×', 'aria-label': 'dismiss', style: { padding: '2px 9px' },
            onclick: () => { localStorage.setItem('skipVersion', u.latest); banner.remove(); } }),
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

    if (State.serverStatus.loopPaused) {
      metricsHost.append(h('div', { class: 'warning-bar', style: { display: 'flex', gap: '10px', alignItems: 'center' } }, [
        h('span', { style: { flex: '1' }, i18n: 'status.crashLoop' }),
        h('button', { i18n: 'action.resume', onclick: async (e) => {
          await withBusy(e.currentTarget, async () => {
            try { await api.post('/api/server/clear-crash-loop'); toast(t('msg.saved'), 'ok'); await navigate('dashboard'); }
            catch (err) { handleErr(err); }
          });
        }}),
      ]));
    }

    metricsHost.append(
      h('div', { class: 'grid-4' }, [
        h('div', { class: 'card' }, [
          h('h3', { i18n: 'nav.server' }),
          h('div', { class: 'kv' }, [
            h('div', { class: 'k', i18n: 'status.running' }),
            h('div', { text: s.running ? t('status.yes') : t('status.no') }),
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
                  onclick: async (e) => {
                    // Capture the button BEFORE any await — e.currentTarget is
                    // only valid during dispatch and turns null after it.
                    const btn = e.currentTarget;
                    // Players online — a mis-click kills their session; confirm.
                    if (players > 0 && !(await confirmModal(t('confirm.playersOnline').replace('{n}', players), { danger: true, okText: t('action.stop') }))) return;
                    await withBusy(btn, async () => { try { await api.post('/api/server/stop'); await navigate('dashboard'); } catch(err){handleErr(err);} });
                  }})
              : h('button', { class: 'primary', i18n: 'action.start',
                  onclick: (e) => withBusy(e.currentTarget, async () => { try { await api.post('/api/server/start'); await navigate('dashboard'); } catch(err){handleErr(err);} })}),
            h('button', { i18n: 'action.restart',
              onclick: async (e) => {
                const btn = e.currentTarget;
                if (players > 0 && !(await confirmModal(t('confirm.playersOnline').replace('{n}', players), { danger: true, okText: t('action.restart') }))) return;
                await withBusy(btn, async () => { try { await api.post('/api/server/restart'); await navigate('dashboard'); } catch(err){handleErr(err);} });
              }}),
          ]),
        ]),
        h('div', { class: 'card' }, [
          h('h3', { i18n: 'dashboard.mods' }),
          h('div', { class: 'kv' }, [
            h('div', { class: 'k', i18n: 'dashboard.mods.active' }),
            h('div', { text: String(mods.active) }),
            h('div', { class: 'k', i18n: 'dashboard.mods.installed' }),
            h('div', { text: String(mods.installed) }),
            h('div', { class: 'k', i18n: 'dashboard.mods.total' }),
            h('div', { text: String(mods.total) }),
          ]),
          h('div', { class: 'actions' }, [
            h('button', { i18n: 'nav.mods', onclick: () => navigate('mods') }),
          ]),
        ]),
        h('div', { class: 'card' }, [
          h('h3', { i18n: 'dashboard.disk' }),
          h('div', { class: 'metric-num', text: disk }),
          h('div', { class: 'metric-sub', i18n: 'dashboard.disk.free' }),
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

  // timers.stopped is set by the teardown registered at the top of this view.
  const tick = async () => {
    if (timers.stopped) return;
    try {
      const m = await api.get('/api/dashboard/metrics');
      if (!timers.stopped) render(m);
    } catch {}
  };
  // Register teardown + interval BEFORE the first await so navigating away
  // during the initial fetch can't leak the interval (which kept hammering
  // /api/dashboard/metrics — and thus RCon — from detached pages).
  // ---- Performance history card (CPU / RAM / players over time). Lives
  // outside the 5s metrics tick — charts refetch on their own 60s cadence.
  const perfCard = h('div', { class: 'card' }, [h('h3', { i18n: 'perf.title' })]);
  const perfRange = h('div', { class: 'chart-range' });
  const perfHost = h('div', { class: 'perf-grid' });
  perfCard.append(perfRange, perfHost);
  root.append(perfCard);
  let perfSec = Number(localStorage.getItem('perfRange')) || 3600;
  const RANGES = [[3600, '1h'], [21600, '6h'], [86400, '24h']];
  function renderRangeButtons() {
    perfRange.innerHTML = '';
    for (const [sec, label] of RANGES) {
      perfRange.append(h('button', {
        class: sec === perfSec ? 'primary' : '',
        text: label,
        onclick: () => { perfSec = sec; localStorage.setItem('perfRange', String(sec)); renderRangeButtons(); loadPerf(); },
      }));
    }
  }
  async function loadPerf() {
    try {
      const d = await api.get(`/api/metrics/history?seconds=${perfSec}`);
      const samples = d.samples || [];
      perfHost.innerHTML = '';
      if (samples.length < 2) {
        perfHost.append(h('p', { class: 'hint', i18n: 'perf.empty' }));
        applyI18n();
        return;
      }
      perfHost.append(
        perfChart(samples, { label: t('dashboard.process.cpu'), val: x => x.running ? x.cpu : null, fmt: v => v.toFixed(1) + '%' }),
        perfChart(samples, { label: t('dashboard.process.mem'), val: x => x.running ? x.mem : null, fmt: v => bytes(v) }),
        perfChart(samples, { label: t('perf.players'), val: x => x.players, fmt: v => String(Math.round(v)) }),
      );
    } catch (e) { console.warn('perf history', e); }
  }
  renderRangeButtons();
  loadPerf();
  timers.perf = setInterval(loadPerf, 60000);
  timers.tick = setInterval(tick, 5000);
  await tick();
  // After tick() so State.serverStatus is fresh; the card renders only when
  // the server is down and something was actually recognised.
  if (myNav === _navSeq) await renderDiagnosis(root, true);
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
  // Never name locals h/t here — they shadow the global DOM builder and the
  // i18n function for this whole scope (a `let t` bug already cost us a
  // broken Types editor).
  const hrs = Math.floor(s / 3600);
  const mins = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const pad = (n) => String(n).padStart(2, '0');
  return `${pad(hrs)}:${pad(mins)}:${pad(sec)}`;
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
    { kind: 'route', label: t('nav.players')   || 'Players',    route: 'players' },
    { kind: 'route', label: t('nav.rcon')      || 'RCon',       route: 'rcon' },
    { kind: 'route', label: t('nav.validator') || 'Validator',  route: 'validator' },
    { kind: 'route', label: t('nav.weather')   || 'Weather & Time', route: 'weather' },
    { kind: 'route', label: t('nav.sync')      || 'Sync',       route: 'sync' },
    { kind: 'route', label: t('nav.wipe')      || 'Wipe',       route: 'wipe' },
    { kind: 'route', label: t('nav.gameplay')  || 'Gameplay',   route: 'gameplay' },
    { kind: 'route', label: t('nav.attach')    || 'Attachments', route: 'attachments' },
    { kind: 'route', label: t('nav.settings')  || 'Settings',   route: 'settings' },
    { kind: 'action', label: (t('action.start') || 'Start server'),
      run: async () => { try { await api.post('/api/server/start'); toast('starting', 'ok'); refreshStatus(); } catch (e) { handleErr(e); } } },
    { kind: 'action', label: (t('action.stop') || 'Stop server'),
      run: async () => { try { await api.post('/api/server/stop'); toast('stopping', 'ok'); refreshStatus(); } catch (e) { handleErr(e); } } },
    { kind: 'action', label: (t('action.restart') || 'Restart server'),
      run: async () => { try { await api.post('/api/server/restart'); toast('restarting', 'ok'); refreshStatus(); } catch (e) { handleErr(e); } } },
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

// confirmModal replaces the native confirm() with a styled modal. Resolves to
// true only when the confirm button is pressed; Esc / backdrop / Cancel → false.
function confirmModal(message, opts = {}) {
  return new Promise((resolve) => {
    let result = false;
    const m = openModal({ title: opts.title || t('confirm.title'), onClose: () => resolve(result) });
    const done = (v) => { result = v; m.close(); };
    const okBtn = h('button', { class: opts.danger ? 'danger' : 'primary',
      text: opts.okText || t('action.confirm'), onclick: () => done(true) });
    const cancelBtn = h('button', { i18n: 'action.cancel', onclick: () => done(false) });
    m.body.append(
      h('p', { text: message, style: { marginBottom: '4px' } }),
      h('div', { class: 'actions' }, [okBtn, cancelBtn]),
    );
    // Destructive confirms default-focus Cancel so a stray Enter can't destroy
    // anything; harmless confirms keep the fast path on OK.
    setTimeout(() => (opts.danger ? cancelBtn : okBtn).focus(), 0);
  });
}

// promptModal replaces the native prompt(): a styled single-input dialog.
// Resolves to the trimmed value on confirm, or null on Esc / backdrop / Cancel /
// empty. Enter in the field confirms.
function promptModal(message, opts = {}) {
  return new Promise((resolve) => {
    let result = null;
    const m = openModal({ title: opts.title || t('confirm.title'), onClose: () => resolve(result) });
    const input = h('input', { type: 'text', value: opts.value || '',
      placeholder: opts.placeholder || '', style: { width: '100%', marginBottom: '10px' } });
    const done = (v) => { result = v; m.close(); };
    const submit = () => { const v = input.value.trim(); done(v || null); };
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); submit(); }
    });
    m.body.append(
      h('p', { text: message, style: { marginBottom: '8px' } }),
      input,
      h('div', { class: 'actions' }, [
        h('button', { class: 'primary', text: opts.okText || t('action.create'), onclick: submit }),
        h('button', { i18n: 'action.cancel', onclick: () => done(null) }),
      ]),
    );
    setTimeout(() => input.focus(), 0);
  });
}

// perfChart renders one single-series time chart (SVG line + soft area) with
// a crosshair tooltip. Gaps (null values — e.g. server stopped for CPU/RAM)
// break the line instead of drawing to zero. Single series → the card title
// names it, no legend needed.
function perfChart(samples, opts) {
  const W = 560, H = 120, PAD_L = 6, PAD_R = 6, PAD_T = 8, PAD_B = 18;
  const vals = samples.map(opts.val);
  let min = Infinity, max = -Infinity;
  for (const v of vals) { if (v == null) continue; if (v < min) min = v; if (v > max) max = v; }
  if (!isFinite(min)) { min = 0; max = 1; }
  if (min === max) { max = min + 1; min = Math.max(0, min - 1); }
  // Zero-based when the data starts near zero — bars/areas mislead otherwise.
  if (min > 0 && min < max * 0.5) min = 0;
  const t0 = samples[0].t, t1 = samples[samples.length - 1].t || t0 + 1;
  const X = ts => PAD_L + (W - PAD_L - PAD_R) * (ts - t0) / Math.max(1, t1 - t0);
  const Y = v => PAD_T + (H - PAD_T - PAD_B) * (1 - (v - min) / (max - min));

  // Build path segments, breaking at nulls.
  let line = '', area = '', segStart = null;
  for (let i = 0; i < samples.length; i++) {
    const v = vals[i];
    if (v == null) {
      if (segStart != null) { area += ` L ${X(samples[i - 1].t).toFixed(1)} ${Y(min).toFixed(1)} Z`; segStart = null; }
      continue;
    }
    const x = X(samples[i].t).toFixed(1), y = Y(v).toFixed(1);
    if (segStart == null) {
      line += ` M ${x} ${y}`;
      area += ` M ${x} ${Y(min).toFixed(1)} L ${x} ${y}`;
      segStart = i;
    } else {
      line += ` L ${x} ${y}`;
      area += ` L ${x} ${y}`;
    }
  }
  if (segStart != null) area += ` L ${X(t1).toFixed(1)} ${Y(min).toFixed(1)} Z`;

  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', `0 0 ${W} ${H}`);
  svg.setAttribute('class', 'chart-svg');
  svg.setAttribute('preserveAspectRatio', 'none');
  const mk = (tag, attrs) => {
    const el = document.createElementNS('http://www.w3.org/2000/svg', tag);
    for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
    return el;
  };
  // Recessive grid: 3 horizontal hairlines.
  for (const f of [0, 0.5, 1]) {
    const y = (PAD_T + (H - PAD_T - PAD_B) * f).toFixed(1);
    svg.append(mk('line', { x1: PAD_L, x2: W - PAD_R, y1: y, y2: y, class: 'chart-grid' }));
  }
  svg.append(mk('path', { d: area || 'M0 0', class: 'chart-area' }));
  svg.append(mk('path', { d: line || 'M0 0', class: 'chart-line' }));
  const cross = mk('line', { x1: 0, x2: 0, y1: PAD_T, y2: H - PAD_B, class: 'chart-cross', style: 'display:none' });
  const dot = mk('circle', { r: 3, class: 'chart-dot', style: 'display:none' });
  svg.append(cross, dot);

  const tip = h('div', { class: 'chart-tip', style: { display: 'none' } });
  const head = h('div', { class: 'chart-head' }, [
    h('span', { class: 'chart-label', text: opts.label }),
    h('span', { class: 'chart-minmax', text: `${opts.fmt(min)} – ${opts.fmt(max)}` }),
  ]);
  const timeAxis = h('div', { class: 'chart-x' }, [
    h('span', { text: fmtClock(t0) }), h('span', { text: fmtClock((t0 + t1) / 2) }), h('span', { text: fmtClock(t1) }),
  ]);
  const box = h('div', { class: 'chart-box' }, [head, svg, timeAxis, tip]);

  // Crosshair + tooltip: nearest sample to the pointer.
  svg.addEventListener('mousemove', ev => {
    const r = svg.getBoundingClientRect();
    const frac = (ev.clientX - r.left) / r.width;
    const tt = t0 + frac * (t1 - t0);
    let best = 0, bd = Infinity;
    for (let i = 0; i < samples.length; i++) {
      const d = Math.abs(samples[i].t - tt);
      if (d < bd) { bd = d; best = i; }
    }
    const v = vals[best];
    const x = X(samples[best].t);
    cross.setAttribute('x1', x); cross.setAttribute('x2', x);
    cross.style.display = '';
    if (v != null) {
      dot.setAttribute('cx', x); dot.setAttribute('cy', Y(v));
      dot.style.display = '';
      tip.textContent = `${fmtClock(samples[best].t)} — ${opts.fmt(v)}`;
    } else {
      dot.style.display = 'none';
      tip.textContent = `${fmtClock(samples[best].t)} — ${t('status.stopped')}`;
    }
    tip.style.display = '';
    const px = (x / W) * r.width;
    tip.style.left = Math.min(Math.max(px, 40), r.width - 60) + 'px';
  });
  svg.addEventListener('mouseleave', () => {
    cross.style.display = 'none'; dot.style.display = 'none'; tip.style.display = 'none';
  });
  return box;
}

function fmtClock(unixSec) {
  const d = new Date(unixSec * 1000);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
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
    'steamProtocolMaxDataSize',
  ];
  // Only the keys whose effect is non-obvious get an explanation; the rest
  // are self-describing and a marker on every row would be noise.
  const CFG_HELP = {
    maxPlayers: 'server.maxPlayers',
    instanceId: 'server.instanceId',
    verifySignatures: 'server.verifySignatures',
    disable3rdPerson: 'server.thirdPerson',
    disableVoN: 'server.von',
    password: 'rcon.password',
  };
  for (const k of KEYS) {
    const val = data.values[k] ?? '';
    const input = h('input', { type: 'text', value: val });
    bag[k] = input;
    const label = h('label', { text: k });
    form.append(h('div', {}, [
      CFG_HELP[k] ? withHelp(label, CFG_HELP[k]) : label,
      input,
    ]));
  }

  const missionSelect = h('select', { id: 'mission-input', 'data-help': 'server.mission' });
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
              const tgt = await promptModal(t('mission.duplicate.prompt'), { value: `${src}.copy`, title: t('mission.duplicate') });
              if (!tgt) return;
              try {
                await api.post('/api/missions/duplicate', { source: src, target: tgt });
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
            const NUMERIC = new Set(['maxPlayers','serverTimeAcceleration','serverNightTimeAcceleration',
              'serverTimePersistent','enableWhitelist','disableVoN','vonCodecQuality','disable3rdPerson',
              'disableCrosshair','disablePersonalLight','verifySignatures','forceSameBuild','instanceId',
              'storageAutoFix','loginQueueConcurrentPlayers','loginQueueMaxPlayers','steamProtocolMaxDataSize']);
            const patch = {};
            for (const k of KEYS) {
              const v = bag[k].value.trim();
              // Coerce ONLY known-numeric keys — hostname "123", a numeric
              // password or a zero-padded value must survive as strings.
              patch[k] = (NUMERIC.has(k) && v !== '' && !isNaN(Number(v))) ? Number(v) : v;
            }
            try {
              await api.post('/api/servercfg', patch);
              toast(t('msg.saved'), 'ok');
            } catch (e) { handleErr(e); }
          }
        }),
      ]),
    ])
  );
};

// --------------------------------------------------------------------- mods

Views.mods = async (root) => {
  // Heavy fetch ahead — remember which navigation we are. If the user
  // switches section while it runs, we must not append into their new page.
  const myNav = _navSeq;
  const d = await api.get('/api/mods');
  root.append(pageHeader('nav.mods', 'mods.subtitle'));
  const wrap = h('div');

  if (State.serverStatus.running) wrap.append(runningBanner());
  if (!d.vanillaPath) wrap.append(h('div', { class: 'warning-bar', text: t('mods.noClientFolder') }));

  const outdatedCount = d.mods.filter(m => m.updateAvailable).length;

  const tbl = h('table');
  tbl.append(
    h('thead', {}, h('tr', {}, [
      h('th', { i18n: 'col.mod' }),
      h('th', { i18n: 'col.status' }),
      h('th', { i18n: 'col.workshop' }),
      h('th', { i18n: 'col.keys' }),
      h('th', { i18n: 'col.size' }),
      h('th', { i18n: 'col.active' }),
      h('th', {}, [h('span', { i18n: 'col.serverMod' }), help('mods.serverSide')]),
      h('th', { text: '' }),
    ]))
  );
  const tbody = h('tbody');
  for (const m of d.mods) {
    // Launch toggle. A mod that isn't installed in the server dir can't be
    // enabled — DayZServer would fail to start — so we lock it with a padlock
    // and a hint to install it first (the backend rejects it too).
    let activeControl;
    if (m.installedInServer) {
      const activeCb = h('input', { type: 'checkbox', class: 'switch' });
      activeCb.checked = (d.activeMods || []).includes(m.name);
      activeCb.onchange = async () => {
        const prev = !activeCb.checked; // value before this toggle
        activeCb.disabled = true;
        try {
          const r = await api.post('/api/mods/enable', { mod: m.name, enabled: activeCb.checked });
          // Keep State.config in sync so a later config POST (language switch,
          // Settings save) can never send a stale mod list back (hotfix).
          if (r) { State.config.mods = r.active; State.config.serverMods = r.server; }
        } catch (e) { handleErr(e); activeCb.checked = prev; }
        finally { activeCb.disabled = false; }
      };
      activeControl = activeCb;
    } else {
      activeControl = h('span', {
        class: 'lock-indicator',
        title: t('mods.installFirst') || 'Install the mod before enabling it',
        text: '🔒',
      });
    }

    // Server-side toggle (-serverMod=). Independent of Active (-mod=). Confirmed
    // on enable so nobody flags a client mod as server-only by accident.
    let serverControl;
    if (m.installedInServer) {
      const srvCb = h('input', { type: 'checkbox', class: 'switch' });
      srvCb.checked = (d.serverMods || []).includes(m.name);
      srvCb.onchange = async () => {
        if (srvCb.checked && !(await confirmModal(t('mods.serverMod.confirm'), { danger: true, okText: t('action.save') }))) { srvCb.checked = false; return; }
        const prev = !srvCb.checked;
        srvCb.disabled = true;
        try {
          const r = await api.post('/api/mods/enable', { mod: m.name, enabled: srvCb.checked, serverSide: true });
          if (r) { State.config.mods = r.active; State.config.serverMods = r.server; }
          toast(t('msg.saved'), 'ok');
        } catch (e) { handleErr(e); srvCb.checked = prev; }
        finally { srvCb.disabled = false; }
      };
      serverControl = srvCb;
    } else {
      serverControl = h('span', { class: 'lock-indicator', title: t('mods.installFirst'), text: '🔒' });
    }

    const actions = h('div', { class: 'actions', style: { margin: 0 } });
    if (!m.installedInServer && m.availableInWorkshop) {
      actions.append(h('button', { class: 'primary', i18n: 'action.install',
        onclick: (e) => withBusy(e.currentTarget, async () => { try { await api.post('/api/mods/install', { mod: m.name }); toast(t('msg.installed'),'ok'); await navigate('mods'); } catch (err) { handleErr(err); } })}));
    }
    if (m.installedInServer && m.availableInWorkshop) {
      actions.append(h('button', {
        class: m.updateAvailable ? 'primary' : '',
        i18n: 'action.update',
        onclick: (e) => withBusy(e.currentTarget, async () => {
          try { await api.post('/api/mods/update', { mod: m.name }); toast(t('msg.updated'),'ok'); await navigate('mods'); }
          catch (err) { handleErr(err); }
        })
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
        onclick: async (ev) => {
          const btn = ev.currentTarget; // must be captured before the await
          if (!(await confirmModal(t('confirm.uninstall').replace('{mod}', m.name), { danger: true, okText: t('action.delete') }))) return;
          await withBusy(btn, async () => {
            try { await api.post('/api/mods/uninstall', { mod: m.name }); toast(t('msg.removed'),'ok'); await navigate('mods'); } catch (e) { handleErr(e); }
          });
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
      h('td', {}, activeControl),
      h('td', {}, serverControl),
      h('td', {}, actions),
    ]);
    if (m.publishedId) tr.dataset.published = m.publishedId;
    tbody.append(tr);
  }
  // Empty state: a fresh setup with zero mods otherwise renders a bare header
  // row with no explanation of what to do next.
  if (!d.mods.length) {
    tbody.append(h('tr', {}, h('td', { colspan: '8' },
      h('div', { class: 'empty-state' }, [
        h('div', { class: 'es-title', i18n: 'mods.title' }),
        h('div', { class: 'es-hint', i18n: 'mods.syncAll.empty.hint' }),
      ]))));
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
    h('button', { i18n: 'mods.refreshList',
      title: t('mods.refreshList.hint') || 'Re-scan !Workshop and the server folder',
      onclick: () => navigate('mods'),
    }),
    h('button', { i18n: 'action.syncKeys',
      onclick: async () => { try { await api.post('/api/mods/sync-keys'); toast(t('msg.keysSynced'),'ok'); } catch (e) { handleErr(e); } }
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

  if (myNav !== _navSeq) return; // superseded while /api/mods was loading
  wrap.append(h('div', { class: 'card' }, [
    h('h2', { i18n: 'mods.title' }),
    toolbar,
    tbl,
  ]));
  wrap.append(collectionCard);

  // Load-order (drag-to-reorder) panel. Reflects the current config.mods list.
  const orderWrap = h('div', { class: 'card' }, [
    withHelp(h('h3', { i18n: 'mods.loadOrder' }), 'mods.order'),
    h('p', { class: 'hint', i18n: 'mods.loadOrder.hint' }),
  ]);
  const orderList = h('div', { class: 'order-list' });
  const order = [...(d.activeMods || [])];

  // applyMove persists a reorder; on failure the previous order is restored so
  // the UI never silently diverges from what the server actually launches.
  async function applyMove(from, to) {
    if (isNaN(from) || isNaN(to) || from === to || to < 0 || to >= order.length) return;
    const before = [...order];
    const [moved] = order.splice(from, 1);
    order.splice(to, 0, moved);
    renderOrder();
    try {
      await api.post('/api/mods/order', { mods: order, serverSide: false });
      toast(t('msg.saved'), 'ok');
    } catch (err) {
      handleErr(err);
      order.length = 0; order.push(...before);
      renderOrder();
    }
  }

  function renderOrder() {
    orderList.innerHTML = '';
    for (let i = 0; i < order.length; i++) {
      const name = order[i];
      // Up/down buttons work everywhere HTML5 drag-and-drop doesn't: touch
      // screens and keyboards.
      const arrows = h('span', { class: 'order-arrows' }, [
        h('button', { text: '↑', 'aria-label': 'up', onclick: () => applyMove(i, i - 1) }),
        h('button', { text: '↓', 'aria-label': 'down', onclick: () => applyMove(i, i + 1) }),
      ]);
      const row = h('div', { class: 'order-row', draggable: 'true' }, [
        h('span', { class: 'drag-handle', text: '⋮⋮' }),
        h('span', { text: `${i + 1}. ${name}` }),
        arrows,
      ]);
      row.dataset.index = String(i);
      row.addEventListener('dragstart', e => {
        row.classList.add('dragging');
        e.dataTransfer.setData('text/plain', String(i));
        e.dataTransfer.effectAllowed = 'move';
      });
      row.addEventListener('dragend', () => row.classList.remove('dragging'));
      row.addEventListener('dragover', e => { e.preventDefault(); e.dataTransfer.dropEffect = 'move'; });
      row.addEventListener('drop', e => {
        e.preventDefault();
        applyMove(Number(e.dataTransfer.getData('text/plain')), Number(row.dataset.index));
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
  // Heavy fetch ahead — remember which navigation we are. If the user
  // switches section while it runs, we must not append into their new page.
  const myNav = _navSeq;
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
  let sortKey = 'name';   // click a column header to sort (item #8)
  let sortDir = 1;        // 1 = asc, -1 = desc

  // Inline editing (item #8): unsaved edits live here (name -> {field: value})
  // and survive sort/search/page changes until saved or discarded.
  const dirty = {};
  const dirtyBtn = h('button', { class: 'primary', onclick: () => saveDirty() });
  const discardBtn = h('button', { onclick: () => { clearDirty(); refreshTable(); } });
  function clearDirty() { for (const k of Object.keys(dirty)) delete dirty[k]; }
  function updateDirtyUI() {
    const n = Object.keys(dirty).length;
    dirtyBtn.textContent = t('types.saveChanges') + (n ? ` (${n})` : '');
    discardBtn.textContent = t('types.discard');
    dirtyBtn.style.display = n ? '' : 'none';
    discardBtn.style.display = n ? '' : 'none';
  }
  async function saveDirty() {
    const names = Object.keys(dirty);
    if (!names.length) return;
    try {
      for (const name of names) {
        const dd = dirty[name], patch = {};
        for (const f of ['nominal', 'min', 'lifetime']) {
          if (f in dd) { const v = parseInt(dd[f], 10); if (Number.isFinite(v)) patch[f] = v; }
        }
        if ('category' in dd) patch.category = dd.category;
        if (Object.keys(patch).length) {
          await api.post('/api/types/bulk-patch', { file: fileSelect.value, names: [name], patch });
        }
      }
      clearDirty();
      toast(t('msg.saved'), 'ok');
      await refreshTable();
    } catch (e) { handleErr(e); }
  }

  const presetsWrap = h('div', { class: 'card' }, [h('h3', { i18n: 'types.presets' })]);
  try {
    const presets = await api.get('/api/types/presets');
    const pills = h('div', { class: 'pills' });
    for (const p of presets) {
      pills.append(h('button', {
        class: 'pill', text: State.lang === 'ru' ? p.labelRu : p.label,
        onclick: async () => {
          const selected = [...tableWrap.querySelectorAll('input[type=checkbox][data-name]:checked')]
            .map(cb => cb.dataset.name);
          if (!selected.length) { toast(t('msg.selectFirst')); return; }
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
    const selected = [...tableWrap.querySelectorAll('input[type=checkbox][data-name]:checked')]
      .map(cb => cb.dataset.name);
    if (!selected.length) { toast(t('msg.selectFirst')); return; }
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
            if (Object.keys(patch).length === 0) { toast(t('msg.setOneField')); return; }
            try {
              const r = await api.post('/api/types/bulk-patch', {
                file: fileSelect.value, names: selected, patch,
              });
              toast(t('msg.patched').replace('{n}', r.touched), 'ok');
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

    // Sort by the active column (numeric columns numerically, text columns
    // alphabetically), then paginate.
    const numericKeys = { nominal: 1, min: 1, lifetime: 1 };
    const sorted = filtered.slice().sort((a, b) => {
      const av = a[sortKey], bv = b[sortKey];
      if (numericKeys[sortKey]) return ((Number(av) || 0) - (Number(bv) || 0)) * sortDir;
      return String(av || '').localeCompare(String(bv || '')) * sortDir;
    });

    const totalPages = Math.max(1, Math.ceil(sorted.length / PAGE_SIZE));
    if (currentPage > totalPages) currentPage = totalPages;
    if (currentPage < 1) currentPage = 1;
    const start = (currentPage - 1) * PAGE_SIZE;
    const slice = sorted.slice(start, start + PAGE_SIZE);

    const tbl = h('table');
    const headCb = h('input', { type: 'checkbox', title: t('types.selectPage') });
    headCb.onchange = () => {
      tableWrap.querySelectorAll('input[type=checkbox][data-name]').forEach(cb => cb.checked = headCb.checked);
    };
    const sortTh = (label, key, num) => {
      const active = sortKey === key;
      return h('th', {
        class: (num ? 'num ' : '') + 'sortable',
        style: { cursor: 'pointer', userSelect: 'none' },
        text: label + (active ? (sortDir === 1 ? '  ▲' : '  ▼') : ''),
        onclick: () => { if (active) sortDir = -sortDir; else { sortKey = key; sortDir = 1; } refreshTable(); },
      });
    };
    tbl.append(h('thead', {}, h('tr', {}, [
      h('th', {}, headCb),
      sortTh(t('col.name'), 'name', false),
      hlp(sortTh(t('types.field.nominal'), 'nominal', true), 'types.nominal'),
      hlp(sortTh(t('types.field.min'), 'min', true), 'types.min'),
      hlp(sortTh(t('types.field.lifetime'), 'lifetime', true), 'types.lifetime'),
      hlp(sortTh(t('types.field.category'), 'category', false), 'types.category'),
      h('th', { text: '' }),
    ])));
    const tbody = h('tbody');
    for (const row of slice) {
      const cb = h('input', { type: 'checkbox' });
      cb.dataset.name = row.name;
      let tr;
      const mkCell = (field, numeric) => {
        const has = dirty[row.name] && (field in dirty[row.name]);
        const inp = h('input', {
          type: numeric ? 'number' : 'text', class: 'inline-cell',
          value: has ? dirty[row.name][field] : (row[field] ?? ''),
        });
        inp.onchange = () => {
          const orig = row[field] ?? '';
          if (String(inp.value) === String(orig)) {
            if (dirty[row.name]) {
              delete dirty[row.name][field];
              if (!Object.keys(dirty[row.name]).length) delete dirty[row.name];
            }
          } else {
            (dirty[row.name] = dirty[row.name] || {})[field] = inp.value;
          }
          if (tr) tr.classList.toggle('dirty', !!dirty[row.name]);
          updateDirtyUI();
        };
        return h('td', { class: numeric ? 'num' : '' }, inp);
      };
      tr = h('tr', {}, [
        h('td', {}, cb),
        h('td', { text: row.name }),
        mkCell('nominal', true),
        mkCell('min', true),
        mkCell('lifetime', true),
        mkCell('category', false),
        h('td', {}, h('button', { i18n: 'action.edit', onclick: () => openEditor(row.name) })),
      ]);
      if (dirty[row.name]) tr.classList.add('dirty');
      tbody.append(tr);
    }
    tbl.append(tbody);
    updateDirtyUI();
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
    // NOTE: never name a local `t` — that shadows the global i18n t() for the
    // whole function scope and every t('key') call inside throws
    // "t is not a function" (this broke Save/Delete in this very editor).
    let item;
    try {
      item = await api.get(`/api/types/item?file=${encodeURIComponent(fileSelect.value)}&name=${encodeURIComponent(name)}`);
    } catch (e) { handleErr(e); return; }
    const m = openModal({ title: name, wide: true });
    const fields = {
      nominal: h('input', { type: 'number', value: item.nominal ?? '', 'data-help': 'types.nominal' }),
      min: h('input', { type: 'number', value: item.min ?? '' }),
      lifetime: h('input', { type: 'number', value: item.lifetime ?? '' }),
      restock: h('input', { type: 'number', value: item.restock ?? '' }),
      quantmin: h('input', { type: 'number', value: item.quantmin ?? '' }),
      quantmax: h('input', { type: 'number', value: item.quantmax ?? '' }),
      cost: h('input', { type: 'number', value: item.cost ?? '' }),
      category: h('input', { type: 'text',   value: item.category?.name || '' }),
    };
    const FIELD_HELP = {
      nominal: 'types.nominal', min: 'types.min', lifetime: 'types.lifetime',
      restock: 'types.restock', quantmin: 'types.quant', quantmax: 'types.quant',
      cost: 'types.cost', category: 'types.category',
    };
    const grid = h('div', { class: 'grid-3' });
    grid.append(
      ...Object.entries(fields).map(([k, el]) => {
        const label = h('label', { text: k });
        return h('div', {}, [FIELD_HELP[k] ? withHelp(label, FIELD_HELP[k]) : label, el]);
      })
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

    const usages = item.usages || [];
    const values = item.values || [];
    const tags = item.tags || [];
    const lists = h('div', { class: 'grid-3' }, [
      editableList('usages', usages, v => {}),
      editableList('values', values, v => {}),
      editableList('tags', tags, v => {}),
    ]);

    // Flags.
    const flags = item.flags || {};
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
              toast(t('msg.saved'), 'ok');
              m.close();
              await refreshTable();
            } catch (e) { handleErr(e); }
          }
        }),
        h('button', { class: 'danger', i18n: 'action.delete',
          onclick: async () => {
            if (!(await confirmModal(t('confirm.delete').replace('{name}', name), { danger: true, okText: t('action.delete') }))) return;
            try {
              await api.del(`/api/types/item?file=${encodeURIComponent(fileSelect.value)}&name=${encodeURIComponent(name)}`);
              toast(t('msg.deleted'), 'ok');
              m.close();
              await refreshTable();
            } catch (e) { handleErr(e); }
          }
        }),
        h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
      ]),
    );
  }

  fileSelect.onchange = () => { clearDirty(); currentPage = 1; refreshTable(); };
  let _searchT = 0;
  search.oninput = () => { clearTimeout(_searchT); _searchT = setTimeout(() => { currentPage = 1; refreshTable(); }, 250); };

  if (myNav !== _navSeq) return;
  if (State.serverStatus.running) root.append(runningBanner());
  root.append(
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'types.title' }),
      h('div', { class: 'toolbar' }, [
        fileSelect, search,
        h('button', { i18n: 'types.bulk', onclick: openBulkEdit }),
        h('span', { class: 'grow' }),
        discardBtn,
        dirtyBtn,
      ]),
      h('p', { class: 'hint', i18n: 'types.inline.hint' }),
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
    h('th', { i18n: 'col.file' }),
    h('th', { i18n: 'col.size' }),
    h('th', { i18n: 'col.registered' }),
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
          if (!(await confirmModal(t('confirm.delete').replace('{name}', f.name), { danger: true, okText: t('action.delete') }))) return;
          try { await api.post('/api/moded/delete', { fileName: f.name }); toast(t('msg.deleted'),'ok'); await navigate('moded'); }
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
  const editorHost = h('textarea', { placeholder: t('files.selectFile') });
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
    backupList.append(withHelp(h('h3', { i18n: 'backup.title' }), 'settings.backup'));
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
          // See what restoring would change before committing to it.
          h('button', { class: 'secondary', i18n: 'backup.diff',
            onclick: () => openBackupDiff(currentPath, b.name) }),
          h('button', { i18n: 'backup.restore', 'data-help': 'backup.restore', onclick: async () => {
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
        // Any error here (413 for a >2 MB file, 404 for one that vanished)
        // used to be an unhandled rejection: no toast, nothing happened.
        try {
        if (e.isDir) return await loadDir(e.path);
        const r = await api.get(`/api/files/read?path=${encodeURIComponent(e.path)}`);
        currentPath = e.path;
        setMode(e.name);
        setValue(r.content);
        pathLabel.textContent = e.path;
        await refreshBackups();
        } catch (err) { handleErr(err); }
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
        h('th', { i18n: 'col.severity' }),
        h('th', { i18n: 'col.file' }),
        h('th', { i18n: 'col.line' }),
        h('th', { i18n: 'col.message' }),
      ])));
      const tb = h('tbody');
      for (const i of d.issues) {
        const cls = i.severity === 'error' ? 'err' : (i.severity === 'warning' ? 'warn' : 'mute');
        tb.append(h('tr', {}, [
          h('td', {}, h('span', { class: `badge ${cls}`, text: t('validator.severity.' + i.severity) || i.severity })),
          h('td', { text: i.file }),
          h('td', { text: i.line || '' }),
          h('td', { text: i.message }),
        ]));
      }
      tbl.append(tb);
      listEl.append(tbl);
    } catch (e) { handleErr(e); }
  };
  const autofix = async (e) => {
    const btn = e.currentTarget;
    btn.disabled = true;
    try {
      const r = await api.post('/api/validate/fix', {});
      if (!r.count) toast(t('validator.fix.none'), 'ok');
      else toast(t('validator.fix.done').replace('{n}', r.count), 'ok');
      await run();
    } catch (e) { handleErr(e); }
    finally { btn.disabled = false; }
  };
  root.append(
    h('div', { class: 'card' }, [
      withHelp(h('h2', { i18n: 'validator.title' }), 'validator.severity'),
      h('div', { class: 'actions' }, [
        h('button', { class: 'primary', i18n: 'action.validate', onclick: run }),
        h('button', { i18n: 'validator.fix', onclick: autofix, 'data-help': 'validator.autofix' }),
      ]),
      h('p', { class: 'hint', i18n: 'validator.fix.hint' }),
      listEl,
    ])
  );
  run();
};

// --------------------------------------------------------------------- logs

Views.logs = async (root) => {
  // All three MUST come before the first await: navigate() has already run the
  // previous view's teardown, so a late assignment lands on the NEXT view and
  // leaks this EventSource for the rest of the session.
  const myNav = _navSeq;
  let source;
  root._teardown = () => { if (source) source.close(); };

  const sources = await api.get('/api/logs/list');
  if (myNav !== _navSeq) return; // superseded while the list was loading
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
  const streamBadge = h('span', { class: 'badge mute', style: { display: 'none' } });

  const MAX_CHARS = 200_000;

  // Sticky-bottom autoscroll: scrolling up suspends following so the user can
  // read; scrolling back to the bottom resumes it. The checkbox is the master
  // switch.
  let stickBottom = true;
  pane.addEventListener('scroll', () => {
    stickBottom = pane.scrollHeight - pane.scrollTop - pane.clientHeight < 24;
  });

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
      if (autoScroll.checked && stickBottom) pane.scrollTop = pane.scrollHeight;
    }, 200);
  }
  // Un-pausing must flush whatever buffered while paused — otherwise the pane
  // stays frozen until the next incoming chunk (possibly never on a quiet log).
  paused.onchange = () => { if (!paused.checked) scheduleFlush(); };
  function append(text) {
    pending += text;
    if (pending.length > MAX_CHARS) pending = pending.slice(-MAX_CHARS);
    scheduleFlush();
  }

  async function attach(id) {
    if (source) { source.close(); source = null; }
    pending = '';
    pane.textContent = '';
    stickBottom = true;
    try {
      const r = await api.get(`/api/logs/read?id=${encodeURIComponent(id)}`);
      append(r.content || '');
    } catch (e) { append(`[snapshot failed: ${e.message}]\n`); }

    source = new EventSource(`/api/logs/stream?id=${encodeURIComponent(id)}`, { withCredentials: true });
    // Connection state lives in a badge, not injected into the log text —
    // a stopped server otherwise steadily fills the pane with reconnect noise.
    source.onopen = () => { streamBadge.style.display = 'none'; };
    source.onmessage = ev => { streamBadge.style.display = 'none'; append(ev.data + '\n'); };
    source.onerror = () => {
      streamBadge.textContent = t('logs.reconnecting');
      streamBadge.style.display = '';
    };
  }

  picker.onchange = () => attach(picker.value);

  root.append(
    h('div', { class: 'card' }, [
      h('h2', { i18n: 'nav.logs' }),
      h('div', { class: 'toolbar' }, [
        picker,
        h('label', {}, [autoScroll, h('span', { i18n: 'logs.autoscroll' })]),
        h('label', {}, [paused, h('span', { i18n: 'logs.pause' })]),
        h('button', { i18n: 'logs.clear', onclick: () => { pane.textContent = ''; pending = ''; } }),
        streamBadge,
        h('span', { class: 'spacer' }),
        h('small', { class: 'hint', i18n: 'logs.capHint' }),
      ]),
      pane,
    ]),
  );

  // Attach to the first source that actually has a file — sources[0] may be a
  // placeholder ("no file yet") while a later entry holds real content.
  const first = sources.find(s => s.exists) || sources[0];
  if (first) { picker.value = first.id; await attach(first.id); }
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
  // Capture the navigation sequence up front. This view awaits before it starts
  // its 5s poll; if the user navigates away during those awaits, navigate() has
  // already torn down and moved on, so we must not start (or leave) a timer that
  // would leak and write into detached DOM. _navSeq changing means superseded.
  const myNav = _navSeq;
  await refreshStatus();
  // Pull current RCon settings so the access form is prefilled and the status
  // badge is accurate regardless of whether the server is running.
  let cfg = State.config || {};
  try { cfg = await api.get('/api/config'); State.config = cfg; } catch {}
  if (myNav !== _navSeq) return; // navigated away while awaiting — bail cleanly

  const card = h('div', { class: 'card' }, [h('h2', { i18n: 'nav.rcon' })]);
  const status = h('div', { class: 'hint' });
  const players = h('div', { class: 'players-grid' });
  const sayInp = h('input', { type: 'text', placeholder: t('rcon.broadcast.ph') });
  const cmdInp = h('input', { type: 'text', placeholder: t('rcon.rawCommand.ph') });
  const cmdOut = h('pre', { class: 'log-pane', style: { height: '180px' } });

  let timer = 0;
  // Never arm the poll if this view has been superseded (navigated away) — the
  // interval would outlive the view and write into detached DOM until the next
  // navigation cleared it.
  const startPolling = () => { if (myNav !== _navSeq) return; if (!timer) timer = setInterval(refresh, 5000); };
  const stopPolling  = () => { if (timer) { clearInterval(timer); timer = 0; } };

  // --- Always-visible access card. The RCon password can be set/changed at
  // any time (even with the server stopped — the correct order, since BattlEye
  // reads beserver_x64.cfg at launch). Saving POSTs the config, which writes
  // beserver_x64.cfg on the backend; it takes effect on the next server start.
  const passInp = h('input', { type: 'password', value: cfg.rconPassword || '',
    placeholder: t('rcon.password'), 'data-help': 'rcon.password',
    style: { flex: '1', minWidth: '180px' } });
  const showChk = h('input', { type: 'checkbox' });
  showChk.onchange = () => { passInp.type = showChk.checked ? 'text' : 'password'; };
  const portInp = h('input', { type: 'number', value: cfg.rconPort || '',
    placeholder: Number(cfg.serverPort) ? (Number(cfg.serverPort) + 4) : 'auto', style: { width: '130px' } });
  const accessBadge = h('span', { class: 'badge ' + (cfg.rconPassword ? 'ok' : 'mute'),
    i18n: cfg.rconPassword ? 'rcon.access.configured' : 'rcon.access.notset' });

  const accessCard = h('div', { class: 'card' }, [
    h('h3', { i18n: 'rcon.access.title' }),
    h('div', { class: 'row', style: { marginBottom: '8px' } }, [accessBadge]),
    h('div', { class: 'row', style: { gap: '8px' } }, [
      passInp, portInp,
      h('button', { class: 'primary', i18n: 'action.save', onclick: async () => {
        try {
          const next = { ...(await api.get('/api/config')) };
          next.rconPassword = passInp.value.trim();
          const p = parseInt(portInp.value, 10);
          next.rconPort = (Number.isFinite(p) && p > 0) ? p : 0;
          await api.post('/api/config', next);
          State.config = next;
          toast(t('rcon.savedRestart'), 'ok');
          await navigate('rcon');
        } catch (e) { handleErr(e); }
      }}),
    ]),
    h('label', { style: { display: 'inline-flex', gap: '8px', alignItems: 'center', marginTop: '8px' } },
      [showChk, h('span', { i18n: 'rcon.showPassword' })]),
    h('p', { class: 'hint', i18n: 'rcon.notConfigured.hint' }),
  ]);

  async function refresh() {
    // If the server isn't running there's nothing to connect to. Show a calm
    // hint instead of the password form. We keep the poll alive (there's no
    // input form to clobber here) but return before any network call, so it
    // costs nothing while down and auto-recovers once the server is up.
    if (!State.serverStatus.running) {
      startPolling();
      players.innerHTML = '';
      status.textContent = t('rcon.serverOffline') || 'Server is not running — start it to use RCon.';
      return;
    }
    try {
      const d = await api.get('/api/rcon/players');
      players.innerHTML = '';
      players.append(
        h('div', { class: 'head', i18n: 'col.id' }),
        h('div', { class: 'head', i18n: 'col.name' }),
        h('div', { class: 'head', i18n: 'col.guid' }),
        h('div', { class: 'head', i18n: 'col.ping' }),
        h('div', { class: 'head', text: '' }),
      );
      for (const p of d.players || []) {
        players.append(
          h('div', { text: p.id }),
          h('div', { text: p.name + (p.lobby ? ' (lobby)' : '') }),
          h('div', { text: p.guid }),
          h('div', { text: p.ping }),
          h('div', {}, [
            h('button', { i18n: 'action.kick', onclick: () => kickDialog(p) }),
            ' ',
            h('button', { class: 'danger', i18n: 'action.ban', onclick: () => banDialog(p) }),
          ]),
        );
      }
      status.textContent = `${t('status.players')}: ${d.count || 0}`;
    } catch (e) {
      const msg = e.message || '';
      if (/not configured|password|auth/i.test(msg)) {
        // Password missing/wrong, or set but not yet loaded (BE reads it at
        // start). The access card above is always available to fix this.
        players.innerHTML = '';
        status.textContent = t('rcon.notActiveYet') ||
          'RCon password isn’t active yet — set it above and restart the server.';
      } else {
        // Transient (timeout, BattlEye still loading). Keep polling quietly.
        status.textContent = `RCon: ${msg}`;
      }
    } finally {
      // Poll regardless of the first outcome — a transient failure right after
      // server start (BattlEye still booting) used to leave the page dead
      // until a manual refresh, because polling only armed on success.
      startPolling();
    }
  }

  // Kick / Ban dialogs — proper modals instead of native prompt().
  function kickDialog(p) {
    const m = openModal({ title: t('action.kick') + ' — ' + p.name });
    const reason = h('input', { type: 'text', value: 'kicked', style: { width: '100%' } });
    m.body.append(
      h('label', { i18n: 'rcon.reason' }), reason,
      h('div', { class: 'actions' }, [
        h('button', { class: 'danger', i18n: 'action.kick', onclick: async () => {
          try { await api.post('/api/rcon/kick', { playerId: p.id, reason: reason.value }); m.close(); await refresh(); }
          catch (e) { handleErr(e); }
        }}),
        h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
      ]),
    );
    setTimeout(() => reason.focus(), 0);
  }
  function banDialog(p) {
    const m = openModal({ title: t('action.ban') + ' — ' + p.name });
    const mins = h('input', { type: 'number', value: '60', min: '0', style: { width: '140px' } });
    const reason = h('input', { type: 'text', value: 'banned', style: { width: '100%' } });
    m.body.append(
      h('label', { i18n: 'rcon.ban.minutes' }), mins,
      h('label', { i18n: 'rcon.reason' }), reason,
      h('div', { class: 'actions' }, [
        h('button', { class: 'danger', i18n: 'action.ban', onclick: async () => {
          try { await api.post('/api/rcon/ban', { playerId: p.id, minutes: Number(mins.value) || 0, reason: reason.value }); m.close(); await refresh(); }
          catch (e) { handleErr(e); }
        }}),
        h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
      ]),
    );
    setTimeout(() => mins.focus(), 0);
  }

  status.style.margin = '4px 0 12px';
  players.style.margin = '8px 0';

  card.append(
    h('div', { class: 'actions', style: { marginTop: 0 } }, [
      h('button', { i18n: 'rcon.refresh', onclick: refresh }),
    ]),
    status,
    players,
    // Broadcast — separated section so it doesn't bleed into the player table.
    h('div', { style: { marginTop: '22px' } }, [
      h('h3', { i18n: 'rcon.broadcast' }),
      h('div', { class: 'row' }, [
        sayInp,
        h('button', { class: 'primary', i18n: 'rcon.say', onclick: doSay }),
      ]),
    ]),
    h('div', { style: { marginTop: '22px' } }, [
      h('h3', { i18n: 'rcon.rawCommand' }),
      h('div', { class: 'row' }, [
        cmdInp,
        h('button', { i18n: 'rcon.send', onclick: doCmd }),
      ]),
      h('div', { style: { marginTop: '10px' } }, [cmdOut]),
    ]),
  );

  async function doSay() {
    if (!sayInp.value.trim()) return;
    try { await api.post('/api/rcon/say', { message: sayInp.value }); sayInp.value = ''; toast(t('msg.sent'), 'ok'); }
    catch (e) { handleErr(e); }
  }
  // Raw console keeps an echoed transcript + ArrowUp/Down history, like a
  // real terminal — output no longer overwrites the previous command's.
  const cmdHist = [];
  let histIdx = -1;
  async function doCmd() {
    const cmd = cmdInp.value.trim();
    if (!cmd) return;
    cmdHist.push(cmd); histIdx = cmdHist.length;
    cmdInp.value = '';
    cmdOut.textContent += (cmdOut.textContent ? '\n' : '') + '> ' + cmd + '\n';
    try {
      const r = await api.post('/api/rcon/command', { command: cmd });
      cmdOut.textContent += (r.output || '(empty)') + '\n';
    } catch (e) { cmdOut.textContent += `ERR: ${e.message}\n`; }
    cmdOut.scrollTop = cmdOut.scrollHeight;
  }
  sayInp.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); doSay(); } });
  cmdInp.addEventListener('keydown', e => {
    if (e.key === 'Enter') { e.preventDefault(); doCmd(); }
    else if (e.key === 'ArrowUp' && cmdHist.length) { e.preventDefault(); histIdx = Math.max(0, histIdx - 1); cmdInp.value = cmdHist[histIdx] || ''; }
    else if (e.key === 'ArrowDown' && cmdHist.length) { e.preventDefault(); histIdx = Math.min(cmdHist.length, histIdx + 1); cmdInp.value = cmdHist[histIdx] || ''; }
  });

  // Access card first (always usable), then the live console below it.
  root.append(accessCard, card);
  root._teardown = stopPolling; // before the await — see logs/battleye above
  await refresh();
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
      hlp(h('th', { class: 'num', i18n: 'events.field.nominal' }), 'events.nominal'),
      hlp(h('th', { class: 'num', i18n: 'events.field.min' }), 'events.minmax'),
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
              toast(t('msg.saved'), 'ok');
              m.close();
              await refreshTable();
            } catch (e) { handleErr(e); }
          }
        }),
        h('button', { class: 'danger', i18n: 'action.delete',
          onclick: async () => {
            if (!(await confirmModal(t('confirm.deleteEvent').replace('{name}', name), { danger: true, okText: t('action.delete') }))) return;
            try {
              await api.del(`/api/events/item?name=${encodeURIComponent(name)}`);
              toast(t('msg.deleted'), 'ok');
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

  let _searchT = 0;
  search.oninput = () => { clearTimeout(_searchT); _searchT = setTimeout(() => { currentPage = 1; refreshTable(); }, 250); };

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
function scheduledRestartsCard(c, F, onChange) {
  const fire = () => onChange && onChange();
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
        onclick: () => { times.splice(i, 1); render(); fire(); } });
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
    withHelp(h('h3', { i18n: 'settings.restarts.title' }), 'settings.restart'),
    h('p', { class: 'hint', i18n: 'settings.restarts.hint' }),
    list,
    h('div', { class: 'actions' }, [
      h('button', { i18n: 'settings.restarts.add',
        onclick: () => { times.push('04:00'); render(); fire(); } }),
    ]),
    h('div', { style: { marginTop: '12px' } }, [
      h('label', { i18n: 'settings.restarts.warn' }),
      warnInput,
      h('small', { class: 'hint', i18n: 'settings.restarts.warn.hint' }),
      (!(c.rconPassword || '').trim())
        ? h('small', { class: 'hint', style: { color: 'var(--warn)' }, i18n: 'settings.restarts.warn.rcon' })
        : null,
    ]),
  ]);
}

function announcementsCard(c, F, onChange) {
  const fire = () => onChange && onChange();
  // Seed from the loaded config (single source of truth) — NOT a separate
  // /api/announcements fetch. Announcements are now persisted by the main
  // Settings save via the F getter below, so there's no second "Save" button
  // to forget (and no desync that used to wipe them, item #2).
  const state = {
    items: (c.scheduledAnnouncements || []).map(a => ({
      time: a.time || '', message: a.message || '', enabled: !!a.enabled,
    })),
  };

  const card = h('div', { class: 'card' }, [
    h('h3', { i18n: 'settings.announcements' }),
    h('p', { class: 'hint', i18n: 'settings.announcements.hint' }),
  ]);
  // #4 — announcements broadcast via RCon, so they need an RCon password.
  if (!(c.rconPassword || '').trim()) {
    card.append(h('p', { class: 'warning-bar', i18n: 'settings.rconNeeded' }));
  }
  const list = h('div');
  card.append(list);

  function render() {
    list.innerHTML = '';
    state.items.forEach((a, i) => {
      const time = h('input', { type: 'text', value: a.time, placeholder: 'HH:MM', style: { width: '80px' } });
      const msg  = h('input', { type: 'text', value: a.message || '', style: { flex: '1' } });
      const en   = h('input', { type: 'checkbox', class: 'switch' });
      en.checked = !!a.enabled;
      const row = h('div', { class: 'row', style: { gap: '8px', marginBottom: '6px' } }, [
        time, msg,
        h('label', {}, [en]),
        h('button', { class: 'danger', text: '×',
          onclick: () => { state.items.splice(i, 1); render(); fire(); } }),
      ]);
      time.onchange = () => { state.items[i].time = time.value.trim(); };
      msg.oninput   = () => { state.items[i].message = msg.value; fire(); };
      en.onchange   = () => { state.items[i].enabled = en.checked; };
      list.append(row);
    });
  }
  render();

  // The main Settings save reads this getter (same pattern as scheduledRestarts).
  F.scheduledAnnouncements = {
    type: 'list-announcements',
    get value() {
      return state.items
        .map(a => ({ time: (a.time || '').trim(), message: a.message || '', enabled: !!a.enabled }))
        .filter(a => a.time);
    },
  };

  card.append(
    h('div', { class: 'actions' }, [
      h('button', { i18n: 'settings.announcements.add',
        onclick: () => { state.items.push({ time: '12:00', message: '', enabled: true }); render(); fire(); }
      }),
    ]),
  );
  return card;
}




// openBackupDiff shows what restoring a .bak would change. Restoring used to be
// a leap of faith: you picked a timestamp and hoped.
async function openBackupDiff(path, backupName) {
  const m = openModal({ title: backupName, wide: true });
  m.body.append(h('p', { class: 'hint', i18n: 'backup.diff.hint' }));
  const host = h('div');
  m.body.append(host);
  applyI18n();

  let d;
  try {
    d = await api.get(`/api/backups/diff?path=${encodeURIComponent(path)}&backup=${encodeURIComponent(backupName)}`);
  } catch (e) { handleErr(e); return; }

  const r = d.diff || {};
  if (r.identical) {
    host.append(h('p', { class: 'hint', i18n: 'backup.diff.same' }));
    applyI18n();
    return;
  }
  host.append(h('div', { class: 'diff-stat' }, [
    h('span', { class: 'diff-plus', text: '+' + (r.added || 0) }),
    h('span', { class: 'diff-minus', text: '-' + (r.removed || 0) }),
    h('span', { class: 'hint', i18n: 'backup.diff.legend' }),
  ]));
  if (r.truncated) {
    host.append(h('p', { class: 'hint', i18n: 'backup.diff.truncated' }));
    applyI18n();
    return;
  }
  const pre = h('div', { class: 'diff-view' });
  for (const op of r.ops || []) {
    pre.append(h('div', { class: 'diff-row d' + (op.kind === '+' ? 'add' : op.kind === '-' ? 'del' : 'ctx') }, [
      h('span', { class: 'diff-num', text: op.oldLine ? String(op.oldLine) : '' }),
      h('span', { class: 'diff-num', text: op.newLine ? String(op.newLine) : '' }),
      h('span', { class: 'diff-sign', text: op.kind }),
      h('span', { class: 'diff-text', text: op.text }),
    ]));
  }
  host.append(pre);
  applyI18n();
}

// ---------------------------------------------------------------- diagnostics
//
// "Why won't my server start?" The answer is in the RPT, a multi-megabyte file
// most admins never open. renderDiagnosis turns the classified findings into
// something readable and puts it where the question is asked: the dashboard,
// but only while the server is down, so a healthy server shows nothing.

async function renderDiagnosis(host, placeAtTop) {
  let d;
  try { d = await api.get('/api/diagnose'); } catch { return; }
  if (!d || !(d.findings || []).length || d.running) return;

  const card = h('div', { class: 'card diag-card' }, [
    withHelp(h('h3', { i18n: 'diag.title' }), 'diag'),
    h('p', { class: 'hint', i18n: 'diag.subtitle' }),
  ]);
  for (const f of d.findings) {
    // The classifier speaks English; State.help carries the translations,
    // keyed by the finding's stable code, and falls back to what Go sent.
    const hx = (State.help || {});
    const title = hx['diag.' + f.code + '.title'] || f.title;
    const detail = (hx['diag.' + f.code + '.detail'] || f.detail)
      .replace('{subject}', f.subject || '');
    card.append(h('div', { class: 'diag-item ' + (f.severity === 'fatal' ? 'fatal' : 'warn') }, [
      h('div', { class: 'diag-head' }, [
        h('span', { class: 'diag-badge', text: t('diag.sev.' + f.severity) }),
        h('strong', { text: title }),
      ]),
      h('p', { class: 'diag-detail', text: detail }),
      // Always show the raw line: the classification is a heuristic and the
      // admin should be able to judge it.
      h('pre', { class: 'diag-line', text: f.line }),
      h('div', { class: 'diag-src' }, [
        h('span', { text: f.source.toUpperCase() }),
        h('button', { class: 'secondary', i18n: 'diag.openLog',
          onclick: () => { location.hash = '#logs'; } }),
      ]),
    ]));
  }
  if (placeAtTop && host.firstChild) host.insertBefore(card, host.firstChild);
  else host.append(card);
  applyI18n();
}

// --------------------------------------------------------------------- guide
//
// The beginner's guide. Content comes from /api/guide rather than the i18n
// bundle — see internal/guide for why. Each chapter renders as numbered steps
// with a "gotchas" list, and steps that name a route get a button that takes
// you straight there, so reading and doing are one click apart.

Views.guide = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.guide', 'guide.subtitle'));

  let chapters = [];
  // Ask for the language the UI is actually showing, not the stored config —
  // the two differ for a moment right after the topbar picker is used.
  try { chapters = (await api.get('/api/guide?lang=' + encodeURIComponent(State.lang || ''))).chapters || []; }
  catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;

  if (!chapters.length) {
    root.append(h('div', { class: 'card empty-state' }, [h('p', { i18n: 'guide.empty' })]));
    return;
  }

  // Contents strip: on a long page the jump list is what makes it usable.
  const toc = h('div', { class: 'guide-toc' }, chapters.map((c, i) =>
    h('button', {
      class: 'guide-toc-item', type: 'button',
      onclick: () => {
        const el = document.getElementById('guide-' + c.id);
        if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
      },
    }, [
      h('span', { class: 'guide-toc-num', text: String(i + 1) }),
      h('span', { text: c.title }),
    ])));
  root.append(h('div', { class: 'card' }, [
    h('h3', { i18n: 'guide.contents' }),
    toc,
  ]));

  const finish = () => root.append(h('div', { class: 'guide-support' }, [
    h('span', { class: 'guide-support-heart', text: '♥' }),
    h('div', {}, [
      h('strong', { i18n: 'support.title' }),
      h('p', { i18n: 'support.text' }),
    ]),
    h('span', { class: 'grow' }),
    h('div', { class: 'donate-buttons' }, donateLinks()),
  ]));

  chapters.forEach((c, ci) => {
    const steps = (c.steps || []).map((st, si) => h('li', { class: 'guide-step' }, [
      h('span', { class: 'guide-step-num', text: String(si + 1) }),
      h('div', { class: 'guide-step-body' }, [
        h('strong', { text: st.title }),
        h('p', { text: st.body }),
        st.route ? h('button', {
          class: 'secondary guide-jump', type: 'button',
          text: t('guide.open'),
          onclick: () => { location.hash = '#' + st.route; },
        }) : null,
      ]),
    ]));

    const tips = (c.tips || []).map(x => h('li', { text: x }));

    root.append(h('section', { class: 'card guide-chapter', id: 'guide-' + c.id }, [
      h('div', { class: 'guide-head' }, [
        // Must go through innerHTML: h() uses createElement, which cannot
        // build a real SVG element (wrong namespace — it renders as nothing).
        h('span', { class: 'guide-badge', html:
          '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"' +
          ' stroke-linecap="round" stroke-linejoin="round"><use href="#' +
          (c.icon || 'i-guide').replace(/[^a-z0-9-]/gi, '') + '"/></svg>' }),
        h('div', {}, [
          h('div', { class: 'guide-kicker', text: t('guide.chapter') + ' ' + (ci + 1) }),
          h('h2', { text: c.title }),
        ]),
      ]),
      h('p', { class: 'guide-intro', text: c.intro }),
      // Screenshot of the real section. loading="lazy" so eight of them do not
      // all decode on first paint.
      c.image ? h('figure', { class: 'guide-shot' }, [
        h('img', { src: c.image, alt: c.title, loading: 'lazy', decoding: 'async' }),
      ]) : null,
      steps.length ? h('ol', { class: 'guide-steps' }, steps) : null,
      tips.length ? h('div', { class: 'guide-tips' }, [
        h('div', { class: 'guide-tips-title', i18n: 'guide.tips' }),
        h('ul', {}, tips),
      ]) : null,
    ]));
  });
  finish();
};

// --------------------------------------------------------------------- weather & time

// Line-style weather icons (stroke=currentColor so they inherit text/accent
// colour and match the manager's design — no emoji).
const WICON = {
  clear:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2.5M12 19.5V22M2 12h2.5M19.5 12H22M4.9 4.9l1.8 1.8M17.3 17.3l1.8 1.8M19.1 4.9l-1.8 1.8M6.7 17.3l-1.8 1.8"/></svg>',
  cloudy:  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M7 18h9.5a3.5 3.5 0 0 0 .4-7 5.2 5.2 0 0 0-10-1.3A3.9 3.9 0 0 0 7 18z"/></svg>',
  foggy:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M7 13h9.5a3.4 3.4 0 0 0 .4-6.8 5 5 0 0 0-9.6-1.2A3.7 3.7 0 0 0 7 13z"/><path d="M5 17h14M7 20.5h10"/></svg>',
  rainy:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M7 14h9.5a3.4 3.4 0 0 0 .4-6.8 5 5 0 0 0-9.6-1.2A3.7 3.7 0 0 0 7 14z"/><path d="M8.5 17l-1 3M12 17l-1 3M15.5 17l-1 3"/></svg>',
  storm:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M7 14h9.5a3.4 3.4 0 0 0 .4-6.8 5 5 0 0 0-9.6-1.2A3.7 3.7 0 0 0 7 14z"/><path d="M12.5 15l-2.5 4h3l-2.5 4"/></svg>',
  snowy:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M7 13h9.5a3.4 3.4 0 0 0 .4-6.8 5 5 0 0 0-9.6-1.2A3.7 3.7 0 0 0 7 13z"/><path d="M9 17v.01M13 17v.01M11 20v.01M15 20v.01"/></svg>',
  dynamic: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 12a8.5 8.5 0 0 1 14.5-6M18.5 3v3.5H15"/><path d="M20.5 12a8.5 8.5 0 0 1-14.5 6M5.5 21v-3.5H9"/></svg>',
  off:     '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="8.5"/><path d="M6.2 6.2l11.6 11.6"/></svg>',
  wind:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M3 9h10a2.6 2.6 0 1 0-2.6-2.6"/><path d="M3 15h13a2.6 2.6 0 1 1-2.6 2.6"/><path d="M3 12h7"/></svg>',
};

Views.weather = async (root) => {
  await refreshStatus();
  const running = State.serverStatus.running;
  const d = await api.get('/api/weather');
  const params = d.params || {};
  const presets = d.presets || [];
  const tm = d.time || {};

  root.append(pageHeader('nav.weather', 'weather.subtitle'));
  const wrap = h('div');
  root.append(wrap);
  if (running) wrap.append(runningBanner());

  // ---- presets ----
  // "Enabled" (not mere file existence) decides whether the manager controls
  // weather — the vanilla cfgweather.xml ships present but with enable="0".
  const enabled = !!params.enable;
  const matchedBadge = h('span', { class: 'badge ' + (enabled ? 'ok' : 'mute'),
    text: enabled ? (t('weather.preset.' + d.matched) || d.matched) : '—' });

  const applyPreset = async (name) => {
    try {
      // Carry the current smoothness through so clicking a preset doesn't
      // quietly revert it.
      const tr = (document.querySelector('#weather-trans') || {}).value || (params.transition || 'smooth');
      await api.post('/api/weather/preset', { name, transition: tr });
      toast(t('msg.saved'), 'ok'); await navigate('weather');
    }
    catch (e) { handleErr(e); }
  };
  const presetTile = (name) => h('button', {
    class: 'preset-tile' + ((name === d.matched && enabled) ? ' active' : ''),
    disabled: running, onclick: () => applyPreset(name),
  }, [
    h('span', { class: 'ic', html: WICON[name] || '' }),
    h('span', { i18n: 'weather.preset.' + name }),
  ]);
  const tiles = presets.map(presetTile);
  tiles.push(presetTile('off'));

  wrap.append(h('div', { class: 'card' }, [
    h('h3', { i18n: 'weather.presets.title' }),
    h('p', { class: 'hint', i18n: 'weather.presets.hint' }),
    h('div', { class: 'row', style: { gap: '6px', alignItems: 'center', marginBottom: '10px' } }, [
      h('span', { class: 'k', i18n: 'weather.current' }), matchedBadge,
    ]),
    enabled ? null : h('p', { class: 'hint', i18n: 'weather.disabled' }),
    h('div', { class: 'preset-grid' }, tiles),
  ]));

  // ---- manual editor ----
  // Generic slider row. `unit` is '%' (value stored 0..1) or 'm/s' (raw value).
  const sliderRow = (labelKey, icon, val, opts = {}) => {
    const unit = opts.unit || '%';
    const max = opts.max || 100;
    const step = opts.step || '1';
    const toDisplay = (v) => unit === '%' ? Math.round((v || 0) * 100) : (v || 0);
    const fmt = (n) => unit === '%' ? n + '%' : n + ' m/s';
    const cur = toDisplay(val);
    const out = h('span', { class: 'slider-val', text: fmt(cur) });
    const range = h('input', { type: 'range', class: 'wslider', min: '0', max: String(max), step,
      value: String(cur), style: { flex: '1' } });
    const fill = () => range.style.setProperty('--val', (Number(range.value) / max * 100) + '%');
    range.oninput = () => { out.textContent = fmt(Number(range.value)); fill(); };
    fill();
    const row = h('div', { class: 'wrow' }, [
      h('span', { class: 'slider-label' }, [
        h('span', { class: 'ic', html: WICON[icon] || '' }),
        h('span', { i18n: labelKey }),
        opts.help ? help(opts.help) : null,
      ]),
      range, out,
    ]);
    return { row, get: () => unit === '%' ? Number(range.value) / 100 : Number(range.value) };
  };
  const oc = sliderRow('weather.overcast', 'cloudy', params.overcast);
  const fog = sliderRow('weather.fog', 'foggy', params.fog);
  const rain = sliderRow('weather.rain', 'rainy', params.rain);
  const snow = sliderRow('weather.snowfall', 'snowy', params.snowfall);
  const storm = sliderRow('weather.storm', 'storm', params.stormDensity, { help: 'weather.storm' });
  const wind = sliderRow('weather.wind', 'wind', params.wind, { unit: 'm/s', max: 20, step: '0.5' });
  const dynChk = h('input', { type: 'checkbox', class: 'switch' });
  dynChk.checked = !!params.dynamic;
  // How gradually weather moves. The old build hard-coded 2-minute ramps with
  // full-range steps, which is why changes felt like a light switch.
  const transSel = h('select', { id: 'weather-trans', style: { maxWidth: '260px' } }, [
    h('option', { value: 'smooth', i18n: 'weather.trans.smooth' }),
    h('option', { value: 'normal', i18n: 'weather.trans.normal' }),
    h('option', { value: 'fast',   i18n: 'weather.trans.fast' }),
  ]);
  transSel.value = params.transition || 'smooth';

  wrap.append(h('div', { class: 'card' }, [
    h('h3', { i18n: 'weather.custom.title' }),
    h('p', { class: 'hint', i18n: 'weather.custom.hint' }),
    oc.row, fog.row, rain.row, wind.row, snow.row, storm.row,
    h('label', {}, [dynChk, h('span', { i18n: 'weather.preset.dynamic' }), help('weather.dynamic')]),
    h('div', { style: { marginTop: '10px' } }, [
      withHelp(h('label', { i18n: 'weather.trans.title', style: { display: 'inline-block' } }), 'weather.transition'),
      transSel,
      h('small', { class: 'hint', i18n: 'weather.trans.hint' }),
    ]),
    h('div', { class: 'actions' }, [
      h('button', { class: 'primary', i18n: 'weather.apply', disabled: running,
        onclick: async () => {
          try {
            await api.post('/api/weather/custom', {
              overcast: oc.get(), fog: fog.get(), rain: rain.get(),
              snowfall: snow.get(), stormDensity: storm.get(),
              wind: wind.get(), dynamic: dynChk.checked,
              transition: transSel.value,
            });
            toast(t('msg.saved'), 'ok'); await navigate('weather');
          } catch (e) { handleErr(e); }
        } }),
    ]),
    h('p', { class: 'hint', i18n: 'weather.timedHint' }),
  ]));

  // ---- in-game time ----
  const accel = h('input', { type: 'number', min: '0.1', max: '24', step: '0.1', value: tm.serverTimeAcceleration ?? '1', style: { width: '90px' } });
  const nAccel = h('input', { type: 'number', min: '0.1', max: '64', step: '0.1', value: tm.serverNightTimeAcceleration ?? '1', style: { width: '90px' } });
  const sTime = h('input', { type: 'text', value: tm.serverTime ?? 'SystemTime', style: { width: '180px' } });
  const persist = h('input', { type: 'checkbox', class: 'switch' });
  persist.checked = String(tm.serverTimePersistent ?? '0') === '1';

  // Quick-pick chips so users don't have to know the multiplier / time format.
  const accelQuick = h('div', { class: 'pills' }, [1, 6, 12, 24].map(v =>
    h('button', { class: 'pill', text: v + '×', onclick: () => { accel.value = v; } })));
  const startQuick = h('div', { class: 'pills' }, [
    ['weather.time.start.system', 'SystemTime'],
    ['weather.time.start.morning', '2024/7/1/8/0'],
    ['weather.time.start.noon', '2024/7/1/12/0'],
    ['weather.time.start.evening', '2024/7/1/19/0'],
    ['weather.time.start.night', '2024/7/1/0/0'],
  ].map(([k, val]) => h('button', { class: 'pill', i18n: k, onclick: () => { sTime.value = val; } })));

  const tRow = (labelKey, el, extra, hintKey) => h('div', { style: { marginBottom: '12px' } }, [
    h('label', { i18n: labelKey }), el,
    extra || null,
    hintKey ? h('small', { class: 'hint', i18n: hintKey }) : null,
  ]);

  wrap.append(h('div', { class: 'card' }, [
    withHelp(h('h3', { i18n: 'weather.time.title' }), 'weather.accel'),
    tRow('weather.time.accel', accel, accelQuick),
    tRow('weather.time.nightAccel', nAccel),
    tRow('weather.time.serverTime', sTime, startQuick, 'weather.time.serverTime.hint'),
    h('label', {}, [persist, h('span', { i18n: 'weather.time.persistent' })]),
    h('div', { class: 'actions' }, [
      h('button', { class: 'primary', i18n: 'weather.time.save', disabled: running,
        onclick: (e) => withBusy(e.currentTarget, async () => {
          // Validate instead of silently substituting: `Number(x) || 1` used to
          // turn a typo (or a deliberate 0) into 1 without telling anyone.
          const clamp = (inp, lo, hi) => {
            const v = Number(inp.value);
            if (!Number.isFinite(v) || v < lo || v > hi) {
              inp.style.borderColor = 'var(--error)';
              return null;
            }
            inp.style.borderColor = '';
            return v;
          };
          const day = clamp(accel, 0.1, 24);
          const night = clamp(nAccel, 0.1, 64);
          if (day === null || night === null) { toast(t('weather.time.range'), 'error'); return; }
          try {
            await api.post('/api/weather/time', {
              serverTimeAcceleration: day,
              serverNightTimeAcceleration: night,
              serverTime: sTime.value.trim() || 'SystemTime',
              serverTimePersistent: persist.checked ? 1 : 0,
            });
            toast(t('msg.saved'), 'ok');
          } catch (err) { handleErr(err); }
        }) }),
    ]),
  ]));
};

// --------------------------------------------------------------------- wipe

Views.wipe = async (root) => {
  const d = await api.get('/api/wipe/preview');
  root.append(pageHeader('nav.wipe', 'wipe.subtitle'));
  const wrap = h('div');
  root.append(wrap);

  if (d.serverRunning) wrap.append(runningBanner());

  const folders = d.folders || [];
  const list = h('div');
  if (folders.length === 0) {
    list.append(h('p', { class: 'hint', i18n: 'wipe.nothing' }));
  } else {
    const tbl = h('table');
    tbl.append(h('thead', {}, h('tr', {}, [h('th', { i18n: 'col.storage' }), h('th', { i18n: 'col.size' })])));
    const tb = h('tbody');
    for (const f of folders) {
      tb.append(h('tr', {}, [h('td', { text: f.name }), h('td', { text: bytes(f.sizeBytes) })]));
    }
    tbl.append(tb);
    list.append(tbl);
  }

  const confirm = h('input', { type: 'checkbox' });
  const wipeBtn = h('button', { class: 'danger', i18n: 'wipe.button',
    disabled: true,
    onclick: async (e) => {
      const btn = e.currentTarget; // before the await — null afterwards
      // Checkbox + button + THIS modal: an irreversible multi-folder delete
      // deserves the same danger confirm as deleting a single event.
      if (!(await confirmModal(t('wipe.warning'), { danger: true, okText: t('wipe.button') }))) return;
      await withBusy(btn, async () => {
        try {
          await api.post('/api/wipe', {});
          toast(t('wipe.done'), 'ok');
          await navigate('wipe');
        } catch (err) { handleErr(err); }
      });
    } });
  const refreshBtnState = () => {
    wipeBtn.disabled = !(confirm.checked && !d.serverRunning && folders.length > 0);
  };
  confirm.onchange = refreshBtnState;
  refreshBtnState();

  wrap.append(h('div', { class: 'card', style: { borderColor: 'var(--error)' } }, [
    h('h3', { i18n: 'nav.wipe' }),
    h('p', { class: 'warning-bar', i18n: 'wipe.warning', 'data-help': 'wipe' }),
    h('p', { class: 'hint', i18n: 'wipe.whatItDoes' }),
    h('p', { class: 'hint', i18n: 'wipe.backupNote' }),
    d.instanceId ? h('p', { class: 'hint', text: 'instanceId: ' + d.instanceId }) : null,
    h('h4', { i18n: 'wipe.preview.title' }),
    list,
    d.serverRunning ? h('p', { class: 'hint', i18n: 'wipe.serverRunning' }) : null,
    h('label', { style: { display: 'flex', gap: '8px', alignItems: 'center', margin: '10px 0' } }, [confirm, h('span', { i18n: 'wipe.confirm' })]),
    h('div', { class: 'actions' }, [wipeBtn]),
  ]));

  // ---- previous wipes, and putting one back -------------------------------
  const undoCard = h('div', { class: 'card' });
  wrap.append(undoCard);

  async function refreshWipes() {
    undoCard.innerHTML = '';
    undoCard.append(
      withHelp(h('h3', { i18n: 'wipe.undo.title' }), 'wipe.undo'),
      h('p', { class: 'hint', i18n: 'wipe.undo.hint' }),
    );
    let w;
    try { w = await api.get('/api/wipe/list'); }
    catch (e) { handleErr(e); return; }

    const sets = w.wipes || [];
    if (!sets.length) {
      undoCard.append(h('p', { class: 'hint', i18n: 'wipe.undo.none' }));
      applyI18n();
      return;
    }
    for (const set of sets) {
      const when = set.when ? new Date(set.when).toLocaleString() : set.id;
      // Blocked means the mission has that folder again: restoring would bury
      // a world players have been building on since the wipe.
      const blocked = (set.blocked || []).length > 0;
      undoCard.append(h('div', { class: 'wipe-set' + (blocked ? ' blocked' : '') }, [
        h('div', { class: 'grow' }, [
          h('strong', { text: when }),
          h('div', { class: 'hint', text: (set.folders || []).join(', ') + ' · ' + bytes(set.size || 0) }),
          blocked ? h('div', { class: 'wipe-blocked', text: t('wipe.undo.blocked').replace('{names}', set.blocked.join(', ')) }) : null,
        ]),
        h('button', {
          class: 'secondary', i18n: 'wipe.undo.button',
          disabled: blocked || d.serverRunning,
          onclick: async (e) => {
            const btn = e.currentTarget; // capture BEFORE the await
            if (!(await confirmModal(t('wipe.undo.confirm').replace('{when}', when), { okText: t('wipe.undo.button') }))) return;
            await withBusy(btn, async () => {
              try {
                const r = await api.post('/api/wipe/restore', { id: set.id });
                toast(t('wipe.undo.done').replace('{n}', r.count), 'ok');
                await navigate('wipe');
              } catch (err) { handleErr(err); }
            });
          },
        }),
      ]));
    }
    applyI18n();
  }
  await refreshWipes();
};

Views.settings = async (root) => {
  const c = State.config;
  const F = {
    language:        h('select', {}),
    vanillaDayZPath: h('input',  { type: 'text', value: c.vanillaDayZPath || '' }),
    exposure:        h('select', {}, [
                       h('option', { value: 'local',    i18n: 'settings.exposure.local' }),
                       h('option', { value: 'lan',      i18n: 'settings.exposure.lan' }),
                       h('option', { value: 'internet', i18n: 'settings.exposure.internet' }),
                     ]),
    serverPort:      h('input',  { type: 'number', value: c.serverPort }),
    serverCfg:       h('input',  { type: 'text', value: c.serverCfg }),
    cpuCount:        h('input',  { type: 'number', value: c.cpuCount }),
    bePath:          h('input',  { type: 'text', value: c.bePath }),
    profilesDir:     h('input',  { type: 'text', value: c.profilesDir }),
    autoRestartSeconds: h('input', { type: 'number', value: c.autoRestartSeconds }),
    autoRestartEnabled: h('input', { type: 'checkbox', class: 'switch' }),
    autoUpdateCheckMinutes: h('input', { type: 'number', value: c.autoUpdateCheckMinutes ?? 0 }),
    autoUpdateModsOnRestart: h('input', { type: 'checkbox', class: 'switch' }),
    restartOnCrash: h('input', { type: 'checkbox', class: 'switch' }),
    backupIntervalHours: h('input', { type: 'number', min: '0', value: c.backupIntervalHours ?? 0 }),
    backupKeep: h('input', { type: 'number', min: '1', value: c.backupKeep || 10 }),
    discordEnabled: h('input', { type: 'checkbox', class: 'switch' }),
    discordWebhookURL: h('input', { type: 'text', value: c.discordWebhookURL || '', placeholder: 'https://discord.com/api/webhooks/…' }),
    doLogs:       h('input', { type: 'checkbox', class: 'switch' }),
    adminLog:     h('input', { type: 'checkbox', class: 'switch' }),
    netLog:       h('input', { type: 'checkbox', class: 'switch' }),
    freezeCheck:  h('input', { type: 'checkbox', class: 'switch' }),
    filePatching: h('input', { type: 'checkbox', class: 'switch' }),
  };
  fillLangSelect(F.language, c.language, false);
  F.exposure.value = c.exposure || 'local';
  for (const k of ['autoRestartEnabled','autoUpdateModsOnRestart','restartOnCrash','discordEnabled','doLogs','adminLog','netLog','freezeCheck','filePatching']) {
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
      F.vanillaDayZPath.dispatchEvent(new Event('change', { bubbles: true })); // trigger auto-save
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

  // ---- Auto-save: no manual Save button. Field changes persist on their own
  // (item #2) — kills the whole class of "forgot to save / save reset X" bugs.
  const savedNote = h('span', { class: 'saved-note', i18n: 'settings.saved' });
  const gather = () => {
    // Send ONLY the fields this form owns — never spread State.config, which can
    // hold stale values for state changed elsewhere (e.g. mods toggled via
    // /api/mods/enable). The merge backend preserves every field we omit, so a
    // Settings save can no longer reset mods (critical hotfix).
    const next = {};
    for (const [k, el] of Object.entries(F)) {
      if (el.type === 'checkbox') next[k] = el.checked;
      else if (el.type === 'number') {
        // A field cleared mid-edit must NOT auto-save as 0 (imagine serverPort
        // becoming 0 six hundred ms after you select-all-delete). Omitting the
        // key keeps the old value via the merge backend.
        const v = el.value.trim();
        if (v === '' || Number.isNaN(Number(v))) continue;
        next[k] = Number(v);
      }
      else next[k] = el.value; // text/select + the list getters (restarts/announcements)
    }
    return next;
  };
  const save = async () => {
    const next = gather();
    const langChanged = next.language !== State.config.language;
    try {
      State.config = await api.post('/api/config', next);
      savedNote.classList.add('show');
      clearTimeout(save._t); save._t = setTimeout(() => savedNote.classList.remove('show'), 1500);
      if (langChanged) {
        await loadI18n(next.language);
        const ls = document.getElementById('lang-switch'); if (ls) ls.value = next.language;
      }
    } catch (e) { handleErr(e); }
  };
  let _dbT;
  const autoSave = () => { clearTimeout(_dbT); _dbT = setTimeout(save, 600); };
  root._save = save; // Ctrl+S still saves immediately

  const body = h('div');
  // One delegated listener catches every field change in Settings — inputs in
  // the schedule/announcement cards bubble here too, so no per-field wiring.
  body.addEventListener('change', (e) => {
    if (e.target && e.target.matches('input, select, textarea')) autoSave();
  });
  body.append(
    section('settings.title', [
      row('settings.language', F.language),
      row('settings.theme', themeSel),
      h('div', {}, [
        h('label', { i18n: 'settings.vanillaPath' }),
        h('div', { class: 'row' }, [F.vanillaDayZPath, detectBtn]),
      ]),
      h('div', {}, [
        withHelp(h('label', { i18n: 'settings.exposure' }), 'settings.exposure'),
        F.exposure,
        h('small', { class: 'hint', i18n: 'settings.exposure.hint' }),
      ]),
    ]),

    section('nav.server', [
      // Server name is edited via the serverDZ.cfg "hostname" field in the
      // Server section — it lived here too, which only caused confusion (#7).
      h('div', { class: 'grid-2' }, [
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
        h('label', {}, [F.adminLog,     h('span', { i18n: 'settings.flag.adminlog' }), help('settings.adminlog')]),
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
      h('label', {}, [F.restartOnCrash, h('span', { i18n: 'settings.watchdog' }), help('settings.watchdog')]),
      h('small', { class: 'hint', i18n: 'settings.watchdog.hint' }),
    ]),

    section('settings.autoupdate.title', [
      h('label', { style: { display: 'flex', gap: '8px', alignItems: 'center' } },
        [F.autoUpdateModsOnRestart, h('span', { i18n: 'settings.autoupdate.onRestart' })]),
      h('small', { class: 'hint', i18n: 'settings.autoupdate.onRestart.hint' }),
      h('div', { style: { marginTop: '12px' } }, [
        h('label', { i18n: 'settings.autoupdate.check' }),
        F.autoUpdateCheckMinutes,
        h('small', { class: 'hint', i18n: 'settings.autoupdate.check.hint' }),
      ]),
    ]),

    networkCard(),

    scheduledRestartsCard(c, F, autoSave),

    announcementsCard(c, F, autoSave),

    intervalAnnouncementsCard(c, F, autoSave),

    section('settings.autobackup.title', [
      h('p', { class: 'hint', i18n: 'settings.autobackup.hint' }),
      h('div', { class: 'grid-2' }, [
        row('settings.autobackup.every', F.backupIntervalHours),
        row('settings.autobackup.keep', F.backupKeep),
      ]),
      h('div', { class: 'actions' }, [
        h('button', { i18n: 'settings.autobackup.now', onclick: (e) => withBusy(e.currentTarget, async () => {
          try {
            const r = await api.post('/api/backup/run', {});
            toast(`${t('msg.saved')} — ${r.path}`, 'ok');
          } catch (err) { handleErr(err); }
        })}),
      ]),
    ]),

    section('settings.discord.title', [
      h('p', { class: 'hint', i18n: 'settings.discord.hint' }),
      h('label', {}, [F.discordEnabled, h('span', { i18n: 'settings.discord.enable' })]),
      row('settings.discord.url', F.discordWebhookURL),
      h('div', { class: 'actions' }, [
        h('button', { i18n: 'settings.discord.test', onclick: (e) => withBusy(e.currentTarget, async () => {
          const url = F.discordWebhookURL.value.trim();
          if (!url) return;
          try { await api.post('/api/discord/test', { url }); toast(t('msg.sent'), 'ok'); }
          catch (err) { handleErr(err); }
        })}),
      ]),
    ]),

    backupCard(),
  );

  root.append(
    pageHeader('nav.settings', 'settings.subtitle', [savedNote]),
    body,
  );
  // Support card is appended globally by navigate(), so it isn't repeated here.
};

// intervalAnnouncementsCard — repeat a message every N minutes while running.
function intervalAnnouncementsCard(c, F, onChange) {
  const fire = () => onChange && onChange();
  const state = {
    items: (c.intervalAnnouncements || []).map(a => ({
      intervalMinutes: a.intervalMinutes || 30, message: a.message || '', enabled: !!a.enabled,
    })),
  };
  const card = h('div', { class: 'card' }, [
    h('h3', { i18n: 'settings.intervalAnn' }),
    h('p', { class: 'hint', i18n: 'settings.intervalAnn.hint' }),
  ]);
  if (!(c.rconPassword || '').trim()) {
    card.append(h('p', { class: 'warning-bar', i18n: 'settings.rconNeeded' }));
  }
  const list = h('div');
  card.append(list);

  function render() {
    list.innerHTML = '';
    state.items.forEach((a, i) => {
      const every = h('input', { type: 'number', min: '1', value: a.intervalMinutes, style: { width: '80px' } });
      const msg = h('input', { type: 'text', value: a.message || '', style: { flex: '1' } });
      const en = h('input', { type: 'checkbox', class: 'switch' });
      en.checked = !!a.enabled;
      const row = h('div', { class: 'row', style: { gap: '8px', marginBottom: '6px' } }, [
        h('span', { class: 'k', i18n: 'settings.intervalAnn.every' }), every,
        h('span', { class: 'k', i18n: 'settings.intervalAnn.min' }),
        msg,
        h('label', {}, [en]),
        h('button', { class: 'danger', text: '×', onclick: () => { state.items.splice(i, 1); render(); fire(); } }),
      ]);
      every.onchange = () => { state.items[i].intervalMinutes = parseInt(every.value, 10) || 0; };
      msg.oninput = () => { state.items[i].message = msg.value; fire(); };
      en.onchange = () => { state.items[i].enabled = en.checked; };
      list.append(row);
    });
  }
  render();

  F.intervalAnnouncements = {
    type: 'list-interval',
    get value() {
      return state.items
        .map(a => ({ intervalMinutes: parseInt(a.intervalMinutes, 10) || 0, message: a.message || '', enabled: !!a.enabled }))
        .filter(a => a.intervalMinutes > 0);
    },
  };

  card.append(h('div', { class: 'actions' }, [
    h('button', { i18n: 'settings.intervalAnn.add',
      onclick: () => { state.items.push({ intervalMinutes: 30, message: '', enabled: true }); render(); fire(); } }),
  ]));
  return card;
}

// networkCard shows how to reach the panel from other devices on the LAN
// (item 9). Read-only — exposure itself is the select above; this just renders
// the URLs to type on a phone, or a hint when the panel is local-only.
function networkCard() {
  const card = h('div', { class: 'card' }, [
    h('h3', { i18n: 'settings.network.title' }),
    h('p', { class: 'hint', i18n: 'settings.network.hint' }),
  ]);
  const body = h('div');
  card.append(body);
  (async () => {
    try {
      const d = await api.get('/api/network/addresses');
      body.innerHTML = '';
      if (!d.lanEnabled) {
        body.append(h('p', { class: 'warning-bar', i18n: 'settings.network.localOnly' }));
        return;
      }
      const urls = d.urls || [];
      if (urls.length === 0) {
        body.append(h('p', { class: 'hint', i18n: 'settings.network.localOnly' }));
      } else {
        body.append(h('div', { class: 'k', i18n: 'settings.network.urls' }));
        const ul = h('ul', { style: { margin: '6px 0' } });
        for (const u of urls) {
          ul.append(h('li', {}, h('a', { href: u, target: '_blank', rel: 'noopener', text: u })));
        }
        body.append(ul);
      }
      body.append(h('p', { class: 'hint', i18n: 'settings.network.firewall' }));
      body.append(h('p', { class: 'hint', i18n: 'settings.network.restartNote' }));
    } catch (e) { /* non-fatal */ }
  })();
  return card;
}

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
      toast(t('msg.saved'), 'ok');
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

// donateLinks builds the two links once; the card, the modal and the guide
// strip all reuse them so a URL is never duplicated.
function donateLinks() {
  return [
    h('a', { href: 'https://buymeacoffee.com/aristarh.ucolov', target: '_blank', rel: 'noopener', class: 'coffee' }, [
      '☕ ', h('span', { i18n: 'support.coffee' }),
    ]),
    h('a', { href: 'https://ko-fi.com/aristarhucolov', target: '_blank', rel: 'noopener', class: 'kofi' }, [
      '🧡 ', h('span', { i18n: 'support.kofi' }),
    ]),
    h('a', { href: 'https://www.donationalerts.com/r/aristarh_ucolov', target: '_blank', rel: 'noopener', class: 'da' }, [
      '♥ ', h('span', { i18n: 'support.donationalerts' }),
    ]),
  ];
}

// openSupport shows the links in a modal. Bound to the topbar heart, which is
// deliberately the smallest possible permanent presence: one icon, no layout.
function openSupport() {
  const m = openModal({ title: t('support.title') });
  m.body.append(
    h('p', { i18n: 'support.text' }),
    h('div', { class: 'donate-buttons', style: { marginTop: '14px' } }, donateLinks()),
  );
  applyI18n();
}

function donationCard() {
  return h('div', { class: 'donate-card' }, [
    h('h3', { i18n: 'support.title' }),
    h('p',  { i18n: 'support.text' }),
    h('div', { class: 'donate-buttons' }, donateLinks()),
  ]);
}

// --------------------------------------------------------------------- theme

function bindSupportButton() {
  const btn = document.getElementById('support-btn');
  if (btn) btn.onclick = openSupport;
}

function setTheme(mode) {
  localStorage.setItem('theme', mode);
  let applied = mode;
  if (mode === 'auto') {
    applied = window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  }
  document.documentElement.setAttribute('data-theme', applied);
  const btn = document.querySelector('#theme-toggle .theme-icon');
  if (btn) btn.textContent = applied === 'light' ? '☀' : '☾';
  // Keep mobile browser chrome in sync with the panel background.
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute('content', applied === 'light' ? '#f4f6fa' : '#0c0f16');
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
                // A mod that could not be copied used to vanish from the
                // result with no message at all.
                if ((r.failedMods || []).length) {
                  toast(r.failedMods.join('; '), 'error');
                }
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
      withHelp(h('h2', { i18n: 'sync.title' }), 'sync.import'),
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
    // Apply theme as early as possible so the first-run modal doesn't flash
    // dark-on-light. First visit follows the OS preference; an explicit choice
    // (topbar toggle / Settings) is remembered and wins afterwards.
    setTheme(localStorage.getItem('theme') ||
      (window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'));
    bindSupportButton();
    // Topbar elevation on scroll — glass strip gains a shadow once content
    // slides underneath it.
    const tb = document.querySelector('.topbar');
    if (tb) window.addEventListener('scroll', () => {
      tb.classList.toggle('scrolled', window.scrollY > 2);
    }, { passive: true });
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

    State.config = await api.get('/api/config');
    if (State.config.language && State.config.language !== State.lang) {
      await loadI18n(State.config.language);
    }
    fillLangSelect(document.getElementById('lang-switch'), State.lang, true);
    document.getElementById('lang-switch').onchange = async e => {
      const v = e.target.value;
      localStorage.setItem('lang', v);
      // Send ONLY the language field. Posting the whole State.config here would
      // clobber fields changed elsewhere (e.g. mods toggled via /api/mods/enable
      // that never updated State.config) — the merge backend preserves the rest.
      // The response is the full merged config, so State.config re-syncs to truth.
      try { State.config = await api.post('/api/config', { language: v }); }
      catch (err) { State.config.language = v; }
      // Without this guard a failing /api/i18n left the picker on the new
      // language while nothing re-rendered, with no error shown.
      try {
        await loadI18n(v);
        await navigate(currentRoute());
      } catch (err) { handleErr(err); }
    };
    await ensureFirstRunDone();
    await refreshStatus();
    if (window._statusInterval) clearInterval(window._statusInterval);
    window._statusInterval = setInterval(refreshStatus, 5000);
    // Support footer is rendered once here (outside #view) so it appears under
    // every section without any chance of duplication.
    renderSupportFooter();
    // Restore the section from the URL hash so a reload keeps the user where
    // they were (defaults to dashboard for a fresh load / unknown hash).
    await navigate(routeFromHash());
  } catch (err) {
    handleErr(err);
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
      toast(t('msg.saved'), 'ok');
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

  // ---- Structured bans.txt editor. Hand-editing the `GUID minutes reason`
  // format is the #1 way admins silently break every ban; saving here also
  // issues RCon `loadBans` so changes apply to a live server.
  const bansCard = h('div', { class: 'card' }, [
    withHelp(h('h3', { i18n: 'battleye.bans.title' }), 'battleye.bans'),
    h('p', { class: 'hint', i18n: 'battleye.bans.hint' }),
  ]);
  const bansHost = h('div');
  bansCard.append(bansHost);
  let bans = [];
  function renderBans() {
    bansHost.innerHTML = '';
    const tbl = h('table');
    tbl.append(h('thead', {}, h('tr', {}, [
      h('th', { i18n: 'battleye.bans.id' }),
      h('th', { i18n: 'rcon.ban.minutes' }),
      h('th', { i18n: 'rcon.reason' }),
      h('th', { text: '' }),
    ])));
    const tb = h('tbody');
    bans.forEach((ban, i) => {
      const idInp = h('input', { class: 'inline-cell mono', type: 'text', value: ban.id });
      const minInp = h('input', { class: 'inline-cell', type: 'text', value: ban.minutes, style: { maxWidth: '90px' } });
      const rsnInp = h('input', { class: 'inline-cell', type: 'text', value: ban.reason });
      idInp.onchange = () => { ban.id = idInp.value.trim(); };
      minInp.onchange = () => { ban.minutes = minInp.value.trim(); };
      rsnInp.onchange = () => { ban.reason = rsnInp.value; };
      tb.append(h('tr', {}, [
        h('td', {}, idInp),
        h('td', {}, minInp),
        h('td', {}, rsnInp),
        h('td', {}, h('button', { class: 'danger', text: '×', onclick: () => { bans.splice(i, 1); renderBans(); } })),
      ]));
    });
    tbl.append(tb);
    if (!bans.length) {
      bansHost.append(h('p', { class: 'hint', text: '—' }));
    } else {
      bansHost.append(h('div', { class: 'table-scroll' }, tbl));
    }
    bansHost.append(h('div', { class: 'actions' }, [
      h('button', { i18n: 'battleye.bans.add', onclick: () => { bans.push({ id: '', minutes: '0', reason: '' }); renderBans(); applyI18n(); } }),
      h('button', { class: 'primary', i18n: 'action.save', onclick: (e) => withBusy(e.currentTarget, async () => {
        try {
          const r = await api.post('/api/battleye/bans', { bans });
          toast(r.reloaded ? t('battleye.bans.reloaded') : t('msg.saved'), 'ok');
          await loadBans();
        } catch (err) { handleErr(err); }
      })}),
    ]));
  }
  async function loadBans() {
    try {
      const r = await api.get('/api/battleye/bans');
      bans = r.bans || [];
      renderBans();
      applyI18n();
    } catch (e) { bansHost.textContent = e.message; }
  }
  wrap.append(bansCard);

  root.append(wrap);
  await load();
  await loadBans();

  // Registered BEFORE the await: navigating away mid-mount must not leave the
  // next view's teardown overwritten by this one.
  root._teardown = () => { try { cm && cm.toTextArea(); } catch {} };
  try {
    cm = await CM.mount(editorHost, { mode: null, lineWrapping: true });
    // Re-apply initial value that was set before mount.
    if (editorHost.value && !cm.getValue()) cm.setValue(editorHost.value);
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
      toast(t('msg.saved'), 'ok');
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

  root._teardown = () => { try { cm && cm.toTextArea(); } catch {} };
  try {
    cm = await CM.mount(editorHost, { mode: CM.modeFor(fileSelect.value) });
  } catch (e) { console.warn('CodeMirror load failed', e); }

  if (firstExisting) await load();
};

// --------------------------------------------------------------------- players

Views.players = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.players', 'players.subtitle'));
  let d;
  try { d = await api.get('/api/players'); }
  catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return; // ADM ingestion can take a moment

  // Summary tiles.
  root.append(h('div', { class: 'grid-4' }, [
    h('div', { class: 'card' }, [
      h('h3', { i18n: 'players.total' }),
      h('div', { class: 'metric-num', text: String(d.total || 0) }),
    ]),
    h('div', { class: 'card' }, [
      h('h3', { i18n: 'players.online' }),
      h('div', { class: 'metric-num', text: String(d.online || 0) }),
    ]),
  ]));

  // Player table.
  const tbl = h('table');
  tbl.append(h('thead', {}, h('tr', {}, [
    h('th', { i18n: 'col.name' }),
    h('th', { i18n: 'col.guid' }),
    h('th', { i18n: 'col.lastSeen' }),
    h('th', { class: 'num', i18n: 'col.sessions' }),
    h('th', { class: 'num', i18n: 'col.playtime' }),
    h('th', { class: 'num', i18n: 'col.kills' }),
    h('th', { class: 'num', i18n: 'col.deaths' }),
  ])));
  const tb = h('tbody');
  const list = d.players || [];
  for (const p of list.slice(0, 500)) {
    const nameCell = h('td', {}, [
      h('div', { style: { fontWeight: '600', display: 'flex', gap: '8px', alignItems: 'center' } }, [
        h('span', { text: p.name || '—' }),
        p.online ? h('span', { class: 'badge ok', i18n: 'players.onlineBadge' }) : null,
      ]),
      p.aliases && p.aliases.length
        ? h('div', { class: 'hint', text: t('players.aliases') + ': ' + p.aliases.join(', ') })
        : null,
    ]);
    tb.append(h('tr', {}, [
      nameCell,
      h('td', { class: 'mono', text: p.id ? p.id.slice(0, 12) + (p.id.length > 12 ? '…' : '') : '—', title: p.id || '' }),
      h('td', { text: fmtWhen(p.lastSeen) }),
      h('td', { class: 'num', text: String(p.sessions || 0) }),
      h('td', { class: 'num', text: fmtMinutes(p.playtimeMinutes || 0) }),
      h('td', { class: 'num', text: String(p.kills || 0) }),
      h('td', { class: 'num', text: String(p.deaths || 0) }),
    ]));
  }
  const tableCard = h('div', { class: 'card' }, [h('h2', { i18n: 'nav.players' })]);
  if (!list.length) {
    tableCard.append(h('div', { class: 'empty-state' }, [
      h('div', { class: 'es-hint', i18n: 'players.noData' }),
    ]));
  } else {
    tbl.append(tb);
    tableCard.append(h('div', { class: 'table-scroll' }, tbl));
  }
  root.append(tableCard);

  // Killfeed.
  const kfCard = h('div', { class: 'card' }, [h('h3', { i18n: 'players.killfeed' })]);
  const kf = d.killfeed || [];
  if (!kf.length) {
    kfCard.append(h('p', { class: 'hint', i18n: 'admlog.noEvents' }));
  } else {
    const listEl = h('div', { class: 'adm-list' });
    for (const k of kf) {
      // kind is "pvp" | "env" | "suicide". Older data has no kind, so fall
      // back to the old suicide flag.
      const kind = k.kind || (k.suicide ? 'suicide' : (k.killer ? 'pvp' : 'env'));
      const pieces = [
        h('span', { class: 'adm-time', text: k.time || '' }),
        h('span', { class: 'adm-type adm-kill', text: t('kill.kind.' + kind) }),
      ];
      // Whoever did it: a player, or the zombie/animal/fall that the log named.
      const by = kind === 'pvp' ? k.killer : (kind === 'env' ? k.source : '');
      if (by) {
        pieces.push(h('span', { class: kind === 'pvp' ? 'adm-player' : 'adm-meta', text: by }));
        pieces.push(h('span', { class: 'adm-arrow', text: '→' }));
      }
      pieces.push(h('span', { class: 'adm-player', text: k.victim }));
      const meta = [];
      if (k.weapon) meta.push(k.weapon);
      if (k.distance) meta.push(k.distance + 'm');
      if (meta.length) pieces.push(h('span', { class: 'adm-meta', text: '(' + meta.join(', ') + ')' }));
      listEl.append(h('div', { class: 'adm-row' }, pieces));
    }
    kfCard.append(listEl);
  }
  root.append(kfCard);
};

function fmtWhen(rfc) {
  if (!rfc) return '—';
  const d = new Date(rfc);
  if (isNaN(d)) return rfc;
  return d.toLocaleDateString() + ' ' + String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
}
function fmtMinutes(min) {
  if (min < 60) return min + 'm';
  return Math.floor(min / 60) + 'h ' + (min % 60) + 'm';
}

// --------------------------------------------------------------------- gameplay

Views.gameplay = async (root) => {
  root.append(pageHeader('nav.gameplay', 'gameplay.subtitle'));
  if (State.serverStatus.running) root.append(runningBanner());
  let d;
  try { d = await api.get('/api/gameplay'); }
  catch (e) { handleErr(e); return; }

  const card = h('div', { class: 'card' }, [h('h2', { i18n: 'nav.gameplay' }),
    h('p', { class: 'hint', text: d.path })]);
  root.append(card);

  if (!d.exists) {
    card.append(h('div', { class: 'empty-state' }, [
      h('div', { class: 'es-hint', i18n: 'gameplay.missing' }),
    ]));
    return;
  }

  let obj;
  try { obj = JSON.parse(d.content); }
  catch (e) {
    // The file on disk is broken JSON — offer the raw editor only.
    card.append(h('p', { class: 'warning-bar', text: t('gameplay.invalid') + ' ' + e.message }));
    obj = null;
  }

  const formHost = h('div');
  const rawHost = h('textarea', { rows: 26, style: { display: 'none', width: '100%' } });
  rawHost.value = d.content;
  let rawMode = !obj;
  const modeBtn = h('button', {});

  // Generic recursive form: booleans → switches, numbers → number inputs,
  // strings → text, arrays → JSON one-liners, objects → collapsible groups.
  // Schema-free, so every key of every DayZ version is editable.
  function fieldFor(parent, key) {
    const val = parent[key];
    if (typeof val === 'boolean') {
      const cb = h('input', { type: 'checkbox', class: 'switch' });
      cb.checked = val;
      cb.onchange = () => { parent[key] = cb.checked; };
      return h('label', {}, [cb, h('span', { text: key })]);
    }
    if (typeof val === 'number') {
      const inp = h('input', { type: 'number', step: 'any', value: String(val) });
      inp.onchange = () => { const n = Number(inp.value); if (!Number.isNaN(n)) parent[key] = n; };
      return h('div', {}, [h('label', { text: key }), inp]);
    }
    if (typeof val === 'string') {
      const inp = h('input', { type: 'text', value: val });
      inp.onchange = () => { parent[key] = inp.value; };
      return h('div', {}, [h('label', { text: key }), inp]);
    }
    if (Array.isArray(val)) {
      const inp = h('input', { type: 'text', value: JSON.stringify(val), class: 'mono' });
      inp.onchange = () => {
        try { const v = JSON.parse(inp.value); if (Array.isArray(v)) { parent[key] = v; inp.style.borderColor = ''; return; } } catch {}
        inp.style.borderColor = 'var(--error)';
      };
      return h('div', {}, [h('label', { text: key + ' []' }), inp]);
    }
    if (val && typeof val === 'object') {
      const det = h('details', { class: 'gp-group' }, [h('summary', { text: key })]);
      const inner = h('div', { class: 'gp-grid' });
      for (const k of Object.keys(val)) inner.append(fieldFor(val, k));
      det.append(inner);
      return det;
    }
    return h('div', { class: 'hint', text: key + ': null' });
  }

  function renderForm() {
    formHost.innerHTML = '';
    if (!obj) return;
    for (const k of Object.keys(obj)) {
      const f = fieldFor(obj, k);
      if (f.tagName === 'DETAILS') formHost.append(f);
      else formHost.append(h('div', { class: 'gp-grid', style: { marginBottom: '6px' } }, f));
    }
  }
  renderForm();

  function setMode(raw) {
    rawMode = raw;
    formHost.style.display = raw ? 'none' : '';
    rawHost.style.display = raw ? '' : 'none';
    modeBtn.textContent = raw ? t('gameplay.form') : t('gameplay.raw');
    if (raw && obj) rawHost.value = JSON.stringify(obj, null, 4);
    if (!raw) {
      try { obj = JSON.parse(rawHost.value); renderForm(); }
      catch (e) { toast(t('gameplay.invalid') + ' ' + e.message, 'error'); rawMode = true; formHost.style.display = 'none'; rawHost.style.display = ''; modeBtn.textContent = t('gameplay.form'); }
    }
  }
  modeBtn.onclick = () => setMode(!rawMode);
  modeBtn.textContent = rawMode ? t('gameplay.form') : t('gameplay.raw');

  async function doSave() {
    const content = rawMode ? rawHost.value : JSON.stringify(obj, null, 4);
    try { JSON.parse(content); }
    catch (e) { toast(t('gameplay.invalid') + ' ' + e.message, 'error'); return; }
    try {
      await api.post('/api/gameplay', { content });
      toast(t('msg.saved'), 'ok');
    } catch (e) { handleErr(e); }
  }
  root._save = doSave;

  card.append(
    h('div', { class: 'toolbar' }, [
      modeBtn,
      h('span', { class: 'grow' }),
      h('button', { class: 'primary', i18n: 'action.save', title: 'Ctrl+S', onclick: (e) => withBusy(e.currentTarget, doSave) }),
    ]),
    formHost,
    rawHost,
    h('p', { class: 'hint', i18n: 'gameplay.enabledNote' }),
  );
};

// --------------------------------------------------------------- attachments
//
// cfgspawnabletypes.xml editor. DayZ semantics, which the UI makes explicit:
//   <attachments chance="0.35">   → this SLOT spawns something 35% of the time
//     <item name="X" chance="60"/> → relative WEIGHT inside the slot; DayZ
//     <item name="Y" chance="40"/>   picks exactly ONE of them
// so the real probability of X is 0.35 * 60/(60+40) = 21%. We compute and show
// that number next to every item — the raw file makes it far too easy to
// misread weights as probabilities.

Views.attachments = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.attach', 'attach.subtitle'));
  if (State.serverStatus.running) root.append(runningBanner());

  let d, presets = [], classNames = [];
  try { d = await api.get('/api/spawnable'); }
  catch (e) { handleErr(e); return; }
  try { presets = (await api.get('/api/spawnable/presets')).presets || []; } catch {}
  try { classNames = (await api.get('/api/spawnable/classnames')).names || []; } catch {}
  if (myNav !== _navSeq) return; // types.xml parsing is not instant

  // A shared <datalist> lets every class-name input autocomplete against the
  // types actually present on THIS server (vanilla + mods), so typos — the
  // usual reason an attachment silently never spawns — mostly disappear.
  const dl = h('datalist', { id: 'dz-classnames' });
  for (const n of classNames.slice(0, 5000)) dl.append(h('option', { value: n }));
  root.append(dl);

  const card = h('div', { class: 'card' }, [
    h('h2', { i18n: 'nav.attach' }),
    h('p', { class: 'hint', text: d.path || '' }),
  ]);
  root.append(card);

  if (!d.exists) {
    card.append(h('div', { class: 'empty-state' }, [h('div', { class: 'es-hint', i18n: 'attach.missing' })]));
    return;
  }

  const search = h('input', { type: 'search', placeholder: t('types.search') });
  const listHost = h('div');

  // Filter by what an entry is. Counts come from the server so they describe
  // the whole file, not just the page being shown.
  let kindFilter = '';
  const kinds = d.kinds || {};
  const KIND_ORDER = ['weapon', 'clothing', 'container', 'vehicle', 'other'];
  const chipHost = h('div', { class: 'kind-chips' });
  const buildChips = () => {
    chipHost.innerHTML = '';
    const mk = (id, label, count) => {
      const b = h('button', {
        class: 'kind-chip' + (kindFilter === id ? ' active' : ''),
        type: 'button',
        onclick: () => { kindFilter = id; buildChips(); renderList(); },
      }, [h('span', { text: label }), h('span', { class: 'kind-count', text: String(count) })]);
      return b;
    };
    chipHost.append(mk('', t('attach.kind.all'), (d.types || []).length));
    for (const k of KIND_ORDER) {
      if (kinds[k]) chipHost.append(mk(k, t('attach.kind.' + k), kinds[k]));
    }
  };
  buildChips();

  const presetSel = h('select', { style: { maxWidth: '240px' } });
  presetSel.append(h('option', { value: '', text: t('attach.preset.pick') }));
  // Grouped by kind: the file covers weapons, clothing, containers and
  // vehicles, and the picker should make that visible rather than implying
  // the editor is weapons-only.
  for (const k of ['weapon', 'clothing', 'container', 'vehicle']) {
    const inKind = presets.filter(p => (p.kind || 'weapon') === k);
    if (!inKind.length) continue;
    const g = h('optgroup', { label: t('attach.kind.' + k) });
    for (const p of inKind) g.append(h('option', { value: p.id, text: p.label }));
    presetSel.append(g);
  }

  card.append(
    h('div', { class: 'toolbar' }, [
      search,
      h('span', { class: 'grow' }),
      presetSel,
      h('button', { class: 'primary', i18n: 'attach.add', onclick: () => {
        const p = presets.find(x => x.id === presetSel.value);
        // Deep-copy the preset so editing never mutates the cached template.
        const seed = p ? JSON.parse(JSON.stringify(p.type)) : { name: '', attachments: [] };
        openAttachEditor(seed, true);
      }}),
    ]),
    chipHost,
    listHost,
  );

  let all = d.types || [];
  function renderList() {
    listHost.innerHTML = '';
    const q = search.value.trim().toLowerCase();
    let rows = all;
    if (kindFilter) rows = rows.filter(x => (x.kind || 'other') === kindFilter);
    if (q) rows = rows.filter(x => x.name.toLowerCase().includes(q));
    if (!rows.length) {
      listHost.append(h('div', { class: 'empty-state' }, [h('div', { class: 'es-hint', i18n: 'attach.none' })]));
      applyI18n();
      return;
    }
    const tbl = h('table', { class: 'attach-table' });
    tbl.append(h('thead', {}, h('tr', {}, [
      h('th', { i18n: 'col.name' }),
      h('th', { i18n: 'attach.col.kind' }),
      hlp(h('th', { class: 'num', i18n: 'attach.col.on' }), 'attach.slotChance'),
      hlp(h('th', { class: 'num', i18n: 'attach.col.inside' }), 'attach.cargo'),
      h('th', { i18n: 'attach.summary' }),
      h('th', { text: '' }),
    ])));
    const tb = h('tbody');
    // Cap the DOM at 400 rows; the file can hold thousands on a modded server.
    for (const st of rows.slice(0, 400)) {
      const att = st.attachments || [];
      const cargo = st.cargo || [];
      const kind = st.kind || 'other';

      // Summarise whichever side actually has content, so a crate describes
      // its contents rather than showing an empty attachments column.
      const describe = (groups) => groups.slice(0, 3)
        .map(g => g.preset ? '⚙ ' + g.preset : (g.items || []).map(i => i.name).join(' / '))
        .filter(Boolean).join('  •  ');
      let summary = describe(att.length ? att : cargo);
      const shown = (att.length ? att : cargo).length;
      if (shown > 3) summary += ' …';
      if (!summary && st.hoarder) summary = t('attach.hoarderOnly');

      const num = (v) => h('td', { class: 'num' + (v ? '' : ' zero'), text: v ? String(v) : '—' });

      tb.append(h('tr', {}, [
        h('td', {}, [
          h('span', { class: 'attach-name-cell', text: st.name }),
          st.category ? h('span', { class: 'attach-cat', text: st.category }) : null,
        ]),
        h('td', {}, h('span', { class: 'kind-badge k-' + kind, text: t('attach.kind.' + kind) })),
        num(att.length),
        num(cargo.length),
        h('td', { class: 'hint attach-sum', text: summary }),
        h('td', {}, h('button', { i18n: 'action.edit', onclick: () => openAttachEditor(st, false) })),
      ]));
    }
    tbl.append(tb);
    listHost.append(h('div', { class: 'table-scroll' }, tbl));
    if (rows.length > 400) {
      listHost.append(h('p', { class: 'hint', text: t('attach.truncated').replace('{n}', rows.length) }));
    }
    applyI18n();
  }
  let _t = 0;
  search.oninput = () => { clearTimeout(_t); _t = setTimeout(renderList, 200); };
  renderList();

  async function reload() {
    try {
      const fresh = await api.get('/api/spawnable');
      all = fresh.types || [];
      renderList();
    } catch (e) { handleErr(e); }
  }

  // ---- editor modal -------------------------------------------------------
  function openAttachEditor(seed, isNew) {
    // Work on a private copy; nothing touches the table until a successful save.
    const model = {
      name: seed.name || '',
      attachments: JSON.parse(JSON.stringify(seed.attachments || [])),
      cargo: JSON.parse(JSON.stringify(seed.cargo || [])),
      hoarder: !!seed.hoarder,
      damageMin: seed.damageMin || '',
      damageMax: seed.damageMax || '',
      tags: (seed.tags || []).slice(),
    };
    const m = openModal({ title: isNew ? t('attach.add') : model.name, wide: true });

    const nameInp = h('input', { type: 'text', value: model.name, list: 'dz-classnames',
      placeholder: 'AKM' });
    nameInp.oninput = () => { model.name = nameInp.value.trim(); };

    const groupsHost = h('div');

    // Draws one list of slots — <attachments> or <cargo>. They share the exact
    // same shape in the file, so they share the renderer; only the label and
    // the array differ.
    function renderSlots(host, list, kindKey) {
      host.innerHTML = '';
      list.forEach((g, gi) => {
        const items = g.items || (g.items = []);

        const chanceInp = h('input', { type: 'text', value: g.chance ?? '1.00',
          class: 'attach-num', title: t('attach.slotChance') });
        const slotBadge = h('span', { class: 'attach-pct slot' });

        const head = h('div', { class: 'attach-head' }, [
          h('span', { class: 'attach-slot-no', text: '#' + (gi + 1) }),
          h('span', { class: 'attach-head-label', i18n: 'attach.slotChance' }),
          help('attach.slotChance'),
          chanceInp,
          h('span', { class: 'attach-eq', text: '=' }),
          slotBadge,
          h('span', { class: 'grow' }),
          h('button', { class: 'icon-x', text: '\u00d7', title: t('attach.removeSlot'),
            onclick: () => { list.splice(gi, 1); renderAll(); } }),
        ]);

        // Badges update IN PLACE. Re-rendering the group on every keystroke
        // detached the focused input mid-typing, so only the first character
        // ever landed and the field ended up holding garbage.
        const itemBadges = [];
        const recalc = () => {
          const slotPct = chanceOf(g.chance);
          const total = items.reduce((a, it) => a + chanceOf(it.chance), 0);
          slotBadge.textContent = fmtPct(slotPct);
          for (const b of itemBadges) {
            const w = chanceOf(b.it.chance);
            const real = total > 0 ? slotPct * (w / total) : 0;
            b.el.textContent = fmtPct(real);
            b.el.className = 'attach-pct' + (real > 0 ? '' : ' zero');
          }
        };
        chanceInp.oninput = () => { g.chance = chanceInp.value.trim(); recalc(); };

        // One grid so the columns line up down the whole slot; without it the
        // class input ate the row and the weight beside it looked incidental.
        const itemsHost = h('div', { class: 'attach-items' });
        itemsHost.append(h('div', { class: 'attach-item attach-cols' }, [
          h('span', { i18n: 'attach.col.class' }),
          h('span', {}, [h('span', { i18n: 'attach.col.weight' }), help('attach.itemWeight')]),
          h('span', { class: 'ta-r', i18n: 'attach.col.chance' }),
          h('span', {}),
        ]));
        items.forEach((it, ii) => {
          const nameIn = h('input', { type: 'text', value: it.name || '', list: 'dz-classnames',
            placeholder: t('attach.itemName') });
          const wIn = h('input', { type: 'text', value: it.chance ?? '1.00', class: 'attach-num',
            title: t('attach.weight') });
          const pctBadge = h('span', { class: 'attach-pct', title: t('attach.realChance') });
          const warnBadge = h('span', { class: 'attach-warn', text: '!', title: t('attach.unknownClass'),
            style: { display: 'none' } });
          const row = h('div', { class: 'attach-item' }, [
            h('span', { class: 'attach-name' }, [nameIn, warnBadge]),
            wIn, pctBadge,
            h('button', { class: 'icon-x', text: '\u00d7', title: t('attach.removeItem'),
              onclick: () => { items.splice(ii, 1); renderAll(); } }),
          ]);
          // A class absent from types.xml is the #1 reason an attachment
          // silently never spawns — flag it live as the name is typed.
          const checkName = () => {
            const unknown = !!it.name && classNames.length > 0 && !classNames.includes(it.name);
            row.classList.toggle('unknown', unknown);
            warnBadge.style.display = unknown ? '' : 'none';
          };
          nameIn.oninput = () => { it.name = nameIn.value.trim(); checkName(); };
          wIn.oninput = () => { it.chance = wIn.value.trim(); recalc(); };
          checkName();
          itemBadges.push({ it, el: pctBadge });
          itemsHost.append(row);
        });
        recalc();

        host.append(h('div', { class: 'attach-group' }, [
          head,
          h('div', { class: 'attach-body' }, [
            itemsHost,
            h('button', { class: 'attach-add', i18n: 'attach.addItem',
              onclick: () => { items.push({ name: '', chance: '1.00' }); renderAll(); applyI18n(); } }),
          ]),
        ]));
      });
      if (!list.length) {
        host.append(h('p', { class: 'hint', i18n: kindKey === 'cargo' ? 'attach.noCargo' : 'attach.noSlots' }));
      }
    }

    const cargoHost = h('div');
    function renderAll() {
      renderSlots(groupsHost, model.attachments, 'attachments');
      renderSlots(cargoHost, model.cargo, 'cargo');
      applyI18n();
    }
    renderAll();

    // <hoarder> marks persistent storage (barrels, tents, stashes): DayZ counts
    // what is inside towards the hoarder limit instead of world loot.
    const hoarderChk = h('input', { type: 'checkbox', class: 'switch' });
    hoarderChk.checked = !!model.hoarder;
    hoarderChk.onchange = () => { model.hoarder = hoarderChk.checked; };

    // <damage min max>: the condition range the item spawns in, 0..1.
    const dmgMin = h('input', { type: 'text', class: 'attach-num', value: model.damageMin || '' });
    const dmgMax = h('input', { type: 'text', class: 'attach-num', value: model.damageMax || '' });
    dmgMin.oninput = () => { model.damageMin = dmgMin.value.trim(); };
    dmgMax.oninput = () => { model.damageMax = dmgMax.value.trim(); };

    const tagsInp = h('input', { type: 'text', value: (model.tags || []).join(', '),
      placeholder: 'floor, shelves' });
    tagsInp.oninput = () => {
      model.tags = tagsInp.value.split(',').map(x => x.trim()).filter(Boolean);
    };

    const addSlot = (list) => {
      list.push({ chance: '1.00', items: [{ name: '', chance: '1.00' }] });
      renderAll(); applyI18n();
    };

    m.body.append(
      h('div', {}, [withHelp(h('label', { i18n: 'attach.weapon' }), 'attach.class'), nameInp]),

      // --- attachments: what is mounted ON the item ---
      h('div', { class: 'attach-section' }, [
        withHelp(h('h3', { i18n: 'attach.section.attachments' }), 'attach.slotChance'),
        h('p', { class: 'hint', i18n: 'attach.help' }),
        groupsHost,
        h('button', { class: 'attach-add', i18n: 'attach.addSlot', onclick: () => addSlot(model.attachments) }),
      ]),

      // --- cargo: what is INSIDE it. Half of cfgspawnabletypes.xml is this,
      // and it was not editable at all before. ---
      h('div', { class: 'attach-section' }, [
        withHelp(h('h3', { i18n: 'attach.section.cargo' }), 'attach.cargo'),
        h('p', { class: 'hint', i18n: 'attach.cargo.help' }),
        cargoHost,
        h('button', { class: 'attach-add', i18n: 'attach.addCargo', onclick: () => addSlot(model.cargo) }),
      ]),

      // --- properties that apply to the whole entry ---
      h('div', { class: 'attach-section' }, [
        h('h3', { i18n: 'attach.section.props' }),
        h('div', { class: 'attach-props' }, [
          h('label', {}, [hoarderChk, h('span', { i18n: 'attach.hoarder' }), help('attach.hoarder')]),
          h('div', { class: 'attach-prop' }, [
            withHelp(h('span', { class: 'k', i18n: 'attach.damage' }), 'attach.damage'),
            dmgMin, h('span', { class: 'attach-eq', text: '–' }), dmgMax,
          ]),
          h('div', { class: 'attach-prop grow' }, [
            withHelp(h('span', { class: 'k', i18n: 'attach.tags' }), 'attach.tags'),
            tagsInp,
          ]),
        ]),
      ]),

      h('div', { class: 'actions' }, [
        h('span', { class: 'grow' }),
        h('button', { class: 'primary', i18n: 'action.save', onclick: (e) => withBusy(e.currentTarget, doSave) }),
        !isNew ? h('button', { class: 'danger', i18n: 'action.delete', onclick: async (e) => {
          const btn = e.currentTarget; // capture BEFORE the await
          if (!(await confirmModal(t('confirm.delete').replace('{name}', model.name), { danger: true, okText: t('action.delete') }))) return;
          await withBusy(btn, async () => {
            try {
              await api.del('/api/spawnable/item?name=' + encodeURIComponent(model.name));
              toast(t('msg.deleted'), 'ok');
              m.close();
              await reload();
            } catch (err) { handleErr(err); }
          });
        }}) : null,
        h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
      ]),
    );

    async function doSave() {
      if (!model.name) { toast(t('attach.needName'), 'error'); return; }
      const payload = {
        name: model.name,
        hoarder: model.hoarder,
        damageMin: model.damageMin,
        damageMax: model.damageMax,
        tags: model.tags,
        attachments: model.attachments,
        cargo: model.cargo,
      };
      try {
        await api.post('/api/spawnable/item', payload);
        toast(t('msg.saved'), 'ok');
        m.close();
        await reload();
      } catch (e) { handleErr(e); }
    }
  }
};

// fmtPct renders a 0..1 probability as a percentage with sane precision.
// chanceOf reads a chance/weight the way DayZ does: absent or blank means 1.
// Reading it as 0 made every badge on a vanilla-formatted entry show 0%.
function chanceOf(v, dflt = 1) {
  if (v === undefined || v === null || String(v).trim() === '') return dflt;
  const n = parseFloat(v);
  return Number.isFinite(n) ? n : dflt;
}

function fmtPct(v) {
  if (!Number.isFinite(v) || v <= 0) return '0%';
  const pct = v * 100;
  return (pct >= 10 ? pct.toFixed(0) : pct.toFixed(1)) + '%';
}

main();
