# 文档目录

本目录集中存放 OFD Go Toolkit 项目的格式支持文档、标准官方链接索引和运行截图。

## 支持清单

各支持清单都区分「已支持」「部分支持」「暂不支持」，描述项目当前的实现范围，不代表完整的标准符合性认证。

| 文档                                   | 内容                                                                 |
|----------------------------------------|----------------------------------------------------------------------|
| [OFD-SUPPORT.md](OFD-SUPPORT.md)       | OFD（GB/T 33190-2016）各要素的解析、校验、创建、渲染和阅读支持范围   |
| [PDF-SUPPORT.md](PDF-SUPPORT.md)       | PDF（ISO 32000-1/-2）转 OFD 的支持范围（内容流、文字、图像、容错等） |
| [OFFICE-SUPPORT.md](OFFICE-SUPPORT.md) | Office 文档（doc/docx/odt/rtf/wps/pptx/xlsx 等）转 OFD/PDF 支持范围  |
| [HTML-SUPPORT.md](HTML-SUPPORT.md)     | HTML/MHTML 转 OFD/PDF 的支持范围与打印选项                           |

## 库使用指南

| 文档 | 内容 |
|------|------|
| [创建器库指南](../pkg/creator/README.md) | Go 创建 API、文字定位、资源、页面组织、附加结构和流式创建。 |

## 开发与维护

支持清单用于查询能力和限制；跨文件实现说明、测试入口及依赖维护记录放在对应代码目录。

| 文档 | 内容 |
|------|------|
| [PDF 转换维护说明](../internal/pdf2ofd/README.md) | 转换链路、不变量、回归入口、外部字体、独立阅读器验证与依赖补丁。 |

## 标准资料

以下标准用于 OFD 格式、电子公文和电子签章相关功能的开发参考。链接指向国家标准全文公开系统或行业标准信息服务平台，不代表项目已通过完整标准符合性认证。

| 标准 | 内容 |
|---|---|
| [GB/T 33190-2016](https://openstd.samr.gov.cn/bzgk/gb/newGbInfo?hcno=3AF6682D939116B6F5EED53D01A9DB5D) · [修订计划](https://std.samr.gov.cn/gb/search/gbDetailed?id=529C162AC641CB8FE06397BE0A0A4AB4) | 电子文件存储与交换格式 版式文档；修订计划号 20262656-T-244 |
| [GM/T 0099-2020](https://std.samr.gov.cn/hb/search/stdHBDetailed?id=C60C676BC220358AE05397BE0A0AFABB) | 开放式版式文档密码应用技术规范（签名、加密与完整性保护） |
| [GB/T 42133-2022](https://openstd.samr.gov.cn/bzgk//std/newGbInfo?hcno=CF545613ABF25B011116F79DCCD3C45B) | 信息技术 OFD 档案应用指南 |
| [GB/T 33476.1-2016](https://openstd.samr.gov.cn/bzgk/gb/newGbInfo?hcno=419233A04D029066CF032CFACBC8713B) | 党政机关电子公文格式规范 第 1 部分：公文结构 |
| [GB/T 33476.2-2016](https://openstd.samr.gov.cn/bzgk/gb/newGbInfo?hcno=5B4E730245FF2FAF909435EBE6A659D2) | 党政机关电子公文格式规范 第 2 部分：显现 |
| [GB/T 39362-2020](https://openstd.samr.gov.cn/bzgk/gb/newGbInfo?hcno=C6B230010286DC79C84CF8EB72DE3F97) | 党政机关电子公文归档规范 |
| [GB/T 38540-2020](https://openstd.samr.gov.cn/bzgk/gb/newGbInfo?hcno=EBF1360C272E40E7A8B9B32ED0724AB1) | 信息安全技术 安全电子签章密码技术规范 |
| [GB/T 9704-2012](https://openstd.samr.gov.cn/bzgk//std/newGbInfo?hcno=F3CC9BEF482524C895FDA7A08BB4A70E) | 党政机关公文格式 |
| GB/T 48666-2026（征求意见稿） | 电子病历版式文档技术要求（征求意见稿，尚未正式发布） |

## 截图

[screenshots/](screenshots/) 目录按工具分类存放运行截图：

- [viewer/](screenshots/viewer/)：桌面版阅读器（[Linux](screenshots/viewer/linux.png)、[Windows](screenshots/viewer/windows.png)）和 [Android 阅读器](screenshots/viewer/android.png)
- [wasm/](screenshots/wasm/)：浏览器版 WASM 阅读器（[webreader_1.png](screenshots/wasm/webreader_1.png)、[webreader_2.png](screenshots/wasm/webreader_2.png)）
- [validator/](screenshots/validator/) 与 [analyzer/](screenshots/analyzer/)：校验器和分析器的报告示例（[validator/markdown.png](screenshots/validator/markdown.png)、[validator/pdf.png](screenshots/validator/pdf.png)、[analyzer/markdown.png](screenshots/analyzer/markdown.png)）

## 相关链接

- 项目主 README：[../README.md](../README.md)
- 命令行工具说明：[cmd/ofd-converter/README.md](../cmd/ofd-converter/README.md)、[cmd/ofd-validator/README.md](../cmd/ofd-validator/README.md)、[cmd/ofd-analyzer/README.md](../cmd/ofd-analyzer/README.md)、[cmd/ofd-archive/README.md](../cmd/ofd-archive/README.md)、[cmd/ofd-creator/README.md](../cmd/ofd-creator/README.md)、[cmd/ofd-viewer/README.md](../cmd/ofd-viewer/README.md)、[cmd/ofd-wasm/README.md](../cmd/ofd-wasm/README.md)、[cmd/ofd-thumbnailer/README.md](../cmd/ofd-thumbnailer/README.md)
