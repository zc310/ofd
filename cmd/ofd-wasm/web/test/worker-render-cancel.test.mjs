import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import { createSuite } from './harness.mjs';

const suite = createSuite('Worker 渲染取消');
const source = readFileSync(new URL('../worker.js', import.meta.url), 'utf8');
const tick = () => new Promise(resolve => setTimeout(resolve, 1));

async function waitFor(predicate) {
  for (let i = 0; i < 200; i++) {
    if (predicate()) return;
    await tick();
  }
  assert.fail('Worker 未在预期时间内处理消息');
}

function workerState(startup = Promise.resolve()) {
  const replies = [], calls = [], canceled = [], pending = new Map();
  const begin = (command, id, callback) => {
    calls.push({ command, id, callback });
    return new Promise((resolve, reject) => pending.set(id, { resolve, reject }));
  };
  const ofd = {
    renderPage: (_index, _options, id) => begin('renderPage', id),
    renderPages: (_indices, _options, id) => begin('renderPages', id),
    renderStream: (_indices, _options, callback, id) => begin('renderStream', id, callback),
    cancelRender: id => {
      canceled.push(id);
      pending.get(id)?.reject(new Error('context canceled'));
      pending.delete(id);
    },
  };
  const self = { ofd, addEventListener() {}, postMessage: message => replies.push(message) };
  vm.runInNewContext(source, {
    self, console, setTimeout, clearTimeout, ArrayBuffer, Uint8Array, Error,
    importScripts() {},
    Go: class { importObject = {}; run() { return Promise.resolve(); } },
    fetch: async () => {
      await startup;
      return { ok: true, clone() { return this; } };
    },
    WebAssembly: { instantiateStreaming: async () => ({ instance: {} }) },
  });
  return { replies, calls, canceled, pending, send: message => self.onmessage({ data: message }) };
}

for (const request of [
  { command: 'renderPage', index: 0 },
  { command: 'renderPages', indices: [0, 1] },
  { command: 'renderStream', indices: [0], options: { format: 'png' } },
  { command: 'renderStream', indices: [0, 1], options: { format: 'png' } },
  { command: 'renderStream', indices: [0], options: { format: 'pdf' } },
]) {
  const label = `${request.command}/${request.options?.format || 'png'}/${request.indices?.length || 1}`;
  const state = workerState();
  state.send({ ...request, id: 10 });
  await waitFor(() => state.pending.has(10));
  suite.check(state.calls[0].id === 10, `${label} 将请求 ID 传给 WASM`);
  if (request.command === 'renderStream' && request.indices.length === 1 && request.options.format !== 'pdf') {
    suite.check(state.calls[0].callback === null, '单页流式渲染用空回调占位并传入 ID');
  }
  suite.check(!state.replies.some(reply => reply.id === 10), `${label} 等待异步渲染结果`);

  state.send({ ...request, id: 11 });
  state.send({ command: 'cancel', target: 10 });
  await waitFor(() => state.replies.some(reply => reply.id === 10));
  const canceled = state.replies.find(reply => reply.id === 10);
  suite.check(state.canceled.includes(10) && !canceled.ok && canceled.error === '请求已取消', `${label} 取消正在运行的渲染`);

  await waitFor(() => state.pending.has(11));
  const bytes = new Uint8Array([1, 2, 3]);
  const streaming = request.command === 'renderStream' && (request.indices.length > 1 || request.options.format === 'pdf');
  if (streaming) state.calls[1].callback(bytes, 1);
  state.pending.get(11).resolve(request.command === 'renderPages' ? [bytes, bytes] : streaming ? null : bytes);
  state.pending.delete(11);
  await waitFor(() => state.replies.some(reply => reply.id === 11 && 'ok' in reply));
  const success = state.replies.find(reply => reply.id === 11 && 'ok' in reply);
  suite.check(success.ok, `${label} 取消后继续处理下一个请求`);
  if (streaming) {
    suite.check(state.replies.some(reply => reply.type === 'stream-chunk' && reply.id === 11 && reply.value.byteLength === 3), `${label} 保留分块传输`);
  } else if (request.command === 'renderPages') {
    suite.check(success.value.length === 2 && success.value.every(value => value instanceof ArrayBuffer && value.byteLength === 3), '批量渲染等待 Promise 后转换全部页面');
  } else {
    suite.check(success.value instanceof ArrayBuffer && success.value.byteLength === 3, `${label} 等待 Promise 后复制图像数据`);
  }
}

{
  const state = workerState();
  state.send({ command: 'renderPage', id: 20, index: 0 });
  await waitFor(() => state.pending.has(20));
  state.send({ command: 'renderPages', id: 21, indices: [0, 1] });
  state.send({ command: 'cancel', target: 21 });
  state.pending.get(20).resolve(new Uint8Array([1]));
  await waitFor(() => state.replies.some(reply => reply.id === 21));
  suite.check(!state.calls.some(call => call.id === 21) && state.replies.find(reply => reply.id === 21).error === '请求已取消', '排队期间取消的渲染不会进入 WASM');
}

{
  let ready;
  const startup = new Promise(resolve => { ready = resolve; });
  const state = workerState(startup);
  state.send({ command: 'renderPage', id: 30, index: 0 });
  state.send({ command: 'cancel', target: 30 });
  ready();
  await waitFor(() => state.replies.some(reply => reply.id === 30));
  suite.check(!state.calls.some(call => call.id === 30) && state.replies.find(reply => reply.id === 30).error === '请求已取消', '引擎启动期间取消的请求不会在就绪后开始渲染');
}

if (suite.report()) process.exitCode = 1;
