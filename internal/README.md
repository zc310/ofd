# 内部包导航

`internal` 存放仓库内部共享的实现，对外入口位于 `pkg`，应用入口位于 `cmd`。
包按具体职责命名；跨模块共享的基础能力直接放在一级目录，某个模块的专用实现放在其子目录。

| 职责                     | 包                                                                  |
|--------------------------|---------------------------------------------------------------------|
| OFD 包访问、模型与解析   | `container`（ZIP 索引及逻辑包视图）、`models`、`parser`             |
| XML 读写与校验           | `fastxml`、`xmlcodec`、`schema`                                     |
| 渲染与图像、字体处理     | `render`（含绘制后端）、`media`、`coloricc`、`fontrepair`、`cidmap` |
| 文字提取、结构分析与输出 | `textdoc`、`textdoc/markdown`、`textdoc/docx`、`search`             |
| 清单导入导出与排版       | `manifest`、`manifest/export`、`layout`                             |
| 外部转换工具             | `convert/browser`、`convert/office`、`convert/xps`                  |
| PDF 导入与文字识别       | `pdf2ofd`、`ocr`                                                    |
| 加解密、签名与印章       | `crypt`、`ses`、`signeddata`、`physicalsig`、`securexml`、`sealimg` |
| ZIP 配置与受限包写出     | `container`（压缩策略与规模预算）、`container/entrywriter`          |
| 应用支持                 | `server`、`localization`、`version`                                 |
| 共享基础能力与测试辅助   | `cache`、`fileutil`、`reportfmt`、`contextio`、`testutil`           |

共享基础包的边界：

- `cache`：并发安全的有界 LRU，支持权重限制、淘汰回调和条目保护。
- `fileutil`：文件查找和路径身份判断。
- `reportfmt`：按输出路径扩展名推断报告格式。
- `contextio`：在读写边界检查上下文取消。
- `testutil`：跨包测试共用的辅助函数。

`manifest/export` 依赖 `manifest` 的清单模型，供 creator CLI 和模型级合并共同使用。
清单解析和资源加载保留在 `manifest` 包中。

`container` 统一维护 ZIP 压缩策略、规模限制和流式写入预算；`container/entrywriter`
在其上实现包内路径与重复条目检查、受限写入和确定性时间，供合并、替换及解密共用。
两者均不依赖 `pkg`。`pkg/creator` 的公开 ZIP 类型和辅助函数通过别名及转调复用这些实现。

`textdoc` 负责文字及图片提取和版面结构分析，`textdoc/docx`、`textdoc/markdown`
消费这些结果并编排输出。格式注册和转换流程位于 `pkg/converter`。

`convert` 下的子包负责调用外部工具生成 PDF；格式注册、输入适配以及衔接
`pdf2ofd` 的流程位于 `pkg/converter/import`，详见 [外部转换工具](convert/README.md)。

各模块的开发和验证约定见对应目录的 `AGENTS.md`。
