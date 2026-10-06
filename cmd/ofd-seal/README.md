# ofd-seal

`ofd-seal` 把一枚印章图片和元数据封装成符合 GB/T 38540-2020 的 SES 电子印章
文件（DER 编码的 ASN.1，通常命名 `Seal.esl`）。该文件可被 `ofd-creator` 的
`--sign-seal` 打入 OFD 包的签名目录，让阅读器从独立印章文件提取印章图片。

## 用法

```bash
go build -o /tmp/ofd-seal ./cmd/ofd-seal
/tmp/ofd-seal --image seal.png --name "示例印章" --esid "seal@example.com" -o seal.esl
```

选项：

| 选项              | 说明                                                        |
|-------------------|-------------------------------------------------------------|
| `-i, --image`     | 印章图片路径（png/jpeg/gif/bmp），必填                      |
| `--name`          | 印章名称，默认使用图片文件名                                |
| `--esid`          | 印章唯一编号（IA5 字符串），默认 `name@vid`                 |
| `--vid`           | 印章商标识，默认 `ofd-seal`                                 |
| `-o, --out`       | 输出文件路径，默认 `seal.esl`                               |
| `--key`           | 复用已有 SM2 私钥（PKCS#8 PEM 或 DER），需同时提供 `--cert` |
| `--cert`          | 复用证书（PEM 或 DER）；默认自签生成                        |
| `--key-out`       | 把新生成的私钥以 PKCS#8 PEM 写到指定路径                    |
| `--validity-days` | 证书与印章的有效天数，默认 3650                             |
| `--version`       | 输出工具版本                                                |

## 说明

- `SES_Header.ID` 固定为 `"ES"`，`SES_Header.Version` 与印章内部签名结构的
  版本均为 `4`；算法固定为 SM2 + SM3。
- 印章图片格式使用 Go `image.DecodeConfig` 得到的格式名（`png`、`jpeg`、
  `gif`、`bmp`）。
- 不带 `--key`/`--cert` 时每次运行生成新的随机 SM2 私钥和自签名证书，产出的
  印章仅用于演示与测试；生产环境应通过 `--key`/`--cert` 接入受信任的证书与
  私钥管理。
- `--esid` 和 `--vid` 会写入 SES 的 IA5 字段，包含非 ASCII 字符时会被拒绝。

## 生成测试用印章图片

`--generate` 只出图片，不封装成印章文件：

```bash
ofd-seal --generate -o seal.png
ofd-seal --generate --shape ellipse --size 600 -o seal-ellipse.png
```

图片由 `internal/sealimg` 渲染：顶部固定印「OFD 测试专用章」，底部固定印「非正式印章」，
中心是五角星、其下方小字「zc310/ofd」，外框为朱红双线（圆或椭圆），背景透明。

**这张图只能用于演示和测试。** 底部那行字是刻意印在图片里的——印章图片会被
单独复制传播，脱离本仓库之后就再也看不到文档里的免责声明，图片自带的标识是唯一
还留着的提醒。真实印章图片必须来自单位刻章备案，证书必须来自 CA；用本工具
生成的图去签正式文件属于伪造印章。

需要中文字体。字体按家族名从系统查找（覆盖 Linux/Windows/macOS 常见字体），
也可以用 `--font` 指定；指定的名字找不到或不含中文字形时会报错，不会静默换一个
字体——换出来的图里底字是方框。

## 与签名管线的衔接

```bash
/tmp/ofd-seal --image seal.png --name "示例印章" -o seal.esl
go run ./cmd/ofd-creator merge \
  --sign-cmd /tmp/ofd-signer-demo \
  --sign-seal seal.esl \
  -o /tmp/signed.ofd \
  --pages 1 \
  testdata/hello.ofd
```

`ofd-creator` 把 `seal.esl` 复制进 `Doc_0/Signatures/`，在 `Signature.xml` 中写入
`<Seal BaseLoc="Seal.esl"/>`，并把该文件纳入签名引用。阅读器按 `Seal@BaseLoc`
→ `ExtractSealData` → 页面渲染的链路提取印章图片；找不到或无法解析时回退到
`SignedValue.dat` 中的内嵌印章。
