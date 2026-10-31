import { sliceFunction, load, createSuite } from './harness.mjs';
import { readFileSync } from 'node:fs';

const suite = createSuite('内部链接页面预览');
const indexHTML = readFileSync(new URL('../index.html', import.meta.url), 'utf8');

const eligibilityState = load({ pageInfos: Array.from({ length: 3 }) }, sliceFunction('linkPreviewEligible'), {
  expose: { eligible: 'linkPreviewEligible' },
});
suite.group('预览目标判断');
suite.check(eligibilityState.eligible({ target_page: 1 }), '有效内部链接可预览');
suite.check(!eligibilityState.eligible({ target_page: 3 }), '越界目标不预览');
suite.check(!eligibilityState.eligible({ target_page: 1, uri: 'https://example.com' }), '外部链接不预览');
suite.check(!eligibilityState.eligible({ target_page: 1, attachment_id: 'a1' }), '附件动作不预览');
suite.check(!eligibilityState.eligible({ target_page: 1, media_kind: 'movie' }), '媒体动作不预览');

suite.group('预览浮层');
const positioned = { style: {}, getBoundingClientRect: () => ({ width: 260, height: 200 }) };
load({ linkPreviewElement: positioned, window: { innerWidth: 800, innerHeight: 600 } }, sliceFunction('positionLinkPreview'), {
  expose: { position: 'positionLinkPreview' },
}).position({ left: 100, right: 120, top: 300, bottom: 320 });
suite.check(positioned.style.left === '12px' && positioned.style.top === '88px', '在链接上方居中显示并限制视口边距');
suite.check(/\.link-preview\s*\{[^}]*width: min\(820px, calc\(100vw - 24px\)\); height: min\(45vh, 420px\)/s.test(indexHTML), '桌面悬停预览放大并限制视口尺寸');
suite.check(/@media \(hover: none\), \(max-width: 700px\) \{ \.link-preview \{ display: none !important; \} \}/.test(indexHTML), '手机与触屏继续隐藏悬停预览');

let cleared = false;
const preview = { hidden: false, replaceChildren: () => { preview.cleared = true; } };
const hideState = load({
  linkPreviewElement: preview,
  linkPreviewTimer: 1,
  linkPreviewToken: 4,
  clearTimeout: id => { cleared = id === 1; },
}, sliceFunction('hideLinkPreview'), { expose: { hide: 'hideLinkPreview' } });
hideState.hide();
suite.check(cleared && preview.hidden && preview.cleared && hideState.linkPreviewToken === 5, '收起预览并使旧请求失效');

process.exitCode = suite.report() ? 1 : 0;
