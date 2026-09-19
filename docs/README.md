# 文档目录

本目录集中存放 OFD Go Toolkit 项目的格式支持文档、标准资料和运行截图。

## 支持清单

各支持清单都区分「已支持」「部分支持」「暂不支持」，描述项目当前的实现范围，不代表完整的标准符合性认证。

| 文档                                   | 内容                                                                 |
|----------------------------------------|----------------------------------------------------------------------|
| [OFD-SUPPORT.md](OFD-SUPPORT.md)       | OFD（GB/T 33190-2016）各要素的解析、校验、创建、渲染和阅读支持范围   |
| [PDF-SUPPORT.md](PDF-SUPPORT.md)       | PDF（ISO 32000-1/-2）转 OFD 的支持范围（内容流、文字、图像、容错等） |
| [OFFICE-SUPPORT.md](OFFICE-SUPPORT.md) | Office 文档（doc/docx/odt/rtf/wps/pptx/xlsx 等）转 OFD/PDF 支持范围  |
| [HTML-SUPPORT.md](HTML-SUPPORT.md)     | HTML/MHTML 转 OFD/PDF 的支持范围与打印选项                           |

## 标准资料

[standards/](standards/) 目录存放项目中使用的国家标准原始文本，供格式学习和开发参考：

- [GBT_33190-2016.pdf](standards/GBT_33190-2016.pdf) 与 [GBT_33190-2016_报批稿.pdf](standards/GBT_33190-2016_报批稿.pdf)：GB/T 33190-2016《电子文件存储与交换格式 版式文档》
- [GBT_42133-2022.pdf](standards/GBT_42133-2022.pdf)：GB/T 42133-2022《信息技术 OFD 档案应用指南》
- [GBT_9704-2012.pdf](standards/GBT_9704-2012.pdf)：GB/T 9704-2012《党政机关公文格式》

## 截图

[screenshots/](screenshots/) 目录按工具分类存放运行截图：

- [viewer/](screenshots/viewer/)：桌面版阅读器（[Linux](screenshots/viewer/linux.png)、[Windows](screenshots/viewer/windows.png)）和 [Android 阅读器](screenshots/viewer/android.png)
- [wasm/](screenshots/wasm/)：浏览器版 WASM 阅读器（[webreader_1.png](screenshots/wasm/webreader_1.png)、[webreader_2.png](screenshots/wasm/webreader_2.png)）
- [validator/](screenshots/validator/) 与 [analyzer/](screenshots/analyzer/)：校验器和分析器的报告示例（[validator/markdown.png](screenshots/validator/markdown.png)、[validator/pdf.png](screenshots/validator/pdf.png)、[analyzer/markdown.png](screenshots/analyzer/markdown.png)）

## 相关链接

- 项目主 README：[../README.md](../README.md)
- 命令行工具说明：[cmd/ofd-converter/README.md](../cmd/ofd-converter/README.md)、[cmd/ofd-validator/README.md](../cmd/ofd-validator/README.md)、[cmd/ofd-analyzer/README.md](../cmd/ofd-analyzer/README.md)、[cmd/ofd-archive/README.md](../cmd/ofd-archive/README.md)、[cmd/ofd-creator/README.md](../cmd/ofd-creator/README.md)、[cmd/ofd-viewer/README.md](../cmd/ofd-viewer/README.md)、[cmd/ofd-wasm/README.md](../cmd/ofd-wasm/README.md)、[cmd/ofd-thumbnailer/README.md](../cmd/ofd-thumbnailer/README.md)