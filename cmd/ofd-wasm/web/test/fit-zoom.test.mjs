// 适应缩放需按每页的实际物理尺寸布局，不能把不同规格页面统一拉伸到同一宽度。
import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('异尺寸页面适应缩放');
const code = [
  'pageBaseDimensions', 'spreadDimensions', 'layoutBaseWidth', 'fitWidthZoom', 'fitPageZoom',
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
    getComputedStyle: () => ({ paddingTop: '0px', paddingBottom: '0px' }),
    headerHeight: () => 0,
    pageLayoutIsDouble: () => pageLayout !== 'single',
    currentSpreadPosition: () => spreadPages.findIndex(group => group.includes(state.current)),
    setZoom(value, mode) { this.zoom = value; this.zoomMode = mode; },
  };
  return load(state, code, { expose: {
    dimensions: 'spreadDimensions', baseWidth: 'layoutBaseWidth', fitWidth: 'fitWidthZoom', fitPage: 'fitPageZoom',
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
suite.check(single.zoom < 0.5 && single.zoom > 0.05, '适应页面可缩小到 50% 以下', single.zoom);

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

if (suite.report()) process.exitCode = 1;
