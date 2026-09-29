// 「?file=<url> 远程文档入口」的行为测试：参数解析、下载取消与状态交接、
// 地址栏改写，以及被取代的下载能否真正中止。
//
// 这组用例守着几个已经修过、但都很容易被后续改动踩回去的缺陷：
//   - 切换文档时若不接管 pendingDownload，被取代的下载会一路下完（上限 256MB）
//   - 滞留的 pendingDownload 会让 popstate 去重误判，吞掉前进/后退
//   - openRemoteDocument 与 fetchRemoteFile 各自 new AbortController 且互不传递，
//     外部的 abort() 作用不到 fetch 上（"取消打开"按钮形同虚设）
//   - fetchRemoteFile 的超时计时器在提前抛错的分支上泄漏
//   - 打开失败时若忽略 keepURL，会改写用户刚导航到的那条历史记录
import {
  sliceFunction, sliceFunctionUntil, load, viewerSource, createSuite,
} from './harness.mjs';

const suite = createSuite('远程文档 ?file=');

const code = [
  sliceFunction('remoteDocumentSource'),
  sliceFunction('decodePathName'),
  sliceFunction('sanitizeRemoteName'),
  sliceFunction('fetchRemoteFile'),
  sliceFunction('openRemoteDocument'),
  sliceFunction('documentURLForSource'),
  sliceFunction('replaceDocumentURL'),
].join('\n');
// openSelectedFile 很长，只取它开头负责"接管下载状态"的那几行。
const openSelectedPrefix = sliceFunctionUntil('openSelectedFile', 'pageCache.clear();');

/** 构造一个带远程下载相关桩的 state，并载入被测函数。 */
function makeState({ openSelectedBody = openSelectedPrefix, supported = true } = {}) {
  const state = {
    // 这些是 viewer.js 的顶层 const，被测函数以自由变量引用；with(state) 下必须
    // 由 state 提供，否则 ReferenceError 会被函数内部的 catch 吞掉、表现为"解析不出
    // 远程地址"这种难以定位的失败。
    remoteFileQueryKey: 'file',
    remoteFileNameQueryKey: 'name',
    remoteFileTimeout: 120_000,
    remoteFileMaxBytes: 256 << 20,
    opening: false,
    pendingDownload: undefined,
    currentRemoteSource: undefined,
    documentGeneration: 0,
    startupNotice: undefined,
    localFontsIntentApplied: false,
    cancelOpen: { hidden: true },
    startupScreen: { hidden: false },
    openRequest: undefined,
    saveReadingPosition() {},
    clearInjectedFonts() {},
    removeLocalFonts() {},
    localFontsActive: false,
    localFontsGestureUsed: false,
    localFontsIntentApplied: false,
    // localFontsSelect 为 null 时前缀里的 if 分支整段跳过；这里给一个真实对象，
    // 让 applyLocalFontsIntent 也参与，覆盖"切换文档后恢复勾选意向"这一步。
    localFontsSelect: { checked: false, disabled: false },
    localFontsStorageKey: 'ofd-local-fonts',
    startupProgressActive: true,
    pageCache: { clear() {} },
    applyLocalFontsIntent: () => true,
    setStatus() {},
    isCancelledError: error => error?.name === 'AbortError',
    formatFileSize: bytes => `${bytes} B`,
    _openCalls: 0,
    _status: [],
    _timers: [],
  };
  state.setStatus = message => { state._status.push(message); };
  state.localStorage = {
    store: new Map(),
    getItem(key) { return this.store.has(key) ? this.store.get(key) : null; },
    setItem(key, value) { this.store.set(key, value); },
  };
  state.window = {
    location: {
      href: 'https://reader.example/app/index.html',
      pathname: '/app/index.html',
      _search: '',
      get search() { return this._search; },
    },
    history: {
      replaceState(_state, _title, next) {
        state.window.location._search = next.split('?')[1] || '';
      },
    },
  };
  // with(state) 里 state 的同名属性会遮蔽块内的函数声明，因此先在 state 上占位，
  // 保证 openRemoteDocument 里的 openSelectedFile(...) 一定解析到桩；随后把切片出的
  // 真实实现装到 state 上，再包一层计数用来观察它是否被调用。
  state.openSelectedFile = async () => { state._openCalls++; return true; };
  if (openSelectedBody) {
    // 前缀会调用 replaceDocumentURL 等共享函数，所以要和 code 一起载入。
    load(state, [code, openSelectedBody].join('\n'),
      { expose: { _realOpen: 'openSelectedFile' } });
    const real = state._realOpen;
    state.openSelectedFile = async (file, options = {}) => {
      state._openCalls++;
      return real(file, options);
    };
  }
  state.File = undefined; // 走 Blob 分支，避开 Node 缺少 File 构造器的问题
  return state;
}

const tick = () => new Promise(r => setImmediate(r));
const realSetTimeout = globalThis.setTimeout;

/** 临时接管 setTimeout/clearTimeout 以观察超时计时器是否被清理。 */
function captureTimers(state) {
  const timers = [];
  globalThis.setTimeout = (fn, ms) => { const t = { fn, ms, cleared: false }; timers.push(t); return t; };
  globalThis.clearTimeout = t => { if (t) t.cleared = true; };
  state.restoreTimers = () => { globalThis.setTimeout = realSetTimeout; };
  return timers;
}

// ---------------------------------------------------------------- 参数解析
suite.group('remoteDocumentSource 参数解析');
{
  const parse = search => {
    const state = makeState();
    state.window.location._search = search;
    load(state, code, { expose: { _parse: 'remoteDocumentSource' } });
    return state._parse();
  };
  const ok = parse('?file=https%3A%2F%2Fcdn.example%2Fa.ofd');
  suite.check(ok && !ok.error && ok.href === 'https://cdn.example/a.ofd', '解析 http(s) 地址', ok && ok.href);
  suite.check(ok && ok.name === 'a.ofd', '取路径末段作文件名', ok && ok.name);
  const named = parse('?file=https%3A%2F%2Fcdn.example%2Fa.ofd&name=%E6%B5%8B%E8%AF%95.ofd');
  suite.check(named && named.name === '测试.ofd', '?name= 覆盖文件名', named && named.name);
  const bad = parse('?file=javascript%3Aalert(1)');
  suite.check(bad && bad.error, '非 http(s) 协议被拒绝', bad && bad.error);
  const ftp = parse('?file=ftp%3A%2F%2Fexample.com%2Fa.ofd');
  suite.check(ftp && ftp.error, 'ftp 被拒绝', ftp && ftp.error);
  suite.check(parse('') === undefined, '没有 ?file= 时返回 undefined');
  suite.check(parse('?name=only.ofd') === undefined, '只有 ?name= 时返回 undefined');
}

// ---------------------------------------------------------------- 下载被取代
suite.group('下载途中切换文档');
{
  const state = makeState();
  state.window.location._search = '?file=https%3A%2F%2Fcdn.example%2Fa.ofd';
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  let signal = null;
  let aborted = false;
  const realFetch = globalThis.fetch;
  globalThis.fetch = async (_url, options) => {
    signal = options.signal;
    options.signal.addEventListener('abort', () => { aborted = true; });
    await gate;
    return { ok: true, status: 200, headers: { get: () => '1024' },
             blob: async () => ({ size: 1024, type: 'application/ofd' }) };
  };
  captureTimers(state);

  const source = (() => {
    const probe = makeState();
    probe.window.location._search = '?file=https%3A%2F%2Fcdn.example%2Fa.ofd';
    load(probe, code, { expose: { _parse: 'remoteDocumentSource' } });
    return probe._parse();
  })();

  load(state, code, { expose: { _open: 'openRemoteDocument' } });
  const inflight = state._open(source, {});
  await tick();
  suite.check(!!state.pendingDownload, '下载期间 pendingDownload 已登记',
              `href=${state.pendingDownload && state.pendingDownload.href}`);

  // 用户改开本地文件：走真实 openSelectedFile 开头的接管逻辑。
  await state.openSelectedFile({ name: 'local.ofd' });
  release();
  await inflight;
  await tick();
  globalThis.fetch = realFetch;
  state.restoreTimers();

  // 被取代的下载必须止步于 generation 检查，不能再打开文档顶掉用户刚选的文件。
  suite.check(state._openCalls === 1, '被取代的下载没有再打开文档',
              `openSelectedFile 调用次数=${state._openCalls}（1 = 只有用户手动那一次）`);
  suite.check(state.pendingDownload === undefined, '切换文档后 pendingDownload 已清理',
              state.pendingDownload ? `仍滞留 ${state.pendingDownload.href}` : '');
  suite.check(aborted && signal?.aborted, '被取代的下载已真正中止（fetch 收到的 signal 已 aborted）',
              `abort 事件=${aborted} signal.aborted=${signal && signal.aborted}`);
}

// ---------------------------------------------------------------- popstate 去重
suite.group('popstate 去重不被滞留记录吞掉');
{
  // popstate 的去重条件是 currentRemoteSource?.href === source.href ||
  // pendingDownload?.href === source.href。切文档后两者都必须为空，否则退回该地址
  // 时会被静默跳过（地址栏变了但什么都不加载）。
  const state = makeState();
  state.pendingDownload = { name: 'a.ofd', href: 'https://cdn.example/a.ofd',
                            controller: { abort() {} } };
  state.currentRemoteSource = { name: 'a.ofd', href: 'https://cdn.example/a.ofd' };
  state.openSelectedFile = async () => { state._openCalls++; return true; };
  load(state, [code, openSelectedPrefix].join('\n'), { expose: { _open: 'openSelectedFile' } });
  await state._open({ name: 'local.ofd' });
  const target = 'https://cdn.example/a.ofd';
  const blocked = state.currentRemoteSource?.href === target || state.pendingDownload?.href === target;
  suite.check(!blocked, '切文档后去重条件不再命中，该地址可被 popstate 重新加载',
              `currentRemoteSource=${state.currentRemoteSource} pendingDownload=${state.pendingDownload}`);
}

// ---------------------------------------------------------------- 超时计时器
suite.group('下载失败路径的超时计时器清理');
{
  for (const [label, respond] of [
    ['HTTP 状态失败', async () => ({ ok: false, status: 404, headers: { get: () => '0' } })],
    ['Content-Length 超限', async () => ({ ok: true, status: 200,
      headers: { get: () => String(300 << 20) }, blob: async () => ({ size: 1 }) })],
  ]) {
    const state = makeState();
    const timers = captureTimers(state);
    const realFetch = globalThis.fetch;
    globalThis.fetch = respond;
    const source = { href: 'https://cdn.example/a.ofd', name: 'a.ofd' };
    load(state, code, { expose: { _fetch: 'fetchRemoteFile' } });
    try { await state._fetch(source); } catch (_) { /* 预期失败 */ }
    globalThis.fetch = realFetch;
    state.restoreTimers();
    suite.check(timers.length === 1 && timers[0].cleared, `${label}时清理了超时计时器`,
                timers.length ? (timers[0].cleared ? '' : '计时器泄漏') : '未创建计时器');
  }
}

// ---------------------------------------------------------------- 地址栏
suite.group('地址栏改写');
{
  const state = makeState();
  load(state, code, { expose: { _url: 'documentURLForSource', _replace: 'replaceDocumentURL' } });
  const url = state._url({ raw: 'https://cdn.example/a.ofd', href: 'https://cdn.example/a.ofd', name: 'a.ofd' });
  suite.check(url.searchParams.get('file') === 'https://cdn.example/a.ofd', '写入 ?file=',
              url.search);
  suite.check(url.searchParams.get('name') === 'a.ofd', '写入 ?name=', url.search);
  const rel = state._url({ raw: '../docs/a.ofd', href: 'https://reader.example/docs/a.ofd', name: 'a.ofd' });
  suite.check(rel.searchParams.get('file') === '../docs/a.ofd', '优先保留用户书写的相对地址',
              rel.search);
  state._replace(undefined);
  suite.check(state.window.location.search === '', '本地文档清掉 ?file=/?name=',
              JSON.stringify(state.window.location.search));
  state._replace({ raw: 'https://cdn.example/a.ofd', href: 'https://cdn.example/a.ofd', name: 'a.ofd' });
  suite.check(state.window.location.search.includes('file='), '远程文档写回 ?file= 以便刷新恢复',
              state.window.location.search);
}

// ---------------------------------------------------------------- 下载失败与地址栏
suite.group('下载失败时按 keepURL 决定是否改写地址栏');
{
  const failLoad = async keepURL => {
    const state = makeState();
    const timers = captureTimers(state);
    const realFetch = globalThis.fetch;
    globalThis.fetch = async () => ({ ok: false, status: 500, headers: { get: () => '0' } });
    state.window.location._search = '?file=https%3A%2F%2Fcdn.example%2Fa.ofd';
    const probe = makeState();
    probe.window.location._search = state.window.location._search;
    load(probe, code, { expose: { _parse: 'remoteDocumentSource' } });
    const source = probe._parse();
    load(state, code, { expose: { _open: 'openRemoteDocument' } });
    await state._open(source, { keepURL });
    globalThis.fetch = realFetch;
    state.restoreTimers();
    return state;
  };

  const mine = await failLoad(false);
  suite.check(mine.window.location.search === '',
              'keepURL 为假（本次加载）→ 失败时清掉 ?file=，不留打不开的地址',
              JSON.stringify(mine.window.location.search));

  const back = await failLoad(true);
  suite.check(back.window.location.search.includes('file='),
              'keepURL 为真（前进/后退触发）→ 不改写历史记录，保留返回目标',
              JSON.stringify(back.window.location.search));
}

// ---------------------------------------------------------------- keepURL 结构守卫
suite.group('openSelectedFile 失败分支的 keepURL 守卫（结构）');
{
  // 打开成功后的失败分支要走到函数尾部，行为测试需要的桩过多；这里对源码断言
  // 该分支同样受 !options.keepURL 约束——这与 openRemoteDocument 的下载失败分支
  // 是同一类缺陷，改动时两处要一起看。
  const source = viewerSource();
  const at = source.indexOf('async function openSelectedFile');
  const end = source.indexOf('\nfunction ', at + 1);
  const body = source.slice(at, end < 0 ? undefined : end);
  const hasGuard = /if \(remoteSource && !options\.keepURL\) replaceDocumentURL\(undefined\);/.test(body);
  suite.check(hasGuard, '打开失败时也遵守 keepURL，不改写用户刚导航到的历史记录');
}

process.exit(suite.report() === 0 ? 0 : 1);
