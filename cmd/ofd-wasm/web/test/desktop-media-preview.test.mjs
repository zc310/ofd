import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

let resolve, reject, cancelled = 0;
const urls = [], revoked = [];
function node(tag = '') {
  return { tag, children: [], dataset: {}, style: {}, isConnected: true, classList: { remove() {} },
    append(...children) { this.children.push(...children); children.forEach(child => { child.parent = this; }); },
    replaceWith(child) { this.parent.children[this.parent.children.indexOf(this)] = child; child.parent = this.parent; },
    setAttribute(name, value) { this[name] = value; }, addEventListener(type, fn) { this[type] = fn; },
    querySelector(selector) { return selector === '.media-thumb' ? this.thumb : selector === '.material-symbols-outlined' ? this.icon : null; },
    showModal() { this.open = true; }, close() { this.open = false; }, remove() { this.removed = true; } };
}
const state = {
  stopActiveMedia() {}, closeDesktopAttachmentPreview() {},
  engine: { nativeDesktop: true, mediaImagePreview: () => {
    const request = new Promise((yes, no) => { resolve = yes; reject = no; });
    request.cancel = () => { cancelled++; };
    return request;
  } },
  thumbnailJobs: new Map(), thumbnailQueue: [], thumbnailRevision: 0,
  documentGeneration: 1, desktopMediaDialog: undefined, desktopMediaPreviewRevision: 0,
  mediaPreviewRequests: new Set(), mediaObjectURLs: [], mediaObserver: null,
  URL: { createObjectURL: () => { const url = `blob:${urls.length}`; urls.push(url); return url; }, revokeObjectURL: url => revoked.push(url) },
  Blob, document: { createElement: node, body: node('body') }, setStatus: text => { state.status = text; },
  ResizeObserver: class { observe() {} disconnect() { state.disconnected = (state.disconnected || 0) + 1; } },
};
load(state, ['closeDesktopMediaPreview', 'revokeMediaURLs', 'openDesktopMediaPreview', 'openDesktopImagePreview', 'createImagePreviewControls', 'loadMediaThumb'].map(sliceFunction).join('\n'),
  { expose: { open: 'openDesktopMediaPreview', close: 'closeDesktopMediaPreview', revoke: 'revokeMediaURLs', thumb: 'loadMediaThumb' } });
const element = node('button');
state.engine.mediaThumbnail = state.engine.mediaImagePreview;
element.item = { scope: 0, id: 1, name: '图片.png' };
element.thumb = node('span');
element.thumb.icon = node('icon');
const thumb = state.thumb(element);
resolve({ mime: 'image/png', data: new Uint8Array([1]), width: 2000, height: 1000 });
await thumb;
assert.equal(element.thumb.children[0].src, 'blob:0', '默认加载资源缩略图');
const first = state.open(element, element.item);
resolve({ mime: 'image/png', data: new Uint8Array([1]) });
await first;
assert.equal(state.desktopMediaDialog.open, true);
assert.equal(state.desktopMediaDialog.children[1].children[1].children[0].src, 'blob:1', '点击使用单独读取的大图');
const closeButton = state.desktopMediaDialog.children[0];
assert.equal(closeButton.textContent, '×', '图片预览使用关闭图标');
assert.equal(closeButton.title, '关闭');
assert.equal(closeButton['aria-label'], '关闭', '图标按钮仍有可访问名称');
closeButton.click();
assert.equal(state.desktopMediaDialog, undefined, '图标仍可关闭预览');
assert.equal(state.disconnected, 1, '关闭释放尺寸监听');
assert.deepEqual(revoked, ['blob:1'], '关闭大图只释放大图 URL');
const second = state.open(element, element.item);
state.revoke();
resolve({ mime: 'image/png', data: new Uint8Array([1]) });
await second;
assert.equal(cancelled, 1);
assert.deepEqual(revoked, ['blob:1', 'blob:0']);
assert.equal(state.desktopMediaDialog, undefined, '取消后不能打开旧大图');
const failed = state.open(element, element.item);
reject(new Error('权限过期'));
await failed;
assert.match(state.status, /权限过期/);
state.engine.nativeDesktop = false;
state.fetchMediaData = () => state.engine.mediaImagePreview();
state.mediaMime = () => 'image/png';
delete element.dataset.loaded;
const browserThumb = state.thumb(element);
state.documentGeneration++;
state.revoke();
const before = urls.length;
resolve(new Uint8Array([1])); await browserThumb;
assert.equal(urls.length, before, '浏览器资源的迟到缩略图也不能泄漏到新文档');
assert.equal(state.mediaPreviewRequests.size, 0);
console.log('桌面图片缩略图、大图和取消测试通过');
