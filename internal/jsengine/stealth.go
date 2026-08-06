package jsengine

import (
	"context"

	"github.com/chromedp/chromedp"
)

const StealthScript = `
		(function() {
			// Remove CDP-specific $cdc_ properties from window
			for (const key of Object.getOwnPropertyNames(window)) {
				if (key.startsWith('$cdc_') || key.startsWith('$chrome_')) {
					delete window[key];
				}
			}

			// Override webdriver detection
			Object.defineProperty(navigator, 'webdriver', {
				get: () => undefined,
				configurable: true,
			});

			// Override plugins
			Object.defineProperty(navigator, 'plugins', {
				get: () => {
					const plugins = [
						{ name: 'Chrome PDF Plugin', filename: 'internal-pdf-viewer', description: 'Portable Document Format' },
						{ name: 'Chrome PDF Viewer', filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai', description: '' },
						{ name: 'Native Client', filename: 'internal-nacl-plugin', description: '' }
					];
					plugins.length = 3;
					plugins.item = function(i) { return this[i]; };
					plugins.namedItem = function(name) {
						for (var j = 0; j < this.length; j++) {
							if (this[j].name === name) return this[j];
						}
						return null;
					};
					return plugins;
				},
				configurable: true,
			});

			// Canvas fingerprint protection: add deterministic-ish noise to
			// both getImageData and toDataURL outputs without mutating the
			// live canvas (mutating it is detectable and breaks page logic).
			try {
				const origToDataURL = HTMLCanvasElement.prototype.toDataURL;
				const origGetImageData = CanvasRenderingContext2D.prototype.getImageData;

				CanvasRenderingContext2D.prototype.getImageData = function() {
					const imageData = origGetImageData.apply(this, arguments);
					for (let i = 0; i < imageData.data.length; i += 4) {
						imageData.data[i] = imageData.data[i] ^ (imageData.data[i+3] % 3 === 0 ? 1 : 0);
						imageData.data[i+1] = imageData.data[i+1] ^ (imageData.data[i+3] % 3 === 1 ? 1 : 0);
						imageData.data[i+2] = imageData.data[i+2] ^ (imageData.data[i+3] % 3 === 2 ? 1 : 0);
					}
					return imageData;
				};

				HTMLCanvasElement.prototype.toDataURL = function() {
					const ctx = this.getContext('2d');
					if (ctx) {
						const imageData = origGetImageData.call(ctx, 0, 0, this.width, this.height);
						for (let i = 0; i < imageData.data.length; i += 4) {
							imageData.data[i] = imageData.data[i] ^ 1;
							imageData.data[i+1] = imageData.data[i+1] ^ 1;
							imageData.data[i+2] = imageData.data[i+2] ^ 1;
						}
						const tmp = document.createElement('canvas');
						tmp.width = this.width;
						tmp.height = this.height;
						const tmpCtx = tmp.getContext('2d');
						tmpCtx.putImageData(imageData, 0, 0);
						return origToDataURL.call(tmp, ...arguments);
					}
					return origToDataURL.apply(this, arguments);
				};
			} catch(e) {}

			// WebRTC IP leak protection: mask the ICE candidate addresses in
			// both createOffer and createAnswer SDP.
			try {
				if (window.RTCPeerConnection) {
					const maskSDP = (sdp) => sdp
						.replace(/c=IN IP4 \d+\.\d+\.\d+\.\d+/g, 'c=IN IP4 0.0.0.0')
						.replace(/c=IN IP6 [0-9a-f:]+/gi, 'c=IN IP6 ::')
						.replace(/a=ice-ufrag:[^\r\n]+/g, 'a=ice-ufrag:abcdefgh')
						.replace(/a=ice-pwd:[^\r\n]+/g, 'a=ice-pwd:abcdefghijklmnopqrstuvwxyz');
					const origCreateOffer = RTCPeerConnection.prototype.createOffer;
					const origCreateAnswer = RTCPeerConnection.prototype.createAnswer;
					RTCPeerConnection.prototype.createOffer = function() {
						const result = origCreateOffer.apply(this, arguments);
						if (result && result.then) {
							return result.then(offer => {
								offer.sdp = maskSDP(offer.sdp || '');
								return offer;
							});
						}
						if (result && result.sdp) {
							result.sdp = maskSDP(result.sdp);
						}
						return result;
					};
					RTCPeerConnection.prototype.createAnswer = function() {
						const result = origCreateAnswer.apply(this, arguments);
						if (result && result.then) {
							return result.then(answer => {
								answer.sdp = maskSDP(answer.sdp || '');
								return answer;
							});
						}
						if (result && result.sdp) {
							result.sdp = maskSDP(result.sdp);
						}
						return result;
					};
				}
			} catch(e) {}

			// Font fingerprint protection - intentionally left untouched: an
			// always-true override breaks layout logic and is itself detectable.
			// The real engine font stack renders consistently across sessions.

			// Override AudioContext fingerprinting
			try {
				if (window.OfflineAudioContext || window.webkitOfflineAudioContext) {
					const AudioContextClass = window.OfflineAudioContext || window.webkitOfflineAudioContext;
					const origStartRendering = AudioContextClass.prototype.startRendering;
					AudioContextClass.prototype.startRendering = function() {
						const result = origStartRendering.apply(this, arguments);
						if (result && result.then) {
							return result.then(buffer => {
								if (buffer && buffer.getChannelData) {
									const data = buffer.getChannelData(0);
									for (let i = 0; i < data.length; i++) {
										data[i] += (Math.random() - 0.5) * 0.0001;
									}
								}
								return buffer;
							});
						}
						return result;
					};
				}
			} catch(e) {}

			// Override languages
			Object.defineProperty(navigator, 'languages', {
				get: () => ['en-US', 'en'],
				configurable: true,
			});

			// Override permissions
			const originalQuery = window.navigator.permissions.query;
			window.navigator.permissions.query = function(parameters) {
				if (parameters.name === 'notifications') {
					return Promise.resolve({ state: 'prompt', onchange: null });
				}
				return originalQuery(parameters);
			};

			// Chrome runtime - complete mock
			window.chrome = {
				runtime: {
					id: 'abcdefghijklmnopqrstuvwxyz',
					onConnect: { addListener: function() {} },
					onMessage: { addListener: function() {} },
					onInstalled: { addListener: function() {} },
					connect: function() { return null; },
					sendMessage: function() {},
					getManifest: function() { return {}; },
					getURL: function(path) { return path; },
				},
				loadTimes: function() {
					return {
						requestTime: 0,
						startLoadTime: 0,
						commitLoadTime: 0,
						finishDocumentLoadTime: 0,
						finishLoadTime: 0,
						firstPaintTime: 0,
						firstPaintAfterLoadTime: 0,
						wasFetchedViaSpdy: false,
						wasNpnNegotiated: false,
						wasAlternateProtocolAvailable: false,
						connectionInfo: ''
					};
				},
				csi: function() {
					return { startE: 0, onloadT: 0, pageT: 0, tran: 0 };
				},
				app: {
					isInstalled: false,
					InstallState: { DISABLED: 'disabled', INSTALLED: 'installed', NOT_INSTALLED: 'not_installed' },
					RunningState: { CANNOT_RUN: 'cannot_run', READY_TO_RUN: 'ready_to_run', RUNNING: 'running' },
					getDetails: function() {},
					getIsInstalled: function() {},
					installState: function() { return 'not_installed'; },
					runningState: function() { return 'cannot_run'; }
				},
				webstore: {
					onInstallStageChanged: {},
					onDownloadProgress: {},
				}
			};

			// Override WebGL vendor and renderer
			try {
				const getParameter = WebGLRenderingContext.prototype.getParameter;
				WebGLRenderingContext.prototype.getParameter = function(parameter) {
					if (parameter === 37445) {
						return 'Intel Inc.';
					}
					if (parameter === 37446) {
						return 'Intel Iris OpenGL Engine';
					}
					return getParameter.apply(this, arguments);
				};
			} catch(e) {}

			// Fix navigator.hardwareConcurrency
			Object.defineProperty(navigator, 'hardwareConcurrency', {
				get: () => 8,
				configurable: true,
			});

			// Fix navigator.deviceMemory
			Object.defineProperty(navigator, 'deviceMemory', {
				get: () => 8,
				configurable: true,
			});

			// Fix navigator.maxTouchPoints
			Object.defineProperty(navigator, 'maxTouchPoints', {
				get: () => 0,
				configurable: true,
			});

			// Override navigator.connection to look real
			if (navigator.connection) {
				Object.defineProperty(navigator.connection, 'effectiveType', {
					get: () => '4g',
					configurable: true,
				});
			}

			// Override navigator.platform
			Object.defineProperty(navigator, 'platform', {
				get: () => 'Linux x86_64',
				configurable: true,
			});

			// Override navigator.vendor
			Object.defineProperty(navigator, 'vendor', {
				get: () => 'Google Inc.',
				configurable: true,
			});

			// Hide headless by fixing Screen properties
			Object.defineProperty(screen, 'width', { get: () => 1920, configurable: true });
			Object.defineProperty(screen, 'height', { get: () => 1080, configurable: true });
			Object.defineProperty(screen, 'availWidth', { get: () => 1920, configurable: true });
			Object.defineProperty(screen, 'availHeight', { get: () => 1040, configurable: true });
			Object.defineProperty(screen, 'colorDepth', { get: () => 24, configurable: true });
			Object.defineProperty(screen, 'pixelDepth', { get: () => 24, configurable: true });

			// Add missing window properties
			if (window.speechSynthesis) {
				Object.defineProperty(window.speechSynthesis, 'speaking', { get: () => false });
			}
			if (window.AudioContext) {
				const origCreateOscillator = AudioContext.prototype.createOscillator;
				AudioContext.prototype.createOscillator = function() {
					const osc = origCreateOscillator.apply(this, arguments);
					osc.type = 'sine';
					osc.frequency.value = 440;
					return osc;
				};
			}
		})();
	`

// InjectStealth injects the stealth script to hide automation fingerprints.
func InjectStealth(ctx context.Context) error {
	return chromedp.Run(ctx, chromedp.Evaluate(StealthScript, nil))
}
