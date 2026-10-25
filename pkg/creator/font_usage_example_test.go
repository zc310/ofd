package creator_test

import (
	"fmt"

	"github.com/zc310/ofd/pkg/creator"
)

func ExampleDocument_PruneUnusedFonts() {
	doc := creator.Document{
		Fonts: []creator.Font{{Name: "Body"}, {Name: "Unused"}},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Text{Font: "Body", Value: "Text"},
		}}},
	}
	doc.PruneUnusedFonts()
	fmt.Println(len(doc.Fonts), doc.Fonts[0].Name)
	// Output: 1 Body
}
