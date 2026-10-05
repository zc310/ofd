// CACHE_NAME 由 make build-wasm / make package-wasm-web 根据
// index.html、viewer.js、worker.js、wasm_exec.js、ofd.wasm 的内容哈希生成
// ofd-reader-shell_<hash>；资源路径保持固定，发布时重新构建即可。
const CACHE_NAME = 'ofd-reader-shell_fc4cdca5abba9226';
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
  // 逐个添加而不是 addAll：addAll 里任何一个文件取不到就整批失败，新 SW 装不上，
  // 浏览器会永久继续用旧缓存，而且没有任何提示。逐个添加失败只影响那一个资源，
  // 运行时再报错，便于定位。
  event.waitUntil(caches.open(CACHE_NAME).then(async cache => {
    const settled = await Promise.allSettled(SHELL_FILES.map(file => cache.add(freshRequest(file))));
    const failed = SHELL_FILES.filter((_, index) => settled[index].status === 'rejected');
    if (failed.length) console.warn('[OFD] shell 资源缓存失败:', failed);
  }));
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
    // index.html 与 viewer.js/worker.js/ofd.wasm 一样走缓存优先，让整个 shell 永远
    // 是同一版本。曾经让导航强制走网络，结果是新 SW 装好后这一页变成"新 HTML +
    // 旧 JS/WASM"：HTML 里可能引用了新接口，而旧 wasm 里根本没有，两边静默错配。
    // 改回缓存优先后代价只是整体滞后一次打开，换版由刷新按钮提示用户点一次。
    event.respondWith(
      caches.match('./index.html').then(cached => cached || fetch(request).then(response => {
        if (!response.ok) return response;
        const copy = response.clone();
        caches.open(CACHE_NAME).then(cache => cache.put('./index.html', copy));
        return response;
      })),
    );
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
