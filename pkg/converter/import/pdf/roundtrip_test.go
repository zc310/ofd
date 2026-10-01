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

// TestGBT33190PDFRoundTripSimilarity 检查 GBT_33190-2016.pdf 经 PDF→OFD→PDF
// 往返后的视觉相似度。PDF 重新生成后对象顺序和压缩内容本来就会变化，因此按
// 同一 DPI 栅格化后比较像素，而不是比较 PDF 二进制或文本结构。
func TestGBT33190PDFRoundTripSimilarity(t *testing.T) {
	pdftoppm, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skipf("未找到 pdftoppm，跳过 PDF 视觉相似度测试: %v", err)
	}
	original, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "standards", "GBT_33190-2016.pdf"))
	if err != nil {
		t.Fatal(err)
	}

	var ofd bytes.Buffer
	if err := converter.Convert(t.Context(), "pdf", "ofd", original, &ofd); err != nil {
		t.Fatalf("PDF 转 OFD 失败: %v", err)
	}
	var roundTrip bytes.Buffer
	if err := converter.Convert(t.Context(), "ofd", "pdf", ofd.Bytes(), &roundTrip); err != nil {
		t.Fatalf("OFD 转 PDF 失败: %v", err)
	}

	originalImages, err := testutil.RasterizePDFPages(t.Context(), pdftoppm, original, 36)
	if err != nil {
		t.Fatalf("栅格化原 PDF 失败: %v", err)
	}
	roundTripImages, err := testutil.RasterizePDFPages(t.Context(), pdftoppm, roundTrip.Bytes(), 36)
	if err != nil {
		t.Fatalf("栅格化往返 PDF 失败: %v", err)
	}
	comparison, err := testutil.ComparePageImages(originalImages, roundTripImages, 16)
	if err != nil {
		t.Fatalf("比较 PDF 页面失败: %v", err)
	}
	t.Logf("总计: 相似度 %.4f，像素匹配率 %.4f，平均绝对误差 %.2f，页数 %d",
		comparison.Similarity, comparison.PixelMatchRate, comparison.MeanAbsoluteError, comparison.PageCount)
	t.Logf("最差页面: 第 %d 页，相似度 %.4f，像素匹配率 %.4f",
		comparison.WorstPage, comparison.WorstPageSimilarity, comparison.WorstPageMatchRate)
	if comparison.Similarity < 0.90 || comparison.PixelMatchRate < 0.75 {
		t.Fatalf("PDF 往返视觉相似度过低: similarity=%.4f match_rate=%.4f",
			comparison.Similarity, comparison.PixelMatchRate)
	}
}
