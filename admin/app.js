/* Fibr admin panel logic. */
(function () {
  'use strict';

  var DICT = {
    ru: {
      loginHint: 'Введите админ-токен из /opt/fibr/install/.env (FIBR_ADMIN_TOKEN)',
      loginBtn: 'Войти', logout: 'Выйти',
      tabDash: 'Дашборд', tabUsers: 'Пользователи', tabDevices: 'Устройства',
      stOnline: 'Онлайн', stUsers: 'Пользователи', stDevices: 'Устройства',
      stTraffic: 'Трафик', stTunnel: 'Туннель сейчас',
      serverCard: 'Сервер', recentCard: 'Активные устройства',
      fPublicKey: 'Публичный ключ', fPort: 'Порт', fHost: 'Endpoint', fSubnet: 'Подсеть',
      fDNS: 'DNS', fMTU: 'MTU', fPassive: 'Режим', passiveOn: 'активный', passiveOff: 'только конфиги (нет kernel WG)',
      thUser: 'Пользователь', thTelegram: 'Telegram', thRole: 'Роль', thDevices: 'Устр-ва',
      thStatus: 'Статус', thTraffic: 'Трафик', thActions: 'Действия',
      thDevice: 'Устройство', thIP: 'IP', thLastSeen: 'Был(а)', thEnabled: 'Вкл',
      phTelegram: 'Telegram ID (необяз.)', phUsername: 'Имя пользователя',
      roleUser: 'пользователь', roleAdmin: 'админ',
      addUser: 'Добавить', statusActive: 'активен', statusDisabled: 'отключён',
      online: 'онлайн', offline: 'офлайн', never: 'никогда',
      btnQR: 'QR', btnConfig: 'Конфиг', btnDelete: 'Удалить', btnDisable: 'Откл', btnEnable: 'Вкл',
      btnMakeAdmin: 'Сделать админом', btnMakeUser: 'Сделать пользователем', btnAddDevice: '+ устройство',
      confirmDeleteUser: 'Удалить пользователя и все его устройства?',
      confirmDeleteDevice: 'Удалить устройство?',
      errAuth: 'Неверный токен', errGeneric: 'Ошибка запроса',
      userCreated: 'Пользователь создан', userDeleted: 'Пользователь удалён',
      deviceDeleted: 'Устройство удалено', updated: 'Обновлено',
      deviceLimit: 'Достигнут лимит устройств',
      copy: 'Копировать', copied: 'Скопировано', download: 'Скачать .conf',
      qrTitle: 'QR-код конфигурации', qrHint: 'Отсканируйте в приложении WireGuard или в клиенте Fibr',
      configTitle: 'Конфигурация устройства', noTG: '—'
    },
    en: {
      loginHint: 'Enter the admin token from /opt/fibr/install/.env (FIBR_ADMIN_TOKEN)',
      loginBtn: 'Sign in', logout: 'Sign out',
      tabDash: 'Dashboard', tabUsers: 'Users', tabDevices: 'Devices',
      stOnline: 'Online', stUsers: 'Users', stDevices: 'Devices',
      stTraffic: 'Traffic', stTunnel: 'Tunnel now',
      serverCard: 'Server', recentCard: 'Active devices',
      fPublicKey: 'Public key', fPort: 'Port', fHost: 'Endpoint', fSubnet: 'Subnet',
      fDNS: 'DNS', fMTU: 'MTU', fPassive: 'Mode', passiveOn: 'active', passiveOff: 'configs only (no kernel WG)',
      thUser: 'User', thTelegram: 'Telegram', thRole: 'Role', thDevices: 'Devices',
      thStatus: 'Status', thTraffic: 'Traffic', thActions: 'Actions',
      thDevice: 'Device', thIP: 'IP', thLastSeen: 'Last seen', thEnabled: 'On',
      phTelegram: 'Telegram ID (optional)', phUsername: 'Username',
      roleUser: 'user', roleAdmin: 'admin',
      addUser: 'Add', statusActive: 'active', statusDisabled: 'disabled',
      online: 'online', offline: 'offline', never: 'never',
      btnQR: 'QR', btnConfig: 'Config', btnDelete: 'Delete', btnDisable: 'Disable', btnEnable: 'Enable',
      btnMakeAdmin: 'Make admin', btnMakeUser: 'Make user', btnAddDevice: '+ device',
      confirmDeleteUser: 'Delete this user and all their devices?',
      confirmDeleteDevice: 'Delete this device?',
      errAuth: 'Invalid token', errGeneric: 'Request failed',
      userCreated: 'User created', userDeleted: 'User deleted',
      deviceDeleted: 'Device deleted', updated: 'Updated',
      deviceLimit: 'Device limit reached',
      copy: 'Copy', copied: 'Copied', download: 'Download .conf',
      qrTitle: 'Configuration QR code', qrHint: 'Scan it in the WireGuard app or the Fibr client',
      configTitle: 'Device configuration', noTG: '—'
    }
  };

  var t = window.Fibr.makeT(DICT);
  var esc = window.Fibr.esc;
  var fmtBytes = window.Fibr.fmtBytes;
  var fmtBits = window.Fibr.fmtBits;
  var fmtAgo = window.Fibr.fmtAgo;

  var TOKEN_KEY = 'fibr-admin-token';
  var token = null;
  try { token = localStorage.getItem(TOKEN_KEY); } catch (e) { /* ignore */ }
  var api = window.Fibr.makeApi(function () { return token; });

  var el = function (id) { return document.getElementById(id); };
  var refreshTimer = null;

  /* ---------- i18n ---------- */

  function applyI18n() {
    document.querySelectorAll('[data-i18n]').forEach(function (node) {
      node.textContent = t(node.getAttribute('data-i18n'));
    });
    document.querySelectorAll('[data-i18n-ph]').forEach(function (node) {
      node.placeholder = t(node.getAttribute('data-i18n-ph'));
    });
    var sel = el('lang');
    sel.value = t.lang;
  }

  el('lang').addEventListener('change', function () {
    try { localStorage.setItem('fibr-lang', el('lang').value); } catch (e) { /* ignore */ }
    location.reload();
  });

  /* ---------- toast & modal ---------- */

  var toastTimer = null;
  function toast(msg, isErr) {
    document.querySelectorAll('.toast').forEach(function (n) { n.remove(); });
    var div = document.createElement('div');
    div.className = 'toast' + (isErr ? ' pill--danger' : '');
    div.textContent = msg;
    document.body.appendChild(div);
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { div.remove(); }, 2600);
  }

  function openModal(html) {
    el('modal-body').innerHTML = html;
    el('modal').classList.remove('hidden');
  }

  function closeModal() {
    el('modal').classList.add('hidden');
    el('modal-body').innerHTML = '';
  }

  el('modal').addEventListener('click', function (e) {
    if (e.target === el('modal')) closeModal();
  });

  function fail(e) {
    if (e && e.status === 401) { logout(); toast(t('errAuth'), true); return; }
    toast((e && e.message) || t('errGeneric'), true);
  }

  /* ---------- auth ---------- */

  function logout() {
    token = null;
    try { localStorage.removeItem(TOKEN_KEY); } catch (e) { /* ignore */ }
    el('login').classList.remove('hidden');
    stopAutoRefresh();
  }

  el('login-form').addEventListener('submit', function (e) {
    e.preventDefault();
    token = el('login-token').value.trim();
    api('/api/admin/overview').then(function () {
      try { localStorage.setItem(TOKEN_KEY, token); } catch (er) { /* ignore */ }
      el('login').classList.add('hidden');
      el('login-err').textContent = '';
      boot();
    }).catch(function (er) {
      token = null;
      el('login-err').textContent = er.status === 403 ? t('errAuth') : er.message;
    });
  });

  el('logout').addEventListener('click', logout);

  /* ---------- tabs ---------- */

  document.querySelectorAll('.tab').forEach(function (tab) {
    tab.addEventListener('click', function () {
      document.querySelectorAll('.tab').forEach(function (x) { x.classList.remove('active'); });
      tab.classList.add('active');
      var name = tab.getAttribute('data-tab');
      document.querySelectorAll('.page').forEach(function (p) { p.classList.add('hidden'); });
      el('tab-' + name).classList.remove('hidden');
      if (name === 'dashboard') startAutoRefresh(); else stopAutoRefresh();
      if (name === 'users') loadUsers();
      if (name === 'devices') loadDevices();
    });
  });

  function startAutoRefresh() {
    stopAutoRefresh();
    loadOverview();
    refreshTimer = setInterval(loadOverview, 15000);
  }

  function stopAutoRefresh() {
    if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null; }
  }

  /* ---------- dashboard ---------- */

  function loadOverview() {
    api('/api/admin/overview').then(function (ov) {
      el('st-online').textContent = ov.totals.online;
      el('st-users').textContent = ov.totals.users;
      el('st-devices').textContent = ov.totals.devices;
      el('st-traffic').innerHTML = '↓ ' + fmtBytes(ov.totals.total_rx) + ' · ↑ ' + fmtBytes(ov.totals.total_tx);
      el('st-tunnel').innerHTML = '↓ ' + fmtBits(ov.throughput.wg_in_bps) + ' · ↑ ' + fmtBits(ov.throughput.wg_out_bps);
      el('server-host').textContent = ov.server.host + ':' + ov.server.port;

      var kv = [
        [t('fPublicKey'), '<span class="mono">' + esc(ov.server.public_key) + '</span>'],
        [t('fPort'), ov.server.port],
        [t('fHost'), esc(ov.server.host)],
        [t('fSubnet'), '<span class="mono">' + esc(ov.server.subnet) + '</span>'],
        [t('fDNS'), esc((ov.server.dns || []).join(', '))],
        [t('fMTU'), ov.server.mtu],
        [t('fPassive'), ov.server.passive ? t('passiveOff') : t('passiveOn')]
      ];
      el('server-info').innerHTML = kv.map(function (p) {
        return '<dt>' + p[0] + '</dt><dd>' + p[1] + '</dd>';
      }).join('');

      var rows = (ov.recent_devices || []).map(function (d) {
        return '<tr><td>' + deviceStatus(d) + '</td>' +
          '<td class="device-cell">' + esc(d.name) + '<small>' + esc(d.username || '#' + d.user_id) + '</small></td>' +
          '<td class="mono">' + esc(d.ipv4) + '</td>' +
          '<td>' + (d.last_handshake ? (fmtAgo(d.last_handshake, t.lang) || '') : t('never')) + '</td>' +
          '<td>↓ ' + fmtBytes(d.total_rx) + '<br>↑ ' + fmtBytes(d.total_tx) + '</td></tr>';
      }).join('');
      el('recent-table').innerHTML = rows || '<tr><td class="muted">—</td></tr>';
    }).catch(fail);
  }

  function deviceStatus(d) {
    var dot = d.online ? 'dot--on' : 'dot--off';
    var label = d.enabled ? (d.online ? t('online') : t('offline')) : t('statusDisabled');
    var pill = d.enabled ? (d.online ? 'pill--ok' : 'pill--off') : 'pill--danger';
    return '<span class="pill ' + pill + '"><span class="dot ' + dot + '"></span>' + label + '</span>';
  }

  /* ---------- users ---------- */

  function loadUsers() {
    Promise.all([api('/api/admin/users'), api('/api/admin/devices')]).then(function (res) {
      var users = res[0];
      var byUser = {};
      (res[1] || []).forEach(function (d) {
        byUser[d.user_id] = byUser[d.user_id] || [];
        byUser[d.user_id].push(d);
      });

      el('users-table').innerHTML =
        '<thead><tr><th>ID</th><th>' + t('thUser') + '</th><th>' + t('thTelegram') + '</th>' +
        '<th>' + t('thRole') + '</th><th>' + t('thDevices') + '</th><th>' + t('thTraffic') + '</th>' +
        '<th>' + t('thStatus') + '</th><th></th></tr></thead><tbody>' +
        users.map(function (u) {
          var devs = byUser[u.id] || [];
          var rx = 0, tx = 0, online = 0;
          devs.forEach(function (d) { rx += d.total_rx; tx += d.total_tx; if (d.online) online++; });
          var roleBtn = u.role === 'admin'
            ? '<button class="btn btn--ghost btn--small" data-act="role" data-id="' + u.id + '" data-role="user">' + t('btnMakeUser') + '</button>'
            : '<button class="btn btn--ghost btn--small" data-act="role" data-id="' + u.id + '" data-role="admin">' + t('btnMakeAdmin') + '</button>';
          var disBtn = u.disabled
            ? '<button class="btn btn--ghost btn--small" data-act="undisable" data-id="' + u.id + '">' + t('btnEnable') + '</button>'
            : '<button class="btn btn--ghost btn--small" data-act="disable" data-id="' + u.id + '">' + t('btnDisable') + '</button>';
          return '<tr>' +
            '<td class="row-id mono">#' + u.id + '</td>' +
            '<td class="user-cell">' + esc(u.username || '—') + '</td>' +
            '<td class="mono">' + (u.telegram_id || t('noTG')) + '</td>' +
            '<td>' + (u.role === 'admin' ? '<span class="pill pill--live">admin</span>' : '<span class="pill pill--off">user</span>') + '</td>' +
            '<td>' + devs.length + (online ? ' <span class="pill pill--ok">' + online + ' ' + t('online') + '</span>' : '') + '</td>' +
            '<td>↓ ' + fmtBytes(rx) + '<br>↑ ' + fmtBytes(tx) + '</td>' +
            '<td>' + (u.disabled ? '<span class="pill pill--danger">' + t('statusDisabled') + '</span>' : '<span class="pill pill--ok">' + t('statusActive') + '</span>') + '</td>' +
            '<td><div class="actions">' + roleBtn + disBtn +
            '<button class="btn btn--tertiary btn--small" data-act="adddev" data-id="' + u.id + '">' + t('btnAddDevice') + '</button>' +
            '<button class="btn btn--danger btn--small" data-act="deluser" data-id="' + u.id + '">' + t('btnDelete') + '</button>' +
            '</div></td></tr>';
        }).join('') + '</tbody>';
    }).catch(fail);
  }

  el('nu-create').addEventListener('click', function () {
    var body = { username: el('nu-username').value.trim(), role: el('nu-role').value };
    var tg = el('nu-telegram').value.trim();
    if (tg) body.telegram_id = parseInt(tg, 10);
    api('/api/admin/users', { method: 'POST', body: body }).then(function () {
      toast(t('userCreated'));
      el('nu-username').value = '';
      el('nu-telegram').value = '';
      loadUsers();
    }).catch(fail);
  });

  el('users-table').addEventListener('click', function (e) {
    var btn = e.target.closest('button[data-act]');
    if (!btn) return;
    var id = btn.getAttribute('data-id');
    var act = btn.getAttribute('data-act');

    if (act === 'role') {
      api('/api/admin/users/' + id, { method: 'PATCH', body: { role: btn.getAttribute('data-role') } })
        .then(function () { toast(t('updated')); loadUsers(); }).catch(fail);
    } else if (act === 'disable' || act === 'undisable') {
      api('/api/admin/users/' + id, { method: 'PATCH', body: { disabled: act === 'disable' } })
        .then(function () { toast(t('updated')); loadUsers(); }).catch(fail);
    } else if (act === 'deluser') {
      if (!confirm(t('confirmDeleteUser'))) return;
      api('/api/admin/users/' + id, { method: 'DELETE' })
        .then(function () { toast(t('userDeleted')); loadUsers(); }).catch(fail);
    } else if (act === 'adddev') {
      var name = prompt(t('thDevice') + ':', 'device');
      if (name === null) return;
      api('/api/admin/users/' + id + '/devices', { method: 'POST', body: { name: name } })
        .then(function (d) { toast(t('updated')); loadUsers(); showQR(d.id); })
        .catch(fail);
    }
  });

  /* ---------- devices ---------- */

  function loadDevices() {
    api('/api/admin/devices').then(function (devices) {
      el('devices-table').innerHTML =
        '<thead><tr><th>ID</th><th>' + t('thDevice') + '</th><th>' + t('thUser') + '</th>' +
        '<th>' + t('thIP') + '</th><th>' + t('thStatus') + '</th><th>' + t('thLastSeen') + '</th>' +
        '<th>' + t('thTraffic') + '</th><th></th></tr></thead><tbody>' +
        (devices || []).map(function (d) {
          return '<tr>' +
            '<td class="row-id mono">#' + d.id + '</td>' +
            '<td class="device-cell">' + esc(d.name) + '</td>' +
            '<td>' + esc(d.username || '#' + d.user_id) + '</td>' +
            '<td class="mono">' + esc(d.ipv4) + '</td>' +
            '<td>' + deviceStatus(d) + '</td>' +
            '<td>' + (d.last_handshake ? (fmtAgo(d.last_handshake, t.lang) || '') : t('never')) + '</td>' +
            '<td>↓ ' + fmtBytes(d.total_rx) + '<br>↑ ' + fmtBytes(d.total_tx) + '</td>' +
            '<td><div class="actions">' +
            '<button class="btn btn--secondary btn--small" data-act="qr" data-id="' + d.id + '">' + t('btnQR') + '</button>' +
            '<button class="btn btn--tertiary btn--small" data-act="cfg" data-id="' + d.id + '">' + t('btnConfig') + '</button>' +
            (d.enabled
              ? '<button class="btn btn--ghost btn--small" data-act="off" data-id="' + d.id + '">' + t('btnDisable') + '</button>'
              : '<button class="btn btn--ghost btn--small" data-act="on" data-id="' + d.id + '">' + t('btnEnable') + '</button>') +
            '<button class="btn btn--danger btn--small" data-act="del" data-id="' + d.id + '">' + t('btnDelete') + '</button>' +
            '</div></td></tr>';
        }).join('') + '</tbody>';
    }).catch(fail);
  }

  el('devices-table').addEventListener('click', function (e) {
    var btn = e.target.closest('button[data-act]');
    if (!btn) return;
    var id = btn.getAttribute('data-id');
    var act = btn.getAttribute('data-act');
    if (act === 'qr') { showQR(id); return; }
    if (act === 'cfg') { showConfig(id); return; }
    if (act === 'on' || act === 'off') {
      api('/api/admin/devices/' + id, { method: 'PATCH', body: { enabled: act === 'on' } })
        .then(function () { loadDevices(); }).catch(fail);
    } else if (act === 'del') {
      if (!confirm(t('confirmDeleteDevice'))) return;
      api('/api/admin/devices/' + id, { method: 'DELETE' })
        .then(function () { toast(t('deviceDeleted')); loadDevices(); }).catch(fail);
    }
  });

  /* ---------- qr & config ---------- */

  function authFetch(path) {
    return fetch(path, { headers: { Authorization: 'Bearer ' + token } });
  }

  function showQR(id) {
    authFetch('/api/admin/devices/' + id + '/qr.png').then(function (res) {
      if (!res.ok) throw new Error('HTTP ' + res.status);
      return res.blob();
    }).then(function (blob) {
      var url = URL.createObjectURL(blob);
      openModal('<h3>' + esc(t('qrTitle')) + '</h3>' +
        '<p class="muted modal-sub">' + esc(t('qrHint')) + '</p>' +
        '<div class="qr-wrap"><img src="' + url + '" alt="QR"></div>' +
        '<div class="modal-actions"><button class="btn btn--tertiary btn--small" onclick="document.getElementById(\'modal\').classList.add(\'hidden\')">OK</button></div>');
    }).catch(fail);
  }

  function showConfig(id) {
    authFetch('/api/admin/devices/' + id + '/config').then(function (res) {
      if (!res.ok) throw new Error('HTTP ' + res.status);
      return res.text();
    }).then(function (text) {
      openModal('<h3>' + esc(t('configTitle')) + '</h3>' +
        '<pre class="config-pre mono">' + esc(text) + '</pre>' +
        '<div class="modal-actions">' +
        '<button class="btn btn--tertiary btn--small" id="copy-cfg">' + esc(t('copy')) + '</button>' +
        '<button class="btn btn--secondary btn--small" id="dl-cfg">' + esc(t('download')) + '</button></div>');
      el('copy-cfg').addEventListener('click', function () {
        navigator.clipboard.writeText(text).then(function () { toast(t('copied')); });
      });
      el('dl-cfg').addEventListener('click', function () {
        var a = document.createElement('a');
        a.href = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
        a.download = 'fibr-device-' + id + '.conf';
        a.click();
      });
    }).catch(fail);
  }

  /* ---------- boot ---------- */

  function boot() {
    startAutoRefresh();
  }

  applyI18n();
  if (token) {
    api('/api/admin/overview').then(function () {
      el('login').classList.add('hidden');
      boot();
    }).catch(function () { logout(); });
  }
})();
