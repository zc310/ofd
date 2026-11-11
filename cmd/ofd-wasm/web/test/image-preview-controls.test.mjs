import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

// 用真实预览函数驱动几何与生命周期；桩只提供 DOM 和尺寸。
function node(tag = '') {
  const classes = new Set(), captures = new Set();
  return {
    tag, children: [], style: {}, isConnected: true, clientWidth: 800, clientHeight: 600,
    classList: { add: name => classes.add(name), remove: name => classes.delete(name),
      contains: name => classes.has(name), toggle: (name, on) => on ? classes.add(name) : classes.delete(name) },
    append(...items) { this.children.push(...items); items.forEach(item => { item.parent = this; }); },
    replaceWith(item) { this.parent.children[this.parent.children.indexOf(this)] = item; item.parent = this.parent; },
    setAttribute(name, value) { this[name] = value; },
    addEventListener(type, listener, options) { this[type] = listener; this[`${type}Options`] = options; },
    showModal() { this.open = true; }, close() { this.open = false; }, remove() { this.removed = true; },
    getBoundingClientRect: () => ({ left: 20, top: 30 }),
    setPointerCapture: id => captures.add(id), hasPointerCapture: id => captures.has(id),
    releasePointerCapture: id => captures.delete(id), focus() { this.focused = true; },
  };
}
function fixture({ fallback = false } = {}) {
  const observers = [], listeners = new Map(), urls = [], revoked = [];
  const state = {
    document: { createElement: node, body: node('body') },
    ResizeObserver: fallback ? undefined : class {
      constructor(callback) { this.callback = callback; observers.push(this); }
      observe(element) { this.element = element; }
      disconnect() { this.disconnected = true; }
    },
    window: { addEventListener: (type, handler) => listeners.set(type, handler),
      removeEventListener: (type, handler) => { assert.equal(listeners.get(type), handler); listeners.delete(type); } },
    documentGeneration: 1, desktopMediaPreviewRevision: 0, desktopMediaDialog: undefined,
    mediaPreviewRequests: new Set(), Blob,
    URL: { createObjectURL: () => { const url = `blob:${urls.length}`; urls.push(url); return url; }, revokeObjectURL: url => revoked.push(url) },
    setStatus: value => { state.status = value; }, engine: { nativeDesktop: false }, mediaMime: () => 'image/png',
  };
  load(state, ['createImagePreviewControls', 'openDesktopImagePreview', 'openDesktopMediaPreview', 'closeDesktopMediaPreview'].map(sliceFunction).join('\n'),
    { expose: { create: 'createImagePreviewControls', open: 'openDesktopImagePreview', openResource: 'openDesktopMediaPreview', close: 'closeDesktopMediaPreview' } });
  const dialog = node('dialog'), image = node('img');
  const surface = state.create(dialog, image);
  const [toolbar, viewport] = surface.children;
  const [smaller, ratio, larger, original, fit] = toolbar.children;
  const resize = () => fallback ? listeners.get('resize')?.() : observers[0].callback();
  const decode = (width = 2000, height = 1000) => {
    image.naturalWidth = width; image.naturalHeight = height; image.load();
  };
  return { state, dialog, image, surface, viewport, smaller, ratio, larger, original, fit, resize, decode, observers, listeners, revoked, urls };
}
const transform = image => image.style.transform.match(/translate\(([-\d.e+]+)px, ([-\d.e+]+)px\) scale\(([-\d.e+]+)\)/).slice(1).map(Number);
const near = (actual, expected) => assert.ok(Math.abs(actual - expected) < 0.00001, `${actual} != ${expected}`);
const event = extra => ({ prevented: false, stopped: false, preventDefault() { this.prevented = true; }, stopPropagation() { this.stopped = true; }, ...extra });

{
  const f = fixture();
  assert.ok(f.larger.disabled && f.fit.disabled, '解码完成前禁用操作');
  f.decode();
  assert.deepEqual(transform(f.image), [0, 100, 0.4], '默认适应窗口并居中');
  assert.equal(f.ratio.textContent, '40%');
  assert.equal(f.fit['aria-pressed'], 'true');
  assert.equal(f.image.draggable, false);
  f.original.click();
  assert.deepEqual(transform(f.image), [-600, -200, 1], '100% 围绕窗口中心放大');
  assert.equal(f.viewport.classList.contains('pannable'), true);
  f.larger.click(); near(transform(f.image)[2], 1.25);
  f.smaller.click(); near(transform(f.image)[2], 1);
  for (let i = 0; i < 40; i++) f.larger.click();
  assert.equal(f.ratio.textContent, '800%'); assert.ok(f.larger.disabled);
  for (let i = 0; i < 80; i++) f.smaller.click();
  assert.equal(f.ratio.textContent, '10%'); assert.ok(f.smaller.disabled);
  f.fit.click();
  assert.deepEqual(transform(f.image), [0, 100, 0.4]);
  f.decode(80, 40);
  assert.deepEqual(transform(f.image), [360, 280, 1], '小图默认不放大');
  assert.equal(f.image.style.width, '80px');
  f.dialog.previewCleanup();
}

{
  const f = fixture(); f.decode(); f.original.click();
  const before = transform(f.image), ax = 200, ay = 150;
  const wheel = event({ deltaY: -100, deltaMode: 0, clientX: ax + 20, clientY: ay + 30 });
  f.viewport.wheel(wheel);
  const after = transform(f.image);
  near((ax - before[0]) / before[2], (ax - after[0]) / after[2]);
  near((ay - before[1]) / before[2], (ay - after[1]) / after[2]);
  assert.ok(wheel.prevented && wheel.stopped, '滚轮只缩放弹窗，不滚动底层文档');
  assert.equal(f.viewport.wheelOptions.passive, false);
  f.viewport.wheel(event({ deltaY: Number.MAX_VALUE, deltaMode: 2, clientX: 220, clientY: 180 }));
  assert.ok(transform(f.image).every(Number.isFinite), '异常大滚轮增量也保持有界');
  f.dialog.previewCleanup();
}

{
  const f = fixture(); f.decode(); f.original.click();
  const down = event({ pointerId: 1, button: 0, clientX: 300, clientY: 200 });
  f.viewport.pointerdown(down);
  assert.ok(down.prevented && f.viewport.focused && f.viewport.hasPointerCapture(1));
  f.viewport.pointermove(event({ pointerId: 2, buttons: 1, clientX: 600, clientY: 500 }));
  assert.deepEqual(transform(f.image), [-600, -200, 1], '其他触点不抢占拖动');
  f.viewport.pointermove(event({ pointerId: 1, buttons: 1, clientX: 400, clientY: 250 }));
  assert.deepEqual(transform(f.image), [-500, -150, 1]);
  f.viewport.pointermove(event({ pointerId: 1, buttons: 1, clientX: 10000, clientY: -10000 }));
  assert.deepEqual(transform(f.image), [0, -400, 1], '拖动限制在图像边缘，不丢失图片');
  f.viewport.pointercancel(event({ pointerId: 1 }));
  assert.ok(!f.viewport.hasPointerCapture(1) && !f.viewport.classList.contains('dragging'));
  f.viewport.pointerdown(event({ pointerId: 3, button: 2 }));
  assert.equal(f.viewport.hasPointerCapture(3), false, '右键不拖动');
  f.viewport.pointerdown(event({ pointerId: 3, button: 0, isPrimary: false }));
  assert.equal(f.viewport.hasPointerCapture(3), false, '非主触点不拖动');
  f.viewport.pointerdown(event({ pointerId: 3, button: 0, clientX: 100, clientY: 100 }));
  f.viewport.pointermove(event({ pointerId: 3, buttons: 0, clientX: 200, clientY: 200 }));
  assert.equal(f.viewport.hasPointerCapture(3), false, '按钮已释放时停止拖动');
  f.fit.click();
  f.viewport.pointerdown(event({ pointerId: 4, button: 0 }));
  assert.equal(f.viewport.hasPointerCapture(4), false, '无溢出时不劫持点击');
  f.dialog.previewCleanup();
}

{
  const f = fixture(); f.decode();
  f.viewport.clientWidth = 400; f.viewport.clientHeight = 300; f.resize();
  assert.deepEqual(transform(f.image), [0, 50, 0.2], '适应模式随窗口尺寸更新');
  f.original.click();
  f.viewport.clientWidth = 600; f.viewport.clientHeight = 400; f.resize();
  assert.deepEqual(transform(f.image), [-700, -300, 1], '手动缩放模式保持比例和中心');
  const key = key => event({ key, target: f.viewport });
  const right = key('ArrowRight'); f.dialog.keydown(right);
  assert.deepEqual(transform(f.image), [-740, -300, 1]); assert.ok(right.prevented);
  const fit = key('F'); f.dialog.keydown(fit);
  assert.deepEqual(transform(f.image), [0, 50, 0.3]);
  f.dialog.keydown(key('0')); assert.equal(transform(f.image)[2], 1);
  const plus = key('+'); f.dialog.keydown(plus); assert.equal(transform(f.image)[2], 1.25);
  const modified = event({ key: '+', ctrlKey: true }); f.dialog.keydown(modified);
  assert.equal(modified.prevented, false, '不接管浏览器修饰键快捷键');
  f.viewport.pointerdown(event({ pointerId: 1, button: 0, clientX: 200, clientY: 200 }));
  f.dialog.previewCleanup();
  assert.equal(f.viewport.hasPointerCapture(1), false, '关闭释放指针捕获');
  assert.equal(f.observers[0].disconnected, true);
  const before = f.image.style.transform;
  f.decode(300, 200); f.resize(); f.larger.click();
  assert.equal(f.image.style.transform, before, '迟到 load/resize 和旧按钮不能恢复已关闭的图片');
}

{
  const f = fixture({ fallback: true });
  f.decode(1000, 10000);
  assert.equal(transform(f.image)[2], 0.06, '极长图片能完整适应，不受手动缩放下限裁切');
  f.smaller.click(); assert.equal(transform(f.image)[2], 0.06);
  f.viewport.clientWidth = 300; f.viewport.clientHeight = 200; f.fit.click(); f.resize();
  assert.equal(transform(f.image)[2], 0.02);
  f.dialog.previewCleanup(); assert.equal(f.listeners.size, 0, '无 ResizeObserver 时移除全局 resize 监听');
}

{
  const f = fixture(), calls = [];
  let resolve, cancelled = 0;
  f.state.engine.mediaData = (...args) => {
    calls.push(args);
    const request = new Promise(done => { resolve = done; });
    request.cancel = () => { cancelled++; };
    return request;
  };
  const button = node('button');
  const pending = f.state.openResource(button, { scope: 2, id: 7, name: '资源.png' });
  assert.deepEqual(calls, [[2, 7, 0]], '浏览器资源保留原读取参数及权限语义');
  resolve(new Uint8Array([1])); await pending;
  const dialog = f.state.desktopMediaDialog;
  const [toolbar, viewport] = dialog.children[1].children;
  const image = viewport.children[0];
  image.naturalWidth = 2000; image.naturalHeight = 1000; image.load();
  toolbar.children[3].click();
  viewport.pointerdown(event({ pointerId: 8, button: 0, clientX: 200, clientY: 200 }));
  const escape = event({ key: 'Escape' }); dialog.keydown(escape);
  assert.ok(escape.stopped && !escape.prevented, '隔离底层快捷键，但保留原生 Esc cancel');
  dialog.cancel(event({}));
  assert.equal(f.state.desktopMediaDialog, undefined);
  assert.equal(viewport.hasPointerCapture(8), false);
  assert.equal(f.observers.at(-1).disconnected, true);
  assert.deepEqual(f.revoked, ['blob:0']);
  const late = f.state.openResource(button, { scope: 0, id: 8 });
  f.state.close(); resolve(new Uint8Array([2])); await late;
  assert.equal(cancelled, 1, '包装浏览器请求后仍可取消源请求');
  assert.equal(f.urls.length, 1, '关闭后迟到资源不创建图片 URL');
  f.state.engine.mediaData = () => Promise.reject(new Error('导出权限拒绝'));
  await f.state.openResource(button, { scope: 0, id: 8 });
  assert.match(f.state.status, /导出权限拒绝/);
  assert.equal(f.state.desktopMediaDialog, undefined, '浏览器资源权限拒绝关闭预览，不加载图片');
  assert.equal(f.urls.length, 1);
  assert.equal(f.state.mediaPreviewRequests.size, 0);
  f.dialog.previewCleanup();
}
console.log('图片预览适应、缩放锚点、有界拖动、键盘、窗口尺寸、关闭回收及浏览器资源权限测试通过');
