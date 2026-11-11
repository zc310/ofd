import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('桌面连续滚动调度');
let next = 0, updates = 0;
const timers = new Map(), cleared = [];
const state = {
  engine: { nativeDesktop: true }, pageVirtualUpdateTimer: undefined,
  setTimeout: (callback, delay) => { const id = ++next; timers.set(id, { callback, delay }); return id; },
  clearTimeout: id => { cleared.push(id); timers.delete(id); },
  updatePageVirtualWindow: () => { updates++; },
};
load(state, sliceFunction('schedulePageVirtualUpdate'), { expose: { schedule: 'schedulePageVirtualUpdate' } });
state.schedule();
const first = state.pageVirtualUpdateTimer;
for (let i = 0; i < 20; i++) state.schedule();
suite.check(state.pageVirtualUpdateTimer === first && timers.get(first).delay === 32 && !cleared.includes(first),
  '连续滚动不推迟已经安排的桌面更新');
timers.get(first).callback();
state.schedule();
suite.check(updates === 1 && state.pageVirtualUpdateTimer !== first, '执行后下一次滚动可安排新更新');
state.engine.nativeDesktop = false;
state.schedule();
const browserTimer = state.pageVirtualUpdateTimer;
state.schedule();
suite.check(cleared.includes(browserTimer) && timers.get(state.pageVirtualUpdateTimer).delay === 80,
  '浏览器仍使用原有防抖');
process.exit(suite.report() ? 1 : 0);
