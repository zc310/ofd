import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { sliceFunction, load } from './harness.mjs';

// 布局逻辑合回 viewer.js 后，不依赖额外脚本或全局对象，且不改变合并、坐标和字体样式。
const base = { x: 10, y: 20, width: 3, height: 4, size: 3, fontFamily: '内嵌字体',
  weight: 400, bold: false, italic: false };
const runs = Object.freeze([
  { ...base, text: '' },
  { ...base, text: 'O' },
  { ...base, x: 13, y: 21, width: 2, height: 5, text: 'FD' },
  { ...base, x: 17, fontFamily: '其他字体', text: '开' },
  { ...base, x: 20, fontFamily: '其他字体', weight: 700, bold: true, text: '源' },
  { ...base, x: 23, italic: true, glyphs: [{ angle: 90 }], text: '旋' },
  { ...base, x: 26, charDirection: 270, text: '转' },
].map(Object.freeze));
const snapshot = JSON.stringify(runs);
const layer = { parentElement: {}, children: [], replaceChildren() { layer.children = []; },
  append(element) { layer.children.push(element); } };
const card = { clientWidth: 500, querySelector: () => layer };
const state = {
  pageCards: [card], pageInfos: [{ width: 100, height: 200 }],
  textCache: new Map([[0, runs]]), textLayerVisible: true, pagePermissionAllowed: () => true,
  searchResults: [], document: { createElement: () => ({ style: {} }) },
};
load(state, ['textLayerSegments', 'buildTextLayer'].map(sliceFunction).join('\n'),
  { expose: { segments: 'textLayerSegments', build: 'buildTextLayer' } });
const segments = state.segments(runs);
assert.deepEqual(segments.map(segment => segment.text), ['OFD', '开', '源', '旋', '转']);
assert.equal(segments[0].width, 5);
assert.equal(segments[0].height, 5);
assert.deepEqual(state.segments([]), []);
state.build(0);
assert.equal(layer.children.length, 5);
assert.deepEqual(layer.children[0].style, {
  left: '10%', top: '10%', width: '5%', height: '2.5%',
  fontSize: '15px', lineHeight: '25px', fontFamily: "'内嵌字体', sans-serif",
  fontWeight: '400', fontStyle: 'normal',
});
assert.equal(layer.children[2].style.fontWeight, '700');
assert.equal(layer.children[3].style.fontStyle, 'italic');
assert.equal(layer.children[3].style.transform, 'rotate(90deg)');
assert.equal(layer.children[4].style.transform, 'rotate(270deg)');
assert.equal(JSON.stringify(runs), snapshot, '合并与绘制不得修改原文字数据');
card.clientWidth = 250;
state.build(0);
assert.equal(layer.children[0].style.fontSize, '7.5px', '页面缩放后重新计算字号');
assert.equal(layer.children[0].style.lineHeight, '12.5px');
state.pagePermissionAllowed = () => false;
state.build(0);
assert.equal(layer.children.length, 0, '复制权限拒绝后不生成文字层');
state.pagePermissionAllowed = () => true;
state.textLayerVisible = false;
state.build(0);
assert.equal(layer.children.length, 0, '隐藏文字层时不生成文字节点');
for (const file of ['index.html', 'service-worker.tmpl.js', 'assets.go']) {
  const source = readFileSync(new URL(`../${file}`, import.meta.url), 'utf8');
  assert.ok(!source.includes('text-layout.js'), `${file} 不得再引用已合并的独立脚本`);
}
console.log('文字布局合并、字体样式、旋转、缩放、权限及脚本依赖清理回归通过');
