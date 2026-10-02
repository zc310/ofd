package pdf_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/pkg/converter"
)

// intro.pdf 往返视觉相似度的阈值。与同目录的 GBT33190、magazine 用例同一套
// 判据：Similarity 偏低说明整体色调或渐变走样，PixelMatchRate 偏低说明有图元
// 丢失或错位，后者更严重，所以两个都要卡。
//
// 阈值比 GBT33190（0.90/0.75）严，因为 intro.pdf 是满幅照片叠高对比文字的
// 宣传册：第 15、16 页整页都是照片加描边文字，逐像素完全相等的比例天然很低。
// 实测 42 页在 pixelTolerance=16 下 Similarity 0.978、PixelMatchRate 0.877，
// 最差页 Similarity 0.81——那是文字重排带来的亚像素偏移，不是图元丢失
// （比对两页渲染图可见内容完整，只有文字边缘与照片色偏）。
const (
	introRoundTripSimilarityThreshold = 0.95
	introRoundTripMatchRateThreshold  = 0.80
	introRoundTripPixelTolerance      = 16
	introRoundTripDPI                 = 36
)

// TestIntroPDFRoundTripSimilarity 检查 intro.pdf 经 PDF→OFD→PDF 往返后的视觉
// 相似度。
//
// intro.pdf 是 42 页的企业宣传册，每页都是满幅照片铺底再叠中文标题与正文，
// 文字多为描边或高对比配色。它是现有 fixture 里对坐标精度最敏感的一份：裁剪区
// 算错会让整页背景图消失（Clips/Clip/Area/Path 的 Fill 缺省为 false，省略时
// 裁剪区为空），文字步进算错会让整段文字叠在首字符位置（缺 TextCode@DeltaX
// 时按零步进排版）。这两类问题在本项目渲染器里都看不出来——buildImageClipRegion
// 忽略 Fill 属性、文字另有 hmtx 回退——所以只能靠与源 PDF 的像素比对发现。
//
// PDF 重新生成后对象顺序与压缩内容本来就会变，所以按同一 DPI 栅格化后比像素，
// 不比二进制或文本结构。这与 import/pdf 里其余往返用例是同一思路。
func TestIntroPDFRoundTripSimilarity(t *testing.T) {
	pdftoppm, err := exec.LookPath("pdftoppm")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("CI 环境必须安装 pdftoppm: %v", err)
		}
		t.Skipf("未找到 pdftoppm，跳过 PDF 视觉相似度测试: %v", err)
	}

	source := filepath.Join("..", "..", "..", "..", "test", "testdata", "pdf", "intro.pdf")
	original, err := os.ReadFile(source)
	if err != nil {
		t.Skipf("缺少 %s，跳过: %v", source, err)
	}

	// 走公开 API：正是这个包把 pdf2ofd 注册成了「pdf」输入的导入器，
	// converter.Convert("pdf", "ofd") 落到 pdf2ofd.Convert 上。
	var ofd bytes.Buffer
	if err := converter.Convert(t.Context(), "pdf", "ofd", original, &ofd); err != nil {
		t.Fatalf("PDF 转 OFD 失败: %v", err)
	}
	var roundTrip bytes.Buffer
	if err := converter.Convert(t.Context(), "ofd", "pdf", ofd.Bytes(), &roundTrip); err != nil {
		t.Fatalf("OFD 转 PDF 失败: %v", err)
	}

	originalImages, err := testutil.RasterizePDFPages(t.Context(), pdftoppm, original, introRoundTripDPI)
	if err != nil {
		t.Fatalf("栅格化原 PDF 失败: %v", err)
	}
	roundTripImages, err := testutil.RasterizePDFPages(t.Context(), pdftoppm, roundTrip.Bytes(), introRoundTripDPI)
	if err != nil {
		t.Fatalf("栅格化往返 PDF 失败: %v", err)
	}

	// ComparePageImages 要求两边页数一致；页数不符本身就是往返丢页。
	if len(originalImages) != len(roundTripImages) {
		t.Fatalf("往返后页数不一致：原始 %d 页，往返 %d 页", len(originalImages), len(roundTripImages))
	}

	comparison, err := testutil.ComparePageImages(originalImages, roundTripImages, introRoundTripPixelTolerance)
	if err != nil {
		t.Fatalf("比较 PDF 页面失败: %v", err)
	}

	t.Logf("比较 %d 页：相似度 %.4f，像素匹配率 %.4f，平均绝对误差 %.2f",
		comparison.PageCount, comparison.Similarity,
		comparison.PixelMatchRate, comparison.MeanAbsoluteError)
	t.Logf("最差页面：第 %d 页，相似度 %.4f，像素匹配率 %.4f",
		comparison.WorstPage, comparison.WorstPageSimilarity, comparison.WorstPageMatchRate)

	if comparison.Similarity < introRoundTripSimilarityThreshold ||
		comparison.PixelMatchRate < introRoundTripMatchRateThreshold {
		t.Fatalf("intro.pdf 往返视觉相似度过低：similarity=%.4f（阈值 %.2f）match_rate=%.4f（阈值 %.2f）最差第 %d 页",
			comparison.Similarity, introRoundTripSimilarityThreshold,
			comparison.PixelMatchRate, introRoundTripMatchRateThreshold,
			comparison.WorstPage)
	}
}
