# OFD 签署与验证

`Sign` 为 OFD 文档计算引用摘要、生成 `Signature.xml`，把原始 XML 字节交给外部命令，
并将命令输出的 `SignedValue.dat` 写回包中。外部命令协议见
[ofd-creator](../../cmd/ofd-creator/README.md#外部命令重签名)。

`CreateSeal`、`CreateSignedValue` 提供 GB/T 38540-2020 SES V4 的 SM2/SM3 编码与签署。
证书与私钥由调用方提供；`CreateSeal` 的 `Width`、`Height` 是整数毫米，不是图片像素。
`SignerCertificates` 设置获授权签章者的完整 DER 证书；也可用 `SignerDigests` 设置
`certListType=2` 的 `{Type, Value}` 列表，支持 SM3、SHA256 及其 OID。
两种列表互斥，缺省授权制章者证书。印章名称按 UTF8String 编码。

`CreateSignedValue` 接收受保护的 `Signature.xml` 原始字节和该文件的包内路径。
签署前检查印章签名、授权、有效期、证书用途及证书与私钥匹配。`SigningOptions.Validate`
用于接入签署前的信任链、证书吊销及印章状态检查；调用方要求完整签署流程时必须提供。
`SigningOptions.Timestamp` 接收新生成的签名值字节，返回 DER TimeStampToken；可在回调中
调用带超时的 TSA 客户端。返回整个时间戳响应包或与签名值不匹配的令牌会被拒绝。
`Extensions` 支持非关键厂商扩展，`critical=false` 在 DER 中省略；未知关键扩展不能得到
完整验证通过结论。

验证器保留第三方显式编码 `critical=FALSE` 的解析信息和原始签名字节，但按
GB/T 38540 §6.1.2.6、DER（X.690 §11.5）将完整格式检查 `ses.format` 判为失败。
例如仓库的 `999.ofd` 样例含此非规范编码，其两层数学签名仍可通过验证。

```go
seal, err := sign.CreateSeal(sign.SealParams{
    Provider: "example", ESID: "seal@example", Name: "示例印章",
    PictureType: "png", PictureData: picturePNG, Width: 40, Height: 40,
    SignerCertificates: [][]byte{signerCertDER},
}, makerCertDER, makerKey, signingTime)
// 检查 err 后，以 signerCertDER/signerKey 签章；制章者与签章者可以不同。
value, err := sign.CreateSignedValue(signatureXML, seal, signerCertDER, signerKey,
    "/Doc_0/Signatures/Signature_sign-1.xml", signingTime, signingOptions)
```

上述代码中的证书、密钥、图片与 `signingOptions` 由应用提供。生成签名值时
SM2 对原文使用 SM3，独立于 OFD `References.CheckMethod`；引用摘要支持 MD5、SHA1、SM3。
引用摘要可为 SHA1，而 SES `DataHash` 仍使用 SM3。

## 完整验证

```go
results, err := sign.Verify(ctx, "document.ofd", sign.VerificationOptions{
    Trust: &sign.CertificateTrustOptions{Roots: roots, Intermediates: intermediates},
    Revocation: &sign.CertificateRevocationOptions{CRLs: crls, Issuers: issuers},
    SealStatus: sealStatusAtTime,
})
```

`roots` 为 `smx509.CertPool`，`intermediates`、`crls`、`issuers` 为 DER 字节列表。
`SealStatus(id, at)` 由应用根据可信的印章状态记录返回指定时刻的 `good`、`revoked`
或 `unknown`。验证器不会隐式加载系统根证书、下载 CRL 或查询印章服务。
`CurrentTime` 指定完整验签的验证时钟，缺省时在入口读取一次当前时间，组件与完整
策略共用此时刻。入口先检查签章时间非空且不晚于验证时刻；未来签章的完整结论为
`invalid`，`FullyValid()` 返回 false，即使其证书在声明的签章时间有效且两层签名正确。
标准结论中的证书信任和吊销判定以签章时刻为参考，同时要求 CRL 在验证时钟有效，
能使用签章之后发布的最新 CRL 判定历史吊销时间。当前支持完整、直接签发且带
有效 `NextUpdate` 的 CRL；增量 CRL、分区/间接 CRL 及未知关键扩展会报告证据未完成。
时间戳可通过 `TimestampTrust` 使用独立的信任根。

`Trust.CurrentTime` 可覆盖组件级证书链诊断的时间；`Revocation.CurrentTime` 可覆盖
组件级 CRL 诊断的证据时钟及吊销判定时间。这些覆盖影响 `Trusted`、`RevocationStatus`
等诊断字段，完整策略仍按签章时间判定历史证书状态，并按顶层 `CurrentTime` 检查
证据与时间顺序。`VerificationTime` 保留证书有效期所使用的签章时间语义。

每项结果包含文档体索引、签名 ID、引用摘要结果、两层签名结果，以及
`Result.Validation.Status` 和 `Checks`：

| 状态 | 含义 |
|---|---|
| `valid` | 必需的格式、签名、授权、印章有效期与状态、证书用途/信任/吊销、时间、原文绑定检查全部通过；存在时间戳时也已通过其验证。 |
| `invalid` | 至少一个检查已经确认失败。 |
| `incomplete` | 没有已确认失败的检查，但缺少必需证据、未支持关键扩展或尚不能解释原文保护范围。 |

接受签章时使用 `item.Result.FullyValid()`：只有完整状态为 `valid` 才返回 true；
结果为空、未完成或失败均返回 false。应先处理 `Verify` 返回的错误，并按业务要求
检查签名数量（空结果表示没有签名）。

`DigestValid` 表示引用摘要与 SES `DataHash` 是否匹配，`Result.Valid` 表示两层签名
检查是否通过；两者均不能代替完整结论。算法声明不一致会使完整结论为 `invalid`，
保护范围无法确认会使其为 `incomplete`。已确认的引用或原文摘要错误优先报告
`invalid`，不会被尚未确认的范围或未支持算法遮蔽。

两层签名的错误保存在 `Result.Seal.Error`、`Result.Outer.Error` 及对应的
`Validation.Checks` 中。SM2 摘要计算失败会保留具体原因（如 UID 过长），
摘要计算成功但签名不匹配时报告「SM2 签名验证失败」。

`PropertyInfo` 目前支持指向受保护 `Signature.xml` 的包内路径；其他厂商元数据
编码可解析保留，但保护范围会报告未完成。时间戳支持 CMS/TSTInfo 的签名值绑定、
TSA 专用关键 EKU、ESS 签名者证书摘要绑定、信任及整条证书链吊销检查；
不带时间戳的签章是合法情况。

时间戳回归目前使用测试 CA 和独立编码的 CMS/TSTInfo 令牌；真实第三方 TSA
服务的互操作尚未验证，后续需提供服务地址、认证配置（如需）及可信证书/吊销证据进行实测。

存在时间戳时，`timestamp.signing_time` 还按 §7.3 h）4）引用 f）的要求，检查
签章者证书在时间戳时刻的有效期和吊销状态。签章后、时间戳前的证书吊销会使
完整结论失败；缺少该时刻可验证的吊销证据则报告未完成。

## GM/T 0099 普通数字签名与附录 B

`Sign(ctx, input, output, options)` 的 `Options.Type` 缺省为 `Seal`；设置 `Type: "Sign"` 时外部命令或
`Options.Sign(ctx, signatureXML)` 回调应返回 GB/T 35275 的 SM2/SM3 SignedData。
外部命令仍从 stdin 读取完整 Signature.xml、向 stdout 返回 SignedValue.dat，
环境变量 `OFD_SIGN_TYPE` 表明类型。上下文可取消外部进程，命令输出受大小限制。
普通签名的输出会预检原文字节绑定和附加时间戳的数学有效性。

```go
err := sign.Sign(ctx, input, output, sign.Options{
    Type: "Sign",
    ProviderName: "my signer",
    Sign: func(ctx context.Context, signatureXML []byte) ([]byte, error) {
        return sign.CreateDigitalSignedValue(signatureXML, certificate, signer,
            sign.DigitalSigningOptions{Time: signingTime, Timestamp: timestampService})
    },
    Date: signingTime,
    ProtocolVersion: "1",
    Parameters: []sign.Parameter{{Name: "purpose", Type: "string", Value: "approval"}},
})
```

`CreateDigitalSignedValue` 供外部签名器和硬件适配器使用，不生成或保存私钥。
`crypto.Signer.Sign` 接收完整 DER 签名属性及 SM2 签名选项；可用 `DigitalSigningOptions.UID`
指定用户标识，包写入时相应设置 `Options.UID`。
`VerifyDigitalSignedValue` 只验证单独消息，完整 OFD 应调用 `Verify` 同时检查 References。

读取接受单一签名者的 v1 GB/T 35275 SignedData，也接受 SM2/SM3 的 CMS/PKCS#7 SignedData
外层 OID。支持 detached/attached 原文、contentType/messageDigest/signingTime 签名属性和
signatureTimeStampToken 非签名属性。原文使用文件原始字节，不规范化 XML。
未知算法、未解释的签名属性和其他非签名属性保持未支持/未完成，不作 RSA/ECDSA 验证。
每份数字签名/XML 硬上限 4 MiB，证书数量上限 128。

普通签名的 `Result.Kind=Sign`，`Result.Valid/Outer` 表示单层数学签名，`Seal` 不适用；
SES 继续保留两层签名语义。两种类型都用 `FullyValid()` 判断完整结论。
缺少证书信任根或离线吊销证据时为 incomplete；附加时间戳时还验证 TSA 专用关键 EKU、
ESS 证书绑定、时间顺序、TSA 信任/吊销及时间戳时刻的签名者证书。
时间戳失败与普通签名数学结果分别保留。普通文档签名的时间戳可选，完整性清单签名必须有。

附录 B 的 `ProtocolVersion/ExtendData/Parameters` 可写入提供者及扩展属性。
`ProtocolVersion` 写为 Provider 的 `ProtocolVer` 属性；`ExtendData` 按图 B.2 写为
`Provider/ExtendData` 子元素，内容为 Base64 编码，不接受同名属性形式。
`SealImage` 可把图片打入签名目录、写入 `Seal/ImageLoc` 并加入摘要，配合 Stamp/StampSeam
显示外观。读取 `Relative` 时验证悬空、循环和 32 层深度，并验证全部基础签名。
普通签名缺省覆盖文档目录、最终 OFD.xml 和当前签名列表，避免保护更新前的入口字节。
显式 `ReferenceOptions` 仍控制文档和入口范围，本次 Signature.xml/签名值不纳入自身引用。
validator 使用 `WithGMT0099(true)` 或 `ofd-validator --gmt0099` 启用独立扩展 XSD。

## 普通签名追加

设置 `Options{Type: "Sign", Append: true}`，在每个文档体的当前签名列表中增加记录，
将新签名放入独立的 `Signatures/<ID>/` 目录。ID 为空时自动选择未使用的 `sign-N`；
`Relative` 可指定本次依赖的已有签名 ID，该 ID 必须在每个文档体的列表中存在。
每份 SignedData 仍为单一签名者，多人签署由多个 OFD 签名记录表达。

```go
err := sign.Sign(ctx, input, output, sign.Options{
    Type: "Sign", Append: true,
    ID: "approval-2", Relative: "approval-1",
    Sign: externalSigner,
    LockSignatures: true, // 最后一次签署时固定全部列表记录。
})
```

首次签署也应使用 `Append: true`，以便继续追加。追加时新签名缺省保护文档目录、
OFD.xml、全部已有签名 XML、签名值及独立印章文件；不摘要仍需修改的当前签名列表。
尚未锁定的列表不能提供完整记录绑定，完整性覆盖检查会将其列为未保护文件。
最后设置 `LockSignatures: true`，将最终列表也纳入本次摘要，之后可生成带时间戳的完整性清单。
`Relative` 的完整验证会检查基础签名的信任和吊销证据；独立联签可不设置该属性。

追加会先检查全部旧签名的结构、数学签名及文件摘要；缺少信任证据不妨碍原字节搬运，
不表示旧签名已受信。既有入口保持原字节，旧签名、签名值和图片不会重命名或覆盖。
需要修改已被旧签名摘要保护的列表或 OFD.xml 时返回 `crypt.ErrProtectedContent`，
因此默认替换模式生成并保护了列表的签名、已锁定的追加结果均不能继续追加。
输入含 SES 签章、原始加密入口、完整性清单、路径歧义、无效旧签名或悬空/循环/超深 Relative 时拒绝追加。
追加的默认限制为输入与解压/输出各 512 MiB、单条目 64 MiB、10000 个条目，
签名 XML/签名值各 4 MiB；任何错误都要求丢弃部分输出。

命令行使用 [`ofd-creator sign --append`](../../cmd/ofd-creator/README.md#独立签署与普通签名追加)。

## 加密物理包签署

`Options{Type: "Sign", Physical: true}` 对加密完成后的物理文件签署，覆盖输入的全部文件、
更新后的明文 OFD.xml 和最终签名列表。使用标准 References/CheckValue 的 SM3 摘要，
不增加清单格式字段。外部签名器仍返回绑定 Signature.xml 的 SM2/SM3 SignedData，
命令环境变量 `OFD_SIGN_SCOPE=physical` 说明本次操作范围。

```go
err := sign.Sign(ctx, encrypted, output, sign.Options{
    Type: "Sign", Physical: true, Sign: externalSigner,
})
results, err := sign.VerifyPhysical(ctx, "signed-encrypted.ofd", verificationOptions)
```

加密时须保留 OFD.xml 明文，输入不能有已有签名入口或完整性清单。
物理模式不组合 Append、Relative、引用筛选或印模选项，签署后也不能再改写加密层。
OFD.xml 和外签元数据必须在解密后保持相同字节；映射覆盖这些路径会使完整性验证失败。
当前不支持内层逻辑签名与外层物理签名的混合编排，也不支持加密的主入口发现。

`VerifyPhysical` 无需解密凭据，结果标记 `Scope=physical`，仅验证物理签名及其引用。
`Verify` 接收完整解锁视图且识别到物理签名时，也明确读取其原始物理包。
`locked` 或 `partially_unlocked` 视图传给 `Verify` 返回 `crypt.ErrLocked`，不返回签名结果。
需要在无凭据时检查外签，应将原始包路径或字节交给 `VerifyPhysical`。
完整信任结论仍要求证书链、吊销及相应时间戳证据。随后用 `integrity.Protect/Verify`
增加带时间戳的清单并检查文档闭包；无解密凭据时闭包保持 incomplete。
物理签名覆盖范围不能代替文档可达性，已签署并加密的无关文件仍可判为夹带。
实现已通过临时测试 CA/CRL、独立编码测试时间戳及篡改回归，尚未做第三方 P4 产品互通。

GM/T 0099 的 SM4 文档解密与只读视图由 [`pkg/crypt`](../crypt/README.md) 提供；
可先恢复明文 OFD，再调用本包验签。本包不包含 RSA/ECDSA OFD 文档验签。标准支持范围及验证配置
应随使用场景声明；实现与回归测试不等同于外部符合性认证。

`Verify` 也可直接接收完整解锁的 `crypt.View`，通过独立租约读取原始明文字节，无需创建
明文 ZIP。部分视图返回 `crypt.ErrLocked`，不会把占位内容报成签名摘要不匹配。

完整性清单、时间戳与防夹带见 [`pkg/integrity`](../integrity/README.md)。已有清单的包拒绝重签，
应在专门流程中处理旧保护；原始加密包须显式使用上述受限物理签署模式。
