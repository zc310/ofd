import assert from 'node:assert/strict';
import { createLinkHistoryHarness } from '../../ofd-wasm/web/test/link-history-harness.mjs';
import { viewerSource } from '../../ofd-wasm/web/test/harness.mjs';

function mouse(s, type, button, options = {}) {
  const event = {
    type, button, buttons: type === 'mousedown' ? (button === 3 ? 8 : 16) : 0,
    prevented: false, stopped: false,
    preventDefault() { event.prevented = true; },
    stopImmediatePropagation() { event.stopped = true; },
    ...options,
  };
  s.mouse(event);
  return event;
}

function key(s, name, options = {}) {
  const event = {
    key: name, prevented: false, stopped: false,
    preventDefault() { event.prevented = true; },
    stopImmediatePropagation() { event.stopped = true; },
    ...options,
  };
  s.key(event);
  return event;
}

for (const layout of ['single', 'double', 'double-odd-left']) {
  for (const compressed of [false, true]) {
    const h = createLinkHistoryHarness({ native: true, compressed }), s = h.state;
    s.pageLayout = layout;
    const source = s.capture();
    s.jump(5, { type: 'XYZ', top: 70, left: 30, zoom: 2.5 });
    const destination = s.capture();
    for (let i = 0; i < 3; i++) {
      const back = mouse(s, 'mousedown', 3);
      assert.ok(back.prevented && back.stopped, '侧键必须拦截 WebView 默认导航和事件传播');
      assert.deepEqual(s.capture(), source, `后退恢复完整视图：${layout}/压缩${compressed}`);
      mouse(s, 'mouseup', 3); mouse(s, 'auxclick', 3);
      assert.deepEqual(s.capture(), source, '按下、松开、auxclick 不能重复后退');
      mouse(s, 'mousedown', 4);
      assert.deepEqual(s.capture(), destination, '前进恢复目标位置和缩放');
      mouse(s, 'mouseup', 4); mouse(s, 'auxclick', 4);
      assert.equal(s.linkNavigationHistory.back.length, 1);
      assert.equal(s.linkNavigationHistory.forward.length, 0);
    }
    assert.equal(h.entries.length, 1, 'Wails 不得写入或修改 WebView 会话历史');
    assert.equal(s.window.history.scrollRestoration, 'auto');
    assert.equal(s.remoteLoads, 0);
  }
}

{
  const h = createLinkHistoryHarness({ native: true }), s = h.state;
  Object.defineProperty(s.window, 'history', { get() { throw new Error('不允许读取 WebView History API'); } });
  const original = s.capture();
  s.jump(3);
  assert.equal(s.nativeMove(-1), true);
  assert.deepEqual(s.capture(), original);
  s.reset();
  assert.equal(s.linkNavigationHistory, undefined, '无需 History API 即可清理 Wails 导航栈');
}

{
  const s = createLinkHistoryHarness({ native: true }).state;
  const source = s.capture();
  s.jump(0, { top: 100, type: 'XYZ' });
  const first = s.capture();
  s.jump(5);
  s.nativeMove(-1);
  assert.deepEqual(s.capture(), first, '同页书签也可记录和恢复');
  s.jump(2);
  assert.equal(s.linkNavigationHistory.forward.length, 0, '后退后选新目标截断前进分支');
  assert.equal(s.nativeMove(1), false);
  s.nativeMove(-1); s.nativeMove(-1);
  assert.deepEqual(s.capture(), source);
  assert.equal(s.nativeMove(-1), false, '空历史无副作用');
}

{
  const s = createLinkHistoryHarness({ native: true }).state;
  s.jump(4);
  for (const [back, forward, options] of [
    ['BrowserBack', 'BrowserForward', {}],
    ['ArrowLeft', 'ArrowRight', { altKey: true }],
  ]) {
    const event = key(s, back, options);
    assert.ok(event.prevented && event.stopped, '键盘导航不得继续触发文档翻页或 WebView 导航');
    assert.equal(s.current, 0);
    key(s, forward, options);
    assert.equal(s.current, 4);
  }
  key(s, 'BrowserBack', { repeat: true });
  key(s, 'ArrowLeft');
  key(s, 'ArrowLeft', { altKey: true, ctrlKey: true });
  key(s, 'ArrowLeft', { altKey: true, shiftKey: true });
  assert.equal(s.current, 4, '重复键、普通翻页方向键或组合键不能误触历史');
  const editing = key(s, 'ArrowLeft', { altKey: true, target: { closest: () => ({}) } });
  assert.equal(editing.prevented, false, '输入框键盘编辑不能被截获');
}

for (const busy of ['opening', 'versionSwitching', 'documentActionBusy', 'nativeSelecting', 'modal', 'external', 'engineSelecting']) {
  const s = createLinkHistoryHarness({ native: true }).state;
  s.jump(4);
  if (busy === 'external') s.desktopExternalLinkRequest = {};
  else if (busy === 'engineSelecting') s.engine.selectionPending = true;
  else s[busy] = true;
  const before = s.capture();
  assert.equal(s.nativeMove(-1), false, `${busy} 期间不能切换阅读位置`);
  const blocked = mouse(s, 'mousedown', 3);
  assert.ok(blocked.prevented && blocked.stopped, '忙碌时仍要阻止 WebView 默认导航');
  assert.deepEqual(s.capture(), before);
  assert.equal(s.linkNavigationHistory.back.length, 1);
}

{
  const s = createLinkHistoryHarness({ native: true }).state;
  s.jump(4);
  s.mountPageSpread = () => { throw new Error('恢复失败'); };
  assert.equal(s.nativeMove(-1), false);
  assert.equal(s.linkNavigationHistory.back.length, 1, '恢复失败不能提前弹出后退记录');
  assert.equal(s.linkNavigationHistory.forward.length, 0);
  assert.equal(s.linkHistoryRestoring, false, '恢复失败仍需释放正在恢复标志');
  assert.equal(s.statuses.at(-1), '恢复失败');
}

{
  const s = createLinkHistoryHarness({ native: true }).state;
  const before = s.capture();
  s.jump(4);
  s.pageLayout = 'double';
  s.pageRotation = 90;
  s.zoom = 3;
  s.window.scrollY += 50;
  s.pagesElement.scrollLeft = 200;
  const changed = s.capture();
  s.nativeMove(-1);
  assert.deepEqual(s.capture(), before, '后退可跨越后来修改的缩放、布局和旋转');
  s.nativeMove(1);
  assert.deepEqual(s.capture(), changed, '前进保留离开目标时的实际视图');
}

{
  const s = createLinkHistoryHarness({ native: true }).state;
  for (let index = 0; index < 150; index++) s.jump(index % 8, { type: 'XYZ', top: index });
  assert.equal(s.linkNavigationHistory.back.length, 128);
  for (let index = 0; index < 128; index++) assert.equal(s.nativeMove(-1), true);
  assert.equal(s.linkNavigationHistory.forward.length, 128);
  assert.equal(s.nativeMove(-1), false, '过老历史已淘汰');
  s.documentGeneration++;
  assert.equal(s.nativeMove(1), false, '新文档或新版本不能恢复旧页码');
  s.reset();
  assert.equal(s.linkNavigationHistory, undefined);
}

{
  const s = createLinkHistoryHarness({ native: false }).state;
  s.jump(4);
  assert.equal(mouse(s, 'mousedown', 3).prevented, false, '浏览器侧键继续使用原生会话历史');
  assert.equal(key(s, 'BrowserBack').prevented, false);
  assert.equal(s.nativeMove(-1), false);
  s.engine.nativeDesktop = true;
  assert.equal(mouse(s, 'mousedown', 0).prevented, false, '不能拦截普通左键点击');
  assert.equal(mouse(s, 'mousedown', 1).prevented, false, '不能拦截中键');
  assert.equal(mouse(s, 'mousedown', 2).prevented, false, '不能拦截右键');
}

const source = viewerSource();
assert.match(source, /if \(engine\.nativeDesktop\) \{\s*for \(const type of \['mousedown', 'mouseup', 'auxclick'\]\)/);
assert.match(source, /addEventListener\(type, handleDesktopHistoryMouse, true\)/);
assert.match(source, /addEventListener\('keydown', handleDesktopHistoryKey, true\)/);
console.log('Wails 鼠标侧键及键盘导航：单双页、虚拟轨道、同页书签、分支回收、弹窗阻断和 WebView 历史隔离测试通过');
