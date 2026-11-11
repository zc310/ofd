// 与 WASM 接口使用相同的权限字段名。
export function mapPermissions(permissions) {
  return permissions || [];
}

// 资源和印章只接受原生端检查过的 PNG/JPEG；额外光栅格式由 Go 转为静态 PNG。
function decodeImagePreview(value) {
  if (!['image/png', 'image/jpeg'].includes(value?.mime) || typeof value.data !== 'string') throw new Error('图片预览格式无效');
  const binary = atob(value.data);
  if (!binary.length || binary.length > 8 * 1024 * 1024) throw new Error('图片预览大小无效');
  return { mime: value.mime, data: Uint8Array.from(binary, char => char.charCodeAt(0)) };
}

function decodeImageThumbnail(value) {
  const result = decodeImagePreview(value);
  if (result.mime !== 'image/png' || result.data.length > 1 << 20
    || ![value.width, value.height].every(Number.isSafeInteger) || value.width <= 0 || value.height <= 0
    || value.width > 25000000 / value.height) throw new Error('缩略图格式无效');
  return { ...result, width: value.width, height: value.height };
}

function decodePlaybackPreview(value) {
  if (!['audio/mpeg', 'audio/mp4', 'audio/wave', 'audio/wav', 'application/ogg', 'video/mp4', 'video/webm'].includes(value?.mime)
    || typeof value.data !== 'string' || value.data.length > 44739244) throw new Error('音视频预览格式无效');
  const binary = atob(value.data);
  if (!binary.length || binary.length > 32 * 1024 * 1024) throw new Error('音视频预览大小无效');
  return { mime: value.mime, data: Uint8Array.from(binary, char => char.charCodeAt(0)) };
}

// NativePageClient 与共用阅读器的 renderPage 约定一致：返回可取消的 ArrayBuffer 请求。
// 页面格式支持 PNG、JPG 和 SVG；页面资源通过宿主资源服务读取。
export class NativePageClient {
  constructor(prefix, fetcher = globalThis.fetch.bind(globalThis)) {
    if (!/^\/reader\/[0-9a-f]{64}\/pages\/$/.test(prefix)) throw new Error('无效页面资源地址');
    this.prefix = prefix;
    this.fetcher = fetcher;
  }

  pageURL(index, options = {}) {
    if (!Number.isSafeInteger(index) || index < 0) throw new Error('无效页码');
    const { dpi = 96, format = 'png', background = '#00000000', purpose } = options;
    if (!Number.isFinite(dpi) || dpi <= 0 || dpi > 600) throw new Error('无效 DPI');
    if (!['png', 'jpg', 'svg'].includes(format)) throw new Error('当前原生引擎仅支持 PNG、JPG 和 SVG');
    if (!/^#[0-9a-fA-F]{8}$/.test(background)) throw new Error('无效背景色');
    if (purpose !== undefined && !['export', 'print'].includes(purpose)) throw new Error('无效渲染用途');
    const query = new URLSearchParams({ dpi: String(dpi), format, background });
    if (purpose !== undefined) query.set('purpose', purpose);
    return `${this.prefix}${index}?${query}`;
  }

  renderPage(index, options = {}) {
    const controller = new AbortController();
    const cancelled = () => {
      const error = new Error('请求已取消');
      error.name = 'AbortError';
      return error;
    };
    const promise = Promise.resolve().then(async () => {
      const url = this.pageURL(index, options);
      if (controller.signal.aborted) throw cancelled();
      const response = await this.fetcher(url, { signal: controller.signal, cache: 'no-store' });
      if (controller.signal.aborted) throw cancelled();
      if (!response.ok) throw new Error(`页面渲染失败（${response.status}）`);
      const data = await response.arrayBuffer();
      if (controller.signal.aborted) throw cancelled();
      return data;
    });
    promise.cancel = () => controller.abort();
    return promise;
  }

  call(method, args, convert, validate = () => {}) {
    let pending;
    let rejectCancel;
    let cancelled = false;
    let settled = false;
    const promise = new Promise((resolve, reject) => {
      rejectCancel = reject;
      Promise.resolve().then(async () => {
        if (cancelled) return;
        validate();
        pending = globalThis.wails.Call.ByName(`main.Desktop.${method}`, this.prefix, ...args);
        const value = await pending;
        if (!cancelled) {
          const result = convert(value);
          settled = true;
          resolve(result);
        }
      }).catch(error => { if (!cancelled) { settled = true; reject(error); } });
    });
    promise.cancel = () => {
      if (cancelled || settled) return;
      cancelled = true;
      pending?.cancel?.();
      const error = new Error('请求已取消');
      error.name = 'AbortError';
      rejectCancel(error);
    };
    return promise;
  }

  request(method, index, convert) {
    return this.call(method, [index], convert, () => {
      if (!Number.isSafeInteger(index) || index < 0) throw new Error('无效页码');
    });
  }

  pageInfo(index) {
    return this.request('PageInfo', index, page => page);
  }

  text(index) {
    return this.request('Text', index, response => {
      if (response.denied) {
        const error = new Error(response.denied.message);
        Object.assign(error, response.denied, { code: 'permission_denied' });
        throw error;
      }
      return response.runs || [];
    });
  }

  permissions() {
    return this.call('Permissions', [], mapPermissions);
  }

  outline() {
    return this.call('Outline', [], tree => tree);
  }

  customTags() { return this.call('CustomTags', [], value => value); }
  versions() { return this.call('Versions', [], value => value); }

  selectVersion(scope, id) {
    const request = this.call('SelectVersion', [scope, id], value => value, () => {
      if (!Number.isSafeInteger(scope) || scope < 0 || scope > 1 << 20 || typeof id !== 'string') throw new Error('版本参数无效');
    });
    request.cancel = () => {};
    return request;
  }

  preferences() {
    return this.call('Preferences', [], value => value);
  }

  stats() {
    return this.call('Stats', [], value => value);
  }

  attachments() {
    return this.call('Attachments', [], value => value);
  }

  attachmentTextPreview(scope, id) {
    return this.call('AttachmentTextPreview', [scope, id], value => {
      if (typeof value !== 'string') throw new Error('文本预览格式无效');
      return value;
    }, () => {
      if (!Number.isSafeInteger(scope) || scope < 0 || typeof id !== 'string' || !id) throw new Error('无效附件标识');
    });
  }

  attachmentImagePreview(scope, id) {
    return this.call('AttachmentImagePreview', [scope, id], decodeImagePreview, () => {
      if (!Number.isSafeInteger(scope) || scope < 0 || typeof id !== 'string' || !id) throw new Error('无效附件标识');
    });
  }

  attachmentPlayback(scope, id) {
    return this.call('AttachmentPlayback', [scope, id], decodePlaybackPreview, () => {
      if (!Number.isSafeInteger(scope) || scope < 0 || typeof id !== 'string' || !id) throw new Error('无效附件标识');
    });
  }

  mediaPlayback(scope, id, templateID = 0) {
    return this.call('MediaPlayback', [scope, id, templateID], decodePlaybackPreview, () => {
      if (![scope, id, templateID].every(Number.isSafeInteger) || scope < 0 || id <= 0 || templateID < 0) throw new Error('无效媒体标识');
    });
  }

  openExternalLink(uri, scope = 0) {
    return this.call('OpenExternalLink', [scope, uri], value => value === true, () => {
      if (!Number.isSafeInteger(scope) || scope < 0 || typeof uri !== 'string' || !uri || uri.length > 4096) throw new Error('无效外部链接');
    });
  }

  saveAttachment(scope, id) {
    return this.call('SaveAttachment', [scope, id], value => value === true, () => {
      if (!Number.isSafeInteger(scope) || scope < 0 || typeof id !== 'string' || !id) throw new Error('无效附件标识');
    });
  }

  saveMedia(scope, id) {
    return this.call('SaveMedia', [scope, id], value => value === true, () => {
      if (![scope, id].every(Number.isSafeInteger) || scope < 0 || id < 0) throw new Error('无效资源标识');
    });
  }

  saveSignatureValue(scope, id) {
    return this.call('SaveSignatureValue', [scope, id], value => value === true, () => {
      if (!Number.isSafeInteger(scope) || scope < 0 || typeof id !== 'string' || !id.trim()) throw new Error('无效签名导出参数');
    });
  }

  saveSignatureCertificate(scope, id, slot, format) {
    return this.call('SaveSignatureCertificate', [scope, id, slot, format], value => value === true, () => {
      if (!Number.isSafeInteger(scope) || scope < 0 || typeof id !== 'string' || !id.trim()
        || !['seal', 'outer'].includes(slot) || !['der', 'pem'].includes(format)) throw new Error('无效证书导出参数');
    });
  }

  exportPages(indices, options) {
    const { format, dpi, background } = options;
    return this.call('ExportPages', [indices, format, dpi, background], value => value === true, () => {
      if (!Array.isArray(indices) || !indices.length || indices.length > 200 || new Set(indices).size !== indices.length
        || indices.some(index => !Number.isSafeInteger(index) || index < 0)
        || !['png', 'jpg', 'svg', 'pdf', 'txt'].includes(format)
        || !Number.isFinite(dpi) || dpi < 1 || dpi > 600 || !/^#[0-9a-f]{8}$/i.test(background)) throw new Error('无效页面导出参数');
    });
  }

  printPages(indices) {
    return this.call('PrintPages', [indices], value => value === true, () => {
      if (!Array.isArray(indices) || !indices.length || indices.length > 200 || new Set(indices).size !== indices.length
        || indices.some(index => !Number.isSafeInteger(index) || index < 0)) throw new Error('打印请选择 1-200 个不重复页面');
    });
  }

  media() {
    return this.call('Media', [], value => value);
  }

  mediaImagePreview(scope, id) {
    return this.call('MediaImagePreview', [scope, id], decodeImagePreview, () => {
      if (![scope, id].every(Number.isSafeInteger) || scope < 0 || id < 0) throw new Error('无效资源标识');
    });
  }

  mediaThumbnail(scope, id) {
    return this.call('MediaThumbnail', [scope, id], decodeImageThumbnail, () => {
      if (![scope, id].every(Number.isSafeInteger) || scope < 0 || id < 0) throw new Error('无效资源标识');
    });
  }

  signatureSealThumbnail(scope, id, index) {
    return this.call('SignatureSealThumbnail', [scope, id, index], decodeImageThumbnail, () => {
      if (![scope, index].every(Number.isSafeInteger) || scope < 0 || index < 0 || typeof id !== 'string' || !id.trim()) throw new Error('无效印章预览参数');
    });
  }

  signatureSealPreview(scope, id, stampIndex) {
    return this.call('SignatureSealPreview', [scope, id, stampIndex], decodeImagePreview, () => {
      if (![scope, stampIndex].every(Number.isSafeInteger) || scope < 0 || stampIndex < 0 || typeof id !== 'string' || !id.trim()) throw new Error('无效印章预览参数');
    });
  }

  annotations(index) {
    return this.request('AnnotationsForPage', index, value => value);
  }

  pageLinksForPage(index) {
    return this.request('PageLinksForPage', index, value => value);
  }

  layers(index) {
    return this.request('Layers', index, value => value);
  }

  signatureSummaries() {
    return this.call('SignatureSummaries', [], value => value);
  }

  signatureDetails() {
    return this.call('SignatureDetails', [], value => value);
  }

  setHiddenLayers(index, ids) {
    const request = this.call('SetHiddenLayers', [index, ids], () => true, () => {
      if (!Number.isSafeInteger(index) || index < 0) throw new Error('无效页码');
      if (!Array.isArray(ids) || ids.some(id => typeof id !== 'string')) throw new Error('图层 ID 必须为字符串数组');
    });
    // 状态写入不可在发送后撤销；必须等待真实结果，避免界面和阅读器状态分叉。
    request.cancel = () => {};
    return request;
  }

  annotationReport() {
    return this.call('Annotations', [], value => value);
  }

  fontUsage(scope, id, options = {}) {
    const { templateID = 0, maxScan = 10000, maxPages = 500 } = options;
    return this.call('FontUsage', [scope, id, templateID, maxScan, maxPages], value => value, () => {
      if (![scope, id, templateID].every(Number.isSafeInteger) || scope < 0 || id < 0 || templateID < 0) {
        throw new Error('无效字体标识');
      }
    });
  }

  fontUsageAll(options = {}) {
    const { maxScan = 10000, maxPages = 500 } = options;
    return this.call('FontUsageAll', [maxScan, maxPages], value => value);
  }

  info() {
    return this.call('Info', [], result => {
      const value = result.info;
      return { ...value, fonts_error: result.fonts_error,
        fonts: result.fonts || [],
        security: result.security, permissions: mapPermissions(result.permissions) };
    });
  }

  checkPermission(action, indices) {
    return this.call('CheckPermission', [action, indices], response => {
      if (response.denied) {
        const error = new Error(response.denied.message);
        Object.assign(error, response.denied, { code: 'permission_denied' });
        throw error;
      }
      return true;
    });
  }

  checkDocumentPermission(scope, action) {
    return this.call('CheckDocumentPermission', [scope, action], response => {
      if (response.denied) {
        const error = new Error(response.denied.message);
        Object.assign(error, response.denied, { code: 'permission_denied' });
        throw error;
      }
      return true;
    }, () => {
      if (!Number.isSafeInteger(scope) || scope < 0 || !['access', 'export'].includes(action)) throw new Error('无效文档权限参数');
    });
  }

  search(query) {
    return this.call('Search', [query], report => report, () => {
      if (typeof query !== 'string') throw new Error('搜索内容必须是字符串');
    });
  }
}
