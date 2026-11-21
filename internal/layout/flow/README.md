# 流式排版实现

公开入口为 [`pkg/creator/flow`](../../../pkg/creator/flow)，Markdown 和清单导入均通过该入口使用本包。
本包将顺序内容转换为 `creator.Document`，不负责 CLI、清单解析或 XML/ZIP 写出。

| 文件 | 职责 |
| --- | --- |
| `model.go`、`options.go` | 内容与排版配置；类型无需 `Flow` 前缀 |
| `compose.go`、`engine.go` | 多轮重排编排、页面游标及运行态 |
| `geometry.go` | 独立于主题和字体选项的页面几何 |
| `block.go`、`paragraph.go`、`segment.go` | 内容准备、UAX #14 分段与文字排版 |
| `pagination.go`、`column.go`、`balance.go`、`keep.go` | 分页、分栏与跟随约束 |
| `font.go`、`measure.go` | 嵌入字体度量与逻辑字体回退 |
| `table*.go`、`callout*.go`、`official.go` | 表格、提示框与公文 |
| `navigation.go`、`catalog.go`、`footnote.go` | 链接、目录与脚注 |

输入 `Block` 与准备后的 `preparedBlock`、输入 `Section` 与运行态 `sectionState`
分别命名，避免把排版状态当作公开创作模型。公开 API 的模型转换层仍保留在
`pkg/creator/flow`，不使用类型别名暴露内部状态。

旧 `internal/layout.Document` / `Build` 路径已移除；其仍适用的断行、链接及背景对齐约束
由现用入口的回归测试保护。测试及基准：

```sh
go test ./internal/layout/... ./pkg/creator/flow ./pkg/converter/import/markdown -count=1
go test ./internal/manifest ./cmd/ofd-creator ./pkg/creator -count=1
go test ./internal/layout/flow -run '^$' -bench BenchmarkComposeReport -benchmem
```
