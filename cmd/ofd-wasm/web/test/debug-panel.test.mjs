// 内存诊断面板的测试：?debug=1 开关的解析与记忆、内存口径的分组渲染。
//
// 被测代码全部从 viewer.js 原文切出（见 harness.mjs），不在测试里重抄逻辑。
import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('内存诊断面板');

const code = {
  formatFileSize: sliceFunction('formatFileSize'),
  debugRequested: sliceFunction('debugRequested'),
  debugEnabled: sliceFunction('debugEnabled'),
  debugMemoryRows: sliceFunction('debugMemoryRows'),
  clampDebugPosition: sliceFunction('clampDebugPosition'),
  debugPositionFromStorage: sliceFunction('debugPositionFromStorage'),
};

function baseState(overrides = {}) {
  const store = new Map();
  const state = {
    debugQueryKey: 'debug',
    debugStorageKey: 'ofd-debug',
    window: { location: { search: '' } },
    localStorage: {
      getItem: key => (store.has(key) ? store.get(key) : null),
      setItem: (key, value) => store.set(key, String(value)),
    },
    _store: store,
  };
  return Object.assign(state, overrides);
}

// ------------------------------------------------------------ URL 参数解析
suite.group('?debug 参数解析');
{
  const state = baseState();
  load(state, [code.debugRequested].join('\n'),
    { expose: { _requested: 'debugRequested' } });
  for (const [search, want] of [
    ['', undefined],
    ['?file=x', undefined],
    ['?debug=1', true],
    ['?debug=true', true],
    ['?debug=0', false],
    ['?debug=false', false],
    ['?file=x&debug=1', true],
  ]) {
    const got = state._requested(search);
    suite.check(got === want, `${JSON.stringify(search)} → ${String(want)}`, `got=${String(got)}`);
  }
}

// ------------------------------------------------------------ 开关记忆
suite.group('开关记忆（URL 优先，其次 localStorage）');
{
  const make = (search, stored) => {
    const state = baseState({ window: { location: { search } } });
    if (stored !== null) state._store.set('ofd-debug', stored);
    load(state, [code.debugRequested, code.debugEnabled].join('\n'),
      { expose: { _enabled: 'debugEnabled' } });
    return state;
  };

  const urlOn = make('?debug=1', null);
  suite.check(urlOn._enabled() === true, '?debug=1 → 开启', `enabled=${urlOn._enabled()}`);
  suite.check(urlOn._store.get('ofd-debug') === 'true', '?debug=1 写入记忆',
              `stored=${urlOn._store.get('ofd-debug')}`);

  const urlOff = make('?debug=0', 'true');
  suite.check(urlOff._enabled() === false, '?debug=0 覆盖已记忆的开启', `enabled=${urlOff._enabled()}`);
  suite.check(urlOff._store.get('ofd-debug') === 'false', '?debug=0 写入关闭',
              `stored=${urlOff._store.get('ofd-debug')}`);

  const rememberedOn = make('', 'true');
  suite.check(rememberedOn._enabled() === true, '无参数时沿用记忆的开启',
              `enabled=${rememberedOn._enabled()}`);

  const rememberedOff = make('', null);
  suite.check(rememberedOff._enabled() === false, '无参数且无记忆时默认关闭',
              `enabled=${rememberedOff._enabled()}`);
}

// ------------------------------------------------------------ 内存口径分组
suite.group('内存口径分组');
{
  const wasm = {
    linearBytes: 96 << 20,
    heapAlloc: 12 << 20,
    heapInuse: 20 << 20,
    heapSys: 64 << 20,
    heapReleased: 30 << 20,
    heapObjects: 12345,
    totalAlloc: 300 << 20,
    numGC: 7,
  };
  const report = {
    pageCacheBytes: 8 << 20,
    thumbnailCacheBytes: 1 << 20,
    textCacheSize: 3,
    pageRequests: 2,
    pageCardRequests: 1,
    pageInfoRequests: 0,
    fontBytes: 4 << 20,
    mountedPages: 5,
    jsHeap: null,
  };

  const state = baseState();
  load(state, [code.formatFileSize, code.debugMemoryRows].join('\n'),
    { expose: { _rows: 'debugMemoryRows' } });
  const result = state._rows(report, wasm);

  const flat = new Map();
  for (const section of result.sections) {
    for (const [label, value] of section.rows) flat.set(label, value);
  }
  suite.check(flat.get('线性内存（保留）') === '96.0 MB',
              '线性内存用 linearBytes 而不是 Go Sys', flat.get('线性内存（保留）'));
  suite.check(flat.get('Go 存活堆 heapAlloc') === '12.0 MB', '存活堆已展示',
              flat.get('Go 存活堆 heapAlloc'));
  suite.check(flat.get('页面缓存') === '8.0 MB', '页面缓存已展示', flat.get('页面缓存'));
  suite.check(flat.get('文字缓存条目') === '3', '文字缓存条目已展示', flat.get('文字缓存条目'));
  suite.check(!flat.has('JS 堆（Chromium）'), '无 performance.memory 时不显示 JS 堆');
  suite.check(typeof result.note === 'string' && result.note.includes('只增不减'),
              'note 说明 WASM 内存只增不减', result.note);

  const withHeap = state._rows({ ...report, jsHeap: 6 << 20 }, wasm);
  const heapRow = withHeap.sections
    .flatMap(section => section.rows)
    .find(([label]) => label === 'JS 堆（Chromium）');
  suite.check(heapRow && heapRow[1] === '6.0 MB', '有 performance.memory 时显示 JS 堆',
              JSON.stringify(heapRow));

  const noWasm = state._rows(report, null);
  const statusRow = noWasm.sections
    .flatMap(section => section.rows)
    .find(([label]) => label === '状态');
  suite.check(statusRow && statusRow[1] === 'WASM 未就绪', 'WASM 未就绪时给出占位行',
              JSON.stringify(statusRow));
}

// ------------------------------------------------------------ 面板位置
suite.group('拖动位置（视口夹取与解析）');
{
  const state = baseState();
  load(state, [code.clampDebugPosition, code.debugPositionFromStorage].join('\n'),
    { expose: { _clamp: 'clampDebugPosition', _parse: 'debugPositionFromStorage' } });

  const inside = state._clamp(100, 80, 340, 400, 1200, 900);
  suite.check(inside.left === 100 && inside.top === 80, '视口内的位置原样保留',
              JSON.stringify(inside));

  const negative = state._clamp(-50, -20, 340, 400, 1200, 900);
  suite.check(negative.left === 0 && negative.top === 0, '负坐标夹到 0',
              JSON.stringify(negative));

  const overflow = state._clamp(1100, 800, 340, 400, 1200, 900);
  suite.check(overflow.left === 860 && overflow.top === 500, '右下越界夹回视口内',
              JSON.stringify(overflow));

  const larger = state._clamp(50, 50, 1400, 1000, 1200, 900);
  suite.check(larger.left === 0 && larger.top === 0, '面板比视口大时左上对齐',
              JSON.stringify(larger));

  for (const [raw, want] of [
    [null, null],
    ['', null],
    ['not json', null],
    ['{}', null],
    ['{"left":"10","top":20}', null],
    ['{"left":10}', null],
    ['{"left":10,"top":20}', { left: 10, top: 20 }],
    ['{"left":-3.5,"top":7}', { left: -3.5, top: 7 }],
  ]) {
    const got = state._parse(raw);
    suite.check(JSON.stringify(got) === JSON.stringify(want),
                `解析 ${JSON.stringify(raw)} → ${JSON.stringify(want)}`, JSON.stringify(got));
  }
}

process.exit(suite.report() === 0 ? 0 : 1);
