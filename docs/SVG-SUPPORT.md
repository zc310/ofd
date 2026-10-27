# SVG 转 OFD/PDF 支持清单

`pkg/converter/import/svg` 注册 `.svg` / `image/svg+xml` 输入。将自包含 SVG 作为
独立图片文档嵌入 HTML，由 Chrome/Chromium 等比放入所选纸张；PDF 目标直接输出
浏览器 PDF，OFD 目标复用 PDF 导入器。

## 已支持

| 能力 | 范围 |
|---|---|
| 输入 | 文件路径、`[]byte`、`io.Reader`；支持 XML 声明和 UTF-8 BOM。 |
| SVG 根节点 | 单个 `svg` 根节点，使用 `http://www.w3.org/2000/svg` 命名空间；转换前校验 XML。 |
| 静态画面 | 浏览器支持的路径、图形、变换、裁剪、文字、渐变、标记和内嵌图片。 |
| 页面 | 默认 A4 纵向，整幅画面等比适配单页，可指定横向、纸张或自定义尺寸。 |
| 矢量与文字 | 普通路径和文字保留为 PDF/OFD 图元；回归样本验证路径、渐变和中英文文字，未整页栅格化。 |
| 输出 | OFD 与 PDF；现有 OFD→SVG 导出功能继续使用独立输出编码器。 |

## 范围与限制

- 输入需自包含，图片和字体等资源应内联。相对文件、外部图片、外部 CSS/Web 字体不在
  本导入范围；`--allow-remote` 不会使 SVG 图片上下文加载这些资源。
- SVG 在浏览器图片上下文中渲染，不执行 SVG 脚本，不保留链接和其他交互。
  动画不保留时间轴，复杂滤镜、蒙版和效果可能由浏览器局部栅格化。
- 页面采用用户选择的纸张，不自动把 SVG 的像素尺寸当作 OFD 页面毫米尺寸。宽图通常
  适合 `--landscape`；Go 调用方可用 `WithPaperDimensions(widthMM, heightMM)` 指定尺寸。
  浏览器输出纸张可能存在像素取整带来的微小尺寸差异。
- 中文与未内嵌字体依赖转换机器的可用字体；缺字体时浏览器会替换。需要固定字形时，
  在 SVG 中内嵌字体或将文字转为路径。
- 原始输入最多 16MiB，XML 最多 128 层。损坏 XML、错误根节点或缺少命名空间返回错误，
  后续受浏览器超时和 PDF 导入预算限制。
- SVG→OFD 的保真度受浏览器及 [PDF 导入支持范围](PDF-SUPPORT.md)共同影响。
  未安装 Chrome/Chromium 时明确报错；CLI 转换失败保留已有目标。

## 用法

```bash
ofd-converter --landscape testdata/svg/workflow.svg /tmp/workflow.ofd
ofd-converter --landscape testdata/svg/workflow.svg /tmp/workflow.pdf
ofd-converter --paper A3 --landscape drawing.svg /tmp/drawing.ofd
```

容器环境按需增加 `--chrome-no-sandbox`。`--chrome`、`--office-timeout` 和
`--temp-dir` 沿用 HTML 转换配置，浏览器查找见 [运行依赖](HTML-SUPPORT.md#运行依赖)。

Go 调用按需注册：

```go
import (
    "github.com/zc310/ofd/pkg/converter"
    _ "github.com/zc310/ofd/pkg/converter/import/svg"
)

err := converter.Convert(ctx, "svg", "ofd", input, output,
    converter.WithLandscape(true),
)
```

`ctx` 是 `context.Context`，`output` 是 `io.Writer`；转换为 PDF 将目标改为 `"pdf"`。
`WithPDFImportOptions` 作用于 SVG→OFD 的 PDF 阶段，直接 SVG→PDF 不经过该阶段。
