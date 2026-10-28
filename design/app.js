/* Shared Fibr front-end helpers: theme, i18n, formatting, API client.
 * Served at /shared/app.js for both the admin panel and the Mini App. */
window.Fibr = (function () {
  'use strict';

  /* ---------- theme ---------- */

  function detectTheme() {
    var saved = null;
    try { saved = localStorage.getItem('fibr-theme'); } catch (e) { /* private mode */ }
    if (saved === 'dark' || saved === 'light') return saved;
    var tg = window.Telegram && window.Telegram.WebApp;
    if (tg && tg.colorScheme === 'light') return 'light';
    if (tg && tg.colorScheme === 'dark') return 'dark';
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches
      ? 'light' : 'dark';
  }

  function setTheme(t) {
    document.documentElement.setAttribute('data-theme', t);
    try { localStorage.setItem('fibr-theme', t); } catch (e) { /* ignore */ }
  }

  function initTheme() {
    setTheme(detectTheme());
    var btn = document.querySelector('[data-theme-toggle]');
    if (btn) {
      btn.addEventListener('click', function () {
        var cur = document.documentElement.getAttribute('data-theme');
        setTheme(cur === 'dark' ? 'light' : 'dark');
      });
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initTheme);
  } else {
    initTheme();
  }

  /* ---------- i18n ---------- */

  /* makeT(dict) -> t(key, vars). Language priority:
   * saved choice > Telegram user language > browser > en. */
  function makeT(dict) {
    var lang = (navigator.language || 'en').toLowerCase().slice(0, 2);
    var saved = null;
    try { saved = localStorage.getItem('fibr-lang'); } catch (e) { /* ignore */ }
    var tgUser = window.Telegram && window.Telegram.WebApp && window.Telegram.WebApp.initDataUnsafe && window.Telegram.WebApp.initDataUnsafe.user;
    if (saved) lang = saved;
    else if (tgUser && tgUser.language_code) lang = tgUser.language_code.toLowerCase().slice(0, 2);
    if (!dict[lang]) lang = 'en';

    function t(key, vars) {
      var s = (dict[lang] && dict[lang][key]) || dict.en[key] || key;
      if (vars) {
        Object.keys(vars).forEach(function (k) {
          s = s.split('{' + k + '}').join(String(vars[k]));
        });
      }
      return s;
    }
    t.lang = lang;
    return t;
  }

  /* ---------- formatting ---------- */

  function fmtBytes(b) {
    if (b == null || isNaN(b)) return '—';
    if (b < 1024) return b + ' B';
    var units = ['KB', 'MB', 'GB', 'TB', 'PB'];
    var u = -1;
    do { b /= 1024; u++; } while (b >= 1024 && u < units.length - 1);
    return b.toFixed(b >= 100 ? 0 : 1) + ' ' + units[u];
  }

  function fmtBits(bps) {
    if (bps == null || isNaN(bps) || bps <= 0) return '0';
    if (bps < 1000) return Math.round(bps) + ' bit/s';
    var units = ['kbit/s', 'Mbit/s', 'Gbit/s'];
    var u = -1;
    do { bps /= 1000; u++; } while (bps >= 1000 && u < units.length - 1);
    return bps.toFixed(bps >= 100 ? 0 : 1) + ' ' + units[u];
  }

  function fmtAgo(iso, lang) {
    if (!iso) return null;
    var sec = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
    if (isNaN(sec)) return null;
    if (sec < 60) return lang === 'ru' ? 'только что' : 'just now';
    var m = Math.floor(sec / 60);
    if (m < 60) return lang === 'ru' ? m + ' мин назад' : m + 'm ago';
    var h = Math.floor(m / 60);
    if (h < 24) return lang === 'ru' ? h + ' ч назад' : h + 'h ago';
    var d = Math.floor(h / 24);
    return lang === 'ru' ? d + ' дн назад' : d + 'd ago';
  }

  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  /* ---------- money (referral program; docs/referrals.md) ---------- */

  /* All amounts travel as INTEGER minor units (kopecks). toMinor parses the
   * human form into minor units without any float math; null = bad input. */
  function toMinor(s) {
    var m = String(s == null ? '' : s).trim().replace(',', '.').match(/^(\d+)(?:\.(\d{1,2}))?$/);
    if (!m) return null;
    return parseInt(m[1], 10) * 100 + (m[2] ? parseInt(m[2].padEnd(2, '0'), 10) : 0);
  }

  function fmtMoney(minor, currency) {
    if (minor == null || isNaN(minor)) return '—';
    var v = (Math.round(minor) / 100).toFixed(2);
    if (v.slice(-3) === '.00') v = v.slice(0, -3);
    return currency ? v + ' ' + currency : v;
  }

  /* ---------- api client ---------- */

  /* makeApi(getToken) -> (path, {method, body}) -> Promise<json>.
   * Non-2xx rejects with Error(.status, .message). */
  function makeApi(getToken) {
    return function (path, opts) {
      opts = opts || {};
      var headers = {};
      var tok = getToken();
      if (tok) headers['Authorization'] = 'Bearer ' + tok;
      if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
      return fetch(path, {
        method: opts.method || 'GET',
        headers: headers,
        body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined
      }).then(function (res) {
        if (res.status === 204) return null;
        return res.json().catch(function () { return {}; }).then(function (data) {
          if (!res.ok) {
            var e = new Error((data && data.error) || 'HTTP ' + res.status);
            e.status = res.status;
            throw e;
          }
          return data;
        });
      });
    };
  }

  /* ---------- "How to connect" guide (Mini App + cabinet) ----------
   * One plain-language walkthrough for non-technical people, shared by the
   * Mini App and the web cabinet (the bot and the Android app carry the
   * same steps in their own words). Sections are <details> so a person
   * opens only the path they need. */
  var GUIDE = {
    ru: {
      title: 'Как подключиться',
      web: ['📱 На этом телефоне — файлом (проще всего)', [
        'Установите приложение <b>WireGuard</b> (Google Play / App Store) или <b>AmneziaVPN</b>.',
        'Здесь введите имя устройства (например, «Телефон») и нажмите <b>«Добавить»</b>.',
        'У устройства нажмите <b>«Скачать файл»</b>.',
        'Откройте скачанный файл (в «Загрузках» или в уведомлении) → <b>WireGuard</b> или <b>AmneziaVPN</b>. В AmneziaVPN можно и так: «Файл с настройками подключения» → выберите этот файл.',
        'Нажмите <b>«Разрешить»</b>, когда телефон спросит про VPN, и включите переключатель.'
      ]],
      intro: 'Выберите удобный способ и откройте его. Одно устройство = один гаджет: для каждого телефона, ноутбука или ТВ создайте своё.',
      sections: [
        ['📱 На этом телефоне — файлом (проще всего)', [
          'Установите приложение <b>WireGuard</b> (Google Play / App Store) или <b>AmneziaVPN</b>.',
          'Нажмите у устройства кнопку <b>«Файл в Telegram»</b> (или напишите боту <b>/new</b>).',
          'Откройте чат с ботом и нажмите на пришедший файл <b>.conf</b>.',
          'Выберите <b>«Открыть в…»</b> → WireGuard или AmneziaVPN.',
          'Нажмите <b>«Разрешить»</b>, когда телефон спросит про VPN, и включите переключатель.'
        ]],
        ['🅰️ Через AmneziaVPN', [
          'Установите <b>AmneziaVPN</b> (Google Play, App Store или amnezia.org).',
          'Получите файл: кнопка <b>«Скачать файл»</b> у устройства (или «Файл в Telegram» / <b>/new</b> в боте).',
          'В AmneziaVPN нажмите <b>«Файл с настройками подключения»</b> и выберите этот файл (он в папке Telegram / Загрузки).',
          'Или нажмите <b>«QR-код»</b> и наведите камеру на QR с другого экрана.',
          'Нажмите <b>«Подключиться»</b>.'
        ]],
        ['🔳 По QR-коду (нужен второй экран)', [
          'Откройте этот кабинет на компьютере или другом телефоне.',
          'У устройства нажмите <b>«QR-код»</b>.',
          'На телефоне в WireGuard: <b>«+»</b> → <b>«Создать из QR-кода»</b>. В AmneziaVPN: <b>«QR-код»</b>.',
          'Наведите камеру на QR, разрешите VPN и включите переключатель.'
        ]],
        ['🤖 Приложение Fibr (Android)', [
          'Напишите боту <b>/app</b> — придёт файл приложения. Нажмите на него и разрешите установку.',
          'Откройте Fibr → <b>«Добавить»</b> → <b>«Вставить / файл»</b>.',
          'Нажмите <b>«Выбрать файл»</b> (файл .conf из Telegram) или <b>«Из буфера»</b>, если скопировали конфиг или код.',
          'Можно и проще: нажмите на файл .conf в Telegram → <b>«Открыть в…»</b> → Fibr.'
        ]],
        ['📺 Телевизор (Android TV)', [
          'Установите на ТВ приложение Fibr (файл из команды <b>/app</b> — через флешку или «Send Files to TV»).',
          'Откройте Fibr на ТВ — на экране появится <b>QR-код</b> и короткий код.',
          'Отсканируйте QR камерой телефона → откроется бот → «Открыть Fibr» → <b>«Новое устройство для ТВ»</b>.',
          'Без камеры: здесь нажмите <b>«📺 Подключить телевизор»</b> и введите код с экрана ТВ.',
          'Телевизор подключится сам через пару секунд.'
        ]],
        ['✅ Проверить и если не работает', [
          'Откройте сайт <b>2ip.ru</b>: должен показываться IP сервера, а не ваш домашний.',
          'Не работает — выключите и включите VPN в приложении.',
          'Один файл не работает на двух телефонах одновременно: создайте для второго своё устройство.',
          'Если WireGuard блокируют — напишите боту <b>/vless</b> (или создайте здесь устройство с протоколом <b>Xray</b>) и вставьте ссылку в AmneziaVPN («Вставить ключ»), v2rayNG, Hiddify или Streisand.',
          'Не получилось — напишите администратору.'
        ]]
      ]
    },
    en: {
      title: 'How to connect',
      web: ['📱 On this phone — with a file (easiest)', [
        'Install <b>WireGuard</b> (Google Play / App Store) or <b>AmneziaVPN</b>.',
        'Here, type a device name (e.g. “Phone”) and tap <b>“Add”</b>.',
        'Tap <b>“Download file”</b> on the device.',
        'Open the downloaded file (Downloads or the notification) → <b>WireGuard</b> or <b>AmneziaVPN</b>. In AmneziaVPN you can also tap “File with connection settings” and pick it.',
        'Tap <b>“Allow”</b> when the phone asks about VPN and switch it on.'
      ]],
      intro: 'Pick the way that suits you and open it. One device = one gadget: create a separate one for each phone, laptop or TV.',
      sections: [
        ['📱 On this phone — with a file (easiest)', [
          'Install <b>WireGuard</b> (Google Play / App Store) or <b>AmneziaVPN</b>.',
          'Tap <b>“File to Telegram”</b> on the device (or send <b>/new</b> to the bot).',
          'Open the bot chat and tap the <b>.conf</b> file.',
          'Choose <b>“Open in…”</b> → WireGuard or AmneziaVPN.',
          'Tap <b>“Allow”</b> when the phone asks about VPN and switch it on.'
        ]],
        ['🅰️ With AmneziaVPN', [
          'Install <b>AmneziaVPN</b> (Google Play, App Store or amnezia.org).',
          'Get the file: <b>“Download file”</b> on the device (or “File to Telegram” / <b>/new</b> in the bot).',
          'In AmneziaVPN tap <b>“File with connection settings”</b> and pick that file (Telegram / Downloads folder).',
          'Or tap <b>“QR code”</b> and point the camera at the QR on another screen.',
          'Tap <b>“Connect”</b>.'
        ]],
        ['🔳 With a QR code (needs a second screen)', [
          'Open this page on a computer or another phone.',
          'Tap <b>“QR code”</b> on the device.',
          'On the phone in WireGuard: <b>“+”</b> → <b>“Create from QR code”</b>. In AmneziaVPN: <b>“QR code”</b>.',
          'Point the camera at the QR, allow the VPN and switch it on.'
        ]],
        ['🤖 The Fibr app (Android)', [
          'Send <b>/app</b> to the bot — the app arrives as a file. Tap it and allow the install.',
          'Open Fibr → <b>“Add”</b> → <b>“Paste / file”</b>.',
          'Tap <b>“Choose file”</b> (the .conf from Telegram) or <b>“From clipboard”</b> if you copied a config or code.',
          'Even simpler: tap the .conf in Telegram → <b>“Open in…”</b> → Fibr.'
        ]],
        ['📺 TV (Android TV)', [
          'Install Fibr on the TV (the file from <b>/app</b> — via a USB stick or “Send Files to TV”).',
          'Open Fibr on the TV — a <b>QR code</b> and a short code appear.',
          'Scan the QR with your phone camera → the bot opens → “Open Fibr” → <b>“New device for the TV”</b>.',
          'No camera: tap <b>“📺 Connect a TV”</b> here and type the code from the TV.',
          'The TV connects by itself in a couple of seconds.'
        ]],
        ['✅ Check it, and if it does not work', [
          'Open <b>2ip.ru</b>: it should show the server IP, not your home one.',
          'Not working — switch the VPN off and on again.',
          'One file does not work on two phones at once: create a separate device for the second one.',
          'If WireGuard is blocked — send <b>/vless</b> to the bot (or create an <b>Xray</b> device here) and paste the link into AmneziaVPN (“Paste key”), v2rayNG, Hiddify or Streisand.',
          'Still stuck — message the administrator.'
        ]]
      ]
    }
  };

  // guideHTML renders the guide; opts.firstOpen opens the first section,
  // opts.web swaps the first section for the browser path ("Download file"
  // instead of the Telegram bot — the web cabinet is the main door).
  // Strings are trusted constants above (they carry <b> markup).
  function guideHTML(lang, opts) {
    var g = GUIDE[lang === 'ru' ? 'ru' : 'en'];
    var first = !opts || opts.firstOpen !== false;
    var sections = opts && opts.web ? [g.web].concat(g.sections.slice(1)) : g.sections;
    return '<h3>' + g.title + '</h3>' +
      '<p class="muted modal-sub">' + g.intro + '</p>' +
      '<div class="guide">' + sections.map(function (sec, i) {
        return '<details class="guide-sec"' + (i === 0 && first ? ' open' : '') + '>' +
          '<summary>' + sec[0] + '</summary>' +
          '<ol class="qr-steps">' + sec[1].map(function (step) { return '<li>' + step + '</li>'; }).join('') + '</ol>' +
          '</details>';
      }).join('') + '</div>';
  }

  function guideTitle(lang) { return GUIDE[lang === 'ru' ? 'ru' : 'en'].title; }

  return { setTheme: setTheme, makeT: makeT, fmtBytes: fmtBytes, fmtBits: fmtBits, fmtAgo: fmtAgo, esc: esc, makeApi: makeApi, toMinor: toMinor, fmtMoney: fmtMoney, guideHTML: guideHTML, guideTitle: guideTitle };
})();
