// 「使用系统字体」的行为测试：勾选意向的跨会话保留、读取时机（首屏渲染前等待 +
// 超时）、手势退路、失败归因，以及勾选框的点击语义。
//
// 被测代码全部从 viewer.js 原文切出（见 harness.mjs），不在测试里重抄逻辑。
import {
  sliceFunction, sliceFunctionUntil, sliceListenerHandler, load, bind,
  viewerSource, createSuite,
} from './harness.mjs';

const suite = createSuite('系统字体');

const code = {
  supported: sliceFunction('localFontsSupported'),
  intent: sliceFunction('applyLocalFontsIntent'),
  hint: sliceFunction('localFontsPendingHint'),
  arm: sliceFunction('armLocalFontsOnGesture'),
  disarm: sliceFunction('disarmLocalFontsOnGesture'),
  wanted: sliceFunction('localFontsIntentWanted'),
  beforeRender: sliceFunction('applyLocalFontsBeforeRender'),
  onOpen: sliceFunction('applyLocalFontsIntentOnOpen'),
  needsGesture: sliceFunction('needsUserGesture'),
  fallback: sliceFunction('handleLocalFontsGestureFallback'),
  fetch: sliceFunction('fetchLocalFontsForDocument'),
};
// 勾选框的 change 回调是内联箭头函数，单独切出，避免在测试里重抄一份语义。
const changeBody = sliceListenerHandler("localFontsSelect?.addEventListener", 'change');

/**
 * 一份覆盖 localFonts* 全部自由变量的最小 state。
 *
 * 桩一律写成闭包引用 state，而不是对象方法：被测代码在 `with (state)` 里以
 * `localStorage.getItem(...)`、`setStatus(...)` 的形式调用，写成方法会把 this
 * 绑到 state 自身属性上（例如 this._store 取的是 localStorage 上的字段）。
 */
function baseState(overrides = {}) {
  const store = new Map();
  const state = {
    localFontsSelect: { checked: false, disabled: false },
    localFontsActive: false,
    localFontsGestureFire: undefined,
    localFontsGesturePending: false,
    localFontsGestureUsed: false,
    localFontsIntentApplied: false,
    localFontsStorageKey: 'ofd-local-fonts',
    localFontsFirstRenderTimeout: 3000,
    localFontFamilies: new Set(),
    pageInfos: [],
    documentGeneration: 0,
    window: { queryLocalFonts: () => {} },
    localStorage: {
      getItem: key => (store.has(key) ? store.get(key) : null),
      setItem: (key, value) => store.set(key, value),
    },
    _store: store,
    _status: [],
    _reads: 0,
    _armed: 0,
    _disarms: 0,
  };
  state.setStatus = message => { state._status.push(message); };
  state.setLocalFonts = async enabled => { if (enabled) state._reads++; };
  state.disarmLocalFontsOnGesture = () => { state._disarms++; };
  state.armLocalFontsOnGesture = () => { state._armed++; return true; };
  return Object.assign(state, overrides);
}

// ------------------------------------------------------------ 勾选意向跨会话保留
suite.group('勾选意向跨会话保留');
for (const [label, stored, supported, want] of [
  ['上次为开 → 恢复为勾选', 'true', true, true],
  ['上次为关 → 保持未勾选', 'false', true, false],
  ['从未设置 → 默认未勾选', null, true, false],
  ['浏览器不支持 → 不恢复（避免显示永远不可用的已勾选项）', 'true', false, false],
]) {
  const state = baseState();
  if (stored !== null) state._store.set('ofd-local-fonts', stored);
  if (!supported) state.window = {};
  load(state, [code.supported, code.intent].join('\n'), { invoke: ['applyLocalFontsIntent'] });
  suite.check(state.localFontsSelect.checked === want, label,
              `checked=${state.localFontsSelect.checked}`);
}

// ------------------------------------------------------------ 勾选框点击语义
suite.group('勾选框点击语义（恢复态尚未生效时按激活处理）');
{
  const click = (checked, active) => {
    const state = baseState({ localFontsSelect: { checked, disabled: false }, localFontsActive: active });
    load(state, [code.supported, code.intent].join('\n'));
    const handler = bind(state, changeBody);
    state.localFontsSelect.checked = !state.localFontsSelect.checked; // 浏览器先翻转勾选
    handler();
    return state;
  };

  const restored = click(true, false);
  suite.check(restored.localFontsSelect.checked === true, '恢复态下点一下不会把选项关掉',
              `checked=${restored.localFontsSelect.checked}`);
  suite.check(restored._reads === 1, '恢复态下点一下触发读取', `reads=${restored._reads}`);

  const disable = click(true, true);
  suite.check(disable.localFontsSelect.checked === false, '已生效后取消 → 保持取消');
  suite.check(disable._reads === 0, '已生效后取消不触发读取');
  suite.check(disable._store.get('ofd-local-fonts') === 'false', '关闭意向被记住',
              `stored=${disable._store.get('ofd-local-fonts')}`);

  const enable = click(false, false);
  suite.check(enable._reads === 1, '从关闭到开启 → 触发读取', `reads=${enable._reads}`);
  suite.check(enable._store.get('ofd-local-fonts') === 'true', '开启意向被记住',
              `stored=${enable._store.get('ofd-local-fonts')}`);
}

// ------------------------------------------------------------ 首屏渲染前读取
suite.group('首屏渲染前读取（消除字体跳变 + 只渲染一遍）');
{
  const make = ({ checked = true, active = false, pages = 3, supported = true,
                  applied = false, slow = false, timeout = 3000 } = {}) => {
    const state = baseState({
      localFontsSelect: { checked, disabled: false },
      localFontsActive: active,
      localFontsIntentApplied: applied,
      localFontsFirstRenderTimeout: timeout,
      pageInfos: new Array(pages).fill({}),
      window: supported ? { queryLocalFonts: () => {} } : {},
    });
    if (!supported) state.window = {};
    state.setLocalFonts = async enabled => {
      if (!enabled) return;
      state._reads++;
      if (slow) await new Promise(resolve => { state._settle = resolve; });
      state.localFontsActive = true;
    };
    load(state, [code.supported, code.wanted, code.beforeRender, code.onOpen].join('\n'),
      { expose: { _before: 'applyLocalFontsBeforeRender', _open: 'applyLocalFontsIntentOnOpen' } });
    return state;
  };
  const tick = () => new Promise(r => setTimeout(r, 5));

  const fast = make({});
  await fast._before();
  suite.check(fast._reads === 1, '读取一次', `reads=${fast._reads}`);
  suite.check(fast.localFontsActive === true, '等待返回时字体已生效');
  suite.check(fast._status.includes('正在读取系统字体...'), '等待期间状态栏有反馈',
              JSON.stringify(fast._status));

  // 这里必须给等待本身加一道兜底：一旦产品代码去掉超时上限，_before() 会永远
  // 不返回，测试就会挂起而不是失败——CI 里挂起比失败更难排查。
  const slow = make({ slow: true, timeout: 60 });
  const started = Date.now();
  const outcome = await Promise.race([
    slow._before().then(() => 'returned'),
    new Promise(resolve => { setTimeout(() => resolve('hung'), 2000); }),
  ]);
  const waited = Date.now() - started;
  suite.check(outcome === 'returned', '读取慢时超时放行，不阻塞文档出现',
              outcome === 'hung' ? '等待 2 秒仍未返回（超时上限被移除）' : `等待 ${waited}ms`);
  suite.check(slow._reads === 1, '超时后后台读取仍在进行', `reads=${slow._reads}`);
  slow._settle();
  await tick();
  suite.check(slow.localFontsActive === true, '后台完成后字体补上生效');

  for (const [label, options] of [
    ['意向为关', { checked: false }],
    ['字体已生效', { active: true }],
    ['无文档', { pages: 0 }],
    ['浏览器不支持', { supported: false }],
    ['本轮已尝试过', { applied: true }],
  ]) {
    const state = make(options);
    await state._before();
    suite.check(state._reads === 0, `${label} → 不读取`, `reads=${state._reads}`);
    suite.check(!state._status.includes('正在读取系统字体...'), `${label} → 不显示等待提示`);
  }

  const already = make({});
  await already._before();
  already._open();
  suite.check(already._reads === 1, 'buildPages 兜底不重复读', `reads=${already._reads}`);

  const fresh = make({});
  fresh._open();
  await tick();
  suite.check(fresh._reads === 1, '未走渲染前路径时兜底仍会读', `reads=${fresh._reads}`);
}

// ------------------------------------------------------------ 手势退路
suite.group('手势退路（仅当直接调用被判定为手势问题时）');
{
  const make = ({ checked = true, active = false, pages = 3, supported = true, used = false } = {}) => {
    const listeners = [];
    const state = baseState({
      localFontsSelect: { checked, disabled: false },
      localFontsActive: active,
      localFontsGestureUsed: used,
      pageInfos: new Array(pages).fill({}),
    });
    state.window = supported ? {
      queryLocalFonts: () => {},
      addEventListener: (type, fn) => listeners.push({ type, fn }),
      removeEventListener: (type, fn) => {
        const at = listeners.findIndex(l => l.type === type && l.fn === fn);
        if (at >= 0) listeners.splice(at, 1);
      },
    } : {};
    load(state, [code.supported, code.hint, code.arm, code.disarm].join('\n'),
      { expose: { _arm: 'armLocalFontsOnGesture', _disarm: 'disarmLocalFontsOnGesture',
                   _hint: 'localFontsPendingHint' } });
    return { state, listeners, fire: () => { const l = listeners[0]; if (l) l.fn(); } };
  };

  const armed = make({});
  armed.state._arm();
  suite.check(armed.listeners.length === 2, '挂上 pointerdown + keydown', `监听数=${armed.listeners.length}`);
  armed.fire();
  suite.check(armed.state._reads === 1, '首次交互触发一次读取', `reads=${armed.state._reads}`);
  suite.check(armed.listeners.length === 0, '触发后监听已摘除', `监听数=${armed.listeners.length}`);
  armed.fire();
  suite.check(armed.state._reads === 1, '再次交互不会重复读取', `reads=${armed.state._reads}`);

  for (const [label, options] of [
    ['意向为关', { checked: false }],
    ['字体已生效', { active: true }],
    ['文档未就绪', { pages: 0 }],
    ['浏览器不支持', { supported: false }],
    ['本轮已退回过', { used: true }],
  ]) {
    const target = make(options);
    target.state._arm();
    suite.check(target.listeners.length === 0, `${label} → 不挂监听`, `监听数=${target.listeners.length}`);
  }

  const repeated = make({});
  repeated.state._arm();
  repeated.state._arm();
  repeated.state._arm();
  suite.check(repeated.listeners.length === 2, '多次挂载只保留一组', `监听数=${repeated.listeners.length}`);
  repeated.fire();
  suite.check(repeated.state._reads === 1, '只触发一次', `reads=${repeated.state._reads}`);

  const manual = make({});
  manual.state._arm();
  manual.state._disarm();
  await manual.state.setLocalFonts(true);
  manual.fire();
  suite.check(manual.state._reads === 1, 'setLocalFonts 先摘除，手动点击不双触发',
              `reads=${manual.state._reads}`);

  const idempotent = make({});
  idempotent.state._arm();
  idempotent.state._disarm();
  idempotent.state._disarm();
  suite.check(idempotent.listeners.length === 0, '重复摘除幂等', `监听数=${idempotent.listeners.length}`);

  const hinted = make({});
  suite.check(hinted.state._hint() === '', '摘除状态下无待办后缀', JSON.stringify(hinted.state._hint()));
  hinted.state._arm();
  suite.check(hinted.state._hint().includes('点击页面任意处'), '挂载后有待办后缀',
              JSON.stringify(hinted.state._hint()));
}

// ------------------------------------------------------------ 失败归因
suite.group('失败归因（区分"无需补齐"与"无匹配字体"）');
{
  const make = ({ used = false } = {}) => {
    const state = baseState({ localFontsGestureUsed: used });
    load(state, [code.needsGesture, code.fallback].join('\n'),
      { expose: { _needs: 'needsUserGesture', _fallback: 'handleLocalFontsGestureFallback' } });
    return state;
  };

  for (const [name, want] of [['SecurityError', true], ['NotAllowedError', true],
                              ['TypeError', false], ['Error', false], [undefined, false]]) {
    const state = make();
    suite.check(state._needs({ name }) === want,
                `${String(name)} → ${want ? '判为需要手势' : '判为普通失败'}`);
  }

  const gesture = make();
  gesture._fallback({ name: 'SecurityError', message: '需要手势' });
  suite.check(gesture._armed === 1, '手势类错误 → 挂监听', `armed=${gesture._armed}`);
  suite.check(gesture._status.some(m => m.includes('点击页面任意处')), '提示用户点击',
              JSON.stringify(gesture._status));

  const plain = make();
  plain._fallback({ name: 'TypeError', message: '坏了' });
  suite.check(plain._armed === 0, '非手势类错误 → 不挂监听', `armed=${plain._armed}`);
  suite.check(plain._status.some(m => m.includes('读取系统字体失败')), '按普通失败提示',
              JSON.stringify(plain._status));

  const already = make({ used: true });
  already._fallback({ name: 'SecurityError', message: '还是不行' });
  suite.check(already._armed === 0, '本轮已退回过 → 不再重挂（防每点一次失败一次）',
              `armed=${already._armed}`);
  suite.check(already._status.some(m => m.includes('读取系统字体失败')), '直接报失败',
              JSON.stringify(already._status));
}

suite.group('fetchLocalFontsForDocument 的返回值');
{
  const run = async entries => {
    const state = baseState({ pageInfos: [{}] });
    state.engine = { addFallbackFont: async () => {} };
    const wanted = new Map(entries);
    // 该函数内直接调用 missingFontFamilies()，这里换成桩。
    const body = code.fetch.replace('await missingFontFamilies()', 'await state_missing()');
    const factory = new Function('state', 'state_missing',
      `with (state) { ${body}\n return fetchLocalFontsForDocument; }`);
    return factory(state, async () => wanted)(Promise.resolve([]), 0);
  };

  const embedded = await run([]);
  suite.check(embedded.registered === 0 && embedded.wanted === 0,
              '文档字体已全部内嵌 → wanted=0', JSON.stringify(embedded));
  suite.check(embedded.unmatched.length === 0, '无缺失字体时 unmatched 为空',
              JSON.stringify(embedded.unmatched));

  const missing = await run([['仿宋', '仿宋_GB2312'], ['黑体', '黑体']]);
  suite.check(missing.registered === 0 && missing.wanted === 2,
              '有缺失但无匹配 → wanted=2', JSON.stringify(missing));
  suite.check(missing.unmatched.includes('仿宋_GB2312'), 'unmatched 列出缺失族名',
              JSON.stringify(missing.unmatched));
}

// ------------------------------------------------- 首屏等待的调用点（结构守卫）
suite.group('首屏渲染前等待的调用位置');
{
  // 行为测试只能证明 applyLocalFontsBeforeRender 本身正确，证明不了它被放在了
  // buildPages 之前——删掉调用点时行为测试察觉不到。这里直接对源码断言顺序。
  const source = viewerSource();
  const at = source.indexOf('async function openSelectedFile');
  const body = source.slice(at, source.indexOf('\nfunction ', at + 1) < 0 ? undefined : source.indexOf('\nfunction ', at + 1));
  const awaitAt = body.indexOf('await applyLocalFontsBeforeRender();');
  const buildAt = body.indexOf('buildPages();');
  suite.check(awaitAt >= 0, 'openSelectedFile 里确实调用了 applyLocalFontsBeforeRender',
              `位置 ${awaitAt}`);
  suite.check(awaitAt >= 0 && buildAt >= 0 && awaitAt < buildAt,
              '等待发生在 buildPages（首次渲染）之前',
              `await@${awaitAt} buildPages@${buildAt}`);
  const resetAt = source.indexOf('localFontsIntentApplied = false;');
  suite.check(resetAt >= 0, '切换/关闭文档时重置"已尝试"标志', `位置 ${resetAt}`);
}

process.exit(suite.report() === 0 ? 0 : 1);
