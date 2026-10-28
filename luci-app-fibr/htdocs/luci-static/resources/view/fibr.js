/* Fibr VPN setup page for LuCI (client-side view, OpenWrt 23.05+ / 24.10).
 *
 * One page, three things:
 *   1. Setup: exchange a quick-connect join code for the device config
 *      directly with the Fibr server (fetch POST https://<domain>/api/join/<code>
 *      from the router's browser) or accept a pasted wg-quick .conf.
 *   2. Apply: parse the config on the client and write UCI via rpc:
 *        network.wg_fibr            interface, proto=wireguard (23.05+ names)
 *        network.@wireguard_wg_fibr[0]   peer: public_key, allowed_ips,
 *                                        endpoint_host/port, keepalive
 *        firewall zone "fibr"       masq + mtu_fix, forwarding lan -> fibr
 *   3. Status + on/off: ubus network.interface.wg_fibr status, the toggle
 *      flips netifd's "auto" flag and applies the config.
 *
 * Interface texts are Russian by design: this plugin ships no .po catalogs,
 * and Fibr's audience is Russian-first (see docs/routers.md).
 */

'use strict';
'require view';
'require uci';
'require rpc';
'require ui';
'require poll';

/* UCI names written by this page (see the header comment). */
var IFACE = 'wg_fibr';
var PEER_TYPE = 'wireguard_wg_fibr';
var ZONE = 'fibr';

/* ubus: per-interface status object published by netifd. */
var callInterfaceStatus = rpc.declare({
	object: 'network.interface.' + IFACE,
	method: 'status',
	expect: { '': {} }
});

/* Fallback apply path when the running LuCI lacks uci.apply(): a plain
 * non-rollback ubus uci apply (no confirmation timers to manage). */
var callUciApply = rpc.declare({
	object: 'uci',
	method: 'apply',
	params: [ 'rollback' ]
});

/* --------------------------- wg-quick parser --------------------------- */

function splitList(s) {
	return String(s || '').split(',').map(function(v) { return v.trim(); })
		.filter(function(v) { return v.length > 0; });
}

/* "host:51820" / "[2001:db8::1]:51820" -> [host, port]. */
function splitEndpoint(s) {
	var m = String(s || '').trim().match(/^(?:\[([^\]]+)\]|([^:\s]+)):([0-9]+)$/);
	return m ? [m[1] || m[2], m[3]] : null;
}

/* Minimal INI parser for wg-quick configs. Returns the [Interface] key
 * map, the array of [Peer] key maps and convenience lists. AmneziaWG
 * extra keys (Jc/Jmin/Jmax/S1/S2/H1..H4) are detected and reported:
 * stock OpenWrt WireGuard cannot use them. */
function parseWgConf(text) {
	var conf = { interface: {}, peers: [], awg: false },
	    section = null;

	String(text || '').split(/\r?\n/).forEach(function(line) {
		line = line.trim();
		if (!line || line.charAt(0) === '#' || line.charAt(0) === ';')
			return;

		var m = line.match(/^\[(.+)\]$/);
		if (m) {
			section = m[1].trim().toLowerCase();
			if (section === 'peer')
				conf.peers.push({});
			return;
		}

		m = line.match(/^([A-Za-z0-9_]+)\s*=\s*(.*)$/);
		if (!m || !section)
			return;

		var target = null;
		if (section === 'interface')
			target = conf.interface;
		else if (section === 'peer')
			target = conf.peers[conf.peers.length - 1];

		if (target)
			target[m[1]] = m[2].trim();

		if (/^(Jc|Jmin|Jmax|S1|S2|H[1-4])$/.test(m[1]))
			conf.awg = true;
	});

	conf.addresses = splitList(conf.interface.Address);
	conf.dns = splitList(conf.interface.DNS);
	conf.allowedIps = conf.peers.length ? splitList(conf.peers[0].AllowedIPs) : [];
	var ep = conf.peers.length ? splitEndpoint(conf.peers[0].Endpoint) : null;
	conf.endpointHost = ep ? ep[0] : null;
	conf.endpointPort = ep ? ep[1] : null;
	return conf;
}

function validateConf(conf) {
	if (!conf.interface.PrivateKey)
		throw new Error('В конфиге нет строки PrivateKey.');
	if (!conf.addresses.length)
		throw new Error('В конфиге нет строки Address.');
	if (!conf.peers.length)
		throw new Error('В конфиге нет секции [Peer].');
	if (!conf.peers[0].PublicKey)
		throw new Error('В секции [Peer] нет PublicKey.');
	if (!splitEndpoint(conf.peers[0].Endpoint))
		throw new Error('В секции [Peer] нет корректного Endpoint (host:порт).');
}

/* ------------------------- UCI state writers ------------------------- */

function interfaceConfigured() {
	return uci.get('network', IFACE, 'proto') === 'wireguard';
}

/* Idempotent: wipes the previous fibr sections, then writes everything
 * from the parsed config. Array values become UCI lists (addresses,
 * allowed_ips, zone network). */
function writeUci(conf) {
	uci.sections('network', PEER_TYPE).forEach(function(s) {
		uci.remove('network', s['.name']);
	});
	if (interfaceConfigured())
		uci.remove('network', IFACE);

	var sid = uci.add('network', 'interface', IFACE);
	uci.set('network', sid, 'proto', 'wireguard');
	uci.set('network', sid, 'private_key', conf.interface.PrivateKey);
	uci.set('network', sid, 'addresses', conf.addresses);
	var mtu = parseInt(conf.interface.MTU, 10);
	if (mtu > 0)
		uci.set('network', sid, 'mtu', String(mtu));

	var p = conf.peers[0],
	    pid = uci.add('network', PEER_TYPE);
	uci.set('network', pid, 'public_key', p.PublicKey);
	if (p.PresharedKey)
		uci.set('network', pid, 'preshared_key', p.PresharedKey);
	uci.set('network', pid, 'allowed_ips', splitList(p.AllowedIPs));
	uci.set('network', pid, 'endpoint_host', conf.endpointHost);
	uci.set('network', pid, 'endpoint_port', conf.endpointPort);
	var ka = parseInt(p.PersistentKeepalive, 10);
	if (ka > 0)
		uci.set('network', pid, 'persistent_keepalive', String(ka));
	uci.set('network', pid, 'route_allowed_ips', '1');

	writeFirewallUci();
}

/* Zone "fibr": reject inbound, masquerade the home subnet behind the
 * router's /32 tunnel address (the Fibr server routes only the router
 * itself), plus forwarding lan -> fibr. Reused on re-apply. */
function writeFirewallUci() {
	var zone = null;
	uci.sections('firewall', 'zone').forEach(function(z) {
		if (z.name === ZONE)
			zone = z['.name'];
	});
	if (!zone) {
		zone = uci.add('firewall', 'zone');
		uci.set('firewall', zone, 'name', ZONE);
	}

	uci.set('firewall', zone, 'input', 'REJECT');
	uci.set('firewall', zone, 'output', 'ACCEPT');
	uci.set('firewall', zone, 'forward', 'REJECT');
	uci.set('firewall', zone, 'masq', '1');
	uci.set('firewall', zone, 'mtu_fix', '1');

	var nets = uci.get('firewall', zone, 'network');
	if (!Array.isArray(nets))
		nets = nets ? [nets] : [];
	if (nets.indexOf(IFACE) < 0) {
		nets.push(IFACE);
		uci.set('firewall', zone, 'network', nets);
	}

	var fwd = null;
	uci.sections('firewall', 'forwarding').forEach(function(f) {
		if (f.src === 'lan' && f.dest === ZONE)
			fwd = f['.name'];
	});
	if (!fwd) {
		fwd = uci.add('firewall', 'forwarding');
		uci.set('firewall', fwd, 'src', 'lan');
		uci.set('firewall', fwd, 'dest', ZONE);
	}
}

function removeUci() {
	uci.sections('network', PEER_TYPE).forEach(function(s) {
		uci.remove('network', s['.name']);
	});
	if (interfaceConfigured())
		uci.remove('network', IFACE);
	uci.sections('firewall', 'zone').forEach(function(z) {
		if (z.name === ZONE)
			uci.remove('firewall', z['.name']);
	});
	uci.sections('firewall', 'forwarding').forEach(function(f) {
		if (f.dest === ZONE)
			uci.remove('firewall', f['.name']);
	});
}

/* save() flushes the staged changes over ubus uci; apply() is LuCI's
 * rollback-managed apply when present (23.05+ has it), otherwise a plain
 * ubus uci apply without rollback. */
function saveAndApply() {
	return uci.save().then(function() {
		if (typeof uci.apply === 'function')
			return uci.apply();
		return callUciApply(false);
	});
}

/* ------------------------- join code exchange ------------------------- */

function joinError(status, msg) {
	switch (status) {
	case 404:
		return 'Код не найден — проверьте ввод.';
	case 410:
		return 'Код уже использован или истёк. Сгенерируйте новый в Mini App.';
	case 403:
		if (msg === 'subscription expired')
			return 'Подписка владельца устройства истекла.';
		if (msg === 'account disabled')
			return 'Аккаунт владельца отключён.';
		if (msg === 'device disabled')
			return 'Устройство отключено на сервере.';
		return 'Сервер отказал: ' + (msg || status);
	default:
		return 'Сервер ответил ' + status + (msg ? ': ' + msg : '');
	}
}

/* Simple CORS request (POST without body or custom headers needs no
 * preflight); the server must answer Access-Control-Allow-Origin for the
 * browser to let us read the response — fresh Fibr servers do. */
function exchangeJoinCode(domain, code) {
	var url = 'https://' + domain + '/api/join/' + encodeURIComponent(code);

	return fetch(url, { method: 'POST' }).then(function(resp) {
		return resp.json().catch(function() { return {}; }).then(function(body) {
			if (!resp.ok)
				throw new Error(joinError(resp.status, body.error));
			if (body.protocol && body.protocol !== 'wg')
				throw new Error('Устройство использует протокол «' + body.protocol +
					'» (vless-ссылка). Роутер поддерживает только WireGuard.');
			if (!body.config)
				throw new Error('Сервер не вернул конфиг.');
			return body.config;
		});
	}).catch(function(e) {
		if (e instanceof TypeError)
			throw new Error('Не удалось соединиться с ' + domain + '. Проверьте домен ' +
				'и выход роутера в интернет. Если сервер Fibr старый (без CORS-заголовка ' +
				'на /api/join), обмен кода из браузера не сработает — вставьте конфиг ' +
				'вручную в поле ниже.');
		throw e;
	});
}

/* ------------------------------ the page ------------------------------ */

var statusEl = null;

function fmtUptime(s) {
	if (!s || s < 1)
		return '—';
	var d = Math.floor(s / 86400),
	    h = Math.floor((s % 86400) / 3600),
	    m = Math.floor((s % 3600) / 60);
	if (d) return d + ' д ' + h + ' ч ' + m + ' мин';
	if (h) return h + ' ч ' + m + ' мин';
	if (m) return m + ' мин';
	return Math.floor(s) + ' с';
}

function statusRow(label, value) {
	return E('div', { 'class': 'cbi-value' }, [
		E('label', { 'class': 'cbi-value-title' }, label),
		E('div', { 'class': 'cbi-value-field' }, E('em', {}, String(value)))
	]);
}

function renderStatus(st) {
	var configured = interfaceConfigured(),
	    nodes = [];

	if (!configured) {
		nodes.push(E('p', {}, 'Туннель ещё не настроен — заполните форму ниже.'));
		return nodes;
	}

	var addresses = uci.get('network', IFACE, 'addresses'),
	    peer = uci.sections('network', PEER_TYPE)[0];

	if (st && st.up) {
		nodes.push(statusRow('Состояние', 'Подключено'));
		nodes.push(statusRow('Время работы', fmtUptime(st.uptime)));
		if (st.l3_device)
			nodes.push(statusRow('Устройство', st.l3_device));
	} else {
		nodes.push(statusRow('Состояние', st ? 'Отключено' : 'Не поднят'));
	}
	nodes.push(statusRow('Адреса туннеля',
		(Array.isArray(addresses) ? addresses : [addresses]).join(', ') || '—'));
	if (peer && peer.endpoint_host)
		nodes.push(statusRow('Сервер', peer.endpoint_host +
			(peer.endpoint_port ? ':' + peer.endpoint_port : '')));
	if (peer && peer.allowed_ips)
		nodes.push(statusRow('AllowedIPs',
			(Array.isArray(peer.allowed_ips) ? peer.allowed_ips : [peer.allowed_ips]).join(', ')));
	nodes.push(statusRow('Автозапуск',
		(st && st.autostart === false) || uci.get('network', IFACE, 'auto') === '0'
			? 'выключен' : 'включён'));

	var up = !!(st && st.up),
	    autoOn = uci.get('network', IFACE, 'auto') !== '0';
	nodes.push(E('div', { 'class': 'cbi-value' }, [
		E('label', { 'class': 'cbi-value-title' }, 'Управление'),
		E('div', { 'class': 'cbi-value-field' }, [
			(!up || !autoOn) ? E('button', {
				'class': 'cbi-button cbi-button-apply important',
				'click': ui.createHandlerFn(toggleEnabled, true)
			}, 'Включить') : E('button', {
				'class': 'cbi-button cbi-button-reset',
				'click': ui.createHandlerFn(toggleEnabled, false)
			}, 'Выключить'),
			' ',
			E('button', {
				'class': 'cbi-button',
				'click': ui.createHandlerFn(function() { return refreshStatus(); })
			}, 'Обновить')
		])
	]));
	return nodes;
}

function refreshStatus() {
	if (!statusEl)
		return Promise.resolve();

	return callInterfaceStatus().then(function(st) {
		renderStatusInto(st);
	}, function() {
		renderStatusInto(null);
	});
}

function renderStatusInto(st) {
	if (!statusEl)
		return;
	while (statusEl.firstChild)
		statusEl.removeChild(statusEl.firstChild);
	renderStatus(st).forEach(function(n) { statusEl.appendChild(n); });
}

function toggleEnabled(enabled) {
	if (!interfaceConfigured())
		return Promise.reject(new Error('Туннель ещё не настроен.'));

	/* netifd's auto flag: 0 keeps the interface down across reloads and
	 * reboots, 1 brings it up; uci apply reloads network+firewall. */
	uci.set('network', IFACE, 'auto', enabled ? '1' : '0');

	return saveAndApply().then(function() {
		ui.addNotification(null, E('p', {},
			enabled ? 'Туннель включён.' : 'Туннель выключен.'));
		return refreshStatus();
	}).catch(function(e) {
		ui.addNotification('Ошибка', E('p', {}, String(e.message || e)));
	});
}

function handleApply() {
	var domainEl = document.getElementById('fibr-domain'),
	    codeEl = document.getElementById('fibr-code'),
	    confEl = document.getElementById('fibr-conf');

	var domain = (domainEl && domainEl.value || '').trim()
		.replace(/^https?:\/\//, '').replace(/\/+$/, '');
	var code = (codeEl && codeEl.value || '').trim();
	var text = (confEl && confEl.value || '').trim();

	if (domain) {
		try { window.localStorage.setItem('fibr-domain', domain); }
		catch (e) { /* private mode etc. */ }
	}

	var p;
	if (text)
		p = Promise.resolve(text);
	else if (code) {
		if (!domain)
			return Promise.reject(new Error('Для обмена кода укажите домен сервера.'));
		p = exchangeJoinCode(domain, code);
	} else
		return Promise.reject(new Error('Введите join-код или вставьте конфиг.'));

	return p.then(function(cfgText) {
		var conf = parseWgConf(cfgText);
		validateConf(conf);
		writeUci(conf);

		return saveAndApply().then(function() {
			if (codeEl)
				codeEl.value = '';
			if (confEl)
				confEl.value = '';

			if (conf.awg)
				ui.addNotification('Внимание', E('p', {}, [
					'В конфиге есть параметры AmneziaWG (Jc/S1/H1…). Обычный WireGuard ',
					E('br'),
					'на OpenWrt с ними не подключится: нужен сервер Fibr без движка ',
					'AmneziaWG либо пакеты AmneziaWG на роутере.'
				]));

			ui.addNotification(null, E('p', {}, [
				'Настройка применена: интерфейс ' + IFACE + ', пир ' + PEER_TYPE +
				' и зона файрвола ' + ZONE + ' (masq, lan→' + ZONE + ').',
				E('br'),
				'DNS из конфига (' + (conf.dns.join(', ') || '—') +
				') при желании пропишите в Network → DHCP and DNS.'
			]));

			renderStatusInto(null);
			return refreshStatus();
		});
	}).catch(function(e) {
		ui.addNotification('Ошибка', E('p', {}, String(e.message || e)));
	});
}

function performRemove() {
	removeUci();

	return saveAndApply().then(function() {
		ui.addNotification(null, E('p', {},
			'Настройка Fibr удалена (интерфейс, пир, зона файрвола).'));
		renderStatusInto(null);
		return refreshStatus();
	}).catch(function(e) {
		ui.addNotification('Ошибка', E('p', {}, String(e.message || e)));
	});
}

return view.extend({
	title: 'Fibr VPN',

	load: function() {
		return Promise.all([
			uci.load('network'),
			uci.load('firewall')
		]);
	},

	render: function() {
		var stored = '';
		try { stored = window.localStorage.getItem('fibr-domain') || ''; }
		catch (e) { /* private mode etc. */ }

		var page = E('div', { 'class': 'cbi-map' }, [
			E('h2', {}, 'Fibr VPN'),
			E('div', { 'class': 'cbi-map-descr' },
				'Туннель WireGuard до сервера Fibr: весь трафик роутера и домашней ' +
				'сети идёт через VPN (TV, приставки, консоли — без клиента на каждом).'),

			E('div', { 'class': 'cbi-section' }, [
				E('h3', {}, 'Статус'),
				statusEl = E('div', { id: 'fibr-status' })
			]),

			E('div', { 'class': 'cbi-section' }, [
				E('h3', {}, 'Настройка'),
				E('div', { 'class': 'cbi-section-descr' },
					'Введите join-код из Mini App (устройство → «Код») или вставьте ' +
					'конфиг WireGuard целиком — например, из скачанного .conf.'),
				E('div', { 'class': 'cbi-value' }, [
					E('label', { 'class': 'cbi-value-title' }, 'Домен сервера'),
					E('div', { 'class': 'cbi-value-field' }, E('input', {
						'type': 'text',
						'id': 'fibr-domain',
						'class': 'cbi-input-text',
						'placeholder': 'vpn.example.com',
						'value': stored
					}))
				]),
				E('div', { 'class': 'cbi-value' }, [
					E('label', { 'class': 'cbi-value-title' }, 'Join-код'),
					E('div', { 'class': 'cbi-value-field' }, E('input', {
						'type': 'text',
						'id': 'fibr-code',
						'class': 'cbi-input-text',
						'placeholder': 'XXXXXXXX',
						'style': 'text-transform:uppercase'
					}))
				]),
				E('div', { 'class': 'cbi-value' }, [
					E('label', { 'class': 'cbi-value-title' }, '… или конфиг'),
					E('div', { 'class': 'cbi-value-field' }, E('textarea', {
						'id': 'fibr-conf',
						'class': 'cbi-input-textarea',
						'style': 'width:100%;min-height:9em;font-family:monospace',
						'placeholder': '[Interface]\nPrivateKey = …\nAddress = 10.8.0.5/32\n…\n\n[Peer]\nPublicKey = …\nAllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = vpn.example.com:51820\n…'
					}))
				]),
				E('div', { 'class': 'cbi-value' }, [
					E('label', { 'class': 'cbi-value-title' }, ' '),
					E('div', { 'class': 'cbi-value-field' }, [
						E('button', {
							'class': 'cbi-button cbi-button-apply important',
							'click': ui.createHandlerFn(handleApply)
						}, 'Настроить'),
						' ',
						E('button', {
							'class': 'cbi-button cbi-button-negative',
							'click': ui.createHandlerFn(function() {
								if (!interfaceConfigured())
									return Promise.reject(new Error('Нечего удалять — туннель не настроен.'));
								return performRemove();
							})
						}, 'Удалить настройку Fibr')
					])
				])
			]),

			E('div', { 'class': 'cbi-section' }, [
				E('h3', {}, 'Что делает плагин'),
				E('ul', {}, [
					E('li', {}, 'обменивает код на конфиг напрямую с сервером ' +
						'(POST /api/join/<код> из браузера роутера; код одноразовый и ' +
						'гасится на сервере как обычно)'),
					E('li', {}, 'пишет интерфейс network.' + IFACE + ' (proto wireguard, ' +
						'ключи и адреса из [Interface], MTU при наличии)'),
					E('li', {}, 'пишет пир ' + PEER_TYPE + ': ключ сервера, PresharedKey, ' +
						'AllowedIPs, Endpoint, keepalive, route_allowed_ips'),
					E('li', {}, 'создаёт зону файрвола ' + ZONE + ' с маскарадингом ' +
						'(домашняя подсеть прячется за адресом роутера /32) и forwarding ' +
						'lan → ' + ZONE)
				])
			])
		]);

		renderStatusInto(null);
		refreshStatus();
		poll.add(refreshStatus, 5);

		return page;
	}
});
