package docx

import (
	"reflect"
	"slices"
	"testing"

	"github.com/zc310/ofd/internal/textdoc"
)

func TestDropAnnotatedEntriesPreservesContentAndInput(t *testing.T) {
	page := textdoc.Entry{Text: "正文", Source: textdoc.EntrySourcePage}
	template := textdoc.Entry{Text: "页眉", Source: textdoc.EntrySourceTemplate}
	annotation := textdoc.Entry{Text: "水印", Source: textdoc.EntrySourceAnnotation}
	cases := []struct {
		name    string
		entries []textdoc.Entry
		want    []textdoc.Entry
	}{
		{"空页面", nil, nil},
		{"无批注", []textdoc.Entry{template, page}, []textdoc.Entry{template, page}},
		{"批注在首尾与中间", []textdoc.Entry{annotation, template, annotation, page, annotation}, []textdoc.Entry{template, page}},
		{"批注在末尾", []textdoc.Entry{template, page, annotation}, []textdoc.Entry{template, page}},
		{"全是批注", []textdoc.Entry{annotation, annotation}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := slices.Clone(tc.entries)
			if got := dropAnnotatedEntries(tc.entries); !slices.Equal(got, tc.want) {
				t.Errorf("过滤后条目 = %+v，期望 %+v", got, tc.want)
			}
			if !slices.Equal(tc.entries, original) {
				t.Error("批注过滤修改了输入条目")
			}
		})
	}
}

func TestMergeImagesPreservesReadingOrderAndInput(t *testing.T) {
	blocks := []Block{{Kind: BlockParagraph, anchor: 10}, {Kind: BlockHeading, anchor: 20}}
	images := []textdoc.Image{{Y: 5}, {Y: 10}, {Y: 15}, {Y: 25}}
	originalBlocks, originalImages := slices.Clone(blocks), slices.Clone(images)
	got := mergeImages(blocks, images)
	wantKinds := []BlockKind{BlockImage, BlockImage, BlockParagraph, BlockImage, BlockHeading, BlockImage}
	wantAnchors := []BlockAnchor{5, 10, 10, 15, 20, 25}
	if len(got) != len(wantKinds) {
		t.Fatalf("块数量 = %d，期望 %d", len(got), len(wantKinds))
	}
	imageIndex := 0
	for i, block := range got {
		if block.Kind != wantKinds[i] || block.anchor != wantAnchors[i] {
			t.Errorf("第 %d 个块 = %+v，期望类型 %d、锚点 %g", i, block, wantKinds[i], wantAnchors[i])
		}
		if block.Kind == BlockImage {
			if block.Image == nil || !reflect.DeepEqual(*block.Image, images[imageIndex]) {
				t.Errorf("第 %d 张图片内容不匹配：%+v", imageIndex, block.Image)
			}
			imageIndex++
		}
	}
	if !reflect.DeepEqual(blocks, originalBlocks) || !reflect.DeepEqual(images, originalImages) {
		t.Error("图片合并修改了输入")
	}
}
