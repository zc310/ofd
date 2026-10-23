# OFD 完整性清单与防夹带

`pkg/integrity` 实现 GM/T 0099-2020 §7.4、附录 D：检查或生成根目录
`OFDEntries.xml`，验证其带时间戳的 SM2/SM3 SignedData 签名，并分别检查物理文件集合、
引用闭包和文件内容的签名覆盖。CLI 见 [ofd-crypt integrity](../../cmd/ofd-crypt/README.md)。

## 验证

```go
report, err := integrity.Verify(ctx, "protected.ofd", integrity.Options{
    Verification: sign.VerificationOptions{
        Trust:          &sign.CertificateTrustOptions{Roots: roots},
        TimestampTrust: &sign.CertificateTrustOptions{Roots: tsaRoots},
        Revocation:     &sign.CertificateRevocationOptions{CRLs: crls, Issuers: issuers},
    },
})
if err != nil {
    return err
}
if report.Status != "valid" {
    // 检查 report.Checks、report.Signature.Validation 及文件清单。
}
```

输入接受原始物理 ZIP 的路径、`[]byte` 或 `io.Reader`；调用方持有的 Reader 不被关闭。
逻辑视图不能替代物理容器审计，因此输入不接受已经解锁的视图。
零预算采用 `crypt.Limits` 的有限默认值；负数拒绝。单份签名/XML 还具有 4 MiB 硬上限，
SignedData 最多包含 128 张证书。不会下载证书、CRL、时间戳或外部引用。

`Inspect` 只检查清单结构及物理文件集合，存在清单时仍报告未完成。
`Verify` 的 `Status` 为：

| 状态 | 含义 |
|---|---|
| `absent` | 未发现规范文件名的完整性清单 |
| `invalid` | 已确认的签名、时间戳、文件集合或引用/内容保护失败 |
| `incomplete` | 缺少信任/吊销证据、解密凭据、完整闭包或文件内容保护，不能给出完整通过结论 |
| `valid` | 所有必要检查均完成并通过 |

调用方须先处理 `Verify` 的错误。错误口令、预算超限或取消可能返回错误及部分报告，
此时 `verification` 检查记录中止原因，报告为 incomplete；此前已确认 invalid 的结果保留。
已经确认的元数据错误仍为 invalid，不会留下整体 valid 的部分报告。

已执行的检查分别记录为 `manifest.files`、`manifest.signature`、`references`、
`content.coverage`；已确认的文档签名错误还会记录 `content.signature`。
`Unlisted/Missing` 表示物理集合与 FileList 的差异，`Unreferenced` 表示充分遍历后的未引用文件，
`Unprotected` 表示缺少完整通过验证的内容签名覆盖。闭包不完整时，不能把暂时的
`Unreferenced` 集合当作可删除文件清单。

引用闭包中的已解释 XML 必须通过所选模式检查；未知结构、命名空间或模式失败记为未完成，
不会因为已知字段遍历结束而宣称闭包完整。普通 `PackageReferences` 同样采用这一规则，
GM/T 0099 文档使用 `validator.WithGMT0099(true)` 选择扩展模式。

附录 D 的 FileList 不含逐文件摘要。**清单签名和时间戳有效，也不能单独证明文件内容未变。**
完整通过还要求现有 OFD 签名覆盖文件内容；其证书信任、吊销、时间戳及 Relative 依赖均须完成。
经过完整验证的签名 XML 和签名值作为已验证的协议数据计入保护，清单与自身签名值亦按协议处理。
使用普通数字签名时，`sign.Options{Type: "Sign"}` 默认保护文档目录、最终 OFD.xml 和签名列表；
可在包内容签署完成后再追加清单，已有文件字节不再变化。
多人签署使用 `Append: true` 时，最后一次还需 `LockSignatures: true` 固定最终列表；
未锁定的列表仍可修改，清单不会替代其内容摘要保护，`content.coverage` 保持 incomplete。

## 生成

```go
report, err := integrity.Protect(ctx, "signed.ofd", output, integrity.ProtectOptions{
    ID:          "integrity1",
    CreatorName: "my application",
    Sign: func(ctx context.Context, manifest []byte) ([]byte, error) {
        // 外部签名器/硬件适配器提供证书、crypto.Signer 和时间戳服务。
        return sign.CreateDigitalSignedValue(manifest, certificate, signer,
            sign.DigitalSigningOptions{
                Timestamp: timestampService,
            })
    },
})
```

`Sign` 收到最终清单的原始字节副本，返回 GB/T 35275 detached 或 attached SignedData。
签名必须绑定整份清单，并附加绑定签名值的有效 RFC 3161/GB/T 20520 时间戳。
创建时预检数学签名、时间戳绑定、证书用途/有效期及时间顺序；提供信任策略时也检查该策略。
时间戳服务及生产密钥由调用方接入，库不内置或创建私钥。

返回 `Status=signed` 只表示生成完成，不表示内容签名覆盖或证据齐全。随后应使用 `Verify`。
不会删除夹带文件或自动重签文档，已经存在清单时返回 `ErrProtected`。
清单列出输入的全部物理文件；新增清单和自身签名值由协议路径确定，不要求自我摘要。
其他文件，包括已列入清单的文件，仍须从文档/加密入口进行独立引用遍历。

输出逐条写 ZIP，保留输入文件原字节。发生任何错误均须丢弃部分输出；文件流程使用 CLI
的同目录临时文件与原子提交，避免失败时覆盖已有文件。

## 加密包与格式约定

加密包可先生成最终密文，再附加清单。`Verify` 不提供 `Decrypt` 时，引用和内容检查保持未完成；
提供 `Options.Decrypt` 后建立只读解密视图，复用 validator 的文档引用遍历，并将各层
seed/map/密文及占位条目按 Encryptions.xml 的引用关系计入物理闭包。清单自身的 FileList
不会作为普通文件的遍历入口，避免用清单“洗白”夹带。

加密包中对逻辑明文的签名不能自动证明物理密文和加密控制文件的字节完整性。
可在保留主入口明文、没有已有签名入口的包上使用 `sign.Options{Type: "Sign", Physical: true}`，
随后追加清单。`Verify` 先检查外层物理签名，只有 FullyValid 的实际引用计入物理内容覆盖，
再解密并独立检查文档闭包。外签 References 不用来认定文档可达性；映射表中的密文也只有在
对应明文属于文档闭包时才计为可达，避免通过签名或加密映射隐藏无关文件。
根入口和外签元数据不允许被解密映射覆盖。

JSON 的 `physical_signatures` 保留每份外签的结果，`Scope=physical`；检查项
`physical.signature` 单独报告其信任与摘要结论。物理内容已受信但没有解密凭据时，
`content.coverage` 可通过，整体仍因 references 未完成而为 incomplete。
混合内外签名编排和加密主入口的外签发现尚未支持。物化解密、再加密和重签仍拒绝
会破坏现有保护的变换；只读视图允许保留清单开展检查。

图 D.1 的 `OFDEntries` 与表 D.1 的 `DocEntries` 根拼写均接受，后者产生诊断。
表 D.1 的 ID、CreatorName、Version、CreationDate 说明存在复制错误；ID 用作本清单标识，
不据此猜测某个加密操作。File.ID 必须为唯一 xs:ID，FileLoc 按包内路径解析，
生成时使用包内绝对路径；重复路径、大小写歧义、目录目标、路径越界和 DTD 均拒绝。
文件列表不添加标准中不存在的摘要字段。实现与回归尚不代表第三方完整性协议互通或外部认证。

回归入口：

```text
go test ./internal/signeddata ./internal/ses ./pkg/integrity ./pkg/sign ./pkg/validator ./cmd/ofd-crypt -count=1
```
