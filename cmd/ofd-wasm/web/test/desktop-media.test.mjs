import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

function element() {
  return {
    children: [], classList: { remove() {} },
    append(...items) { this.children.push(...items); },
    replaceChildren(...items) { this.children = items; },
    querySelectorAll() { return []; },
    setAttribute() {}, addEventListener(type, callback) { this[type] = callback; },
  };
}
const state = {
  engine: { nativeDesktop: true, media: async () => [{ id: 1, name: 'image.png', format: 'PNG', type: 'Image', size: 10, exists: false }] },
  document: { createElement: element }, mediaElement: element(), documentGeneration: 1, mediaRenderRequest: 0,
  pageInfos: [{}], sidebarFilterValue: '', revokeMediaURLs() {}, outlineEmptyMessage: text => text,
  loadDesktopList: () => state.engine.media(),
  restorePanelScroll() {}, formatAttachmentBytes: size => String(size),
  mediaKind: () => 'image', mediaTypeLabel: () => '图片', mediaIcon: () => 'image',
  mediaMime: () => 'image/png',
  loadMediaThumb: () => { throw new Error('桌面端不能读取资源内容'); },
};
load(state, ['buildMediaItem', 'buildDesktopMediaResource', 'renderMedia'].map(sliceFunction).join('\n'), { expose: { buildMediaItem: 'buildMediaItem', card: 'buildDesktopMediaResource', render: 'renderMedia' } });
state.render();
await Promise.resolve();
const [notice, grid] = state.mediaElement.children;
assert.match(notice, /音视频最多 32 MiB/);
const item = grid.children[0].children[0];
assert.equal(grid.children[0].children[1].disabled, true);
assert.equal(item.disabled, true);
assert.equal(item.click, undefined);
assert.match(item.children[2].textContent, /image\.png.*PNG.*文件不存在/);
state.mediaKind = () => 'audio';
state.mediaMime = () => 'audio/wav';
const audio = state.card({ id: 200, type: 'Audio', name: 'audio.wav', exists: true, size: 10 });
assert.equal(audio.children[0].disabled, false, '音频提供按需预览入口');
assert.equal(state.card({ id: 200, exists: true, size: 33554433 }).children[0].disabled, true, '超限媒体不预览');
assert.equal(audio.children[1].disabled, false, '不能预览的资源仍可另存为');
assert.equal(audio.children[1].textContent, '另存为');
let savedKind;
state.runAttachmentAction = (_, action) => action();
state.saveDesktopResource = (_, kind) => { savedKind = kind; };
audio.children[1].click();
assert.equal(savedKind, 'media');
assert.equal(state.card({ exists: true, size: 67108865 }).children[1].disabled, true, '超限资源禁用保存入口');
state.mediaKind = () => 'image';
state.mediaMime = () => 'image/png';
state.engine.nativeDesktop = false;
assert.equal(typeof state.buildMediaItem({ name: 'image.png' }).click, 'function', '浏览器保留资源预览入口');
const previews = [], external = [];
state.openDesktopMediaPreview = (_, item) => { previews.push(item.name); };
state.previewMedia = async item => { external.push(item.name); };
state.buildMediaItem({ name: 'image.png' }).click();
assert.deepEqual(previews, ['image.png'], '浏览器 PNG 资源进入共用缩放预览');
state.mediaMime = () => 'image/jpeg';
state.buildMediaItem({ name: 'image.jpg' }).click();
assert.deepEqual(previews, ['image.png', 'image.jpg']);
state.mediaMime = () => 'image/gif';
state.buildMediaItem({ name: 'image.gif' }).click();
state.mediaKind = () => 'audio';
state.buildMediaItem({ name: 'audio.wav' }).click();
assert.deepEqual(external, ['image.gif', 'audio.wav'], '其他格式保留原预览方式');
state.mediaKind = () => 'image';
state.mediaMime = () => 'image/png';

let oldResolve, newResolve;
state.engine.nativeDesktop = true;
state.engine.media = () => new Promise(resolve => { if (!oldResolve) oldResolve = resolve; else newResolve = resolve; });
state.render();
state.render();
newResolve([]);
await Promise.resolve();
oldResolve([{ name: '旧结果' }]);
await Promise.resolve();
assert.deepEqual(state.mediaElement.children, ['此文档没有多媒体资源']);
console.log('桌面资源只读清单及请求乱序测试通过');
