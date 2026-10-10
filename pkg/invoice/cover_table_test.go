package invoice

import (
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
)

// longSellerGroups 造一批销售方，名称长到会触发单元格换行。
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

// 汇总页改用 creator.Table 后，表不再自己画文字，长名称应换行而不是被截断。
//
// 此前 coverRow 按 textWidth 估算宽度、超出列宽就 ellipsize 成"某某…"，税号这
// 个对账时真正要看的字段反而看不全。
func TestCoverTableWrapsLongSellerName(t *testing.T) {
	summary := Summary{Count: 2, BySeller: longSellerGroups(2)}
	// 换行会把一个单元格拆成多个文字对象，比较前先把换行去掉。
	text := strings.ReplaceAll(coverTextOf(t, coverOFD(t, summary, "")), "\n", "")
	for _, group := range summary.BySeller {
		if !strings.Contains(text, group.Name) {
			t.Errorf("销售方名称被截断，生成结果里找不到 %q", group.Name)
		}
	}
}

// 每张表都应是 creator.Table，而不是散落的文字图元。
func TestCoverPagesUseCreatorTable(t *testing.T) {
	summary := Summary{Count: 2, BySeller: longSellerGroups(2)}
	tables := 0
	for _, page := range coverPages(summary, "", coverDefaultPageWidth, coverDefaultPageHeight) {
		for _, item := range page.Items {
			if _, ok := item.(creator.Table); ok {
				tables++
			}
		}
	}
	if tables != 2 {
		t.Errorf("合计表加按销售方汇总表应共 2 张 creator.Table，实际 %d 张", tables)
	}
}

// 表多于一页时，标题与口径说明只在首页出现，续页再画一遍会与表格叠在一起。
func TestCoverHeaderOnlyOnFirstPage(t *testing.T) {
	summary := Summary{Count: 200, BySeller: longSellerGroups(200)}
	pages := coverPages(summary, "发票汇总对账表", coverDefaultPageWidth, coverDefaultPageHeight)
	if len(pages) < 2 {
		t.Fatalf("200 个销售方应分多页，实际 %d 页", len(pages))
	}
	for index, page := range pages {
		hasTitle := false
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && text.Value == "发票汇总对账表" {
				hasTitle = true
			}
		}
		if index == 0 && !hasTitle {
			t.Error("首页应有标题")
		}
		if index > 0 && hasTitle {
			t.Errorf("第 %d 页重复出现了标题", index+1)
		}
	}
}

// 同一页上的相邻两张表之间要留得出 coverGap 的间距。
//
// 创建器把表题画在 table.Y 之上，若只按内容高度排布，下一张表的表题会正好
// 落在上一张表的下边框上——表题与上方表格贴在一起就是这么来的。
func TestCoverTablesLeaveGapAboveCaptions(t *testing.T) {
	summary := Summary{
		Count:    6,
		BySeller: longSellerGroups(3),
		ByType:   []Group{{Key: "01", Name: "增值税专用发票", Count: 4}},
		ByMonth:  []Group{{Key: "2026-07", Name: "2026-07", Count: 2}},
	}
	checked := 0
	// 两组数据：短的走单页，200 个销售方的会分页，续页首表也带表题。
	summaries := []Summary{summary, {Count: 200, BySeller: longSellerGroups(200)}}
	for _, s := range summaries {
		for pageIndex, page := range coverPages(s, "", coverDefaultPageWidth, coverDefaultPageHeight) {
			var placed []creator.Table
			for _, item := range page.Items {
				table, ok := item.(creator.Table)
				if !ok {
					continue
				}
				if captionTop := table.Y - coverCaptionBlock; table.Caption != "" && captionTop < coverMarginTop-0.01 {
					t.Errorf("第 %d 页「%s」的表题顶边 %.2fmm 越出版心上边 %.2fmm",
						pageIndex+1, table.Caption, captionTop, coverMarginTop)
				}
				if len(placed) > 0 {
					previous := placed[len(placed)-1]
					height, err := creator.TableHeight(previous, coverDefaultPageWidth)
					if err != nil {
						t.Fatalf("上一张表的高度算不出来: %v", err)
					}
					captionTop := table.Y
					if table.Caption != "" {
						captionTop -= coverCaptionBlock
					}
					if gap := captionTop - (previous.Y + height); gap < coverGap-0.01 {
						t.Errorf("第 %d 页「%s」与上一张表只隔了 %.2fmm，应至少 %.2fmm",
							pageIndex+1, table.Caption, gap, coverGap)
					}
					checked++
				}
				placed = append(placed, table)
			}
		}
	}
	if checked == 0 {
		t.Fatal("没有可比对的相邻表，样例数据不足")
	}
}

// coverPageProvider 的越界页码要报错，而不是返回零值页。
func TestCoverPageProviderRejectsOutOfRange(t *testing.T) {
	provider := coverPageProvider{pages: coverPages(Summary{Count: 1}, "", coverDefaultPageWidth, coverDefaultPageHeight)}
	if _, err := provider.PageAt(-1); err == nil {
		t.Error("负数页码应报错")
	}
	if _, err := provider.PageAt(provider.PageCount()); err == nil {
		t.Error("越界页码应报错")
	}
	if _, err := provider.PageAt(0); err != nil {
		t.Errorf("首页应能取到: %v", err)
	}
}
