// 影片播放窗口摆放的行为测试：GB/T 33190—2016 第 12 章要求"图元对象关联的
// 视频播放使用图元的外观区域大小作为嵌入式播放窗口，其他情况使用弹出式窗口"。
//
// 覆盖图元动作走嵌入式、页面/文档级动作走弹出式、页面未挂载与页面尺寸无效时
// 的退路、同一资源被不同图元触发时复用同一个 <video> 并移动位置，以及页面卡片
// 被虚拟化回收时把嵌入式窗口移回弹出式。
//
// 被测代码全部从 viewer.js 原文切出（见 harness.mjs），不在测试里重抄逻辑。
import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('影片播放窗口');

const code = [
  sliceFunction('mediaPlacementTarget'),
  sliceFunction('placeMediaElement'),
  sliceFunction('rehomeEmbeddedMedia'),
].join('\n');

/**
 * 假 style 对象：产品代码用 `element.style.left = ...` 直接赋值，并用
 * `removeProperty('max-width')` 这种连字符形式删除，因此这里按
 * CSSStyleDeclaration 的语义把连字符名映射回驼峰再删除。
 */
function fakeStyle() {
  return {
    removeProperty(name) {
      const camel = String(name).replace(/-([a-z])/g, (_, ch) => ch.toUpperCase());
      delete this[camel];
      return '';
    },
  };
}

/**
 * 按 DOM 语义实现 append：把节点挂到本节点下，已在其他父节点下的节点会被
 * "移动"而不是复制（浏览器的 Node.append 就是这个行为）。
 */
function domAppend(host, nodes) {
  nodes.forEach(node => {
    if (node.parent && Array.isArray(node.parent.children)) {
      const at = node.parent.children.indexOf(node);
      if (at >= 0) node.parent.children.splice(at, 1);
    }
    if (Array.isArray(host.children)) {
      const own = host.children.indexOf(node);
      if (own >= 0) host.children.splice(own, 1);
      host.children.push(node);
    }
    node.parent = host;
  });
  return host;
}

/** 读取假元素上的样式值；未设置时返回空串，便于断言。 */
function css(element, name) {
  const value = element.style[name];
  return value === undefined || value === null ? '' : String(value);
}

/** 记录 append 目标的假元素。 */
function fakeElement(tag) {
  return {
    tagName: tag,
    style: fakeStyle(),
    parent: undefined,
    controls: false,
    children: [],
    append(...nodes) { return domAppend(this, nodes); },
    remove() { this.parent = undefined; },
    play: () => Promise.resolve(),
    pause() {},
  };
}

/** 假页面卡片：只有 .media-layer 是嵌入式窗口的挂载点。 */
function fakeCard() {
  const layer = { name: 'media-layer', children: [], append(...nodes) { return domAppend(this, nodes); } };
  return { mediaLayer: layer, querySelector: selector => (selector === '.media-layer' ? layer : null) };
}

/**
 * 一份覆盖摆放逻辑全部自由变量的最小 state。
 *
 * 桩写成闭包引用 state（而不是对象方法），避免 `with (state)` 下 this 绑错。
 */
function baseState(overrides = {}) {
  const body = { children: [], append(...nodes) { return domAppend(this, nodes); } };
  const state = {
    pageInfos: [{ width: 210, height: 297 }],
    pageCards: [fakeCard()],
    activeMedia: new Map(),
    document: { body },
    _body: body,
  };
  return Object.assign(state, overrides);
}

/** 图元级 CLICK 动作：带外观区域，page 指向已挂载页面。 */
function objectAction(boundary, page = 0) {
  return { scope: 0, media_id: 40, media_kind: 'movie', operator: 'Play', event: 'CLICK', page, boundary };
}

/** 页面级/文档级动作：没有外观区域。 */
function pageAction() {
  return { scope: 0, media_id: 40, media_kind: 'movie', operator: 'Play', event: 'PO', page: 0, boundary: null };
}

function loaded(state) {
  return load(state, code, { expose: { placeMediaElement: 'placeMediaElement', rehomeEmbeddedMedia: 'rehomeEmbeddedMedia' } });
}

// ------------------------------------------------------------ 图元动作走嵌入式
suite.group('图元动作使用外观区域作为嵌入式播放窗口');
{
  const state = loaded(baseState());
  const entry = { element: fakeElement('video'), placement: 'none', placementPage: -1 };
  state.placeMediaElement(entry, objectAction({ x: 105, y: 148.5, width: 105, height: 148.5 }));
    suite.check(css(entry.element, 'position') === 'absolute', '图元动作使用绝对定位', css(entry.element, 'position'));
  suite.check(css(entry.element, 'left') === '50%' && css(entry.element, 'top') === '50%', '左上角按页面尺寸换算', `${css(entry.element, 'left')} / ${css(entry.element, 'top')}`);
  suite.check(css(entry.element, 'width') === '50%' && css(entry.element, 'height') === '50%', '窗口尺寸等于外观区域', `${css(entry.element, 'width')} / ${css(entry.element, 'height')}`);
  suite.check(entry.element.parent === state.pageCards[0].mediaLayer, '挂在该页的 media-layer 上');
  suite.check(entry.placement === 'embedded' && entry.placementPage === 0, '记录为嵌入式');
  suite.check(state._body.children.length === 0, '不再挂到 body');
}

// ---------------------------------------------------- 其他情况使用弹出式窗口
suite.group('页面级与文档级动作使用弹出式窗口');
for (const [label, action] of [
  ['页面级 PO 动作无外观区域', pageAction()],
  ['文档级 DO 动作无外观区域', { ...pageAction(), event: 'DO', page: 0 }],
  ['外观区域宽度为零', objectAction({ x: 10, y: 10, width: 0, height: 20 })],
  ['外观区域坐标非有限值', objectAction({ x: Number.NaN, y: 10, width: 20, height: 20 })],
  ['页码越界', objectAction({ x: 10, y: 10, width: 20, height: 20 }, 5)],
  ['页面尚未挂载', objectAction({ x: 10, y: 10, width: 20, height: 20 }, 1)],
]) {
  const state = loaded(baseState());
  const entry = { element: fakeElement('video'), placement: 'none', placementPage: -1 };
  state.placeMediaElement(entry, action);
    suite.check(css(entry.element, 'position') === 'fixed', `${label} → 弹出式定位`, css(entry.element, 'position'));
  suite.check(css(entry.element, 'left') === '50%' && css(entry.element, 'top') === '50%', `${label} → 居中`, `${css(entry.element, 'left')} / ${css(entry.element, 'top')}`);
  suite.check(css(entry.element, 'transform') === 'translate(-50%, -50%)', `${label} → 居中位移`, css(entry.element, 'transform'));
  suite.check(entry.element.parent === state._body, `${label} → 挂到 body`);
  suite.check(entry.placement === 'popup' && entry.placementPage === -1, `${label} → 记录为弹出式`);
}

suite.group('页面尺寸无效时退回弹出式');
{
  const state = loaded(baseState({ pageInfos: [{ width: 0, height: 297 }] }));
  const entry = { element: fakeElement('video'), placement: 'none', placementPage: -1 };
  state.placeMediaElement(entry, objectAction({ x: 10, y: 10, width: 20, height: 20 }));
  suite.check(css(entry.element, 'position') === 'fixed', '页宽为零时用弹出式', css(entry.element, 'position'));
  suite.check(entry.element.parent === state._body, '页宽为零时挂到 body');
}

// ------------------------------------------- 同一资源换图元触发时只移动位置
suite.group('同一资源被不同图元触发时复用同一元素');
{
  const state = loaded(baseState());
  const entry = { element: fakeElement('video'), placement: 'none', placementPage: -1 };
  state.activeMedia.set('0:40', entry);
  // 模拟 ensureMediaElement 的复用分支：重新摆放但不新建元素。
  state.placeMediaElement(entry, objectAction({ x: 105, y: 148.5, width: 105, height: 148.5 }));
  suite.check(css(entry.element, 'left') === '50%', '先落在第一个图元位置', css(entry.element, 'left'));
  state.placeMediaElement(entry, objectAction({ x: 0, y: 0, width: 210, height: 297 }));
    suite.check(css(entry.element, 'left') === '0%' && css(entry.element, 'top') === '0%', '再移动到第二个图元位置', `${css(entry.element, 'left')} / ${css(entry.element, 'top')}`);
  suite.check(css(entry.element, 'width') === '100%' && css(entry.element, 'height') === '100%', '尺寸跟随新图元', `${css(entry.element, 'width')} / ${css(entry.element, 'height')}`);
  suite.check(state.pageCards[0].mediaLayer.children.length === 1, '仍只有一个 video 元素', state.pageCards[0].mediaLayer.children.length);
  suite.check(entry.element.parent === state.pageCards[0].mediaLayer, '位置改到新图元区域');
}

// 嵌入式 → 弹出式切换时必须清掉上一轮的定位残留
suite.group('从嵌入式切回弹出式时清除残留定位');
{
  const state = loaded(baseState());
  const entry = { element: fakeElement('video'), placement: 'none', placementPage: -1 };
  state.placeMediaElement(entry, objectAction({ x: 105, y: 148.5, width: 105, height: 148.5 }));
  state.placeMediaElement(entry, pageAction());
    suite.check(css(entry.element, 'position') === 'fixed', '切回弹出式定位', css(entry.element, 'position'));
  suite.check(css(entry.element, 'width') === '' && css(entry.element, 'height') === '', '清掉嵌入式宽高', `${css(entry.element, 'width')} / ${css(entry.element, 'height')}`);
  suite.check(css(entry.element, 'transform') === 'translate(-50%, -50%)', '应用居中位移', css(entry.element, 'transform'));
  suite.check(entry.element.parent === state._body, '挂到 body');
}

// ------------------------------------------------------ 卡片回收时移回弹出式
suite.group('页面卡片被回收时把嵌入式窗口移回弹出式');
{
  const state = loaded(baseState());
  const embedded = { element: fakeElement('video'), placement: 'none', placementPage: -1 };
  state.activeMedia.set('0:40', embedded);
  state.placeMediaElement(embedded, objectAction({ x: 105, y: 148.5, width: 105, height: 148.5 }));
  suite.check(embedded.placement === 'embedded', '先落在页面内');
  state.rehomeEmbeddedMedia(0);
  suite.check(embedded.placement === 'popup', '回收后改为弹出式', embedded.placement);
  suite.check(embedded.element.parent === state._body, '回收后挂到 body 保持可见', embedded.element.parent?.tagName);
  suite.check(css(embedded.element, 'position') === 'fixed', '回收后为弹出式定位');
}

suite.group('回收其它页面不影响当前播放窗口');
{
  const state = loaded(baseState());
  const entry = { element: fakeElement('video'), placement: 'none', placementPage: -1 };
  state.activeMedia.set('0:40', entry);
  state.placeMediaElement(entry, objectAction({ x: 105, y: 148.5, width: 105, height: 148.5 }));
  state.rehomeEmbeddedMedia(7);
  suite.check(entry.placement === 'embedded', '别的页码回收不影响', entry.placement);
  suite.check(entry.element.parent === state.pageCards[0].mediaLayer, '仍在原页面层内');
}

// 声音没有外观区域概念，不参与摆放
suite.group('声音动作不参与窗口摆放');
{
  const state = loaded(baseState());
  const entry = { element: fakeElement('audio'), placement: 'none', placementPage: -1 };
  state.placeMediaElement(entry, { scope: 0, media_id: 41, media_kind: 'sound', operator: 'Play', page: 0, boundary: null });
  suite.check(css(entry.element, 'position') === '', '不设置定位', css(entry.element, 'position'));
  suite.check(entry.element.parent === undefined, '不挂到页面层', String(entry.element.parent));
  suite.check(entry.placement === 'none', '不记录摆放方式', entry.placement);
}

process.exit(suite.report());
