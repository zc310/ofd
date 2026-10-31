// 页面上可点击签章热区的行为测试：签名清单里的 stamps 按页面归集成热区，热区
// 按签章边界做百分比定位并点击弹出签章属性；弹层在滚动/视口变化时收起，但弹层
// 内部列表滚动不触发收起。
//
// 被测代码全部从 viewer.js 原文切出（见 harness.mjs），不在测试里重抄逻辑。
import { sliceFunction, viewerSource, load, createSuite } from './harness.mjs';

const suite = createSuite('签章热区');

const code = [
  sliceFunction('buildSignatureStamp'),
  sliceFunction('indexSealStamps'),
  sliceFunction('applySealHotspots'),
  sliceFunction('buildSealHotspot'),
  sliceFunction('positionSealPopover'),
  sliceFunction('handleSealPopoverScroll'),
].join('\n');

/** 假元素：只实现被测代码用到的 DOM 子集。 */
function fakeElement(tag = 'div') {
  return {
    tagName: tag,
    className: '',
    type: '',
    title: '',
    style: {},
    attrs: {},
    children: [],
    hidden: false,
    listeners: {},
    setAttribute(name, value) { this.attrs[name] = value; },
    addEventListener(name, fn) { this.listeners[name] = fn; },
    append(...nodes) { this.children.push(...nodes); },
    replaceChildren(...nodes) { this.children = nodes.slice(); },
  };
}

/** 假页面卡片：只有 .seal-layer 是热区挂载点。 */
function fakeCard() {
  const layer = fakeElement('div');
  return { layer, querySelector: selector => (selector === '.seal-layer' ? layer : null) };
}

/**
 * 一份覆盖热区逻辑全部自由变量的最小 state。桩写成 state 上的属性，避免
 * `with (state)` 里出现未定义的自由变量。
 */
function baseState(overrides = {}) {
  const state = {
    sealStampsByPage: new Map(),
    pageInfos: [{ width: 210, height: 297 }],
    pageCards: [],
    opened: [],
    closed: false,
    hideLinkPreview: () => {},
    document: { createElement: tag => fakeElement(tag) },
    // handleSealPopoverScroll 用 `instanceof Node` 守卫，测试环境补一个构造器。
    Node: class FakeNode {},
  };
  state.openSealPopover = (info, anchor) => { state.opened.push({ info, anchor }); };
  state.closeSealPopover = () => { state.closed = true; };
  return Object.assign(state, overrides);
}

function loaded(state) {
  return load(state, code, {
    expose: {
      indexSealStamps: 'indexSealStamps',
      applySealHotspots: 'applySealHotspots',
      buildSealHotspot: 'buildSealHotspot',
      buildSignatureStamp: 'buildSignatureStamp',
      positionSealPopover: 'positionSealPopover',
      handleSealPopoverScroll: 'handleSealPopoverScroll',
    },
  });
}

const boundary = (x, y, width, height) => ({ x, y, width, height });

// ------------------------------------------------------ 按页面归集签章
suite.group('indexSealStamps 按页面归集有效签章');
{
  const state = loaded(baseState());
  const info = {
    scope: 0,
    id: 'S1',
    provider: '示例提供方',
    company: '',
    stamps: [
      { page: 1, boundary: boundary(10, 20, 30, 40) },
      { page: -1, boundary: boundary(1, 1, 1, 1) },
      { page: 2, boundary: null },
      { page: 1, boundary: boundary(0, 0, 0, 10) },
      { page: 1, boundary: boundary(Number.NaN, 0, 10, 10) },
    ],
  };
  state.indexSealStamps([info]);
  const page1 = state.sealStampsByPage.get(1) || [];
  suite.check(page1.length === 1, '只保留页面有效且边界有效的签章', page1.length);
  suite.check(!state.sealStampsByPage.has(2), '页面号无效或边界缺失的签章不归集');
  suite.check(page1[0]?.info === info && page1[0]?.stampIndex === 0, '记录签名与签章索引');
  const info2 = { scope: 1, id: 'S2', stamps: [{ page: 1, boundary: boundary(0, 0, 5, 5) }] };
  state.indexSealStamps([info, info2]);
  suite.check((state.sealStampsByPage.get(1) || []).length === 2, '同一页多个签章都保留');
  suite.check(state.sealStampsByPage.size === 1, '重建索引不残留旧记录', state.sealStampsByPage.size);
}

// ------------------------------------------------------ 热区定位与点击
suite.group('buildSealHotspot 按边界百分比定位并弹出属性');
{
  const state = loaded(baseState());
  const info = { scope: 0, id: 'S1', provider: '示例提供方' };
  const entry = { info, stamp: { page: 0, boundary: boundary(105, 148.5, 105, 148.5) }, stampIndex: 0 };
  const button = state.buildSealHotspot(entry, { width: 210, height: 297 });
  suite.check(button.className === 'seal-hotspot', '热区类名为 seal-hotspot', button.className);
  suite.check(button.style.left === '50%' && button.style.top === '50%', '左上角按页面尺寸换算', `${button.style.left} / ${button.style.top}`);
  suite.check(button.style.width === '50%' && button.style.height === '50%', '尺寸按页面尺寸换算', `${button.style.width} / ${button.style.height}`);
  suite.check(String(button.title).includes('示例提供方'), '提示包含签名提供方', button.title);
  suite.check(button.attrs['aria-label'] === button.title, '无障碍标签与提示一致');
  button.listeners.click();
  suite.check(state.opened.length === 1 && state.opened[0].info === info, '点击打开该签名的属性');
}

suite.group('buildSealHotspot 缺少提供者时回退到签名 ID');
{
  const state = loaded(baseState());
  const entry = { info: { scope: 0, id: 'S9' }, stamp: { page: 0, boundary: boundary(0, 0, 10, 10) }, stampIndex: 0 };
  const button = state.buildSealHotspot(entry, { width: 100, height: 100 });
  suite.check(String(button.title).includes('S9'), '回退到签名 ID', button.title);
}

suite.group('骑缝章只高亮当前页可见切片');
{
  const state = loaded(baseState());
  const info = { scope: 0, id: 'seam' };
  const visible = boundary(202, 128.5, 8, 40);
  const stamp = { page: 0, boundary: boundary(186, 128.5, 40, 40), visible_boundary: visible };
  state.indexSealStamps([{ ...info, stamps: [stamp] }]);
  const button = state.buildSealHotspot(state.sealStampsByPage.get(0)[0], { width: 210, height: 297 });
  suite.check(Math.abs(parseFloat(button.style.left) - 202 / 210 * 100) < 1e-9,
    '热区横坐标包含 Clip 偏移', button.style.left);
  suite.check(Math.abs(parseFloat(button.style.width) - 8 / 210 * 100) < 1e-9,
    '热区宽度只覆盖 8mm 切片', button.style.width);
  suite.check(Math.abs(parseFloat(button.style.height) - 40 / 297 * 100) < 1e-9,
    '纵向尺寸沿用当前切片高度', button.style.height);
  button.listeners.click();
  suite.check(state.opened.length === 1, '切片仍可点击打开签章属性');
}

suite.group('侧栏跳转高亮同一骑缝章切片');
{
  const state = loaded(baseState({
    sealMime: () => '',
    signatureSealIcon: () => fakeElement('span'),
    jumps: [],
    collapsed: false,
  }));
  state.goToAnnotation = target => state.jumps.push(target);
  state.collapseSidebarAfterJump = () => { state.collapsed = true; };
  const visible = boundary(202, 128.5, 8, 40);
  const stamp = { page: 0, boundary: boundary(186, 128.5, 40, 40), visible_boundary: visible };
  const button = state.buildSignatureStamp({ scope: 0, id: 'seam' }, stamp, 0);
  button.listeners.click();
  suite.check(state.jumps[0]?.boundary === visible, '跳转使用裁剪后边界');
  suite.check(state.collapsed, '跳转后仍按原逻辑收起侧栏');
  delete stamp.visible_boundary;
  state.buildSignatureStamp({ scope: 0, id: 'whole' }, stamp, 0).listeners.click();
  suite.check(state.jumps[1]?.boundary === stamp.boundary, '无裁剪信息时使用原始边界');
}

// ------------------------------------------------------ 按页重建热区
suite.group('applySealHotspots 在对应页面重建热区');
{
  const card = fakeCard();
  const state = loaded(baseState({ pageCards: [card] }));
  const info = { scope: 0, id: 'S1', provider: '提供方' };
  state.sealStampsByPage.set(0, [{ info, stamp: { page: 0, boundary: boundary(10, 10, 20, 20) }, stampIndex: 0 }]);
  state.applySealHotspots(0);
  suite.check(card.layer.children.length === 1, '指定页生成一个热区', card.layer.children.length);
  state.applySealHotspots(0);
  suite.check(card.layer.children.length === 1, '重建前先清空旧热区', card.layer.children.length);
  state.sealStampsByPage.delete(0);
  state.applySealHotspots(0);
  suite.check(card.layer.children.length === 0, '该页没有签章时清空热区', card.layer.children.length);
}

suite.group('applySealHotspots 不传页时覆盖全部有签章的页面');
{
  const card0 = fakeCard();
  const card1 = fakeCard();
  const state = loaded(baseState({
    pageCards: [card0, card1],
    pageInfos: [{ width: 210, height: 297 }, { width: 210, height: 297 }],
  }));
  const info = { scope: 0, id: 'S1' };
  state.sealStampsByPage.set(0, [{ info, stamp: { page: 0, boundary: boundary(0, 0, 10, 10) }, stampIndex: 0 }]);
  state.sealStampsByPage.set(1, [{ info, stamp: { page: 1, boundary: boundary(0, 0, 10, 10) }, stampIndex: 0 }]);
  state.applySealHotspots();
  suite.check(card0.layer.children.length === 1 && card1.layer.children.length === 1, '两页都生成热区');
}

suite.group('页面尺寸无效时不生成热区');
{
  const card = fakeCard();
  const state = loaded(baseState({ pageCards: [card], pageInfos: [{ width: 0, height: 297 }] }));
  const info = { scope: 0, id: 'S1' };
  state.sealStampsByPage.set(0, [{ info, stamp: { page: 0, boundary: boundary(0, 0, 10, 10) }, stampIndex: 0 }]);
  state.applySealHotspots(0);
  suite.check(card.layer.children.length === 0, '页宽为零时不生成热区', card.layer.children.length);
}

// ------------------------------------------------------ 弹层定位
suite.group('positionSealPopover 把弹层夹回视口内');
{
  const popover = { style: {}, getBoundingClientRect: () => ({ width: 340, height: 200 }) };
  const state = loaded(baseState({
    sealPopoverElement: popover,
    window: { innerWidth: 800, innerHeight: 600 },
  }));
  state.positionSealPopover({ left: 700, right: 750, top: 500, bottom: 550 });
  suite.check(popover.style.left === '350px', '右侧放不下时翻到热区左侧', popover.style.left);
  suite.check(popover.style.top === '390px', '底部放不下时上夹到视口内', popover.style.top);

  state.positionSealPopover({ left: 10, right: 40, top: 20, bottom: 50 });
  suite.check(popover.style.left === '50px', '空间充足时贴在热区右侧', popover.style.left);
  suite.check(popover.style.top === '20px', '顶部对齐热区', popover.style.top);
}

// ------------------------------------------------------ 滚动收起
suite.group('handleSealPopoverScroll 滚动时收起弹层');
{
  const state = loaded(baseState());
  const inside = new state.Node();
  state.sealPopoverElement = { hidden: false, contains: node => node === inside };
  state.handleSealPopoverScroll({ target: new state.Node() });
  suite.check(state.closed === true, '外部滚动收起弹层', state.closed);
  state.closed = false;
  state.handleSealPopoverScroll({ target: inside });
  suite.check(state.closed === false, '弹层内部滚动不收起', state.closed);
}

// 结构守卫：弹层复用侧栏详情组件时跳过签章位置按钮（行为测试覆盖不到调用点）。
suite.group('结构守卫');
{
  suite.check(viewerSource().includes('!options.skipStamps'),
    'buildSignatureItem 支持 skipStamps（弹层跳过签章位置按钮）');
}

process.exit(suite.report());
