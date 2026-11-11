import assert from 'node:assert/strict';
import { bind, load, sliceFunction, sliceListenerHandler } from './harness.mjs';

const code = ['localFontsSupported', 'configureLocalFontsOption', 'applyLocalFontsIntent'].map(sliceFunction).join('\n');
for (const nativeDesktop of [true, false]) {
  const option = { hidden: false, style: {} };
  const state = {
    engine: { nativeDesktop }, window: { queryLocalFonts() { throw new Error('不能调用字体枚举'); } },
    localFontsSelect: { checked: false, disabled: false, closest: () => option },
    localFontsStorageKey: 'fonts', reads: 0, writes: 0, requests: 0,
  };
  state.localStorage = { getItem: () => { state.reads++; return 'true'; }, setItem: () => { state.writes++; } };
  state.setLocalFonts = () => { state.requests++; };
  state.localFontsActive = false;
  load(state, code, { expose: { configure: 'configureLocalFontsOption', supported: 'localFontsSupported', intent: 'applyLocalFontsIntent' } });
  state.configure();
  assert.equal(state.supported(), !nativeDesktop);
  assert.equal(option.hidden, nativeDesktop, '仅桌面隐藏整行选项');
  assert.equal(state.localFontsSelect.disabled, nativeDesktop);
  assert.equal(state.intent(), !nativeDesktop);
  bind(state, sliceListenerHandler("localFontsSelect?.addEventListener", 'change'))();
  if (nativeDesktop) {
    assert.equal(option.style.display, 'none', '样式不能覆盖隐藏状态');
    assert.equal(state.reads, 0, '桌面不恢复浏览器授权意向');
    assert.equal(state.writes, 0, '桌面不修改浏览器授权意向');
    assert.equal(state.requests, 0, '桌面不发起浏览器字体请求');
  } else {
    assert.equal(state.localFontsSelect.checked, true);
    assert.equal(state.requests, 1, '浏览器原有字体开关仍然可用');
  }
}
console.log('桌面隐藏系统字体授权选项，浏览器行为保留测试通过');
