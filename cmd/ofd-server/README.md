# ofd-server

异步 OFD 转换服务。HTTP 提交任务，后台队列执行，完成后回调通知。

本目录同时含 `Dockerfile`（Ubuntu 运行镜像）与 `docker-config.json`
（镜像内置的容器默认配置）。

## 快速开始

```bash
make build-ofd-server        # 或 go build -o ofd-server ./cmd/ofd-server

export OFD_SERVER_API_KEY="$(openssl rand -hex 32)"

cat > ofd-server.json <<'EOF'
{
  "db_path":    "/var/lib/ofd-server/jobs.db",
  "output_dir": "/var/lib/ofd-server/out"
}
EOF

./ofd-server --check-config -c ofd-server.json   # 先校验配置
./ofd-server -c ofd-server.json
```

不指定 `listen` 时默认监听 `:9705`。`db_path`、`output_dir` 与令牌必填。
令牌建议走 `OFD_SERVER_API_KEY` 环境变量而不是写进配置文件：
`/proc/<pid>/environ` 对同用户可读，而配置文件常常是 0644 跟着镜像和备份
走一圈。写在配置里用 `api_key` 也行。

提交一次转换。`output.kind` 决定它是同步还是异步。先看最简单的同步形态——
`stream` 直接把产物作为响应体返回：

```bash
AUTH="Authorization: Bearer $OFD_SERVER_API_KEY"

python3 - <<'PY' > req.json
import base64, json
data = open("sample.ofd", "rb").read()
print(json.dumps({
    "input":  {"kind": "upload", "file_name": "a.ofd",
               "bytes": base64.b64encode(data).decode()},
    "output": {"kind": "stream", "format": "pdf"},
}))
PY

# 响应体就是 PDF 本身；另有 X-Ofd-Job-Id 响应头可供事后查询
curl -sS -H "$AUTH" -X POST --data-binary @req.json \
     http://127.0.0.1:9705/v1/convert -o result.pdf
```

把 `"kind": "stream"` 换成 `"kind": "dir"` 就走异步：提交立即返回 `202` 与任务
ID，转换在后台队列执行，结果落在 `output_dir/<ID 末 2 位>/<ID 剩余段>/`：

```bash
ID=$(curl -sS -H "$AUTH" -X POST --data-binary @req.json \
     http://127.0.0.1:9705/v1/convert | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')

curl -sS -H "$AUTH" "http://127.0.0.1:9705/v1/jobs/$ID"
```

## API

### 认证

`/v1/*` 需要 Bearer 令牌，两种传递方式等价：

```
Authorization: Bearer <api_key>     # 命令行与脚本
X-OFD-Api-Key: <api_key>            # 浏览器页面
```

请求体里的 `input.file_name` 与 `output.file_name` 是本接口自己的字段，与
`multipart/form-data` 表单里的 `filename` 无关——后者是 HTML 规范定的名字，
两者不通用。

`/healthz` 与 `/readyz` 不需要令牌：kubelet 与负载均衡器不持有业务令牌，
挡掉它们只会让健康检查一直失败、容器被反复重启。

令牌比较用 `crypto/subtle.ConstantTimeCompare`，逐字节提前返回的写法会把
密钥前缀的信息泄进响应时间。

### `GET /v1/stats`

转换量统计的 JSON 视图，需要令牌。与 `/metrics` 同源不同形——同一个 jobstore
计数渲染成两种格式，所以两边数字必然一致，可以互相对账。

```jsonc
{
  "started_at": "2026-09-29T03:16:49Z",  // 服务启动时刻
  "uptime_seconds": 8,                   // 到本次抓取的运行时长
  "since": "2026-09-29T03:16:55Z",       // 首次记账时间；没有转换时省略
  "note": "口径说明，字段含义见下",
  "totals": {
    "conversions": 2,                 // 计入统计的终态任务数
    "succeeded": 2,
    "failed": 0,
    "input_bytes": 4738,
    "output_bytes": 523537
  },
  "queue": {                          // 抓取瞬间的瞬时值，与上面的累计值不是一回事
    "queued": 0,                      // 含重试退避中的任务
    "running": 0,
    "workers": {"fast": 4, "heavy": 1, "notify": 4}
  },
  "formats": [                        // 按 输入格式 -> 输出格式 分组
    {"from": "ofd", "to": "pdf", "conversions": 1, "succeeded": 1, "failed": 0,
     "input_bytes": 4738, "output_bytes": 249961}
  ]
}
```

`started_at` 与 `since` 要一起看：服务重启后 `started_at` 会变而 `since` 不变，
两者相差很大就说明这些累计数字跨过了重启——既没被清零，也不是新起的一段。
只看 `conversions` 是看不出这一点的。

`uptime_seconds` 在每次抓取时现算，不是启动时算好的定值。

几条容易读错的口径，都写在响应的 `note` 字段里：

- **计数是累计值，不受 `retention` 影响。** 任务记录会被清理，计数独立存放，
  所以"总转换量"真的是自服务启动以来的总量，不是"最近 N 天"。
- **`conversions` 不含已取消的任务。** `Cancel` 绕过了记账——取消的任务没进
  转换。所以 `conversions` 也不等于提交总数，提交总数服务端不留存。
- **`from` 用服务端判定的实际格式**，不是调用方声明的值。失败任务没走到判定
  格式那步，归为 `unknown`，并带 `label_unknown: true` 标记以便与真实格式
  区分，排序时排在最后。
- **`formats[].input_bytes` 是该输入格式的总输入量**，不是这一条转换路径的量。
  输入字节只按 `from` 索引，所以同一 `from` 的多行会重复计入，各行相加大于
  `totals.input_bytes`。这是口径的已知代价，不是 bug；`output_bytes` 则是精确的。
- **`input_bytes` 是"实际读入的字节"**，不是"提交的字节"。URL 输入的转换如果
  失败，这部分记为 0。
- `formats` 始终是空数组而非 `null`，调用方可以直接遍历。

### `GET /metrics`

Prometheus 文本格式（`text/plain; version=0.0.4`），需要令牌。

本服务自己的累计计数：

| 指标                     | 类型    | 标签                  | 含义                                            |
|--------------------------|---------|-----------------------|-------------------------------------------------|
| `ofd_conversions_total`  | counter | `from`, `to`, `state` | 到达终态的转换任务数                            |
| `ofd_input_bytes_total`  | counter | `from`                | 输入字节总量                                    |
| `ofd_output_bytes_total` | counter | `from`, `to`          | 输出字节总量                                    |
| `ofd_queue_depth`        | gauge   | `state`               | 未终结任务数（`queued` / `running`）            |
| `ofd_workers`            | gauge   | `lane`                | 各通道 worker 数（`fast` / `heavy` / `notify`） |

外加 Go 运行时与进程指标：`go_goroutines`、`go_threads`、
`go_memstats_*`、`go_gc_pause_seconds`、`go_info`、
`process_resident_memory_bytes`、`process_open_fds`、`process_max_fds`、
`process_start_time_seconds`。命名与 `client_golang` 的 collector 一致，
现成的 Grafana Go 面板可以直接用。

**计数独立于任务记录存在。** 终态任务会按 `retention` 清理，如果计数从
任务表现算，清理后数值会往下掉——Prometheus 的 counter 一旦出现下降，
`rate()` 与 `increase()` 产出的就是错值，而且不报错。所以计数落在
bbolt 的独立 bucket，在任务进入终态的同一个事务里累加，不参与清理。

`ofd_queue_depth{state="queued"}` **包含处于重试退避中的任务**：那些任务状态
是 `queued` 但躺在 delayed 队列里，还没进通道队列。只数通道队列的话，一次大规模
失败重试期间深度会显示为 0——而那正是最需要告警的时候。

计数用状态索引的前缀扫描实现，成本是 O(该状态任务数)而非 O(全部任务)：索引键的
首字节是状态码，同状态的任务在 B+ 树里连续。终态任务积到百万条也不影响耗时。
刻意不维护"计数器自增自减"——那需要每个状态迁移点都记得同步，漏一处就是永久性
错误数字且不报错。

`ofd_workers` 是为了让深度可判读：`ofd_queue_depth{state="running"}` 除以
`ofd_workers{lane=...}` 才是利用率。深度 50 本身说明不了问题——并发配 4 还是
50 完全不同。

队列深度与累计计数是两个生命周期不同的量：计数跨重启累计，深度每次抓取现算。
读不到深度时整组 gauge 不输出而不是输出 0——输出缺失与值为 0 在 Prometheus 里
是两件事，前者显示 `no data`，后者会被告警读成"队列是空的"。

`from` 标签用服务端判定的实际格式，不是调用方声明的值。失败任务没有实际
格式（转换没跑到判定那步），统一归到 `unknown`——不用声明值补，否则
"没判出来"和"判成这个格式"会混成一类。

标签只有 `from`、`to`、`state` 三个有限集合值。基数一旦随任务数或文件名
增长就是几百万条 series，这是把 Prometheus 和本服务一起拖垮的标准做法。
单任务明细在 `GET /v1/jobs/{id}`。

进程级指标从 `/proc` 读，非 Linux 或读不到时**不输出该指标**而不是让整个
端点失败。输出缺失与值为 0 在 Prometheus 里是两件事，前者显示成 `no data`，
后者会显示成"资源用光了"。

Prometheus 采集配置示例：

```yaml
scrape_configs:
  - job_name: ofd-server
    scrape_interval: 30s
    static_configs:
      - targets: ['ofd-server:9705']
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/ofd-api-key
```

### `POST /v1/convert`

受理转换，返回 `202` 与任务 ID。**不会挂起等待结果**——同步等待会把 HTTP
连接数和队列深度绑在一起，一批慢任务就能耗尽连接数。

```jsonc
{
  "input": {
    "kind": "upload",          // "upload" | "url"
    "file_name": "a.ofd",
    "bytes": "<base64>",      // kind=upload 时必填
    "format": "ofd",          // 可选。显式声明输入格式
    "url": "https://…/a.ofd"  // kind=url 时必填
  },
  "output": {
    "kind": "stream",         // "stream" | "dir"
    "format": "pdf",
    "dir": "/…/out/sub"       // 可选，只能在配置的 output_dir 之下
    "file_name": "INV-2026-0815",  // 可选，产物文件名的基名，不含扩展名
    "remote": {"target": "minio", "path": "incoming/2026/08"}  // 仅远端 kind
  },
  "notify": { "target": "erp" },
  "high_priority": false
}
```

#### `input.kind`

支持以下两种输入类型：

| `kind`   | 必填字段             | 说明                                                                                                      |
|----------|----------------------|-----------------------------------------------------------------------------------------------------------|
| `upload` | `file_name`、`bytes` | 上传文件内容；`bytes` 为 Base64 编码。省略 `kind` 时按 `upload` 处理。                                    |
| `url`    | `url`                | 由服务端下载远程文件；需在服务配置中设置 `allow_input_url_hosts`，否则请求返回 `403 url_input_disabled`。 |

两种类型都可以提供可选的 `format` 字段，显式声明输入格式；省略时由服务端识别。
`upload` 请求不使用 `url` 字段，`url` 请求不使用 `file_name` 和 `bytes` 字段。
URL 输入仅接受白名单允许的 HTTP/HTTPS 主机，配置方式见[URL 输入默认关闭](#url-输入默认关闭)。

#### `output.kind`

`output.kind` 支持以下输出类型：

| `kind`   | 返回方式 | 产物位置          | 说明                                                             |
|----------|----------|-------------------|------------------------------------------------------------------|
| `stream` | `200`    | 不落盘            | 同步转换，直接将单个产物写入响应体；省略 `kind` 时按此类型处理。 |
| `dir`    | `202`    | 本地 `output_dir` | 异步转换，产物写入本地目录，可通过任务内容接口取回。             |
| `ftp`    | `202`    | FTP/FTPS          | 异步上传到服务端已配置的 FTP 目标。                              |
| `s3`     | `202`    | S3 兼容对象存储   | 异步上传到服务端已配置的 S3 目标。                               |
| `webdav` | `202`    | WebDAV            | 异步上传到服务端已配置的 WebDAV 目标。                           |
| `sftp`   | `202`    | SFTP              | 异步上传到服务端已配置的 SFTP 目标。                             |

远程输出类型都需要通过 `output.remote.target` 指定服务端已注册的目标名。
`stream` 仅适用于可以生成单个文件的输出格式，逐页图像格式应使用 `dir` 或远程输出。
详细规则见下文 [`stream` 是同步的，`dir` 才是异步的](#stream-是同步的dir-才是异步的)。

`input.bytes` 用 base64（JSON 里的 `[]byte` 标准编码）。注意 base64 会让
请求体膨胀约 33%，`max_upload_bytes` 要按原始文档大小留够余量。

响应：

  ```json
  {"id":"1a2b3c9d","state":"queued","lane":"fast","to":"pdf",
   "created_at":"…","status_url":"/v1/jobs/1a2b3c9d"}
  ```

  任务 ID 是**纳秒时间戳的 base36，不带前缀**。选时间戳
  而不是随机 UUID：ID 会出现在日志、通知回调和状态查询里，排查时能直接看出大致
  提交时间。当前量级下固定 12 位，`base36` 的字典序与数值
  序一致。

  ### `GET /v1/jobs/{id}`

  ```json
  {"id":"1a2b3c9d","state":"succeeded","lane":"fast","from":"ofd",
   "to":"pdf","attempt":1,"created_at":"…","started_at":"…","finished_at":"…",
   "from_actual":"ofd","input_bytes":4738,
   "output":{"kind":"stream","path":"output.pdf","size":11350}}
  ```

  失败时多一个 `error` 字段。终态才有 `output` 与 `error`。

  `from_actual` 是服务端解析后判定的输入格式，与调用方声明的 `from` 分开记。
  两者会不一致：输入格式由服务端解析、不采信文件名（见下文），而 `from` 恰好
  是那个声明值——一个 OFD 内容配了 `.html` 文件名的任务，声明是 html、实际是
  ofd。统计按实际格式归类，否则口径会被声明值污染。

  `input_bytes` 取输入落盘后的实际尺寸，覆盖上传与 URL 两种来源：提交时先记已
  收到的字节数，转换成功后用落盘尺寸覆盖，因此上传失败的任务也有值。URL 输入
  在下载失败时为 0，那时确实没有拿到完整内容。

  这两个字段属于任务记录，会随 `retention` 一起清理，只覆盖保留期内的任务。
  要做跨清理周期的累计统计，用 `/metrics` 的计数器（不参与清理）。

### `GET /v1/jobs/{id}/content`

取回 `dir` 输出的产物，需要令牌。

| 用法                | 返回                                           |
|---------------------|------------------------------------------------|
| `/content`          | 恰好一个产物时返回该文件；多个产物时打包成 zip |
| `/content?name=xxx` | 返回指定的某个产物                             |

响应头带 `Content-Type`（按扩展名查转换库的格式注册表）、`Content-Disposition`
与 `Cache-Control: no-store`。产物不该被中间层缓存：任务同名重跑后内容会变，
而 URL 不变。

多文件先在临时目录中完整生成 ZIP，再以文件流发送，不在内存里缓冲。归档中的
文件缺失、读取失败或单项超过 1 GiB 上限时，整个请求失败，不会返回缺项或被截断
的 ZIP；临时归档会在响应结束后删除。

**只服务本地 `dir` 产物。** `stream` 的内容在提交响应里当场给完了、不落盘，
没有可取的东西（`409` 并提示改用 `dir`）；远端目标的产物在 FTP/S3/WebDAV/SFTP
上，由那边的存储负责，本服务不回源（`409` 并在消息里给出远端位置）。

`name` 必须在该任务记录的 `output.files` 里精确匹配，不能直接拼到目录上。
`output.dir` 允许指向共享目录（显式指定时不追加任务 ID），同一个目录里可能躺着
别的任务的产物——按记录清单匹配既挡了 `../` 穿越，也挡了跨任务读取。清单是转换
结束时由服务端写下的，调用方改不动。

产物目录还要落在当前配置的 `output_dir` 之下。防的是"配置被改过"：把
`output_dir` 指到别处之后，历史任务记录的路径仍然有效，但已不在新配置范围内，
不该继续可读。

错误码：

| 状态  | 场景                                                        |
|-------|-------------------------------------------------------------|
| `401` | 缺令牌                                                      |
| `404` | 任务不存在；`name` 不在产物清单内；文件已被外部清理         |
| `409` | 任务未到终态；终态不是 `succeeded`；产物不是本地 `dir` 输出 |

**不支持 Range 请求**，整个文件一次性返回。浏览器内置 PDF 阅读器对小文件会
整份下载，所以够用；要支持断点续传得另做。

### `POST /v1/jobs/{id}/cancel`

取消**仍在排队**的任务，返回 `200`。已运行（正在转换）或已终结的任务返回
`409`：正在跑的任务正占着外部进程，中途杀掉只会留下半截输出。

### `GET /healthz` / `GET /readyz`

存活与就绪探针。`readyz` 返回 `503` 表示尚未就绪。

### 错误响应

统一形状：`{"code":"…","message":"…"}`。常见 `code`：

| code                    | HTTP | 含义                           |
|-------------------------|------|--------------------------------|
| `invalid_request`       | 400  | 请求体或参数不合法             |
| `url_input_disabled`    | 403  | 未配置 `allow_input_url_hosts` |
| `url_not_allowed`       | 403  | 主机/地址不在允许列表内        |
| `unknown_notify_target` | 400  | 通知目标未注册                 |
| `payload_too_large`     | 413  | 请求体超过 `max_upload_bytes`  |
| `job_not_found`         | 404  | 任务不存在                     |
| `not_cancellable`       | 409  | 任务运行中或已终结             |
| `method_not_allowed`    | 405  | 方法不匹配                     |
| `internal_error`        | 500  | 服务内部错误                   |

## 配置

完整示例见 `example-config.json`。`DisallowUnknownFields` 是开的：配置里的
拼写错误会直接报 `unknown field`，而不是静默用默认值——后者表现为"服务起来了
但参数没生效"，排查起来很费时间。

| 字段                           | 默认                 | 说明                                                     |
|--------------------------------|----------------------|----------------------------------------------------------|
| `listen`                       | `:9705`              | 监听地址，host 为空表示所有网卡                          |
| `api_key`                      | **必填**             | 访问 `/v1/*` 的 Bearer 令牌；也可用 `OFD_SERVER_API_KEY` |
| `db_path`                      | **必填**             | bbolt 任务库                                             |
| `temp_dir`                     | `$TMPDIR/ofd-server` | 转换期间的临时目录                                       |
| `output_dir`                   | **必填**             | 结果根目录                                               |
| `log_level`                    | `info`               | `debug`/`info`/`warn`/`error`                            |
| `log_dir`                      | 空（只写 stdout）    | 日志目录，自动创建。空表示不落文件                       |
| `log_file`                     | `ofd-server.log`     | `log_dir` 下的文件名                                     |
| `log_max_size_mb`              | 100                  | 单文件大小上限，超过即轮转                               |
| `log_max_backups`              | 10                   | 保留的历史文件个数                                       |
| `log_max_age_days`             | 30                   | 历史日志保留天数                                         |
| `log_compress`                 | `false`              | 是否 gzip 压缩轮转出的历史日志                           |
| `log_to_stdout`                | 跟着 `log_dir` 走    | 是否同时写标准输出，见下                                 |
| `max_upload_bytes`             | 64 MiB               | 请求体上限                                               |
| `max_stream_bytes`             | 64 MiB               | stream 输出内存上限                                      |
| `fast_workers`                 | 4                    | 快速通道并发                                             |
| `heavy_workers`                | 1                    | 重通道并发，见下                                         |
| `notify_workers`               | 4                    | 通知投递并发                                             |
| `job_timeout`                  | 0（不限）            | 单次转换超时                                             |
| `max_job_attempts`             | 0（不重试）          | 任务自动重试次数                                         |
| `job_retry_backoff`            | `5s`                 | 首次重试的等待时长，之后按次数指数翻倍                   |
| `retention`                    | 168h                 | 终态任务保留时长                                         |
| `allow_input_url_hosts`        | `[]`                 | URL 输入白名单，**默认关闭**                             |
| `url_timeout`                  | 30s                  | 拉取远程输入的超时                                       |
| `notify_targets`               | `{}`                 | 预注册通知目标                                           |
| `ftp_targets`                  | `{}`                 | 预注册 FTP/FTPS 目标                                     |
| `s3_targets`                   | `{}`                 | 预注册 S3 兼容对象存储目标                               |
| `webdav_targets`               | `{}`                 | 预注册 WebDAV 目标                                       |
| `allow_insecure_ftp`           | `false`              | 允许明文 FTP                                             |
| `allow_insecure_s3`            | `false`              | 允许 http 的对象存储                                     |
| `soffice_path` / `chrome_path` | 自动探测             | 外部转换程序路径                                         |
| `chrome_no_sandbox`            | `false`              | 给 Chrome 加 `--no-sandbox`；容器内非 root 运行时需要    |

环境变量可覆盖 `listen`、`db_path`、`temp_dir`、`output_dir`、`log_level`、
`max_upload_bytes`（前缀 `OFD_SERVER_`）。配置里没有的项不要靠环境变量补，
它们不会被校验。

## Docker

`Dockerfile` 提供 Ubuntu 24.04 运行镜像，已装 LibreOffice（Office 文档）与
Chrome（HTML），heavy 通道开箱可用。

```bash
docker build -f cmd/ofd-server/Dockerfile -t ofd-server:0.1.2 .

docker run -d --name ofd-server -p 9705:9705 \
  -e OFD_SERVER_API_KEY="$(openssl rand -hex 32)" \
  -v ofd-data:/var/lib/ofd-server \
  -v ofd-logs:/var/log/ofd-server \
  -v ofd-tmp:/var/tmp/ofd-server \
  -v "$PWD/ofd-server.json:/etc/ofd-server/config.json:ro" \
  ofd-server:0.1.2
```

镜像内置的 `docker-config.json` 把 `api_key` 留空，所以不给令牌容器会直接
拒绝启动。这是刻意的：内置一个占位符等于随镜像发布一把公开的钥匙。

镜像内已预置一份无凭据的 `docker-config.json`；挂载自己的配置即可覆盖，
不挂也能跑（仅本机目录输出，无远程目标）。arm64 主机加
`--platform linux/arm64`。

三个卷都要保留：数据库在 `/var/lib/ofd-server`（丢了会丢在途任务），
产物在 `/var/lib/ofd-server/out`，日志与临时文件分别在自己的卷里。临时目录
按任务隔离，删卷不影响结果，但会影响正在跑的转换。

镜像以 `ofd` 用户（uid 10001）运行，因此有两处和裸机部署不同：

- `chrome_no_sandbox` 默认为 `true`。Chrome 沙箱依赖 user namespace，
  容器默认不给，而 `chromedp` 只在进程 uid 为 0 时自动补 `--no-sandbox`。
  不开这个开关，HTML 任务会在 Chrome 启动阶段直接失败。代价是 Chrome 失去
  沙箱隔离，只应在容器边界内使用。
- `log_to_stdout` 默认为 `true`，日志交给 docker/journald 收集。lumberjack
  轮转文件同时开启会重复写，且在日志目录只读时启动即失败。

镜像里的字体是渲染质量的硬依赖，不是体积负担：`fonts-noto-cjk` 缺失时中文
会渲染为空白，而任务仍然成功——排版和字宽计算都正常，只有字形画不出来，
从日志上看不出来。`fonts-liberation` 补的是 Arial / Times New Roman /
Courier New 的 metric 兼容替代，OFD 文档大量引用这些字体名。
`fonts-wqy-microhei` 对应默认系统字体候选里显式列出的 `WenQuanYi Micro Hei`，
`fonts-wqy-zenhei` 作为额外的中文兜底。

## 注意事项

### 输入格式由服务端解析，不采信调用方的文件名

转换库靠扩展名区分 `docx`/`xlsx`/`pptx`/`html`，而这些格式的魔数分不开
（docx 和 xlsx 都是 ZIP 容器）。所以扩展名是**必需的**输入信息，但也正因如此
它不能随便信：一个 OFD 内容配 `.html` 名字就会被送进 HTML 导入器。

本服务的做法是先由服务端解析出可信格式（显式 `input.format` > 扩展名 >
魔数），再用该格式的规范扩展名给临时文件命名，转换库的输入路径由服务端生成。
魔数只能分出 OFD 与 PDF，分不出来时要求显式声明，不猜。

### 产物文件名

`output.file_name` 指定产物文件名的**基名**，扩展名由 `output.format` 决定：

| `file_name`        | `format` | 产物名                                 |
|--------------------|----------|----------------------------------------|
| 不填               | `pdf`    | `output.pdf`                           |
| `INV-2026-0815`    | `pdf`    | `INV-2026-0815.pdf`                    |
| `report.final.pdf` | `pdf`    | `report.final.pdf`（不重复追加扩展名） |
| `report.final`     | `pdf`    | `report.final.pdf`（基名可含点）       |
| 不填               | `png`    | `page-0001.png`、`page-0002.png`……     |
| `thumb`            | `png`    | `thumb-0001.png`、`thumb-0002.png`……   |

给基名而不是完整文件名，是因为扩展名本就由格式决定：让调用方也能指定就多了一处
可能自相矛盾的地方（声明 `pdf` 却起名 `.txt`）。逐页输出时基名替换 `page` 段、
页号保留。

产物名出现在 `GET /v1/jobs/{id}` 的 `output.files` 里，逐页输出会有多项。只报
目录等于让调用方自己猜名字或去列目录，那正是这个字段要解决的问题。

**文件名必须是单个路径组件**：不含 `/`、`\`、NUL、不是 `.` 或 `..`、不含控制
字符、基名不超过 200 字节。违反这些的请求在提交阶段就返回 `400`，不会占队列
位置。完整路径仍由服务端决定（`output_dir` 之下、每个任务一个子目录），任务
隔离不受影响。

### `stream` 是同步的，`dir` 才是异步的

`output.kind` 决定接口语义，不只是换个落点：

| kind     | 状态码 | 响应体   | 产物落点                               |
|----------|--------|----------|----------------------------------------|
| `stream` | `200`  | 产物字节 | 不落盘                                 |
| `dir`    | `202`  | 任务 ID  | `output_dir/<ID 末 2 位>/<ID 剩余段>/` |
| 远端目标 | `202`  | 任务 ID  | 由目标方存储管                         |

`kind` **省略时等同 `stream`**，大小写与首尾空白不敏感（`"STREAM"`、`" dir "` 也
认）。判定在提交阶段完成，因此省略 `kind` 与写 `kind: "stream"` 的响应完全一致。
未知的 `kind` 在提交阶段返回 `400`，不会入队。

图像格式不能走 `stream`（逐页输出塞不进单文件），这种组合——包括省略 `kind`
时的 `png`——同样在提交阶段返回 `400`，提示改用 `dir`。

`stream` 必须同步：它的产物只在内存里，异步提交的结果没有任何人能取到——任务
照样记为 `succeeded`、`size` 照样报得出来，但内容随内存里的 `Result` 一起丢掉，
转换被完整执行一遍而产出为零。

同步转换由请求自己执行，并发上限取该通道的 `fast_workers` / `heavy_workers`；
满了返回 `503` 加 `Retry-After`，而不是让调用方挂在连接上等。它不共用异步任务的
worker 池，因此两种请求互相限制不了对方的 CPU 占用。好处是客户端断连能直接取消
转换，不必再起一个监视协程。

同步转换照样写任务记录，所以 `GET /v1/jobs/{id}` 与 `GET /v1/stats` 都不会漏掉
它。响应头带 `X-Ofd-Job-Id` 与 `X-Ofd-Took-Ms`。

**同步转换不发通知**：结果已经在响应里了，再回调一次没有意义。`dir` 与远端目标
仍按配置投递通知。

失败时直接返回 `4xx`/`5xx` 与错误体，而不是 `202` 再去轮询；任务记录仍写成
`failed`，所以统计里的失败数包含同步转换。客户端主动断开记为 `cancelled` 而非
`failed`——那不是转换失败，混在一起会让失败率虚高。

### 图像格式不能用 `stream` 输出

`png`/`jpeg`/`svg` 等是逐页输出，塞不进单文件流。这类请求返回 `400`，提示改用
`output.kind: "dir"`。与其悄悄只给第一页，不如当场报错。

### `dir` 输出按任务隔离到子目录

未指定 `output.dir` 时，结果落在 `<output_dir>/<ID 末 2 位>/<ID 剩余段>/`。目录是
每个任务独占的——所有任务共用一个目录写 `output.pdf` 的话，第二个任务（哪怕顺序
执行）就会因为不允许覆盖而失败，并发时更会互相抢文件名。

切两级是**按 ID 尾部**切的（`1a2b3c9d` → `8d/1a2b3c9`），扇出 36² = 1296 个叶子
目录。取尾部而不是头部是有原因的：头部是纳秒时间戳的高位，变化极慢——实测前 2 位
字符要 42 天才变一次，拿它分目录等于所有任务落进同一处。尾部逐任务变化，20 万任务
实测铺满 1296 个取值，最大单目录占比 0.10%。

**显式指定 `output.dir` 就用调用方写的路径，任务 ID 不再拼进去**，隔离也就没有了：
落点是 `<output_dir>/<output.dir>`，多个任务写同一目录时按上面的规则撞名。要既
共享目录又避免相撞，就在 `output.dir`（或 `file_name`）里带上区分。

目录不会被自动清理。`retention` 只清任务记录，不删产物。需要回收得靠外部的
定时任务或卷的生命周期策略。

要注意**回收只能按文件时间，不能按任务**：`retention`（默认 7 天）到期后任务
记录被清掉，此后无法从服务侧判断某个文件属于哪个任务。所以外部清理只能用
mtime，且要留出足够长的时间窗——按 mtime 删和按"最后一次修改"判断是同一件事，
一个正在转换的任务的产物也在被写。

需要长期留存、或本来就有归档要求的，就直接用远端输出目标：产物落在
S3/FTP/SFTP/WebDAV 上，服务端不碰，生命周期由那边的存储策略管。

### 通道由服务端判定，请求里的 `lane` 会被拒绝

`fast` 是纯 Go 的 OFD 解析与 PDF/文本/Markdown/图像输出；`heavy` 是需要拉起
外部进程的（Office 走 LibreOffice、HTML/MHTML 走 Chrome）以及 URL 输入
（耗时由对端决定）。两者各有独立的 worker 数。

`heavy_workers` 默认 1，因为每个 heavy 任务都会拉起一个外部进程，几百 MB 内存
起步。调高之前先算一下容器的内存余量。

通道写进任务记录供队列分派，提交后不可改——事后改会让已入队的任务和实际执行的
资源池对不上。

请求体里的 `lane` 会被**拒绝**（`400`）而不是被忽略。静默忽略会让调用方以为
自己拿到了 heavy 优先级、实际走了 fast，这种"看起来生效了"的偏差比直接报错
难查得多。同理，请求体里的未知字段一律报错——包括把 `file_name` 拼错的情况：
那会让任务照常成功、产物名安静地退回 `output.pdf`。

### URL 输入默认关闭

`allow_input_url_hosts` 为空时 `input.kind: "url"` 一律返回 403。这是默认值，
不是"配置出错后的降级"。

白名单支持精确主机、`*.example.com` 子域通配、CIDR 与单个 IP。检查分两层：

- **提交时**只做准入判断（主机在不在名单内），让调用方当场拿到 403，而不是
  提交成功、轮询一圈才发现失败。字面量 IP 走 `AllowAddr`、主机名走
  `AllowedHost`——两者适用对象不同，混用会让 `10.0.0.0/8` 这类受控内网配置在
  提交阶段被误拒。
- **拨号时**才是权威检查：校验 DNS 解析后的**实际 IP**，防 DNS rebinding。

通配符不接受 `*.com`、`*.co.uk` 这类过宽的顶级域。

两个已知的边界，配置时注意：

- 白名单里的 CIDR **优先于**敏感网段黑名单。所以显式写 `0.0.0.0/0` 会放行
  `169.254.169.254`。这是"受控内网放行"的一部分，属于运维自己的决定，但要清楚
  自己在做什么。
- 白名单**只管主机**。目标站点的重定向每跳仍会重新校验，因此不会绕过限制；但
  重定向会把原始地址泄露给第三方，所以 `AllowRedirect` 默认关闭。

### 远程输出：FTP / S3 / WebDAV

除 `stream` 与 `dir`，还可以直接上传到远程存储（FTP、S3、WebDAV、SFTP），结果不落本地磁盘：

```jsonc
"output": {
  "kind": "ftp",        // "ftp" | "s3" | "webdav" | "sftp"
  "format": "pdf",
  "file_name": "INV-2026-0815",   // 可选，基名
  // 四个协议共用同一组字段：kind 已经表明用哪种协议，再按协议分字段是冗余的，
  // 而且调用方无法从字段名判断该填哪个。
  "remote": {
    "target": "archive",         // 必须是服务端已注册的目标名
    "path": "2026/09/28"         // 已注册目标下的子路径，可选
  }
}
```

`path` 的含义由目标自身的配置决定：FTP/SFTP/WebDAV 是远端 `base_dir`，S3 是
桶内 prefix。调用方不必、也无法区分。

**`target` 必须已注册**：地址、桶、凭据只存在于服务端配置里，调用方能给的只是
一个名字。如果让请求带地址，它就能被当作往任意主机上传数据的通道。

`remote` 只在 `kind` 为远端类型时有效。本地落点（`stream` / `dir`）带上它会被
拒绝，而不是静默忽略——静默忽略会让人以为产物传上了远端、其实落在本地。

逐页图像格式（`png` 等）在远程输出下同样按页分文件，命名为
`page-0001.png`、`page-0002.png`……

共同规则：

- `path` 不能是绝对路径、不能含 `..` 段。`path.Join` 会把 `..` 规整掉，所以这个
  检查必须发生在拼接**之前**，否则输出会静默落到配置范围之外。检查是**逐段**
  判断而不是整串匹配 `..`：`v1.2`、`a..b` 这类合法名字带两个点是常事，整串匹配
  会把它们一起误杀。
- 默认不覆盖同名文件/对象。要覆盖需显式 `overwrite: true`。
- 单文件大小受 `max_bytes` 限制，单位为字节，`0` 表示使用默认的 64 MiB 上限。
  限制在转换写入阶段和远端上传阶段都会执行：超限会尽早失败，不会先把完整产物
  缓存在内存中再上传——远端往往是别人的机器。

连接复用：

FTP 与 SFTP 的连接由**连接池**持有，挂在注册目标上，`WithBaseDir` 派生的子目录
实例共享它。逐任务重连的开销是 TLS/SSH 握手加认证往返，在远端可能几百毫秒；
逐页输出几百个文件时，逐任务重连的代价不可接受。

取用时先**探活**（FTP `NOOP`、SFTP `Getwd`，各一次控制通道往返），
失败就丢弃重建。这一点必须在上传**之前**：连接空闲久了会被服务端关闭，
若照发，调用方会在写到一半时才遇到坏连接——那时 `io.Reader` 已经被读掉一截，
没法重放，会变成静默的数据损坏而不是一个干净的错误。

失败时连接一律丢弃而不归还：它可能已处于半坏状态，放回池里只会让下一个
任务在写到一半时才暴露问题。

空闲连接由定时清扫器回收（默认 30s 一轮），超过 `idle_ttl` 的直接断开。
主动回收而不是等下次取用时才发现，是因为连接不占进程资源也在占服务端的会话表。

相关配置（`ftp_targets` 与 `sftp_targets` 各自一份）：

| 字段       | 默认 | 含义                         |
|------------|------|------------------------------|
| `max_idle` | `2`  | 空闲连接上限，超出的直接断开 |
| `idle_ttl` | `2m` | 连接最多空闲多久             |

`idle_ttl` 应比服务端的会话空闲超时**短**，这样回收是我们主动做的。

派生实例的 `Close()` 不会关闭共享池——一个任务收尾时把池关掉，会连带掐掉正在
上传的其它任务。只有注册目标（配置里声明的那个）才关池。

各自的安全取舍：

**FTP/FTPS**

- 默认要求 TLS（`insecure: false`）。明文 FTP 的账号密码在链路上没有任何保护，
  必须显式声明 `insecure: true`，而且还要在配置里打开 `allow_insecure_ftp`；
  启动时会记一条 WARN。
- 不信任 PASV 回报的地址。被动模式下服务器会告诉客户端"把数据连到这个地址"，
  照着连就等于让一个被攻陷的 FTP 服务器把服务的数据流引到内网任意地址。代价是
  少数配置了 PASV 外部地址的服务器用不了。
- `implicit` 区分隐式/显式 FTPS。多数部署是显式（先读 220 再 `AUTH TLS`），
  保持默认即可。
- 少数服务器没实现 EPSV（客户端默认优先试它），需要时强制走 PASV。

**S3 兼容对象存储**

- `secure: true` 走 https。S3 的签名本身防篡改，但流量与凭据仍是明文，http 需要
  显式打开 `allow_insecure_s3`。
- 桶必须已存在，本服务不负责建桶。
- 上传前先探测对象是否存在（不允许覆盖时）。认证或网络失败不会被当成"不存在"
  ——那会让覆盖检查变成假阳性，然后去覆盖别人的数据。

**WebDAV**

- 跑在 HTTP 上，强制 https（回环地址豁免，仅供本地联调）。
- 远端路径是相对 endpoint 根的，**不带前导斜杠**；这与 FTP 相反，远端 FTP 的
  `/results` 与 `results` 确实不是一回事。
- 写入用分块传输（长度未知时），大小限制由 reader 兜着。

**SFTP**

自动登录完全支持——SSH 本身没有交互式登录的概念，交互提示只对人工敲密码有意义。
三种方式按推荐程度排：

| 方式               | 密钥存放     | 说明                                            |
|--------------------|--------------|-------------------------------------------------|
| `agent_socket`     | 不进配置文件 | 最干净：私钥与已解密的口令都由 agent 持有       |
| `private_key_file` | 文件路径     | 主流做法；私钥加密时用 `private_key_passphrase` |
| `password`         | 配置文件     | 内网临时可用。配置文件里的口令等同于明文存储    |

私钥加密但没配 `private_key_passphrase` 时连接会失败并明确提示——这是有意的：
不想把口令写进配置文件，就该走 `agent_socket`。

**主机密钥校验是 SFTP 独有的开关，也是它相对明文 FTP 的核心安全优势。**
SSH 靠 host key 识别对方；只连不管的话，攻击者可以在中间人位置冒充服务器，
拿到私钥或口令、读到全部内容，而连接照样"成功"。因此：

- `host_key_sha256`：写死指纹（`ssh-keygen` 打印的那个）。最严格，推荐。
- `host_key_file`：读 OpenSSH 的 `known_hosts`。
- `insecure_ignore_host_key: true`：跳过校验。必须显式声明，启动时记 WARN。

**三者都不配时服务直接拒绝启动**，而不是退回"跳过校验"——那正是最容易出事
的情况，配置校验会把它拦下来。

其他：
- 远端路径是绝对路径，`base_dir` 的前导斜杠会被保留（与 WebDAV 相反——
  gowebdav 要相对路径，sftp 要绝对路径）。
- 超限时已写入远端的半个文件会被删除。留着截断的文件比删掉更糟：调用方看到
  文件存在就会去取，取到的是残缺内容。

### 不要往一个目录里堆太多文件

这是部署时最容易踩、也最难在开发阶段暴露的问题：**单个目录（或单个对象前缀）下的
文件数量要有上限**，不要让所有产物落在同一处。合理做法是按日期、业务或租户分层，
例如 `path` 给到 `2026/09/30/erp`，而不是让几千个文件都挂在 `archive` 根下。

先看放大倍数——一次任务产出多少文件，取决于输出格式：

| 输出格式         | 产物                                      |
|------------------|-------------------------------------------|
| `pdf`/`txt`/...  | 单文件                                    |
| `png`/`jpeg`/... | **每页一个文件**，1000 页就是 1000 个文件 |

`dir` 输出**没有**指定 `output.dir` 时按任务隔离到 `<output_dir>/<ID 末 2 位>/<ID
剩余段>/`（见上一节）；显式指定了则以调用方的路径为准，任务 ID 不再拼进去，多个
任务可能落进同一个目录。但**一个任务内部的逐页输出始终没有再分目录**：一份千页文档转 PNG，
几千个 `page-*.png` 仍然挤在同一个目录里。远端输出同理，`path` 相同就是同一个
目录/前缀。

代价分两类，本地与对象存储的表现不同，但都随文件数上升：

**本地文件系统**（`dir` 输出，或 FTP/SFTP/WebDAV 落在普通磁盘上）。目录的查找与
创建代价都随条目数上升，条目累积到很大之后，`readdir`、按名查找、创建新文件都会
明显变慢。平时几百个文件看不出来，攒到很多之后备份、`ls`、清理脚本会一起变慢。

**S3 兼容对象存储（含 MinIO）**。S3 本身没有真正的目录，`path` 只是前缀，
但**后端要为前缀维护索引**：同一前缀下的对象越多，列举和取元数据越慢。MinIO
尤其明显——单机部署时所有元数据集中在一份索引里，某个前缀堆到大量对象后，
列举该前缀会拖慢整个实例的操作，磁盘 IO 和内存都会被占住。这不是本服务的实现
问题，是 MinIO 的架构决定的，但症状会先出现在这个服务上，所以要提前规划目录层级。

顺带一提，FTP/SFTP/WebDAV 的**目录层级也有成本**，方向正好相反：建目录是每级一次
往返（FTP 每级一次 `MakeDir`，已存在时还要再列一次目录以区分 550；SFTP 与 WebDAV
每级一次 `MkdirAll`），4 级路径乘 200 个文件就是 800 次纯网络往返。本服务按 Sink
实例记住已建目录来消掉这部分重复，粒度是单个任务——所以层级也不宜过深，
够用即可。

实践上：把 `path` 当成有层级的路径来设计（`年/月/日/业务`），逐页输出量大时
再在末尾加一层页码段或按页分桶；定期把历史产物按生命周期策略下沉或归档，别让
任何一个目录无限增长。


### 通知是 at-least-once，接收方必须幂等

投递语义是"至少一次"。超时等服务端无法判定结果的场景下，接收方可能收到重复
请求。每条通知带稳定的 `X-OFD-Delivery` 头，**按它去重**。签名形如
`v1=<hex>`，覆盖 `v1.<时间戳>.<请求体>`，接收方用同一密钥重算并常量时间比较；
时间戳让同一份请求体无法被无限期重放。

回调只接受预注册的目标名，URL 与密钥留在服务端。配置校验强制 `https`，
回环地址豁免（仅供本地联调）。**正式环境务必配 `secret`**：没有它接收方无法
验证来源，任何人都能伪造回调。

### `notify.events` 收窄本任务要收的事件

```jsonc
"notify": {"target": "erp", "events": ["failed"]}   // 这个任务只报失败
"notify": {"target": "erp"}                          // 留空：不收窄
"notify": {"target": "erp", "events": ["*"]}        // 等同留空
```

事件名只有 `succeeded`、`failed` 与通配符 `*`，与配置侧
`notify_targets[].events` 同一套名字。**拼错在提交阶段返回 `400`**——否则这个
任务会一条通知都收不到，而调用方能观察到的只有"没收到"。

两处是**取交集**，不是覆盖：请求侧只能在目标愿意收的范围里再收窄，不会把
`notify_targets[].events` 显式拒掉的事件放回来。典型用法是回调地址属于下游
系统、调用方管不了服务端的 `notify_targets`，但需要「这批任务只报失败、那一批
全报」。

`cancelled` 的任务不发通知（取消走 `Cancel` 绕过记账），所以可订阅的事件只有
上面两个。

通知目标未注册时提交阶段就返回 400——调用方需要当场知道通知不会送达。
投递失败时，不可重试的（4xx、目标失效）一次就判死，可重试的（5xx、408、429、
网络错误）按指数退避重试，超过 `MaxAttempts` 转为 `abandoned` 等人工处理。

### 任务默认不自动重试

`max_job_attempts` 默认 0。因为转换大多确定性：参数错误、格式不支持、超限都
不会因为重试而变好，这些直接判死。只有超时、崩溃、磁盘瞬时不足这类才值得重试，
配置后才生效，且按指数退避（不 `sleep`——那会占住一个 worker）。

### 单节点部署

bbolt 对数据库文件加排他锁，**同一文件只能由一个进程打开**。多副本需要换
SQLite 或外部队列。

进程崩溃时留在 `running` / `inflight` 的记录会在下次启动时自动重新排队。

### 日志

日志是 JSON 行格式。**配了 `log_dir` 就只写文件，不配就只写 stdout**——两处
都写等于每行日志序列化一次、写两次，既没有额外信息也白花 I/O。确实需要两处时
（比如容器里收 stdout，同时本地留档），显式设 `log_to_stdout: true`，此时用
`io.MultiWriter` 扇出，每行只序列化一次。

轮转由 `lumberjack` 负责。服务要长跑，日志不切迟早把磁盘写满，而磁盘写满的
表现是任务静默失败，比服务直接挂掉更难查。

容器部署下通常不配 `log_dir`：runtime 收集 stdout 才是正路，`kubectl logs`
直接看，不需要额外配文件采集。落文件适合裸机部署或需要留存审计的场景。

`log_to_stdout` 用指针类型，是为了区分"未设"和"显式关掉"——`bool` 的零值
是 `false`，配上"配了目录就不写 stdout"的默认就正好反了。

一个出口都不留（`log_to_stdout: false` 且不配 `log_dir`）会在启动时报错：
日志被静默丢弃比启动失败更难排查。`--check-config` 也会检查这一项。

### 其他

- 转换过程中输入总是落盘一次（LibreOffice、Chrome 都需要真实文件路径），
  临时目录在任务结束时清理。
- 状态查询**不回显原始请求体**，里面可能有调用方上传的内容。
- 服务不返回 `Server` 头。
- 处理函数里的 panic 会被兜住并返回 500，不会拖垮进程。
- 不支持 32 位构建（`knroy/go-xml` 的限制，与本服务无关）。
- 图像输出用 Canvas/TinySkia 后端时不需要外部程序；`soffice`/`chromium` 只有在
  实际处理 Office 或 HTML 时才需要存在。

## 目录

| 路径                    | 职责                                                         |
|-------------------------|--------------------------------------------------------------|
| `cmd/ofd-server`        | HTTP 层与配置                                                |
| `internal/runner`       | 编排：双通道 worker、任务与通知循环                          |
| `internal/convertersvc` | 单次转换；输入格式解析、通道判定                             |
| `internal/jobstore`     | bbolt 任务队列、延迟重试、通知队列                           |
| `internal/notify`       | 预注册目标投递、HMAC 签名                                    |
| `internal/transfer`     | Source/Sink（内存、文件、HTTP、目录、FTP、S3、WebDAV、SFTP） |
| `internal/allowlist`    | URL 输入白名单与地址策略                                     |
