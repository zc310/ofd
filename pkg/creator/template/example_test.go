package template_test

import (
	"fmt"
	"strings"

	ofdtemplate "github.com/zc310/ofd/pkg/creator/template"
)

func ExampleTemplate_Render() {
	tmpl, err := ofdtemplate.Parse([]byte(`
version: 1
document: {id: repeated-cards}
pages:
  - repeat: customers
    items:
      - type: text
        x: 20
        y: 20
        width: 100
        height: 10
        bind: {path: name}
`), "yaml", "")
	if err != nil {
		panic(err)
	}
	document, err := tmpl.Render(strings.NewReader(`{"customers":[{"name":"Alpha"},{"name":"Beta"}]}`), ofdtemplate.Options{})
	if err != nil {
		panic(err)
	}
	// document 可交给 creator.CreateFile 写出 OFD。
	fmt.Println(len(document.Pages))
	// Output: 2
}
