// 图标清单的一致性检查：material-symbols-outlined-subset.woff2 只包含
// icons.json 里列出的图标连字，而字体子集没法在测试里解析（仓库没有 brotli
// 解压工具），所以改为守住清单本身：
//
//   - 代码里用到的每个图标名都必须在清单内，否则页面上会显示成空白
//   - 清单里的图标名必须都是合法的小写下划线形式，且没有重复
//   - CSS 里的 font-variation-settings 必须与清单的 axes 一致，否则字重与
//     填充样式会和字体子集里固化的实例不符
//
// 改动图标后要执行 make wasm-icon-font 重新生成字体，再执行 make build-wasm
// 重算 Service Worker 的 CACHE_NAME。
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { createSuite } from './harness.mjs';

const webDir = join(dirname(fileURLToPath(import.meta.url)), '..');
const suite = createSuite('图标清单');

const manifest = JSON.parse(readFileSync(join(webDir, 'icons.json'), 'utf8'));
const sources = {
  'index.html': readFileSync(join(webDir, 'index.html'), 'utf8'),
  'viewer.js': readFileSync(join(webDir, 'viewer.js'), 'utf8'),
};

const known = new Set(manifest.icons);

// 收集代码里真正用到的图标名。HTML 是 "…>icon<"，JS 走 textContent 赋值；
// 顺带排掉 font-variation-settings 里那些同形的键值（wght、FILL 等）。
const iconPattern = /^[a-z][a-z0-9_]*$/;
const settingsKeys = new Set(['wght', 'FILL', 'GRAD', 'opsz', 'normal', 'italic', 'auto']);

function usedIcons() {
  const used = new Map();
  const add = (name, where) => {
    if (!iconPattern.test(name) || settingsKeys.has(name)) return;
    if (!used.has(name)) used.set(name, where);
  };
  for (const [file, text] of Object.entries(sources)) {
    // HTML：<span class="material-symbols-outlined" …>icon_name</span>
    for (const match of text.matchAll(/material-symbols-outlined[^>]*>\s*([a-z][a-z0-9_]*)\s*</g)) {
      add(match[1], file);
    }
    // JS：element.textContent = 'icon_name'
    for (const match of text.matchAll(/\.textContent\s*=\s*'([a-z][a-z0-9_]*)'/g)) {
      add(match[1], file);
    }
    // JS 三元：只认紧跟在 textContent 赋值后面的那种。图元类型判断也写成
    // cond ? 'music_note' : 'movie'，但那是 mediaIcon() 的 return，不带 textContent；
    // 而 <audio>/<video> 的 'audio'/'video' 和 scrollTo 的 'smooth' 同理，不该算图标。
    for (const match of text.matchAll(/\.textContent\s*=\s*[^;?]*\?\s*'([a-z][a-z0-9_]*)'\s*:\s*'([a-z][a-z0-9_]*)'/g)) {
      add(match[1], file);
      add(match[2], file);
    }
    // mediaIcon 这类纯函数返回图标名：三元链 return cond ? 'a' : cond ? 'b' : 'c'，
    // 末支是 ': 'c''，所以第三段不带问号。
    for (const match of text.matchAll(/function\s+\w*[Ii]con\w*\([^)]*\)\s*\{\s*return[^;]*?\?\s*'([a-z][a-z0-9_]*)'\s*:[^;]*?\?\s*'([a-z][a-z0-9_]*)'\s*:\s*'([a-z][a-z0-9_]*)'/g)) {
      for (const group of match.slice(1)) add(group, file);
    }
  }
  return used;
}

suite.group('代码用到的图标都在清单内');
for (const [name, where] of [...usedIcons()].sort()) {
  suite.check(known.has(name), `${where} 的 ${name} 已列入 icons.json`);
}

suite.group('清单自身合法');
{
  const sorted = [...manifest.icons].sort();
  suite.check(
    sorted.every((name, index) => name === manifest.icons[index]),
    '清单按字典序排列，便于比对差异',
    manifest.icons.join(','),
  );
  suite.check(new Set(manifest.icons).size === manifest.icons.length, '清单没有重复项');
  suite.check(
    manifest.icons.every(name => iconPattern.test(name)),
    '图标名都是合法的小写形式',
  );
  suite.check(manifest.icons.includes('refresh'), '清单包含刷新图标');
  suite.check(manifest.family === 'Material Symbols Outlined', '字体族名与 @font-face 一致', manifest.family);
}

// 侧栏从纯文字改成图标后，下面几条守的是"图标能显示且不被 CSS 挤掉"。
suite.group('侧栏图标结构');
{
  const html = sources['index.html'];
  for (const [id, icon] of [['thumbnails', 'grid_view'], ['outline', 'account_tree'], ['bookmarks', 'bookmark']]) {
    const button = html.match(new RegExp(`<button id="sidebar-tab-${id}"[^>]*>([\\s\\S]*?)</button>`));
    suite.check(button !== null, `侧栏 ${id} 按钮存在`);
    if (!button) continue;
    suite.check(/sidebar-has-icon/.test(button[0]), `${id} 带 sidebar-has-icon（纵向排布）`);
    suite.check(
      new RegExp(`sidebar-tab-icon[^>]*>${icon}<`).test(button[1]),
      `${id} 用 ${icon} 图标`,
      button[1].replace(/\s+/g, ' ').slice(0, 70),
    );
    suite.check(/class="sidebar-tab-label"/.test(button[1]), `${id} 文字保留在 sidebar-tab-label 里`);
  }
  // 图标必须排在文字之前：纵向布局靠 flex-direction: column，顺序反了图标就在下面。
  const first = html.match(/<button id="sidebar-tab-thumbnails"[\s\S]*?<\/button>/)[0];
  suite.check(
    first.indexOf('sidebar-tab-icon') < first.indexOf('sidebar-tab-label'),
    '图标排在文字之前',
  );
  suite.check(
    /#sidebar-tabs > button\.sidebar-has-icon\s*\{[^}]*flex-direction:\s*column/.test(html),
    'CSS 把带图标的 tab 设为纵向排列',
  );
  // 「更多」按钮的 label id 被 JS 用来切换文字，改结构时不能弄丢。
  suite.check(/id="sidebar-tab-more-label"/.test(html), '「更多」的 label id 保留（JS 依赖它改文字）');
}

// 侧栏页签改成图标在上、文字在下后，默认宽度必须够放下"缩略图"三个字。
suite.group('侧栏默认宽度与页签文字');
{
  const html = sources['index.html'];
  const js = sources['viewer.js'];
  const declared = js.match(/const defaultSidebarWidth = (\d+);/);
  const cssInitial = html.match(/--thumbnail-column:\s*(\d+)px/);
  if (suite.check(declared !== null, 'viewer.js 声明了 defaultSidebarWidth')) {
    if (suite.check(cssInitial !== null, 'CSS 有 --thumbnail-column 初值')) {
      suite.check(
        Number(cssInitial[1]) === Number(declared[1]),
        'CSS 初值与 defaultSidebarWidth 一致，避免首帧宽度跳变',
        `CSS ${cssInitial[1]}px vs JS ${declared[1]}px`,
      );
      // 页签文字净宽 ≈ (宽度 - 页签区 padding 20 - (n-1) 个 gap 3) / n - 按钮 padding 6 - 边框 2。
      // n 必须按实际页签数算：搜索页签补上文字后从纯图标窄条变成参与平分，n 从 5 变 6。
      const tabButtons = [...html.matchAll(/<button id="sidebar-tab-[a-z]+"/g)];
      const inner = (Number(declared[1]) - 20 - 3 * (tabButtons.length - 1)) / tabButtons.length - 6 - 2;
      suite.check(
        inner >= 36,
        `默认宽度下最宽的页签文字（"缩略图" 3 字 × 12px ≈ 36px）放得下`,
        `${tabButtons.length} 个页签，文字净宽约 ${inner.toFixed(1)}px`,
      );
    }
  }
  for (const id of ['thumbnails', 'outline', 'bookmarks', 'search', 'more']) {
    const button = html.match(new RegExp(`<button id="sidebar-tab-${id}"[^>]*>([\\s\\S]*?)</button>`));
    if (!suite.check(button !== null, `页签 ${id} 存在`)) continue;
    suite.check(
      /class="sidebar-tab-label"[^>]*>[^<]+<\//.test(button[1]),
      `${id} 有可见文字标签`,
      button[1].replace(/\s+/g, ' ').slice(0, 60),
    );
    // 有可见文字就不该再有 aria-label，否则屏幕阅读器会把同一句话读两遍。
    const head = button[0].slice(0, button[0].indexOf('>') + 1);
    suite.check(!/aria-label=/.test(head), `${id} 不用 aria-label 重复可见文字`);
  }
  // 搜索页签补文字后不能再写死窄宽度，否则图标+文字会被压扁。
  suite.check(
    !/#sidebar-tab-search \{[^}]*flex:\s*0\s+0/.test(html),
    '搜索页签不再固定成窄条，已参与宽度平分',
  );
  // 页签区左右 padding 与 gap 改了会让上面的估算失效，锁住取值。
  suite.check(/#sidebar-tabs \{[^}]*padding: 0 10px/.test(html), '页签区 padding 是 0 10px');
  suite.check(/#sidebar-tabs > button \{[^}]*padding: 5px 3px/.test(html), '页签按钮 padding 是 5px 3px');
  suite.check(/\.sidebar-tab-label \{[^}]*text-overflow: ellipsis/.test(html), '文字过长时仍走 ellipsis 而不是撑破布局');
}

suite.group('CSS 变体设置与清单 axes 一致');
{
  const css = sources['index.html'];
  const match = css.match(/font-variation-settings:\s*'wght'\s*(\d+)\s*,\s*'FILL'\s*(\d+)\s*,\s*'GRAD'\s*(\d+)\s*,\s*'opsz'\s*(\d+)/);
  if (suite.check(match !== null, '找到 .material-symbols-outlined 的 font-variation-settings')) {
    const [, wght, fill, grad, opsz] = match;
    const axes = manifest.axes;
    suite.check(Number(wght) === axes.wght, `wght 一致（${wght}）`);
    suite.check(Number(fill) === axes.FILL, `FILL 一致（${fill}）`);
    suite.check(Number(grad) === axes.GRAD, `GRAD 一致（${grad}）`);
    suite.check(Number(opsz) === axes.opsz, `opsz 一致（${opsz}）`);
  }
  const face = css.match(/@font-face\s*\{[^}]*font-family:\s*'Material Symbols Outlined'[^}]*\}/);
  if (suite.check(face !== null, '找到 @font-face 定义')) {
    // 字体的 woff2 由 Google Fonts 按 axes 里固定的 wght 生成，@font-face 的
    // font-weight 必须跟着改成同一个值，否则浏览器匹配不到该字重而回退字体，
    // 图标会整片变成空白。
    suite.check(
      /font-weight:\s*500/.test(face[0]),
      '@font-face 的 font-weight 与 axes.wght 一致（500）',
      face[0].replace(/\s+/g, ' ').slice(0, 120),
    );
  }
}

process.exit(suite.report());