# 更新日志

本文件只记录**已发布 API 的破坏性变更**、**新增能力**与**需要用户动手修改配置**
的事项。日常修复与重构不在此列出，见 `git log`——提交信息使用 Conventional
Commits。

未发布周期内新增的组件（例如 `ofd-server`）不算破坏性变更：它从未发布过，
没有可破坏的使用者。这类组件的接口形态写在「新增」里。

## 未发布

### 破坏性变更

- `pkg/converter` 的转换入口新增首个参数 `context.Context`。

  ```go
  // 之前
  converter.Convert("ofd", "pdf", input, output, opts...)
  // 现在
  converter.Convert(ctx, "ofd", "pdf", input, output, opts...)
  ```

  涉及 `Convert`、`Encode`、`EncodeDocuments`，以及各格式的 `PDF`/`Text`/
  `Markdown`/`HTML`/`Image` 与它们的 `*Document(s)` 变体。`Encoder`、
  `Importer`、`Transformer` 三个接口的签名未变。

  调用方传 `context.Background()` 即可保持原行为；若能传入带超时的
  `context`，大文档在页与页之间即可被取消，不必渲染完全部页。

- `ofd-converter` 在用户按 `Ctrl-C` 时以退出码 `130` 退出（原为 `0` 或 `1`）。
  脚本若依赖退出码判断成败，需要把 `130` 也视为"用户主动停止"。



### 新增

- `cmd/ofd-server`：异步 OFD 转换 HTTP 服务。任务状态持久化在 bbolt 中，进程
重启后中断的任务与未投递的通知会自动恢复。完整用法见
[`cmd/ofd-server/README.md`](cmd/ofd-server/README.md)。

**提交接口。** `POST /v1/convert` 需要 Bearer 令牌。`output.kind` 决定同步还是
异步：

| `kind` | 状态码 | 响应体 | 产物落点 |
|---|---|---|---|
| `stream` | `200` | 产物字节 | 不落盘 |
| `dir` | `202` | 任务 ID | `output_dir/<任务 ID>/` |
| `ftp` / `s3` / `webdav` / `sftp` | `202` | 任务 ID | 由目标方存储管 |

`stream` 是同步的：它的产物只在内存里，异步提交的结果没有任何人能取到。同步
转换由请求自己执行，并发上限取该通道的 `fast_workers` / `heavy_workers`，满了
返回 `503` 与 `Retry-After`；客户端断连直接取消转换；不发通知；失败直接返回
`4xx` / `5xx`。任务记录照常写入，所以统计与任务查询都不受影响。

`stream` 之外都是异步：提交立即返回 `202` 与任务 ID，转换在后台队列执行。

**产物落点。** `output.file_name` 指定基名，扩展名由 `output.format` 决定。
给基名而不是完整文件名，是因为扩展名本就由格式决定，允许一并指定就多了一处
自相矛盾的地方。逐页输出的基名替换 `page` 段、页号保留：

| `file_name` | `format` | 产物名 |
|---|---|---|
| 不填 | `pdf` | `output.pdf` |
| `INV-2026-0815` | `pdf` | `INV-2026-0815.pdf` |
| `report.final.pdf` | `pdf` | `report.final.pdf`（不重复追加扩展名） |
| `thumb` | `png` | `thumb-0001.png`、`thumb-0002.png`…… |

`file_name` 必须是单个路径组件（不含分隔符、NUL、控制字符，不是 `.` 或 `..`，
长度受限），否则提交阶段返回 `400`。`GET /v1/jobs/{id}` 的 `output.files` 列出
产物文件名，逐页输出会有多项。

`output.dir` 是 `output_dir` 之下的**相对**子路径（`"a/b/c"`），不写绝对路径。
显式指定时不再追加任务 ID——调用方写了什么就落在什么位置；不指定时仍按任务
ID 隔离。代价是并发任务写同一目录且同名文件会撞，后者失败且错误明确。

**远端输出。** 四个协议共用一组字段：

```jsonc
{"kind": "s3",  "format": "pdf", "remote": {"target": "minio",   "path": "incoming/2026"}}
{"kind": "ftp", "format": "pdf", "remote": {"target": "archive", "path": "2026/09/28"}}
```

`kind` 已经表明用哪种协议，再按协议分字段是冗余的。`path` 的含义由目标自身
配置决定：FTP/SFTP/WebDAV 是远端 `base_dir`，S3 是桶内 prefix。目标名必须
已注册：地址、桶、凭据只存在于服务端配置里，调用方能给的只是一个名字。

远端子路径的 `..` 按**段**判断，不是整串匹配——`v1.2`、`a..b` 这类合法目录名
带两个点是常事，整串匹配会一起误杀。

**配置。**

- `/v1/*` 需要 Bearer 令牌，`api_key` 必填、留空则拒绝启动：

```bash
export OFD_SERVER_API_KEY="$(openssl rand -hex 32)"
```

令牌可用 `Authorization: Bearer <key>` 或 `X-OFD-Api-Key: <key>` 传递，后者
供浏览器页面使用。`/healthz` 与 `/readyz` 免鉴权——编排系统不持有业务令牌，
挡掉它们只会让健康检查一直失败。要求这一项是因为服务会把上传文件交给
LibreOffice 与 Chrome 解析，两者都是解析不可信输入的经典目标，而 `listen`
默认 `:9705` 监听所有网卡。

容器部署的 `docker-config.json` 把 `api_key` 留空，需通过
`OFD_SERVER_API_KEY` 环境变量传入——内置一个占位符等于随镜像发布一把公开
的钥匙。

- `chrome_no_sandbox`（默认 `false`）。容器内以非 root 用户运行时需显式设为
`true`，否则 Chrome 起不来（沙箱依赖容器默认不提供的 user namespace）。
开启后 Chrome 不再受沙箱隔离。

- 日志默认行为与是否落盘有关：配置了 `log_dir` 就只写轮转文件，不配置则只写
标准输出。两者都想要时显式设 `log_to_stdout: true`。相关字段为 `log_dir`、
`log_file`、`log_max_size_mb`、`log_max_backups`、`log_max_age_days`、
`log_compress`。

- `job_retry_backoff`（默认 `5s`）。首次重试的等待时长，之后按次数指数翻倍、
封顶 10 分钟。

- 明文 FTP 与 http 对象存储需要双重声明才允许：目标上设 `insecure: true`，
并在配置顶层设 `allow_insecure_ftp` / `allow_insecure_s3`。SFTP 的主机密钥
校验同理：`host_key_sha256` 与 `host_key_file` 都不配置时服务拒绝启动，确需
跳过要显式设 `insecure_ignore_host_key: true`。

**提交接口拒绝未知字段**，与配置文件同一套严格解码。拼错字段名会返回 `400`，
而不是让任务照常成功、产物名安静地退回默认值。请求里的 `lane` 同样返回 `400`
而非被忽略：静默忽略会让调用方以为自己拿到了 heavy 优先级、实际走了 fast。


而不是让任务照常成功、产物名安静地退回默认值。请求里的 `lane` 同样返回 `400`
而非被忽略：静默忽略会让调用方以为自己拿到了 heavy 优先级、实际走了 fast。

- `ofd-converter` 支持 `Ctrl-C` 立即停止，不再跑完当前文件或整个批次。

- 任务记录新增 `from_actual` 与 `input_bytes` 字段，供转换量统计使用。

  `from_actual` 是服务端实际判定的输入格式，与调用方声明的 `from` 分开：
  README 明确说明输入格式由服务端解析、不采信调用方的文件名，而 `from`
  恰好就是那个声明值——一个 OFD 内容配了 `.html` 名字的任务，在 `from`
  里会记成 `ofd→pdf`。

  `input_bytes` 取输入落盘后的实际尺寸，覆盖上传与 URL 两种来源。提交时先记
  已收到的字节数，转换成功后用落盘尺寸覆盖，因此上传失败的任务也有值；URL
  输入失败时为 0，那时确实没有拿到完整内容。

  任务仍按 `retention` 清理，所以这两个字段只覆盖保留期内的任务。要做跨
  清理周期的累计统计，需要另行引入只增不改的计数聚合。

- `ofd-server` 新增 `GET /metrics`，输出 Prometheus 文本格式，含转换计数
  （`ofd_conversions_total`、`ofd_input_bytes_total`、
  `ofd_output_bytes_total`）与 Go 运行时、进程级指标。

  计数落在 bbolt 的独立 bucket，在任务进入终态的同一事务里累加，不参与
  `retention` 清理——否则清理后计数会下降，Prometheus 的 `rate()` 与
  `increase()` 会产出错值且不报错。`from` 标签用服务端判定的实际格式，
  失败任务归到 `unknown`。

  另有 `ofd_queue_depth{state}` 与 `ofd_workers{lane}` 两个 gauge：前者是
  未终结任务数（`queued` 含重试退避中的任务），后者让深度可换算成利用率。
  队列深度每次抓取现算，与跨重启累计的计数是两个生命周期。

  `GET /v1/stats` 提供同一份计数的 JSON 视图，含累计转换数、输入输出字节总量、
  按 输入格式 -> 输出格式 的分组，以及瞬时的队列深度。`since` 给出首次记账
  时间，调用方据此才能算速率；`started_at` 与 `uptime_seconds` 给出服务启动
  时刻与运行时长，二者与 `since` 对照可判断累计计数是否跨过重启。口径说明
  写在响应的 `note` 字段里。

  该端点需要 API 令牌；`/healthz` 与 `/readyz` 仍免鉴权。

- `ofd-server` 提供 Ubuntu 运行镜像 `cmd/ofd-server/Dockerfile`，内置
  LibreOffice 与 Chrome，Office 文档与 HTML 的转换开箱可用。镜像预置的
  `docker-config.json` 打开了 `chrome_no_sandbox`——Chrome 沙箱依赖容器默认
  不提供的 user namespace，镜像以非 root 用户运行，不加这个开关 HTML 任务
  会直接失败。
