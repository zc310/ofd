// 浏览器阅读器脚本的测试支撑：直接从 viewer.js 原文按括号配平切出待测函数，
// 在 Node 里用桩驱动真实代码，而不是复制一份逻辑来测。
//
// 之所以切源码而不是引入构建步骤或 jsdom：viewer.js 是一个 6000 多行的浏览器
// 脚本，顶层就访问 document/window，没有模块边界可以只加载其中一部分。切原文
// 能保证"测的就是页面上跑的那份代码"，代价是函数改名或移动时测试要跟着调整
// （切不到会在下面的报错里直接指出函数名）。
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));

/** viewer.js 的绝对路径。 */
export const viewerPath = join(here, '..', 'viewer.js');

let cachedSource = null;

/** 读取并缓存 viewer.js 原文。 */
export function viewerSource() {
  if (cachedSource === null) cachedSource = readFileSync(viewerPath, 'utf8');
  return cachedSource;
}

/**
 * 跳过参数列表，返回函数体第一个 `{` 的位置。参数列表里可能有
 * `options = {}` 这类默认值，直接取第一个 `{` 会在这里截断。
 */
function bodyStart(source, from) {
  let index = from;
  let paren = 0;
  for (; index < source.length; index++) {
    const char = source[index];
    if (char === '(') paren++;
    else if (char === ')') {
      paren--;
      if (paren === 0) { index++; break; }
    }
  }
  while (index < source.length && source[index] !== '{') index++;
  if (index >= source.length) throw new Error('找不到函数体起始的 {');
  return index;
}

/** 从 openBrace 起做花括号配平，返回闭合位置之后的下标。 */
function matchBraces(source, openBrace) {
  let depth = 0;
  for (let index = openBrace; index < source.length; index++) {
    if (source[index] === '{') depth++;
    else if (source[index] === '}' && --depth === 0) return index + 1;
  }
  throw new Error('花括号不配平');
}

/** 切出一个具名函数（含 async）的完整源码。 */
export function sliceFunction(name) {
  const source = viewerSource();
  const match = new RegExp(`^(?:async\\s+)?function\\s+${name}\\s*\\(`, 'm').exec(source);
  if (!match) throw new Error(`viewer.js 里找不到函数 ${name}`);
  const end = matchBraces(source, bodyStart(source, match.index));
  return source.slice(match.index, end);
}

/** 切出具名函数从开头到指定标记语句结束的部分，用于只驱动函数开头的接管逻辑。 */
export function sliceFunctionUntil(name, marker) {
  const source = viewerSource();
  const match = new RegExp(`^(?:async\\s+)?function\\s+${name}\\s*\\(`, 'm').exec(source);
  if (!match) throw new Error(`viewer.js 里找不到函数 ${name}`);
  const at = source.indexOf(marker, match.index);
  if (at < 0) throw new Error(`函数 ${name} 中找不到标记语句 ${marker}`);
  return `${source.slice(match.index, at + marker.length)}\n  return true;\n}`;
}

/**
 * 切出形如 `x.addEventListener('event', () => { ... })` 里的箭头函数体。
 * 这类内联回调无法用上面的具名函数切法取到。
 */
export function sliceListenerHandler(anchor, event) {
  const source = viewerSource();
  const at = source.indexOf(anchor);
  if (at < 0) throw new Error(`viewer.js 里找不到 ${anchor}`);
  const eventAt = source.indexOf(`'${event}'`, at);
  if (eventAt < 0) throw new Error(`${anchor} 上没有 '${event}' 监听`);
  const arrowAt = source.indexOf('=>', eventAt);
  if (arrowAt < 0) throw new Error(`${anchor} 的 '${event}' 监听不是箭头函数`);
  // 从箭头函数参数列表的 '(' 开始切，才能拿到可直接调用的 `() => {...}`。
  let paramStart = arrowAt - 1;
  while (paramStart > 0 && source[paramStart] !== '(') paramStart--;
  if (source[paramStart] !== '(') throw new Error(`${anchor} 的 '${event}' 监听没有参数列表`);
  const braceAt = source.indexOf('{', arrowAt);
  if (braceAt < 0) throw new Error(`${anchor} 的 '${event}' 监听没有函数体`);
  return source.slice(paramStart, matchBraces(source, braceAt));
}

/**
 * 在一个 state 对象上执行切片得到的代码，返回 state 供调用方检查。
 *
 * 顶层 `export`/绑定在这个环境里不可用，因此用 `with (state)` 让被测代码里的自由
 * 变量解析到桩上。new Function 的函数体默认是 sloppy mode，所以 `with` 可用。
 */
export function runInState(code, state, extra = {}) {
  const names = Object.keys(extra);
  const values = names.map(name => extra[name]);
  // eslint-disable-next-line no-new-func
  const factory = new Function('state', ...names, `with (state) { ${code}\n }`);
  factory(state, ...values);
  return state;
}

/**
 * 载入切片代码：把函数声明与待调用表达式拼进同一个函数体执行。
 *
 * 必须放在同一个 `with` 块里——块内的函数声明不会泄漏出去，另开一个
 * new Function 就找不到它们了。
 *
 * expose 传 别名 → 真实函数名 的映射，把切片出来的函数挂到 state 上便于调用；
 * invoke 传函数名列表，按顺序无参调用（例如载入后立刻应用一次勾选意向）。
 */
export function load(state, code, { expose = {}, invoke = [] } = {}) {
  const calls = invoke.map(name => `${name}();`).join('\n');
  const exposeFns = Object.entries(expose)
    .map(([alias, local]) => `state[${JSON.stringify(alias)}] = ${local};`)
    .join('\n');
  // eslint-disable-next-line no-new-func
  const factory = new Function('state', `with (state) { ${code}\n${calls}\n${exposeFns}\n }`);
  factory(state);
  return state;
}

/** 把切片得到的回调源码变成可调用函数（回调体可能引用 state 上的桩）。 */
export function bind(state, callbackSource) {
  return new Function('state', `with (state) { return (${callbackSource}); }`)(state);
}


/** 断言收集器：统计通过数并在失败时保留原因，末尾统一报告。 */
export function createSuite(title) {
  const failures = [];
  let passed = 0;
  const show = value => (typeof value === 'string' ? value : JSON.stringify(value));

  const suite = {
    title,
    check(ok, label, detail) {
      const suffix = detail === undefined || detail === '' ? '' : ` — ${show(detail)}`;
      if (ok) {
        passed++;
        console.log(`  PASS  ${label}${suffix}`);
      } else {
        failures.push(label);
        console.log(`  FAIL  ${label}${suffix}`);
      }
      return ok;
    },
    group(name) { console.log(`\n${name}`); },
    report() {
      console.log(`\n${title}: ${passed} 项通过，${failures.length} 项失败`);
      if (failures.length) {
        console.log('失败项：');
        for (const item of failures) console.log(`  - ${item}`);
      }
      return failures.length;
    },
  };
  return suite;
}
