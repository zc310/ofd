# ofd-crypt

检查、加密和解密 GM/T 0099-2020 OFD 包。写端支持多接收者、独立条目密钥、追加层及页面范围；
读端支持口令/SM2 证书、独立条目 seed、多操作和 Relative 多层依赖。
`integrity inspect/protect/verify` 处理 GM/T 0099 完整性清单、带时间戳的数字签名和防夹带检查。
实现范围、资源预算及完整性保护限制见
[`pkg/crypt`](../../pkg/crypt/README.md) 和 [格式约定](../../docs/OFD-CRYPTO.md)。

```bash
go build -o ofd-crypt ./cmd/ofd-crypt
# 或生成包含程序与本说明的 ZIP 安装包
make package-crypt
```

## 示例

```bash
# 无凭据检查；即使 OFD.xml 已加密也可读取可见元数据
ofd-crypt inspect --format json encrypted.ofd

# 口令文件按 UTF-8 原样读取，包括末尾换行；最多 64 KiB
ofd-crypt encrypt --password-file password.txt --user 张三 -o encrypted.ofd input.ofd
ofd-crypt decrypt --password-file password.txt --user 张三 -o plain.ofd encrypted.ofd

# 从标准输入读取口令；不会将口令放入进程命令行
ofd-crypt decrypt --password-file - -o plain.ofd encrypted.ofd

# 同方案多接收者，任选一个可解开该次操作
ofd-crypt encrypt --password-recipient 张三=zhang.txt --password-recipient 李四=li.txt -o encrypted.ofd input.ofd
ofd-crypt encrypt --recipient 张三=zhang.cer --recipient 李四=li.cer -o encrypted.ofd input.ofd
ofd-crypt decrypt --key recipient-key.pem -o plain.ofd encrypted.ofd

# 每层使用不同凭据；ID 来自 inspect 的操作清单
ofd-crypt decrypt --password-for outer=outer.txt --key-for inner=inner-key.pem -o plain.ofd layered.ofd

# 独立条目 seed 的凭据优先于操作凭据；seed 路径为规范 ZIP 相对路径
ofd-crypt decrypt --password-file common.txt --password-for-seed Security/page.seed=page.txt -o plain.ofd encrypted.ofd

# 尝试已提供的凭据并报告部分/完整解锁状态，不输出文档内容
ofd-crypt inspect --password-for outer=outer.txt --format json layered.ofd

# 保留主入口明文，或只加密指定条目；路径为 ZIP 相对路径
ofd-crypt encrypt --cert recipient.cer --keep-root -o encrypted.ofd input.ofd
ofd-crypt encrypt --password-file password.txt --entry Doc_0/Pages/Page_0/Content.xml --entry Doc_0/Res/image.png -o encrypted.ofd input.ofd

# 每条目生成独立随机 FEK/IV；大量条目在读取时需增加累计凭据尝试预算
ofd-crypt encrypt --independent-keys --password-file password.txt -o independent.ofd input.ofd
ofd-crypt decrypt --max-credential-attempts 256 --password-file password.txt -o plain.ofd independent.ofd

# 追加一层：新口令和旧层预检凭据分别输入
ofd-crypt encrypt --append --input-password-file inner.txt --password-file outer.txt -o layered.ofd encrypted.ofd
# 多个旧层也可通过 --password-for/--key-for 或 seed 选择器指定

# 按文档内页面 ID 选择，并包含模板、注释、印章和整份共用资源
ofd-crypt encrypt --password-file password.txt --page-document Doc_0/Document.xml --page-id 1 --page-id 2 -o pages.ofd input.ofd

# 已确认的 ofdrw 缺省方案兼容；必须显式选择缺失的方案
ofd-crypt decrypt --mode compat --missing-scheme password --password-file password.txt -o plain.ofd ofdrw-encrypted.ofd

# 用明文 OFD 调用现有工具
ofd-validator --format json plain.ofd
ofd-converter plain.ofd output.pdf
```

CLI 不提供明文口令参数。口令文件和标准输入的所有字节都参与派生；例如 `secret` 与
`secret` 后跟换行是不同口令。空文件表示空口令。为避免意外读取，证书和私钥必须使用
文件路径，只有口令支持 `-`；同次调用至多一个口令接收者读取标准输入。
口令、证书、私钥及完整性证据的文件路径必须指向普通文件；FIFO、设备文件和指向管道的
`/dev/fd/...` 会在打开前被拒绝。流式口令使用 `--password-file -`，读取期间可取消。

证书支持单个 PEM `CERTIFICATE` 或 DER；私钥支持未加密 PKCS#8 `PRIVATE KEY` PEM 或 DER。
不会自动下载证书、验证信任链或调用外部密钥程序；硬件密钥通过 Go API 的
`crypto.Decrypter` 接入。证书和私钥文件最大 4 MiB。证书加密会检查 SM2 公钥及已声明的
KeyUsage 是否允许加密，证书信任/有效期由调用方另行判断。

## 选项

通用选项适用于三个子命令：

| 选项 | 说明 |
|---|---|
| `-o, --output PATH` | inspect 默认 stdout，可用 `-`；加解密必须指定文件 |
| `--format text\|markdown\|json` | 结果报告格式，默认 text；加解密报告写 stdout，OFD 写输出文件 |
| `--force` | 替换已有普通输出文件 |
| `--max-input-bytes N` | 输入 ZIP 字节上限 |
| `--max-entry-bytes N` | 单条解压及单条输出字节上限 |
| `--max-total-bytes N` | 输入解压总量、输出总量分别限制 |
| `--max-entries N` | 物理/输出条目数量上限 |
| `--max-metadata-bytes N` | 单份加密元数据字节上限 |
| `--max-credential-attempts N` | 所有 seed 的累计解包尝试上限，默认 32 |
| `--max-recipients N` | 所有 seed 的累计接收者上限，默认 1024 |
| `--max-layers N` | Relative 依赖深度上限，默认 32 |
| `--max-processed-bytes N` | 视图预检/读取累计处理字节上限，默认 2 GiB |
| `-v, --version` | 输出由 Makefile 注入的工具版本 |
| `-h, --help` | 帮助 |

预算取字节数或整数，不支持单位后缀；`0` 使用库的有限默认值，负数为用法错误。
输入条目路径须为有效 UTF-8 且能无损写入 XML 1.0，含非法 XML 字符的路径会在加密输出前被拒绝。

加解密共用 `--password-file PATH` 和 `--user NAME`。加密时默认用户名称为 `User`，
解密省略用户名称时在预算内自动匹配；显式指定时按原始名称匹配。

| 子命令 | 选项 | 说明 |
|---|---|---|
| 全部 | `--mode strict\|compat` | 默认 strict；encrypt 仅在追加时指定旧层读取模式 |
| inspect/decrypt | `--key PATH` | 缺省私钥，与缺省 `--password-file` 互斥 |
| 全部 | `--password-for ID=PATH` / `--key-for ID=PATH` | 按已有操作选择凭据；encrypt 仅用于 --append 预检 |
| 全部 | `--password-for-seed SEED=PATH` / `--key-for-seed SEED=PATH` | 按 seed 路径选择凭据，优先于操作选择；encrypt 仅用于 --append 预检 |
| 全部 | `--missing-scheme password\|certificate` | 仅在 compat 且源方案缺失时使用，不能覆盖已声明方案 |
| encrypt | `--cert PATH` | 单证书接收者，与其他接收者输入形式互斥 |
| encrypt | `--recipient NAME=PATH` | 多证书接收者，可重复；名称由此指定 |
| encrypt | `--password-recipient NAME=PATH` | 多口令接收者，可重复 |
| encrypt | `--role User\|Owner` | 所有接收者的声明角色，默认 User；不表示额外认证权限 |
| encrypt | `--entry PATH` | 选择加密的精确 ZIP 相对路径，可重复；省略则全部文件 |
| encrypt | `--keep-root` | 全部选择时保留 OFD.xml 明文；不能同时显式选择该文件 |
| encrypt | `--plaintext-map` | 明文输出映射表，默认加密以隐藏原路径 |
| encrypt | `--independent-keys` | 每个选中条目生成独立 FEK/IV 和 seed；通用 seed 用于映射表 |
| encrypt | `--append` | 旧凭据完整预检后，在已有线性加密链外追加一层 |
| encrypt | `--input-password-file PATH` / `--input-key PATH` | 追加时的缺省旧凭据，二选一 |
| encrypt | `--input-user NAME` | 旧凭据接收者筛选；省略则自动匹配，与新接收者 --user 分开 |
| encrypt | `--page-document PATH` / `--page-id ID` | 页面范围，页面 ID 可重复指定不同值；与 --entry、--append 互斥 |

`--entry` 不会自动计算页面引用闭包。选择页面内容而保留共享图片/字体等资源可能暴露内容，
应按所需范围同时选择相关文件。页面模式按 C.1 加入模板、注释和印章，对资源索引及所列数据
整体加密，共享资源可能同时锁定其他页面；报告列出实际范围。文档结构、附件、自定义标签和
历史版本不自动加入页面范围。ofdrw 2.4.0 对非分组对齐长度的明文映射表存在异常，
跨工具交换时使用默认加密映射表。
包内页面、模板及注释/印章引用的 ID 按无符号整数值匹配，`01` 与 `1` 指向同一页面；
数值相同的重复页面/模板 ID 和非法引用均失败。

独立 seed 的解包次数和接收者数量跨 seed 累计；默认 32 次尝试只适用于有限规模输入，
需要处理更多条目时显式设置上述预算。独立 seed 和追加层尚未通过第三方互通验证。

## 完整性保护子命令

```bash
ofd-crypt integrity inspect --format json protected.ofd

# 外部签名器从 stdin 接收最终 OFDEntries.xml，stdout 返回带时间戳的 SM2/SM3 SignedData
ofd-crypt integrity protect --sign-cmd ./manifest-signer -o protected.ofd signed.ofd

# 签名、TSA 的显式信任根和离线 CRL；文件选项均可重复
ofd-crypt integrity verify --trust-root roots.pem --tsa-root tsa-roots.pem \
  --crl issuer.crl --issuer issuer.pem --format json protected.ofd

# 加密包需要凭据才能完成映射与文档闭包遍历
ofd-crypt integrity verify --trust-root roots.pem --crl issuer.crl --issuer issuer.pem \
  --password-file password.txt protected-encrypted.ofd
```

三个子命令接受 `--output/--format/--force`，并与普通加解密命令共用上述全部预算选项，
包括 `--max-recipients`、`--max-layers`、`--max-processed-bytes` 和 `--max-credential-attempts`。
大量独立 seed 的文档可能需要同时提高累计接收者和凭据尝试预算。
`integrity` 父命令和三个子命令均支持 `--version` / `-v`。
`protect` 必须指定输出及 `--sign-cmd`，可用 `--id` 设置清单标识；命令按空白拆参数，
不解释 shell 引号或管道。外部签名命令 stdout 上限 4 MiB、stderr 上限 64 KiB。
演示签名器不提供 TSA，不能用不带时间戳的演示输出代替完整性保护签名。

`verify` 可用 `--password-file/--key/--user` 或操作/seed 选择器解锁；`--mode` 和
`--missing-scheme` 的语义与解密一致。`--trust-root` 缺省不使用系统根；`--tsa-root`
省略时复用签名信任根。`--crl/--issuer` 支持 PEM 或 DER；根证书文件使用 PEM。
所有证据文件与输出路径冲突均被拒绝。

`inspect` 只检查可见清单与物理集合；`protect` 成功返回 `signed`，不表示全部内容已验证。
`verify` 报告清单签名/时间戳、文件集合、引用闭包和内容签名覆盖，只有 `status=valid`
退出 `0`；`absent/incomplete/invalid` 都退出 `1`，报告仍正常输出。
中途因错误口令、预算超限等失败时，已生成的部分报告仍输出到 stdout 或 `--output` 指定的文件，
原错误写入 stderr，并保留失败退出码；部分报告仅包含已执行的检查，不能作为验证成功的依据。
缺少信任/吊销证据、解密凭据或文件内容签名保护时保持未完成。
文件列入可信清单仍须独立满足引用闭包；清单自身没有逐文件摘要，不把清单签名当作内容完整性。
加密包的逻辑签名不自动覆盖物理密文/控制文件，内容检查可能继续未完成。
可在 `encrypt --keep-root` 后用 `ofd-creator sign --physical` 签署物理包，再执行清单保护。
验证报告的 `physical.signature` 与 JSON `physical_signatures` 单独呈现外签结果；
外签完整通过后实际密文和控制文件可获得内容覆盖，解密凭据仍用于完成文档闭包。
外签 References 和映射中的无关条目不会自动成为文档可达文件。
详细语义见 [`pkg/integrity`](../../pkg/integrity/README.md)。

## 输出与保护范围

OFD 输出先写入目标目录的权限为 `0600` 的临时文件，完整处理、同步和关闭后才提交。
失败时删除临时文件，保留已有输出；目标父目录必须存在。
默认不覆盖，使用同目录硬链接提交以避免覆盖竞态；文件系统不支持此操作时明确失败。
`--force` 使用同目录 rename 替换；不使用删除后复制回退，平台不支持替换时失败。

`decrypt` 必须提供至少一项缺省或按选择器指定的凭据，并要求所有操作和条目完整解锁；
缺少某层/seed 凭据时失败，不发布部分明文。`inspect` 无凭据时只检查可见元数据；
提供凭据时建立只读视图，可返回 `partially_unlocked`，检查成功退出 `0`，不代表文档已完整解锁。
错误口令、损坏密文、预算超限和坏元数据仍返回失败。标准输入至多被一个凭据读取。

输入与输出、输出与凭据不得为同一路径或同一物理文件；符号链接输出和目录输出被拒绝。
多层解密按 Relative 从后层到前层处理，不按 XML 列表顺序。追加层的 `--entry` 指当前物理
ZIP 路径；默认加密旧 seed/map/密文并保留明文入口，输入需有规范入口名和单一线性链。
旧凭据仅供预检，新层输出仍需要内外层凭据。写端不重建现有 `OFDEntries.xml`。
先签章后加密可在恢复原字节后
独立验签；涉及无法保留的签名保护范围时失败。成功解密不等于 CBC 内容完整性或签名有效性。

报告写 stdout，错误诊断写 stderr，任何口令、KEK、FEK、完整私钥都不进入报告。
如果报告写出失败，但 OFD 已完整提交，命令仍返回失败；错误不表示已完成的 OFD 被回滚。

## 退出码

| 退出码 | 含义 |
|---|---|
| `0` | 检查/加解密成功，或帮助/版本正常输出；inspect 成功只表示完成可见信息检查 |
| `1` | 文档、凭据文件、输出环境或处理失败，包括不支持的方案/层次、密码错误或密文损坏 |
| `2` | 用法/参数组合错误、输入不存在、输入输出或凭据路径冲突、未授权覆盖已有输出 |
| `130` | 用户取消或上下文超时 |
