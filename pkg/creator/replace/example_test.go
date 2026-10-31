package replace_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/replace"
)

func replaceExampleDocument() []byte {
	data, err := creator.Marshal(creator.Document{
		ID:       "replace-example",
		Title:    "替换前标题",
		PageSize: creator.PageSize{Width: 100, Height: 100},
		Pages:    []creator.Page{{}},
	})
	if err != nil {
		panic(err)
	}
	return data
}

func replaceExampleEntry(data []byte, name string) []byte {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		panic(err)
	}
	for _, file := range archive.File {
		if file.Name != name {
			continue
		}
		r, err := file.Open()
		if err != nil {
			panic(err)
		}
		content, readErr := io.ReadAll(r)
		closeErr := r.Close()
		if readErr != nil {
			panic(readErr)
		}
		if closeErr != nil {
			panic(closeErr)
		}
		return content
	}
	panic("未找到 OFD 条目 " + name)
}

// ExampleFiles 演示替换已有的文档元数据条目，并在输出后执行严格校验。
func ExampleFiles() {
	input := replaceExampleDocument()
	rootXML := replaceExampleEntry(input, "OFD.xml")
	updatedXML := bytes.Replace(rootXML, []byte("替换前标题"), []byte("替换后标题"), 1)
	if bytes.Equal(updatedXML, rootXML) {
		panic("文档标题未找到")
	}

	var output bytes.Buffer
	err := replace.Files(input, []replace.Operation{{
		Kind: replace.OpSet,
		Name: "OFD.xml",
		Data: updatedXML,
	}}, &output, replace.Options{Validate: true, Deterministic: true})
	if err != nil {
		panic(err)
	}
	resultXML := replaceExampleEntry(output.Bytes(), "OFD.xml")
	fmt.Println(bytes.Contains(resultXML, []byte("替换后标题")))
	// Output: true
}
