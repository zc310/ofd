package creator_test

import (
	"bytes"
	"context"
	"fmt"
	"log"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
)

// ExampleCustomTag 演示创建 TypeID 标引并显式启用对应校验。
// 这只是标引入口示例，不代表完整电子证照合规。
func ExampleCustomTag() {
	data, err := creator.Marshal(creator.Document{
		ID: "certificate-tags",
		CustomTags: []creator.CustomTag{{
			TypeID: "cert", DataName: "Tag_Certificate.xml",
			Data: []byte(`<Certificate xmlns="urn:example:certificate"/>`),
		}},
		Pages: []creator.Page{{}},
	})
	if err != nil {
		log.Fatal(err)
	}
	instance, err := validator.New(validator.WithCustomTagTypeID(true))
	if err != nil {
		log.Fatal(err)
	}
	report := instance.ValidateReader(context.Background(), bytes.NewReader(data), "certificate.ofd")
	fmt.Println(report.HasErrors())
	// Output: false
}

// reportPages 按索引即时生成报表页面，不把全部页面保存在内存中。
// 实际使用时可以从磁盘、数据库或业务数据流中构造页面。
type reportPages struct {
	total int
}

func (p reportPages) PageCount() int { return p.total }

func (p reportPages) PageAt(index int) (creator.Page, error) {
	fill := true
	return creator.Page{
		Items: []creator.Item{
			creator.Text{
				X: 35, Y: 99, Width: 170, Height: 10,
				Value: "欢迎使用 OFD！",
				Font:  "楷体", Size: 12, Fill: &fill, FillColor: &creator.Color{R: 90, G: 100, B: 120},
			},
			creator.Text{
				X: 80, Y: 279, Width: 170, Height: 10,
				Value: fmt.Sprintf("第 %d / %d 页", index+1, p.total),
				Font:  "SimSun", Size: 7, Fill: &fill, FillColor: &creator.Color{R: 189, G: 189, B: 189},
			},
		},
	}, nil
}

// ExampleCreateWithPages 演示用 PageProvider 按需生成大量页面。
func ExampleCreateWithPages() {
	// 文档元数据与资源放在 meta 中，Pages 为空，页面由 PageProvider 提供。
	meta := creator.Document{
		ID:       "report",
		Title:    "按需生成报表",
		PageSize: creator.A4,
	}

	var output bytes.Buffer
	if err := creator.CreateWithPages(meta, reportPages{total: 1000}, &output); err != nil {
		log.Fatal(err)
	}
	fmt.Println(output.Len() > 0)
	// Output: true
}
