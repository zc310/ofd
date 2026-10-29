# OFD 拆分

`pkg/split` 按页范围、每 N 页或完整文档体拆分 OFD。保留原页面对象 ID、包内路径、
坐标和绘制内容；只改页面目录、导航及资源索引，未修改的文件保留原始字节。
CLI 见 [ofd-creator split](../../cmd/ofd-creator/README.md#拆分-ofd)。

```go
session, err := split.Open(ctx, "input.ofd", split.Options{Deterministic: true})
if err != nil {
    return err
}
defer session.Close()

parts, err := session.Plan(split.Selection{Every: 10})
if err != nil {
    return err
}
for _, part := range parts {
    var output bytes.Buffer
    report, err := session.Write(ctx, part, &output)
    if err != nil {
        return err // 丢弃部分 output
    }
    // 保存 output.Bytes()，并处理 report.Dropped。
    _ = report
}
```

完整可运行示例见 [examples_test.go](examples_test.go)。`Open` 和 `Write` 要求非 nil 的
`context.Context`；取消错误保留 `errors.Is(err, context.Canceled/DeadlineExceeded)`。
最后一次输出写入期间触发取消也返回失败，实际写入字节仍计入累计预算，不计入成功分卷数。
文件写入使用临时文件，成功关闭、校验后再发布；库本身只负责调用方提供的 writer。

## 输入和分组

- `Open` 接受文件路径、完整 `[]byte` 或 `Source`。`Source` 的 `Path`、`Data`、
  `Reader` 必须且只能设置一个；随机访问 `Reader` 同时提供正数 `Size`，调用方负责关闭。
- `Documents` 返回文档体序号、DocID、标题和页数。文档体序号、页码均从 **1** 开始。
- `Selection{Every: 10}` 按连续 10 页分卷，最后一份允许不足 10 页。
- `Selection{Groups: [][]int{{1, 2, 3}, {5, 4}}}` 支持不连续选页和重排。
  同一组不能重复页面，不同组可以重叠；空组和越界页码拒绝。
- `Selection{ByDocument: true}` 每个完整 `DocBody` 一份，不同时指定 `Document`。
- 页面模式的多文档输入必须显式设置 `Document`，避免隐式展平文档体。
- `ParseGroups("1-3;4-8;10", maxPages, maxParts)` 在展开前检查规模。
- `Plan` 在任何写入前检查全部组。直接调用 `Write` 时也会校验 `Part`。

## 内容处理

| 内容 | 页面分卷 | 完整文档体 |
|---|---|---|
| 页面、模板 | 保留所选页及递归引用的模板，页 ID 不变 | 按原始引用复制 |
| 资源 | 按 ID 递归保留字体、颜色空间、绘制参数继承、图片/蒙版/替代图、复合图元和媒体动作依赖，再裁剪文件 | 保留文档引用的完整资源索引和文件 |
| 注解 | 只保留所选页上的注解，清理无效导航 | 原样保留 |
| 导航 | 清理指向未选页、已删除书签和被移除附件的动作；清理空大纲 | 原样保留 |
| 元数据、封面、文档类型、权限 | 保留；生成新 DocID，记录来源 DocID 和页序 | 保留原 DocID |
| 附件 | 默认移除；`KeepAttachments` 保留整份附件集 | 保留 |
| 自定义标引、扩展、历史版本 | 移除并通过 `Report.Dropped` 报告 | 保留 |
| 数字签名及关联印章外观 | 移除并报告；显式 preserve 遇到签名时拒绝 | 默认检查能否原样保留；可设置 drop |

资源追踪依据 GB/T 33190—2016 §7.3–7.9、§8、§15、§18，覆盖图案、裁剪、
渐变色和嵌套图元中的资源引用。对象 ID 按 `unsignedInt` 数值比较；缺失、重复或
类型错误的资源、模板循环及过深引用均拒绝。文件闭包复用 validator，未知 XML、
缺失路径或无法完整解释的引用在写入前失败，不根据不完整闭包删除文件。

这是结构拆分，元数据和封面仍可能包含整份原文档的信息；保留的字体程序、媒体文件及
共享二进制数据整体复制，不做字体子集化或二进制内部清理。输出保留原 `DocType`，
声明 OFD-A/OFD-H 不代表分卷自动满足专用规范，应对结果执行相应严格校验。

## 签名、加密和完整性

完整文档体拆分保持原路径。若签名引用分卷外文件，或保护将改变的 `OFD.xml` 等文件，
preserve 在写入前报错；不会静默输出失效签名。`Report.Signatures` 只报告原字节保留，
不代表证书信任、吊销、时间戳或完整验签通过；可另用 `pkg/sign.Verify` 检查。
页面分卷需要在最终内容确定后重新签署，普通页面中的印章图片仍属于页面内容。
`Report.SourceID/DocID` 给出来源和输出标识，`DroppedSignatures` 列出被移除的签名路径。
页面分卷的 `DocInfo/CustomDatas` 还记录 `ofd.split.source_doc_id` 和
`ofd.split.source_pages`，便于保存后追溯来源。

加密输入返回 locked 错误，需要先通过 `pkg/crypt` 独立处理。发现 `OFDEntries.xml`
时默认拒绝；显式 `DropIntegrity: true` 才移除旧清单及其独立签名文件并报告。
签名仍保护该清单时，文档体 preserve 也会拒绝。需要保护输出时重新签署、生成清单。

## 规模、内存与错误

默认限制：压缩输入 512 MiB、条目数 10000、单条解压 64 MiB、解压总量 512 MiB、
分卷数 10000、每文档体/分卷 100000 页、累计压缩输出 2 GiB。
`Limits` 的零值采用默认值，负数拒绝。输出字节预算也累计失败写入的实际字节。

一个会话建立一次受限的解压索引，缓存原始 XML 的引用分析结果；资源通过条目写入器
流式复制。解压索引及 XML 缓存会占用内存，不能把压缩输入上限视为内存上限。
每个分卷使用独立 XML 副本；会话方法可并发调用，内部串行处理。`Close` 释放句柄和缓存。
会话存续期间输入文件、字节切片和随机访问数据源必须保持不变。
底层 I/O 错误保留错误链，输入/分组/依赖限制错误可用 `errors.Is(err, split.ErrInvalid)` 判断。

```text
go test ./pkg/split ./pkg/merge ./pkg/validator ./cmd/ofd-creator -count=1
```
