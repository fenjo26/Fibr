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

  return { setTheme: setTheme, makeT: makeT, fmtBytes: fmtBytes, fmtBits: fmtBits, fmtAgo: fmtAgo, esc: esc, makeApi: makeApi };
})();
