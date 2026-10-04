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

// 往返视觉相似度的阈值。与 pkg/converter/import/pdf 里的 GBT33190 用例同一套
// 标准：Similarity 偏低说明整体色调或渐变走样，PixelMatchRate 偏低说明有图元
// 丢失或错位——后者更严重，所以两个都要卡。
//
// 往返本身有损：渐变降精度、文本抗锯齿变化、图像重编码。GBT33190 实测
// 落在阈值之上不少，magazine 是大版面、文字与图像密集，留的余量比它小一些。
const (
	roundTripSimilarityThreshold = 0.90
	roundTripMatchRateThreshold  = 0.75
	// 只比前几页：magazine 有 60 页，全量栅格化在这个测试里太慢，而前 3 页已经
	// 覆盖标题、导航、多栏排版与图文混排。
	roundTripComparePages = 3
)

// TestMagazinePDFRoundTripSimilarity 检查 magazine.pdf 经 PDF→OFD→PDF 往返后的
// 视觉相似度。
//
// magazine.pdf 是从真实杂志导出的大版面（16 MB），含大量文字、图像、渐变、
// 裁剪与多栏排版，是现有 fixture 里最容易暴露图元映射问题的一份：丢图、丢字、
// 坐标整体偏移、颜色错乱都会让相似度掉下来。
//
// PDF 重新生成后对象顺序与压缩内容本来就会变，所以按同一 DPI 栅格化后比像素，
// 不比二进制或文本结构——这与 import/pdf 里的 GBT33190 用例是同一思路。
func TestMagazinePDFRoundTripSimilarity(t *testing.T) {
	pdftoppm, err := exec.LookPath("pdftoppm")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("CI 环境必须安装 pdftoppm: %v", err)
		}
		t.Skipf("未找到 pdftoppm，跳过 PDF 视觉相似度测试: %v", err)
	}

	source := filepath.Join("..", "..", "..", "..", "testdata", "pdf", "magazine.pdf")
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

	originalImages, err := testutil.RasterizePDFPages(t.Context(), pdftoppm, original, 96)
	if err != nil {
		t.Fatalf("栅格化原 PDF 失败: %v", err)
	}
	roundTripImages, err := testutil.RasterizePDFPages(t.Context(), pdftoppm, roundTrip.Bytes(), 96)
	if err != nil {
		t.Fatalf("栅格化往返 PDF 失败: %v", err)
	}

	// 只取前几页比较。RasterizePDFPages 会栅格化全部页面，但比较前先截断，
	// 省掉后面几十页的逐像素计算。
	compared := min(len(roundTripImages), min(len(originalImages), roundTripComparePages))
	if compared == 0 {
		t.Fatal("没有可比较的页面")
	}
	comparison, err := testutil.ComparePageImages(
		originalImages[:compared], roundTripImages[:compared], 16)
	if err != nil {
		t.Fatalf("比较 PDF 页面失败: %v", err)
	}

	t.Logf("比较 %d 页：相似度 %.4f，像素匹配率 %.4f，平均绝对误差 %.2f",
		comparison.PageCount, comparison.Similarity,
		comparison.PixelMatchRate, comparison.MeanAbsoluteError)
	t.Logf("最差页面：第 %d 页，相似度 %.4f，像素匹配率 %.4f",
		comparison.WorstPage, comparison.WorstPageSimilarity, comparison.WorstPageMatchRate)

	if comparison.Similarity < roundTripSimilarityThreshold ||
		comparison.PixelMatchRate < roundTripMatchRateThreshold {
		t.Fatalf("magazine.pdf 往返视觉相似度过低：similarity=%.4f（阈值 %.2f）match_rate=%.4f（阈值 %.2f）最差第 %d 页",
			comparison.Similarity, roundTripSimilarityThreshold,
			comparison.PixelMatchRate, roundTripMatchRateThreshold,
			comparison.WorstPage)
	}
}
