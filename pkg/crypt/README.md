# OFD 加密元数据检查与加解密

`pkg/crypt` 实现 GM/T 0099-2020 的 `Inspect`、`Encrypt`、`Decrypt` 和 `Open`：
支持口令/SM2 证书、多接收者、独立条目密钥、多层解密、只读解密视图及明文 OFD 输出。
加密写端支持整包、显式条目或页面范围、逐条独立 FEK/IV，以及在已有线性加密链外追加一层。
命令行见 [`ofd-crypt`](../../cmd/ofd-crypt/README.md)。
标准条款、格式歧义、互通范围和已知限制见 [实现约定](../../docs/OFD-CRYPTO.md)。

```go
info, err := crypt.Inspect(ctx, "encrypted.ofd", crypt.InspectOptions{
    Mode: crypt.ModeCompat,
})
if err != nil {
    // info 可能包含检查失败前取得的部分信息，不能据此处理文档。
    return err
}
if info.Encrypted {
    // info.State == "locked"；Operations 列出可见的加密操作。
}
```

输入支持文件路径、`[]byte` 和 `io.Reader`。路径输入使用随机访问读取，不整体复制；
`io.Reader` 按输入预算读取，调用方负责关闭。检查不会要求 `OFD.xml` 存在或可解析。

## 加密

```go
var encrypted bytes.Buffer
report, err := crypt.Encrypt(ctx, "plain.ofd", &encrypted, crypt.EncryptOptions{
    Recipients: []crypt.EncryptRecipient{
        {UserName: "张三", Password: []byte("口令一")},
        {UserName: "李四", Password: []byte("口令二")},
    },
    KeepRoot: true,
})
if err != nil {
    // 发生错误时丢弃 encrypted。
    return err
}
```

`Recipients` 至少一个，每项提供非 nil UTF-8 `Password` 或 DER `Certificate`，二选一。
同次操作的所有接收者使用同一种方案，不能混合口令与证书。证书必须包含有效 SM2 公钥；
KeyUsage 非零时必须含 KeyEncipherment 或 DataEncipherment。加密不验证证书信任/有效期/吊销，
用户名称与 User/Owner 声明角色不提供额外认证。调用方须在加密前执行自己的证书使用策略。

`Entries=nil` 默认加密全部文件，包括 `OFD.xml`；`KeepRoot=true` 将主入口保留为明文。
非 nil 的 `Entries` 选择精确、唯一的 ZIP 相对路径，例如 `Doc_0/Pages/Page_0/Content.xml`，
空列表、目录、缺失条目和不规范路径被拒绝。`Entries` 按精确列表执行。
已选明文从输出中删除，不生成占位内容；未选条目按原始字节保留。

每次操作使用 `crypto/rand` 生成新的 16 字节 FEK 和 IV，所有接收者分别包装同一 FEK。
默认沿用已互通的一次操作共用 FEK/IV 格式，IV 在各 UserInfo 中显式记录；
相同前缀跨条目会暴露 CBC 相同前缀关系。`IndependentKeys=true` 为每个条目分别生成
随机 FEK/IV 和 `DecryptSeedLoc`，通用 seed 只用于映射表；各 seed 使用本次操作的同一组接收者。
两种形式均为 SM4-CBC，不提供认证。独立 seed/追加层尚未通过第三方互通验证。

独立 seed 增加接收者元数据和解包次数，预算按 seed 累计。超过 31 个条目的默认加密映射包
在读取时需要显式增加 `MaxCredentialAttempts`（缺省 32），并按 seed 数量设置 `MaxRecipients`。
读写端各自遵守调用方预算，不自动放宽限制。

控制文件位于自动避让既有名称的 `Encrypt_N/`，映射路径使用包内绝对路径。
默认加密映射表以隐藏原路径；`PlaintextMap=true` 输出可读映射。
`EncryptReport` 在完整输出后返回，不含密钥。加密没有确定性模式：重复调用应生成不同密文。
已有加密入口且未指定 `Append` 时返回 `ErrAlreadyEncrypted`。已有完整性清单需要先独立处理。

### 追加加密层

```go
report, err := crypt.Encrypt(ctx, "inner.ofd", output, crypt.EncryptOptions{
    Recipients: []crypt.EncryptRecipient{{UserName: "外层用户", Password: outerPassword}},
    IndependentKeys: true,
    Append: &crypt.DecryptOptions{Password: innerPassword},
})
```

`Append` 先用旧凭据完全解锁并执行物化保护预检，再对原始物理包创建新层。
旧层使用不同凭据时可提供 `Append.Resolve`；`Append.Limits` 由外层 `EncryptOptions.Limits`
统一覆盖。追加要求规范入口名 `Encryptions.xml` 和单一线性链；新 ID 自动避让，新 `Relative`
指向上一次操作。旧操作及扩展节点的原始 XML 字节保留，只在根结束标签前插入新记录。
受入口签名、完整性清单或无法保留的保护范围约束时失败。

`Entries` 在追加时指物理 ZIP 路径，默认选中除明文入口以外的所有文件，包括旧 seed/map/密文。
旧层的内容按原字节包裹，不先解密后重新加密；`KeepRoot` 仅保留当前物理 `OFD.xml`，不恢复它。
输出依然需要内外各层凭据。多分支链和页面范围追加目前返回错误；兼容旧包可在 `Append`
设置 `ModeCompat/MissingScheme`。元数据、接收者和映射预算对旧层预检与新层写入累计。

### 页面范围

```go
report, err := crypt.Encrypt(ctx, "plain.ofd", output, crypt.EncryptOptions{
    Recipients: []crypt.EncryptRecipient{{UserName: "用户", Password: password}},
    Pages: &crypt.PageSelection{Document: "Doc_0/Document.xml", IDs: []string{"1", "2"}},
    IndependentKeys: true,
})
```

`Pages` 与 `Entries/Append` 互斥，页面用文档内 ID 选择。依据 C.1 计算页面内容、所用模板、
页面注释及其资源文件；匹配页面印章时同时包含签名 XML、签名值和外部印章数据。
资源索引按完整文件加密，其列出的字体、媒体和色彩配置全部纳入；文档的 PublicRes/DocumentRes
也整体纳入，覆盖复合对象等间接资源依赖。不会拆分资源索引或改写原文档。
共享资源可能使其他页面同时无法使用；报告用 `SharedResources=true` 标明此策略，
`SelectedEntries` 给出实际选择清单。该标记不表示已经统计出所有受影响页面。

缺失文件、未知模板、重复页面 ID、模板循环、路径越界及不支持的 XML 命名空间均失败。
资源路径按 GB/T 33190 §7.9 的 `BaseLoc` 解析。基础入口、文档结构、注释目录、附件、
自定义标签及历史版本不因页面选择自动加密；需要覆盖这些数据时选择整包或显式范围。

## 解密

```go
var plaintext bytes.Buffer
report, err := crypt.Decrypt(ctx, "encrypted.ofd", &plaintext, crypt.DecryptOptions{
    Password: []byte("打开口令"),
    UserName: "接收者名称", // 可省略；省略时受限尝试 seed 中的接收者
})
if err != nil {
    // 必须丢弃 plaintext：发生错误时输出可能不完整。
    return err
}
// report.State == "unlocked"；plaintext.Bytes() 是明文 OFD，可交给转换或校验接口。
```

`Password` 是不作归一化的 UTF-8 字节；`nil` 表示未提供，非 nil 的空切片表示空口令。
内部按 SM3 KDF 派生 16 字节 KEK，用 SM4-CBC/PKCS#7 解包 FEK，再恢复条目。
调用方仍拥有口令切片；库不会修改或清零调用方的数据。

证书方案使用 `PrivateKey: key`，其中 `key` 可为 `*sm2.PrivateKey` 或实现
`crypto.Decrypter` 的密钥服务适配器。库检查接收者证书公钥与 `key.Public()` 匹配，
向 `Decrypt` 传入 `crypto/rand.Reader`、ASN.1 SM2Cipher 和 `sm2.ASN1DecrypterOpts`。适配器应执行完整 SM2
解密及 C3 校验；密钥服务错误保留 `errors.Is` 可识别性。这里不判断证书信任、有效期或吊销，
以便解开历史文档；这些结论需在应用的信任策略中单独处理。

**解锁成功不表示证书受信任、签名有效或文档未被篡改。** 应用须单独执行证书使用策略，
签名与完整性结论分别通过 `pkg/sign` 和 `pkg/integrity` 取得。硬件适配器可在自己的
`Decrypt` 实现中执行设备策略；当前 gmsm 软件 SM2 解密不使用传入的随机源。

严格模式要求 ASN.1 SM2Cipher；compat 模式还接受带非压缩点前缀 `04` 的裸 C1C3C2，
并报告 `encryption.sm2_raw`。不会尝试 C1C2C3、压缩点或任意未知编码。
对于缺少 `EncryptCaseId` 的已知兼容包，必须同时指定：

```go
crypt.DecryptOptions{
    Mode: crypt.ModeCompat,
    MissingScheme: crypt.PasswordScheme, // 证书方案使用 crypt.CertificateScheme
    Password: password,
}
```

`MissingScheme` 不能覆盖显式声明的未知或不同方案；缺少 `EPath` 的映射返回 `ErrUnsupported`。
多次操作按 `Relative` 依赖从后层向前层恢复，XML 顺序不决定解密顺序。
没有依赖关系却重叠的恢复范围或跨操作密文依赖被拒绝。每项映射可用独立 `DecryptSeedLoc`，
独立 seed 可以使用不同于通用 seed 的口令/证书方案。操作可覆盖全部或部分条目，也可使
`Path==EPath`；解密后占位内容被恢复内容替换，仅清理该操作明确引用的控制文件和密文。
`Path/EPath` 仅大小写不同也可按精确映射恢复（ofdrw 签章样本使用这种命名）；
只有该物理名称确为本条映射密文时才允许，无关条目大小写冲突仍失败。

输出逐条写入 ZIP，不重新编码文档 XML，不创建明文临时文件。字节预算同时覆盖未加密
条目和恢复条目；空目录条目不写回。`DecryptReport` 仅在完整 ZIP 写出后返回，包含接收者、
操作/方案、输出数量和字节数，以及采用的兼容诊断，不含 FEK、KEK 或口令。
多操作结果在 `DecryptReport.Operations` 中按处理顺序返回；顶层 `OperationID/Scheme/Recipient`
保留首个处理操作的摘要语义，`DecryptedEntries` 为各操作恢复次数之和。
各操作的 `Keys` 记录实际解开的 seed、方案和接收者；操作级 `Recipient` 仅描述通用 seed，
通用 seed 未用于解密时可为空，独立 seed 的结果以 Keys 为准。

`io.Writer` 无法回滚。文件调用方应先写临时文件，处理 `Decrypt` 和文件 `Close` 的错误，
两者成功后再替换目标；失败时删除临时文件。不要在解密前截断输入文件或直接覆盖目标。

## 只读解密视图

```go
view, err := crypt.Open(ctx, "encrypted.ofd", crypt.OpenOptions{
    DecryptOptions: crypt.DecryptOptions{Password: password},
})
if err != nil {
    return err
}
defer view.Close()

// 直接使用视图，过程中不创建明文 ZIP。
err = converter.PDF(ctx, view, output)
// v 为已有 *validator.Validator。
report := v.Validate(ctx, view, "encrypted.ofd")
results, err := sign.Verify(ctx, view, verificationOptions)
reader, err := webreader.OpenInput(view, webreader.OpenOptions{})
// reader 使用完毕后单独 Close。
```

`Open` 建立不可变逻辑索引：按层读取元数据，验证已解锁条目的填充、大小和 CRC，随后立即
清理条目明文缓冲；之后每次读取按需重新解密，不缓存整份明文 ZIP 或明文资源。
这是打开时预检、读取时解密的视图，尚未实现按需跳过打开阶段的全部条目预检。

`View.Read/ReadContext` 返回调用方拥有的字节副本；`Entries` 返回逻辑条目快照。
逻辑条目的 `Virtual=true`，`UncompressedSize/CRC32` 描述明文，压缩大小和方法为零。
`OpenPhysicalPackage` 可取得独立原始容器读取租约，保留 ZIP 压缩元数据，用后需关闭。
它供容器审计使用，不是解密读取入口。

视图传给 parser、converter、sign、analyzer、webreader 或 validator 时，消费方取得独立租约。
消费方关闭不会关闭原视图；视图所有者关闭后不再允许新读取/新租约，已取得的租约继续可用。
最后一个租约关闭时释放输入文件并清零自持 FEK/IV。视图不会保存调用方的口令、私钥句柄或
resolver 闭包；读取时不再调用私钥服务。创建上下文仅控制打开阶段；后续使用
`ReadContext` 或消费方的上下文。输入字节切片/底层文件在视图存活期间不得修改。

不同层或条目使用不同凭据时配置 `Resolve`：

```go
crypt.DecryptOptions{
    Resolve: func(ctx context.Context, request crypt.CredentialRequest) (crypt.Credentials, error) {
        // 应用按 OperationID、DecryptSeedLoc、Scheme、Recipients 查找凭据。
        if password, ok := passwordsBySeed[request.DecryptSeedLoc]; ok {
            return crypt.Credentials{Password: password}, nil
        }
        return crypt.Credentials{}, crypt.ErrCredentialsRequired
    },
}
```

存在 resolver 时，每份 seed 的凭据由它决定，不自动回退到顶层口令。请求的 seed 路径
是规范 ZIP 相对路径；结果可指定 `UserName` 和仅用于缺失方案的 `MissingScheme`。
同一操作内相同 seed 仅恢复一次密钥。超时/取消及密钥服务错误不降级为缺少凭据。

`OpenOptions.AllowPartial=true` 仅允许缺少凭据时保留部分视图：

- `EncryptionInfo().State` 为 `locked/partially_unlocked/unlocked`（普通明文输入为 `none`）。
- 已知锁定路径的条目带 `Locked=true, SizeUnknown=true`，读取返回 `ErrLocked`，即使原路径有
  占位文件也不会回退。错误口令、损坏密文和坏元数据仍失败。
- 任一映射表无法解开时，未知路径可能遮蔽其他条目，`Info.Opaque=true`，逻辑枚举为空且所有
  逻辑读取返回 `ErrLocked`；原始容器审计仍可用。
- 部分视图不能交给普通解析/渲染/验签链，也不能物化输出；validator 可以报告未完成状态，
  对可安全读取的主入口仍识别长期保存 profile。`Decrypt` 始终要求完整解锁。

`Validator.Validate` 先检查原始物理条目，再检查完全解锁的逻辑内容，并保留原始加密状态；
OFD-A/OFD-H 的禁止加密规则不会因视图已解锁而被绕过。只有完整解锁且必要检查通过时，
普通 OFD 才可能得到有效结论。物化后的独立明文 OFD 是新的输入，需单独校验。

### 签名与完整性保护

先签名后加密的包恢复相同的路径和原始文件字节后，既有签章仍可独立验证。
库会检查可识别的 OFD `Signature` XML 对恢复/删除条目的引用：
恢复内容与引用摘要不符、摘要算法无法判断，或引用了将删除的密文/加密元数据时，返回
`ErrProtectedContent`。这一保护检查支持 MD5、SHA1、SM3 及 SM3 OID；不替代完整验签，
既有未受此次变换影响的无效签名也不会自动变成有效。签章接受仍需调用 `sign.Verify`。

存在物理或恢复后的 `OFDEntries.xml` 时拒绝物化，需在专门的完整性保护流程中重建。
只读视图可以保留该清单供 [`pkg/integrity`](../integrity/README.md) 审计；视图解锁成功
不表示清单、时间戳或文件内容已通过完整性验证。
CBC 填充正确及解密成功不能证明资源未被篡改；没有认证信息时不能可靠区分所有错误口令和
损坏密文。内部持有的 FEK/KEK 与明文条目缓冲使用后尽力清零，不保证擦除运行时或密码库的副本。

## 元数据结果

- `Inspect` 的 `Info.State` 为 `none/locked`；视图快照还可为 `partially_unlocked/unlocked`。
  `none` 只表示没有根目录解密入口，不代表 OFD 有效。
- `Operation.MapState=plaintext` 时 `Entries` 提供规范化后的 ZIP 相对路径。
  `encrypted` 表示映射表为不透明二进制，未验证解密结果；受保护路径和条目数未知。
  `unknown` 表示检查未完成；`Deferred=true` 表示前层元数据需先解开后层。
- `Seed.Scheme` 保留 `EncryptCaseId` 原值。未知方案返回诊断；兼容模式下缺失时保持为空，
  不根据包装密钥的形状猜算法。
- `Recipient` 提供用户名称、角色、是否采用缺省 IV 以及可选的证书 SHA-256 指纹。
  结果不返回包装密钥、IV 原始值或完整证书，也不判断证书信任。
- 默认 `strict`；`compat` 接受已确认的数字 ID、遗漏方案、遗漏扩展节点、无命名空间及
  入口文件名大小写变体，并记录 `Diagnostics`。其他命名空间、路径歧义和重复单值字段仍失败。
- 标准图表中的 `EncryptEntries/EncryptedEntries` 两种拼写均接受并记录别名诊断。
  `EPath` 缺失时保留为空并诊断，不推断为原路径。

`Limits` 的零值采用有限默认值，负数返回配置错误：

| 字段 | 默认值 | 计数方式 |
|---|---:|---|
| `MaxInputBytes` | 512 MiB | 输入 ZIP 字节数 |
| `MaxEntries` | 10,000 | 物理 ZIP 条目，含目录 |
| `MaxMetadataBytes` | 4 MiB | 单次读取元数据的解压后字节数，含 `.dat` |
| `MaxTotalMetadataBytes` | 32 MiB | 本次检查实际读取元数据的总字节数；重复读取也计入 |
| `MaxXMLNodes` | 100,000 | 每份读/写元数据 XML，以及被处理的主入口/签名 XML 的元素数 |
| `MaxXMLDepth` | 64 | 同上各份 XML 的元素嵌套深度 |
| `MaxOperations` | 64 | 加密操作总数 |
| `MaxRecipients` | 1,024 | 不同密钥描述中的接收者总数 |
| `MaxMappings` | 10,000 | 已读取的明文映射条目总数 |
| `MaxEntryBytes` | 64 MiB | 加解密使用：单个输入条目解压后字节数及单个输出条目字节数，密文含填充 |
| `MaxTotalBytes` | 512 MiB | 加解密使用：输入 ZIP 解压总字节数和输出条目总字节数分别限制 |
| `MaxCredentialAttempts` | 32 | 整个打开/解密流程的接收者解包尝试总次数，跨层和独立 seed 累计 |
| `MaxLayers` | 32 | Relative 依赖链深度 |
| `MaxProcessedBytes` | 2 GiB | 视图预检及存活期间逻辑读取的累计处理字节数，含中间层和重复读取；原始容器审计使用审计方预算 |

元数据、映射和接收者预算也跨操作累计。页面范围读取的结构 XML 与新写控制文件共用元数据预算。
视图反复读取会消耗 `MaxProcessedBytes`，长会话
应按预期读取量设置上限；达到上限返回 `ErrLimitExceeded`，不会自动重置。必要时关闭并重新打开。

大小同时检查条目声明和实际读取量。读取元数据使用精确物理条目，不采用大小写回退；
加密包中重复路径或大小写碰撞会失败。XML 拒绝 DTD、重复单值元素及额外根节点。

错误使用 `errors.Is` 判断：

| 类别 | 含义 |
|---|---|
| `ErrInvalidMetadata` | 可见元数据、路径或其引用损坏/歧义 |
| `ErrLimitExceeded` | 输入、XML 或元数据处理超过预算 |
| `context.Canceled` / `context.DeadlineExceeded` | 调用已取消或超时 |
| `ErrLocked` | 消费 OFD 原文的接口遇到尚未解锁的包 |
| `ErrClosed` | 视图/租约已关闭，不再允许读取 |
| `ErrNotEncrypted` | 解密输入没有根目录解密入口，不自动复制普通 ZIP |
| `ErrAlreadyEncrypted` | 加密输入已存在解密入口，不隐式增加加密层 |
| `ErrUnsupported` | 未支持的方案/包装编码或缺少 EPath 等执行语义 |
| `ErrCredentialsRequired` | 对应方案所需的口令或私钥未提供 |
| `ErrDecryptFailed` | 未匹配到接收者、凭据不匹配或密文损坏 |
| `ErrProtectedContent` | 现有完整性保护或签名引用需要单独处理 |

`Inspect` 本身不会因正常锁定返回 `ErrLocked`。解析、转换、验签和引用闭包接口会返回
可由 `errors.As` 取得的 `*crypt.LockedError`；其 `Info` 为可见元数据，`Cause` 非空表示
元数据检查也未成功。分析器同时返回 `partial` 报告与此错误；校验器报告用法见
[ofd-validator](../../cmd/ofd-validator/README.md#加密文档)。

`Inspect` 始终检查输入包的可见状态，不缓存解锁会话。将 `Decrypt` 的明文输出再交给
`Inspect` 会得到 `none`，再交给现有转换、阅读、校验或验签 API 会按普通 OFD 处理。
使用 `ofd-crypt` 完成凭据输入和原子文件输出，再交给既有工具；`ofd-converter -password`
仍只用于 PDF，其他工具不自动解锁。

固定版本 ofdrw 2.4.0 的口令/证书、整包/局部默认加密映射已通过双向互通。
其明文映射处理存在已复现异常，详细证据和重跑方法见
[`testdata/crypt/ofdrw`](../../testdata/crypt/ofdrw/README.md)。可运行 Go 示例见
[`examples_test.go`](examples_test.go)。
