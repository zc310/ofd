import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

let poll, unload, resolve;
let queries = 0, opened = 0, cleared = 0;
const state = {
  nativeSelecting: true, opening: false, versionSwitching: false, documentActionBusy: false,
  engine: { nextLaunch() { queries++; return new Promise(done => { resolve = done; }); } },
  setInterval(fn) { poll = fn; return 42; },
  clearInterval(id) { assert.equal(id, 42); cleared++; },
  window: { addEventListener(event, fn) { assert.equal(event, 'beforeunload'); unload = fn; } },
  async openNativeDocument(selection) { assert.equal(selection.launch, true); opened++; },
  setStatus() { assert.fail('不应报告错误'); },
};
load(state, sliceFunction('startNativeLaunchPolling'), { invoke: ['startNativeLaunchPolling'] });
assert.equal(queries, 0, '前端忙碌时不能消费启动文件');
state.nativeSelecting = false;
const first = poll();
await poll();
assert.equal(queries, 1, '多个轮询不能并发打开文档');
resolve(true); await first;
assert.equal(opened, 1);
const second = poll();
unload(); resolve(true); await second;
assert.equal(opened, 1, '卸载后迟到请求不能重新打开文档');
assert.equal(cleared, 1);
await poll();
assert.equal(queries, 2, '卸载后不能继续查询');
console.log('启动文件就绪等待、串行打开与卸载回收测试通过');
