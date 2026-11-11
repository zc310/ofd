import assert from 'node:assert/strict';
import { load, sliceFunction, viewerSource } from './harness.mjs';

// 运行页面中的真实工厂，确认桌面引擎不会启动 WASM Worker。
{
  const engine = { ready: Promise.resolve(), terminate() {} };
  const state = {
    window: { ofdReaderEngineFactory: () => engine },
    OFDWorkerClient: class { constructor() { throw new Error('不应启动 Worker'); } },
  };
  load(state, sliceFunction('createReaderEngine'), { expose: { create: 'createReaderEngine' } });
  assert.equal(state.create(), engine);
}

// 未注入时保留浏览器的 Worker 引擎。
{
  class WorkerClient {}
  const state = { window: {}, OFDWorkerClient: WorkerClient };
  load(state, sliceFunction('createReaderEngine'), { expose: { create: 'createReaderEngine' } });
  assert.ok(state.create() instanceof WorkerClient);
}

// 页面关闭时通过引擎契约释放资源，而非访问具体 Worker。
assert.match(viewerSource(), /engine\.terminate\(\)/);
assert.doesNotMatch(viewerSource(), /engine\.worker\.terminate\(\)/);
console.log('引擎工厂与释放契约测试通过');
