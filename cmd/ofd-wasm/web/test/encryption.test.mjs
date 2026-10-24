import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import { load, sliceFunction, viewerSource, createSuite } from './harness.mjs';

const suite = createSuite('加密文档解锁');
const tick = () => new Promise(resolve => setImmediate(resolve));
const credentialError = code => Object.assign(new Error(code), { code });

function openState() {
  const state = {
    documentGeneration: 1, openRequest: undefined,
    openDocumentOptions: { pageCacheCapacity: 12 },
    isCancelledError: error => error.name === 'AbortError',
    calls: [], statuses: [], prompts: [], cancelled: false,
  };
  state.engine = { open: (data, options) => {
    state.calls.push({ data, options: { ...options }, originalOptions: options });
    return state.onOpen(data, options);
  } };
  state.setStatus = status => state.statuses.push(status);
  state.cancelOpening = () => { state.cancelled = true; state.documentGeneration++; };
  state.requestUnlockCredentials = async (_name, error) => {
    state.prompts.push(error.code);
    return state.credentials.shift();
  };
  state.file = { name: 'encrypted.ofd', arrayBuffer: async () => new Uint8Array([1, 2, 3]).buffer };
  return load(state, sliceFunction('openFileWithCredentials'), { expose: { open: 'openFileWithCredentials' } });
}

suite.group('打开、重试及不可重试错误');
{
  const state = openState();
  const result = { pageCount: 1 };
  state.onOpen = async () => result;
  assert.equal(await state.open(state.file, 1), result);
  suite.check(state.prompts.length === 0 && state.calls.length === 1, '普通文档直接打开');
}
{
  const state = openState();
  state.credentials = [{ password: 'wrong' }, { password: '  中文🔐  ', userName: 'User' }];
  state.onOpen = async (_data, options) => {
    if (options.password === undefined) throw credentialError('credentials_required');
    if (options.password === 'wrong') throw credentialError('decrypt_failed');
    return { pageCount: 2 };
  };
  assert.equal((await state.open(state.file, 1)).pageCount, 2);
  suite.check(state.prompts.join(',') === 'credentials_required,decrypt_failed', '口令错误后允许重试');
  suite.check(state.calls[2].options.password === '  中文🔐  ' && state.calls[2].options.userName === 'User', '保留口令空格、Unicode 和接收者');
  suite.check(new Set(state.calls.map(call => call.data)).size === 3, '重试使用独立缓冲，不复用已转移的数据');
  suite.check(state.calls.every(call => !Object.hasOwn(call.originalOptions, 'password')), '完成后移除临时配置中的口令');
  suite.check(!Object.hasOwn(state.openDocumentOptions, 'password'), '凭据不写入共享阅读配置');
}
{
  const state = openState();
  state.credentials = [{ password: '' }];
  state.onOpen = async (_data, options) => {
    if (options.password === undefined) throw credentialError('credentials_required');
    assert.equal(options.password, '');
    return { pageCount: 1 };
  };
  suite.check(!!await state.open(state.file, 1), '支持显式空口令');
}
for (const code of ['invalid_encryption', 'unsupported_encryption', 'limit_exceeded']) {
  const state = openState();
  state.onOpen = async () => { throw credentialError(code); };
  await assert.rejects(state.open(state.file, 1), error => error.code === code);
  suite.check(state.prompts.length === 0, `${code} 直接报告，不循环索要凭据`);
}

suite.group('取消和文档切换');
{
  const state = openState();
  state.credentials = [null];
  state.onOpen = async () => { throw credentialError('credentials_required'); };
  assert.equal(await state.open(state.file, 1), undefined);
  suite.check(state.cancelled && state.calls.length === 1, '取消凭据输入会取消打开');
}
{
  const state = openState();
  let resolvePrompt;
  state.requestUnlockCredentials = () => new Promise(resolve => { resolvePrompt = resolve; });
  state.onOpen = async () => { throw credentialError('credentials_required'); };
  const request = state.open(state.file, 1);
  await tick();
  state.documentGeneration++;
  const key = new Uint8Array([4, 5, 6]);
  resolvePrompt({ privateKey: key.buffer });
  assert.equal(await request, undefined);
  suite.check(state.calls.length === 1 && key.every(value => value === 0), '切换文档后丢弃旧凭据，并清理私钥缓冲');
}
{
  const state = openState();
  const key = new Uint8Array([4, 5, 6]);
  state.credentials = [{ privateKey: key.buffer }, null];
  state.onOpen = async (_data, options) => {
    throw credentialError(options.privateKey ? 'decrypt_failed' : 'credentials_required');
  };
  await state.open(state.file, 1);
  suite.check(key.every(value => value === 0), '私钥解锁失败后也清理缓冲');
}

function dialogState() {
  const state = {
    unlockPrompt: undefined,
    unlockUser: { value: '' }, unlockPassword: { value: '', disabled: false, focus() {} },
    unlockKeyPassword: { value: '', disabled: true },
    unlockKey: { value: '', disabled: true, files: [] },
    unlockPasswordFile: { value: '', disabled: true, files: [] },
    unlockMethod: { value: 'password' }, unlockError: { textContent: '', hidden: true },
    unlockDocument: { textContent: '' },
  };
  state.unlockDialog = { open: false, showModal: () => { state.unlockDialog.open = true; }, close: () => { state.unlockDialog.open = false; } };
  state.document = { querySelector: () => ({ hidden: false }) };
  return load(state, ['updateUnlockMethod', 'finishUnlockPrompt', 'requestUnlockCredentials', 'submitUnlockCredentials'].map(sliceFunction).join('\n'), {
    expose: { prompt: 'requestUnlockCredentials', submit: 'submitUnlockCredentials', finish: 'finishUnlockPrompt', update: 'updateUnlockMethod' },
  });
}

suite.group('实际对话框逻辑');
{
  const state = dialogState();
  const promise = state.prompt('<unsafe>.ofd', credentialError('decrypt_failed'));
  suite.check(state.unlockDialog.open && state.unlockDocument.textContent === '<unsafe>.ofd' && !state.unlockError.hidden, '文件名用文本显示，错误反馈留在对话框');
  state.unlockPassword.value = 'secret';
  state.unlockUser.value = 'User';
  await state.submit({ preventDefault() {} });
  assert.deepEqual(await promise, { password: 'secret', userName: 'User' });
  suite.check(!state.unlockDialog.open && state.unlockPassword.value === '' && state.unlockUser.value === '', '提交后关闭并清空输入字段');
}
{
  const state = dialogState();
  const promise = state.prompt('encrypted-key.ofd', credentialError('credentials_required'));
  state.unlockMethod.value = 'privateKey';
  state.update();
  state.unlockKey.files = [{ size: 3, arrayBuffer: async () => new Uint8Array([1, 2, 3]).buffer }];
  state.unlockKeyPassword.value = 'key-password';
  await state.submit({ preventDefault() {} });
  assert.deepEqual(await promise, {
    userName: '',
    privateKey: new Uint8Array([1, 2, 3]).buffer,
    privateKeyPassword: 'key-password',
  });
  suite.check(state.unlockKeyPassword.value === '', '提交后清空私钥密码字段');
}
{
  const state = dialogState();
  const promise = state.prompt('key.ofd', credentialError('credentials_required'));
  state.unlockMethod.value = 'privateKey';
  state.update();
  suite.check(state.unlockPassword.disabled && !state.unlockKey.disabled, '选择私钥后切换输入方式');
  suite.check(!state.unlockKeyPassword.disabled, '私钥方式启用私钥密码输入');
  await state.submit({ preventDefault() {} });
  suite.check(!state.unlockError.hidden && state.unlockDialog.open, '未选择私钥时保留对话框并提示');
  let resolveKey;
  state.unlockKey.files = [{ size: 3, arrayBuffer: () => new Promise(resolve => { resolveKey = resolve; }) }];
  state.unlockKeyPassword.value = 'key-password';
  const submission = state.submit({ preventDefault() {} });
  state.finish(null);
  const key = new Uint8Array([1, 2, 3]);
  resolveKey(key.buffer);
  await submission;
  assert.equal(await promise, null);
  suite.check(key.every(value => value === 0), '取消期间读完的私钥不会用于解锁');
}

{
  const state = dialogState();
  const promise = state.prompt('password-file.ofd', credentialError('credentials_required'));
  state.unlockMethod.value = 'passwordFile';
  state.update();
  suite.check(state.unlockPassword.disabled && state.unlockKey.disabled && !state.unlockPasswordFile.disabled, '口令文件使用独立输入方式');
  await state.submit({ preventDefault() {} });
  suite.check(!state.unlockError.hidden && state.unlockDialog.open, '未选择口令文件时提示');
  const password = '\uFEFF  中文🔐\r\n';
  const bytes = new TextEncoder().encode(password);
  state.unlockPasswordFile.value = 'password.txt';
  state.unlockPasswordFile.files = [{ size: bytes.length, arrayBuffer: async () => bytes.buffer }];
  await state.submit({ preventDefault() {} });
  assert.deepEqual(await promise, { password, userName: '' });
  suite.check(bytes.every(value => value === 0), '口令文件保留 BOM、空格和换行并清理读取缓冲');
  suite.check(state.unlockPasswordFile.value === '' && !state.unlockDialog.open, '提交后清空口令文件选择');
}
for (const [label, size, content] of [
  ['空口令', 0, []], ['非法 UTF-8', 1, [255]], ['超限口令文件', 65537, []],
]) {
  const state = dialogState();
  const promise = state.prompt('password-file.ofd', credentialError('credentials_required'));
  state.unlockMethod.value = 'passwordFile';
  const bytes = new Uint8Array(content);
  state.unlockPasswordFile.files = [{ size, arrayBuffer: async () => bytes.buffer }];
  await state.submit({ preventDefault() {} });
  if (label === '空口令') {
    assert.deepEqual(await promise, { password: '', userName: '' });
    suite.check(!state.unlockDialog.open, '空口令文件可提交');
  } else {
    suite.check(!state.unlockError.hidden && state.unlockDialog.open, `${label}被拒绝`);
    state.finish(null);
    await promise;
  }
}
{
  const state = dialogState();
  const promise = state.prompt('password-file.ofd', credentialError('credentials_required'));
  state.unlockMethod.value = 'passwordFile';
  let resolveFile;
  state.unlockPasswordFile.files = [{ size: 3, arrayBuffer: () => new Promise(resolve => { resolveFile = resolve; }) }];
  const submission = state.submit({ preventDefault() {} });
  state.finish(null);
  const bytes = new Uint8Array([65, 66, 67]);
  resolveFile(bytes.buffer);
  await submission;
  assert.equal(await promise, null);
  suite.check(bytes.every(value => value === 0), '取消期间读完的口令文件不会解锁且清理缓冲');
}

suite.group('Worker 和页面之间保留错误代码');
{
  const source = readFileSync(new URL('../worker.js', import.meta.url), 'utf8').replace(/\nstart\(\);\s*$/, '');
  const replies = [];
  const context = vm.createContext({
    importScripts() {}, setTimeout, ArrayBuffer, Uint8Array, console,
    self: { addEventListener() {}, postMessage: message => replies.push(message), ofd: { open: () => ({ error: '凭据不匹配', code: 'decrypt_failed' }) } },
  });
  vm.runInContext(`${source}\nreadyResolve(); self.testAPI = { execute, drain };`, context);
  const key = new Uint8Array([1, 2, 3]);
  const options = { password: 'secret', privateKey: key.buffer, privateKeyPassword: 'key-password' };
  await assert.rejects(context.self.testAPI.execute({ command: 'open', data: new ArrayBuffer(1), options }), error => error.code === 'decrypt_failed');
  suite.check(!Object.hasOwn(options, 'password') && !Object.hasOwn(options, 'privateKey') && !Object.hasOwn(options, 'privateKeyPassword') && key.every(value => value === 0), 'Worker 在失败后清理凭据');
  context.self.onmessage({ data: { id: 9, command: 'open', data: new ArrayBuffer(1) } });
  await tick();
  suite.check(replies[0]?.code === 'decrypt_failed' && replies[0]?.id === 9, 'Worker 回复附带稳定错误代码');
  const clientSource = viewerSource().slice(0, viewerSource().indexOf("window.addEventListener('error'"));
  const Client = new Function(`${clientSource}\nreturn OFDWorkerClient;`)();
  let rejected;
  Client.prototype.handleMessage.call({ pending: new Map([[9, { reject: error => { rejected = error; } }]]) }, replies[0]);
  suite.check(rejected?.code === 'decrypt_failed', '页面客户端保留错误代码供重试逻辑使用');
}

process.exitCode = suite.report() ? 1 : 0;
