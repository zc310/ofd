import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

function node(tag = '') {
  return { tag, children: [], style: {}, isConnected: true, classList: { remove() {} },
    append(...items) { this.children.push(...items); items.forEach(item => { item.parent = this; }); },
    replaceWith(item) { const index = this.parent.children.indexOf(this); this.parent.children[index] = item; item.parent = this.parent; },
    setAttribute(name, value) { this[name] = value; }, addEventListener(type, fn) { this[type] = fn; },
    showModal() { this.open = true; }, close() { this.open = false; }, remove() { this.removed = true; } };
}
const pending = [], urls = [], revoked = [], statuses = [];
let cancelled = 0;
const state = {
  stopActiveMedia() {}, closeDesktopAttachmentPreview() {},
  document: { createElement: node, body: node('body') }, documentGeneration: 1,
  desktopMediaDialog: undefined, desktopMediaPreviewRevision: 0, mediaPreviewRequests: new Set(),
  mediaObjectURLs: [], mediaObserver: null, pageInfos: [{}],
  thumbnailJobs: new Map(), thumbnailQueue: [], thumbnailRevision: 0,
  URL: { createObjectURL: () => { const url = `blob:${urls.length}`; urls.push(url); return url; }, revokeObjectURL: url => revoked.push(url) },
  Blob, setStatus: text => statuses.push(text), signatureMethodLabel: value => value,
  ResizeObserver: class { observe() {} disconnect() {} },
  loadSealThumbnail() {}, signatureSealIcon: () => node(),
  engine: { nativeDesktop: true, signatureSealPreview(scope, id, index) {
    const request = new Promise((resolve, reject) => pending.push({ resolve, reject, scope, id, index }));
    request.cancel = () => { cancelled++; };
    return request;
  } },
};
load(state, ['buildSignatureSummary', 'buildSignatureStamp', 'buildSealPreview', 'signatureSealImage', 'sealMime', 'openDesktopImagePreview', 'closeDesktopMediaPreview', 'createImagePreviewControls', 'revokeMediaURLs'].map(sliceFunction).join('\n'),
  { expose: { build: 'buildSignatureSummary', close: 'closeDesktopMediaPreview', revoke: 'revokeMediaURLs' } });
const item = state.build({ scope: 2, id: 's1', stamps: [
  { id: 'none', page: -1 },
  { id: 'png', page: -1, has_seal: true, seal_type: 'png' },
  { id: 'ofd', page: 0, has_seal: true, seal_type: 'ofd' },
] });
const previews = item.children.filter(child => child.thumbnailLoad || child.title?.includes('OFD'));
assert.equal(previews.length, 2, '没有印章数据时不显示预览入口');
const [previewButton, ofd] = previews;
assert.equal(previewButton.disabled, false, '页码无法解析也可以独立查看所属文档体的印章');
assert.equal(ofd.disabled, false, 'OFD 印章复用图片预览入口');
const png = { mime: 'image/png', data: new Uint8Array([1]) };
const first = previewButton.click();
const firstDialog = state.desktopMediaDialog;
assert.equal(firstDialog.open, true, '读取中即可关闭弹窗');
assert.equal(firstDialog.children[0].textContent, '×', '签章与资源共用关闭图标');
assert.equal(firstDialog.children[0]['aria-label'], '关闭');
assert.equal(firstDialog.children[0].title, '关闭');
assert.match(firstDialog.children[1].textContent, /正在读取/);
assert.match(firstDialog['aria-label'], /不代表验证通过/);
assert.deepEqual([pending[0].scope, pending[0].id, pending[0].index], [2, 's1', 1], '过滤无数据条目不能改变原签章索引');
pending[0].resolve(png); await first;
const imageOf = dialog => dialog.children[1].children[1].children[0];
assert.equal(imageOf(firstDialog).src, 'blob:0');
state.close();
assert.deepEqual(revoked, ['blob:0']);
const closed = previewButton.click();
const loadingDialog = state.desktopMediaDialog;
loadingDialog.cancel({ preventDefault() {} });
pending[1].resolve(png); await closed;
assert.equal(cancelled, 1);
assert.equal(loadingDialog.removed, true);
assert.equal(state.desktopMediaDialog, undefined);
assert.equal(urls.length, 1, '关闭后迟到数据不能创建 URL');
const older = previewButton.click();
const newer = previewButton.click();
pending[3].resolve(png); await newer;
const latest = state.desktopMediaDialog;
pending[2].resolve(png); await older;
assert.equal(state.desktopMediaDialog, latest, '乱序响应不能替换当前印章');
assert.equal(imageOf(latest).src, 'blob:1');
const denied = previewButton.click();
pending[4].reject(new Error('访问权限过期')); await denied;
assert.match(statuses.at(-1), /权限过期/);
assert.equal(previewButton.disabled, false, '失败后允许重新读取');
const retry = previewButton.click();
pending[5].resolve(png); await retry;
imageOf(state.desktopMediaDialog).error();
assert.match(statuses.at(-1), /损坏或无法解码/);
assert.equal(state.desktopMediaDialog, undefined);
const stale = previewButton.click();
const staleDialog = state.desktopMediaDialog;
state.documentGeneration++;
state.revoke();
const before = urls.length;
pending[6].resolve(png); await stale;
assert.equal(urls.length, before);
assert.equal(staleDialog.removed, true);
assert.equal(state.mediaPreviewRequests.size, 0);
assert.equal(state.desktopMediaDialog, undefined);
assert.deepEqual(revoked, urls, '所有已创建的预览 URL 都已释放');
console.log('印章预览入口、原索引、关闭、乱序、权限拒绝、失败重试和旧文档隔离测试通过');
