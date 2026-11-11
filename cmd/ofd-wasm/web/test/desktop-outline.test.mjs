import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('桌面大纲跳转');
const opened = [], statuses = [], destinations = [];
function element() {
  return { children: [], style: {}, dataset: {}, classList: { add() {} },
    append(...nodes) { this.children.push(...nodes); },
    addEventListener(type, callback) { this[type] = callback; } };
}
const state = {
  document: { createElement: element }, engine: { nativeDesktop: true },
  window: { open: uri => opened.push(uri) }, setStatus: text => statuses.push(text),
  appendOutlinePageTag() {}, goToDestination: (...args) => destinations.push(args), collapseSidebarAfterJump() {},
};
load(state, sliceFunction('buildOutlineList'), { expose: { build: 'buildOutlineList' } });
const nodes = [{ title: '外部', page: -1, uri: 'javascript:alert(1)' }, { title: '章节', page: 1, dest: { type: 'Fit' } }];
const list = state.build(nodes, 0);
list.children[0].children[0].children[1].click();
list.children[1].children[0].children[1].click();
suite.check(opened.length === 0 && statuses[0].includes('暂不支持'), '桌面外部链接不打开不可信内容');
suite.check(destinations.length === 1 && destinations[0][0] === 1 && destinations[0][1].type === 'Fit', '内部目标继续使用共用跳转');
state.engine.nativeDesktop = false;
state.build(nodes.slice(0, 1), 0).children[0].children[0].children[1].click();
suite.check(opened.length === 1, '浏览器原有外部链接行为不变');
process.exit(suite.report() ? 1 : 0);
