// 虚拟列表大文档路径的回归测试：页面范围二分应只返回相交 spread，滚动更新不得
// 再全量扫描文档页数或缩略图槽位。
import { sliceFunction, viewerSource, load, createSuite } from './harness.mjs';

const suite = createSuite('虚拟列表性能');
const code = sliceFunction('pageSpreadRange');

suite.group('页面 spread 范围查找');
const spreads = Array.from({ length: 100_000 }, (_, index) => ({
  offset: index * 120,
  height: 100,
}));
const state = load({ pageSpreads: spreads }, code, { expose: { range: 'pageSpreadRange' } });
for (const [top, bottom, expected] of [
  [0, 99, [0, 1]],
  [120, 220, [1, 2]],
  [120 * 99_999, 120 * 99_999 + 100, [99_999, 100_000]],
  [120 * 50_000, 120 * 50_000 + 100, [50_000, 50_001]],
  [-500, -1, [0, 0]],
]) {
  const actual = state.range(top, bottom);
  suite.check(actual[0] === expected[0] && actual[1] === expected[1],
    `区间 ${top}..${bottom}`, `实际 ${actual}，期望 ${expected}`);
}

suite.group('窗口更新不全量扫描');
const source = viewerSource();
const pageUpdate = source.slice(source.indexOf('function updatePageVirtualWindow('), source.indexOf('\nfunction createThumbnail('));
const thumbnailUpdate = source.slice(source.indexOf('function updateThumbnailVirtualWindow('), source.indexOf('\nfunction documentKey('));
suite.check(!/pageSpreads\.(?:forEach|map|filter|some)\(/.test(pageUpdate),
  '页面滚动不遍历全部 spread');
suite.check(!/thumbnailSlots\.forEach\(/.test(thumbnailUpdate),
  '缩略图窗口不遍历全部槽位');
suite.check(/for \(const position of mountedPageSpreads\)/.test(pageUpdate),
  '离屏页面只清理已挂载 spread');
suite.check(/for \(const index of mountedThumbnailIndexes\)/.test(thumbnailUpdate),
  '离屏缩略图只清理已挂载按钮');

if (suite.report()) process.exitCode = 1;
