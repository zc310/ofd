import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

function node(tag = '') {
  return { tag, children: [], isConnected: true,
    append(...items) { this.children.push(...items); }, prepend(item) { this.children.unshift(item); },
    addEventListener(type, fn) { this[type] = fn; }, remove() { this.removed = true; } };
}
const pending = [], urls = [], revoked = [], big = [];
const requests = [];
let cancelled = 0;
const state = {
  document: { createElement: node }, documentGeneration: 1,
  mediaPreviewRequests: new Set(), mediaObjectURLs: [], mediaObserver: null,
  thumbnailJobs: new Map(), thumbnailQueue: [], thumbnailRevision: 0,
  closeDesktopMediaPreview() {}, Blob,
  URL: { createObjectURL: () => { const url = `blob:${urls.length}`; urls.push(url); return url; }, revokeObjectURL: url => revoked.push(url) },
  engine: { nativeDesktop: true, signatureSealPreview(scope, id, index) {
    const request = new Promise((resolve, reject) => pending.push({ resolve, reject, scope, id, index }));
    request.cancel = () => { cancelled++; };
    return request;
  } },
  openDesktopImagePreview: (_, label, start) => { big.push(label); return start(); },
};
const nativeRequest = state.engine.signatureSealPreview;
state.engine.signatureSealPreview = (...args) => { requests.push('preview'); return nativeRequest(...args); };
state.engine.signatureSealThumbnail = (...args) => { requests.push('thumbnail'); return nativeRequest(...args); };
load(state, ['buildSealPreview', 'loadSealThumbnail', 'signatureSealImage', 'sealMime', 'revokeMediaURLs'].map(sliceFunction).join('\n'),
  { expose: { build: 'buildSealPreview', revoke: 'revokeMediaURLs' } });
const info = { scope: 2, id: 's1' }, stamp = { id: 'a', has_seal: true, seal_type: 'png' };
const png = { mime: 'image/png', data: new Uint8Array([1]) };
const flush = () => new Promise(resolve => setImmediate(resolve));
const button = state.build(info, stamp, 3);
assert.equal(pending.length, 0, '构建离屏卡片不立即读取图片');
button.thumbnailLoad();
assert.equal(pending.length, 1, '不点击也会请求印章图片');
assert.equal(requests[0], 'thumbnail', '默认图片使用原生缩小接口');
assert.deepEqual([pending[0].scope, pending[0].id, pending[0].index], [2, 's1', 3]);
pending[0].resolve(png); await flush();
assert.equal(button.children[0].tag, 'img');
assert.equal(button.children[0].src, 'blob:0', '默认显示缩略图');
assert.equal(button.children[0].className, 'signature-seal', '复用浏览器印章缩略图样式');
assert.equal(button.children[1].textContent, '查看印章大图');
assert.equal(big.length, 0, '自动加载不打开大图弹窗');
const large = button.click();
assert.equal(pending.length, 2, '查看大图重新请求，不能直接使用缩略图缓存');
assert.equal(requests[1], 'preview', '大图独立读取原始图片');
pending[1].resolve(png); await large;
assert.match(big[0], /不代表验证通过/);
const unsupported = state.build(info, { ...stamp, seal_type: 'svg' }, 4);
assert.equal(unsupported.disabled, true);
assert.equal(pending.length, 2, '不请求不支持的格式');
const denied = state.build(info, stamp, 5);
denied.thumbnailLoad();
pending[2].reject(new Error('访问权限过期')); await flush();
assert.match(denied.children[0].textContent, /加载失败.*重试/);
assert.match(denied.title, /权限过期/);
assert.equal(denied.disabled, false);
const retry = denied.click(); pending[3].resolve(png); await retry;
const corrupt = state.build(info, stamp, 6);
corrupt.thumbnailLoad();
pending[4].resolve(png); await flush();
corrupt.children[0].error();
assert.equal(corrupt.children[0].removed, true);
assert.match(corrupt.children[1].textContent, /加载失败/);
assert.ok(revoked.includes('blob:1'), '解码失败即释放图片 URL');
const detached = state.build(info, stamp, 7);
detached.thumbnailLoad();
detached.isConnected = false;
pending[5].resolve(png); await flush();
assert.equal(urls.length, 2, '被替换的旧侧栏不能创建图片 URL');
const stale = state.build(info, stamp, 8);
stale.thumbnailLoad();
state.documentGeneration++;
state.revoke();
pending[6].resolve(png); await flush();
assert.equal(cancelled, 1);
assert.equal(urls.length, 2, '文档切换后迟到结果不显示图片');
assert.equal(stale.children.length, 1);
assert.equal(state.mediaPreviewRequests.size, 0);
assert.deepEqual([...revoked].sort(), [...urls].sort(), '侧栏或文档切换释放全部缩略图 URL');
// 共用组件在浏览器仍通过 Worker 读取，不调用原生绑定。
state.engine.nativeDesktop = false;
state.engine.signatureSeal = (...args) => { requests.push('worker'); return nativeRequest(...args); };
const browser = state.build(info, stamp, 9);
browser.thumbnailLoad();
pending[7].resolve(new Uint8Array([2])); await flush();
assert.equal(requests.at(-1), 'worker');
assert.equal(browser.children[0].className, 'signature-seal');
const browserBig = browser.click(); pending[8].resolve(new Uint8Array([2])); await browserBig;
assert.equal(requests.at(-1), 'worker', '浏览器大图也重新请求 Worker');
state.revoke();
state.engine.signatureSealPreview = (scope, id, index, thumbnail) => {
  requests.push(thumbnail ? 'worker-ofd-thumbnail' : 'worker-ofd-preview');
  return nativeRequest(scope, id, index);
};
const ofd = state.build(info, { ...stamp, seal_type: 'ofd' }, 10);
assert.equal(ofd.disabled, false);
let responseIndex = pending.length;
ofd.thumbnailLoad(); pending[responseIndex].resolve(new Uint8Array([3])); await flush();
assert.equal(requests.at(-1), 'worker-ofd-thumbnail');
assert.equal(ofd.children[0].className, 'signature-seal');
responseIndex = pending.length;
const ofdBig = ofd.click(); pending[responseIndex].resolve(new Uint8Array([4])); await ofdBig;
assert.equal(requests.at(-1), 'worker-ofd-preview', 'OFD 大图重新请求 Worker 渲染，不复用缩略图');
state.revoke();
console.log('印章默认缩略图、独立大图复核、权限拒绝、解码失败与旧侧栏隔离测试通过');
