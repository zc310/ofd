import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

const saved = new Map();
const state = {
  engine: { nativeDesktop: true }, renderFormat: 'png', renderFormatSelect: {}, pageInfos: [],
  localStorage: { setItem: (key, value) => saved.set(key, value) }, renderFormatStorageKey: 'format',
  updateNavigation() {}, zoom: 3,
};
load(state, ['setRenderFormat', 'imageRenderFormat', 'pageDPI', 'renderMimeType'].map(sliceFunction).join('\n'),
  { expose: { set: 'setRenderFormat', imageRenderFormat: 'imageRenderFormat', dpi: 'pageDPI', mime: 'renderMimeType' } });
state.set('svg');
assert.equal(state.renderFormat, 'svg');
assert.equal(state.renderFormatSelect.value, 'svg');
assert.equal(saved.get('format'), 'svg');
assert.equal(state.dpi(), 96, 'SVG 缩放不应提高渲染 DPI');
assert.equal(state.mime('svg'), 'image/svg+xml');
state.set('jpg');
assert.equal(state.dpi(), 288, '光栅缩放仍提高渲染 DPI');

let reloads = 0, layers = 0;
Object.assign(state, {
  renderFormat: 'svg', pageInfos: [{}], zoom: 1, zoomMode: 'manual',
  pageDPI: state.dpi,
  pageCards: [{}], textCache: new Map([[0, []]]), applyPageWidth() {},
  buildTextLayer() { layers++; }, reloadPageImages() { reloads++; }, scheduleVirtualUpdate() {},
});
load(state, sliceFunction('setZoom'), { expose: { zoomTo: 'setZoom' } });
state.zoomTo(2);
assert.equal(reloads, 0, 'SVG 缩放应复用已有矢量图');
assert.equal(layers, 1, '缩放后仍需更新文字层');
state.renderFormat = 'png';
state.zoomTo(3);
assert.equal(reloads, 1, '光栅缩放仍需重新渲染');
console.log('桌面 SVG 格式选择与渲染参数测试通过');
