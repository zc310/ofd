package renderstate

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func TestRenderBudgetLimitsCompositeExpansion(t *testing.T) {
	var budget Budget
	budget.Reset()
	for i := range MaxCompositeExpansions {
		if !budget.AllowComposite(models.StID(i + 1)) {
			t.Fatalf("复合展开 %d 被拒绝", i)
		}
	}
	if budget.AllowComposite(models.StID(MaxCompositeExpansions + 1)) {
		t.Fatal("复合展开超过总上限后仍被接受")
	}
}

func TestRenderBudgetLimitsRepeatedCompositeResource(t *testing.T) {
	var budget Budget
	budget.Reset()
	for i := range MaxCompositeResourceExpansions {
		if !budget.AllowComposite(1) {
			t.Fatalf("资源重复展开 %d 被拒绝", i)
		}
	}
	if budget.AllowComposite(1) {
		t.Fatal("资源重复展开超过上限后仍被接受")
	}
}

func TestRenderBudgetLimitsPatternTilesAndOffscreenPixels(t *testing.T) {
	var budget Budget
	budget.Reset()
	if !budget.AllowPatternTiles(MaxPatternTiles - 1) {
		t.Fatal("额度内图块被拒绝")
	}
	if budget.AllowPatternTiles(2) {
		t.Fatal("超额图块被接受")
	}
	budget.Reset()
	if !budget.AllowOffscreenPixels(10, 10, 10) {
		t.Fatal("额度内离屏分配被拒绝")
	}
	if budget.AllowOffscreenPixels(1e9, 1e9, 1200) {
		t.Fatal("超额离屏分配被接受")
	}
}

func TestRenderBudgetResetClearsUsage(t *testing.T) {
	var budget Budget
	budget.Reset()
	if !budget.AllowComposite(1) || !budget.AllowPatternTiles(1) || !budget.AllowOffscreenPixels(10, 10, 10) {
		t.Fatal("消费预算失败")
	}
	budget.Report, budget.ObjectID, budget.ResourceID = &ReportBuilder{}, 2, 3
	budget.Reset()
	if budget.HasUsage() || budget.Report != nil || budget.ObjectID != 0 || budget.ResourceID != 0 || budget.CompositeCount(1) != 0 {
		t.Fatal("预算或报告位置没有重置")
	}
	if !budget.AllowComposite(1) || !budget.AllowPatternTiles(1) || !budget.AllowOffscreenPixels(10, 10, 10) {
		t.Fatal("重置后无法消费预算")
	}
}

func TestRenderBudgetDiagnosticsAreOncePerReasonAndReset(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	var budget Budget
	budget.Reset()
	budget.AllowPatternTiles(MaxPatternTiles)
	for range 3 {
		budget.AllowPatternTiles(1)
	}
	for range 3 {
		budget.AllowOffscreenPixels(1e9, 1e9, 300)
	}
	logs := output.String()
	if strings.Count(logs, `"budget":"pattern_tiles"`) != 1 || strings.Count(logs, `"budget":"offscreen_pixels"`) != 1 {
		t.Fatalf("缺失诊断或重复刷屏：%s", logs)
	}
	budget.Reset()
	budget.AllowPatternTiles(MaxPatternTiles)
	budget.AllowPatternTiles(1)
	if strings.Count(output.String(), `"budget":"pattern_tiles"`) != 2 {
		t.Fatal("重置预算没有恢复诊断")
	}
}

func TestCachedUsageRemainsReadOnlyAndChecksPerResourceLimit(t *testing.T) {
	var cached, page Budget
	cached.Reset()
	page.Reset()
	if !cached.AllowComposite(7) || !cached.AllowPatternTiles(3) || !cached.AllowOffscreenPixels(4, 1, 25.4) {
		t.Fatal("缓存用量准备失败")
	}
	want := cached.Usage()
	for range 2 {
		if !page.CanReuse(&cached) {
			t.Fatal("额度内缓存用量被拒绝")
		}
		page.AddUsage(&cached)
	}
	if cached.Usage() != want || cached.CompositeCount(7) != 1 || page.CompositeCount(7) != 2 || page.Usage().PatternTiles != 6 || page.Usage().OffscreenPixels != 2*want.OffscreenPixels {
		t.Fatal("复用缓存修改了共享用量或没有累计计费")
	}
	for range MaxCompositeResourceExpansions - 2 {
		page.AllowComposite(7)
	}
	before := page.Usage()
	if page.CanReuse(&cached) || page.Usage() != before {
		t.Fatal("资源上限被绕过，或额度检查修改了用量")
	}
}
