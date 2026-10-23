# ofdrw 2.4.0 互通回归

固定使用 Maven Central 的 `org.ofdrw:ofdrw-crypto:2.4.0` 及其版本化依赖；
`Interop.java` 只适配调用入口，不修改第三方类或输出内容。测试运行时检查
`GlobalVar.Version == "2.4.0"`。正式 Go 库和 CLI 不依赖 Java。

在仓库根目录准备 Java/Javac 和 Maven，执行：

```bash
mvn -B -f testdata/crypt/ofdrw/pom.xml \
  -Dmaven.repo.local=/tmp/ofd-crypt-maven \
  -Dinterop.build=/tmp/ofd-crypt-java-build \
  -DoutputDirectory=/tmp/ofd-crypt-java-deps \
  org.apache.maven.plugins:maven-dependency-plugin:3.8.1:copy-dependencies

OFD_CRYPTO_OFDRW_CLASSPATH='/tmp/ofd-crypt-java-deps/*' \
  go test ./pkg/crypt -run TestOFDRW240Interoperability -v -count=1
```

编译器输出、Java 工作目录、测试 OFD、口令、证书和私钥都位于测试临时目录，
普通 Go 测试未设置变量时跳过这项检查，不下载依赖。首次解析 Maven 插件可能需要较长时间。

2026-10-23 使用 OpenJDK 25.0.4.1、Maven 3.9.11 完成实测：

| 方向 | 口令 | 证书 |
|---|---|---|
| ofdrw 加密 → Go 解密，全包 | 逐条路径和原字节一致 | 逐条路径和原字节一致 |
| ofdrw 加密 → Go 解密，局部 | 逐条路径和原字节一致 | 逐条路径和原字节一致 |
| Go 加密 → ofdrw 解密，全包，默认加密映射 | 逐条路径和原字节一致 | 逐条路径和原字节一致 |
| Go 加密 → ofdrw 解密，局部，默认加密映射 | 逐条路径和原字节一致 | 逐条路径和原字节一致 |
| Go 加密 → ofdrw 解密，明文映射 | 下述第三方异常 | 下述第三方异常 |

另用已有 `testdata/ofdrw/999.ofd` 做口令方案双向互通，检查逐条原字节、签名摘要及两层
数学签名结果均保持。这里没有为文档签章补充信任根、吊销或印章状态证据，不据此认定完整验签通过。

签章样本另复现了 `Path=/.../SignedValue.dat`、`EPath=/.../signedvalue.dat` 的仅大小写改名。
Go 精确读取该映射的 EPath，移除密文后恢复原名；不对任意大小写别名做回退。
普通回归 `TestDecryptAllowsCaseOnlyCipherRenameButRejectsUnrelatedCaseAliases` 固定这条边界。

**已复现的第三方限制**：ofdrw 2.4.0 的 `decryptEntriesMap` 先尝试 SM4 解密明文映射，
只捕获 `CryptoException`。明文 XML 长度不为 16 的倍数时，BouncyCastle 抛出未捕获的
`DataLengthException: last block incomplete in decryption`，因此该路径失败。
测试精确识别并记录该异常，其他异常仍失败；默认加密映射用例必须成功。
此项不计作明文映射互通成功，也没有为绕过异常给 XML 人为补齐长度。

ofdrw 生成的数字 ID 和缺失 `EncryptCaseId` 在 Go 侧通过 compat 模式和显式
`MissingScheme` 处理。该第三方实现没有用 `Relative` 排序、多层行为不在这次互通结论中。

本次下载的核心制品 SHA-256：

| 制品 | SHA-256 |
|---|---|
| `ofdrw-crypto-2.4.0.jar` | `4a4098d136c0b597036f8f8904859c31fb4203c5ad5bfa515da2c918d4ce9a54` |
| `ofdrw-core-2.4.0.jar` | `c959d6b097af39baf76bd2b3aaf0f5b1c1703261ddfd589a782b81fcadad7e08` |
| `bcprov-jdk18on-1.84.jar` | `64d6c5a6121fcd927152dd182cbed39afe0fda641a970d9bcc0c9cb1858b2731` |
