package invoice

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
)

func longSellerGroups(n int) []Group {
	groups := make([]Group, 0, n)
	for index := 1; index <= n; index++ {
		groups = append(groups, Group{
			Key:   "913100001322008" + string(rune('0'+index%10)),
			Name:  "上海市税务局第" + strings.Repeat("很长的", 6) + "税务稽查局" + string(rune('A'+index%26)),
			Count: index,
		})
	}
	return groups
}

func mustCoverModel(t *testing.T, summary Summary, title string) *creator.Document {
	t.Helper()
	model, err := coverModel(t.Context(), summary, title, coverDefaultPageWidth, coverDefaultPageHeight)
	if err != nil {
		t.Fatal(err)
	}
	return model
}

// 长名称与税号必须完整保留，不能为了固定行高截断。
func TestCoverTableWrapsLongSellerName(t *testing.T) {
	summary := Summary{Count: 2, BySeller: longSellerGroups(2)}
	text := strings.ReplaceAll(coverTextOf(t, coverOFD(t, summary, "")), "\n", "")
	for _, group := range summary.BySeller {
		if !strings.Contains(text, group.Name) || !strings.Contains(text, group.Key) {
			t.Errorf("销售方名称或税号被截断: %+v", group)
		}
	}
}

// flow 已完成分页，固定创建器只接收文字和路径，续页不得重复首页标题。
func TestCoverFlowPaginatesInsideMarginsAndRepeatsHeaders(t *testing.T) {
	model := mustCoverModel(t, Summary{Count: 200, BySeller: longSellerGroups(200)}, defaultCoverTitle)
	if len(model.Pages) < 2 {
		t.Fatal("长表未分页")
	}
	titles := 0
	for index, page := range model.Pages {
		header := false
		for _, item := range page.Items {
			switch value := item.(type) {
			case creator.Text:
				text := creatorTextValue(value)
				if text == defaultCoverTitle {
					titles++
					if index != 0 {
						t.Fatal("续页重复首页标题")
					}
				}
				header = header || text == coverHeader()[0]
				if value.Y < coverMarginTop-0.01 || value.Y+value.Height > coverDefaultPageHeight-coverMarginBottom+0.01 {
					t.Fatalf("第 %d 页文字越出版心: %+v", index+1, value)
				}
			case creator.Path:
			default:
				t.Fatalf("flow 输出包含未展开图元: %T", item)
			}
		}
		if !header {
			t.Fatalf("第 %d 页缺少重复表头", index+1)
		}
	}
	if titles != 1 {
		t.Fatalf("首页标题次数 = %d", titles)
	}
}

// 题注属于下一张表，与前表之间必须留有段后间距。
func TestCoverTablesLeaveGapAboveCaptions(t *testing.T) {
	model := mustCoverModel(t, Summary{Count: 2, BySeller: longSellerGroups(2)}, "")
	found := false
	for _, page := range model.Pages {
		for index, item := range page.Items {
			caption, ok := item.(creator.Text)
			if !ok || creatorTextValue(caption) != "按销售方汇总" {
				continue
			}
			previousBottom := 0.0
			for _, previous := range page.Items[:index] {
				if path, ok := previous.(creator.Path); ok {
					previousBottom = max(previousBottom, path.Y+path.Height)
				}
			}
			if previousBottom > 0 {
				if gap := caption.Y - previousBottom; gap < coverGap-0.01 {
					t.Fatalf("表题与前表间距不足: %.2fmm", gap)
				}
				found = true
			}
		}
	}
	if !found {
		t.Fatal("未找到可比较的相邻表")
	}
}

func creatorTextValue(text creator.Text) string {
	if len(text.TextCodes) == 0 {
		return text.Value
	}
	var value strings.Builder
	for _, code := range text.TextCodes {
		value.WriteString(code.Value)
	}
	return value.String()
}

func TestCoverModelPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := coverModel(ctx, Summary{}, "", 210, 297); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误不可识别: %v", err)
	}
}

// 流式迁移不应把逻辑中文字体改为内置 Go 字体，数值列仍须右对齐。
func TestCoverFlowPreservesLogicalFontAndRightAlignedAmounts(t *testing.T) {
	zero := mustDecimal(t, "0")
	model := mustCoverModel(t, Summary{Count: 1, Amount: zero, TaxAmount: zero, TotalAmount: zero}, "")
	if len(model.Fonts) != 1 || model.Fonts[0].Name != coverFont || len(model.Fonts[0].Data) != 0 {
		t.Fatalf("汇总页逻辑字体契约改变: %+v", model.Fonts)
	}
	rights := newCoverGeometry(coverDefaultPageWidth).widths()
	x := coverMarginLeft
	for index := range rights {
		x += rights[index]
		rights[index] = x - 1.5
	}
	amounts := 0
	for _, item := range model.Pages[0].Items {
		text, ok := item.(creator.Text)
		if !ok || creatorTextValue(text) != "0.00" {
			continue
		}
		matched := false
		for _, right := range rights[2:] {
			matched = matched || math.Abs(text.X+text.Width-right) < 0.01
		}
		if !matched {
			t.Fatalf("金额未按列右对齐: %+v", text)
		}
		amounts++
	}
	if amounts != 3 {
		t.Fatalf("三列金额不完整: %d", amounts)
	}
}

func TestCoverPageProviderRejectsOutOfRange(t *testing.T) {
	provider := coverPageProvider{pages: mustCoverModel(t, Summary{Count: 1}, "").Pages}
	for _, index := range []int{-1, provider.PageCount()} {
		if _, err := provider.PageAt(index); err == nil {
			t.Fatalf("越界页码 %d 应报错", index)
		}
	}
	if _, err := provider.PageAt(0); err != nil {
		t.Fatal(err)
	}
}
