# 文档目录

本目录提供 OFD Go Toolkit 的格式支持清单、库使用指南、构建与维护文档、标准资料和运行截图索引。首次使用可从[主 README 的快速开始](../README.md#快速开始)进入。

## 支持清单

各支持清单都区分「已支持」「部分支持」「暂不支持」，描述项目当前的实现范围，不代表完整的标准符合性认证。

| 文档                                   | 内容                                                                 |
|----------------------------------------|----------------------------------------------------------------------|
| [OFD-SUPPORT.md](OFD-SUPPORT.md)       | OFD（GB/T 33190-2016）各要素的解析、校验、创建、渲染和阅读支持范围   |
| [OFD-CRYPTO.md](OFD-CRYPTO.md)         | GM/T 0099-2020 加解密、普通签名、完整性保护、互通与格式约定          |
| [PDF-SUPPORT.md](PDF-SUPPORT.md)       | PDF（ISO 32000-1/-2）转 OFD 的支持范围（内容流、文字、图像、容错等） |
| [OFFICE-SUPPORT.md](OFFICE-SUPPORT.md) | Office 文档（doc/docx/odt/rtf/wps/pptx/xlsx 等）转 OFD/PDF 支持范围  |
| [HTML-SUPPORT.md](HTML-SUPPORT.md)     | HTML/MHTML 转 OFD/PDF 的支持范围与打印选项                           |

## 库使用指南

| 文档                                                                  | 内容                                                        |
|-----------------------------------------------------------------------|-------------------------------------------------------------|
| [Go API 使用指南](GO-API.md)                                          | 输入输出、格式导入、按页阅读、创建、合并、签章与字体说明。  |
| [转换 API 与示例](../pkg/converter/examples_test.go)                  | OFD 输出、格式导入、选页和 PDF 资源预算的可运行 Go 示例。   |
| [按页阅读 API](https://pkg.go.dev/github.com/zc310/ofd/pkg/webreader) | 页面渲染、文字、搜索、导航、版本与导出。                    |
| [创建器库指南](../pkg/creator/README.md)                              | Go 创建 API、文字定位、资源、页面组织、附加结构和流式创建。 |
| [签署与验证指南](../pkg/sign/README.md)                               | 外部命令签署、SES 印章与签章生成、完整验证与证据配置。      |
| [OFD 加解密](../pkg/crypt/README.md)                                  | Inspect/Encrypt/Decrypt API、资源预算、保护范围与错误处理。 |
| [完整性保护](../pkg/integrity/README.md)                              | 清单生成、时间戳、受信签名覆盖、引用闭包与防夹带。          |
| [主 README 的 Go API 示例](../README.md#go-api)                       | 依赖安装、OFD 转 PDF 入门示例与各包入口。                   |

## 开发与维护

支持清单用于查询能力和限制；跨文件实现说明、测试入口及依赖维护记录放在对应代码目录。

| 文档                                                 | 内容                                                             |
|------------------------------------------------------|------------------------------------------------------------------|
| [构建与打包指南](BUILDING.md)                        | 运行依赖、Make 目标、平台工具链、交叉编译、Android 与 Web 打包。 |
| [PDF 转换维护说明](../internal/pdf2ofd/README.md)    | 转换链路、不变量、回归入口、外部字体、独立阅读器验证与依赖补丁。 |
| [内嵌字体修复](../internal/fontrepair/README.md)     | OpenType/CFF 格式依据、字形映射、稀疏 CID 兼容和回归入口。       |
| [Adobe-GB1 映射数据](../internal/cidmap/README.md)   | 字体修复与 PDF 提取共用的数据来源、许可及生成方法。              |
| [WASM 构建与脚本测试](../cmd/ofd-wasm/web/README.md) | 浏览器 API、字体加载、缓存、构建和 Node 测试。                   |

## 标准资料

以下标准用于 OFD 格式、电子公文和电子签章相关功能的开发参考。链接指向国家标准全文公开系统或行业标准信息服务平台，不代表项目已通过完整标准符合性认证。

| 标准                                                                                                                                                                                                  | 内容                                                       |
|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|------------------------------------------------------------|
| [GB/T 33190-2016](https://openstd.samr.gov.cn/bzgk/gb/newGbInfo?hcno=3AF6682D939116B6F5EED53D01A9DB5D) · [修订计划](https://std.samr.gov.cn/gb/search/gbDetailed?id=529C162AC641CB8FE06397BE0A0A4AB4) | 电子文件存储与交换格式 版式文档；修订计划号 20262656-T-244 |
| [GM/T 0099-2020](https://std.samr.gov.cn/hb/search/stdHBDetailed?id=C60C676BC220358AE05397BE0A0AFABB)                                                                                                 | 开放式版式文档密码应用技术规范（签名、加密与完整性保护）   |
| [GB/T 42133-2022](https://openstd.samr.gov.cn/bzgk//std/newGbInfo?hcno=CF545613ABF25B011116F79DCCD3C45B)                                                                                              | 信息技术 OFD 档案应用指南                                  |
| [GB/T 33476.1-2016](https://openstd.samr.gov.cn/bzgk/gb/newGbInfo?hcno=419233A04D029066CF032CFACBC8713B)                                                                                              | 党政机关电子公文格式规范 第 1 部分：公文结构               |
| [GB/T 33476.2-2016](https://openstd.samr.gov.cn/bzgk/gb/newGbInfo?hcno=5B4E730245FF2FAF909435EBE6A659D2)                                                                                              | 党政机关电子公文格式规范 第 2 部分：显现                   |
| [GB/T 39362-2020](https://openstd.samr.gov.cn/bzgk/gb/newGbInfo?hcno=C6B230010286DC79C84CF8EB72DE3F97)                                                                                                | 党政机关电子公文归档规范                                   |
| [GB/T 38540-2020](https://openstd.samr.gov.cn/bzgk/gb/newGbInfo?hcno=EBF1360C272E40E7A8B9B32ED0724AB1)                                                                                                | 信息安全技术 安全电子签章密码技术规范                      |
| [GB/T 9704-2012](https://openstd.samr.gov.cn/bzgk//std/newGbInfo?hcno=F3CC9BEF482524C895FDA7A08BB4A70E)                                                                                               | 党政机关公文格式                                           |
| [GB/T 48666-2026](https://openstd.samr.gov.cn/bzgk//std/newGbInfo?hcno=CDD0F56BBBDDF7B69D01AC4DCFAB0C76)                                                                                              | 电子病历版式文档技术要求       |

## 截图

运行截图从 [GitHub Releases](https://github.com/zc310/ofd/releases/tag/v0.1.4) 下载：

- 桌面版阅读器：[Linux](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-viewer-linux.png)、[Windows](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-viewer-windows.png)、[Android](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-viewer-android.png)
- 浏览器版 WASM 阅读器：[webreader_1.png](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-wasm-webreader_1.png)、[webreader_2.png](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-wasm-webreader_2.png)、[webreader_3.png](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-wasm-webreader_3.png)
- 校验器报告：[Markdown](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-validator-markdown.png)、[PDF](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-validator-pdf.png)
- 分析器报告：[Markdown](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-analyzer-markdown.png)
- GNOME Nautilus 缩略图：[截图](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-thumbnailer-gnome.Nautilus.png)

## 相关链接

- 项目主 README：[../README.md](../README.md)
- 命令行工具与阅读器：[完整文档入口](../README.md#支持清单与工具文档)
- 已发布 API 的破坏性变更：[CHANGELOG.md](../CHANGELOG.md)，日常历史见 `git log`。
