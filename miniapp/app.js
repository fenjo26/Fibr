/* Fibr Telegram Mini App. */
(function () {
  'use strict';

  var DICT = {
    ru: {
      greeting: 'Привет, {name}!',
      lblDevices: 'Устройства', lblOnline: 'Онлайн', lblTraffic: 'Трафик',
      phName: 'Название устройства (телефон, ноутбук…)',
      addDevice: 'Добавить',
      online: 'онлайн', offline: 'офлайн', disabled: 'отключено', never: 'не подключалось',
      lastSeen: 'было(а) {ago}', rx: '↓ {v}', tx: '↑ {v}',
      btnQR: 'QR-код', btnConfig: 'Конфиг', btnDelete: 'Удалить',
      confirmDelete: 'Удалить устройство {name}?',
      errNotTelegram: 'Откройте это окно из бота Fibr в Telegram.',
      errAuth: 'Не удалось войти через Telegram. Сообщите админу.',
      errDisabled: 'Ваш аккаунт отключён администратором.',
      errGeneric: 'Ошибка запроса',
      deviceLimit: 'Лимит устройств исчерпан',
      empty: 'Пока нет устройств.\nДобавьте первое — и отсканируйте QR.',
      qrTitle: 'QR-код конфигурации',
      qrHint: 'Отсканируйте камерой телефона или приложением WireGuard (Импорт туннеля → из QR-кода)',
      configTitle: 'Конфигурация',
      copy: 'Копировать', copied: 'Скопировано', close: 'Закрыть',
      footHint: 'QR также совместим с официальным приложением WireGuard'
    },
    en: {
      greeting: 'Hi, {name}!',
      lblDevices: 'Devices', lblOnline: 'Online', lblTraffic: 'Traffic',
      phName: 'Device name (phone, laptop…)',
      addDevice: 'Add',
      online: 'online', offline: 'offline', disabled: 'disabled', never: 'never connected',
      lastSeen: 'last seen {ago}', rx: '↓ {v}', tx: '↑ {v}',
      btnQR: 'QR code', btnConfig: 'Config', btnDelete: 'Delete',
      confirmDelete: 'Delete device {name}?',
      errNotTelegram: 'Open this window from the Fibr bot in Telegram.',
      errAuth: 'Telegram sign-in failed. Contact the admin.',
      errDisabled: 'Your account is disabled by the administrator.',
      errGeneric: 'Request failed',
      deviceLimit: 'Device limit reached',
      empty: 'No devices yet.\nAdd one and scan the QR code.',
      qrTitle: 'Configuration QR code',
      qrHint: 'Scan with your phone camera or the WireGuard app (Import tunnel → from QR code)',
      configTitle: 'Configuration',
      copy: 'Copy', copied: 'Copied', close: 'Close',
      footHint: 'The QR also works with the official WireGuard app'
    }
  };

  var t = window.Fibr.makeT(DICT);
  var esc = window.Fibr.esc;
  var fmtBytes = window.Fibr.fmtBytes;
  var fmtAgo = window.Fibr.fmtAgo;

  var token = null;
  var me = null; // {user, devices, max_devices}
  var api = window.Fibr.makeApi(function () { return token; });

  var el = function (id) { return document.getElementById(id); };

  /* ---------- telegram ---------- */

  var tg = window.Telegram && window.Telegram.WebApp;
  if (tg) {
    tg.ready();
    tg.expand();
  }

  /* ---------- static i18n ---------- */

  el('lbl-devices').textContent = t('lblDevices');
  el('lbl-online').textContent = t('lblOnline');
  el('lbl-traffic').textContent = t('lblTraffic');
  el('new-name').placeholder = t('phName');
  el('add-device').textContent = t('addDevice');
  el('foot-hint').textContent = t('footHint');

  /* ---------- modal ---------- */

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
    if (e && e.status === 401) { showError(t('errAuth')); return; }
    alert((e && e.message) || t('errGeneric'));
  }

  function showError(msg) {
    el('error-text').textContent = msg;
    el('error-view').classList.remove('hidden');
    el('app-view').classList.add('hidden');
  }

  /* ---------- auth ---------- */

  function signIn() {
    if (!tg || !tg.initData) {
      showError(t('errNotTelegram'));
      return;
    }
    fetch('/api/auth/telegram', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ init_data: tg.initData })
    }).then(function (res) {
      return res.json().then(function (data) {
        if (!res.ok) {
          if (res.status === 403) { showError(t('errDisabled')); throw new Error('disabled'); }
          throw new Error(data && data.error ? data.error : 'auth failed');
        }
        return data;
      });
    }).then(function (data) {
      token = data.token;
      try { sessionStorage.setItem('fibr-token', token); } catch (e) { /* ignore */ }
      load();
    }).catch(function (e) {
      if (e && e.message === 'disabled') return;
      showError(t('errAuth'));
    });
  }

  /* ---------- data ---------- */

  function load() {
    api('/api/me').then(function (data) {
      me = data;
      var name = data.user.username || (tg && tg.initDataUnsafe && tg.initDataUnsafe.user && tg.initDataUnsafe.user.first_name) || '';
      el('greeting').textContent = t('greeting', { name: name });
      el('app-view').classList.remove('hidden');
      el('error-view').classList.add('hidden');
      render();
    }).catch(fail);
  }

  function render() {
    var devices = me.devices || [];
    var online = devices.filter(function (d) { return d.online; }).length;
    var rx = 0, tx = 0;
    devices.forEach(function (d) { rx += d.total_rx; tx += d.total_tx; });

    el('sum-devices').textContent = devices.length + (me.max_devices ? ' / ' + me.max_devices : '');
    el('sum-online').textContent = online;
    el('sum-traffic').innerHTML = fmtBytes(rx) + ' · ' + fmtBytes(tx);

    if (!devices.length) {
      el('devices').innerHTML = '<div class="card empty">' + esc(t('empty')).replace(/\n/g, '<br>') + '</div>';
      return;
    }

    el('devices').innerHTML = devices.map(function (d) {
      var pill = d.enabled ? (d.online ? 'pill--ok' : 'pill--off') : 'pill--danger';
      var label = d.enabled ? (d.online ? t('online') : t('offline')) : t('disabled');
      var ago = d.last_handshake ? fmtAgo(d.last_handshake, t.lang) : null;
      var seen = ago ? t('lastSeen', { ago: ago }) : t('never');
      return '<div class="card device">' +
        '<div class="device-top">' +
        '<span class="dot ' + (d.online ? 'dot--on' : 'dot--off') + '"></span>' +
        '<div class="grow"><h3>' + esc(d.name) + '</h3>' +
        '<span class="ip mono">' + esc(d.ipv4) + '</span></div>' +
        '<span class="pill ' + pill + '">' + label + '</span></div>' +
        '<div class="device-meta"><span>' + esc(seen) + '</span>' +
        '<span>' + t('rx', { v: fmtBytes(d.total_rx) }) + '</span>' +
        '<span>' + t('tx', { v: fmtBytes(d.total_tx) }) + '</span></div>' +
        '<div class="device-actions">' +
        '<button class="btn btn--secondary btn--small" data-act="qr" data-id="' + d.id + '">' + t('btnQR') + '</button>' +
        '<button class="btn btn--tertiary btn--small" data-act="cfg" data-id="' + d.id + '">' + t('btnConfig') + '</button>' +
        '<button class="btn btn--ghost btn--small" data-act="del" data-id="' + d.id + '" data-name="' + esc(d.name) + '">' + t('btnDelete') + '</button>' +
        '</div></div></div>';
    }).join('');
  }

  el('add-device').addEventListener('click', function () {
    var name = el('new-name').value.trim();
    api('/api/me/devices', { method: 'POST', body: { name: name } }).then(function (d) {
      el('new-name').value = '';
      load();
      showQR(d.id);
    }).catch(function (e) {
      if (e.status === 409) { alert(t('deviceLimit')); return; }
      fail(e);
    });
  });

  el('devices').addEventListener('click', function (e) {
    var btn = e.target.closest('button[data-act]');
    if (!btn) return;
    var id = btn.getAttribute('data-id');
    var act = btn.getAttribute('data-act');
    if (act === 'qr') showQR(id);
    else if (act === 'cfg') showConfig(id);
    else if (act === 'del') {
      if (!confirm(t('confirmDelete', { name: btn.getAttribute('data-name') }))) return;
      api('/api/me/devices/' + id, { method: 'DELETE' }).then(load).catch(fail);
    }
  });

  /* ---------- qr & config ---------- */

  function showQR(id) {
    fetch('/api/me/devices/' + id + '/qr.png', { headers: { Authorization: 'Bearer ' + token } })
      .then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        return res.blob();
      })
      .then(function (blob) {
        var url = URL.createObjectURL(blob);
        openModal('<h3>' + esc(t('qrTitle')) + '</h3>' +
          '<p class="muted modal-sub">' + esc(t('qrHint')) + '</p>' +
          '<div class="qr-wrap"><img src="' + url + '" alt="QR"></div>' +
          '<div class="modal-actions"><button class="btn" onclick="document.getElementById(\'modal\').classList.add(\'hidden\')">' + esc(t('close')) + '</button></div>');
      })
      .catch(fail);
  }

  function showConfig(id) {
    fetch('/api/me/devices/' + id + '/config', { headers: { Authorization: 'Bearer ' + token } })
      .then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        return res.text();
      })
      .then(function (text) {
        openModal('<h3>' + esc(t('configTitle')) + '</h3>' +
          '<pre class="config-pre mono">' + esc(text) + '</pre>' +
          '<div class="modal-actions">' +
          '<button class="btn btn--tertiary btn--small" id="copy-cfg">' + esc(t('copy')) + '</button>' +
          '<button class="btn" id="close-cfg">' + esc(t('close')) + '</button></div>');
        el('copy-cfg').addEventListener('click', function () {
          navigator.clipboard.writeText(text).then(function () {
            el('copy-cfg').textContent = t('copied');
          });
        });
        el('close-cfg').addEventListener('click', closeModal);
      })
      .catch(fail);
  }

  /* ---------- boot ---------- */

  try { token = sessionStorage.getItem('fibr-token'); } catch (e) { /* ignore */ }
  if (token) { load(); } else { signIn(); }
  setInterval(function () {
    if (token && !el('app-view').classList.contains('hidden')) load();
  }, 20000);
})();
