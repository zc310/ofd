# 标准声明模型指南

`github.com/zc310/ofd/pkg/spec` 提供 OFD 及已支持密码应用规范的标准声明、
基础值类型和固定常量。它不负责文档加载、资源解析、渲染、权限执行或密码学验证。
高层操作入口见 [Go API 使用指南](../../docs/GO-API.md)。

## 解码 XML

```go
var document spec.Document
err := xml.Unmarshal(data, &document)
```

使用前导入 `encoding/xml` 和 `github.com/zc310/ofd/pkg/spec`。完整可运行示例见
[examples_test.go](examples_test.go)，全部类型见 [Go 文档](https://pkg.go.dev/github.com/zc310/ofd/pkg/spec)。

模型以声明和 XML 解码为主，**不承诺 `xml.Marshal` 能写出完整标准文档**。
创建 OFD 使用 [`pkg/creator`](../creator/README.md)，加密与完整性元数据分别由
`crypt.Encrypt`、`integrity.Protect` 生成。基础值已有的 XML 编码能力不代表整文档写出能力。

普通 XML 解码不是严格标准校验或安全读取；节点预算、重复元素检查、路径约束、
文件加载及信任验证仍由调用链负责。

## 类型索引

| 分类 | 主要类型 |
|---|---|
| 常量与包内路径 | `Namespace`、`RootDocument`、`EncryptionEntryPoint`、`IntegrityEntryPoint`、`SigTypeSeal/SigTypeSign`、`StLoc` |
| 标识、几何与数组 | `StID`、`StRefID`、`StPos`、`StBox`、`CTM`、`StArray[T]`、`IdentityCTM()` |
| 主入口与元数据 | `OFD`、`DocBody`、`DocInfo`、`Keywords`、`CustomDatas/CustomData` |
| 文档、页面与版本 | `Document`、`CommonData`、`PageArea`、`PageList/PageRef`、`TemplatePage`、`ZOrder`、`Versions/Version`、`DocVersion`、`FileList/VersionFile` |
| 权限与阅读偏好 | `Permissions`、`PrintSettings`、`ValidPeriod`、`DateTime`、`OptionalBool`、`VPreferences` 及偏好枚举 |
| 动作与导航 | `Action/ActionList`、各 `Action*` 类型、`Dest`、`Region/RegionArea/RegionPath`、`OutlineList/OutlineElem`、`BookmarkList/Bookmark/BookmarkRef` |
| 资源 | `Res`、`ColorSpaces/ColorSpace/Palette`、`Fonts/Font`、`MultiMedias/MultiMedia`、`DrawParams/DrawParam`、`CompositeGraphicUnits/CompositeGraphicUnit` |
| 页面图元 | `PageContent`、`Content/Layer`、`CTPageBlock/PageItem/PageBlock`、文字/路径/图像/复合对象、`CTGraphicUnit`、裁剪与文字定位类型 |
| 颜色与路径 | `ColorValue`、`PathData/PathCommand/ArcData`、`CTColor`、`CtPattern/CellContent`、轴向/径向/网格渐变及渐变段 |
| 附件、标引与注释 | `Attachment/Attachments`、`CustomTag/CustomTags`、`AnnotPage/Annotations`、`PageAnnot/Annot`、`AnnotType`、`Params/Parameter`、`Appearance` |
| 签名 | `Signatures/SignatureRef`、`Signature/SignedInfo`、`Provider`、`References/Reference`、`StampAnnot/Seal`、签名扩展参数 |
| 扩展 | `Extensions/Extension`、`ExtensionProperty`、`ExtensionData` |
| 加密与完整性 | `Encryptions/EncryptInfo`、`EncryptionProvider`、`EncryptionParameters/EncryptionParameter`、`DecryptSeed/EncryptionUser`、`EncryptedEntries/EncryptEntry`、`IntegrityManifest/IntegrityFile` |

## 基础值与编码

### 标识、几何、数组与路径

- `StID/StRefID/StPos/StBox` 按 GB/T 33190—2016 §7.3 表2声明，`CTM` 按 §8.5
  保存六个仿射变换分量。`IdentityCTM()` 每次返回独立的单位矩阵值。
- 几何和数值数组的 XML 元素、属性编码为空格分隔的有限数值；`CTM.String()` 仅用于诊断。
  JSON 中坐标和矩形仍为对象，矩阵和数组仍为数组，不改成字符串。
- 兼容解码保留非数字或溢出 ID 的确定性 CRC64 映射（不保证无碰撞）、逗号分隔点坐标、
  零/负矩形宽高及整数数组忽略非法 token 的行为。浮点数组支持文字定位的 `g` 重复记法，
  XML 编码时展开；重复次数和元素总数上限属于资源保护，不是标准限制。
- `StLoc` 使用包内斜杠路径。解码保留原文，`Resolve/Clean/Join` 仅做词法处理，
  它们以及 `In/HasPrefix` 均不是安全校验，不能代替 ZIP 访问边界检查。

`PathData` 对应 §9.1 表36的 OFD 紧缩路径，不是 SVG 路径：`B` 是三次贝塞尔曲线，
`C` 是闭合。`ParsePathData` 和 XML 解码兼容 `S` 归一化为 `M`、隐式直线、逗号分隔、
SVG `Z/z` 闭合及负弧半径取绝对值；这些行为不表示通过严格校验。

路径 XML 编码保留 float64 精度，拒绝非有限数值和缺少参数的命令；`String()` 只有两位
小数的诊断精度，不能用于保真写回。`CalculateBoundingBox` 只统计显式控制点和弧终点，
不是曲线的精确可见范围。JSON 保持命令数组。

### 颜色通道

`ColorValue` 对应 §8.3.2 表26/27，支持十进制和 `#` 十六进制。XML 编解码保留原始
通道文本，不把 16 位或 CMYK 分量压缩为 RGBA。`Components()` 返回前四个整数分量及
原始总数；分量数不匹配、负值和越界值沿用兼容解码，实际解释由颜色空间和位深决定。

嵌入的 `RGBA` 与 `String()` 仅保留既有 8 位回退表示，JSON 仍为 R/G/B/A 对象。
XML 优先使用解码时的原文；修改通道时应重新解码新文本，或构造新的
`ColorValue{RGBA: ...}`，不要只修改已解码对象的 RGBA 字段。

### 可选状态、日期与权限声明

`OptionalBool` 区分属性省略与显式 false，可用 `NewOptionalBool(false)` 构造后者。
权限模型的可选布尔元素使用 `*bool`，不能把 nil 与 false 混为一谈。

`Permissions` 保留布尔字段和有效期的存在状态。非 nil 的空 `ValidPeriod` 不是省略，
不能删除它来改变声明。`PrintSettings` 的 XML 解码应用 `Printable=true, Copies=-1`
缺省值；直接构造时应显式填写，零值表示禁止打印。

`DateTime{Time: t}` 可构造日期，解码保留既有 UTC 缺省、时区和纳秒精度。
`Permissions.Clone()` 深拷贝可变字段并保留 nil，不缓存授权结果。
业务检查使用 [`pkg/permission`](../permission/README.md)，检查期间不得并发修改共享声明。

## 文档、页面与资源

主入口按 §7.4 表3/4、版本按 §19 表70/71保留字段及顺序；版本加载由读取链处理。
`Document` 按 §7.5 表5/6保存原始声明，`PageRef` 是 §7.6 表11的页树引用，
不是页面内容或加载后的页面对象。文档根命名空间见 `XMLName.Space`。

`PageArea` 按表7保留原始区域，不应用 A4 兜底或页面/模板/文档区域继承。
`PageContent` 按 §7.7 表12—16保存区域、模板、资源和图层；混合图元位于
`CTPageBlock.Items`，不得按对象种类重新分组后绘制。底纹和复合资源复用同一有序页面块。
`MaxPageBlockDepth` 是解码资源上限，超限可用 `errors.Is(err, spec.ErrPageBlockDepth)` 判断，
不是标准对文档嵌套层数的限制。

### 动作与阅读偏好

动作按 §14 表51—59及附录A保存原始事件、目标和可选参数，不触发外部操作，
不将页面标识提前换成页码。大纲按 §7.8 表17、书签按 §7.5 表10保留原始顺序。
`ActionGotoA.NewWindow` 为 `*bool`；nil 表示省略，显式 false 保留。
附录A规定的缺省值 true 由执行方解释。

`VPreferences` 按表9保留缺省状态，从同级 `ZoomMode` 和 `Zoom` 元素解码
`ZoomSetting` 分组，JSON 仍保留 `Zoom` 对象。

### 资源及文字

资源集合按 §7.9 表18/19保留声明顺序，所有路径保持词法原文。
`ColorSpace` 按 §8.3.1 表25保存位深、配置文件路径和调色板，不加载配置文件或执行转换。
`Palette.CV` 每行对应一个从 0 开始的索引；XML 每个 `CV` 保存为一行，JSON 为二维字符串数组。
分量保留原文；空行和负值等兼容解码不代表通过校验。

`Font` 按 §11.1 表44保存声明：没有 `FontFile` 时是逻辑字体，不自动匹配或加载；
省略 `Charset` 保持空字符串，四个字型标志沿用 bool 表示，显式 false 与省略等价。
`MultiMedia` 不推断格式或读取媒体内容。

`DrawParam` 保留 `Relative`，不加载父资源。`HasLineWidth/HasDashOffset/HasMiterLimit`
区分缺省和显式零；直接构造需保留零覆盖时使用对应 `Set*` 方法。
`Override` 只合并明确给出的属性；渐变段用 `PositionSet` 区分省略与显式 0。

文字解码保留 OFD 转义及首尾 XML 排版换行兼容，可用 `DecodeTextCodeValue` 独立解码。
字符到字形声明不做 `CodePosition` 展开或字体解析，这些兼容和加载策略由读取链负责。

## 附件、注释与扩展

- 附件按 §20 表72/73保存原始路径，不读取内容；缺省日期保持零值，
  `Visible` 用 `OptionalBool` 保留省略状态。
- 自定义标引按 §16 表63及附录A保存入口；`NameSpace` 对应附录A模式，
  `TypeID` 保留正文表格写法。`Annotations` 按 §15.1 表60保存页引用和分页注释路径，
  不包含注释外观。
- 分页注释按 §15 表61/62保存原始 ID、参数、备注和外观。
  `Visible/Print/ReadOnly` 区分省略和显式 false，由使用方解释缺省值 true。
  `Print/NoZoom/NoRotate/ReadOnly` 针对 `Remark`，不控制 `Appearance` 的静态呈现。
  外观复用有序页面块及其嵌套上限，不提前变换 `Boundary`。
- `Annot.ID` 为字符串；`LastModDate` 沿用 `DateTime` 解析，但标准表61声明的是
  xs:date，解码任意日期时间值不代表通过严格校验。

扩展声明按 §17 表64/65保存属性、引用标识及路径，不加载外部扩展文件。
`Extension.Data == nil` 表示省略，非 nil 空值表示存在空 `Data`。
`InnerXML` 保留原始内部 XML；`Text` 只包含直接字符数据（含 CDATA），不包含后代文本；
`Attrs` 保存该元素的属性和局部命名空间声明。前缀可能依赖祖先上下文，不能把片段直接当独立文档。

扩展解码只读取直接子元素，未知子树整体跳过。`Date` 沿用 `*time.Time` 与 RFC3339，
不支持的日期文本忽略；非数字 `RefId` 沿用标识映射。这是兼容行为，不是严格校验。
清单导出从原文规范化内联命名空间并保留首尾空白；内联同名节点不参与外层索引。
创建时使用 creator 的 `Data/DataXML/DataFile`，不要将 `InnerXML` 与 `Text` 重复拼接。

## 签名、加密与完整性声明

签名索引按 §18.1 表66使用 `Signatures/SignatureRef`，索引 `ID/Relative` 保持字符串，
不使用数字 `StID`。`Signature` 对应 §18.2 表67—69，不含索引类型或加载路径；
`SignedValue` 与文件引用保留原始路径。`Reference.CheckValue` 保存 Base64 文本字节，
不是已解码的二进制摘要。

`SignatureParameters`、`Provider.ProtocolVer/ExtendData`、`Seal.ImageLoc` 及索引
`Relative` 保留既有 GM/T 0099 扩展；描述中的可选 `ID` 兼容生产者重复标识。
这些声明不加载文件、不计算摘要、不验证签名，不能用普通 XML 解码代替安全验证。

GM/T 0099—2020 加密声明对应附录 C.2—C.4 表C.1—C.3（纸面16—20页），完整性清单对应
D.2 图D.1/表D.1（纸面21—22页）。`Encryptions.Operations` 保留 XML 顺序，
不按 `Relative` 重排或验证依赖。Go 名称 `DecryptSeed` 对应标准 XML 根名 `DecyptSeed`；
映射表保留 `EncryptEntries/EncryptedEntries` 的实际根名，不额外接受图C.2的 `EncryptList` 别名。
提供者和密钥描述的扩展参数只保存原始声明，不意味着执行器支持其语义。

标识、日期和路径保持字符串原文，证书、包装密钥及 IV 保持 Base64 文本，
不自动解码、验证证书、补角色或零 IV。`EncryptionUser.IV` 的 nil、空字符串与给定文本
分别保留，`ExtendParams` 用 `*ExtensionData` 保留存在状态和原始内容。
这些是**未脱敏的安全元数据**，不能替代 `crypt.Info` 作为公开检查报告。

`IntegrityManifest` 不加载文件、不验签；`integrity.Manifest/File` 以别名复用声明并保留
已发布名称和 JSON 字段。`integrity.Inspect` 报告会规范化路径，与原始声明的词法路径不同。
安全读取、路径检查、解密和验证仍使用 `crypt` / `integrity` 的公开入口。
