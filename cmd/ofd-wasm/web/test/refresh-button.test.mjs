// 刷新换版的行为测试：shell 资源走 Service Worker 缓存优先，index.html 每次从
// 网络取，所以新 SW 装好后当前页是"新 HTML + 旧 JS/WASM"的混合状态。刷新按钮
// 要在这种状态下高亮提示，点击后整页 reload 换新；首次访问不能误报。
//
// 被测代码全部从 viewer.js 原文切出（见 harness.mjs），不在测试里重抄逻辑。
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('刷新换版');

const code = [
  sliceFunction('markUpdateAvailable'),
  sliceFunction('dismissUpdateBanner'),
  sliceFunction('watchServiceWorkerUpdate'),
].join('\n');

/** 假按钮：记录 class、title 和 aria-label 的变化。桩一律闭包引用 button，
 * 写成对象方法会把 this 绑到 classList 上。 */
function fakeButton() {
  const button = {
    hidden: true,
    classes: new Set(),
    title: '',
    label: '',
    classList: {
      add(name) { button.classes.add(name); },
    },
    setAttribute(name, value) { if (name === 'aria-label') button.label = value; },
  };
  return button;
}

/**
 * 一份覆盖两个被测函数全部自由变量的最小 state。
 *
 * 桩写成闭包引用 state，避免 `with (state)` 下 this 绑错。
 */
function baseState({ controlled = true, button = true, banner = true, dismissed = false, storageFails = false } = {}) {
  const element = button ? fakeButton() : undefined;  // 平时 hidden，与 index.html 一致
  const store = new Map();
  if (dismissed) store.set('ofd-update-dismissed', '1');
  const sessionStorage = {
    getItem: key => (storageFails ? null : (store.has(key) ? store.get(key) : null)),
    setItem: (key, value) => { if (!storageFails) store.set(key, value); },
  };
  const state = {
    refreshButton: element,
    updateBanner: banner ? { hidden: true } : undefined,
    UPDATE_DISMISS_KEY: 'ofd-update-dismissed',
    navigator: { serviceWorker: controlled ? { controller: {} } : { controller: null } },
    _store: store,
  };
  state.window = {
    location: { reload() { state._reloads = (state._reloads || 0) + 1; } },
    sessionStorage,
  };
  return state;
}

/**
 * 假 Service Worker worker：记录 statechange 监听器，可用 activate()/terminate()
 * 推进状态。stateChanges 里的 handler 就是产品代码注册的回调。
 */
function fakeWorker(initial = 'installing') {
  const worker = {
    state: initial,
    stateChanges: [],
    addEventListener(name, handler) {
      if (name === 'statechange') worker.stateChanges.push(handler);
    },
  };
  worker.advance = state => {
    worker.state = state;
    worker.stateChanges.forEach(handler => handler());
  };
  return worker;
}

/** 假 registration：可指定 waiting / installing，并记录监听器以便 emit。 */
function fakeRegistration({ waiting = null, installing = null, controller = {} } = {}) {
  const listeners = new Map();
  return {
    waiting,
    installing,
    controller,
    listeners,
    addEventListener(name, handler) { listeners.set(name, handler); },
    emit(name, ...args) { const h = listeners.get(name); if (h) h(...args); },
  };
}

function loaded(state) {
  return load(state, code, {
    expose: {
      watchServiceWorkerUpdate: 'watchServiceWorkerUpdate',
      markUpdateAvailable: 'markUpdateAvailable',
      dismissUpdateBanner: 'dismissUpdateBanner',
    },
  });
}

// ---------------------------------------------------------------- 高亮提示
suite.group('检测到新版本时按钮高亮');
{
  const state = loaded(baseState());
  const worker = fakeWorker();
  const registration = fakeRegistration({ installing: worker });
  state.watchServiceWorkerUpdate(registration);
  // statechange 监听是在 updatefound 里挂上的，所以要先触发它。
  registration.emit('updatefound');
  suite.check(state.refreshButton.hidden === true, '装到一半不提示，按钮仍隐藏');
  worker.advance('activated');
  suite.check(state.refreshButton.hidden === false, '新 SW 激活后按钮出现', state.refreshButton.hidden);
  suite.check(state.refreshButton.title.includes('新版本'), 'title 说明有新版本', state.refreshButton.title);
  suite.check(state.refreshButton.label.includes('新版本'), 'aria-label 同步更新', state.refreshButton.label);
}

suite.group('SW 字节相同被丢弃时不提示');
{
  const state = loaded(baseState());
  const worker = fakeWorker();
  const registration = fakeRegistration({ installing: worker });
  state.watchServiceWorkerUpdate(registration);
  registration.emit('updatefound');
  worker.advance('redundant');
  suite.check(state.refreshButton.hidden === true, 'redundant 不等于有更新，按钮仍隐藏');
}

suite.group('首次访问不误报');
{
  // controller 为 null 说明页面还没被 SW 控制过，资源本来就是网络最新的。
  const state = loaded(baseState({ controlled: false }));
  const registration = fakeRegistration({ controller: null });
  state.watchServiceWorkerUpdate(registration);
  registration.emit('updatefound');
  suite.check(state.refreshButton.hidden === true, '首访的 updatefound 不提示，按钮仍隐藏');
}

suite.group('等待中的 SW 直接接上');
{
  // 上一次访问已经把新 SW 装好但没等到接管，这次打开仍在 waiting。
  const state = loaded(baseState());
  const waiting = fakeWorker('installed');
  state.watchServiceWorkerUpdate(fakeRegistration({ waiting }));
  waiting.advance('activated');
  suite.check(state.refreshButton.hidden === false, '等待中的 SW 激活后按钮出现', state.refreshButton.hidden);
}

suite.group('同一页只提示一次');
{
  const state = loaded(baseState());
  const worker = fakeWorker();
  const registration = fakeRegistration({ installing: worker });
  state.watchServiceWorkerUpdate(registration);
  worker.advance('activated');
  let calls = 0;
  state.markUpdateAvailable = () => { calls++; };
  worker.stateChanges.forEach(handler => handler());
  suite.check(calls === 0, '重复触发不重复处理', calls);
}

suite.group('没有按钮时不报错');
{
  const state = loaded(baseState({ button: false }));
  const worker = fakeWorker();
  const registration = fakeRegistration({ installing: worker });
  let returned;
  try {
    returned = state.watchServiceWorkerUpdate(registration);
  } catch (error) {
    suite.check(false, '不应抛错', error.message);
  }
  suite.check(returned === registration, '返回原 registration 便于串联调用');
}

// ------------------------------------------------------------------ 顶部横幅
suite.group('检测到新版本时显示顶部横幅');
{
  const state = loaded(baseState());
  const worker = fakeWorker();
  const registration = fakeRegistration({ installing: worker });
  state.watchServiceWorkerUpdate(registration);
  registration.emit('updatefound');
  worker.advance('activated');
  suite.check(state.updateBanner.hidden === false, '横幅显示出来', state.updateBanner.hidden);
  suite.check(state.refreshButton.hidden === false, '按钮同时出现', state.refreshButton.hidden);
}

suite.group('关闭横幅后本次会话不再打扰');
{
  const state = loaded(baseState());
  state.markUpdateAvailable();
  suite.check(state.updateBanner.hidden === false, '先显示横幅');
  state.dismissUpdateBanner();
  suite.check(state.updateBanner.hidden === true, '关闭后隐藏');
  suite.check(state._store.get('ofd-update-dismissed') === '1', '记入 sessionStorage');
  // 同一会话里再次检测到更新：横幅不再打扰，但按钮仍高亮，用户可主动换版。
  state.updateBanner.hidden = false;
  state.markUpdateAvailable();
  suite.check(state.updateBanner.hidden === true, '再次提示时尊重已关闭的选择');
  suite.check(state.refreshButton.hidden === false, '按钮不受关闭横幅影响，仍可主动换版', state.refreshButton.hidden);
}

suite.group('新会话没有关闭记录时照常显示');
{
  // sessionStorage 是每个标签页会话独立的，刷新换版后不该再被旧记录挡住。
  const state = loaded(baseState());
  state.markUpdateAvailable();
  suite.check(state.updateBanner.hidden === false, '未关闭过时显示横幅');
}

suite.group('sessionStorage 不可用时仍能提示');
{
  // 隐私模式下 setItem 会抛错，不能因此把提示一起吞掉。
  const state = loaded(baseState({ storageFails: true }));
  state.markUpdateAvailable();
  suite.check(state.updateBanner.hidden === false, '存储不可用也显示横幅', state.updateBanner.hidden);
  let threw = false;
  try {
    state.dismissUpdateBanner();
  } catch (error) {
    threw = true;
  }
  suite.check(!threw, '存储不可用时关闭不抛错');
  suite.check(state.updateBanner.hidden === true, '本次仍能隐藏');
}

suite.group('只有横幅没有按钮时也能提示');
{
  const state = loaded(baseState({ button: false }));
  const worker = fakeWorker();
  const registration = fakeRegistration({ installing: worker });
  state.watchServiceWorkerUpdate(registration);
  registration.emit('updatefound');
  worker.advance('activated');
  suite.check(state.updateBanner.hidden === false, '横幅独立生效');
}

// ------------------------------------------------- 无更新时不占位（HTML 与 CSS）
suite.group('按钮默认隐藏且不占位');
{
  // 这条守的是 CSS 的一个坑：#refresh-button 自己设了 display: inline-flex，
  // 会盖掉 hidden 属性带来的默认 display: none。若漏了 [hidden] 规则，按钮
  // 会在无更新时照样显示出来，而 DOM 里的 hidden 属性看上去是对的。
  const html = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '..', 'index.html'), 'utf8');
  const tag = html.match(/<button id="refresh-button"[^>]*>/);
  if (suite.check(tag !== null, 'index.html 里有刷新按钮')) {
    suite.check(/\bhidden\b/.test(tag[0]), '按钮在 HTML 里就带 hidden', tag[0]);
  }
  suite.check(
    /#refresh-button\[hidden\]\s*\{\s*display:\s*none/.test(html),
    'CSS 显式声明 #refresh-button[hidden] 覆盖 display: inline-flex',
  );
  // 按钮 hidden 时不占位，移动端给文件名预留的宽度就不该为它多留 30px。
  suite.check(
    !/calc\(100vw - 180px\)/.test(html),
    '移动端文件名宽度没有为隐藏的按钮额外预留',
  );
}


// ------------------------------------------------- 横幅在两种布局下都铺满
suite.group('横幅跨满整行（flex 与 grid 两种 header 布局）');
{
  const html = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '..', 'index.html'), 'utf8');
  const rule = html.match(/#update-banner\s*\{([^}]*)\}/);
  if (suite.check(rule !== null, '找到 #update-banner 基础样式')) {
    const body = rule[1];
    suite.check(/flex:\s*1\s+0\s+100%/.test(body), '桌面端 header 是 flex，用 flex-basis 100% 独占一行', body.trim().slice(0, 60));
    suite.check(/order:\s*-1/.test(body), 'order: -1 让横幅排在第一行，上方无 gap 干扰');
    // 与下一行的 gap 也要抵消，否则工具栏上方会留一条 header 底色的缝。
    suite.check(/margin:\s*-10px\s+-18px\s+-10px/.test(body), '负 margin 同时抵消 header padding 与下方 gap', body.match(/margin:[^;]+/)?.[0]);
  }
  // 移动端 header 是 grid（grid-template-columns 有 7 列），flex 属性失效，
  // 且 header > * { margin: 0 } 会抹掉负边距，必须在媒体查询里重设。
  const mobile = html.match(/@media \(max-width: 620px\)([\s\S]*?)\n    \}/);
  if (suite.check(mobile !== null, '找到移动端媒体查询')) {
    const block = mobile[1];
    const bannerRule = block.match(/#update-banner\s*\{([^}]*)\}/);
    if (suite.check(bannerRule !== null, '移动端为横幅单独设了规则')) {
      suite.check(/grid-column:\s*1\s*\/\s*-1/.test(bannerRule[1]), '移动端用 grid-column 跨满 7 列', bannerRule[1].trim());
      suite.check(/margin:/.test(bannerRule[1]), '移动端重设负边距，因为 header > * { margin: 0 }');
    }
  }
}

process.exit(suite.report());