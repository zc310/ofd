# ofd-signer-demo

`ofd-signer-demo` 是 [`github.com/zc310/ofd`](https://github.com/zc310/ofd)
提供的**最小演示签名器**，用于说明 `ofd-creator merge --sign-cmd` 协议需要
实现什么，以及 `SignedValue.dat` 的 SES 结构。

## SES 结构（V4）

输出遵循 GM/T 0031 的 V4 电子印章结构，与 `test/testdata/999.ofd` 等真实样例一致：

```asn1
SES_Signature ::= SEQUENCE {
    tbsSign            TBS_Sign,
    certificate        OCTET STRING,
    signatureAlgorithm OBJECT IDENTIFIER,   -- 1.2.156.10197.1.501 (SM2-with-SM3)
    signData           BIT STRING
}
TBS_Sign ::= SEQUENCE {
    version     INTEGER,                    -- 4
    eseal       SESeal,
    timeInfo    GeneralizedTime,
    dataHash    BIT STRING,                 -- Signature.xml 的 SM3 摘要
    propertyInfo IA5String                   -- 签名 XML 包内路径
}
SESeal ::= SEQUENCE {
    esealInfo           SES_Seal_Info,
    cert                OCTET STRING,
    signatureAlgorithm  OBJECT IDENTIFIER,
    signData            BIT STRING
}
SES_Seal_Info ::= SEQUENCE {
    header    SES_Header,
    esID      IA5String,
    property  SES_ESPropertyInfo,
    picture   SES_ESPictrueInfo
}
SES_Header ::= SEQUENCE {
    id      IA5String,                      -- 固定值 "ES"
    version INTEGER,                        -- 4
    vid     IA5String                       -- 厂商标识
}
```

注意 `SES_Header.ID` 是**固定值 `"ES"`**，不是签名 ID；`SES_Header.Version` 与
`TBS_Sign.Version` 都是 `4`。把签名 ID 填进 `SES_Header.ID` 会导致其他工具报
`invalid ses header`。

## 来源标识

演示签名器把项目来源写入签名结构，便于确认签名是由本项目制作的：

- `--version` / `--help` 输出 `ofd-signer-demo 0.1.0 (github.com/zc310/ofd)`；
- 生成的 `SES_Seal_Info`：
  - `Header.VID = "ofd-signer-demo/0.1.0"`；
  - `ESID = "ofd-signer-demo-<sign-id>@github.com/zc310/ofd"`（`esID` 是厂商自定义标识）；
  - `Property.Name = "ofd-signer-demo 0.1.0 (演示印章, github.com/zc310/ofd)"`；
- `TBS_Sign.PropertyInfo = "/Doc_0/Signatures/Signature_<sign-id>.xml"`（签名 XML 包内路径）；
- 自签名证书主体和签发者均为 `ofd-signer-demo (github.com/zc310/ofd)`。

`internal/parser` 解析 `SignedValue.dat` 后可从 `SES.TBS.Seal.SealInfo.ESID`、
`SES.TBS.Seal.SealInfo.Property.Name` 和证书主体读到这些标识。

## 用途

它从标准输入读取 `ofd-creator` 生成的 `Signature.xml`，向标准输出写出
`SignedValue.dat`。运行时不读取任何密钥或证书文件，所有材料都在进程内生成：

- 现场生成一对 SM2 私钥/公钥；
- 用自签名证书封装公钥（`SM2WithSM3`）；
- 计算 `Signature.xml` 的 SM3 摘要作为 `TBS_Sign.DataHash`；
- 用 SM2 分别签署 `SES_Seal_Info`（印章内部签名）和 `TBS_Sign`（外层签名）；
- 生成一张 PNG 印章占位图（`SES_ESPictrueInfo.Type = "png"`）：红色圆环加「中」字，
  空白区域透明，尺寸 938×938（`SES_ESPictrueInfo.Width/Height` 与图元尺寸一致）。

与真实印章样例的差异：真实印章的 `esID`、`SES_Header.VID`、`Property.Name`
和证书来自厂商/CA，`SES_ESPictrueInfo.Data` 是真实印章图片；本演示器用固定的
厂商/项目标识和现场生成的占位图代替，其余字段类型与顺序保持一致。

## 让印章显示在页面上

阅读器只会在 `Signature.xml` 含 `StampAnnot` 时把 `SES_ESPictrueInfo` 图片绘制到页面。
`pkg/sign` 默认不写 `StampAnnot`（只验签），需要显示印章时用 `--sign-stamp`：

```bash
go run ./cmd/ofd-creator merge -o /tmp/signed.ofd --pages 1 \
  --sign-cmd /tmp/ofd-signer-demo --sign-stamp --verify-signatures test/testdata/hello.ofd
```

## 用法

```bash
go build -o /tmp/ofd-signer-demo ./cmd/ofd-signer-demo
go run ./cmd/ofd-creator merge \
  -o /tmp/signed.ofd --pages 1 \
  --sign-cmd /tmp/ofd-signer-demo \
  --sign-provider "OFD Signer Demo" \
  --sign-provider-version 0.1.0 \
  --sign-company "OFD Signer Demo" \
  --verify-signatures \
  test/testdata/hello.ofd
```

预期输出：

```
ofd-creator merge: 签名 sign-1：摘要有效，密码学签名有效
```

## 限制

这是演示程序，**不能用于生产**：

- 证书是每次运行随机生成的自签名证书，没有信任链，重新运行会得到不同的证书和签名；
- 印章图片只是内嵌的 PNG 占位图（红色圆环加「中」字），不是有效签章；
- 私钥不落盘、不持久化，验证方无法确认印章归属；
- `--signatures drop` 后重签的文档只用于演示 `pkg/merge` 与 `pkg/sign` 的链路。

生产签名器需要接入受信任的证书、密钥管理（HSM/软证书）、时间戳与吊销信息，
并按 `Signature.xml` 中声明的 `SignatureMethod`/`References@CheckMethod` 选择算法。
