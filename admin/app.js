/* Fibr admin panel logic. */
(function () {
  'use strict';

  var DICT = {
    ru: {
      loginHint: 'Введите админ-токен из /opt/fibr/install/.env (FIBR_ADMIN_TOKEN)',
      loginBtn: 'Войти', logout: 'Выйти',
      tabDash: 'Дашборд', tabUsers: 'Пользователи', tabDevices: 'Устройства',
      stOnline: 'Онлайн', stUsers: 'Пользователи', stDevices: 'Устройства',
      stExpired: 'Истекших', stTraffic: 'Трафик', stTunnel: 'Туннель сейчас',
      serverCard: 'Сервер', recentCard: 'Активные устройства',
      fPublicKey: 'Публичный ключ', fPort: 'Порт', fHost: 'Endpoint', fSubnet: 'Подсеть',
      fDNS: 'DNS', fMTU: 'MTU', fPassive: 'Режим', passiveOn: 'активный', passiveOff: 'только конфиги (нет kernel WG)',
      thUser: 'Пользователь', thTelegram: 'Telegram', thRole: 'Роль', thDevices: 'Устр-ва',
      thOwner: 'Кто создал', thExpiry: 'Действует до',
      thStatus: 'Статус', thTraffic: 'Трафик', thSpeed: 'Скорость', thActions: 'Действия',
      thDevice: 'Устройство', thIP: 'IP', thLastSeen: 'Был(а)', thEnabled: 'Вкл',
      phTelegram: 'Telegram ID (необяз.)', phUsername: 'Имя пользователя',
      roleUser: 'пользователь', roleAdmin: 'админ',
      addUser: 'Добавить', statusActive: 'активен', statusDisabled: 'отключён',
      online: 'онлайн', offline: 'офлайн', never: 'никогда',
      btnQR: 'QR', btnConfig: 'Конфиг', btnDelete: 'Удалить', btnDisable: 'Откл', btnEnable: 'Вкл',
      btnMakeAdmin: 'Сделать админом', btnMakeUser: 'Сделать пользователем', btnAddDevice: '+ устройство',
      unlimited: '∞',
      expiredAgo: 'истёк {n} дн назад',
      btnPlus30: '+30 дн', btnUntilDate: 'До даты…', btnUnlimited: 'Бессрочно',
      promptDate: 'Дата окончания (YYYY-MM-DD):',
      errBadDate: 'Неверный формат даты (нужен YYYY-MM-DD)',
      confirmDeleteUser: 'Удалить пользователя и все его устройства?',
      confirmDeleteDevice: 'Удалить устройство?',
      errAuth: 'Неверный токен', errGeneric: 'Ошибка запроса',
      userCreated: 'Пользователь создан', userDeleted: 'Пользователь удалён',
      deviceDeleted: 'Устройство удалено', updated: 'Обновлено',
      deviceLimit: 'Достигнут лимит устройств',
      copy: 'Копировать', copied: 'Скопировано', download: 'Скачать .conf',
      qrTitle: 'QR-код конфигурации', qrHint: 'Отсканируйте в приложении WireGuard или в клиенте Fibr',
      configTitle: 'Конфигурация устройства', noTG: '—',
      thProtocol: 'Протокол', protWG: 'WG', protXray: 'Xray',
      addDevTitle: 'Новое устройство', fieldName: 'Название', fieldProtocol: 'Протокол',
      btnCancel: 'Отмена', btnCreate: 'Создать', errXrayDisabled: 'Xray выключен на сервере',
      invitesTitle: 'Приглашения', phInviteNote: 'Заметка (кому ссылка)',
      btnInviteCreate: 'Создать приглашение',
      thCode: 'Код', thLink: 'Ссылка', thNote: 'Заметка', thCreated: 'Создано', thUsed: 'Использовано',
      inviteUnused: 'не использовано', inviteCreated: 'Приглашение создано, ссылка скопирована',
      inviteDeleted: 'Приглашение удалено', confirmDeleteInvite: 'Удалить приглашение?',
      thReferrer: 'Пришёл от',
      btnPay: 'Пополнить', payTitle: 'Пополнение',
      payAmount: 'Сумма', payDays: 'Дней подписки', payNote: 'Заметка',
      payErrAmount: 'Сумма должна быть числом (например 500 или 500.50)',
      payDone: 'Пополнение записано', payEarned: 'Комиссий начислено: {n}',
      tabReferrals: 'Рефералы', tabSettings: 'Настройки',
      refTreeTitle: 'Реферальная программа',
      refTermsLine: '{p1}% с пополнений приглашённых, {p2}% с их гостей · Валюта: {c}',
      thRefInvited: 'Приглашено', thRefL1: 'L1', thRefL2: 'L2', thRefPaidOut: 'Выплачено', thRefBalance: 'Баланс',
      btnPayout: 'Выплатить', payoutTitle: 'Выплата',
      payoutOf: 'Баланс {u}: {b}', payoutDone: 'Выплата записана', payoutErrFunds: 'Сумма больше баланса',
      noReferrals: '—',
      payoutsTitle: 'Выплаты', paymentsTitle: 'Пополнения',
      thPayUser: 'Пользователь', thPayAmount: 'Сумма', thPayDays: 'Дни', thPayDate: 'Дата', thPayNote: 'Заметка',
      settingsTitle: 'Настройки',
      setEnabled: 'Реферальная программа включена',
      setPercents: 'Проценты комиссий',
      setCurrency: 'Валюта',
      setContacts: 'Контакты для пользователей',
      setContactsHint: 'Показываются в приложении: как продлить подписку и как вывести реферальный баланс.',
      setSave: 'Сохранить',
      phContacts: '@fibr_admin — Telegram\nUSDT TRC-20: T… — вывод реферального баланса',
      settingsSaved: 'Настройки сохранены',
      /* per-user device limits (docs/devices.md) */
      thLimit: 'Лимит', limitDefault: 'по умолчанию',
      btnLimit: 'Лимит…', btnLimitReset: 'Сброс',
      promptLimit: 'Лимит устройств для {u} (целое число ≥ 0):',
      errBadLimit: 'Лимит должен быть целым числом ≥ 0',
      confirmLimitReset: 'Сбросить личный лимит устройств (вернуть лимит по умолчанию)?',
      /* multi-node (docs/multinode.md) */
      tabNodes: 'Ноды',
      nodesHint: 'Активная нода одна — её адрес попадает во все новые конфиги и QR.',
      btnAddNode: 'Добавить ноду', addNodeTitle: 'Новая нода',
      fieldHost: 'Хост (IP или домен)', fieldWgPort: 'WG-порт', fieldXrayPort: 'Xray-порт (необяз.)',
      errNodeReq: 'Имя и хост обязательны',
      nodeCreated: 'Нода создана', nodeDeleted: 'Нода удалена',
      thNode: 'Нода', thEndpoint: 'Адрес', thOnline: 'Онлайн', thPeers: 'Пиры',
      nodeReady: 'готова', nodePending: 'ожидает', nodeBlocked: 'заблокирована',
      nodeActiveBadge: 'активная', nodeLocalHint: 'этот сервер (control plane)',
      btnNodeActivate: 'Сделать активной', btnNodeBlock: 'Заблокировать', btnNodeUnblock: 'Разблокировать',
      btnNodeToken: 'Токен…', btnNodeDelete: 'Удалить',
      confirmNodeActivate: 'Сделать ноду «{name}» активной? Новые скачиваемые конфиги будут указывать на неё — пользователям придётся скачать их заново.',
      confirmNodeBlock: 'Заблокировать ноду «{name}»? Агент удалит на ней всех пиров.',
      confirmNodeDelete: 'Удалить ноду «{name}»?',
      confirmNodeToken: 'Выпустить новый токен ноды «{name}»? Прежний перестанет работать сразу.',
      nodeTokenTitle: 'Токен ноды',
      nodeTokenOnce: 'Токен показывается только один раз — скопируйте его сейчас.',
      nodeTokenUsage: 'Вставьте его в FIBR_NODE_TOKEN на машине ноды.',
      nodeTokenDocs: 'Как поднять ноду — docs/multinode.md',
      copyFallback: 'Текст выделен — скопируйте вручную (Ctrl+C)',
      btnDone: 'Готово',
      activeNodeLine: 'Активная нода: {node} ({host})'
    },
    en: {
      loginHint: 'Enter the admin token from /opt/fibr/install/.env (FIBR_ADMIN_TOKEN)',
      loginBtn: 'Sign in', logout: 'Sign out',
      tabDash: 'Dashboard', tabUsers: 'Users', tabDevices: 'Devices',
      stOnline: 'Online', stUsers: 'Users', stDevices: 'Devices',
      stExpired: 'Expired', stTraffic: 'Traffic', stTunnel: 'Tunnel now',
      serverCard: 'Server', recentCard: 'Active devices',
      fPublicKey: 'Public key', fPort: 'Port', fHost: 'Endpoint', fSubnet: 'Subnet',
      fDNS: 'DNS', fMTU: 'MTU', fPassive: 'Mode', passiveOn: 'active', passiveOff: 'configs only (no kernel WG)',
      thUser: 'User', thTelegram: 'Telegram', thRole: 'Role', thDevices: 'Devices',
      thOwner: 'Created by', thExpiry: 'Valid until',
      thStatus: 'Status', thTraffic: 'Traffic', thSpeed: 'Speed', thActions: 'Actions',
      thDevice: 'Device', thIP: 'IP', thLastSeen: 'Last seen', thEnabled: 'On',
      phTelegram: 'Telegram ID (optional)', phUsername: 'Username',
      roleUser: 'user', roleAdmin: 'admin',
      addUser: 'Add', statusActive: 'active', statusDisabled: 'disabled',
      online: 'online', offline: 'offline', never: 'never',
      btnQR: 'QR', btnConfig: 'Config', btnDelete: 'Delete', btnDisable: 'Disable', btnEnable: 'Enable',
      btnMakeAdmin: 'Make admin', btnMakeUser: 'Make user', btnAddDevice: '+ device',
      unlimited: '∞',
      expiredAgo: 'expired {n} d ago',
      btnPlus30: '+30 d', btnUntilDate: 'Until date…', btnUnlimited: 'Unlimited',
      promptDate: 'Expiry date (YYYY-MM-DD):',
      errBadDate: 'Invalid date format (YYYY-MM-DD expected)',
      confirmDeleteUser: 'Delete this user and all their devices?',
      confirmDeleteDevice: 'Delete this device?',
      errAuth: 'Invalid token', errGeneric: 'Request failed',
      userCreated: 'User created', userDeleted: 'User deleted',
      deviceDeleted: 'Device deleted', updated: 'Updated',
      deviceLimit: 'Device limit reached',
      copy: 'Copy', copied: 'Copied', download: 'Download .conf',
      qrTitle: 'Configuration QR code', qrHint: 'Scan it in the WireGuard app or the Fibr client',
      configTitle: 'Device configuration', noTG: '—',
      thProtocol: 'Protocol', protWG: 'WG', protXray: 'Xray',
      addDevTitle: 'New device', fieldName: 'Name', fieldProtocol: 'Protocol',
      btnCancel: 'Cancel', btnCreate: 'Create', errXrayDisabled: 'Xray is disabled on the server',
      invitesTitle: 'Invites', phInviteNote: 'Note (who the link is for)',
      btnInviteCreate: 'Create invite',
      thCode: 'Code', thLink: 'Link', thNote: 'Note', thCreated: 'Created', thUsed: 'Used',
      inviteUnused: 'unused', inviteCreated: 'Invite created, link copied',
      inviteDeleted: 'Invite deleted', confirmDeleteInvite: 'Delete this invite?',
      thReferrer: 'Referred by',
      btnPay: 'Top up', payTitle: 'Top up',
      payAmount: 'Amount', payDays: 'Subscription days', payNote: 'Note',
      payErrAmount: 'Amount must be a number (e.g. 500 or 500.50)',
      payDone: 'Payment recorded', payEarned: 'Commissions minted: {n}',
      tabReferrals: 'Referrals', tabSettings: 'Settings',
      refTreeTitle: 'Referral program',
      refTermsLine: '{p1}% of invitees\u2019 top-ups, {p2}% of their guests\u2019 · Currency: {c}',
      thRefInvited: 'Invited', thRefL1: 'L1', thRefL2: 'L2', thRefPaidOut: 'Paid out', thRefBalance: 'Balance',
      btnPayout: 'Pay out', payoutTitle: 'Payout',
      payoutOf: 'Balance of {u}: {b}', payoutDone: 'Payout recorded', payoutErrFunds: 'Amount exceeds the balance',
      noReferrals: '—',
      payoutsTitle: 'Payouts', paymentsTitle: 'Top-ups',
      thPayUser: 'User', thPayAmount: 'Amount', thPayDays: 'Days', thPayDate: 'Date', thPayNote: 'Note',
      settingsTitle: 'Settings',
      setEnabled: 'Referral program enabled',
      setPercents: 'Commission percentages',
      setCurrency: 'Currency',
      setContacts: 'Contact details for users',
      setContactsHint: 'Shown in the app: how to renew and how to withdraw the referral balance.',
      setSave: 'Save',
      phContacts: '@fibr_admin — Telegram\nUSDT TRC-20: T… — referral balance withdrawals',
      settingsSaved: 'Settings saved',
      /* per-user device limits (docs/devices.md) */
      thLimit: 'Limit', limitDefault: 'default',
      btnLimit: 'Limit…', btnLimitReset: 'Reset',
      promptLimit: 'Device limit for {u} (integer ≥ 0):',
      errBadLimit: 'The limit must be an integer ≥ 0',
      confirmLimitReset: 'Reset the personal device limit (back to the default)?',
      /* multi-node (docs/multinode.md) */
      tabNodes: 'Nodes',
      nodesHint: 'Exactly one node is active — its endpoint goes into every newly downloaded config and QR.',
      btnAddNode: 'Add node', addNodeTitle: 'New node',
      fieldHost: 'Host (IP or domain)', fieldWgPort: 'WG port', fieldXrayPort: 'Xray port (optional)',
      errNodeReq: 'Name and host are required',
      nodeCreated: 'Node created', nodeDeleted: 'Node deleted',
      thNode: 'Node', thEndpoint: 'Endpoint', thOnline: 'Online', thPeers: 'Peers',
      nodeReady: 'ready', nodePending: 'pending', nodeBlocked: 'blocked',
      nodeActiveBadge: 'active', nodeLocalHint: 'this server (control plane)',
      btnNodeActivate: 'Make active', btnNodeBlock: 'Block', btnNodeUnblock: 'Unblock',
      btnNodeToken: 'Token…', btnNodeDelete: 'Delete',
      confirmNodeActivate: 'Make node “{name}” active? Newly downloaded configs will point at it — users will be prompted to re-download.',
      confirmNodeBlock: 'Block node “{name}”? The agent will drop all its peers.',
      confirmNodeDelete: 'Delete node “{name}”?',
      confirmNodeToken: 'Issue a new token for node “{name}”? The old one stops working immediately.',
      nodeTokenTitle: 'Node token',
      nodeTokenOnce: 'The token is shown only once — copy it now.',
      nodeTokenUsage: 'Paste it into FIBR_NODE_TOKEN on the node machine.',
      nodeTokenDocs: 'How to set up a node — docs/multinode.md',
      copyFallback: 'Text selected — copy it manually (Ctrl+C)',
      btnDone: 'Done',
      activeNodeLine: 'Active node: {node} ({host})'
    }
  };

  var t = window.Fibr.makeT(DICT);
  var esc = window.Fibr.esc;
  var fmtBytes = window.Fibr.fmtBytes;
  var fmtBits = window.Fibr.fmtBits;
  var fmtAgo = window.Fibr.fmtAgo;
  var fmtMoney = window.Fibr.fmtMoney;
  var toMinor = window.Fibr.toMinor;

  var TOKEN_KEY = 'fibr-admin-token';
  var token = null;
  try { token = localStorage.getItem(TOKEN_KEY); } catch (e) { /* ignore */ }
  var api = window.Fibr.makeApi(function () { return token; });
  var xrayEnabled = false; // refreshed from the overview on each users-tab load

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
      if (name === 'referrals') loadReferrals();
      if (name === 'settings') loadSettings();
      if (name === 'nodes') loadNodes();
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
      el('st-expired').textContent = ov.totals.expired != null ? ov.totals.expired : '—';
      el('st-traffic').innerHTML = '↓ ' + fmtBytes(ov.totals.total_rx) + ' · ↑ ' + fmtBytes(ov.totals.total_tx);
      el('st-tunnel').innerHTML = '↓ ' + fmtBits(ov.throughput.wg_in_bps) + ' · ↑ ' + fmtBits(ov.throughput.wg_out_bps);
      el('server-host').textContent = ov.server.host + ':' + ov.server.port;

      // Multi-node (docs/multinode.md): which node renders into client
      // configs right now. server.host above already mirrors it.
      var nodeLine = el('active-node');
      if (ov.active_node) {
        nodeLine.textContent = t('activeNodeLine', { node: ov.active_node, host: ov.server.host });
        nodeLine.classList.remove('hidden');
      } else {
        nodeLine.classList.add('hidden');
      }

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

  function protoPill(p) {
    return p === 'xray'
      ? '<span class="pill pill--live">' + t('protXray') + '</span>'
      : '<span class="pill pill--off">' + t('protWG') + '</span>';
  }

  // Role pill for the three-level model (see docs/roles.md).
  function rolePill(role) {
    if (role === 'superadmin') return '<span class="pill pill--live">superadmin</span>';
    if (role === 'admin') return '<span class="pill pill--ok">admin</span>';
    return '<span class="pill pill--off">user</span>';
  }

  /* ---------- users ---------- */

  // Last users response, by id — the expiry action handlers below need the
  // row's current expires_at (e.g. "+30 days" extends a live deadline).
  var usersById = {};

  function fmtDate(iso) {
    var d = new Date(iso);
    if (isNaN(d)) return '—';
    return d.toLocaleDateString(t.lang === 'ru' ? 'ru-RU' : 'en-GB', { day: '2-digit', month: 'short', year: 'numeric' });
  }

  function daysOver(iso) {
    var t = new Date(iso).getTime();
    if (isNaN(t)) return 0;
    return Math.max(0, Math.floor((Date.now() - t) / 86400000));
  }

  // "Valid until" cell: green date pill / muted ∞ / red "expired N days ago",
  // plus the superadmin renewal actions (see docs/billing.md). "Top up"
  // records a payment instead: amount + optional days, with referral
  // commissions minted server-side (docs/referrals.md).
  function expiryCell(u) {
    var pill;
    if (u.expired) {
      pill = '<span class="pill pill--danger">' + esc(t('expiredAgo', { n: daysOver(u.expires_at) })) + '</span>';
    } else if (u.expires_at) {
      pill = '<span class="pill pill--ok">' + esc(fmtDate(u.expires_at)) + '</span>';
    } else {
      pill = '<span class="pill pill--off">' + t('unlimited') + '</span>';
    }
    return pill + '<div class="actions actions--tight">' +
      '<button class="btn btn--ghost btn--small" data-act="pay" data-id="' + u.id + '">' + t('btnPay') + '</button>' +
      '<button class="btn btn--ghost btn--small" data-act="exp30" data-id="' + u.id + '">' + t('btnPlus30') + '</button>' +
      '<button class="btn btn--ghost btn--small" data-act="expdate" data-id="' + u.id + '">' + t('btnUntilDate') + '</button>' +
      (u.expires_at ? '<button class="btn btn--ghost btn--small" data-act="expnull" data-id="' + u.id + '">' + t('btnUnlimited') + '</button>' : '') +
      '</div>';
  }

  // "Limit" cell, same pattern as expiryCell: a pill (personal number /
  // grey "default" — the panel does not know the global value) plus the
  // set/reset buttons. Server-side null = fall back to the global default
  // (docs/devices.md).
  function limitCell(u) {
    var pill = u.max_devices != null
      ? '<span class="pill pill--live">' + u.max_devices + '</span>'
      : '<span class="pill pill--off" title="' + esc(t('limitDefault')) + '">' + esc(t('limitDefault')) + '</span>';
    return pill + '<div class="actions actions--tight">' +
      '<button class="btn btn--ghost btn--small" data-act="devlimit" data-id="' + u.id + '">' + t('btnLimit') + '</button>' +
      (u.max_devices != null ? '<button class="btn btn--ghost btn--small" data-act="devlimitreset" data-id="' + u.id + '">' + t('btnLimitReset') + '</button>' : '') +
      '</div>';
  }

  function loadUsers() {
    Promise.all([api('/api/admin/users'), api('/api/admin/devices'), api('/api/admin/overview'), api('/api/admin/invites')]).then(function (res) {
      var users = res[0];
      xrayEnabled = !!(res[2] && res[2].server && res[2].server.xray_enabled);
      var byId = {};
      users.forEach(function (u) { byId[u.id] = u; });
      usersById = byId;
      var byUser = {};
      (res[1] || []).forEach(function (d) {
        byUser[d.user_id] = byUser[d.user_id] || [];
        byUser[d.user_id].push(d);
      });

      el('users-table').innerHTML =
        '<thead><tr><th>ID</th><th>' + t('thUser') + '</th><th>' + t('thTelegram') + '</th>' +
        '<th>' + t('thRole') + '</th><th>' + t('thOwner') + '</th><th>' + t('thReferrer') + '</th><th>' + t('thExpiry') + '</th><th>' + t('thDevices') + '</th><th>' + t('thLimit') + '</th><th>' + t('thTraffic') + '</th><th>' + t('thSpeed') + '</th>' +
        '<th>' + t('thStatus') + '</th><th></th></tr></thead><tbody>' +
        users.map(function (u) {
          var devs = byUser[u.id] || [];
          var rx = 0, tx = 0, online = 0;
          devs.forEach(function (d) { rx += d.total_rx; tx += d.total_tx; if (d.online) online++; });
          // Role buttons: the panel token is the superadmin, who may promote
          // users to admins and demote admins back; superadmin accounts come
          // from the env/static token and are not managed here.
          var roleBtn = '';
          if (u.role === 'user') {
            roleBtn = '<button class="btn btn--ghost btn--small" data-act="role" data-id="' + u.id + '" data-role="admin">' + t('btnMakeAdmin') + '</button>';
          } else if (u.role === 'admin') {
            roleBtn = '<button class="btn btn--ghost btn--small" data-act="role" data-id="' + u.id + '" data-role="user">' + t('btnMakeUser') + '</button>';
          }
          var disBtn = u.disabled
            ? '<button class="btn btn--ghost btn--small" data-act="undisable" data-id="' + u.id + '">' + t('btnEnable') + '</button>'
            : '<button class="btn btn--ghost btn--small" data-act="disable" data-id="' + u.id + '">' + t('btnDisable') + '</button>';
          var owner = u.owner_id
            ? (byId[u.owner_id]
              ? esc(byId[u.owner_id].username || ('#' + u.owner_id))
              : '<span class="mono muted">#' + u.owner_id + '</span>')
            : '<span class="muted">—</span>';
          // Referral attribution: whose link the account signed up with
          // (docs/referrals.md); owner_id and referred_by are different links.
          var referrer = u.referred_by_name
            ? esc(u.referred_by_name)
            : '<span class="muted">—</span>';
          return '<tr>' +
            '<td class="row-id mono">#' + u.id + '</td>' +
            '<td class="user-cell">' + esc(u.username || '—') + '</td>' +
            '<td class="mono">' + (u.telegram_id || t('noTG')) + '</td>' +
            '<td>' + rolePill(u.role) + '</td>' +
            '<td>' + owner + '</td>' +
            '<td>' + referrer + '</td>' +
            '<td>' + expiryCell(u) + '</td>' +
            '<td>' + (devs.length + (u.max_devices != null ? ' / ' + u.max_devices : '')) + (online ? ' <span class="pill pill--ok">' + online + ' ' + t('online') + '</span>' : '') + '</td>' +
            '<td>' + limitCell(u) + '</td>' +
            '<td>↓ ' + fmtBytes(rx) + '<br>↑ ' + fmtBytes(tx) + '</td>' +
            '<td>↓ ' + fmtBits(u.speed_rx_bps) + '<br>↑ ' + fmtBits(u.speed_tx_bps) + '</td>' +
            '<td>' + (u.disabled ? '<span class="pill pill--danger">' + t('statusDisabled') + '</span>' : '<span class="pill pill--ok">' + t('statusActive') + '</span>') + '</td>' +
            '<td><div class="actions">' + roleBtn + disBtn +
            '<button class="btn btn--tertiary btn--small" data-act="adddev" data-id="' + u.id + '">' + t('btnAddDevice') + '</button>' +
            '<button class="btn btn--danger btn--small" data-act="deluser" data-id="' + u.id + '">' + t('btnDelete') + '</button>' +
            '</div></td></tr>';
        }).join('') + '</tbody>';
      renderInvites(res[3] || []);
    }).catch(fail);
  }

  /* ---------- subscription deadlines (manual monetization) ---------- */

  function patchExpiry(id, value) {
    api('/api/admin/users/' + id, { method: 'PATCH', body: { expires_at: value } })
      .then(function () { toast(t('updated')); loadUsers(); }).catch(fail);
  }

  // "+30 days": extend a still-live deadline, or start counting from today.
  function extendBy30(u) {
    var base = Date.now();
    if (u.expires_at && new Date(u.expires_at).getTime() > base) {
      base = new Date(u.expires_at).getTime();
    }
    patchExpiry(u.id, new Date(base + 30 * 86400000).toISOString());
  }

  // "Until date…": prompt for YYYY-MM-DD, validate, send end-of-day UTC.
  function promptUntilDate(u) {
    var raw = (prompt(t('promptDate'), '') || '').trim();
    if (!raw) return;
    var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(raw);
    if (!m) { toast(t('errBadDate'), true); return; }
    var d = new Date(+m[1], +m[2] - 1, +m[3], 23, 59, 59);
    if (d.getFullYear() !== +m[1] || d.getMonth() !== +m[2] - 1 || d.getDate() !== +m[3]) {
      toast(t('errBadDate'), true);
      return;
    }
    patchExpiry(u.id, d.toISOString());
  }

  // "Limit…": prompt for an integer >= 0, validate client-side, PATCH.
  // The panel token is the superadmin, so the server accepts any value;
  // a 403 (or any other error) still surfaces as a red toast via fail().
  function promptDevLimit(u) {
    var current = u.max_devices != null ? String(u.max_devices) : '';
    var raw = (prompt(t('promptLimit', { u: u.username || ('#' + u.id) }), current) || '').trim();
    if (!raw) return;
    if (!/^\d+$/.test(raw)) { toast(t('errBadLimit'), true); return; }
    api('/api/admin/users/' + u.id, { method: 'PATCH', body: { max_devices: parseInt(raw, 10) } })
      .then(function () { toast(t('updated')); loadUsers(); }).catch(fail);
  }

  /* ---------- invites (self-enroll links) ---------- */

  // Trim a link for the table cell; the full link travels in data-link and is
  // copied on click.
  function shortLink(link) {
    return link.length > 34 ? link.slice(0, 34) + '…' : link;
  }

  function renderInvites(invites) {
    if (!invites.length) {
      el('invites-table').innerHTML = '<thead><tr><th>' + t('thCode') + '</th><th>' + t('thLink') + '</th>' +
        '<th>' + t('thNote') + '</th><th>' + t('thCreated') + '</th><th>' + t('thUsed') + '</th><th></th></tr></thead>' +
        '<tbody><tr><td class="muted">—</td></tr></tbody>';
      return;
    }
    el('invites-table').innerHTML =
      '<thead><tr><th>' + t('thCode') + '</th><th>' + t('thLink') + '</th>' +
      '<th>' + t('thNote') + '</th><th>' + t('thCreated') + '</th><th>' + t('thUsed') + '</th><th></th></tr></thead><tbody>' +
      invites.map(function (inv) {
        var used = inv.used_by
          ? esc(inv.username || ('#' + inv.used_by)) + '<br><small class="muted">' + (inv.used_at ? (fmtAgo(inv.used_at, t.lang) || '') : '') + '</small>'
          : '<span class="pill pill--off">' + t('inviteUnused') + '</span>';
        return '<tr>' +
          '<td class="mono">' + esc(inv.code) + '</td>' +
          '<td><button class="btn btn--ghost btn--small mono" data-act="copylink" data-link="' + esc(inv.link) + '" title="' + esc(inv.link) + '">' + esc(shortLink(inv.link)) + '</button></td>' +
          '<td>' + esc(inv.note || '—') + '</td>' +
          '<td>' + (fmtAgo(inv.created_at, t.lang) || '') + '</td>' +
          '<td>' + used + '</td>' +
          '<td><button class="btn btn--danger btn--small" data-act="delinv" data-code="' + esc(inv.code) + '">' + t('btnDelete') + '</button></td>' +
          '</tr>';
      }).join('') + '</tbody>';
  }

  el('inv-create').addEventListener('click', function () {
    api('/api/admin/invites', { method: 'POST', body: { note: el('inv-note').value.trim() } }).then(function (inv) {
      el('inv-note').value = '';
      if (inv.link && navigator.clipboard) {
        navigator.clipboard.writeText(inv.link).then(function () { toast(t('inviteCreated')); }, function () { toast(t('inviteCreated')); });
      } else {
        toast(t('inviteCreated'));
      }
      loadUsers();
    }).catch(fail);
  });

  el('invites-table').addEventListener('click', function (e) {
    var btn = e.target.closest('button[data-act]');
    if (!btn) return;
    var act = btn.getAttribute('data-act');
    if (act === 'copylink') {
      navigator.clipboard.writeText(btn.getAttribute('data-link')).then(function () {
        toast(t('copied'));
      }, function () { toast(t('copy'), false); });
    } else if (act === 'delinv') {
      if (!confirm(t('confirmDeleteInvite'))) return;
      api('/api/admin/invites/' + encodeURIComponent(btn.getAttribute('data-code')), { method: 'DELETE' })
        .then(function () { toast(t('inviteDeleted')); loadUsers(); }).catch(fail);
    }
  });

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
      showAddDevice(id);
    } else if (act === 'pay') {
      showPayment(id);
    } else if (act === 'exp30') {
      extendBy30(usersById[id] || {});
    } else if (act === 'expdate') {
      promptUntilDate(usersById[id] || {});
    } else if (act === 'expnull') {
      patchExpiry(id, null);
    } else if (act === 'devlimit') {
      promptDevLimit(usersById[id] || {});
    } else if (act === 'devlimitreset') {
      if (!confirm(t('confirmLimitReset'))) return;
      api('/api/admin/users/' + id, { method: 'PATCH', body: { max_devices: null } })
        .then(function () { toast(t('updated')); loadUsers(); }).catch(fail);
    }
  });

  /* ---------- referral program (docs/referrals.md) ---------- */

  var refCurrency = ''; // from /api/admin/referrals settings, for money cells

  // Record a top-up: amount + optional subscription days + note. One POST
  // writes the ledger row, extends the deadline and mints L1/L2 commissions;
  // the response says how many earnings were credited.
  function showPayment(userId) {
    var u = usersById[userId] || {};
    openModal('<h3>' + esc(t('payTitle')) + ' — ' + esc(u.username || ('#' + userId)) + '</h3>' +
      '<p class="muted modal-sub">' + esc(t('payAmount')) + '</p>' +
      '<input class="input" id="pay-amount" placeholder="500" inputmode="decimal">' +
      '<p class="muted modal-sub" style="margin-top:12px">' + esc(t('payDays')) + '</p>' +
      '<input class="input" id="pay-days" type="number" min="0" max="3650" value="30">' +
      '<p class="muted modal-sub" style="margin-top:12px">' + esc(t('payNote')) + '</p>' +
      '<input class="input" id="pay-note" maxlength="120">' +
      '<div class="modal-actions">' +
      '<button class="btn btn--tertiary btn--small" id="pay-cancel">' + esc(t('btnCancel')) + '</button>' +
      '<button class="btn btn--small" id="pay-create">' + esc(t('btnPay')) + '</button></div>');
    el('pay-cancel').addEventListener('click', closeModal);
    el('pay-create').addEventListener('click', function () {
      var amount = toMinor(el('pay-amount').value);
      if (amount == null || amount <= 0) { toast(t('payErrAmount'), true); return; }
      var days = parseInt(el('pay-days').value, 10);
      if (isNaN(days) || days < 0) days = 0;
      api('/api/admin/users/' + userId + '/payments', {
        method: 'POST',
        body: { amount: amount, days: days, note: el('pay-note').value.trim() }
      }).then(function (res) {
        closeModal();
        var n = (res && res.earnings ? res.earnings.length : 0);
        toast(t('payDone') + (n ? ' · ' + t('payEarned', { n: n }) : ''));
        loadUsers();
      }).catch(fail);
    });
  }

  // The referrals tab: the who-invited-whom table with balances, the payout
  // ledger and the top-up ledger.
  function loadReferrals() {
    Promise.all([api('/api/admin/referrals'), api('/api/admin/payments')]).then(function (res) {
      var data = res[0] || {};
      var set = data.settings || {};
      refCurrency = set.currency || '';
      el('ref-terms').textContent = t('refTermsLine', {
        p1: set.percent_l1 != null ? set.percent_l1 : 15,
        p2: set.percent_l2 != null ? set.percent_l2 : 5,
        c: refCurrency || '—'
      });
      var rows = data.rows || [];
      el('referrals-table').innerHTML =
        '<thead><tr><th>ID</th><th>' + t('thUser') + '</th><th>' + t('thReferrer') + '</th>' +
        '<th>' + t('thRefInvited') + '</th><th>' + t('thRefL1') + '</th><th>' + t('thRefL2') + '</th>' +
        '<th>' + t('thRefPaidOut') + '</th><th>' + t('thRefBalance') + '</th><th></th></tr></thead><tbody>' +
        (rows.length ? rows.map(function (r) {
          return '<tr>' +
            '<td class="row-id mono">#' + r.user_id + '</td>' +
            '<td class="user-cell">' + esc(r.username || ('#' + r.user_id)) + '</td>' +
            '<td>' + (r.referrer_name ? esc(r.referrer_name) : '<span class="muted">—</span>') + '</td>' +
            '<td>' + (r.invited || 0) + '</td>' +
            '<td>' + esc(fmtMoney(r.earned_l1, refCurrency)) + '</td>' +
            '<td>' + esc(fmtMoney(r.earned_l2, refCurrency)) + '</td>' +
            '<td>' + esc(fmtMoney(r.paid_out, refCurrency)) + '</td>' +
            '<td><b>' + esc(fmtMoney(r.balance, refCurrency)) + '</b></td>' +
            '<td>' + (r.balance > 0
              ? '<button class="btn btn--ghost btn--small" data-act="payout" data-id="' + r.user_id + '" data-name="' + esc(r.username || ('#' + r.user_id)) + '" data-balance="' + r.balance + '">' + t('btnPayout') + '</button>'
              : '') + '</td></tr>';
        }).join('') : '<tr><td class="muted">' + t('noReferrals') + '</td></tr>') + '</tbody>';

      var payouts = data.payouts || [];
      el('payouts-table').innerHTML =
        '<thead><tr><th>ID</th><th>' + t('thPayUser') + '</th><th>' + t('thPayAmount') + '</th>' +
        '<th>' + t('thPayNote') + '</th><th>' + t('thPayDate') + '</th></tr></thead><tbody>' +
        (payouts.length ? payouts.map(function (p) {
          return '<tr>' +
            '<td class="row-id mono">#' + p.id + '</td>' +
            '<td class="user-cell">' + esc(p.username || ('#' + p.user_id)) + '</td>' +
            '<td><b>' + esc(fmtMoney(p.amount, refCurrency)) + '</b></td>' +
            '<td>' + esc(p.note || '—') + '</td>' +
            '<td>' + esc(fmtDate(p.created_at)) + '</td></tr>';
        }).join('') : '<tr><td class="muted">' + t('noReferrals') + '</td></tr>') + '</tbody>';

      var payments = res[1] || [];
      el('payments-table').innerHTML =
        '<thead><tr><th>ID</th><th>' + t('thPayUser') + '</th><th>' + t('thPayAmount') + '</th>' +
        '<th>' + t('thPayDays') + '</th><th>' + t('thPayNote') + '</th><th>' + t('thPayDate') + '</th></tr></thead><tbody>' +
        (payments.length ? payments.map(function (p) {
          return '<tr>' +
            '<td class="row-id mono">#' + p.id + '</td>' +
            '<td class="user-cell">' + esc(p.username || ('#' + p.user_id)) + '</td>' +
            '<td><b>' + esc(fmtMoney(p.amount, refCurrency)) + '</b></td>' +
            '<td>' + (p.days || '—') + '</td>' +
            '<td>' + esc(p.note || '—') + '</td>' +
            '<td>' + esc(fmtDate(p.created_at)) + '</td></tr>';
        }).join('') : '<tr><td class="muted">' + t('noReferrals') + '</td></tr>') + '</tbody>';
    }).catch(fail);
  }

  el('referrals-table').addEventListener('click', function (e) {
    var btn = e.target.closest('button[data-act="payout"]');
    if (!btn) return;
    showPayout(btn.getAttribute('data-id'), btn.getAttribute('data-name'), parseInt(btn.getAttribute('data-balance'), 10) || 0);
  });

  // Record a manual payout: the money moves outside the system (crypto,
  // transfer); the ledger keeps the user's balance honest.
  function showPayout(userId, name, balance) {
    openModal('<h3>' + esc(t('payoutTitle')) + ' — ' + esc(name) + '</h3>' +
      '<p class="muted modal-sub">' + esc(t('payoutOf', { u: name, b: fmtMoney(balance, refCurrency) })) + '</p>' +
      '<input class="input" id="po-amount" placeholder="' + esc(fmtMoney(balance, refCurrency)) + '" inputmode="decimal">' +
      '<p class="muted modal-sub" style="margin-top:12px">' + esc(t('payNote')) + '</p>' +
      '<input class="input" id="po-note" maxlength="120">' +
      '<div class="modal-actions">' +
      '<button class="btn btn--tertiary btn--small" id="po-cancel">' + esc(t('btnCancel')) + '</button>' +
      '<button class="btn btn--small" id="po-create">' + esc(t('btnPayout')) + '</button></div>');
    el('po-cancel').addEventListener('click', closeModal);
    el('po-create').addEventListener('click', function () {
      var amount = toMinor(el('po-amount').value);
      if (amount == null || amount <= 0) { toast(t('payErrAmount'), true); return; }
      if (amount > balance) { toast(t('payoutErrFunds'), true); return; }
      api('/api/admin/referrals/payouts', {
        method: 'POST',
        body: { user_id: parseInt(userId, 10), amount: amount, note: el('po-note').value.trim() }
      }).then(function () {
        closeModal();
        toast(t('payoutDone'));
        loadReferrals();
      }).catch(fail);
    });
  }

  /* ---------- referral settings ---------- */

  function loadSettings() {
    api('/api/admin/settings').then(function (s) {
      el('set-enabled').checked = !!s.enabled;
      el('set-p1').value = s.percent_l1 != null ? s.percent_l1 : 15;
      el('set-p2').value = s.percent_l2 != null ? s.percent_l2 : 5;
      el('set-currency').value = s.currency || '';
      el('set-contact').value = s.contact_info || '';
    }).catch(fail);
  }

  el('set-save').addEventListener('click', function () {
    var p1 = parseInt(el('set-p1').value, 10);
    var p2 = parseInt(el('set-p2').value, 10);
    if (isNaN(p1) || p1 < 0 || p1 > 100 || isNaN(p2) || p2 < 0 || p2 > 100) {
      toast(t('errGeneric'), true);
      return;
    }
    api('/api/admin/settings', {
      method: 'PATCH',
      body: {
        referral_enabled: el('set-enabled').checked,
        referral_percent_l1: p1,
        referral_percent_l2: p2,
        currency: el('set-currency').value.trim(),
        contact_info: el('set-contact').value.trim()
      }
    }).then(function () {
      toast(t('settingsSaved'));
    }).catch(fail);
  });

  /* Issue a device for a user: name + protocol, then jump straight to the QR. */
  function showAddDevice(userId) {
    var protoOptions = '<option value="wg">' + t('protWG') + ' — WireGuard</option>' +
      (xrayEnabled ? '<option value="xray">' + t('protXray') + ' — VLESS/Reality</option>' : '');
    openModal('<h3>' + esc(t('addDevTitle')) + '</h3>' +
      '<p class="muted modal-sub">' + esc(t('fieldName')) + '</p>' +
      '<input class="input" id="nd-name" maxlength="32" value="device">' +
      '<p class="muted modal-sub" style="margin-top:12px">' + esc(t('fieldProtocol')) + '</p>' +
      '<select class="input" id="nd-proto">' + protoOptions + '</select>' +
      '<div class="modal-actions">' +
      '<button class="btn btn--tertiary btn--small" id="nd-cancel">' + esc(t('btnCancel')) + '</button>' +
      '<button class="btn btn--small" id="nd-create">' + esc(t('btnCreate')) + '</button></div>');
    el('nd-cancel').addEventListener('click', closeModal);
    el('nd-create').addEventListener('click', function () {
      var proto = el('nd-proto').value;
      if (proto === 'xray' && !xrayEnabled) { toast(t('errXrayDisabled'), true); return; }
      api('/api/admin/users/' + userId + '/devices', {
        method: 'POST',
        body: { name: el('nd-name').value.trim(), protocol: proto }
      }).then(function (d) {
        closeModal();
        toast(t('updated'));
        loadUsers();
        showQR(d.id);
      }).catch(fail);
    });
  }

  /* ---------- devices ---------- */

  function loadDevices() {
    api('/api/admin/devices').then(function (devices) {
      el('devices-table').innerHTML =
        '<thead><tr><th>ID</th><th>' + t('thDevice') + '</th><th>' + t('thUser') + '</th>' +
        '<th>' + t('thProtocol') + '</th>' +
        '<th>' + t('thIP') + '</th><th>' + t('thStatus') + '</th><th>' + t('thLastSeen') + '</th>' +
        '<th>' + t('thTraffic') + '</th><th>' + t('thSpeed') + '</th><th></th></tr></thead><tbody>' +
        (devices || []).map(function (d) {
          return '<tr>' +
            '<td class="row-id mono">#' + d.id + '</td>' +
            '<td class="device-cell">' + esc(d.name) + '</td>' +
            '<td>' + esc(d.username || '#' + d.user_id) + '</td>' +
            '<td>' + protoPill(d.protocol) + '</td>' +
            '<td class="mono">' + esc(d.ipv4) + '</td>' +
            '<td>' + deviceStatus(d) + '</td>' +
            '<td>' + (d.last_handshake ? (fmtAgo(d.last_handshake, t.lang) || '') : t('never')) + '</td>' +
            '<td>↓ ' + fmtBytes(d.total_rx) + '<br>↑ ' + fmtBytes(d.total_tx) + '</td>' +
            '<td>↓ ' + fmtBits(d.speed_rx_bps) + '<br>↑ ' + fmtBits(d.speed_tx_bps) + '</td>' +
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

  /* ---------- nodes (multi-node; docs/multinode.md) ---------- */

  // The control-plane box itself; its row mirrors env (FIBR_WG_HOST) and has
  // no agent token — the server would 400 every edit/delete on it.
  var LOCAL_NODE = 'local';

  // Node timestamps may arrive as unix seconds or ISO strings depending on
  // the API version — normalize so fmtAgo understands them.
  function nodeSeenIso(v) {
    if (v == null) return null;
    if (typeof v === 'number') return new Date(v * 1000).toISOString();
    return v;
  }

  function nodeStatusPill(n) {
    if (n.status === 'ready') return '<span class="pill pill--ok">' + t('nodeReady') + '</span>';
    if (n.status === 'blocked') return '<span class="pill pill--danger">' + t('nodeBlocked') + '</span>';
    return '<span class="pill pill--off">' + t('nodePending') + '</span>';
  }

  function nodeEndpoint(n) {
    var s = '<span class="mono">' + esc(n.host) + ':' + n.wg_port + '</span>';
    if (n.xray_port != null) s += '<small class="muted">xray :' + n.xray_port + '</small>';
    return s;
  }

  // One row: #id, name (+ «активная» badge), endpoint, status pill, online
  // dot, peers, last seen, actions. Buttons that the server would reject
  // (400) are hidden for the local node and disabled for the active one.
  function nodeRow(n) {
    var isLocal = n.name === LOCAL_NODE;
    var acts = [];
    if (!n.is_active) {
      var canActivate = isLocal || n.status === 'ready'; // activate 400s unless ready
      acts.push('<button class="btn btn--secondary btn--small" data-nact="activate" data-id="' + n.id + '" data-name="' + esc(n.name) + '"' + (canActivate ? '' : ' disabled') + '>' + t('btnNodeActivate') + '</button>');
    }
    if (!isLocal) {
      if (n.status === 'blocked') {
        acts.push('<button class="btn btn--ghost btn--small" data-nact="unblock" data-id="' + n.id + '" data-name="' + esc(n.name) + '">' + t('btnNodeUnblock') + '</button>');
      } else {
        acts.push('<button class="btn btn--ghost btn--small" data-nact="block" data-id="' + n.id + '" data-name="' + esc(n.name) + '"' + (n.is_active ? ' disabled' : '') + '>' + t('btnNodeBlock') + '</button>');
      }
      acts.push('<button class="btn btn--ghost btn--small" data-nact="token" data-id="' + n.id + '" data-name="' + esc(n.name) + '">' + t('btnNodeToken') + '</button>');
      acts.push('<button class="btn btn--danger btn--small" data-nact="delnode" data-id="' + n.id + '" data-name="' + esc(n.name) + '"' + (n.is_active ? ' disabled' : '') + '>' + t('btnNodeDelete') + '</button>');
    }
    var name = '<div class="user-cell"><b>' + esc(n.name) + '</b>' +
      (n.is_active ? ' <span class="pill pill--ok">' + t('nodeActiveBadge') + '</span>' : '') +
      (isLocal ? '<small class="muted">' + esc(t('nodeLocalHint')) + '</small>' : '') + '</div>';
    var seen = fmtAgo(nodeSeenIso(n.last_seen_at), t.lang);
    return '<tr>' +
      '<td class="row-id mono">#' + n.id + '</td>' +
      '<td>' + name + '</td>' +
      '<td class="node-endpoint">' + nodeEndpoint(n) + '</td>' +
      '<td>' + nodeStatusPill(n) + '</td>' +
      '<td><span class="pill ' + (n.online ? 'pill--ok' : 'pill--off') + '"><span class="dot ' + (n.online ? 'dot--on' : 'dot--off') + '"></span>' + (n.online ? t('online') : t('offline')) + '</span></td>' +
      '<td>' + (n.peers != null ? n.peers : '—') + '</td>' +
      '<td>' + (seen || '—') + '</td>' +
      '<td><div class="actions">' + acts.join('') + '</div></td></tr>';
  }

  function loadNodes() {
    api('/api/admin/nodes').then(function (data) {
      var nodes = (data && data.nodes) || [];
      el('nodes-table').innerHTML =
        '<thead><tr><th>ID</th><th>' + t('thNode') + '</th><th>' + t('thEndpoint') + '</th>' +
        '<th>' + t('thStatus') + '</th><th>' + t('thOnline') + '</th><th>' + t('thPeers') + '</th>' +
        '<th>' + t('thLastSeen') + '</th><th></th></tr></thead><tbody>' +
        (nodes.length ? nodes.map(nodeRow).join('') : '<tr><td class="muted">—</td></tr>') + '</tbody>';
    }).catch(fail);
  }

  el('nodes-table').addEventListener('click', function (e) {
    var btn = e.target.closest('button[data-nact]');
    if (!btn) return;
    var id = btn.getAttribute('data-id');
    var name = btn.getAttribute('data-name');
    var act = btn.getAttribute('data-nact');
    if (act === 'activate') {
      if (!confirm(t('confirmNodeActivate', { name: name }))) return;
      api('/api/admin/nodes/' + id + '/activate', { method: 'POST' })
        .then(function () { toast(t('updated')); loadNodes(); }).catch(fail);
    } else if (act === 'block') {
      if (!confirm(t('confirmNodeBlock', { name: name }))) return;
      api('/api/admin/nodes/' + id, { method: 'PATCH', body: { status: 'blocked' } })
        .then(function () { toast(t('updated')); loadNodes(); }).catch(fail);
    } else if (act === 'unblock') {
      api('/api/admin/nodes/' + id, { method: 'PATCH', body: { status: 'ready' } })
        .then(function () { toast(t('updated')); loadNodes(); }).catch(fail);
    } else if (act === 'token') {
      if (!confirm(t('confirmNodeToken', { name: name }))) return;
      api('/api/admin/nodes/' + id + '/token', { method: 'POST' })
        .then(function (res) { showNodeToken(res && res.token, name, false); }).catch(fail);
    } else if (act === 'delnode') {
      if (!confirm(t('confirmNodeDelete', { name: name }))) return;
      api('/api/admin/nodes/' + id, { method: 'DELETE' })
        .then(function () { toast(t('nodeDeleted')); loadNodes(); }).catch(fail);
    }
  });

  // The agent token is returned exactly once (create / rotate) and never
  // again — the modal warns and offers copy with a select-fallback.
  function showNodeToken(token, name, created) {
    var tok = token || '';
    openModal('<h3>' + esc(t('nodeTokenTitle')) + ' — ' + esc(name) + '</h3>' +
      (created ? '<p class="muted modal-sub">' + esc(t('nodeCreated')) + '</p>' : '') +
      '<p class="token-warn">' + esc(t('nodeTokenOnce')) + '</p>' +
      '<pre class="config-pre mono" id="node-token" tabindex="0">' + esc(tok) + '</pre>' +
      '<p class="muted modal-sub">' + esc(t('nodeTokenUsage')) + '</p>' +
      '<p class="muted modal-sub"><a href="/docs/multinode.md" target="_blank" rel="noopener">' + esc(t('nodeTokenDocs')) + '</a></p>' +
      '<div class="modal-actions">' +
      '<button class="btn btn--tertiary btn--small" id="nt-done">' + esc(t('btnDone')) + '</button>' +
      '<button class="btn btn--secondary btn--small" id="nt-copy">' + esc(t('copy')) + '</button></div>');
    el('nt-done').addEventListener('click', closeModal);
    el('nt-copy').addEventListener('click', function () {
      var pre = el('node-token');
      var markCopied = function () { el('nt-copy').textContent = t('copied'); };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(tok).then(markCopied, function () { selectTokenText(pre); });
      } else {
        selectTokenText(pre);
      }
    });
  }

  // Clipboard fallback: select the token text so Ctrl+C is all it takes.
  function selectTokenText(pre) {
    var range = document.createRange();
    range.selectNodeContents(pre);
    var sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);
    toast(t('copyFallback'));
  }

  // "Add node": name + host + optional ports. On success the same modal is
  // replaced by the show-once token view (the list reloads behind it).
  el('node-create').addEventListener('click', function () {
    openModal('<h3>' + esc(t('addNodeTitle')) + '</h3>' +
      '<p class="muted modal-sub">' + esc(t('fieldName')) + '</p>' +
      '<input class="input" id="nn-name" maxlength="32">' +
      '<p class="muted modal-sub" style="margin-top:12px">' + esc(t('fieldHost')) + '</p>' +
      '<input class="input" id="nn-host" maxlength="190" placeholder="vpn2.example.com">' +
      '<p class="muted modal-sub" style="margin-top:12px">' + esc(t('fieldWgPort')) + '</p>' +
      '<input class="input" id="nn-wgport" type="number" min="1" max="65535" value="51820">' +
      '<p class="muted modal-sub" style="margin-top:12px">' + esc(t('fieldXrayPort')) + '</p>' +
      '<input class="input" id="nn-xrayport" type="number" min="1" max="65535" placeholder="8443">' +
      '<div class="modal-actions">' +
      '<button class="btn btn--tertiary btn--small" id="nn-cancel">' + esc(t('btnCancel')) + '</button>' +
      '<button class="btn btn--small" id="nn-create">' + esc(t('btnCreate')) + '</button></div>');
    el('nn-cancel').addEventListener('click', closeModal);
    el('nn-create').addEventListener('click', function () {
      var name = el('nn-name').value.trim();
      var host = el('nn-host').value.trim();
      if (!name || !host) { toast(t('errNodeReq'), true); return; }
      var body = { name: name, host: host };
      var wg = parseInt(el('nn-wgport').value, 10);
      if (!isNaN(wg)) body.wg_port = wg;
      var xp = parseInt(el('nn-xrayport').value, 10);
      if (!isNaN(xp)) body.xray_port = xp;
      api('/api/admin/nodes', { method: 'POST', body: body }).then(function (res) {
        showNodeToken(res && res.token, name, true);
        loadNodes();
      }).catch(fail);
    });
  });

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
