// 适应缩放需按每页的实际物理尺寸布局，不能把不同规格页面统一拉伸到同一宽度。
import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('异尺寸页面适应缩放');
const code = [
  'pageBaseDimensions', 'spreadDimensions', 'layoutBaseWidth', 'fitViewport', 'fitScale', 'fittedZoom', 'fitWidthZoom', 'fitHeightZoom', 'fitPageZoom', 'refitZoom', 'fitRectZoom', 'mapDocumentZoomMode',
].map(sliceFunction).join('\n');
const setCurrentSource = sliceFunction('setCurrent');

function setup(pageLayout, pages, spreadPages, current = 0, rotation = 0) {
  const scale = 820 / 210;
  const spreadWidths = spreadPages.map(group => group.reduce((sum, index) => {
    const info = pages[index >= 0 ? index : group.find(page => page >= 0)];
    const pageWidth = (rotation % 180 !== 0 ? info.height : info.width) * scale;
    return sum + pageWidth;
  }, Math.max(0, group.length - 1) * 18));
  const state = {
    pageLayout,
    pageInfos: pages,
    pageSpreads: spreadPages.map(pages => ({ pages })),
    widestSpreadBaseWidth: Math.max(...spreadWidths),
    current,
    pageRotation: rotation,
    zoom: 1,
    zoomMode: 'manual',
    minFitZoom: 0.05,
    basePagePixels: 820,
    referencePageWidthMM: 210,
    pagesElement: { clientWidth: 900 },
    readerElement: {},
    status: { offsetHeight: 0 },
    document: { querySelector: () => ({}) },
    window: { innerHeight: 600 },
    getComputedStyle: () => ({ paddingTop: '0px', paddingBottom: '0px', paddingLeft: '0px', paddingRight: '0px' }),
    headerHeight: () => 0,
    pageLayoutIsDouble: () => pageLayout !== 'single',
    currentSpreadPosition: () => spreadPages.findIndex(group => group.includes(state.current)),
    setZoom: (value, mode, persist) => { if (value > 0) { state.zoom = value; state.zoomMode = mode; state.persist = persist; } },
  };
  return load(state, code, { expose: {
    dimensions: 'spreadDimensions', baseWidth: 'layoutBaseWidth', fitWidth: 'fitWidthZoom', fitHeight: 'fitHeightZoom', fitPage: 'fitPageZoom', refit: 'refitZoom', viewport: 'fitViewport', destFit: 'fitRectZoom', mapMode: 'mapDocumentZoomMode',
  } });
}

const a4 = { width: 210, height: 297 };
const a3Landscape = { width: 420, height: 297 };

suite.group('页面宽高按物理尺寸缩放');
const single = setup('single', [a4, a3Landscape], [[0], [1]]);
const a4Dimensions = single.dimensions(0);
const a3Dimensions = single.dimensions(1);
suite.check(Math.abs(a4Dimensions.width - 820) < 0.01, 'A4 基准宽度保持 820px', a4Dimensions);
suite.check(Math.abs(a3Dimensions.width - 1640) < 0.01, '双倍宽页面按比例显示', a3Dimensions);

suite.group('适应宽度固定以最宽页面组为基准');
suite.check(Math.abs(single.baseWidth() - 1640) < 0.01, '混合竖页/横页按最宽页组算适应宽度', single.baseWidth());
single.fitWidth();
suite.check(Math.abs(single.zoom - 900 / 1640) < 0.001, '适应宽度保证最宽页面完整可见', single.zoom);
single.current = 1;
single.fitWidth();
suite.check(Math.abs(single.zoom - 900 / 1640) < 0.001, '滚动到其他尺寸页维持同一适应比例', single.zoom);
single.fitPage();
suite.check(Math.abs(single.zoom - 600 / a3Dimensions.height) < 1e-10, '适应页面精确使用实际视口高度', single.zoom);

suite.group('滚动切页不自动重设适应缩放');
suite.check(!/zoomMode === 'fit'\) fitWidthZoom\(\)|zoomMode === 'page'\) fitPageZoom\(\)/.test(setCurrentSource),
  'setCurrent 不因当前页变化触发重新适应');

const double = setup('double', [a4, a3Landscape], [[0, 1]]);
suite.check(Math.abs(double.dimensions(0).width - (820 + 1640 + 18)) < 0.01,
  '双页 spread 累加各页实际宽度与页间距', double.dimensions(0));
double.fitWidth();
suite.check(Math.abs(double.zoom - 900 / (820 + 1640 + 18)) < 0.001,
  '双页适应宽度能容纳不同规格页面', double.zoom);
double.fitPage();
suite.check(double.zoom < 0.5 && double.zoom > 0.05,
  '适应页面允许小于 50% 以容纳大页面', double.zoom);

const rotated = setup('single', [a3Landscape], [[0]], 0, 90);
suite.check(Math.abs(rotated.dimensions(0).width - (820 * 297 / 210)) < 0.01 &&
  Math.abs(rotated.dimensions(0).height - 1640) < 0.01,
  '旋转页面交换适应尺寸', rotated.dimensions(0));

suite.group('高度适配不受宽度约束，区域适配同时约束宽高');
{
  const state = setup('single', [a3Landscape], [[0]]);
  state.pagesElement.clientWidth = 300;
  state.fitHeight();
  const height = state.dimensions(0).height;
  suite.check(Math.abs(state.zoom - 600 / height) < 1e-10, 'FitHeight 精确使用可用高度');
  suite.check(state.dimensions(0).width * state.zoom > 300, '高度适配允许横向溢出');
  state.fitPage();
  suite.check(Math.abs(state.zoom - 300 / state.dimensions(0).width) < 1e-10, 'FitRect 受限宽度且完整容纳页面');
  suite.check(state.mapMode('FitHeight') === 'height' && state.mapMode('FitRect') === 'page', '文档缩放偏好不再混淆');
}
suite.group('真实视口扣除内边距、工具栏和横向滚动条');
{
  const state = setup('double', [a4, a3Landscape], [[0, 1]]);
  state.headerHeight = () => 70;
  state.status.offsetHeight = 30;
  state.pagesElement.offsetHeight = 1200;
  state.pagesElement.clientHeight = 1181;
  state.getComputedStyle = element => element === state.readerElement
    ? { paddingTop: '10px', paddingBottom: '20px' }
    : { paddingTop: '4px', paddingBottom: '24px', paddingLeft: '5px', paddingRight: '7px', borderTopWidth: '1px', borderBottomWidth: '1px' };
  const viewport = state.viewport();
  suite.check(viewport.width === 888 && viewport.height === 425, '实际可用区域排除边框与17px滚动条', viewport);
  state.fitHeight();
  suite.check(Math.abs(state.zoom * state.dimensions(0).height - 425) < 1e-8, '混合尺寸双页按最高页适配');
  state.fitPage();
  suite.check(state.zoom * state.dimensions(0).width <= 888.000001 && state.zoom * state.dimensions(0).height <= 425.000001, '双页连同缩放后的间距完整容纳');
}
suite.group('旋转、空白配页和极端尺寸');
for (const rotation of [0, 90, 180, 270]) {
  const state = setup('double', [a3Landscape], [[-1, 0]], 0, rotation);
  state.fitHeight();
  suite.check(Math.abs(state.dimensions(0).height * state.zoom - 600) < 1e-8, `${rotation}°双页包含配页仍精确适配高度`);
}
{
  const state = setup('single', [{ width: 1, height: 1 }], [[0]]);
  state.fitPage();
  suite.check(state.zoom > 3 && Math.abs(state.dimensions(0).height * state.zoom - 600) < 1e-8, '自动适配不被300%上限截断');
  state.pageInfos[0] = { width: 21000, height: 29700 };
  state.fitPage();
  suite.check(state.zoom < .05, '超大页面不被5%下限截断');
  state.pagesElement.clientWidth = 0;
  const previous = state.zoom;
  state.fitPage();
  suite.check(state.zoom === previous, '隐藏或零尺寸视口不产生非法缩放');
}
suite.group('重算保留模式且不覆盖全局偏好，手动缩放不重算');
{
  const state = setup('single', [a4], [[0]]);
  state.fitHeight();
  suite.check(state.persist === true, '显式选择可以保存模式');
  state.window.innerHeight = 800;
  state.refit();
  suite.check(state.zoomMode === 'height' && state.persist === false && Math.abs(state.zoom * state.dimensions(0).height - 800) < 1e-8, '窗口变化重算高度但不写用户偏好');
  state.zoomMode = 'manual'; state.zoom = 1.7;
  state.refit();
  suite.check(state.zoom === 1.7, '手动缩放退出自动适配');
}
suite.group('Dest/FitR使用自己的矩形而不是整页区域');
{
  const state = setup('single', [a4], [[0]]);
  const ratio = state.destFit(a4, { left: 0, top: 0, right: 1, bottom: 1 });
  suite.check(ratio > 3, '小目标矩形也不受手动缩放上限截断');
  state.pageRotation = 90;
  const zoom = state.destFit(a4, { left: 0, top: 0, right: 100, bottom: 200 });
  suite.check(Math.abs(zoom - Math.min(900 / (200 * 820 / 210), 600 / (100 * 820 / 210))) < 1e-10, '目标矩形旋转后共用视口适配');
}

suite.group('真实缩放入口保留精确比例和持久化边界');
{
  const state = {
    pageInfos: [a4], zoom: 1, zoomMode: 'manual', zoomModeStorageKey: 'zoom',
    writes: [], pageCards: [], textCache: new Map(),
    pageDPI: () => 96, applyPageWidth() {}, reloadPageImages() {}, scheduleVirtualUpdate() {}, updateNavigation() {},
  };
  state.fittedZoom = () => state.zoom;
  state.localStorage = { setItem: (...args) => state.writes.push(args) };
  load(state, sliceFunction('setZoom'), { expose: { set: 'setZoom' } });
  state.set(8, 'height');
  suite.check(state.zoom === 8 && state.zoomMode === 'height' && state.writes.length === 1, '高度适配超过300%不截断');
  state.set(.01, 'page', false);
  suite.check(state.zoom === .01 && state.writes.length === 1, '重算小比例不截断且不写存储');
  state.set(7, 'destination');
  suite.check(state.zoom === 7 && state.zoomMode === 'destination' && state.writes.length === 1, 'FitR一次性缩放不持久化也不截断');
  state.set(8);
  suite.check(state.zoom === 3 && state.zoomMode === 'manual', '手动缩放仍使用明确的交互范围');
  state.set(Number.NaN, 'height');
  suite.check(state.zoom === 3 && state.zoomMode === 'manual', '非法数值不污染缩放状态');
  state.fittedZoom = () => state.zoom > 1 ? 1.5 : 2;
  state.set(2, 'height', false);
  suite.check(state.zoom === 1.5, '新增横向滚动条后重新校准高度比例');
  state.fittedZoom = () => state.zoom >= 2 ? 1.5 : 2;
  state.set(2, 'height', false);
  suite.check(state.zoom === 1.5, '滚动条临界振荡使用安全比例且终止重算');
}

if (suite.report()) process.exitCode = 1;
