# Go API 使用指南

安装依赖与完整的 OFD 转 PDF 入门示例见[主 README](../README.md#go-api)。可运行的
转换示例位于 [pkg/converter/examples_test.go](../pkg/converter/examples_test.go)。

## 标准数据模型

`pkg/spec` 提供标准常量和可公开使用的底层XML模型，已迁移：

- 权限与日期：`Permissions`、`PrintSettings`、`ValidPeriod`、`DateTime`。
- 主入口与元数据：`OFD`、`DocBody`、`DocInfo`、`Keywords`、`CustomDatas/CustomData`。
- 版本声明：`Versions/Version`、`DocVersion`、`FileList/VersionFile`。
- 包内路径：`StLoc` 及其词法路径操作。
- 标识与几何：`StID`、`StRefID`、`StPos`、`StBox`、`CTM`、`StArray[T]`，
  以及返回独立单位矩阵的 `IdentityCTM()`。
- 附件声明：`Attachment/Attachments`、可保留属性省略状态的 `OptionalBool`。
- 自定义标引与注释索引：`CustomTag/CustomTags`、`AnnotPage/Annotations`。
- 文档主体与页树：`Document`、`CommonData`、`PageArea`、`PageList/PageRef`、`TemplatePage`、`ZOrder`。
- 动作与导航：`Action/ActionList`、各 `Action*` 选择项、`Dest`、`Region/RegionArea/RegionPath`、
  `OutlineList/OutlineElem`、`BookmarkList/Bookmark/BookmarkRef`。
- 视图首选项：`VPreferences`、`PageMode`、`PageLayout`、`TabDisplay`、`ZoomSetting`。
- 简单资源声明：`ColorSpaces/ColorSpace/Palette`、`Fonts/Font`、`MultiMedias/MultiMedia`。
- 图形基础：颜色通道 `ColorValue`、紧缩路径 `PathData/PathCommand/ArcData` 和 `ParsePathData`。
- 页面与图元：`PageContent`、`Content/Layer`、`CTPageBlock/PageItem/PageBlock`、
  `TextObject/PathObject/ImageObject/CompositeObject`、`CTGraphicUnit`、裁剪及文字定位声明。
- 复杂颜色：`CTColor`、`CtPattern/CellContent`、`CTAxialShd/CTRadialShd`、
  `CTGouraudShd/CTLaGouraudShd` 及渐变段、网格点声明。
- 资源总表：`Res`、`DrawParams/DrawParam`、`CompositeGraphicUnits/CompositeGraphicUnit`。
- 分页注释：`PageAnnot/Annot`、`AnnotType`、`Params/Parameter`、`Appearance`。
- 签名声明：`Signatures/SignatureRef`、`Signature/SignedInfo`、`Provider`、
  `References/Reference`、`StampAnnot/Seal` 及签名扩展参数。
- 扩展声明：`Extensions/Extension`、`ExtensionProperty`、`ExtensionData`。
- GM/T 0099 加密声明：`Encryptions/EncryptInfo`、`EncryptionProvider`、
  `EncryptionParameters/EncryptionParameter`、`DecryptSeed/EncryptionUser`、
  `EncryptedEntries/EncryptEntry`。
- GM/T 0099 完整性声明：`IntegrityManifest/IntegrityFile`。

公开标准声明以以上范围为准；解析器、资源加载状态及渲染缓存不属于本包。
以上类型可以直接使用 `encoding/xml` 解码，不需要导入内部模型。主入口按§7.4表3/4、
版本按§19表70/71保留字段和列表顺序，版本实际加载仍由解析器处理。
`StLoc` 按§7.3表2使用包内斜杠路径，不依赖操作系统文件系统；`Resolve/Clean/Join`
只做词法处理，XML解码不会提前规范化原始路径。它们和 `In/HasPrefix` 都不是安全校验，
打开ZIP条目时仍须由容器检查穿越、预算及访问边界。
可运行的主入口示例见 [pkg/spec/examples_test.go](../pkg/spec/examples_test.go)。

结构模型以声明和 XML 解码为主，不提供另一套整文档写出接口。文档、附件及其索引、
自定义标引、视图首选项、调色板等不实现自定义 `MarshalXML`；其字段标签或基础值编码
不代表 `xml.Marshal` 能生成完整标准文档。实际文档创建使用 `pkg/creator`，
相关回归以 creator 产物解码为准，不为模型往返另加结构编码器。

标识、坐标和矩形按§7.3表2声明，`CTM` 按§8.5保存六个仿射变换分量。
几何类型和数值数组支持 XML 元素、属性编解码，编码为空格分隔的有限数值；
`CTM.String()` 仅用于诊断，不是 XML 格式。XML 编码不会把这些类型的 JSON
表示改成字符串：坐标和矩形仍为对象，矩阵和数组仍为数组。

解析保留既有非规范文档兼容行为，不代表通过标准校验：非数字或溢出的 ID 使用
确定性的 CRC64 映射（并不保证无碰撞），点坐标兼容逗号分隔，矩形保留零/负宽高，
整数数组忽略非法 token。浮点数组支持文字定位的 `g` 重复记法，解码后的数值在
编码时展开输出；重复次数及总元素数上限用于资源保护，不是 OFD 标准限制。
页面尺寸的 A4 兜底、资源加载和快速 XML 解析仍由内部实现负责。

`PathData` 对应§9.1表36的 OFD 紧缩路径，不是 SVG 路径：`B` 是三次贝塞尔曲线，
`C` 是闭合。`ParsePathData` 和 XML 解码沿用 `S` 归一化为 `M`、隐式直线、逗号分隔、
SVG `Z/z` 闭合和负弧半径取绝对值的兼容行为，不表示已通过标准校验。
XML 编码保留 float64 精度，拒绝非有限数值和缺少参数的命令；`String()` 仅返回两位小数
诊断文本，不能用于保真写回。`CalculateBoundingBox` 只统计显式控制点和弧终点，
不是曲线的精确可见范围。JSON 仍使用命令数组，不改为路径字符串。

`ColorValue` 对应§8.3.2表26/27的颜色通道值，支持十进制和 `#` 十六进制，
XML 元素或属性往返保留原始通道文本，不把16位或 CMYK 分量压缩为 RGBA。
`Components()` 返回前四个整数分量及原始总数；分量数不匹配、负值和越界值沿用兼容解码，
实际颜色由使用方结合颜色空间、位深解释。嵌入的 `RGBA` 与 `String()` 仅保留既有8位回退表示，
JSON 仍使用 R/G/B/A 对象。XML 优先使用解码时的原文；需修改通道值时，应重新解码新文本，
或直接构造新的 `ColorValue{RGBA: ...}`，不应只修改解码后对象的 RGBA 字段。

`Document` 按§7.5表5/6保存原始声明，`PageRef` 是§7.6表11的页树引用，
并非页面内容或加载后的页面对象。`PageArea` 按表7保留原始区域，不应用 A4 兜底或
页面/模板/文档区域继承；模板按§7.7表13保存。解码后的根命名空间在
`XMLName.Space` 中，不依赖原文使用默认命名空间还是前缀。
文档及视图首选项的可运行示例见同一示例文件。

动作按§14表51—59及附录A保存原始事件、目标和可选参数，不触发外部链接或媒体播放，
也不将目标页面标识提前换算为页码。大纲按§7.8表17、书签按§7.5表10保存原始顺序。
`ActionGotoA.NewWindow` 为 `*bool`，nil 表示省略，显式 false 会在 XML 和清单导出中
保留；附录A规定的缺省值 true 由执行方解释，模型不提前补值。
`VPreferences` 按表9保留缺省状态，XML 解码从同级的 `ZoomMode` 和 `Zoom` 元素
读取 `ZoomSetting` 分组，JSON 中仍保留 `Zoom` 对象。

资源集合按§7.9表18/19保留声明顺序。`ColorSpace` 按§8.3.1表25保存位深、颜色配置文件路径
和调色板，不加载配置文件或执行颜色转换；`Palette.CV` 的每行对应一个从0开始的调色板索引，
XML 解码将每个空白分隔的 `CV` 元素保存为一行，JSON 仍是二维字符串数组。分量保留原始词法值，
支持表27规定的 `#` 十六进制；空行、负值等沿用既有兼容解码，校验和按位深解释由使用方负责。
`Font` 按§11.1表44保存字体声明，没有 `FontFile` 时是逻辑字体，不自动匹配或加载字体；
省略的 `Charset` 保持空字符串，四个字型标志沿用 bool 表示，显式 false 与省略等价。
`MultiMedia` 不推断格式或读取媒体内容。所有资源路径保持词法原文，不在解码时归一化。

`PageContent` 按§7.7表12—16保存区域、模板、资源及图层；混合图元统一放在
`CTPageBlock.Items`，不可按对象种类重新分组后绘制。图元、裁剪、文字和复杂颜色声明
对应§8.3—8.5、§9—12；底纹与复合资源继续使用同一有序页面块。
本批页面、复杂图元和资源总表仅提供声明与 XML 解码，不承诺通过 `xml.Marshal`
生成完整标准文档；实际创建使用 `pkg/creator`，不为迁移另加一套编码逻辑。

`DrawParam` 保留 `Relative`，不在解码时加载父资源。`HasLineWidth/HasDashOffset/HasMiterLimit`
区分缺省与显式零值；直接构造需保留零值覆盖时使用对应的 `Set*` 方法。
`Override` 只合并明确给出的属性。渐变段用 `PositionSet` 区分缺省位置与显式0。
文字对象解码沿用 OFD 转义及首尾 XML 排版换行兼容；独立读取 `TextCode` 文本可用
`DecodeTextCodeValue`。字符到字形的声明不做 CodePosition 展开或字体资源解析，
这些兼容修正和加载策略仍由内部读取链处理。
页面块解码使用 `MaxPageBlockDepth` 资源上限，超限错误可用 `errors.Is(err, spec.ErrPageBlockDepth)`
识别；这不是标准对文档嵌套层数的限制。

附件按§20表72/73保存声明及原始包内路径，不读取附件内容；缺省日期保持零值，
`Visible` 使用 `OptionalBool` 区分省略与显式 false。通过 `NewOptionalBool(false)`
可构造显式隐藏声明；附件文档写出仍使用 `pkg/creator`，示例见同一示例文件。
`OptionalBool` 用于可选 XML 属性，权限模型的可选布尔元素仍使用 `*bool`。
自定义标引按§16表63及附录A保存入口，`NameSpace` 对应附录A模式、`TypeID`
保留正文表格写法；注释索引按§15.1表60保存页引用及分页文件路径，不包含注释外观。

分页注释按§15表61/62保存原始标识、参数、备注及外观；`Visible/Print/ReadOnly`
使用 `OptionalBool` 区分省略与显式 false，由使用方按规范解释缺省值 true。
`Print/NoZoom/NoRotate/ReadOnly` 针对 `Remark`，不控制 `Appearance` 的静态呈现。
`Appearance` 复用有序页面块及其嵌套上限，`Boundary` 不参与提前变换。
`Annot.ID` 沿用字符串表示，不在迁移中改写标识；`LastModDate` 沿用 `DateTime`
解析，标准表61声明的是 xs:date，不代表任意日期时间值都通过严格校验。

签名索引按§18.1表66使用 `Signatures/SignatureRef`；索引中的 `ID/Relative`
保留字符串，不使用数字 `StID`。`Signature` 是§18.2表67—69的描述文件，
不含索引类型、关联签名或加载路径，`SignedValue` 和所有文件引用保留原始路径。
`Reference.CheckValue` 沿用读取链约定，保存 Base64 文本字节而非已解码的二进制摘要。
`SignatureParameters`、`Provider.ProtocolVer/ExtendData`、`Seal.ImageLoc`
和索引 `Relative` 保留现有 GM/T 0099 扩展声明；描述中的可选 `ID` 仅兼容生产者重复标识。
这些类型只提供声明与 XML 解码，不执行文件加载、摘要计算或密码学验证；
签名的安全 XML 读取、标识兼容及验证策略仍在内部链路中，不能用普通 `xml.Unmarshal`
代替安全验证。分页注释和签名与页面图元一样，不承诺 `xml.Marshal` 可生成完整标准文档，
创建产物仍使用 `pkg/creator`。

扩展信息按§17表64/65保存应用属性、引用标识和扩展数据路径，不加载外部扩展文件。
`Extension.Data` 使用 `*ExtensionData`：nil 表示省略，非 nil 空值表示存在空 `Data`。
`InnerXML` 保存原始内部 XML，`Text` 只包含该元素直接的字符数据（含 CDATA），不包含
后代元素文本；`Attrs` 保存 `Data` 自身的属性及局部命名空间声明。原始 XML 中的前缀
可能依赖祖先命名空间，不能脱离原上下文直接当作独立 XML 文档使用；本模型不解释厂商
数据，也不提供完整扩展文档编码器。扩展解码只读取直接子元素，未知子树整体跳过。
`Date` 沿用 `*time.Time` 和 RFC3339 解析，不支持的日期文本继续忽略，非数字 `RefId`
沿用基础标识映射；这些是读取兼容行为，不代表通过严格标准校验。
清单导出继续从原文规范化内联 XML 的元素命名空间，并保留内容首尾空白；
内联 XML 中的同名 `Extension/Data` 不参与外层扩展索引计数或数据提取。
实际写出仍使用 `pkg/creator` 的 `Data/DataXML/DataFile`，不要将 `InnerXML` 与 `Text`
同时拼接写回，以免重复内容。

GM/T 0099—2020 的加密 XML 声明对应附录 C.2—C.4 表C.1—C.3（纸面16—20页），
完整性清单对应 D.2 图D.1/表D.1（纸面21—22页）。`Encryptions.Operations`
保留 XML 顺序，不按 `Relative` 重排或验证操作依赖。`DecryptSeed` 的 Go 名称采用
正确拼写，XML 根名仍为标准中的 `DecyptSeed`；映射表保留标准图表中
`EncryptEntries/EncryptedEntries` 的实际根名。入口不额外支持图C.2的 `EncryptList` 别名。
`Provider` 的 `ProtocolVer/ExtendData`、操作 `Parameters` 及密钥描述 `ExtendParams`
仅保留原始声明，不代表执行器支持这些扩展参数的语义。

这些声明的标识、日期和路径保留字符串原文；证书、包装密钥及 IV 保留 Base64 文本，
不自动解码、校验证书、填充默认角色或零 IV。`EncryptionUser.IV` 的 nil、空字符串和
给定文本分别保留，`ExtendParams` 用 `*ExtensionData` 保留存在状态和原始子内容。
它们包含未脱敏的原始安全元数据，不应代替 `crypt.Info` 用作公开检查报告。
`IntegrityManifest` 不加载文件、不验签；`integrity.Manifest/File` 以别名复用声明并
保留已发布 Go 名称和 JSON 字段，`integrity.Inspect` 的报告仍规范化路径，不能和原始
声明的词法路径混同。普通 `xml.Unmarshal` 不能替代 `crypt`/`integrity` 的受限安全读取、
路径检查、解密和完整性验证；也不因解码成功而证明文档受信任。
本批不新增 `MarshalXML` 或改变既有写入链：文档创建仍使用 `pkg/creator`，
密码应用元数据仍由已有 `crypt.Encrypt`、`integrity.Protect` 写端生成。

`spec.Permissions` 保留布尔字段的省略/显式取值及有效期是否存在，可直接使用
`encoding/xml` 解码。`nil`权限或空的Permissions对象默认允许，非nil的空ValidPeriod
仍是非法期限，不能删掉该节点来改变原声明。`spec.PrintSettings` 的XML解码会应用
`Printable=true, Copies=-1`缺省值；直接构造打印设置时应显式填写，零值表示禁止打印。
`spec.DateTime{Time: t}` 可构造日期；XML解析保留既有UTC缺省、时区和纳秒精度。
标准声明不等于业务授权，执行策略仍由 `pkg/permission` 负责。

`Permissions.Clone()` 深拷贝所有可变字段且保留nil，用于长任务固定原始声明，
不缓存授权结果。共享声明在检查期间不得并发修改。

## 转换与渲染

### 阅读器权限策略

`pkg/permission` 提供共用操作类型、`Policy.Check` 和 `DeniedError`。默认执行
GB/T 33190—2016 §7.5 表8；`Copy` 使用 `Export` 声明，`Edit/Annot/Signature/Watermark`
独立判断。`Policy.Check` 直接接收 `*spec.Permissions`，不需要另外构造归一化声明：
`policy.Check(declaration, permission.Export, scope)`；nil声明及省略的布尔字段默认允许。
`Policy` 零值执行声明并使用当前时间；`Now` 可注入测试时钟，须并发安全。
可运行示例见 [pkg/permission/examples_test.go](../pkg/permission/examples_test.go)。

`reader.Open(input, reader.OpenOptions{PermissionPolicy: policy})` 在创建Reader前检查有效期。
reader在内容访问、缓存返回及目标版本发布前重查有效期；逐页操作检查所属文档体，全量接口
检查全部文档体。批量输出先预检全部选页，再逐页复查，到期失败后已有部分输出应丢弃。
起止时间包含端点；空或倒置期限拒绝。无时区时间沿用模型解析的UTC约定，不按客户端本地时区重解释。

`CheckPermission`/`CheckDocumentPermission` 支持 `PermissionAccess/Copy/Export/Print/Edit/Annot/Signature/Watermark/PrintScreen`。
普通Access不受导出/打印声明限制。`errors.Is(err, reader.ErrPermissionDenied)` 与
`errors.Is(err, permission.ErrDenied)` 等价；`errors.As` 可取得 `*permission.DeniedError`
中的 `Action/Scope/Reason`，原因包括 `forbidden/not_yet_valid/expired/invalid_period`。
`Permissions()`、`PageCount()`、`Close()` 保留诊断和释放能力；快照 `Accessible/AccessReason`
只代表调用时刻，不是永久授权。

获明确授权的诊断/修复程序可设置 `permission.Policy{Mode: permission.IgnoreDeclarations}`，
它同时忽略有效期和操作声明，不绕过未知操作或非法策略校验。WASM接口不提供该选项。
权限声明不是加密/DRM，无法撤销已返回字节或可靠阻止系统截图，也不能单靠库控制系统打印份数。
本共用层已接入reader/WASM、条目替换、水印、签署/签名移除，以及converter的OFD导出。
ZIP级合并、拆分、模型级拼页与清单导出也已接入；归档按下述独立用途处理，其它未列出的加工入口不能视为全面门控。

业务入口按表8的独立操作判断：`replace.Files/Paths` 检查所有原始文档体的 `Edit`；任意条目
可能涉及共享资源，因此不猜测修改路径的归属。`watermark.Add/Replace/Remove` 只检查目标
文档体的 `Watermark`，不额外要求 `Edit/Annot`；旧 `SkipPermissionsCheck` 仅跳过水印布尔声明，
仍检查有效期。`sign.Sign` 在首次调用签名器前检查全部文档体的 `Signature`，每次签署前及
输出前复查；`sign.Remove` 属于编辑操作，检查全部文档体的 `Edit`。移除签名归入Edit、
水印替换/删除沿用Watermark均为本库入口策略，表8未另设删除权限。

上述入口的 `Options.PermissionPolicy`（水印为内嵌replace.Options）可设置共用策略。
权限以原始输入为准，不能靠本次替换后的声明给自己授权。有效期在缓冲结果发布前复查；
外部签名器已经完成的调用无法撤销。物理签署若无法读取加密的DocRoot声明，默认失败，
授权Go流程可明确使用 `IgnoreDeclarations`，不自动把物理签署视为豁免。

`converter.WithPermissionPolicy(policy)` 配置所有内置OFD导出格式和已解析文档入口。
导出始终要求 `Export`，不因图像、缩略图或文字格式改变语义；预览和打印使用reader的用途接口。
输出前预检全部选页所属文档体，处理检查点复查；无关的未选文档体不会拒绝选页导出。
拒绝发生在预检时不写输出，也不调用图像/文件Writer；运行中到期可能已有部分输出，应丢弃。
非OFD外部工具的直接转换不涉及OFD声明，第三方自定义编码器须自行执行策略。

`merge.Options.PermissionPolicy` 控制ZIP级 `Files/Bytes/Sources/Marshal`，全部输入文档体
按 `Export` 和有效期预检；`Edit=false` 不额外禁止导出合并。这是本库依据表8制定的入口策略，
标准未单独定义合并权限。合并保留同一批打开的输入直至结束，不在预检后重新打开文件路径；
首个权限拒绝不产生输出或签名/警告回调，处理及底层写出前后重新评估时间。流式处理中
到期可能已有部分字节，必须丢弃失败结果。

`merge.PageOptions.PermissionPolicy` 控制模型级 `Pages`，也要求 `Export`，不额外要求Edit。
当前拼页保留所有输入文档体的文档级资源、附件、动作等，即使使用 `Pages/Selectors/Compositions`
只输出部分页面，也须检查全部输入文档体，而不是只看底页或所选页。所有来源处理完并预检后才
触发用户签名事件回调；串行合并、回调后和实际输出期间复查。授权忽略策略传递到内部清单适配层。

清单导出 `Options.PermissionPolicy` 默认检查目标文档体Export及有效期，包括复用已打开OFD的
Build入口；单文档体导出不受其它文档体限制，WriteBundle在开始导出前预检全部文档体，
发布目录前再次检查。逐页、逐模板、每个AssetSink调用前后及manifest写出前后复查。
初始拒绝不输出manifest或资源；处理中到期可能已经交付部分资源或回调数据，应丢弃。
批量导出使用临时目录，不在失败时发布最终目录。单边有效期及诊断导出的空有效期保留原声明，
不能将缺失端点当作日期解析错误或把空期限静默改成没有期限。

归档不是普通导出：`archive` 的检查、证据清点、Prepare原包保存及Verify固定性验证不执行
文档Edit/Export或ValidPeriod声明，过期或禁止导出的原件仍可作为证据保存。Prepare保留原始
OFD字节，包括原权限和签名；附件副本属于归档准备材料，由归档系统控制访问，不是公开下载授权。
`archive/preserve.Plan/Apply` 是业务方主动发起的长期保存处理，也不要求原始Edit/Export或
有效期有效。GB/T 42133—2022 §6.2.2 a)（标准第4页/PDF第8页）要求去除Permissions，
因此Apply明确以归档用途调用底层条目重建，不让普通编辑门控反向阻止该处理。
这不是全局忽略开关，不会改变reader/converter/replace的普通权限行为；业务归档授权和归档
目录访问控制由调用方负责。不自动解密，不放宽资源预算或校验，不静默删除签名证据；
preserve保留签名条目并报告摘要可能失效，始终输出新副本，原件应另行保存。
从归档中进行普通阅读、内容提取或导出时，仍使用对应入口的权限策略。

`split.Options.PermissionPolicy` 控制每次 `Session.Write` 对当前分卷来源文档体的 `Export`
及有效期检查，页面分卷和完整文档体分卷一致，不额外要求 `Edit`。`Open/Documents/Plan`
只提供结构与计划，不构成导出授权；未选中文档体的禁令不阻止当前分卷。写出期间逐条目和
底层Writer前后复查，失败字节仍计入输出预算，但不计入成功分卷数。两种Options均可由
获明确授权的Go诊断流程显式设置 `IgnoreDeclarations`，CLI不提供全部声明豁免。

### 阅读偏好与页面内容

`Reader.Preferences()` 返回首个声明阅读偏好的文档体设置。`TabDisplay` 缺省为
`FileName`，`DocTitle` 使用同一文档体的 `DocumentTitle`，无标题时由展示端回退输入文件名。
`HideToolbar`、`HideMenubar`、`HideWindowUI` 缺省为 `false`，是可被用户撤销的初始界面建议，
不是访问权限。标题默认值依据 GB/T 33190—2016 §7.5 表9（与附录 schema 存在差异）。

`Reader.AnnotationsForPage(index)` 只加载目标页注解；`Annotations()` 返回全量清单。
读取或解析注解失败会返回错误，不把损坏注解当成不存在。
`AnnotationInfo.Print/NoZoom/NoRotate/ReadOnly` 按 GB/T 33190—2016 §15 表61描述
`Remark`，不控制 `Appearance`；缺省分别为 true/false/false/true。
备注文本没有标准化的字号、面板尺寸或无外观时的定位，展示端应明确自身呈现约定。
WASM阅读器提供独立可展开备注层，浏览器打印按页附备注清单；渲染和PDF导出仍保留原注释外观。

阅读器 `PageLinksForPage` 返回的 `PageLink.Region` 是相对 `Boundary` 归一化到 `[0,1]`
的 SVG 启动路径，不能只按外接矩形判断复杂热区。`Actions` 非空时按其顺序执行，
根节点的 URI/目标字段保留首条动作以便展示，不应把根节点再执行一次。
每条动作带独立的区域、文档体与页面作用域；鼠标激活只执行命中区域内的动作。
影片的 `AppearanceBoundary` 是原图元外观，用于播放窗口，不应按启动区域缩小窗口。
正文、模板和 Link 注解外观中的嵌套复合资源同样返回完整动作序列；热区与影片外观已映射到页面坐标，
无需再次乘复合对象矩阵。书签按来源文档体解析，附件和媒体通过动作的 `Scope` 查询，不按资源 ID 跨文档查找。
`Synchronous=true` 的非循环声音应等待播放结束后继续，切换文档时取消旧序列。
页面进入动作通过 `PageEntryActionsForPage` 读取，页码均从零开始；
动作覆盖范围与 PDF 限制见 [标准支持清单](OFD-SUPPORT.md#标准语义与动作边界)。

模板声明 `PageRes` 时使用独立资源视图，只继承公共/文档资源，不借用正文页资源。
`TextRun`、`FontInfo`、`FontResource`、`FontRef`、`FontUsageSummary` 和 `PageLink` 的
`TemplateID` 非零时标识该视图；字体用 `(Scope, TemplateID, ID)` 区分，浏览器使用返回的
`FontFamily`。模板媒体动作调用 `TemplateMediaData(scope, templateID, mediaID, maxBytes)`；
模板 ID 为零等同 `MediaData`。字体列表包含模板视图中的继承字体。
模板解析视图按有界缓存保存；专属绘制上下文每次同步使用后释放，不与正文共用私有字体缓存，
因此大量引用带专属字体的模板可能增加字体加载开销。

图像替换依据 GB/T 33190—2016 §10 表43、§13 表50：按放置变换、容器变换及输出
分辨率计算像素需求，而非固定 DPI 阈值。主图不足时才选择采样密度更高的 `Substitution`。
复合图元优先使用足够清晰的 `Thumbnail`，其次 `Substitution`，都不足时绘制矢量内容；
预览对应完整单元，不按矢量墨迹裁去透明边。文字和链接仍从矢量描述提取。
直接光栅绘制读取后端实际分辨率；记录矢量表面时使用内部 `NewDocument(background, doc, dpi)` 的目标分辨率，
之后仅对同一表面调用 `Rasterize` 不会重新选择资源，变更输出质量应重新绘制页面。

`pkg/converter` 的转换入口首参为 `context.Context`。长任务可传入带取消或超时的
上下文。`input` 支持文件路径、`*os.File` 或字节切片；写入调用方提供的文件后，
调用方应处理关闭错误。

取消或超时会原样返回 `context.Canceled` 或 `context.DeadlineExceeded`，包括发生在
最后一页写入、输出回调或写入器关闭期间的取消。取消在页间和主要阶段的读写边界
生效，不能强行中断已经阻塞的底层 I/O。失败后应丢弃部分输出；PDF 仍会尝试完成
结构收尾，但包含 `%%EOF` 不代表转换成功，必须检查返回错误。
Text、Markdown、DOCX 转换遇到无法读取的页面时返回包含全局页码的错误，不把损坏页
当作空白页。逐页文字收集和 DOCX 图片提取都会响应取消。

`converter.PNGCompressionLevel(level)` 设置 PNG 无损压缩级别：`1` 优先速度，`9` 优先压缩率，`0` 使用默认 `7`。
适用于 `Convert`、`Encode`、`Image` 及已解析文档入口的 PNG 字节输出。

`converter.PDFLossyImages(true)` 允许 OFD 转 PDF 时把图片颜色数据转为有损 JPEG，
默认关闭；已有 JPEG 尽量不重复压缩，透明蒙版仍无损保存，文字和矢量图形不受影响。
新编码 JPEG 使用固定质量 `75`，不保证输出一定更小。适用于 `PDF`、`Encode`、`Convert`
和已解析文档的 PDF 输出入口，不控制 PDF 导入或外部工具直接生成的 PDF。

```go
err := converter.PDF(ctx, input, output, converter.PDFLossyImages(true))
```

`converter.Page(n)` 从 1 开始选择多文档体合并后的全局页码，`converter.DPI(dpi)`
设置栅格输出分辨率。库默认使用 96 DPI 和透明背景；CLI 默认使用 150 DPI 和白色背景。

| 输出                     | Go 入口                                             | 输出特点                                                               |
|--------------------------|-----------------------------------------------------|------------------------------------------------------------------------|
| PDF                      | `converter.PDF`                                     | 保留页面尺寸，文字和矢量内容按支持范围写入 PDF 对象。                  |
| PNG/JPG/TIFF/SVG | `converter.Image` 配合格式选项和 `converter.Writer` | 通过回调提供输出流；TIFF 写成单个多页文件。                            |
| 纯文本                   | `converter.Text`                                    | 按页面位置聚行、用空格近似对齐列，页面间使用分页符 `\f`。              |
| Markdown                 | `converter.Markdown`                                | 识别段落与标题，转义特殊字符，有文字的页面间使用 `---`；可选表格识别。 |
| DOCX                     | `converter.DOCX`                                    | 输出可编辑段落、表格和图片，版面由 Word 重新排版。                     |
| 单文件 HTML              | `converter.HTML`                                    | 内嵌 PNG/JPG/SVG 与默认透明文字层，支持选择文字、查找和打印分页。      |

完整接口见 [converter API](https://pkg.go.dev/github.com/zc310/ofd/pkg/converter)。

文件与已解析输入统一经过 `Encode`、`PDF`、`HTML`、`DOCX`、`Image`、`Text`、`Markdown`，不再提供
依赖 `internal/render` 参数的 `*Documents` 专用入口。外部项目使用 OFD 路径、字节、
`io.Reader` 或已公开的只读包来源，不需要构造内部渲染类型。图像输出必须配置
`Writer` 或 `ImageWriter`；错误和取消处理遵循同一入口契约。

## OFD 加解密

`pkg/crypt.Inspect/Encrypt/Decrypt/Open` 实现 GM/T 0099-2020 元数据检查、
口令/SM2 证书、多接收者、独立条目密钥读写和多层解密；写端支持整包/条目/页面范围，
以及用旧凭据预检后追加加密层。页面选择整体纳入共用资源，可能影响其他页面。
输入支持路径、字节和 `io.Reader`，
加解密输出写调用方的 `io.Writer`；任何错误都要求丢弃部分输出。
解密结果是普通明文 OFD，可直接交给 converter、validator 或 sign 的既有接口。
构造包和签名元数据时可复用 `pkg/spec` 的 `Namespace`、`RootDocument`、
`EncryptionEntryPoint`、`IntegrityEntryPoint` 及 `SigTypeSeal/SigTypeSign` 常量。

可运行示例见 [pkg/crypt/examples_test.go](../pkg/crypt/examples_test.go)，预算、错误和
签名保护边界见 [库指南](../pkg/crypt/README.md)。文件流程可使用
[ofd-crypt](../cmd/ofd-crypt/README.md) 的口令文件/标准输入、私钥输入及原子输出。
其他工具不会自动解锁加密原包；Go 调用方可直接传入完整解锁的只读视图：

```go
view, err := crypt.Open(ctx, input, crypt.OpenOptions{
    DecryptOptions: crypt.DecryptOptions{Password: password},
})
if err != nil {
    return err
}
defer view.Close()
err = converter.PDF(ctx, view, output)
```

`reader.Open(view)` 创建自己的读取租约，关闭阅读器不关闭
原视图。`validator.Validate(ctx, view, name)` 先检查原始物理条目，再检查逻辑文档，
保留原始加密状态；`sign.Verify` 和 `analyzer.Analyze` 也直接接受完整视图。
存在外层物理签名时，validator 在原包检查其引用和摘要，解密视图用于普通文档校验。
`AllowPartial` 只用于明确检查已解锁条目，不能把部分视图交给普通渲染/验签链。

`pkg/sign` 的 `Options.Type="Sign"` 接入 SM2/SM3 SignedData 或 RSA/ECDSA CMS 数字签名，
`SignatureMethod` 与 `DigitalSigningOptions.Algorithm` 使用相同组合 OID；`CheckMethod` 可独立选择 SHA256/384/512，缺省 SM3。
`Verify` 按 Type 验证普通签名或 SES，并检查 Relative 基础签名。`FullyValid()` 是完整结论。
`sign.Sign(ctx, input, output, Options{Type: "Sign", Append: true})` 保留旧普通签名并增加记录，
`Relative` 指定基础签名，最后用 `LockSignatures: true` 固定列表；已被摘要保护的列表拒绝追加。
用法和范围见 [签名指南](../pkg/sign/README.md#普通签名追加)。
`Options{Type: "Sign", Physical: true}` 为主入口明文、没有已有签名入口的加密物理包签署；
`VerifyPhysical` 直接检查密文和控制文件，`Verify` 在解锁视图上也保留该物理读取域。
未完整解锁的视图传给 `Verify` 返回 `crypt.ErrLocked`；无凭据验外签使用 `VerifyPhysical`。
`pkg/integrity.Inspect/Protect/Verify` 提供附录 D 清单、带时间戳签名、文件集合、引用闭包及
内容覆盖检查。清单签名通过不等于所有文件内容完整；具体结果和证据要求见
[完整性保护指南](../pkg/integrity/README.md)。
验证中止时须处理返回错误，已有部分报告保持 incomplete 或已确认的 invalid。

## 导入其他格式

使用统一的 `converter.Convert` 入口，并空白导入需要的导入器。以下示例将 Markdown
排版为 OFD：

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/import/markdown"
)

func main() {
	output, err := os.Create("output.ofd")
	if err != nil {
		log.Fatal(err)
	}

	err = converter.Convert(context.Background(), "md", "ofd", "input.md", output)
	closeErr := output.Close()
	if err != nil {
		log.Fatal(err)
	}
	if closeErr != nil {
		log.Fatal(closeErr)
	}
}
```

`from` 为空时按输入文件扩展名或内容识别格式。导入器按需注册：

| 输入       | 空白导入路径                                         |
|------------|------------------------------------------------------|
| PDF        | `github.com/zc310/ofd/pkg/converter/import/pdf`      |
| Markdown   | `github.com/zc310/ofd/pkg/converter/import/markdown` |
| CSV/TSV    | `github.com/zc310/ofd/pkg/converter/import/csv`      |
| EML 邮件   | `github.com/zc310/ofd/pkg/converter/import/eml`      |
| SVG        | `github.com/zc310/ofd/pkg/converter/import/svg`      |
| Office     | `github.com/zc310/ofd/pkg/converter/import/office`   |
| HTML/MHTML | `github.com/zc310/ofd/pkg/converter/import/html`     |
| XPS        | `github.com/zc310/ofd/pkg/converter/import/xps`      |
| 图片       | `github.com/zc310/ofd/pkg/converter/import/image`    |

Markdown 会重新排版为 A4，远程图片会跳过并记录警告。Office/HTML/XPS 转 OFD 时先由
外部工具生成 PDF，再执行 PDF 导入；直接转 PDF 时使用外部工具产物。外部工具要求见
[运行依赖](BUILDING.md#运行依赖)。

CSV/TSV 以 Go 直接排版，默认 UTF-8（接受 BOM）、首行表头、自动列宽和跨页重复表头。
通过 `converter.WithCSVImportOptions(converter.CSVImportOptions{NoHeader: true,
Encoding: "gb18030", Title: "设备台账", FontFile: "fonts/cjk.ttf"})` 配置输入；
`FontData` 可替代 `FontFile` 提供字体字节，两者互斥。纸张和方向沿用
`WithPaperSize`、`WithPaperDimensions`、`WithLandscape`。字段保持文本，编号前导零和金额
小数位不会被自动转换；纯拉丁文本可使用内置字体，中文自动查找系统字体并嵌入。
范围和限制见 [CSV/TSV 支持清单](CSV-SUPPORT.md)，可运行用法见 `ExampleConvert_csv`。

EML 与 SVG 先预处理为 HTML，再通过本机 Chrome/Chromium 转换；直接输出 PDF 时使用
浏览器产物，输出 OFD 时执行 PDF 导入并沿用 PDF 资源预算。调用
`converter.Convert(ctx, "eml", "ofd", input, output)` 或将 `from` 改为 `"svg"`，
纸张、方向和浏览器选项沿用 `WithPaperSize`、`WithLandscape`、`WithChrome` 等公共配置。
EML 输出邮件头、选中的正文、CID 图片和附件清单，OFD 中同时嵌入原附件，支持通过
`reader.Attachments` / `AttachmentData` 列出和读取；SVG 按纸张等比放置。
详见 [EML 支持清单](EML-SUPPORT.md)、[SVG 支持清单](SVG-SUPPORT.md)。

Office、HTML 转换会把调用方上下文传给外部进程，取消后结束处理并清理临时目录。
也可直接调用 Markdown 包的 `Convert(ctx, input, output)` 或
`ConvertFile(ctx, inputPath, outputPath)`；不需要取消时传 `context.Background()`。
`ConvertFile` 在入口已取消时不会创建或截断目标文件。

PDF 导入默认实施资源预算。可通过 `converter.WithPDFImportOptions` 设置预算、同步
`OnDiagnostic` 回调和 `Strict` 模式；这些设置也作用于 Office/HTML/XPS 转 OFD 的
PDF 阶段。默认值、错误判断和示例见 [PDF 资源预算与诊断](PDF-SUPPORT.md#资源预算与诊断)。

PDF 导入默认保存解码后的原始内嵌字体程序；设置
`converter.PDFImportOptions{SaveRepairedFonts: true}` 可将修复结果写入 OFD 包。
这项设置同样作用于经 PDF 的导入阶段，字形映射和度量始终使用修复后的字体。

## 接入按页阅读接口

`pkg/reader` 提供页面渲染、文字、搜索、导航与导出接口，供界面按需调用：

阅读器的页面、文档信息、权限、字体声明和使用统计、资源统计、附件及多媒体资源清单、注解、图层、页面链接、大纲、显示偏好、文字与搜索结果
类型提供 JSON tag；字段名与网页阅读器对应契约一致。JSON v2 默认将 nil 切片编码为
空数组；其他 JSON 编码器的 nil 切片行为可能不同。
字体使用页统计 `FontUsage` 和 `FontUsageAll` 接收上下文，在页面之间响应取消；
取消时不返回部分结果。

```go
package main

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"log"
	"os"

	"github.com/zc310/ofd/pkg/reader"
)

func main() {
	data, err := os.ReadFile("input.ofd")
	if err != nil {
		log.Fatal(err)
	}
	r, err := reader.Open(data)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	fmt.Println("总页数：", r.PageCount())
	// reader 的页索引从 0 开始。
	var png bytes.Buffer
	err = r.RenderPage(context.Background(), &png, 0, reader.RenderOptions{
		DPI: 144, Background: color.White, Format: reader.RenderPNG,
	})
	if err != nil {
		log.Fatal(err)
	}
	_ = png.Bytes() // PNG 字节可交给界面显示。

	runs, err := r.Text(0)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("首页文字段数：", len(runs))

	// 长文档搜索可限制工作量，并用同一个 context 取消。
	report, err := r.Search(context.Background(), "关键词", reader.SearchOptions{
		MaxPages: 2000, MaxResults: 5000,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("命中 %d 处（扫描 %d 页，截断=%v）\n", len(report.Results), report.Scanned, report.Truncated)
}
```

打开后调用 `Close` 释放资源。长文档可用
`reader.Open(input, reader.OpenOptions{...})` 调整页面缓存，最多传一组选项，省略时使用默认值。
`input` 支持 OFD 字节、文件路径、`io.Reader` 或已完全解锁的 `crypt.View`；渲染 DPI
范围为 1–600，默认 96，单页还受像素预算限制。版本清单和切换由 `Versions`、
`SelectVersion` 提供，切换成功后需重新读取页面、字体和导航信息。
完整接口见 [reader API](https://pkg.go.dev/github.com/zc310/ofd/pkg/reader)。

`RenderPage(ctx, output, index, options)` 和 `RenderPDF(ctx, output, indices, options)`
将编码结果写入 `io.Writer`；`RenderPages(ctx, indices, options, consume)` 按页同步调用
`consume(ctx, position, pageIndex, data)`，页数没有固定上限。回调参数中的 `data` 在回调
返回后会复用，需要保留时必须复制。多页回调在 Reader 读锁内执行，不要在回调中重入同一 Reader。
以上接口均显式接收上下文。取消在页面加载、绘制、
栅格化、编码及页间的阶段边界生效，返回原始 `ctx.Err()`；单次底层绘制或编码仍需
执行到阶段边界。输出 writer 可能已写入不完整数据；多页消费回调此前已处理的数据不会回滚。
WASM 下可取消的调用会在
阶段边界让出浏览器事件循环，使取消消息不必等待输出分块才被处理。
不需要取消或超时时，首参可传 `context.Background()`。

`Text` 和 `Search` 默认包含正文、模板及其复合对象内的文字；复合单元的缩放、偏移
和裁边映射也用于字符高亮与链接热区。调用 `SetTextOptions` 可将水印、批注和 OFD
印章子文档里的文字加入覆盖层与搜索，设置会同时清理两种缓存。图片和字形轮廓
不执行 OCR。`TextRun.Source` 为 `page`、`template`、`watermark`、`annotation` 或
`seal`；印章字体在子文档中解析，返回的印章 run 不借用父文档的字体 ID。

```go
err := r.SetTextOptions(reader.TextOptions{
    IncludeAnnotations: true,
    IncludeWatermarks:  false,
    IncludeSeals:       true,
})
if err != nil { return err }

png, diagnostics, err := r.RenderPageWithReport(ctx, 0, reader.RenderOptions{DPI: 144})
if err != nil { return err }
_ = png
for _, issue := range diagnostics.Diagnostics {
    fmt.Println(issue.Code, issue.ObjectID, issue.ResourceID, issue.Count)
}
```

诊断报告属于单次调用，包含页 ID、实际绘制操作的资源用量和可恢复问题。相同原因、
对象与资源会合并计数，最多保留 128 种问题，超过部分记入 `Dropped`。记录内容包括
字体不可用、字体面创建失败或回退，图片/复合资源缺失，模板、注解与印章加载失败，
不支持或空的 OFD 印章、颜色空间缺失与 Profile 读取失败，以及资源预算拒绝；
没有诊断不代表完成了 OFD 合法性校验。请求报告时模板重新绘制，以捕获本次诊断。
颜色缓存命中仍重放本次诊断；正常隐藏、透明和裁剪为空不作为失败报告。
消息最多保留 512 字节且不截断 UTF-8 字符。可恢复问题不会在库内输出 Warn/Error 日志。

PDF 导出同时保留内部跳转大纲与外部 URI 大纲。选页导出会移除指向未导出页的
内部条目，外部 URI 不受选页范围影响，两种条目可在同一大纲树中混排。

## 创建、合并与签章

| 需求                     | Go 入口与说明                                                                                                                                       |
|--------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------|
| 创建 OFD                 | `pkg/creator` 的 `CreateFile`、`Create`、`Marshal` 及流式页面接口；坐标、边界和字号以毫米计，见[创建器库指南](../pkg/creator/README.md)。           |
| 打包多个文档体           | `pkg/creator/merge.Files`、`Bytes`、`Sources` 保留文档体目录，签名处理由 `Options.Signatures` 控制。                                                        |
| 拼页、选页、重排和页面叠加 | `pkg/creator/merge.Pages` 解析并生成单文档 OFD，重映射资源与跳转引用；可用 `PageOptions.Compositions` 将多张来源页按顺序叠加为一页；原签名和版本丢弃，见[合并命令与选页说明](../cmd/ofd-creator/README.md#合并多个-ofd)。 |
| 按页或文档体拆分         | `pkg/creator/split.Open(ctx, input, options)` 建立可复用会话，`Plan` 校验分组，`Write` 逐份写出；见[拆分库指南](../pkg/creator/split/README.md)。 |
| 条目编辑与水印           | `pkg/creator/replace.Files/Paths` 编辑包内条目，`pkg/creator/watermark.Add/Replace/Remove` 编辑水印注解。 |
| 长期保存转换             | `pkg/archive/preserve.Plan/Apply` 生成与执行按标准条款记录的长期保存改动计划。 |
| 外部命令签署             | `pkg/sign.Sign` 计算引用摘要，将 `Signature.xml` 交给外部命令，写回其输出的 `SignedValue.dat`。                                                     |
| 生成印章与签章、完整验证 | `pkg/sign.CreateSeal`、`CreateSignedValue`、`Verify`，见[签署与验证指南](../pkg/sign/README.md)。                                                   |

批量修改 XML 后检查文件依赖时，可用 `validator.NewReferenceResolver(ctx, input, options...)`
建立一次解压索引，再调用 `Resolve(ctx, overrides)`。`overrides` 仅替换既有文件，调用之间
互不影响；未修改 XML 的引用分析结果会缓存。只有返回索引的 `Complete()` 为真时，才可
根据 `Reachable` 删除其他文件。该检查不验证对象 ID 语义、摘要或密码学签名，也不执行资源
ID 粒度的裁剪；需要像拆分器一样先裁剪资源定义，再分析文件依赖。

SES 完整验证区分通过、失败和证据不足，证书信任根、CRL 与印章状态由应用提供。
分析报告和浏览器签名面板展示该结论，校验器通过 `--check-signatures` 启用对应检查。
`ofd-seal` 的显示尺寸以毫米计，支持独立配置获授权签章者。

欢迎使用自己的测试文档和证书试用。外部签名协议与 SES 结构示例见
[ofd-signer-demo](../cmd/ofd-signer-demo/README.md)，仅供演示与测试。第三方 TSA 服务的
实际互操作尚未验证；兼容性问题可通过 [Issues](https://github.com/zc310/ofd/issues/new) 反馈。

## 字体与显示

渲染优先使用修复后的内嵌字体，再尝试原始字体和外部回退；逻辑字体或缺字可能需要
可用的中文字体。SVG 为同名字体的不同子集分配独立字体族，保留普通文字与矢量轮廓。
低 DPI 图片和 SVG 的极细描边采用参考线宽，以改善标签显示；具体兼容策略见
[OFD 支持清单](OFD-SUPPORT.md)。

字体修复由仓库内的 [internal/fontrepair](../internal/fontrepair/README.md) 按 OpenType
与 Adobe CFF/Type 2 公开规范实现；与 PDF 文字提取共用
[Adobe-GB1 映射数据](../internal/cidmap/README.md)，保留 Adobe BSD 许可。
依赖维护与回归入口见[开发与维护索引](README.md#开发与维护)。
