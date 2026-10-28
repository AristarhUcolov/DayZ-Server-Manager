/*
  DayZ Server Manager — panel login and remote access (Settings).
  Copyright (c) 2026 Aristarh Ucolov. All rights reserved.

  Loaded after app.js / features.js and sharing their globals.
*/
'use strict';

// Short device label from a User-Agent ("Chrome · Windows").
function deviceLabel(ua) {
  ua = String(ua || '');
  const browser = /Edg\//.test(ua) ? 'Edge' : /OPR\//.test(ua) ? 'Opera' : /YaBrowser/.test(ua) ? 'Yandex'
    : /Firefox\//.test(ua) ? 'Firefox' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : '';
  const os = /Android/.test(ua) ? 'Android' : /iPhone|iPad|iOS/.test(ua) ? 'iOS' : /Windows/.test(ua) ? 'Windows'
    : /Mac OS X|Macintosh/.test(ua) ? 'macOS' : /Linux/.test(ua) ? 'Linux' : '';
  return [browser, os].filter(Boolean).join(' · ') || (ua ? ua.slice(0, 40) : '—');
}

function pwInput(labelKey, autocomplete) {
  return h('input', { type: 'password', autocomplete: autocomplete || 'new-password', 'aria-label': t(labelKey) });
}

function labeled(key, el) {
  const id = assocId(el);
  return h('div', {}, [h('label', id ? { i18n: key, for: id } : { i18n: key }), el]);
}

// checkNewPassword returns the i18n key of the first problem, or ''.
function checkNewPassword(user, p1, p2) {
  if (!String(user || '').trim()) return 'auth.err.user';
  if (String(p1).length < 8) return 'auth.err.short';
  if (p1 !== p2) return 'auth.err.mismatch';
  return '';
}

async function refreshAuthInfo() {
  try { State.info = await api.get('/api/info'); } catch {}
  setupLogoutButton();
}

// ---------------------------------------------------------------- login card

function authCard() {
  const card = h('div', { class: 'card', id: 'auth-card' }, [
    cardTitle('i-shield', 'auth.card.title'),
    h('p', { class: 'hint', i18n: 'auth.card.hint' }),
  ]);
  // These controls post their own endpoints — keep them out of the Settings
  // auto-save (which listens for "change" on the whole page).
  card.addEventListener('change', (e) => e.stopPropagation());
  const body = h('div');
  card.append(body);

  async function render() {
    let d;
    try { d = await api.get('/api/auth/settings'); }
    catch (e) { body.textContent = e.message; return; }
    body.innerHTML = '';

    if (!d.enabled) {
      const user = h('input', { type: 'text', value: 'admin', autocomplete: 'username', autocapitalize: 'none', spellcheck: 'false' });
      const p1 = pwInput('auth.password'), p2 = pwInput('auth.password2');
      body.append(
        h('p', { class: 'warning-bar', i18n: 'auth.card.off' }),
        h('div', { class: 'grid-3' }, [labeled('auth.user', user), labeled('auth.password', p1), labeled('auth.password2', p2)]),
        h('div', { class: 'actions' }, [
          h('button', { class: 'primary', i18n: 'auth.enable', onclick: (e) => withBusy(e.currentTarget, async () => {
            const bad = checkNewPassword(user.value, p1.value, p2.value);
            if (bad) { toast(t(bad), 'error'); return; }
            try {
              await api.post('/api/auth/setup', { user: user.value.trim(), password: p1.value });
              try { localStorage.setItem('loginUser', user.value.trim()); } catch {}
              await refreshAuthInfo();
              toast(t('auth.enabled.msg'), 'ok');
              render();
            } catch (err) { handleErr(err); }
          }) }),
        ]),
        h('small', { class: 'hint', i18n: 'auth.fr.hint' }),
      );
      applyI18n();
      return;
    }

    // ---- login is on
    body.append(h('p', { class: 'ok-bar', text: t('auth.card.on').replace('{user}', d.user) }));

    const trust = h('input', { type: 'checkbox', class: 'switch' });
    trust.checked = !!d.trustLocalApp;
    trust.onchange = async () => {
      try { await api.post('/api/auth/trust', { trustLocalApp: trust.checked }); toast(t('msg.saved'), 'ok'); }
      catch (err) { trust.checked = !trust.checked; handleErr(err); }
    };
    body.append(
      h('label', {}, [trust, h('span', { i18n: 'auth.trust' })]),
      h('small', { class: 'hint', i18n: 'auth.trust.hint' }),
    );

    // Change username / password.
    const cur = pwInput('auth.current', 'current-password');
    const nu = h('input', { type: 'text', value: d.user, autocomplete: 'username', autocapitalize: 'none', spellcheck: 'false' });
    const np1 = pwInput('auth.newPassword'), np2 = pwInput('auth.password2');
    body.append(
      h('h4', { i18n: 'auth.change.title', style: { marginTop: '16px' } }),
      h('div', { class: 'grid-2' }, [labeled('auth.current', cur), labeled('auth.newUser', nu), labeled('auth.newPassword', np1), labeled('auth.password2', np2)]),
      h('div', { class: 'actions' }, [
        h('button', { class: 'primary', i18n: 'auth.change.button', onclick: (e) => withBusy(e.currentTarget, async () => {
          if (!cur.value) { toast(t('auth.current'), 'error'); cur.focus(); return; }
          if (np1.value || np2.value) {
            const bad = checkNewPassword(nu.value, np1.value, np2.value);
            if (bad) { toast(t(bad), 'error'); return; }
          } else if (!nu.value.trim()) { toast(t('auth.err.user'), 'error'); return; }
          try {
            await api.post('/api/auth/change', { current: cur.value, user: nu.value.trim(), password: np1.value });
            try { localStorage.setItem('loginUser', nu.value.trim()); } catch {}
            toast(t('auth.changed'), 'ok');
            render();
          } catch (err) { handleErr(err); cur.value = ''; }
        }) }),
      ]),
    );

    // Signed-in devices.
    const sess = d.sessions || [];
    const list = h('div', { class: 'session-list' });
    for (const s of sess) {
      const ago = fmtAge(Date.now() / 1000 - s.lastSeen + ((State.clockSkew || 0) / 1000));
      list.append(h('div', { class: 'session-row' + (s.current ? ' current' : '') }, [
        h('div', { class: 'session-main' }, [
          h('b', { text: deviceLabel(s.agent) }),
          s.current ? h('span', { class: 'badge ok', i18n: 'auth.sessions.current' }) : null,
          h('div', { class: 'hint mono', text: (s.ip || '') + ' · ' + t('auth.sessions.lastSeen').replace('{when}', ago) }),
        ]),
        h('button', { class: s.current ? '' : 'danger', i18n: 'auth.sessions.revoke', onclick: (e) => withBusy(e.currentTarget, async () => {
          try {
            await api.post('/api/auth/revoke', { id: s.id });
            if (s.current) { location.reload(); return; }
            render();
          } catch (err) { handleErr(err); }
        }) }),
      ]));
    }
    body.append(h('h4', { i18n: 'auth.sessions.title', style: { marginTop: '16px' } }), list);
    if (sess.filter(s => !s.current).length) {
      body.append(h('div', { class: 'actions' }, [
        h('button', { i18n: 'auth.sessions.revokeOthers', onclick: (e) => withBusy(e.currentTarget, async () => {
          try { await api.post('/api/auth/revoke', { others: true }); render(); }
          catch (err) { handleErr(err); }
        }) }),
      ]));
    } else {
      body.append(h('p', { class: 'hint', i18n: 'auth.sessions.none' }));
    }

    // Turn the login off.
    const dc = pwInput('auth.current', 'current-password');
    body.append(
      h('h4', { i18n: 'auth.disable.title', style: { marginTop: '16px' } }),
      h('div', { class: 'row', style: { gap: '8px', alignItems: 'flex-end', flexWrap: 'wrap' } }, [
        labeled('auth.current', dc),
        h('button', { class: 'danger', i18n: 'auth.disable.button', onclick: (e) => withBusy(e.currentTarget, async () => {
          if (!dc.value) { toast(t('auth.current'), 'error'); dc.focus(); return; }
          if (!(await confirmModal(t('auth.disable.confirm'), { danger: true, okText: t('auth.disable.button') }))) return;
          try {
            await api.post('/api/auth/disable', { current: dc.value });
            await refreshAuthInfo();
            toast(t('auth.disabled'), 'ok');
            render();
          } catch (err) { handleErr(err); dc.value = ''; }
        }) }),
      ]),
      h('small', { class: 'hint', i18n: 'auth.login.forgot' }),
    );
    applyI18n();
  }
  render();
  return card;
}

// ---------------------------------------------------------- remote access card

function remoteAccessCard(c, F) {
  F.panelHTTPS = h('input', { type: 'checkbox', class: 'switch' });
  F.panelHTTPS.checked = !!c.panelHTTPS;
  const card = h('div', { class: 'card', id: 'remote-card' }, [
    cardTitle('i-livemap', 'remote.title'),
    h('p', { class: 'hint', i18n: 'remote.hint' }),
  ]);
  const body = h('div', { class: 'remote-body' });
  card.append(body);
  let checkResult = null;

  const mark = (state) => h('span', { class: 'rs-mark ' + state, 'aria-hidden': 'true', text: state === 'ok' ? '✓' : state === 'warn' ? '!' : '✗' });
  const stepRow = (state, labelKey, extra) => h('div', { class: 'remote-step ' + state }, [
    mark(state),
    h('div', { class: 'remote-step-main' }, [h('div', { class: 'remote-step-title', i18n: labelKey }), ...(extra || [])]),
  ]);
  const portList = (ports) => h('div', { class: 'port-list' }, (ports || []).map(p =>
    h('span', { class: 'port-chip ' + (p.ok ? 'ok' : 'bad'), title: p.error || '', text: `${p.proto} ${p.port} ${p.ok ? '✓' : '✗'}` })));
  const scrollTo = (id) => { const el = document.getElementById(id); if (el) { el.scrollIntoView({ behavior: 'smooth', block: 'center' }); el.classList.add('flash-target'); setTimeout(() => el.classList.remove('flash-target'), 1600); } };

  async function load() {
    try { render(await api.get('/api/remote/status')); }
    catch (e) { body.textContent = e.message; }
  }

  function upnpSwitch(checked, key, field) {
    const sw = h('input', { type: 'checkbox', class: 'switch' });
    sw.checked = !!checked;
    sw.addEventListener('change', async (e) => {
      e.stopPropagation(); // own endpoint, not the Settings auto-save
      sw.disabled = true;
      try { render(await api.post('/api/remote/upnp', { [field]: sw.checked })); }
      catch (err) { sw.checked = !sw.checked; handleErr(err); }
      finally { sw.disabled = false; }
    });
    return h('label', {}, [sw, h('span', { i18n: key })]);
  }

  function render(d) {
    body.innerHTML = '';
    // The last check survives restarts: the address for the phone is there
    // at once, without clicking "Check" again after every reboot.
    if (!checkResult && d.lastCheck) {
      checkResult = Object.assign({ remembered: true, cgnatUnknown: d.lastCheck.cgnat == null }, d.lastCheck);
    }
    const up = d.upnp || {};
    const port = d.panelPort;
    const panelPorts = (up.ports || []).filter(p => p.purpose === 'panel');
    const gamePorts = (up.ports || []).filter(p => p.purpose !== 'panel');

    // Inside the home network.
    body.append(h('h4', { i18n: 'remote.lan.title' }));
    if (d.exposure === 'local') {
      body.append(h('p', { class: 'warning-bar', i18n: 'settings.network.localOnly' }));
    } else {
      body.append(h('ul', { class: 'url-list' }, (d.lanUrls || []).map(u =>
        h('li', {}, h('a', { href: u, target: '_blank', rel: 'noopener', text: u })))));
    }

    // From the internet: each requirement with its state.
    body.append(h('h4', { i18n: 'remote.inet.title', style: { marginTop: '14px' } }));
    const expoState = d.exposure === 'internet' ? 'ok' : 'bad';
    body.append(stepRow(expoState, 'remote.step.exposure', [
      h('div', { class: 'hint', i18n: 'settings.exposure.' + (d.exposure || 'local') }),
      expoState !== 'ok' ? h('button', { type: 'button', class: 'linkish', i18n: 'action.change', onclick: () => scrollTo('settings-exposure') }) : null,
    ]));
    body.append(stepRow(d.authEnabled ? 'ok' : 'bad', 'remote.step.login', [
      !d.authEnabled ? h('button', { type: 'button', class: 'linkish', i18n: 'setup.go', onclick: () => scrollTo('auth-card') }) : null,
    ]));
    body.append(stepRow(d.https ? 'ok' : 'warn', 'remote.step.https', [
      h('label', {}, [F.panelHTTPS, h('span', { i18n: 'remote.https.toggle' })]),
      h('small', { class: 'hint', i18n: 'remote.https.hint' }),
      (!!d.httpsWanted !== !!d.https) ? h('small', { class: 'hint warn-text', i18n: 'remote.restartNote' }) : null,
    ]));
    const fwdOK = panelPorts.length > 0 && panelPorts.every(p => p.ok);
    body.append(stepRow(fwdOK ? 'ok' : (d.upnpPanel ? 'bad' : 'warn'), 'remote.step.forward', [
      upnpSwitch(d.upnpPanel, 'remote.panel.upnp', 'panel'),
      d.upnpPanel ? portList(panelPorts) : null,
      d.upnpPanel && d.exposure !== 'internet' ? h('small', { class: 'hint warn-text', i18n: 'remote.upnp.panelNeedsInternet' }) : null,
      up.error && d.upnpPanel ? h('small', { class: 'hint warn-text', text: t('remote.upnp.noRouter') + ' (' + up.error + ')' }) : null,
      h('small', { class: 'hint', text: t('remote.upnp.hint').replace('{port}', port).replace('{ip}', up.localIp || (d.lanUrls && d.lanUrls[0] ? new URL(d.lanUrls[0]).hostname : '…')) }),
    ]));
    if (d.firewall && d.firewall.supported) {
      // One button adds both rules (panel TCP + game UDP ports).
      const fwOK = !!d.firewall.panel && !!d.firewall.game;
      body.append(stepRow(fwOK ? 'ok' : 'warn', 'remote.step.firewall', [
        !fwOK && d.local ? h('button', { type: 'button', i18n: 'remote.fw.button', onclick: (e) => withBusy(e.currentTarget, async () => {
          try { await api.post('/api/remote/firewall', {}); toast(t('remote.fw.started'), 'ok'); setTimeout(load, 8000); }
          catch (err) { handleErr(err); }
        }) }) : null,
        !fwOK && !d.local ? h('small', { class: 'hint', i18n: 'remote.fw.localOnly' }) : null,
      ]));
    } else {
      body.append(stepRow('warn', 'remote.step.firewall', [h('small', { class: 'hint mono', text: t('remote.fw.linux').replace('{port}', port) })]));
    }

    // Public address / CGNAT check (on request: it asks an external service).
    const checkBox = h('div', { class: 'remote-check' });
    const runCheck = (e) => withBusy(e.currentTarget, async () => {
      try { checkResult = await api.post('/api/remote/check', { public: true }); }
      catch (err) { handleErr(err); return; }
      showCheck();
    });
    function showCheck() {
      checkBox.innerHTML = '';
      const r = checkResult;
      if (!r) return;
      if (r.router || r.externalIp) {
        checkBox.append(h('div', { text: t('remote.check.router').replace('{name}', r.router || '—').replace('{ip}', r.externalIp || '—') }));
      } else {
        checkBox.append(h('div', { class: 'hint', i18n: 'remote.check.noUpnp' }));
      }
      if (r.publicIp) {
        checkBox.append(h('div', { text: t('remote.check.public').replace('{ip}', r.publicIp) }));
      } else if (r.publicError) {
        checkBox.append(h('div', { class: 'hint warn-text', text: t('remote.check.failed').replace('{err}', r.publicError) }));
      }
      const scheme = d.https ? 'https' : 'http';
      const pubHost = r.publicIp && r.publicIp.includes(':') ? `[${r.publicIp}]` : r.publicIp;
      if (r.cgnat === true) {
        checkBox.append(h('p', { class: 'warning-bar', i18n: 'remote.check.cgnat' }));
      } else if (r.publicIp && r.cgnatUnknown) {
        // No WAN address from the router: CGNAT can be neither confirmed nor
        // ruled out, so no "open it from outside" promise.
        checkBox.append(h('p', { class: 'warning-bar', text: t('remote.check.unknown')
          .replace('{ip}', r.publicIp).replaceAll('{port}', port).replace('{url}', `${scheme}://${pubHost}:${port}/`) }));
      } else if (r.publicIp) {
        const url = `${scheme}://${pubHost}:${port}/`;
        checkBox.append(h('p', { class: 'ok-bar' }, [
          h('span', { text: t('remote.check.open').split('{url}')[0] }),
          h('code', { class: 'mono', text: url }),
          h('span', { text: t('remote.check.open').split('{url}')[1] || '' }),
        ]));
      }
      if (r.at) {
        checkBox.append(h('div', { class: 'hint', text: t('remote.check.at').replace('{age}', fmtAge(Date.now() / 1000 - r.at)) }));
      }
      if (r.changed && r.previous) {
        // A router restart usually brings a new public address: say so, or
        // the phone keeps knocking on the old one.
        checkBox.append(h('div', { class: 'hint warn-text', text: t('remote.check.changed')
          .replace('{time}', fmtDateTime(r.changed)).replace('{ip}', r.previous) }));
      }
      applyI18n();
    }
    // Keep checking: the address is re-checked every 15 minutes (one request
    // to a what-is-my-IP service each time), and a change is posted to Discord.
    const watchSw = h('input', { type: 'checkbox', class: 'switch' });
    watchSw.checked = !!d.watch;
    watchSw.addEventListener('change', async (e) => {
      e.stopPropagation(); // own endpoint, not the Settings auto-save
      watchSw.disabled = true;
      try { render(await api.post('/api/remote/watch', { on: watchSw.checked })); }
      catch (err) { watchSw.checked = !watchSw.checked; handleErr(err); }
      finally { watchSw.disabled = false; }
    });
    body.append(stepRow(checkResult ? (checkResult.cgnat ? 'bad' : (checkResult.publicIp && !checkResult.cgnatUnknown ? 'ok' : 'warn')) : 'warn', 'remote.step.public', [
      h('button', { type: 'button', i18n: 'remote.check.button', onclick: runCheck }),
      checkBox,
      h('label', { class: 'remote-watch' }, [watchSw, h('span', { i18n: 'remote.watch' })]),
      h('small', { class: 'hint', i18n: 'remote.watch.hint' }),
    ]));
    showCheck();

    // Tunnels: the way when there is no public IP (or no router access).
    body.append(h('details', { class: 'remote-tunnel' }, [
      h('summary', { i18n: 'remote.tunnel.title' }),
      h('p', { class: 'hint', i18n: 'remote.tunnel.text' }),
    ]));

    // The game server itself.
    const gp = (d.gamePorts || []).map(p => p.port).join(', ');
    body.append(...[
      h('h4', { i18n: 'remote.game.title', style: { marginTop: '16px' } }),
      h('p', { class: 'hint', text: t('remote.game.hint').replace('{ports}', 'UDP ' + gp) }),
      upnpSwitch(d.upnpGame, 'remote.game.upnp', 'game'),
      d.upnpGame ? portList(gamePorts) : null,
    ].filter(Boolean)); // Element.append(null) would print "null"
    applyI18n();
  }

  load();
  return card;
}
