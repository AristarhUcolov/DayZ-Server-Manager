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

// --------------------------------------------------------------------- servers

// Srv is the DayZ server this tab manages (multi-server). Every API call names
// it in a header; URLs the browser loads by itself (images, downloads, the log
// stream) carry ?srv= via srvUrl(). Kept per tab, so two tabs can manage two
// servers; the last choice is where a new tab starts.
const Srv = {
  id: 'main',
  list: [],
  init() {
    let id = '';
    try {
      id = new URLSearchParams(location.search).get('srv') || sessionStorage.getItem('srv') ||
        localStorage.getItem('srv') || '';
    } catch {}
    this.id = /^[A-Za-z0-9]{1,16}$/.test(id) ? id : 'main';
  },
  remember(id) {
    try { sessionStorage.setItem('srv', id); localStorage.setItem('srv', id); } catch {}
  },
  // switchTo opens another server in this tab: a full reload, so no view keeps
  // data (caches, open editors, polls) of the server it left.
  switchTo(id) {
    if (!id) return;
    this.remember(id);
    const url = new URL(location.href);
    url.searchParams.delete('srv');
    location.replace(url.pathname + url.search + location.hash);
    if (url.pathname + url.search === location.pathname + location.search) location.reload();
  },
  current() { return this.list.find(s => s.id === this.id) || null; },
};
Srv.init();

// srvUrl adds the selected server to an API URL the browser fetches itself.
function srvUrl(path) {
  if (!Srv.id || Srv.id === 'main') return path;
  return path + (path.includes('?') ? '&' : '?') + 'srv=' + encodeURIComponent(Srv.id);
}

// srvHeaders are the request headers naming the selected server.
function srvHeaders(extra) {
  return Object.assign({ 'X-DZM-Server': Srv.id || 'main' }, extra || {});
}

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

// apiError turns a failed response into an Error. Auth and guard errors come
// as JSON {error: "<i18n key>", retryAfter?, message?} and are translated;
// everything else keeps the server's plain-text message. A 401 asking for a
// login (session expired, password changed elsewhere) brings the login screen
// back.
async function apiError(r) {
  const text = await r.text();
  let data = null;
  try { data = JSON.parse(text); } catch { /* plain text */ }
  if (r.status === 401 && data && data.auth) onAuthRequired();
  // The server this tab was managing is no longer in the panel (removed in
  // another tab): fall back to the main one.
  if (r.status === 404 && data && data.error === 'servers.unknown' && Srv.id !== 'main') {
    Srv.remember('main');
    Srv.id = 'main';
    setTimeout(() => location.reload(), 50);
  }
  let msg = text;
  if (data && typeof data.error === 'string') {
    const k = data.error;
    msg = (State.i18n[k] ? t(k) : (data.message || k)).replace('{s}', String(data.retryAfter ?? ''));
  }
  const err = new Error(msg || (r.status + ' ' + r.statusText));
  err.status = r.status;
  err.data = data;
  return err;
}

const api = {
  // opts.timeout (ms) aborts a hung request — the status poll uses it so a
  // dead connection is noticed instead of waiting on the browser's default.
  async get(path, opts) {
    Progress.start();
    const ctl = opts && opts.timeout ? new AbortController() : null;
    const timer = ctl ? setTimeout(() => ctl.abort(), opts.timeout) : 0;
    try {
      const r = await fetch(path, { credentials: 'same-origin', headers: srvHeaders(), signal: ctl ? ctl.signal : undefined });
      if (!r.ok) throw await apiError(r);
      return r.json();
    } finally { clearTimeout(timer); Progress.end(); }
  },
  async send(path, method, body) {
    Progress.start();
    try {
      const r = await fetch(path, {
        method,
        credentials: 'same-origin',
        headers: srvHeaders(body ? { 'Content-Type': 'application/json' } : {}),
        body: body ? JSON.stringify(body) : undefined,
      });
      if (!r.ok) throw await apiError(r);
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
  // Right-to-left languages (Arabic) mirror the whole layout.
  document.documentElement.dir = data.dir === 'rtl' ? 'rtl' : 'ltr';
  applyI18n();
}

function t(key) { return State.i18n[key] || key; }

// tn translates key and fills in {vars}. A count shown with a noun is written
// as an ICU-style plural block — "{n, plural, one {# point} other {# points}}"
// — and the form is picked by the UI language's own rules (Intl.PluralRules:
// Russian one/few/many, English one/other, …), # being the number. That is
// what keeps "4 точек" and "1 points" out of the panel.
let _plural = null;
function pluralCategory(n) {
  if (!_plural || _plural.lang !== State.lang) {
    let rules;
    // "md" (Moldovan) is the panel's code for Romanian as written in Moldova;
    // Intl knows the language as "ro" (one / few / other).
    const lang = State.lang === 'md' ? 'ro' : (State.lang || 'en');
    try { rules = new Intl.PluralRules(lang); } catch { rules = new Intl.PluralRules('en'); }
    _plural = { lang: State.lang, rules };
  }
  return _plural.rules.select(Number(n) || 0);
}
const PLURAL_RE = /\{(\w+),\s*plural,((?:\s*(?:=\d+|zero|one|two|few|many|other)\s*\{(?:[^{}]|\{\w+\})*\})+)\s*\}/g;
const PLURAL_FORM_RE = /(=\d+|zero|one|two|few|many|other)\s*\{((?:[^{}]|\{\w+\})*)\}/g;
function fmtCount(n) {
  if (typeof n !== 'number') return String(n);
  try { return n.toLocaleString(State.lang || undefined, { maximumFractionDigits: 2 }); } catch { return String(n); }
}
function tn(key, vars = {}) {
  let s = t(key).replace(PLURAL_RE, (m, name, body) => {
    const n = vars[name];
    const forms = {};
    body.replace(PLURAL_FORM_RE, (mm, cat, txt) => { forms[cat] = txt; return mm; });
    const txt = forms['=' + n] ?? forms[pluralCategory(n)] ?? forms.other ?? '';
    return txt.replace(/#/g, fmtCount(n));
  });
  for (const [k, v] of Object.entries(vars)) s = s.split('{' + k + '}').join(typeof v === 'number' ? fmtCount(v) : String(v));
  return s;
}

// matchBrowserLang picks the UI language closest to the browser's preferred
// languages, or '' when none is available. Mirrors i18n.Match on the server.
function matchBrowserLang() {
  const codes = new Set((State.languages || []).map(l => l.code));
  const prefs = (navigator.languages && navigator.languages.length) ? navigator.languages : [navigator.language || ''];
  for (const raw of prefs) {
    const tag = String(raw || '').toLowerCase().replace(/_/g, '-');
    if (!tag) continue;
    if (codes.has(tag)) return tag;
    const [base, ...rest] = tag.split('-');
    const region = rest.join('-');
    if (base === 'zh' && /hant|tw|hk|mo/.test(region) && codes.has('zh-tw')) return 'zh-tw';
    if ((base === 'ro' || base === 'mo') && codes.has('md')) return 'md';
    if (codes.has(base)) return base;
  }
  return '';
}

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
  // Icon-only controls: translated tooltip + accessible name.
  document.querySelectorAll('[data-i18n-title]').forEach(el => {
    const v = t(el.dataset.i18nTitle);
    el.title = v;
    el.setAttribute('aria-label', v);
  });
  // Accessible name only (the control already shows visible text).
  document.querySelectorAll('[data-i18n-aria]').forEach(el => {
    el.setAttribute('aria-label', t(el.dataset.i18nAria));
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

// assocId gives a form control an id and returns it, so a sibling <label> can
// point at it with `for=` — a visual label sitting next to an input is not
// enough; a screen reader needs the association. No-op if it already has one.
let _assocSeq = 0;
function assocId(el) {
  if (el && !el.id && /^(INPUT|SELECT|TEXTAREA)$/.test(el.tagName || '')) el.id = 'fld-' + (++_assocSeq);
  return el ? el.id : '';
}

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

// toastAction is a toast with one button (e.g. "Open Vehicles").
function toastAction(msg, kind, actionLabel, onAction) {
  const region = document.getElementById('toast-region');
  if (!region) return;
  while (region.children.length >= 3) region.firstChild.remove();
  const el = h('div', { class: `toast toast-action ${kind || ''}`, role: 'status' });
  const dismiss = () => { el.classList.remove('visible'); setTimeout(() => el.remove(), 250); };
  el.append(h('span', { text: msg }), h('button', { class: 'small', text: actionLabel, onclick: (e) => { e.stopPropagation(); dismiss(); onAction(); } }));
  el.onclick = dismiss;
  region.append(el);
  requestAnimationFrame(() => el.classList.add('visible'));
  setTimeout(dismiss, 9000);
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
  const base = String(name || '').split('/')[0];
  document.querySelectorAll('.nav a').forEach(a => {
    const on = a.dataset.route === base;
    a.classList.toggle('active', on);
    if (on) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
  });
  // Keep the active entry visible in the (scrolling) sidebar — the lower
  // groups sat below its fold. Not scrollIntoView: that can move the page too.
  const side = document.getElementById('sidebar');
  const act = side && side.querySelector('.nav a.active');
  if (act && side.scrollHeight > side.clientHeight) {
    const s = side.getBoundingClientRect(), r = act.getBoundingClientRect();
    if (r.top < s.top + 8 || r.bottom > s.bottom - 8) side.scrollTop += r.top - s.top - (side.clientHeight - r.height) / 2;
  }
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

// _route is the full route being shown, sub-tab included ('settings/access').
// The sidebar only marks the base section, so it can't answer this itself.
let _route = 'dashboard';

async function navigate(name) {
  // Unsaved edits on the page being left: ask first.
  const cur = document.getElementById('view');
  if (cur && typeof cur._isDirty === 'function' && cur._isDirty()) {
    if (!(await confirmModal(t('refresh.discard'), { danger: true, okText: t('action.discard') }))) {
      if (routeFromHash() !== _route) history.replaceState(null, '', '#' + _route);
      return;
    }
    cur._isDirty = null;
  }
  const myNav = ++_navSeq;
  _route = Views[name] ? name : 'dashboard';
  setActiveRoute(name);
  // The Guide and Sponsors pages end with their own support call — no second
  // card from the persistent footer under them.
  document.body.classList.toggle('no-support-footer', _route === 'guide' || _route === 'sponsors');
  // Mirror the route into the URL hash so a reload restores this section.
  // Done before awaiting the view so the resulting hashchange sees the active
  // route already updated and becomes a no-op (avoids a double render).
  if (routeFromHash() !== name) location.hash = name;
  const view = Views[name] || Views.dashboard;
  const root = document.getElementById('view');
  if (root._teardown) { try { root._teardown(); } catch {} root._teardown = null; }
  root._save = null; // stale Ctrl+S handlers must not survive navigation
  // Same for the unsaved-changes hooks Refresh consults; a page that never sets
  // them would otherwise inherit the previous page's prompt.
  root._isDirty = null;
  root._beforeRefresh = null;
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

// Closing or reloading the tab with unsaved edits: the browser asks.
window.addEventListener('beforeunload', (e) => {
  const root = document.getElementById('view');
  if (root && typeof root._isDirty === 'function' && root._isDirty()) { e.preventDefault(); e.returnValue = ''; }
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
  // e.code too: with a Russian layout e.key is "ы", and Ctrl+S did nothing.
  if (e.key === 's' || e.key === 'S' || e.code === 'KeyS') {
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
    else if (k === 'style' && typeof v === 'object') {
      for (const [sk, sv] of Object.entries(v)) {
        if (sk.startsWith('--')) el.style.setProperty(sk, sv); else el.style[sk] = sv;
      }
    }
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

// adminLogWarning returns a warning bar for ADM-backed pages (players, live map,
// activity) when -adminlog is off — those features read the admin log the server
// writes, so without it they stay empty. Returns null when the log is enabled.
function adminLogWarning() {
  if (!State.config || State.config.adminLog !== false) return null;
  return h('div', { class: 'warning-bar', i18n: 'warn.adminlogOff' });
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
    const s = await api.get('/api/server/status', { timeout: 10000 });
    State.serverStatus = s;
    _statusFails = 0;
    if (Number.isFinite(s.serverTime)) State.clockSkew = s.serverTime - Date.now();
    chip.classList.remove('offline');
    chip.removeAttribute('title');
    // "Starting…" until the server answers the Steam query (mission loaded).
    const loading = s.running && !s.stopping && !!s.loading;
    chip.classList.toggle('running', s.running && !s.stopping && !loading);
    chip.classList.toggle('stopping', !!s.stopping || loading);
    chip.classList.toggle('stopped', !s.running);
    txt.dataset.i18n = s.stopping ? 'status.stopping' : (loading ? 'status.loading' : (s.running ? 'status.running' : 'status.stopped'));
    txt.textContent = t(txt.dataset.i18n);
  } catch (err) {
    // After a few consecutive failures the manager itself is unreachable —
    // show that instead of freezing on the last known Running/Stopped forever.
    if (err && err.status === 401) return; // the login screen is taking over
    if (++_statusFails >= 3 && chip) {
      chip.classList.remove('running', 'stopped');
      chip.classList.add('offline');
      txt.dataset.i18n = 'status.offline';
      txt.textContent = t('status.offline');
      // The usual reason on a phone: the LAN address only works at home.
      chip.title = t('status.offline.hint');
    }
  }
}

// --------------------------------------------------------------------- login

// Panel login (Settings → Panel login). The login screen is a separate layer
// over the app; on success the page reloads so every view starts signed in.
let _loginShown = false;

function onAuthRequired() {
  if (_loginShown) return;
  showLogin().then(() => location.reload());
}

// tryLocalAutologin trades the one-time token the manager puts into the URL
// it opens on this computer (#autologin=…) for a session. The token is
// removed from the address bar either way.
async function tryLocalAutologin() {
  const m = /(?:^#|&)autologin=([A-Za-z0-9_-]+)/.exec(location.hash);
  if (!m) return false;
  history.replaceState(null, '', location.pathname + location.search);
  try {
    await api.post('/api/auth/local', { token: m[1] });
    return true;
  } catch { return false; }
}

// showLogin resolves once the user has signed in.
function showLogin() {
  _loginShown = true;
  document.body.classList.add('auth-locked');
  const screen = document.getElementById('login-screen');
  const form = document.getElementById('login-form');
  const user = document.getElementById('login-user');
  const pass = document.getElementById('login-pass');
  const remember = document.getElementById('login-remember');
  const errBox = document.getElementById('login-error');
  const submit = document.getElementById('login-submit');
  const langSel = document.getElementById('login-lang');
  screen.classList.remove('hidden');
  applyI18n();
  fillLangSelect(langSel, State.lang, false);
  langSel.onchange = async () => {
    localStorage.setItem('lang', langSel.value);
    try { await loadI18n(langSel.value); } catch {}
  };
  try { user.value = localStorage.getItem('loginUser') || user.value; } catch {}
  (user.value ? pass : user).focus();
  const showErr = (msg) => { errBox.textContent = msg; errBox.hidden = !msg; };
  let lockTimer = 0;
  return new Promise((resolve) => {
    form.onsubmit = async (e) => {
      e.preventDefault();
      if (submit.disabled) return;
      showErr('');
      submit.disabled = true;
      let locked = false;
      try {
        await api.post('/api/auth/login', { user: user.value.trim(), password: pass.value, remember: remember.checked });
        try { localStorage.setItem('loginUser', user.value.trim()); } catch {}
        pass.value = '';
        screen.classList.add('hidden');
        document.body.classList.remove('auth-locked');
        _loginShown = false;
        resolve();
      } catch (err) {
        pass.value = '';
        pass.focus();
        const wait = err.data && err.data.retryAfter;
        if (err.status === 429 && wait) {
          // Too many attempts: count the lock down, then allow the next try.
          locked = true;
          let left = wait;
          clearInterval(lockTimer);
          const tick = () => {
            if (left <= 0) { clearInterval(lockTimer); showErr(''); submit.disabled = false; return; }
            showErr(t('auth.err.locked').replace('{s}', String(left--)));
          };
          tick();
          lockTimer = setInterval(tick, 1000);
          return;
        }
        showErr(err.message || t('auth.err.invalid'));
      } finally {
        if (!locked) submit.disabled = false;
      }
    };
  });
}

// Logout button in the topbar (only when a login is configured).
function setupLogoutButton() {
  const btn = document.getElementById('logout-btn');
  if (!btn) return;
  const on = !!(State.info && State.info.auth && State.info.auth.enabled);
  btn.hidden = !on;
  btn.onclick = async () => {
    try { await api.post('/api/auth/logout', {}); } catch {}
    location.reload();
  };
}

// --------------------------------------------------------------------- first-run

async function ensureFirstRunDone() {
  if (State.config.firstRunDone) return;
  const modal = document.getElementById('first-run');
  modal.classList.remove('hidden');
  fillLangSelect(document.getElementById('fr-lang'), State.lang || State.config.language || 'en', false);
  document.getElementById('fr-vanilla').value = State.config.vanillaDayZPath || '';
  document.getElementById('fr-modsfolder').value = State.config.modsFolderPath || '';
  // Mod source choice shows only the field that mode needs.
  const frSource = () => (document.querySelector('input[name=fr-modsource]:checked') || {}).value || 'client';
  const frSyncSource = () => {
    const v = frSource();
    document.getElementById('fr-client-block').classList.toggle('hidden', v !== 'client');
    document.getElementById('fr-folder-block').classList.toggle('hidden', v !== 'folder');
    document.getElementById('fr-server-block').classList.toggle('hidden', v !== 'server');
    document.getElementById('fr-steamcmd-block').classList.toggle('hidden', v !== 'steamcmd');
  };
  document.querySelectorAll('input[name=fr-modsource]').forEach(r => {
    r.checked = r.value === (State.config.modSource || 'client');
    r.addEventListener('change', frSyncSource);
  });
  frSyncSource();

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

  // Panel login: optional, but required when the panel is opened to the
  // internet (the server would otherwise belong to whoever finds the address).
  const frLoginOn = () => (document.querySelector('input[name=fr-login]:checked') || {}).value === 'on';
  const frSyncLogin = () => {
    const internet = (document.querySelector('input[name=fr-exposure]:checked') || {}).value === 'internet';
    const off = document.querySelector('input[name=fr-login][value=off]');
    if (internet) document.querySelector('input[name=fr-login][value=on]').checked = true;
    if (off) off.disabled = internet;
    document.getElementById('fr-login-internet').classList.toggle('hidden', !internet);
    document.getElementById('fr-login-block').classList.toggle('hidden', !frLoginOn());
  };
  document.querySelectorAll('input[name=fr-login], input[name=fr-exposure]').forEach(r => r.addEventListener('change', frSyncLogin));
  frSyncLogin();

  document.getElementById('fr-finish').onclick = (e) => withBusy(e.currentTarget, async () => {
    const exposure = document.querySelector('input[name=fr-exposure]:checked').value;
    const body = {
      language: document.getElementById('fr-lang').value,
      vanillaDayZPath: document.getElementById('fr-vanilla').value.trim(),
      modSource: frSource(),
      modsFolderPath: document.getElementById('fr-modsfolder').value.trim(),
      exposure,
    };
    if (frLoginOn()) {
      const user = document.getElementById('fr-user').value.trim();
      const p1 = document.getElementById('fr-pass').value;
      const p2 = document.getElementById('fr-pass2').value;
      const bad = !user ? 'auth.err.user' : p1.length < 8 ? 'auth.err.short' : p1 !== p2 ? 'auth.err.mismatch' : '';
      if (bad) { toast(t(bad), 'error'); return; }
      body.login = { user, password: p1 };
    }
    try {
      State.config = await api.post('/api/config/finish-first-run', body);
      if (body.login) {
        try { localStorage.setItem('loginUser', body.login.user); } catch {}
        State.info = await api.get('/api/info');
        setupLogoutButton();
      }
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

// managerNotices tells the admin how the previous run of the manager ended
// when that was not a normal exit (a crash — with what Go printed, to send to
// the developer — or the process ended from outside), and when the computer
// slept (the server and the panel were down meanwhile).
function managerNotices() {
  const host = h('div', { class: 'mgr-notices' });
  (async () => {
    let d;
    try { d = await api.get('/api/app/lastexit'); } catch { return; }
    const close = (what, card) => h('button', { type: 'button', class: 'icon-btn mgr-notice-x', title: t('action.close'), 'aria-label': t('action.close'), text: '×',
      onclick: async () => { card.remove(); try { await api.post('/api/app/lastexit', { what }); } catch {} } });
    const awakeBtn = () => h('button', { type: 'button', class: 'secondary small', i18n: 'mgr.keepAwake.go',
      onclick: () => navigate('settings').then(() => {
        const el = document.getElementById('settings-startup');
        if (el) { el.scrollIntoView({ behavior: 'smooth', block: 'center' }); el.classList.add('flash-target'); setTimeout(() => el.classList.remove('flash-target'), 1600); }
      }) });
    if (d.exit) {
      const e = d.exit, crash = e.kind === 'crash';
      const when = fmtWhen(e.lastSeen || e.started);
      const card = h('div', { class: 'card mgr-notice ' + (crash ? 'is-crash' : 'is-killed') });
      const body = [
        h('div', { class: 'mgr-notice-head' }, [
          h('span', { class: 'mgr-notice-icon', html: svgIcon(crash ? 'i-health' : 'i-clock', 18) }),
          h('b', { i18n: crash ? 'mgr.crash.title' : 'mgr.killed.title' }),
          h('span', { class: 'grow' }),
        ]),
        h('p', { text: t(crash ? 'mgr.crash.text' : 'mgr.killed.text').replace('{when}', when) }),
      ];
      // The guardian brought it back at once: the server was managed again
      // within seconds.
      if (e.autoRestarted) body.push(h('p', { class: 'mgr-restarted' }, [h('span', { html: svgIcon('i-refresh', 14) }), h('span', { i18n: 'mgr.restarted' })]));
      // Windows' own record names the part that failed.
      if (e.fault) body.push(h('p', { class: 'hint mono mgr-fault', text: t('mgr.fault').replace('{fault}', e.fault) }));
      if (crash && (e.crash || e.fault)) {
        const report = [e.fault ? 'Windows: ' + e.fault : '', e.crash || ''].filter(Boolean).join('\n\n');
        const pre = h('pre', { class: 'mono mgr-crash-log', text: report });
        body.push(h('details', {}, [h('summary', { i18n: 'mgr.crash.details' }), pre,
          h('button', { type: 'button', class: 'secondary small', i18n: 'action.copy', onclick: () => copyText(report) })]));
      } else if (!crash) {
        body.push(h('ul', { class: 'mgr-notice-tips' }, [
          h('li', { i18n: 'mgr.killed.tip.sleep' }),
          h('li', { i18n: 'mgr.killed.tip.rdp' }),
          h('li', { i18n: 'mgr.killed.tip.power' }),
        ]));
        body.push(h('div', { class: 'actions' }, [awakeBtn()]));
      }
      card.append(...body);
      card.querySelector('.mgr-notice-head').append(close('exit', card));
      host.append(card);
    }
    if (d.sleep) {
      const card = h('div', { class: 'card mgr-notice is-sleep' });
      card.append(
        h('div', { class: 'mgr-notice-head' }, [
          h('span', { class: 'mgr-notice-icon', html: svgIcon('i-clock', 18) }),
          h('b', { i18n: 'mgr.sleep.title' }), h('span', { class: 'grow' }),
        ]),
        h('p', { text: t('mgr.sleep.text').replace('{from}', fmtWhen(d.sleep.from)).replace('{to}', fmtWhen(d.sleep.to)) }),
        h('div', { class: 'actions' }, [awakeBtn()]),
      );
      card.querySelector('.mgr-notice-head').append(close('sleep', card));
      host.append(card);
    }
    applyI18n();
  })();
  return host;
}

Views.dashboard = async (root) => {
  const myNav = _navSeq;
  // Timers are created much further down, after several awaits. Register the
  // teardown NOW against mutable refs: navigate() already ran the previous
  // view's teardown, so assigning later would land on the next view instead
  // and leak both intervals for the rest of the session.
  const timers = { tick: 0, perf: 0, cd: 0, fleet: 0, stopped: false };
  root._teardown = () => {
    timers.stopped = true;
    if (timers.fleet) clearInterval(timers.fleet);
    if (timers.tick) clearInterval(timers.tick);
    if (timers.perf) clearInterval(timers.perf);
    if (timers.cd) clearInterval(timers.cd);
  };
  // 1s local tick for the restart countdown between 5s metric polls.
  timers.cd = setInterval(() => document.querySelectorAll('.restart-countdown').forEach(updateRestartCountdown), 1000);
  await refreshStatus();
  // Start/Stop/Restart live in the Server card below — no duplicate set in the
  // header (was confusing to have two).
  root.append(pageHeader('nav.dashboard', 'dashboard.subtitle'));
  root.append(managerNotices());
  // Open to the internet without a login: the most dangerous setting there is.
  if (State.config && State.config.exposure === 'internet' && !(State.info && State.info.auth && State.info.auth.enabled)) {
    root.append(h('div', { class: 'warning-bar' }, [
      h('span', { i18n: 'dashboard.openNoLogin' }), ' ',
      h('a', { href: '#settings/access', i18n: 'settings.tab.access' }),
    ]));
  }

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
  // The live state of this server comes first; then every server of the
  // panel at a glance, then the getting-started steps (a compact strip).
  const fleetCard = allServersCard();
  root.append(metricsHost);
  if (fleetCard) root.append(fleetCard.card);
  root.append(setupChecklistCard());

  // Ring buffers for sparklines. 60 samples @ 5s = 5 minutes of history.
  const cpuHist = [];
  const memHist = [];
  const pushHist = (buf, v) => { buf.push(v); if (buf.length > 60) buf.shift(); };

  const render = (m) => {
    metricsHost.innerHTML = '';
    const s = {
      running: m.running, pid: m.pid, port: m.port,
      // Localized ("2 h 5 min", "2 ч 5 мин"); a stopped server has no uptime.
      uptime: m.running ? fmtAge(m.uptimeSec) : '',
    };
    // No count yet (the first mods walk is still running): '—', not a false 0.
    const mods = m.mods || null;
    const disk = m.diskFreeBytes != null ? bytes(m.diskFreeBytes) : '—';
    const players = m.playerCount != null ? m.playerCount : '—';
    // RCon answered with an error while the server is up → show an honest
    // "not connected" indicator rather than a misleading 0-player count.
    const rconDown = !!(s.running && m.rconError);
    // Without RCon the Steam query still knows how many players are on.
    const q = m.query || null;
    const qPlayers = rconDown && q && q.ok && q.info ? q.info.players : null;
    // Who would a Stop / Restart throw out? RCon's count, else the query's.
    const online = qPlayers != null ? qPlayers : (Number.isFinite(players) ? players : 0);
    const recent = Array.isArray(m.recentAdm) ? m.recentAdm : [];
    const proc = m.proc || null;
    if (proc) {
      pushHist(cpuHist, proc.cpuPercent || 0);
      pushHist(memHist, proc.memBytes || 0);
    }
    if (Number.isFinite(m.serverTime)) State.clockSkew = m.serverTime - Date.now();
    const nextRestart = nextRestartNode(m);

    if (m.adopted && s.running) {
      metricsHost.append(h('div', { class: 'info-bar', text: t('dashboard.adopted').replace('{pid}', s.pid) }));
    }
    if (State.serverStatus.loopPaused) {
      metricsHost.append(h('div', { class: 'warning-bar bar-action' }, [
        h('span', { class: 'bar-text', i18n: 'status.crashLoop' }),
        h('button', { i18n: 'action.resume', onclick: async (e) => {
          await withBusy(e.currentTarget, async () => {
            try { await api.post('/api/server/clear-crash-loop'); toast(t('msg.saved'), 'ok'); await navigate('dashboard'); }
            catch (err) { handleErr(err); }
          });
        }}),
      ]));
    }

    // Status as a coloured badge, like the Servers page.
    const loading = s.running && !m.stopping && !!m.loading;
    const stateBadge = m.stopping ? h('span', { class: 'badge warn', i18n: 'status.stopping' })
      : loading ? h('span', { class: 'badge warn', i18n: 'status.loading' })
      : s.running ? h('span', { class: 'badge ok', i18n: 'status.running' })
      : h('span', { class: 'badge mute', i18n: 'status.stopped' });
    const confirmOnline = async (okKey) => online <= 0 ||
      confirmModal(t('confirm.playersOnline').replace('{n}', online), { danger: true, okText: t(okKey) });

    const serverActions = h('div', { class: 'actions dash-actions' });
    if (s.running) {
      serverActions.append(
        h('button', { class: 'secondary', i18n: 'action.restart',
          onclick: async (e) => {
            const btn = e.currentTarget;
            if (!(await confirmOnline('action.restart'))) return;
            await withBusy(btn, async () => { try { await api.post('/api/server/restart'); await navigate('dashboard'); } catch (err) { handleErr(err); } });
          }}),
        h('button', { class: 'danger', i18n: 'action.stop',
          onclick: async (e) => {
            // Capture the button BEFORE any await — e.currentTarget is only
            // valid during dispatch and turns null after it.
            const btn = e.currentTarget;
            // Players online — a mis-click kills their session; confirm.
            if (!(await confirmOnline('action.stop'))) return;
            await withBusy(btn, async () => { try { await api.post('/api/server/stop'); await navigate('dashboard'); } catch (err) { handleErr(err); } });
          }}),
      );
    } else {
      serverActions.append(h('button', { class: 'primary', i18n: 'action.start',
        onclick: (e) => withBusy(e.currentTarget, async () => { try { await api.post('/api/server/start'); await navigate('dashboard'); } catch (err) { handleErr(err); } }) }));
    }

    metricsHost.append(
      h('div', { class: 'grid-3 dash-grid' }, [
        h('div', { class: 'card dash-server' }, [
          h('div', { class: 'card-head' }, [cardTitle('i-server', 'nav.server'), stateBadge]),
          h('div', { class: 'kv' }, [
            h('div', { class: 'k', i18n: 'status.pid' }),
            h('div', { text: s.pid || '—' }),
            h('div', { class: 'k', i18n: 'status.uptime' }),
            h('div', { text: s.uptime || '—' }),
            h('div', { class: 'k', i18n: 'status.port' }),
            h('div', { text: s.port }),
            h('div', { class: 'k', i18n: 'status.players' }),
            qPlayers != null
              ? h('div', { title: t('dashboard.playersViaQuery') }, [
                  h('span', { text: `${qPlayers} / ${q.info.maxPlayers} ` }),
                  h('span', { class: 'rcon-down', i18n: m.rconConfigured === false ? 'chat.needRcon.badge' : 'dashboard.rconDown' }),
                ])
              : rconDown
              ? h('div', { class: 'rcon-down', title: m.rconError },
                  [h('span', { i18n: m.rconConfigured === false ? 'chat.needRcon.badge' : 'dashboard.rconDown' })])
              : h('div', { text: String(players) }),
            // Server FPS from the RPT log (logAverageFps + -dologs).
            ...(s.running ? [
              h('div', { class: 'k', i18n: 'status.serverFps' }),
              m.serverFps != null
                ? h('div', { class: 'fps-val ' + (m.serverFps >= 20 ? 'ok' : m.serverFps >= 10 ? 'warn' : 'bad'), text: String(m.serverFps) })
                : h('div', {}, h('button', { class: 'fps-none link-btn', type: 'button', text: '—', title: t('status.serverFps.off'),
                    'aria-label': t('status.serverFps.off'), onclick: () => focusTarget('server', '[data-key="logAverageFps"]') })),
            ] : []),
            ...(s.running && !m.stopping ? [
              h('div', { class: 'k', i18n: 'dashboard.browser', title: t('dashboard.browser.hint') }),
              q && q.ok
                ? h('div', { class: 'browser-ok', title: q.info ? `${q.info.name}
${q.info.map} · ${q.info.version || ''}` : '' },
                    [h('span', { class: 'dot-ok' }), h('span', { i18n: 'dashboard.browser.listed' })])
                : m.loading
                ? h('div', { class: 'browser-wait' }, [h('span', { class: 'dot-wait' }), h('span', { i18n: 'dashboard.browser.loading' })])
                : h('div', { class: 'browser-bad', title: (q && q.error) || '',
                    text: t('dashboard.browser.noAnswer').replace('{port}', (q && q.port) || 27016) }),
            ] : []),
            ...(nextRestart ? [
              h('div', { class: 'k', i18n: 'status.nextRestart' }),
              nextRestart,
            ] : []),
          ]),
          serverActions,
        ]),
        h('div', { class: 'card' }, [
          cardTitle('i-mods', 'dashboard.mods'),
          h('div', { class: 'kv' }, [
            h('div', { class: 'k', i18n: 'dashboard.mods.active' }),
            h('div', { text: mods ? String(mods.active) : '—' }),
            h('div', { class: 'k', i18n: 'dashboard.mods.installed' }),
            h('div', { text: mods ? String(mods.installed) : '—' }),
            h('div', { class: 'k', i18n: 'dashboard.mods.total' }),
            h('div', { text: mods ? String(mods.total) : '—' }),
          ]),
          h('div', { class: 'actions' }, [
            h('button', { class: 'secondary', i18n: 'nav.mods', onclick: () => navigate('mods') }),
          ]),
        ]),
        // Disk and the server process in one "resources" card.
        h('div', { class: 'card dash-res' }, [
          cardTitle('i-health', 'dashboard.resources'),
          h('div', { class: 'res-row' }, [
            h('div', {}, [h('div', { class: 'metric-num', text: disk }), h('div', { class: 'metric-sub', i18n: 'dashboard.disk.free' })]),
          ]),
          s.running ? h('div', { class: 'res-row' }, [
            h('div', {}, [
              h('div', { class: 'metric-num', text: proc ? (Math.round(proc.cpuPercent * 10) / 10).toFixed(1) + ' %' : '—' }),
              h('div', { class: 'metric-sub', i18n: 'dashboard.process.cpu' }),
            ]),
            sparkline(cpuHist, 100),
          ]) : null,
          s.running ? h('div', { class: 'res-row' }, [
            h('div', {}, [
              h('div', { class: 'metric-num', text: proc ? bytes(proc.memBytes) : '—' }),
              h('div', { class: 'metric-sub', i18n: 'dashboard.process.mem' }),
            ]),
            sparkline(memHist, 0),
          ]) : h('p', { class: 'hint', i18n: 'dashboard.process.idle' }),
        ].filter(Boolean)),
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
        h('div', { class: 'card-head' }, [
          cardTitle('i-admlog', 'dashboard.recentAdm'),
          h('button', { class: 'small secondary', i18n: 'nav.admlog', onclick: () => navigate('admlog') }),
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
  const perfCard = h('div', { class: 'card' }, [cardTitle('i-economy', 'perf.title')]);
  const perfRange = h('div', { class: 'chart-range' });
  const perfHost = h('div', { class: 'perf-grid' });
  perfCard.append(perfRange, perfHost);
  root.append(perfCard, quickFoldersCard());
  let perfSec = Number(localStorage.getItem('perfRange')) || 3600;
  const RANGES = [[3600, 'perf.range.1h'], [21600, 'perf.range.6h'], [86400, 'perf.range.24h']];
  function renderRangeButtons() {
    perfRange.innerHTML = '';
    for (const [sec, label] of RANGES) {
      perfRange.append(h('button', {
        class: sec === perfSec ? 'primary' : '',
        text: t(label),
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
      // Stopped the whole time: one sentence, not three empty charts.
      if (!samples.some(x => x.running)) {
        perfHost.append(h('p', { class: 'hint', i18n: 'perf.notRunning' }));
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
  if (fleetCard && !timers.stopped) timers.fleet = setInterval(fleetCard.refresh, 10000);
  await tick();
  // After tick() so State.serverStatus is fresh; the card renders only when
  // the server is down and something was actually recognised.
  if (myNav === _navSeq) await renderDiagnosis(root, true);
};

// nextRestartNode renders the dashboard's "Next restart" value from the plan
// the supervisor itself reports (m.nextRestart) — never re-derived in the
// browser, whose clock and timezone may differ from the server's. That drift
// (plus a stale server-side timer) is how players once heard "3 minutes" while
// the panel still showed 8. Returns null when nothing is scheduled at all.
function nextRestartNode(m) {
  const c = State.config || {};
  const configured = (Array.isArray(c.scheduledRestarts) && c.scheduledRestarts.length > 0) ||
    (!!c.autoRestartEnabled && (c.autoRestartSeconds || 0) > 0) ||
    (Array.isArray(c.scheduledProfiles) && c.scheduledProfiles.some(p => p.enabled));
  const plan = m.nextRestart || null;
  if (!plan && !configured) return null;
  if (!m.running) return h('div', { text: t('status.notRunning') || '—' });
  if (!plan) return h('div', { text: '—' });
  const span = h('span', { class: 'restart-countdown mono', 'data-at': String(plan.at) });
  updateRestartCountdown(span);
  const tags = [h('span', { class: 'badge mute', i18n: 'restart.reason.' + plan.reason })];
  if (plan.kicking) tags.push(h('span', { class: 'badge err', i18n: 'restart.kicking' }));
  else if (plan.active) tags.push(h('span', { class: 'badge warn', i18n: 'restart.active' }));
  if (plan.waitingEmpty) tags.push(h('span', { class: 'badge', i18n: 'restart.waitingEmpty' }));
  return h('div', { class: 'restart-next' }, [span, ...tags]);
}

// updateRestartCountdown refreshes one countdown element against the server
// clock (State.clockSkew = server time − browser time at the last poll).
function updateRestartCountdown(el) {
  const at = Number(el.dataset.at);
  if (!Number.isFinite(at)) return;
  const now = Date.now() + (State.clockSkew || 0);
  const sec = Math.max(0, Math.round((at - now) / 1000));
  const pad = (n) => String(n).padStart(2, '0');
  const hrs = Math.floor(sec / 3600), mins = Math.floor((sec % 3600) / 60), secs = sec % 60;
  // The wall-clock moment, in the viewer's own timezone (same instant).
  const when = new Date(at - (State.clockSkew || 0)).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  el.textContent = `${pad(hrs)}:${pad(mins)}:${pad(secs)} · ${when}`;
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

  // Every page in the sidebar, in sidebar order — the list used to be a
  // hand-kept subset that missed half the sections added since.
  const items = [
    ...[...document.querySelectorAll('.nav a[data-route]')].map(a => ({
      kind: 'route', label: a.textContent.trim(), route: a.dataset.route,
    })),
    { kind: 'action', label: (t('action.start') || 'Start server'),
      run: async () => { try { await api.post('/api/server/start'); toast(t('msg.starting'), 'ok'); refreshStatus(); } catch (e) { handleErr(e); } } },
    { kind: 'action', label: (t('action.stop') || 'Stop server'),
      run: async () => {
        if (!(await confirmModal(t('servers.confirmStop').replace('{name}', (Srv.current() || {}).name || 'DayZ'), { danger: true, okText: t('action.stop') }))) return;
        try { await api.post('/api/server/stop'); toast(t('msg.stopping'), 'ok'); refreshStatus(); } catch (e) { handleErr(e); }
      } },
    { kind: 'action', label: (t('action.restart') || 'Restart server'),
      run: async () => {
        if (!(await confirmModal(t('servers.confirmRestart').replace('{name}', (Srv.current() || {}).name || 'DayZ'), { okText: t('action.restart') }))) return;
        try { await api.post('/api/server/restart'); toast(t('msg.restarting'), 'ok'); refreshStatus(); } catch (e) { handleErr(e); }
      } },
    { kind: 'action', label: (t('action.theme.toggle') || 'Toggle theme'),
      run: () => { const cur = (localStorage.getItem('theme') || 'dark'); setTheme(cur === 'dark' ? 'light' : 'dark'); } },
    // Multi-server: jump to another server of the panel.
    ...(Srv.list.length > 1 ? Srv.list.filter(s => s.id !== Srv.id && !s.error).map(s => ({
      kind: 'action', label: t('servers.switch') + ': ' + s.name, run: () => Srv.switchTo(s.id),
    })) : []),
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
    // Typed text also goes to config Search and Where to find, so a class
    // name ("AKM") leads somewhere instead of "No matches".
    if (q.length >= 2) {
      filtered.push(
        { kind: 'action', label: tn('cmdp.searchConfigs', { q }), run: () => { _lastSearch = q; return navigate('search'); } },
        { kind: 'action', label: tn('cmdp.whereLoot', { q }), run: () => { _lastLoot = q; return navigate('lootwhere'); } },
      );
    }
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
  const left = h('div', { class: 'page-title' }, [h('h1', { i18n: titleKey })]);
  if (subKey) left.append(h('div', { class: 'page-sub', i18n: subKey }));
  // The section's own sidebar icon heads the page, so every page carries the
  // same visual identity as its menu entry.
  const navLabel = document.querySelector(`.nav a[data-route] span[data-i18n="${titleKey}"]`);
  const use = navLabel && navLabel.closest('a').querySelector('use');
  const iconId = use && (use.getAttribute('href') || '').replace('#', '');
  const head = [];
  if (iconId) head.push(h('span', { class: 'page-icon', 'aria-hidden': 'true', html: `<svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><use href="#${iconId}"/></svg>` }));
  head.push(left);
  const right = h('div', { class: 'page-actions' }, actions || []);
  return h('div', { class: 'page-header' }, [h('div', { class: 'page-head-main' }, head), right]);
}

// openModal renders a content-agnostic modal overlay with a close button.
// Returns { close, body } so callers can build their UI inside `body` and
// dismiss programmatically. Click on backdrop or Escape both close. The
// caller's onClose callback (if provided) runs after the DOM is removed.
function openModal({ title, onClose, wide, isDirty } = {}) {
  const body = h('div', { class: 'modal-body' });
  const closeBtn = h('button', { class: 'modal-close', text: '×',
    'aria-label': t('action.close'), onclick: () => tryClose() });
  const card = h('div', { class: 'modal-card' + (wide ? ' wide' : '') }, [
    h('div', { class: 'modal-header' }, [
      h('h3', { text: title || '' }),
      closeBtn,
    ]),
    body,
  ]);
  const overlay = h('div', { class: 'modal modal-overlay' }, card);
  overlay.addEventListener('click', (e) => { if (e.target === overlay) tryClose(); });
  // Esc closes only the top dialog (a confirm over an editor, not both).
  const onKey = (e) => {
    if (e.key !== 'Escape') return;
    const open = document.querySelectorAll('.modal-overlay');
    if (open[open.length - 1] === overlay) tryClose();
  };
  document.addEventListener('keydown', onKey);
  // A stray click outside an editor must not throw its edits away: with
  // isDirty, the backdrop / Esc / × ask first (close() from code does not).
  let asking = false;
  async function tryClose() {
    if (isDirty && isDirty()) {
      if (asking) return;
      asking = true;
      const ok = await confirmModal(t('refresh.discard'), { danger: true, okText: t('action.discard') });
      asking = false;
      if (!ok) return;
    }
    close();
  }
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

// killSourceLabel names a non-player killer the admin log writes as a class
// ("ZmbM_HermitSkinny_Beard", "Animal_UrsusArctos", "FallDamage") in words.
function killSourceLabel(src) {
  const s = String(src || '');
  if (/^(Zmb|Infected)/i.test(s)) return t('kill.src.infected');
  if (/^Animal/i.test(s)) return t('kill.src.animal');
  if (/^Fall/i.test(s)) return t('kill.src.fall');
  return s;
}
// killSourceEl shows it, keeping the raw class in the tooltip.
function killSourceEl(src) {
  const label = killSourceLabel(src);
  return h('span', { class: 'adm-meta adm-source', text: label, title: label !== src ? src : null });
}
// fmtMeters is a log distance ("167.2") with the UI language's unit.
function fmtMeters(d) { return d + ' ' + t('unit.m'); }
// admName is a player's name in a log row: a link to the profile when
// opts.keyOf(name) knows the player (opts.onChange runs after edits there).
function admName(name, opts = {}) {
  const key = name && opts.keyOf ? opts.keyOf(name) : '';
  return key
    ? h('span', { class: 'adm-player player-link', text: name, onclick: () => openPlayerProfile(key, opts.onChange) })
    : h('span', { class: 'adm-player', text: name || '—' });
}

function admEventRow(e, opts = {}) {
  const type = e.type || 'other';
  const pieces = [
    h('span', { class: 'adm-time', text: e.time || '' }),
    // Base building (placed, built, destroyed, stashes) gets its own badge.
    e.base ? h('span', { class: 'adm-type adm-base-' + e.base, text: t('profile.base.' + e.base) })
      : h('span', { class: 'adm-type adm-' + type, text: t('admlog.ev.' + type) }),
  ];
  if (type === 'kill' || type === 'hit') {
    // The log names the victim first ("Player «Victim» … killed by Player
    // «Killer»"): show it the Killfeed's way, whoever did it → victim. A
    // zombie, animal or fall is the source, not a player.
    if (e.target || e.source) {
      pieces.push(e.target ? admName(e.target, opts) : killSourceEl(e.source));
      pieces.push(h('span', { class: 'adm-arrow', text: '→' }));
    }
    if (e.player) pieces.push(admName(e.player, opts));
    const meta = [];
    if (e.weapon) meta.push(e.weapon);
    if (e.distance) meta.push(fmtMeters(e.distance));
    if (meta.length) pieces.push(h('span', { class: 'adm-meta', text: '(' + meta.join(', ') + ')' }));
  } else {
    if (e.player) pieces.push(admName(e.player, opts));
    if (type === 'chat' && e.message) pieces.push(h('span', { class: 'adm-msg', text: e.message }));
    else if (e.message) pieces.push(h('span', { class: 'adm-meta', text: e.message }));
  }
  return h('div', { class: 'adm-row' }, pieces);
}

// --------------------------------------------------------------------- server.cfg

// Views.server lives in sections.js (map, serverDZ.cfg, launch, performance).

// --------------------------------------------------------------------- mods

Views.mods = async (root) => {
  // Heavy fetch ahead — remember which navigation we are. If the user
  // switches section while it runs, we must not append into their new page.
  const myNav = _navSeq;
  const d = await api.get('/api/mods');
  root.append(pageHeader('nav.mods', 'mods.subtitle', [revealBtn({ kind: 'server' }, { label: 'folders.server' }),
    d.sourceDir ? revealBtn({ kind: 'modsource' }, { label: 'folders.modsource' }) : null].filter(Boolean)));
  const wrap = h('div');

  if (State.serverStatus.running) wrap.append(runningBanner());
  if (!d.sourceDir && d.modSource !== 'steamcmd') {
    // No mod source: server-only hosting (deliberate) or nothing configured yet.
    const serverOnly = d.modSource === 'server';
    wrap.append(h('div', { class: serverOnly ? 'info-bar' : 'warning-bar', style: { display: 'flex', gap: '10px', alignItems: 'center' } }, [
      h('span', { style: { flex: '1' }, text: t(serverOnly ? 'mods.serverOnly' : 'mods.noSource') }),
      h('button', { i18n: 'mods.chooseSource', onclick: () => navigate('settings') }),
    ]));
  }

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
  const managed = new Set(d.managed || []);
  for (const m of d.mods) {
    if (managed.has(m.name)) {
      // The panel's own server-side mod: created and connected automatically.
      const tr = h('tr', { class: 'mod-managed' }, [
        h('td', {}, [h('div', { text: m.name, style: { fontWeight: '600' } }), h('div', { class: 'hint', i18n: 'mods.managed.hint' })]),
        h('td', {}, h('span', { class: 'badge info', i18n: 'mods.managed' })),
        h('td', {}, h('span', { class: 'badge mute', text: '—' })),
        h('td', { text: m.keyCount }),
        h('td', { text: bytes(m.sizeBytes) }),
        h('td', {}, h('span', { class: 'badge mute', text: '—' })),
        h('td', {}, h('span', { class: 'badge ok', text: '✓', title: t('mods.managed.hint') })),
        h('td', {}, h('button', { type: 'button', class: 'secondary small', i18n: 'nav.vehicles', onclick: () => navigate('vehicles') })),
      ]);
      tr.dataset.search = m.name.toLowerCase();
      tr.dataset.state = 'server';
      tbody.append(tr);
      continue;
    }
    // Launch toggle. A mod that isn't installed in the server dir can't be
    // enabled — DayZServer would fail to start — so we lock it with a padlock
    // and a hint to install it first (the backend rejects it too).
    let activeControl;
    if (m.installedInServer) {
      const activeCb = h('input', { type: 'checkbox', class: 'switch' });
      activeCb.checked = (d.activeMods || []).includes(m.name);
      // Mod toggles change the launch args, so they only apply on the next
      // start — lock them while the server is running (the backend refuses too).
      if (State.serverStatus.running) { activeCb.disabled = true; activeCb.title = t('mods.toggleRunning'); }
      activeCb.onchange = async () => {
        const prev = !activeCb.checked; // value before this toggle
        activeCb.disabled = true;
        try {
          const r = await api.post('/api/mods/enable', { mod: m.name, enabled: activeCb.checked });
          // Keep State.config in sync so a later config POST (language switch,
          // Settings save) can never send a stale mod list back (hotfix).
          if (r) { State.config.mods = r.active; State.config.serverMods = r.server; syncRowSwitches(); }
          reportModVehicles(r);
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
      if (State.serverStatus.running) { srvCb.disabled = true; srvCb.title = t('mods.toggleRunning'); }
      srvCb.onchange = async () => {
        if (srvCb.checked && !(await confirmModal(t('mods.serverMod.confirm'), { danger: true, okText: t('action.save') }))) { srvCb.checked = false; return; }
        const prev = !srvCb.checked;
        srvCb.disabled = true;
        try {
          const r = await api.post('/api/mods/enable', { mod: m.name, enabled: srvCb.checked, serverSide: true });
          if (r) { State.config.mods = r.active; State.config.serverMods = r.server; syncRowSwitches(); }
          reportModVehicles(r);
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
        onclick: (e) => withBusy(e.currentTarget, async () => { try { const r = await api.post('/api/mods/install', { mod: m.name }); toast(t('msg.installed'),'ok'); await navigate('mods'); reportModMissions(r); } catch (err) { handleErr(err); } })}));
    }
    if (m.installedInServer && m.availableInWorkshop) {
      actions.append(h('button', {
        class: m.updateAvailable ? 'primary' : '',
        i18n: 'action.update',
        onclick: (e) => withBusy(e.currentTarget, async () => {
          try { const r = await api.post('/api/mods/update', { mod: m.name }); toast(t('msg.updated'),'ok'); await navigate('mods'); reportModMissions(r); }
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
      actions.append(revealBtn({ kind: 'mod', mod: m.name }, { iconOnly: true }));
      // Uninstall also offers to remove a map mod's mission from mpmissions.
      actions.append(h('button', { class: 'danger', i18n: 'action.uninstall',
        onclick: (ev) => uninstallModFlow(m.name, ev.currentTarget) }));
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
    tr.dataset.search = [m.name, m.displayName || '', m.publishedId || ''].join(' ').toLowerCase();
    tr.dataset.state = [
      (d.activeMods || []).includes(m.name) ? 'on' : '',
      (d.serverMods || []).includes(m.name) ? 'server' : '',
      m.updateAvailable ? 'update' : '',
      m.installedInServer ? '' : 'notinstalled',
    ].filter(Boolean).join(' ');
    tr._switches = { mod: m.name, active: activeControl, server: serverControl };
    tbody.append(tr);
  }
  // After a toggle the server may have moved the mod from one list to the
  // other (a mod is either for everyone or server-only): mirror that.
  function syncRowSwitches() {
    for (const tr of tbody.querySelectorAll('tr')) {
      const sw = tr._switches;
      if (!sw) continue;
      if (sw.active && sw.active.type === 'checkbox') sw.active.checked = (State.config.mods || []).includes(sw.mod);
      if (sw.server && sw.server.type === 'checkbox') sw.server.checked = (State.config.serverMods || []).includes(sw.mod);
    }
  }
  // Empty state: a fresh setup with zero mods otherwise renders a bare header
  // row with no explanation of what to do next.
  if (!d.mods.length) {
    tbl.classList.add('is-empty'); // column heads over nothing read as a broken table
    tbody.append(h('tr', {}, h('td', { colspan: '8' },
      h('div', { class: 'empty-state' }, [
        h('div', { class: 'es-title', i18n: 'mods.empty.title' }),
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
        label = t('mods.steam.outdated') + ` (~${days} ${t('unit.days')})`;
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

  const wsSource = d.modSource === 'steamcmd';
  const wsCard = wsSource ? workshopDownloadCard() : null;
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
          else toast(out > 0 ? tn('mods.checkSteam.outdatedN', { n: out }) : t('mods.checkSteam.allOk'), out > 0 ? 'warn' : 'ok');
        } catch (err) { handleErr(err); }
        finally { btn.disabled = false; btn.textContent = original; }
      }
    }),
    wsSource
      // Steam Workshop source: download what is newer on the Workshop with
      // SteamCMD and install it (progress in the Direct download card).
      ? h('button', { class: 'primary', i18n: 'mods.syncAll', title: t('mods.wsSource.sync.hint'),
        disabled: !!State.serverStatus.running,
        onclick: () => { wsCard._sync(); wsCard.scrollIntoView({ behavior: 'smooth', block: 'start' }); } })
      : h('button', { class: 'primary', i18n: 'mods.syncAll',
        onclick: () => openSyncAllPicker(d),
      }),
  ]);
  if (outdatedCount > 0) {
    toolbar.append(h('button', {
      class: 'primary',
      onclick: async () => {
        try {
          const r = await api.post('/api/mods/update-all');
          toast(t('msg.modsUpdated').replace('{n}', r.count || 0), 'ok');
          await navigate('mods');
          reportModMissions(r);
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
              h('div', { text: t('mods.collection.summary').replace('{id}', r.collectionId).replace('{ok}', ok.length).replace('{missing}', missing.length) }),
            );
            if (ok.length) {
              const list = h('ul');
              for (const m of ok) list.append(h('li', { text: `${m.modName}${m.displayName ? ' — ' + m.displayName : ''} (id ${m.publishedId})` }));
              collectionResults.append(list);
              // Found in the mod source is not installed on the server: copy
              // the missing ones over first, then switch every one on — and
              // report what really happened (it used to say "enabled" when
              // nothing could be).
              const activate = h('button', { class: 'primary', style: { marginTop: '8px' }, text: t('mods.collection.activate'),
                disabled: !!State.serverStatus.running, title: State.serverStatus.running ? t('mods.toggleRunning') : '',
                onclick: (e) => withBusy(e.currentTarget, async () => {
                  const installed = new Set((d.mods || []).filter(x => x.installedInServer).map(x => x.name));
                  const toInstall = ok.map(m => m.modName).filter(n => !installed.has(n));
                  let synced = null;
                  if (toInstall.length) {
                    try { synced = await api.post('/api/mods/sync-all', { only: toInstall }); }
                    catch (err) { handleErr(err); return; }
                  }
                  const vehicles = [], failed = [];
                  let enabled = 0;
                  for (const m of ok) {
                    try {
                      const r = await api.post('/api/mods/enable', { mod: m.modName, enabled: true });
                      enabled++;
                      vehicles.push(...((r && r.vehicles) || []));
                    } catch (err) { failed.push(m.modName + ' — ' + (err.message || err)); }
                  }
                  reportModVehicles({ vehicles });
                  if (synced) reportModMissions(synced);
                  if (failed.length) toast(tn('mods.collection.partial', { done: enabled, total: ok.length }) + ': ' + failed.slice(0, 3).join('; '), 'warn');
                  else toast(tn('mods.collection.enabledN', { n: enabled }), 'ok');
                  await navigate('mods');
                })
              });
              collectionResults.append(activate);
            }
            if (missing.length) {
              const dl = h('div', { style: { marginTop: '8px' } });
              dl.append(h('div', { text: t('mods.collection.missing') }));
              for (const id of missing) {
                dl.append(h('div', {}, h('a', {
                  href: `https://steamcommunity.com/sharedfiles/filedetails/?id=${id}`,
                  target: '_blank', rel: 'noopener', text: t('mods.collection.subscribe').replace('{id}', id),
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
  // Find a mod among dozens: a search box and state chips with counts.
  const modSearch = h('input', { type: 'search', placeholder: t('mods.search.ph'), 'aria-label': t('mods.search.ph') });
  const modChips = h('div', { class: 'pills mods-chips' });
  let modFilter = 'all';
  const MOD_FILTERS = [['all', ''], ['on', 'on'], ['update', 'update'], ['notinstalled', 'notinstalled'], ['server', 'server']];
  const applyModFilter = () => {
    const q = modSearch.value.trim().toLowerCase();
    const want = (MOD_FILTERS.find(f => f[0] === modFilter) || MOD_FILTERS[0])[1];
    for (const tr of tbody.querySelectorAll('tr[data-search]')) {
      const okState = !want || (' ' + tr.dataset.state + ' ').includes(' ' + want + ' ');
      tr.hidden = !(okState && (!q || tr.dataset.search.includes(q)));
    }
  };
  const renderModChips = () => {
    modChips.innerHTML = '';
    const rows = [...tbody.querySelectorAll('tr[data-search]')];
    for (const [key, state] of MOD_FILTERS) {
      const n = state ? rows.filter(tr => (' ' + tr.dataset.state + ' ').includes(' ' + state + ' ')).length : rows.length;
      if (!n && key !== 'all') continue;
      modChips.append(h('button', { type: 'button', class: 'pill' + (modFilter === key ? ' active' : ''),
        onclick: () => { modFilter = key; renderModChips(); applyModFilter(); } },
      [h('span', { text: t('mods.filter.' + key) }), h('span', { class: 'pill-count', text: String(n) })]));
    }
  };
  modSearch.oninput = applyModFilter;
  renderModChips();
  wrap.append(pendingModMissionsBanner());
  // Steam Workshop source: downloading is how mods arrive, so its card comes
  // first; collections are pasted there too.
  if (wsCard) wrap.append(wsCard);
  wrap.append(h('div', { class: 'card' }, [
    toolbar,
    d.mods.length > 5 ? h('div', { class: 'players-toolbar mods-find' }, [modSearch, modChips]) : null,
    h('div', { class: 'table-scroll-narrow' }, tbl),
  ].filter(Boolean)));
  if (!wsCard) {
    wrap.append(collectionCard);
    wrap.append(h('div', { class: 'info-bar mods-ws-tip' }, [
      h('span', { class: 'grow', i18n: 'mods.wsSource.tip' }),
      h('button', { type: 'button', class: 'secondary small', i18n: 'mods.chooseSource', onclick: () => navigate('settings') }),
    ]));
  }

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

// Types filter chips. Categories are DayZ's own <category> names; ammo and
// magazines are split out by class-name prefix (see typeKind in Views.types).
// lootdispatch is the loot events hand out (heli crashes, police cars) — its
// own chip, as on the Economy page, not lumped into "Other".
const TYPE_KINDS = ['weapons', 'tools', 'food', 'clothes', 'containers', 'explosives', 'books', 'lootdispatch'];
const TYPE_KIND_ORDER = ['weapons', 'ammo', 'mags', 'clothes', 'food', 'tools', 'containers', 'explosives', 'books', 'lootdispatch', 'other'];
// The icon each kind of item gets in the item tables (Types, Loot economy).
const TYPE_KIND_ICON = { weapons: 'i-s-shoulder', ammo: 'i-c-ammo', mags: 'i-c-mag', clothes: 'i-s-top', food: 'i-c-food', tools: 'i-c-tool',
  containers: 'i-c-box', explosives: 'i-c-boom', books: 'i-guide', lootdispatch: 'i-events', other: 'i-c-other' };
// typeKindOf: an item's kind from its class name and <category>.
function typeKindOf(name, category) {
  const n = String(name || '').toLowerCase();
  if (n.startsWith('ammo_') || n.startsWith('ammobox')) return 'ammo';
  if (n.startsWith('mag_')) return 'mags';
  const c = String(category || '').toLowerCase();
  return TYPE_KINDS.includes(c) ? c : 'other';
}
// Attachments entries are weapons, clothing, containers, infected, animals
// or vehicles: their icon and colour.
const ATTACH_KIND_ICON = { weapon: 'i-s-shoulder', clothing: 'i-s-top', container: 'i-c-box', infected: 'i-skull', animal: 'i-paw', vehicle: 'i-vehicles', other: 'i-c-other' };
const ATTACH_KIND_TK = { weapon: 'weapons', clothing: 'clothes', container: 'containers', infected: 'infected', animal: 'animal', vehicle: 'vehicle', other: 'other' };
const typeKindIcon = (kind, size = 15) => h('span', { class: 'tk-ico tk-' + kind, 'aria-hidden': 'true', html: svgIcon(TYPE_KIND_ICON[kind] || 'i-c-other', size) });

// _typesTarget is a pending filter ({file, q, usage, tier, category}) the Types
// view applies on its next mount — set by the Economy charts and Where-to-find
// before navigating there. category '' means "no category".
let _typesTarget = null;

// typeCategoryLabel shows a DayZ <category> name in the UI language when the
// panel knows it (weapons → Оружие); a modded category keeps its own name.
function typeCategoryLabel(name) {
  if (!name) return t('economy.cat.none');
  const k = 'types.kind.' + String(name).toLowerCase();
  return State.i18n[k] ? t(k) : name;
}

// The last query of config Search and of Where to find. A page opened from a
// hit and then left with Back comes back with its query and results, and the
// Ctrl+K palette hands a typed word to either page through these.
let _lastSearch = '';
let _lastLoot = '';

// _pageQuery pre-fills the search box of the next page opened ({route, q}) —
// "show this item over there" links from Where to find. takePageQuery reads
// it once.
let _pageQuery = null;
function takePageQuery(route) {
  if (!_pageQuery || _pageQuery.route !== route) return '';
  const q = _pageQuery.q || '';
  _pageQuery = null;
  return q;
}

Views.types = async (root) => {
  // Heavy fetch ahead — remember which navigation we are. If the user
  // switches section while it runs, we must not append into their new page.
  const myNav = _navSeq;
  // A pending deep link (an Economy chart row, Where to find) is taken at once,
  // so no later visit inherits it.
  const target = _typesTarget; _typesTarget = null;
  const fileSelect = h('select', { 'aria-label': t('economy.file') });
  fileSelect.append(h('option', { value: '', text: t('types.baseFile') }));

  const search = h('input', { type: 'search', placeholder: t('types.search'), 'aria-label': t('types.search') });
  if (target && target.q) search.value = target.q;
  // Until the first fetch lands the table area says so: it used to sit empty
  // under blank filters with no sign that anything was loading.
  const tableWrap = h('div', { class: 'table-scroll-narrow' }, [
    h('p', { class: 'hint', i18n: 'msg.loading' }),
    ...[0, 1, 2, 3, 4, 5].map(() => h('div', { class: 'skeleton types-skel' })),
  ]);
  const pagerHost = h('div');
  const catList = h('datalist', { id: 'dz-types-cats' });
  let typesFilePath = '';

  // Filters (v1.0.0): kind chips + usage / tier / spawning-only. The list is
  // fetched once per file and filtered here — it used to re-download the whole
  // types file on every keystroke, sort click and page turn.
  let cache = null;          // { file, d }
  let kindFilter = 'all';
  // Category comes only from a deep link ('' = no category, null = any): the
  // chips split weapons into ammo and magazines, an Economy bar does not.
  let catFilter = target && typeof target.category === 'string' ? target.category.toLowerCase() : null;
  const usageSel = h('select', { class: 'types-filter', 'aria-label': t('types.filter.usage') },
    h('option', { value: '', i18n: 'types.filter.anyUsage' }));
  const tierSel = h('select', { class: 'types-filter', 'aria-label': t('types.filter.tier') },
    h('option', { value: '', i18n: 'types.filter.anyTier' }));
  // A deep-linked value is added now, so fillFilterOptions finds it selected.
  if (target && target.usage) { usageSel.append(h('option', { value: target.usage, text: target.usage })); usageSel.value = target.usage; }
  if (target && target.tier) { tierSel.append(h('option', { value: target.tier, text: target.tier })); tierSel.value = target.tier; }
  const spawnOnly = h('input', { type: 'checkbox', class: 'switch' });
  const kindBar = h('div', { class: 'kind-chips type-kinds' });
  const catHost = h('span', { class: 'types-catfilter' });

  // Pagination. Search resets to page 1. A phone gets shorter pages: 200 rows
  // made a 12,000px page with the pager at the very end.
  const PAGE_SIZE = window.matchMedia && window.matchMedia('(max-width: 600px)').matches ? 50 : 200;
  let currentPage = 1;
  let lastFiltered = [];  // every row matching the filters, for "select all matching"
  let sortKey = 'name';   // click a column header to sort (item #8)
  let sortDir = 1;        // 1 = asc, -1 = desc
  let syncHead = () => {};

  // Inline editing (item #8): unsaved edits live here (name -> {field: value})
  // and survive sort/search/page changes until saved or discarded.
  const dirty = {};
  const dirtyCount = () => Object.keys(dirty).length;
  function clearDirty() { for (const k of Object.keys(dirty)) delete dirty[k]; }

  // Ticked rows, by name. They survive sorting, searching, filtering and
  // paging — the checkboxes are redrawn on every refresh, this set is not.
  const selected = new Set();

  // Bottom dock: the selection actions and the unsaved-changes bar stay in
  // view while scrolling a long table (Save used to be only at the top).
  const selText = h('span');
  const selAllBtn = h('button', { class: 'secondary small', onclick: () => {
    for (const r of lastFiltered) selected.add(r.name);
    syncChecks(); updateSelUI();
  } });
  let presetsList = [];
  const presetSel = h('select', { class: 'types-preset-sel', 'aria-label': t('types.presets') },
    h('option', { value: '', i18n: 'types.presets.apply' }));
  presetSel.title = t('types.presets.hint');
  presetSel.onchange = () => applyPreset();
  const selBar = h('div', { class: 'sticky-save types-selbar', hidden: true }, [
    selText, selAllBtn,
    h('button', { class: 'secondary small', i18n: 'types.sel.clear', onclick: () => { selected.clear(); syncChecks(); updateSelUI(); } }),
    h('span', { class: 'grow' }),
    hlp(h('button', { class: 'small', i18n: 'types.bulk', onclick: () => openBulkEdit() }), 'types.bulk'),
    presetSel,
  ]);
  const saveText = h('span');
  const saveBtn = h('button', { class: 'primary small', i18n: 'action.save', title: 'Ctrl+S', onclick: (e) => saveDirty(e.currentTarget) });
  const saveBar = h('div', { class: 'sticky-save', hidden: true }, [
    saveText, h('span', { class: 'grow' }),
    h('button', { class: 'secondary small', i18n: 'action.discard', onclick: () => { clearDirty(); updateDirtyUI(); refreshTable(); } }),
    saveBtn,
  ]);
  const dock = h('div', { class: 'types-dock', hidden: true }, [selBar, saveBar]);
  const updateDock = () => { dock.hidden = selBar.hidden && saveBar.hidden; };
  function updateDirtyUI() {
    const n = dirtyCount();
    saveBar.hidden = n === 0;
    saveText.textContent = tn('server.cfg.unsaved', { n });
    updateDock();
  }
  function updateSelUI() {
    const n = selected.size;
    selBar.hidden = n === 0;
    selText.textContent = tn('types.sel.count', { n });
    selAllBtn.hidden = !lastFiltered.some(r => !selected.has(r.name));
    selAllBtn.textContent = tn('types.sel.allMatching', { n: lastFiltered.length });
    updateDock();
  }
  function syncChecks() {
    tableWrap.querySelectorAll('input[type=checkbox][data-name]').forEach(cb => { cb.checked = selected.has(cb.dataset.name); });
    syncHead();
  }

  async function saveDirty(btn) {
    const rows = [], bad = [];
    for (const [name, dd] of Object.entries(dirty)) {
      const patch = {};
      for (const f of ['nominal', 'min', 'lifetime']) {
        if (!(f in dd)) continue;
        const v = String(dd[f]).trim();
        if (!/^\d+$/.test(v)) { bad.push(name); continue; }
        patch[f] = parseInt(v, 10);
      }
      if ('category' in dd) patch.category = String(dd.category).trim();
      if (Object.keys(patch).length) rows.push({ name, patch });
    }
    // A blank or broken number used to be skipped without a word — the cell
    // looked saved but kept its old value.
    if (bad.length) { toast(tn('types.badNumbers', { names: [...new Set(bad)].slice(0, 5).join(', ') }), 'error'); return; }
    if (!rows.length) { clearDirty(); updateDirtyUI(); return; }
    await withBusy(btn, async () => {
      try {
        // One request: the file is written once and backed up once, so the copy
        // of it as it was before this save stays restorable.
        await api.post('/api/types/patch-many', { file: fileSelect.value, rows });
        clearDirty();
        updateDirtyUI();
        toast(t('msg.saved'), 'ok');
        await refreshTable(true);
      } catch (e) { handleErr(e); }
    });
  }

  async function applyPreset() {
    const p = presetsList.find(x => x.id === presetSel.value);
    presetSel.value = '';
    if (!p) return;
    const names = [...selected];
    if (!names.length) { toast(t('msg.selectFirst')); return; }
    const label = State.lang === 'ru' ? p.labelRu : p.label;
    if (!(await confirmModal(tn('types.presets.confirm', { name: label, n: names.length })))) return;
    try {
      await api.post('/api/types/apply-preset', { file: fileSelect.value, names, presetId: p.id });
      toast(t('msg.saved'), 'ok');
      await refreshTable(true);
    } catch (e) { handleErr(e); }
  }

  // The scalar fields, shared by the bulk and the full editor: a localized
  // label with the types.xml tag as small code — the bare tags read as English
  // in any UI language.
  const FIELDS = [
    { k: 'nominal', label: 'types.field.nominal', help: 'types.nominal' },
    { k: 'min', label: 'types.field.min', help: 'types.min' },
    { k: 'lifetime', label: 'types.field.lifetime', help: 'types.lifetime' },
    { k: 'restock', label: 'types.field.restock', help: 'types.restock' },
    { k: 'quantmin', label: 'types.field.quantmin', help: 'types.quant' },
    { k: 'quantmax', label: 'types.field.quantmax', help: 'types.quant' },
    { k: 'cost', label: 'types.field.cost', help: 'types.cost' },
    { k: 'category', label: 'types.field.category', help: 'types.category', text: true },
  ];
  const fieldInput = (f, value) => f.text
    ? h('input', { type: 'text', value: value ?? '', list: 'dz-types-cats', spellcheck: 'false' })
    : h('input', { type: 'number', value: value ?? '', step: '1' });
  const fieldBox = (f, el) => h('div', { class: 'types-field' }, [
    h('div', { class: 'cfg-label' }, [
      withHelp(h('label', { for: assocId(el), i18n: f.label }), f.help),
      h('code', { class: 'cfg-key', text: f.k }),
    ]),
    el,
  ]);
  // Reads the fields into a patch; returns null (after a toast) when a number
  // is not a whole number.
  function readFields(inputs, toPatch) {
    for (const f of FIELDS) {
      const v = inputs[f.k].value.trim();
      if (v === '') continue;
      if (f.text) { toPatch(f.k, v); continue; }
      if (!/^-?\d+$/.test(v)) { toast(tn('types.badNumber', { field: t(f.label) }), 'error'); inputs[f.k].focus(); return false; }
      toPatch(f.k, parseInt(v, 10));
    }
    return true;
  }

  // Bulk-edit modal — opened from the selection bar. Scalar fields only; the
  // full editor handles usages/values/tags.
  function openBulkEdit() {
    const names = [...selected];
    if (!names.length) { toast(t('msg.selectFirst')); return; }
    const inputs = {};
    const grid = h('div', { class: 'grid-3 types-fields' });
    for (const f of FIELDS) { inputs[f.k] = fieldInput(f, ''); grid.append(fieldBox(f, inputs[f.k])); }
    let edited = false;
    const m = openModal({ title: tn('types.bulk.title', { n: names.length }), wide: true, isDirty: () => edited });
    m.body.addEventListener('input', () => { edited = true; });
    m.body.append(
      h('p', { class: 'hint', i18n: 'types.bulk.hint' }),
      grid,
      h('div', { class: 'actions' }, [
        h('button', { class: 'primary', i18n: 'types.bulk.apply', onclick: (e) => withBusy(e.currentTarget, async () => {
          const patch = {};
          if (!readFields(inputs, (k, v) => { patch[k] = v; })) return;
          if (Object.keys(patch).length === 0) { toast(t('msg.setOneField')); return; }
          try {
            const r = await api.post('/api/types/bulk-patch', { file: fileSelect.value, names, patch });
            toast(tn('msg.patched', { n: r.touched || 0 }), 'ok');
            m.close();
            await refreshTable(true);
          } catch (err) { handleErr(err); }
        }) }),
        h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
      ]),
    );
    applyI18n();
  }

  async function refreshTable(refetch) {
    let d;
    if (!cache || refetch || cache.file !== fileSelect.value) {
      try { d = await api.get(`/api/types?file=${encodeURIComponent(fileSelect.value)}`); }
      catch (e) {
        if (myNav !== _navSeq) return;
        tableWrap.innerHTML = ''; pagerHost.innerHTML = '';
        tableWrap.append(h('div', { class: 'warning-bar err-bar', text: e.message }));
        return;
      }
      if (myNav !== _navSeq) return;
      cache = { file: fileSelect.value, d };
      typesFilePath = d.file || '';
      fillFilterOptions(d.types || []);
    }
    d = cache.d;
    tableWrap.innerHTML = '';
    pagerHost.innerHTML = '';

    const q = search.value.trim().toLowerCase();
    const us = usageSel.value, tier = tierSel.value, onlySpawn = spawnOnly.checked;
    const base = (d.types || []).filter(r =>
      (!q || r.name.toLowerCase().includes(q)) &&
      (!us || (r.usages || []).includes(us)) &&
      (!tier || (r.values || []).includes(tier)) &&
      (catFilter === null || (r.category || '').toLowerCase() === catFilter) &&
      (!onlySpawn || (r.nominal || 0) > 0));
    renderKindBar(base);
    renderCatFilter();
    const filtered = kindFilter === 'all' ? base : base.filter(r => typeKind(r) === kindFilter);
    lastFiltered = filtered;

    if (!filtered.length) {
      syncHead = () => {};
      const fileEmpty = !(d.types || []).length;
      tableWrap.append(h('div', { class: 'empty-state' }, [
        h('div', { class: 'es-title', i18n: fileEmpty ? 'types.emptyFile' : 'filter.none' }),
        fileEmpty ? null : h('div', { class: 'actions' }, [h('button', { class: 'secondary', i18n: 'filter.reset', onclick: resetFilters })]),
      ].filter(Boolean)));
      updateSelUI();
      applyI18n();
      return;
    }

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
    const pageNames = slice.map(r => r.name);

    const tbl = h('table', { class: 'types-table' });
    const headCb = h('input', { type: 'checkbox', title: t('types.selectPage'), 'aria-label': t('types.selectPage') });
    syncHead = () => {
      const n = pageNames.filter(x => selected.has(x)).length;
      headCb.checked = n > 0 && n === pageNames.length;
      headCb.indeterminate = n > 0 && n < pageNames.length;
    };
    headCb.onchange = () => {
      for (const nm of pageNames) { if (headCb.checked) selected.add(nm); else selected.delete(nm); }
      syncChecks(); updateSelUI();
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
      const cb = h('input', { type: 'checkbox', 'aria-label': t('col.select') + ' ' + row.name });
      cb.dataset.name = row.name;
      cb.checked = selected.has(row.name);
      cb.onchange = () => {
        if (cb.checked) selected.add(row.name); else selected.delete(row.name);
        syncHead(); updateSelUI();
      };
      let tr;
      const mkCell = (field, numeric) => {
        const has = dirty[row.name] && (field in dirty[row.name]);
        const inp = h('input', {
          type: numeric ? 'number' : 'text', class: 'inline-cell',
          value: has ? dirty[row.name][field] : (row[field] ?? ''),
          'aria-label': row.name + ' ' + field,
          list: numeric ? undefined : 'dz-types-cats',
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
        if (field === 'lifetime') {
          // The seconds in words beside them: 14400 → 4 h.
          const words = h('span', { class: 'types-life-h', text: fmtLifetime(inp.value) });
          inp.addEventListener('input', () => { words.textContent = fmtLifetime(inp.value); });
          return h('td', { class: 'num' }, h('div', { class: 'types-life' }, [inp, words]));
        }
        if (field === 'category') {
          // Its colour, as in the chips above.
          return h('td', {}, h('div', { class: 'types-cat tk-' + typeKind(row) }, [h('span', { class: 'types-cat-dot', 'aria-hidden': 'true' }), inp]));
        }
        return h('td', { class: numeric ? 'num' : '' }, inp);
      };
      // Where it spawns and at which tier, in small print under the name.
      const sub = [(row.usages || []).join(', '), (row.values || []).join(' ')].filter(Boolean).join(' · ');
      tr = h('tr', { class: (Number(row.nominal) || 0) === 0 ? 'zero' : '' }, [
        h('td', {}, cb),
        // The name opens the full editor too — on a phone it is the only way
        // in, the Edit column does not fit there.
        h('td', {}, h('div', { class: 'types-namecell' }, [
          typeKindIcon(typeKind(row)),
          h('div', { class: 'types-namecol' }, [
            h('button', { type: 'button', class: 'types-name-btn', text: row.name,
              title: t('types.openEditor'), onclick: () => openEditor(row.name) }),
            sub ? h('span', { class: 'types-sub', text: sub, title: sub }) : null,
          ].filter(Boolean)),
        ])),
        mkCell('nominal', true),
        mkCell('min', true),
        mkCell('lifetime', true),
        mkCell('category', false),
        h('td', {}, h('button', { type: 'button', class: 'icon-btn types-edit', title: t('action.edit'), 'aria-label': t('action.edit') + ' ' + row.name,
          html: svgIcon('i-edit', 15), onclick: () => openEditor(row.name) })),
      ]);
      if (dirty[row.name]) tr.classList.add('dirty');
      tbody.append(tr);
    }
    tbl.append(tbody);
    tableWrap.append(tbl);
    syncHead();
    updateSelUI();

    // Pager: prev / page X of Y / next + jump-to.
    const prev = h('button', { text: '←', onclick: () => { currentPage--; refreshTable(); } });
    const next = h('button', { text: '→', onclick: () => { currentPage++; refreshTable(); } });
    if (currentPage <= 1) prev.disabled = true;
    if (currentPage >= totalPages) next.disabled = true;
    const jump = h('input', { type: 'number', value: currentPage, min: 1, max: totalPages, 'aria-label': t('pager.jump') });
    jump.onchange = () => {
      const n = parseInt(jump.value, 10);
      if (Number.isFinite(n) && n >= 1 && n <= totalPages) { currentPage = n; refreshTable(); }
    };
    pagerHost.append(h('div', { class: 'pager' }, [
      prev, next,
      h('span', { class: 'pager-num',
        text: `${start + 1}–${Math.min(start + PAGE_SIZE, filtered.length)} / ${filtered.length}${filtered.length !== d.count ? ` (${t('types.filter.of')} ${d.count})` : ''}` }),
      h('span', { class: 'spacer' }),
      h('span', { class: 'hint', text: t('pager.jump') || 'Page' }),
      jump,
      h('span', { class: 'hint', text: `/ ${totalPages}` }),
    ]));
    applyI18n();
  }

  async function openEditor(name) {
    // NOTE: never name a local `t` — that shadows the global i18n t() for the
    // whole function scope and every t('key') call inside throws
    // "t is not a function" (this broke Save/Delete in this very editor).
    let item;
    try {
      item = await api.get(`/api/types/item?file=${encodeURIComponent(fileSelect.value)}&name=${encodeURIComponent(name)}`);
    } catch (e) { handleErr(e); return; }
    if (myNav !== _navSeq) return;
    // Esc, a backdrop click or × ask before throwing edits away.
    let edited = false;
    const markEdited = () => { edited = true; };
    const m = openModal({ title: name, wide: true, isDirty: () => edited });
    m.body.addEventListener('input', markEdited);
    m.body.addEventListener('change', markEdited);

    const fields = {};
    const grid = h('div', { class: 'grid-3 types-fields' });
    for (const f of FIELDS) {
      fields[f.k] = fieldInput(f, f.k === 'category' ? (item.category?.name || '') : item[f.k]);
      grid.append(fieldBox(f, fields[f.k]));
    }

    // Usages / Values / Tags as editable pill lists. Text typed but not yet
    // added with Enter is added on Save, so it is never dropped silently.
    const pendingAdds = [];
    function editableList(labelKey, helpKey, items) {
      const wrap = h('div', { class: 'pills' });
      const inp = h('input', { type: 'text', class: 'types-list-inp', placeholder: t('types.list.add'), spellcheck: 'false' });
      const add = () => {
        const v = inp.value.trim();
        if (!v) return false;
        if (!items.some(x => x.name.toLowerCase() === v.toLowerCase())) items.push({ name: v });
        inp.value = '';
        return true;
      };
      const render = () => {
        wrap.innerHTML = '';
        items.forEach((it, i) => {
          wrap.append(h('span', { class: 'pill' }, [it.name,
            h('button', { type: 'button', text: '×', 'aria-label': t('action.delete') + ' ' + it.name,
              onclick: () => { items.splice(i, 1); markEdited(); render(); } })]));
        });
        wrap.append(inp);
      };
      inp.onkeydown = (e) => {
        if (e.key !== 'Enter') return;
        e.preventDefault();
        if (add()) { markEdited(); render(); inp.focus(); }
      };
      pendingAdds.push(add);
      render();
      return h('div', {}, [withHelp(h('label', { for: assocId(inp), i18n: labelKey }), helpKey), wrap]);
    }

    const usages = item.usages || [];
    const values = item.values || [];
    const tags = item.tags || [];
    const lists = h('div', { class: 'grid-3' }, [
      editableList('types.filter.usage', 'types.usage', usages),
      editableList('types.filter.tier', 'types.value', values),
      editableList('attach.tags', 'types.tag', tags),
    ]);

    // Flags are 0/1 in the file — switches, labelled, with the XML name.
    const FLAGS = [['countInCargo', 'count_in_cargo'], ['countInHoarder', 'count_in_hoarder'], ['countInMap', 'count_in_map'],
      ['countInPlayer', 'count_in_player'], ['crafted', 'crafted'], ['deloot', 'deloot']];
    const flags = item.flags || {};
    const flagInputs = {};
    const flagGrid = h('div', { class: 'cfg-bools' });
    for (const [key, xml] of FLAGS) {
      const sw = h('input', { type: 'checkbox', class: 'switch' });
      sw.checked = Number(flags[key]) > 0;
      flagInputs[key] = sw;
      flagGrid.append(h('label', { class: 'cfg-bool' }, [sw, h('span', { class: 'cfg-bool-text' }, [
        h('span', { class: 'cfg-bool-label', i18n: 'types.flag.' + key }),
        h('code', { class: 'cfg-key', text: xml }),
      ])]));
    }

    m.body.append(
      grid,
      lists,
      h('h4', { class: 'sub-title' }, withHelp(h('span', { i18n: 'types.flags.title' }), 'types.flags')),
      flagGrid,
      h('div', { class: 'actions' }, [
        h('button', { class: 'primary', i18n: 'action.save', onclick: (e) => withBusy(e.currentTarget, async () => {
          pendingAdds.forEach(add => add());
          const body = { name };
          if (!readFields(fields, (k, v) => { body[k] = k === 'category' ? { name: v } : v; })) return;
          body.usages = usages;
          body.values = values;
          body.tags = tags;
          body.flags = Object.fromEntries(Object.entries(flagInputs).map(([k, el]) => [k, el.checked ? 1 : 0]));
          try {
            await api.post(`/api/types/item?file=${encodeURIComponent(fileSelect.value)}&name=${encodeURIComponent(name)}`, body);
            toast(t('msg.saved'), 'ok');
            m.close();
            await refreshTable(true);
          } catch (err) { handleErr(err); }
        }) }),
        h('button', { class: 'danger', i18n: 'action.delete',
          onclick: async (e) => {
            const btn = e.currentTarget; // capture BEFORE the await
            if (!(await confirmModal(t('confirm.delete').replace('{name}', name), { danger: true, okText: t('action.delete') }))) return;
            await withBusy(btn, async () => {
              try {
                await api.del(`/api/types/item?file=${encodeURIComponent(fileSelect.value)}&name=${encodeURIComponent(name)}`);
                toast(t('msg.deleted'), 'ok');
                selected.delete(name);
                delete dirty[name];
                updateDirtyUI();
                m.close();
                await refreshTable(true);
              } catch (err) { handleErr(err); }
            });
          }
        }),
        h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
      ]),
    );
    applyI18n();
  }

  let shownFile = '';
  fileSelect.onchange = async () => {
    // Switching file used to drop the unsaved inline edits without a word.
    if (dirtyCount() && !(await confirmModal(t('refresh.discard'), { danger: true, okText: t('action.discard') }))) {
      fileSelect.value = shownFile;
      return;
    }
    shownFile = fileSelect.value;
    clearDirty(); selected.clear();
    currentPage = 1; kindFilter = 'all'; catFilter = null; usageSel.value = ''; tierSel.value = '';
    updateDirtyUI(); updateSelUI();
    refreshTable(true);
  };
  [usageSel, tierSel, spawnOnly].forEach(el => el.addEventListener('change', () => { currentPage = 1; refreshTable(); }));

  function resetFilters() {
    search.value = ''; usageSel.value = ''; tierSel.value = ''; spawnOnly.checked = false;
    kindFilter = 'all'; catFilter = null; currentPage = 1;
    refreshTable();
  }

  // typeKind sorts an entry into the filter chips. Ammo and magazines are
  // "weapons" in vanilla's category, but admins look for them separately, so
  // they are recognised by their standard class-name prefixes.
  const typeKind = (r) => typeKindOf(r.name, r.category);
  function renderKindBar(rows) {
    const counts = { all: rows.length };
    for (const r of rows) { const k = typeKind(r); counts[k] = (counts[k] || 0) + 1; }
    kindBar.innerHTML = '';
    for (const k of ['all', ...TYPE_KIND_ORDER]) {
      if (k !== 'all' && !counts[k] && kindFilter !== k) continue;
      kindBar.append(h('button', {
        class: 'kind-chip' + (kindFilter === k ? ' active' : ''), type: 'button',
        onclick: () => { kindFilter = k; currentPage = 1; refreshTable(); },
      }, [k === 'all' ? null : typeKindIcon(k, 13), h('span', { i18n: 'types.kind.' + k }), h('span', { class: 'kind-count', text: String(counts[k] || 0) })]));
    }
  }
  // The deep-linked category, as a chip that clears it.
  function renderCatFilter() {
    catHost.innerHTML = '';
    if (catFilter === null) return;
    catHost.append(h('button', { type: 'button', class: 'pill', title: t('filter.remove'),
      onclick: () => { catFilter = null; currentPage = 1; refreshTable(); } }, [
      h('span', { text: tn('types.filter.catChip', { cat: typeCategoryLabel(catFilter) }) }),
      h('span', { class: 'types-chip-x', 'aria-hidden': 'true', text: '×' }),
    ]));
  }
  function fillFilterOptions(rows) {
    const us = new Set(), vs = new Set(), cs = new Set();
    for (const r of rows) {
      (r.usages || []).forEach(u => us.add(u));
      (r.values || []).forEach(v => vs.add(v));
      if (r.category) cs.add(r.category);
    }
    const fill = (sel, set, allKey) => {
      const cur = sel.value;
      sel.innerHTML = '';
      sel.append(h('option', { value: '', i18n: allKey }));
      [...set].sort((a, b) => a.localeCompare(b)).forEach(v => sel.append(h('option', { value: v, text: v })));
      sel.value = set.has(cur) ? cur : '';
    };
    fill(usageSel, us, 'types.filter.anyUsage');
    fill(tierSel, vs, 'types.filter.anyTier');
    catList.innerHTML = '';
    [...cs].sort((a, b) => a.localeCompare(b)).forEach(c => catList.append(h('option', { value: c })));
  }
  let _searchT = 0;
  search.oninput = () => { clearTimeout(_searchT); _searchT = setTimeout(() => { currentPage = 1; refreshTable(); }, 250); };

  root.append(catList);
  root.append(pageHeader('nav.types', 'types.subtitle', [revealBtn(() => typesFilePath ? { path: typesFilePath } : { kind: 'db' })]));
  if (State.serverStatus.running) root.append(runningBanner());
  root.append(
    h('div', { class: 'card' }, [
      h('div', { class: 'toolbar' }, [
        fileSelect, search, usageSel, tierSel,
        h('label', {}, [spawnOnly, h('span', { i18n: 'types.filter.spawning' })]),
        catHost,
      ]),
      kindBar,
      h('p', { class: 'hint', i18n: 'types.inline.hint' }),
      tableWrap,
      pagerHost,
      dock,
    ]),
  );
  // Leaving the page or reloading the tab with unsaved cells asks first, and
  // Ctrl+S saves them.
  root._isDirty = () => dirtyCount() > 0;
  root._save = () => saveDirty(saveBtn);
  applyI18n();

  // Custom types files and the spawn presets, then the table.
  const [moded, presets] = await Promise.all([
    api.get('/api/moded').catch(() => null),
    api.get('/api/types/presets').catch(() => []),
  ]);
  if (myNav !== _navSeq) return;
  for (const f of (moded && moded.files) || []) {
    fileSelect.append(h('option', { value: f.name, text: `moded_types/${f.name}${f.registered ? '' : ' (' + t('moded.notRegistered') + ')'}` }));
  }
  if (target && target.file && [...fileSelect.options].some(o => o.value === target.file)) fileSelect.value = target.file;
  shownFile = fileSelect.value;
  presetsList = Array.isArray(presets) ? presets : [];
  for (const p of presetsList) presetSel.append(h('option', { value: p.id, text: State.lang === 'ru' ? p.labelRu : p.label }));
  await refreshTable();
};

// --------------------------------------------------------------------- moded types

Views.moded = async (root) => {
  const myNav = _navSeq;
  let d;
  try { d = await api.get('/api/moded'); }
  catch (e) { root.append(h('div', { class: 'card' }, h('p', { text: e.message }))); return; }
  if (myNav !== _navSeq) return;
  const running = !!State.serverStatus.running;
  const files = d.files || [];

  const nameInput = h('input', { type: 'text', placeholder: 'mymod_types.xml', spellcheck: 'false' });
  const regCb = h('input', { type: 'checkbox', class: 'switch' }); regCb.checked = true;

  const filesCard = h('div', { class: 'card' }, [cardTitle('i-moded', 'moded.title')]);
  if (!files.length) {
    // Bare column headers over nothing told the admin nothing.
    filesCard.append(h('div', { class: 'empty-state' }, [
      h('div', { class: 'es-icon', html: svgIcon('i-moded', 34) }),
      h('div', { class: 'es-title', i18n: 'moded.empty.title' }),
      h('div', { class: 'es-hint', i18n: 'moded.empty.hint' }),
      h('div', { class: 'actions' }, [
        h('button', { class: 'primary', i18n: 'moded.import.pick', onclick: () => openImportFromMod() }),
        h('button', { class: 'secondary', i18n: 'moded.create', onclick: () => { nameInput.scrollIntoView({ block: 'center' }); nameInput.focus(); } }),
      ]),
    ]));
  } else {
    const table = h('table', { class: 'moded-table' });
    table.append(h('thead', {}, h('tr', {}, [
      h('th', { i18n: 'col.file' }),
      h('th', { class: 'num', i18n: 'moded.col.types' }),
      h('th', { class: 'num', i18n: 'col.size' }),
      h('th', { i18n: 'col.status' }),
      h('th', { text: '' }),
    ])));
    const tbody = h('tbody');
    for (const f of files) {
      // DayZ only loads a types file cfgeconomycore.xml lists — say so in words,
      // and offer the fix right on the row.
      const status = f.registered
        ? h('span', { class: 'badge ok', i18n: 'moded.registered' })
        : h('span', { class: 'badge warn', i18n: 'moded.notRegistered', title: t('moded.notRegistered.hint') });
      const actions = [
        // An unreadable file is not offered for registration: DayZ would then
        // try to load the broken XML.
        f.registered || f.broken ? null : h('button', { class: 'small primary', i18n: 'moded.register', disabled: running,
          title: t('moded.notRegistered.hint'), onclick: (e) => withBusy(e.currentTarget, async () => {
            try {
              await api.post('/api/moded/register', { fileName: f.name });
              toast(tn('moded.registeredOk', { name: f.name }), 'ok');
              await navigate('moded');
            } catch (err) { handleErr(err); }
          }) }),
        f.broken ? null : h('button', { class: 'small secondary', i18n: 'moded.openInTypes',
          onclick: () => { _typesTarget = { file: f.name }; navigate('types'); } }),
        h('button', { class: 'small danger', i18n: 'action.delete', disabled: running, onclick: async (e) => {
          const btn = e.currentTarget; // capture BEFORE the await
          if (!(await confirmModal(tn('moded.deleteConfirm', { name: f.name, n: f.types || 0 }), { danger: true, okText: t('action.delete') }))) return;
          await withBusy(btn, async () => {
            try {
              await api.post('/api/moded/delete', { fileName: f.name });
              toast(t('moded.deleted'), 'ok');
              await navigate('moded');
            } catch (err) { handleErr(err); }
          });
        } }),
      ].filter(Boolean);
      tbody.append(h('tr', {}, [
        h('td', {}, [
          h('span', { class: 'mono', text: f.name }),
          f.broken ? h('span', { class: 'badge err moded-broken', i18n: 'moded.broken', title: t('moded.broken.hint') }) : null,
        ].filter(Boolean)),
        h('td', { class: 'num', 'data-label': t('moded.col.types'), text: f.broken ? '—' : fmtCount(f.types || 0) }),
        h('td', { class: 'num', text: bytes(f.size) }),
        h('td', {}, status),
        h('td', {}, h('div', { class: 'moded-actions' }, actions)),
      ]));
    }
    table.append(tbody);
    filesCard.append(h('div', { class: 'table-scroll-narrow' }, table));
  }

  async function create(btn) {
    let name = nameInput.value.trim();
    // An empty name used to do nothing at all.
    if (!name) { toast(t('moded.needName'), 'warn'); nameInput.focus(); return; }
    if (!/\.xml$/i.test(name)) name += '.xml';
    await withBusy(btn, async () => {
      try {
        await api.post('/api/moded/create', { fileName: name, autoRegister: regCb.checked });
        toast(t('msg.created'), 'ok');
        await navigate('moded');
      } catch (e) { handleErr(e); }
    });
  }
  nameInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); create(createBtn); } });
  const createBtn = h('button', { class: 'primary', i18n: 'action.create', disabled: running, onclick: (e) => create(e.currentTarget) });

  // The folder path lives in the Open-folder button's tooltip: as a hint it
  // took three monospace lines on a phone.
  const reveal = revealBtn({ kind: 'moded' });
  reveal.removeAttribute('data-i18n-title');
  reveal.title = t('reveal.title') + ': ' + d.folder;
  root.append(pageHeader('nav.modedTypes', 'moded.subtitle', [reveal]));
  if (running) root.append(runningBanner());
  root.append(
    filesCard,
    h('div', { class: 'card' }, [
      cardTitle('i-mods', 'moded.import'),
      h('p', { class: 'hint', i18n: 'moded.import.hint' }),
      h('div', { class: 'actions' }, [
        h('button', { class: 'primary', i18n: 'moded.import.pick', onclick: () => openImportFromMod() }),
      ]),
    ]),
    h('div', { class: 'card' }, [
      cardTitle('i-plus', 'moded.create'),
      field('moded.fileName', nameInput),
      h('label', {}, [regCb, h('span', { i18n: 'moded.autoRegister' })]),
      h('div', { class: 'actions' }, [createBtn]),
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
            const found = r.files || [];
            if (!found.length) { toast(t('modtypes.none')); return; }
            m.close();
            // The list is rebuilt once the dialog closes, so an installed file
            // shows up right away.
            await openModTypesDialog(mod.name, found, () => navigate('moded'));
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
  const crumbs = h('nav', { class: 'files-crumbs', 'aria-label': t('files.tree') });
  const editorHost = h('textarea', { placeholder: t('files.selectFile'), 'aria-label': t('files.editor') });
  const pathLabel = h('div', { class: 'hint mono files-path' });
  const backupList = h('div', { class: 'card', style: { marginTop: '12px' }, hidden: true });
  let currentPath = '';
  let loadedText = '';
  let cm = null;
  const isDirty = () => !!currentPath && getValue() !== loadedText;
  // Refresh keeps the open file and warns about unsaved edits.
  root._isDirty = isDirty;
  root._beforeRefresh = () => { if (currentPath) _filesTarget = { path: currentPath }; };

  const getValue = () => cm ? cm.getValue() : editorHost.value;
  const setValue = (v) => { if (cm) cm.setValue(v || ''); else editorHost.value = v || ''; };
  const setMode = (path) => { if (cm) cm.setOption('mode', CM.modeFor(path)); };

  // Opening another file used to replace unsaved edits without a word.
  const mayLeaveFile = async () => !isDirty() ||
    await confirmModal(tn('eventmap.discard', { name: currentPath }), { danger: true, okText: t('action.discard') });

  async function refreshBackups() {
    backupList.innerHTML = '';
    // An empty card is a blank framed box — show it only once a file is open.
    backupList.hidden = !currentPath;
    if (!currentPath) return;
    backupList.append(withHelp(h('h3', { i18n: 'backup.title' }), 'settings.backup'));
    try {
      const d = await api.get('/api/backups/list?path=' + encodeURIComponent(currentPath));
      const baks = d.backups || [];
      if (!baks.length) {
        backupList.append(h('p', { class: 'hint', i18n: 'backup.none' }));
        return;
      }
      const running = !!State.serverStatus.running;
      for (const b of baks) {
        const when = new Date(b.time * 1000).toLocaleString();
        backupList.append(h('div', { class: 'row', style: { padding: '4px 0' } }, [
          h('span', { style: { flex: '1' }, text: b.name }),
          h('small', { class: 'hint', text: `${when} · ${bytes(b.size)}` }),
          // See what restoring would change before committing to it.
          h('button', { class: 'secondary', i18n: 'backup.diff',
            onclick: () => openBackupDiff(currentPath, b.name) }),
          // Confirmed like History's restore; the backend refuses while the
          // server runs, so the button says so up front.
          h('button', { i18n: 'backup.restore', 'data-help': 'backup.restore', disabled: running,
            title: running ? t('history.restoreStopped') : null, onclick: async (ev) => {
              const btn = ev.currentTarget;
              const msg = tn('history.restoreConfirm', { file: currentPath }) + (isDirty() ? ' ' + t('files.restoreUnsaved') : '');
              if (!(await confirmModal(msg, { danger: true, okText: t('backup.restore') }))) return;
              await withBusy(btn, async () => {
                try {
                  await api.post('/api/backups/restore', { path: currentPath, backup: b.name });
                  const r = await api.get('/api/files/read?path=' + encodeURIComponent(currentPath));
                  setValue(r.content);
                  loadedText = getValue(); // what is on disk now — not an unsaved change
                  toast(t('backup.restored'), 'ok');
                  refreshBackups();
                } catch (e) { handleErr(e); }
              });
            } }),
        ]));
      }
    } catch (e) { handleErr(e); }
  }

  // Until a file is open the column says what to do instead of showing an
  // empty editor whose Save silently did nothing.
  const pickHint = h('div', { class: 'empty-state' }, [
    h('div', { class: 'es-icon', html: svgIcon('i-files', 34) }),
    h('div', { class: 'es-title', i18n: 'files.pick.title' }),
    h('div', { class: 'es-hint', i18n: 'files.pick.hint' }),
  ]);
  const editorWrap = h('div', { hidden: true }, [editorHost]);
  const saveBtn = h('button', { class: 'primary', i18n: 'files.save', title: 'Ctrl+S', disabled: true, onclick: () => doSave() });
  const reloadBtn = h('button', { class: 'secondary', i18n: 'files.reload', disabled: true, onclick: async () => {
    if (!currentPath) return;
    if (isDirty() && !(await confirmModal(t('refresh.discard'), { danger: true }))) return;
    try { await openFilePath(currentPath); toast(t('refresh.done'), 'ok'); } catch (e) { handleErr(e); }
  } });
  const markActive = () => {
    for (const n of tree.querySelectorAll('.node[data-path]')) n.classList.toggle('active', n.dataset.path === currentPath);
  };

  // Open a single file into the editor. Shared by tree clicks and the global
  // config-search deep-link (via _filesTarget). Optionally jumps to a line.
  async function openFilePath(path, line) {
    const r = await api.get(`/api/files/read?path=${encodeURIComponent(path)}`);
    currentPath = path;
    pickHint.hidden = true;
    editorWrap.hidden = false;
    setMode(path.split('/').pop());
    setValue(r.content);
    loadedText = getValue();
    if (cm) cm.refresh(); // it was measured while hidden
    pathLabel.textContent = path;
    saveBtn.disabled = reloadBtn.disabled = false;
    markActive();
    await refreshBackups();
    if (cm && line > 0) {
      cm.setCursor(line - 1, 0);
      cm.scrollIntoView({ line: line - 1, ch: 0 }, 120);
      cm.focus();
    }
  }

  // The folder being shown, as a clickable path: inside mpmissions/…/db the
  // lone "⬆ .." gave no idea where you were.
  function renderCrumbs(path) {
    const parts = path ? path.split('/') : [];
    crumbs.innerHTML = '';
    const crumb = (label, target, last) => last
      ? h('span', { class: 'files-crumb current', text: label, 'aria-current': 'location' })
      : h('button', { type: 'button', class: 'files-crumb', text: label, onclick: () => loadDir(target).catch(handleErr) });
    crumbs.append(crumb(t('files.root'), '', !parts.length));
    parts.forEach((p, i) => {
      crumbs.append(h('span', { class: 'files-crumb-sep', 'aria-hidden': 'true', text: '/' }));
      crumbs.append(crumb(p, parts.slice(0, i + 1).join('/'), i === parts.length - 1));
    });
  }

  async function loadDir(path) {
    const d = await api.get(`/api/files/tree?path=${encodeURIComponent(path || '')}`);
    tree.innerHTML = '';
    renderCrumbs(path || '');
    for (const e of d.entries) {
      if (/\.bak\.\d/.test(e.name)) continue; // hide .bak.* clutter from the tree
      const node = h('div', { class: `node ${e.isDir ? 'dir' : ''}`,
        text: `${e.isDir ? '📁' : '📄'} ${e.name}`,
      });
      if (!e.isDir) node.dataset.path = e.path;
      node.onclick = async () => {
        // Any error here (413 for a >2 MB file, 404 for one that vanished)
        // used to be an unhandled rejection: no toast, nothing happened.
        try {
          if (e.isDir) return await loadDir(e.path);
          if (e.path === currentPath && isDirty()) return; // already open, keep the edits
          if (!(await mayLeaveFile())) return;
          await openFilePath(e.path);
          // One column on phones: the editor is below the tree, out of sight.
          if (window.matchMedia('(max-width: 900px)').matches) editorCol.scrollIntoView({ behavior: 'smooth', block: 'start' });
        } catch (err) { handleErr(err); }
      };
      tree.append(node);
    }
    markActive();
  }
  try { await loadDir(''); } catch (e) { handleErr(e); }

  const editorCol = h('div', { class: 'files-editor-col' }, [
    h('div', { class: 'files-editor-head' }, [
      h('h3', { i18n: 'files.editor' }),
      h('button', { type: 'button', class: 'small secondary files-totree', text: '↑ ' + t('files.tree'),
        onclick: () => crumbs.scrollIntoView({ behavior: 'smooth', block: 'start' }) }),
    ]),
    pathLabel,
    pickHint,
    editorWrap,
    h('div', { class: 'actions' }, [
      saveBtn,
      reloadBtn,
      revealBtn(() => currentPath ? { path: currentPath } : null),
    ]),
    backupList,
  ]);
  root.append(pageHeader('nav.files', 'files.subtitle'));
  if (State.serverStatus.running) root.append(runningBanner());
  root.append(
    h('div', { class: 'card files-view' }, [
      h('div', { class: 'grid-2' }, [
        h('div', {}, [h('h3', { i18n: 'files.tree' }), crumbs, tree]),
        editorCol,
      ]),
    ])
  );

  async function doSave() {
    if (!currentPath) return;
    try {
      await api.post('/api/files/write', { path: currentPath, content: getValue() });
      loadedText = getValue();
      toast(t('msg.saved'), 'ok');
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

  // Deep-link from the global config search: open the requested file (and jump
  // to the matched line) once the editor is ready.
  if (_filesTarget) {
    const tgt = _filesTarget; _filesTarget = null;
    const dir = tgt.path.split('/').slice(0, -1).join('/');
    try { await loadDir(dir); } catch {}
    try { await openFilePath(tgt.path, tgt.line); } catch (err) { handleErr(err); }
  }
};

// _filesTarget is a pending {path, line} the Files view should open on its next
// mount — set by the global config search before navigating to Files.
let _filesTarget = null;

// ------------------------------------------------------------- config search

// highlightNodes returns DOM nodes for `text` with every case-insensitive
// occurrence of `q` wrapped in <mark>. Built from text nodes (never innerHTML)
// so config contents can't inject markup.
function highlightNodes(text, q) {
  const out = [];
  const lc = text.toLowerCase(), ql = q.toLowerCase();
  let i = 0;
  while (true) {
    const idx = lc.indexOf(ql, i);
    if (idx < 0) { out.push(document.createTextNode(text.slice(i))); break; }
    if (idx > i) out.push(document.createTextNode(text.slice(i, idx)));
    out.push(h('mark', { text: text.slice(idx, idx + q.length) }));
    i = idx + q.length;
  }
  return out;
}

Views.search = async (root) => {
  root.append(pageHeader('nav.search', 'search.subtitle'));
  const input = h('input', { type: 'search', placeholder: t('search.placeholder'), 'aria-label': t('search.placeholder') });
  // The last query comes back: opening a hit in Files and pressing Back used
  // to land on an empty search.
  input.value = _lastSearch;
  const resultsCard = h('div', { class: 'card' });
  root.append(h('div', { class: 'card' }, h('div', { class: 'toolbar' }, [
    input,
    h('button', { class: 'primary', i18n: 'search.go', onclick: () => run() }),
  ])));
  root.append(resultsCard);
  // No auto-focus on touch screens: it popped the keyboard over the page.
  if (!(window.matchMedia && window.matchMedia('(pointer: coarse)').matches)) setTimeout(() => input.focus(), 0);

  let timer = null, seq = 0;
  input.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(run, 350); });
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter') { clearTimeout(timer); run(); } });

  // Before the first query: what can be searched, and a few examples — an
  // empty framed box said nothing.
  function intro() {
    resultsCard.innerHTML = '';
    resultsCard.append(h('div', { class: 'empty-state' }, [
      h('div', { class: 'es-icon', html: svgIcon('i-search', 34) }),
      h('div', { class: 'es-title', i18n: 'search.intro.title' }),
      h('div', { class: 'es-hint', i18n: 'search.intro.hint' }),
      h('div', { class: 'actions' }, [
        h('span', { class: 'hint', i18n: 'lootwhere.example' }),
        ...['AKM', 'MaxPlayers', 'Mag_STANAG'].map(ex => h('button', { type: 'button', class: 'pill', text: ex,
          onclick: () => { input.value = ex; run(); } })),
      ]),
    ]));
    applyI18n();
  }

  async function run() {
    const q = input.value.trim();
    _lastSearch = q;
    const mine = ++seq;
    resultsCard.innerHTML = '';
    if (!q) { intro(); return; }
    if (q.length < 2) { resultsCard.append(h('p', { class: 'hint', i18n: 'search.hintShort' })); applyI18n(); return; }
    let d;
    try { d = await api.get('/api/config/search?q=' + encodeURIComponent(q)); }
    catch (e) { handleErr(e); return; }
    if (mine !== seq) return; // a newer keystroke already fired
    // A class-like word is also a loot question: link the loot lookup.
    if (/^[A-Za-z][A-Za-z0-9_]+$/.test(q)) {
      resultsCard.append(h('div', { class: 'search-xlink' }, h('button', { type: 'button', class: 'secondary small',
        html: svgIcon('i-lootwhere', 14), onclick: () => { _lastLoot = q; navigate('lootwhere'); } },
        [h('span', { text: tn('search.whereLoot', { q }) })])));
    }
    const matches = d.matches || [];
    if (!matches.length) {
      resultsCard.append(h('div', { class: 'empty-state' }, h('div', { class: 'es-hint', i18n: 'search.none' })));
      applyI18n();
      return;
    }
    resultsCard.append(h('p', { class: 'hint',
      text: tn('search.count', { n: d.total }) + (d.truncated ? ' ' + t('search.truncated') : '') }));

    // Group consecutive matches by file (backupItems order keeps them together).
    let curFile = null, group = null;
    for (const m of matches) {
      if (m.file !== curFile) {
        curFile = m.file;
        group = h('div', { class: 'search-group' }, [
          h('div', { class: 'search-file' }, [
            h('span', { class: 'search-file-name', text: m.file }),
            m.path ? null : h('span', { class: 'badge mute', i18n: 'search.readonly' }),
          ]),
        ]);
        resultsCard.append(group);
      }
      const row = h('div', { class: 'search-hit' + (m.path ? ' link' : '') }, [
        h('span', { class: 'search-ln', text: String(m.line) }),
        h('code', { class: 'search-snip' }, highlightNodes(m.text, q)),
      ]);
      if (m.path) {
        row.title = t('search.open');
        row.onclick = () => { _filesTarget = { path: m.path, line: m.line }; navigate('files'); };
      }
      group.append(row);
    }
    applyI18n();
  }
  if (input.value.trim()) await run(); else intro();
};

// ----------------------------------------------------------- config profiles

Views.cfgprofiles = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.cfgProfiles', 'cfgProfiles.subtitle'));
  const running = !!State.serverStatus.running;
  if (running) root.append(runningBanner());

  const nameIn = h('input', { type: 'text', placeholder: t('cfgProfiles.namePh'), 'aria-label': t('cfgProfiles.namePh') });
  const noteIn = h('input', { type: 'text', placeholder: t('cfgProfiles.notePh'), 'aria-label': t('cfgProfiles.notePh') });
  const saveBtn = h('button', { class: 'primary', i18n: 'cfgProfiles.save', onclick: () => save(saveBtn) });
  nameIn.addEventListener('keydown', (e) => { if (e.key === 'Enter') save(saveBtn); });
  root.append(h('div', { class: 'card' }, [
    cardTitle('i-plus', 'cfgProfiles.saveTitle'),
    h('p', { class: 'hint', i18n: 'cfgProfiles.saveHint' }),
    h('div', { class: 'toolbar' }, [nameIn, noteIn, saveBtn]),
  ]));

  const listCard = h('div', { class: 'card' }, [cardTitle('i-cfgprofiles', 'cfgProfiles.list')]);
  const schedCard = h('div', { class: 'card' });
  root.append(listCard, schedCard);
  let profiles = [];

  async function save(btn) {
    const name = nameIn.value.trim();
    if (!name) { toast(t('cfgProfiles.needName'), 'warn'); nameIn.focus(); return; }
    // The same name (any case) replaces that profile — say so first.
    const same = profiles.find(p => String(p.name || '').trim().toLowerCase() === name.toLowerCase());
    if (same && !(await confirmModal(tn('cfgProfiles.overwriteConfirm', { name: same.name }), { okText: t('cfgProfiles.overwrite') }))) return;
    await withBusy(btn, async () => {
      try {
        await api.post('/api/config-profiles/save', { name, note: noteIn.value.trim() });
        toast(t('cfgProfiles.saved'), 'ok');
        nameIn.value = ''; noteIn.value = '';
        await load();
      } catch (e) { handleErr(e); }
    });
  }

  // Refresh a profile from the current config without retyping its name.
  async function update(p, btn) {
    if (!(await confirmModal(tn('cfgProfiles.updateConfirm', { name: p.name }), { okText: t('cfgProfiles.update') }))) return;
    await withBusy(btn, async () => {
      try {
        await api.post('/api/config-profiles/save', { name: p.name, note: p.note || '', slug: p.slug });
        toast(t('cfgProfiles.saved'), 'ok');
        await load();
      } catch (e) { handleErr(e); }
    });
  }

  const LAUNCH_LABEL = {
    mods: 'nav.mods', serverCfg: 'settings.serverCfg', cpuCount: 'settings.cpuCount', limitFPS: 'settings.limitFps',
    extraLaunchParams: 'settings.extraParams', doLogs: 'settings.flag.dologs', adminLog: 'settings.flag.adminlog',
    netLog: 'settings.flag.netlog', freezeCheck: 'settings.flag.freezecheck', filePatching: 'settings.flag.filePatching',
  };
  function launchRow(c) {
    let val;
    if (c.key === 'mods' || c.key === 'serverMods') {
      const a = c.from ? c.from.split(';') : [], b = c.to ? c.to.split(';') : [];
      const add = b.filter(x => !a.includes(x)), del = a.filter(x => !b.includes(x));
      val = (add.length || del.length)
        ? [...add.map(x => h('span', { class: 'diff-plus', text: '+' + x })), ...del.map(x => h('span', { class: 'diff-minus', text: '−' + x }))]
        : [h('span', { class: 'hint', i18n: 'cfgProfiles.diff.order' })];
    } else {
      const fmt = (v) => v === 'true' ? t('cfgProfiles.on') : v === 'false' ? t('cfgProfiles.off') : (v || '—');
      val = [h('span', { text: fmt(c.from) + ' → ' + fmt(c.to) })];
    }
    return h('div', { class: 'cfgp-diff-row' }, [
      LAUNCH_LABEL[c.key] ? h('span', { class: 'cfgp-diff-key', i18n: LAUNCH_LABEL[c.key] }) : h('code', { class: 'cfgp-diff-key', text: '-serverMod' }),
      h('span', { class: 'cfgp-diff-val' }, val),
    ]);
  }

  // The confirm lists what applying would actually change instead of a blind
  // "every config is overwritten".
  function applyPreview(p, d) {
    return new Promise((resolve) => {
      let ok = false;
      const m = openModal({ title: tn('cfgProfiles.applyTitle', { name: p.name }), wide: true, onClose: () => resolve(ok) });
      const files = d.files || [], launch = d.launch || [];
      const body = [];
      if (!files.length && !launch.length) {
        body.push(h('p', { class: 'hint', i18n: 'cfgProfiles.diff.none' }));
      } else {
        body.push(h('p', { class: 'hint', i18n: 'cfgProfiles.applyConfirm' }));
        if (launch.length) {
          body.push(h('h4', { class: 'sub-title', i18n: 'server.launch.title' }), h('div', { class: 'cfgp-diff' }, launch.map(launchRow)));
        }
        if (files.length) {
          body.push(h('h4', { class: 'sub-title', i18n: 'files.title' }), h('div', { class: 'cfgp-diff' }, files.map(f => h('div', { class: 'cfgp-diff-row' }, [
            h('span', { class: 'cfgp-diff-path', text: f.path }),
            f.status === 'new'
              ? h('span', { class: 'badge', i18n: 'cfgProfiles.diff.new' })
              : h('span', { class: 'cfgp-diff-val' }, [h('span', { class: 'diff-plus', text: '+' + (f.added || 0) }), h('span', { class: 'diff-minus', text: '−' + (f.removed || 0) })]),
          ]))));
        }
      }
      if (d.same) body.push(h('p', { class: 'hint', text: tn('cfgProfiles.diff.same', { n: d.same }) }));
      const okBtn = h('button', { class: 'primary', i18n: 'cfgProfiles.apply', onclick: () => { ok = true; m.close(); } });
      body.push(h('div', { class: 'actions' }, [okBtn, h('button', { i18n: 'action.cancel', onclick: () => m.close() })]));
      m.body.append(...body);
      setTimeout(() => okBtn.focus(), 0);
    });
  }

  async function apply(p, btn) {
    let d = null;
    await withBusy(btn, async () => {
      try { d = await api.get('/api/config-profiles/diff?slug=' + encodeURIComponent(p.slug)); } catch (e) { handleErr(e); }
    });
    if (!d || !(await applyPreview(p, d))) return;
    await withBusy(btn, async () => {
      try {
        const r = await api.post('/api/config-profiles/apply', { slug: p.slug });
        toast(tn('cfgProfiles.applied', { n: r.count || 0 }), 'ok');
        // The mod list and launch flags may have changed with it.
        State.config = await api.get('/api/config');
      } catch (e) { handleErr(e); }
    });
  }

  async function del(p, btn) {
    if (!(await confirmModal(t('cfgProfiles.deleteConfirm').replace('{name}', p.name), { danger: true, okText: t('action.delete') }))) return;
    await withBusy(btn, async () => {
      try {
        await api.post('/api/config-profiles/delete', { slug: p.slug });
        await load();
      } catch (e) { handleErr(e); }
    });
  }

  // Swaps on a timer are set up in Settings → Restarts; this page only said
  // nothing about them.
  function renderSchedule() {
    const names = Object.fromEntries(profiles.map(p => [p.slug, p.name]));
    const sched = ((State.config && State.config.scheduledProfiles) || []).filter(s => s.enabled && s.profile);
    schedCard.innerHTML = '';
    schedCard.append(cardTitle('i-clock', 'cfgProfiles.schedule.title'));
    if (!sched.length) schedCard.append(h('p', { class: 'hint', i18n: 'cfgProfiles.schedule.none' }));
    for (const s of sched) {
      schedCard.append(h('div', { class: 'cfgp-sched-row' }, [
        h('span', { class: 'mono', text: s.time || '—' }),
        h('span', { text: '→ ' + (names[s.profile] || s.profile) }),
        (s.days || []).length ? h('span', { class: 'hint', text: weekdayList(s.days) }) : null,
      ].filter(Boolean)));
    }
    schedCard.append(h('div', { class: 'actions' }, [h('button', { class: 'secondary', i18n: 'cfgProfiles.schedule.open', onclick: async () => {
      await navigate('settings/restarts');
      const card = document.getElementById('sched-profiles');
      if (card) card.scrollIntoView({ behavior: 'smooth', block: 'start' });
    } })]));
  }

  async function load() {
    let d;
    try { d = await api.get('/api/config-profiles'); }
    catch (e) { handleErr(e); return; }
    if (myNav !== _navSeq) return;
    profiles = d.profiles || [];
    listCard.innerHTML = '';
    listCard.append(cardTitle('i-cfgprofiles', 'cfgProfiles.list'));
    if (!profiles.length) {
      listCard.append(h('div', { class: 'empty-state' }, h('div', { class: 'es-hint', i18n: 'cfgProfiles.none' })));
    }
    for (const p of profiles) {
      listCard.append(h('div', { class: 'profile-row' }, [
        h('div', { class: 'profile-main' }, [
          h('div', { class: 'profile-name', text: p.name }),
          p.note ? h('div', { class: 'hint', text: p.note }) : null,
          h('div', { class: 'hint', text:
            fmtWhen(p.created) + ' · ' + tn('cfgProfiles.files', { n: p.files }) + ' · ' + bytes(p.size) }),
        ]),
        h('div', { class: 'profile-actions' }, [
          h('button', { class: 'primary', i18n: 'cfgProfiles.apply', disabled: running, title: running ? t('guard.serverRunning') : null,
            onclick: (e) => apply(p, e.currentTarget) }),
          h('button', { class: 'secondary', i18n: 'cfgProfiles.update', onclick: (e) => update(p, e.currentTarget) }),
          h('button', { class: 'danger', i18n: 'action.delete', onclick: (e) => del(p, e.currentTarget) }),
        ]),
      ]));
    }
    renderSchedule();
    applyI18n();
  }
  await load();
};

// ----------------------------------------------------------- loot economy

Views.economy = async (root) => {
  const myNav = _navSeq;
  const running = !!State.serverStatus.running;

  // File picker — vanilla types.xml plus any custom (moded) types files.
  const files = ['types.xml'];
  try { const md = await api.get('/api/moded'); (md.files || []).forEach(f => files.push(f.name)); }
  catch {}
  if (myNav !== _navSeq) return;
  let curFile = '';
  let cats = [];          // the file's real categories, for the tuning scope
  let tuneCat = '';       // '' = scale the whole file
  let tuneScaleMin = true;
  let baselineAt = 0;     // when the ×1 snapshot was taken (0 = none yet)
  let stats = {};         // the last /api/economy/stats
  const fileSel = h('select', { 'aria-label': t('economy.file'), onchange: () => { curFile = fileSel.value; loadStats(); } });
  files.forEach(f => fileSel.append(h('option', { value: f === 'types.xml' ? '' : f, text: f })));
  // The file picker lives in the header: a card holding one select was a box
  // in a box and, on a phone, a whole screen row.
  root.append(pageHeader('nav.economy', 'economy.subtitle', [
    h('label', { class: 'econ-file' }, [h('span', { i18n: 'economy.file' }), fileSel]),
    revealBtn({ kind: 'db' }, { label: 'economy.openDb' }),
  ]));

  // players-tiles: 2×2 on a phone instead of four full-width cards.
  const tiles = h('div', { class: 'grid-4 players-tiles' });
  const tuneCard = h('div', { class: 'card' });
  const chartsHost = h('div');
  root.append(tiles, tuneCard, chartsHost);

  const tile = (key, val, helpKey) => h('div', { class: 'card' }, [
    helpKey ? withHelp(h('h3', { i18n: key }), helpKey) : h('h3', { i18n: key }),
    h('div', { class: 'metric-num', text: val })]);

  // One bar. With a target the row opens Types filtered to exactly that slice,
  // so a chart is a way in rather than a dead end. Spans, not divs: the row
  // can be a <button>.
  function hbar(label, value, max, sub, target, kind) {
    const pct = max > 0 ? Math.max(2, Math.round(value / max * 100)) : 0;
    const lbl = h('span', { class: 'ebar-label' }, kind ? [typeKindIcon(kind, 13), label] : label);
    lbl.title = lbl.textContent;
    const kids = [
      lbl,
      h('span', { class: 'ebar-track' }, h('span', { class: 'ebar-fill', style: { width: pct + '%' } })),
      h('span', { class: 'ebar-val', text: sub != null ? sub : fmtCount(value) }),
    ];
    const cls = 'ebar-row' + (kind ? ' tk-' + kind : '');
    if (!target) return h('div', { class: cls }, kids);
    return h('button', { type: 'button', class: cls + ' ebar-link', title: t('economy.openInTypes'),
      onclick: () => { _typesTarget = Object.assign({ file: curFile }, target); navigate('types'); } }, kids);
  }

  // A category in the UI language, with DayZ's own name as small code.
  function catLabel(name) {
    if (!name) return h('span', { text: t('economy.cat.none') });
    const loc = typeCategoryLabel(name);
    return loc === name ? h('span', { text: name })
      : h('span', { class: 'ebar-cat' }, [h('span', { text: loc }), ' ', h('code', { class: 'cfg-key', text: name })]);
  }

  function renderTune() {
    tuneCard.innerHTML = '';
    tuneCard.append(cardTitle('i-globals', 'economy.tune.title', 'economy.tune'), h('p', { class: 'hint', i18n: 'economy.tune.hint' }));
    if (running) tuneCard.append(runningBanner());
    // Scope: the whole file or one category — "less military loot, more food"
    // is the usual request.
    const catSel = h('select', {});
    catSel.append(h('option', { value: '', text: t('economy.tune.allCats') }));
    for (const c of cats) {
      const loc = typeCategoryLabel(c);
      catSel.append(h('option', { value: c, text: loc === c ? c : `${loc} (${c})` }));
    }
    if (!cats.includes(tuneCat)) tuneCat = '';
    catSel.value = tuneCat;
    // Each button says roughly what it gives: the spawn total of the scope
    // (the whole file or the chosen category) after scaling.
    const preview = [];
    const scopeNominal = () => tuneCat ? ((stats.categories || []).find(c => c.name === tuneCat) || {}).nominal || 0 : stats.totalNominal || 0;
    const updatePreview = () => { for (const { el, f } of preview) el.textContent = '≈ ' + fmtCount(Math.round(scopeNominal() * f)); };
    catSel.onchange = () => { tuneCat = catSel.value; updatePreview(); };
    const scaleBtn = (p) => {
      const est = h('small', { class: 'econ-est' });
      preview.push({ el: est, f: p.f });
      return h('button', { class: 'secondary econ-scale', disabled: running, title: t('economy.tune.est'), onclick: (e) => tune(p.f, e.currentTarget) },
        [h('span', { i18n: p.label }), est]);
    };
    // ×1 restores the amounts captured before the first scaling — a one-click undo.
    // It sits between the "more" and "less" presets so the row reads high→low.
    const resetBtn = h('button', { class: 'secondary econ-scale', title: t('economy.tune.reset.hint'), disabled: running, onclick: (e) => tuneReset(e.currentTarget) },
      [h('span', { i18n: 'economy.tune.reset' }), h('small', { class: 'econ-est', i18n: baselineAt ? 'economy.tune.reset.sub' : 'economy.tune.reset.subNone' })]);
    const scaleMin = h('input', { type: 'checkbox', class: 'switch' });
    scaleMin.checked = tuneScaleMin;
    scaleMin.onchange = () => { tuneScaleMin = scaleMin.checked; };
    tuneCard.append(
      h('div', { class: 'econ-tune-scope' }, [h('label', { for: assocId(catSel), i18n: 'economy.tune.scope' }), catSel]),
      h('div', { class: 'toolbar' }, [
        scaleBtn({ f: 2, label: 'economy.tune.x2' }),
        scaleBtn({ f: 1.5, label: 'economy.tune.x15' }),
        resetBtn,
        scaleBtn({ f: 0.5, label: 'economy.tune.half' }),
        scaleBtn({ f: 0.25, label: 'economy.tune.quarter' }),
      ]),
      h('label', {}, [scaleMin, h('span', { i18n: 'economy.tune.scaleMin' })]),
    );
    updatePreview();
    // Say what ×1 would bring back, so it is never a blind overwrite.
    if (baselineAt) {
      tuneCard.append(h('p', { class: 'hint', text: tn('economy.tune.baseline', { when: new Date(baselineAt * 1000).toLocaleString() }) }));
    }
  }

  async function tune(factor, btn) {
    const pct = Math.round(factor * 100);
    const msg = tuneCat ? tn('economy.tune.confirmCat', { pct, cat: typeCategoryLabel(tuneCat) }) : tn('economy.tune.confirm', { pct });
    if (!(await confirmModal(msg))) return;
    await withBusy(btn, async () => {
      try {
        const r = await api.post('/api/economy/tune', { file: curFile, factor, nominal: true, min: tuneScaleMin, category: tuneCat });
        toast(tn('economy.tune.done', { n: r.touched || 0 }), 'ok');
        // Rare items (nominal 1) are held at 1 instead of rounding to 0 and
        // never spawning again — name them, so the floor is no surprise.
        const fl = r.floored || [];
        if (fl.length) {
          toast(tn('economy.tune.floored', { n: fl.length, names: fl.slice(0, 6).join(', ') + (fl.length > 6 ? ', …' : '') }), 'warn');
        }
        await loadStats();
      } catch (e) { handleErr(e); }
    });
  }

  async function tuneReset(btn) {
    if (!baselineAt) { toast(t('economy.tune.reset.none'), 'warn'); return; }
    const when = new Date(baselineAt * 1000).toLocaleString();
    if (!(await confirmModal(tn('economy.tune.reset.confirm', { when })))) return;
    await withBusy(btn, async () => {
      try {
        const r = await api.post('/api/economy/tune', { file: curFile, reset: true });
        if (r.hadBaseline) { toast(tn('economy.tune.reset.doneN', { n: r.restored || 0 }), 'ok'); await loadStats(); }
        else toast(t('economy.tune.reset.none'), 'warn');
      } catch (e) { handleErr(e); }
    });
  }

  // opts: { icon, sub(r), limit, label(r), target(r), kind(r) }.
  function barCard(titleKey, rows, valOf, opts = {}) {
    if (!rows || !rows.length) return null;
    const list = rows.slice(0, opts.limit || rows.length);
    const max = Math.max.apply(null, list.map(valOf));
    const bars = h('div', { class: 'ebar-list' });
    for (const r of list) {
      bars.append(hbar(opts.label ? opts.label(r) : r.name, valOf(r), max,
        opts.sub ? opts.sub(r) : null, opts.target ? opts.target(r) : null, opts.kind ? opts.kind(r) : null));
    }
    return h('div', { class: 'card' }, [cardTitle(opts.icon || 'i-economy', titleKey), bars]);
  }
  // Tiers in their own order (Tier1 → Tier4, then the rest), not by count.
  const tierOrder = (rows) => (rows || []).slice().sort((a, b) => {
    const ta = /^tier(\d+)$/i.exec(a.name), tb = /^tier(\d+)$/i.exec(b.name);
    if (ta && tb) return Number(ta[1]) - Number(tb[1]);
    if (ta || tb) return ta ? -1 : 1;
    return a.name.localeCompare(b.name);
  });

  async function loadStats() {
    let d;
    try { d = await api.get('/api/economy/stats' + (curFile ? '?file=' + encodeURIComponent(curFile) : '')); }
    catch (e) { handleErr(e); return; }
    if (myNav !== _navSeq) return;
    // "" is the no-category bucket: shown as a bar, but not one of the file's
    // categories.
    const realCats = (d.categories || []).filter(c => c.name);
    cats = realCats.map(c => c.name);
    baselineAt = d.baselineAt || 0;
    stats = d;
    tiles.innerHTML = '';
    tiles.append(
      tile('economy.tiles.items', fmtCount(d.total || 0), 'economy.items'),
      tile('economy.tiles.nominal', fmtCount(d.totalNominal || 0), 'economy.nominal'),
      tile('economy.tiles.categories', fmtCount(realCats.length), 'economy.categories'),
      tile('economy.tiles.min', fmtCount(d.totalMin || 0), 'economy.min'),
    );
    renderTune();
    chartsHost.innerHTML = '';
    const catCard = barCard('economy.byCategory', d.categories, r => r.nominal, {
      limit: 14, label: r => catLabel(r.name), target: r => ({ category: r.name }), kind: r => typeKindOf('', r.name),
      sub: r => fmtCount(r.nominal) + ' · ' + tn('economy.itemsN', { n: r.count }) });
    const usageCard = barCard('economy.byUsage', d.usages, r => r.count, { limit: 12, target: r => ({ usage: r.name }) });
    const tierCard = barCard('economy.byTier', tierOrder(d.values), r => r.count, { icon: 'i-layers', target: r => ({ tier: r.name }) });
    const topCard = barCard('economy.top', d.top, r => r.nominal, { icon: 'i-leaderboard', limit: 20, target: r => ({ q: r.name }),
      kind: r => typeKindOf(r.name, r.category) });
    // Where and at which tier side by side: two short lists, one screen.
    const pair = [usageCard, tierCard].filter(Boolean);
    const cards = [catCard, pair.length ? h('div', { class: 'econ-pair' }, pair) : null, topCard].filter(Boolean);
    if (cards.length) chartsHost.append(h('p', { class: 'hint econ-drill-hint', i18n: 'economy.drillHint' }), ...cards);
    applyI18n();
  }

  renderTune();
  await loadStats();
  applyI18n();
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
          reportModMissions(r);
        } catch (e) { handleErr(e); }
      }}),
      h('button', { i18n: 'action.cancel', onclick: () => m.close() }),
    ]),
  );
}

// Inline modal-ish dialog that lists candidate types.xml inside a mod and
// lets the user install each one into the active mission with a chosen name.
// onDone (optional) runs when the dialog closes after at least one install, so
// the calling page can show the new file.
async function openModTypesDialog(mod, files, onDone) {
  let installedAny = false;
  const m = openModal({ title: tn('modtypes.title', { mod }), wide: true,
    onClose: () => { if (installedAny && onDone) onDone(); } });
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
        installedAny = true;
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

// Views.validator lives in features.js (v1.0.0: grouped by issue code, per-kind fixes).

Views.logs = async (root) => {
  // All three MUST come before the first await: navigate() has already run the
  // previous view's teardown, so a late assignment lands on the NEXT view and
  // leaks this EventSource for the rest of the session.
  const myNav = _navSeq;
  let source;
  root._teardown = () => { if (source) source.close(); };

  const sources = await api.get('/api/logs/list');
  if (myNav !== _navSeq) return; // superseded while the list was loading
  const srcLabel = (s) => {
    const k = 'logs.src.' + (s.kind || s.id);
    return State.i18n[k] ? t(k) : s.label;
  };
  const picker = h('select', { 'aria-label': t('logs.source') });
  for (const s of sources.filter(s => !s.prev)) {
    picker.append(h('option', { value: s.id, text: s.exists ? srcLabel(s) : `${srcLabel(s)} (${t('logs.noFileYet')})` }));
  }
  // Earlier runs: DayZ starts a new RPT / ADM on every launch, so after a crash
  // and a watchdog restart the crashed run's log is no longer the newest.
  const prev = sources.filter(s => s.prev);
  if (prev.length) {
    picker.append(h('optgroup', { label: t('logs.prevRuns') }, prev.map(s =>
      h('option', { value: s.id, text: srcLabel(s) + ' · ' + fmtDateTime(s.started) }))));
  }

  const pane = h('pre', { class: 'log-pane', tabindex: '0' });
  const search = h('input', { type: 'search', placeholder: t('logs.search.ph'), 'aria-label': t('logs.search.ph') });
  const errSw = h('input', { type: 'checkbox', class: 'switch' });
  const pauseBtn = h('button', { type: 'button', class: 'secondary logs-pause', 'aria-pressed': 'false' });
  const dlLink = h('a', { class: 'btn-link', download: '', i18n: 'logs.download', title: t('logs.download.hint') });
  const streamBadge = h('span', { class: 'badge mute', hidden: true });
  const matchInfo = h('span', { class: 'hint' });

  const MAX_CHARS = 200_000;
  // While filtering, at most this many matching lines stay on screen.
  const MAX_SHOWN = 5000;
  // What "errors only" keeps: script errors, exceptions, crashes.
  const ERR_RE = /error|exception|script\s*\(e\)|crash|fatal/i;
  let buf = '';          // the tail received so far
  let query = '';        // search text, lower-cased
  let onlyErr = false;
  let paused = false;
  let missing = false;   // the file is not written yet
  let cleared = false;   // the admin emptied the screen
  let shown = 0;         // matching lines on screen
  const filtering = () => !!(query || onlyErr);
  const keep = (line) => (!onlyErr || ERR_RE.test(line)) && (!query || line.toLowerCase().includes(query));

  // Sticky-bottom autoscroll: scrolling up suspends following so the user can
  // read; scrolling back to the bottom resumes it.
  let stickBottom = true;
  pane.addEventListener('scroll', () => {
    stickBottom = pane.scrollHeight - pane.scrollTop - pane.clientHeight < 24;
  });

  // lineEl is one matching line with the search text marked (none when case
  // folding changes the length — the marks would land in the wrong place).
  function lineEl(line) {
    const el = h('span', { class: 'log-line' });
    const low = line.toLowerCase();
    let i = 0;
    if (query && low.length === line.length) {
      for (let j = low.indexOf(query); j >= 0; j = low.indexOf(query, i)) {
        if (j > i) el.append(line.slice(i, j));
        el.append(h('mark', { text: line.slice(j, j + query.length) }));
        i = j + query.length;
      }
    }
    el.append(line.slice(i) + '\n');
    return el;
  }
  function addMatches(text) {
    const frag = document.createDocumentFragment();
    for (const line of text.split('\n')) {
      if (line && keep(line)) { frag.append(lineEl(line)); shown++; }
    }
    pane.append(frag);
    while (shown > MAX_SHOWN && pane.firstChild) { pane.firstChild.remove(); shown--; }
  }
  // updateState: what an empty pane says, the waiting badge, the counter.
  function updateState() {
    let msg = '';
    if (!buf) msg = t(missing ? 'logs.empty.missing' : cleared ? 'logs.empty.cleared' : 'logs.empty.none');
    else if (filtering() && !shown) msg = t('logs.empty.nomatch');
    pane.classList.toggle('is-empty', !!msg);
    if (msg) pane.dataset.empty = msg; else delete pane.dataset.empty;
    matchInfo.textContent = filtering() && buf ? tn('logs.matches', { n: shown }) : '';
    dlLink.hidden = missing && !buf;
    if (missing && !buf) { streamBadge.textContent = t('logs.waitingFile'); streamBadge.hidden = false; }
  }
  // render draws the whole buffer again: all of it, or the matching lines.
  function render() {
    shown = 0;
    pane.textContent = filtering() ? '' : buf;
    if (filtering()) addMatches(buf);
    updateState();
    if (stickBottom) pane.scrollTop = pane.scrollHeight;
  }
  function trimBuf() {
    if (buf.length <= MAX_CHARS) return;
    const cut = buf.indexOf('\n', buf.length - MAX_CHARS);
    buf = buf.slice(cut >= 0 ? cut + 1 : buf.length - MAX_CHARS);
  }

  // Buffer incoming chunks and flush at most every 200 ms so a chatty RPT
  // does not produce hundreds of DOM mutations per second (which is what
  // froze the panel before).
  let pending = '';
  let flushScheduled = false;
  function scheduleFlush() {
    if (flushScheduled || paused) return;
    flushScheduled = true;
    setTimeout(() => {
      flushScheduled = false;
      if (!pending || paused) return;
      const chunk = pending;
      pending = '';
      buf += chunk;
      trimBuf();
      if (filtering()) addMatches(chunk);
      else pane.textContent = buf;
      updateState();
      if (stickBottom) pane.scrollTop = pane.scrollHeight;
    }, 200);
  }
  function append(text) {
    pending += text;
    if (pending.length > MAX_CHARS) pending = pending.slice(-MAX_CHARS);
    scheduleFlush();
  }
  // One pause / resume toggle. Resuming flushes what came in meanwhile —
  // otherwise the pane stays frozen until the next line (maybe never).
  function setPaused(v) {
    paused = v;
    pauseBtn.textContent = (v ? '▶ ' : '⏸ ') + t(v ? 'logs.resume' : 'logs.pause');
    pauseBtn.setAttribute('aria-pressed', String(v));
    if (!v) scheduleFlush();
  }
  pauseBtn.onclick = () => setPaused(!paused);
  setPaused(false);

  let searchT = 0;
  search.addEventListener('input', () => {
    clearTimeout(searchT);
    searchT = setTimeout(() => { query = search.value.trim().toLowerCase(); stickBottom = true; render(); }, 250);
  });
  errSw.addEventListener('change', () => { onlyErr = errSw.checked; stickBottom = true; render(); });

  async function attach(id) {
    if (source) { source.close(); source = null; }
    pending = '';
    buf = '';
    missing = false;
    cleared = false;
    stickBottom = true;
    streamBadge.hidden = true;
    dlLink.href = srvUrl('/api/logs/download?id=' + encodeURIComponent(id));
    // The stream continues exactly where the snapshot ended, so the tail is
    // not shown twice (it used to replay the last 64 KB on top of it).
    let r = null, err = null;
    try { r = await api.get(`/api/logs/read?id=${encodeURIComponent(id)}`); } catch (e) { err = e; }
    // Switched source, or left the page, while loading — a stream opened now
    // would outlive the view's teardown.
    if (picker.value !== id || myNav !== _navSeq) return;
    let from = '';
    if (r) {
      missing = !!r.missing;
      buf = r.content || '';
      trimBuf();
      if (Number.isFinite(r.end)) from = `&offset=${r.end}`;
    } else buf = `[${err.message}]\n`;
    render();

    source = new EventSource(srvUrl(`/api/logs/stream?id=${encodeURIComponent(id)}${from}`), { withCredentials: true });
    // Connection state lives in a badge, not injected into the log text —
    // a stopped server otherwise steadily fills the pane with reconnect noise.
    // A file not written yet keeps its "waiting for the file" badge.
    source.onopen = () => { streamBadge.hidden = true; updateState(); };
    source.onmessage = ev => { missing = false; streamBadge.hidden = true; append(ev.data + '\n'); };
    source.onerror = () => {
      streamBadge.textContent = t('logs.reconnecting');
      streamBadge.hidden = false;
    };
  }

  picker.onchange = () => attach(picker.value);

  root.append(
    pageHeader('nav.logs', 'logs.subtitle'),
    h('div', { class: 'card' }, [
      h('div', { class: 'toolbar logs-toolbar' }, [
        picker,
        search,
        h('label', { title: t('logs.onlyErrors.hint') }, [errSw, h('span', { i18n: 'logs.onlyErrors' })]),
        h('span', { class: 'spacer' }),
        pauseBtn,
        h('button', { type: 'button', class: 'secondary', i18n: 'logs.clear', title: t('logs.clear.hint'),
          onclick: () => { buf = ''; pending = ''; cleared = true; render(); } }),
        dlLink,
        revealBtn({ kind: 'profiles' }, { label: 'folders.profiles' }),
        streamBadge,
      ]),
      pane,
      h('div', { class: 'logs-foot' }, [matchInfo, h('span', { class: 'hint', i18n: 'logs.capHint' })]),
    ]),
  );

  // Attach to the first source that actually has a file — sources[0] may be a
  // placeholder ("no file yet") while a later entry holds real content.
  const first = sources.find(s => s.exists && !s.prev) || sources[0];
  if (first) { picker.value = first.id; await attach(first.id); }
};

// --------------------------------------------------------------------- admlog

Views.admlog = async (root) => {
  const myNav = _navSeq;
  const typeSel = h('select', { 'aria-label': t('admlog.type.all') });
  const types = ['all','connect','disconnect','kill','hit','chat','death','position','base','other'];
  for (const tp of types) {
    typeSel.append(h('option', { value: tp === 'all' ? '' : tp, i18n: `admlog.type.${tp}` }));
  }
  // This run's .ADM or an earlier run's (DayZ starts a new file every launch).
  const fileSel = h('select', { 'aria-label': t('admlog.file'), hidden: true });
  const playerInp = h('input', { type: 'search', placeholder: t('admlog.player'), 'aria-label': t('admlog.player') });
  const reloadBtn = h('button', { type: 'button', class: 'secondary', i18n: 'rcon.refresh' });
  const pathLbl = h('small', { class: 'hint' });
  const capNote = h('p', { class: 'hint', hidden: true });
  const list = h('div', { class: 'adm-list' });
  const LIMIT = 500;
  let keyOf = () => '';
  let loadSeq = 0;
  const setFilters = (on) => { typeSel.disabled = playerInp.disabled = fileSel.disabled = !on; };
  const openLaunch = async () => {
    await navigate('server');
    const el = document.getElementById('srv-launch');
    if (el) el.scrollIntoView({ block: 'start' });
  };

  async function load() {
    const seq = ++loadSeq; // a slower, older answer must not overwrite a newer one
    const q = new URLSearchParams();
    if (typeSel.value) q.set('type', typeSel.value);
    if (playerInp.value.trim()) q.set('player', playerInp.value.trim());
    if (fileSel.value) q.set('file', fileSel.value);
    q.set('limit', String(LIMIT));
    let r;
    try { r = await api.get('/api/admlog/recent?' + q.toString()); }
    catch (err) { if (seq === loadSeq) handleErr(err); return; }
    if (seq !== loadSeq || myNav !== _navSeq) return;
    list.innerHTML = '';
    pathLbl.textContent = r.path || '';
    capNote.hidden = true;
    if (!r.path) {
      // Nothing to filter yet: say why, and where the switch is.
      setFilters(false);
      list.append(h('div', { class: 'empty-state' }, [
        h('div', { class: 'es-icon', html: svgIcon('i-admlog', 34) }),
        h('div', { class: 'es-title', i18n: 'admlog.empty.title' }),
        h('div', { class: 'es-hint', i18n: 'admlog.empty.hint' }),
        h('div', { class: 'actions' }, [h('button', { type: 'button', class: 'secondary', i18n: 'server.launch.title', onclick: openLaunch })]),
      ]));
      return;
    }
    setFilters(true);
    const events = r.events || [];
    if (!events.length) { list.append(h('p', { class: 'hint', i18n: 'admlog.noEvents' })); return; }
    for (const e of events.slice().reverse()) list.append(admEventRow(e, { keyOf }));
    if (events.length >= LIMIT) { capNote.textContent = tn('admlog.capped', { n: LIMIT }); capNote.hidden = false; }
  }

  // Earlier runs' files come from the log list; names link to the profiles.
  async function loadContext() {
    const [srcs, pl] = await Promise.all([
      api.get('/api/logs/list').catch(() => []),
      api.get('/api/players').catch(() => null),
    ]);
    if (myNav !== _navSeq) return;
    if (pl) keyOf = playerKeyLookup(pl.players);
    const prev = (srcs || []).filter(s => s.prev && s.kind === 'adm');
    fileSel.append(h('option', { value: '', i18n: 'admlog.file.current' }),
      ...prev.map(s => h('option', { value: s.id.slice(s.id.indexOf(':') + 1), text: tn('admlog.file.prev', { date: fmtDateTime(s.started) }) })));
    fileSel.hidden = !prev.length;
  }

  let playerT = 0;
  reloadBtn.onclick = load;
  typeSel.onchange = load;
  fileSel.onchange = load;
  playerInp.addEventListener('input', () => { clearTimeout(playerT); playerT = setTimeout(load, 300); });

  root.append(pageHeader('nav.admlog', 'admlog.hint'));
  { const w = adminLogWarning(); if (w) root.append(w); }
  root.append(h('div', { class: 'card' }, [
    h('div', { class: 'toolbar' }, [typeSel, fileSel, playerInp, reloadBtn, pathLbl]),
    list,
    capNote,
  ]));
  await loadContext();
  if (myNav !== _navSeq) return;
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
    placeholder: t('rcon.password'), 'data-help': 'rcon.password', 'aria-label': t('rcon.password'),
    autocomplete: 'new-password' });
  const eye = h('button', { type: 'button', class: 'icon-btn eye-btn', title: t('rcon.showPassword'),
    'aria-label': t('rcon.showPassword'), text: '👁',
    onclick: () => { passInp.type = passInp.type === 'password' ? 'text' : 'password'; } });
  const portInp = h('input', { type: 'number', value: cfg.rconPort || '', 'aria-label': t('rcon.port'),
    placeholder: Number(cfg.serverPort) ? (Number(cfg.serverPort) + 4) : 'auto' });
  // The badge says what the password does right now (loadLive refines it).
  const accessBadge = h('span', { class: 'badge mute' });
  const setBadge = (cls, key) => { accessBadge.className = 'badge ' + cls; accessBadge.dataset.i18n = key; accessBadge.textContent = t(key); };
  if (!cfg.rconPassword) setBadge('mute', 'rcon.access.notset');
  else setBadge(State.serverStatus.running ? 'ok' : 'info', State.serverStatus.running ? 'rcon.access.active' : 'rcon.access.atStart');
  // The long how-it-works hint matters only until a password is set.
  const setupHint = h('p', { class: 'hint', i18n: 'rcon.notConfigured.hint', hidden: !!cfg.rconPassword });
  let configured = !!cfg.rconPassword;
  // What the running BattlEye actually uses: it loads its password when the
  // server starts, so a newer one saved here waits for the next restart.
  const liveNote = h('div', { class: 'rcon-live hidden' });
  async function loadLive() {
    let st;
    try { st = await api.get('/api/rcon/status'); } catch { return; }
    if (myNav !== _navSeq) return;
    configured = !!(st.configured || cfg.rconPassword);
    if (!configured) setBadge('mute', 'rcon.access.notset');
    else if (!st.running) setBadge('info', 'rcon.access.atStart');
    else if (st.errorKey === 'rcon.err.auth') setBadge('err', 'rcon.err.auth.short');
    else if (st.pending) setBadge('warn', 'rcon.access.pending');
    else setBadge('ok', 'rcon.access.active');
    setupHint.hidden = configured;
    liveNote.innerHTML = '';
    const lines = [];
    if (st.running && st.activeFile) {
      const [pre, post = ''] = t('rcon.live.file').split('{file}');
      lines.push(h('p', { class: 'hint' }, [
        pre, h('code', { text: st.activeFile }), post,
        st.activePort ? ' · ' + t('rcon.port') + ' ' + st.activePort : null,
      ]));
    }
    if (st.running && st.pending) {
      lines.push(h('div', { class: 'warning-bar' }, [h('span', { i18n: 'rcon.live.pending' })]));
    }
    if (st.running && st.errorKey === 'rcon.err.auth') {
      lines.push(h('div', { class: 'warning-bar err-bar' }, [h('span', { i18n: 'rcon.err.auth' })]));
    }
    liveNote.append(...lines);
    liveNote.classList.toggle('hidden', !lines.length);
  }

  // Labelled fields with the beserver_x64.cfg key each one writes.
  const labelled = (cls, input, body, key, cfgKey) => h('div', { class: 'field ' + cls }, [
    h('div', { class: 'cfg-label' }, [h('label', { for: assocId(input), i18n: key }), h('code', { class: 'cfg-key', text: cfgKey })]),
    body,
  ]);
  const accessCard = h('div', { class: 'card' }, [
    h('div', { class: 'card-head' }, [cardTitle('i-shield', 'rcon.access.title'), accessBadge]),
    h('div', { class: 'row rcon-access-row' }, [
      labelled('rcon-pass', passInp, h('div', { class: 'secret-wrap' }, [passInp, eye]), 'rcon.password', 'RConPassword'),
      labelled('rcon-port', portInp, portInp, 'rcon.port', 'RConPort'),
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
    setupHint,
    liveNote,
  ]);

  // --- Who is online: a count and the way to the full list. Kick, ban and
  // private messages live on Players › Online now (one place, one table).
  const statusBody = h('div');
  let statusSig = '';
  const openOnline = () => { try { localStorage.setItem('playersTab', 'online'); } catch {} navigate('players'); };
  function showStatus(kind, n, errText) {
    const sig = kind + ':' + n + ':' + (errText || '') + ':' + configured;
    if (sig === statusSig) return; // no rebuild every poll — a focused button would lose focus
    statusSig = sig;
    statusBody.innerHTML = '';
    if (kind === 'off') {
      // Not usable yet: say what is missing — the password first, then a start.
      statusBody.append(configured
        ? emptyBox('i-server', 'online.stopped.title', 'rcon.off.stopped.hint', [startServerBtn(() => refresh())])
        : emptyBox('i-rcon', 'rcon.off.noPassword.title', 'rcon.off.noPassword.hint'));
    } else if (kind === 'ok') {
      statusBody.append(h('div', { class: 'rcon-online' }, [
        h('div', {}, [
          h('div', { class: 'metric-num', text: String(n) }),
          h('div', { class: 'hint', text: tn('rcon.online.count', { n }) }),
        ]),
        h('span', { class: 'grow' }),
        h('button', { type: 'button', class: 'secondary', i18n: 'rcon.online.open', onclick: openOnline }),
      ]), h('p', { class: 'hint', i18n: 'rcon.online.hint' }));
    } else {
      statusBody.append(h('p', { class: 'hint', text: errText }));
    }
  }

  // --- Raw console. Keeps an echoed transcript + ArrowUp/Down history, like a
  // real terminal — output no longer overwrites the previous command's.
  const cmdInp = h('input', { type: 'text', placeholder: t('rcon.rawCommand.ph'), 'aria-label': t('rcon.rawCommand') });
  const cmdBtn = h('button', { i18n: 'rcon.send', onclick: () => doCmd() });
  const cmdOut = h('pre', { class: 'log-pane', style: { height: '180px' }, hidden: true });
  // Everyday read-only / reversible commands, put into the line (Enter runs).
  const chips = h('div', { class: 'rcon-chips' }, [
    h('span', { class: 'hint', i18n: 'rcon.chips.label' }),
    ...['players', 'bans', '#lock', '#unlock'].map(c => h('button', { type: 'button', class: 'pill mono', text: c, title: t('rcon.chip.hint'),
      onclick: () => { cmdInp.value = c; cmdInp.focus(); } })),
  ]);
  // Live tools work only while the server runs with a password BattlEye took.
  function setLive(on) {
    cmdInp.disabled = cmdBtn.disabled = !on;
    chips.querySelectorAll('button').forEach(b => { b.disabled = !on; });
    cmdInp.placeholder = t(on ? 'rcon.rawCommand.ph' : 'rcon.rawCommand.off');
    cmdOut.hidden = !on && !cmdOut.textContent;
  }

  async function refresh() {
    // If the server isn't running there's nothing to connect to: say so and
    // keep the poll alive (it costs nothing while down and auto-recovers once
    // the server is up).
    try {
      if (!State.serverStatus.running) {
        setLive(false);
        showStatus('off', 0);
        return;
      }
      try {
        const d = await api.get('/api/rcon/players');
        if (myNav !== _navSeq) return;
        setLive(true);
        showStatus('ok', d.count || (d.players || []).length);
      } catch (e) {
        const key = e.data && e.data.error;
        // No password / a rejected one: commands would fail the same way.
        setLive(key !== 'rcon.err.notConfigured' && key !== 'rcon.err.auth');
        // No password anywhere yet — the access card above sets one;
        // rejected password / no answer: translated, with what to do.
        showStatus('err', 0, key === 'rcon.err.notConfigured' ? t('rcon.notActiveYet') : key ? t(key) : `RCon: ${e.message || ''}`);
      }
    } finally {
      await loadLive();
      // Poll regardless of the first outcome — a transient failure right after
      // server start (BattlEye still booting) used to leave the page dead
      // until a manual refresh, because polling only armed on success.
      startPolling();
    }
  }

  const playersCard = h('div', { class: 'card' }, [
    h('div', { class: 'card-head' }, [
      cardTitle('i-players', 'perf.players'),
      h('button', { class: 'small', i18n: 'rcon.refresh', onclick: refresh }),
    ]),
    statusBody,
  ]);
  // Messages to players are written on the Chat page (live feed, quick messages).
  const chatLink = h('div', { class: 'card rcon-chat-link' }, [
    h('span', { class: 'card-title-icon', html: svgIcon('i-chat', 16) }),
    h('span', { class: 'grow', i18n: 'rcon.toChat' }),
    h('button', { type: 'button', class: 'secondary small', i18n: 'rcon.toChat.open', onclick: () => navigate('chat') }),
  ]);
  const consoleCard = h('div', { class: 'card' }, [
    cardTitle('i-rcon', 'rcon.rawCommand'),
    chips,
    h('div', { class: 'row' }, [cmdInp, cmdBtn]),
    h('div', { style: { marginTop: '10px' } }, [cmdOut]),
  ]);

  // Commands that end sessions or ban: one Enter (or an ArrowUp recall) must
  // not be enough.
  const DANGER = [
    [/^#shutdown\b/i, 'rcon.cmd.shutdown'],
    [/^#restart/i, 'rcon.cmd.restart'],
    [/^#exec\b/i, 'rcon.cmd.exec'],
    [/^#?kick\b/i, 'rcon.cmd.kick'],
    [/^(#?ban|addban)\b/i, 'rcon.cmd.ban'],
  ];
  const cmdHist = [];
  let histIdx = -1;
  async function doCmd() {
    const cmd = cmdInp.value.trim();
    if (!cmd || cmdInp.disabled) return;
    const danger = DANGER.find(([re]) => re.test(cmd));
    if (danger && !(await confirmModal(tn(danger[1], { cmd: cmd.split(/\s+/)[0] }), { danger: true, okText: t('rcon.cmd.run') }))) return;
    cmdHist.push(cmd); histIdx = cmdHist.length;
    cmdInp.value = '';
    cmdOut.hidden = false;
    cmdOut.textContent += (cmdOut.textContent ? '\n' : '') + '> ' + cmd + '\n';
    try {
      const r = await api.post('/api/rcon/command', { command: cmd });
      cmdOut.textContent += (r.output || t('rcon.noOutput')) + '\n';
    } catch (e) { cmdOut.textContent += `ERR: ${e.message}\n`; }
    cmdOut.scrollTop = cmdOut.scrollHeight;
  }
  cmdInp.addEventListener('keydown', e => {
    if (e.key === 'Enter') { e.preventDefault(); doCmd(); }
    else if (e.key === 'ArrowUp' && cmdHist.length) { e.preventDefault(); histIdx = Math.max(0, histIdx - 1); cmdInp.value = cmdHist[histIdx] || ''; }
    else if (e.key === 'ArrowDown' && cmdHist.length) { e.preventDefault(); histIdx = Math.min(cmdHist.length, histIdx + 1); cmdInp.value = cmdHist[histIdx] || ''; }
  });

  // Access card first (always usable), then the live tools below it.
  root.append(pageHeader('nav.rcon', 'rcon.subtitle'), accessCard, playersCard, chatLink, consoleCard);
  root._teardown = stopPolling; // before the await — see logs/battleye above
  await refresh();
};

// --------------------------------------------------------------------- events

Views.events = async (root) => {
  const myNav = _navSeq;
  const running = !!State.serverStatus.running;
  const search = h('input', { type: 'search', placeholder: t('events.search'), 'aria-label': t('events.search') });
  const chips = h('div', { class: 'pills evl-cats' });
  const tableWrap = h('div', { class: 'evl-table-wrap' });
  const foot = h('div', { class: 'evl-foot hint' });
  const problemsHost = h('div');
  let all = [];
  let cat = 'all';
  let sortKey = 'name';
  let sortDir = 1;
  try { const s = JSON.parse(localStorage.getItem('eventsView') || '{}'); cat = s.cat || 'all'; sortKey = s.key || 'name'; sortDir = s.dir || 1; } catch {}
  const remember = () => { try { localStorage.setItem('eventsView', JSON.stringify({ cat, key: sortKey, dir: sortDir })); } catch {} };

  // Categories come from DayZ's naming (AnimalDeer, InfectedCity, VehicleSedan02, …).
  const isVanillaFile = (f) => !f || f.toLowerCase() === 'db/events.xml';
  const CATS = [
    ['all', () => true],
    ['animals', r => /^(Ambient|Animal)/i.test(r.name)],
    ['infected', r => /^Infected/i.test(r.name)],
    ['vehicles', r => /^Vehicle/i.test(r.name)],
    ['static', r => /^Static/i.test(r.name)],
    ['trajectory', r => /^Trajectory/i.test(r.name)],
    ['other', r => !/^(Ambient|Animal|Infected|Vehicle|Static|Trajectory)/i.test(r.name)],
    ['mods', r => !isVanillaFile(r.file)],
  ];
  const catOf = (k) => k === 'problems' ? (r => problemsOf(r).length > 0) : (CATS.find(c => c[0] === k) || CATS[0])[1];
  // Which kind an event is, for its icon: paw, skull, car, flame, route.
  const KIND_ICON = { animals: 'i-paw', infected: 'i-skull', vehicles: 'i-vehicles', static: 'i-flame', trajectory: 'i-route', other: 'i-events' };
  const kindOf = (r) => (CATS.slice(1, 7).find(([, fn]) => fn(r)) || ['other'])[0];
  // What keeps an event from working as meant: a fixed-position event that
  // is on but has no spawn points never spawns; a name defined twice is a
  // duplicate DayZ resolves unpredictably.
  const problemsOf = (r) => {
    const out = [];
    if (r.active && pointsMode(r) === 'points' && !r.points && (Number(r.nominal) || 0) > 0) out.push('noPoints');
    if (r.dup) out.push('dup');
    return out;
  };
  // Spawn points only exist for "fixed" events; animals take theirs from the
  // env territory files, and "player"/"uniform" events need none.
  const pointsMode = (r) => {
    const pos = r.position || 'fixed';
    if (pos === 'player') return 'player';
    if (pos === 'uniform') return 'uniform';
    if (/^(Ambient|Animal)/i.test(r.name)) return 'territory';
    return 'points';
  };

  const SORTS = {
    name: r => r.name.toLowerCase(),
    nominal: r => r.nominal ?? -1,
    max: r => r.max ?? -1,
    lifetime: r => r.lifetime ?? -1,
    points: r => r.points || 0,
    children: r => r.children || 0,
  };

  function renderChips() {
    chips.innerHTML = '';
    const nProblems = all.filter(r => problemsOf(r).length).length;
    if (cat === 'problems' && !nProblems) cat = 'all';
    for (const [k, fn] of [...CATS, ['problems', null]]) {
      const n = k === 'problems' ? nProblems : all.filter(fn).length;
      if (!n && k !== 'all') continue;
      chips.append(h('button', { type: 'button', class: 'pill' + (cat === k ? ' active' : '') + (k === 'problems' ? ' warn' : ''),
        onclick: () => setCat(k) },
      [h('span', { text: t(k === 'problems' ? 'vehicles.tab.problems' : 'events.cat.' + k) }), h('span', { class: 'pill-count', text: String(n) })]));
    }
  }
  function setCat(k) { cat = k; remember(); renderChips(); renderTable(); }

  // At a glance: how many kinds, how many on, how much is on the map, and
  // what needs a look (a click shows just those).
  const summary = h('div', { class: 'veh-summary evl-summary' });
  function renderSummary() {
    summary.innerHTML = '';
    const on = all.filter(r => r.active);
    const onMap = on.reduce((s, r) => s + (Number(r.nominal) || 0), 0);
    const bad = all.filter(r => problemsOf(r).length).length;
    const tile = (key, value, kind, onClick) => h(onClick ? 'button' : 'div', {
      type: onClick ? 'button' : null, class: 'veh-tile' + (kind ? ' ' + kind : '') + (onClick ? ' clickable' : ''), onclick: onClick || null }, [
      h('div', { class: 'veh-tile-v', text: String(value) }), h('div', { class: 'veh-tile-k', i18n: key }),
    ]);
    summary.append(
      tile('events.sum.kinds', all.length),
      tile('events.sum.active', on.length + ' / ' + all.length),
      tile('events.sum.onMap', onMap, 'accent'),
      tile('vehicles.sum.problems', bad, bad ? 'warn' : 'ok', bad ? () => setCat('problems') : null),
    );
    applyI18n();
  }

  function renderTable() {
    tableWrap.innerHTML = '';
    const q = search.value.trim().toLowerCase();
    const fn = catOf(cat);
    const rows = all.filter(r => fn(r) && (!q || r.name.toLowerCase().includes(q) || (r.file || '').toLowerCase().includes(q)));
    const key = SORTS[sortKey] ? sortKey : 'name';
    rows.sort((a, b) => {
      const x = SORTS[key](a), y = SORTS[key](b);
      return (x < y ? -1 : x > y ? 1 : 0) * sortDir || a.name.localeCompare(b.name);
    });
    const th = (k, label, cls, helpKey) => {
      const el = h('th', { class: (cls || '') + (SORTS[k] ? ' sortable' : '') + (sortKey === k ? ' sorted' : ''),
        'aria-sort': sortKey === k ? (sortDir > 0 ? 'ascending' : 'descending') : null,
        onclick: SORTS[k] ? () => { if (sortKey === k) sortDir = -sortDir; else { sortKey = k; sortDir = k === 'name' ? 1 : -1; } remember(); renderTable(); } : null },
      [h('span', { i18n: label })]);
      return helpKey ? hlp(el, helpKey) : el;
    };
    const tbl = h('table', { class: 'evl-table' });
    tbl.append(h('thead', {}, h('tr', {}, [
      th('name', 'col.name'),
      th('nominal', 'events.col.nominal', 'num', 'events.nominal'),
      th('max', 'events.col.minmax', 'num evl-hide-sm', 'events.minmax'),
      th('lifetime', 'events.col.lifetime', 'num evl-hide-sm', 'events.lifetime'),
      th('', 'events.field.active', 'evl-c', 'events.active'),
      th('points', 'events.col.points', 'num evl-hide-sm'),
      th('children', 'events.col.children', 'num evl-hide-sm', 'events.children'),
      h('th', { class: 'evl-act', 'aria-label': t('action.edit') }),
    ])));
    const tbody = h('tbody');
    for (const r of rows) {
      const off = !r.active;
      const kind = kindOf(r);
      const probs = problemsOf(r);
      const nameCell = h('td', { class: 'evl-name' }, [
        h('span', { class: 'evl-icon k-' + kind, html: svgIcon(KIND_ICON[kind], 15), 'aria-hidden': 'true' }),
        h('span', { class: 'mono', text: r.name }),
        probs.includes('noPoints') ? h('span', { class: 'badge warn', i18n: 'events.noPoints', title: t('events.points.none') }) : null,
        !isVanillaFile(r.file) ? h('span', { class: 'badge mute mono evl-file', text: r.file, title: r.file }) : null,
        r.dup ? h('span', { class: 'badge warn', i18n: 'events.dup', title: t('events.dup.hint') }) : null,
        // Phone: the hidden columns as one line under the name.
        h('span', { class: 'evl-sub hint', text: [
          (r.min ?? '?') + '–' + (r.max ?? '?'), fmtLifetime(r.lifetime),
          pointsMode(r) === 'points' ? t('events.col.points') + ': ' + (r.points || 0) : null].filter(Boolean).join(' · ') }),
      ]);
      const mode = pointsMode(r);
      const pts = mode === 'points'
        ? h('button', { type: 'button', class: 'link-btn' + (r.points ? '' : ' warn-text'), text: String(r.points || 0),
          title: r.points ? t('events.points.open') : t('events.points.none'),
          onclick: (e) => { e.stopPropagation(); window._eventmapTarget = r.name; navigate('eventmap'); } })
        : h('span', { class: 'hint', text: '—', title: t('events.pos.' + mode) });
      const edit = h('button', { type: 'button', class: 'icon-btn', title: t('action.edit'), 'aria-label': t('action.edit') + ' ' + r.name,
        html: svgIcon('i-edit', 15), onclick: (e) => { e.stopPropagation(); openEditor(r); } });
      tbody.append(h('tr', { class: 'evl-row' + (off ? ' off' : ''), tabindex: '0',
        onclick: () => openEditor(r), onkeydown: (e) => { if (e.key === 'Enter') openEditor(r); } }, [
        nameCell,
        h('td', { class: 'num strong', text: r.nominal ?? '' }),
        h('td', { class: 'num evl-hide-sm', text: (r.min ?? '') + '–' + (r.max ?? '') }),
        h('td', { class: 'num evl-hide-sm', text: fmtLifetime(r.lifetime), title: r.lifetime != null ? r.lifetime + ' s' : '' }),
        h('td', { class: 'evl-c' }, off ? h('span', { class: 'badge mute', i18n: 'events.state.off' })
          : h('span', { class: 'evl-dot', title: t('events.state.on'), 'aria-label': t('events.state.on') })),
        h('td', { class: 'num evl-hide-sm' }, pts),
        h('td', { class: 'num evl-hide-sm', text: r.children || 0 }),
        h('td', { class: 'evl-act' }, edit),
      ]));
    }
    tbl.append(tbody);
    if (!rows.length) {
      tableWrap.append(h('div', { class: 'empty-state' }, [h('div', { class: 'es-hint', i18n: q ? 'events.none.search' : 'events.none' })]));
    } else {
      tableWrap.append(h('div', { class: 'table-scroll-narrow' }, tbl));
    }
    const spawning = rows.filter(r => r.active).reduce((s, r) => s + (Number(r.nominal) || 0), 0);
    foot.textContent = tn('events.count', { n: rows.length, total: all.length, nominal: spawning });
  }

  async function load() {
    let d;
    try { d = await api.get('/api/events'); }
    catch (e) { tableWrap.innerHTML = ''; tableWrap.append(h('p', { class: 'hint', text: e.message })); return; }
    if (myNav !== _navSeq) return;
    all = d.events || [];
    renderSummary();
    problemsHost.innerHTML = '';
    if (d.problems && d.problems.length) {
      problemsHost.append(h('div', { class: 'warning-bar' }, [h('span', { text: t('events.problems') + ' ' + d.problems.join('; ') })]));
    }
    renderChips();
    renderTable();
    // Deep link from the event-spawn map ("Open in Events"): its settings.
    const target = window._eventsTarget;
    window._eventsTarget = null;
    const row = target ? all.find(r => r.name === target) : null;
    if (row) openEditor(row);
  }

  // ---- editor
  let classDL = null;
  async function classList() {
    if (classDL) return;
    classDL = h('datalist', { id: 'dz-event-classes' });
    document.body.append(classDL);
    let names = [];
    try { names = (await api.get('/api/spawnable/classnames')).names || []; } catch {}
    for (const n of names) classDL.append(h('option', { value: n }));
  }

  async function openEditor(row) {
    const name = row.name;
    const fileQ = isVanillaFile(row.file) ? '' : '&file=' + encodeURIComponent(row.file);
    let ev;
    try { ev = await api.get(`/api/events/item?name=${encodeURIComponent(name)}${fileQ}`); }
    catch (e) { handleErr(e); return; }
    classList();
    // The item comes as the Go struct (Nominal, Min, …); accept either case.
    const val = (k) => ev[k] ?? ev[k.charAt(0).toUpperCase() + k.slice(1)];
    let dirty = false;
    const m = openModal({ title: name, wide: true, isDirty: () => dirty });
    m.body.addEventListener('input', () => { dirty = true; });
    m.body.addEventListener('change', () => { dirty = true; });

    const inputs = {};
    const grid = h('div', { class: 'grid-3 evl-fields' });
    const NUMS = [
      ['nominal', 'vehicles.f.nominal', 'events.nominal'],
      ['min', 'vehicles.f.min', 'events.minmax'],
      ['max', 'vehicles.f.max', 'events.minmax'],
      ['lifetime', 'vehicles.f.lifetime', 'events.lifetime'],
      ['restock', 'vehicles.f.restock', 'events.restock'],
    ];
    for (const [k, label, helpKey] of NUMS) {
      const el = h('input', { type: 'number', min: '0', value: val(k) ?? '', disabled: running });
      inputs[k] = el;
      grid.append(h('div', { class: 'field' }, [
        h('div', { class: 'cfg-label' }, [withHelp(h('label', { for: assocId(el), i18n: label }), helpKey), h('code', { class: 'cfg-key', text: k })]),
        el,
      ]));
    }
    const sw = (k, label, helpKey) => {
      const cb = h('input', { type: 'checkbox', class: 'switch', disabled: running });
      cb.checked = Number(val(k)) === 1;
      inputs[k] = cb;
      return h('label', { class: 'cfg-bool' }, [cb, h('span', { class: 'cfg-bool-text' }, [
        h('span', { class: 'cfg-bool-label' }, [h('span', { i18n: label }), help(helpKey)]), h('code', { class: 'cfg-key', text: k })])]);
    };
    const bools = h('div', { class: 'cfg-bools evl-bools' }, [sw('active', 'vehicles.activeOn', 'events.active'), sw('saveable', 'events.f.saveable', 'events.saveable')]);

    // Children: the classes the event spawns.
    const original = (ev.Children && ev.Children.Child) ? ev.Children.Child : [];
    const children = original.map(c => ({ ...c }));
    const childrenWrap = h('div', { class: 'evl-kids' });
    function renderChildren() {
      childrenWrap.innerHTML = '';
      if (!children.length) childrenWrap.append(h('p', { class: 'hint', i18n: 'events.kids.none' }));
      children.forEach((c, i) => {
        const num = (k, label, def) => {
          const el = h('input', { type: 'number', value: c[k] ?? def, disabled: running, 'aria-label': t(label) });
          el.oninput = () => { c[k] = Number(el.value) || 0; };
          return h('label', { class: 'evl-kid-num' }, [h('span', { i18n: label }), el]);
        };
        const typeIn = h('input', { type: 'text', class: 'mono', value: c.Type || '', list: 'dz-event-classes', spellcheck: 'false',
          placeholder: t('events.child.class.ph'), disabled: running, 'aria-label': t('events.child.type') });
        typeIn.oninput = () => { c.Type = typeIn.value.trim(); };
        childrenWrap.append(h('div', { class: 'evl-kid' }, [
          typeIn,
          num('Min', 'events.child.min', 0), num('Max', 'events.child.max', -1),
          num('LootMin', 'events.child.lootmin', 0), num('LootMax', 'events.child.lootmax', 0),
          h('button', { type: 'button', class: 'icon-btn danger-text', title: t('action.delete'), 'aria-label': t('action.delete'), text: '×', disabled: running,
            onclick: () => { children.splice(i, 1); dirty = true; renderChildren(); } }),
        ]));
      });
      if (!running) {
        childrenWrap.append(h('button', { type: 'button', class: 'small secondary', i18n: 'events.addChild',
          onclick: () => { children.push({ Type: '', Min: 1, Max: -1, LootMin: 0, LootMax: 0 }); dirty = true; renderChildren(); } }));
      }
    }
    renderChildren();

    const save = async (btn) => {
      const body = { Name: name };
      for (const [k] of NUMS) {
        const v = inputs[k].value.trim();
        if (v !== '') body[k.charAt(0).toUpperCase() + k.slice(1)] = Number(v);
      }
      body.Active = inputs.active.checked ? 1 : 0;
      body.Saveable = inputs.saveable.checked ? 1 : 0;
      // An emptied list must reach the server too, or the old rows stay.
      if (children.length || original.length) body.Children = { Child: children.filter(c => (c.Type || '').trim()) };
      await withBusy(btn, async () => {
        try {
          await api.post(`/api/events/item?name=${encodeURIComponent(name)}${fileQ}`, body);
          toast(t('msg.saved'), 'ok');
          dirty = false;
          m.close();
          await load();
        } catch (e) { handleErr(e); }
      });
    };
    const del = async () => {
      if (!(await confirmModal(t('confirm.deleteEvent').replace('{name}', name), { danger: true, okText: t('action.delete') }))) return;
      try {
        await api.del(`/api/events/item?name=${encodeURIComponent(name)}${fileQ}`);
        toast(t('msg.deleted'), 'ok');
        dirty = false;
        m.close();
        await load();
      } catch (e) { handleErr(e); }
    };

    const links = h('div', { class: 'evl-links' }, [
      !isVanillaFile(row.file) ? h('span', { class: 'badge mute mono', text: row.file }) : null,
      pointsMode(row) === 'points'
        ? h('button', { type: 'button', class: 'small secondary', onclick: () => { dirty = false; m.close(); window._eventmapTarget = name; navigate('eventmap'); } },
          [h('span', { html: svgIcon('i-eventmap', 14) }), h('span', { text: ' ' + t('events.points.btn').replace('{n}', row.points || 0) })])
        : h('span', { class: 'hint', text: t('events.pos.' + pointsMode(row)) }),
      /^Vehicle/i.test(name) ? h('button', { type: 'button', class: 'small secondary', i18n: 'events.openVehicles',
        onclick: () => { dirty = false; m.close(); navigate('vehicles'); } }) : null,
    ].filter(Boolean));

    m.body.append(...[
      running ? h('div', { class: 'warning-bar', i18n: 'guard.serverRunning' }) : null,
      links,
      grid,
      bools,
      h('h4', { class: 'sub-title evl-kids-title' }, [h('span', { i18n: 'events.field.children' }), help('events.children')]),
      childrenWrap,
      h('div', { class: 'actions' }, [
        running ? null : h('button', { type: 'button', class: 'danger-text', i18n: 'action.delete', onclick: del }),
        h('span', { class: 'grow' }),
        h('button', { type: 'button', i18n: 'action.cancel', onclick: () => m.close() }),
        running ? null : h('button', { type: 'button', class: 'primary', i18n: 'action.save', onclick: (e) => save(e.currentTarget) }),
      ].filter(Boolean)),
    ].filter(Boolean));
  }

  async function newEvent() {
    const name = await promptModal(t('events.new.prompt'), { title: t('events.new'), placeholder: 'VehicleMyCar' });
    if (!name) return;
    if (!/^[A-Za-z0-9_]+$/.test(name)) { toast(t('events.new.bad'), 'error'); return; }
    if (all.some(r => r.name.toLowerCase() === name.toLowerCase())) { toast(t('events.new.exists'), 'error'); return; }
    try {
      await api.post(`/api/events/item?name=${encodeURIComponent(name)}`,
        { Name: name, Nominal: 0, Min: 0, Max: 0, Lifetime: 300, Restock: 0, Saveable: 0, Active: 0 });
      await load();
      const row = all.find(r => r.name === name);
      if (row) openEditor(row);
    } catch (e) { handleErr(e); }
  }

  let _searchT = 0;
  search.oninput = () => { clearTimeout(_searchT); _searchT = setTimeout(renderTable, 200); };

  root.append(pageHeader('nav.events', 'events.hint', [
    running ? null : h('button', { type: 'button', class: 'primary', onclick: newEvent }, [h('span', { html: svgIcon('i-plus', 15) }), h('span', { text: ' ' + t('events.new') })]),
    revealBtn({ kind: 'missionfile', path: 'db/events.xml' }),
  ].filter(Boolean)));
  if (running) root.append(runningBanner());
  root.append(
    problemsHost,
    summary,
    h('div', { class: 'card' }, [
      h('div', { class: 'players-toolbar evl-toolbar' }, [search, chips]),
      tableWrap,
      foot,
    ]),
  );
  await load();
};

// --------------------------------------------------------------------- settings

// hhmm normalises a stored time ("6:00", "06:00") to what <input type=time>
// accepts ("06:00"); anything else shows empty rather than a wrong time.
function hhmm(v) {
  const m = /^(\d{1,2}):(\d{2})$/.exec(String(v || '').trim());
  return m && Number(m[1]) < 24 && Number(m[2]) < 60 ? m[1].padStart(2, '0') + ':' + m[2] : '';
}

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
      if (hhmm(tm)) times[i] = hhmm(tm); // "6:00" → "06:00", as the field shows it
      const inp = h('input', { type: 'time', value: hhmm(tm), style: { width: '120px' } });
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
    cardTitle('i-clock', 'settings.restarts.title', 'settings.restart'),
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

// scheduledProfilesCard — apply a saved config profile at a daily HH:MM (the
// swap runs inside a restart). Rows are {time, profile, enabled}; the getter on
// F is picked up by the main Settings save, like scheduledRestarts.
function scheduledProfilesCard(c, F, onChange) {
  const fire = () => onChange && onChange();
  const state = {
    items: (c.scheduledProfiles || []).map(a => ({
      time: a.time || '', profile: a.profile || '', enabled: !!a.enabled,
    })),
  };
  let profiles = []; // {slug, name}

  const card = h('div', { class: 'card' }, [
    cardTitle('i-cfgprofiles', 'settings.schedprofiles.title'),
    h('p', { class: 'hint', i18n: 'settings.schedprofiles.hint' }),
  ]);
  if (!(c.rconPassword || '').trim()) {
    card.append(h('p', { class: 'warning-bar', i18n: 'settings.rconNeeded' }));
  }
  const list = h('div');
  card.append(list);

  function profileSelect(cur, onch) {
    const sel = h('select', { style: { flex: '1' }, 'aria-label': t('settings.schedprofiles.title') });
    sel.append(h('option', { value: '', text: '—' }));
    for (const p of profiles) {
      const o = h('option', { value: p.slug, text: p.name || p.slug });
      if (p.slug === cur) o.selected = true;
      sel.append(o);
    }
    if (cur && !profiles.some(p => p.slug === cur)) { // a deleted profile — keep it visible
      const o = h('option', { value: cur, text: cur });
      o.selected = true;
      sel.append(o);
    }
    sel.onchange = onch;
    return sel;
  }

  function render() {
    list.innerHTML = '';
    if (!profiles.length) list.append(h('p', { class: 'hint', i18n: 'settings.schedprofiles.none' }));
    state.items.forEach((a, i) => {
      if (hhmm(a.time)) state.items[i].time = hhmm(a.time);
      const time = h('input', { type: 'time', value: hhmm(a.time), style: { width: '120px' } });
      const sel = profileSelect(a.profile, () => { state.items[i].profile = sel.value; fire(); });
      const en = h('input', { type: 'checkbox', class: 'switch' });
      en.checked = !!a.enabled;
      time.onchange = () => { state.items[i].time = time.value.trim(); };
      en.onchange = () => { state.items[i].enabled = en.checked; };
      list.append(h('div', { class: 'row', style: { gap: '8px', marginBottom: '6px' } }, [
        time, sel, h('label', {}, [en]),
        h('button', { class: 'danger', text: '×', onclick: () => { state.items.splice(i, 1); render(); fire(); } }),
      ]));
    });
    applyI18n();
  }
  render();
  api.get('/api/config-profiles')
    .then(d => { profiles = (d.profiles || []).map(p => ({ slug: p.slug, name: p.name })); render(); })
    .catch(() => {});

  F.scheduledProfiles = {
    type: 'list-schedprofiles',
    get value() {
      return state.items
        .map(a => ({ time: (a.time || '').trim(), profile: (a.profile || '').trim(), enabled: !!a.enabled }))
        .filter(a => a.time && a.profile);
    },
  };

  card.append(h('div', { class: 'actions' }, [
    h('button', { i18n: 'settings.schedprofiles.add',
      onclick: () => { state.items.push({ time: '18:00', profile: '', enabled: true }); render(); fire(); } }),
  ]));
  return card;
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
    cardTitle('i-chat', 'settings.announcements'),
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
      if (hhmm(a.time)) state.items[i].time = hhmm(a.time);
      const time = h('input', { type: 'time', value: hhmm(a.time), style: { width: '120px' } });
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

// Player loadout: see loadout.js.

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
  const tocCard = h('div', { class: 'card', id: 'guide-toc' }, [
    h('h3', { i18n: 'guide.contents' }),
    toc,
  ]);
  root.append(tocCard);
  // A long page: a floating button back to the contents once they scroll away.
  const toTop = h('button', { type: 'button', class: 'guide-to-top', hidden: true, text: t('guide.toTop'),
    onclick: () => tocCard.scrollIntoView({ behavior: 'smooth', block: 'start' }) });
  root.append(toTop);
  if ('IntersectionObserver' in window) {
    const io = new IntersectionObserver((entries) => { for (const en of entries) toTop.hidden = en.isIntersecting; });
    io.observe(tocCard);
    const prevTeardown = root._teardown;
    root._teardown = () => { io.disconnect(); if (prevTeardown) prevTeardown(); };
  }
  const zoom = (src, title) => {
    const m = openModal({ title, wide: true });
    m.body.append(h('img', { src, alt: title, class: 'guide-zoom' }));
  };

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
        h('img', { src: c.image, alt: c.title, loading: 'lazy', decoding: 'async', title: t('guide.zoom'),
          onclick: () => zoom(c.image, c.title),
          // A missing picture hides its frame instead of showing a broken one.
          onerror: (e) => { const f = e.currentTarget.closest('figure'); if (f) f.hidden = true; } }),
      ]) : null,
      steps.length ? h('ol', { class: 'guide-steps' }, steps) : null,
      tips.length ? h('div', { class: 'guide-tips' }, [
        h('div', { class: 'guide-tips-title', i18n: 'guide.tips' }),
        h('ul', {}, tips),
      ]) : null,
      chapters[ci + 1] ? h('button', { type: 'button', class: 'link-btn guide-next',
        text: t('guide.next').replace('{title}', chapters[ci + 1].title),
        onclick: () => { const el = document.getElementById('guide-' + chapters[ci + 1].id); if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' }); } }) : null,
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
  const myNav = _navSeq;
  await refreshStatus();
  const running = State.serverStatus.running;
  let d;
  try { d = await api.get('/api/weather'); }
  catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;
  const params = d.params || {};
  const presets = d.presets || [];
  const tm = d.time || {};

  root.append(pageHeader('nav.weather', 'weather.subtitle', [revealBtn({ kind: 'missionfile', path: 'cfgweather.xml' })]));
  const wrap = h('div');
  root.append(wrap);
  if (running) wrap.append(runningBanner());

  // How gradually weather moves — shared by the presets and the manual
  // values, so it sits with the presets, above both.
  const transSel = h('select', { id: 'weather-trans', 'aria-label': t('weather.trans.title'), disabled: running }, [
    h('option', { value: 'smooth', i18n: 'weather.trans.smooth' }),
    h('option', { value: 'normal', i18n: 'weather.trans.normal' }),
    h('option', { value: 'fast',   i18n: 'weather.trans.fast' }),
  ]);
  transSel.value = params.transition || 'smooth';

  // ---- presets ----
  // "Enabled" (not mere file existence) decides whether the manager controls
  // weather — the vanilla cfgweather.xml ships present but with enable="0".
  const matchedBadge = h('span', { class: 'badge' });
  const disabledNote = h('p', { class: 'hint', i18n: 'weather.disabled' });
  const presetTile = (name) => h('button', { type: 'button', class: 'preset-tile', 'data-name': name, disabled: running, onclick: () => applyPreset(name) }, [
    h('span', { class: 'ic', html: WICON[name] || '' }),
    h('span', { i18n: 'weather.preset.' + name }),
  ]);
  const tiles = presets.map(presetTile);
  tiles.push(presetTile('off'));

  wrap.append(h('div', { class: 'card' }, [
    h('div', { class: 'card-head' }, [cardTitle('i-weather', 'weather.presets.title')]),
    h('p', { class: 'hint', i18n: 'weather.presets.hint' }),
    h('div', { class: 'wx-now' }, [h('span', { class: 'k', i18n: 'weather.current' }), matchedBadge]),
    disabledNote,
    h('div', { class: 'preset-grid' }, tiles),
    // Its own row: the label on one line, what it means under it, the
    // choice beside — shared by the presets and the manual values.
    h('div', { class: 'wx-trans-row' }, [
      h('div', { class: 'wx-trans-k' }, [
        withHelp(h('label', { i18n: 'weather.trans.title', for: assocId(transSel) }), 'weather.transition'),
        h('small', { class: 'hint', i18n: 'weather.trans.hint' }),
      ]),
      transSel,
    ]),
  ]));

  // ---- manual editor ----
  // Generic slider row. `unit` is '%' (value stored 0..1) or 'm/s' (raw value).
  const sliderRow = (labelKey, icon, val, opts = {}) => {
    const unit = opts.unit || '%';
    const max = opts.max || 100;
    const step = opts.step || '1';
    const toDisplay = (v) => unit === '%' ? Math.round((v || 0) * 100) : (v || 0);
    const fmt = (n) => unit === '%' ? n + '%' : n + ' ' + t('unit.mps');
    const out = h('span', { class: 'slider-val' });
    const range = h('input', { type: 'range', class: 'wslider', min: '0', max: String(max), step,
      style: { flex: '1' }, 'aria-label': t(labelKey), disabled: running });
    const fill = () => range.style.setProperty('--val', (Number(range.value) / max * 100) + '%');
    const set = (v) => { range.value = String(toDisplay(v)); out.textContent = fmt(Number(range.value)); fill(); };
    range.oninput = () => { out.textContent = fmt(Number(range.value)); fill(); };
    set(val);
    const row = h('div', { class: 'wrow' }, [
      h('span', { class: 'slider-label' }, [
        h('span', { class: 'ic', html: WICON[icon] || '' }),
        h('span', { i18n: labelKey }),
        opts.help ? help(opts.help) : null,
      ]),
      range, out,
    ]);
    return { row, set, get: () => unit === '%' ? Number(range.value) / 100 : Number(range.value) };
  };
  const oc = sliderRow('weather.overcast', 'cloudy', params.overcast, { help: 'weather.overcast' });
  const fog = sliderRow('weather.fog', 'foggy', params.fog, { help: 'weather.fog' });
  const rain = sliderRow('weather.rain', 'rainy', params.rain, { help: 'weather.rain' });
  const snow = sliderRow('weather.snowfall', 'snowy', params.snowfall, { help: 'weather.snowfall' });
  const storm = sliderRow('weather.storm', 'storm', params.stormDensity, { help: 'weather.storm' });
  const wind = sliderRow('weather.wind', 'wind', params.wind, { unit: 'm/s', max: 20, step: '0.5', help: 'weather.wind' });
  const dynChk = h('input', { type: 'checkbox', class: 'switch', disabled: running });
  dynChk.checked = !!params.dynamic;

  // Live "what players will see" preview, updated as the sliders move.
  const preview = weatherPreview({
    overcast: oc.get, fog: fog.get, rain: rain.get, snow: snow.get, storm: storm.get, wind: wind.get,
  });
  wrap.addEventListener('input', (e) => { if (e.target && e.target.classList.contains('wslider')) preview.update(); });

  // After a save the page shows the saved state in place — no reload, so
  // unsaved day/night edits below survive and the scroll stays put.
  const showState = (p, matched) => {
    const on = !!(p && p.enable);
    matchedBadge.className = 'badge ' + (on ? 'ok' : 'mute');
    matchedBadge.textContent = on ? (State.i18n['weather.preset.' + matched] ? t('weather.preset.' + matched) : (matched || '—')) : '—';
    tiles.forEach(tl => tl.classList.toggle('active', on && tl.dataset.name === matched));
    disabledNote.hidden = on;
  };
  const applySaved = (r) => {
    const p = (r && r.params) || {};
    oc.set(p.overcast); fog.set(p.fog); rain.set(p.rain); snow.set(p.snowfall); storm.set(p.stormDensity); wind.set(p.wind);
    dynChk.checked = !!p.dynamic;
    if (p.transition) transSel.value = p.transition;
    preview.update();
    showState(p, r && r.matched);
  };
  showState(params, d.matched);

  async function applyPreset(name) {
    await withBusyAll(async () => {
      try {
        const r = await api.post('/api/weather/preset', { name, transition: transSel.value });
        applySaved(r);
        toast(t('msg.saved'), 'ok');
      } catch (e) { handleErr(e); }
    });
  }

  wrap.append(h('div', { class: 'card' }, [
    h('div', { class: 'card-head' }, [cardTitle('i-weather', 'weather.custom.title')]),
    h('p', { class: 'hint', i18n: 'weather.custom.hint' }),
    preview.el,
    oc.row, fog.row, rain.row, wind.row, snow.row, storm.row,
    h('label', { class: 'wx-dyn' }, [dynChk, h('span', { i18n: 'weather.custom.dynamic' }), help('weather.dynamic')]),
    h('div', { class: 'actions' }, [
      h('button', { type: 'button', class: 'primary', i18n: 'weather.apply', disabled: running,
        onclick: (e) => withBusy(e.currentTarget, async () => {
          try {
            const r = await api.post('/api/weather/custom', {
              overcast: oc.get(), fog: fog.get(), rain: rain.get(),
              snowfall: snow.get(), stormDensity: storm.get(),
              wind: wind.get(), dynamic: dynChk.checked,
              transition: transSel.value,
            });
            applySaved(r);
            toast(t('msg.saved'), 'ok');
          } catch (err) { handleErr(err); }
        }) }),
    ]),
    h('p', { class: 'hint', i18n: 'weather.timedHint' }),
  ]));

  // ---- in-game time: day/night planner (features.js)
  const dn = dayNightCard(tm, d.map, running);
  wrap.append(dn);
  root._isDirty = () => dn.isDirty();
};

// --------------------------------------------------------------------- wipe

Views.wipe = async (root) => {
  const d = await api.get('/api/wipe/preview');
  root.append(pageHeader('nav.wipe', 'wipe.subtitle', [revealBtn({ kind: 'mission' }, { label: 'folders.mission' })]));
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
    folders.length ? h('label', { style: { display: 'flex', gap: '8px', alignItems: 'center', margin: '10px 0' } }, [confirm, h('span', { i18n: 'wipe.confirm' })]) : null,
    folders.length ? h('div', { class: 'actions' }, [wipeBtn]) : null,
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

// Views.settings lives in sections.js (one tab per topic).

// intervalAnnouncementsCard — repeat a message every N minutes while running.
function intervalAnnouncementsCard(c, F, onChange) {
  const fire = () => onChange && onChange();
  const state = {
    items: (c.intervalAnnouncements || []).map(a => ({
      intervalMinutes: a.intervalMinutes || 30, message: a.message || '', enabled: !!a.enabled,
    })),
  };
  const card = h('div', { class: 'card' }, [
    cardTitle('i-chat', 'settings.intervalAnn'),
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

function backupCard() {
  const fileInp = h('input', { type: 'file', accept: '.zip', 'aria-label': t('settings.backup.import') });
  fileInp.style.display = 'none';
  // The card sits inside the Backups tab's config form, whose autosave would
  // otherwise post the tab's pre-import values over the restored manager.json.
  fileInp.addEventListener('change', (e) => e.stopPropagation());
  const resultBox = h('div', { class: 'hint' });

  const exportBtn = h('a', { class: 'btn-link', href: srvUrl('/api/backup/export'), i18n: 'settings.backup.export' });
  const importBtn = h('button', { i18n: 'settings.backup.import',
    onclick: () => fileInp.click() });

  fileInp.onchange = async () => {
    const f = fileInp.files && fileInp.files[0];
    if (!f) return;
    const fd = new FormData();
    fd.append('zip', f);
    try {
      const r = await fetch('/api/backup/import', {
        headers: srvHeaders(),
        method: 'POST', credentials: 'same-origin', body: fd,
      });
      if (!r.ok) throw new Error(await r.text());
      const data = await r.json();
      const msg = t('settings.backup.importResult')
        .replace('{restored}', (data.restored || []).length).replace('{skipped}', (data.skipped || []).length);
      resultBox.textContent = msg;
      toast(msg, 'ok');
      // Refresh config and redraw the tab so its inputs show the restored
      // values; stale inputs would revert them on the next edit.
      State.config = await api.get('/api/config');
      if (resultBox.isConnected) await navigate(currentRoute());
    } catch (e) { handleErr(e); }
    fileInp.value = '';
  };

  return h('div', { class: 'card' }, [
    cardTitle('i-sync', 'settings.backup'),
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
  const src = h('input', { type: 'text', placeholder: 'F:\\SteamLibrary\\steamapps\\common\\DayZServer',
    'aria-label': t('sync.source'), list: 'sync-recent' });
  src.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); runPreview(); } });
  const out = h('div');
  // Folders imported from before, offered as suggestions (this browser only).
  let recent = [];
  try { recent = JSON.parse(localStorage.getItem('syncRecent') || '[]').filter(p => typeof p === 'string').slice(0, 6); } catch {}
  const recentList = h('datalist', { id: 'sync-recent' }, recent.map(p => h('option', { value: p })));
  const remember = (dir) => {
    recent = [dir, ...recent.filter(p => p !== dir)].slice(0, 6);
    try { localStorage.setItem('syncRecent', JSON.stringify(recent)); } catch {}
  };
  // Another server of this panel as the source: no path to type.
  const others = (Srv.list || []).filter(s => s.id !== Srv.id && s.dir);
  const srvSel = others.length ? h('select', { 'aria-label': t('sync.fromServer') }, [
    h('option', { value: '', i18n: 'sync.fromServer.pick' }),
    ...others.map(s => h('option', { value: s.dir, text: s.name })),
  ]) : null;
  if (srvSel) srvSel.onchange = () => { if (srvSel.value) { src.value = srvSel.value; runPreview(); } };

  const runPreview = async () => {
    out.innerHTML = '';
    const dir = src.value.trim();
    if (!dir) { toast(t('sync.needSource'), 'warn'); src.focus(); return; }
    let p;
    try { p = await api.post('/api/import/preview', { sourceDir: dir }); }
    catch (e) { handleErr(e); return; }
    remember(dir);
    const missing = new Set(p.missingMissions || []);
    const copyMods = h('input', { type: 'checkbox', checked: true });
    const copyCfg  = h('input', { type: 'checkbox' });
    // "Don't change" by default: switching the mission is a choice of its
    // own, not a side effect of importing mods.
    const missionSel = h('select', { 'aria-label': t('sync.mission') }, [h('option', { value: '', i18n: 'sync.mission.keep' }),
      ...(p.missions || []).map(m => h('option', { value: m, text: missing.has(m) ? m + ' — ' + t('sync.mission.willCopy') : m }))]);
    const modsList = (p.mods || []).length
      ? h('ul', {}, (p.mods || []).map(m => h('li', { text: m })))
      : h('p', { class: 'hint', text: '—' });
    // The result shows here, under the button that started it.
    const result = h('div', { class: 'sync-result' });
    const inputs = [copyMods, copyCfg, missionSel];
    const applyBtn = h('button', { class: 'primary', i18n: 'sync.apply' });
    applyBtn.onclick = () => withBusy(applyBtn, async () => {
      // Copying whole @mod folders takes a while; nothing may change meanwhile.
      inputs.forEach(el => { el.disabled = true; });
      result.innerHTML = '';
      try {
        const r = await api.post('/api/import/apply', {
          sourceDir: dir,
          copyMods: copyMods.checked,
          copyCfg: copyCfg.checked,
          mission: missionSel.value,
        });
        State.config = await api.get('/api/config');
        const copied = r.copiedMods || [], failed = r.failedMods || [];
        const lines = [h('div', { i18n: 'sync.done' })];
        if (copyMods.checked) {
          lines.push(h('div', { text: copied.length ? tn('sync.copiedMods', { n: copied.length, list: copied.join(', ') }) : t('sync.noNewMods') }));
        }
        if (r.copiedMission) lines.push(h('div', { text: tn('sync.copiedMission', { name: r.copiedMission }) }));
        if (r.missionError) lines.push(h('div', { class: 'warn-text', text: tn('sync.missionFailed', { name: r.mission }) }));
        // A mod that could not be copied used to vanish from the result.
        for (const f of failed) lines.push(h('div', { class: 'warn-text', text: f }));
        result.append(h('div', { class: (failed.length || r.missionError) ? 'warning-bar' : 'ok-bar' }, lines),
          h('div', { class: 'actions' }, [h('button', { class: 'secondary', i18n: 'profiles.openMods', onclick: () => navigate('mods') })]));
      } catch (e) { handleErr(e); }
      finally { inputs.forEach(el => { el.disabled = false; }); }
    });
    out.append(
      h('div', { class: 'card' }, [
        h('h3', { i18n: 'sync.preview' }),
        h('div', {}, [h('label', { i18n: 'sync.mods' }), modsList]),
        h('div', {}, [h('label', { i18n: 'sync.cfgMission' }), h('code', { text: p.mission || '—' })]),
        h('div', {}, [h('label', {}, [copyMods, h('span', { i18n: 'sync.copyMods' })])]),
        h('div', {}, [h('label', {}, [copyCfg,  h('span', { i18n: 'sync.copyCfg'  })])]),
        h('div', {}, [h('label', { i18n: 'sync.mission' }), missionSel]),
        h('div', { class: 'actions' }, [applyBtn]),
        result,
      ]),
    );
    applyI18n();
  };

  root.append(
    pageHeader('nav.sync', 'sync.subtitle'),
    h('div', { class: 'card' }, [
      cardTitle('i-sync', 'sync.title', 'sync.import'),
      h('p', { class: 'hint', i18n: 'sync.hint' }),
      srvSel ? h('div', { class: 'field' }, [h('label', { i18n: 'sync.fromServer' }), srvSel]) : null,
      h('label', { i18n: 'sync.source' }),
      h('div', { class: 'row path-row' }, [src, h('button', { class: 'primary', i18n: 'sync.preview', onclick: () => runPreview() })]),
      recentList,
    ].filter(Boolean)),
    out,
  );
};

// --------------------------------------------------------------------- mod profile configs

Views.profiles = async (root) => {
  const tree = h('div', { class: 'left' });
  const editor = h('textarea', { style: { height: '60vh', fontFamily: 'monospace' }, placeholder: t('files.selectFile'), 'aria-label': t('files.editor') });
  // Which file Save writes: mod folders hold dozens of look-alike JSONs.
  const pathLabel = h('div', { class: 'hint mono files-path' });
  const savedPath = { path: '' };
  let cm = null;
  let loadedText = '';
  const isDirty = () => !!savedPath.path && getValue() !== loadedText;
  root._isDirty = isDirty;
  const getValue = () => cm ? cm.getValue() : editor.value;
  const setValue = (v) => { if (cm) cm.setValue(v || ''); else editor.value = v || ''; };
  const setMode  = (p) => { if (cm) cm.setOption('mode', CM.modeFor(p)); };
  const markActive = () => {
    for (const n of tree.querySelectorAll('.tree-node[data-path]')) n.classList.toggle('active', n.dataset.path === savedPath.path);
  };

  async function doSave() {
    if (!savedPath.path) return;
    try {
      await api.post('/api/profiles/write', { path: savedPath.path, content: getValue() });
      loadedText = getValue();
      toast(t('msg.saved'), 'ok');
    } catch (e) { handleErr(e); }
  }
  const savebtn = h('button', { class: 'primary', i18n: 'files.save', disabled: true, title: 'Ctrl+S',
    onclick: doSave });
  const reloadBtn = h('button', { class: 'secondary', i18n: 'files.reload', disabled: true, onclick: async () => {
    if (!savedPath.path) return;
    if (isDirty() && !(await confirmModal(t('refresh.discard'), { danger: true }))) return;
    if (await openFile(savedPath.path)) toast(t('refresh.done'), 'ok');
  } });
  root._save = doSave;

  async function openFile(path) {
    try {
      const r = await api.get('/api/profiles/read?path=' + encodeURIComponent(path));
      setMode(path);
      setValue(r.content);
      loadedText = getValue();
      savedPath.path = path;
      pathLabel.textContent = path;
      savebtn.disabled = reloadBtn.disabled = false;
      markActive();
      return true;
    } catch (e) { handleErr(e); return false; }
  }

  // Opening another file used to replace unsaved edits without a word.
  async function pickFile(path) {
    if (path === savedPath.path && isDirty()) return; // already open, keep the edits
    if (isDirty() && !(await confirmModal(tn('eventmap.discard', { name: savedPath.path }), { danger: true, okText: t('action.discard') }))) return;
    if (await openFile(path) && window.matchMedia('(max-width: 900px)').matches) {
      editorCol.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
  }

  // Returns how many entries the folder has (-1 when it could not be read).
  async function renderDir(rel) {
    tree.innerHTML = '';
    if (rel) {
      const up = rel.split('/').slice(0, -1).join('/');
      tree.appendChild(h('div', { class: 'tree-node dir', text: '../', onclick: () => renderDir(up) }));
    }
    try {
      const r = await api.get('/api/profiles/tree?path=' + encodeURIComponent(rel));
      const entries = r.entries || [];
      if (!entries.length) {
        tree.appendChild(h('p', { class: 'hint', i18n: 'profiles.empty' }));
      }
      for (const e of entries) {
        if (e.isDir) {
          tree.appendChild(h('div', { class: 'tree-node dir', text: e.name + '/', onclick: () => renderDir(e.path) }));
        } else {
          tree.appendChild(h('div', { class: 'tree-node', 'data-path': e.path, text: e.name, onclick: () => pickFile(e.path) }));
        }
      }
      markActive();
      return entries.length;
    } catch (e) { handleErr(e); return -1; }
  }

  const editorCol = h('div', {}, [pathLabel, editor, h('div', { class: 'actions' }, [
    savebtn,
    reloadBtn,
    revealBtn(() => ({ kind: 'profilesfile', path: savedPath.path })),
  ])]);
  const twoCol = h('div', { class: 'two-col' }, [tree, editorCol]);
  root.append(
    pageHeader('nav.profiles', 'profiles.subtitle'),
    h('div', { class: 'card' }, [
      h('p', { class: 'hint', i18n: 'profiles.hint' }),
      twoCol,
    ]),
  );
  // Nothing written by mods yet: say so with a way forward instead of a
  // full-size blank editor.
  if (await renderDir('') === 0) {
    twoCol.replaceWith(h('div', { class: 'empty-state' }, [
      h('div', { class: 'es-icon', html: svgIcon('i-profiles', 34) }),
      h('div', { class: 'es-title', i18n: 'profiles.none.title' }),
      h('div', { class: 'es-hint', i18n: 'profiles.empty' }),
      h('div', { class: 'actions' }, [
        h('button', { class: 'secondary', i18n: 'profiles.openMods', onclick: () => navigate('mods') }),
        revealBtn({ kind: 'profiles' }),
      ]),
    ]));
    return;
  }
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

    // Panel login: the window this computer opened signs in with its one-time
    // token; anyone else gets the login screen (in their browser's language
    // on first visit).
    const auth = State.info.auth || {};
    if (auth.enabled && !auth.authenticated) {
      if (!(await tryLocalAutologin())) {
        if (!localStorage.getItem('lang')) {
          const bl = matchBrowserLang();
          if (bl && bl !== State.lang) { try { await loadI18n(bl); } catch {} }
        }
        await showLogin();
      }
      State.info = await api.get('/api/info');
    } else if (/autologin=/.test(location.hash)) {
      history.replaceState(null, '', location.pathname + location.search);
    }

    State.config = await api.get('/api/config');
    let wantLang = State.config.language;
    // A brand-new install greets the admin in their browser's language
    // (uk-UA → Українська, pt-BR → Português do Brasil…) instead of English.
    if (!State.config.firstRunDone && !localStorage.getItem('lang')) {
      wantLang = matchBrowserLang() || wantLang;
    }
    if (wantLang && wantLang !== State.lang) {
      await loadI18n(wantLang);
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
    // Multi-server: the switcher, and the server list the dashboard and the
    // command palette use.
    await setupServerSwitcher();
    setupLogoutButton();
    await refreshStatus();
    if (window._statusInterval) clearInterval(window._statusInterval);
    window._statusInterval = setInterval(refreshStatus, 5000);
    // Support footer is rendered once here (outside #view) so it appears under
    // every section without any chance of duplication.
    renderSupportFooter();
    // Wire the clickable version in the sidebar, and pop "What's new" once when
    // the exe has been updated since the last visit.
    setupVersionBadge();
    maybeShowWhatsNew();
    startWatchlistPing();
    // Restore the section from the URL hash so a reload keeps the user where
    // they were (defaults to dashboard for a fresh load / unknown hash).
    await navigate(routeFromHash());
  } catch (err) {
    handleErr(err);
  }
}

function currentRoute() {
  return _route;
}

// startWatchlistPing polls for connects by watch-flagged players and toasts new
// ones. The newest already-seen timestamp lives in localStorage, so a page
// reload never re-announces old connects, and the first run only sets a baseline
// (no backlog spam).
function startWatchlistPing() {
  const KEY = 'watchSeenAt';
  async function poll(seedOnly) {
    let d;
    try { d = await api.get('/api/watchlist/activity'); }
    catch { return; }
    const events = d.events || [];
    if (!events.length) return;
    const newest = events[events.length - 1].at;
    const seen = localStorage.getItem(KEY) || '';
    if (!seedOnly && seen) {
      const fresh = events.filter(e => e.at > seen);
      for (const e of fresh.slice(-3)) {
        toast('👁 ' + t('watchping.connected').replace('{name}', e.name), 'warn');
      }
    }
    localStorage.setItem(KEY, newest);
  }
  poll(true).then(() => {
    if (window._watchInterval) clearInterval(window._watchInterval);
    window._watchInterval = setInterval(() => poll(false), 45000);
  });
}

// ---- What's new -----------------------------------------------------------

// setupVersionBadge shows the running version in the sidebar and makes it open
// the changelog on click.
function setupVersionBadge() {
  const el = document.getElementById('app-version');
  if (!el) return;
  const v = (State.info && State.info.version) || '';
  if (!v) return;
  el.textContent = 'v' + v;
  el.title = t('whatsnew.title');
  el.hidden = false;
  el.onclick = () => openChangelog(false);
}

// maybeShowWhatsNew pops the changelog once, the first time the UI loads after
// the exe was updated. A brand-new install (no remembered version) just records
// the current one silently — no nagging on first run.
function maybeShowWhatsNew() {
  const cur = (State.info && State.info.version) || '';
  if (!cur) return;
  const seen = localStorage.getItem('seenVersion');
  localStorage.setItem('seenVersion', cur);
  if (seen && seen !== cur) openChangelog(true);
}

// openChangelog fetches the release notes and shows them in a modal. `auto` is
// true when triggered by an update (stay quiet on error) vs. a manual click.
async function openChangelog(auto) {
  let d;
  try { d = await api.get('/api/changelog?lang=' + encodeURIComponent(State.lang || 'en')); }
  catch (e) { if (!auto) handleErr(e); return; }
  const releases = d.releases || [];
  if (!releases.length) return;
  const m = openModal({ title: t('whatsnew.title'), wide: true });
  m.body.append(h('p', { class: 'hint', text: (t('whatsnew.version') || 'Version {v}').replace('{v}', d.current || '') }));
  const host = h('div', { class: 'changelog' });
  for (const r of releases) {
    host.append(h('div', { class: 'changelog-rel' }, [
      h('div', { class: 'changelog-head' }, [
        h('span', { class: 'changelog-ver', text: 'v' + r.version }),
        h('span', { class: 'changelog-date', text: r.date || '' }),
      ]),
      h('ul', { class: 'changelog-notes' }, (r.notes || []).map(n => h('li', { text: n }))),
    ]));
  }
  m.body.append(host);
  m.body.append(h('div', { class: 'actions' }, [
    h('button', { class: 'primary', i18n: 'action.close', onclick: () => m.close() }),
  ]));
  applyI18n();
}

// --------------------------------------------------------------------- battleye

Views.battleye = async (root) => {
  const myNav = _navSeq;
  const d = await api.get('/api/battleye/list');
  if (myNav !== _navSeq) return;
  const wrap = h('div');
  if (State.serverStatus.running) wrap.append(runningBanner());

  // bans.txt has one editor, Players → Bans (card below): listed here as raw
  // text too, it was a second editor whose Ctrl+S reverted the other's saves.
  const files = (d.files || []).filter(f => f.kind !== 'bans' && f.name.toLowerCase() !== 'bans.txt');
  const fileOf = (name) => files.find(f => f.name === name) || {};
  const fileSelect = h('select', { 'aria-label': t('battleye.file') });
  for (const f of files) {
    const label = f.exists ? `${f.name} (${bytes(f.size)})` : `${f.name} (${t('file.empty')})`;
    fileSelect.append(h('option', { value: f.name, text: label }));
  }
  const firstExisting = files.find(f => f.exists);
  if (firstExisting) fileSelect.value = firstExisting.name;
  const fileDesc = h('p', { class: 'hint' });
  const editorHost = h('textarea', { rows: 22, style: { width: '100%', fontFamily: 'monospace' }, 'aria-label': t('files.editor') });
  let cm = null;
  let loadedText = '', shownName = '';
  const getValue = () => cm ? cm.getValue() : editorHost.value;
  const setValue = (v) => { if (cm) cm.setValue(v || ''); else editorHost.value = v || ''; };
  const isDirty = () => !!shownName && !fileOf(shownName).readOnly && getValue() !== loadedText;
  root._isDirty = isDirty;

  const saveBtn = h('button', { class: 'primary', i18n: 'action.save', title: 'Ctrl+S', onclick: () => doSave() });
  // One line on what the selected file is; the settings are read-only here.
  const describe = () => {
    const f = fileOf(shownName);
    const ro = !!f.readOnly;
    fileDesc.innerHTML = '';
    fileDesc.append(h('span', { i18n: f.kind === 'settings' ? 'battleye.kind.settings' : f.kind === 'active' ? 'battleye.kind.active' : 'battleye.kind.file' }));
    if (ro) fileDesc.append(' ', h('button', { type: 'button', class: 'small btn-link', i18n: 'battleye.openRcon', onclick: () => navigate('rcon') }));
    saveBtn.disabled = ro;
    if (cm) cm.setOption('readOnly', ro); else editorHost.readOnly = ro;
  };
  const load = async () => {
    if (!fileSelect.value) return;
    try {
      const r = await api.get('/api/battleye/read?name=' + encodeURIComponent(fileSelect.value));
      setValue(r.content || '');
      loadedText = getValue();
      shownName = fileSelect.value;
      describe();
    } catch (e) { handleErr(e); }
  };
  // Switching files used to drop unsaved edits; Cancel puts the select back.
  fileSelect.onchange = async () => {
    if (isDirty() && !(await confirmModal(tn('eventmap.discard', { name: shownName }), { danger: true, okText: t('action.discard') }))) {
      fileSelect.value = shownName;
      return;
    }
    await load();
  };
  const reloadBtn = h('button', { class: 'secondary', i18n: 'files.reload', onclick: async () => {
    if (isDirty() && !(await confirmModal(t('refresh.discard'), { danger: true }))) return;
    await load();
  } });

  async function doSave() {
    if (!shownName || fileOf(shownName).readOnly) return;
    try {
      await api.post('/api/battleye/write', { name: shownName, content: getValue() });
      loadedText = getValue();
      toast(t('msg.saved'), 'ok');
    } catch (e) { handleErr(e); }
  }
  root._save = doSave;

  wrap.prepend(pageHeader('nav.battleye', 'battleye.subtitle'));
  if (!files.length) {
    wrap.append(h('div', { class: 'card' }, [
      cardTitle('i-battleye', 'files.title'),
      h('div', { class: 'empty-state' }, [
        h('div', { class: 'es-hint', i18n: 'battleye.none' }),
        h('div', { class: 'actions' }, [
          h('button', { class: 'secondary', i18n: 'battleye.openRcon', onclick: () => navigate('rcon') }),
          revealBtn({ kind: 'battleye' }),
        ]),
      ]),
    ]));
  } else {
    wrap.append(h('div', { class: 'card' }, [
      cardTitle('i-battleye', 'files.title'),
      h('div', { class: 'toolbar' }, [
        fileSelect,
        reloadBtn,
        saveBtn,
        revealBtn(() => ({ path: d.dir + '/' + (fileSelect.value || '') })),
      ]),
      fileDesc,
      editorHost,
      h('p', { class: 'hint mono', text: d.dir }),
      h('p', { class: 'hint', i18n: 'battleye.hint' }),
    ]));
  }

  // One ban list: Players → Bans bans by Steam ID, GUID or IP, keeps the
  // history and who banned, and lists bans.txt too. The table that used to be
  // here bypassed that history.
  const bansCount = h('span', { class: 'hint' });
  wrap.append(h('div', { class: 'card' }, [
    cardTitle('i-shield', 'battleye.bans.title'),
    h('p', { class: 'hint', i18n: 'battleye.bans.moved' }),
    h('div', { class: 'actions' }, [
      h('button', { class: 'primary', i18n: 'battleye.bans.open', onclick: () => {
        try { localStorage.setItem('playersTab', 'bans'); } catch {}
        navigate('players');
      } }),
      bansCount,
    ]),
  ]));
  root.append(wrap);
  api.get('/api/battleye/bans').then(r => {
    bansCount.textContent = tn('battleye.bans.count', { n: (r.bans || []).filter(b => !b.expired).length });
  }).catch(() => {});

  if (!files.length) return;
  await load();
  if (myNav !== _navSeq) return;
  // Registered BEFORE the await: navigating away mid-mount must not leave the
  // next view's teardown overwritten by this one.
  root._teardown = () => { try { cm && cm.toTextArea(); } catch {} };
  try {
    cm = await CM.mount(editorHost, { mode: null, lineWrapping: true, readOnly: !!fileOf(shownName).readOnly });
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
    const label = f.exists ? `${f.path} (${bytes(f.size)})` : `${f.path} (${t('file.missing')})`;
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

  wrap.prepend(pageHeader('nav.missionDb', 'missionDb.subtitle'));
  wrap.append(h('div', { class: 'card' }, [
    h('p', { class: 'hint mono', text: d.dir }),
    h('div', { class: 'toolbar' }, [
      fileSelect,
      h('button', { i18n: 'action.reload', onclick: load }),
      h('button', { class: 'primary', i18n: 'action.save', title: 'Ctrl+S', onclick: doSave }),
      revealBtn(() => ({ path: d.dir + '/' + (fileSelect.value || '') })),
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

// playerNameLink is a clickable player name that opens their profile panel.
function playerNameLink(p, opts = {}) {
  return h('span', Object.assign({ class: 'player-link', text: p.name || '—',
    onclick: () => openPlayerProfile(p.key) }, opts));
}

// The Players page, the player profile and the ban / kick dialogs live in
// players.js.

function fmtWhen(rfc) {
  if (!rfc) return '—';
  const d = new Date(rfc);
  if (isNaN(d)) return rfc;
  return d.toLocaleDateString() + ' ' + String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
}
// fmtMinutes renders a duration with the UI language's units ("1 h 4 min",
// "1 ч 4 мин") — the hard-coded "1h 4m" read as English everywhere.
function fmtMinutes(min) {
  min = Math.max(0, Math.round(min || 0));
  if (min < 60) return `${min} ${t('unit.minutes')}`;
  const hrs = Math.floor(min / 60);
  if (hrs >= 48) return `${Math.floor(hrs / 24)} ${t('unit.days')} ${hrs % 24} ${t('unit.hours')}`;
  return `${hrs} ${t('unit.hours')} ${min % 60} ${t('unit.minutes')}`;
}
// fmtAge turns a seconds count into a short localized "12 s" / "3 min" label.
function fmtAge(sec) {
  sec = Math.max(0, Math.round(sec || 0));
  if (sec < 60) return `${sec} ${t('unit.seconds')}`;
  return fmtMinutes(Math.floor(sec / 60));
}

// ------------------------------------------------------------------ leaderboard

Views.leaderboard = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.leaderboard', 'leaderboard.subtitle'));
  let d, bans = { active: [] };
  try { [d, bans] = await Promise.all([api.get('/api/players'), api.get('/api/bans').catch(() => bans)]); }
  catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;

  const list = d.players || [];
  if (!list.length) {
    root.append(h('div', { class: 'card' }, h('div', { class: 'empty-state' },
      h('div', { class: 'es-hint', i18n: 'leaderboard.empty' }))));
    return;
  }

  // Banned players stay off the board unless asked for — a cheater topping
  // K/D is the last thing it should show — and carry a badge when shown.
  const bannedIds = new Set();
  for (const b of bans.active || []) {
    if (b.expired) continue;
    if (b.key) bannedIds.add(b.key);
    if (b.dayzId) bannedIds.add(b.dayzId);
  }
  const isBanned = (p) => bannedIds.has(p.key) || (!!p.id && bannedIds.has(p.id));
  const anyBanned = list.some(isBanned);
  let showBanned = false;
  try { showBanned = localStorage.getItem('lbShowBanned') === '1'; } catch {}

  // K/D needs a sample: one lucky kill and no deaths must not top the board.
  const KD_MIN_KILLS = 5;
  // Each metric knows how to score a player and who qualifies.
  const metrics = {
    playtime: { label: 'leaderboard.metric.playtime', val: p => p.playtimeMinutes || 0 },
    kills:    { label: 'leaderboard.metric.kills',    val: p => p.kills || 0 },
    kd:       { label: 'leaderboard.metric.kd',       val: p => (p.deaths ? (p.kills || 0) / p.deaths : (p.kills || 0)),
      eligible: p => (p.kills || 0) >= KD_MIN_KILLS },
    sessions: { label: 'leaderboard.metric.sessions', val: p => p.sessions || 0 },
  };
  const medals = ['🥇', '🥈', '🥉'];
  let cur = 'playtime';

  const card = h('div', { class: 'card' });
  root.append(card);

  function render() {
    card.innerHTML = '';
    const tabs = h('div', { class: 'section-tabs btn-tabs lb-metrics', role: 'tablist' }, Object.keys(metrics).map(k =>
      h('button', { type: 'button', role: 'tab', class: 'section-tab' + (k === cur ? ' active' : ''),
        'aria-selected': String(k === cur), i18n: metrics[k].label, onclick: () => { cur = k; render(); } })));
    const bannedSw = h('input', { type: 'checkbox', class: 'switch' });
    bannedSw.checked = showBanned;
    bannedSw.onchange = () => {
      showBanned = bannedSw.checked;
      try { localStorage.setItem('lbShowBanned', showBanned ? '1' : '0'); } catch {}
      render();
    };
    card.append(h('div', { class: 'lb-head' }, [
      tabs, help('leaderboard.metrics'), h('span', { class: 'grow' }),
      anyBanned ? switchRow(bannedSw, 'leaderboard.showBanned') : null,
    ].filter(Boolean)));

    const m = metrics[cur];
    if (m.eligible) card.append(h('p', { class: 'hint lb-note', text: tn('leaderboard.kdMin', { n: KD_MIN_KILLS }) }));
    const ranked = list
      .filter(p => (showBanned || !isBanned(p)) && (!m.eligible || m.eligible(p)) && m.val(p) > 0)
      .sort((a, b) => m.val(b) - m.val(a))
      .slice(0, 50);

    if (!ranked.length) {
      card.append(h('div', { class: 'empty-state' }, h('div', { class: 'es-hint', i18n: 'leaderboard.empty' })));
      return;
    }

    // One column per stat; the active ranking metric's column is highlighted
    // rather than duplicated as a separate "score" column.
    const cols = [
      { key: 'playtime', i18n: 'col.playtime', val: p => fmtMinutes(p.playtimeMinutes || 0) },
      { key: 'kills', i18n: 'col.kills', val: p => String(p.kills || 0) },
      { key: 'deaths', i18n: 'col.deaths', val: p => String(p.deaths || 0) },
      { key: 'kd', i18n: 'col.kd', val: p => (p.deaths ? ((p.kills || 0) / p.deaths) : (p.kills || 0)).toFixed(2) },
      { key: 'sessions', i18n: 'col.sessions', val: p => String(p.sessions || 0) },
    ];
    // Phone: only the ranked column stays next to the name (pl-hide-sm).
    const colCls = (c, on) => 'num' + (c.key === cur ? ' ' + on : ' pl-hide-sm');
    const tbl = h('table');
    tbl.append(h('thead', {}, h('tr', {}, [
      h('th', { class: 'lb-rank-h', i18n: 'col.rank' }),
      h('th', { i18n: 'col.name' }),
      ...cols.map(c => h('th', { class: colCls(c, 'lb-active'), i18n: c.i18n })),
    ])));
    const tb = h('tbody');
    ranked.forEach((p, i) => {
      tb.append(h('tr', { class: i < 3 ? 'lb-top' : '' }, [
        h('td', { class: 'lb-rank' }, i < 3
          ? h('span', { class: 'lb-medal', text: medals[i] })
          : h('span', { text: String(i + 1) })),
        h('td', { class: 'pl-name' }, [
          playerNameLink(p, { style: { fontWeight: '600' } }),
          p.watch ? h('span', { class: 'lb-star', text: '★', title: t('players.watched'), 'aria-label': t('players.watched') }) : null,
          isBanned(p) ? h('span', { class: 'badge err lb-banned', i18n: 'profile.banned' }) : null,
        ]),
        ...cols.map(c => h('td', { class: colCls(c, 'lb-score'), text: c.val(p) })),
      ]));
    });
    tbl.append(tb);
    card.append(h('div', { class: 'table-scroll' }, tbl));
  }
  render();
};

// ------------------------------------------------------------------- game map

// MAP_SHAPES holds a license-safe, hand-drawn schematic for a world — original
// artwork built from independent knowledge and public facts (town names and
// their approximate positions), NOT traced or copied from any commercial map
// render. Facts (a town's name and rough location) aren't copyrightable; the
// specific drawn expression of a map is, so we never trace one. For a truly
// accurate map the admin sets a real image; this is an orientation schematic.
//
// Coordinates are world metres [x east, z north]. A shape can carry:
//   coast     — true → sea base with a land polygon; false → landlocked (land base)
//   arid      — true → desert palette (sand instead of green)
//   land      — main landmass polygon (closed)
//   islands   — extra land polygons
//   terrain   — elevation bands {level:1 hills | 2 mountains, poly} for relief
//   lakes     — inland water polygons
//   rivers    — open polylines (stroked)
//   roads     — {major?, pts} open polylines for main roads
//   pois      — {n name, x, z, t type} where t ∈ city|town|mil|air|poi
// Worlds without a shape fall back to a clean placeholder + upload prompt. We
// only fill in worlds we actually know; we don't invent names we're unsure of.
const MAP_SHAPES = {
  // Chernarus (15360) — the flagship schematic: south + east seaboard, the
  // northern highland belt, main roads and ~30 known settlements. Positions are
  // approximate orientation aids drawn from knowledge, not a traced map.
  chernarus: {
    coast: true,
    // Perimeter clockwise from the NW corner; sea fills the S wedge, the east
    // seaboard and the NE corner (so the coastal towns read as coastal).
    land: [
      [0, 15360], [12700, 15360], [13500, 14300], [14200, 13100], [14100, 10700],
      [13000, 9500], [12500, 9000], [13100, 7000], [13800, 6100], [13300, 4300],
      [12700, 3000], [10800, 2000], [8000, 1700], [5200, 1650], [3000, 1750],
      [1400, 1650], [600, 2200], [0, 3400],
    ],
    islands: [
      // Skalisty Island, off the SE coast (south of Kamyshovo).
      [[13100, 1450], [13650, 1400], [13800, 1900], [13400, 2150], [13000, 1850]],
    ],
    terrain: [
      // Northern highland belt — the north third rises into forested hills.
      { level: 1, poly: [[500, 9200], [2500, 8800], [5000, 9100], [7500, 8700], [10000, 9000], [12800, 8900], [12600, 15000], [500, 14900]] },
      // Black Mountains — the high NW corner.
      { level: 2, poly: [[600, 11400], [2000, 11000], [3800, 11500], [4600, 12800], [4200, 14600], [2600, 15000], [900, 14700], [500, 13000]] },
      // Central ridge around Green Mountain / the Black Forest.
      { level: 1, poly: [[2600, 7000], [4200, 6700], [5400, 7100], [5600, 8600], [4600, 9600], [2900, 9500], [2200, 8300]] },
    ],
    lakes: [
      // Small reservoir near the central dam (stylised).
      [[8780, 10820], [9150, 10720], [9420, 10880], [9450, 11150], [9180, 11300], [8820, 11220], [8700, 11000]],
    ],
    rivers: [
      // A gentle central watercourse easing toward the east coast (stylised).
      [[8500, 11100], [9200, 9900], [10000, 9200], [10900, 8600], [11800, 8000]],
    ],
    roads: [
      // Coastal highway (major): SW corner along the south coast and up the east.
      { major: true, pts: [
        [2000, 2050], [4700, 2550], [6650, 2600], [10380, 2350], [12100, 3600],
        [13100, 5000], [13400, 5960], [12200, 8600], [11900, 9100], [12900, 9700],
        [13400, 11500], [13850, 13250],
      ] },
      // Central spine (major): the south coast up through the Sobors to the north.
      { major: true, pts: [[6650, 2600], [7000, 5200], [7350, 7650], [7900, 10400], [8100, 12550], [10500, 13200], [11850, 13400]] },
      // East–west connector: Zelenogorsk to Berezino through the Sobors.
      { pts: [[2720, 5250], [4300, 6300], [5900, 6900], [7350, 7650], [9500, 8800], [11900, 9100]] },
      // North-west run: Zelenogorsk to the airfield and Grishino.
      { pts: [[2720, 5250], [4050, 9250], [4550, 10450], [5100, 11000], [5900, 10100]] },
    ],
    pois: [
      { n: 'Chernogorsk', x: 6650, z: 2500, t: 'city' },
      { n: 'Elektrozavodsk', x: 10380, z: 2200, t: 'city' },
      { n: 'Berezino', x: 11900, z: 9100, t: 'city' },
      { n: 'Novodmitrovsk', x: 11850, z: 13400, t: 'city' },
      { n: 'Severograd', x: 8100, z: 12550, t: 'city' },
      { n: 'Zelenogorsk', x: 2720, z: 5250, t: 'city' },
      { n: 'Kamenka', x: 2000, z: 2000, t: 'town' },
      { n: 'Komarovo', x: 3800, z: 2350, t: 'town' },
      { n: 'Prigorodky', x: 7500, z: 3100, t: 'town' },
      { n: 'Kamyshovo', x: 12100, z: 3450, t: 'town' },
      { n: 'Solnichniy', x: 13400, z: 5960, t: 'town' },
      { n: 'Svetlojarsk', x: 13850, z: 13250, t: 'town' },
      { n: 'Krasnostav', x: 11100, z: 12100, t: 'town' },
      { n: 'Staroye', x: 9200, z: 6400, t: 'town' },
      { n: 'Gorka', x: 9500, z: 8800, t: 'town' },
      { n: 'Stary Sobor', x: 7350, z: 7650, t: 'town' },
      { n: 'Novy Sobor', x: 7050, z: 7900, t: 'town' },
      { n: 'Kabanino', x: 6400, z: 8100, t: 'town' },
      { n: 'Grishino', x: 5900, z: 10100, t: 'town' },
      { n: 'Novaya Petrovka', x: 4900, z: 11100, t: 'town' },
      { n: 'Myshkino', x: 2050, z: 7500, t: 'town' },
      { n: 'Guglovo', x: 8600, z: 7400, t: 'town' },
      { n: 'Dubrovka', x: 11400, z: 10700, t: 'town' },
      { n: 'Orlovets', x: 10600, z: 9500, t: 'town' },
      { n: 'Nizhnoye', x: 12900, z: 9700, t: 'town' },
      { n: 'Msta', x: 9700, z: 7400, t: 'town' },
      { n: 'NW Airfield', x: 4550, z: 10450, t: 'air' },
      { n: 'NE Airfield', x: 12150, z: 12550, t: 'air' },
      { n: 'Balota Airstrip', x: 4700, z: 2500, t: 'air' },
      { n: 'Tisy Military', x: 1600, z: 13850, t: 'mil' },
      { n: 'Vybor Military', x: 4050, z: 9250, t: 'mil' },
      { n: 'Pavlovo Military', x: 2450, z: 3850, t: 'mil' },
      { n: 'Green Mountain', x: 3720, z: 8900, t: 'poi' },
      { n: "Devil's Castle", x: 9550, z: 9560, t: 'poi' },
      { n: 'Skalisty Island', x: 13400, z: 1750, t: 'poi' },
    ],
  },
  // Livonia (Enoch), 12800 m: landlocked, so the base is land, not sea. Nadbor
  // (largest) sits south, Topolin north, the lake district + Biela river and the
  // north-east towns near Grabin, Karpaty highland in the SE, Lukow airfield
  // central. Approximate original art for orientation — set a real image for
  // accuracy.
  livonia: {
    coast: false,
    land: [[0, 0], [12800, 0], [12800, 12800], [0, 12800]],
    terrain: [
      // Rolling forested highland through the centre / centre-east (Livonia is
      // gently hilly, not mountainous).
      { level: 1, poly: [[3500, 5000], [7000, 4800], [9500, 5500], [10200, 8300], [7800, 10000], [4500, 9600], [3000, 7400]] },
    ],
    lakes: [
      // Lakes in the north-east (near Sitnik / Tymianka).
      [[10600, 9000], [11300, 8850], [11700, 9300], [11500, 10000], [10800, 10050], [10450, 9600]],
      [[9500, 9700], [10050, 9600], [10250, 10100], [9750, 10350], [9400, 10000]],
    ],
    rivers: [
      // River winding from the north-west down through the centre.
      [[1500, 11400], [3000, 9500], [4300, 7600], [5300, 5600], [5900, 3600], [6300, 1600]],
    ],
    roads: [
      // Northern belt: Lukow → Brena → Kolembrody → Tarnow → Grabin.
      { major: true, pts: [[2500, 11000], [5000, 11400], [7000, 11600], [8500, 11500], [11000, 11400]] },
      // Central spine: Nadbor up through Radunin to the north.
      { major: true, pts: [[5000, 3000], [5800, 5800], [5500, 8500], [5300, 11300]] },
      // East run: Nadbor → Wrzeszcz → Gieraltow, and up to Sitnik.
      { pts: [[5000, 3000], [8000, 4000], [11000, 3600], [11000, 9000]] },
    ],
    pois: [
      { n: 'Nadbor', x: 5000, z: 3000, t: 'city' },
      { n: 'Tarnow', x: 8500, z: 11500, t: 'town' },
      { n: 'Grabin', x: 11000, z: 11500, t: 'town' },
      { n: 'Kolembrody', x: 7000, z: 11600, t: 'town' },
      { n: 'Brena', x: 5000, z: 11500, t: 'town' },
      { n: 'Sitnik', x: 11000, z: 9000, t: 'town' },
      { n: 'Gliniska', x: 4000, z: 10000, t: 'town' },
      { n: 'Topolin', x: 1600, z: 7200, t: 'town' },
      { n: 'Radunin', x: 6000, z: 6000, t: 'town' },
      { n: 'Nidek', x: 5000, z: 7600, t: 'town' },
      { n: 'Lembork', x: 8500, z: 6100, t: 'town' },
      { n: 'Karlin', x: 10200, z: 7000, t: 'town' },
      { n: 'Adamow', x: 3000, z: 6000, t: 'town' },
      { n: 'Sarnowek', x: 4000, z: 4200, t: 'town' },
      { n: 'Wrzeszcz', x: 8000, z: 4000, t: 'town' },
      { n: 'Gieraltow', x: 11000, z: 3500, t: 'town' },
      { n: 'Swarog', x: 5000, z: 1500, t: 'town' },
      { n: 'Lukow Airfield', x: 2500, z: 11000, t: 'air' },
    ],
  },
  // Sakhal (15360): a large main island (centre / north-east) plus a smaller
  // south-west island linked by a land bridge near Petropavlovsk; the volcano
  // sits centre-east. Simplified original outline with positions read from the
  // in-game map (facts), drawn our own way — not a trace of any commercial map.
  sakhal: {
    coast: true,
    land: [
      [4000, 7900], [4300, 10500], [5400, 13200], [7000, 14300], [9200, 13900],
      [11200, 12900], [12900, 11600], [13600, 9800], [13200, 8300], [12400, 6900],
      [11400, 6100], [9500, 5700], [7400, 5900], [5900, 6400], [4700, 7000],
    ],
    islands: [
      // South-west island (Burukan / Neft), linked near Petropavlovsk.
      [[3900, 8100], [2800, 7900], [1800, 7600], [1500, 9000], [2100, 11000], [3000, 12100], [3600, 11200], [3900, 9600]],
    ],
    terrain: [
      // Central volcanic massif + its broader flanks.
      { level: 2, poly: [[8000, 9400], [9400, 9000], [10500, 9600], [10600, 11100], [9400, 12000], [7900, 11600], [7300, 10400]] },
      { level: 1, poly: [[5500, 7500], [9000, 6800], [11800, 7400], [12600, 10200], [10800, 12600], [7200, 12800], [5000, 10800], [4600, 8800]] },
    ],
    roads: [
      { major: true, pts: [[4200, 8300], [6000, 7300], [8000, 7200], [10000, 7500], [11400, 6300]] },
      { pts: [[4200, 8300], [3200, 11200]] },
    ],
    pois: [
      { n: 'Petropavlovsk', x: 4200, z: 8300, t: 'city' },
      { n: 'Severomorsk', x: 6500, z: 13500, t: 'city' },
      { n: 'Rudnogorsk', x: 12500, z: 11800, t: 'city' },
      { n: 'Nogovo', x: 6000, z: 7200, t: 'city' },
      { n: 'Aniva', x: 11400, z: 6400, t: 'town' },
      { n: 'Vysokoye', x: 10000, z: 7500, t: 'town' },
      { n: 'Baykovo', x: 12800, z: 8800, t: 'town' },
      { n: 'Vostok', x: 12500, z: 7100, t: 'town' },
      { n: 'Rybnoye', x: 10000, z: 12500, t: 'town' },
      { n: 'Orlovo', x: 8500, z: 13200, t: 'town' },
      { n: 'Yuzhnoye', x: 8500, z: 6300, t: 'town' },
      { n: 'Volnoye', x: 4200, z: 9200, t: 'town' },
      { n: 'Burukan', x: 2500, z: 11000, t: 'town' },
      { n: 'Neft', x: 1900, z: 8400, t: 'town' },
      { n: 'Volcano', x: 9000, z: 10500, t: 'poi' },
    ],
  },
  // Nasdara (16384, DayZ Badlands): a landlocked desert province, so a land
  // base with no sea. Built from the mission's own data, not from any map
  // image: the relief from the altitudes of the trees and buildings listed in
  // mapgroupcluster*.xml / mapgrouppos.xml, settlements from clusters of
  // houses, military sites from barracks and tents. Only three places are
  // named, where the buildings leave no doubt: the airbase in the east (the
  // only control tower and hangars), the hydro dam, and the city with the
  // only hospital, the great mosque and the bazaar.
  nasdara: {
    coast: false,
    arid: true,
    land: [[0, 0], [16384, 0], [16384, 16384], [0, 16384]],
    terrain: [
      { level: 1, poly: [[8320, 0], [8580, 0], [8740, 60], [8800, 190], [8770, 380], [8510, 860], [8040, 1370], [7980, 1600], [8020, 1730], [8110, 1750], [8500, 1490], [9020, 940], [9500, 140], [9950, 0], [10490, 20], [10600, 70], [10610, 140], [10340, 600], [10060, 1630], [9780, 1980], [9790, 2250], [9180, 2640], [8810, 3040], [8640, 3100], [8810, 3390], [9140, 3580], [9220, 3820], [9140, 3970], [8890, 4100], [8480, 4480], [8350, 4460], [8160, 4240], [7970, 3380], [7590, 3290], [7220, 3090], [6990, 2930], [6780, 2630], [6800, 2500], [7010, 2290], [7140, 2260], [7440, 2340], [7560, 2250], [7640, 2010], [7780, 160], [7950, 50]] },
      { level: 1, poly: [[12770, 260], [12880, 320], [12930, 640], [12900, 1220], [12830, 1330], [12900, 2000], [12610, 2820], [12330, 3020], [11780, 3120], [11560, 3310], [11220, 3300], [10940, 2920], [10700, 2720], [10620, 2530], [11070, 2850], [11460, 2940], [11620, 2870], [11650, 2700], [11770, 2560], [12250, 2370], [12260, 2110], [11980, 1930], [11900, 1760], [12100, 1430], [12110, 1000]] },
      { level: 1, poly: [[2770, 480], [3180, 620], [3570, 920], [4160, 520], [4550, 450], [4850, 620], [5130, 940], [5250, 1340], [5200, 1550], [5050, 1760], [5060, 2030], [4520, 2600], [4490, 2880], [4560, 3020], [4960, 3000], [5390, 3260], [5540, 3590], [5490, 4320], [5880, 4520], [6010, 4790], [5990, 5060], [5780, 5280], [5540, 5140], [4900, 4220], [4770, 4260], [4500, 4640], [4340, 4740], [4050, 4690], [3740, 4530], [2870, 4410], [2270, 4170], [1870, 4340], [1610, 4370], [1540, 4450], [1660, 4590], [2070, 4620], [2560, 4850], [3090, 4890], [3520, 5100], [3700, 5390], [3880, 6110], [4050, 6300], [4100, 6830], [4180, 7000], [4360, 7110], [4980, 7170], [5740, 7090], [6020, 7180], [6140, 7360], [6140, 7490], [5880, 7720], [5000, 8020], [4740, 8210], [4780, 8400], [5240, 8850], [5290, 9070], [5220, 9710], [4970, 10200], [4780, 10340], [4260, 10430], [3960, 10260], [3380, 10220], [3280, 10130], [2850, 10030], [2700, 10060], [2560, 10270], [2560, 10460], [2730, 10970], [3300, 11940], [3650, 12880], [3910, 12910], [4260, 12710], [4460, 12840], [4460, 13020], [4100, 13440], [4020, 14030], [4200, 14050], [4570, 13660], [4800, 13570], [5110, 13620], [5380, 13810], [5870, 13820], [7120, 13380], [8660, 13240], [8950, 13000], [9360, 12930], [9790, 13120], [10080, 13460], [10160, 13680], [10740, 14070], [10860, 14060], [10960, 13810], [10950, 13580], [10770, 13380], [10810, 13120], [11310, 12980], [11810, 13060], [12370, 13330], [12730, 13650], [12800, 13810], [12750, 14140], [12510, 14140], [12320, 14270], [12050, 14140], [12010, 14310], [11790, 14580], [11420, 14740], [11080, 15080], [10810, 15090], [10490, 14910], [10370, 15050], [9980, 15030], [9860, 15180], [9700, 15230], [9300, 14900], [9310, 14750], [9430, 14560], [9380, 14380], [9540, 14200], [9500, 14020], [9310, 13750], [9140, 13820], [9170, 14060], [8980, 14320], [8780, 14390], [8470, 14270], [8380, 14520], [8040, 14800], [7780, 14750], [7590, 14810], [7420, 14650], [6990, 14640], [6730, 14390], [6640, 14130], [6500, 14080], [5780, 14660], [5300, 14680], [5040, 14540], [4920, 15020], [4750, 15220], [4630, 15260], [4320, 15170], [3970, 14960], [3740, 14940], [3620, 15110], [3680, 15340], [3580, 15780], [3440, 16020], [3130, 16230], [2760, 16230], [2450, 16020], [2380, 15850], [2520, 15610], [2460, 15500], [2510, 15330], [1700, 16000], [1570, 15980], [1280, 15710], [630, 15900], [490, 15900], [270, 15760], [270, 15520], [680, 14970], [550, 14690], [580, 14470], [120, 13910], [0, 13630], [20, 13390], [160, 13150], [450, 13100], [730, 13220], [1520, 13910], [2100, 14180], [2530, 14590], [2720, 14470], [2910, 14060], [3160, 13780], [3060, 13620], [2590, 13520], [2180, 13280], [1580, 12750], [1630, 12660], [1580, 12230], [1780, 11920], [1940, 11850], [2210, 11990], [2490, 11940], [2740, 12000], [2820, 11980], [2790, 11920], [2480, 11770], [2100, 11370], [1780, 11340], [1570, 11140], [980, 11420], [710, 11190], [640, 10980], [980, 10590], [930, 10340], [980, 10150], [780, 9940], [770, 9820], [1580, 8820], [1620, 8400], [1870, 8160], [2060, 8210], [2300, 8400], [2420, 8350], [2700, 8390], [2780, 8220], [2650, 7760], [2890, 7500], [3140, 7500], [3110, 7120], [3240, 6840], [2660, 6830], [2380, 6580], [2340, 5910], [2480, 5650], [2480, 5290], [1850, 5030], [360, 4820], [120, 4660], [0, 4430], [0, 4140], [70, 3940], [220, 3830], [1100, 3900], [1320, 3730], [1440, 3710], [1500, 3590], [1490, 3040], [1650, 2830], [1760, 2790], [2030, 2880], [2330, 3160], [2770, 4020], [2880, 4050], [3170, 3930], [3180, 3740], [2860, 3420], [2860, 3210], [2050, 2460], [2070, 2330], [2290, 2060], [2590, 1970], [2060, 1300], [1920, 990], [2240, 580], [2390, 500]] },
      { level: 1, poly: [[6370, 2940], [6720, 3140], [6910, 3450], [6590, 3590], [6100, 3950], [5970, 3810], [5970, 3300], [6130, 3090]] },
      { level: 1, poly: [[9870, 3070], [10090, 3170], [10110, 3390], [10020, 3530], [9790, 3500], [9660, 3390], [9730, 3140]] },
      { level: 1, poly: [[1260, 6260], [1420, 6420], [1550, 6710], [1420, 6980], [1180, 7040], [760, 6740], [640, 6500], [880, 6160], [990, 6130]] },
      { level: 1, poly: [[7150, 11520], [7540, 11590], [7630, 11760], [7600, 11840], [7120, 11980], [6740, 11940], [6700, 11810], [6750, 11680], [6900, 11570]] },
      { level: 1, poly: [[6210, 14740], [6430, 14920], [6530, 15140], [6290, 15470], [6070, 15520], [5730, 15290], [5630, 15070], [5870, 14740], [5980, 14690]] },
      { level: 2, poly: [[8130, 0], [8420, 70], [8400, 380], [8210, 480], [7790, 380], [7840, 140]] },
      { level: 2, poly: [[2770, 480], [3180, 620], [3570, 920], [4160, 520], [4420, 430], [4700, 510], [5010, 780], [5210, 1080], [5230, 1460], [5050, 1760], [5060, 2030], [4600, 2490], [4480, 2800], [4620, 2940], [5130, 3070], [5390, 3220], [5550, 3400], [5550, 3620], [5340, 3700], [4570, 3760], [4180, 3900], [4000, 3820], [3810, 3610], [3680, 3580], [3550, 3660], [3400, 3900], [3710, 4050], [3040, 4070], [3180, 3930], [3200, 3810], [2900, 3490], [2860, 3210], [2050, 2460], [2070, 2330], [2290, 2060], [2590, 1970], [1920, 1060], [1950, 910], [2240, 580], [2390, 500]] },
      { level: 2, poly: [[9700, 830], [9760, 960], [9720, 1220], [9340, 1610], [9180, 2190], [8980, 2350], [8620, 2330], [8620, 1990], [9200, 1440], [9440, 800], [9570, 720]] },
      { level: 2, poly: [[7280, 2270], [7560, 2410], [7680, 2670], [7540, 2910], [7140, 3070], [6800, 2830], [6750, 2620], [7010, 2290]] },
      { level: 2, poly: [[2030, 2880], [2440, 3310], [2620, 3870], [2280, 3730], [1700, 3670], [1490, 3470], [1460, 3170], [1550, 2930], [1760, 2790]] },
      { level: 2, poly: [[2850, 5250], [3190, 5260], [3410, 5410], [3220, 5980], [3220, 6420], [3490, 6790], [3700, 7300], [4120, 7670], [4180, 7920], [4140, 8220], [4410, 8570], [4480, 8980], [4340, 9150], [3710, 9170], [3400, 9390], [2090, 9380], [1850, 9400], [1630, 9500], [1540, 9790], [1560, 10040], [1850, 10460], [1960, 10950], [2430, 11540], [2480, 11730], [2100, 11370], [1780, 11340], [1570, 11140], [980, 11420], [710, 11190], [640, 10980], [980, 10590], [930, 10340], [980, 10150], [780, 9940], [770, 9820], [1490, 8980], [1580, 8820], [1580, 8500], [1680, 8300], [1970, 8160], [2300, 8400], [2420, 8350], [2700, 8390], [2780, 8220], [2650, 7760], [2890, 7500], [3140, 7500], [3110, 7120], [3240, 6840], [2660, 6830], [2350, 6500], [2400, 6220], [2340, 5910], [2420, 5850], [2580, 5370]] },
      { level: 2, poly: [[1260, 6260], [1420, 6420], [1550, 6710], [1420, 6980], [1180, 7040], [760, 6740], [640, 6500], [880, 6160], [990, 6130]] },
      { level: 2, poly: [[2020, 11870], [2160, 11980], [2500, 11950], [2680, 12220], [2760, 13250], [2620, 13520], [2020, 13200], [1580, 12750], [1620, 12120], [1860, 11860]] },
      { level: 2, poly: [[450, 13100], [700, 13200], [1240, 13730], [2100, 14260], [2130, 14350], [2010, 14540], [1650, 14510], [1410, 14690], [1450, 14840], [1810, 15250], [1960, 15540], [1890, 15810], [1700, 16000], [1570, 15980], [1280, 15710], [630, 15900], [490, 15900], [270, 15760], [270, 15520], [630, 15080], [670, 14880], [550, 14690], [580, 14470], [120, 13910], [0, 13500], [160, 13150]] },
      { level: 2, poly: [[11760, 13410], [12140, 13620], [12230, 13870], [11970, 13870], [11760, 13740], [11610, 13420]] },
      { level: 2, poly: [[9330, 13540], [9670, 13710], [10110, 14080], [10750, 14820], [10740, 14920], [10490, 14900], [10370, 15050], [9980, 15030], [9760, 15230], [9540, 15160], [9300, 14900], [9310, 14750], [9430, 14560], [9380, 14380], [9540, 14200], [9500, 14020], [9310, 13750], [9140, 13820], [9170, 14060], [8980, 14320], [8780, 14390], [8470, 14270], [8380, 14520], [8040, 14800], [7780, 14750], [7590, 14810], [7420, 14650], [7080, 14660], [6850, 14480], [6660, 14180], [6770, 13940], [7120, 13780], [8150, 13780], [8980, 13530]] },
      { level: 2, poly: [[6240, 14130], [6310, 14220], [6070, 14340], [5750, 14670], [5570, 14620], [5300, 14680], [5040, 14540], [4920, 15020], [4750, 15220], [4630, 15260], [4320, 15170], [3970, 14960], [3740, 14940], [3620, 15110], [3680, 15340], [3580, 15780], [3240, 16180], [3010, 16260], [2760, 16230], [2540, 16110], [2300, 15820], [2350, 15780], [2620, 15840], [2880, 15740], [3230, 15240], [3980, 14640], [4300, 14580], [4630, 14380], [5570, 14320]] },
      { level: 2, poly: [[6210, 14740], [6430, 14920], [6530, 15140], [6290, 15470], [6070, 15520], [5730, 15290], [5630, 15070], [5870, 14740], [5980, 14690]] },
      { level: 3, poly: [[2770, 480], [3180, 620], [3570, 920], [4160, 520], [4550, 450], [5010, 780], [5250, 1220], [5200, 1550], [5030, 1820], [5060, 2030], [4600, 2500], [4450, 2980], [4000, 3120], [3780, 3470], [3310, 3710], [2980, 3620], [2870, 3490], [2860, 3210], [2050, 2460], [2070, 2330], [2290, 2060], [2590, 1970], [2060, 1300], [1920, 990], [2240, 580], [2390, 500]] },
      { level: 3, poly: [[1260, 6260], [1420, 6420], [1550, 6710], [1420, 6980], [1180, 7040], [760, 6740], [640, 6500], [880, 6160], [990, 6130]] },
      { level: 3, poly: [[2990, 7470], [3110, 7520], [3300, 7460], [3490, 7580], [3660, 8240], [3610, 8520], [3230, 8600], [3190, 8670], [2740, 8580], [2340, 8580], [2060, 8670], [1680, 8570], [1680, 8380], [1870, 8180], [2060, 8210], [2300, 8400], [2420, 8350], [2700, 8390], [2780, 8220], [2650, 7760], [2800, 7570]] },
      { level: 3, poly: [[960, 10750], [1190, 10780], [1330, 10940], [1350, 11180], [1220, 11310], [880, 11340], [700, 11160], [720, 10900]] },
      { level: 3, poly: [[6080, 14850], [6390, 14920], [6480, 15090], [6470, 15260], [6220, 15470], [5890, 15460], [5790, 15350], [5840, 15040]] },
    ],
    pois: [
      { n: '', x: 3720, z: 11450, t: 'town' },
      { n: 'Nasdara City', x: 10090, z: 4220, t: 'city' },
      { n: '', x: 9460, z: 12030, t: 'town' },
      { n: '', x: 9230, z: 4590, t: 'town' },
      { n: '', x: 10420, z: 8010, t: 'town' },
      { n: '', x: 8820, z: 8720, t: 'town' },
      { n: '', x: 7640, z: 12400, t: 'town' },
      { n: '', x: 6920, z: 4690, t: 'town' },
      { n: '', x: 10990, z: 7720, t: 'town' },
      { n: '', x: 7750, z: 8800, t: 'town' },
      { n: '', x: 5190, z: 13390, t: 'town' },
      { n: '', x: 7260, z: 5690, t: 'town' },
      { n: '', x: 6800, z: 7470, t: 'town' },
      { n: '', x: 10420, z: 10670, t: 'town' },
      { n: '', x: 5370, z: 9900, t: 'town' },
      { n: '', x: 9430, z: 3420, t: 'town' },
      { n: '', x: 4420, z: 10120, t: 'town' },
      { n: '', x: 5810, z: 12820, t: 'town' },
      { n: '', x: 13250, z: 12080, t: 'town' },
      { n: '', x: 5260, z: 8160, t: 'town' },
      { n: '', x: 8300, z: 11460, t: 'town' },
      { n: '', x: 7890, z: 8080, t: 'town' },
      { n: '', x: 11440, z: 9490, t: 'town' },
      { n: '', x: 11040, z: 10900, t: 'town' },
      { n: '', x: 5380, z: 9220, t: 'town' },
      { n: '', x: 3990, z: 9420, t: 'town' },
      { n: '', x: 6090, z: 11880, t: 'town' },
      { n: '', x: 9320, z: 7620, t: 'town' },
      { n: '', x: 4580, z: 5700, t: 'town' },
      { n: '', x: 8350, z: 10090, t: 'town' },
      { n: '', x: 10540, z: 11490, t: 'town' },
      { n: '', x: 5170, z: 6530, t: 'town' },
      { n: '', x: 6660, z: 12900, t: 'town' },
      { n: '', x: 4590, z: 10930, t: 'town' },
      { n: '', x: 10810, z: 13980, t: 'town' },
      { n: '', x: 4540, z: 11660, t: 'town' },
      { n: '', x: 5100, z: 7300, t: 'town' },
      { n: '', x: 7850, z: 6830, t: 'town' },
      { n: '', x: 9910, z: 5380, t: 'town' },
      { n: '', x: 11160, z: 2490, t: 'town' },
      { n: '', x: 13630, z: 12920, t: 'town' },
      { n: '', x: 4310, z: 14420, t: 'town' },
      { n: '', x: 6210, z: 5770, t: 'town' },
      { n: '', x: 10710, z: 8620, t: 'town' },
      { n: '', x: 10140, z: 12810, t: 'town' },
      { n: '', x: 6740, z: 11250, t: 'town' },
      { n: '', x: 7850, z: 9610, t: 'town' },
      { n: '', x: 9930, z: 10530, t: 'town' },
      { n: '', x: 3610, z: 6040, t: 'town' },
      { n: '', x: 5300, z: 10710, t: 'town' },
      { n: '', x: 2950, z: 6330, t: 'town' },
      { n: '', x: 3470, z: 12870, t: 'town' },
      { n: '', x: 7420, z: 6280, t: 'town' },
      { n: '', x: 9710, z: 6400, t: 'town' },
      { n: '', x: 4410, z: 7670, t: 'town' },
      { n: '', x: 10130, z: 9280, t: 'town' },
      { n: '', x: 12290, z: 12680, t: 'town' },
      { n: '', x: 4290, z: 7260, t: 'town' },
      { n: '', x: 6450, z: 12410, t: 'town' },
      { n: '', x: 10810, z: 9260, t: 'town' },
      { n: '', x: 14190, z: 13400, t: 'town' },
      { n: '', x: 9600, z: 11270, t: 'town' },
      { n: '', x: 10620, z: 6010, t: 'town' },
      { n: '', x: 6190, z: 9050, t: 'town' },
      { n: '', x: 12100, z: 2200, t: 'town' },
      { n: '', x: 4170, z: 13650, t: 'town' },
      { n: '', x: 6220, z: 8180, t: 'town' },
      { n: '', x: 9430, z: 9760, t: 'town' },
      { n: '', x: 9360, z: 10450, t: 'town' },
      { n: '', x: 2820, z: 5930, t: 'town' },
      { n: '', x: 7330, z: 13200, t: 'town' },
      { n: '', x: 9030, z: 7230, t: 'town' },
      { n: '', x: 10210, z: 9900, t: 'town' },
      { n: '', x: 11000, z: 8190, t: 'town' },
      { n: '', x: 6410, z: 8660, t: 'town' },
      { n: '', x: 13760, z: 13520, t: 'town' },
      { n: '', x: 6860, z: 3750, t: 'town' },
      { n: '', x: 3070, z: 5470, t: 'town' },
      { n: '', x: 3810, z: 5070, t: 'town' },
      { n: '', x: 5650, z: 11530, t: 'town' },
      { n: '', x: 10690, z: 3450, t: 'town' },
      { n: '', x: 4190, z: 6160, t: 'town' },
      { n: '', x: 5170, z: 11540, t: 'town' },
      { n: '', x: 5060, z: 12350, t: 'town' },
      { n: '', x: 9130, z: 6500, t: 'town' },
      { n: '', x: 12680, z: 12770, t: 'town' },
      { n: '', x: 5490, z: 12180, t: 'town' },
      { n: '', x: 5540, z: 7240, t: 'town' },
      { n: '', x: 7740, z: 13020, t: 'town' },
      { n: '', x: 11110, z: 11770, t: 'town' },
      { n: '', x: 11390, z: 12290, t: 'town' },
      { n: '', x: 3290, z: 4700, t: 'mil' },
      { n: '', x: 8230, z: 5700, t: 'mil' },
      { n: '', x: 10210, z: 8140, t: 'mil' },
      { n: '', x: 6530, z: 10210, t: 'mil' },
      { n: '', x: 12780, z: 6710, t: 'mil' },
      { n: '', x: 4750, z: 7380, t: 'mil' },
      { n: '', x: 4320, z: 12970, t: 'mil' },
      { n: '', x: 6040, z: 9190, t: 'mil' },
      { n: '', x: 10190, z: 4260, t: 'mil' },
      { n: '', x: 10680, z: 3730, t: 'mil' },
      { n: '', x: 3150, z: 5580, t: 'mil' },
      { n: '', x: 6660, z: 7500, t: 'mil' },
      { n: '', x: 4680, z: 10150, t: 'mil' },
      { n: '', x: 11350, z: 9020, t: 'mil' },
      { n: 'Dashtabad Airbase', x: 11930, z: 10770, t: 'air' },
      { n: 'Marek Barq Dam', x: 3570, z: 14380, t: 'poi' },
      { n: '', x: 10140, z: 8210, t: 'poi' },
      { n: '', x: 4790, z: 12000, t: 'poi' },
      { n: '', x: 4800, z: 5400, t: 'poi' },
      { n: '', x: 5390, z: 6100, t: 'poi' },
      { n: '', x: 8300, z: 7810, t: 'poi' },
      { n: '', x: 8800, z: 9920, t: 'poi' },
      { n: '', x: 9690, z: 4560, t: 'poi' },
    ],
  },
  // ---- Popular community worlds (approximate original schematics from
  // knowledge — positions are rough orientation aids, not survey-accurate; set a
  // real image for precision). Only worlds we actually know are drawn. ----
  //
  // Takistan (12800): arid and very mountainous. Feruz Abad in the south, Rasman
  // + its airfield in the north-east, ringed by highland.
  takistan: {
    coast: false,
    arid: true,
    land: [[0, 0], [12800, 0], [12800, 12800], [0, 12800]],
    terrain: [
      { level: 1, poly: [[500, 6800], [3500, 6400], [6500, 7000], [9500, 6600], [12300, 7000], [12300, 12300], [500, 12300]] },
      { level: 2, poly: [[1300, 8100], [2400, 7700], [3600, 7900], [4000, 9200], [3700, 10800], [2600, 11500], [1500, 11200], [1000, 9800]] },
      { level: 2, poly: [[9900, 7700], [11000, 7400], [12100, 7800], [12400, 9200], [12100, 10900], [11000, 11500], [9900, 11100], [9600, 9400]] },
      { level: 1, poly: [[4300, 1400], [8300, 1300], [9000, 4200], [5400, 5000], [4000, 3400]] },
    ],
    roads: [
      { major: true, pts: [[2500, 3500], [5500, 3200], [6500, 3000], [8500, 4500], [9700, 7000], [9600, 9600]] },
      { pts: [[6500, 3000], [6500, 6500], [5500, 9500], [3000, 9500]] },
    ],
    pois: [
      { n: 'Feruz Abad', x: 6500, z: 3000, t: 'city' },
      { n: 'Rasman', x: 9600, z: 9600, t: 'city' },
      { n: 'Loy Manara', x: 2500, z: 3500, t: 'town' },
      { n: 'Nagara', x: 8500, z: 6500, t: 'town' },
      { n: 'Zavarak', x: 3000, z: 9500, t: 'town' },
      { n: 'Sakhe', x: 5500, z: 7000, t: 'town' },
      { n: 'Chak Chak', x: 10200, z: 4200, t: 'town' },
      { n: 'Rasman Airfield', x: 10000, z: 10300, t: 'air' },
    ],
  },
  // Chiemsee (10240): a real Bavarian lake district — the big Chiemsee lake sits
  // central, Alpine foothills rise along the south edge, towns ring the shore.
  chiemsee: {
    coast: false,
    land: [[0, 0], [10240, 0], [10240, 10240], [0, 10240]],
    terrain: [
      { level: 1, poly: [[0, 0], [10240, 0], [10240, 3200], [7000, 2600], [3500, 2800], [0, 3000]] },
      { level: 2, poly: [[2000, 0], [4000, 300], [6000, 200], [7000, 900], [6400, 1900], [4200, 2100], [2600, 1800], [1800, 900]] },
    ],
    lakes: [
      [[3600, 5200], [5200, 4700], [6700, 5000], [7000, 6200], [6200, 7300], [4600, 7500], [3400, 6700], [3200, 5900]],
    ],
    roads: [
      { major: true, pts: [[1800, 5000], [2600, 4200], [5200, 3400], [7600, 3800], [8600, 5200], [8200, 6600], [6600, 7600], [4200, 7800], [2800, 6800], [1500, 5800]] },
    ],
    pois: [
      { n: 'Prien', x: 2600, z: 5600, t: 'city' },
      { n: 'Seebruck', x: 5200, z: 8100, t: 'town' },
      { n: 'Gstadt', x: 3300, z: 7600, t: 'town' },
      { n: 'Chieming', x: 7900, z: 6600, t: 'town' },
      { n: 'Übersee', x: 7600, z: 3600, t: 'town' },
      { n: 'Herreninsel', x: 5000, z: 6150, t: 'poi' },
      { n: 'Fraueninsel', x: 5950, z: 6350, t: 'poi' },
    ],
  },
  // Pripyat (20480): the Chernobyl exclusion zone — Pripyat city NW, the power
  // plant (ChNPP) with its cooling pond central-east, Chernobyl town SE, the Duga
  // radar in the forest, the Pripyat river curving down.
  pripyat: {
    coast: false,
    land: [[0, 0], [20480, 0], [20480, 20480], [0, 20480]],
    terrain: [
      { level: 1, poly: [[6000, 9000], [11000, 9500], [11500, 14000], [6500, 14500], [5000, 11500]] },
    ],
    lakes: [
      [[9200, 10000], [12500, 10200], [12700, 11400], [12400, 13800], [11000, 14000], [9400, 12800], [9100, 11200]],
    ],
    rivers: [
      [[3500, 18000], [4500, 15000], [6000, 12500], [7500, 10000], [8500, 7000], [9500, 4000]],
    ],
    roads: [
      { major: true, pts: [[6500, 15500], [8000, 13000], [9000, 11000], [10500, 8000], [12000, 5500]] },
      { pts: [[8500, 12500], [11500, 12000]] },
    ],
    pois: [
      { n: 'Pripyat', x: 6800, z: 15200, t: 'city' },
      { n: 'ChNPP', x: 8600, z: 12200, t: 'mil' },
      { n: 'Chernobyl', x: 12000, z: 5200, t: 'town' },
      { n: 'Duga Radar', x: 9200, z: 9000, t: 'poi' },
      { n: 'Jupiter Factory', x: 7200, z: 14200, t: 'poi' },
      { n: 'Kopachi', x: 8000, z: 10400, t: 'town' },
    ],
  },
  // Deer Isle (16384): a large detailed island. Rough outline — a highland core,
  // the international airport and the dam. Positions are approximate.
  deerisle: {
    coast: true,
    land: [
      [2000, 2500], [6000, 1800], [10000, 2200], [13500, 3500], [14500, 7000], [14000, 10500],
      [11500, 13500], [8000, 14500], [4500, 14000], [2200, 11500], [1500, 7500], [1800, 4500],
    ],
    terrain: [
      { level: 1, poly: [[5000, 6000], [10000, 5500], [11000, 10000], [6000, 10500], [4500, 8000]] },
      { level: 2, poly: [[7000, 7000], [8200, 6700], [9400, 6900], [9900, 8100], [9500, 9300], [8200, 9600], [7100, 9200], [6700, 8000]] },
    ],
    roads: [
      { major: true, pts: [[4000, 3500], [7000, 4500], [10000, 4500], [12500, 7000], [11000, 11000], [7500, 12500], [4500, 10500]] },
    ],
    pois: [
      { n: 'Deer Isle', x: 5000, z: 4200, t: 'city' },
      { n: 'International Airport', x: 11000, z: 5000, t: 'air' },
      { n: 'The Dam', x: 8200, z: 8200, t: 'poi' },
    ],
  },
  // Namalsk (12800): an arctic island ringed by pack ice, with a large central
  // lake, snow-capped mountains in the south-west, an airstrip centre-north and
  // the small Lantia island off the south-west. Shape read from the in-game map;
  // town positions approximate (the reference carries no labels).
  namalsk: {
    coast: true,
    land: [
      [3000, 2000], [5500, 2200], [7500, 3200], [9000, 4500], [9200, 6500],
      [8500, 8500], [9200, 10000], [7500, 11200], [5500, 11000], [4200, 9500],
      [3500, 7500], [2500, 6000], [2800, 4000],
    ],
    islands: [
      // Lantia — small island off the south-west.
      [[1400, 2200], [2400, 2000], [2700, 2900], [1900, 3300], [1200, 2900]],
    ],
    terrain: [
      // Snow-capped mountains in the south-west.
      { level: 2, poly: [[3000, 3000], [4500, 2800], [5200, 4000], [4600, 5200], [3200, 5000], [2700, 3900]] },
      { level: 1, poly: [[3000, 5500], [6500, 5000], [8500, 6000], [8200, 9000], [5500, 10000], [3800, 8500]] },
    ],
    lakes: [
      // The large central lake.
      [[5000, 6200], [6500, 6000], [7300, 6800], [7200, 8000], [6200, 8600], [5000, 8200], [4600, 7200]],
    ],
    roads: [
      { major: true, pts: [[2800, 6800], [4500, 8500], [5500, 9500], [7000, 10500]] },
      { pts: [[5500, 9500], [6500, 6000], [6800, 3200]] },
    ],
    pois: [
      { n: 'Vorkuta', x: 2700, z: 6900, t: 'city' },
      { n: 'Sebjan', x: 4700, z: 10200, t: 'town' },
      { n: 'Norinsk', x: 6600, z: 3200, t: 'town' },
      { n: 'Tara Harbor', x: 8400, z: 3800, t: 'town' },
      { n: 'Jalovisko', x: 5200, z: 5600, t: 'town' },
      { n: 'Airfield', x: 5500, z: 9000, t: 'air' },
      { n: 'Namalsk Lake', x: 6000, z: 7300, t: 'poi' },
      { n: 'Lantia', x: 1900, z: 2600, t: 'poi' },
    ],
  },
  // Bitterroot (12288): a mountainous inland US region. Jackson Falls (W),
  // Lakeland (centre), Tyler (SE), Broken Arrow (SW) are the main towns, with
  // military bases (March / Glasgow AFB, Fort Simmons) and a river valley
  // through the centre. Positions read from the in-game map.
  bitterroot: {
    coast: false,
    land: [[0, 0], [12288, 0], [12288, 12288], [0, 12288]],
    terrain: [
      { level: 1, poly: [[500, 3000], [4000, 2500], [7000, 3500], [9500, 3000], [11700, 4000], [11700, 9500], [8500, 10500], [5000, 10000], [2000, 10500], [500, 9000]] },
      { level: 2, poly: [[1500, 2500], [3800, 2200], [4600, 3800], [3800, 5200], [2000, 5000], [1200, 3800]] },
      { level: 2, poly: [[9800, 3500], [11500, 3800], [11700, 6200], [10200, 6600], [9500, 5000]] },
      { level: 2, poly: [[6000, 8200], [8200, 8000], [8600, 10000], [6400, 10200], [5600, 9000]] },
    ],
    lakes: [
      [[2200, 1600], [2900, 1500], [3200, 2000], [2700, 2400], [2100, 2100]],
      [[6900, 5600], [7500, 5500], [7700, 6100], [7100, 6300], [6700, 5900]],
    ],
    rivers: [
      [[6200, 11200], [6800, 9000], [7400, 6500], [7600, 4000], [7600, 1500]],
    ],
    roads: [
      { major: true, pts: [[2700, 8000], [5200, 5500], [7600, 5900], [10500, 4300], [10800, 900]] },
      { major: true, pts: [[2700, 8000], [4300, 11300], [7600, 11670], [10450, 11670]] },
      { pts: [[1500, 2500], [4300, 615], [7600, 615], [10800, 900]] },
    ],
    pois: [
      { n: 'Jackson Falls', x: 2700, z: 8000, t: 'city' },
      { n: 'Lakeland', x: 7600, z: 5900, t: 'city' },
      { n: 'Tyler', x: 10800, z: 900, t: 'city' },
      { n: 'Broken Arrow', x: 1500, z: 2500, t: 'city' },
      { n: 'March AFB', x: 10450, z: 11670, t: 'air' },
      { n: 'Glasgow AFB', x: 1230, z: 4300, t: 'air' },
      { n: 'International Airport', x: 7600, z: 1470, t: 'air' },
      { n: 'Bishop Air Station', x: 6760, z: 8600, t: 'air' },
      { n: 'Fort Simmons', x: 11300, z: 8850, t: 'mil' },
      { n: 'Hawkins Nuclear', x: 9600, z: 8850, t: 'mil' },
      { n: 'Black Rock Barracks', x: 5530, z: 11430, t: 'mil' },
      { n: 'Checkpoint West', x: 1230, z: 7130, t: 'mil' },
      { n: 'Centerville', x: 5200, z: 5500, t: 'town' },
      { n: 'Silver Springs', x: 5500, z: 3900, t: 'town' },
      { n: 'Fairfax', x: 7400, z: 3440, t: 'town' },
      { n: 'Juno', x: 8000, z: 2200, t: 'town' },
      { n: 'Blairsville', x: 9460, z: 1840, t: 'town' },
      { n: 'Darby', x: 10450, z: 4300, t: 'town' },
      { n: 'Rolling Hills', x: 9800, z: 5530, t: 'town' },
      { n: 'Riggins', x: 10700, z: 7130, t: 'town' },
      { n: 'Collins', x: 6760, z: 10690, t: 'town' },
      { n: 'Hamilton', x: 4300, z: 11300, t: 'town' },
      { n: 'Dixie Town', x: 2090, z: 11670, t: 'town' },
      { n: 'Jasper', x: 5900, z: 980, t: 'town' },
      { n: 'Coleman', x: 8850, z: 980, t: 'town' },
      { n: 'Eagle Mountain', x: 11400, z: 4670, t: 'town' },
      { n: 'Bitterroot Speedway', x: 9840, z: 7130, t: 'poi' },
      { n: 'Lake Horn', x: 2460, z: 1840, t: 'poi' },
    ],
  },
};

const SVGNS = 'http://www.w3.org/2000/svg';
function svgEl(tag, attrs, parent) {
  const el = document.createElementNS(SVGNS, tag);
  for (const [k, v] of Object.entries(attrs || {})) el.setAttribute(k, v);
  if (parent) parent.append(el);
  return el;
}

// drawPoi renders one typed schematic point of interest into parent: the marker
// shape and label weight vary by type — city (dot with a red core, bold label),
// town (small dot), mil military (triangle), air airfield (diamond), poi
// landmark (hollow ring). w2s converts world metres → the 0..1000 SVG space.
function drawPoi(parent, poi, w2s) {
  const [sx, sy] = w2s(poi.x, poi.z);
  const type = poi.t || 'town';
  // Each POI is a <g> translated to its point, marker + label drawn at the local
  // origin. buildGameMap counter-scales the group by the zoom factor so the
  // marker and label keep a constant on-screen size instead of ballooning, and
  // reads data-sx/sy to rebuild the transform. Cities / airfields / military are
  // "major" (labelled at any zoom); towns and landmarks are "minor" (labelled
  // only once zoomed in — see the gm-lod-in toggle) so the overview stays clean.
  const major = type === 'city' || type === 'air' || type === 'mil';
  const g = svgEl('g', { class: 'gm-poi-g gm-tier-' + (major ? 'major' : 'minor') + ' gm-t-' + type,
    'data-sx': sx, 'data-sy': sy, transform: `translate(${sx} ${sy})` }, parent);
  const tri = (r) => `M 0 ${-r} L ${r} ${r * 0.85} L ${-r} ${r * 0.85} Z`;
  const dia = (r) => `M 0 ${-r} L ${r} 0 L 0 ${r} L ${-r} 0 Z`;
  if (type === 'city') {
    svgEl('circle', { cx: 0, cy: 0, r: 3.4, class: 'gm-city' }, g);
    svgEl('circle', { cx: 0, cy: 0, r: 1.4, class: 'gm-city-core' }, g);
  } else if (type === 'mil') {
    svgEl('path', { d: tri(3.4), class: 'gm-mil' }, g);
  } else if (type === 'air') {
    svgEl('path', { d: dia(3.4), class: 'gm-air' }, g);
  } else if (type === 'poi') {
    svgEl('circle', { cx: 0, cy: 0, r: 2.7, class: 'gm-poi' }, g);
  } else {
    svgEl('circle', { cx: 0, cy: 0, r: 2, class: 'gm-town' }, g);
  }
  const cls = type === 'city' ? 'gm-town-label gm-city-label' : 'gm-town-label';
  svgEl('text', { x: 4.8, y: 3, class: cls }, g).textContent = poi.n;
}

// buildGameMap draws a correctly-scaled, license-safe schematic map: a 1 km
// grid with kilometre labels, known landmarks (Chernarus), and an empty <g>
// layer for callers to plot points into. Returns helpers to convert world
// coordinates to the 0..1000 SVG space and back.
//
// Pan/zoom is done by mutating the root viewBox (never a group transform) so
// world coordinates read from getScreenCTM().inverse() stay correct at any zoom
// or pan — see gm.userAt / clientToWorld / mapCoordReadout. opts.panLeft lets
// read-only maps (heat/live) pan with the left button at once; on editor maps a
// left/touch drag pans only once it moves, so a click still adds a point.
// Callers that plot points should set gm.onView(k) to re-scale point radii
// (gm.ptR / gm.px) so markers keep a constant on-screen size while zoomed.
// opts.onImageChange re-renders the page after the map picture changes (the
// picture controls live in the layers panel).
function buildGameMap(map, opts = {}) {
  const size = (map && map.size) || 15360;
  const S = 1000;
  const w2s = (x, z) => [x / size * S, S - z / size * S];        // flip Z (north up)
  const s2w = (sx, sy) => [sx / S * size, (S - sy) / S * size];  // inverse

  const hasImage = !!(map && map.hasImage);
  const svg = svgEl('svg', { viewBox: `0 0 ${S} ${S}`, class: 'game-map' + (hasImage ? ' has-img' : ''), preserveAspectRatio: 'xMidYMid meet' });
  svgEl('rect', { x: 0, y: 0, width: S, height: S, class: 'gm-bg' }, svg);

  // Admin-supplied background image, stretched to the world square so positions
  // line up. The version param busts the cache after a re-upload.
  if (hasImage) {
    const img = svgEl('image', { x: 0, y: 0, width: S, height: S, preserveAspectRatio: 'none' }, svg);
    img.setAttribute('href', srvUrl('/api/map/image?map=' + encodeURIComponent(map.key) + (map.imageVer ? '&v=' + map.imageVer : '')));
  }

  // Built-in schematic landmass (when there's no uploaded image).
  const shape = (!hasImage && map) ? MAP_SHAPES[map.key] : null;
  if (shape) {
    svg.classList.add('gm-has-shape');
    if (shape.arid) svg.classList.add('gm-arid'); // desert palette (Nasdara, Takistan)
    const toPath = (pts) => pts.map((p, i) => (i ? 'L' : 'M') + w2s(p[0], p[1]).join(' ')).join(' ') + ' Z';
    const toLine = (pts) => pts.map((p, i) => (i ? 'L' : 'M') + w2s(p[0], p[1]).join(' ')).join(' ');
    const isCoast = shape.coast !== false;
    // Base: sea for coastal/island worlds, land tint for landlocked ones.
    svgEl('rect', { x: 0, y: 0, width: S, height: S, class: isCoast ? 'gm-water' : 'gm-land' }, svg);
    if (shape.land) svgEl('path', { d: toPath(shape.land), class: 'gm-land' }, svg);
    for (const isl of (shape.islands || [])) svgEl('path', { d: toPath(isl), class: 'gm-land' }, svg);
    // Elevation: graduated hypsometric fills (lowland → peak, lowest drawn first),
    // then the same outlines as topographic contour lines. Original stylised
    // relief, not a trace of any real map.
    const bands = (shape.terrain || []).slice().sort((a, b) => (a.level || 1) - (b.level || 1));
    for (const band of bands) svgEl('path', { d: toPath(band.poly), class: 'gm-hi gm-hi' + (band.level || 1) }, svg);
    if (isCoast && shape.land) svgEl('path', { d: toPath(shape.land), class: 'gm-contour gm-shore' }, svg);
    for (const isl of (isCoast ? (shape.islands || []) : [])) svgEl('path', { d: toPath(isl), class: 'gm-contour gm-shore' }, svg);
    for (const band of bands) svgEl('path', { d: toPath(band.poly), class: 'gm-contour' }, svg);
    for (const lake of (shape.lakes || [])) svgEl('path', { d: toPath(lake), class: 'gm-water gm-lake' }, svg);
    // Rivers: open polylines (stroked, not filled), drawn over the land.
    for (const river of (shape.rivers || [])) svgEl('path', { d: toLine(river), class: 'gm-river' }, svg);
    // Main roads — light strokes; major arteries a touch heavier.
    for (const road of (shape.roads || [])) {
      svgEl('path', { d: toLine(road.pts || road), class: 'gm-road' + (road.major ? ' gm-road-major' : '') }, svg);
    }
  } else if (!hasImage) {
    // No schematic and no real image (Livonia/Sakhal have shapes; modded and
    // unknown worlds land here): a neutral land tint instead of a bare grid, so
    // the page never looks broken. A centred CTA (added after the grid) invites
    // uploading a real top-down map for accurate placement.
    svg.classList.add('gm-placeholder');
    svgEl('rect', { x: 0, y: 0, width: S, height: S, class: 'gm-land-soft' }, svg);
  }

  // Real-data overlays (land, loot tiers, buildings, zones) from the mission's
  // own files — filled by attachMapLayers (features.js).
  const overlay = svgEl('g', { class: 'gm-overlay' }, svg);

  const grid = svgEl('g', { class: 'gm-grid-g' }, svg);
  for (let m = 0; m <= size + 1; m += 1000) {
    const p = m / size * S;
    svgEl('line', { x1: p, y1: 0, x2: p, y2: S, class: 'gm-grid' }, grid);
    svgEl('line', { x1: 0, y1: p, x2: S, y2: p, class: 'gm-grid' }, grid);
    const km = m / 1000;
    if (km % 2 === 0 && m < size) {
      svgEl('text', { x: p + 3, y: 14, class: 'gm-axis' }, grid).textContent = km;
      svgEl('text', { x: 3, y: S - p - 4, class: 'gm-axis' }, grid).textContent = km;
    }
  }

  // Points of interest — typed markers with labels, only when there's no image
  // (a real map carries its own). Drawn from the schematic's POI list.
  const marks = svgEl('g', { class: 'gm-marks' }, svg);
  for (const poi of (hasImage || !shape ? [] : (shape.pois || []))) drawPoi(marks, poi, w2s);

  // Placeholder call-to-action: only when there's neither a real image nor a
  // built-in schematic — invites the admin to upload a top-down map image.
  if (!hasImage && !shape) {
    const cta = svgEl('g', { class: 'gm-cta' }, svg);
    const title = svgEl('text', { x: S / 2, y: S / 2 - 10, class: 'gm-cta-title', 'text-anchor': 'middle' }, cta);
    title.textContent = (map && map.key && map.key !== 'unknown') ? (map.name || map.key) : t('map.unknownWorld');
    const hint = svgEl('text', { x: S / 2, y: S / 2 + 16, class: 'gm-cta-hint', 'text-anchor': 'middle' }, cta);
    hint.textContent = t('map.uploadCta');
  }

  const layer = svgEl('g', { class: 'gm-layer' }, svg);

  // ---- pan / zoom (root viewBox mutation) -------------------------------
  const view = { x: 0, y: 0, w: S, h: S };
  const MINW = S / 10; // deepest zoom ≈ 10×
  // Marker and label sizes are tuned on the 760px desktop map. widthPx is the
  // map's real width on screen (~330px on a phone), so they keep that screen
  // size on any screen and at any zoom instead of shrinking to smudges.
  const REF_W = 760;
  const LOD_PX = REF_W / (S * 0.55); // screen px per unit at which minor labels show
  let widthPx = 0;
  const gm = {
    svg, w2s, s2w, size, layer, S, overlay,
    onView: null,
    // Shared tools (maptools.js) re-plot on zoom too: onView belongs to the page.
    viewHooks: [],
    get k() { return view.w / S; },              // <1 when zoomed in
    // n screen pixels in user units at the current zoom.
    px(n) { return n * view.w / (widthPx || REF_W); },
    // Point radius: `base` user units on the unzoomed desktop map, and the
    // same screen size at any zoom and on any screen.
    ptR(base) { return gm.px(base * REF_W / S); },
  };

  // Client pixel → user (0..1000) space via the live CTM, so it's correct at any
  // zoom/pan. Falls back to a plain ratio if the CTM isn't available yet.
  gm.userAt = (clientX, clientY) => {
    const ctm = svg.getScreenCTM();
    if (ctm && typeof svg.createSVGPoint === 'function') {
      const pt = svg.createSVGPoint(); pt.x = clientX; pt.y = clientY;
      const u = pt.matrixTransform(ctm.inverse());
      return [u.x, u.y];
    }
    const r = svg.getBoundingClientRect();
    return [view.x + (clientX - r.left) / r.width * view.w,
            view.y + (clientY - r.top) / r.height * view.h];
  };

  // Keep POI markers/labels their desktop screen size (counter-scale each <g>
  // around its own point by zoom and screen width) and reveal the minor labels
  // only once the map is shown big enough, so the overview stays uncluttered.
  // A narrow (phone) map labels only the cities until zoomed in.
  let lastPoiK = -1;
  function updatePois() {
    const w = widthPx || REF_W;
    marks.classList.toggle('gm-lod-in', w / view.w > LOD_PX);
    svg.classList.toggle('gm-narrow', w < 500);
    const sc = view.w / w * REF_W / S;
    if (Math.abs(sc - lastPoiK) < 0.0005) return; // only rescale when it changed
    lastPoiK = sc;
    const kk = sc.toFixed(4);
    for (const g of marks.querySelectorAll('.gm-poi-g')) {
      g.setAttribute('transform', `translate(${g.getAttribute('data-sx')} ${g.getAttribute('data-sy')}) scale(${kk})`);
    }
  }
  function applyView() {
    view.w = Math.min(S, Math.max(MINW, view.w));
    view.h = view.w; // keep square
    view.x = Math.min(S - view.w, Math.max(0, view.x));
    view.y = Math.min(S - view.h, Math.max(0, view.y));
    svg.setAttribute('viewBox', `${view.x} ${view.y} ${view.w} ${view.h}`);
    svg.classList.toggle('gm-zoomed', view.w < S - 0.5);
    updatePois();
    if (gm.onView) gm.onView(view.w / S);
    for (const fn of gm.viewHooks) fn(view.w / S);
  }
  // The on-screen width drives marker and label sizes: re-plot when it changes.
  if (typeof ResizeObserver === 'function') {
    const ro = new ResizeObserver(() => {
      if (!svg.isConnected) { ro.disconnect(); return; }
      const w = svg.getBoundingClientRect().width;
      if (w > 0 && Math.abs(w - widthPx) > 0.5) { widthPx = w; applyView(); }
    });
    ro.observe(svg);
  }
  function zoomTo(ux, uy, factor) {
    const newW = Math.min(S, Math.max(MINW, view.w * factor));
    const fx = (ux - view.x) / view.w, fy = (uy - view.y) / view.h; // cursor fraction
    view.w = newW;
    view.x = ux - fx * newW;
    view.y = uy - fy * newW;
    applyView();
  }
  gm.resetView = () => { view.x = 0; view.y = 0; view.w = S; view.h = S; applyView(); };
  // Centre on a world point, zooming in to width w (user units) if wider.
  gm.focus = (x, z, w = S / 6) => {
    const [sx, sy] = w2s(x, z);
    view.w = Math.min(view.w, w);
    view.h = view.w;
    view.x = sx - view.w / 2;
    view.y = sy - view.h / 2;
    applyView();
  };

  svg.addEventListener('wheel', (e) => {
    e.preventDefault();
    const [ux, uy] = gm.userAt(e.clientX, e.clientY);
    zoomTo(ux, uy, e.deltaY < 0 ? 0.83 : 1 / 0.83);
  }, { passive: false });

  // Drag to pan, two fingers to pinch — Pointer Events, so mouse, pen and touch
  // all work. The middle button pans at once, the left one too on read-only
  // maps (opts.panLeft); on editor maps a left/touch drag turns into a pan once
  // it has moved a few pixels, and the click that ends it is swallowed so it
  // adds no point. Editor points stop pointerdown to run their own drag.
  const ptrs = new Map(); // active pointerId → { x, y }
  let pan = null;         // { id, ux, uy, x0, y0, live (moves the view), moved (past the slop) }
  let pinch = null;       // { d0, w0, ux, uy } at the pinch start
  let swallowUntil = 0;
  // swallowClick drops the click (and context menu) that follows a pan, a
  // point drag or a long press within ms.
  gm.swallowClick = (ms = 400) => { swallowUntil = performance.now() + ms; };
  const swallow = (e) => {
    if (performance.now() >= swallowUntil) return false;
    e.preventDefault();
    e.stopImmediatePropagation();
    return true;
  };
  svg.addEventListener('click', (e) => { if (swallow(e)) swallowUntil = 0; }, true);
  svg.addEventListener('contextmenu', (e) => {
    // No browser menu over an editor map: right-click is "delete point" there.
    if (!swallow(e) && svg.classList.contains('gm-edit')) e.preventDefault();
  }, true);
  const startPan = (id, cx, cy, live) => {
    const [ux, uy] = gm.userAt(cx, cy);
    pan = { id, ux, uy, x0: cx, y0: cy, live, moved: false };
    svg.classList.toggle('gm-panning', live);
  };
  // Capture only once a drag is real: a capture set at pointerdown would make
  // the browser send the click to the map instead of the dot under it.
  const capture = (id) => { try { svg.setPointerCapture(id); } catch {} };
  svg.addEventListener('pointerdown', (e) => {
    if (e.pointerType === 'mouse' && e.button !== 0 && e.button !== 1) return;
    ptrs.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (ptrs.size === 2) {
      const [a, b] = [...ptrs.values()];
      const [ux, uy] = gm.userAt((a.x + b.x) / 2, (a.y + b.y) / 2);
      pinch = { d0: Math.hypot(a.x - b.x, a.y - b.y) || 1, w0: view.w, ux, uy };
      pan = null;
      svg.classList.remove('gm-panning');
      return;
    }
    if (ptrs.size > 2) return;
    if (e.button === 1) { e.preventDefault(); capture(e.pointerId); } // no autoscroll cursor
    startPan(e.pointerId, e.clientX, e.clientY, e.button === 1 || !!opts.panLeft);
  });
  svg.addEventListener('pointermove', (e) => {
    const p = ptrs.get(e.pointerId);
    if (!p) return;
    p.x = e.clientX; p.y = e.clientY;
    if (pinch) {
      if (ptrs.size < 2) return;
      const [a, b] = [...ptrs.values()];
      const r = svg.getBoundingClientRect();
      view.w = Math.min(S, Math.max(MINW, pinch.w0 * pinch.d0 / (Math.hypot(a.x - b.x, a.y - b.y) || 1)));
      view.h = view.w;
      // The pinched spot stays under the fingers' midpoint.
      view.x = pinch.ux - ((a.x + b.x) / 2 - r.left) / r.width * view.w;
      view.y = pinch.uy - ((a.y + b.y) / 2 - r.top) / r.height * view.h;
      applyView();
      return;
    }
    if (!pan || pan.id !== e.pointerId) return;
    if (!pan.moved && Math.hypot(e.clientX - pan.x0, e.clientY - pan.y0) >= (e.pointerType === 'mouse' ? 4 : 8)) {
      pan.moved = true;
      capture(e.pointerId); // keep panning when the pointer leaves the map
      if (!pan.live) { pan.live = true; svg.classList.add('gm-panning'); }
    }
    if (!pan.live) return;
    const [ux, uy] = gm.userAt(e.clientX, e.clientY);
    view.x += pan.ux - ux; // shift so the grabbed point stays under the pointer
    view.y += pan.uy - uy;
    applyView();
  });
  const endPointer = (e) => {
    if (!ptrs.delete(e.pointerId)) return;
    if (pinch) {
      if (ptrs.size >= 2) return;
      pinch = null;
      gm.swallowClick();
      // One finger still down: carry on panning with it.
      const rest = [...ptrs.entries()][0];
      if (rest) startPan(rest[0], rest[1].x, rest[1].y, true);
      return;
    }
    if (pan && pan.id === e.pointerId) {
      if (pan.moved) gm.swallowClick();
      pan = null;
      svg.classList.remove('gm-panning');
    }
  };
  svg.addEventListener('pointerup', endPointer);
  svg.addEventListener('pointercancel', endPointer);

  // On-screen controls (zoom in / out / reset) for discoverability.
  const zbtn = (sym, key, fn) => h('button', { class: 'gm-zbtn', type: 'button',
    title: t(key), 'aria-label': t(key), text: sym,
    onclick: (e) => { e.preventDefault(); fn(); } });
  gm.controls = h('div', { class: 'gm-zoom' }, [
    zbtn('＋', 'map.zoomIn', () => zoomTo(view.x + view.w / 2, view.y + view.h / 2, 0.7)),
    zbtn('－', 'map.zoomOut', () => zoomTo(view.x + view.w / 2, view.y + view.h / 2, 1 / 0.7)),
    zbtn('⟲', 'map.zoomReset', () => gm.resetView()),
  ]);
  if (typeof attachMapLayers === 'function') attachMapLayers(gm, map, opts);
  if (typeof attachMapTools === 'function') attachMapTools(gm, map, opts);

  return gm;
}

// mapPointDrag makes an editor point draggable with any pointer (mouse, pen,
// touch). A press-and-release selects it, a drag moves it; right-click — or a
// long press on touch, a phone's right-click — deletes it. The point stops the
// map's own pan. cb: { onMove(wx, wz), onEnd(moved), onDelete(), locked() }.
function mapPointDrag(gm, el, cb) {
  let lpTimer = 0, lpFired = false;
  el.addEventListener('pointerdown', (e) => {
    if (e.pointerType === 'mouse' && e.button !== 0) return;
    e.stopPropagation(); // a drag of the point, not of the map
    if (cb.locked && cb.locked()) return;
    const id = e.pointerId, x0 = e.clientX, y0 = e.clientY;
    let moved = false;
    lpFired = false;
    try { el.setPointerCapture(id); } catch {}
    const done = () => {
      clearTimeout(lpTimer);
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', up);
      el.removeEventListener('pointercancel', up);
    };
    const move = (ev) => {
      if (ev.pointerId !== id) return;
      if (!moved) {
        if (Math.hypot(ev.clientX - x0, ev.clientY - y0) < (ev.pointerType === 'mouse' ? 3 : 6)) return;
        moved = true;
        clearTimeout(lpTimer);
      }
      const [wx, wz] = clientToWorld(gm, ev);
      cb.onMove(wx, wz);
    };
    const up = (ev) => {
      if (ev.pointerId !== id) return;
      done();
      if (moved) gm.swallowClick();
      cb.onEnd(moved);
    };
    clearTimeout(lpTimer);
    if (e.pointerType !== 'mouse' && cb.onDelete) {
      lpTimer = setTimeout(() => {
        if (moved) return;
        lpFired = true;
        done();
        gm.swallowClick(800); // the lift and Android's own long-press menu
        cb.onDelete();
      }, 550);
    }
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', up);
    el.addEventListener('pointercancel', up);
  });
  el.addEventListener('contextmenu', (e) => {
    e.preventDefault();
    e.stopPropagation();
    clearTimeout(lpTimer);
    if (lpFired || !cb.onDelete || (cb.locked && cb.locked())) return;
    gm.swallowClick();
    cb.onDelete();
  });
}

// coarsePointer: the main input is a finger, so editor points get a larger
// invisible hit area around their small dot.
function coarsePointer() {
  return !!(window.matchMedia && window.matchMedia('(pointer: coarse)').matches);
}

// scrollBoxTo brings a row into view inside its own scroll box only —
// scrollIntoView also scrolled the page, pushing the map out of view.
function scrollBoxTo(box, row) {
  const b = box.getBoundingClientRect(), r = row.getBoundingClientRect();
  if (r.top < b.top || r.bottom > b.bottom) box.scrollTop += r.top - b.top - (box.clientHeight - r.height) / 2;
}

// scrollToCard scrolls the page so el starts just below the sticky topbar.
function scrollToCard(el) {
  const bar = document.querySelector('.topbar');
  const top = el.getBoundingClientRect().top + window.scrollY - ((bar ? bar.offsetHeight : 0) + 12);
  window.scrollTo({ top: Math.max(0, top), behavior: 'smooth' });
}

// mapCoordReadout wires a hover readout showing the world X/Z under the cursor.
// Uses the live CTM so the readout stays correct at any zoom/pan.
function mapCoordReadout(gm, readoutEl) {
  gm.svg.addEventListener('mousemove', (e) => {
    const [ux, uy] = gm.userAt(e.clientX, e.clientY);
    const [wx, wz] = gm.s2w(ux, uy);
    readoutEl.textContent = 'X ' + Math.round(wx) + ' · Z ' + Math.round(wz);
  });
  gm.svg.addEventListener('mouseleave', () => { readoutEl.textContent = ''; });
}

// mapImageControls returns an upload/remove control for a map's background
// image (shown in the map's layers panel). onDone re-renders the host view so
// the new (or removed) image shows.
function mapImageControls(map, onDone) {
  const key = (map && map.key) || 'unknown';
  const fileInput = h('input', { type: 'file', accept: 'image/png,image/jpeg,image/webp', style: { display: 'none' }, 'aria-label': t('map.image.upload') });
  fileInput.addEventListener('change', async () => {
    const f = fileInput.files && fileInput.files[0];
    if (!f) return;
    const fd = new FormData();
    fd.append('image', f);
    try {
      const r = await fetch('/api/map/image?map=' + encodeURIComponent(key), { method: 'POST', body: fd, headers: srvHeaders() });
      if (!r.ok) throw new Error(await r.text());
      toast(t('map.image.uploaded'), 'ok');
      onDone && onDone();
    } catch (e) { handleErr(e); }
    fileInput.value = '';
  });
  // Fetch a map picture from a URL the admin supplies. The manager ships no map
  // imagery — the admin points it at a source they have the right to use; the
  // server downloads it into this map's slot (see /api/map/image/fetch).
  async function fromUrl() {
    const url = await promptModal(t('map.image.urlPrompt'), { title: t('map.image.fromUrl'), placeholder: 'https://…', okText: t('map.image.fromUrl') });
    if (!url) return;
    try {
      await api.post('/api/map/image/fetch', { map: key, url });
      toast(t('map.image.uploaded'), 'ok');
      onDone && onDone();
    } catch (e) { handleErr(e); }
  }
  const wrap = h('div', { class: 'map-img-ctl' }, [
    h('button', { type: 'button', class: 'secondary small', i18n: 'map.bg.upload', title: t('map.image.hint'),
      onclick: () => fileInput.click() }),
    h('button', { type: 'button', class: 'secondary small', i18n: 'map.image.fromUrl', title: t('map.image.urlHint'),
      onclick: fromUrl }),
    fileInput,
  ]);
  if (map && map.hasImage) {
    wrap.append(h('button', { type: 'button', class: 'secondary small', i18n: 'map.image.remove', onclick: async () => {
      if (!(await confirmModal(t('map.image.removeConfirm'), { danger: true, okText: t('map.image.remove') }))) return;
      try {
        await api.post('/api/map/image/delete?map=' + encodeURIComponent(key), {});
        toast(t('map.image.removed'), 'ok');
        onDone && onDone();
      } catch (e) { handleErr(e); }
    } }));
  }
  // Modded/custom worlds: let the admin set the real edge length so the grid and
  // plotted points line up. Official worlds have a known size, so no field.
  if (map && !map.official && map.sizeFromMission) {
    // areaflags.map told us the real size — nothing for the admin to type.
    wrap.append(h('span', { class: 'badge info', text: t('map.sizeDetected').replace('{n}', map.size) }));
  } else if (map && !map.official) {
    const sizeInp = h('input', { type: 'number', value: map.size || 15360, min: '1000', style: { width: '110px' } });
    wrap.append(
      h('label', { class: 'map-size-lbl', i18n: 'map.size', for: assocId(sizeInp) }),
      sizeInp,
      h('button', { type: 'button', class: 'secondary small', i18n: 'action.save', title: t('map.size.hint'), onclick: async () => {
        const v = parseInt(sizeInp.value, 10);
        if (!(v >= 1000 && v <= 65536)) { toast(t('map.size.range'), 'warn'); return; }
        try { await api.post('/api/map/size', { map: key, size: v }); toast(t('msg.saved'), 'ok'); onDone && onDone(); }
        catch (e) { handleErr(e); }
      } }),
    );
  }
  return wrap;
}

// heatDeathText is one death for a heatmap dot's tooltip:
// "Miller ← Rook, SVD 167 m · 12.10 18:30".
function heatDeathText(p) {
  const by = p.kind === 'pvp' ? p.killer : (p.kind === 'env' ? p.source : '');
  let s = p.victim || '—';
  if (by) s += ' ← ' + by;
  else if (p.kind === 'suicide') s += ' · ' + t('heatmap.tip.suicide');
  const dist = parseFloat(p.distance);
  const how = [p.weapon, Number.isFinite(dist) && dist > 0 ? Math.round(dist) + ' ' + t('unit.m') : ''].filter(Boolean).join(' ');
  if (how) s += (by ? ', ' : ' · ') + how;
  if (p.at) s += ' · ' + fmtWhen(p.at);
  return s;
}

Views.heatmap = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.heatmap', 'heatmap.subtitle'));
  // Built from the admin log: with -adminlog off it would stay empty forever.
  { const w = adminLogWarning(); if (w) root.append(w); }
  let d;
  try { d = await api.get('/api/map/heat'); }
  catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;

  const points = d.points || [];
  const bases = (d.bases || []).filter(b => b.pos && b.pos.length >= 2);
  const card = h('div', { class: 'card' });
  root.append(card);

  if (d.map && d.map.key === 'unknown') {
    card.append(h('p', { class: 'hint', i18n: 'map.genericGrid' }));
  }
  // Building and raids are logged only with two serverDZ.cfg switches on.
  const baseLog = d.baseLog || {};
  if (!(baseLog.placement && baseLog.build)) {
    const tip = h('div', { class: 'live-tip' }, [
      h('span', { class: 'live-tip-text', i18n: 'heatmap.baseLog.hint' }),
      h('button', { type: 'button', class: 'secondary small', i18n: 'heatmap.baseLog.on',
        onclick: (e) => withBusy(e.currentTarget, async () => {
          try {
            const r = await api.post('/api/map/bases/log', {});
            toast(t(r.restart ? 'heatmap.baseLog.restart' : 'heatmap.baseLog.done'), 'ok');
            tip.remove();
          } catch (err) { handleErr(err); }
        }) }),
    ]);
    card.append(tip);
  }
  // The map shows even with nothing logged yet, so the background picture
  // (layers panel) can be set up on a fresh server.
  if (!points.length && !bases.length) {
    card.append(h('div', { class: 'empty-state map-empty' }, [
      h('div', { class: 'es-icon', html: svgIcon('i-heatmap', 34) }),
      h('div', { class: 'es-title', i18n: 'heatmap.empty.title' }),
      h('div', { class: 'es-hint', i18n: 'heatmap.none' }),
    ]));
  }

  // Period + kind filters; the counts follow the period.
  const PERIODS = [['24h', 86400], ['7d', 7 * 86400], ['30d', 30 * 86400]];
  const lastWipe = d.lastWipe ? Date.parse(d.lastWipe) : NaN;
  let period = 'all';
  try { period = localStorage.getItem('heatPeriod') || 'all'; } catch {}
  if (period === 'wipe' ? !Number.isFinite(lastWipe) : (period !== 'all' && !PERIODS.some(p => p[0] === period))) period = 'all';
  const periodSel = h('select', { class: 'heat-period', 'aria-label': t('heatmap.period') }, [
    ...PERIODS.map(([k]) => h('option', { value: k, i18n: 'heatmap.period.' + k })),
    Number.isFinite(lastWipe) ? h('option', { value: 'wipe', text: t('heatmap.period.wipe').replace('{date}', fmtWhen(d.lastWipe)) }) : null,
    h('option', { value: 'all', i18n: 'heatmap.period.all' }),
  ].filter(Boolean));
  periodSel.value = period;
  periodSel.addEventListener('change', () => {
    period = periodSel.value;
    try { localStorage.setItem('heatPeriod', period); } catch {}
    draw();
  });
  const since = () => {
    if (period === 'wipe') return lastWipe;
    const p = PERIODS.find(x => x[0] === period);
    return p ? Date.now() - p[1] * 1000 : -Infinity;
  };

  // Deaths and base building, each kind on its own switch (remembered).
  const show = { pvp: true, env: true, suicide: true, build: true, raid: true, stash: true };
  try { Object.assign(show, JSON.parse(localStorage.getItem('heatKinds') || '{}') || {}); } catch {}
  const readout = h('div', { class: 'map-readout' });
  const countEls = {};
  const legendGroup = (titleKey, kinds) => {
    const g = h('div', { class: 'map-legend' }, [h('span', { class: 'map-leg-title', i18n: titleKey })]);
    for (const [k, key, tip] of kinds) {
      const cb = h('input', { type: 'checkbox' });
      cb.checked = !!show[k];
      cb.addEventListener('change', () => {
        show[k] = cb.checked;
        try { localStorage.setItem('heatKinds', JSON.stringify(show)); } catch {}
        draw();
      });
      countEls[k] = h('span', { class: 'hint' });
      g.append(h('label', { class: 'map-leg map-leg-' + k, title: tip ? t(tip) : null }, [
        cb, h('span', { class: 'map-dot map-dot-' + k }),
        h('span', { i18n: key }), countEls[k],
      ]));
    }
    return g;
  };
  if (points.length || bases.length) {
    card.append(h('div', { class: 'map-toolbar' }, [
      h('label', { i18n: 'heatmap.period', for: assocId(periodSel) }), periodSel,
      h('div', { class: 'grow' }), readout,
    ]));
    card.append(h('div', { class: 'map-legends' }, [
      legendGroup('heatmap.group.deaths', [
        ['pvp', 'heatmap.pvp'],
        ['env', 'heatmap.env', 'heatmap.env.hint'],
        ['suicide', 'heatmap.suicide'],
      ]),
      legendGroup('heatmap.group.bases', [
        ['build', 'heatmap.build', 'heatmap.build.hint'],
        ['raid', 'heatmap.raid', 'heatmap.raid.hint'],
        ['stash', 'heatmap.stash', 'heatmap.stash.hint'],
      ]),
    ]));
  }

  const gm = buildGameMap(d.map, { panLeft: true, onImageChange: () => navigate('heatmap') });
  mapCoordReadout(gm, readout);
  card.append(h('div', { class: 'map-wrap' }, [gm.svg, gm.controls]));
  const foot = h('p', { class: 'hint' });
  if (points.length || bases.length) card.append(foot);

  // Shapes are rebuilt when a filter changes; zoom and pan only resize them.
  let dots = [], shapes = [];
  const BASE_SHAPE = {
    build: (g) => svgEl('rect', { x: -3.4, y: -3.4, width: 6.8, height: 6.8, rx: 1 }, g),
    raid: (g) => svgEl('path', { d: 'M0 -5L5 0L0 5L-5 0Z' }, g),
    stash: (g) => svgEl('path', { d: 'M0 -4.6L4.8 3.8L-4.8 3.8Z' }, g),
  };
  function draw() {
    gm.layer.innerHTML = '';
    dots = [];
    shapes = [];
    const from = since();
    const inPeriod = (at) => !(from > -Infinity) || Date.parse(at || '') >= from;
    const counts = { pvp: 0, env: 0, suicide: 0, build: 0, raid: 0, stash: 0 };
    const r = gm.ptR(4);
    // Bases first: the deaths stay on top.
    const k = r / 4;
    for (const b of bases) {
      if (!inPeriod(b.at) || !(b.kind in BASE_SHAPE)) continue;
      counts[b.kind]++;
      if (!show[b.kind]) continue;
      const [sx, sy] = gm.w2s(b.pos[0], b.pos[1]);
      const g = svgEl('g', { class: 'heat-b heat-' + b.kind, transform: `translate(${sx} ${sy}) scale(${k})` }, gm.layer);
      BASE_SHAPE[b.kind](g);
      const text = heatBaseText(b);
      svgEl('title', {}, g).textContent = text;
      g.addEventListener('click', () => toast(text));
      shapes.push([g, sx, sy]);
    }
    for (const p of points) {
      if (!inPeriod(p.at)) continue;
      counts[p.kind in counts ? p.kind : 'env']++;
      if (!show[p.kind]) continue;
      const [sx, sy] = gm.w2s(p.x, p.z);
      const c = svgEl('circle', { cx: sx, cy: sy, r, class: 'heat heat-' + p.kind }, gm.layer);
      const text = heatDeathText(p);
      svgEl('title', {}, c).textContent = text;
      c.addEventListener('click', () => toast(text)); // touch screens have no hover
      dots.push(c);
    }
    for (const kk of Object.keys(countEls)) countEls[kk].textContent = '(' + counts[kk] + ')';
    const n = Object.values(counts).reduce((a, b) => a + b, 0);
    foot.textContent = (d.map ? d.map.name + ' · ' : '') + (n ? tn('heatmap.points', { n }) : t('heatmap.nonePeriod'));
  }
  gm.onView = () => {
    const r = gm.ptR(4);
    for (const c of dots) c.setAttribute('r', r);
    const k = (r / 4).toFixed(4);
    for (const [g, sx, sy] of shapes) g.setAttribute('transform', `translate(${sx} ${sy}) scale(${k})`);
  };
  draw();
  applyI18n();
};

// heatBaseText is a base event as one line: who did what, with what, when.
function heatBaseText(b) {
  const key = {
    placed: 'heatmap.act.placed', built: 'heatmap.act.built', destroyed: 'heatmap.act.destroyed',
    'flag-up': 'heatmap.act.flagUp', 'flag-down': 'heatmap.act.flagDown',
    'dug-in': 'heatmap.act.dugIn', 'dug-out': 'heatmap.act.dugOut',
  }[b.action] || 'heatmap.act.placed';
  let s = t(key).replace('{player}', b.player || '—').replace('{what}', b.object || b.class || '?').replace('{part}', b.part || '');
  if (b.tool && b.tool !== 'hands') s += ' · ' + b.tool;
  if (b.at) s += ' · ' + fmtWhen(b.at);
  return s;
}

// ------------------------------------------------------------- event spawns

function clientToWorld(gm, e) {
  const [ux, uy] = gm.userAt(e.clientX, e.clientY);
  return gm.s2w(ux, uy);
}

// Views.eventmap lives in features.js (v1.0.0 editor with manual X/Y/Z editing
// and coordinate-list import).

// --------------------------------------------------------- player spawn points

// PS_PARAM_UNIT is the unit shown after a cfgplayerspawnpoints value. Labels
// and tooltips are i18n keys (spawnpoints.param.<key> and .tip); an unknown
// (modded) key shows its raw XML name and still round-trips.
const PS_PARAM_UNIT = {
  min_dist_infected: 'm', max_dist_infected: 'm', min_dist_player: 'm', max_dist_player: 'm',
  min_dist_static: 'm', max_dist_static: 'm', grid_width: 'm', grid_height: 'm',
  min_steepness: 'deg', max_steepness: 'deg', lifetime: 's',
};

Views.spawnpoints = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.spawnpoints', 'spawnpoints.subtitle', [revealBtn({ kind: 'missionfile', path: 'cfgplayerspawnpoints.xml' })]));
  const running = !!State.serverStatus.running;
  if (running) root.append(runningBanner());

  let d;
  try { d = await api.get('/api/spawnpoints'); }
  catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;

  const SECTIONS = ['fresh', 'hop', 'travel'];
  const doc = d.doc || {};
  for (const s of SECTIONS) {
    doc[s] = doc[s] || {};
    doc[s].groups = doc[s].groups || [];
    doc[s].spawnParams = doc[s].spawnParams || [];
    doc[s].genParams = doc[s].genParams || [];
    doc[s].groupParams = doc[s].groupParams || [];
    for (const g of doc[s].groups) g.pos = g.pos || [];
  }
  let curSection = SECTIONS.find(s => doc[s].present) || 'fresh';
  doc[curSection].present = true;
  let curGroup = 0;
  let selPt = -1;    // selected point of the active group
  // Unsaved = the document differs from the one loaded or last saved; every
  // change before that is an undo step (Ctrl+Z).
  let saved = JSON.stringify(doc);
  let last = saved;
  let undo = [];
  let lastKey = '', lastAt = 0;
  const isDirty = () => JSON.stringify(doc) !== saved;
  root._isDirty = isDirty;
  // 16 clearly different colours; more groups go on around the colour wheel.
  const PALETTE = ['#ff7a2b', '#4da3ff', '#4dd08a', '#e05c8a', '#b98cff', '#f2c14e', '#3fb9bd', '#ff4d4d',
    '#8fca4d', '#2457c5', '#d65db1', '#a8743a', '#7cf2e1', '#7a4fd1', '#2e9e4f', '#ffb3c7'];
  const colorFor = (i) => (i < PALETTE.length ? PALETTE[i] : `hsl(${Math.round(i * 137.5) % 360} 75% 58%)`);
  const sec = () => doc[curSection];
  const group = () => sec().groups[curGroup] || null;

  const gm = buildGameMap(d.map, {
    onImageChange: () => { if (isDirty()) toast(t('map.image.savedReload'), 'ok'); else navigate('spawnpoints'); },
  });
  gm.svg.classList.add('gm-edit');
  const coarse = coarsePointer();

  // ---- saving: bottom button + a sticky bar while anything is unsaved
  const saveBtn = h('button', { type: 'button', class: 'primary', i18n: 'action.save', disabled: true, onclick: (e) => save(e.currentTarget) });
  const barSave = h('button', { type: 'button', class: 'primary small', i18n: 'action.save', disabled: running, onclick: (e) => save(e.currentTarget) });
  const undoBtn = h('button', { type: 'button', class: 'secondary small', i18n: 'map.undo', title: t('map.undo.hint'), onclick: () => undoStep() });
  const bar = h('div', { class: 'sticky-save', hidden: true }, [
    h('span', { i18n: 'map.unsaved' }), h('span', { class: 'grow' }), undoBtn,
    // navigate() asks before throwing the edits away.
    h('button', { type: 'button', class: 'secondary small', i18n: 'action.discard', onclick: () => navigate('spawnpoints') }),
    barSave,
  ]);
  function updateBar() {
    const dirty = isDirty();
    bar.hidden = !dirty;
    saveBtn.disabled = running || !dirty;
    undoBtn.disabled = !undo.length;
  }
  // markDirty records a change. Typing into one field (same key, within a
  // moment) is one undo step, not one per keystroke.
  function markDirty(key) {
    const now = JSON.stringify(doc);
    if (now !== last) {
      if (!(key && key === lastKey && Date.now() - lastAt < 1500)) {
        undo.push(last);
        if (undo.length > 100) undo.shift();
      }
      last = now;
    }
    lastKey = key || '';
    lastAt = Date.now();
    updateBar();
  }
  function undoStep() {
    if (!undo.length) return;
    last = undo.pop();
    lastKey = '';
    const prev = JSON.parse(last);
    for (const s of SECTIONS) doc[s] = prev[s];
    selPt = -1;
    renderAll();
    updateBar();
  }
  root._save = () => (isDirty() ? save(barSave) : undefined);
  // Ctrl+Z undoes, Delete removes the selected point — except while typing in
  // a field, where both keep their usual meaning.
  const onKey = (e) => {
    if (myNav !== _navSeq || document.querySelector('.modal-overlay')) return;
    const el = e.target;
    if (el && (/^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName) || el.isContentEditable)) return;
    if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey && (e.code === 'KeyZ' || e.key === 'z' || e.key === 'Z')) {
      e.preventDefault();
      undoStep();
    } else if (e.key === 'Delete' && selPt >= 0 && !running) {
      e.preventDefault();
      removePt(selPt);
    }
  };
  document.addEventListener('keydown', onKey);
  root._teardown = () => document.removeEventListener('keydown', onKey);

  const readout = h('div', { class: 'map-readout' });
  const countLbl = h('span', { class: 'hint' });

  // ---- section tabs ----
  const tabsHost = h('div', { class: 'sp-tabs' });
  function renderTabs() {
    tabsHost.innerHTML = '';
    for (const s of SECTIONS) {
      const present = !!doc[s].present;
      const tab = h('button', {
        type: 'button',
        class: 'sp-tab' + (s === curSection ? ' active' : '') + (present ? '' : ' sp-absent'),
        text: t('spawnpoints.section.' + s) + (present ? '' : ' +'),
        title: present ? '' : t('spawnpoints.addSection'),
        onclick: () => {
          if (!doc[s].present) { doc[s].present = true; markDirty(); }
          curSection = s; curGroup = 0; selPt = -1; renderAll();
        },
      });
      tabsHost.append(tab);
    }
  }

  // ---- group chips + add ----
  const groupHost = h('div', { class: 'sp-groups' });
  function renderGroups() {
    groupHost.innerHTML = '';
    const gs = sec().groups;
    if (curGroup >= gs.length) curGroup = Math.max(0, gs.length - 1);
    gs.forEach((g, i) => {
      groupHost.append(h('button', {
        type: 'button',
        class: 'sp-group-chip' + (i === curGroup ? ' active' : ''),
        onclick: () => { curGroup = i; selPt = -1; renderMap(); renderGroups(); renderGroupEdit(); renderList(); },
      }, [
        h('span', { class: 'sp-dot', style: { background: colorFor(i) } }),
        h('span', { class: 'sp-gname', text: g.name || '—' }),
        h('span', { class: 'sp-gcount', text: String(g.pos.length) }),
      ]));
    });
    groupHost.append(h('button', { type: 'button', class: 'secondary small', i18n: 'spawnpoints.addGroup', disabled: running,
      onclick: () => {
        sec().groups.push({ name: 'group' + (sec().groups.length + 1), block: 0, pos: [] });
        curGroup = sec().groups.length - 1; selPt = -1;
        markDirty(); renderGroups(); renderGroupEdit(); renderMap(); renderList();
      } }));
  }

  // ---- active-group name / delete ----
  const groupEdit = h('div', { class: 'sp-group-edit' });
  function renderGroupEdit() {
    groupEdit.innerHTML = '';
    const g = group();
    if (!g) { groupEdit.append(h('p', { class: 'hint', i18n: 'spawnpoints.noGroup' })); applyI18n(); return; }
    const nameInp = h('input', { type: 'text', value: g.name, placeholder: t('spawnpoints.groupName'), 'aria-label': t('spawnpoints.groupName'), disabled: running });
    nameInp.addEventListener('input', () => { g.name = nameInp.value; markDirty('name:' + curSection + curGroup); });
    nameInp.addEventListener('change', () => { renderGroups(); renderList(); });
    const del = h('button', { type: 'button', class: 'danger small', i18n: 'spawnpoints.delGroup', disabled: running,
      onclick: async () => {
        if (!(await confirmModal(t('spawnpoints.delGroupConfirm').replace('{name}', g.name || '—')))) return;
        sec().groups.splice(curGroup, 1); curGroup = Math.max(0, curGroup - 1); selPt = -1; markDirty(); renderAll();
      } });
    groupEdit.append(
      h('span', { class: 'sp-active-lbl' }, [h('span', { class: 'sp-dot', style: { background: colorFor(curGroup) } }), h('span', { i18n: 'spawnpoints.activeGroup' })]),
      nameInp, del);
  }

  // ---- map points ----
  // Other groups: plain dots under the active group's. Active group: draggable
  // (any pointer), right-click / long-press / Delete removes, a press selects.
  let act = [];   // per active point: { c, hit }
  const selInfo = h('span', { class: 'hint sp-sel-info' });
  const delPtBtn = h('button', { type: 'button', class: 'danger small', i18n: 'spawnpoints.delPoint', disabled: running,
    onclick: () => { if (selPt >= 0) removePt(selPt); } });
  delPtBtn.hidden = true;
  function renderSel() {
    const g = group();
    const p = g && selPt >= 0 ? g.pos[selPt] : null;
    selInfo.textContent = p ? '#' + (selPt + 1) + ' · X ' + p.x + ' · Z ' + p.z : '';
    delPtBtn.hidden = !p;
  }
  function renderMap() {
    gm.layer.innerHTML = '';
    act = [];
    const gs = sec().groups;
    const rI = gm.px(3.5), rA = gm.px(5);
    gs.forEach((g, gi) => {
      if (gi === curGroup) return;
      for (const p of g.pos) {
        const [sx, sy] = gm.w2s(p.x, p.z);
        svgEl('circle', { cx: sx, cy: sy, r: rI, class: 'sp-pt sp-pt-other', fill: colorFor(gi) }, gm.layer);
      }
    });
    const g = group();
    if (g) g.pos.forEach((p, j) => {
      const [sx, sy] = gm.w2s(p.x, p.z);
      const grp = svgEl('g', { class: 'sp-pt-g' }, gm.layer);
      // Touch: a finger-sized invisible target around the dot.
      const hit = coarse ? svgEl('circle', { cx: sx, cy: sy, r: gm.px(14), class: 'map-hit' }, grp) : null;
      const c = svgEl('circle', { cx: sx, cy: sy, r: j === selPt ? rA * 1.4 : rA,
        class: 'sp-pt sp-pt-active' + (j === selPt ? ' sel' : ''), fill: colorFor(curGroup) }, grp);
      act[j] = { c, hit };
      if (running) return;
      mapPointDrag(gm, grp, {
        onMove: (wx, wz) => {
          p.x = round2(wx);
          p.z = round2(wz);
          const [nx, ny] = gm.w2s(p.x, p.z);
          for (const el of grp.children) { el.setAttribute('cx', nx); el.setAttribute('cy', ny); }
          readout.textContent = 'X ' + Math.round(wx) + ' · Z ' + Math.round(wz);
        },
        onEnd: (moved) => {
          if (moved) { markDirty(); renderList(); }
          // After the click this press makes, so that click still lands on
          // the point (and adds nothing to the map).
          setTimeout(() => selectPt(j, true), 0);
        },
        onDelete: () => removePt(j),
      });
    });
    countLbl.textContent = tn('spawnpoints.points', { n: g ? g.pos.length : 0 });
    renderSel();
  }
  // Zoom and pan only resize the dots.
  gm.onView = () => {
    const rI = gm.px(3.5), rA = gm.px(5), hr = gm.px(14);
    for (const c of gm.layer.querySelectorAll('.sp-pt-other')) c.setAttribute('r', rI);
    act.forEach((e, j) => {
      e.c.setAttribute('r', j === selPt ? rA * 1.4 : rA);
      if (e.hit) e.hit.setAttribute('r', hr);
    });
  };
  function selectPt(j, scroll) {
    selPt = j;
    renderMap();
    listHost.querySelectorAll('tr.sel').forEach(tr => tr.classList.remove('sel'));
    const tr = j >= 0 ? listHost.querySelector(`tr[data-j="${j}"]`) : null;
    if (!tr) return;
    tr.classList.add('sel');
    if (scroll) scrollBoxTo(listHost, tr);
  }
  function removePt(j) {
    const g = group();
    if (!g || j < 0 || j >= g.pos.length) return;
    g.pos.splice(j, 1);
    if (selPt === j) selPt = -1; else if (selPt > j) selPt--;
    markDirty(); renderMap(); renderGroups(); renderList();
  }
  // A click on empty map adds a point to the active group. A drag that panned
  // the map, or a point's own press, never gets here (see buildGameMap).
  gm.svg.addEventListener('click', (e) => {
    if (running) return;
    if (e.target && e.target.closest && e.target.closest('.sp-pt-g')) return;
    const g = group();
    if (!g) return;
    const [wx, wz] = clientToWorld(gm, e);
    g.pos.push({ x: round2(wx), z: round2(wz) });
    markDirty(); renderGroups(); renderList();
    selectPt(g.pos.length - 1, true);
  });

  // ---- the active group's points as a list: exact X / Z, paste, export ----
  const listTitle = h('h3');
  const listHost = h('div', { class: 'sp-list-wrap' });
  const showOnMap = () => {
    const r = gm.svg.getBoundingClientRect();
    if (r.bottom < 80 || r.top > window.innerHeight - 80) scrollToCard(mapCard);
  };
  function renderList() {
    listHost.innerHTML = '';
    const g = group();
    listTitle.textContent = g ? t('spawnpoints.list.title').replace('{name}', g.name || '—') : t('spawnpoints.list.titleNone');
    if (!g || !g.pos.length) {
      listHost.append(h('p', { class: 'hint', i18n: g ? 'spawnpoints.list.empty' : 'spawnpoints.noGroup' }));
      applyI18n();
      return;
    }
    const tbl = h('table', { class: 'sp-list' });
    tbl.append(h('thead', {}, h('tr', {}, [
      h('th', { text: '#' }), h('th', { class: 'num', text: 'X' }), h('th', { class: 'num', text: 'Z' }), h('th', { text: '' }),
    ])));
    const tb = h('tbody');
    g.pos.forEach((p, j) => {
      const cell = (f, label) => {
        const inp = h('input', { type: 'number', step: 'any', class: 'inline-cell', value: String(round2(p[f])), disabled: running, 'aria-label': `#${j + 1} ${label}` });
        inp.addEventListener('focus', () => { if (selPt !== j) selectPt(j, false); });
        inp.addEventListener('change', () => {
          const n = parseFloat(inp.value);
          if (!Number.isFinite(n)) { inp.value = String(round2(p[f])); return; }
          p[f] = round2(n);
          markDirty(); renderMap();
        });
        return h('td', { class: 'num' }, inp);
      };
      tb.append(h('tr', { 'data-j': String(j), class: j === selPt ? 'sel' : '' }, [
        h('td', { class: 'hint', text: String(j + 1) }), cell('x', 'X'), cell('z', 'Z'),
        h('td', {}, h('div', { class: 'row', style: { gap: '4px', flexWrap: 'nowrap', justifyContent: 'flex-end' } }, [
          h('button', { type: 'button', class: 'secondary icon-only', title: t('eventmap.zoomTo'), 'aria-label': t('eventmap.zoomTo'), html: svgIcon('i-search', 14),
            onclick: () => { selectPt(j, false); gm.focus(p.x, p.z); showOnMap(); } }),
          h('button', { type: 'button', class: 'danger icon-only', title: t('action.delete'), 'aria-label': t('action.delete'), text: '×', disabled: running,
            onclick: () => removePt(j) }),
        ])),
      ]));
    });
    tbl.append(tb);
    listHost.append(tbl);
  }

  // Paste: X Z per line (iZurvive "x / z"), X Y Z (DayZ Editor) or <pos/> lines.
  const pasteArea = h('textarea', { class: 'coord-paste sp-paste', placeholder: '7500.12 3200.40\n7512.5 / 3190.2\n<pos x="7486.2" z="3211.9" />' });
  const pasteInfo = h('div', { class: 'hint' });
  pasteArea.addEventListener('input', () => {
    const r = parseCoordList(pasteArea.value, 'xyz');
    pasteInfo.textContent = pasteArea.value.trim() ? tn('eventmap.paste.parsed', { n: r.points.length, bad: r.bad }) : '';
  });
  const pasteTo = async (replace) => {
    const g = group();
    if (!g) return;
    const r = parseCoordList(pasteArea.value, 'xyz');
    if (!r.points.length) { toast(t('eventmap.paste.none'), 'warn'); return; }
    const pts = r.points.map(p => ({ x: round2(p.x), z: round2(p.z) }));
    if (replace) {
      if (!(await confirmModal(tn('spawnpoints.paste.replaceConfirm', { name: g.name || '—', n: pts.length }), { danger: true }))) return;
      g.pos = pts;
      selPt = -1;
    } else g.pos.push(...pts);
    markDirty(); renderMap(); renderGroups(); renderList();
    toast(tn('eventmap.paste.added', { n: pts.length }), 'ok');
    pasteArea.value = '';
    pasteInfo.textContent = '';
  };

  // ---- params (spawn / generator / group) of the selected section ----
  const UNIT = { m: () => t('unit.m'), deg: () => '°', s: () => t('unit.seconds') };
  const paramsTitle = h('h3');
  const paramsHost = h('div', { class: 'sp-params' });
  function renderParams() {
    paramsHost.innerHTML = '';
    paramsTitle.textContent = t('spawnpoints.paramsFor').replace('{section}', t('spawnpoints.section.' + curSection));
    const s = sec();
    const block = (titleKey, kvs, openIt) => {
      if (!kvs || !kvs.length) return null;
      const grid = h('div', { class: 'sp-param-grid' });
      kvs.forEach(kv => {
        const lk = 'spawnpoints.param.' + kv.key;
        const known = t(lk) !== lk;
        const tip = known && t(lk + '.tip') !== lk + '.tip' ? t(lk + '.tip') : '';
        const text = known ? t(lk) : kv.key;
        const key = known ? h('code', { class: 'cfg-key', text: kv.key }) : null;
        const v = String(kv.value ?? '').trim();
        // true/false values are a switch, numbers a number field with a unit.
        if (/^(true|false)$/i.test(v)) {
          const cb = h('input', { type: 'checkbox', class: 'switch', disabled: running });
          cb.checked = v.toLowerCase() === 'true';
          cb.addEventListener('change', () => { kv.value = cb.checked ? 'true' : 'false'; markDirty(); });
          grid.append(h('label', { class: 'cfg-bool sp-param-bool', title: tip }, [cb, h('span', { class: 'cfg-bool-text' }, [
            h('span', { class: 'cfg-bool-label', text }), key].filter(Boolean))]));
          return;
        }
        const isNum = v !== '' && Number.isFinite(Number(v));
        const inp = h('input', { type: isNum ? 'number' : 'text', step: isNum ? 'any' : null, value: kv.value ?? '', disabled: running });
        inp.addEventListener('input', () => { kv.value = inp.value; markDirty('p:' + curSection + kv.key); });
        const unit = UNIT[PS_PARAM_UNIT[kv.key]];
        grid.append(h('div', { class: 'sp-param' }, [
          h('div', { class: 'cfg-label' }, [h('label', { for: assocId(inp), text, title: tip }), key].filter(Boolean)),
          unit ? h('div', { class: 'sp-unit-wrap' }, [inp, h('span', { class: 'sp-unit', text: unit() })]) : inp,
        ]));
      });
      const det = h('details', { class: 'sp-param-block' }, [h('summary', { i18n: titleKey }), grid]);
      if (openIt) det.setAttribute('open', '');
      return det;
    };
    const bs = [
      block('spawnpoints.spawnParams', s.spawnParams, true),
      block('spawnpoints.genParams', s.genParams, false),
      block('spawnpoints.groupParams', s.groupParams, false),
    ].filter(Boolean);
    if (!bs.length) paramsHost.append(h('p', { class: 'hint', i18n: 'spawnpoints.noParams' }));
    else bs.forEach(b => paramsHost.append(b));
  }

  const mapCard = h('div', { class: 'card' }, [
    tabsHost,
    h('p', { class: 'hint', i18n: 'spawnpoints.help' }),
    groupHost,
    groupEdit,
    h('div', { class: 'map-toolbar' }, [selInfo, delPtBtn, h('div', { class: 'grow' }), countLbl, readout]),
    h('div', { class: 'map-wrap' }, [gm.svg, gm.controls]),
  ]);
  root.append(
    mapCard,
    h('div', { class: 'card' }, [
      listTitle,
      h('div', { class: 'sp-list-cols' }, [
        listHost,
        h('div', { class: 'sp-paste-col' }, [
          h('label', { i18n: 'eventmap.paste.title', for: assocId(pasteArea) }),
          pasteArea,
          h('small', { class: 'hint', i18n: 'spawnpoints.paste.hint' }),
          pasteInfo,
          h('div', { class: 'actions' }, [
            h('button', { type: 'button', class: 'primary', i18n: 'spawnpoints.paste.add', disabled: running, onclick: () => pasteTo(false) }),
            h('button', { type: 'button', i18n: 'eventmap.paste.replace', disabled: running, onclick: () => pasteTo(true) }),
            h('button', { type: 'button', class: 'secondary', i18n: 'eventmap.export', onclick: () => {
              const g = group();
              if (g) copyText(g.pos.map(p => `<pos x="${p.x}" z="${p.z}" />`).join('\n'));
            } }),
          ]),
        ]),
      ]),
    ]),
    h('div', { class: 'card' }, [paramsTitle, paramsHost, h('div', { class: 'actions' }, [saveBtn])]),
    bar,
  );
  mapCoordReadout(gm, readout);

  function renderAll() { renderTabs(); renderGroups(); renderGroupEdit(); renderMap(); renderList(); renderParams(); applyI18n(); }

  async function save(btn) {
    for (const s of SECTIONS) {
      if (doc[s].present) {
        for (const g of doc[s].groups) {
          if (!(g.name || '').trim()) { toast(t('spawnpoints.needGroupName'), 'error'); return; }
        }
      }
    }
    await withBusy(btn, async () => {
      try {
        await api.post('/api/spawnpoints', doc);
        toast(t('spawnpoints.saved'), 'ok');
        saved = JSON.stringify(doc);
        updateBar();
      } catch (e) { handleErr(e); }
    });
  }

  renderAll();
  updateBar();
};

// ---------------------------------------------------------------- live map

Views.livemap = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.livemap', 'livemap.subtitle'));
  { const w = adminLogWarning(); if (w) root.append(w); }
  const status = h('span', { class: 'hint' });
  const rconLink = h('button', { type: 'button', class: 'link-btn', i18n: 'livemap.setupRcon', onclick: () => navigate('rcon') });
  rconLink.hidden = true;
  const updated = h('span', { class: 'hint live-updated' });
  const stoppedHost = h('div');
  const tipHost = h('div');
  const mapHost = h('div', { class: 'live-map-col' });
  const listHost = h('div', { class: 'live-list' });
  const layout = h('div', { class: 'live-layout' }, [mapHost, listHost]);
  // Trails: where each player went before the current dot (players.go keeps
  // the last two hours in memory).
  let trailsOn = true;
  try { trailsOn = localStorage.getItem('liveTrails') !== '0'; } catch {}
  const trailsCb = h('input', { type: 'checkbox' });
  trailsCb.checked = trailsOn;
  trailsCb.addEventListener('change', () => {
    trailsOn = trailsCb.checked;
    try { localStorage.setItem('liveTrails', trailsOn ? '1' : '0'); } catch {}
    plotPlayers();
  });
  const readout = h('div', { class: 'map-readout' });
  root.append(h('div', { class: 'card' }, [
    h('div', { class: 'map-toolbar' }, [
      status, rconLink, h('div', { class: 'grow' }),
      h('label', { class: 'map-leg', title: t('livemap.trails.hint') }, [trailsCb, h('span', { class: 'live-trail-swatch' }), h('span', { i18n: 'livemap.trails' })]),
      readout, updated,
    ]),
    stoppedHost,
    tipHost,
    layout,
    h('p', { class: 'hint', i18n: 'livemap.note' }),
  ]));
  // The square map takes the height left below the bars, so the whole map
  // is in view without scrolling (never smaller than 420px).
  const fitMap = () => {
    const top = layout.getBoundingClientRect().top + (document.scrollingElement ? document.scrollingElement.scrollTop : 0);
    const size = Math.max(420, Math.min(880, Math.floor(window.innerHeight - top - 20)));
    layout.style.setProperty('--live-map', size + 'px');
  };
  window.addEventListener('resize', fitMap);

  // Without adminLogPlayerList a dot only moves on connect, combat and chat;
  // with it DayZ logs every player's position every five minutes.
  let tipShown = null;
  function renderTip(on) {
    if (tipShown === on) return;
    tipShown = on;
    tipHost.innerHTML = '';
    if (on) return;
    tipHost.append(h('div', { class: 'live-tip' }, [
      h('span', { class: 'live-tip-text', i18n: 'livemap.playerList.hint' }),
      h('button', { type: 'button', class: 'secondary small', i18n: 'livemap.playerList.on',
        onclick: (e) => withBusy(e.currentTarget, async () => {
          try {
            const r = await api.post('/api/map/live/playerlist', {});
            toast(t(r.restart ? 'livemap.playerList.restart' : 'livemap.playerList.done'), 'ok');
            renderTip(true);
          } catch (err) { handleErr(err); }
        }) }),
    ]));
    applyI18n();
  }

  let gm = null, mapWrap = null, timer = null, lastPlayers = [], focusKey = '';
  // A profile's "Show on the live map" leaves the player to centre on.
  let wantFocus = '';
  try { wantFocus = sessionStorage.getItem('liveFocus') || ''; sessionStorage.removeItem('liveFocus'); } catch {}
  root._teardown = () => { if (timer) clearInterval(timer); window.removeEventListener('resize', fitMap); };

  // Each player is a <g> at its point, scaled to screen pixels (gm.px), so the
  // dot and the name keep one size at any zoom and on any screen. Also the
  // gm.onView hook, so zoom/pan re-plots at the new scale.
  function plotPlayers() {
    if (!gm) return;
    gm.layer.innerHTML = '';
    const s = gm.px(1);
    if (trailsOn) {
      for (const p of lastPlayers) {
        if (!p.trail || !p.trail.length) continue;
        const pts = p.trail.concat([[p.x, p.z]]).map(([x, z]) => gm.w2s(x, z));
        const d = pts.map((q, i) => (i ? 'L' : 'M') + q[0].toFixed(1) + ' ' + q[1].toFixed(1)).join(' ');
        const focus = p.key && p.key === focusKey;
        svgEl('path', { d, class: 'live-trail' + (focus ? ' focus' : '') }, gm.layer);
        for (const q of pts.slice(0, -1)) svgEl('circle', { cx: q[0], cy: q[1], r: 2.4 * s, class: 'live-trail-pt' + (focus ? ' focus' : '') }, gm.layer);
      }
    }
    for (const p of lastPlayers) {
      const [sx, sy] = gm.w2s(p.x, p.z);
      const stale = p.ageSec > 300;
      const offline = p.online === false; // last-known, not RCon-confirmed online
      const g = svgEl('g', { class: 'live-g' + (p.key && p.key === focusKey ? ' focus' : ''), transform: `translate(${sx} ${sy}) scale(${s})` }, gm.layer);
      const c = svgEl('circle', { cx: 0, cy: 0, r: 5, class: 'live-pt' + (offline ? ' offline' : (stale ? ' stale' : '')) }, g);
      svgEl('title', {}, c).textContent = p.name + ' · ' + fmtAge(p.ageSec) + (offline ? ' · ' + t('livemap.lastKnown') : '');
      svgEl('text', { x: 8, y: 4, class: 'live-label' + (stale || offline ? ' stale' : '') }, g).textContent = p.name;
      // A click (not a pan) on a player opens the profile.
      if (p.key) g.addEventListener('click', () => openPlayerProfile(p.key));
    }
  }

  // The list next to the map: who is plotted, how old the position is, and a
  // click that centres the map on them.
  function renderList() {
    listHost.innerHTML = '';
    listHost.append(h('div', { class: 'live-list-head' }, [
      h('span', { i18n: 'livemap.list.title' }), h('span', { class: 'pill-count', text: String(lastPlayers.length) }),
    ]));
    if (!lastPlayers.length) {
      listHost.append(h('p', { class: 'hint', i18n: 'livemap.list.empty' }));
      return;
    }
    const sorted = lastPlayers.slice().sort((a, b) => (b.online === true) - (a.online === true) || a.ageSec - b.ageSec);
    for (const p of sorted) {
      const offline = p.online === false;
      const row = h('div', { class: 'live-row' + (p.key && p.key === focusKey ? ' active' : '') }, [
        h('button', { type: 'button', class: 'live-row-main', title: t('livemap.list.focus'), onclick: () => {
          focusKey = p.key || '';
          if (gm) gm.focus(p.x, p.z);
          plotPlayers();
          renderList();
        } }, [
          h('span', { class: 'live-row-dot' + (offline ? ' offline' : (p.ageSec > 300 ? ' stale' : '')) }),
          h('span', { class: 'live-row-name', text: p.name }),
          // Second line: how old the position is, and whether RCon sees them online.
          h('span', { class: 'live-row-sub' }, [
            fmtAge(p.ageSec) + ' · ',
            h('span', { class: offline ? 'is-last' : 'is-online', text: offline ? t('livemap.lastKnown') : t('players.onlineBadge') }),
          ]),
        ]),
        p.key ? h('button', { type: 'button', class: 'icon-btn live-row-prof', title: t('livemap.openProfile'), 'aria-label': t('livemap.openProfile') + ' ' + p.name,
          html: svgIcon('i-players', 15), onclick: () => openPlayerProfile(p.key) }) : null,
      ]);
      listHost.append(row);
    }
  }

  const clock = () => {
    try { return new Date().toLocaleTimeString(State.lang || undefined); } catch { return new Date().toLocaleTimeString(); }
  };

  async function refresh() {
    let d;
    try { d = await api.get('/api/map/live'); }
    catch { return; } // stay quiet on background-poll failures
    if (myNav !== _navSeq) { if (timer) clearInterval(timer); return; }

    // The map (with its real-data layers) shows even while the server is
    // stopped; only the players are missing.
    if (!gm) {
      mapHost.innerHTML = '';
      gm = buildGameMap(d.map, { panLeft: true, onImageChange: () => { gm = null; refresh(); } });
      gm.onView = plotPlayers;
      mapCoordReadout(gm, readout);
      mapWrap = h('div', { class: 'map-wrap' }, [gm.svg, gm.controls]);
      mapHost.append(mapWrap);
    }
    updated.textContent = t('livemap.updated').replace('{time}', clock());
    mapWrap.classList.toggle('is-idle', !d.running);
    renderTip(d.playerListLog !== false);
    rconLink.hidden = true;
    if (!d.running) {
      lastPlayers = [];
      status.textContent = '';
      // A slim bar, not a block: the map (and its layers) stays in view.
      if (!stoppedHost.firstChild) {
        stoppedHost.append(h('div', { class: 'live-stopped' }, [
          h('span', { class: 'live-stopped-dot' }),
          h('span', { class: 'live-stopped-text' }, [
            h('b', { i18n: 'livemap.stopped.title' }), ' ',
            h('span', { class: 'hint', i18n: 'livemap.stopped.hint' }),
          ]),
          h('button', { type: 'button', class: 'primary small', i18n: 'action.start', onclick: (e) => withBusy(e.currentTarget, async () => {
            try { await api.post('/api/server/start'); toast(t('msg.starting'), 'ok'); refreshStatus(); }
            catch (err) { handleErr(err); }
          }) }),
        ]));
      }
    } else {
      stoppedHost.innerHTML = '';
      lastPlayers = d.players || [];
      if (d.rconConfigured === false) {
        // Without RCon nobody is known to be online: the points are last-known.
        status.textContent = t('livemap.noRcon');
        rconLink.hidden = false;
      } else if (d.rconError) {
        status.textContent = t('livemap.rconOffline');
      } else {
        status.textContent = t('livemap.count')
          .replace('{online}', d.onlineCount || 0)
          .replace('{shown}', lastPlayers.length);
      }
    }
    if (wantFocus && gm) {
      const p = lastPlayers.find(x => x.key === wantFocus);
      if (p) { focusKey = p.key; gm.focus(p.x, p.z); }
      else toast(t('livemap.notPlotted'), 'error');
      wantFocus = '';
    }
    if (focusKey && !lastPlayers.some(p => p.key === focusKey)) focusKey = '';
    plotPlayers();
    renderList();
    applyI18n();
    fitMap();
  }

  await refresh();
  timer = setInterval(refresh, 10000);
};

// --------------------------------------------------------- where does it spawn

Views.lootwhere = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.lootwhere', 'lootwhere.subtitle'));
  const input = h('input', { type: 'search', placeholder: t('lootwhere.placeholder'), 'aria-label': t('lootwhere.placeholder'),
    list: 'dz-loot-classes', spellcheck: 'false', autocomplete: 'off' });
  input.value = _lastLoot;
  const dl = h('datalist', { id: 'dz-loot-classes' });
  const results = h('div', { class: 'card' });
  root.append(dl, h('div', { class: 'card' }, h('div', { class: 'toolbar' }, [
    input, h('button', { class: 'primary', i18n: 'search.go', onclick: () => run() }),
  ])));
  root.append(results);
  // No auto-focus on touch screens: it popped the keyboard over the page.
  if (!(window.matchMedia && window.matchMedia('(pointer: coarse)').matches)) setTimeout(() => input.focus(), 0);

  let timer = null, seq = 0;
  input.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(run, 350); });
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter') { clearTimeout(timer); run(); } });

  // Before the first query: what the answer contains, plus examples.
  function intro() {
    results.innerHTML = '';
    results.append(h('div', { class: 'empty-state' }, [
      h('div', { class: 'es-icon', html: svgIcon('i-lootwhere', 34) }),
      h('div', { class: 'es-title', i18n: 'lootwhere.intro.title' }),
      h('div', { class: 'es-hint', i18n: 'lootwhere.intro.hint' }),
      h('div', { class: 'actions' }, [
        h('span', { class: 'hint', i18n: 'lootwhere.example' }),
        ...['AKM', 'Mag_STANAG_30Rnd', 'Canteen'].map(ex => h('button', { type: 'button', class: 'pill', text: ex,
          onclick: () => { input.value = ex; run(); } })),
      ]),
    ]));
    applyI18n();
  }

  const chips = (cls, arr) => (arr || []).map(x => h('span', { class: 'badge ' + cls, text: x }));

  // One other way the item reaches the world, as a link to where it is set.
  function refChip(r) {
    const pct = r.chance > 0 ? ' · ' + fmtPct(r.chance) : '';
    const label = tn('lootwhere.ref.' + r.kind, { parent: r.parent }) + pct;
    let go = null;
    if (r.kind === 'attachment' || r.kind === 'cargo') go = () => { _pageQuery = { route: 'attachments', q: r.parent }; navigate('attachments'); };
    else if (r.kind === 'preset') go = () => { _pageQuery = { route: 'randompresets', q: r.parent }; navigate('randompresets'); };
    else if (r.kind === 'event') go = () => navigate('events');
    return go ? h('button', { type: 'button', class: 'pill loot-ref k-' + r.kind, text: label, onclick: go })
      : h('span', { class: 'pill loot-ref', text: label });
  }

  function renderHit(it) {
    const also = it.alsoIn || [];
    const row = h('div', { class: 'loot-hit' }, [
      h('div', { class: 'loot-hit-head' }, [
        h('span', { class: 'loot-name', text: it.name }),
        h('span', { class: 'hint', text: it.file }),
        it.registered === false ? h('span', { class: 'badge warn', i18n: 'lootwhere.unregBadge' }) : null,
      ].filter(Boolean)),
      h('div', { class: 'hint', text:
        t('lootwhere.nominal') + ': ' + (it.nominal ?? '—') + ' · ' +
        t('lootwhere.min') + ': ' + (it.min ?? '—') +
        (it.lifetime != null ? ' · ' + t('lootwhere.lifetime') + ': ' + fmtAge(it.lifetime) : '') }),
      h('div', { class: 'chip-list' }, [
        it.category ? h('span', { class: 'badge info', text: typeCategoryLabel(it.category), title: it.category }) : null,
        ...chips('ok', it.usages),
        ...chips('warn', it.values),
        ...chips('mute', it.tags),
      ].filter(Boolean)),
    ]);
    // Why an entry that looks fine may still never spawn.
    if (it.registered === false) {
      row.append(h('div', { class: 'loot-warn-row' }, [
        h('p', { class: 'hint loot-warn', i18n: 'lootwhere.unregistered' }),
        h('button', { type: 'button', class: 'secondary small', i18n: 'lootwhere.toModed', onclick: () => navigate('moded') }),
      ]));
    }
    const other = also.length ? ' ' + t('lootwhere.otherPaths') : '';
    if (!it.nominal) {
      row.append(h('p', { class: 'hint loot-warn', text: t('lootwhere.nominalZero') + other }));
    } else if (!(it.usages && it.usages.length)) {
      row.append(h('p', { class: 'hint loot-warn', text: t('lootwhere.noUsage') + other }));
    }
    if (also.length) {
      row.append(h('div', { class: 'loot-also' }, [
        h('span', { class: 'hint', i18n: 'lootwhere.alsoIn' }),
        ...also.map(refChip),
        it.alsoMore ? h('span', { class: 'hint', text: tn('lootwhere.more', { n: it.alsoMore }) }) : null,
      ].filter(Boolean)));
    }
    row.append(h('div', { class: 'loot-actions' }, [
      it.editable ? h('button', { type: 'button', class: 'secondary small', i18n: 'lootwhere.editInTypes',
        onclick: () => { _typesTarget = { file: it.typesFile || '', q: it.name }; navigate('types'); } }) : null,
      h('button', { type: 'button', class: 'secondary small', i18n: 'lootwhere.allMentions',
        onclick: () => { _lastSearch = it.name; navigate('search'); } }),
    ].filter(Boolean)));
    return row;
  }

  async function run() {
    const q = input.value.trim();
    _lastLoot = q;
    const mine = ++seq;
    results.innerHTML = '';
    if (!q) { intro(); return; }
    if (q.length < 2) { results.append(h('p', { class: 'hint', i18n: 'search.hintShort' })); applyI18n(); return; }
    let d;
    try { d = await api.get('/api/loot/where?q=' + encodeURIComponent(q)); }
    catch (e) { handleErr(e); return; }
    if (mine !== seq) return;
    const hits = d.hits || [];
    if (!hits.length) {
      results.append(h('div', { class: 'empty-state' }, [
        h('div', { class: 'es-hint', i18n: 'lootwhere.none' }),
        h('div', { class: 'actions' }, [h('button', { type: 'button', class: 'secondary small', i18n: 'lootwhere.allMentions',
          onclick: () => { _lastSearch = q; navigate('search'); } })]),
      ]));
      applyI18n();
      return;
    }
    results.append(h('p', { class: 'hint',
      text: tn('search.count', { n: d.total }) + (d.truncated ? ' ' + t('search.truncated') : '') }));
    for (const it of hits) results.append(renderHit(it));
    applyI18n();
  }

  if (input.value.trim()) run(); else intro();
  // Autocomplete from the class names the server really has, as on
  // Attachments and Presets.
  try {
    const names = (await api.get('/api/spawnable/classnames')).names || [];
    if (myNav !== _navSeq) return;
    for (const n of names.slice(0, 5000)) dl.append(h('option', { value: n }));
  } catch {}
};

// ----------------------------------------------------------------- globals

function prettifyVar(name) {
  return name
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/([A-Z]+)([A-Z][a-z])/g, '$1 $2');
}

// Vanilla globals.xml (DayZ 1.29 — the same on every official map): the
// value each variable has before anyone edits it, the group it is shown in,
// and how it reads — seconds in words, metres and percent as units, 0/1
// flags as switches. Variables a mod adds land in "Other".
const GLOBALS_META = {
  TimeLogin: { g: 'players', d: '15', u: 's' },
  TimeLogout: { g: 'players', d: '15', u: 's' },
  TimeHopping: { g: 'players', d: '60', u: 's' },
  TimePenalty: { g: 'players', d: '20', u: 's' },
  LoginTimeMax: { g: 'players', u: 's' },
  LogoutTimeMax: { g: 'players', u: 's' },
  InitialSpawn: { g: 'loot', d: '100', u: '%' },
  SpawnInitial: { g: 'loot', d: '1200' },
  RestartSpawn: { g: 'loot', d: '0', flag: true },
  LootDamageMin: { g: 'loot', d: '0.0' },
  LootDamageMax: { g: 'loot', d: '0.82' },
  LootProxyPlacement: { g: 'loot', d: '1', flag: true },
  LootSpawnAvoidance: { g: 'loot', d: '100', u: 'm' },
  RespawnAttempt: { g: 'loot', d: '2' },
  RespawnLimit: { g: 'loot', d: '20' },
  RespawnTypes: { g: 'loot', d: '12' },
  FoodDecay: { g: 'loot', d: '1', flag: true },
  CleanupAvoidance: { g: 'cleanup', d: '100', u: 'm' },
  CleanupLifetimeDefault: { g: 'cleanup', d: '45', u: 's' },
  CleanupLifetimeDeadPlayer: { g: 'cleanup', d: '3600', u: 's' },
  CleanupLifetimeDeadInfected: { g: 'cleanup', d: '330', u: 's' },
  CleanupLifetimeDeadAnimal: { g: 'cleanup', d: '1200', u: 's' },
  CleanupLifetimeRuined: { g: 'cleanup', d: '330', u: 's' },
  CleanupLifetimeLimit: { g: 'cleanup', d: '50' },
  AnimalMaxCount: { g: 'world', d: '200' },
  ZombieMaxCount: { g: 'world', d: '1000' },
  ZoneSpawnDist: { g: 'world', d: '300', u: 'm' },
  WorldWetTempUpdate: { g: 'world', d: '1', flag: true },
  IdleModeCountdown: { g: 'world', d: '60', u: 's' },
  IdleModeStartup: { g: 'world', d: '1', flag: true },
  FlagRefreshFrequency: { g: 'bases', d: '432000', u: 's' },
  FlagRefreshMaxDuration: { g: 'bases', d: '3456000', u: 's' },
};
const GLOBALS_GROUPS = [
  ['world', 'i-players'], ['loot', 'i-economy'], ['cleanup', 'i-refresh'],
  ['players', 'i-clock'], ['bases', 'i-shield'], ['other', 'i-settings'],
];

Views.globals = async (root) => {
  const myNav = _navSeq;
  const running = !!State.serverStatus.running;
  root.append(pageHeader('nav.globals', 'globals.subtitle', [revealBtn({ kind: 'missionfile', path: 'db/globals.xml' })]));
  if (running) root.append(runningBanner());
  let d;
  try { d = await api.get('/api/globals'); }
  catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;

  if (!d.exists) {
    root.append(h('div', { class: 'card' }, h('div', { class: 'empty-state' },
      h('div', { class: 'es-hint', i18n: 'globals.missing' }))));
    return;
  }
  const vars = d.vars || [];
  const same = (a, b) => {
    const x = parseFloat(a), y = parseFloat(b);
    return (!Number.isNaN(x) && !Number.isNaN(y)) ? x === y : String(a).trim() === String(b).trim();
  };
  const label = (name) => State.i18n['glv.' + name] ? t('glv.' + name) : prettifyVar(name);
  const helpText = (name) => (State.help && State.help['gl.' + name]) || '';

  // Every variable as a row: label, help, the tag, the value, and a chip that
  // says whether it is the game's default.
  const rows = []; // { name, orig, read(), set(v), el, meta }
  function row(v) {
    const meta = GLOBALS_META[v.name] || { g: 'other' };
    let control, read, set;
    if (meta.flag) {
      const cb = h('input', { type: 'checkbox', class: 'switch', disabled: running, 'aria-label': label(v.name) });
      cb.checked = String(v.value).trim() === '1';
      control = cb;
      read = () => (cb.checked ? '1' : '0');
      set = (x) => { cb.checked = String(x).trim() === '1'; };
    } else {
      // type=text, not number: a number input re-formats "0.0" to "0", and
      // the untouched value would look changed and be rewritten on save.
      const inp = h('input', { type: 'text', class: 'gl-input', value: v.value, disabled: running,
        inputmode: v.type === '1' ? 'decimal' : 'numeric', spellcheck: 'false', 'aria-label': label(v.name) });
      const unit = h('span', { class: 'gl-unit' });
      const paintUnit = () => {
        unit.textContent = meta.u === 's' ? fmtLifetime(inp.value) : meta.u === 'm' ? t('unit.m') : meta.u === '%' ? '%' : '';
      };
      inp.addEventListener('input', paintUnit);
      paintUnit();
      control = h('div', { class: 'gl-value' }, [inp, unit]);
      read = () => inp.value.trim();
      set = (x) => { inp.value = x; paintUnit(); };
    }
    const chip = h('span', { class: 'gp2-chip' });
    const resetBtn = h('button', { type: 'button', class: 'gp2-reset', text: '↺', hidden: true,
      title: meta.d != null ? t('globals.resetTo').replace('{v}', meta.d) : '',
      'aria-label': t('gameplay.field.reset'),
      onclick: (e) => { e.preventDefault(); set(meta.d); paint(); changed(); } });
    const paint = () => {
      const cur = read();
      const isDefault = meta.d != null && same(cur, meta.d);
      chip.className = 'gp2-chip' + (meta.d == null ? '' : isDefault ? ' def' : ' mod');
      chip.textContent = meta.d == null ? '' : isDefault ? t('gameplay.chip.default') : t('gameplay.chip.changed');
      chip.title = meta.d == null || isDefault ? '' : t('globals.defaultIs').replace('{v}', meta.d);
      resetBtn.hidden = meta.d == null || isDefault || running;
    };
    const hm = helpText(v.name) ? help('gl.' + v.name) : null;
    if (hm) hm.addEventListener('click', (e) => e.preventDefault());
    const el = h('div', { class: 'gl-row' + (meta.flag ? ' gl-flag' : '') }, [
      h('div', { class: 'gl-label' }, [
        h('span', { class: 'gl-name' }, [h('span', { text: label(v.name) }), hm].filter(Boolean)),
        h('span', { class: 'gp2-keyline' }, [h('code', { class: 'cfg-key', text: v.name }), chip, resetBtn]),
      ]),
      control,
    ]);
    el.dataset.search = (label(v.name) + ' ' + v.name + ' ' + helpText(v.name)).toLowerCase();
    el.addEventListener('input', () => { paint(); changed(); });
    el.addEventListener('change', () => { paint(); changed(); });
    paint();
    const r = { name: v.name, orig: String(v.value).trim(), read, set, el, paint, meta };
    rows.push(r);
    return el;
  }

  const search = h('input', { type: 'search', placeholder: t('globals.search'), 'aria-label': t('globals.search') });
  const noMatch = h('p', { class: 'hint', i18n: 'gameplay.nothing', hidden: true });
  const groupsHost = h('div', { class: 'gl-groups' });
  let vis = null;
  for (const [g, icon] of GLOBALS_GROUPS) {
    const mine = vars.filter(v => ((GLOBALS_META[v.name] || {}).g || 'other') === g);
    if (!mine.length) continue;
    const count = h('span', { class: 'gp2-count' });
    const sec = h('details', { class: 'gp2-group gl-group', open: true }, [
      h('summary', {}, [
        h('span', { class: 'card-title-icon', html: svgIcon(icon, 16) }),
        h('span', { class: 'gp2-title', i18n: 'globals.group.' + g }),
        h('span', { class: 'grow' }),
        count,
      ]),
      h('div', { class: 'gl-grid' }, mine.map(row)),
    ]);
    sec._count = count;
    groupsHost.append(sec);
  }

  // The main numbers as pictures with sliders, tied to the rows above.
  vis = glVisuals(rows, () => changed(), running, label);

  function recount() {
    groupsHost.querySelectorAll('details.gl-group').forEach(sec => {
      const n = sec.querySelectorAll('.gp2-chip.mod').length;
      sec._count.textContent = n ? t('gameplay.changedCount').replace('{n}', n) : '';
      sec._count.hidden = !n;
    });
  }
  function filter() {
    const q = search.value.trim().toLowerCase();
    let any = false;
    for (const r of rows) {
      const show = !q || r.el.dataset.search.includes(q);
      r.el.hidden = !show;
      any = any || show;
    }
    groupsHost.querySelectorAll('details.gl-group').forEach(sec => {
      const visible = [...sec.querySelectorAll('.gl-row')].some(el => !el.hidden);
      sec.hidden = !visible;
      if (q && visible) sec.open = true;
    });
    noMatch.hidden = any || !q;
  }
  search.addEventListener('input', filter);

  // Unsaved changes: a sticky bar, and the page asks before leaving.
  const edits = () => rows.filter(r => r.read() !== r.orig);
  const barText = h('span');
  const saveBtn = h('button', { type: 'button', class: 'primary small', i18n: 'action.save', title: 'Ctrl+S', disabled: running,
    onclick: (e) => withBusy(e.currentTarget, save) });
  const bar = h('div', { class: 'sticky-save', hidden: true }, [
    barText, h('span', { class: 'grow' }),
    h('button', { type: 'button', class: 'secondary small', i18n: 'action.discard', onclick: () => {
      for (const r of rows) { r.set(r.orig); r.paint(); }
      changed();
    } }),
    saveBtn,
  ]);
  function changed() {
    const n = edits().length;
    bar.hidden = n === 0;
    barText.textContent = tn('server.cfg.unsaved', { n });
    recount();
    if (vis) vis.paint();
  }
  async function save() {
    const list = edits().map(r => ({ name: r.name, value: r.read() }));
    if (!list.length) { toast(t('globals.noChanges'), 'warn'); return; }
    try {
      const res = await api.post('/api/globals/save', { vars: list });
      for (const r of rows) r.orig = r.read();
      changed();
      toast(t('globals.saved').replace('{n}', res.applied || 0), 'ok');
    } catch (e) { handleErr(e); }
  }
  root._save = () => save();
  root._isDirty = () => edits().length > 0;

  const changedTotal = rows.filter(r => r.meta.d != null && !same(r.orig, r.meta.d)).length;
  if (vis.count) {
    root.append(h('div', { class: 'card gl-vis-card' }, [
      cardTitle('i-globals', 'gl.vis.title'),
      h('p', { class: 'hint', i18n: 'gl.vis.hint' }),
      vis.el,
    ]));
  }
  root.append(h('div', { class: 'card gl-card' }, [
    h('div', { class: 'players-toolbar gp2-toolbar' }, [
      search,
      h('span', { class: 'grow' }),
      h('span', { class: 'badge mute', text: tn('globals.summary', { n: vars.length, changed: changedTotal }) }),
      h('button', { type: 'button', class: 'secondary', i18n: 'globals.resetAll', disabled: running || !changedTotal, onclick: async () => {
        if (!(await confirmModal(t('globals.resetAll.confirm'), { danger: true, okText: t('globals.resetAll') }))) return;
        for (const r of rows) if (r.meta.d != null) { r.set(r.meta.d); r.paint(); }
        changed();
      } }),
    ]),
    groupsHost,
    noMatch,
    h('p', { class: 'hint mono gp2-path', text: d.path }),
  ]), bar);
  recount();
  applyI18n();
};

// -------------------------------------------------------------- mod drift

Views.moddrift = async (root) => {
  root.append(pageHeader('nav.moddrift', 'moddrift.subtitle'));
  const card = h('div', { class: 'card' });
  root.append(card);
  card.append(h('p', { class: 'hint', i18n: 'moddrift.scanning' }));
  applyI18n();

  let d;
  try { d = await api.get('/api/mods/drift'); }
  catch (e) { handleErr(e); return; }
  card.innerHTML = '';

  const list = d.mods || [];
  if (!list.length) {
    card.append(h('div', { class: 'empty-state' }, h('div', { class: 'es-hint', i18n: 'moddrift.none' })));
    return;
  }
  const issues = list.filter(m => m.status !== 'ok').length;
  card.append(h('p', { class: 'hint',
    text: t('moddrift.summary').replace('{checked}', d.checked || 0).replace('{issues}', issues) }));

  const statusBadge = (s) => {
    if (s === 'notmerged') return h('span', { class: 'badge err', i18n: 'moddrift.st.notmerged' });
    if (s === 'partial') return h('span', { class: 'badge warn', i18n: 'moddrift.st.partial' });
    return h('span', { class: 'badge ok', i18n: 'moddrift.st.ok' });
  };

  const tbl = h('table');
  tbl.append(h('thead', {}, h('tr', {}, [
    h('th', { text: '' }),
    h('th', { i18n: 'moddrift.mod' }),
    hlp(h('th', { class: 'num', i18n: 'moddrift.ships' }), 'moddrift.help.ships'),
    hlp(h('th', { class: 'num', i18n: 'moddrift.active' }), 'moddrift.help.active'),
    hlp(h('th', { class: 'num', i18n: 'moddrift.missing' }), 'moddrift.help.missing'),
  ])));
  const tb = h('tbody');
  for (const m of list) {
    tb.append(h('tr', { class: m.status !== 'ok' ? 'row-watch' : '' }, [
      h('td', {}, statusBadge(m.status)),
      h('td', { class: 'mono', text: m.mod }),
      h('td', { class: 'num', text: String(m.ships) }),
      h('td', { class: 'num', text: String(m.active) }),
      h('td', { class: 'num', text: String(m.missing) }),
    ]));
    if (m.missing > 0 && m.sample && m.sample.length) {
      const chips = m.sample.map(n => h('span', { class: 'badge mute', text: n }));
      if (m.missing > m.sample.length) chips.push(h('span', { class: 'hint', text: '+' + (m.missing - m.sample.length) }));
      tb.append(h('tr', {}, h('td', { colspan: '5' }, [
        h('span', { class: 'hint', i18n: 'moddrift.missingList' }),
        h('div', { class: 'chip-list', style: { marginTop: '4px' } }, chips),
      ])));
    }
  }
  tbl.append(tb);
  card.append(h('div', { class: 'table-scroll' }, tbl));
  applyI18n();
};

// ----------------------------------------------------------- random presets

Views.randompresets = async (root) => {
  const myNav = _navSeq;
  const preQuery = takePageQuery('randompresets'); // a link from Where to find
  root.append(pageHeader('nav.randompresets', 'randompresets.subtitle', [revealBtn({ kind: 'missionfile', path: 'cfgrandompresets.xml' })]));
  const running = !!State.serverStatus.running;
  let d;
  try { d = await api.get('/api/randompresets'); }
  catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;

  // An unreadable file is not "no presets": adding one and saving would have
  // replaced every preset the file really holds. Show the parse error instead
  // of an editor.
  if (d.error) {
    const base = String((State.info && State.info.serverDir) || '').replace(/\\/g, '/').replace(/\/+$/, '');
    const abs = String(d.path || '').replace(/\\/g, '/');
    const rel = base && abs.toLowerCase().startsWith(base.toLowerCase() + '/') ? abs.slice(base.length + 1) : '';
    root.append(h('div', { class: 'card' }, h('div', { class: 'empty-state' }, [
      h('div', { class: 'es-title', i18n: 'randompresets.unreadable' }),
      h('div', { class: 'es-hint mono', text: d.error }),
      rel ? h('div', { class: 'actions' }, [h('button', { class: 'secondary', i18n: 'search.open',
        onclick: () => { _filesTarget = { path: rel }; navigate('files'); } })]) : null,
    ].filter(Boolean))));
    applyI18n();
    return;
  }

  // Editable working copy. Chances are kept as strings while editing so a
  // locale keyboard's comma never corrupts the value; parseNum reads them.
  let presets = (d.presets || []).map(p => ({
    kind: p.kind, name: p.name, chance: String(p.chance),
    items: (p.items || []).map(it => ({ name: it.name, chance: String(it.chance) })),
  }));
  const parseNum = (v) => { const n = parseFloat(String(v).replace(',', '.')); return isFinite(n) ? n : 0; };

  // Class-name autocomplete + unknown-class flagging, exactly like Attachments.
  // Compared case-insensitively: vanilla's own files write AmmoBox_9x19_25Rnd
  // where types.xml has AmmoBox_9x19_25rnd, and those spawn fine.
  let classNames = [];
  try { classNames = (await api.get('/api/spawnable/classnames')).names || []; } catch {}
  // How many Attachments entries reference each preset (cargo and attachments
  // presets are separate namespaces in the game).
  const uses = {};
  try {
    const sp = await api.get('/api/spawnable');
    for (const st of (sp.types || [])) {
      const seen = new Set();
      for (const [side, groups] of [['cargo', st.cargo], ['attachments', st.attachments]]) {
        for (const g of (groups || [])) {
          if (!g.preset) continue;
          const k = side + '|' + g.preset.toLowerCase();
          if (!seen.has(k)) { seen.add(k); uses[k] = (uses[k] || 0) + 1; }
        }
      }
    }
  } catch {}
  if (myNav !== _navSeq) return;
  const classLower = new Set(classNames.map(n => n.toLowerCase()));
  const isUnknown = (name) => { const n = String(name || '').trim(); return !!n && classLower.size > 0 && !classLower.has(n.toLowerCase()); };
  const unknownCount = (p) => p.items.filter(it => isUnknown(it.name)).length;
  const usesOf = (p) => uses[p.kind + '|' + String(p.name || '').trim().toLowerCase()] || 0;
  const classDL = h('datalist', { id: 'dz-preset-classes' });
  for (const n of classNames.slice(0, 5000)) classDL.append(h('option', { value: n }));
  root.append(classDL);

  const search = h('input', { type: 'search', placeholder: t('randompresets.search'), 'aria-label': t('randompresets.search') });
  search.value = preQuery;
  const countHint = h('span', { class: 'hint rp-count' });
  // Which presets to list: all, one kind, or the ones nothing uses.
  let kindFilter = 'all';
  try { kindFilter = localStorage.getItem('presetsKind') || 'all'; } catch {}
  const kindChips = h('div', { class: 'kind-chips rp-kinds' });
  const KIND_FILTERS = [
    ['all', 'events.cat.all', () => true],
    ['cargo', 'randompresets.kind.cargo', p => p.kind === 'cargo'],
    ['attachments', 'randompresets.kind.attachments', p => p.kind === 'attachments'],
    ['unused', 'randompresets.filter.unused', p => usesOf(p) === 0],
  ];
  function renderKinds() {
    kindChips.innerHTML = '';
    for (const [k, label, fn] of KIND_FILTERS) {
      const n = presets.filter(fn).length;
      if (!n && k !== 'all' && kindFilter !== k) continue;
      kindChips.append(h('button', { type: 'button', class: 'kind-chip' + (kindFilter === k ? ' active' : ''),
        onclick: () => { kindFilter = k; try { localStorage.setItem('presetsKind', k); } catch {} render(); } },
      [h('span', { i18n: label }), h('span', { class: 'kind-count', text: String(n) })]));
    }
  }
  let onlyUnknown = false;
  const unknownBtn = h('button', { type: 'button', class: 'secondary small rp-unknown-btn', 'aria-pressed': 'false',
    onclick: () => { onlyUnknown = !onlyUnknown; render(); } });
  const addCargo = h('button', { class: 'secondary', i18n: 'randompresets.addCargo', disabled: running,
    onclick: () => openPresetEditor({ kind: 'cargo', name: '', chance: '0.3', items: [] }, true) });
  const addAttach = h('button', { class: 'primary', i18n: 'randompresets.addAttach', disabled: running,
    onclick: () => openPresetEditor({ kind: 'attachments', name: '', chance: '1', items: [] }, true) });

  root.append(h('div', { class: 'card' }, [
    h('p', { class: 'hint mono', text: d.path || '' }),
    h('p', { class: 'hint', i18n: 'randompresets.editHint' }),
    running ? runningBanner() : null,
    // A missing file is fine — saving creates it — but say so.
    d.exists ? null : h('div', { class: 'warning-bar', i18n: 'randompresets.missingCreate' }),
    h('div', { class: 'toolbar' }, [search, countHint, unknownBtn, h('span', { class: 'grow' }), addCargo, addAttach]),
    kindChips,
  ].filter(Boolean)));
  const list = h('div');
  root.lastChild.append(list);

  function matches(p, q) {
    if (!q) return true;
    if ((p.name || '').toLowerCase().includes(q)) return true;
    return p.items.some(it => (it.name || '').toLowerCase().includes(q));
  }

  const kindLabel = (k) => t(k === 'cargo' ? 'randompresets.kind.cargo' : 'randompresets.kind.attachments');
  const pct = (v) => Math.round(Math.max(0, Math.min(1, parseNum(v))) * 1000) / 10;

  // The list: one row per preset, like the Attachments table; a row opens
  // the preset's editor.
  function render() {
    const q = search.value.trim().toLowerCase();
    list.innerHTML = '';
    const totalUnknown = presets.reduce((a, p) => a + unknownCount(p), 0);
    if (!totalUnknown) onlyUnknown = false;
    unknownBtn.hidden = !totalUnknown;
    unknownBtn.textContent = tn('randompresets.unknownTotal', { n: totalUnknown });
    unknownBtn.classList.toggle('active', onlyUnknown);
    unknownBtn.setAttribute('aria-pressed', onlyUnknown ? 'true' : 'false');
    unknownBtn.title = t('randompresets.unknownFilter');
    renderKinds();
    const kindFn = (KIND_FILTERS.find(k => k[0] === kindFilter) || KIND_FILTERS[0])[2];
    const shown = presets.filter(p => kindFn(p) && matches(p, q) && (!onlyUnknown || unknownCount(p) > 0));
    countHint.textContent = tn('randompresets.count', { shown: shown.length, total: presets.length });
    if (!presets.length) {
      list.append(h('div', { class: 'empty-state' }, h('div', { class: 'es-hint', i18n: 'randompresets.empty' })));
      applyI18n();
      return;
    }
    if (!shown.length) {
      list.append(h('div', { class: 'empty-state' }, [
        h('div', { class: 'es-title', i18n: 'randompresets.none' }),
        h('div', { class: 'actions' }, [h('button', { class: 'secondary', i18n: 'filter.reset',
          onclick: () => { search.value = ''; onlyUnknown = false; kindFilter = 'all'; render(); } })]),
      ]));
      applyI18n();
      return;
    }
    const tbl = h('table', { class: 'attach-table rp-table' });
    tbl.append(h('thead', {}, h('tr', {}, [
      h('th', { i18n: 'col.name' }),
      h('th', { i18n: 'attach.col.kind' }),
      hlp(h('th', { i18n: 'randompresets.groupChance' }), 'attach.slotChance'),
      h('th', { class: 'num', i18n: 'randompresets.col.items' }),
      h('th', { i18n: 'attach.summary' }),
      h('th', { i18n: 'randompresets.col.used' }),
      h('th', { text: '' }),
    ])));
    const tb = h('tbody');
    for (const p of shown) {
      const names = p.items.map(it => (it.name || '').trim()).filter(Boolean);
      const unk = unknownCount(p);
      const u = usesOf(p);
      const c = pct(p.chance);
      const tr = h('tr', { class: 'attach-row', title: t('action.edit') }, [
        h('td', {}, [
          h('span', { class: 'attach-name-cell', text: (p.name || '').trim() || t('randompresets.unnamed') }),
          unk ? h('span', { class: 'attach-cat warn-text', text: tn('randompresets.unknownN', { n: unk }) }) : null,
        ]),
        h('td', {}, h('span', { class: 'kind-badge ' + (p.kind === 'cargo' ? 'k-container' : 'k-weapon'), text: kindLabel(p.kind) })),
        h('td', {}, h('span', { class: 'rp-chance' }, [
          h('span', { class: 'rp-bar' }, h('span', { style: { width: c + '%' } })),
          h('span', { class: 'rp-pct', text: c + '%' }),
        ])),
        h('td', { class: 'num' + (p.items.length ? '' : ' zero'), text: p.items.length ? String(p.items.length) : '—' }),
        h('td', { class: 'attach-sum' }, [h('span', { class: 'attach-sum-in hint', title: names.join(', '),
          text: names.slice(0, 4).join('  •  ') + (names.length > 4 ? '  … +' + (names.length - 4) : '') })]),
        h('td', {}, u ? h('span', { class: 'badge ok', text: tn('randompresets.usedIn', { n: u }) })
          : h('span', { class: 'badge mute', text: t('randompresets.unused'), title: t('randompresets.unused.hint') })),
        h('td', {}, h('button', { class: 'small', i18n: 'action.edit', onclick: () => openPresetEditor(p, false) })),
      ]);
      tr.addEventListener('click', (e) => { if (!e.target.closest('button')) openPresetEditor(p, false); });
      tb.append(tr);
    }
    tbl.append(tb);
    list.append(h('div', { class: 'table-scroll-narrow' }, tbl));
    applyI18n();
  }

  // The editor: a preset is one group — its chance and the items it picks
  // from by weight — drawn exactly like a slot in the Attachments editor.
  // Save writes cfgrandompresets.xml straight away; nothing is left unsaved.
  function openPresetEditor(orig, isNew) {
    const draft = { kind: orig.kind, name: orig.name || '', chance: String(orig.chance ?? '1'),
      items: (orig.items || []).map(it => ({ name: it.name || '', chance: String(it.chance ?? '1') })) };
    let edited = isNew;
    const m = openModal({ title: isNew ? t(draft.kind === 'cargo' ? 'randompresets.addCargo' : 'randompresets.addAttach') : (draft.name || t('randompresets.unnamed')),
      wide: true, isDirty: () => edited });
    m.body.addEventListener('input', () => { edited = true; });
    const nameInp = h('input', { type: 'text', class: 'preset-name-inp', value: draft.name, placeholder: t('randompresets.namePh'),
      disabled: running, 'aria-label': t('randompresets.namePh') });
    nameInp.oninput = () => { draft.name = nameInp.value; };
    // The preset's chance and items, laid out like a slot of the Attachments
    // editor and the loadout's rows: items come from the catalog.
    const cardHost = h('div');
    let rpCat = null;
    const drawCard = () => {
      cardHost.replaceChildren(seGroupCard(draft, { kind: draft.kind, cat: rpCat, classLower, removable: false,
        chanceKey: 'randompresets.groupChance', disabled: running,
        edited: () => { edited = true; }, changed: () => { edited = true; drawCard(); } }));
      applyI18n();
    };
    drawCard();
    loCatalog().then((c) => { rpCat = c; if (cardHost.isConnected) drawCard(); }).catch(() => {});
    const u = isNew ? 0 : usesOf(orig);
    const err = h('p', { class: 'hint err-text', hidden: true });
    async function save(btn) {
      const name = draft.name.trim();
      err.hidden = true;
      if (!name) { err.textContent = t('randompresets.needName'); err.hidden = false; nameInp.focus(); return; }
      if (presets.some(x => x !== orig && x.kind === draft.kind && (x.name || '').trim().toLowerCase() === name.toLowerCase())) {
        err.textContent = t('randompresets.dupName').replace('{name}', name); err.hidden = false; nameInp.focus(); return;
      }
      const clean = { kind: draft.kind, name, chance: draft.chance, items: draft.items.filter(it => (it.name || '').trim()) };
      const next = isNew ? [clean, ...presets] : presets.map(x => x === orig ? clean : x);
      if (await saveAll(next, btn)) { edited = false; m.close(); }
    }
    async function del(btn) {
      const msg = tn('randompresets.deleteConfirm', { name: (orig.name || '').trim() || t('randompresets.unnamed'), n: orig.items.length }) +
        (u ? ' ' + tn('randompresets.deleteUsed', { n: u }) : '');
      if (!(await confirmModal(msg, { danger: true, okText: t('action.delete') }))) return;
      if (await saveAll(presets.filter(x => x !== orig), btn)) { edited = false; m.close(); }
    }
    m.body.append(
      h('div', { class: 'rp-edit-head' }, [
        h('label', { class: 'rp-edit-name' }, [h('span', { class: 'attach-head-label', i18n: 'col.name' }), nameInp]),
        h('span', { class: 'kind-badge ' + (draft.kind === 'cargo' ? 'k-container' : 'k-weapon'), text: kindLabel(draft.kind) }),
        isNew ? null : (u ? h('span', { class: 'badge ok', text: tn('randompresets.usedIn', { n: u }) })
          : h('span', { class: 'badge mute', text: t('randompresets.unused'), title: t('randompresets.unused.hint') })),
      ].filter(Boolean)),
      cardHost,
      h('p', { class: 'hint', i18n: draft.kind === 'cargo' ? 'randompresets.cargoHint' : 'randompresets.attachHint' }),
      err,
      h('div', { class: 'actions' }, [
        isNew ? null : h('button', { class: 'danger secondary', i18n: 'randompresets.delete', disabled: running, onclick: (e) => del(e.currentTarget) }),
        h('span', { class: 'grow' }),
        h('button', { class: 'secondary', i18n: 'action.cancel', onclick: () => m.close() }),
        h('button', { class: 'primary', i18n: 'action.save', disabled: running, onclick: (e) => save(e.currentTarget) }),
      ].filter(Boolean)),
    );
    applyI18n();
    setTimeout(() => { if (isNew) nameInp.focus(); }, 30);
  }

  // saveAll writes the whole file with next as its presets; true on success.
  async function saveAll(next, btn) {
    if (running) return false;
    const payload = next.map(p => ({
      kind: p.kind, name: (p.name || '').trim(), chance: parseNum(p.chance),
      items: p.items.map(it => ({ name: (it.name || '').trim(), chance: parseNum(it.chance) })).filter(it => it.name),
    }));
    let ok = false;
    await withBusy(btn, async () => {
      try {
        await api.post('/api/randompresets/save', { presets: payload });
        presets = next.map(p => ({ kind: p.kind, name: (p.name || '').trim(), chance: String(parseNum(p.chance)),
          items: p.items.filter(it => (it.name || '').trim()).map(it => ({ name: it.name.trim(), chance: String(parseNum(it.chance)) })) }));
        toast(t('randompresets.saved'), 'ok');
        ok = true;
        render();
      } catch (e) { handleErr(e); }
    });
    return ok;
  }

  let _st = 0;
  search.addEventListener('input', () => { clearTimeout(_st); _st = setTimeout(render, 150); });
  render();
};

// ------------------------------------------------------------------- live chat

Views.chat = async (root) => {
  const myNav = _navSeq;
  root.append(pageHeader('nav.chat', 'chat.subtitle'));
  let timer = null;
  let lastSig = null; // avoid rebuilding the feed (and flickering) when nothing changed
  root._teardown = () => { if (timer) clearInterval(timer); };
  // Names in the feed open the player's profile (kick / message / ban there).
  const playersP = api.get('/api/players').catch(() => null);
  let keyOf = () => '';

  // Connection status badge — chat is captured live from the BattlEye RCon
  // stream, so the feed only fills while the server is running with RCon on.
  const dot = h('span', { class: 'chat-dot' });
  const statusTxt = h('span', {});
  const status = h('div', { class: 'chat-status' }, [dot, statusTxt]);

  const feed = h('div', { class: 'chat-feed' });
  const input = h('input', { type: 'text', placeholder: t('chat.placeholder'), 'aria-label': t('chat.placeholder'), maxlength: '400' });
  const sendBtn = h('button', { class: 'primary', i18n: 'chat.send', onclick: send });
  // Quick messages — saved broadcasts in the manager config. Chat is the one
  // place to write to players: a click sends one, ☆ keeps what is typed.
  const presets = Array.isArray(State.config && State.config.broadcastPresets) ? State.config.broadcastPresets.slice() : [];
  const presetsHost = h('div', { class: 'chat-presets' });
  const liveCard = h('div', { class: 'card' }, [
    status,
    feed,
    h('div', { class: 'row', style: { marginTop: '10px' } }, [input, sendBtn]),
    presetsHost,
  ]);

  // Live / Archive tabs. The archive keeps every line (incl. what the panel
  // sent) on disk, one file per day.
  const archiveCard = h('div', { class: 'card hidden' });
  const tabs = h('div', { class: 'section-tabs btn-tabs', role: 'tablist' });
  const mkTab = (key, show) => h('button', { type: 'button', role: 'tab', class: 'section-tab', i18n: key, onclick: (e) => {
    const me = e.currentTarget;
    tabs.querySelectorAll('.section-tab').forEach(b => { b.classList.toggle('active', b === me); b.setAttribute('aria-selected', String(b === me)); });
    show();
  } });
  const liveTab = mkTab('chat.tab.live', () => { liveCard.classList.remove('hidden'); archiveCard.classList.add('hidden'); });
  const archTab = mkTab('chat.tab.archive', () => { liveCard.classList.add('hidden'); archiveCard.classList.remove('hidden'); loadArchive(); });
  liveTab.classList.add('active');
  liveTab.setAttribute('aria-selected', 'true');
  archTab.setAttribute('aria-selected', 'false');
  tabs.append(liveTab, archTab);
  root.append(tabs, liveCard, archiveCard);

  // canSend: a message can get through — a password is set, BattlEye took it,
  // and the server runs (the panel connects on demand while it does).
  let canSend = true;
  function setSendable(configured, connected, errKey) {
    canSend = configured && errKey !== 'rcon.err.auth' && (connected || !!State.serverStatus.running);
    input.disabled = sendBtn.disabled = !canSend;
    input.placeholder = canSend ? t('chat.placeholder')
      : !configured ? t('chat.ph.needRcon') : errKey === 'rcon.err.auth' ? t('rcon.err.auth.short') : t('chat.ph.offline');
    presetsHost.querySelectorAll('.chat-preset-send').forEach(b => { b.disabled = !canSend; });
  }

  async function say(text) {
    await api.post('/api/rcon/say', { message: text });
    lastSig = null;
    await refresh(); // show the sent line right away
  }
  let sending = false;
  async function send() {
    const v = input.value.trim();
    if (!v || sending || !canSend) return;
    sending = true;
    sendBtn.disabled = true;
    try { await say(v); input.value = ''; }
    catch (e) { handleErr(e); }
    finally { sending = false; sendBtn.disabled = !canSend; input.focus(); }
  }
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); send(); } });

  function renderPresets() {
    presetsHost.innerHTML = '';
    presetsHost.append(h('span', { class: 'chat-presets-label', i18n: 'rcon.presets.title' }));
    for (const [i, msg] of presets.entries()) {
      presetsHost.append(h('span', { class: 'chat-preset' }, [
        h('button', { type: 'button', class: 'chat-preset-send', text: msg, title: t('rcon.presets.sendHint'), disabled: !canSend,
          onclick: (e) => withBusy(e.currentTarget, async () => { try { await say(msg); toast(t('msg.sent'), 'ok'); } catch (err) { handleErr(err); } }) }),
        h('button', { type: 'button', class: 'chat-preset-x', text: '×', title: t('action.delete'), 'aria-label': t('action.delete') + ': ' + msg,
          onclick: () => { presets.splice(i, 1); savePresets(); } }),
      ]));
    }
    if (!presets.length) presetsHost.append(h('span', { class: 'hint', i18n: 'chat.presets.none' }));
    presetsHost.append(h('button', { type: 'button', class: 'small secondary', text: '☆ ' + t('chat.presets.save'), title: t('rcon.presets.saveHint'),
      onclick: () => {
        const v = input.value.trim();
        if (!v) { toast(t('rcon.presets.needText'), 'warn'); input.focus(); return; }
        if (!presets.includes(v)) { presets.push(v); savePresets(); }
      } }));
  }
  async function savePresets() {
    renderPresets();
    try { State.config = await api.post('/api/config', { broadcastPresets: presets }); toast(t('msg.saved'), 'ok'); }
    catch (e) { handleErr(e); }
  }

  const fmtTime = (ms) => {
    if (!ms) return '';
    try { return new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }); }
    catch { return ''; }
  };

  // chatLine renders one message; lines sent from the panel (and scheduled
  // announcements) are marked as the manager's, other RCon admins as admin.
  // A player the database knows by that name links to the profile.
  function chatLine(m) {
    const mine = m.source === 'manager', admin = m.source === 'admin';
    const who = mine ? t('chat.fromManager') + (m.to ? ' → ' + m.to : '') : (m.name || '');
    const key = !mine && !admin && m.name ? keyOf(m.name) : '';
    return h('div', { class: 'chat-line' + (mine ? ' mine' : '') + (admin ? ' admin' : '') }, [
      h('span', { class: 'chat-time', text: fmtTime(m.at) }),
      m.channel ? h('span', { class: 'chat-ch', text: m.channel }) : null,
      key ? h('span', { class: 'chat-player player-link', text: who, title: t('chat.openProfile'), onclick: () => openPlayerProfile(key) })
        : h('span', { class: 'chat-player', text: who }),
      h('span', { class: 'chat-text', text: m.text || '' }),
    ]);
  }

  // ---- archive
  const daySel = h('select', { 'aria-label': t('chat.archive.day') });
  const search = h('input', { type: 'search', placeholder: t('chat.archive.search'), 'aria-label': t('chat.archive.search') });
  const exportLink = h('a', { class: 'btn-link', i18n: 'chat.archive.export', download: '' });
  const keepInp = h('input', { type: 'number', min: '0', step: '1', style: { width: '90px' }, value: (State.config && State.config.chatKeepDays) ?? 90 });
  const archInfo = h('div', { class: 'hint' });
  const archFeed = h('div', { class: 'chat-feed archive' });
  keepInp.addEventListener('change', async () => {
    const n = parseInt(keepInp.value, 10);
    if (!(n >= 0)) return;
    try { State.config = await api.post('/api/config', { chatKeepDays: n }); toast(t('msg.saved'), 'ok'); }
    catch (e) { handleErr(e); }
  });
  const archBar = h('div', { class: 'row chat-archive-bar' }, [daySel, search, exportLink]);
  archiveCard.append(
    archBar,
    archInfo,
    archFeed,
    h('div', { class: 'row', style: { marginTop: '10px', gap: '8px', alignItems: 'center' } }, [
      h('label', { i18n: 'chat.archive.keep', for: assocId(keepInp) }), keepInp,
    ]),
  );
  let daysLoaded = false;
  async function loadArchive() {
    if (!daysLoaded) {
      let d;
      try { d = await api.get('/api/chat/days'); } catch (e) { handleErr(e); return; }
      daySel.innerHTML = '';
      const days = d.days || [];
      archBar.classList.toggle('hidden', !days.length);
      if (!days.length) {
        archFeed.innerHTML = '';
        archFeed.append(h('p', { class: 'hint', i18n: 'chat.archive.empty' }));
        archInfo.textContent = '';
        applyI18n();
        return;
      }
      for (const day of days) {
        daySel.append(h('option', { value: day.date, text: `${day.date} · ${t('chat.archive.count').replace('{n}', day.count)}` }));
      }
      daysLoaded = true;
    }
    await showDay();
  }
  let searchT = 0;
  async function showDay() {
    const day = daySel.value;
    if (!day) return;
    exportLink.classList.remove('hidden');
    exportLink.href = srvUrl('/api/chat/export?date=' + encodeURIComponent(day));
    let d;
    try { d = await api.get('/api/chat/archive?date=' + encodeURIComponent(day) + '&q=' + encodeURIComponent(search.value.trim())); }
    catch (e) { handleErr(e); return; }
    if (myNav !== _navSeq) return;
    const msgs = d.messages || [];
    archFeed.innerHTML = '';
    if (!msgs.length) archFeed.append(h('p', { class: 'hint', i18n: 'chat.archive.none' }));
    for (const m of msgs) archFeed.append(chatLine(m));
    archInfo.textContent = d.truncated ? tn('chat.archive.truncated', { n: msgs.length }) : '';
    applyI18n();
  }
  daySel.addEventListener('change', showDay);
  search.addEventListener('input', () => { clearTimeout(searchT); searchT = setTimeout(showDay, 300); });

  function setStatus(connected, configured, errKey) {
    dot.className = 'chat-dot' + (connected ? ' on' : errKey === 'rcon.err.auth' ? ' bad' : '');
    statusTxt.textContent = connected ? t('chat.live')
      : errKey === 'rcon.err.auth' ? t('rcon.err.auth.short')
      : configured ? t('chat.disc') : t('chat.needRcon.badge');
  }

  // emptyHint says why the feed is empty — and offers the next step.
  function emptyHint(configured, connected, errKey) {
    const running = !!State.serverStatus.running;
    feed.innerHTML = '';
    if (!configured) {
      feed.append(h('p', { class: 'hint', i18n: 'chat.needRcon' }), h('div', { class: 'actions' }, [
        h('button', { type: 'button', class: 'primary', i18n: 'online.setRcon', onclick: () => navigate('rcon') })]));
    } else if (errKey === 'rcon.err.auth') {
      feed.append(h('p', { class: 'hint', i18n: errKey }));
    } else if (!connected && !running) {
      feed.append(h('p', { class: 'hint', i18n: 'chat.offline' }), h('div', { class: 'actions' }, [startServerBtn()]));
    } else {
      feed.append(h('p', { class: 'hint', i18n: connected ? 'chat.none' : 'chat.connecting' }));
    }
  }

  async function refresh() {
    let d;
    try { d = await api.get('/api/rcon/chat/recent?limit=100'); }
    catch { return; }
    if (myNav !== _navSeq) { if (timer) clearInterval(timer); return; }
    const msgs = d.messages || [];
    setStatus(!!d.connected, !!d.configured, d.errorKey);
    setSendable(!!d.configured, !!d.connected, d.errorKey);

    if (!msgs.length) {
      const sig = 'empty:' + d.configured + ':' + d.connected + ':' + (d.errorKey || '') + ':' + !!State.serverStatus.running;
      if (sig !== lastSig) { emptyHint(!!d.configured, !!d.connected, d.errorKey); lastSig = sig; }
      return;
    }
    // Cheap signature so a steady feed isn't torn down and rebuilt every poll.
    const last = msgs[msgs.length - 1];
    const sig = msgs.length + ':' + (last.at || 0) + ':' + (last.text || '');
    if (sig === lastSig) return;
    lastSig = sig;

    const atBottom = feed.scrollHeight - feed.scrollTop - feed.clientHeight < 40;
    feed.innerHTML = '';
    for (const m of msgs) feed.append(chatLine(m));
    if (atBottom) feed.scrollTop = feed.scrollHeight; // don't yank the view while reading up
  }
  renderPresets();
  const pl = await playersP;
  if (myNav !== _navSeq) return;
  if (pl) keyOf = playerKeyLookup(pl.players);
  await refresh();
  feed.scrollTop = feed.scrollHeight;
  timer = setInterval(refresh, 2500);
};

// --------------------------------------------------------------------- gameplay

// gpNightSVG draws one night of lightingConfig n (0 bright, 1 dark, 2
// Sakhal, 3 Nasdara): the sky, the moon, the land, and a candle — in the
// dark nights its glow is all there is to see by. Our own drawing.
function gpNightSVG(n) {
  const P = [
    { sky: ['#16244a', '#33497a'], far: '#2c4066', near: '#3a527c', tree: '#1c2a49', moon: 1, stars: .8, glow: .45 },
    { sky: ['#020306', '#06080d'], far: '#07090e', near: '#0a0d13', tree: '#040509', moon: .22, stars: .25, glow: 1 },
    { sky: ['#0b2232', '#21506a'], far: '#7d9db0', near: '#b9d1de', tree: '#183448', moon: .95, stars: .7, glow: .5 },
    { sky: ['#1d1430', '#53304a'], far: '#4c3327', near: '#6e4c38', tree: '#2a1b24', moon: .9, stars: .6, glow: .5 },
  ][n] || null;
  if (!P) return '';
  const stars = [[14, 12], [34, 26], [52, 9], [70, 20], [92, 8], [108, 24], [146, 34], [24, 38]]
    .map(([x, y]) => `<circle cx="${x}" cy="${y}" r=".9" fill="#fff" opacity="${P.stars}"/>`).join('');
  return `<svg viewBox="0 0 160 90" preserveAspectRatio="xMidYMid slice" aria-hidden="true">
  <defs>
    <linearGradient id="gpn${n}s" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${P.sky[0]}"/><stop offset="1" stop-color="${P.sky[1]}"/></linearGradient>
    <radialGradient id="gpn${n}g"><stop offset="0" stop-color="#ffd27a" stop-opacity="${(0.85 * P.glow).toFixed(2)}"/><stop offset=".5" stop-color="#ff9d42" stop-opacity="${(0.3 * P.glow).toFixed(2)}"/><stop offset="1" stop-color="#ff9d42" stop-opacity="0"/></radialGradient>
  </defs>
  <rect width="160" height="90" fill="url(#gpn${n}s)"/>
  ${stars}
  <circle cx="128" cy="19" r="7" fill="#eef2fa" opacity="${P.moon}"/>
  <path d="M0 60 Q28 46 58 56 T118 50 T160 56 V90 H0 Z" fill="${P.far}"/>
  <path d="M14 60 l6-17 6 17 Z M28 58 l5-13 5 13 Z M126 55 l6-16 6 16 Z M140 57 l4-11 4 11 Z" fill="${P.tree}"/>
  <path d="M0 71 Q40 63 80 69 T160 66 V90 H0 Z" fill="${P.near}"/>
  <circle cx="80" cy="70" r="30" fill="url(#gpn${n}g)"/>
  <rect x="77" y="65" width="6" height="11" rx="1.5" fill="#efe5cf"/>
  <path d="M80 57 Q83 61 80 64 Q77 61 80 57 Z" fill="#ffcf6b"/>
</svg>`;
}

// Settings the Gameplay page shows as pictures to pick from (visuals.js);
// the yes/no ones among them leave the switch list for the grid.
const GP_PICTURE_KEYS = ['lightingConfig', 'hitDirectionStyle', 'hitDirectionBehaviour', 'use3DMap', 'disablePersonalLight'];
const GP_PICTURE_BOOLS = ['use3DMap', 'disablePersonalLight'];

// gpNightPicker shows lightingConfig as the four nights to pick from.
function gpNightPicker(cur, onPick, disabled) {
  const wrap = h('div', { class: 'gp-night', role: 'radiogroup', 'aria-label': t('gp.light.title') });
  const draw = () => wrap.replaceChildren(...[0, 1, 2, 3].map(n => h('button', {
    type: 'button', role: 'radio', 'aria-checked': String(n === cur), class: 'gp-night-opt' + (n === cur ? ' on' : ''), disabled,
    onclick: () => { cur = n; onPick(n); draw(); },
  }, [
    h('span', { class: 'gp-night-art', html: gpNightSVG(n) }),
    h('b', { text: t('gp.light.' + n) }),
    h('small', { text: t('gp.light.' + n + '.sub') }),
    h('span', { class: 'gp-night-n mono', text: String(n) }),
  ])));
  draw();
  return wrap;
}

Views.gameplay = async (root) => {
  const myNav = _navSeq;
  const running = !!State.serverStatus.running;
  root.append(pageHeader('nav.gameplay', 'gameplay.subtitle', [revealBtn({ kind: 'missionfile', path: 'cfggameplay.json' })]));
  if (running) root.append(runningBanner());
  let d;
  try { d = await api.get('/api/gameplay'); }
  catch (e) { handleErr(e); return; }
  if (myNav !== _navSeq) return;
  const defaults = d.defaults || {};
  const sc = d.serverCfg || {};
  const clone = (v) => JSON.parse(JSON.stringify(v));
  const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);

  // No file: DayZ runs on its built-in values. Offer to create it.
  if (!d.exists) {
    root.append(h('div', { class: 'card' }, h('div', { class: 'empty-state' }, [
      h('div', { class: 'es-icon', html: svgIcon('i-gameplay', 34) }),
      h('div', { class: 'es-title', i18n: 'gameplay.missing.title' }),
      h('div', { class: 'es-hint', i18n: 'gameplay.missing.hint' }),
      h('div', { class: 'actions' }, [
        h('button', { type: 'button', class: 'primary', i18n: 'gameplay.create', disabled: running,
          onclick: (e) => withBusy(e.currentTarget, async () => {
            try {
              await api.post('/api/gameplay', { content: JSON.stringify(defaults, null, '\t') });
              toast(t('gameplay.created'), 'ok');
              await navigate('gameplay');
            } catch (err) { handleErr(err); }
          }) }),
      ]),
      h('p', { class: 'hint mono', text: d.path }),
    ])));
    return;
  }

  let obj = null;
  let parseErr = '';
  try {
    obj = JSON.parse(d.content);
    if (!obj || typeof obj !== 'object' || Array.isArray(obj)) throw new Error('not a JSON object');
  } catch (e) { obj = null; parseErr = e.message; }
  let baseline = obj ? JSON.stringify(obj) : null;
  let rawMode = !obj;

  const label = (k) => State.i18n['gp.' + k] ? t('gp.' + k) : k.replace(/([a-z0-9])([A-Z])/g, '$1 $2');
  const helpText = (k) => (State.help && State.help['gp.' + k]) || '';
  // A help marker inside a <label> must not flip its switch.
  const gpHelp = (k) => {
    if (!helpText(k)) return null;
    const m = help('gp.' + k);
    m.addEventListener('click', (e) => e.preventDefault());
    return m;
  };
  const GROUP_ICON = { GeneralData: 'i-settings', PlayerData: 'i-players', WorldsData: 'i-weather', BaseBuildingData: 'i-missiondb',
    UIData: 'i-dashboard', MapData: 'i-livemap', VehicleData: 'i-vehicles', ExternalLockData: 'i-shield', SandstormData: 'i-weather' };
  // The file's keys in its own order, then the defaults it leaves out.
  const keysOf = (f, def) => {
    const out = (f && typeof f === 'object') ? Object.keys(f) : [];
    if (def && typeof def === 'object') for (const k of Object.keys(def)) if (!out.includes(k)) out.push(k);
    return out;
  };
  const get = (path) => { let o = obj; for (const p of path) { if (!o || typeof o !== 'object') return undefined; o = o[p]; } return o; };
  const ensure = (path) => {
    let o = obj;
    for (const p of path) { if (!o[p] || typeof o[p] !== 'object' || Array.isArray(o[p])) o[p] = {}; o = o[p]; }
    return o;
  };
  const has = (path, key) => { const p = get(path); return !!p && typeof p === 'object' && Object.prototype.hasOwnProperty.call(p, key); };
  const monthNames = Array.from({ length: 12 }, (_, i) => {
    try { return new Date(2024, i, 15).toLocaleString(State.lang || undefined, { month: 'short' }); } catch { return String(i + 1); }
  });

  const formHost = h('div', { class: 'gp2' });
  const rawHost = h('textarea', { rows: 28, class: 'mono gp2-raw', spellcheck: 'false', 'aria-label': 'cfggameplay.json', hidden: !rawMode });
  rawHost.value = d.content;
  const search = h('input', { type: 'search', placeholder: t('gameplay.search'), 'aria-label': t('gameplay.search') });
  const noMatch = h('p', { class: 'hint gp2-nomatch', i18n: 'gameplay.nothing', hidden: true });
  const versionBadge = h('span', { class: 'badge mute mono' });

  // A note under the two settings serverDZ.cfg also has.
  const conflict = (key, cur) => {
    const v = sc[key];
    if (v === undefined || v === '') return null;
    const scVal = key === 'disablePersonalLight' ? (v === '1' || String(v).toLowerCase() === 'true') : Number(v);
    if (same(scVal, cur)) return null;
    return h('div', { class: 'gp2-note warn-text', text: t('gameplay.overrides').replace('{v}', String(v)) });
  };

  function leaf(path, key, def) {
    const inFile = has(path, key);
    const val = inFile ? get(path)[key] : def;
    const chip = h('span', { class: 'gp2-chip' });
    const resetBtn = h('button', { type: 'button', class: 'gp2-reset', title: t('gameplay.field.reset'), 'aria-label': t('gameplay.field.reset'), text: '↺', hidden: true,
      onclick: (e) => { e.preventDefault(); e.stopPropagation(); ensure(path)[key] = clone(def); changed(); renderForm(); } });
    const paintState = () => {
      const present = has(path, key);
      const cur = present ? get(path)[key] : def;
      const modified = def !== undefined && present && !same(cur, def);
      chip.className = 'gp2-chip' + (!present ? ' def' : modified ? ' mod' : '');
      chip.textContent = !present && def !== undefined ? t('gameplay.chip.default') : modified ? t('gameplay.chip.changed') : '';
      resetBtn.hidden = !modified || running;
    };
    const write = (v) => { ensure(path)[key] = v; paintState(); changed(); };
    const keyLine = h('span', { class: 'gp2-keyline' }, [h('code', { class: 'cfg-key', text: key }), chip, resetBtn]);
    let block;

    if (typeof val === 'boolean' && !GP_PICTURE_BOOLS.includes(key)) {
      const cb = h('input', { type: 'checkbox', class: 'switch', disabled: running });
      cb.checked = val;
      cb.onchange = () => write(cb.checked);
      block = h('label', { class: 'cfg-bool gp2-bool' }, [cb, h('span', { class: 'cfg-bool-text' }, [
        h('span', { class: 'cfg-bool-label' }, [h('span', { text: label(key) }), gpHelp(key)].filter(Boolean)), keyLine,
        conflict(key, val)].filter(Boolean))]);
    } else {
      let control;
      const hitColour = () => (get(path) || {}).hitDirectionIndicatorColorStr;
      if (key === 'lightingConfig') {
        control = gpNightPicker(Number(val), (n) => write(n), running);
      } else if (key === 'hitDirectionStyle') {
        control = gpHitStylePicker(Number(val), (n) => write(n), running, hitColour);
        control.classList.add('gp-hit');
      } else if (key === 'hitDirectionBehaviour') {
        control = gpHitBehaviourPicker(Number(val), (n) => write(n), running, hitColour);
        control.classList.add('gp-hit');
      } else if (key === 'use3DMap') {
        control = gpMapPicker(!!val, (b) => write(b), running);
      } else if (key === 'disablePersonalLight') {
        control = gpLightPicker(!!val, (b) => write(b), running);
      } else if (typeof val === 'number') {
        control = h('input', { type: 'number', step: 'any', value: String(val), disabled: running });
        control.oninput = () => { const n = Number(control.value); if (control.value.trim() !== '' && !Number.isNaN(n)) write(n); };
      } else if (typeof val === 'string' && key === 'hitDirectionIndicatorColorStr') {
        const sw = h('span', { class: 'gp2-swatch', 'aria-hidden': 'true' });
        const paint = (s) => {
          const m = /^0x([0-9a-f]{2})([0-9a-f]{6})$/i.exec(String(s).trim());
          sw.style.background = m ? '#' + m[2] : 'transparent';
          sw.style.opacity = m ? String(Math.max(0.15, parseInt(m[1], 16) / 255)) : '1';
        };
        const inp = h('input', { type: 'text', class: 'mono', value: val, spellcheck: 'false', disabled: running });
        // The hit indicator pictures are drawn in this colour.
        inp.oninput = () => { write(inp.value.trim()); paint(inp.value); formHost.querySelectorAll('.gp-hit').forEach(w => w._redraw && w._redraw()); };
        paint(val);
        control = h('div', { class: 'gp2-color' }, [sw, inp]);
      } else if (typeof val === 'string') {
        control = h('input', { type: 'text', value: val, disabled: running });
        control.oninput = () => write(control.value);
      } else if (key === 'spawnGearPresetFiles') {
        const files = Array.isArray(val) ? val : [];
        control = h('div', { class: 'gp2-chips' }, [
          ...(files.length ? files.map(f => h('span', { class: 'badge mute mono', text: f })) : [h('span', { class: 'hint', i18n: 'gameplay.gear.none' })]),
          h('button', { type: 'button', class: 'small secondary', i18n: 'gameplay.gear.manage', onclick: () => navigate('loadout') }),
        ]);
      } else if (/^environment(Min|Max)Temps$/.test(key)) {
        if (!Array.isArray(val)) {
          control = h('p', { class: 'hint', i18n: 'gameplay.temps.map' });
        } else {
          const arr = val.slice();
          control = h('div', { class: 'gp2-months' }, monthNames.map((m, i) => {
            const inp = h('input', { type: 'number', step: 'any', value: arr[i] ?? '', disabled: running, 'aria-label': label(key) + ' — ' + m });
            inp.oninput = () => { const n = Number(inp.value); if (inp.value.trim() !== '' && !Number.isNaN(n)) { arr[i] = n; write(arr.slice()); } };
            return h('label', { class: 'gp2-month' }, [h('span', { text: m }), inp]);
          }));
        }
      } else if (Array.isArray(val) && val.every(x => typeof x === 'string' || typeof x === 'number')) {
        const numeric = (Array.isArray(def) && def.length ? def : val).every(x => typeof x === 'number') && (val.length || (Array.isArray(def) && def.length));
        control = h('input', { type: 'text', class: 'mono', value: val.join(', '), spellcheck: 'false', disabled: running,
          placeholder: numeric ? '1, 1, 1.33' : 'a, b, c' });
        control.oninput = () => {
          const parts = control.value.split(',').map(s => s.trim()).filter(Boolean);
          if (numeric) {
            if (parts.some(p => Number.isNaN(Number(p)))) { control.classList.add('bad'); return; }
            control.classList.remove('bad');
            write(parts.map(Number));
          } else write(parts);
        };
      } else {
        control = h('input', { type: 'text', class: 'mono', value: JSON.stringify(val ?? null), spellcheck: 'false', disabled: running });
        control.oninput = () => {
          try { write(JSON.parse(control.value)); control.classList.remove('bad'); } catch { control.classList.add('bad'); }
        };
      }
      const wide = Array.isArray(val) || key === 'spawnGearPresetFiles' || GP_PICTURE_KEYS.includes(key);
      const lab = h('label', { text: label(key) });
      if (control.tagName === 'INPUT') lab.setAttribute('for', assocId(control));
      const hm = gpHelp(key);
      block = h('div', { class: 'gp2-field' + (wide ? ' wide' : '') }, [
        h('div', { class: 'cfg-label' }, [hm ? h('span', { class: 'with-help' }, [lab, hm]) : lab, keyLine]),
        control,
        conflict(key, val),
      ].filter(Boolean));
    }
    block.dataset.search = (label(key) + ' ' + key + ' ' + helpText(key)).toLowerCase();
    paintState();
    return block;
  }

  // A group of settings: top level = a collapsible card section, nested =
  // a titled block inside it.
  function group(path, key, def, depth) {
    const here = [...path, key];
    const fileObj = get(here);
    const defObj = (def && typeof def === 'object' && !Array.isArray(def)) ? def : {};
    const grid = h('div', { class: 'gp2-grid' });
    const bools = h('div', { class: 'cfg-bools gp2-bools' });
    const subs = [];
    for (const k of keysOf(fileObj, defObj)) {
      const v = (fileObj && typeof fileObj === 'object' && k in fileObj) ? fileObj[k] : defObj[k];
      if (v && typeof v === 'object' && !Array.isArray(v)) subs.push(group(here, k, defObj[k], depth + 1));
      else (typeof v === 'boolean' && !GP_PICTURE_BOOLS.includes(k) ? bools : grid).append(leaf(here, k, defObj[k]));
    }
    const body = [grid.childElementCount ? grid : null, bools.childElementCount ? bools : null, ...subs].filter(Boolean);
    if (depth === 0) {
      const count = h('span', { class: 'gp2-count' });
      const sec = h('details', { class: 'gp2-group', open: true }, [
        h('summary', {}, [
          h('span', { class: 'card-title-icon', html: svgIcon(GROUP_ICON[key] || 'i-settings', 16) }),
          h('span', { class: 'gp2-title', text: label(key) }),
          h('code', { class: 'cfg-key', text: key }),
          gpHelp(key),
          h('span', { class: 'grow' }),
          count,
        ].filter(Boolean)),
        h('div', { class: 'gp2-body' }, body),
      ]);
      sec._count = count;
      return sec;
    }
    return h('section', { class: 'gp2-sub' }, [
      h('h4', { class: 'gp2-subtitle' }, [h('span', { text: label(key) }), h('code', { class: 'cfg-key', text: key }), gpHelp(key)].filter(Boolean)),
      ...body,
    ]);
  }

  function renderForm() {
    formHost.innerHTML = '';
    if (!obj) return;
    const ver = has([], 'version') ? obj.version : defaults.version;
    versionBadge.textContent = t('gp.version') + ': ' + (ver ?? '—');
    const loose = h('div', { class: 'gp2-grid' });
    for (const k of keysOf(obj, defaults)) {
      if (k === 'version') continue;
      const v = k in obj ? obj[k] : defaults[k];
      if (v && typeof v === 'object' && !Array.isArray(v)) formHost.append(group([], k, defaults[k], 0));
      else loose.append(leaf([], k, defaults[k]));
    }
    if (loose.childElementCount) {
      formHost.append(h('details', { class: 'gp2-group', open: true }, [
        h('summary', {}, [h('span', { class: 'gp2-title', i18n: 'gameplay.group.other' })]),
        h('div', { class: 'gp2-body' }, loose),
      ]));
    }
    formHost.append(noMatch);
    recount();
    filter();
  }

  function recount() {
    formHost.querySelectorAll('details.gp2-group').forEach(sec => {
      if (!sec._count) return;
      const n = sec.querySelectorAll('.gp2-chip.mod').length;
      sec._count.textContent = n ? t('gameplay.changedCount').replace('{n}', n) : '';
      sec._count.hidden = !n;
    });
  }

  function filter() {
    const q = search.value.trim().toLowerCase();
    let any = false;
    formHost.querySelectorAll('[data-search]').forEach(el => {
      const show = !q || el.dataset.search.includes(q);
      el.hidden = !show;
      any = any || show;
    });
    formHost.querySelectorAll('.gp2-sub, details.gp2-group').forEach(sec => {
      const visible = [...sec.querySelectorAll('[data-search]')].some(el => !el.hidden);
      sec.hidden = !visible;
      if (q && visible && sec.tagName === 'DETAILS') sec.open = true;
    });
    noMatch.hidden = any || !q;
  }
  let _searchT = 0;
  search.oninput = () => { clearTimeout(_searchT); _searchT = setTimeout(filter, 120); };

  // Unsaved state: a sticky bar, and the page asks before leaving.
  const isDirty = () => rawMode ? rawHost.value !== d.content : (!!obj && JSON.stringify(obj) !== baseline);
  const barText = h('span', { i18n: 'gameplay.unsaved' });
  const bar = h('div', { class: 'sticky-save', hidden: true }, [
    barText, h('span', { class: 'grow' }),
    h('button', { type: 'button', class: 'secondary small', i18n: 'action.discard', onclick: () => { root._isDirty = null; navigate('gameplay'); } }),
    h('button', { type: 'button', class: 'primary small', i18n: 'action.save', disabled: running, onclick: (e) => withBusy(e.currentTarget, doSave) }),
  ]);
  function changed() { bar.hidden = !isDirty(); recount(); }
  rawHost.addEventListener('input', changed);

  const modeBtn = h('button', { type: 'button', class: 'secondary' });
  function setMode(raw) {
    if (!raw) {
      try {
        const o = JSON.parse(rawHost.value);
        if (!o || typeof o !== 'object' || Array.isArray(o)) throw new Error('not a JSON object');
        obj = o;
        if (baseline === null) baseline = JSON.stringify(obj);
      } catch (e) { toast(t('gameplay.invalid') + ' ' + e.message, 'error'); return; }
      renderForm();
    } else if (obj) {
      rawHost.value = JSON.stringify(obj, null, '\t');
    }
    rawMode = raw;
    formHost.hidden = raw;
    search.hidden = raw;
    rawHost.hidden = !raw;
    modeBtn.textContent = raw ? t('gameplay.form') : t('gameplay.raw');
    changed();
  }
  modeBtn.onclick = () => setMode(!rawMode);
  modeBtn.textContent = rawMode ? t('gameplay.form') : t('gameplay.raw');

  async function doSave() {
    const content = rawMode ? rawHost.value : JSON.stringify(obj, null, '\t');
    try { JSON.parse(content); }
    catch (e) { toast(t('gameplay.invalid') + ' ' + e.message, 'error'); return; }
    try {
      const r = await api.post('/api/gameplay', { content });
      d.content = content;
      if (!rawMode) baseline = JSON.stringify(obj);
      toast(t('msg.saved') + (r && r.enabledFlag ? ' · ' + t('gameplay.enabledNow') : ''), 'ok');
      changed();
    } catch (e) { handleErr(e); }
  }
  root._save = doSave;
  root._isDirty = isDirty;

  // Back to DayZ's values. Content the admin added (loadouts, object
  // spawners) and the map's monthly temperatures stay; nothing is written
  // until Save.
  async function doReset() {
    const ok = await confirmModal(t('gameplay.reset.confirm'), { title: t('gameplay.reset'), danger: true, okText: t('gameplay.reset') });
    if (!ok) return;
    const keep = obj ? {
      version: obj.version,
      gear: get(['PlayerData', 'spawnGearPresetFiles']),
      spawners: get(['WorldsData', 'objectSpawnersArr']),
      tmin: get(['WorldsData', 'environmentMinTemps']),
      tmax: get(['WorldsData', 'environmentMaxTemps']),
    } : {};
    obj = clone(defaults);
    if (keep.version !== undefined) obj.version = keep.version;
    if (Array.isArray(keep.gear)) obj.PlayerData.spawnGearPresetFiles = keep.gear;
    if (Array.isArray(keep.spawners)) obj.WorldsData.objectSpawnersArr = keep.spawners;
    if (Array.isArray(keep.tmin)) obj.WorldsData.environmentMinTemps = keep.tmin;
    if (Array.isArray(keep.tmax)) obj.WorldsData.environmentMaxTemps = keep.tmax;
    if (rawMode) setMode(false); else renderForm();
    changed();
    toast(t('gameplay.reset.done'), 'ok');
  }

  root.append(h('div', { class: 'card gp2-card' }, [
    // A missing line counts as 0 — and DayZ 1.29's serverDZ.cfg has none.
    d.serverCfg && sc.enableCfgGameplayFile !== '1'
      ? h('div', { class: 'warning-bar', i18n: 'gameplay.off' }) : null,
    parseErr ? h('div', { class: 'warning-bar err-bar', text: t('gameplay.invalid') + ' ' + parseErr }) : null,
    h('div', { class: 'players-toolbar gp2-toolbar' }, [
      search,
      h('span', { class: 'grow' }),
      versionBadge,
      modeBtn,
      h('button', { type: 'button', class: 'secondary', i18n: 'gameplay.reset', disabled: running, onclick: doReset }),
      h('button', { type: 'button', class: 'primary', i18n: 'action.save', title: 'Ctrl+S', disabled: running, onclick: (e) => withBusy(e.currentTarget, doSave) }),
    ]),
    formHost,
    rawHost,
    h('p', { class: 'hint mono gp2-path', text: d.path }),
  ].filter(Boolean)), bar);
  formHost.hidden = rawMode;
  search.hidden = rawMode;
  renderForm();
};

// --------------------------------------------------------------- history
//
// Every config snapshot the manager has taken (it keeps a `.bak.<ts>` next to
// each file it overwrites), grouped by file, newest first — so "what changed
// and roll it back" lives in one place instead of only inside the Files editor.
// Diff and restore reuse the same endpoints the per-file backup list uses.

Views.history = async (root) => {
  root.append(pageHeader('nav.history', 'history.subtitle'));
  if (State.serverStatus.running) root.append(runningBanner());
  let d;
  try { d = await api.get('/api/backups/all'); }
  catch (e) { handleErr(e); return; }
  const files = d.files || [];
  if (!files.length) {
    root.append(h('div', { class: 'card' }, [h('p', { class: 'hint', i18n: 'history.empty' })]));
    return;
  }

  const host = h('div');
  function render() {
    host.innerHTML = '';
    files.forEach(f => {
      const rows = h('div', { class: 'hist-versions' });
      f.versions.forEach(v => {
        rows.append(h('div', { class: 'hist-row' }, [
          h('span', { class: 'hist-when', text: new Date(v.time * 1000).toLocaleString() }),
          h('small', { class: 'hint hist-size', text: bytes(v.size) }),
          h('span', { class: 'grow' }),
          h('button', { class: 'secondary', i18n: 'backup.diff', onclick: () => openBackupDiff(f.path, v.name) }),
          h('button', { i18n: 'backup.restore', onclick: async () => {
            const ok = await confirmModal(t('history.restoreConfirm').replace('{file}', f.path),
              { title: t('backup.restore'), danger: true, okText: t('backup.restore') });
            if (!ok) return;
            try {
              await api.post('/api/backups/restore', { path: f.path, backup: v.name });
              toast(t('backup.restored'), 'ok');
              await navigate('history');
            } catch (err) { handleErr(err); }
          }}),
        ]));
      });
      host.append(h('div', { class: 'card hist-file' }, [
        h('div', { class: 'hist-head' }, [
          h('code', { class: 'hist-path', text: f.path }),
          f.exists ? null : h('span', { class: 'pill pill-warn', i18n: 'history.deleted' }),
          h('span', { class: 'grow' }),
          h('small', { class: 'hint', text: String(f.versions.length) }),
        ]),
        rows,
      ]));
    });
    applyI18n();
  }
  render();
  root.append(host);
};

// --------------------------------------------------------------- server health
//
// One page that answers "is anything wrong?" — server up / crash-loop, disk
// room, config validity, and start-readiness — each a coloured tile linking to
// the page that fixes it. Detailed start-failure findings reuse the same
// diagnosis card the dashboard shows when the server is down.

Views.health = async (root) => {
  root.append(pageHeader('nav.health', 'health.subtitle'));
  // /api/health runs the full config validator, which can take a second on a
  // big mission — show a placeholder so the page isn't blank meanwhile.
  const loading = h('div', { class: 'card' }, [h('p', { class: 'hint', i18n: 'health.checking' })]);
  root.append(loading);
  applyI18n();
  let d;
  try { d = await api.get('/api/health'); }
  catch (e) { loading.remove(); handleErr(e); return; }
  loading.remove();

  const tiles = h('div', { class: 'health-tiles' });
  const setTile = (el, level, titleKey, detail) => {
    el.className = 'health-tile ' + level + (el.dataset.route ? ' clickable' : '');
    el.querySelector('.health-tile-t').setAttribute('data-i18n', titleKey);
    el.querySelector('.health-tile-t').textContent = t(titleKey);
    el.querySelector('.health-tile-d').textContent = detail;
  };
  const tile = (level, titleKey, detail, route) => {
    const el = h('div', { class: 'health-tile ' + level + (route ? ' clickable' : '') }, [
      h('div', { class: 'health-dot' }),
      h('div', { class: 'health-tile-body' }, [
        h('div', { class: 'health-tile-t', i18n: titleKey }),
        h('div', { class: 'health-tile-d', text: detail }),
      ]),
    ]);
    if (route) { el.dataset.route = route; el.onclick = () => { location.hash = '#' + route; }; }
    tiles.append(el);
    return el;
  };

  const s = d.server || {};
  if (s.running) tile('ok', 'health.server', t('health.server.up').replace('{up}', s.uptimeSec != null ? fmtAge(s.uptimeSec) : (s.uptime || '')), 'dashboard');
  else if (s.loopPaused) tile('error', 'health.server', t('health.server.crashloop'), 'dashboard');
  else tile('idle', 'health.server', t('health.server.stopped'), 'dashboard');

  const free = (d.disk || {}).free || 0;
  const GB = 1024 * 1024 * 1024;
  let dl = 'ok';
  if (free > 0 && free < 2 * GB) dl = 'error';
  else if (free > 0 && free < 10 * GB) dl = 'warn';
  tile(free ? dl : 'idle', 'health.disk', t('health.disk.free').replace('{free}', bytes(free)), 'files');

  // Config validity runs the full validator (~seconds on a big mission), so it
  // is fetched separately: the tile shows "checking" and fills in when ready,
  // instead of holding up the whole page.
  const cfgTile = tile('idle', 'health.config', t('health.checking'), 'validator');

  const st = d.startup || {};
  if (s.running) tile('ok', 'health.start', t('health.start.running'), null);
  else if ((st.fatal || 0) > 0) tile('error', 'health.start', tn('health.start.issues', { n: st.fatal }), null);
  else if ((st.warnings || 0) > 0) tile('warn', 'health.start', tn('health.start.issues', { n: st.warnings }), null);
  else tile('ok', 'health.start', t('health.start.ok'), null);

  root.append(h('div', { class: 'card' }, [tiles]));
  applyI18n();

  api.get('/api/validate').then((v) => {
    const iss = v.issues || [];
    const e = iss.filter((x) => x.severity === 'error').length;
    const wn = iss.filter((x) => x.severity === 'warning').length;
    if (e > 0) setTile(cfgTile, 'error', 'health.config', tn('health.config.issues', { e, w: wn }));
    else if (wn > 0) setTile(cfgTile, 'warn', 'health.config', tn('health.config.issues', { e: 0, w: wn }));
    else setTile(cfgTile, 'ok', 'health.config', t('health.config.ok'));
  }).catch(() => setTile(cfgTile, 'idle', 'health.config', '—'));

  // Disk cleanup — server logs and .bak snapshots quietly eat gigabytes on a
  // long-lived server. Show how much each occupies and clear it on demand.
  const cleanupHost = h('div', { class: 'cleanup-rows' });
  root.append(h('div', { class: 'card' }, [
    h('h3', { i18n: 'health.cleanup.title' }),
    h('p', { class: 'hint', i18n: 'health.cleanup.hint' }),
    cleanupHost,
  ]));
  async function renderCleanup() {
    let c;
    try { c = await api.get('/api/cleanup/scan'); } catch { return; }
    cleanupHost.innerHTML = '';
    const row = (target, labelKey, info) => {
      const clear = h('button', { class: 'secondary', i18n: 'health.cleanup.clear', disabled: !info.count,
        onclick: async () => {
          const ok = await confirmModal(
            tn('health.cleanup.confirm.' + target, { n: info.count, size: bytes(info.bytes) }),
            { danger: true, okText: t('health.cleanup.clear') });
          if (!ok) return;
          try {
            const r = await api.post('/api/cleanup', { target });
            toast(tn('health.cleanup.done', { n: r.deleted, size: bytes(r.freed) }), 'ok');
            await renderCleanup();
          } catch (e) { handleErr(e); }
        } });
      return h('div', { class: 'cleanup-row' }, [
        h('span', { class: 'cleanup-label', i18n: labelKey }),
        h('span', { class: 'cleanup-size', text: info.count ? (info.count + ' · ' + bytes(info.bytes)) : t('health.cleanup.none') }),
        h('span', { class: 'grow' }),
        clear,
      ]);
    };
    cleanupHost.append(row('logs', 'health.cleanup.logs', c.logs || {}));
    cleanupHost.append(row('adm', 'health.cleanup.adm', c.adm || {}));
    cleanupHost.append(row('backups', 'health.cleanup.backups', c.backups || {}));
    cleanupHost.append(row('wipes', 'health.cleanup.wipes', c.wipes || {}));
    applyI18n();
  }
  renderCleanup();

  // The detailed "why won't it start" card — self-fetching, and it only renders
  // when the server is down and the log actually shows a cause.
  await renderDiagnosis(root, false);
};

// --------------------------------------------------------------- sponsors
//
// A thank-you wall for the people funding the manager's development, and a nudge
// for anyone who wants the project to keep growing. The sponsor's own words and
// the developer's reply are shown verbatim; only the surrounding labels are
// translated. Anonymous supporters who'd like their real name/Steam shown are
// told how to reach the developer.

Views.sponsors = async (root) => {
  root.append(pageHeader('nav.sponsors', 'sponsors.subtitle'));
  let d;
  try { d = await api.get('/api/sponsors'); }
  catch (e) { handleErr(e); return; }
  const list = d.sponsors || [];

  root.append(h('div', { class: 'card' }, [
    h('p', { class: 'sponsors-intro', i18n: 'sponsors.intro' }),
  ]));

  if (!list.length) {
    root.append(h('div', { class: 'card' }, [h('p', { class: 'hint', i18n: 'sponsors.none' })]));
  } else {
    const wall = h('div', { class: 'sponsors-wall' });
    for (const s of list) {
      const head = h('div', { class: 'sponsor-head' }, [
        h('span', { class: 'sponsor-name', text: s.anon ? t('sponsors.anonymous') : (s.name || '—') }),
        // A title the project gave this sponsor (the lead tester of a platform).
        s.role ? h('span', { class: 'sponsor-role' }, [
          h('span', { class: 'sponsor-role-icon', html: svgIcon(s.role.endsWith('Linux') ? 'i-rcon' : 'i-monitor', 14) }),
          h('span', { i18n: s.role }),
        ]) : null,
        s.amount ? h('span', { class: 'sponsor-amount', text: s.amount }) : null,
        s.date ? h('span', { class: 'sponsor-date', text: s.date }) : null,
      ]);
      const body = [head];
      if (s.link) {
        // A Steam profile link is a long number; its label says what it is.
        const steam = /steamcommunity\.com\//i.test(s.link);
        body.push(h('a', { class: 'sponsor-link', href: s.link, target: '_blank', rel: 'noopener',
          text: steam ? t('sponsors.link.steam') : s.link.replace(/^https?:\/\/(www\.)?/, '').replace(/\/$/, '') }));
      }
      // Testimonials are shown in the UI language when a translation exists,
      // otherwise verbatim in the language the sponsor wrote them in.
      const lang = State.lang || 'en';
      const msg = (s.messageI18n && s.messageI18n[lang]) || s.message;
      const reply = (s.replyI18n && s.replyI18n[lang]) || s.reply;
      if (msg) body.push(h('blockquote', { class: 'sponsor-msg', text: '“' + msg + '”' }));
      if (reply) body.push(h('div', { class: 'sponsor-reply' }, [
        h('span', { class: 'sponsor-reply-label', i18n: 'sponsors.reply.label' }),
        h('span', { text: reply }),
      ]));
      if (s.anon) body.push(h('p', { class: 'hint sponsor-anon', i18n: 'sponsors.anon.note' }));
      wall.append(h('div', { class: 'card sponsor-card' + (s.highlight ? ' sponsor-hl' : '') }, body));
    }
    root.append(wall);
  }

  // Call to action. The donation buttons themselves live in the always-present
  // support footer right below, so this stays a motivational heading + note
  // rather than duplicating the exact same button row.
  root.append(h('div', { class: 'card sponsors-cta' }, [
    h('h3', { i18n: 'sponsors.cta.title' }),
    h('p', { class: 'hint', i18n: 'sponsors.cta.hint' }),
    h('div', { class: 'donate-buttons' }, donateLinks()),
  ]));
  applyI18n();
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
  // A vehicle's "Parts & cargo" opens its editor straight away.
  const openFirst = !!(_pageQuery && _pageQuery.route === 'attachments' && _pageQuery.open);
  const preQuery = takePageQuery('attachments'); // a link from Where to find
  root.append(pageHeader('nav.attach', 'attach.subtitle', [revealBtn({ kind: 'missionfile', path: 'cfgspawnabletypes.xml' })]));
  if (State.serverStatus.running) root.append(runningBanner());

  let d, presets = [], classNames = [];
  try { d = await api.get('/api/spawnable'); }
  catch (e) { handleErr(e); return; }
  try { presets = (await api.get('/api/spawnable/presets')).presets || []; } catch {}
  try { classNames = (await api.get('/api/spawnable/classnames')).names || []; } catch {}
  // Random-preset names (cfgrandompresets.xml) for the ⚙ preset-reference groups,
  // split by kind so the cargo/attachments datalists offer the right ones.
  const rpNames = { cargo: [], attachments: [] };
  try {
    const rp = await api.get('/api/randompresets');
    for (const p of (rp.presets || [])) {
      (p.kind === 'cargo' ? rpNames.cargo : rpNames.attachments).push(p.name);
    }
  } catch {}
  if (myNav !== _navSeq) return; // types.xml parsing is not instant
  // Class names compare case-insensitively, like the backend does: Bohemia's
  // own files write Mag_CZ550_10rnd where types.xml has Mag_CZ550_10Rnd, and
  // those spawn fine — they must not be flagged as typos.
  const classLower = new Set(classNames.map(n => n.toLowerCase()));

  // A shared <datalist> lets every class-name input autocomplete against the
  // types actually present on THIS server (vanilla + mods), so typos — the
  // usual reason an attachment silently never spawns — mostly disappear.
  const dl = h('datalist', { id: 'dz-classnames' });
  for (const n of classNames.slice(0, 5000)) dl.append(h('option', { value: n }));
  root.append(dl);
  const dlCargoP = h('datalist', { id: 'dz-cargopresets' });
  for (const n of rpNames.cargo) dlCargoP.append(h('option', { value: n }));
  const dlAttachP = h('datalist', { id: 'dz-attachpresets' });
  for (const n of rpNames.attachments) dlAttachP.append(h('option', { value: n }));
  root.append(dlCargoP, dlAttachP);

  const card = h('div', { class: 'card' }, [
    h('p', { class: 'hint mono', text: d.path || '' }),
  ]);
  root.append(card);

  if (!d.exists) {
    card.append(h('div', { class: 'empty-state' }, [h('div', { class: 'es-hint', i18n: 'attach.missing' })]));
    return;
  }

  const search = h('input', { type: 'search', placeholder: t('types.search'), 'aria-label': t('types.search') });
  search.value = preQuery;
  const listHost = h('div');

  // Filter by what an entry is. Counts come from the server so they describe
  // the whole file, not just the page being shown.
  let kindFilter = '';
  const kinds = d.kinds || {};
  const KIND_ORDER = ['weapon', 'clothing', 'container', 'infected', 'animal', 'vehicle', 'other'];
  const chipHost = h('div', { class: 'kind-chips' });
  const buildChips = () => {
    chipHost.innerHTML = '';
    const mk = (id, label, count) => {
      const b = h('button', {
        class: 'kind-chip' + (kindFilter === id ? ' active' : ''),
        type: 'button',
        onclick: () => { kindFilter = id; page = 1; buildChips(); renderList(); },
      }, [h('span', { text: label }), h('span', { class: 'kind-count', text: String(count) })]);
      return b;
    };
    chipHost.append(mk('', t('attach.kind.all'), (d.types || []).length));
    for (const k of KIND_ORDER) {
      if (kinds[k]) chipHost.append(mk(k, t('attach.kind.' + k), kinds[k]));
    }
  };
  buildChips();

  const presetSel = h('select', { style: { maxWidth: '240px' }, 'aria-label': t('attach.preset.pick') });
  presetSel.append(h('option', { value: '', text: t('attach.preset.pick') }));
  // Grouped by kind: the file covers weapons, clothing, containers and
  // vehicles, and the picker should make that visible rather than implying
  // the editor is weapons-only.
  for (const k of ['weapon', 'clothing', 'container', 'infected', 'animal', 'vehicle']) {
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
  // A pager instead of a hard 400-row cap: the rest of a big file used to be
  // reachable only through search.
  const PAGE = 100;
  let page = 1;
  function renderList() {
    listHost.innerHTML = '';
    const q = search.value.trim().toLowerCase();
    let rows = all;
    if (kindFilter) rows = rows.filter(x => (x.kind || 'other') === kindFilter);
    if (q) rows = rows.filter(x => x.name.toLowerCase().includes(q));
    if (!rows.length) {
      // A search that matched nothing is not an empty file: "pick a template
      // and press Add" made a 574-entry file look empty.
      const filtering = !!(q || kindFilter);
      listHost.append(h('div', { class: 'empty-state' }, filtering ? [
        h('div', { class: 'es-title', i18n: 'filter.none' }),
        h('div', { class: 'actions' }, [h('button', { class: 'secondary', i18n: 'filter.reset',
          onclick: () => { search.value = ''; kindFilter = ''; page = 1; buildChips(); renderList(); } })]),
      ] : [h('div', { class: 'es-hint', i18n: 'attach.none' })]));
      applyI18n();
      return;
    }
    const pages = Math.max(1, Math.ceil(rows.length / PAGE));
    if (page > pages) page = pages;
    const start = (page - 1) * PAGE;
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
    for (const st of rows.slice(start, start + PAGE)) {
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

      // The whole row opens the editor — on a phone the Edit column does not fit.
      const tr = h('tr', { class: 'attach-row', title: t('action.edit') }, [
        h('td', {}, h('div', { class: 'types-namecell' }, [
          h('span', { class: 'tk-ico tk-' + (ATTACH_KIND_TK[kind] || 'other'), 'aria-hidden': 'true', html: svgIcon(ATTACH_KIND_ICON[kind] || 'i-c-other', 15) }),
          h('div', { class: 'types-namecol' }, [
            h('span', { class: 'attach-name-cell', text: st.name }),
            st.category ? h('span', { class: 'attach-cat', text: st.category }) : null,
          ]),
        ])),
        h('td', {}, h('span', { class: 'kind-badge k-' + kind, text: t('attach.kind.' + kind) })),
        num(att.length),
        num(cargo.length),
        h('td', { class: 'attach-sum' }, [h('span', { class: 'attach-sum-in hint', text: summary })]),
        h('td', {}, h('button', { class: 'small', i18n: 'action.edit', onclick: () => openAttachEditor(st, false) })),
      ]);
      tr.addEventListener('click', (e) => { if (!e.target.closest('button')) openAttachEditor(st, false); });
      tb.append(tr);
    }
    tbl.append(tb);
    // table-scroll-narrow: on desktop the header follows the page (a plain
    // .table-scroll is a scroll box, so its sticky header stuck to nothing).
    listHost.append(h('div', { class: 'table-scroll-narrow' }, tbl));
    if (pages > 1) {
      const go = (p) => {
        page = p;
        renderList();
        window.scrollTo({ top: Math.max(0, listHost.getBoundingClientRect().top + window.scrollY - 80) });
      };
      const jump = h('input', { type: 'number', value: page, min: 1, max: pages, 'aria-label': t('pager.jump') });
      jump.onchange = () => { const n = parseInt(jump.value, 10); if (n >= 1 && n <= pages) go(n); };
      listHost.append(h('div', { class: 'pager' }, [
        h('button', { text: '←', disabled: page <= 1, onclick: () => go(page - 1) }),
        h('button', { text: '→', disabled: page >= pages, onclick: () => go(page + 1) }),
        h('span', { class: 'pager-num', text: `${start + 1}–${Math.min(start + PAGE, rows.length)} / ${rows.length}` }),
        h('span', { class: 'spacer' }),
        h('span', { class: 'hint', text: t('pager.jump') }),
        jump,
        h('span', { class: 'hint', text: `/ ${pages}` }),
      ]));
    }
    applyI18n();
  }
  let _t = 0;
  search.oninput = () => { clearTimeout(_t); _t = setTimeout(() => { page = 1; renderList(); }, 200); };
  renderList();
  if (openFirst && preQuery) {
    // From a vehicle: its entry, or a new one named after it.
    const hit = all.find(x => x.name.toLowerCase() === preQuery.toLowerCase());
    openAttachEditor(hit || { name: preQuery, attachments: [], cargo: [] }, !hit);
  }

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
    // Esc, a stray backdrop click or × ask before throwing slot edits away.
    let edited = false;
    const m = openModal({ title: isNew ? t('attach.add') : model.name, wide: true, isDirty: () => edited });
    m.body.addEventListener('input', () => { edited = true; });
    m.body.addEventListener('change', () => { edited = true; });

    const nameInp = h('input', { type: 'text', value: model.name, list: 'dz-classnames',
      placeholder: 'AKM', spellcheck: 'false' });

    // The attachments side: the item drawn with a tile per slot (when the
    // game files describe the class) and the group list under it. Rebuilt
    // when the class name changes; the model itself is kept.
    const markEdited = () => { edited = true; };
    const attHost = h('div');
    let cat = null, det = null;
    function buildAtt() {
      const se = seEditor(model.attachments, { cls: model.name, det, cat, info: cat && model.name ? loItemInfo(cat, model.name) : null,
        classLower, edited: markEdited });
      attHost.replaceChildren(se.el);
    }
    buildAtt();
    // What is inside: groups like the loadout's "what they carry".
    const cargoOpts = { kind: 'cargo', cat, classLower, edited: markEdited, changed: markEdited,
      addKey: 'se.addCargo', titleKey: 'se.cargoGroup', emptyKey: 'attach.noCargo' };
    const cargo = seGroupList(model.cargo, cargoOpts);
    let loadSeq = 0;
    async function loadDetail() {
      const seq = ++loadSeq;
      if (!cat) { try { cat = await loCatalog(); } catch {} }
      const d2 = model.name ? await loDetail(model.name) : null;
      if (seq !== loadSeq) return;
      det = d2;
      buildAtt();
      cargoOpts.cat = cat;
      cargo.refresh();
    }
    loadDetail();
    nameInp.oninput = () => {
      model.name = nameInp.value.trim();
      clearTimeout(nameInp._t);
      nameInp._t = setTimeout(loadDetail, 500);
    };

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


    m.body.append(
      h('div', {}, [withHelp(h('label', { i18n: 'attach.weapon' }), 'attach.class'), nameInp]),

      // --- attachments: what is mounted ON the item ---
      h('div', { class: 'attach-section' }, [
        withHelp(h('h3', { i18n: 'attach.section.attachments' }), 'attach.slotChance'),
        h('p', { class: 'hint', i18n: 'attach.help' }),
        attHost,
      ]),

      // --- cargo: what is INSIDE it. Half of cfgspawnabletypes.xml is this,
      // and it was not editable at all before. ---
      h('div', { class: 'attach-section' }, [
        withHelp(h('h3', { i18n: 'attach.section.cargo' }), 'attach.cargo'),
        h('p', { class: 'hint', i18n: 'attach.cargo.help' }),
        cargo.el,
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
          // The entry as it is in the file — the name field may hold a rename
          // in progress.
          const delName = seed.name || model.name;
          if (!(await confirmModal(t('confirm.delete').replace('{name}', delName), { danger: true, okText: t('action.delete') }))) return;
          await withBusy(btn, async () => {
            try {
              await api.del('/api/spawnable/item?name=' + encodeURIComponent(delName));
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
      const origName = isNew ? '' : (seed.name || '');
      const renamed = !isNew && model.name !== origName;
      // Saving replaces the entry with exactly this name, and DayZ reads class
      // names case-insensitively: another entry must never be overwritten (or
      // shadowed by a case variant) without asking.
      const lower = model.name.toLowerCase();
      const clash = (isNew || renamed) ? all.find(x => x.name !== origName && x.name.toLowerCase() === lower) : null;
      if (clash) {
        if (!(await confirmModal(tn('attach.replaceConfirm', { name: clash.name }), { danger: true, okText: t('attach.replace') }))) return;
        model.name = clash.name;
      } else if (renamed) {
        // A rename used to save a second entry and leave the old one behind.
        if (!(await confirmModal(tn('attach.renameConfirm', { from: origName, to: model.name })))) return;
      }
      // Drop preset-reference groups whose name was left blank — an empty
      // preset="" is meaningless and would clutter the file.
      const cleanGroups = (list) => list.filter(g => (typeof g.preset === 'string') ? g.preset.trim() !== '' : true);
      const payload = {
        name: model.name,
        hoarder: model.hoarder,
        damageMin: model.damageMin,
        damageMax: model.damageMax,
        tags: model.tags,
        attachments: cleanGroups(model.attachments),
        cargo: cleanGroups(model.cargo),
      };
      try {
        await api.post('/api/spawnable/item', payload);
        if (renamed && origName) await api.del('/api/spawnable/item?name=' + encodeURIComponent(origName));
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

// Start once every script has run (features.js registers more views after this
// file), so the first navigation can never race a view that isn't defined yet.
if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', main);
else main();
