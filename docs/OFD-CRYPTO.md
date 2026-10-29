# GM/T 0099-2020 密码功能与实现约定

本文区分规范要求、标准图表歧义和兼容读取。依据是
[`GM/T 0099-2020`](../testdata/standards/GMT_0099-2020.pdf)
《开放式版式文档密码应用技术规范》，属于密码行业标准。
下面为纸面页码，PDF 页码加 3。附录 A 为规范性附录，B、C、D 为资料性附录。

包的选择、验证结论和组合操作顺序见 [安全能力指南](SECURITY.md)。

## 已实现能力

加密元数据检查、口令/证书加解密、多层只读解密视图、普通数字签名和完整性保护的主体功能
均已实现。本文按当前能力说明使用约定，验证范围和不支持项见下文。

| 功能         | Go API / 工具入口                                       | 当前范围                                                                            |
|--------------|---------------------------------------------------------|-------------------------------------------------------------------------------------|
| 加密检查     | `crypt.Inspect`、`ofd-crypt inspect`                    | 可见元数据、操作依赖、映射与接收者检查，以及锁定状态和错误分类                      |
| 加解密       | `crypt.Encrypt/Decrypt`、`ofd-crypt encrypt/decrypt`    | 口令方案 `1.1.1`、SM2 证书方案 `1.1.2`、同方案多接收者、明文/密文映射、原始字节恢复 |
| 加密范围     | `IndependentKeys`、`Append`、`Pages`                    | 独立条目密钥、线性链追加层、页面及关联资源范围                                      |
| 只读解密视图 | `crypt.Open`                                            | 多层依赖、按操作/seed 提供凭据、部分解锁状态、租约与密钥释放                        |
| 文档处理     | parser、converter、sign、analyzer、webreader、validator | 完全解锁视图直接接入；校验器分别检查物理容器与逻辑文档                              |
| 图形阅读器 | `ofd-viewer`、WASM 阅读器及 `ofd.open` | 口令、UTF-8 口令文件或 SM2 私钥解锁，默认兼容读取；WASM 解锁后按文档体和当前版本执行打印、复制、导出权限检查，范围见 [文档权限](../cmd/ofd-wasm/web/README.md#文档权限) |
| 普通数字签名 | `pkg/sign`、`ofd-creator sign`                          | SM2/SM3 SignedData、RSA/ECDSA CMS、SHA-2 文件引用摘要、附录 B 扩展、普通签名追加及受限物理包签署 |
| 完整性保护   | `pkg/integrity`、`ofd-crypt integrity`                  | 带时间戳清单签名、物理文件集合、引用闭包和内容签名覆盖检查                          |

### 加解密与逻辑视图

`Inspect` 只检查可见元数据；`EncryptScope` 仅作声明保留，不据此确定真实加密范围。
`Decrypt` 输出明文 OFD，逐条保留原始字节；CLI 使用临时文件及原子提交保护输出。
API、预算及失败输出约定见 [`pkg/crypt`](../pkg/crypt/README.md)，命令行见
[`ofd-crypt`](../cmd/ofd-crypt/README.md)。

映射表按内容区分明文 XML 与分组密文，不依赖扩展名。`Inspect` 不接收凭据，不能先取得
FEK 试解；遇到有效 UTF-8 且去除 BOM/空白后以 `<` 开头的内容，会按明文 XML 严格检查。
密文恰好满足该条件时可能被误拒绝；当前不会将 XML 检查失败自动降级为密文。
`MapState=encrypted` 只表示映射表仍不可读，不能作为密文有效性的证明。

`Open` 建立只读逻辑视图，不保存明文 ZIP。打开时建立索引并预检已解锁条目，
清理条目明文缓冲；后续每次读取重新解密。
parser/converter/sign/analyzer 可直接接收完全解锁视图，webreader 使用 `OpenInput`，
validator 使用 `Validate` 同时检查物理容器与逻辑文档。部分解锁不允许占位内容回退。

`IndependentKeys` 为每条目生成独立 FEK/IV/seed；`Append` 用旧凭据完整预检，
再对原始物理包追加层，保留入口旧记录字节并设置 Relative，只接受单一线性链。
`Pages` 依据 C.1 和 GB/T 33190 §7.4–7.9、§15、§18，选中页面内容、模板、注释、相关印章，
以及整份资源索引和所列数据。共用资源可能影响其他页面，报告列出实际选中项。
页面选择与追加互斥，不自动加密文档结构、附件、自定义标签或历史版本。
页面、模板及其引用依据 GB/T 33190 §7.3 表 2、附录 A 的 `ST_ID/ST_RefID` 按数值匹配和检查重复；
前导零或正号不改变所指页面，无法解析的引用会在加密输出前失败。
`999.ofd` 注释中的 Parameters/Parameter 未带命名空间，作为不定义文件引用的参数原样加密；
其他外来命名空间不作范围推断。页面 XML 的 BOM 和原始字节均保留。

### 数字签名与完整性保护

普通签名实现 §7.2：SM2/SM3 使用 GB/T 35275，RSA PKCS#1 v1.5/ECDSA 使用 RFC 5652 CMS
（§7.2.2 c），支持 SHA256/384/512。文件引用摘要独立选择 SM3 或 SHA-2，
XML SignatureMethod 与实际 CMS 算法必须一致。支持附录 B 的 Type/Relative、
ProtocolVer/ExtendData、Parameters、Seal/ImageLoc；SES 与普通签名按列表 Type 分派。
每份普通 SignedData 限定为单一签名者及已解释的属性，多人签署使用多个 OFD 记录。

`sign.Options.Append` 和 `ofd-creator sign --append` 保留旧签名文件，新建独立目录，
按表 B.1 写入 Relative，并预检旧引用不会因更新入口而失效。
首次使用追加模式，末次用 `LockSignatures`/`--lock-signatures` 固定最终列表后再保护清单；
旧签名已摘要当前列表时拒绝追加，未锁定列表的内容覆盖保持 incomplete。

`pkg/integrity` 与 `ofd-crypt integrity` 实现 §7.4 的清单生成、带时间戳签名验证、文件集合、
引用闭包及内容覆盖检查。缺少证据或充分遍历时保持 incomplete，生成清单不替代逐文件签名。
`sign.Options.Physical` / `ofd-creator sign --physical` 支持加密物理包签署，要求主入口明文且
没有已有签名入口，覆盖全部物理文件、最终入口与列表，再附加时间戳清单。
完整性验证分别检查物理内容与逻辑闭包；证据齐全时该流程可完整通过，无凭据仍保持闭包未完成。
完整用法见 [签名指南](../pkg/sign/README.md) 和 [完整性保护指南](../pkg/integrity/README.md)。

### 已知限制与验证范围

- 普通数字签名暂不支持 RSA-PSS、多签名者 CMS、SubjectKeyIdentifier 签名者、内外签名混合编排或加密主入口的物理签名发现。
- 固定 ofdrw 2.4.0 已完成单层、共用密钥、默认加密映射的双向互通；
  多层、独立条目 seed 和页面范围由构造样本及读写回归验证，不扩大该第三方互通结论。
- RSA/ECDSA 的六种 SHA-2 CMS 组合已完成本地 OpenSSL 双向验证；普通数字签名和完整性保护
  已有构造回归及完整 CLI 流程验证，尚未完成服务商、HSM 和具体 OFD 阅读器的协议互通；
  时间戳使用测试 CA 和独立编码令牌验证，尚未实测真实在线 TSA 服务。
- 实现与测试结论限于本文范围，不代表标准全部能力覆盖或外部符合性认证。

## 标准格式与歧义处理

| 依据                        | 原文与问题                                                | 当前读取决定                                                                                                       |
|-----------------------------|-----------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------|
| §7.3.4 e）、C.3，p.6、17–19 | 解密入口固定为根目录 `Encryptions.xml`，明文写入          | 从物理 ZIP 识别，先于 `OFD.xml` 解析；附件目录同名文件不算入口                                                     |
| C.3 图 C.2 与表 C.2，p.18   | 图根元素为 `EncryptList`，正文/表为 `Encryptions`         | 采用正文/表的 `Encryptions`；暂不增加未有样本证据的 `EncryptList` 别名                                             |
| C.2 图/表 C.1，p.16         | 根元素拼写为 `DecyptSeed`                                 | 按原文拼写；不自行改为 `DecryptSeed`                                                                               |
| C.4 图/表 C.3，p.19–20      | 根元素分别为 `EncryptEntries` / `EncryptedEntries`        | 两种均接受，别名产生 `encryption.root_alias` 诊断                                                                  |
| C.4 表 C.3                  | `Path/EPath` 为包内绝对路径                               | 接受 `/Doc_0/…`，转换为 ZIP 相对名称；拒绝 `..`、反斜杠、盘符、URI 和重复路径，映射不使用大小写回退                |
| C.4 图/表 C.3               | `EPath` 在图中必选，表中可选，无缺省语义                  | 缺失时诊断且保留为空；不推断为 `Path`                                                                              |
| C.3、C.4                    | seed/map 路径由入口或映射引用，映射加密后的文件名仅为建议 | 不写死位置或扩展名；相对引用基于引用文件目录，绝对引用基于包根                                                     |
| C.4 表 C.3                  | 条目可指定独立 `DecryptSeedLoc`                           | 检查/解密均支持；可按 seed 使用独立凭据和不同方案，seed ID 仍须匹配操作 ID                                         |
| C.2 表 C.1，p.17            | `IVValue` 可选，缺省 16 个零                              | 记录 `IVDefaulted`；显式 IV 必须解码为 16 字节                                                                     |
| C.2 图 C.1 与表 C.1         | 图中 UserInfo 数量下限为 2，表和协议允许单个访问者        | 接受至少一个接收者；扩展节点按表在 strict 下要求存在                                                               |
| C.3                         | `Relative` 为上次操作 ID                                  | 检查 ID、悬空引用、环和深度；从后层向前层恢复。无凭据检查仍将前层标为 deferred；解密时先恢复前层所需 seed/map/密文 |
| C.1，p.15                   | 明文可以删除或替换为非保密占位内容                        | 锁定包不能作为普通文档继续渲染/验签；不能因为原路径存在而宣称恢复成功                                              |

检查器不是完整 XSD 验证器，未知扩展不提供语义结论。缺省 `EPath` 的执行语义仍留待证据；
没有声明依赖却重叠的恢复范围、跨操作 seed/map/密文依赖或缺少控制文件仍失败，不猜测 XML 顺序。
按 C.4 写入 XML 映射的路径须能无损表示为 XML 1.0 文本；写端在输出前拒绝无效 UTF-8 和非法 XML
字符，并核对生成映射中的路径与原路径一致，避免替换字符造成重名或恢复时改名。
多层和独立条目 seed 由标准构造样本、公开写读接口和真实签章恢复测试验证，
不扩大 ofdrw 2.4.0 的单层互通结论。

## 已确认的 ofdrw 格式

对照 ofdrw 仓库的 `OFDEncryptor`、`UserPasswordEncryptor`、`UserCertEncryptor`
和 `EncryptEntries` 源码，确认以下形式；固定版本 2.4.0 的完整包互通记录见
[`testdata/crypt/ofdrw`](../testdata/crypt/ofdrw/README.md)。

- 数字操作 ID（不满足 `xs:ID`）和生成流程遗漏 `EncryptCaseId`：compat 允许并诊断，
  strict 拒绝；缺失方案保留未知，显式未知方案不猜测或替换。
- 映射表根节点为 `EncryptEntries`；沿用 OFD 2016 命名空间。
- 口令按 UTF-8 转字节，通过 GB/T 32918.3 §5.4.3 的 SM3 KDF 派生 16 字节 KEK。
- FEK 使用 SM4-CBC/PKCS#7 包装；16 字节 FEK 的包装值为 32 字节。
- 证书包装使用 C1C3C2，再编码为 ASN.1 `SM2Cipher`。

`Inspect` 只校验包装数据的 Base64、已知口令方案的长度和 IV 长度。
`Decrypt` 检查 ASN.1 SM2Cipher 的 DER 编码、曲线点、C3/FEK 密文长度；使用 gmsm 执行
SM2 解密及 C3 校验。compat 模式接受非压缩点裸 C1C3C2，转换为 ASN.1 后调用解密器。
缺少方案时须显式指定 `MissingScheme`，不根据有无证书或包装密钥长度猜测。

独立密码学向量由 OpenSSL 3.5.5 产生，覆盖空/ASCII/中文及 emoji 口令、SM4 填充边界和
证书 FEK 包装。裸编码回归从独立 ASN.1 向量转换生成。这些检查验证原语和编码，
它们不单独代表 OFD 产品互通，完整包回归另用 ofdrw 2.4.0 和已有签章样本验证。

实测口令/证书、整包/局部、默认加密映射双向恢复字节一致；口令签章样本的双向恢复
还检查签名摘要和两层数学签名。ofdrw 2.4.0 在明文映射长度不为分组倍数时抛出未捕获
`DataLengthException`，该情况已记录为第三方限制，不计入成功互通。
ofdrw 还会将 `SignedValue.dat` 加密为 `signedvalue.dat`：只有大小写不同的物理名称正好是
该映射的精确 EPath 时才允许先移除密文再恢复原名；无关大小写别名和物理条目冲突仍拒绝。

### 读取模式与阅读器默认值

Go 加解密 API 和 `ofd-crypt` 默认严格模式。历史 ofdrw 文件需显式使用 `ModeCompat`，
缺少方案时再指定 `MissingScheme`；CLI 示例：

```bash
ofd-crypt decrypt --mode compat --missing-scheme password \
  --password-file password.txt -o plain.ofd ofdrw-encrypted.ofd
```

`ofd-viewer` 与 WASM 阅读器默认使用兼容模式。缺少 `EncryptCaseId` 时，用户输入口令或选择口令文件即选择
`1.1.1`，选择 SM2 私钥即选择 `1.1.2`；未提供凭据时显示解锁框，已声明方案保持原值。
UTF-8 口令文件保留 BOM、空格和换行，适用于无法用单行密码框输入的口令。
图形阅读器的具体用法和资源限制见 [viewer 加密文档说明](../cmd/ofd-viewer/README.md#加密文档)
与 [WASM 加密文档说明](../cmd/ofd-wasm/web/README.md#加密文档)。

## 解锁状态与报告

`Inspect` 返回的 `Info.State` 为 `none/locked`；解密视图另有 `partially_unlocked/unlocked`。
加密入口存在而元数据损坏时，检查返回错误及已有部分信息，不能按 `Info` 继续文档处理。
解析器使用兼容检查并返回
`*crypt.LockedError`，错误可通过 `pkg/crypt.ErrLocked` 判断。

validator 在检查物理 ZIP 后进入加密检查：

- `encryption.locked` 表示未解锁；文档引用、语义、摘要及按需启用的签章检查为 `incomplete`。
  没有其他错误时总状态为 `partial`，CLI 退出 `1`。
- 明文映射全部可见时，检查未被映射遮蔽的明文 XML；不验证原路径的占位内容。
  映射不透明时只尝试读取可见且看似 XML 的 `OFD.xml` 以识别其 profile。
  文档级引用闭包和语义尚未执行，不能把报告当作明文部分的完整验证。
- OFD-A/OFD-H 的禁止加密规则仍执行；其他依赖原始内容的 profile 规则不宣称通过。
- 显式启用的 XSD/签名检查不能因为加密而标为通过；ZIP 路径和重复条目错误仍正常报告。
- 元数据损坏与检查超出预算分别使用 `encryption.metadata_invalid` / `encryption.limit_exceeded`。
- `PackageReferences` 遇到锁定包返回错误，不能输出可用于删除文件的完整闭包。

解密视图用 Opaque 标记仍不可见的映射。
只读逻辑包与物理 ZIP 使用独立元数据/条目标识，消费者关闭只释放其租约；最后租约关闭清零
视图持有的 FEK/IV。不得把视图的零压缩大小当作原始 ZIP 压缩统计。
validator 保留 `encryption.encrypted=true` 的原始事实，并以 `state=unlocked` 表示内容可检查；
解锁完整且普通 OFD 所需检查通过时可得有效结论，原始 OFD-A/OFD-H 加密违规仍是错误。

analyzer 返回 `partial` 报告和 `ErrLocked`；页面、资源和签名统计未执行，数值不能解释为
文档原始内容数量。JSON 可选字段 `encryption` 给出元数据，文本等报告展示未解锁原因。
报告 schema 保持 `1`，新增字段不改变旧字段的语义。

## 密码学与原始字节约定

附录 A 的 `1.1.1/1.1.2` 使用 SM4-CBC 和 PKCS#7；CBC 不提供认证。
解密失败不能总能区分错误口令和损坏密文，填充正确也不能证明明文未被篡改。
A.2.1 d）引用步骤 b）的结果存在笔误，结合 c）和 A.2.2，应使用 KDF 派生 KEK 包装 FEK。
UTF-8、KDF 输入、输出长度和 SM2 包装编码应由固定测试向量约束，不能只靠自身往返测试。

写端每次使用密码学随机源生成 FEK/IV，并显式记录 IV。默认一次操作的条目和映射共用
FEK/IV，与已实测的 ofdrw 格式一致，存在 CBC 跨条目相同前缀关系暴露。
`IndependentKeys=true` 读写按 C.4 的 `DecryptSeedLoc` 使用逐条独立随机密钥/IV。
读端按表 C.1 接受缺省零 IV，用于读取历史包；这不保证来源使用了唯一 FEK，也不消除
同一 FEK/IV 被多个条目复用时的相同前缀暴露。写端始终显式生成随机 IV。
不能直接改变现有方案的编码。加解密物化逐条保留明文字节，避免 XML 模型序列化改变
已签名内容。已有 `OFDEntries.xml`、密文签名或加密元数据签名需要检查其保护范围。

证书解锁只检查 SM2 公钥匹配和包装密钥的解密结果，不验证证书信任链、有效期或吊销状态。
**解锁成功不表示接收者身份受信任、签名有效或文档内容完整。** 应用须独立执行证书策略，
签名与完整性保护按各自验证选项提供信任证据。CBC 填充检查采用常量时间比较，但不提供认证，
也不能单凭这一点宣称整个调用链不存在填充预言机；错误暴露和调用行为仍影响应用边界。
CBC 原语失败时清零整个输入缓冲并返回空明文，成功缓冲由消费方在使用后清零。

### 保护范围与变换限制

物化解密采取拒绝策略：发现 `OFDEntries.xml`，或可识别签名对删除/恢复条目的保护范围不能保留时，
返回 `ErrProtectedContent`。只比较受此次恢复影响的引用摘要，不重写签名 XML、签名值或时间戳，
不作完整验签结论。MD5/SHA1 仅用于保留原文件既有保护，不用于创建新的签名。
物化可能在末尾才发现保护冲突，通用 Writer 的部分输出必须丢弃。

只读解密视图允许保留完整性清单以供审计，物化解密仍拒绝该清单；追加/加密/重签流程
也不隐式重建旧完整性保护。普通签名缺省覆盖最终入口及签名列表，外部签名器返回值需绑定
当前原始 XML 字节。Relative 验证基础签名，普通写端可显式 Append 保留旧签名。
物理签署使用现有 §7.2 和附录 B 的 References 描述物理条目，不添加附录 D 的摘要字段。
外签 References 不作为逻辑文档闭包入口；只有被实际文档引用的明文所对应密文才计为可达。

## 签名扩展与完整性清单格式

附录 B/D 的 XSD 使用显式 `--gmt0099` / `WithGMT0099(true)` 扩展模式，基础 GB/T 33190
结构模式保留。GB/T 33190 §18.2.2 表68允许按场景选择引用摘要；原版附录 A 的 CheckMethod
枚举仅含 MD5/SHA1，项目内置基础 XSD 另接受 SHA256/384/512、SM3 及 SM3 OID，属于项目支持的扩展。
表 B.2 的 ImageLoc 类型误写 ST_Box，按图 B.2 的 ST_Loc 和“图片存储位置”
解释。图 B.2 的 Provider/ExtendData 是 Base64 子元素，ProtocolVer 才是属性。
图 D.1 为 OFDEntries，表 D.1 为 DocEntries，两者均接受并记录差异；表中若干字段
说明沿用了加密映射文字，ID 不用来推断加密操作。清单 FileList 不作为文档引用遍历入口。

完整性协议按 §7.4.2 要求对清单实施带时间戳的数字签名。表 D.1 文件列表本身没有逐条
摘要，清单签名通过不能单独证明所有文件内容未变；防夹带还须比较物理条目、可信清单及
从 OFD/加密入口遍历的引用闭包。遍历未完成时不能作“无夹带”的结论。

## 验证与回归入口

```text
go test ./internal/crypt ./pkg/crypt ./internal/parser ./pkg/validator ./pkg/analyzer -count=1
go test ./internal/container ./cmd/ofd-crypt ./cmd/ofd-validator ./cmd/ofd-analyzer -count=1
go test ./internal/signeddata ./internal/ses ./pkg/sign ./pkg/integrity ./cmd/ofd-creator ./cmd/ofd-wasm -count=1
go vet ./internal/crypt ./pkg/crypt ./internal/parser ./pkg/validator ./pkg/analyzer ./cmd/ofd-crypt ./cmd/ofd-validator ./cmd/ofd-analyzer
git diff --check
```

样本与向量见 [`testdata/crypt`](../testdata/crypt/README.md)。元数据 XML 样本用固定字节模拟密文，
仅用于结构检查；密码学原语使用独立 OpenSSL 向量验证。完整包回归检查签章包恢复后的逐条字节、
签名摘要和数学签名结果，完整性回归分别检查信任证据、物理集合、引用闭包及内容覆盖。
ofdrw 的固定版本互通命令见 [互通测试说明](../testdata/crypt/ofdrw/README.md)，
WASM 的网页及运行时验证入口见 [阅读器说明](../cmd/ofd-wasm/web/README.md)。
