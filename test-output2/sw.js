const CACHE = 'clone-v1';
const API_MAP = {};
const WS_MAP = {};
const URL_MAP = {"data:image/avif;base64,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":"_unknown/index.html","https://ajax.googleapis.com/ajax/libs/webfont/1.6.26/webfont.js":"ajax.googleapis.com/ajax/libs/webfont/1.6.26/webfont.js","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/673ff5cf97fea25394869252_Frame%25201171277833-p-2000.png":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/673ff5cf97fea25394869252_Frame 1171277833-p-2000.png","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a262f12cbd3cc088156c9_contact%20inner%20Image.avif":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a262f12cbd3cc088156c9_contact inner Image.avif","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a29a907952e5f9651cde6_link%20inner%20page%20Image.avif":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a29a907952e5f9651cde6_link inner page Image.avif","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a2aff775039660891461b_Figma.avif":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a2aff775039660891461b_Figma.avif","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a2ba06ad7ecc4ce4b5818_link%20inner%20page%20one%20Image.avif":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a2ba06ad7ecc4ce4b5818_link inner page one Image.avif","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a39da790d05b4b1eda64a_creative%20home%20page%20Image.png":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a39da790d05b4b1eda64a_creative home page Image.png","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a3ca59451e34447292693_scroll%20Image%20oneImage.png":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a3ca59451e34447292693_scroll Image oneImage.png","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a8165b5e080b651c6c813_header%20style%20two%20Image.png":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a8165b5e080b651c6c813_header style two Image.png","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a871775b8bec685bca9f3_dark%20Logo.png":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/685a871775b8bec685bca9f3_dark Logo.png","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/css/farmex-webflow-template.shared.fa7d74cbd.min.css":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/css/farmex-webflow-template.shared.fa7d74cbd.min.css","https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/js/farmex-webflow-template.9ae39829.8faae3e35ca9fc44.js":"cdn.prod.website-files.com/673ff5cf97fea2539486915c/js/farmex-webflow-template.9ae39829.8faae3e35ca9fc44.js","https://d3e54v103j8qbb.cloudfront.net/js/jquery-3.5.1.min.dc5e7f18c8.js?site=673ff5cf97fea2539486915c":"d3e54v103j8qbb.cloudfront.net/js/jquery-3.5.1.min.dc5e7f18c8_site=673ff5cf97fea2539486915c.js","https://farmex-webflow-template.webflow.io/":"farmex-webflow-template.webflow.io/index.html","https://farmex-webflow-template.webflow.io/about-us":"farmex-webflow-template.webflow.io/about-us/index.html","https://fonts.googleapis.com/css?family=Plus+Jakarta+Sans:200,300,regular,500,600,700,800":"fonts.googleapis.com/css/index_family=Plus+Jakarta+Sans_200,300,regular,500,600,700,800.html"};
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