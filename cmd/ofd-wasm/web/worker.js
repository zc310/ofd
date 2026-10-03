/* 在浏览器主线程之外运行 Go WASM 引擎。 */
importScripts('wasm_exec.js');

let readyResolve;
let readyReject;
const ready = new Promise((resolve, reject) => {
  readyResolve = resolve;
  readyReject = reject;
});
const pendingTasks = [];
const queuedRequests = new Set();
const activeRequests = new Set();
const cancelledRequests = new Set();
let running = false;
// goInstance 保留 Go 运行实例，用于读取 WebAssembly 线性内存的真实大小。
// wasm_exec.js 在 run() 里把实例挂到 _inst，exports.mem 是 WebAssembly.Memory。
let goInstance = null;

// wasmLinearBytes 返回 WebAssembly 线性内存当前保留的字节数。Go 运行时只会
// 增长线性内存、不会缩小，所以该值单调不减，反映浏览器实际占用的地址空间；
// 与 Go MemStats 的存活堆配合可以看出 GC 后仍保留的空闲空间。
function wasmLinearBytes() {
  try {
    const mem = goInstance?._inst?.exports?.mem;
    return mem?.buffer ? mem.buffer.byteLength : 0;
  } catch (_) {
    return 0;
  }
}

function errorText(error) {
  return error instanceof Error ? error.message : String(error);
}

self.addEventListener('error', event => {
  console.error('[OFD Worker] JavaScript 异常', event.error || event.message);
});

self.addEventListener('unhandledrejection', event => {
  console.error('[OFD Worker] 未处理的 Promise 异常', event.reason);
});

function unwrap(value) {
  if (value && value.error) throw new Error(value.error);
  return value;
}

function reportStartFailure(error) {
  readyReject(error);
  self.postMessage({ type: 'fatal', error: errorText(error) });
}

async function start() {
  try {
    const go = new Go();
    goInstance = go;
    const response = await fetch('ofd.wasm');
    if (!response.ok) {
      reportStartFailure(new Error(`加载 ofd.wasm 失败: ${response.status}`));
      return;
    }

    let result;
    try {
      result = await WebAssembly.instantiateStreaming(response.clone(), go.importObject);
    } catch (_) {
      result = await WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
    }
    go.run(result.instance).catch(error => readyReject(error));

    const waitForAPI = () => {
      if (self.ofd) {
        readyResolve();
        self.postMessage({ type: 'ready' });
        return;
      }
      setTimeout(waitForAPI, 0);
    };
    waitForAPI();
  } catch (error) {
    reportStartFailure(error);
  }
}

async function execute(message) {
  await ready;
  switch (message.command) {
    case 'open': {
      const result = unwrap(self.ofd.open(message.data, message.options || {}));
      return { pageCount: result.pageCount, pages: result.pages, fonts: result.fonts || [] };
    }
    case 'addFallbackFont':
      return unwrap(self.ofd.addFallbackFont(
        message.data,
        message.family,
        message.weight ?? 400,
        !!message.italic,
      ));
    case 'removeFallbackFont':
      return unwrap(self.ofd.removeFallbackFont(message.family));
    case 'close':
      return unwrap(self.ofd.close());
    case 'memStats': {
      const stats = unwrap(self.ofd.memStats());
      stats.linearBytes = wasmLinearBytes();
      return stats;
    }
    case 'gc':
      return unwrap(self.ofd.gc());
    case 'info':
      return unwrap(self.ofd.info());
    case 'outline':
      return unwrap(self.ofd.outline());
    case 'preferences':
      return unwrap(self.ofd.preferences());
    case 'fontUsage':
      return unwrap(self.ofd.fontUsage(message.scope, message.fontID, message.maxScan, message.maxPages));
    case 'fontUsageAll':
      return unwrap(self.ofd.fontUsageAll(message.maxScan, message.maxPages));
    case 'versions':
      return unwrap(self.ofd.versions());
    case 'attachments':
      return unwrap(self.ofd.attachments());
    case 'attachmentData': {
      const result = unwrap(self.ofd.attachmentData(message.scope, message.attachmentID, message.maxBytes));
      const data = new Uint8Array(result);
      return data.slice().buffer;
    }
    case 'media':
      return unwrap(self.ofd.media());
    case 'mediaData': {
      const result = unwrap(self.ofd.mediaData(message.scope, message.mediaID, message.maxBytes));
      const data = new Uint8Array(result);
      return data.slice().buffer;
    }
    case 'annotations':
      return unwrap(self.ofd.annotations());
    case 'pageLinks':
      return unwrap(self.ofd.pageLinks());
    case 'pageMediaActions':
      return unwrap(self.ofd.pageMediaActions());
    case 'signatures':
      return unwrap(self.ofd.signatures());
    case 'signatureCertificate': {
      const result = unwrap(self.ofd.signatureCertificate(message.scope, message.signatureID, message.slot));
      const data = new Uint8Array(result);
      return data.slice().buffer;
    }
    case 'signatureValue': {
      const result = unwrap(self.ofd.signatureValue(message.scope, message.signatureID));
      const data = new Uint8Array(result);
      return data.slice().buffer;
    }
    case 'stats':
      return unwrap(self.ofd.stats());
    case 'signatureSeal': {
      const result = unwrap(self.ofd.signatureSeal(message.scope, message.signatureID, message.stampIndex));
      const data = new Uint8Array(result);
      return data.slice().buffer;
    }
    case 'pageInfo':
      return unwrap(self.ofd.pageInfo(message.index));
    case 'renderPage': {
      const result = unwrap(self.ofd.renderPage(message.index, message.options || {}));
      const data = new Uint8Array(result);
      return data.slice().buffer;
    }
    case 'renderPages': {
      const result = unwrap(self.ofd.renderPages(message.indices || [], message.options || {}));
      return Array.from(result, page => new Uint8Array(page).slice().buffer);
    }
    case 'renderStream': {
      if ((message.indices || []).length === 1) {
        const format = String(message.options?.format || 'png').toLowerCase();
        if (format !== 'pdf') {
          const result = unwrap(self.ofd.renderStream(message.indices, message.options || {}));
          const data = new Uint8Array(result);
          return data.slice().buffer;
        }
      }
      const emit = (value, sequence) => {
        const data = new Uint8Array(value);
        const buffer = data.slice().buffer;
        self.postMessage({ type: 'stream-chunk', id: message.id, sequence, value: buffer }, [buffer]);
      };
      return unwrap(self.ofd.renderStream(message.indices || [], message.options || {}, emit, message.id));
    }
    case 'text':
      return unwrap(self.ofd.text(message.index));
    case 'search':
      return unwrap(self.ofd.search(message.query || ''));
    default:
      throw new Error(`未知 Worker 操作: ${message.command}`);
  }
}

function isCancelled(id) {
  if (!cancelledRequests.has(id)) return false;
  cancelledRequests.delete(id);
  return true;
}

async function drain() {
  if (running) return;
  running = true;
  while (pendingTasks.length) {
    const message = pendingTasks.shift();
    queuedRequests.delete(message.id);
    if (isCancelled(message.id)) {
      reply(message.id, false, null, '请求已取消');
      continue;
    }
    activeRequests.add(message.id);
    try {
      const value = await execute(message);
      activeRequests.delete(message.id);
      if (isCancelled(message.id)) reply(message.id, false, null, '请求已取消');
      else reply(message.id, true, value);
    } catch (error) {
      activeRequests.delete(message.id);
      if (isCancelled(message.id)) reply(message.id, false, null, '请求已取消');
      else reply(message.id, false, null, errorText(error));
    }
    // 让取消消息有机会在继续执行下一个同步 WASM 任务前被处理。
    await new Promise(resolve => setTimeout(resolve, 0));
  }
  running = false;
}

function reply(id, ok, value, error) {
  if (ok && value instanceof ArrayBuffer) {
    self.postMessage({ id, ok: true, value }, [value]);
    return;
  }
  if (ok && Array.isArray(value)) {
    const transfer = value.filter(item => item instanceof ArrayBuffer);
    self.postMessage({ id, ok: true, value }, transfer);
    return;
  }
  self.postMessage(ok ? { id, ok: true, value } : { id, ok: false, error });
}

self.onmessage = event => {
  const message = event.data || {};
  if (message.command === 'cancel') {
    if (queuedRequests.has(message.target) || activeRequests.has(message.target)) {
      cancelledRequests.add(message.target);
      if (activeRequests.has(message.target) && self.ofd?.cancelStream) {
        self.ofd.cancelStream(message.target);
      }
    }
    return;
  }
  if (message.command === 'streamAck') {
    if (self.ofd?.streamAck) {
      self.ofd.streamAck(message.target, message.sequence, message.error || null);
    }
    return;
  }
  if (!message.id) return;
  pendingTasks.push(message);
  queuedRequests.add(message.id);
  drain();
};

start();
