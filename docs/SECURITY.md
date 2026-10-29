# OFD 安全能力指南

`pkg/sign`、`pkg/crypt` 和 `pkg/integrity` 分别提供签名、加解密与整包完整性保护。
选择入口时，先确定要保护的是逻辑文档内容、物理 ZIP 文件，还是包内文件集合。

| 需求 | 包与入口 | 结果的含义 |
|---|---|---|
| 数字签名、电子签章及验签 | [`sign.Sign/Verify`](../pkg/sign/README.md) | 验证每份签名、实际引用的文件摘要及信任证据；接受签名使用 `Result.FullyValid()` |
| 追加或移除签名 | [`sign.Options.Append`、`sign.Remove`](../pkg/sign/README.md) | 改写签名记录，并检查此次改动能否保留已有保护 |
| 加密信息、加解密和解锁视图 | [`crypt.Inspect/Encrypt/Decrypt/Open`](../pkg/crypt/README.md) | 识别加密状态、生成密文或恢复可读取内容 |
| 清单、物理文件集合、引用闭包与内容覆盖 | [`integrity.Inspect/Protect/Verify`](../pkg/integrity/README.md) | 分别检查清单可信性、夹带/缺失文件、文档引用可达性及文件内容保护 |
| 加密物理包签署与验签 | [`sign.Options.Physical`、`sign.VerifyPhysical`](../pkg/sign/README.md#加密物理包签署) | 验证密文及加密控制文件的原始字节保护 |

`integrity` 组合签名验证、文档引用遍历和只读解密视图。三个包各自保留公开接口，
调用方按需求组合；标准条款、格式歧义和互通范围见 [OFD-CRYPTO.md](OFD-CRYPTO.md)。

## 分别判断验证结论

| 已取得的结果 | 可以确认什么 | 还需检查什么 |
|---|---|---|
| `crypt.Open/Decrypt` 成功 | 内容已按凭据恢复，通过了相应解密检查 | 签名、证书信任及内容完整性；SM4-CBC 本身不提供认证 |
| `sign.Verify` 某项 `FullyValid()` 为真 | 该签名及其引用、信任等必需检查完整通过 | 业务要求的签名数量、签名者与覆盖范围，以及其他包内文件 |
| 清单签名完整通过 | 清单原始字节及其时间戳、信任证据通过验证 | 物理集合、引用闭包及内容覆盖；附录 D 的 FileList 没有逐文件摘要 |
| `manifest.files` 通过 | 实际物理文件与清单列举的集合一致 | 清单可信性、文件内容、列入清单却不属于文档的夹带文件 |
| `references` 通过 | 引用遍历完成，未发现不可达的物理文件 | 文件内容是否有受信签名覆盖 |
| `content.coverage` 通过 | 物理文件均获得完整通过验证的签名覆盖或按协议计入保护 | 文件集合与文档引用闭包是否也通过 |
| `integrity.Verify` 返回 `Status="valid"` 且无错误 | 完整性协议的全部必要检查均完成并通过 | 应用自己的签名者、用途等业务策略 |

始终先处理错误，再读取接受结论。`sign.Verify` 的空结果表示没有签名，不能按
“所有返回项都通过”接受。`integrity.Protect` 的 `Status="signed"` 只表示清单生成完成，
之后仍应调用 `Verify`。`incomplete` 表示必要检查尚未完成，`invalid` 表示已确认失败。

## 先确定内容，再签署

合并、拆分、条目替换、水印和其他内容编辑应在签署之前完成。签名保护原始文件字节；
重新序列化 XML 或改动已签条目，都可能使摘要失效。

### 明文文档与整包清单

1. 完成文档内容编辑。
2. 调用 `sign.Sign` 签署。普通数字签名使用 `Type: "Sign"`，默认保护文档目录、
   最终 `OFD.xml` 和签名列表；自定义引用筛选时需自行确认覆盖范围。
3. 如需多人追加，从首次签署就使用 `Append: true`，末次设置 `LockSignatures: true`，
   固定最终列表。已锁定的列表不能继续追加。
4. 调用 `integrity.Protect` 追加带时间戳的清单。此时输入文件的内容字节保持不变。
5. 使用完整信任证据调用 `integrity.Verify`，检查清单、物理集合、引用和内容覆盖。

清单会列出输入中的所有文件，不会删除夹带文件或自动补齐内容签名。
完整验证发现 `Unreferenced` 时应检查文件来源；引用遍历未完成时，不能将该集合直接作为删除清单。

### 文档签名后加密

1. 完成内容编辑并签署文档。
2. 使用 `crypt.Encrypt` 生成加密包。
3. 接收方用 `crypt.Open` 建立完整解锁的只读视图，将视图交给 `sign.Verify`。

该流程用于保留并验证逻辑明文签名。逻辑签名不能自动证明密文和加密控制文件的物理字节
完整性；在密文外只追加清单，也不能补齐这部分签名覆盖。需要外层物理保护时，使用下一流程。

### 加密后保护物理包

当前支持的流程有明确前提：输入没有已有签名入口，加密时保留 `OFD.xml` 明文。

1. 完成内容编辑，使用 `crypt.Encrypt` 和 `KeepRoot: true` 生成最终密文。
2. 使用 `sign.Sign` 和 `Options{Type: "Sign", Physical: true}` 签署物理包。
   默认引用覆盖全部输入文件、更新后的明文入口和最终签名列表。
3. 使用 `integrity.Protect` 追加带时间戳的清单。
4. 使用原始包调用 `integrity.Verify`，提供信任证据和 `Options.Decrypt`，分别检查
   物理签名及解锁后的文档引用闭包。

物理签名可通过 `sign.VerifyPhysical` 在没有解密凭据时验证；缺少解密凭据时，
完整性验证的引用闭包仍为 `incomplete`。物理签名的引用不能让无关文件自动成为文档的一部分。
当前尚不支持内层逻辑签名与外层物理签名的混合编排，也不支持加密主入口的物理签名发现。

已有清单的包需要在专门流程中处理旧保护。物化解密、追加加密和重签不会隐式重建清单；
检查受保护加密包时使用原始物理输入和只读解密视图。

## 读取加密文档时选择输入

普通文档验签可以使用完整解锁视图：

```go
view, err := crypt.Open(ctx, "encrypted.ofd", crypt.OpenOptions{
    DecryptOptions: crypt.DecryptOptions{Password: password},
})
if err != nil {
    return err
}
defer view.Close()

results, err := sign.Verify(ctx, view, verificationOptions)
// 先处理 err，再按业务要求检查签名数量及每项 Result.FullyValid()。
```

整包完整性检查需要原始物理 ZIP，解密参数用于内部建立视图：

```go
report, err := integrity.Verify(ctx, "protected-encrypted.ofd", integrity.Options{
    Verification: verificationOptions,
    Decrypt:      &crypt.DecryptOptions{Password: password},
})
// 先处理 err；只有 report.Status == "valid" 才表示全部必要检查通过。
```

`verificationOptions` 中的证书根、吊销证据和时间戳信任由应用提供；电子签章还需要相应
印章状态证据。验证不会隐式访问网络获取证据。部分解锁视图不能用于普通文档验签。
视图所有者负责 `Close`；消费方使用独立租约。

各写出 API 出错时都必须丢弃 Writer 的部分输出。文件流程应写入临时文件，处理 API 和
文件关闭的错误后再替换目标。详细预算、凭据、签名协议和错误分类见三个包的指南。
