# OFD Converter [![GoDoc](https://godoc.org/github.com/zc310/fastjsonrpc?status.svg)](http://godoc.org/github.com/zc310/ofd)

一个用于将 OFD 文件转换为 PDF 和图像格式的 Go 语言工具包。

## 功能特性

- ✅ **OFD 转 PDF** - 支持将 OFD 文档转换为标准的 PDF 文件
- ✅ **OFD 转 图像** - 支持将 OFD 页面转换为 PNG、JPG 等图像格式
- ✅ **多页面支持** - 支持多页面 OFD 文档的转换
- ✅ **灵活配置** - 支持自定义 DPI、背景颜色、页面选择等参数
- ✅ **高效处理** - 基于 Go 语言开发，性能优异

## 安装

```bash
go get github.com/zc310/ofd
```


## 快速开始

### OFD 转 PDF

```go
package main

import (
    "os"
    "github.com/zc310/ofd/pkg/converter"
)

func main() {
    output, _ := os.Create("output.pdf")
    defer output.Close()
    
    err := converter.PDF("input.ofd", output)
    if err != nil {
        panic(err)
    }
}
```


### OFD 转图像

#### 转换为 PNG

```go
err := converter.Image("input.ofd",
    converter.Writer(func(page int) (io.WriteCloser, error) {
        return os.Create(fmt.Sprintf("output_%d.png", page))
    }),
    converter.BgColor(color.White),
    converter.PNG(),
)
```

#### 转换为 JPG

```go
err := converter.Image("input.ofd",
    converter.Writer(func(page int) (io.WriteCloser, error) {
        return os.Create(fmt.Sprintf("output_%d.jpg", page))
    }),
    converter.BgColor(color.White),
    converter.JPG(),
    converter.Page(3),        // 指定转换特定页面
    converter.DPI(300),       // 设置输出分辨率
)
```



## 注意事项

- 背景颜色默认为白色，可根据需要调整
- 支持效果见 `input.ofd` 转换结果
- 不支持 OFD 文件内字体
- 不支持 `GBT 33190-2016` 很多标准😅。。。


## 特别感谢

本项目的开发离不开以下开源项目的启发和帮助：

- [国家标准化管理委员会发布的 GB/T 33190-2016 标准](http://std.samr.gov.cn/)
- https://github.com/GreenYun/OFD-Schema
- https://github.com/itlabers/ofd-go-reference
- https://github.com/itlabers/ofd-go
- https://github.com/xiaoqidun/ofdgo

感谢所有为开源社区做出贡献的开发者！
