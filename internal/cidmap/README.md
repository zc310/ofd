# Adobe-GB1 共享映射

字体修复和 PDF 文字提取共用一份 Adobe-GB1 数据。`GB1GlyphUnicode` 返回字体
单码位映射，`GB1Text` 返回文字映射，保留普通标点和异体选择符序列。
例如 CID580 的字体映射是 `︵`，文字映射是 `（`；CID22031 的文字是 `郎\uFE00`。

基础 BMP 表以 zlib 嵌入，另外记录6个补充平面码位和402处文字差异。有效字体 CID
为30,010个，文字 CID 为30,284个。越界返回 false，调用方决定替换字符。
全16位 CID 输入的两种语义由 SHA-256 指纹测试保护。

来源是 poppler-data 分发的 Adobe 数据：

- `cidToUnicode/Adobe-GB1`：字体映射，Adobe mapping-resources-pdf。
- `cMap/Adobe-GB1/Adobe-GB1-UCS2`：文字映射，版本9.000。
- 上游：[mapping-resources-pdf](https://github.com/adobe-type-tools/mapping-resources-pdf)、
  [cmap-resources](https://github.com/adobe-type-tools/cmap-resources)。

原数据哈希见 [SHA256SUMS](SHA256SUMS)，Adobe BSD 许可见 [NOTICE](NOTICE)。
数据和生成脚本不包含字体修复或 PDF 阅读器的实现。

从已安装的 poppler-data 重建：

```sh
python3 internal/cidmap/generate.py /usr/share/poppler
gofmt -w internal/cidmap/gb1_data.go
go test ./internal/cidmap ./internal/pdf2ofd
```
