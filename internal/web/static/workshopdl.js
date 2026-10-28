// Copyright (c) 2026 Aristarh Ucolov.
//
// Mods → Direct download: Steam Workshop mods straight onto the server with
// Valve's SteamCMD — no DayZ client, no download folder. Three steps: get
// SteamCMD, sign in once with a Steam account that owns DayZ (the password
// goes to SteamCMD only; it keeps its own login token), paste Workshop links.
// The backend is internal/web/workshop_dl.go.

function workshopDownloadCard() {
  const card = h('div', { class: 'card wsdl', id: 'workshop-download' }, [
    cardTitle('i-mods', 'wsdl.title', 'wsdl'),
    h('p', { class: 'hint', i18n: 'wsdl.hint' }),
  ]);
  const body = h('div', { class: 'wsdl-body' });
  card.append(body);
  let st = null;
  let poll = 0;
  let found = [];       // looked-up items
  const picked = new Set();
  let codeNeed = '';    // 'guard' | '2fa' after a sign-in asked for a code
  let lastUser = '';
  let lastPass = '';    // kept in memory for the code step only

  async function load() {
    try { st = await api.get('/api/workshop/dl/status'); }
    catch (e) { body.textContent = e.message; return; }
    render();
    const running = st.job && st.job.state === 'running';
    clearTimeout(poll);
    if (running && card.isConnected) poll = setTimeout(load, 1500);
    // A finished download: the mods table above shows the new mods.
    if (!running && st.job && (st.job.kind === 'download' || st.job.kind === 'sync') && card._watching) {
      card._watching = false;
      const ok = (st.job.items || []).filter(i => i.state === 'done').length;
      if (st.job.kind === 'sync' && !(st.job.items || []).length && !st.job.error) toast(t('wsdl.updates.none'), 'ok');
      else if (st.job.kind === 'sync' && ok) { toast(tn('wsdl.updatedN', { n: ok }), 'ok'); setTimeout(() => navigate('mods'), 600); }
      else if (ok) { toast(tn('wsdl.installedN', { n: ok }), 'ok'); setTimeout(() => navigate('mods'), 600); }
    }
    if (!running && st.job && (st.job.kind === 'login' || st.job.kind === 'check') && card._watching) {
      card._watching = false;
      const lr = st.job.login || {};
      if (lr.ok) { lastPass = ''; codeNeed = ''; toast(t('wsdl.login.ok'), 'ok'); }
      else if (lr.need) { codeNeed = lr.need; }
      render();
    }
  }
  const watch = async (p) => {
    try { await p; card._watching = true; await load(); }
    catch (e) { handleErr(e); }
  };

  const step = (n, key, state, content) => h('div', { class: 'wsdl-step ' + state }, [
    h('span', { class: 'wsdl-n', text: state === 'ok' ? '✓' : String(n) }),
    h('div', { class: 'wsdl-step-main' }, [h('div', { class: 'wsdl-step-title', i18n: key }), ...content]),
  ]);

  function render() {
    body.innerHTML = '';
    const job = st.job || null;
    const busy = job && job.state === 'running';
    const fmtB = (n) => (n ? bytes(n) : '—');

    // 1 — SteamCMD itself.
    const s1 = [];
    if (st.installed) s1.push(h('div', { class: 'hint', text: t('wsdl.tool.ok').replace('{dir}', st.dir) }));
    else {
      s1.push(h('p', { class: 'hint', i18n: 'wsdl.tool.need' }));
      s1.push(h('button', { class: 'primary', i18n: 'wsdl.tool.install', disabled: busy,
        onclick: (e) => withBusy(e.currentTarget, () => watch(api.post('/api/workshop/dl/setup', {}))) }));
    }
    if (job && job.kind === 'setup') {
      if (busy) s1.push(h('div', { class: 'wsdl-progress', i18n: 'wsdl.tool.installing' }));
      if (job.state === 'error') s1.push(h('div', { class: 'warning-bar err-bar', text: t('wsdl.tool.failed').replace('{err}', job.error || '') }));
    }
    body.append(step(1, 'wsdl.step.tool', st.installed ? 'ok' : 'todo', s1));

    // 2 — the Steam account.
    const s2 = [];
    const signedIn = st.user && st.loggedIn === true;
    if (!st.installed) {
      s2.push(h('p', { class: 'hint', i18n: 'wsdl.login.afterTool' }));
    } else if (signedIn && !codeNeed) {
      s2.push(h('div', { class: 'wsdl-signed' }, [
        h('span', { text: t('wsdl.login.as').replace('{user}', st.user) }),
        h('button', { class: 'secondary small', i18n: 'wsdl.login.check', disabled: busy,
          onclick: (e) => withBusy(e.currentTarget, () => watch(api.post('/api/workshop/dl/check', {}))) }),
        h('button', { class: 'secondary small', i18n: 'wsdl.login.forget', disabled: busy,
          onclick: (e) => withBusy(e.currentTarget, async () => {
            if (!(await confirmModal(t('wsdl.login.forgetConfirm'), { okText: t('wsdl.login.forget') }))) return;
            try { await api.post('/api/workshop/dl/forget', {}); await load(); } catch (err) { handleErr(err); }
          }) }),
      ]));
    } else {
      const user = h('input', { type: 'text', autocomplete: 'username', placeholder: t('wsdl.login.user'), 'aria-label': t('wsdl.login.user'), value: lastUser || st.user || '' });
      const pass = h('input', { type: 'password', autocomplete: 'current-password', placeholder: t('wsdl.login.pass'), 'aria-label': t('wsdl.login.pass') });
      const code = h('input', { type: 'text', inputmode: 'text', maxlength: '10', autocomplete: 'one-time-code', class: 'mono wsdl-code',
        placeholder: t(codeNeed === '2fa' ? 'wsdl.code.2fa' : 'wsdl.code.mail'), 'aria-label': t('wsdl.code.label') });
      const go = (e) => withBusy(e.currentTarget, async () => {
        const u = user.value.trim();
        const p = codeNeed ? lastPass : pass.value;
        if (!u || !p) { toast(t('wsdl.login.needBoth'), 'error'); return; }
        lastUser = u; lastPass = p;
        await watch(api.post('/api/workshop/dl/login', { user: u, password: p, code: codeNeed ? code.value.trim() : '' }));
      });
      if (!st.secure) s2.push(h('p', { class: 'warning-bar', i18n: 'wsdl.login.insecure' }));
      if (codeNeed) {
        s2.push(h('p', { class: 'info-bar', i18n: codeNeed === '2fa' ? 'wsdl.code.2faHint' : 'wsdl.code.mailHint' }));
        s2.push(h('div', { class: 'row wsdl-row' }, [code, h('button', { class: 'primary', i18n: 'wsdl.code.send', disabled: busy || !st.secure, onclick: go }),
          h('button', { class: 'linkish', i18n: 'action.cancel', onclick: () => { codeNeed = ''; lastPass = ''; render(); } })]));
      } else {
        s2.push(h('div', { class: 'row wsdl-row' }, [user, pass, h('button', { class: 'primary', i18n: 'wsdl.login.signin', disabled: busy || !st.secure, onclick: go })]));
      }
      s2.push(h('small', { class: 'hint', i18n: 'wsdl.login.why' }));
    }
    if (job && (job.kind === 'login' || job.kind === 'check')) {
      if (busy) s2.push(h('div', { class: 'wsdl-progress', i18n: 'wsdl.login.waiting' }));
      else if (job.login && !job.login.ok && !job.login.need) {
        s2.push(h('div', { class: 'warning-bar err-bar', text: t('wsdl.login.err.' + (job.login.reason || 'unknown')) + (job.login.detail ? ' (' + job.login.detail + ')' : '') }));
      }
    }
    body.append(step(2, 'wsdl.step.login', signedIn ? 'ok' : 'todo', s2));

    // 3 — the mods.
    const s3 = [];
    const links = h('textarea', { rows: '3', class: 'mono', placeholder: t('wsdl.links.ph'), 'aria-label': t('wsdl.links.ph') });
    links.value = card._links || '';
    links.oninput = () => { card._links = links.value; };
    const serverSide = h('input', { type: 'checkbox', class: 'switch' });
    serverSide.checked = !!card._serverSide;
    serverSide.onchange = () => { card._serverSide = serverSide.checked; };
    s3.push(links, h('div', { class: 'row wsdl-row' }, [
      h('button', { i18n: 'wsdl.links.lookup', disabled: busy, onclick: (e) => withBusy(e.currentTarget, async () => {
        try {
          const r = await api.post('/api/workshop/dl/lookup', { text: links.value });
          found = r.items || [];
          picked.clear();
          for (const it of found) if (it.found && it.dayz && !it.installed) picked.add(it.id);
          render();
        } catch (err) { handleErr(err); }
      }) }),
      h('button', { class: 'secondary', i18n: 'wsdl.updates', disabled: busy, onclick: (e) => withBusy(e.currentTarget, async () => {
        try {
          const r = await api.post('/api/workshop/dl/updates', {});
          found = (r.mods || []).map(m => ({ id: m.id, title: m.title, size: m.size, updated: m.updated, found: true, dayz: true, installed: m.mod, newer: m.newer }));
          picked.clear();
          for (const it of found) if (it.newer) picked.add(it.id);
          if (!found.some(f => f.newer)) toast(t('wsdl.updates.none'), 'ok');
          render();
        } catch (err) { handleErr(err); }
      }) }),
    ]));
    if (found.length) {
      const list = h('div', { class: 'wsdl-list' });
      for (const it of found) {
        const cb = h('input', { type: 'checkbox', disabled: !it.found || !it.dayz });
        cb.checked = picked.has(it.id);
        cb.onchange = () => { if (cb.checked) picked.add(it.id); else picked.delete(it.id); render(); };
        list.append(h('label', { class: 'wsdl-item' + (!it.found || !it.dayz ? ' bad' : '') }, [
          cb,
          h('span', { class: 'wsdl-item-main' }, [
            h('b', { text: it.found ? it.title : t('wsdl.item.notFound') }),
            h('span', { class: 'hint mono', text: it.id + (it.size ? ' · ' + fmtB(it.size) : '') + (it.updated ? ' · ' + fmtDateTime(it.updated) : '') }),
          ]),
          it.installed ? h('span', { class: 'badge ' + (it.newer ? 'warn' : 'mute'), text: (it.newer ? t('wsdl.item.newer') + ' · ' : '') + it.installed }) : null,
          it.found && !it.dayz ? h('span', { class: 'badge err', i18n: 'wsdl.item.notDayz' }) : null,
        ]));
      }
      s3.push(list);
      const total = found.filter(f => picked.has(f.id)).reduce((s, f) => s + (f.size || 0), 0);
      s3.push(h('div', { class: 'row wsdl-row' }, [
        h('label', { class: 'inline-switch' }, [serverSide, h('span', { i18n: 'wsdl.serverSide' })]),
        h('span', { class: 'grow' }),
        h('button', { class: 'linkish', i18n: 'wsdl.list.clear', disabled: busy, onclick: () => { found = []; picked.clear(); render(); } }),
        h('button', { class: 'primary', disabled: busy || !picked.size || !signedIn || st.running,
          text: tn('wsdl.download', { n: picked.size }) + (total ? ' · ' + bytes(total) : ''),
          onclick: (e) => withBusy(e.currentTarget, () => watch(api.post('/api/workshop/dl/download', {
            items: found.filter(f => picked.has(f.id)).map(f => ({ id: f.id, title: f.title, size: f.size })),
            serverSide: serverSide.checked,
          }))) }),
      ]));
      if (!signedIn) s3.push(h('small', { class: 'hint', i18n: 'wsdl.needLogin' }));
      if (st.running) s3.push(h('small', { class: 'hint warn-text', i18n: 'wsdl.stopFirst' }));
    }
    // The download in progress (or just finished).
    if (job && (job.kind === 'download' || job.kind === 'sync')) {
      if (job.kind === 'sync') s3.push(h('div', { class: 'wsdl-progress' + (busy ? '' : ' done'), i18n: busy ? 'wsdl.sync.running' : 'wsdl.sync.done' }));
      const rows = h('div', { class: 'wsdl-job' });
      // The list is a report of the last download: rows go once read, and a
      // mod deleted from the server since can be installed again.
      const clear = (id) => withBusyAll(async () => {
        try { await api.post('/api/workshop/dl/clear', id ? { id } : {}); await load(); } catch (err) { handleErr(err); }
      });
      const again = (it) => withBusyAll(() => watch(api.post('/api/workshop/dl/download', {
        items: [{ id: it.id, title: it.title, size: it.size }], serverSide: !!card._serverSide })));
      for (const it of job.items || []) {
        const pct = it.size ? Math.min(100, Math.round((it.bytes || 0) * 100 / it.size)) : (it.state === 'done' ? 100 : 0);
        const state = it.gone ? t('wsdl.state.gone').replace('{mod}', it.mod)
          : t('wsdl.state.' + it.state) + (it.mod ? ' → ' + it.mod : '') + (it.error ? ' · ' + it.error : '');
        rows.append(h('div', { class: 'wsdl-job-row ' + it.state + (it.gone ? ' gone' : '') }, [
          h('span', { class: 'wsdl-job-name', text: it.title || it.id }),
          h('span', { class: 'wsdl-job-state', text: state }),
          it.gone ? h('button', { class: 'secondary small', i18n: 'wsdl.again', disabled: busy || !signedIn || st.running, onclick: () => again(it) })
            : h('div', { class: 'progress' }, h('div', { class: 'progress-bar', style: { width: (it.state === 'done' ? 100 : pct) + '%' } })),
          h('span', { class: 'hint mono', text: fmtB(it.bytes) + (it.size ? ' / ' + fmtB(it.size) : '') }),
          busy ? h('span') : h('button', { class: 'icon-x', text: '×', title: t('wsdl.list.remove'), 'aria-label': t('wsdl.list.remove') + ': ' + (it.title || it.id), onclick: () => clear(it.id) }),
        ]));
      }
      if (!busy && (job.items || []).length) {
        rows.append(h('div', { class: 'wsdl-job-foot' }, [h('span', { class: 'grow' }),
          h('button', { class: 'linkish', i18n: 'wsdl.list.clearAll', onclick: () => clear('') })]));
      }
      if (job.error === 'login') rows.append(h('div', { class: 'warning-bar err-bar', i18n: 'wsdl.loginLost' }));
      if (busy && (job.log || []).length) rows.append(h('pre', { class: 'wsdl-log', text: job.log.join('\n') }));
      s3.push(rows);
    }
    body.append(step(3, 'wsdl.step.mods', 'todo', s3));
    applyI18n();
  }

  // "Install & update mods" of the Steam Workshop source.
  card._sync = () => watch(api.post('/api/workshop/dl/sync', {}));
  load();
  return card;
}
