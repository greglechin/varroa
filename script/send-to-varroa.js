// ==UserScript==
// @name           varroa musica
// @namespace      varroa
// @description    Adds a VM link for each torrent, to send directly to varroa musica.
// @include        http*://*apollo.rip/*
// @include        http*://*redacted.ch/*
// @include        http*://*notwhat.cd/*
// @version        9
// @date           2017-03
// @grant          GM_getValue
// @grant          GM_setValue
// @grant          GM_notification
// @grant          GM_addStyle
// ==/UserScript==

// with some help from `xo --fix send_to_varroa.js`
/* global window document MutationObserver GM_notification GM_getValue GM_setValue */
/* eslint new-cap: "off" */

const linkregex = /torrents\.php\?action=download.*?id=(\d+).*?authkey=.*?torrent_pass=(?=([a-z0-9]+))\2(?!&)/i;
const divider = ' | ';

// Get userid
const userinfoElement = document.getElementsByClassName('username')[0];
const userid = userinfoElement.href.match(/user\.php\?id=(\d+)/)[1];
// Get current hostname
const siteHostname = window.location.host;
// Get domain-specific settings prefix to make this script multi-site
const settingsNamePrefix = siteHostname + '_' + userid + '_';
// Settings
const settings = getSettings();
// Checks for current page
const settingsPage = window.location.href.match('user.php\\?action=edit&userid=');
const userPage = window.location.href.match('user.php\\?id=' + userid);
const top10Page = window.location.href.match('top10.php');
const torrentPage = window.location.href.match('torrents.php$');
const torrentUserPage = window.location.href.match('torrents.php?(.*)&userid');
// Check if tokens are available
const FLTokensAvailable = areFLTokensAvailable();
// Misc strings
const vmUnknown = 'Pinging VM...';
const vmOK = 'VM is up.';
const vmKO = 'VM is offline (click to check again).';
const vmGet = 'VM: sent torrent with ID #';
const vmCannotGet = 'VM is offline, cannot get torrent (click to check again).';
const vmLinkInfo = 'Send to varroa musica';

let obsElem;
let linkLabel = 'VM';
let linkLabelFL = 'VM FL';
if (top10Page) {
	linkLabel = '[' + linkLabel + ']';
	linkLabelFL = '[' + linkLabelFL + ']';
}
let isWebSocketConnected = false;
let vmStatusDiv = null;
let sock;
let hello;
let alreadyAddedLinks = false;

if (settings) {
	if (settings.https === true) {
		hello = {
			Command: 'hello',
			Token: settings.token,
			Site: settings.site
		};
		// Open the websocket to varroa
		newSocket();
	} else {
		// Add http links
		addLinks();
	}
	// Add stats if on user page
	addStatsToUserPage();
}
if (settingsPage) {
	appendSettings();
	document.getElementById('varroa_settings_token').addEventListener('change', saveSettings, false);
	document.getElementById('varroa_settings_url').addEventListener('change', saveSettings, false);
	document.getElementById('varroa_settings_port').addEventListener('change', saveSettings, false);
	document.getElementById('varroa_settings_https').addEventListener('change', saveSettings, false);
	document.getElementById('varroa_settings_site').addEventListener('change', saveSettings, false);
}
if (!settings && !settingsPage) {
	GM_notification({
		text: 'Missing configuration\nVisit user settings and setup',
		title: 'Varroa Musica:',
		timeout: 6000
	});
}

function addLinks() {
	if (alreadyAddedLinks === true) {
		return;
	}
	const alltorrents = [];
	for (let i = 0; i < document.links.length; i++) {
		alltorrents.push(document.links[i]);
	}

	for (let i = 0; i < alltorrents.length; i++) {
		if (linkregex.exec(alltorrents[i])) {
			const id = RegExp.$1;
			createLink(alltorrents[i], id, false);
			if (FLTokensAvailable) {
				createLink(alltorrents[i], id, true);
			}
		}
	}

	MutationObserver = window.MutationObserver || window.WebKitMutationObserver; // eslint-disable-line no-global-assign
	const obs = new MutationObserver(mutations => {
		mutations.forEach(mutation => {
			mutation.addedNodes.forEach(node => {
				if (linkregex.exec(node.querySelector('a'))) {
					const id = RegExp.$1;
					createLink(node.querySelector('a'), id, false);
					if (FLTokensAvailable) {
						createLink(node.querySelector('a'), id, true);
					}
				}
			});
		});
	});

	if (torrentPage) {
		obsElem = document.querySelector('#torrent_table > tbody'); // eslint-disable-line no-unused-vars
	} else if (torrentUserPage) {
		obsElem = document.querySelector('.torrent_table > tbody'); // eslint-disable-line no-unused-vars
	}
	if (obsElem) { // eslint-disable-line no-undef
		obs.observe(obsElem, { // eslint-disable-line no-undef
			childList: true
		});
	}

	alreadyAddedLinks = true;
}

function makeStatsLink(label, filename) {
	let link = settings.url + ':' + settings.port + '/getStats/' + filename + '?token=' + settings.token + '&site=' + settings.site;
	if (settings.https === true) {
		link = 'https://' + link;
	} else {
		link = 'http://' + link;
	}
	return label + `:  <a href="javascript:void(0);" onclick="BBCode.spoiler(this);">Show</a><blockquote class="hidden spoiler"><div style="text-align: center;"><img class="scale_image" onclick="lightbox.init(this, $(this).width());" alt="` + link + `" src="` + link + `" /></div></blockquote><br />`;
}

function addStatsToUserPage() {
	if (userPage) {
		const main = document.getElementsByClassName('main_column')[0];
		const newBox = document.createElement('div');
		newBox.className = 'box';
		const newBoxHead = document.createElement('div');
		newBoxHead.className = 'head';
		newBoxHead.innerHTML = `Varroa Musica Stats<span style="float: right;"><a href="#" onclick="$('#varroa_stats').gtoggle(); this.innerHTML = (this.innerHTML == 'Hide' ? 'Show' : 'Hide'); return false;" class="brackets">Hide</a></span>&nbsp;`;
		newBox.appendChild(newBoxHead);
		const newBoxContent = document.createElement('div');
		newBoxContent.className = 'pad profileinfo';
		newBoxContent.id = 'varroa_stats';
		newBoxContent.innerHTML = makeStatsLink('Full Stats', 'stats.png') + makeStatsLink('Buffer', 'buffer.png') + makeStatsLink('Upload', 'up.png') + makeStatsLink('Download', 'down.png') + makeStatsLink('Ratio', 'ratio.png');
		newBoxContent.innerHTML += makeStatsLink('Buffer/day', 'buffer_per_day.png') + makeStatsLink('Upload/day', 'up_per_day.png') + makeStatsLink('Download/day', 'down_per_day.png') + makeStatsLink('Ratio/day', 'ratio_per_day.png');
		newBoxContent.innerHTML += makeStatsLink('Snatched/day', 'snatches_per_day.png') + makeStatsLink('Size snatched/day', 'size_snatched_per_day.png') + makeStatsLink('Top Tags', 'top_tags.png') + makeStatsLink('Snatched/filer', 'total_snatched_by_filter.png');
		newBox.appendChild(newBoxContent);
		main.insertBefore(newBox, main.children[1]);
	}
}

function newSocket() {
	// TODO use settings.token
	sock = new WebSocket('wss://' + settings.url + ':' + settings.port + '/ws');
	// Add unknown indicator
	setVMStatus(vmUnknown);

	sock.onopen = function () {
		console.log('Connected to the server');
		isWebSocketConnected = true;
		// Send the msg object as a JSON-formatted string.
		sock.send(JSON.stringify(hello));
	};
	sock.onerror = function (evt) {
		console.log('Websocket error.');
		isWebSocketConnected = false;
		setVMStatus(vmKO);
	};
	sock.onmessage = function (evt) {
		console.log(evt.data);
		const msg = JSON.parse(evt.data);

		if (msg.Status === 0) {
			if (msg.Message === 'hello') {
				setVMStatus(vmOK);
				// Safe to add links
				addLinks();
			} else {
				setVMStatus('VM: ' + msg.Message);
				// TODO change back after a while
			}
		}
	};
	sock.onclose = function () {
		console.log('Server connection closed.');
		isWebSocketConnected = false;
		setVMStatus(vmKO);
		setTimeout(() => {
			newSocket();
		}, 5000);
	};
}

function createLink(linkelement, id, useFLToken) {
	let link = '';
	if (useFLToken) {
		link = document.createElement('varroa_fl_' + id);
	} else {
		link = document.createElement('varroa_' + id);
	}
	link.appendChild(document.createElement('a'));
	if (useFLToken) {
		link.firstChild.appendChild(document.createTextNode(linkLabelFL));
	} else {
		link.firstChild.appendChild(document.createTextNode(linkLabel));
	}
	link.appendChild(document.createTextNode(divider));
	if (settings.https === true && isWebSocketConnected) {
		if (useFLToken) {
			link.addEventListener('click', getTorrentWithFLToken, false);
		} else {
			link.addEventListener('click', getTorrent, false);
		}
	} else {
		link.firstChild.href = 'http://' + settings.url + ':' + settings.port + '/get/' + id + '?token=' + settings.token + '&site=' + settings.site;
		if (useFLToken) {
			link.firstChild.href += '&fltoken=true';
		}
	}
	link.firstChild.target = '_blank';
	link.firstChild.title = vmLinkInfo;
	linkelement.parentNode.insertBefore(link, linkelement);
}

function getTorrent() {
	getTorrentAux(this.nodeName, 'varroa_', false);
}

function getTorrentWithFLToken() {
	getTorrentAux(this.nodeName, 'varroa_fl_', true);
}

function getTorrentAux(nodename, prefix, useFLToken) {
	if (isWebSocketConnected) {
		const id = nodename.toLowerCase().replace(prefix, '');
		console.log('Getting torrent with id: ' + id);
		const get = {
			Command: 'get',
			Token: settings.token,
			Args: [id],
			Site: settings.site,
			FLToken: useFLToken
		};
		sock.send(JSON.stringify(get));
		setVMStatus(vmGet + id);
	} else {
		setVMStatus(vmCannotGet);
	}
}

function areFLTokensAvailable() {
	const tokens = document.getElementById('fl_tokens');
	if (tokens === null) {
		return false;
	}
	return parseInt(tokens.getElementsByClassName('stat')[0].getElementsByTagName('a')[0].innerHTML, 10) > 0;
}

// -- Status -------------------------------------------------------------------

function setVMStatus(label) {
	const a = document.createElement('a');
	a.innerHTML = label;
	if (settings.https === true) {
		a.addEventListener('click', newSocket, false);
	}
	if (vmStatusDiv === null) {
		vmStatusDiv = document.createElement('div');
		vmStatusDiv.id = 'varroa';
		vmStatusDiv.appendChild(a);
		document.body.appendChild(vmStatusDiv);
	} else {
		vmStatusDiv.replaceChild(a, vmStatusDiv.firstChild);
	}
}

(function () {
	'use strict';
	const css = '#varroa a {margin: 3px 0 15px 0;position: fixed;bottom: 0;background-color: #FFFFFF;color: #000000; border: 2px solid #6D6D6D; padding: 5px; cursor: pointer;}';
	GM_addStyle(css);
})();

// -- Settings -----------------------------------------------------------------

function appendSettings() {
	const container = document.getElementsByClassName('main_column')[0];
	const lastTable = container.lastElementChild;
	let settingsHTML = '<a name="varroa_settings"></a>\n<table cellpadding="6" cellspacing="1" border="0" width="100%" class="layout border user_options" id="varroa_settings">\n';
	settingsHTML += '<tbody>\n<tr class="colhead_dark"><td colspan="2"><strong>Varroa Musica Settings (autosaved)</strong></td></tr>\n';
	settingsHTML += '<tr><td class="label" title="Webserver Token">Site</td><td><input type="text" id="varroa_settings_site" placeholder="site label" value="' + GM_getValue(settingsNamePrefix + 'site', '') + '"></td></tr>\n';
	settingsHTML += '<tr><td class="label" title="Webserver Token">Token</td><td><input type="text" id="varroa_settings_token" placeholder="your token" value="' + GM_getValue(settingsNamePrefix + 'token', '') + '"></td></tr>\n';
	settingsHTML += '<tr><td class="label" title="Webserver hostname (seedbox hostname)">Hostname</td><td><input type="text" id="varroa_settings_url" placeholder="hostname.com" value="' + GM_getValue(settingsNamePrefix + 'url', '') + '"></td></tr>\n';
	settingsHTML += '<tr><td class="label" title="Webserver port">Port</td><td><input type="text" id="varroa_settings_port" placeholder="your chosen port" value="' + GM_getValue(settingsNamePrefix + 'port', '') + '"></td></tr>\n';
	let checked = '';
	if (GM_getValue(settingsNamePrefix + 'https', false) === true) {
		checked = 'checked';
	}
	settingsHTML += '<tr><td class="label" title="Webserver HTTPS enabled">HTTPS</td><td><input type="checkbox" id="varroa_settings_https" placeholder="true_or_false" value="HTTPS" ' + checked + '></td></tr>\n';
	settingsHTML += '</tbody>\n</table>';
	lastTable.insertAdjacentHTML('afterend', settingsHTML);

	const sectionsElem = document.querySelectorAll('#settings_sections > ul')[0];
	const sectionsHTML = '<h2><a href="#varroa_settings" class="tooltip" title="Varroa Musica Settings">Varroa Musica</a></h2>';
	const li = document.createElement('li');
	li.innerHTML = sectionsHTML;
	sectionsElem.insertBefore(li, document.querySelectorAll('#settings_sections > ul > li:nth-child(10)')[0]);
}

function getSettings() {
	const token = GM_getValue(settingsNamePrefix + 'token', '');
	const url = GM_getValue(settingsNamePrefix + 'url', '');
	const port = GM_getValue(settingsNamePrefix + 'port', '');
	const https = GM_getValue(settingsNamePrefix + 'https', false);
	const site = GM_getValue(settingsNamePrefix + 'site', '');
	if (token && url && port) {
		return {
			token,
			url,
			port,
			https,
			site
		};
	}
	return false;
}

function saveSettings() {
	const elem = document.getElementById(this.id);
	const setting = this.id.replace('varroa_settings_', settingsNamePrefix);
	const border = elem.style.border;
	if (this.type === 'text') {
		GM_setValue(setting, elem.value);
		if (GM_getValue(setting) === elem.value) {
			elem.style.border = '1px solid green';
			setTimeout(() => {
				elem.style.border = border;
			}, 2000);
		} else {
			elem.style.border = '1px solid red';
		}
	}
	if (this.type === 'checkbox') {
		GM_setValue(setting, elem.checked);
	}
}
