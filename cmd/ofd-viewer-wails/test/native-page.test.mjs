import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('../prototype/native-page.js', import.meta.url), 'utf8');
const { NativePageClient } = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
const prefix = `/reader/${'b'.repeat(64)}/pages/`;
const bytes = new Uint8Array([1, 2, 3]).buffer;
// WebKit 要求 Window.fetch 的接收者为 Window，不能当作客户端方法调用。
const originalFetch = globalThis.fetch;
try {
  globalThis.fetch = async function (url, options) {
    assert.equal(this, globalThis);
    assert.equal(url, `${prefix}0?dpi=96&format=png&background=%2300000000`);
    assert.equal(options.cache, 'no-store');
    return { ok: true, arrayBuffer: async () => bytes };
  };
  assert.equal(await new NativePageClient(prefix).renderPage(0), bytes);
} finally {
  globalThis.fetch = originalFetch;
}
let seen;
const client = new NativePageClient(prefix, async (url, options) => {
  seen = { url, options };
  return { ok: true, arrayBuffer: async () => bytes };
});
assert.equal(await client.renderPage(2, { dpi: 144, format: 'jpg', background: '#ffffffff', purpose: 'export' }), bytes);
assert.equal(seen.url, `${prefix}2?dpi=144&format=jpg&background=%23ffffffff&purpose=export`);
assert.equal(seen.options.cache, 'no-store');
assert.equal(await client.renderPage(1, { format: 'svg' }), bytes);
assert.equal(seen.url, `${prefix}1?dpi=96&format=svg&background=%2300000000`);
assert.throws(() => client.pageURL(0, { format: 'pdf' }), /仅支持/);
assert.throws(() => new NativePageClient('/reader/other/pages/'), /无效/);
let resolveFetch;
const delayed = new NativePageClient(prefix, () => new Promise(resolve => { resolveFetch = resolve; }));
const request = delayed.renderPage(0);
await Promise.resolve();
request.cancel();
resolveFetch({ ok: true, arrayBuffer: async () => bytes });
await assert.rejects(request, error => error.name === 'AbortError');
const notStarted = delayed.renderPage(1);
notStarted.cancel();
await assert.rejects(notStarted, error => error.name === 'AbortError');

globalThis.wails = { Call: { ByName: async (method, documentPrefix, index) => {
  assert.equal(documentPrefix, prefix);
  assert.equal(index, 0);
  if (method.endsWith('.PageInfo')) return { index: 0, width: 210, height: 297 };
  return { runs: [{ text: 'A', x: 1, y: 2, width: 3, height: 4, scope: 0, template_id: 7, font: 5,
    size: 4, weight: 400, readDirection: 0, charDirection: 0, fontFamily: 'Go', bold: false, italic: false,
    glyphs: [{ text: 'A', x: 1, y: 2, width: 3, height: 4, angle: 0 }] }] };
} } };
assert.deepEqual(await client.pageInfo(0), { index: 0, width: 210, height: 297 });
const text = await client.text(0);
assert.equal(text[0].template_id, 7);
assert.equal(text[0].fontFamily, 'Go');
assert.equal(text[0].glyphs[0].text, 'A');
globalThis.wails.Call.ByName = async (method, token, query) => {
  assert.equal(method, 'main.Desktop.Search');
  assert.equal(token, prefix);
  assert.equal(query, 'A');
  return { scanned: 1, truncated: false, results: [{ page: 0, run: 2, text: 'A', start: 0, end: 1,
    rects: [{ x: 1, y: 2, width: 3, height: 4, angle: 0 }] }] };
};
const search = await client.search('A');
assert.deepEqual(search, { scanned: 1, truncated: false, results: [{ page: 0, run: 2, text: 'A', start: 0, end: 1,
  rects: [{ x: 1, y: 2, width: 3, height: 4, angle: 0 }] }] });
globalThis.wails.Call.ByName = async (method, token) => {
  assert.equal(method, 'main.Desktop.Outline');
  assert.equal(token, prefix);
  return { page_mode: 'UseOutlines', nodes: [{ title: '章节', page: 0, uri: '', expanded: false,
    dest: { type: 'XYZ', left: null, top: 4, right: null, bottom: null, zoom: 2 },
    children: [{ title: '子项', page: 1, uri: '', dest: null, expanded: null, children: [] }] }],
    bookmarks: [{ name: '书签', page: 1, dest: null }] };
};
assert.deepEqual(await client.outline(), { page_mode: 'UseOutlines', nodes: [{ title: '章节', page: 0, uri: '', expanded: false,
  dest: { type: 'XYZ', left: null, top: 4, right: null, bottom: null, zoom: 2 },
  children: [{ title: '子项', page: 1, uri: '', dest: null, expanded: null, children: [] }] }],
  bookmarks: [{ name: '书签', page: 1, dest: null }] });
globalThis.wails.Call.ByName = async (method, token) => {
  assert.equal(method, 'main.Desktop.Preferences');
  assert.equal(token, prefix);
  return { page_layout: 'TwoPageL', tab_display: 'DocTitle', document_title: '标题', hide_toolbar: true,
    hide_menubar: false, hide_window_ui: false, zoom_mode: 'FitWidth', zoom: null };
};
assert.deepEqual(await client.preferences(), { page_layout: 'TwoPageL', tab_display: 'DocTitle', document_title: '标题',
  hide_toolbar: true, hide_menubar: false, hide_window_ui: false, zoom_mode: 'FitWidth', zoom: null });
globalThis.wails.Call.ByName = async (method, token) => {
  assert.equal(method, 'main.Desktop.Stats');
  assert.equal(token, prefix);
  return { fonts: 2, attachments: 1, media: 3, annotation_pages: 4, signatures: 5 };
};
assert.deepEqual(await client.stats(), { fonts: 2, attachments: 1, media: 3, annotation_pages: 4, signatures: 5 });
globalThis.wails.Call.ByName = async (method, token) => {
  assert.equal(method, 'main.Desktop.Attachments');
  assert.equal(token, prefix);
  return [{ scope: 0, id: '7', name: '说明.txt', exists: true, visible: true }];
};
assert.deepEqual(await client.attachments(), [{ scope: 0, id: '7', name: '说明.txt', exists: true, visible: true }]);
globalThis.wails.Call.ByName = async (method, token) => {
  assert.equal(method, 'main.Desktop.Media');
  assert.equal(token, prefix);
  return [{ scope: 0, id: 200, name: 'sound.wav', type: 'Audio', exists: true }];
};
assert.deepEqual(await client.media(), [{ scope: 0, id: 200, name: 'sound.wav', type: 'Audio', exists: true }]);
globalThis.wails.Call.ByName = async (method, token, page) => {
  assert.equal(method, 'main.Desktop.AnnotationsForPage');
  assert.equal(token, prefix);
  assert.equal(page, 1);
  return [{ page: 1, remark: '备注' }];
};
assert.deepEqual(await client.annotations(1), [{ page: 1, remark: '备注' }]);
globalThis.wails.Call.ByName = async (method, token, page) => {
  assert.equal(method, 'main.Desktop.PageLinksForPage');
  assert.equal(token, prefix);
  assert.equal(page, 1);
  return [{ target_page: 2, boundary: { x: 1, y: 2, width: 3, height: 4 } }];
};
assert.deepEqual(await client.pageLinksForPage(1), [{ target_page: 2, boundary: { x: 1, y: 2, width: 3, height: 4 } }]);
globalThis.wails.Call.ByName = async (method, token, page, ids) => {
  assert.equal(token, prefix);
  assert.equal(page, 1);
  if (method === 'main.Desktop.Layers') return [{ key: '20', id: '20', visible: true }];
  assert.equal(method, 'main.Desktop.SetHiddenLayers');
  assert.deepEqual(ids, ['20']);
  return null;
};
assert.deepEqual(await client.layers(1), [{ key: '20', id: '20', visible: true }]);
const updateLayers = client.setHiddenLayers(1, ['20']);
updateLayers.cancel();
assert.equal(await updateLayers, true, '图层写入不可伪装成已取消');
await assert.rejects(client.setHiddenLayers(1, [42]), /字符串数组/);
globalThis.wails.Call.ByName = async (method, token) => {
  assert.equal(method, 'main.Desktop.SignatureSummaries');
  assert.equal(token, prefix);
  return [{ id: 's1', provider: '测试', pages: [0] }];
};
assert.deepEqual(await client.signatureSummaries(), [{ id: 's1', provider: '测试', pages: [0] }]);
globalThis.wails.Call.ByName = async (method, token) => {
  assert.equal(method, 'main.Desktop.SignatureDetails');
  assert.equal(token, prefix);
  return [{ id: 's1', has_verification: true, verified: true, trust_checked: false, validation: { status: 'incomplete', checks: [] } }];
};
assert.deepEqual(await client.signatureDetails(), [{ id: 's1', has_verification: true, verified: true, trust_checked: false, validation: { status: 'incomplete', checks: [] } }]);
globalThis.wails.Call.ByName = async (method, token, scope, id) => {
  assert.equal(method, 'main.Desktop.MediaImagePreview');
  assert.equal(token, prefix);
  assert.equal(scope, 0);
  assert.equal(id, 200);
  return { mime: 'image/png', data: 'AQID' };
};
const image = await client.mediaImagePreview(0, 200);
assert.equal(image.mime, 'image/png');
assert.deepEqual([...image.data], [1, 2, 3]);
await assert.rejects(client.mediaImagePreview(-1, 200), /无效资源标识/);
globalThis.wails.Call.ByName = async (method, token, scope, id) => {
  assert.equal(method, 'main.Desktop.AttachmentTextPreview');
  assert.equal(token, prefix);
  assert.equal(scope, 0);
  assert.equal(id, 'a');
  return '中文<script/>';
};
assert.equal(await client.attachmentTextPreview(0, 'a'), '中文<script/>');
await assert.rejects(client.attachmentTextPreview(-1, 'a'), /无效附件标识/);
await assert.rejects(client.attachmentTextPreview(0, ''), /无效附件标识/);
globalThis.wails.Call.ByName = async () => null;
await assert.rejects(client.attachmentTextPreview(0, 'a'), /文本预览格式无效/);
globalThis.wails.Call.ByName = async (method, token, scope, id) => {
  assert.equal(method, 'main.Desktop.SaveAttachment');
  assert.equal(token, prefix);
  assert.equal(scope, 0);
  assert.equal(id, 'a');
  return true;
};
assert.equal(await client.saveAttachment(0, 'a'), true);
await assert.rejects(client.saveAttachment(-1, 'a'), /无效附件标识/);
globalThis.wails.Call.ByName = async () => false;
assert.equal(await client.saveAttachment(0, 'a'), false, '取消保存不是失败');
globalThis.wails.Call.ByName = async (method, token, scope, id) => {
  assert.equal(method, 'main.Desktop.SaveMedia');
  assert.equal(token, prefix);
  assert.equal(scope, 0);
  assert.equal(id, 200);
  return true;
};
assert.equal(await client.saveMedia(0, 200), true);
await assert.rejects(client.saveMedia(0, Number.MAX_SAFE_INTEGER + 1), /无效资源标识/);
globalThis.wails.Call.ByName = async (method, token, ...args) => {
  assert.equal(token, prefix);
  if (method.endsWith('.SaveSignatureValue')) { assert.deepEqual(args, [2, 's1']); return true; }
  assert.equal(method, 'main.Desktop.SaveSignatureCertificate');
  assert.deepEqual(args.slice(0, 3), [2, 's1', 'outer']);
  assert.ok(['der', 'pem'].includes(args[3]));
  return true;
};
assert.equal(await client.saveSignatureValue(2, 's1'), true);
for (const format of ['der', 'pem']) assert.equal(await client.saveSignatureCertificate(2, 's1', 'outer', format), true);
await assert.rejects(client.saveSignatureValue(-1, 's1'), /无效签名导出参数/);
await assert.rejects(client.saveSignatureValue(0, '  '), /无效签名导出参数/);
await assert.rejects(client.saveSignatureCertificate(2, 's1', 'unknown', 'der'), /无效证书导出参数/);
await assert.rejects(client.saveSignatureCertificate(2, 's1', 'outer', 'exe'), /无效证书导出参数/);
globalThis.wails.Call.ByName = async () => false;
assert.equal(await client.saveSignatureValue(2, 's1'), false);
assert.equal(await client.saveSignatureCertificate(2, 's1', 'seal', 'pem'), false);
let expectedExportFormat = 'png';
globalThis.wails.Call.ByName = async (method, token, ...args) => {
  assert.equal(method, 'main.Desktop.SignatureSealPreview');
  assert.equal(token, prefix);
  assert.deepEqual(args, [2, 's1', 3]);
  return { mime: 'image/png', data: 'AQID' };
};
assert.deepEqual(await client.signatureSealPreview(2, 's1', 3), { mime: 'image/png', data: new Uint8Array([1, 2, 3]) });
await assert.rejects(client.signatureSealPreview(-1, 's1', 0), /无效印章预览参数/);
await assert.rejects(client.signatureSealPreview(0, '', 0), /无效印章预览参数/);
await assert.rejects(client.signatureSealPreview(0, 's1', -1), /无效印章预览参数/);
globalThis.wails.Call.ByName = async () => ({ mime: 'image/svg+xml', data: 'AQID' });
await assert.rejects(client.signatureSealPreview(2, 's1', 3), /图片预览格式无效/);
globalThis.wails.Call.ByName = async () => ({ mime: 'image/png', data: '' });
await assert.rejects(client.signatureSealPreview(2, 's1', 3), /图片预览大小无效/);
let expectedExportIndices = [0];
globalThis.wails.Call.ByName = async (method, token, ...args) => {
  assert.equal(token, prefix);
  if (method.endsWith('.MediaThumbnail')) assert.deepEqual(args, [0, 200]);
  else { assert.equal(method, 'main.Desktop.SignatureSealThumbnail'); assert.deepEqual(args, [2, 's1', 3]); }
  return { mime: 'image/png', data: 'AQID', width: 2000, height: 1000 };
};
assert.deepEqual(await client.mediaThumbnail(0, 200), { mime: 'image/png', data: new Uint8Array([1, 2, 3]), width: 2000, height: 1000 });
assert.equal((await client.signatureSealThumbnail(2, 's1', 3)).width, 2000);
await assert.rejects(client.mediaThumbnail(-1, 200), /无效资源标识/);
await assert.rejects(client.signatureSealThumbnail(2, '', 3), /无效印章预览参数/);
globalThis.wails.Call.ByName = async () => ({ mime: 'image/png', data: 'AQID', width: -1, height: 10 });
await assert.rejects(client.mediaThumbnail(0, 200), /缩略图格式无效/);
globalThis.wails.Call.ByName = async (method, token, ...args) => {
  assert.equal(method, 'main.Desktop.ExportPages');
  assert.equal(token, prefix);
  assert.deepEqual(args, [expectedExportIndices, expectedExportFormat, 150, '#ffffffff']);
  return true;
};
assert.equal(await client.exportPages([0], { format: 'png', dpi: 150, background: '#ffffffff' }), true);
for (const format of ['pdf', 'txt', 'png', 'jpg', 'svg']) {
  expectedExportFormat = format;
  expectedExportIndices = [1, 0];
  assert.equal(await client.exportPages([1, 0], { format, dpi: 150, background: '#ffffffff' }), true);
}
await assert.rejects(client.exportPages([0], { format: 'zip', dpi: 150, background: '#ffffffff' }), /无效页面导出参数/);
await assert.rejects(client.exportPages(Array.from({ length: 201 }, (_, i) => i), { format: 'png', dpi: 150, background: '#ffffffff' }), /无效页面导出参数/);
await assert.rejects(client.exportPages([0, 0], { format: 'pdf', dpi: 150, background: '#ffffffff' }), /无效页面导出参数/);
await assert.rejects(client.annotations(), /无效页码/);
globalThis.wails.Call.ByName = async (method, token) => {
  assert.equal(method, 'main.Desktop.Annotations');
  assert.equal(token, prefix);
  return { items: [], scanned: 2, truncated: true };
};
assert.deepEqual(await client.annotationReport(), { items: [], scanned: 2, truncated: true });
globalThis.wails.Call.ByName = async (method, token, ...args) => {
  assert.equal(token, prefix);
  if (method.endsWith('.FontUsage')) {
    assert.deepEqual(args, [2, 7, 3, 100, 50]);
    return { pages: [0, 2], scanned: 3, truncated: false };
  }
  assert.equal(method, 'main.Desktop.FontUsageAll');
  assert.deepEqual(args, [100, 50]);
  return { fonts: [{ scope: 2, id: 7, template_id: 3, pages: [0, 2] }], scanned: 3, truncated: false };
};
assert.deepEqual(await client.fontUsage(2, 7, { templateID: 3, maxScan: 100, maxPages: 50 }),
  { pages: [0, 2], scanned: 3, truncated: false });
assert.equal((await client.fontUsageAll({ maxScan: 100, maxPages: 50 })).fonts[0].template_id, 3);
await assert.rejects(client.fontUsage(-1, 7), /无效字体标识/);
globalThis.wails.Call.ByName = async (method, token) => {
  assert.equal(method, 'main.Desktop.Info');
  assert.equal(token, prefix);
  return { info: { docID: 'doc', title: '标题', author: '作者' }, fonts_error: '', security: { encrypted: false, state: 'none' },
    fonts: [{ id: 7, scope: 1, template_id: 4, name: '字体', family: '字体族', embedded: true, fixed_width: true }],
    permissions: [{ scope: 0, pages: [0], accessible: true, export: false }] };
};
const info = await client.info();
assert.equal(info.title, '标题');
assert.equal(info.author, '作者');
assert.equal(info.permissions[0].export, false);
assert.equal(info.fonts_error, '');
assert.equal(info.fonts[0].template_id, 4);
assert.equal(info.fonts[0].fixed_width, true);
assert.equal(info.fonts[0].embedded, true);
assert.equal(info.security.encrypted, false);
globalThis.wails.Call.ByName = async () => ({ info: {}, fonts: null, fonts_error: '读取字体失败', permissions: [] });
const partialInfo = await client.info();
assert.equal(partialInfo.fonts_error, '读取字体失败', '字体读取失败不能伪装成未声明字体');
assert.deepEqual(partialInfo.fonts, []);
globalThis.wails.Call.ByName = async (method, token, ...args) => {
  assert.equal(token, prefix);
  if (method.endsWith('.Permissions')) return [{ scope: 0, pages: [0], accessible: true, export: false, print_copies: -1 }];
  assert.deepEqual(args, ['copy', [0]]);
  return { denied: { message: '禁止复制', reason: 'forbidden', action: 'copy', scope: 0 } };
};
const permissions = await client.permissions();
assert.deepEqual(permissions[0].pages, [0]);
assert.equal(permissions[0].export, false);
assert.equal(permissions[0].print_copies, -1);
await assert.rejects(client.checkPermission('copy', [0]), error => error.code === 'permission_denied' && error.action === 'copy');
globalThis.wails.Call.ByName = async () => ({ denied: { message: '禁止复制', reason: 'forbidden', action: 'copy', scope: 0 } });
await assert.rejects(client.text(0), error => error.code === 'permission_denied' && error.reason === 'forbidden' && error.scope === 0);
let resolveCall;
let underlyingCancelled = false;
globalThis.wails.Call.ByName = () => {
  const promise = new Promise(resolve => { resolveCall = resolve; });
  promise.cancel = () => { underlyingCancelled = true; };
  return promise;
};
const textRequest = client.text(0);
await Promise.resolve();
textRequest.cancel();
await assert.rejects(textRequest, error => error.name === 'AbortError');
assert.equal(underlyingCancelled, true);
resolveCall([]);
underlyingCancelled = false;
const searchRequest = client.search('A');
await Promise.resolve();
searchRequest.cancel();
await assert.rejects(searchRequest, error => error.name === 'AbortError');
assert.equal(underlyingCancelled, true);
resolveCall({ results: [], scanned: 0, truncated: false });
underlyingCancelled = false;
const fontRequest = client.fontUsageAll({ maxScan: 1, maxPages: 1 });
await Promise.resolve();
fontRequest.cancel();
await assert.rejects(fontRequest, error => error.name === 'AbortError');
assert.equal(underlyingCancelled, true);
resolveCall({ fonts: [], scanned: 0, truncated: false });
console.log('原生页面请求和取消测试通过');
