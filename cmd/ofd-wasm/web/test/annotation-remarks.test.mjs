import assert from 'node:assert/strict';
import { sliceFunction, load } from './harness.mjs';

const code = ['annotationRemarkPlacement', 'positionAnnotationRemarks', 'applyAnnotationRemarks', 'loadAnnotationInfos', 'cancelPageAnnotationRequests', 'printAnnotationRemarks'].map(sliceFunction).join('\n');
function setup(infos = []) {
  const layer = { dataset: {}, children: [], append(node) { layer.children.push(node); }, replaceChildren() { layer.children = []; } };
  const card = { querySelector: () => layer, querySelectorAll: () => layer.children };
  const state = {
    documentGeneration: 1, zoom: 2, pageRotation: 0, basePagePixels: 210, referencePageWidthMM: 210,
    pageInfos: [{ width: 100, height: 200 }], pageCards: [card],
    annotationInfos: new Map(), annotationsPromises: new Map(), calls: [], layer,
    document: { createElement: tag => ({ tag, dataset: {}, style: {}, children: [], append(...nodes) { this.children.push(...nodes); } }) },
    escapeHTML: text => String(text).replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;'),
  };
  state.engine = { annotations: async index => { state.calls.push(index); return infos; } };
  return load(state, code, { expose: { place: 'annotationRemarkPlacement', position: 'positionAnnotationRemarks', apply: 'applyAnnotationRemarks', fetch: 'loadAnnotationInfos', cancel: 'cancelPageAnnotationRequests', printNotes: 'printAnnotationRemarks', loadAnnotationInfos: 'loadAnnotationInfos', printAnnotationRemarks: 'printAnnotationRemarks' } });
}
{
  const state = setup();
  let cancelled = 0, resolve;
  state.engine.annotations = () => {
    const request = new Promise(done => { resolve = done; });
    request.cancel = () => { cancelled++; };
    return request;
  };
  const old = state.fetch(0);
  state.cancel();
  state.annotationsPromises.clear();
  resolve([]);
  await assert.rejects(old, /请求已失效/);
  assert.equal(cancelled, 1, '文档清理时取消逐页备注请求');
  assert.equal(state.annotationInfos.size, 0, '已取消请求不能写入缓存');
}
const page = { width: 100, height: 200 };
const boundary = { x: 10, y: 20, width: 30, height: 40 };
for (const no_zoom of [false, true]) for (const no_rotate of [false, true]) {
  const state = setup();
  for (const [rotation, x, y] of [[0,20,40],[90,360,20],[180,180,360],[270,40,180]]) {
    const got = state.place({ boundary, no_zoom, no_rotate }, page, 2, rotation);
    assert.deepEqual(got, { x, y, scale: no_zoom ? 1 : 2, rotation: no_rotate ? 0 : rotation });
  }
  assert.equal(state.place({}, page), null);
  assert.equal(state.place({ boundary: { x: NaN, y: 0 } }, page), null);
}
const infos = [
  { page: 0, visible: true, remark: '<script>危险文本</script>', boundary, print: true, no_zoom: true, no_rotate: true },
  { page: 0, visible: true, remark: '仅屏幕', boundary, print: false },
  { page: 0, visible: false, remark: '隐藏备注', boundary, print: true },
  { page: 1, visible: true, remark: '另一页', boundary, print: true },
  { page: 0, visible: true, remark: '无外观定位的备注', print: true },
];
{
  const state = setup(infos);
  await state.apply(0);
  assert.deepEqual(state.calls, [0]);
  assert.equal(state.layer.children.length, 2);
  const note = state.layer.children[0];
  assert.equal(note.children[1].textContent, '<script>危险文本</script>');
  assert.equal(note.style.transform, 'rotate(0deg) scale(1)');
  note.open = true;
  state.zoom = 3; state.pageRotation = 90;
  state.position(0);
  assert.equal(note.style.left, '540px');
  assert.equal(note.style.top, '30px');
  assert.equal(note.style.transform, 'rotate(0deg) scale(1)');
  assert.equal(note.open, true, '重排不能关闭用户展开的备注');
  assert.equal(state.layer.children[1].style.transform, 'rotate(90deg) scale(3)');
  const html = state.printNotes(infos, 0);
  assert.ok(html.includes('&lt;script&gt;危险文本&lt;/script&gt;'));
  assert.ok(html.includes('无外观定位的备注'));
  for (const excluded of ['仅屏幕','隐藏备注','另一页']) assert.ok(!html.includes(excluded));
  assert.equal(state.printNotes(infos, 2), '');
}
{
  const state = setup(infos);
  await Promise.all([state.fetch(0), state.fetch(0)]);
  assert.equal(state.calls.length, 1, '并发加载同一页只发一次请求');
  await state.fetch(1);
  assert.deepEqual(state.calls, [0,1]);
  for (let page = 2; page < 140; page++) await state.fetch(page);
  assert.equal(state.annotationInfos.size, 128, '逐页缓存必须有界');
}
{
  const state = setup(infos);
  state.engine.nativeDesktop = true;
  await state.apply(0);
  assert.equal(state.layer.children.length, 2);
  state.engine.annotations = async () => { throw new Error('权限已过期'); };
  await state.apply(0);
  assert.equal(state.layer.children.length, 0, '权限过期后清除旧备注');
  assert.match(state.layer.title, /权限已过期/);
}
{
  const state = setup(infos);
  state.engine.nativeDesktop = true;
  let rejectOld, resolveNew, calls = 0;
  state.engine.annotations = () => new Promise((resolve, reject) => { if (calls++ === 0) rejectOld = reject; else resolveNew = resolve; });
  const old = state.apply(0);
  state.annotationsPromises.clear();
  const newest = state.apply(0);
  resolveNew(infos);
  await newest;
  rejectOld(new Error('旧请求失败'));
  await old;
  assert.equal(state.layer.children.length, 2, '旧请求失败不能清除新备注');
}
{
  const state = setup();
  let resolve;
  state.engine.annotations = () => new Promise(done => { resolve = done; });
  const pending = state.apply(0);
  state.documentGeneration++;
  resolve(infos);
  await pending;
  assert.equal(state.annotationInfos.size, 0);
  assert.equal(state.layer.children.length, 0, '旧文档响应不能填入新视图');
}
{
  const state = setup();
  let resolve;
  state.engine.annotations = () => new Promise(done => { resolve = done; });
  const pending = state.apply(0);
  state.pageCards[0] = {};
  resolve(infos);
  await pending;
  assert.equal(state.layer.children.length, 0, '回收的页面不能被延迟响应重新填充');
}
{
  const state = setup(infos);
  let failed = true;
  state.engine.annotations = async () => { if (failed) throw new Error('损坏'); return infos; };
  await assert.rejects(state.fetch(0), /损坏/);
  failed = false;
  assert.equal((await state.fetch(0)).length, infos.length, '失败请求不得永久占据缓存');
}
// 打印经过真实入口，页面图像始终保留，只筛选附带Remark。
{
  const state = setup(infos);
  const writes = [];
  const popup = { document: { write: html => writes.push(html), open() {}, close() {}, images: [] }, close() {}, focus() {}, print() {} };
  Object.assign(state, {
    documentActionBusy: false, documentActionCancelRequested: false, layerUpdates: new Map(), window: { open: () => popup },
    requirePagePermission() {}, setStatus() {}, setDocumentActionBusy() {}, throwIfDocumentActionCancelled() {},
    loadImage: async () => 'blob:original-appearance', documentName: { textContent: '测试文档' },
  });
  state.engine.checkPermission = async () => {};
  load(state, sliceFunction('printSelectedPages'), { expose: { print: 'printSelectedPages' } });
  await state.print([0]);
  const html = writes.at(-1);
  assert.ok(html.includes('<img src="blob:original-appearance"'), 'Print=false不得排除整页外观');
  assert.ok(html.includes('无外观定位的备注'));
  assert.ok(!html.includes('仅屏幕'));
}
