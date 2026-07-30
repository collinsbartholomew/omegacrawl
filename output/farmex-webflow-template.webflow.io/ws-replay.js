(function() {
	var wsData = null;
	fetch('ws-data.json').then(function(r) { return r.json(); }).then(function(data) { wsData = data; }).catch(function() {});
	var NativeWebSocket = window.WebSocket;
	function findMessages(url) {
		if (!wsData) return null;
		var httpURL = url.replace(/^ws:/, 'http:').replace(/^wss:/, 'https:');
		if (wsData[httpURL]) return wsData[httpURL];
		if (wsData[url]) return wsData[url];
		for (var key in wsData) {
			if (key.replace(/^ws:/, 'http:').replace(/^wss:/, 'https:') === httpURL) return wsData[key];
		}
		return null;
	}
	function decodeData(msg) {
		if (msg.is_binary) {
			var binaryStr = atob(msg.data);
			var bytes = new Uint8Array(binaryStr.length);
			for (var i = 0; i < binaryStr.length; i++) {
				bytes[i] = binaryStr.charCodeAt(i);
			}
			return bytes.buffer;
		}
		return msg.data;
	}
	window.WebSocket = function(url, protocols) {
		var msgs = findMessages(url);
		if (msgs) {
			var ws = {
				url: url, readyState: 0,
				CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3,
				onopen: null, onclose: null, onmessage: null, onerror: null,
				bufferedAmount: 0, extensions: '', protocol: protocols || '',
				close: function() { this.readyState = 3; if (this.onclose) this.onclose({ code: 1000, reason: 'Replay complete', wasClean: true }); },
				send: function(data) {},
				addEventListener: function(type, listener) {
					if (type === 'open') this.onopen = listener;
					else if (type === 'message') this.onmessage = listener;
					else if (type === 'close') this.onclose = listener;
					else if (type === 'error') this.onerror = listener;
				}
			};
			ws.readyState = 0;
			setTimeout(function() {
				ws.readyState = 1;
				if (ws.onopen) ws.onopen({ target: ws });
				var receives = [];
				var timestamps = [];
				var baseTime = 0;
				for (var i = 0; i < msgs.length; i++) {
					if (msgs[i].direction === 'receive') {
						receives.push(msgs[i]);
						timestamps.push(msgs[i].timestamp ? new Date(msgs[i].timestamp).getTime() : 0);
					}
				}
				if (timestamps.length > 0 && timestamps[0] > 0) {
					baseTime = timestamps[0];
				}
				for (var i = 0; i < receives.length; i++) {
					(function(idx, msg) {
						var delay = 50;
						if (baseTime > 0 && timestamps[idx] > 0) {
							delay = timestamps[idx] - baseTime;
						} else {
							delay = idx * 50;
						}
						setTimeout(function() {
							if (ws.onmessage) ws.onmessage({ data: decodeData(msg), target: ws, type: 'message' });
						}, delay);
					})(i, receives[i]);
				}
				var lastDelay = receives.length > 0 ? (timestamps[receives.length-1] > 0 ? timestamps[receives.length-1] - baseTime : receives.length * 50) + 100 : 100;
				setTimeout(function() {
					ws.readyState = 3;
					if (ws.onclose) ws.onclose({ code: 1000, reason: 'Replay complete', wasClean: true });
				}, Math.max(lastDelay, 100));
			}, 100);
			return ws;
		}
		return new NativeWebSocket(url, protocols);
	};
	window.WebSocket.CONNECTING = 0;
	window.WebSocket.OPEN = 1;
	window.WebSocket.CLOSING = 2;
	window.WebSocket.CLOSED = 3;
})();