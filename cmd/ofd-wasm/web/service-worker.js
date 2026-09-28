// CACHE_NAME 由 make build-wasm / make package-wasm-web 根据
// index.html、viewer.js、worker.js、wasm_exec.js、ofd.wasm 的内容哈希生成
// ofd-reader-shell_<hash>；资源路径保持固定，发布时重新构建即可。
const CACHE_NAME = 'ofd-reader-shell_defbb1a75dc1a6b6';
const SHELL_FILES = [
  './',
  './index.html',
  './viewer.js',
  './worker.js',
  './wasm_exec.js',
  './ofd.wasm',
  './material-symbols-outlined-subset.woff2',
  './manifest.webmanifest',
  './icon.svg',
  './icon-512.png',
  './icon-192.png',
];

const freshRequest = url => new Request(url, { cache: 'no-cache' });

self.addEventListener('install', event => {
  event.waitUntil(caches.open(CACHE_NAME).then(cache => cache.addAll(SHELL_FILES.map(freshRequest))));
  self.skipWaiting();
});

// ofd-fonts 由 viewer.js 维护，缓存内容寻址（不可变 URL）的回退字体，
// 与应用版本无关，不应随应用更新被清理。
self.addEventListener('activate', event => {
  event.waitUntil(
    caches.keys().then(keys => Promise.all(
      keys.filter(key => key !== CACHE_NAME && key !== 'ofd-fonts').map(key => caches.delete(key)),
    )),
  );
  self.clients.claim();
});

self.addEventListener('fetch', event => {
  const request = event.request;
  const url = new URL(request.url);
  if (request.method !== 'GET' || url.origin !== self.location.origin) return;
  // 同源 .ofd 文档一律不进 shell 缓存：文档体积大且同一地址内容可能已变，命中
  // 缓存会读到陈旧副本。判断必须放在 navigate 分支之前，否则同源 .ofd 的直接
  // 访问会被 navigate 分支接管，并把文档内容写进 './index.html' 键，污染离线
  // 时的应用外壳。
  if (/\.ofd$/i.test(url.pathname)) return;
  if (request.mode === 'navigate' || url.pathname.endsWith('/index.html')) {
    event.respondWith(fetch(request, { cache: 'no-cache' }).then(response => {
      if (response.ok) {
        const copy = response.clone();
        caches.open(CACHE_NAME).then(cache => cache.put('./index.html', copy));
      }
      return response;
    }).catch(() => caches.match('./index.html')));
    return;
  }
  // viewer.js 用 ?file= 下载远程文档时带 cache: 'no-store'，这类响应同样不能
  // 进 shell 缓存。
  if (request.cache === 'no-store') return;
  event.respondWith(
    caches.match(request).then(cached => cached || fetch(request).then(response => {
      if (!response.ok) return response;
      const copy = response.clone();
      caches.open(CACHE_NAME).then(cache => cache.put(request, copy));
      return response;
    })),
  );
});
