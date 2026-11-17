# 权限声明执行指南

`github.com/zc310/ofd/pkg/permission` 评估 OFD 权限声明和有效期。
它不是加密、DRM 或操作系统级访问控制，不能撤销已返回字节或可靠阻止截图，
也不能单靠库控制系统打印份数。高层入口见 [Go API 使用指南](../../docs/GO-API.md)。

## 直接检查声明

`Policy.Check` 直接接收 `*spec.Permissions`，不需要另建归一化模型：

```go
err := policy.Check(declaration, permission.Export, scope)
```

其中 `scope` 是文档体索引，保留在拒绝错误中。可运行示例见 [examples_test.go](examples_test.go)。

`Policy` 零值执行声明并使用当前时间；`Now` 可注入时钟，须并发安全且不得重入持有该策略的组件。
检查不修改声明或缓存授权结果，检查期间不得并发修改声明；长任务可用
`Permissions.Clone()` 固定原始声明。

### 操作与有效期

默认按 GB/T 33190—2016 §7.5 表8判断：

| 操作 | 检查 |
|---|---|
| `Access` | 有效期，不额外要求导出或打印权限。 |
| `Copy`、`Export` | `Export` 声明及有效期，不引入非标准的 Copy 字段。 |
| `Print` | `Print` 设置及有效期。 |
| `Edit/Annot/Signature/Watermark/PrintScreen` | 各自独立的同名声明及有效期。 |

nil 声明和省略的布尔字段默认允许。省略有效期不受时间约束；单边期限允许，
起止端点包含在有效期内。非 nil 但两端均为空的期限、倒置期限均拒绝。
无时区时间沿用模型的 UTC 解析约定，不按客户端本地时区重解释。

### 错误判断

`errors.Is(err, permission.ErrDenied)` 判断声明拒绝或有效期拒绝；
`errors.As` 可读取 `*permission.DeniedError` 的 `Action/Scope/Reason`。
稳定原因值为 `forbidden`、`not_yet_valid`、`expired`、`invalid_period`。
未知操作及非法策略分别使用 `ErrAction`、`ErrMode`。

## 配置业务入口

| 入口 | 配置 | 检查范围 |
|---|---|---|
| `reader.Open` | `OpenOptions.PermissionPolicy` | 打开时检查有效期；逐页操作按所属文档体检查，全量接口检查全部文档体。 |
| converter 的 OFD 导出 | `WithPermissionPolicy` | 所选页所属文档体的 `Export` 及有效期，不因图像、文字等格式改变语义。 |
| `replace.Files/Paths` | `Options.PermissionPolicy` | 全部原始文档体的 `Edit`，不猜测被改条目属于哪个文档体。 |
| `watermark.Add/Replace/Remove` | 内嵌 `replace.Options` 的 `PermissionPolicy` | 目标文档体的 `Watermark`，不额外要求 `Edit/Annot`。 |
| `sign.Sign` | `Options.PermissionPolicy` | 全部文档体的 `Signature`。 |
| `sign.Remove` | `Options.PermissionPolicy` | 全部文档体的 `Edit`。 |
| ZIP 级 `merge.Files/Bytes/Sources/Marshal` | `merge.Options.PermissionPolicy` | 全部输入文档体的 `Export`，不额外要求 `Edit`。 |
| 模型级 `merge.Pages` | `merge.PageOptions.PermissionPolicy` | 全部输入文档体的 `Export`，即使只选择部分页面。 |
| 清单导出 `Build/WriteBundle` | 导出 `Options.PermissionPolicy` | `Build` 检查目标文档体；`WriteBundle` 预检全部文档体。 |
| `split.Session.Write` | `split.Options.PermissionPolicy` | 当前分卷来源文档体的 `Export` 及有效期。 |

合并、移除签名、水印替换/删除等操作的上述权限归类是本库入口策略，
标准没有为这些加工方式另设同名权限。未列出的加工入口不能视为已经全面门控。
非 OFD 外部工具的直接转换不涉及 OFD 声明，第三方自定义编码器须自行执行策略。

Reader 也提供 `CheckPermission` / `CheckDocumentPermission`；
`reader.ErrPermissionDenied` 与 `permission.ErrDenied` 可用 `errors.Is` 等价识别。
`Permissions()`、`PageCount()`、`Close()` 保留诊断或释放能力。
快照中的 `Accessible/AccessReason` 只代表查询时刻，不是永久授权。

## 长任务与部分输出

权限以**原始输入**为准，不能靠本次替换后的声明给自己授权。有效期在处理检查点及
输出发布前复查；Reader 还会在内容访问、缓存返回及版本发布前复查。
批量输出先预检全部选页，再逐页检查。

- 转换器初始拒绝时不写输出，也不调用图像/文件 Writer。运行中到期可能已经输出部分字节。
- ZIP 合并保留同一批打开的输入直到结束，不在预检后重新打开路径；初始拒绝不输出字节或
  签名/警告回调。模型拼页预检全部来源后才触发用户签名事件回调，后续仍会复查。
- 清单导出在逐页、逐模板、AssetSink 调用及 manifest 写入的边界复查；
  批量导出使用临时目录，失败时不发布最终目录。诊断导出保留原始单边或空期限。
- 拆分的 `Open/Documents/Plan` 只提供结构与计划，不构成导出授权。
  `Write` 逐条目和底层 Writer 前后复查；失败字节仍计入预算，但不计入成功分卷数。
- 签署会在首次调用签名器前、每次签署前和输出前检查；已完成的外部签名器调用无法撤销。

任何运行中拒绝都要求调用方丢弃部分输出，已处理回调不会回滚。

## 显式授权的忽略策略

获明确授权的诊断或修复程序可使用：

```go
policy := permission.Policy{Mode: permission.IgnoreDeclarations}
```

它同时忽略操作声明和有效期，但不绕过未知操作或非法策略校验，
也不关闭路径限制、资源预算、解密或签名检查。不要根据不可信输入自动启用。
WASM 阅读器不提供该选项，拆分 CLI 不提供全部声明豁免。

水印的旧 `SkipPermissionsCheck` 只跳过水印布尔声明，仍检查有效期。
物理签署若不能读取加密的 DocRoot 声明，默认失败；授权 Go 流程可明确传入忽略策略，
物理签署本身不是自动豁免。

## 归档用途例外

`archive` 的检查、证据清点、`Prepare` 原包保存及 `Verify` 固定性验证，不执行
Edit/Export/ValidPeriod 声明；过期或禁止导出的原件仍可作为证据保存。
`Prepare` 保留原始 OFD 字节、权限和签名，附件副本由归档系统控制访问，不是公开下载授权。

`archive/preserve.Plan/Apply` 是主动发起的长期保存处理，也不要求原始 Edit/Export
或有效期有效。GB/T 42133—2022 §6.2.2 a)（标准第4页/PDF第8页）要求去除 Permissions，
因此 Apply 明确以归档用途重建条目，不让普通编辑门控阻止该处理。

这不是全局忽略开关。归档授权和目录访问控制仍由业务方负责；处理不自动解密、
不放宽资源预算或校验、不静默删除签名证据。preserve 保留签名条目并报告摘要可能失效，
输出新副本，原件应另行保存。从归档中进行普通阅读、提取或导出时，仍使用对应入口策略。
