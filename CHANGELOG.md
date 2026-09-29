# 更新日志

本文件只记录**破坏性变更**、**新增能力**与**需要用户动手修改配置**的事项。
日常修复与重构不在此列出，见 `git log`——提交信息使用 Conventional Commits。

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

- `cmd/ofd-server`：异步 OFD 转换 HTTP 服务。提交任务后立即返回 `202` 与任务
  ID，转换在后台队列执行并可回调通知。任务状态持久化在 bbolt 中，进程重启后
  中断的任务与未投递的通知会自动恢复。

  输出目标除内存流与本地目录外，还可直接上传到 FTP/FTPS、S3 兼容对象存储、
  WebDAV 与 SFTP。这些目标的地址与凭据只存在于服务端配置，请求方只能引用
  目标名。完整用法见 [`cmd/ofd-server/README.md`](cmd/ofd-server/README.md)。

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

  该端点需要 API 令牌；`/healthz` 与 `/readyz` 仍免鉴权。

- `ofd-server` 提供 Ubuntu 运行镜像 `cmd/ofd-server/Dockerfile`，内置
  LibreOffice 与 Chrome，Office 文档与 HTML 的转换开箱可用。镜像预置的
  `docker-config.json` 打开了 `chrome_no_sandbox`——Chrome 沙箱依赖容器默认
  不提供的 user namespace，镜像以非 root 用户运行，不加这个开关 HTML 任务
  会直接失败。

### 需要修改配置

- `ofd-server` 的 `/v1/*` 接口现在需要 Bearer 令牌，且 `api_key` 为必填项，
  留空则拒绝启动。

  ```bash
  export OFD_SERVER_API_KEY="$(openssl rand -hex 32)"
  ```

  令牌可用 `Authorization: Bearer <key>` 或 `X-OFD-Api-Key: <key>` 传递，
  后者供浏览器页面使用。`/healthz` 与 `/readyz` 保持免鉴权——编排系统不持有
  业务令牌，挡掉它们只会让健康检查一直失败。

  加这一项是因为服务会把上传文件交给 LibreOffice 与 Chrome 解析，两者都是
  解析不可信输入的经典目标，而 `listen` 默认 `:9705` 监听所有网卡。原先
  `/v1/convert` 是匿名开放的。

  容器部署的 `docker-config.json` 把 `api_key` 留空，需要通过
  `OFD_SERVER_API_KEY` 环境变量传入。

- `ofd-server` 新增 `chrome_no_sandbox`（默认 `false`）。容器内以非 root 用户
  运行时需要显式设为 `true`，否则 Chrome 起不来。开启后 Chrome 不再受沙箱
  隔离。

- `ofd-server` 的日志默认行为与是否落盘有关：配置了 `log_dir` 就只写轮转文件，
  不配置则只写标准输出。两者都想要时显式设置 `log_to_stdout: true`。
  相关字段为 `log_dir`、`log_file`、`log_max_size_mb`、`log_max_backups`、
  `log_max_age_days`、`log_compress`。

- `ofd-server` 的明文 FTP 与 http 对象存储需要双重声明才允许：目标上设
  `insecure: true`，并在配置顶层设 `allow_insecure_ftp` / `allow_insecure_s3`。
  SFTP 的主机密钥校验同理：`host_key_sha256` 与 `host_key_file` 都不配置时服务
  拒绝启动，确需跳过要显式设 `insecure_ignore_host_key: true`。
