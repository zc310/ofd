// 自定义标签树点击节点时高亮整棵子树的对象引用。
import { load, sliceFunction, createSuite } from './harness.mjs';

const suite = createSuite('自定义标签树');
const state = {};
load(state, [
  sliceFunction('customTagTargets'),
  sliceFunction('customTagDirectTarget'),
  sliceFunction('customTagLabelText'),
  sliceFunction('customTagCanNavigate'),
].join('\n'), {
  expose: {
    targets: 'customTagTargets',
    labelText: 'customTagLabelText',
    canNavigate: 'customTagCanNavigate',
  },
});

const first = { page: 0, object_id: 7, object_text: 'A' };
const second = { page: 1, object_id: 8, object_text: 'B' };
const third = { page: 1, object_id: 9, object_text: 'C' };
const fourth = { page: 2, object_id: 10, object_text: 'D' };
const fifth = { page: 2, object_id: 11, object_text: 'E' };
const goodsInfo = {
  name: 'GoodsInfo',
  children: [
    { name: 'ItemIndex', children: [{ name: 'ObjectRef', target: first }] },
    { name: 'Item', children: [{ name: 'ObjectRef', target: second }] },
    { name: 'Amount', children: [{ name: 'ObjectRef', target: third }] },
    { name: 'TaxScheme', children: [{ name: 'ObjectRef', target: fourth }] },
    { name: 'TaxAmount', children: [{ name: 'ObjectRef', target: fifth }] },
    { name: 'DuplicateRef', children: [{ name: 'ObjectRef', target: first }] },
  ],
};
const targets = state.targets(goodsInfo);
suite.check(targets.length === 5 && targets.every((target, index) => target === [first, second, third, fourth, fifth][index]),
  'GoodsInfo 收集所有深层 ObjectRef 并去重', targets.map(target => target.object_id));
suite.check(state.labelText(goodsInfo) === 'GoodsInfo',
  '父节点标签不拼接第一个后代的文本', state.labelText(goodsInfo));
suite.check(state.labelText(goodsInfo.children[0]) === 'ItemIndex: A',
  '直接关联对象的字段仍显示对象文字', state.labelText(goodsInfo.children[0]));
suite.check(state.canNavigate(goodsInfo) === true,
  '有可定位后代的树节点可点击');
suite.check(state.targets({ name: 'ItemIndex', children: [{ name: 'ObjectRef', target: first }] })[0] === first,
  '子字段也能独立高亮自己的对象');
suite.check(state.canNavigate({ name: 'Empty', children: [{ name: 'Item' }] }) === false,
  '整棵子树都没有对象引用时不可点击');

const jumpState = {
  pageInfos: [{}, {}, {}],
  pageCards: [],
  customTagHighlightTargets: [],
  jumpedTo: [],
  cleared: 0,
  goTo(page) { jumpState.jumpedTo.push(page); },
  clearCustomTagHighlights() { jumpState.cleared++; },
  requestAnimationFrame(callback) { callback(); },
  window: { setTimeout(callback, delay) { jumpState.timeout = delay; return 1; } },
};
load(jumpState, sliceFunction('scrollToCustomTagTargets'), {
  expose: { jump: 'scrollToCustomTagTargets' },
});
jumpState.jump(targets);
suite.check(jumpState.jumpedTo[0] === 0, 'GoodsInfo 跳到首个对象所在页');
suite.check(jumpState.customTagHighlightTargets.length === 5,
  '页面高亮保留 GoodsInfo 子树的全部对象引用', jumpState.customTagHighlightTargets.length);
suite.check(jumpState.timeout === 2200, '对象高亮按时限自动清除', jumpState.timeout);

if (suite.report()) process.exitCode = 1;
