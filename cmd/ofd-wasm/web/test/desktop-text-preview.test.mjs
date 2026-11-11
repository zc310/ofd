import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

const pending = [];
let cancelled = 0;
function node(tag) {
  return { tag, children: [], append(...items) { this.children.push(...items); },
    setAttribute() {}, addEventListener(type, fn) { this[type] = fn; },
    showModal() { this.open = true; }, close() { this.open = false; }, remove() { this.removed = true; } };
}
const state = {
  stopActiveMedia() {}, closeDesktopMediaPreview() {},
  documentGeneration: 1, desktopAttachmentPreview: undefined,
  desktopAttachmentLinkRequest: undefined,
  document: { createElement: node, body: node('body') },
  engine: { attachmentTextPreview(scope, id) {
    assert.equal(scope, 0); assert.equal(id, 'a');
    const promise = new Promise((resolve, reject) => pending.push({ resolve, reject }));
    promise.cancel = () => { cancelled++; };
    return promise;
  } },
};
load(state, ['openDesktopAttachmentPreview', 'closeDesktopAttachmentPreview'].map(sliceFunction).join('\n'),
  { expose: { open: 'openDesktopAttachmentPreview', close: 'closeDesktopAttachmentPreview' } });
const item = { scope: 0, id: 'a', name: '说明.xml' };
const first = state.open(item);
const firstDialog = state.desktopAttachmentPreview.dialog;
assert.equal(firstDialog.open, true, '读取中可以关闭弹窗');
pending[0].resolve('<script>alert(1)</script>中文');
await first;
assert.equal(firstDialog.children[2].textContent, '<script>alert(1)</script>中文');
assert.equal(firstDialog.children[2].innerHTML, undefined, '不能通过 HTML 插入附件内容');
const second = state.open(item);
state.close();
pending[1].resolve('迟到文本');
await second;
assert.equal(state.desktopAttachmentPreview, undefined);
assert.equal(firstDialog.removed, true);
const third = state.open(item);
const fourth = state.open(item);
pending[3].resolve('最新'); await fourth;
pending[2].resolve('旧内容'); await third;
assert.equal(state.desktopAttachmentPreview.dialog.children[2].textContent, '最新');
const denied = state.open(item);
pending[4].reject(new Error('禁止导出')); await denied;
assert.match(state.desktopAttachmentPreview.dialog.children[2].textContent, /禁止导出/);
const empty = state.open(item);
pending[5].resolve(''); await empty;
assert.equal(state.desktopAttachmentPreview.dialog.children[2].textContent, '', '空附件不显示加载占位');
const stale = state.open(item);
const staleDialog = state.desktopAttachmentPreview.dialog;
state.documentGeneration++;
state.close();
pending[6].resolve('旧文档'); await stale;
assert.equal(staleDialog.removed, true);
assert.ok(cancelled >= 3);
console.log('文本附件纯文本展示、关闭、乱序和文档切换测试通过');
