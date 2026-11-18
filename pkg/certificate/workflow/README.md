# 原件锁定与加注件工作流

本包落实 GB/T 36905—2018 §7.2 的**受限 Go 编排接口**，复用 `pkg/sign`、
`pkg/validator` 和条目编辑工具，不增加私钥存储、签名格式或 OFD 扩展属性。
本轮没有新增证照专用 CLI，也不宣称全面符合 GB/T 36905。

## 锁定与业务授权

GB/T 33190—2016 §18.1 规定允许继续签署时不保护签名列表。
本工作流把最终签名列表及整包文件纳入锁定签名（本次签名 XML 与签名值除外），
比“仅保护列表”的检测更保守。不根据 ID、提供者名称、证书 Subject 或最后一条记录
自动猜测哪个签名是锁定签名，验收时必须明确指定 `LockID` 和 `Role`。

```go
options := workflow.LockOptions{
    Sign: sign.Options{
        ID: "issuer-lock", ProviderName: "application",
        Date: signingTime,
        Sign: externalSigner,
    },
    Verification: verificationOptions,
    Authorize: authorizeIssuer,
}
err := workflow.SignOriginal(ctx, inputBytes, output, options)
```

`externalSigner(ctx, signatureXML)` 使用调用方的硬件或进程签署原始 XML，返回
`sign.CreateDigitalSignedValue` 支持的 SignedData。实际证书、私钥、信任根、
离线 CRL 及机构登记/授权证据由应用提供。**不得用测试 CA、演示签名器作为生产信任。**

工作流强制 `Type=Sign`、追加并锁定最终列表，禁止引用筛选、`Relative`、
物理签署及调用方手动设置 `Append/LockSignatures`。必须指定合法且未用过的签名 ID。
已有普通业务签名保留原字节，全部签名均须完整验证通过。

`Authorize(ctx, request)` 收到实际验签证书的 DER 副本、元数据快照及用途：

- `Original`：核对是否为颁发机构或其授权机构（§7.1 d)、§7.2 a)）。
- `Copy`：核对是否为 `certificateCopyCreator` 对应的制作方（§7.2 c)）。

相同 CA、相同证书 Subject、数学验签通过，都不等于机构角色已获授权。
nil 回调不默认授权；回调返回错误保留其错误链。证书链、吊销、绑定和摘要先通过，
才调用机构授权策略。回调不能替代密码学验证。

原件锁定失败不写入用户 Writer，但外部签名器已发生的调用不能撤销；
若新签名返回后发现证据、授权或时间失效，须丢弃该次签署结果。

## 制作加注件

```go
err := workflow.CreateCopy(ctx, originalBytes, output, workflow.CopyOptions{
    Source: workflow.VerifyOptions{
        LockID: "issuer-lock", Role: workflow.Original,
        Verification: sourceEvidence, Authorize: authorizeIssuer,
    },
    Lock: workflow.LockOptions{
        Sign: sign.Options{ID: "copy-lock", Date: signingTime, Sign: copySigner},
        Verification: allSignersEvidence, Authorize: authorizeCopyCreator,
    },
    Metadata: workflow.CopyMetadata{
        CreationTime: creationTime, Creator: "制作方", Cause: "用于本次指定事项",
        ExpiringDate: "2026-12-01",
    },
    Edit: func(ctx context.Context, input []byte) ([]byte, error) {
        // 调用 watermark.Add 等编辑工具，需显式保留业务签名：
        // watermark.Options{Options: replace.Options{Signatures: creator.SignaturePreserve}}
        // 返回新的OFD字节；不要覆盖原件，不要仅重打包而不加注。
        return editAndReturnNewBytes(ctx, input)
    },
})
```

流程为：验收源件 → 只移除指定锁定签名 → 写入加注件元数据 → 编辑 → 检查业务签名
未被删除/改写且仍有效 → 由制作方重新锁定 → 完整验签及角色授权 → 输出。
`Source.Role` 也可为 `Copy`，用于从已验收的加注件派生新加注件。

GB/T 36903—2018 §5.4 的四项信息在本工作流中**全部要求提供**，这是本工作流策略，
不是将标准中的所有可选项改成无条件必选：
`certificateCopyCreationTime`（RFC3339）、`certificateCopyCreator`、
`certificateCopyCause`、`certificateCopyExpiringTime`（`YYYY-MM-DD`）。
检测标准短名，拒绝重复；新产物使用标准英文名。截止日不能早于制作日。
不声称 RFC3339/日历日期是该标准唯一允许的表达；其他表达暂不在本工作流子集中。

编辑回调得到独立副本，不能改写已准备的 `OFD.xml`（含证照标识、元数据及文档入口）、
业务签名列表或签名负载。其他普通条目仍须实际发生修改；包目录时间/压缩改变不算加注。
**不自动判断加注外观是否可见、内容是否恰当或满足具体事项要求**，需由应用审核编辑器。
派生前与发布前均按源件声明检查 Copy（Export）、Edit、Signature 及有效期；
回调修改后的声明不能用来自授权。水印、注解的具体权限由所选编辑器继续检查。

如果业务签名保护了 `OFD.xml`，补入加注件信息就可能破坏它；如果业务签名保护了
将被修改的页面/注解/资源，也不能继续。工作流拒绝，不静默删除业务签名、不把失效签名
当有效证据，也不把普通编辑接口默认的 `drop` 策略当作证照加注件策略。

## 验收与安全边界

```go
err := workflow.VerifyLock(ctx, data, workflow.VerifyOptions{
    LockID: "copy-lock", Role: workflow.Copy,
    Verification: verificationOptions, Authorize: authorizeCopyCreator,
})
```

成功要求基础结构与元数据严格检查为零错误、零警告，指定签名覆盖整个现存包，
全部保留签名的 `FullyValid()` 为真，且业务授权回调通过。
`--certificate-metadata` 仍只做本地元数据检查，不会自动启用本工作流验收。
失败可通过 `errors.Is` 区分 `ErrOptions`、`ErrInvalid`、`ErrEvidenceRequired`、
`ErrAuthorization`；旧签名保护受影响时也可能返回 `crypt.ErrProtectedContent`。

当前范围：未加密、单文档体、多页、全部签名为普通 `Sign`。SES、加密物理包、
完整性清单需单独流程，不能据此推断为不符合证照标准；未知格式采取拒绝处理。
单次输入512 MiB、单条目64 MiB、解压总量512 MiB、10000条目，XML及签名元数据4 MiB，
XML节点100000、深度64；为保留快照与验收后发布会使用额外内存，峰值不等于输入上限。
所有输入为内存字节，输出由调用方负责存为新文件。业务回调不得并发修改共享输入或选项。
取消保留 `context.Canceled/DeadlineExceeded`，Writer短写返回 `io.ErrShortWrite`；
写入时 I/O 失败仍可能留下部分输出，调用方应丢弃。

不提供：登记/标识唯一性自动查询、内置授权名单、可见加注自动审核、GM 码或全面合规认证。
新接口的测试使用临时 CA、离线 CRL、不同角色证书及真实水印编辑；拒绝漏签、篡改、
丢弃业务签名、缺少证据、伪装机构角色、无实际编辑和锁定后继续追加。
