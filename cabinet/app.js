// Global error trap: a JS crash during init otherwise hides every view
// (black screen); this makes it readable text instead.
(function () {
  function showErr(msg) {
    try {
      var el = document.getElementById('error-text');
      var view = document.getElementById('error-view');
      if (el && view) {
        el.textContent = 'Error: ' + msg;
        view.classList.remove('hidden');
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

/* Fibr web cabinet — the browser twin of the Telegram Mini App.
 * Same screens and the same /api, only the sign-in differs: the Telegram
 * Login Widget (data-auth-url redirect) instead of initData. The server
 * verifies both signature schemes in POST /api/auth/telegram.
 * Keep the non-auth parts in sync with ../miniapp/app.js. */
(function () {
  'use strict';

  var DICT = {
    ru: {
      greeting: 'Привет, {name}!',
      loginHint: 'Войдите по логину и паролю. Первый раз — откройте ссылку-приглашение или ссылку для входа от администратора.',
      loginLinkField: 'Вставьте ссылку для входа', loginGo: 'Войти',
      pwLogin: 'Логин', pwPass: 'Пароль', pwGo: 'Войти',
      codeToggle: 'У меня код или ссылка для входа',
      errPw: 'Неверный логин или пароль.', errPwMany: 'Слишком много попыток — подождите 15 минут.',
      pwTitle: 'Логин и пароль',
      pwHintNone: 'Придумайте логин и пароль — по ним вы войдёте в кабинет с любого устройства, без кодов и Telegram.',
      pwHintSet: 'Ваш логин: {login}. Можно сменить логин или пароль.',
      pwNewLogin: 'Логин (латиница, цифры, . _ -)', pwNewPass: 'Пароль (от 8 символов)', pwSave: 'Сохранить',
      pwSaved: 'Сохранено ✅ Теперь входите по логину и паролю.',
      errPwTaken: 'Такой логин уже занят — придумайте другой.',
      errPwBad: 'Логин: 3–32 символа (латиница, цифры, . _ -). Пароль: от 8 символов.',
      tgToggle: 'Войти через Telegram',
      errLoginLink: 'Ссылка для входа не подходит. Возьмите свежую в профиле или попросите новую у администратора.',
      errInviteUsed: 'Это приглашение уже использовано или недействительно. Если вы уже регистрировались — откройте вашу ссылку для входа или войдите через Telegram.',
      welcomeTitle: 'Добро пожаловать! 🎉',
      welcomeText: 'Кабинет создан и запомнил вас в этом браузере. Ниже в разделе «Логин и пароль» придумайте себе логин и пароль — по ним удобно входить с любого устройства. А пока сохраните ссылку для входа (никому её не передавайте).',
      welcomeNext: 'Дальше: введите имя устройства (например, «Телефон»), нажмите «Добавить», затем «Скачать файл» и откройте его в WireGuard или AmneziaVPN. Подробно — «❓ Как подключиться».',
      loginLinkTitle: 'Ваша ссылка для входа',
      btnDownload: 'Скачать файл',
      loginFoot: 'Вход подтверждает Telegram, мы не видим ваш пароль — его здесь просто нет.',
      openInTg: 'Не получается войти? Открыть в Telegram',
      back: 'Назад ко входу',
      logout: 'Выйти',
      errNoBot: 'Бот ещё не настроен. Попросите администратора задать FIBR_TELEGRAM_BOT_TOKEN.',
      lblDevices: 'Устройства', lblOnline: 'Онлайн', lblTraffic: 'Трафик',
      phName: 'Название устройства (телефон, ноутбук…)',
      devicesHint: 'Одно устройство = один телефон, ноутбук или ТВ. Добавьте по одному на каждый. Не знаете, как подключить? Нажмите «Как подключиться» — там пошагово для WireGuard, AmneziaVPN, QR и телевизора.',
      protoWG: 'WireGuard — быстрый', protoVless: 'VLESS — если блокируют', protoVlessOff: 'VLESS (выключен на сервере)',
      protoAWG: 'AmneziaWG — для AmneziaVPN', protoAWGOff: 'AmneziaWG (выключен на сервере)',
      qrHintAWG: 'Конфиг AmneziaWG — отсканируйте в приложении AmneziaVPN («QR-код») или скачайте файл',
      protoHint: 'WireGuard — обычный и самый быстрый (WireGuard, AmneziaVPN, Fibr). VLESS — если WireGuard блокируют (v2rayNG, Hiddify, AmneziaVPN). AmneziaWG — WireGuard с маскировкой, только для AmneziaVPN.',
      btnSendTg: 'Файл в Telegram', sentTg: 'Файл отправлен в чат с ботом ✅',
      errSendTg: 'Не удалось отправить. Откройте чат с ботом в Telegram, нажмите «Старт» и повторите.',
      addDevice: 'Добавить',
      online: 'онлайн', offline: 'офлайн', disabled: 'отключено', never: 'не подключалось',
      lastSeen: 'было(а) {ago}', rx: '↓ {v}', tx: '↑ {v}',
      btnQR: 'QR-код', btnConfig: 'Конфиг', btnDelete: 'Удалить',
      confirmDelete: 'Удалить устройство {name}?',
      errAuth: 'Не удалось войти через Telegram. Сообщите админу.',
      errDisabled: 'Ваш аккаунт отключён администратором.',
      errExpired: 'Подписка истекла. Свяжитесь с администратором, чтобы продлить.',
      errInvite: 'Регистрация только по приглашению. Попросите у админа ссылку-приглашение и откройте её.',
      errGeneric: 'Ошибка запроса',
      deviceLimit: 'Лимит устройств исчерпан',
      validUntil: 'Действует до {d}',
      unlimited: 'Бессрочно',
      shortExpired: 'истёк',
      validUntilShort: 'до {d}',
      empty: 'Пока нет устройств.\nДобавьте первое — затем «Файл в Telegram» (откроете файл на телефоне) или «QR-код» (отсканируете телефоном с этого экрана).',
      qrTitle: 'QR-код конфигурации',
      qrHint: 'Отсканируйте в приложении WireGuard/Fibr',
      qrHintXray: 'Ссылка VLESS — отсканируйте в клиенте Xray (v2rayNG, Hiddify, Streisand)',
      qrStep1: 'Установите приложение WireGuard (или клиент Fibr)',
      qrStep2: 'Нажмите «+» и выберите «Создать из QR-кода»',
      qrStep3: 'Наведите камеру на код',
      configTitle: 'Конфигурация',
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
      keyTitle: 'Ссылка для входа',
      keyHint: 'Личная ссылка: входит в этот кабинет с любого устройства и в приложение Fibr (вставьте её в «Ключ доступа»). Никому не передавайте.',
      keyExists: 'ключ создан',
      keyNone: 'ключа нет',
      keyGen: 'Сгенерировать',
      keyRegen: 'Сгенерировать заново',
      keyRevoke: 'Отозвать',
      keyModalTitle: 'Ваша ссылка для входа',
      keyWarning: 'Показывается только один раз — сохраните сразу (в заметки или «Избранное»). Прежняя ссылка перестанет работать.',
      keyHowTo: 'Откройте её на другом телефоне или компьютере — вы сразу войдёте. В приложении Fibr: «Ключ доступа» → вставьте эту ссылку.',
      keyConfirmRevoke: 'Отозвать ключ? Вход в приложение по нему перестанет работать.',
      keyRevoked: 'Ключ отозван'
    },
    en: {
      greeting: 'Hi, {name}!',
      loginHint: 'Sign in with your login and password. First time — open the invite link or the login link from your admin.',
      loginLinkField: 'Paste your login link', loginGo: 'Sign in',
      pwLogin: 'Login', pwPass: 'Password', pwGo: 'Sign in',
      codeToggle: 'I have a code or a login link',
      errPw: 'Wrong login or password.', errPwMany: 'Too many attempts — wait 15 minutes.',
      pwTitle: 'Login and password',
      pwHintNone: 'Choose a login and password — they sign you in from any device, no codes and no Telegram.',
      pwHintSet: 'Your login: {login}. You can change the login or password.',
      pwNewLogin: 'Login (latin letters, digits, . _ -)', pwNewPass: 'Password (8+ characters)', pwSave: 'Save',
      pwSaved: 'Saved ✅ Sign in with your login and password from now on.',
      errPwTaken: 'This login is taken — pick another.',
      errPwBad: 'Login: 3–32 chars (latin letters, digits, . _ -). Password: 8+ characters.',
      tgToggle: 'Sign in with Telegram',
      errLoginLink: 'This login link does not work. Get a fresh one in your profile or ask the admin for a new one.',
      errInviteUsed: 'This invite is already used or invalid. If you already signed up — open your login link or sign in with Telegram.',
      welcomeTitle: 'Welcome! 🎉',
      welcomeText: 'Your account is created and remembered in this browser. Below, in “Login and password”, choose your own login and password — the easy way to sign in from any device. Meanwhile save your login link (do not share it).',
      welcomeNext: 'Next: type a device name (e.g. “Phone”), tap “Add”, then “Download file” and open it in WireGuard or AmneziaVPN. Step by step — “❓ How to connect”.',
      loginLinkTitle: 'Your login link',
      btnDownload: 'Download file',
      loginFoot: 'Telegram confirms the sign-in; there is simply no password here to leak.',
      openInTg: 'Cannot sign in? Open in Telegram',
      back: 'Back to sign in',
      logout: 'Sign out',
      errNoBot: 'The bot is not configured yet. Ask the administrator to set FIBR_TELEGRAM_BOT_TOKEN.',
      lblDevices: 'Devices', lblOnline: 'Online', lblTraffic: 'Traffic',
      phName: 'Device name (phone, laptop…)',
      devicesHint: 'One device = one phone, laptop or TV. Add one for each. Not sure how to connect? Tap “How to connect” — step by step for WireGuard, AmneziaVPN, QR and TV.',
      protoWG: 'WireGuard — fast', protoVless: 'VLESS — if blocked', protoVlessOff: 'VLESS (off on this server)',
      protoAWG: 'AmneziaWG — for AmneziaVPN', protoAWGOff: 'AmneziaWG (off on this server)',
      qrHintAWG: 'AmneziaWG config — scan it in the AmneziaVPN app (“QR code”) or download the file',
      protoHint: 'WireGuard is the normal, fastest one (WireGuard, AmneziaVPN, Fibr apps). VLESS is for networks that block WireGuard (v2rayNG, Hiddify, AmneziaVPN). AmneziaWG is masked WireGuard, AmneziaVPN only.',
      btnSendTg: 'File to Telegram', sentTg: 'The file is in your chat with the bot ✅',
      errSendTg: 'Could not send. Open the bot chat in Telegram, press Start and try again.',
      addDevice: 'Add',
      online: 'online', offline: 'offline', disabled: 'disabled', never: 'never connected',
      lastSeen: 'last seen {ago}', rx: '↓ {v}', tx: '↑ {v}',
      btnQR: 'QR code', btnConfig: 'Config', btnDelete: 'Delete',
      confirmDelete: 'Delete device {name}?',
      errAuth: 'Telegram sign-in failed. Contact the admin.',
      errDisabled: 'Your account is disabled by the administrator.',
      errExpired: 'Your subscription has expired. Contact the administrator to renew it.',
      errInvite: 'Sign-up is invite-only. Ask the admin for an invite link and open it.',
      errGeneric: 'Request failed',
      deviceLimit: 'Device limit reached',
      validUntil: 'Valid until {d}',
      unlimited: 'Unlimited',
      shortExpired: 'expired',
      validUntilShort: 'until {d}',
      empty: 'No devices yet.\nAdd one, then “File to Telegram” (open the file on your phone) or “QR code” (scan it with your phone from this screen).',
      qrTitle: 'Configuration QR code',
      qrHint: 'Scan it in the WireGuard/Fibr app',
      qrHintXray: 'VLESS link — scan it with an Xray client (v2rayNG, Hiddify, Streisand)',
      qrStep1: 'Install the WireGuard app (or the Fibr client)',
      qrStep2: 'Tap “+” and choose “Create from QR code”',
      qrStep3: 'Point your camera at the code',
      configTitle: 'Configuration',
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
      keyTitle: 'Login link',
      keyHint: 'Your personal link: signs in to this cabinet from any device and to the Fibr app (paste it into “Access key”). Do not share it.',
      keyExists: 'key created',
      keyNone: 'no key',
      keyGen: 'Generate',
      keyRegen: 'Regenerate',
      keyRevoke: 'Revoke',
      keyModalTitle: 'Your login link',
      keyWarning: 'Shown only once — save it now (notes or bookmarks). The previous link stops working.',
      keyHowTo: 'Open it on another phone or computer — you are signed in right away. In the Fibr app: “Access key” → paste this link.',
      keyConfirmRevoke: 'Revoke the key? Sign-in with it will stop working.',
      keyRevoked: 'Key revoked'
    }
  };

  var t = window.Fibr.makeT(DICT);
  var esc = window.Fibr.esc;
  var fmtBytes = window.Fibr.fmtBytes;
  var fmtAgo = window.Fibr.fmtAgo;
  var fmtMoney = window.Fibr.fmtMoney;

  var TOKEN_KEY = 'fibr-token';
  var INVITE_KEY = 'fibr-invite';
  var REF_KEY = 'fibr-ref';
  var LOGIN_KEY = 'fibr-login-key'; // personal access key = login link, remembered in this browser
  var token = null;
  var me = null;
  var api = window.Fibr.makeApi(function () { return token; });

  var el = function (id) { return document.getElementById(id); };

  /* ---------- static i18n ---------- */

  el('login-hint').textContent = t('loginHint');
  el('login-foot').textContent = t('loginFoot');
  el('back').textContent = t('back');
  el('logout').textContent = t('logout');
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

  /* ---------- views ---------- */

  function showView(id) {
    ['login-view', 'error-view', 'app-view'].forEach(function (v) {
      el(v).classList.toggle('hidden', v !== id);
    });
  }

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

  // The "subscription expired" screen gets the owner's contact line appended
  // (settings.contact_info via /api/public/info) so the user sees who to pay.
  function showExpired() {
    el('error-text').textContent = t('errExpired'); // docs/billing.md
    showView('error-view');
    fetch('/api/public/info').then(function (res) { return res.ok ? res.json() : null; }).then(function (info) {
      if (info && info.contact_info) {
        el('error-text').innerHTML =
          esc(t('errExpired')) + '<br><br><b>' + esc(t('refContactLabel')) + ':</b><br>' +
          esc(info.contact_info).replace(/\n/g, '<br>');
      }
    }).catch(function () { /* keep the plain message */ });
  }

  function fail(e) {
    if (e && e.status === 403 && e.message === 'subscription expired') {
      showExpired();
      return;
    }
    if (e && e.status === 401) { // stale session token
      token = null;
      try { localStorage.removeItem(TOKEN_KEY); } catch (er) { /* ignore */ }
      var k = storedKey();
      if (k) { keyLogin(k); return; } // silently back in with the login link
      showLogin();
      return;
    }
    alert((e && e.message) || t('errGeneric'));
  }

  /* ---------- auth: Telegram Login Widget ---------- */

  // The widget redirects back to <origin>/cabinet/?id=…&hash=… — the same
  // query string the server verifies (flat-fields scheme). ?invite= and
  // ?ref= links are stashed in localStorage because the redirect drops
  // extra params.
  function widgetParams() {
    var p = new URLSearchParams(window.location.search);
    return p.has('hash') && p.has('id') ? window.location.search.slice(1) : null;
  }

  function stashParams() {
    var p = new URLSearchParams(window.location.search);
    var inv = p.get('invite');
    var ref = p.get('ref');
    try {
      if (inv) localStorage.setItem(INVITE_KEY, inv);
      if (ref) localStorage.setItem(REF_KEY, ref);
    } catch (e) { /* ignore */ }
    if (inv || ref) history.replaceState(null, '', '/cabinet/');
  }

  function mountWidget() {
    fetch('/api/public/info').then(function (res) { return res.json(); }).then(function (info) {
      if (!info.bot_username) {
        el('login-hint').textContent = t('errNoBot');
        return;
      }
      var s = document.createElement('script');
      s.src = 'https://telegram.org/js/telegram-widget.js?22';
      s.setAttribute('data-telegram-login', info.bot_username);
      s.setAttribute('data-size', 'large');
      s.setAttribute('data-userpic', 'false');
      s.setAttribute('data-radius', '10');
      s.setAttribute('data-auth-url', window.location.origin + '/cabinet/');
      s.async = true;
      el('tg-widget').appendChild(s);
      // Fallback when the widget cannot work ("Bot domain invalid" until
      // /setdomain is done in @BotFather, blocked telegram.org…): the same
      // invite/referral through the bot, which opens the Mini App.
      var inv = null, ref = null;
      try { inv = localStorage.getItem(INVITE_KEY); ref = localStorage.getItem(REF_KEY); } catch (e) { /* ignore */ }
      var start = inv ? 'inv_' + inv : (ref ? 'ref_' + ref : '');
      var a = document.createElement('a');
      a.href = 'https://t.me/' + encodeURIComponent(info.bot_username) + (start ? '?start=' + encodeURIComponent(start) : '');
      a.className = 'btn btn--tertiary btn--small';
      a.style.display = 'inline-block';
      a.style.marginTop = '12px';
      a.textContent = t('openInTg');
      el('tg-widget').parentNode.insertBefore(a, el('tg-widget').nextSibling);
    }).catch(function () {
      el('login-hint').textContent = t('errAuth');
    });
  }

  /* ---------- auth: invite link / personal login link (main door) ---------- */

  function storedKey() {
    try { return localStorage.getItem(LOGIN_KEY); } catch (e) { return null; }
  }
  function rememberKey(k) {
    try { localStorage.setItem(LOGIN_KEY, k); } catch (e) { /* ignore */ }
  }
  function loginLinkFor(k) { return window.location.origin + '/cabinet/#k=' + k; }

  function startSession(d) {
    token = d.token;
    try { localStorage.setItem(TOKEN_KEY, token); } catch (e) { /* ignore */ }
  }

  // Sign in with the personal access key (= the login link).
  function keyLogin(k, fromForm) {
    fetch('/api/auth/key', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key: k })
    }).then(function (res) {
      return res.json().then(function (d) {
        if (res.ok) return d;
        if (res.status === 403 && d && d.error === 'subscription expired') { showExpired(); throw new Error('expired'); }
        if (res.status === 403) { el('error-text').textContent = t('errDisabled'); showView('error-view'); throw new Error('disabled'); }
        throw new Error('badkey');
      });
    }).then(function (d) {
      rememberKey(k);
      startSession(d);
      load();
    }).catch(function (e) {
      if (e && (e.message === 'expired' || e.message === 'disabled')) return;
      try { localStorage.removeItem(LOGIN_KEY); } catch (er) { /* ignore */ }
      showLogin();
      if (fromForm || (e && e.message === 'badkey')) el('login-hint').textContent = t('errLoginLink');
    });
  }

  // Opening an invite link creates the account right away (no Telegram).
  function inviteSignup(code) {
    var ref = null;
    try { ref = localStorage.getItem(REF_KEY); } catch (e) { /* ignore */ }
    fetch('/api/auth/invite', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ invite_code: code, referral_code: ref || undefined })
    }).then(function (res) {
      return res.json().then(function (d) {
        if (!res.ok) throw new Error(res.status === 403 ? 'invite' : ((d && d.error) || 'auth failed'));
        return d;
      });
    }).then(function (d) {
      try { localStorage.removeItem(INVITE_KEY); } catch (e) { /* ignore */ }
      rememberKey(d.key);
      startSession(d);
      load();
      welcomeModal(loginLinkFor(d.key));
    }).catch(function (e) {
      try { localStorage.removeItem(INVITE_KEY); } catch (er) { /* ignore */ }
      if (e && e.message === 'invite') {
        el('error-text').textContent = t('errInviteUsed');
        showView('error-view');
        return;
      }
      el('error-text').textContent = t('errAuth');
      showView('error-view');
    });
  }

  function linkBox(link) {
    return '<div class="key-value mono" id="login-link-value" title="' + esc(t('copy')) + '" style="word-break:break-all">' + esc(link) + '</div>';
  }

  function welcomeModal(link) {
    openModal('<h3>' + esc(t('welcomeTitle')) + '</h3>' +
      '<p class="muted modal-sub">' + esc(t('welcomeText')) + '</p>' +
      '<p class="muted"><b>' + esc(t('loginLinkTitle')) + '</b></p>' + linkBox(link) +
      '<p class="muted">' + esc(t('welcomeNext')) + '</p>' +
      '<div class="modal-actions">' +
      '<button class="btn btn--tertiary btn--small" id="wl-copy">' + esc(t('copy')) + '</button>' +
      '<button class="btn" id="wl-close">' + esc(t('close')) + '</button></div>');
    el('wl-copy').addEventListener('click', function () {
      navigator.clipboard.writeText(link).then(function () { el('wl-copy').textContent = t('copied'); });
    });
    el('wl-close').addEventListener('click', closeModal);
  }

  // Login view: paste a login link (or key / invite link); Telegram is the
  // optional second way, mounted only on demand.
  function showLogin() {
    showView('login-view');
  }
  el('login-link').placeholder = t('loginLinkField');
  el('login-go').textContent = t('loginGo');
  el('tg-toggle').textContent = t('tgToggle');
  el('login-go').addEventListener('click', function () {
    var raw = (el('login-link').value || '').trim();
    if (!raw) { el('login-link').focus(); return; }
    var inv = raw.match(/[?&]invite=([A-Za-z0-9]+)/);
    if (inv) { inviteSignup(inv[1]); return; }
    var k = raw.match(/#k=([A-Za-z0-9]+)/);
    keyLogin(k ? k[1] : raw, true);
  });
  el('pw-login').placeholder = t('pwLogin');
  el('pw-pass').placeholder = t('pwPass');
  el('pw-go').textContent = t('pwGo');
  el('code-toggle').textContent = t('codeToggle');
  el('pw-form').addEventListener('submit', function (ev) {
    ev.preventDefault();
    var login = (el('pw-login').value || '').trim();
    var pass = el('pw-pass').value || '';
    if (!login || !pass) return;
    fetch('/api/auth/password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ login: login, password: pass })
    }).then(function (res) {
      return res.json().then(function (d) {
        if (res.ok) return d;
        if (res.status === 429) throw new Error(t('errPwMany'));
        if (res.status === 403 && d && d.error === 'subscription expired') { showExpired(); throw new Error(''); }
        if (res.status === 403) throw new Error(t('errDisabled'));
        throw new Error(t('errPw'));
      });
    }).then(function (d) {
      el('pw-pass').value = '';
      startSession(d);
      load();
    }).catch(function (e) {
      if (e && e.message) el('login-hint').textContent = e.message;
    });
  });

  // Profile: set / change the own login + password.
  el('pw-title').textContent = t('pwTitle');
  el('pw-new-login').placeholder = t('pwNewLogin');
  el('pw-new-pass').placeholder = t('pwNewPass');
  el('pw-save').textContent = t('pwSave');
  function loadPwSection() {
    el('pw-view').classList.remove('hidden');
    var login = (me && me.login) || '';
    el('pw-hint').textContent = login ? t('pwHintSet', { login: login }) : t('pwHintNone');
    if (login && !el('pw-new-login').value) el('pw-new-login').value = login;
  }
  el('pw-set').addEventListener('submit', function (ev) {
    ev.preventDefault();
    var login = (el('pw-new-login').value || '').trim();
    var pass = el('pw-new-pass').value || '';
    api('/api/me/password', { method: 'POST', body: { login: login, password: pass } }).then(function () {
      el('pw-new-pass').value = '';
      alert(t('pwSaved'));
      load();
    }).catch(function (e) {
      if (e && e.status === 409) { alert(t('errPwTaken')); return; }
      if (e && e.status === 400) { alert(t('errPwBad')); return; }
      fail(e);
    });
  });

  var widgetMounted = false;
  el('tg-toggle').addEventListener('click', function () {
    el('tg-widget').classList.remove('hidden');
    if (!widgetMounted) { widgetMounted = true; mountWidget(); }
  });

  function signIn() {
    var data = widgetParams();
    if (!data) {
      showLogin();
      return;
    }
    var body = { init_data: data };
    try {
      var inv = localStorage.getItem(INVITE_KEY);
      if (inv) body.invite_code = inv;
    } catch (e) { /* ignore */ }
    try {
      var ref = localStorage.getItem(REF_KEY);
      if (ref) body.referral_code = ref;
    } catch (e) { /* ignore */ }
    fetch('/api/auth/telegram', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }).then(function (res) {
      return res.json().then(function (d) {
        if (!res.ok) {
          var msg = d && d.error ? d.error : 'auth failed';
          if (res.status === 403 && (msg === 'invite required' || msg === 'invalid invite')) {
            el('error-text').textContent = t('errInvite');
            showView('error-view');
            throw new Error('invite');
          }
          if (res.status === 403 && msg === 'subscription expired') {
            showExpired();
            throw new Error('expired');
          }
          if (res.status === 403) {
            el('error-text').textContent = t('errDisabled');
            showView('error-view');
            throw new Error('disabled');
          }
          throw new Error(msg);
        }
        return d;
      });
    }).then(function (d) {
      token = d.token;
      try { localStorage.setItem(TOKEN_KEY, token); } catch (e) { /* ignore */ }
      try {
        localStorage.removeItem(INVITE_KEY);
        localStorage.removeItem(REF_KEY);
      } catch (e) { /* ignore */ }
      history.replaceState(null, '', '/cabinet/'); // drop signed params from the URL
      load();
    }).catch(function (e) {
      if (e && (e.message === 'disabled' || e.message === 'invite' || e.message === 'expired')) return;
      el('error-text').textContent = t('errAuth');
      showView('error-view');
    });
  }

  el('back').addEventListener('click', function () {
    showLogin();
  });

  el('logout').addEventListener('click', function () {
    token = null;
    try {
      localStorage.removeItem(TOKEN_KEY);
      localStorage.removeItem(LOGIN_KEY); // the login link still works elsewhere
    } catch (e) { /* ignore */ }
    location.reload();
  });

  /* ---------- data (in sync with the Mini App) ---------- */

  function load() {
    api('/api/me').then(function (data) {
      me = data;
      // Both protocols are always listed; VLESS is greyed out (with the
      // reason) only when the server has it switched off.
      var proto = document.getElementById('new-proto');
      var wgOpt = proto.querySelector('option[value="wg"]');
      var xOpt = proto.querySelector('option[value="xray"]');
      wgOpt.textContent = t('protoWG');
      xOpt.textContent = me.xray_enabled ? t('protoVless') : t('protoVlessOff');
      xOpt.disabled = !me.xray_enabled;
      var aOpt = proto.querySelector('option[value="awg"]');
      aOpt.textContent = me.awg_enabled ? t('protoAWG') : t('protoAWGOff');
      aOpt.disabled = !me.awg_enabled;
      if ((proto.value === 'xray' && !me.xray_enabled) || (proto.value === 'awg' && !me.awg_enabled)) proto.value = 'wg';
      proto.title = t('protoHint');
      el('greeting').textContent = t('greeting', { name: (data.user && data.user.username) || '' });
      showView('app-view');
      render();
      loadUsersSection();
      loadRefSection();
      loadKeySection();
      loadPwSection();
    }).catch(fail);
  }

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

  function render() {
    var devices = me.devices || [];
    var online = devices.filter(function (d) { return d.online; }).length;
    var rx = 0, tx = 0;
    devices.forEach(function (d) { rx += d.total_rx; tx += d.total_tx; });

    el('sum-devices').textContent = devices.length + (me.max_devices ? ' / ' + me.max_devices : '');
    el('sum-online').textContent = online;
    el('sum-traffic').textContent = fmtBytes(rx + tx);
    el('sum-traffic').title = '↓ ' + fmtBytes(rx) + ' · ↑ ' + fmtBytes(tx);
    el('sum-expiry').textContent = (me.user && me.user.expires_at)
      ? t('validUntil', { d: fmtDate(me.user.expires_at) })
      : t('unlimited');

    if (!devices.length) {
      el('devices').innerHTML = '<div class="card empty">' + esc(t('empty')).replace(/\n/g, '<br>') +
        '<br><button class="btn btn--small" id="guide-empty" style="margin-top:12px">❓ ' + esc(window.Fibr.guideTitle(t.lang)) + '</button></div>';
      el('guide-empty').addEventListener('click', showGuide);
      return;
    }

    el('devices').innerHTML = devices.map(function (d) {
      var pill = d.enabled ? (d.online ? 'pill--ok' : 'pill--off') : 'pill--danger';
      var label = d.enabled ? (d.online ? t('online') : t('offline')) : t('disabled');
      var ago = d.last_handshake ? fmtAgo(d.last_handshake, t.lang) : null;
      var seen = ago ? t('lastSeen', { ago: ago }) : t('never');
      var proto = d.protocol === 'xray'
        ? '<span class="pill pill--live proto-pill">VLESS</span>'
        : d.protocol === 'awg'
          ? '<span class="pill pill--live proto-pill">AWG</span>'
          : '<span class="pill pill--off proto-pill">WG</span>';
      return '<div class="card device">' +
        '<div class="device-top">' +
        '<span class="dot ' + (d.online ? 'dot--on' : 'dot--off') + '"></span>' +
        '<div class="grow"><h3>' + esc(d.name) + '</h3>' +
        '<span class="ip mono">' + esc(d.ipv4) + '</span>' + proto + '</div>' +
        '<span class="pill ' + pill + '">' + label + '</span></div>' +
        '<div class="device-meta"><span>' + esc(seen) + '</span>' +
        '<span>' + t('rx', { v: fmtBytes(d.total_rx) }) + '</span>' +
        '<span>' + t('tx', { v: fmtBytes(d.total_tx) }) + '</span></div>' +
        '<div class="device-actions">' +
        '<button class="btn btn--secondary btn--small" data-act="qr" data-id="' + d.id + '">' + t('btnQR') + '</button>' +
        '<button class="btn btn--small" data-act="dl" data-id="' + d.id + '">' + t('btnDownload') + '</button>' +
        (me.user && me.user.telegram_id ? '<button class="btn btn--secondary btn--small" data-act="send" data-id="' + d.id + '">' + t('btnSendTg') + '</button>' : '') +
        '<button class="btn btn--tertiary btn--small" data-act="cfg" data-id="' + d.id + '">' + t('btnConfig') + '</button>' +
        '<button class="btn btn--danger-ghost btn--small" data-act="del" data-id="' + d.id + '" data-name="' + esc(d.name) + '">' + t('btnDelete') + '</button>' +
        '</div></div></div>';
    }).join('');
  }

  el('add-device').addEventListener('click', function () {
    var name = el('new-name').value.trim();
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

  // "Download file": the .conf (or the VLESS link as .txt) straight to this
  // device — on a phone it then opens in WireGuard / AmneziaVPN.
  function downloadConf(id) {
    var dev = (me.devices || []).filter(function (x) { return String(x.id) === String(id); })[0];
    fetch('/api/me/devices/' + id + '/config', { headers: { Authorization: 'Bearer ' + token } })
      .then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        var cd = res.headers.get('Content-Disposition') || '';
        var m = cd.match(/filename="([^"]+)"/);
        return res.text().then(function (text) { return { text: text, name: m ? m[1] : ('fibr-' + id + (dev && dev.protocol === 'xray' ? '.txt' : '.conf')) }; });
      })
      .then(function (f) {
        var a = document.createElement('a');
        a.href = URL.createObjectURL(new Blob([f.text], { type: 'application/octet-stream' }));
        a.download = f.name;
        document.body.appendChild(a);
        a.click();
        a.remove();
      })
      .catch(fail);
  }

  // The bot drops the device config into the user's Telegram chat; from
  // there it opens straight in WireGuard / AmneziaVPN on the phone.
  function sendToTelegram(id, btn) {
    btn.disabled = true;
    api('/api/me/devices/' + id + '/send', { method: 'POST', body: {} }).then(function () {
      btn.textContent = t('sentTg');
    }).catch(function () {
      btn.disabled = false;
      alert(t('errSendTg'));
    });
  }

  // "How to connect" walkthrough (shared with the Mini App: /shared/app.js).
  function showGuide() {
    openModal(window.Fibr.guideHTML(t.lang, { web: true }) +
      '<div class="modal-actions"><button class="btn" id="guide-close">' + esc(t('close')) + '</button></div>');
    el('guide-close').addEventListener('click', closeModal);
  }
  el('guide-open').textContent = '❓ ' + window.Fibr.guideTitle(t.lang);
  el('guide-open').addEventListener('click', showGuide);

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
    else if (act === 'send') sendToTelegram(id, btn);
    else if (act === 'dl') downloadConf(id);
    else if (act === 'del') {
      if (!confirm(t('confirmDelete', { name: btn.getAttribute('data-name') }))) return;
      api('/api/me/devices/' + id, { method: 'DELETE' }).then(load).catch(fail);
    }
  });

  /* ---------- reseller section (admins / superadmins only) ---------- */

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
        var expPill;
        if (u.expired) {
          expPill = '<span class="pill pill--danger">' + esc(t('shortExpired')) + '</span>';
        } else if (u.expires_at) {
          expPill = '<span class="pill pill--ok">' + esc(t('validUntilShort', { d: fmtDateShort(u.expires_at) })) + '</span>';
        } else {
          expPill = '<span class="pill pill--off">∞</span>';
        }
        return '<div class="card device">' +
          '<div class="device-top">' +
          '<div class="grow"><h3>' + esc(u.username || ('#' + u.id)) + '</h3>' +
          '<span class="muted mono user-tg">' + esc(tg) + '</span></div>' +
          '<span class="user-pills">' + expPill +
          '<span class="pill pill--off">' + esc(t('devicesOf', { n: counts[u.id] || 0 })) + '</span></span></div>' +
          '<div class="device-actions">' +
          '<button class="btn btn--tertiary btn--small btn--block" data-uact="grant" data-id="' + u.id + '">' + esc(t('btnGrantDevice')) + '</button>' +
          '</div></div>';
      }).join('');
    }).catch(function (e) {
      if (e && e.status === 403) { el('users-view').classList.add('hidden'); return; }
      fail(e);
    });
  }

  el('users').addEventListener('click', function (e) {
    var btn = e.target.closest('button[data-uact]');
    if (!btn) return;
    showGrantDevice(btn.getAttribute('data-id'));
  });

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

  // Same card as the Mini App: share link, terms, balance tiles, the invited
  // list, the owner's contact line for withdrawals (docs/referrals.md).
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

  // Kept in sync with the Mini App: the server stores only a hash, the
  // plaintext key is shown exactly once in the modal right after generation.
  // A repeat generate rotates the key; revoke clears it.
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
    rememberKey(key); // the rotated key is this browser's login now
    var link = loginLinkFor(key);
    openModal('<h3>' + esc(t('keyModalTitle')) + '</h3>' +
      '<p class="muted modal-sub key-warn">' + esc(t('keyWarning')) + '</p>' +
      '<div class="key-value mono" id="app-key" title="' + esc(t('copy')) + '" style="word-break:break-all">' + esc(link) + '</div>' +
      '<p class="muted">' + esc(t('keyHowTo')) + '</p>' +
      '<div class="modal-actions">' +
      '<button class="btn btn--tertiary btn--small" id="key-copy">' + esc(t('copy')) + '</button>' +
      '<button class="btn" id="key-close">' + esc(t('close')) + '</button></div>');
    function copyKey() {
      navigator.clipboard.writeText(link).then(function () { /* :active feedback */ });
    }
    el('app-key').addEventListener('click', copyKey);
    el('key-copy').addEventListener('click', function () {
      navigator.clipboard.writeText(link).then(function () {
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
    }).catch(fail);
  });

  el('key-revoke').addEventListener('click', function () {
    if (!confirm(t('keyConfirmRevoke'))) return;
    api('/api/me/access-key', { method: 'DELETE' }).then(function () {
      alert(t('keyRevoked'));
      load();
    }).catch(fail);
  });

  /* ---------- qr & config ---------- */

  function qrSteps(protocol) {
    if (protocol === 'xray' || protocol === 'awg') return ''; // the hint above says where
    return '<ol class="qr-steps muted">' +
      '<li>' + esc(t('qrStep1')) + '</li>' +
      '<li>' + esc(t('qrStep2')) + '</li>' +
      '<li>' + esc(t('qrStep3')) + '</li></ol>';
  }

  function qrModal(url, protocol) {
    var hint = protocol === 'xray' ? t('qrHintXray') : protocol === 'awg' ? t('qrHintAWG') : t('qrHint');
    openModal('<h3>' + esc(t('qrTitle')) + '</h3>' +
      '<p class="muted modal-sub">' + esc(hint) + '</p>' +
      '<div class="qr-wrap"><img src="' + url + '" alt="QR"></div>' +
      qrSteps(protocol) +
      '<div class="modal-actions"><button class="btn" onclick="document.getElementById(\'modal\').classList.add(\'hidden\')">' + esc(t('close')) + '</button></div>');
  }

  function showQR(id, protocol) {
    fetch('/api/me/devices/' + id + '/qr.png', { headers: { Authorization: 'Bearer ' + token } })
      .then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        return res.blob();
      })
      .then(function (blob) { qrModal(URL.createObjectURL(blob), protocol); })
      .catch(fail);
  }

  function showAdminQR(id, protocol) {
    fetch('/api/admin/devices/' + id + '/qr.png', { headers: { Authorization: 'Bearer ' + token } })
      .then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        return res.blob();
      })
      .then(function (blob) { qrModal(URL.createObjectURL(blob), protocol); })
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

  stashParams();
  // Personal login link: /cabinet/#k=KEY (the key never leaves the browser
  // in a URL the server logs).
  var hashKey = (window.location.hash.match(/k=([A-Za-z0-9]+)/) || [])[1];
  if (hashKey) history.replaceState(null, '', '/cabinet/');
  try { token = localStorage.getItem(TOKEN_KEY); } catch (e) { /* ignore */ }
  var pendingInvite = null;
  try { pendingInvite = localStorage.getItem(INVITE_KEY); } catch (e) { /* ignore */ }
  if (hashKey) {
    keyLogin(hashKey);
  } else if (widgetParams()) {
    signIn(); // back from the Telegram widget
  } else if (pendingInvite && !token) {
    inviteSignup(pendingInvite); // invite link opened: create the account
  } else if (token) {
    load();
  } else if (storedKey()) {
    keyLogin(storedKey());
  } else {
    signIn();
  }
  setInterval(function () {
    if (token && !el('app-view').classList.contains('hidden')) load();
  }, 20000);
})();
