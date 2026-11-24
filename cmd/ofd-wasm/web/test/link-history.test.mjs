// 用真实跳转、压缩轨道换算和浏览器会话历史桩覆盖侧键对应的 popstate。
import assert from 'node:assert/strict';
import { sliceFunction, viewerSource } from './harness.mjs';
import { createLinkHistoryHarness as harness } from './link-history-harness.mjs';

for (const layout of ['single', 'double', 'double-odd-left']) {
  for (const compressed of [false, true]) {
    const h = harness({ compressed }), s = h.state;
    s.pageLayout = layout;
    const source = s.capture();
    s.jump(5, { type: 'XYZ', top: 70, left: 30, zoom: 2.5 });
    const destination = s.capture();
    assert.equal(h.entries.length, 2, '一次内部跳转只创建一条新浏览器历史');
    assert.equal(s.window.history.scrollRestoration, 'manual', '禁止浏览器默认滚动恢复覆盖阅读器定位');
    assert.equal(h.entries[0].restoration, 'manual', '来源条目也必须在 push 前禁用自动滚动恢复');
    assert.equal(s.lastBehavior, 'instant', '快速侧键不能记录到平滑滚动的半途');
    assert.equal(h.entries[0].state.preserved, 'other-feature', '不覆盖其他功能的 history.state 字段');
    assert.equal(h.entries[0].url, h.entries[1].url, '页内导航不改写远程文档地址');
    for (let i = 0; i < 3; i++) {
      h.move(-1);
      assert.deepEqual(s.capture(), source, `后退恢复原缩放和滚动：${layout}/压缩${compressed}`);
      h.move(1);
      assert.deepEqual(s.capture(), destination, `前进恢复目标：${layout}/压缩${compressed}`);
    }
    assert.equal(h.entries.length, 2, 'popstate 恢复不能再次 pushState 或膨胀历史');
    assert.ok(s.suppressedActions > 0, '恢复时设置抑制页面进入动作标记，避免自动跳转循环');
    assert.equal(s.linkHistoryRestoring, false);
    assert.equal(s.remoteLoads, 0, '页内前进后退不能重新下载或打开当前远程文档');
  }
}

{
  const h = harness(), s = h.state;
  const source = s.capture();
  s.jump(0, { type: 'XYZ', top: 400, zoom: 2 });
  assert.equal(h.entries.length, 2, '同页的不同书签位置也能后退');
  h.move(-1);
  assert.deepEqual(s.capture(), source);
  s.jump(3, { type: 'XYZ', top: 80 });
  assert.equal(h.entries.length, 2, '后退后选择新目标应截断前进分支');
  assert.equal(s.linkNavigationHistory.locations.size, 2, '同时释放被截断分支的快照');
  h.move(1);
  assert.equal(s.current, 3, '被截断分支不再可前进');
  s.jump(3, { type: 'XYZ', top: 80 });
  assert.equal(h.entries.length, 2, '重复定位到相同落点不增加历史');
}

{
  const h = harness(), s = h.state;
  s.jump(4);
  s.zoom = 3;
  s.window.scrollY += 50;
  s.pagesElement.scrollLeft = 150;
  const adjusted = s.capture();
  h.move(-1);
  h.move(1);
  assert.deepEqual(s.capture(), adjusted, '离开书签目标时保存手工滚动及缩放，前进不退回旧落点');
}

{
  const h = harness(), s = h.state;
  const original = s.capture();
  s.jump(5, { top: 40, type: 'XYZ' });
  s.pageLayout = 'double';
  s.pageRotation = 90;
  s.zoom = 3;
  const changed = s.capture();
  h.move(-1);
  assert.deepEqual(s.capture(), original, '历史可恢复单页布局及旋转角度');
  h.move(1);
  assert.deepEqual(s.capture(), changed, '前进恢复双页布局、旋转及缩放');
}

{
  const h = harness(), s = h.state;
  s.jump(2);
  const old = h.entries[0].state;
  s.reset();
  assert.equal(s.linkNavigationHistory, undefined, '关闭、换文档或版本时释放所有内存快照');
  assert.equal(s.window.history.scrollRestoration, 'auto', '清理后恢复浏览器原来的滚动恢复设置');
  s.documentGeneration++;
  s.current = 1;
  assert.equal(s.pop({ state: old }), false, '旧文档的浏览器记录不能套用到新文档页码');
  assert.equal(s.current, 1);
  s.remoteSource = { href: 'https://other.example/second.ofd' };
  h.move(-1);
  assert.equal(s.remoteLoads, 1, '旧记录仍交由现有远程文档加载流程处理');
}

{
  const h = harness(), s = h.state;
  s.jump(-1); s.jump(8); s.jump(0.5);
  assert.equal(h.entries.length, 1, '无效目标不创建历史');
  s.normalPage(2);
  assert.equal(h.entries.length, 1, '普通翻页不创建书签导航历史');
  s.pageEntryActionDepth = 1;
  s.jump(3);
  assert.equal(h.entries.length, 1, '页面自动动作不创建用户跳转历史');
  s.pageEntryActionDepth = 0;
  s.opening = true; s.jump(4); s.opening = false;
  s.versionSwitching = true; s.jump(5); s.versionSwitching = false;
  s.documentActionBusy = true; s.jump(6); s.documentActionBusy = false;
  assert.equal(h.entries.length, 1, '打开、切版本或文档操作期间不创建历史');
}

{
  const h = harness(), s = h.state;
  for (let index = 0; index < 150; index++) s.jump(index % 8, { top: index, type: 'XYZ' });
  assert.equal(s.linkNavigationHistory.locations.size, 128, '内存最多保留128个轻量位置快照');
  const page = s.current;
  assert.equal(s.pop({ state: h.entries[0].state }), true, '过期的同会话记录不交给远程加载');
  assert.equal(s.current, page);
  assert.equal(s.statuses.at(-1), '该跳转历史已过期');
}

{
  const h = harness(), s = h.state;
  s.pageEntryActionDepth = 1;
  s.jump(2, { type: 'XYZ', top: 50, left: 100 });
  assert.equal(h.callbacks.length, 1, '非用户动作保留延迟横向定位');
  s.pageEntryActionDepth = 0;
  const source = s.capture();
  s.jump(4, { type: 'XYZ', top: 70, left: 80 });
  h.move(-1);
  const restored = s.capture();
  assert.deepEqual(restored, source);
  h.callbacks.forEach(callback => callback());
  assert.deepEqual(s.capture(), restored, '迟到的旧目标帧不能覆盖刚恢复的横向位置');
}

{
  const h = harness({ native: true }), s = h.state;
  s.jump(2);
  assert.equal(h.entries.length, 1, '共享网页不改变原生桌面端的浏览器历史行为');
  assert.equal(s.lastBehavior, 'instant', '原生端也即时定位并记录内存导航历史');
  assert.equal(s.linkNavigationHistory.back.length, 1);
  const h2 = harness(), s2 = h2.state;
  s2.window.history.pushState = () => { throw new Error('History API disabled'); };
  assert.doesNotThrow(() => s2.jump(4));
  assert.equal(s2.current, 4, 'History API 不可用时不能阻断正常书签跳转');
}

{
  const h = harness(), s = h.state;
  s.jump(3);
  const marker = h.entries[0].state;
  s.documentGeneration++;
  assert.equal(s.pop({ state: marker }), false, '异步打开导致代次变化后不能恢复旧历史');
  const source = viewerSource();
  assert.match(sliceFunction('clearDocumentView'), /resetLinkNavigationHistory\(\)/);
  assert.match(sliceFunction('cancelOpening'), /resetLinkNavigationHistory\(\)/);
  assert.match(sliceFunction('setCurrent'), /changed && !linkHistoryRestoring/);
  assert.match(source, /window\.addEventListener\('popstate', \(event\) =>/);
}

console.log('WASM 书签/目录历史：前进后退、同页定位、压缩轨道、分支回收、模式恢复及远程加载隔离测试通过');
