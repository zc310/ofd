// Command ofd-wasm 将 OFD 文档引擎编译为 WebAssembly，供浏览器阅读器解析、渲染、
// 搜索和导出 OFD 文档。它在单个 Web Worker 内运行一个 WASM 实例管理当前文档，
// 通过 ofd.open、ofd.renderPage 等接口暴露给页面脚本，用法见 README。
package main
