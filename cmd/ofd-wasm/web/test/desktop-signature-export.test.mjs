import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

function element() {
  return { children: [], classList: { toggle() {} }, isConnected: true,
    append(...items) { this.children.push(...items); },
    addEventListener(type, callback) { this[type] = callback; } };
}
const calls = [], statuses = [], downloads = [];
let finish, fail, cancellations = 0;
function request(kind, args) {
  calls.push({ kind, args });
  const promise = new Promise((resolve, reject) => { finish = resolve; fail = reject; });
  promise.cancel = () => { cancellations++; fail(new Error('请求已取消')); };
  return promise;
}
const state = {
  engine: { nativeDesktop: true,
    saveSignatureValue: (...args) => request('value', args),
    saveSignatureCertificate: (...args) => request('certificate', args),
  },
  document: { createElement: element }, documentGeneration: 1, documentActionBusy: false,
  desktopAttachmentSaveRequests: new Set(), setStatus: text => statuses.push(text),
  signatureMethodLabel: value => value || '', attachmentBadge: text => ({ text }),
  safeResourceName: ({ name }) => name, downloadBytes: (...args) => downloads.push(args),
};
load(state, ['buildSignatureItem', 'buildCertificateDetail', 'downloadSignatureValue', 'downloadCertificate',
  'saveDesktopFile', 'cancelDesktopAttachmentSaves', 'runAttachmentAction', 'derToPEM', 'certificateFileBase'].map(sliceFunction).join('\n'),
  { expose: { build: 'buildSignatureItem', value: 'downloadSignatureValue', certificate: 'downloadCertificate', cancel: 'cancelDesktopAttachmentSaves' } });
const info = { scope: 2, id: 's1', certificates: [{ slot: '外层', slot_key: 'outer' }] };
const cert = info.certificates[0];
const item = state.build(info, { skipStamps: true });
const valueButton = item.children.at(-1);
const certificateBox = item.children.find(child => child.className === 'signature-certs');
const [derButton, pemButton] = certificateBox.children[0].children.at(-1).children;
assert.equal(valueButton.textContent, '导出签名值');
assert.equal(derButton.textContent, '导出 DER');
assert.equal(pemButton.textContent, '导出 PEM');
const valueSave = valueButton.click();
assert.deepEqual(calls.at(-1), { kind: 'value', args: [2, 's1'] });
assert.equal(valueButton.disabled, true);
const count = calls.length;
await derButton.click();
assert.equal(calls.length, count, '签名和证书共用保存并发保护');
assert.match(statuses.at(-1), /当前保存操作/);
finish(true); await valueSave;
assert.equal(statuses.at(-1), '签名值已保存');
assert.equal(valueButton.disabled, false);
for (const [button, format] of [[derButton, 'der'], [pemButton, 'pem']]) {
  const pending = button.click();
  assert.deepEqual(calls.at(-1), { kind: 'certificate', args: [2, 's1', 'outer', format] });
  finish(true); await pending;
  assert.equal(statuses.at(-1), '证书已保存');
}
const cancelled = state.value(info); finish(false); await cancelled;
assert.match(statuses.at(-1), /已取消保存签名值/);
const denied = derButton.click(); fail(new Error('文档权限禁止导出')); await denied;
assert.match(statuses.at(-1), /保存证书失败.*禁止导出/);
assert.equal(derButton.disabled, false, '失败后允许重试');
state.documentActionBusy = true;
const beforeBusy = calls.length;
await state.value(info);
assert.equal(calls.length, beforeBusy);
state.documentActionBusy = false;
const stale = state.certificate(info, cert, 'pem');
state.documentGeneration++;
state.cancel();
const beforeStale = statuses.length;
await stale;
assert.equal(cancellations, 1);
assert.equal(statuses.length, beforeStale, '切换文档不显示旧保存结果');
assert.equal(downloads.length, 0, '桌面导出不把签名/证书字节交给网页下载');

// 浏览器仍从 Worker 获取字节并触发下载，不走原生保存绑定。
state.engine.nativeDesktop = false;
state.engine.signatureValue = async () => new Uint8Array([1, 2, 3]);
state.engine.signatureCertificate = async () => new Uint8Array([0x30, 1, 0]);
await state.value(info);
await state.certificate(info, cert, 'der');
await state.certificate(info, cert, 'pem');
assert.equal(downloads.length, 3);
assert.equal(downloads[0][1], 's1.dat');
assert.equal(downloads[1][1], 's1-outer.der');
assert.match(new TextDecoder().decode(downloads[2][0]), /^-----BEGIN CERTIFICATE-----\nMAEA\n-----END CERTIFICATE-----\n$/);
console.log('签名值与证书原生导出、权限失败、取消、旧会话及浏览器下载回归测试通过');
