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

`PropertyInfo` 目前支持指向受保护 `Signature.xml` 的包内路径；其他厂商元数据
编码可解析保留，但保护范围会报告未完成。时间戳支持 CMS/TSTInfo 的签名值绑定、
TSA 专用关键 EKU、ESS 签名者证书摘要绑定、信任及整条证书链吊销检查；
不带时间戳的签章是合法情况。

时间戳回归目前使用测试 CA 和独立编码的 CMS/TSTInfo 令牌；真实第三方 TSA
服务的互操作尚未验证，后续需提供服务地址、认证配置（如需）及可信证书/吊销证据进行实测。

存在时间戳时，`timestamp.signing_time` 还按 §7.3 h）4）引用 f）的要求，检查
签章者证书在时间戳时刻的有效期和吊销状态。签章后、时间戳前的证书吊销会使
完整结论失败；缺少该时刻可验证的吊销证据则报告未完成。

该实现不包含 SM4 文档加密或 RSA/ECDSA OFD 文档验签。标准支持范围及验证配置
应随使用场景声明；实现与回归测试不等同于外部符合性认证。
