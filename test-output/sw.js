const CACHE = 'clone-v1';
const API_MAP = {};
const WS_MAP = {};
const URL_MAP = {"https://ajax.googleapis.com/ajax/libs/webfont/1.6.26/webfont.js":"ajax.googleapis.com/ajax/libs/webfont/1.6.26/webfont.js","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/673ff5cf97fea25394869163_Euclid%20Circular%20B%20Medium.ttf":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/673ff5cf97fea25394869163_Euclid Circular B Medium.ttf","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/673ff5cf97fea25394869252_Frame%25201171277833-p-2000.png":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/673ff5cf97fea25394869252_Frame 1171277833-p-2000.png","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/6858febd4636e8e689bbfeac_Logo.avif":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/6858febd4636e8e689bbfeac_Logo.avif","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a2aff7750396608914615_webflow_logo_icon_169218%201.avif":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a2aff7750396608914615_webflow_logo_icon_169218 1.avif","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a2aff775039660891461b_Figma.avif":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a2aff775039660891461b_Figma.avif","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a3ca5078c58224e6a49ee_scroll%20Image%20two%20Image.png":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a3ca5078c58224e6a49ee_scroll Image two Image.png","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a3ca5765ff6562c38a190_scroll%20Image%20five%20Image.png":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a3ca5765ff6562c38a190_scroll Image five Image.png","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a3ca57a1075b3cb8f4847_scroll%20Image%20four%20Image.png":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a3ca57a1075b3cb8f4847_scroll Image four Image.png","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a3ca5f4753525f93f0418_scroll%20Image%20three%20Image.png":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a3ca5f4753525f93f0418_scroll Image three Image.png","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a871775b8bec685bca9f3_dark%20Logo.png":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a871775b8bec685bca9f3_dark Logo.png","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685ba25d14b33e7ffddbbf20_customize%20image%20two%20.png":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685ba25d14b33e7ffddbbf20_customize image two .png","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685ba27346d981141340947a_Customepage%20one.png":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685ba27346d981141340947a_Customepage one.png","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/css/farmex-webflow-template.shared.fa7d74cbd.min.css":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/css/farmex-webflow-template.shared.fa7d74cbd.min.css","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/js/farmex-webflow-template.9ae39829.8faae3e35ca9fc44.js":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/js/farmex-webflow-template.9ae39829.8faae3e35ca9fc44.js","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/js/farmex-webflow-template.schunk.74913c4b4b4ccfa6.js":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/js/farmex-webflow-template.schunk.74913c4b4b4ccfa6.js","https://d3e54v103j8qbb.cloudfront.net/js/jquery-3.5.1.min.dc5e7f18c8.js?site=673ff5cf97fea2539486915c":"d3e54v103j8qbb.cloudfront.net/js/jquery-3.5.1.min.dc5e7f18c8_site=673ff5cf97fea2539486915c.js","https://farmex-webflow-template.webflow.io/":"farmex-webflow-template.webflow.io/index.html","https://fonts.googleapis.com/css?family=Plus+Jakarta+Sans:200,300,regular,500,600,700,800":"fonts.googleapis.com/css/index_family=Plus+Jakarta+Sans_200,300,regular,500,600,700,800.html"};
const STATIC_EXT = /\.(css|js|png|jpg|jpeg|gif|svg|ico|woff2?|ttf|eot|webp)(\?.*)?$/;
const API_PATTERN = /\/api\//;

self.addEventListener('install', event => {
  event.waitUntil(
    caches.open(CACHE).then(function(cache) {
      var urls = Object.keys(URL_MAP).map(function(url) { return URL_MAP[url]; });
      return Promise.all(
        urls.map(function(path) {
          return cache.add(path).catch(function() {});
        })
      );
    }).then(function() {
      self.skipWaiting();
    })
  );
});

self.addEventListener('activate', event => {
  event.waitUntil(caches.keys().then(keys =>
    Promise.all(keys.filter(k => k !== CACHE).map(k => caches.delete(k)))
  ));
  return self.clients.claim();
});

function matchAPI(url, method, body) {
  if (isGraphQL(url) && body) {
    try {
      var parsed = JSON.parse(body);
      if (parsed.operationName) {
        var gqlKey = url + '|gql:' + parsed.operationName;
        var gqlEntry = API_MAP[gqlKey];
        if (gqlEntry && (!gqlEntry.m || gqlEntry.m === method)) return gqlEntry;
      }
    } catch(e) {}
  }
  const entry = API_MAP[url];
  if (entry && (!entry.m || entry.m === method)) return entry;
  const noQuery = url.split('?')[0].split('#')[0];
  if (noQuery !== url) {
    const e2 = API_MAP[noQuery];
    if (e2 && (!e2.m || e2.m === method)) return e2;
  }
  for (const [key, val] of Object.entries(API_MAP)) {
    if (key.includes('|gql:')) continue;
    const base = key.split('?')[0];
    if (base === noQuery && (!val.m || val.m === method)) return val;
  }
  return null;
}

function isGraphQL(url) {
  return url.includes('/graphql') || url.includes('/gql');
}

function getWSMessages(url) {
  const normalized = url.replace(/^ws:/, 'http:').replace(/^wss:/, 'https:');
  let msgs = WS_MAP[url] || WS_MAP[normalized];
  if (msgs) return msgs;
  for (const [key, val] of Object.entries(WS_MAP)) {
    if (key.replace(/^ws:/, 'http:').replace(/^wss:/, 'https:') === normalized) return val;
  }
  return null;
}

function matchURL(url) {
  const withoutQuery = url.split('?')[0].split('#')[0];
  if (URL_MAP[url]) return URL_MAP[url];
  if (URL_MAP[withoutQuery]) return URL_MAP[withoutQuery];
  for (const [key, val] of Object.entries(URL_MAP)) {
    if (key.split('?')[0].split('#')[0] === withoutQuery) return val;
  }
  return null;
}

function isHTML(url) {
  return !STATIC_EXT.test(url) && !API_PATTERN.test(url) && !url.includes('/api/');
}

self.addEventListener('fetch', event => {
  const { request } = event;
  const url = request.url;

  if (API_PATTERN.test(url) || isGraphQL(url)) {
    if (isGraphQL(url) && (request.method === 'POST' || request.method === 'PUT')) {
      event.respondWith(
        request.clone().text().then(function(body) {
          var entry = matchAPI(url, request.method, body);
          if (entry) {
            if (entry.rb && (request.method === 'POST' || request.method === 'PATCH' || request.method === 'PUT')) {
              var replay = new Request(url, {
                method: request.method,
                headers: request.headers,
                body: entry.rb
              });
              return fetch(replay)['catch'](function() {
                return new Response(entry.b, { status: entry.s, statusText: 'OK', headers: entry.h });
              });
            }
            return new Response(entry.b, { status: entry.s, statusText: 'OK', headers: entry.h });
          }
          return fetch(request)['catch'](function() {
            return new Response('', { status: 503 });
          });
        })
      );
      return;
    }
    const entry = matchAPI(url, request.method, null);
    if (entry) {
      if (entry.rb && (request.method === 'POST' || request.method === 'PATCH' || request.method === 'PUT')) {
        const replay = new Request(url, {
          method: request.method,
          headers: request.headers,
          body: entry.rb
        });
        event.respondWith(
          fetch(replay).catch(() => new Response(entry.b, {
            status: entry.s,
            statusText: 'OK',
            headers: entry.h
          }))
        );
        return;
      }
      event.respondWith(new Response(entry.b, {
        status: entry.s,
        statusText: 'OK',
        headers: entry.h
      }));
      return;
    }
  }

  if (STATIC_EXT.test(url)) {
    event.respondWith(
      caches.match(request).then(cached => {
        if (cached) return cached;
        return fetch(request).then(res => {
          const copy = res.clone();
          caches.open(CACHE).then(cache => cache.put(request, copy));
          return res;
        }).catch(async () => {
          const localPath = matchURL(url);
          if (localPath) {
            const cached = await caches.match(localPath);
            if (cached) return cached;
          }
          return new Response('', { status: 404 });
        });
      })
    );
    return;
  }

  if (isHTML(url)) {
    event.respondWith(
      fetch(request).then(res => {
        const copy = res.clone();
        caches.open(CACHE).then(cache => {
          if (copy.ok && copy.headers.get('content-type')?.includes('text/html')) {
            cache.put(request, copy);
          }
        });
        return res;
      }).catch(async () => {
        const cached = await caches.match(request);
        if (cached) return cached;
        const localPath = matchURL(url);
        if (localPath) {
          const localCached = await caches.match(localPath);
          if (localCached) return localCached;
          return fetch(localPath).catch(() => caches.match('/offline.html'));
        }
        return new Response('Offline', { status: 503 });
      })
    );
    return;
  }

  event.respondWith(fetch(request));
});