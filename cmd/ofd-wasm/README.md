# OFD WASM

OFD WASM 将 OFD 文档解析和渲染能力提供给浏览器 JavaScript 使用。

使用阅读器处理不可信或敏感文档前，请阅读项目根目录的 [免责声明](../../DISCLAIMER.md)。在线示例不替代生产环境的文件安全、权限控制和隐私保护措施。

浏览器阅读器示例和可直接部署的 Web 页面位于 [`web`](web) 目录。

阅读器的“更多 → 图层”以页面为父节点、正文图层和背景/前景模板为子节点展示树形列表。点击小三角展开，点击页名定位，复选框控制该页显示，并同步文字、链接、缩略图及导出/打印结果。
设置保留在当前阅读会话中；重新打开文档或成功切换版本后恢复全部显示。

支持浏览 GM/T 0099-2020 加密 OFD：打开时输入口令、选择 UTF-8 口令文件，或选择 SM2 PKCS#8 私钥文件，
可填写接收者名称。解锁后可阅读和搜索，复制、打印和导出遵守当前版本的文档权限。口令错误可重试或取消；
最近文档保存原始加密文件，再次打开需重新解锁。API 参数见 [加密文档](web/README.md#加密文档)。
WASM 默认使用兼容模式读取加密元数据，支持 ofdrw 的数字操作 ID；源文件缺少加密方案时，
按用户提供的口令或 SM2 私钥选择方案，保留源文件中已声明的方案。
口令文件按原始 UTF-8 内容读取，保留空格、BOM 和换行，最大 64 KiB。

支持读取 `Permissions` 并按文档体限制打印、复制和内容导出；`Export=false` 禁止复制文字及导出，
`Print Printable=false` 或 `Copies=0` 禁止打印。默认允许未声明的操作，缺省或负打印份数表示不限。
多文档体按所选页面检查，切换版本后重新读取权限。API 和浏览器限制见 [文档权限](web/README.md#文档权限)。
“文档信息”面板使用“基本信息 / 安全信息 / 字体”三个 Tab，内容区独立滚动。
安全信息展示原文件加密与解锁状态，以及当前版本各文档体的操作权限、打印份数和有效期声明；多文档体可折叠查看。

## 截图

![OFD WASM 网页阅读器](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-wasm-reader_1.png)

![OFD WASM 网页阅读器双页模式](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-wasm-reader_2.png)

![OFD WASM 网页阅读器资源列表](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-wasm-reader_3.png)
## 快速开始

在仓库根目录执行：

```bash
make build-wasm
python3 -m http.server 8080 --directory cmd/ofd-wasm/web
```

然后打开 <http://localhost:8080>，选择一个 `.ofd` 文件即可开始阅读。

浏览器不能通过 `file://` 直接加载 WASM。完整的功能、API、构建、部署和协议说明见 [`web/README.md`](web/README.md)。
