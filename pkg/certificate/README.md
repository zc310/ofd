# 电子证照基础元数据

`pkg/certificate` 提供本地解析与格式检查，不加载文件、不改写原文。
OFD 的标准 XML 声明仍在 `pkg/spec`；业务校验不放入声明层。

```go
metadata := certificate.ParseMetadata(docInfo.CustomDatas.CustomData)
names := metadata.Values("certificateName") // 也识别短名 ZZMC
issues := metadata.Validate()

id, err := certificate.ParseIdentifier(identifier)
```

可运行代码见 [example_test.go](example_test.go)。

## 检查范围

- GB/T 36903—2018 §5.2 基础元数据，GB/T 36902—2018 §5.2.2、§6.3 类型代码，
  GB/T 36905—2018 §4 c)、§5 的标识与元数据要求。
- 必选项：名称、类型代码、编号、标识、颁发机构名称、颁发日期、持证主体名称。
  不要求目录规范中的其他必选字段；可选项不强制出现，但出现时不能留空。
- 精确识别标准英文名或短名，保留顺序、未知字段和重复项，重复声明（含别名）报错。
  `certificateIdentifier` 使用标准英文名，不将附录示例的 `Certificate_Identifier`
  自动作为别名。`certificateHolderCategory` 与 `certificateHolderTypeName` 不混用。
- `^` 多值的空项及名称、代码、代码类型的项数对应关系；
  主体代码类型为 `001`/`111` 或中文名称为“统一社会信用代码”/“公民身份号码”时
  要求主体代码。列表顺序的业务真实性需外部核实。
- 完整日历日期支持 `YYYY-MM-DD`、`YYYYMMDD`、`2018年7月25日`；
  截止日期允许“长期”，可选截止日期省略时不补写。可解释的有效期检查先后关系，
  不按当前时间判定证照失效。其他日期表达仅提示 `date_unchecked`，不宣称其违反
  GB/T 7408；不自动修剪日期或补全缺失日。
- `KZ_` 扩展原样保留，裸前缀报错；没有行业规范时不擅自判定其他未知字段无效，
  不验证扩展的定义、类型、代码表和语义。

`Metadata.Entries()` 返回副本；`Values()` 返回全部声明，不以最后值覆盖前值。
`Issue.Index` 指向输入条目，缺失项为 `-1`；代码供程序分支，消息不包含原始主体代码。

## 标识与标准示例差异

按 GB/T 36904—2018 §5 检查：
`1.2.156.3005.2.<21位类型代码>.<18位颁发机构代码>.<流水号>.<三位版本号>.<校验位>`。
流水号只接受数字和英文大写字母。类型的18位机构前缀属于**定义机构**，
不要求它等于颁发机构。机构代码仅检查长度与字符集，不校验 GB 32100 校验位或登记。

校验位按 §5.7 的 ISO/IEC 7064 MOD 37,36 计算，排除根代码及分隔符，
字符值为 `0—9 = 0—9`、`A—Z = 10—35`：

- GB/T 36905 附录A出现的根代码 `1.2.156.3003.2` 不符合 GB/T 36904 §5.2。
- GB/T 36904 附录A（PDF 第7—8页）称计算值22对应 `L`，表中还有字符值与中间和不符；
  按正文算法复算值22对应 **`M`**。该示例以 `L` 结尾时报告校验位错误。
  独立 MOD 37,36 向量及该差异均有回归测试，不为示例另设兼容字母表。

解析失败可用 `errors.Is(err, certificate.ErrIdentifierFormat)` 或
`errors.Is(err, certificate.ErrIdentifierChecksum)` 区分。
**不会修改文件或自动修正标识，尤其不会重写已签名原件。**

## OFD 文件检查

```bash
ofd-validator --certificate-metadata --format json certificate.ofd
```

Go API：`validator.WithCertificateMetadata(true)`。同时接受 TypeID 标引；
单独的 `WithCustomTagTypeID(true)` / `--custom-tag-type-id` 仍只检查标引入口。
元数据只从实际 `OFD.xml/DocBody/DocInfo/CustomDatas` 读取，各文档体独立检查，
不从业务标引 XML、页面文字或孤立 XML 猜测补齐。

报告检查项为 `certificate_metadata`，问题码为 `certificate.metadata.<code>`，
带 XML 位置和标准条款；`certificate.scope` 提示当前范围。
结构模式及 `--skip-xsd` 也执行元数据检查，未启用时不新增该检查项。
成功只表示这些本地检查通过，不代表全面合规或内容可信。

尚未检查：标识唯一性及历史版本递增、登记与机构授权、行业业务元数据、
代码表真实性、加注件元数据条件、锁定签名及 GM 码。无需机构联网或授权证据
也能通过本地格式检查，不能将其视为已完成这些外部验证。

原件锁定、加注件制作及重新验收由独立子包
[`workflow`](workflow/README.md) 编排，不扩大上述基础检查或 CLI 开关的范围。
该工作流要求实际锁定签名、完整信任/吊销证据和外部机构角色授权回调，
不能只凭本包的格式检查结果执行可信验收。
