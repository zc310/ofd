// 页面位图渲染始终跟随缩放，并限制最高栅格 DPI。
import { sliceFunction, viewerSource, load, createSuite } from './harness.mjs';

const suite = createSuite('页面渲染 DPI');
const code = ['imageRenderFormat', 'pageDPI'].map(sliceFunction).join('\n');

for (const [format, zoom, expected] of [
  ['png', 1, 96],
  ['png', 1.34, 129],
  ['png', 4, 300],
  ['jpg', 1.5, 144],
  ['svg', 2, 96],
]) {
  const state = load({ renderFormat: format, zoom }, code, { expose: { pageDPI: 'pageDPI' } });
  suite.check(state.pageDPI() === expected, `${format} zoom=${zoom} -> ${expected} DPI`, `${state.pageDPI()} DPI`);
}

suite.check(!/clarityPriority|ofd-clarity-priority/.test(viewerSource()), '清晰度优先开关与存储已移除');
suite.check(/cacheKey\('thumbnail', index, generation, 30, format\)/.test(viewerSource()), '缩略图缓存键使用 30 DPI');
suite.check(/format, dpi: 30, background: transparentRenderBackground/.test(viewerSource()), '缩略图按 30 DPI 渲染');
suite.check(/const linkPreviewDPI = 96/.test(viewerSource()) && /dpi: linkPreviewDPI/.test(viewerSource()), '链接预览独立按 96 DPI 渲染');

process.exitCode = suite.report() ? 1 : 0;
