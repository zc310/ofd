import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('关于对话框发行版本');
const aboutCode = [sliceFunction('openAboutDialog'), sliceFunction('updateAboutVersion')].join('\n');

function stateFor(engine) {
  const state = {
    engine,
    versionLink: { textContent: '加载中…' },
    aboutDialog: { showModal() { state.shown = true; } },
    aboutClose: { focus() { state.focused = true; } },
  };
  return load(state, aboutCode, { expose: { open: 'openAboutDialog' } });
}

suite.group('版本来源与显示');
const immediate = stateFor({ version: '1.2.3', ready: Promise.resolve() });
immediate.open();
suite.check(immediate.versionLink.textContent === 'v1.2.3', '已就绪时显示运行时版本并添加 v 前缀');
suite.check(immediate.shown && immediate.focused, '显示对话框并聚焦关闭按钮');

let markReady;
const ready = new Promise(resolve => { markReady = resolve; });
const delayedEngine = { version: '', ready };
const delayed = stateFor(delayedEngine);
delayed.open();
suite.check(delayed.versionLink.textContent === '加载中…', 'WASM 尚未就绪时显示加载提示');
delayedEngine.version = '4.5.6';
markReady();
await ready;
await Promise.resolve();
suite.check(delayed.versionLink.textContent === 'v4.5.6', 'WASM 就绪后更新为注入版本');

process.exitCode = suite.report() ? 1 : 0;
