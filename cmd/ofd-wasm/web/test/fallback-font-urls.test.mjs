import assert from 'node:assert/strict';
import { load, sliceFunction, viewerSource } from './harness.mjs';

const state = {
  fallbackFontData: new Map(),
  fallbackFontLoads: new Map(),
  loadCachedFont: async url => {
    state.attempted.push(url);
    if (url === 'https://font.example/first.ttf') throw new Error('下载失败');
    return new ArrayBuffer(4);
  },
  attempted: [],
};
load(state, sliceFunction('loadFallbackFont'), { expose: { load: 'loadFallbackFont' } });

const source = {
  family: 'Noto Sans SC',
  urls: ['https://font.example/first.ttf', 'https://font.example/second.ttf'],
  weight: 400,
};
const font = await state.load(source);
assert.deepEqual(state.attempted, source.urls, '按配置顺序尝试字体地址，失败后继续下一个');
assert.equal(font.data.byteLength, 4);
assert.equal(font.family, source.family);

const config = viewerSource().match(/const fallbackFontURLs = (\[[\s\S]*?\n\]);/);
assert.ok(config, '应能找到默认回退字体配置');
const urls = [...config[1].matchAll(/'((?:https?:\/\/)[^']+)'/g)].map(match => match[1]);
assert.ok(urls.includes('https://zc310.github.io/assets/fonts/NotoSansSC-Regular.ttf'), '配置应包含指定的字体地址');
assert.equal(urls.length, 3, '当前字体配置包含三个顺序尝试的地址');
console.log('回退字体多地址加载测试通过');
