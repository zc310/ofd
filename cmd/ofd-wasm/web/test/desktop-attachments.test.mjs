import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

function element() {
  return { children: [], classList: { add() {} }, append(...items) { this.children.push(...items); },
    addEventListener(type, callback) { this[type] = callback; } };
}
const state = {
  document: { createElement: element }, engine: { nativeDesktop: true },
  formatAttachmentBytes: size => String(size), attachmentBadge: text => ({ text }),
  attachmentPreviewMime: () => 'text/plain', runAttachmentAction() {}, downloadAttachment() {}, previewAttachment() {},
};
load(state, ['buildAttachmentItem', 'canPreviewDesktopAttachment', 'desktopAttachmentKind'].map(sliceFunction).join('\n'), { expose: { build: 'buildAttachmentItem' } });
const desktop = state.build({ name: '说明.txt', exists: true, visible: true });
const [download, preview] = desktop.children[2].children;
assert.equal(download.disabled, false);
assert.equal(download.textContent, '另存为');
assert.equal(preview.disabled, false);
assert.equal(typeof preview.click, 'function');
assert.equal(state.build({ name: '附件.pdf', exists: true }).children[2].children[1].disabled, true);
assert.equal(state.build({ name: '说明.txt', exists: true, actual_size: 1048577 }).children[2].children[1].disabled, true);
for (const name of ['图片.gif', '图片.bmp', '图片.tiff', '图片.webp', '音乐.mp3', '音乐.m4a', '视频.mp4', '视频.webm']) {
  assert.equal(state.build({ name, exists: true }).children[2].children[1].disabled, false, name);
}
assert.equal(state.build({ name: '图片.svg', exists: true }).children[2].children[1].disabled, true, '不执行活动 SVG');
assert.equal(state.build({ name: '图片.png', exists: true, actual_size: 8388609 }).children[2].children[1].disabled, true);
assert.equal(state.build({ name: '视频.mp4', exists: true, actual_size: 33554433 }).children[2].children[1].disabled, true);
assert.match(download.title, /导出权限/);
assert.equal(typeof download.click, 'function', '桌面附件绑定原生保存操作');
assert.equal(state.build({ name: '缺失.txt', exists: false }).children[2].children[0].disabled, true);
state.engine.nativeDesktop = false;
const browser = state.build({ name: '说明.txt', exists: true, visible: true });
assert.equal(typeof browser.children[2].children[0].click, 'function', '浏览器附件下载保持可用');
console.log('桌面附件文本预览和下载限制测试通过');
