import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

function element() {
  return { children: [], append(...items) { this.children.push(...items); }, replaceChildren(...items) { this.children = items; },
    classList: { toggle() {} }, addEventListener(type, handler) { this[type] = handler; } };
}
const calls = [], statuses = [], jumps = [];
let finish;
const state = {
  engine: { nativeDesktop: true,
    checkPermission(action, indices) { calls.push([action, indices]); return new Promise(resolve => { finish = resolve; }); },
    customTags: async () => [] },
  document: { createElement: element }, documentGeneration: 1,
  customTagTrees: [], customTagsError: '', customTagsElement: element(), sidebarFilterValue: '',
  customTagsRequest: undefined,
  scrollToCustomTagTargets: value => jumps.push(value), collapseSidebarAfterJump() {}, restorePanelScroll() {},
  outlineEmptyMessage: text => text, setStatus: text => statuses.push(text),
};
load(state, ['buildCustomTagList', 'customTagMatches', 'customTagTargets', 'customTagDirectTarget', 'customTagLabelText', 'customTagCanNavigate', 'loadCustomTags', 'renderCustomTags'].map(sliceFunction).join('\n'),
  { expose: { build: 'buildCustomTagList', read: 'loadCustomTags' } });
const target = { page: 0, object_id: 5, object_text: '<script>仅文本</script>' };
const list = state.build([{ name: '标签', target }], '');
const button = list.children[0].children[0].children[1];
assert.match(button.textContent, /<script>仅文本<\/script>/);
const first = button.click();
assert.deepEqual(calls[0], ['access', [0]]);
assert.equal(jumps.length, 0, '定位前等待权限复核'); finish(); await first;
assert.equal(jumps[0][0], target);
const stale = button.click(); state.documentGeneration++; finish(); await stale;
assert.equal(jumps.length, 1, '权限读取中切换文档不跳转旧标签');
state.engine.checkPermission = async () => { throw new Error('权限过期'); };
await button.click(); assert.match(statuses.at(-1), /权限过期/);
state.engine.customTags = async () => { throw new Error('标签 XML 损坏'); };
await state.read(); assert.match(state.customTagsElement.children[0], /获取自定义标签失败.*XML 损坏/);
let rejectOld;
state.engine.customTags = () => new Promise((_, reject) => { rejectOld = reject; });
const oldRead = state.read(); state.documentGeneration++; state.customTagTrees = ['新文档'];
rejectOld(new Error('旧文档失败')); await oldRead;
assert.deepEqual(state.customTagTrees, ['新文档'], '迟到失败不能清空新文档标签');
console.log('原生自定义标签纯文本显示、定位权限、错误反馈与旧文档隔离测试通过');
