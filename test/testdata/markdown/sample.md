# 示例文档

这是一个 **Markdown** 转 OFD 的示例。

- 列表项一
- 列表项二

> 引用文本

| 名称 | 数量 |
| --- | ---: |
| 苹果 | 3 |


```go
package main

import (
	"log"

	"github.com/zc310/ofd/pkg/creator"
)

func main() {
	err := creator.CreateFile(creator.Document{
		ID:    "example-document",
		Title: "创建示例",
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Text{
				X:      20,
				Y:      30,
				Width:  100,
				Height: 10,
				Size:   4.233,
				Font:   "SimSun",
				Value:  "你好，OFD",
			},
		}}}},
	}, "output.ofd")
	if err != nil {
		log.Fatal(err)
	}
}
```