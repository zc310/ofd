# 密码功能测试样本与回归

本目录保存 GM/T 0099-2020 元数据样本、独立密码学向量和 ofdrw 互通测试入口。
加解密、逻辑视图、签名及完整性保护的回归按测试内容分类，功能范围见
[密码功能与实现约定](../../docs/OFD-CRYPTO.md)。

## 加密元数据样本

`Encryptions.xml`、`seed.xml` 和 `map.xml` 按附录 C 构造，供元数据检查回归使用。
测试在内存中组合 ZIP，并用固定字节模拟不透明密文；包装密钥的零字节仅用于
长度/Base64 检查，不能用于密码学正确性或跨工具互通结论。

`Security/map.dat` 的明文内容来自 `map.xml`，用于验证不能按扩展名判断是否加密。
`OFD.xml` 只出现在映射中，物理包没有该文件；`IVValue` 省略，按表 C.1 缺省为零 IV。
针对 ofdrw 已确认的 `EncryptEntries`、数字 ID、遗漏 `EncryptCaseId` 的变体由测试
从这些 XML 构造；严格模式与兼容模式的区别见上述实现约定。

## 独立密码学向量

`vectors.json` 由 OpenSSL 3.5.5 生成，不依赖 Go 解密实现：

- `passwords`：对口令 UTF-8 字节串后拼接 `00 00 00 01`，执行 `openssl dgst -sm3 -binary`，
  取前 16 字节作为 KEK；以给定 IV 执行 `openssl enc -sm4-cbc -K <KEK hex> -iv <IV hex>`
  包装给定 FEK，采用 OpenSSL 默认 PKCS#7 填充。
- `cbc`：明文为长度分别为 0、1、15、16、17、31、32 的递增字节串，以给定 FEK/IV
  执行相同 SM4-CBC 命令，覆盖空文件及完整分组追加填充的边界。
- `sm2`：`openssl genpkey -algorithm SM2` 生成一次性测试私钥；
  `openssl req -new -x509 -sm3 -subj '/CN=OFD Crypto Test Only'` 生成自签测试证书；
  `openssl pkeyutl -encrypt -pubin -inkey <public key>` 对 FEK 生成 ASN.1 SM2Cipher。

私钥和证书仅用于可复现测试，不用于产品或信任根。证书信任/有效期不是文档解密成功的判据。
SM2 随机加密每次生成的密文不同，提交固定值用于回归；测试正常运行不依赖 OpenSSL 命令。
裸 C1C3C2 用例从该 ASN.1 向量转换生成。

## 完整包与第三方互通

`pkg/crypt` 的完整包测试按这些密钥参数构造内存 ZIP，分别覆盖明文/密文映射、零 IV、
局部/原位加密、损坏输入与预算限制，并用已有 `testdata/ofdrw/999.ofd` 检查恢复后每个文件
的原始字节、摘要和两层数学签名结果。ofdrw 2.4.0 的单层双向互通已实测，重跑入口与限制见
[`ofdrw/README.md`](ofdrw/README.md)。

## 多层加密与只读视图

`pkg/crypt/view_test.go` 在内存组合 ZIP，覆盖不同口令的两层依赖（两种 XML 列表顺序）、
独立条目 FEK/IV、同一操作内口令通用 seed 与证书独立 seed、部分解锁、不可见映射防回退、
全局预算和视图租约。两层签章样本同时检查最终每个文件原字节和签名数学验证结果。
测试不写入明文 fixture；这些构造不扩大 ofdrw 单层互通的验证范围。

`pkg/crypt/encrypt_extended_test.go` 验证公开写端生成的独立 FEK/IV、三层追加、默认命名空间
旧记录保留、跨层累计预算、页面模板/注释/资源范围，以及真实签章包的页面选择和恢复验签。
独立 FEK 包装在测试中直接解包并比较，防止只靠自身往返掩盖共用密钥错误。

## 签名与完整性保护

`internal/ses/gmt0099_test.go` 复用临时测试 CA/CRL 和独立编码的 CMS/TSTInfo 时间戳，
验证普通 SignedData 的原字节绑定、gmsm 独立编码器的 attached/detached 消息、附录 B 字段、
ImageLoc、Relative 基础签名、可信清单中的夹带、内容覆盖、加密闭包与预算/取消。
外部签名器子进程与真实 `ofd-crypt integrity protect/verify` 还覆盖完整 CLI 流程。
`gmt0099_append_test.go` 覆盖不同签名者连续追加、旧字节保留、最终列表锁定与清单完整验证，
以及自定义列表、多文档、32 层 Relative 上限、旧摘要/签名失败和路径冲突的签署前拒绝。
`cmd/ofd-creator/sign_test.go` 使用外部演示签名器验证追加命令、标准输出和失败输出保护。
这些回归生成的临时证书、密钥、清单和 OFD 不进入仓库；
普通数字签名与完整性保护的第三方产品互通尚未验证。
`gmt0099_physical_test.go` 使用临时 CA/CRL 和本地独立编码时间戳，验证加密物理包完整保护，
覆盖密文/seed/map/入口篡改、缺凭据/证据、同名物理与逻辑路径、覆盖根入口、夹带与预算。
这些回归不需要第三方 SDK 或在线 TSA，不能替代外部产品互通测试。
`gmt0099_validator_physical_test.go` 覆盖校验器的物理/逻辑范围、信任证据、逻辑结构错误及预算；
`gmt0099_integrity_error_test.go` 确保错误口令、预算或取消不会返回整体 valid 的部分报告。
签名回归还检查图 B.2 的 ExtendData 子元素，以及 Verify 对未完整解锁视图的拒绝。

## 阅读器集成

`cmd/ofd-wasm/document_test.go` 和 `encryption_js_wasm_test.go` 覆盖口令与 SM2 私钥解锁、
兼容格式、JavaScript 凭据传递、错误分类和 Reader 释放；网页脚本测试覆盖解锁对话框、
口令文件、重试、取消、文档切换及凭据缓冲清理。

`cmd/ofd-viewer/encryption_test.go` 覆盖桌面阅读器的口令/私钥解锁、ofdrw 兼容格式、
一次性读取器重试、多层完整解锁、渲染和导出，以及取消时保留当前文档和释放租约。
两类阅读器均通过只读视图消费解锁内容，不生成明文 OFD ZIP。
