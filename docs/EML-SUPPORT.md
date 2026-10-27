# EML 邮件转 OFD/PDF 支持清单

`pkg/converter/import/eml` 注册 `.eml` / `message/rfc822` 输入。先用 Go 解析邮件和
MIME 部分，生成便于阅读的 HTML，再通过 Chrome/Chromium 输出 PDF；OFD 目标继续
复用 PDF 导入器。无需 Outlook 或其他邮件客户端。

## 已支持

| 能力 | 范围 |
|---|---|
| 输入 | 文件路径、`[]byte`、`io.Reader`；路径可按 `.eml` 识别，其他输入显式指定 `eml`。 |
| 邮件头 | Subject、From、To、Cc、Bcc、Date、Reply-To、Message-ID；保留日期原文和时区。 |
| 编码 | RFC 2047 编码词、RFC 2231 文件名；Base64、Quoted-Printable、7bit/8bit/binary。 |
| 正文字符集 | UTF-8，以及由 MIME `charset` 声明的 GBK/GB18030、ISO-8859-1 等已有字符集。未声明时按 UTF-8，不猜测编码。 |
| MIME 正文选择 | `multipart/alternative` 优先 HTML，纯文本为备选；`related` 按 `start` 或首个正文选择根，`mixed` 依次保留正文。 |
| 正文排版 | 纯文本保留换行；HTML 保留常见文字、表格、链接和内联样式。页眉区域展示邮件头，正文自动分页。 |
| CID 图片 | 解析 `<img src="cid:...">`，转换为内嵌 data 图片，支持百分号编码的 CID。 |
| OFD 附件 | MIME 解码后的原始文件写入 OFD 附件区，保留显示名称、格式和媒体类型信息，可由阅读器列出并下载。支持同名及零字节附件。 |
| CID 原文件 | 内嵌图片在正文显示，原文件另存为隐藏附件，可通过附件 API 读取。 |
| 正文附件清单 | 显示文件名、媒体类型和解码后的字节数；附件文本不混入正文。 |
| 输出 | OFD 和浏览器直接输出的 PDF；纸张、方向、字体环境沿用浏览器链路。 |

## 范围与限制

- EML→OFD 原样保留附件文件，不自动把附件转换为 OFD 页面；嵌套邮件作为原文件附件保留。
  附件显示名可以重复，包内文件名由转换器独立生成。OFD 附件结构复用 GB/T 33190—2016
  §20.1/20.2；媒体类型保存在附件 Usage 中，Format 按原文件扩展名填写。
- 直接 EML→PDF 输出邮件画面和附件清单，不嵌入附件文件。`.msg`、MBOX、PST 不属于本导入器的输入。
- HTML 的文档级 CSS、脚本、表单、iframe 和其他交互内容会移除。仅保留正文内联样式，
  不保证复杂邮件模板与邮件客户端逐像素一致。CSS 背景中的 `cid:` 引用暂不展开。
- 默认只显示内嵌图片，不访问远程图片；`--allow-remote` 可允许正文中的 HTTP(S) 图片。
  本地文件和相对文件引用不读取，网络字体和外部样式表不加载。
- 缺少被引用的 CID 图片、损坏的 MIME/传输编码或未知字符集会报错。加密 MIME 邮件与
  S/MIME 封装需先解密或导出；不迁移原邮件签名验证状态。
- 邮件头和正文是重新排版的阅读视图。中文需要运行环境有中文字体；字体嵌入由浏览器
  PDF 和后续 PDF→OFD 链路完成，具体能力见 [PDF 支持清单](PDF-SUPPORT.md)。
- 原始输入最多 32MiB；MIME 最多 16 层、256 个部分，传输解码累计最多 64MiB，单部分
  字符集转换后最多 64MiB。HTML 嵌套最多 256 层。后续还受浏览器超时和 PDF 导入预算限制。
- 未安装 Chrome/Chromium 时明确报错。取消或超时会中止转换；CLI 文件输出失败保留已有目标。

## 用法

```bash
ofd-converter testdata/eml/project-update.eml /tmp/project-update.ofd
ofd-converter testdata/eml/project-update.eml /tmp/project-update.pdf
ofd-converter --paper A5 --landscape message.eml /tmp/message.ofd
ofd-converter --chrome /path/to/chrome --office-timeout 60 message.eml /tmp/message.ofd
```

容器环境按需增加 `--chrome-no-sandbox`；浏览器查找与运行依赖见
[HTML 转换说明](HTML-SUPPORT.md#运行依赖)。

Go 调用按需注册：

```go
import (
    "github.com/zc310/ofd/pkg/converter"
    _ "github.com/zc310/ofd/pkg/converter/import/eml"
)

err := converter.Convert(ctx, "eml", "ofd", input, output,
    converter.WithPaperSize("A4"),
    converter.WithChrome("/path/to/chrome"),
)
```

`ctx` 是 `context.Context`，`output` 是 `io.Writer`；浏览器可自动查找时省略 `WithChrome`。
`WithPDFImportOptions` 作用于 EML→OFD 的 PDF 阶段，直接 EML→PDF 不经过该阶段。
