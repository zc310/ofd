# ofd-converter

OFD 文档转换命令行工具，支持将 OFD 文件转换为 PDF、纯文本和图像格式。

## 编译

```bash
go build .
```

## 用法

```
ofd-converter [选项] <输入文件> [输出文件或目录]
```

## 选项

| 选项 | 说明 |
| --- | --- |
| `-o`, `-output` | 输出文件路径或目录，多页图片时可为 `.zip` 文件或目录 |
| `-format` | 输出格式: `pdf`, `txt`, `png`, `jpg`, `svg`, `eps`, `tex` |
| `-dpi` | 输出分辨率 (1-1200)，默认 150 |
| `-page` | 指定全局页码 (从 1 开始)，0 表示全部文档体的页面 |
| `-bg` | 背景颜色: `transparent`, `white`, `black`，默认 `white` |
| `-dir` | 不压缩，将多页图片直接保存到输出目录下的多个文件 |

输出格式可通过 `-format` 指定，也可根据输出文件扩展名自动推断（`.zip` 需要显式指定 `-format`）。多个文档体按出现顺序合并，页码从所有文档体的第一张页面开始连续计算。

## 示例

### OFD 转 PDF

```bash
ofd-converter input.ofd output.pdf
```

### OFD 转纯文本

只提取页面中的文字内容，不保留字体、颜色和页面布局。不同文字对象按行输出，不同页面使用分页符分隔。

```bash
ofd-converter input.ofd output.txt
ofd-converter -format txt input.ofd output.txt
```

### 转换为指定格式的图片

```bash
ofd-converter -format png input.ofd output.png
ofd-converter -format jpg -bg white input.ofd output.jpg
ofd-converter -format svg input.ofd output.svg
```

### 转换指定页面

```bash
ofd-converter -format png -page 3 input.ofd page3.png
```

### 多页输出为 zip 包或目录

```bash
ofd-converter -format png -o pages.zip input.ofd
ofd-converter -format png -o pages/ input.ofd
ofd-converter -format png -dir -o pages input.ofd
```

### 输出到标准输出

```bash
ofd-converter input.ofd - > output.pdf
ofd-converter -format txt input.ofd - > output.txt
ofd-converter -format png -page 1 input.ofd - > page1.png
```

多页图片输出时文件名格式为 `page-0001.png` 等。
