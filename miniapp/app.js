/* Fibr Telegram Mini App. */
// Global error trap — the very first thing on the page. A JS crash during
// init otherwise leaves both views hidden = the infamous "black screen";
// with this, any error shows as readable text instead (fast support).
(function () {
  function showErr(msg) {
    try {
      var el = document.getElementById('error-text');
      var view = document.getElementById('error-view');
      if (el && view) {
        el.textContent = 'Error: ' + msg;
        view.classList.remove('hidden');
        document.getElementById('app-view') && document.getElementById('app-view').classList.add('hidden');
      }
    } catch (e) { /* nothing left to break */ }
  }
  window.addEventListener('error', function (e) {
    var src = e.filename ? ' @' + String(e.filename).split('/').pop() + ':' + e.lineno : '';
    showErr((e.message || e.type || 'unknown') + src);
  });
  window.addEventListener('unhandledrejection', function (e) {
    var r = e.reason;
    showErr((r && (r.message || r)) || 'promise rejection');
  });
})();

(function () {
  'use strict';

  var DICT = {
    ru: {
      greeting: 'Привет, {name}!',
      lblDevices: 'Устройства', lblOnline: 'Онлайн', lblTraffic: 'Трафик',
      phName: 'Название устройства (телефон, ноутбук…)',
      devicesHint: 'Устройство — это телефон или компьютер. Добавьте по одному на каждый.',
      addDevice: 'Добавить',
      online: 'онлайн', offline: 'офлайн', disabled: 'отключено', never: 'не подключалось',
      lastSeen: 'было(а) {ago}', rx: '↓ {v}', tx: '↑ {v}',
      btnQR: 'QR-код', btnConfig: 'Конфиг', btnDelete: 'Удалить',
      confirmDelete: 'Удалить устройство {name}?',
      errNotTelegram: 'Откройте это окно из бота Fibr в Telegram.',
      openInTelegram: 'Открыть в Telegram',
      errAuth: 'Не удалось войти через Telegram. Сообщите админу.',
      errDisabled: 'Ваш аккаунт отключён администратором.',
      errExpired: 'Подписка истекла. Свяжитесь с администратором, чтобы продлить.',
      errInvite: 'Регистрация только по приглашению. Попросите у админа ссылку-приглашение и откройте её.',
      errGeneric: 'Ошибка запроса',
      deviceLimit: 'Лимит устройств исчерпан',
      nameRequired: 'Введите название устройства — например, «Телефон» или «Ноутбук»',
      validUntil: 'Действует до {d}',
      unlimited: 'Бессрочно',
      shortExpired: 'истёк',
      validUntilShort: 'до {d}',
      empty: 'Пока нет устройств.\nДобавьте первое — и отсканируйте QR.',
      qrTitle: 'QR-код конфигурации',
      qrHint: 'Отсканируйте в приложении WireGuard/Fibr',
      qrHintXray: 'Ссылка VLESS — отсканируйте в клиенте Xray (v2rayNG, Hiddify, Streisand)',
      qrStep1: 'Установите приложение WireGuard (или клиент Fibr)',
      qrStep2: 'Нажмите «+» и выберите «Создать из QR-кода»',
      qrStep3: 'Наведите камеру на код',
      btnCode: 'Код',
      codeTitle: 'Код быстрого подключения',
      codeHintClient: 'В приложении Fibr: «Добавить туннель» → «По коду» — введите код или отсканируйте QR.',
      codeHintLink: 'Или откройте ссылку — на странице будет кнопка «Открыть в приложении».',
      codeExpires: 'Действует {t}',
      codeMin: 'мин',
      codeExpired: 'Код истёк — закройте окно и сгенерируйте новый',
      configTitle: 'Конфигурация', downloadConf: 'Скачать .conf',
      copy: 'Копировать', copied: 'Скопировано', close: 'Закрыть',
      footHint: 'QR также совместим с официальным приложением WireGuard',
      usersTitle: 'Пользователи',
      usersHint: 'Люди, которых вы добавили. Выдайте им устройство или отправьте приглашение.',
      noUsers: 'Пока никого.\nСоздайте приглашение и отправьте ссылку.',
      devicesOf: '{n} устр.',
      btnGrantDevice: 'Выдать устройство', btnInviteCreate: 'Приглашение',
      grantTitle: 'Выдать устройство',
      fieldName: 'Название', fieldProtocol: 'Протокол',
      btnCancel: 'Отмена', btnCreate: 'Создать',
      inviteModalTitle: 'Ссылка-приглашение',
      inviteHint: 'Один человек сможет зарегистрироваться, открыв эту ссылку в Telegram.',
      refTitle: 'Реферальная программа',
      refHint: 'Приглашайте друзей — получайте процент с их пополнений.',
      refCopy: 'Копировать',
      refTerms: '{p1}% с пополнений приглашённых и {p2}% с пополнений их гостей.',
      refInvited: 'Приглашено', refEarned: 'Заработано', refPaidOut: 'Выплачено', refBalance: 'Баланс',
      refEmpty: 'Пока никого.\nОтправьте ссылку другу — он появится здесь после регистрации.',
      refEarnedFrom: 'Заработано',
      refPayout: 'Вывод средств',
      refContactLabel: 'Связь с администратором',
      errXrayDisabled: 'Xray выключен на сервере',
      /* app access key (server login for the Fibr app) */
      keyTitle: 'Ключ для приложения',
      keyHint: 'Личный ключ для входа в приложение Fibr — домен сервера и ключ, без Telegram и QR.',
      keyExists: 'ключ создан',
      keyNone: 'ключа нет',
      keyGen: 'Сгенерировать',
      keyRegen: 'Сгенерировать заново',
      keyRevoke: 'Отозвать',
      keyModalTitle: 'Ключ для приложения',
      keyWarning: 'Показывается только один раз — скопируйте сразу. Прежний ключ перестанет работать.',
      keyHowTo: 'Приложение Fibr → «Вход» → «Ключ доступа»: введите домен сервера и этот ключ.',
      keyConfirmRevoke: 'Отозвать ключ? Вход в приложение по нему перестанет работать.',
      keyRevoked: 'Ключ отозван',
      /* multi-node (docs/multinode.md) */
      sumNode: 'Сервер: {node}',
      staleConfig: 'конфиг устарел',
      userLimit: 'лимит {n}'
    },
    en: {
      greeting: 'Hi, {name}!',
      lblDevices: 'Devices', lblOnline: 'Online', lblTraffic: 'Traffic',
      phName: 'Device name (phone, laptop…)',
      devicesHint: 'A device is a phone or a computer. Add one for each.',
      addDevice: 'Add',
      online: 'online', offline: 'offline', disabled: 'disabled', never: 'never connected',
      lastSeen: 'last seen {ago}', rx: '↓ {v}', tx: '↑ {v}',
      btnQR: 'QR code', btnConfig: 'Config', btnDelete: 'Delete',
      confirmDelete: 'Delete device {name}?',
      errNotTelegram: 'Open this window from the Fibr bot in Telegram.',
      openInTelegram: 'Open in Telegram',
      errAuth: 'Telegram sign-in failed. Contact the admin.',
      errDisabled: 'Your account is disabled by the administrator.',
      errExpired: 'Your subscription has expired. Contact the administrator to renew it.',
      errInvite: 'Sign-up is invite-only. Ask the admin for an invite link and open it.',
      errGeneric: 'Request failed',
      deviceLimit: 'Device limit reached',
      nameRequired: 'Enter a device name — e.g. “Phone” or “Laptop”',
      validUntil: 'Valid until {d}',
      unlimited: 'Unlimited',
      shortExpired: 'expired',
      validUntilShort: 'until {d}',
      empty: 'No devices yet.\nAdd one and scan the QR code.',
      qrTitle: 'Configuration QR code',
      qrHint: 'Scan it in the WireGuard/Fibr app',
      qrHintXray: 'VLESS link — scan it with an Xray client (v2rayNG, Hiddify, Streisand)',
      qrStep1: 'Install the WireGuard app (or the Fibr client)',
      qrStep2: 'Tap “+” and choose “Create from QR code”',
      qrStep3: 'Point your camera at the code',
      btnCode: 'Code',
      codeTitle: 'Quick-connect code',
      codeHintClient: 'In the Fibr app: “Add tunnel” → “By code” — type the code or scan the QR.',
      codeHintLink: 'Or open the link — the page has an “Open in the app” button.',
      codeExpires: 'Valid {t}',
      codeMin: 'min',
      codeExpired: 'The code has expired — close and generate a new one',
      configTitle: 'Configuration', downloadConf: 'Download .conf',
      copy: 'Copy', copied: 'Copied', close: 'Close',
      footHint: 'The QR also works with the official WireGuard app',
      usersTitle: 'Users',
      usersHint: 'People you added. Issue them a device or send an invite.',
      noUsers: 'Nobody yet.\nCreate an invite and send the link.',
      devicesOf: '{n} devices',
      btnGrantDevice: 'Issue device', btnInviteCreate: 'Invite',
      grantTitle: 'Issue device',
      fieldName: 'Name', fieldProtocol: 'Protocol',
      btnCancel: 'Cancel', btnCreate: 'Create',
      inviteModalTitle: 'Invite link',
      inviteHint: 'One person can register by opening this link in Telegram.',
      refTitle: 'Referral program',
      refHint: 'Invite friends — earn a cut of their top-ups.',
      refCopy: 'Copy',
      refTerms: '{p1}% of your invitees\u2019 top-ups and {p2}% of their guests\u2019.',
      refInvited: 'Invited', refEarned: 'Earned', refPaidOut: 'Paid out', refBalance: 'Balance',
      refEmpty: 'Nobody yet.\nSend the link to a friend — they appear here after signing up.',
      refEarnedFrom: 'Earned',
      refPayout: 'Withdrawals',
      refContactLabel: 'Contact the administrator',
      errXrayDisabled: 'Xray is disabled on the server',
      /* app access key (server login for the Fibr app) */
      keyTitle: 'App key',
      keyHint: 'A personal key to sign in to the Fibr app — the server domain and the key, no Telegram and no QR.',
      keyExists: 'key created',
      keyNone: 'no key',
      keyGen: 'Generate',
      keyRegen: 'Regenerate',
      keyRevoke: 'Revoke',
      keyModalTitle: 'App key',
      keyWarning: 'Shown only once — copy it now. The previous key stops working.',
      keyHowTo: 'Fibr app → “Sign in” → “Access key”: enter the server domain and this key.',
      keyConfirmRevoke: 'Revoke the key? Sign-in with it will stop working.',
      keyRevoked: 'Key revoked',
      /* multi-node (docs/multinode.md) */
      sumNode: 'Server: {node}',
      staleConfig: 'config outdated',
      userLimit: 'limit {n}'
    }
  };

  var t = window.Fibr.makeT(DICT);
  var esc = window.Fibr.esc;
  var fmtBytes = window.Fibr.fmtBytes;
  var fmtAgo = window.Fibr.fmtAgo;
  var fmtMoney = window.Fibr.fmtMoney;

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
  el('devices-hint').textContent = t('devicesHint');
  el('users-title').textContent = t('usersTitle');
  el('users-hint').textContent = t('usersHint');
  el('invite-create').textContent = t('btnInviteCreate');
  el('ref-title').textContent = t('refTitle');
  el('ref-hint').textContent = t('refHint');
  el('ref-copy').textContent = t('refCopy');
  el('key-title').textContent = t('keyTitle');
  el('key-hint').textContent = t('keyHint');
  el('key-revoke').textContent = t('keyRevoke');

  /* ---------- modal ---------- */

  function openModal(html) {
    el('modal-body').innerHTML = html;
    el('modal').classList.remove('hidden');
  }

  function closeModal() {
    if (codeTimer) { clearInterval(codeTimer); codeTimer = null; }
    el('modal').classList.add('hidden');
    el('modal-body').innerHTML = '';
  }

  el('modal').addEventListener('click', function (e) {
    if (e.target === el('modal')) closeModal();
  });

  function fail(e) {
    if (e && e.status === 403 && e.message === 'subscription expired') {
      showExpired(); // deadline passed mid-session (docs/billing.md)
      return;
    }
    if (e && e.status === 401) { showError(t('errAuth')); return; }
    alert((e && e.message) || t('errGeneric'));
  }

  function showError(msg) {
    el('error-text').textContent = msg;
    el('error-view').classList.remove('hidden');
    el('app-view').classList.add('hidden');
  }

  // The "subscription expired" screen gets the owner's contact line appended
  // (settings.contact_info via /api/public/info) so the user immediately sees
  // who to pay — no login possible, hence the anonymous fetch.
  function showExpired() {
    showError(t('errExpired'));
    fetch('/api/public/info').then(function (res) { return res.ok ? res.json() : null; }).then(function (info) {
      if (info && info.contact_info) {
        el('error-text').innerHTML =
          esc(t('errExpired')) + '<br><br><b>' + esc(t('refContactLabel')) + ':</b><br>' +
          esc(info.contact_info).replace(/\n/g, '<br>');
      }
    }).catch(function () { /* keep the plain message */ });
  }

  /* ---------- auth ---------- */

  // Invite code for self-enroll: either an explicit ?invite=CODE query (the
  // admin panel copies links like https://<domain>/miniapp/?invite=CODE) or
  // the start_param of a t.me/<bot>?startapp=CODE deep link. Referral deep
  // links carry the "ref_CODE" scheme, which is NOT an invite.
  function startParam() {
    try {
      return (tg && tg.initDataUnsafe && tg.initDataUnsafe.start_param) || null;
    } catch (e) { return null; }
  }

  function inviteCode() {
    try {
      var p = new URLSearchParams(window.location.search).get('invite');
      if (p) return p;
    } catch (e) { /* ignore */ }
    var sp = startParam();
    if (sp && sp.indexOf('ref_') === 0) return null;
    return sp;
  }

  // Referral code: ?ref=CODE query (the referral card and the bot's app
  // button both use it) or a "ref_CODE" start_param deep link.
  function referralCode() {
    try {
      var p = new URLSearchParams(window.location.search).get('ref');
      if (p) return p;
    } catch (e) { /* ignore */ }
    var sp = startParam();
    if (sp && sp.indexOf('ref_') === 0) return sp.slice(4);
    return null;
  }

  // Opened outside Telegram (a plain https link lands in a browser, so there
  // is no initData): bounce to the bot deep link with the same invite/ref
  // code. The bot answers with a Web App button that opens this page for
  // real, inside Telegram. Old https://<domain>/miniapp/?invite=… links keep
  // working this way.
  function redirectToBot() {
    var code = inviteCode();
    var ref = referralCode();
    var start = code ? 'inv_' + code : (ref ? 'ref_' + ref : '');
    fetch('/api/public/info').then(function (res) { return res.json(); }).then(function (info) {
      if (!info || !info.bot_username) return;
      var link = 'https://t.me/' + encodeURIComponent(info.bot_username) +
        (start ? '?start=' + encodeURIComponent(start) : '');
      var a = document.createElement('a');
      a.href = link;
      a.className = 'btn';
      a.textContent = t('openInTelegram');
      a.style.marginTop = '16px';
      a.style.display = 'inline-block';
      el('error-text').parentNode.appendChild(a);
      window.location.href = link;
    }).catch(function () { /* keep the plain error */ });
  }

  function signIn() {
    if (!tg || !tg.initData) {
      showError(t('errNotTelegram'));
      redirectToBot();
      return;
    }
    var body = { init_data: tg.initData };
    var code = inviteCode();
    if (code) body.invite_code = code;
    var ref = referralCode();
    if (ref) body.referral_code = ref;
    fetch('/api/auth/telegram', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }).then(function (res) {
      return res.json().then(function (data) {
        if (!res.ok) {
          var msg = data && data.error ? data.error : 'auth failed';
          if (res.status === 403 && (msg === 'invite required' || msg === 'invalid invite')) {
            showError(t('errInvite'));
            throw new Error('invite');
          }
          if (res.status === 403 && msg === 'subscription expired') {
            showExpired();
            throw new Error('expired');
          }
          if (res.status === 403) { showError(t('errDisabled')); throw new Error('disabled'); }
          throw new Error(msg);
        }
        return data;
      });
    }).then(function (data) {
      token = data.token;
      try { sessionStorage.setItem('fibr-token', token); } catch (e) { /* ignore */ }
      load();
    }).catch(function (e) {
      if (e && (e.message === 'disabled' || e.message === 'invite' || e.message === 'expired')) return;
      // Show the server's actual reason (signature mismatch, stale initData,
      // network…) instead of a generic phrase — it is the fastest way to
      // diagnose a "cannot sign in" report.
      var detail = e && e.message && e.message !== 'Failed to fetch' ? e.message : '';
      showError(detail ? t('errAuth') + ' [' + detail + ']' : t('errAuth'));
    });
  }

  /* ---------- data ---------- */

  function load() {
    api('/api/me').then(function (data) {
      me = data;
      // Xray option appears only when the server has the second protocol on.
      var proto = document.getElementById('new-proto');
      if (me.xray_enabled && !proto.querySelector('option[value="xray"]')) {
        var opt = document.createElement('option');
        opt.value = 'xray';
        opt.textContent = 'Xray';
        proto.appendChild(opt);
      } else if (!me.xray_enabled) {
        var x = proto.querySelector('option[value="xray"]');
        if (x) x.remove();
        proto.value = 'wg';
      }
      var name = data.user.username || (tg && tg.initDataUnsafe && tg.initDataUnsafe.user && tg.initDataUnsafe.user.first_name) || '';
      el('greeting').textContent = t('greeting', { name: name });
      el('app-view').classList.remove('hidden');
      el('error-view').classList.add('hidden');
      render();
      loadUsersSection();
      loadRefSection();
      loadKeySection();
    }).catch(fail);
  }

  /* ---------- formatting ---------- */

  function fmtDate(iso) {
    var d = new Date(iso);
    if (isNaN(d)) return '—';
    return d.toLocaleDateString(t.lang === 'ru' ? 'ru-RU' : 'en-GB', { day: '2-digit', month: 'short', year: 'numeric' });
  }

  function fmtDateShort(iso) {
    var d = new Date(iso);
    if (isNaN(d)) return '—';
    return d.toLocaleDateString(t.lang === 'ru' ? 'ru-RU' : 'en-GB', { day: 'numeric', month: 'short' });
  }

  // Multi-node timestamps (docs/multinode.md): endpoint_changed_at is unix
  // seconds, config_fetched_at may be unix seconds or an ISO string —
  // normalize both to unix seconds (0 = unknown/never).
  function toUnix(v) {
    if (v == null) return 0;
    if (typeof v === 'number') return Math.floor(v < 1e12 ? v : v / 1000);
    var s = String(v).trim();
    if (/^\d+$/.test(s)) return parseInt(s, 10);
    var p = Date.parse(s);
    return isNaN(p) ? 0 : Math.floor(p / 1000);
  }

  // A config downloaded before the last endpoint switch is stale: its
  // embedded endpoint/server key point at the old node. Tapping the badge
  // opens the QR again (the download stamps config_fetched_at server-side,
  // so the badge clears on the next 20s refresh).
  function isStale(d) {
    return (toUnix(me && me.endpoint_changed_at) || 0) > toUnix(d.config_fetched_at);
  }

  function render() {
    var devices = me.devices || [];
    var online = devices.filter(function (d) { return d.online; }).length;
    var rx = 0, tx = 0;
    devices.forEach(function (d) { rx += d.total_rx; tx += d.total_tx; });

    el('sum-devices').textContent = devices.length + (me.max_devices ? ' / ' + me.max_devices : '');
    el('sum-online').textContent = online;
    el('sum-traffic').textContent = fmtBytes(rx + tx);
    el('sum-traffic').title = '↓ ' + fmtBytes(rx) + ' · ↑ ' + fmtBytes(tx);
    // Subscription deadline line (docs/billing.md): the superadmin sells time;
    // an account without a deadline is unlimited.
    el('sum-expiry').textContent = (me.user && me.user.expires_at)
      ? t('validUntil', { d: fmtDate(me.user.expires_at) })
      : t('unlimited');
    // Which node serves the VPN right now (docs/multinode.md).
    var sumNode = el('sum-node');
    if (me.active_node) {
      sumNode.textContent = t('sumNode', { node: me.active_node });
      sumNode.classList.remove('hidden');
    } else {
      sumNode.classList.add('hidden');
    }

    if (!devices.length) {
      el('devices').innerHTML = '<div class="card empty">' + esc(t('empty')).replace(/\n/g, '<br>') + '</div>';
      return;
    }

    el('devices').innerHTML = devices.map(function (d) {
      var pill = d.enabled ? (d.online ? 'pill--ok' : 'pill--off') : 'pill--danger';
      var label = d.enabled ? (d.online ? t('online') : t('offline')) : t('disabled');
      var ago = d.last_handshake ? fmtAgo(d.last_handshake, t.lang) : null;
      var seen = ago ? t('lastSeen', { ago: ago }) : t('never');
      var proto = d.protocol === 'xray'
        ? '<span class="pill pill--live proto-pill">Xray</span>'
        : '<span class="pill pill--off proto-pill">WG</span>';
      // Stale-config badge (both protocols — vless links embed the endpoint
      // too): a button so the delegated handler routes the tap to the QR.
      var stale = isStale(d)
        ? '<button class="pill pill--warn stale-pill" data-act="qr" data-id="' + d.id + '">' + esc(t('staleConfig')) + '</button>'
        : '';
      return '<div class="card device">' +
        '<div class="device-top">' +
        '<span class="dot ' + (d.online ? 'dot--on' : 'dot--off') + '"></span>' +
        '<div class="grow"><h3>' + esc(d.name) + '</h3>' +
        '<span class="ip mono">' + esc(d.ipv4) + '</span>' + proto + '</div>' +
        '<span class="pill ' + pill + '">' + label + '</span>' + stale + '</div>' +
        '<div class="device-meta"><span>' + esc(seen) + '</span>' +
        '<span>' + t('rx', { v: fmtBytes(d.total_rx) }) + '</span>' +
        '<span>' + t('tx', { v: fmtBytes(d.total_tx) }) + '</span></div>' +
        '<div class="device-actions">' +
        '<button class="btn btn--secondary btn--small" data-act="qr" data-id="' + d.id + '">' + t('btnQR') + '</button>' +
        '<button class="btn btn--secondary btn--small" data-act="code" data-id="' + d.id + '">' + t('btnCode') + '</button>' +
        '<button class="btn btn--tertiary btn--small" data-act="cfg" data-id="' + d.id + '">' + t('btnConfig') + '</button>' +
        '<button class="btn btn--danger-ghost btn--small" data-act="del" data-id="' + d.id + '" data-name="' + esc(d.name) + '">' + t('btnDelete') + '</button>' +
        '</div></div></div>';
    }).join('');
  }

  el('add-device').addEventListener('click', function () {
    var name = el('new-name').value.trim();
    if (!name) { alert(t('nameRequired')); el('new-name').focus(); return; }
    var proto = el('new-proto').value || 'wg';
    api('/api/me/devices', { method: 'POST', body: { name: name, protocol: proto } }).then(function (d) {
      el('new-name').value = '';
      load();
      showQR(d.id, d.protocol);
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
    if (act === 'qr') {
      var dev = (me.devices || []).filter(function (x) { return String(x.id) === String(id); })[0];
      showQR(id, dev && dev.protocol);
    }
    else if (act === 'cfg') showConfig(id);
    else if (act === 'code') showJoinCode(id);
    else if (act === 'del') {
      if (!confirm(t('confirmDelete', { name: btn.getAttribute('data-name') }))) return;
      api('/api/me/devices/' + id, { method: 'DELETE' }).then(load).catch(fail);
    }
  });

  /* ---------- reseller section (admins / superadmins only) ---------- */

  // Ordinary users never see the section: their screen stays "greeting,
  // device cards, QR". Admins get a subtree list of their users with an
  // issue-device flow and a one-tap invite.
  function isAdminRole() {
    return !!(me && me.user && (me.user.role === 'admin' || me.user.role === 'superadmin'));
  }

  function loadUsersSection() {
    if (!isAdminRole()) {
      el('users-view').classList.add('hidden');
      return;
    }
    Promise.all([api('/api/admin/users'), api('/api/admin/devices')]).then(function (res) {
      var users = res[0] || [];
      var devices = res[1] || [];
      var counts = {};
      devices.forEach(function (d) { counts[d.user_id] = (counts[d.user_id] || 0) + 1; });
      el('users-view').classList.remove('hidden');
      if (!users.length) {
        el('users').innerHTML = '<div class="card empty">' + esc(t('noUsers')).replace(/\n/g, '<br>') + '</div>';
        return;
      }
      el('users').innerHTML = users.map(function (u) {
        var tg = u.telegram_id ? 'TG ' + u.telegram_id : '—';
        // Deadline status pill (the admin cannot renew — only the superadmin
        // sells time, via the admin panel; see docs/billing.md).
        var expPill;
        if (u.expired) {
          expPill = '<span class="pill pill--danger">' + esc(t('shortExpired')) + '</span>';
        } else if (u.expires_at) {
          expPill = '<span class="pill pill--ok">' + esc(t('validUntilShort', { d: fmtDateShort(u.expires_at) })) + '</span>';
        } else {
          expPill = '<span class="pill pill--off">∞</span>';
        }
        // Own device cap (docs/devices.md): shown only when set personally —
        // null means the global default the miniapp cannot see. No control
        // here: limits are the panel's job (the server 403s non-owners).
        var limitPill = u.max_devices != null
          ? '<span class="pill pill--live">' + esc(t('userLimit', { n: u.max_devices })) + '</span>'
          : '';
        return '<div class="card device">' +
          '<div class="device-top">' +
          '<div class="grow"><h3>' + esc(u.username || ('#' + u.id)) + '</h3>' +
          '<span class="muted mono user-tg">' + esc(tg) + '</span></div>' +
          '<span class="user-pills">' + expPill + limitPill +
          '<span class="pill pill--off">' + esc(t('devicesOf', { n: counts[u.id] || 0 })) + '</span></span></div>' +
          '<div class="device-actions">' +
          '<button class="btn btn--tertiary btn--small btn--block" data-uact="grant" data-id="' + u.id + '">' + esc(t('btnGrantDevice')) + '</button>' +
          '</div></div>';
      }).join('');
    }).catch(function (e) {
      // 403 means the role was revoked mid-session: just hide the section.
      if (e && e.status === 403) { el('users-view').classList.add('hidden'); return; }
      fail(e);
    });
  }

  el('users').addEventListener('click', function (e) {
    var btn = e.target.closest('button[data-uact]');
    if (!btn) return;
    showGrantDevice(btn.getAttribute('data-id'));
  });

  // Issue a device to one of my users: name + protocol, then straight to QR.
  function showGrantDevice(userId) {
    var protoOptions = '<option value="wg">WireGuard</option>' +
      (me.xray_enabled ? '<option value="xray">Xray</option>' : '');
    openModal('<h3>' + esc(t('grantTitle')) + '</h3>' +
      '<p class="muted modal-sub">' + esc(t('fieldName')) + '</p>' +
      '<input class="input" id="gd-name" maxlength="32" value="device">' +
      '<p class="muted modal-sub" style="margin-top:12px">' + esc(t('fieldProtocol')) + '</p>' +
      '<select class="input" id="gd-proto">' + protoOptions + '</select>' +
      '<div class="modal-actions">' +
      '<button class="btn btn--tertiary btn--small" id="gd-cancel">' + esc(t('btnCancel')) + '</button>' +
      '<button class="btn btn--small" id="gd-create">' + esc(t('btnCreate')) + '</button></div>');
    el('gd-cancel').addEventListener('click', closeModal);
    el('gd-create').addEventListener('click', function () {
      api('/api/admin/users/' + userId + '/devices', {
        method: 'POST',
        body: { name: el('gd-name').value.trim() || 'device', protocol: el('gd-proto').value || 'wg' }
      }).then(function (d) {
        closeModal();
        loadUsersSection();
        showAdminQR(d.id, d.protocol);
      }).catch(fail);
    });
  }

  // One-tap invite: the link is copied to the clipboard and shown as text.
  el('invite-create').addEventListener('click', function () {
    api('/api/admin/invites', { method: 'POST', body: {} }).then(function (inv) {
      var link = inv.link || inv.code || '';
      if (link && navigator.clipboard) {
        navigator.clipboard.writeText(link).catch(function () { /* shown below anyway */ });
      }
      openModal('<h3>' + esc(t('inviteModalTitle')) + '</h3>' +
        '<p class="muted modal-sub">' + esc(t('inviteHint')) + '</p>' +
        '<pre class="config-pre mono">' + esc(link) + '</pre>' +
        '<div class="modal-actions">' +
        '<button class="btn btn--tertiary btn--small" id="inv-copy">' + esc(t('copy')) + '</button>' +
        '<button class="btn" id="inv-close">' + esc(t('close')) + '</button></div>');
      el('inv-copy').addEventListener('click', function () {
        navigator.clipboard.writeText(link).then(function () {
          el('inv-copy').textContent = t('copied');
        });
      });
      el('inv-close').addEventListener('click', closeModal);
    }).catch(fail);
  });

  /* ---------- referral section (every account) ---------- */

  var refData = null; // last /api/me/referrals payload (for the copy button)

  // The referral card: share link, terms, balance tiles, the invited list
  // and the owner's contact line for withdrawals. Hidden entirely when the
  // program is off (docs/referrals.md).
  function loadRefSection() {
    api('/api/me/referrals').then(function (ref) {
      if (!ref || !ref.enabled) {
        el('ref-view').classList.add('hidden');
        return;
      }
      refData = ref;
      el('ref-view').classList.remove('hidden');
      var money = function (m) { return fmtMoney(m, ref.currency); };
      var stats = ref.stats || {};
      var rows = (ref.referrals || []).map(function (r) {
        return '<div class="ref-row">' +
          '<span class="grow"><b>' + esc(r.username || ('#' + r.id)) + '</b>' +
          ' <span class="muted">' + fmtDateShort(r.created_at) + '</span></span>' +
          '<span class="ref-earned">' + esc(t('refEarnedFrom')) + ': <b>' + esc(money(r.earned)) + '</b></span>' +
          '</div>';
      }).join('');
      el('ref-body').innerHTML =
        '<pre class="config-pre mono">' + esc(ref.link) + '</pre>' +
        '<p class="muted ref-terms">' + esc(t('refTerms', { p1: ref.percent_l1, p2: ref.percent_l2 })) + '</p>' +
        '<div class="card summary ref-card">' +
        '<div class="summary-row">' +
        '<div><span class="muted">' + esc(t('refInvited')) + '</span><b>' + (stats.invited || 0) + '</b></div>' +
        '<div><span class="muted">' + esc(t('refEarned')) + '</span><b>' + esc(money((stats.earned_l1 || 0) + (stats.earned_l2 || 0))) + '</b></div>' +
        '<div><span class="muted">' + esc(t('refPaidOut')) + '</span><b>' + esc(money(stats.paid_out || 0)) + '</b></div>' +
        '<div><span class="muted">' + esc(t('refBalance')) + '</span><b>' + esc(money(stats.balance || 0)) + '</b></div>' +
        '</div></div>' +
        (rows
          ? '<div class="card ref-list">' + rows + '</div>'
          : '<div class="card empty">' + esc(t('refEmpty')).replace(/\n/g, '<br>') + '</div>') +
        (ref.contact_info
          ? '<p class="muted"><b>' + esc(t('refPayout')) + ':</b> ' + esc(ref.contact_info).replace(/\n/g, '<br>') + '</p>'
          : '');
    }).catch(function (e) {
      // The card is auxiliary: any failure just hides it.
      el('ref-view').classList.add('hidden');
    });
  }

  el('ref-copy').addEventListener('click', function () {
    var link = refData && refData.link;
    if (!link) return;
    navigator.clipboard.writeText(link).then(function () {
      el('ref-copy').textContent = t('copied');
      setTimeout(function () { el('ref-copy').textContent = t('refCopy'); }, 1500);
    });
  });

  /* ---------- app key (server login for the Fibr app) ---------- */

  // The server stores only a hash: the plaintext key is shown exactly once,
  // in the modal right after generation. A repeat generate rotates the key;
  // revoke clears it. State comes from /api/me (has_access_key boolean).
  function loadKeySection() {
    el('key-view').classList.remove('hidden');
    renderKeyState(!!(me && me.has_access_key));
  }

  function renderKeyState(has) {
    var pill = el('key-status');
    pill.textContent = has ? t('keyExists') : t('keyNone');
    pill.className = 'pill ' + (has ? 'pill--ok' : 'pill--off');
    el('key-revoke').classList.toggle('hidden', !has);
    el('key-create').textContent = has ? t('keyRegen') : t('keyGen');
  }

  function showKeyModal(key) {
    openModal('<h3>' + esc(t('keyModalTitle')) + '</h3>' +
      '<p class="muted modal-sub key-warn">' + esc(t('keyWarning')) + '</p>' +
      '<div class="key-value mono" id="app-key" title="' + esc(t('copy')) + '">' + esc(key) + '</div>' +
      '<p class="muted">' + esc(t('keyHowTo')) + '</p>' +
      '<div class="modal-actions">' +
      '<button class="btn btn--tertiary btn--small" id="key-copy">' + esc(t('copy')) + '</button>' +
      '<button class="btn" id="key-close">' + esc(t('close')) + '</button></div>');
    function copyKey() {
      navigator.clipboard.writeText(key).then(function () { /* tap feedback via :active */ });
    }
    el('app-key').addEventListener('click', copyKey);
    el('key-copy').addEventListener('click', function () {
      navigator.clipboard.writeText(key).then(function () {
        el('key-copy').textContent = t('copied');
      });
    });
    el('key-close').addEventListener('click', function () {
      closeModal();
      load(); // refresh has_access_key behind the section
    });
  }

  el('key-create').addEventListener('click', function () {
    api('/api/me/access-key', { method: 'POST', body: {} }).then(function (d) {
      showKeyModal(d.key);
    }).catch(function (e) {
      if (e && e.status === 404) { alert(t('errGeneric')); return; }
      fail(e);
    });
  });

  el('key-revoke').addEventListener('click', function () {
    if (!confirm(t('keyConfirmRevoke'))) return;
    api('/api/me/access-key', { method: 'DELETE' }).then(function () {
      alert(t('keyRevoked'));
      load();
    }).catch(fail);
  });

  /* ---------- qr & config ---------- */

  // qrSteps renders the non-technical walkthrough under a wg QR code
  // (xray keeps its own client hint).
  function qrSteps(protocol) {
    if (protocol === 'xray') return '';
    return '<ol class="qr-steps muted">' +
      '<li>' + esc(t('qrStep1')) + '</li>' +
      '<li>' + esc(t('qrStep2')) + '</li>' +
      '<li>' + esc(t('qrStep3')) + '</li></ol>';
  }

  function qrModal(url, protocol, joinPath) {
    var hint = protocol === 'xray' ? t('qrHintXray') : t('qrHint');
    var joinBtn = joinPath
      ? '<button class="btn btn--secondary btn--small" id="qr-join">' + esc(t('btnCode')) + '</button>'
      : '';
    openModal('<h3>' + esc(t('qrTitle')) + '</h3>' +
      '<p class="muted modal-sub">' + esc(hint) + '</p>' +
      '<div class="qr-wrap"><img src="' + url + '" alt="QR"></div>' +
      qrSteps(protocol) +
      '<div class="modal-actions">' + joinBtn +
      '<button class="btn" onclick="document.getElementById(\'modal\').classList.add(\'hidden\')">' + esc(t('close')) + '</button></div>');
    if (joinPath) {
      el('qr-join').addEventListener('click', function () {
        api(joinPath, { method: 'POST', body: {} }).then(joinCodeModal).catch(fail);
      });
    }
  }

  function showQR(id, protocol) {
    fetch('/api/me/devices/' + id + '/qr.png', { headers: { Authorization: 'Bearer ' + token } })
      .then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        return res.blob();
      })
      .then(function (blob) {
        qrModal(URL.createObjectURL(blob), protocol);
      })
      .catch(fail);
  }

  // Admin variant for devices just issued to one of my users (the QR pops up
  // immediately so it can be handed over right away; the extra "Code" button
  // mints a quick-connect code for the same device via the admin endpoint).
  function showAdminQR(id, protocol) {
    fetch('/api/admin/devices/' + id + '/qr.png', { headers: { Authorization: 'Bearer ' + token } })
      .then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        return res.blob();
      })
      .then(function (blob) {
        qrModal(URL.createObjectURL(blob), protocol, '/api/admin/devices/' + id + '/join');
      })
      .catch(fail);
  }

  // Quick-connect code (docs/join.md): a short one-time code minted for
  // this device. The holder types it in the Fibr client ("By code") or opens
  // the link; the modal shows the code big, the link and a QR of the link,
  // plus a live countdown to the code's TTL.
  var codeTimer = null;

  function showJoinCode(id) {
    api('/api/me/devices/' + id + '/join', { method: 'POST', body: {} }).then(joinCodeModal).catch(function (e) {
      if (e && e.status === 403 && e.message === 'subscription expired') { showExpired(); return; }
      fail(e);
    });
  }

  function joinCodeModal(j) {
    var qrImg = j.qr ? '<div class="qr-wrap"><img src="' + j.qr + '" alt="QR"></div>' : '';
    openModal('<h3>' + esc(t('codeTitle')) + '</h3>' +
      '<p class="muted modal-sub">' + esc(t('codeHintClient')) + '</p>' +
      '<div class="join-code mono" id="join-code" title="' + esc(t('copy')) + '">' + esc(j.code) + '</div>' +
      qrImg +
      '<p class="muted modal-sub">' + esc(t('codeHintLink')) + '</p>' +
      '<div class="join-link mono" id="join-link" title="' + esc(t('copy')) + '">' + esc(j.url) + '</div>' +
      '<p class="muted" id="join-ttl"></p>' +
      '<div class="modal-actions">' +
      '<button class="btn btn--tertiary btn--small" id="copy-join-link">' + esc(t('copy')) + '</button>' +
      '<button class="btn" id="close-join">' + esc(t('close')) + '</button></div>');
    function copyText(txt) {
      navigator.clipboard.writeText(txt).then(function () { /* tap feedback via :active is enough */ });
    }
    el('join-code').addEventListener('click', function () { copyText(j.code); });
    el('join-link').addEventListener('click', function () { copyText(j.url); });
    el('copy-join-link').addEventListener('click', function () {
      navigator.clipboard.writeText(j.url).then(function () { this.textContent = t('copied'); }.bind(this));
    });
    el('close-join').addEventListener('click', closeModal);
    function tick() {
      var ttl = el('join-ttl');
      if (!ttl) { if (codeTimer) { clearInterval(codeTimer); codeTimer = null; } return; }
      var left = Math.max(0, Math.floor((new Date(j.expires_at).getTime() - Date.now()) / 1000));
      if (left <= 0) { ttl.textContent = t('codeExpired'); clearInterval(codeTimer); codeTimer = null; return; }
      var m = Math.floor(left / 60), s = left % 60;
      ttl.textContent = t('codeExpires', { t: m + ' ' + t('codeMin') + ' ' + (s < 10 ? '0' : '') + s });
    }
    if (codeTimer) clearInterval(codeTimer);
    tick();
    codeTimer = setInterval(tick, 1000);
  }

  function showConfig(id) {    fetch('/api/me/devices/' + id + '/config', { headers: { Authorization: 'Bearer ' + token } })
      .then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        return res.text();
      })
      .then(function (text) {
        // One-device flow: saving the .conf lets the WireGuard app import it
        // via "+" -> "Import from file" — no second phone to scan the QR.
        openModal('<h3>' + esc(t('configTitle')) + '</h3>' +
          '<pre class="config-pre mono">' + esc(text) + '</pre>' +
          '<div class="modal-actions">' +
          '<button class="btn btn--tertiary btn--small" id="copy-cfg">' + esc(t('copy')) + '</button>' +
          '<button class="btn btn--secondary btn--small" id="dl-cfg">' + esc(t('downloadConf')) + '</button>' +
          '<button class="btn" id="close-cfg">' + esc(t('close')) + '</button></div>');
        el('copy-cfg').addEventListener('click', function () {
          navigator.clipboard.writeText(text).then(function () {
            el('copy-cfg').textContent = t('copied');
          });
        });
        el('dl-cfg').addEventListener('click', function () {
          var a = document.createElement('a');
          a.href = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
          a.download = 'fibr-' + id + '.conf';
          document.body.appendChild(a);
          a.click();
          a.remove();
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
