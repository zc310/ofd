# 测试样例

本目录中的 OFD、PDF 等测试样例由独立仓库
[`zc310/ofd-fixtures`](https://github.com/zc310/ofd-fixtures) 管理。样例清单、来源和特性说明见
[fixture 文档](https://github.com/zc310/ofd-fixtures/blob/main/docs/testdata/README.md)。

首次运行依赖这些样例的测试前，在 `ofd` 仓库根目录执行：

```sh
make fixtures
```

该命令从 `ofd-fixtures` 的 GitHub `main` 分支下载压缩包，并将其中整个 `testdata/` 目录同步到本地。
需要使用其他分支或版本时，可通过 `FIXTURES_URL` 指定对应的 GitHub archive 地址；运行需要安装
`curl` 和 `unzip`。

渲染测试还需要准备中文和西文字体。CI 镜像包含三个 `hanzi-fonts` 字体包；
本地安装命令及许可提醒见
[测试前准备字体](../docs/BUILDING.md#测试前准备字体linux)。

## 常用命令

以下命令从 `ofd` 仓库根目录运行：

```text
go run ./cmd/ofd-analyzer --format text testdata/ofd-creator/helloworld.ofd
go run ./cmd/ofd-converter -format pdf testdata/ofdrw/intro.ofd /tmp/intro.pdf
go run ./cmd/ofd-converter -format png -page 1 testdata/ofdrw/ano.ofd /tmp/ano-page-1.png
go test ./internal/render ./test -count=1
```

创建器示例的 manifest 位于 [`cmd/ofd-creator/examples`](../cmd/ofd-creator/examples)。修改这些 manifest
或对应 fixture 时，应执行 `--check`、`--validate` 和必要的严格校验。
