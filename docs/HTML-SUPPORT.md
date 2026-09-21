# HTML/MHTML 转 OFD/PDF 支持清单

本文档描述 `pkg/converter/htmlimport` 与 `internal/browser` 对 HTML/MHTML
（`.html`、`.htm`、`.xhtml`、`.mhtml`、`.mht`）转换为 PDF 和 OFD 的支持范围。

实现方式：通过 `github.com/chromedp/chromedp` 驱动 **Chrome/Chromium** 的原生
打印引擎渲染并导出 PDF。转换链路：

- HTML/MHTML → PDF：Chrome `Page.printToPDF` 直接输出。
- HTML/MHTML → OFD：Chrome 先转 PDF，再复用 `internal/pdf2ofd`（见
  [`PDF-SUPPORT.md`](PDF-SUPPORT.md)）生成 OFD，**保留文字层**。

## 支持级别

| 图标 | 级别     | 含义                                                          |
|:------:|----------|---------------------------------------------------------------|
| ✅   | 已支持   | 已有对应实现，并有测试或实际样例依据。                        |
| ⚠️   | 部分支持 | 可以处理主要结构，但部分属性、组合场景或视觉效果有限制。      |
| ❌   | 暂不支持 | 当前没有实现；相关对象通常被忽略，或该对象/页面转换失败。     |

## 运行依赖

| 项目            | 说明                                                                                                                                                            |
|-----------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Chrome/Chromium | 必须已安装（Google Chrome、Chromium 或 headless-shell 均可）。查找顺序：`--chrome` → 环境变量 `OFD_CHROME` → `PATH`，其余平台常见安装目录由 chromedp 内置查找。 |
| 未安装行为      | 返回明确错误（`未找到 Chrome/Chromium…`），不会静默降级。                                                                                                       |
| 沙箱            | 容器或 root 环境可能因缺少 user namespace 启动失败，可用 `--chrome-no-sandbox` 关闭沙箱（默认关闭，存在安全风险）。                                             |

## 支持的输入格式

| 格式                      | 状态 | 说明                                                |
|---------------------------|:----------:|-----------------------------------------------------|
| `.html`、`.htm`、`.xhtml` | ✅ | 本地 HTML，含内联 CSS、`data:` 资源、本地相对资源。 |
| `.mhtml`、`.mht`          | ✅ | MIME multipart 归档，内嵌资源由浏览器解析。         |

## 支持的输出格式

| 目标 | 状态 | 说明                                                            |
|------|:----------:|-----------------------------------------------------------------|
| PDF  | ✅ | Chrome 直接输出；`converter.Convert(from, "pdf", …)`。          |
| OFD  | ✅ | 先转 PDF，再经 `pdf2ofd`；文字可搜索，版式取决于 PDF 转换结果。 |
| 其他 | ✅ | 先导入为 OFD 再走现有导出器（如 HTML→png）。                    |

## 纸张与打印选项

| 选项                    | 默认 | 说明                                                          |
|-------------------------|------|---------------------------------------------------------------|
| `--paper`               | `A4` | 支持 `A4`、`A3`、`A5`、`Letter`、`Legal`、`B5`、`16开`。      |
| `--landscape`           | 关闭 | 横向打印。                                                    |
| `--no-print-background` | 关闭 | 不打印背景颜色和图片；默认打印背景，避免网页底色丢失。        |
| `--allow-remote`        | 关闭 | 允许加载外部资源（`http`/`https` 等）。默认**禁止**外部资源。 |
| `--chrome-no-sandbox`   | 关闭 | 禁用 Chrome 沙箱。                                            |

库接口：`converter.WithPaperSize("A4")`、`WithPaperDimensions(w, h)`、
`WithLandscape(true)`、`WithPrintBackground(false)`、
`WithAllowRemoteResources(true)`、`WithChrome(path)`、`WithChromeNoSandbox(true)`。

## 转换行为

| 项目     | 说明                                                                                                               |
|----------|--------------------------------------------------------------------------------------------------------------------|
| 外部资源 | 默认阻断 `http/https/ftp/ws/wss` 及协议相对地址，仅允许本地文件与 `data:`，保证安全与确定性。                      |
| 等待加载 | 导航后等待 `document.readyState === 'complete'` 再打印。                                                           |
| 隔离     | 每次渲染使用独立临时目录与独立 Chrome 用户数据目录，互不干扰；临时目录默认为系统临时目录，可用 `--temp-dir` 指定。 |
| 超时     | 默认 120 秒（`--office-timeout` 可调），超时结束浏览器进程。                                                       |
| 确定性   | 输出 PDF 含时间戳与 Chrome 版本信息，不同版本渲染结果可能不同。                                                    |
| 并发     | 批量转换通过 `--external-workers`（默认 2）限制 Chrome/LibreOffice 并发。                                          |

## 已知限制

| 范围           | 说明                                                                |
|----------------|---------------------------------------------------------------------|
| 外部依赖       | 必须安装 Chrome/Chromium，无法在纯 Go 环境运行。                    |
| 版本差异       | 不同 Chrome 版本的排版与分页存在差异。                              |
| 脚本与交互内容 | 依赖运行时脚本渲染的内容可能因等待时机不同而不完整。                |
| 远程资源       | 默认不加载；开启 `--allow-remote` 后依赖网络且结果不确定。          |
| 安全           | 处理不可信 HTML 时建议配合 `--chrome-no-sandbox=false` 与沙箱环境。 |

## 使用方式

库：

```go
import (
    "github.com/zc310/ofd/pkg/converter"
    _ "github.com/zc310/ofd/pkg/converter/htmlimport"
)

converter.Convert("html", "pdf", "page.html", w)                          // 直接转 PDF
converter.Convert("mhtml", "ofd", "mail.mht", w, converter.WithPaperSize("A4"))
converter.Convert("", "pdf", "page.html", w)                              // 按扩展名识别
```

命令行：

```bash
ofd-converter page.html page.pdf
ofd-converter --format ofd page.html page.ofd
ofd-converter --paper A3 --landscape page.html page.pdf
ofd-converter mail.mht mail.pdf
ofd-converter --chrome-no-sandbox page.html page.pdf
```

## 验证方式

```bash
go test ./pkg/converter/htmlimport -count=1
go test ./internal/browser -count=1
go run ./cmd/ofd-validator --format text --mode strict output.ofd
```

集成测试在 `PATH` 中找不到 Chrome/Chromium 时会自动跳过；在禁止 user namespace
的容器中需要 `converter.WithChromeNoSandbox(true)`。
