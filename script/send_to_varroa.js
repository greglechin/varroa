// ==UserScript==
// @name           varroa musica
// @namespace      varroa
// @description    Adds a VM link for each torrent, to send directly to varroa musica.
// @include        http*://*redacted.ch/*
// @version        2
// @date           2017-03
// @grant          GM_getValue
// @grant          GM_setValue
// @grant          GM_notification
// ==/UserScript==

var linkregex = /torrents\.php\?action=download.*?id=(\d+).*?authkey=.*?torrent_pass=(?=([a-z0-9]+))\2(?!&)/i;
var divider = ' | ';

var settings = getSettings();
var settingsPage = window.location.href.match('user.php\\?action=edit&userid=');

if (settings.token && settings.url && settings.port) {
	alltorrents = [];
	for (var i = 0; i < document.links.length; i++) {
		alltorrents.push(document.links[i]);
	}

	for (var i = 0; i < alltorrents.length; i++) {
		if (linkregex.exec(alltorrents[i])) {
			id = RegExp.$1;
			createLink(alltorrents[i], id);
		}
	}

  MutationObserver = window.MutationObserver || window.WebKitMutationObserver;
  var obs = new MutationObserver(function (mutations, observer) {
			mutations.forEach(function(mutation) {
				mutation.addedNodes.forEach(function(node){
					if (linkregex.exec(node.querySelector('a'))) {
						id = RegExp.$1;
						createLink(node.querySelector('a'), id);
					}
				});
			});
  });

  obs.observe(document.querySelectorAll('#torrent_table > tbody')[0], {
    childList: true,
  });
}

if (settingsPage) {
	appendSettings();
	document.getElementById('varroa_settings_token').addEventListener('change', saveSettings, false);
	document.getElementById('varroa_settings_url').addEventListener('change', saveSettings, false);
	document.getElementById('varroa_settings_port').addEventListener('change', saveSettings, false);
}

if (!settings && !settingsPage) {
	GM_notification({
		text: 'Missing configuration\nVisit user settings and setup',
		title: 'Varroa Musica:',
		timeout: 6000,
	});
}

function createLink(linkelement, id) {
	var link = document.createElement("varroa");
	link.appendChild(document.createElement("a"));
	link.firstChild.appendChild(document.createTextNode("VM"));
	link.appendChild(document.createTextNode(divider));
	link.firstChild.href = settings.url + ":" + settings.port + "/get/" + id + "?token=" + settings.token;
	link.firstChild.target = "_blank";
	link.firstChild.title = "Direct download to varroa musica";
	linkelement.parentNode.insertBefore(link, linkelement);
}

function appendSettings() {
	var container = document.getElementsByClassName('main_column')[0];
	var lastTable = container.lastElementChild;
	var settingsHTML = '<a name="varroa_settings"></a>\n<table cellpadding="6" cellspacing="1" border="0" width="100%" class="layout border user_options" id="varroa_settings">\n';
	settingsHTML += '<tbody>\n<tr class="colhead_dark"><td colspan="2"><strong>Varroa Musica Settings (autosaved)</strong></td></tr>\n';
	settingsHTML += '<tr><td class="label" title="Token set in varroa">Token</td><td><input type="text" id="varroa_settings_token" placeholder="insert_your_token" value="' + GM_getValue('token', '') + '"></td></tr>\n';
	settingsHTML += '<tr><td class="label" title="Your seedbox hostname set in varroa">Hostname</td><td><input type="text" id="varroa_settings_url" placeholder="http://hostname.com" value="' + GM_getValue('url', '') + '"></td></tr>\n';
	settingsHTML += '<tr><td class="label" title="Your seedbox port set in varroa">Port</td><td><input type="text" id="varroa_settings_port" placeholder="your_chosen_port" value="' + GM_getValue('port', '') + '"></td></tr>\n';
	settingsHTML += '</tbody>\n</table>';
	lastTable.insertAdjacentHTML('afterend', settingsHTML);

  var sectionsElem = document.querySelectorAll('#settings_sections > ul')[0];
  sectionsHTML = '<h2><a href="#varroa_settings" class="tooltip" title="Varroa Musica Settings">Varroa Musica</a></h2>';
  var li = document.createElement('li');
  li.innerHTML = sectionsHTML;
  sectionsElem.insertBefore(li, document.querySelectorAll('#settings_sections > ul > li:nth-child(10)')[0]);
}

function getSettings() {
	var token = GM_getValue('token', '');
	var url = GM_getValue('url', '');
	var port = GM_getValue('port', '');
	if (token && url && port) {
		return {
			token: token,
			url: url,
            port: port
		};
	} else {
		return false;
	}
}

function saveSettings() {
	var elem = document.getElementById(this.id);
	var setting = this.id.replace('varroa_settings_', '');
	var border = elem.style.border;
	GM_setValue(setting, elem.value);
	if (GM_getValue(setting) === elem.value) {
		elem.style.border = '1px solid green';
		setTimeout(function () {
			elem.style.border = border;
		}, 2000);
	} else {
		elem.style.border = '1px solid red';
	}
}
